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
