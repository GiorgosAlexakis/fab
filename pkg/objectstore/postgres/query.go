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
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

// List returns the objects of one type matching a query.
func (s *Store) List(ctx context.Context, query objectstore.Query) ([]objectstore.Object, error) {
	objectType, err := s.binding.ObjectType(query.Type)
	if err != nil {
		return nil, err
	}

	predicate, arguments, err := s.compileFilters(objectType, query.Filters)
	if err != nil {
		return nil, err
	}

	limit, err := pageSize(query.Limit)
	if err != nil {
		return nil, err
	}
	offset := query.Offset
	if offset < 0 {
		return nil, fmt.Errorf("offset %d is negative", query.Offset)
	}

	// Ordering by object id gives paging a stable order without asking the
	// caller for a sort key. It is insertion order, which is the only ordering
	// this schema can provide for free.
	statement := fmt.Sprintf(`
		SELECT o.object_id, o.primary_key, o.created_at, o.updated_at
		FROM objects o
		WHERE %s
		ORDER BY o.object_id
		LIMIT $%d OFFSET $%d`, predicate, len(arguments)+1, len(arguments)+2)
	arguments = append(arguments, limit, offset)

	rows, err := s.db.Query(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", query.Type, err)
	}
	objectRows, err := scanObjectRows(rows, objectType.ID)
	if err != nil {
		return nil, fmt.Errorf("listing %s: %w", query.Type, err)
	}

	return s.hydrate(ctx, objectType, objectRows)
}

// Count returns how many objects match a query, ignoring Limit and Offset.
func (s *Store) Count(ctx context.Context, query objectstore.Query) (int64, error) {
	objectType, err := s.binding.ObjectType(query.Type)
	if err != nil {
		return 0, err
	}

	predicate, arguments, err := s.compileFilters(objectType, query.Filters)
	if err != nil {
		return 0, err
	}

	var count int64
	statement := fmt.Sprintf(`SELECT count(*) FROM objects o WHERE %s`, predicate)
	if err := s.db.QueryRow(ctx, statement, arguments...).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting %s: %w", query.Type, err)
	}
	return count, nil
}

// compileFilters turns a query into a WHERE clause over objects, aliased o.
//
// Each filter becomes an EXISTS over the property table rather than a join, so
// that filters compose without multiplying rows and so that each one can use the
// (prop_id, value) index on its own.
func (s *Store) compileFilters(
	objectType *objectstore.ObjectType,
	filters []objectstore.Filter,
) (string, []any, error) {
	conditions := make([]string, 0, len(filters)+1)
	arguments := make([]any, 0, len(filters)+1)

	arguments = append(arguments, objectType.ID)
	conditions = append(conditions, "o.object_type_id = $1")

	for _, filter := range filters {
		property, ok := objectType.Property(filter.Property)
		if !ok {
			return "", nil, fmt.Errorf("%s has no property %q: %w",
				objectType.QualifiedName, filter.Property, objectstore.ErrUnknownProperty)
		}
		if filter.Value == nil {
			return "", nil, fmt.Errorf("filter on %q has no value; use a nil-free filter: %w",
				filter.Property, objectstore.ErrInvalidValue)
		}

		encoded, err := objectstore.EncodeValue(property, filter.Value)
		if err != nil {
			return "", nil, err
		}

		arguments = append(arguments, property.ID, encoded)
		conditions = append(conditions, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM object_props p
			WHERE p.object_id = o.object_id AND p.prop_id = $%d AND p.value = $%d::jsonb)`,
			len(arguments)-1, len(arguments)))
	}

	return strings.Join(conditions, " AND "), arguments, nil
}

// pageSize applies the default and the ceiling to a requested limit, so that a
// caller who forgets to page cannot pull an entire object type into memory.
func pageSize(limit int) (int, error) {
	switch {
	case limit < 0:
		return 0, fmt.Errorf("limit %d is negative", limit)
	case limit == 0:
		return objectstore.DefaultQueryLimit, nil
	case limit > objectstore.MaxQueryLimit:
		return 0, fmt.Errorf("limit %d exceeds the maximum of %d", limit, objectstore.MaxQueryLimit)
	default:
		return limit, nil
	}
}

// scanObjectRows reads identity rows of one known object type.
func scanObjectRows(rows pgx.Rows, typeID int32) ([]objectRow, error) {
	defer rows.Close()

	var results []objectRow
	for rows.Next() {
		row := objectRow{typeID: typeID}
		if err := rows.Scan(&row.objectID, &row.primaryKey, &row.createdAt, &row.updatedAt); err != nil {
			return nil, err
		}
		results = append(results, row)
	}
	return results, rows.Err()
}
