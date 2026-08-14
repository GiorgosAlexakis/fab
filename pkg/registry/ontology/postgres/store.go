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

// Package postgres implements the ontology registry on PostgreSQL.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
	"github.com/GiorgosAlexakis/fab/pkg/storage/migrate"
)

// Component is the name this schema is tracked under in fab_schema_migrations.
const Component = "registry"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the embedded registry migrations.
func Migrations() ([]migrate.Migration, error) {
	return migrate.Load(migrationsFS, "migrations")
}

// Migrate brings the registry schema up to date.
func Migrate(ctx context.Context, db storage.Beginner) ([]migrate.Migration, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	return migrate.Apply(ctx, db, Component, migrations)
}

// Store is a PostgreSQL-backed ontology registry.
type Store struct {
	db storage.Beginner
}

var _ registry.Interface = &Store{}

// New returns a registry backed by db.
func New(db storage.Beginner) *Store {
	return &Store{db: db}
}

// Publish stores a compiled snapshot as a version of an ontology.
//
// Publishing is idempotent for identical content: re-running a release pipeline
// is safe. Publishing different content under a version that is already
// published is refused, because a published version is what other environments
// and generated clients are pinned to.
func (s *Store) Publish(ctx context.Context, request registry.PublishRequest) (*registry.Ontology, error) {
	if request.Name == "" {
		return nil, errors.New("ontology name is required")
	}
	if request.Version == "" {
		return nil, errors.New("version is required")
	}
	if request.Snapshot == nil {
		return nil, errors.New("snapshot is required")
	}

	digest, err := request.Snapshot.Digest()
	if err != nil {
		return nil, err
	}

	status := registry.StatusPublished
	if request.Draft {
		status = registry.StatusDraft
	}

	err = storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		existingID, existingStatus, existingDigest, err := lockVersion(ctx, tx, request.Name, request.Version)
		switch {
		case err == nil && existingStatus == registry.StatusDraft:
			// Drafts are working copies: replacing one drops its rows and
			// rewrites them. Cascades take the type and property rows with it.
			if _, err := tx.Exec(ctx, `DELETE FROM ontologies WHERE ontology_id = $1`, existingID); err != nil {
				return fmt.Errorf("replacing draft %s:%s: %w", request.Name, request.Version, err)
			}
		case err == nil && existingDigest == digest:
			// Same content, already published: nothing to do.
			return nil
		case err == nil:
			return fmt.Errorf("%s:%s is already %s with digest %s: %w",
				request.Name, request.Version, existingStatus, existingDigest, registry.ErrAlreadyExists)
		case errors.Is(err, registry.ErrNotFound):
		default:
			return err
		}

		return insertVersion(ctx, tx, request, status, digest)
	})
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, request.Name, request.Version)
}

// Get returns the metadata of one version.
func (s *Store) Get(ctx context.Context, name, version string) (*registry.Ontology, error) {
	row := s.db.QueryRow(ctx, selectOntologySQL+`
		WHERE o.name = $1 AND o.version = $2
		GROUP BY o.ontology_id`, name, version)

	result, _, err := scanOntology(row)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, fmt.Errorf("ontology %s:%s: %w", name, version, registry.ErrNotFound)
		}
		return nil, err
	}
	return result, nil
}

// List returns every version of an ontology, newest first.
func (s *Store) List(ctx context.Context, name string) ([]registry.Ontology, error) {
	rows, err := s.db.Query(ctx, selectOntologySQL+`
		WHERE o.name = $1
		GROUP BY o.ontology_id
		ORDER BY o.created_at DESC, o.ontology_id DESC`, name)
	if err != nil {
		return nil, fmt.Errorf("listing ontology versions: %w", err)
	}
	defer rows.Close()

	var results []registry.Ontology
	for rows.Next() {
		result, _, err := scanOntology(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing ontology versions: %w", err)
	}
	return results, nil
}

// Resolve returns the version a tag points at.
func (s *Store) Resolve(ctx context.Context, name, tag string) (*registry.Ontology, error) {
	row := s.db.QueryRow(ctx, selectOntologySQL+`
		WHERE o.name = $1
		  AND o.ontology_id = (SELECT ontology_id FROM ontology_tags WHERE name = $1 AND tag = $2)
		GROUP BY o.ontology_id`, name, tag)

	result, _, err := scanOntology(row)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, fmt.Errorf("tag %q of ontology %s: %w", tag, name, registry.ErrNotFound)
		}
		return nil, err
	}
	return result, nil
}

// GetSnapshot returns the compiled ontology of one version.
func (s *Store) GetSnapshot(ctx context.Context, name, version string) (*snapshot.Snapshot, error) {
	meta, err := s.Get(ctx, name, version)
	if err != nil {
		return nil, err
	}
	return s.readSnapshot(ctx, name, version, meta)
}

// ResolveSnapshot returns the compiled ontology a tag points at.
func (s *Store) ResolveSnapshot(ctx context.Context, name, tag string) (*snapshot.Snapshot, error) {
	meta, err := s.Resolve(ctx, name, tag)
	if err != nil {
		return nil, err
	}
	return s.readSnapshot(ctx, name, meta.Version, meta)
}

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

const selectOntologySQL = `
	SELECT o.ontology_id,
	       o.name,
	       o.version,
	       o.status,
	       o.digest,
	       coalesce(o.git_ref, ''),
	       o.layers,
	       o.created_at,
	       o.published_at,
	       coalesce(array_agg(t.tag ORDER BY t.tag) FILTER (WHERE t.tag IS NOT NULL), '{}') AS tags
	FROM ontologies o
	LEFT JOIN ontology_tags t ON t.name = o.name AND t.ontology_id = o.ontology_id`

// scanner is satisfied by both pgx.Row and pgx.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanOntology(row scanner) (*registry.Ontology, int64, error) {
	var (
		ontologyID  int64
		result      registry.Ontology
		publishedAt *time.Time
	)

	err := row.Scan(
		&ontologyID,
		&result.Name,
		&result.Version,
		&result.Status,
		&result.Digest,
		&result.GitRef,
		&result.Layers,
		&result.CreatedAt,
		&publishedAt,
		&result.Tags,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, registry.ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("reading ontology metadata: %w", err)
	}

	result.PublishedAt = publishedAt
	if len(result.Tags) == 0 {
		result.Tags = nil
	}
	return &result, ontologyID, nil
}

// lockVersion reads a version's identity and takes a row lock, so that
// concurrent publishes of the same version serialise instead of racing.
func lockVersion(ctx context.Context, tx pgx.Tx, name, version string) (int64, registry.Status, string, error) {
	var (
		ontologyID int64
		status     registry.Status
		digest     string
	)
	err := tx.QueryRow(ctx,
		`SELECT ontology_id, status, digest FROM ontologies
		 WHERE name = $1 AND version = $2 FOR UPDATE`, name, version).
		Scan(&ontologyID, &status, &digest)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", "", registry.ErrNotFound
	}
	if err != nil {
		return 0, "", "", fmt.Errorf("reading ontology %s:%s: %w", name, version, err)
	}
	return ontologyID, status, digest, nil
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
