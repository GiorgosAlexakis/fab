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

// Package postgres implements the object store on PostgreSQL.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
	"github.com/GiorgosAlexakis/fab/pkg/storage/migrate"
)

// Component is the name this schema is tracked under in fab_schema_migrations.
const Component = "objectstore"

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrations returns the embedded object store migrations.
func Migrations() ([]migrate.Migration, error) {
	return migrate.Load(migrationsFS, "migrations")
}

// Migrate brings the object store schema up to date.
//
// These are the only migrations the data plane ever needs. Adding an object type
// or a property changes the registry, not this schema.
func Migrate(ctx context.Context, db storage.Beginner) ([]migrate.Migration, error) {
	migrations, err := Migrations()
	if err != nil {
		return nil, err
	}
	return migrate.Apply(ctx, db, Component, migrations)
}

// Store is a PostgreSQL-backed object store bound to one ontology version.
type Store struct {
	db      storage.Beginner
	binding *objectstore.Binding
}

// New returns an object store that reads and writes through the given binding.
func New(db storage.Beginner, binding *objectstore.Binding) *Store {
	return &Store{db: db, binding: binding}
}

// Binding returns the ontology binding this store writes with.
func (s *Store) Binding() *objectstore.Binding {
	return s.binding
}

// objectRow is the identity and lifecycle half of an object, before its
// properties are loaded.
type objectRow struct {
	objectID   int64
	typeID     int32
	primaryKey string
	createdAt  time.Time
	updatedAt  time.Time
}

// Get returns one object by type and primary key.
func (s *Store) Get(ctx context.Context, ref objectstore.Ref) (*objectstore.Object, error) {
	objectType, err := s.binding.ObjectType(ref.Type)
	if err != nil {
		return nil, err
	}

	row := objectRow{typeID: objectType.ID, primaryKey: ref.PrimaryKey}
	err = s.db.QueryRow(ctx, `
		SELECT object_id, created_at, updated_at
		FROM objects
		WHERE object_type_id = $1 AND primary_key = $2`, objectType.ID, ref.PrimaryKey).
		Scan(&row.objectID, &row.createdAt, &row.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%s: %w", ref, objectstore.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("reading object %s: %w", ref, err)
	}

	objects, err := s.hydrate(ctx, objectType, []objectRow{row})
	if err != nil {
		return nil, err
	}
	return &objects[0], nil
}

// GetByID returns one object by its store-internal id.
func (s *Store) GetByID(ctx context.Context, objectID int64) (*objectstore.Object, error) {
	var row objectRow
	row.objectID = objectID

	err := s.db.QueryRow(ctx, `
		SELECT object_type_id, primary_key, created_at, updated_at
		FROM objects
		WHERE object_id = $1`, objectID).
		Scan(&row.typeID, &row.primaryKey, &row.createdAt, &row.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("object %d: %w", objectID, objectstore.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("reading object %d: %w", objectID, err)
	}

	objectType, err := s.binding.ObjectTypeByID(row.typeID)
	if err != nil {
		return nil, err
	}

	objects, err := s.hydrate(ctx, objectType, []objectRow{row})
	if err != nil {
		return nil, err
	}
	return &objects[0], nil
}

// hydrate loads the property values of the given object rows. Rows carrying a
// prop_id the binding does not know about are skipped: a property that a newer
// ontology version dropped is invisible through an older binding rather than an
// error.
func (s *Store) hydrate(
	ctx context.Context,
	objectType *objectstore.ObjectType,
	rows []objectRow,
) ([]objectstore.Object, error) {
	objects := make([]objectstore.Object, 0, len(rows))
	byID := make(map[int64]int, len(rows))
	ids := make([]int64, 0, len(rows))

	for _, row := range rows {
		byID[row.objectID] = len(objects)
		objects = append(objects, objectstore.Object{
			ObjectID:   row.objectID,
			Type:       objectType.QualifiedName,
			PrimaryKey: row.primaryKey,
			Properties: map[string]interface{}{},
			CreatedAt:  row.createdAt,
			UpdatedAt:  row.updatedAt,
		})
		ids = append(ids, row.objectID)
	}
	if len(ids) == 0 {
		return objects, nil
	}

	propertyRows, err := s.db.Query(ctx, `
		SELECT object_id, prop_id, value
		FROM object_props
		WHERE object_id = ANY($1)`, ids)
	if err != nil {
		return nil, fmt.Errorf("reading property values: %w", err)
	}
	defer propertyRows.Close()

	for propertyRows.Next() {
		var (
			objectID int64
			propID   int32
			raw      []byte
		)
		if err := propertyRows.Scan(&objectID, &propID, &raw); err != nil {
			return nil, fmt.Errorf("reading property values: %w", err)
		}

		property, ok := objectType.PropertyByID(propID)
		if !ok {
			continue
		}
		value, err := objectstore.DecodeValue(property, raw)
		if err != nil {
			return nil, err
		}
		if value == nil {
			continue
		}
		objects[byID[objectID]].Properties[property.Name] = value
	}
	if err := propertyRows.Err(); err != nil {
		return nil, fmt.Errorf("reading property values: %w", err)
	}

	return objects, nil
}

func lockObject(ctx context.Context, tx pgx.Tx, typeID int32, primaryKey string) (objectRow, error) {
	row := objectRow{typeID: typeID, primaryKey: primaryKey}

	err := tx.QueryRow(ctx, `
		SELECT object_id, created_at, updated_at
		FROM objects
		WHERE object_type_id = $1 AND primary_key = $2
		FOR UPDATE`, typeID, primaryKey).
		Scan(&row.objectID, &row.createdAt, &row.updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, objectstore.ErrNotFound
	}
	if err != nil {
		return row, fmt.Errorf("reading object %d/%s: %w", typeID, primaryKey, err)
	}
	return row, nil
}
