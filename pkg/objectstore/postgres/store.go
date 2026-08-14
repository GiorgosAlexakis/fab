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

var _ objectstore.Interface = &Store{}

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

// Put creates or updates an object.
//
// The write merges: properties named in Set are written, everything else is left
// as it was. That is what makes concurrent writers touching different properties
// of the same object safe, and it is why removing a value has to be explicit.
func (s *Store) Put(ctx context.Context, request objectstore.PutRequest) (*objectstore.Object, error) {
	objectType, err := s.binding.ObjectType(request.Type)
	if err != nil {
		return nil, err
	}

	primaryKeyValue, ok := request.Set[objectType.PrimaryKey.Name]
	if !ok {
		return nil, fmt.Errorf("%s: the primary key property %q must be set: %w",
			objectType.QualifiedName, objectType.PrimaryKey.Name, objectstore.ErrRequiredProperty)
	}
	primaryKey, err := objectstore.UniqueKey(objectType.PrimaryKey, primaryKeyValue)
	if err != nil {
		return nil, err
	}

	// Validate everything before opening a transaction: a request that names an
	// unknown property or carries a value of the wrong type is a caller bug, and
	// should not consume a connection or a transaction id.
	encoded, err := encodeProperties(objectType, request.Set)
	if err != nil {
		return nil, err
	}
	removals, err := resolveRemovals(objectType, request.Remove)
	if err != nil {
		return nil, err
	}

	var objectID int64
	err = storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		existing, err := lockObject(ctx, tx, objectType.ID, primaryKey)
		switch {
		case err == nil && request.CreateOnly:
			return fmt.Errorf("%s: %w", objectstore.Ref{Type: request.Type, PrimaryKey: primaryKey},
				objectstore.ErrAlreadyExists)
		case err == nil:
			objectID = existing.objectID
		case errors.Is(err, objectstore.ErrNotFound):
			if err := requireCompleteObject(objectType, request.Set); err != nil {
				return err
			}
			objectID, err = insertObject(ctx, tx, objectType.ID, primaryKey)
			if err != nil {
				return err
			}
		default:
			return err
		}

		for _, property := range encoded {
			if err := writeProperty(ctx, tx, objectID, property); err != nil {
				return err
			}
		}
		for _, property := range removals {
			if err := removeProperty(ctx, tx, objectID, property); err != nil {
				return err
			}
		}

		_, err = tx.Exec(ctx, `UPDATE objects SET updated_at = now() WHERE object_id = $1`, objectID)
		if err != nil {
			return fmt.Errorf("updating object %d: %w", objectID, err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return s.GetByID(ctx, objectID)
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

// Delete removes an object, applying the delete policy each link type declares.
func (s *Store) Delete(ctx context.Context, ref objectstore.Ref) error {
	objectType, err := s.binding.ObjectType(ref.Type)
	if err != nil {
		return err
	}

	return storage.InTx(ctx, s.db, func(tx pgx.Tx) error {
		row, err := lockObject(ctx, tx, objectType.ID, ref.PrimaryKey)
		if err != nil {
			if errors.Is(err, objectstore.ErrNotFound) {
				return fmt.Errorf("%s: %w", ref, objectstore.ErrNotFound)
			}
			return err
		}
		return s.deleteObjects(ctx, tx, []int64{row.objectID})
	})
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

// encodedProperty is one validated property value ready to be written.
type encodedProperty struct {
	property  objectstore.Property
	value     []byte
	uniqueKey string
}

func encodeProperties(
	objectType *objectstore.ObjectType,
	values map[string]interface{},
) ([]encodedProperty, error) {
	encoded := make([]encodedProperty, 0, len(values))

	for name, value := range values {
		property, ok := objectType.Property(name)
		if !ok {
			return nil, fmt.Errorf("%s has no property %q: %w",
				objectType.QualifiedName, name, objectstore.ErrUnknownProperty)
		}

		payload, err := objectstore.EncodeValue(property, value)
		if err != nil {
			return nil, err
		}

		item := encodedProperty{property: property, value: payload}
		if property.Unique {
			item.uniqueKey, err = objectstore.UniqueKey(property, value)
			if err != nil {
				return nil, err
			}
		}
		encoded = append(encoded, item)
	}

	return encoded, nil
}

func resolveRemovals(objectType *objectstore.ObjectType, names []string) ([]objectstore.Property, error) {
	removals := make([]objectstore.Property, 0, len(names))

	for _, name := range names {
		property, ok := objectType.Property(name)
		if !ok {
			return nil, fmt.Errorf("%s has no property %q: %w",
				objectType.QualifiedName, name, objectstore.ErrUnknownProperty)
		}
		if name == objectType.PrimaryKey.Name {
			return nil, fmt.Errorf("%s: the primary key %q cannot be removed; delete the object instead: %w",
				objectType.QualifiedName, name, objectstore.ErrRequiredProperty)
		}
		if !property.Nullable {
			return nil, fmt.Errorf("%s.%s is not nullable and cannot be removed: %w",
				objectType.QualifiedName, name, objectstore.ErrRequiredProperty)
		}
		removals = append(removals, property)
	}

	return removals, nil
}

// requireCompleteObject checks that a new object sets every non-nullable
// property. Enforcing this only on create is deliberate: an update names the
// properties it changes, and the ones it leaves alone are already set.
func requireCompleteObject(objectType *objectstore.ObjectType, values map[string]interface{}) error {
	for name, property := range objectType.Properties {
		if property.Nullable {
			continue
		}
		if _, ok := values[name]; !ok {
			return fmt.Errorf("%s.%s is not nullable and must be set when creating the object: %w",
				objectType.QualifiedName, name, objectstore.ErrRequiredProperty)
		}
	}
	return nil
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

func insertObject(ctx context.Context, tx pgx.Tx, typeID int32, primaryKey string) (int64, error) {
	var objectID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO objects (object_type_id, primary_key) VALUES ($1, $2)
		RETURNING object_id`, typeID, primaryKey).Scan(&objectID)
	if storage.IsUniqueViolation(err) {
		// Another transaction created the same object between our lookup and
		// this insert.
		return 0, fmt.Errorf("object with primary key %q already exists: %w",
			primaryKey, objectstore.ErrAlreadyExists)
	}
	if err != nil {
		return 0, fmt.Errorf("creating object %d/%s: %w", typeID, primaryKey, err)
	}
	return objectID, nil
}

func writeProperty(ctx context.Context, tx pgx.Tx, objectID int64, item encodedProperty) error {
	if item.property.Unique {
		// Replace this object's claim on the old value before staking the new
		// one, so that updating a unique property does not collide with itself.
		if _, err := tx.Exec(ctx,
			`DELETE FROM object_prop_unique WHERE prop_id = $1 AND object_id = $2`,
			item.property.ID, objectID); err != nil {
			return fmt.Errorf("releasing the unique value of %q: %w", item.property.Name, err)
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO object_prop_unique (prop_id, value_text, object_id) VALUES ($1, $2, $3)`,
			item.property.ID, item.uniqueKey, objectID)
		if storage.IsUniqueViolation(err) {
			return fmt.Errorf("%q is declared unique and another object already has the value %q: %w",
				item.property.Name, item.uniqueKey, objectstore.ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("claiming the unique value of %q: %w", item.property.Name, err)
		}
	}

	// The WHERE clause on the conflict path keeps updated_at meaning "when this
	// value last changed" rather than "when it was last written".
	_, err := tx.Exec(ctx, `
		INSERT INTO object_props (object_id, prop_id, value, updated_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (object_id, prop_id) DO UPDATE
		    SET value = excluded.value, updated_at = now()
		    WHERE object_props.value IS DISTINCT FROM excluded.value`,
		objectID, item.property.ID, item.value)
	if err != nil {
		return fmt.Errorf("writing property %q: %w", item.property.Name, err)
	}
	return nil
}

func removeProperty(ctx context.Context, tx pgx.Tx, objectID int64, property objectstore.Property) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM object_props WHERE object_id = $1 AND prop_id = $2`,
		objectID, property.ID); err != nil {
		return fmt.Errorf("removing property %q: %w", property.Name, err)
	}
	if property.Unique {
		if _, err := tx.Exec(ctx,
			`DELETE FROM object_prop_unique WHERE prop_id = $1 AND object_id = $2`,
			property.ID, objectID); err != nil {
			return fmt.Errorf("releasing the unique value of %q: %w", property.Name, err)
		}
	}
	return nil
}
