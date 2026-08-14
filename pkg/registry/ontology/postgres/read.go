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

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Dictionary returns the stable type and property identities of a version.
func (s *Store) Dictionary(ctx context.Context, name, version string) (*registry.Dictionary, error) {
	meta, ontologyID, err := s.getWithID(ctx, name, version)
	if err != nil {
		return nil, err
	}
	return s.readDictionary(ctx, ontologyID, meta)
}

// ResolveDictionary returns the stable identities of the version a tag points at.
func (s *Store) ResolveDictionary(ctx context.Context, name, tag string) (*registry.Dictionary, error) {
	meta, err := s.Resolve(ctx, name, tag)
	if err != nil {
		return nil, err
	}
	return s.Dictionary(ctx, name, meta.Version)
}

func (s *Store) getWithID(ctx context.Context, name, version string) (*registry.Ontology, int64, error) {
	row := s.db.QueryRow(ctx, selectOntologySQL+`
		WHERE o.name = $1 AND o.version = $2
		GROUP BY o.ontology_id`, name, version)

	meta, ontologyID, err := scanOntology(row)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, 0, fmt.Errorf("ontology %s:%s: %w", name, version, registry.ErrNotFound)
		}
		return nil, 0, err
	}
	return meta, ontologyID, nil
}

// objectTypeRow is one row of ont_object_types before its properties are
// attached.
type objectTypeRow struct {
	typeID       int32
	primaryKeyID int32
	objectType   snapshot.ObjectType
}

// readSnapshot rebuilds a compiled ontology from the normalized registry rows.
//
// The registry stores no copy of the original YAML or of the compiled JSON: the
// rows are the only representation, and the digest recorded at publish time is
// what proves the reconstruction is faithful.
func (s *Store) readSnapshot(
	ctx context.Context,
	name, version string,
	meta *registry.Ontology,
) (*snapshot.Snapshot, error) {
	_, ontologyID, err := s.getWithID(ctx, name, version)
	if err != nil {
		return nil, err
	}

	typeRows, err := s.readObjectTypes(ctx, ontologyID)
	if err != nil {
		return nil, err
	}
	propertyNames, err := s.attachProperties(ctx, ontologyID, typeRows)
	if err != nil {
		return nil, err
	}

	result := &snapshot.Snapshot{
		Layers:      meta.Layers,
		ObjectTypes: make([]snapshot.ObjectType, 0, len(typeRows)),
	}
	for _, typeRow := range typeRows {
		primaryKey, ok := propertyNames[typeRow.primaryKeyID]
		if !ok {
			return nil, fmt.Errorf("object type %s references primary key property %d which has no row in this version",
				typeRow.objectType.QualifiedName(), typeRow.primaryKeyID)
		}
		typeRow.objectType.PrimaryKey = primaryKey
		result.ObjectTypes = append(result.ObjectTypes, typeRow.objectType)
	}

	result.LinkTypes, err = s.readLinkTypes(ctx, ontologyID)
	if err != nil {
		return nil, err
	}

	result.Normalize()

	digest, err := result.Digest()
	if err != nil {
		return nil, err
	}
	if digest != meta.Digest {
		return nil, fmt.Errorf("%s:%s reconstructs to %s but was published as %s: %w",
			name, version, digest, meta.Digest, registry.ErrDigestMismatch)
	}

	return result, nil
}

func (s *Store) readObjectTypes(ctx context.Context, ontologyID int64) ([]*objectTypeRow, error) {
	rows, err := s.db.Query(ctx, `
		SELECT type_id, layer, name, description, primary_key_prop_id
		FROM ont_object_types
		WHERE ontology_id = $1
		ORDER BY layer, name`, ontologyID)
	if err != nil {
		return nil, fmt.Errorf("reading object types: %w", err)
	}
	defer rows.Close()

	var typeRows []*objectTypeRow
	for rows.Next() {
		typeRow := &objectTypeRow{}
		if err := rows.Scan(
			&typeRow.typeID,
			&typeRow.objectType.Layer,
			&typeRow.objectType.Name,
			&typeRow.objectType.Description,
			&typeRow.primaryKeyID,
		); err != nil {
			return nil, fmt.Errorf("reading object types: %w", err)
		}
		typeRows = append(typeRows, typeRow)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading object types: %w", err)
	}
	return typeRows, nil
}

