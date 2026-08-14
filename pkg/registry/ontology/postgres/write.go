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

	"github.com/jackc/pgx/v5"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// insertVersion writes one snapshot as a new ontology version.
//
// Catalog ids are allocated first and reused if they already exist, so a type or
// property keeps the same id across every version it appears in. That is the
// property the object store depends on: its rows reference prop_ids, and a
// schema change must not rewrite them.
func insertVersion(
	ctx context.Context,
	tx pgx.Tx,
	request registry.PublishRequest,
	status registry.Status,
	digest string,
) error {
	published := status != registry.StatusDraft

	var ontologyID int64
	err := tx.QueryRow(ctx, `
		INSERT INTO ontologies (name, version, status, digest, git_ref, layers, published_at)
		VALUES ($1, $2, $3, $4, nullif($5, ''), $6, CASE WHEN $7 THEN now() ELSE NULL END)
		RETURNING ontology_id`,
		request.Name, request.Version, status, digest, request.GitRef,
		request.Snapshot.Layers, published).Scan(&ontologyID)
	if err != nil {
		return fmt.Errorf("inserting ontology %s:%s: %w", request.Name, request.Version, err)
	}

	typeIDs := make(map[string]int32, len(request.Snapshot.ObjectTypes))

	for i := range request.Snapshot.ObjectTypes {
		objectType := &request.Snapshot.ObjectTypes[i]

		typeID, err := upsertTypeCatalog(ctx, tx, objectType.Layer, objectType.Name)
		if err != nil {
			return err
		}
		typeIDs[objectType.QualifiedName()] = typeID

		propertyIDs := make(map[string]int32, len(objectType.Properties))
		for j := range objectType.Properties {
			propertyID, err := upsertPropCatalog(ctx, tx, typeID, objectType.Properties[j].Name)
			if err != nil {
				return err
			}
			propertyIDs[objectType.Properties[j].Name] = propertyID
		}

		primaryKeyID, ok := propertyIDs[objectType.PrimaryKey]
		if !ok {
			// The compiler rejects a primary key that is not a declared
			// property, so reaching this means the snapshot was not compiled.
			return fmt.Errorf("object type %s declares primary key %q which is not one of its properties",
				objectType.QualifiedName(), objectType.PrimaryKey)
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ont_object_types
			    (ontology_id, type_id, layer, name, description, primary_key_prop_id)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			ontologyID, typeID, objectType.Layer, objectType.Name, objectType.Description, primaryKeyID)
		if err != nil {
			return fmt.Errorf("inserting object type %s: %w", objectType.QualifiedName(), err)
		}

		for j := range objectType.Properties {
			property := &objectType.Properties[j]
			_, err = tx.Exec(ctx, `
				INSERT INTO ont_properties
				    (ontology_id, prop_id, type_id, name, data_type, items_type,
				     enum_values, nullable, is_unique, indexed, description)
				VALUES ($1, $2, $3, $4, $5, nullif($6, ''), $7, $8, $9, $10, $11)`,
				ontologyID, propertyIDs[property.Name], typeID, property.Name, property.Type,
				property.Items, nullableStringArray(property.Values),
				property.Nullable, property.Unique, property.Indexed, property.Description)
			if err != nil {
				return fmt.Errorf("inserting property %s.%s: %w",
					objectType.QualifiedName(), property.Name, err)
			}
		}
	}

	for i := range request.Snapshot.LinkTypes {
		linkType := &request.Snapshot.LinkTypes[i]

		linkID, err := upsertLinkCatalog(ctx, tx, linkType.Layer, linkType.Name)
		if err != nil {
			return err
		}

		sourceTypeID, ok := typeIDs[linkType.Source.QualifiedName()]
		if !ok {
			return fmt.Errorf("link type %s references source object type %s which is not in this snapshot",
				linkType.QualifiedName(), linkType.Source.QualifiedName())
		}
		targetTypeID, ok := typeIDs[linkType.Target.QualifiedName()]
		if !ok {
			return fmt.Errorf("link type %s references target object type %s which is not in this snapshot",
				linkType.QualifiedName(), linkType.Target.QualifiedName())
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO ont_link_types
			    (ontology_id, link_id, layer, name, description, source_type_id, target_type_id,
			     cardinality, forward_name, reverse_name, on_source_delete)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
			ontologyID, linkID, linkType.Layer, linkType.Name, linkType.Description,
			sourceTypeID, targetTypeID, linkType.Cardinality,
			linkType.ForwardName, linkType.ReverseName, linkType.OnSourceDelete)
		if err != nil {
			return fmt.Errorf("inserting link type %s: %w", linkType.QualifiedName(), err)
		}
	}

	return nil
}

// upsertTypeCatalog returns the stable id of an object type, allocating one on
// first sight.
func upsertTypeCatalog(ctx context.Context, tx pgx.Tx, layer, name string) (int32, error) {
	var typeID int32
	err := tx.QueryRow(ctx, `
		INSERT INTO ont_type_catalog (layer, name) VALUES ($1, $2)
		ON CONFLICT (layer, name) DO UPDATE SET name = excluded.name
		RETURNING type_id`, layer, name).Scan(&typeID)
	if err != nil {
		return 0, fmt.Errorf("allocating a type id for %s: %w", snapshot.QualifiedName(layer, name), err)
	}
	return typeID, nil
}

// upsertPropCatalog returns the stable id of a property, allocating one on first
// sight.
func upsertPropCatalog(ctx context.Context, tx pgx.Tx, typeID int32, name string) (int32, error) {
	var propertyID int32
	err := tx.QueryRow(ctx, `
		INSERT INTO ont_prop_catalog (type_id, name) VALUES ($1, $2)
		ON CONFLICT (type_id, name) DO UPDATE SET name = excluded.name
		RETURNING prop_id`, typeID, name).Scan(&propertyID)
	if err != nil {
		return 0, fmt.Errorf("allocating a property id for type %d property %q: %w", typeID, name, err)
	}
	return propertyID, nil
}

// upsertLinkCatalog returns the stable id of a link type, allocating one on
// first sight.
func upsertLinkCatalog(ctx context.Context, tx pgx.Tx, layer, name string) (int32, error) {
	var linkID int32
	err := tx.QueryRow(ctx, `
		INSERT INTO ont_link_catalog (layer, name) VALUES ($1, $2)
		ON CONFLICT (layer, name) DO UPDATE SET name = excluded.name
		RETURNING link_id`, layer, name).Scan(&linkID)
	if err != nil {
		return 0, fmt.Errorf("allocating a link id for %s: %w", snapshot.QualifiedName(layer, name), err)
	}
	return linkID, nil
}

// nullableStringArray stores an empty list as NULL, so that a round trip
// through the registry returns nil rather than an empty slice.
func nullableStringArray(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
