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

// Package migrate applies the embedded SQL migrations that create the registry
// and object store schemas.
//
// These schemas are platform infrastructure: they are written by hand, shipped
// with the binary, and change only when fab changes. They are not the
// per-object-type migrations described in the plans -- those are generated from
// the ontology and owned by Atlas.
package migrate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// advisoryLockKey serialises migration runs across processes. Any constant
// works as long as every fab binary uses the same one.
const advisoryLockKey int64 = 0x6661625f6d6967 // "fab_mig"

// migrationFilePattern matches "0001_create_registry.sql".
var migrationFilePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// Migration is one versioned SQL migration.
type Migration struct {
	// Version orders migrations within a component.
	Version int
	// Name is the human-readable part of the file name.
	Name string
	// SQL is the migration body.
	SQL string
	// Checksum is the digest of SQL, recorded so that editing an applied
	// migration is detected instead of silently diverging.
	Checksum string
}

// Load reads migrations from dir within fsys, ordered by version.
func Load(fsys fs.FS, dir string) ([]Migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading migrations from %s: %w", dir, err)
	}

	var migrations []Migration
	seen := map[int]string{}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		matches := migrationFilePattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			return nil, fmt.Errorf("migration %s does not match NNNN_name.sql", entry.Name())
		}

		version, err := strconv.Atoi(matches[1])
		if err != nil {
			return nil, fmt.Errorf("migration %s has an unparseable version: %w", entry.Name(), err)
		}
		if previous, ok := seen[version]; ok {
			return nil, fmt.Errorf("migrations %s and %s share version %d", previous, entry.Name(), version)
		}
		seen[version] = entry.Name()

		body, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", entry.Name(), err)
		}
		sum := sha256.Sum256(body)

		migrations = append(migrations, Migration{
			Version:  version,
			Name:     matches[2],
			SQL:      string(body),
			Checksum: "sha256:" + hex.EncodeToString(sum[:]),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	return migrations, nil
}

// Apply brings component up to date and returns the migrations it applied.
//
// The whole run happens in one transaction, holding a lock no other migrating
// process can take. Servers start together and each brings its own schema up to
// date, so concurrent runs are the normal case rather than the exception: two of
// them racing must not both decide the same migration is pending, and neither
// must leave a half-migrated schema behind if it fails.
func Apply(ctx context.Context, db storage.Beginner, component string, migrations []Migration) ([]Migration, error) {
	var ran []Migration

	err := storage.InTx(ctx, db, func(tx pgx.Tx) error {
		// A transaction-scoped lock is released by the commit or the rollback,
		// so an exiting process cannot leave the lock held.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", advisoryLockKey); err != nil {
			return fmt.Errorf("acquiring the migration lock: %w", err)
		}
		if err := createBookkeepingTable(ctx, tx); err != nil {
			return err
		}

		applied, err := appliedChecksums(ctx, tx, component)
		if err != nil {
			return err
		}

		for _, migration := range migrations {
			if checksum, ok := applied[migration.Version]; ok {
				if checksum != migration.Checksum {
					return fmt.Errorf(
						"migration %04d_%s has already been applied to %s with a different checksum: "+
							"applied migrations must never be edited; add a new migration instead",
						migration.Version, migration.Name, component)
				}
				continue
			}

			if _, err := tx.Exec(ctx, migration.SQL); err != nil {
				return fmt.Errorf("applying migration %04d_%s: %w", migration.Version, migration.Name, err)
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO fab_schema_migrations (component, version, name, checksum)
				 VALUES ($1, $2, $3, $4)`,
				component, migration.Version, migration.Name, migration.Checksum)
			if err != nil {
				return fmt.Errorf("recording migration %04d_%s: %w", migration.Version, migration.Name, err)
			}
			ran = append(ran, migration)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ran, nil
}

// Pending returns the migrations that have not yet been applied to component.
func Pending(ctx context.Context, db storage.DB, component string, migrations []Migration) ([]Migration, error) {
	applied, err := appliedChecksums(ctx, db, component)
	if err != nil {
		return nil, err
	}

	var pending []Migration
	for _, migration := range migrations {
		if _, ok := applied[migration.Version]; !ok {
			pending = append(pending, migration)
		}
	}
	return pending, nil
}

func createBookkeepingTable(ctx context.Context, db storage.DB) error {
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS fab_schema_migrations (
		    component  text        NOT NULL,
		    version    int         NOT NULL,
		    name       text        NOT NULL,
		    checksum   text        NOT NULL,
		    applied_at timestamptz NOT NULL DEFAULT now(),
		    PRIMARY KEY (component, version)
		)`)
	if err != nil {
		return fmt.Errorf("creating fab_schema_migrations: %w", err)
	}
	return nil
}

func appliedChecksums(ctx context.Context, db storage.DB, component string) (map[int]string, error) {
	rows, err := db.Query(ctx,
		`SELECT version, checksum FROM fab_schema_migrations WHERE component = $1`, component)
	if err != nil {
		return nil, fmt.Errorf("reading applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[int]string{}
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("reading applied migrations: %w", err)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading applied migrations: %w", err)
	}
	return applied, nil
}
