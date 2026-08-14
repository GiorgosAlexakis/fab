/*
Copyright The FAB Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// Tagging completes the registry contract, so the compile-time check that the
// store implements it lives here.
var _ registry.Interface = &Store{}

// Tag points a tag at a published version.
func (s *Store) Tag(ctx context.Context, name, tag, version string) (*registry.Ontology, error) {
	if tag == "" {
		return nil, errors.New("tag is required")
	}

	err := storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		ontologyID, status, _, err := lockVersion(ctx, tx, name, version)
		if err != nil {
			if errors.Is(err, registry.ErrNotFound) {
				return fmt.Errorf("ontology %s:%s: %w", name, version, registry.ErrNotFound)
			}
			return err
		}
		if status == registry.StatusDraft {
			return fmt.Errorf("%s:%s is a draft: publish it before tagging %q: %w",
				name, version, tag, registry.ErrNotPublished)
		}
		return moveTag(ctx, tx, name, tag, ontologyID)
	})
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, name, version)
}

// Promote points toTag at whatever fromTag currently points at. The swap is
// atomic: readers see either the old or the new version, never neither.
func (s *Store) Promote(ctx context.Context, name, fromTag, toTag string) (*registry.Ontology, error) {
	if fromTag == "" || toTag == "" {
		return nil, errors.New("both a source and a target tag are required")
	}
	if fromTag == toTag {
		return nil, fmt.Errorf("cannot promote %q onto itself", fromTag)
	}

	var version string
	err := storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		ontologyID, resolvedVersion, err := lockTag(ctx, tx, name, fromTag)
		if err != nil {
			return err
		}
		version = resolvedVersion
		return moveTag(ctx, tx, name, toTag, ontologyID)
	})
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, name, version)
}

// Rollback returns a tag to the version it pointed at before its last move.
func (s *Store) Rollback(ctx context.Context, name, tag string) (*registry.Ontology, error) {
	var version string

	err := storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		currentID, _, err := lockTag(ctx, tx, name, tag)
		if err != nil {
			return err
		}

		// The previous value of the tag is the most recent event that pointed
		// somewhere else. Reading it from history rather than inferring it from
		// version ordering means rollback works even for a tag that was moved
		// backwards.
		var previousID int64
		err = tx.QueryRow(ctx, `
			SELECT ontology_id
			FROM ontology_tag_events
			WHERE name = $1 AND tag = $2 AND ontology_id <> $3
			ORDER BY event_id DESC
			LIMIT 1`, name, tag, currentID).Scan(&previousID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("tag %q of ontology %s has only ever pointed at one version: %w",
				tag, name, registry.ErrNoPreviousVersion)
		}
		if err != nil {
			return fmt.Errorf("reading tag history: %w", err)
		}

		if err := tx.QueryRow(ctx,
			`SELECT version FROM ontologies WHERE ontology_id = $1`, previousID).Scan(&version); err != nil {
			return fmt.Errorf("reading the previous version: %w", err)
		}

		return moveTag(ctx, tx, name, tag, previousID)
	})
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, name, version)
}

// Deprecate marks a published version as no longer recommended. It stays
// readable, because clients pinned to it must keep working.
func (s *Store) Deprecate(ctx context.Context, name, version string) (*registry.Ontology, error) {
	err := storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		ontologyID, status, _, err := lockVersion(ctx, tx, name, version)
		if err != nil {
			if errors.Is(err, registry.ErrNotFound) {
				return fmt.Errorf("ontology %s:%s: %w", name, version, registry.ErrNotFound)
			}
			return err
		}
		if status == registry.StatusDraft {
			return fmt.Errorf("%s:%s is a draft, not a published version: %w",
				name, version, registry.ErrNotPublished)
		}
		_, err = tx.Exec(ctx,
			`UPDATE ontologies SET status = $2 WHERE ontology_id = $1`,
			ontologyID, registry.StatusDeprecated)
		if err != nil {
			return fmt.Errorf("deprecating %s:%s: %w", name, version, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, name, version)
}

func lockTag(ctx context.Context, tx pgx.Tx, name, tag string) (int64, string, error) {
	var (
		ontologyID int64
		version    string
	)
	err := tx.QueryRow(ctx, `
		SELECT t.ontology_id, o.version
		FROM ontology_tags t
		JOIN ontologies o ON o.ontology_id = t.ontology_id
		WHERE t.name = $1 AND t.tag = $2
		FOR UPDATE OF t`, name, tag).Scan(&ontologyID, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", fmt.Errorf("tag %q of ontology %s: %w", tag, name, registry.ErrNotFound)
	}
	if err != nil {
		return 0, "", fmt.Errorf("reading tag %q: %w", tag, err)
	}
	return ontologyID, version, nil
}

// moveTag points a tag at an ontology and records the move in the tag history.
func moveTag(ctx context.Context, tx pgx.Tx, name, tag string, ontologyID int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ontology_tags (name, tag, ontology_id, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (name, tag) DO UPDATE SET ontology_id = EXCLUDED.ontology_id, updated_at = now()`,
		name, tag, ontologyID)
	if err != nil {
		return fmt.Errorf("pointing tag %q at ontology %d: %w", tag, ontologyID, err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO ontology_tag_events (name, tag, ontology_id) VALUES ($1, $2, $3)`,
		name, tag, ontologyID)
	if err != nil {
		return fmt.Errorf("recording the move of tag %q: %w", tag, err)
	}
	return nil
}