// attachProperties fills in each type's properties and returns the property id
// to name mapping, which the caller needs to resolve primary keys.
func (s *Store) attachProperties(
	ctx context.Context,
	ontologyID int64,
	typeRows []*objectTypeRow,
) (map[int32]string, error) {
	byTypeID := make(map[int32]*objectTypeRow, len(typeRows))
	for _, typeRow := range typeRows {
		byTypeID[typeRow.typeID] = typeRow
	}

	rows, err := s.db.Query(ctx, `
		SELECT prop_id, type_id, name, data_type, coalesce(items_type, ''), enum_values,
		       nullable, is_unique, indexed, description
		FROM ont_properties
		WHERE ontology_id = $1
		ORDER BY type_id, name`, ontologyID)
	if err != nil {
		return nil, fmt.Errorf("reading properties: %w", err)
	}
	defer rows.Close()

	propertyNames := map[int32]string{}
	for rows.Next() {
		var (
			propertyID int32
			typeID     int32
			property   snapshot.Property
		)
		if err := rows.Scan(
			&propertyID,
			&typeID,
			&property.Name,
			&property.Type,
			&property.Items,
			&property.Values,
			&property.Nullable,
			&property.Unique,
			&property.Indexed,
			&property.Description,
		); err != nil {
			return nil, fmt.Errorf("reading properties: %w", err)
		}

		typeRow, ok := byTypeID[typeID]
		if !ok {
			return nil, fmt.Errorf("property %q belongs to type %d which has no row in this version",
				property.Name, typeID)
		}
		typeRow.objectType.Properties = append(typeRow.objectType.Properties, property)
		propertyNames[propertyID] = property.Name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading properties: %w", err)
	}
	return propertyNames, nil
}

func (s *Store) readLinkTypes(ctx context.Context, ontologyID int64) ([]snapshot.LinkType, error) {
	rows, err := s.db.Query(ctx, `
		SELECT l.layer, l.name, l.description,
		       source.layer, source.name,
		       target.layer, target.name,
		       l.cardinality, l.forward_name, l.reverse_name, l.on_source_delete
		FROM ont_link_types l
		JOIN ont_type_catalog source ON source.type_id = l.source_type_id
		JOIN ont_type_catalog target ON target.type_id = l.target_type_id
		WHERE l.ontology_id = $1
		ORDER BY l.layer, l.name`, ontologyID)
	if err != nil {
		return nil, fmt.Errorf("reading link types: %w", err)
	}
	defer rows.Close()

	var linkTypes []snapshot.LinkType
	for rows.Next() {
		var linkType snapshot.LinkType
		if err := rows.Scan(
			&linkType.Layer,
			&linkType.Name,
			&linkType.Description,
			&linkType.Source.Layer,
			&linkType.Source.Type,
			&linkType.Target.Layer,
			&linkType.Target.Type,
			&linkType.Cardinality,
			&linkType.ForwardName,
			&linkType.ReverseName,
			&linkType.OnSourceDelete,
		); err != nil {
			return nil, fmt.Errorf("reading link types: %w", err)
		}
		linkTypes = append(linkTypes, linkType)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading link types: %w", err)
	}
	return linkTypes, nil
}

func (s *Store) readDictionary(
	ctx context.Context,
	ontologyID int64,
	meta *registry.Ontology,
) (*registry.Dictionary, error) {
	dictionary := &registry.Dictionary{
		Ontology:      *meta,
		Types:         map[string]int32{},
		TypeNames:     map[int32]string{},
		Properties:    map[int32]map[string]int32{},
		PropertyNames: map[int32]string{},
		PropertyTypes: map[int32]string{},
		PrimaryKeys:   map[int32]int32{},
	}

	typeRows, err := s.db.Query(ctx, `
		SELECT type_id, layer, name, primary_key_prop_id
		FROM ont_object_types
		WHERE ontology_id = $1`, ontologyID)
	if err != nil {
		return nil, fmt.Errorf("reading the type dictionary: %w", err)
	}
	defer typeRows.Close()

	for typeRows.Next() {
		var (
			typeID       int32
			layer        string
			name         string
			primaryKeyID int32
		)
		if err := typeRows.Scan(&typeID, &layer, &name, &primaryKeyID); err != nil {
			return nil, fmt.Errorf("reading the type dictionary: %w", err)
		}
		qualifiedName := snapshot.QualifiedName(layer, name)
		dictionary.Types[qualifiedName] = typeID
		dictionary.TypeNames[typeID] = qualifiedName
		dictionary.PrimaryKeys[typeID] = primaryKeyID
	}
	if err := typeRows.Err(); err != nil {
		return nil, fmt.Errorf("reading the type dictionary: %w", err)
	}
	typeRows.Close()

	propertyRows, err := s.db.Query(ctx, `
		SELECT prop_id, type_id, name, data_type
		FROM ont_properties
		WHERE ontology_id = $1`, ontologyID)
	if err != nil {
		return nil, fmt.Errorf("reading the property dictionary: %w", err)
	}
	defer propertyRows.Close()

	for propertyRows.Next() {
		var (
			propertyID int32
			typeID     int32
			name       string
			dataType   string
		)
		if err := propertyRows.Scan(&propertyID, &typeID, &name, &dataType); err != nil {
			return nil, fmt.Errorf("reading the property dictionary: %w", err)
		}
		if _, ok := dictionary.Properties[typeID]; !ok {
			dictionary.Properties[typeID] = map[string]int32{}
		}
		dictionary.Properties[typeID][name] = propertyID
		dictionary.PropertyNames[propertyID] = name
		dictionary.PropertyTypes[propertyID] = dataType
	}
	if err := propertyRows.Err(); err != nil {
		return nil, fmt.Errorf("reading the property dictionary: %w", err)
	}

	return dictionary, nil
}
