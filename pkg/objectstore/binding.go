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

package objectstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Property is what the store needs to know about one property: its stable id
// and the constraints its values must satisfy.
type Property struct {
	// ID is the registry catalog prop_id, and is what is stored on every row.
	ID int32
	// Name is the property name in the bound ontology version.
	Name string
	// DataType is the ontology property type.
	DataType string
	// ItemsType is the element type of an array property.
	ItemsType string
	// EnumValues are the permitted values of an enum property.
	EnumValues []string
	// Nullable reports whether the value may be absent.
	Nullable bool
	// Unique reports whether the value must be unique across instances.
	Unique bool
}

// ObjectType is what the store needs to know about one object type.
type ObjectType struct {
	// ID is the registry catalog type_id, stored on every object row.
	ID int32
	// QualifiedName is the layer-qualified type name.
	QualifiedName string
	// PrimaryKey is the identifying property.
	PrimaryKey Property
	// Properties are the type's properties by name.
	Properties map[string]Property
	// propertiesByID resolves a stored prop_id back to a property, which is how
	// reads turn rows into named values.
	propertiesByID map[int32]Property
}

// Property returns the named property of this type.
func (t *ObjectType) Property(name string) (Property, bool) {
	property, ok := t.Properties[name]
	return property, ok
}

// PropertyByID returns the property with the given catalog id.
func (t *ObjectType) PropertyByID(id int32) (Property, bool) {
	property, ok := t.propertiesByID[id]
	return property, ok
}

// LinkType is what the store needs to know about one link type.
type LinkType struct {
	// ID is the registry catalog link_id, stored on every link row.
	ID int32
	// QualifiedName is the layer-qualified link type name.
	QualifiedName string
	// SourceTypeID is the catalog id of the source object type.
	SourceTypeID int32
	// TargetTypeID is the catalog id of the target object type.
	TargetTypeID int32
	// Cardinality constrains both ends of the link.
	Cardinality string
	// ForwardName is the traversal name from source to target.
	ForwardName string
	// ReverseName is the traversal name from target to source.
	ReverseName string
	// OnSourceDelete is what happens to the targets when a source object is
	// deleted.
	OnSourceDelete string
}

// Traversal is one direction of a link, as reached from a particular object type.
type Traversal struct {
	// Link is the link being traversed.
	Link LinkType
	// Forward reports whether the traversal runs from source to target.
	Forward bool
	// FromTypeID is the catalog id of the type the traversal starts at.
	FromTypeID int32
	// ToTypeID is the catalog id of the type the traversal arrives at.
	ToTypeID int32
}

// Binding is an object store bound to one ontology version: the type and
// property names of that version, paired with the stable ids its rows carry.
//
// A store is bound rather than schema-aware. Rebinding to a newer version is how
// a running service picks up a schema change, and because the ids are stable, no
// data moves.
type Binding struct {
	ontology   registry.Ontology
	types      map[string]*ObjectType
	typesByID  map[int32]*ObjectType
	links      map[string]LinkType
	traversals map[string]Traversal
	linksByID  map[int32]LinkType
	// outgoing indexes link types by their source type id, which is what a
	// delete has to walk to apply the policies the ontology declares.
	outgoing map[int32][]LinkType
}

// NewBinding pairs a compiled ontology with the identities the registry
// allocated for it.
//
// It fails when the two disagree, because a store that cannot resolve a property
// to an id would silently drop values.
func NewBinding(compiled *snapshot.Snapshot, dictionary *registry.Dictionary) (*Binding, error) {
	if compiled == nil {
		return nil, fmt.Errorf("a compiled ontology is required")
	}
	if dictionary == nil {
		return nil, fmt.Errorf("an ontology dictionary is required")
	}

	binding := &Binding{
		ontology:   dictionary.Ontology,
		types:      make(map[string]*ObjectType, len(compiled.ObjectTypes)),
		typesByID:  make(map[int32]*ObjectType, len(compiled.ObjectTypes)),
		links:      make(map[string]LinkType, len(compiled.LinkTypes)),
		traversals: make(map[string]Traversal, 2*len(compiled.LinkTypes)),
		linksByID:  make(map[int32]LinkType, len(compiled.LinkTypes)),
		outgoing:   make(map[int32][]LinkType, len(compiled.ObjectTypes)),
	}

	for i := range compiled.ObjectTypes {
		objectType := &compiled.ObjectTypes[i]
		qualifiedName := objectType.QualifiedName()

		typeID, ok := dictionary.TypeID(qualifiedName)
		if !ok {
			return nil, fmt.Errorf("object type %s has no id in the dictionary of %s:%s",
				qualifiedName, dictionary.Ontology.Name, dictionary.Ontology.Version)
		}

		bound := &ObjectType{
			ID:             typeID,
			QualifiedName:  qualifiedName,
			Properties:     make(map[string]Property, len(objectType.Properties)),
			propertiesByID: make(map[int32]Property, len(objectType.Properties)),
		}

		for j := range objectType.Properties {
			property := &objectType.Properties[j]
			propertyID, ok := dictionary.PropertyID(typeID, property.Name)
			if !ok {
				return nil, fmt.Errorf("property %s.%s has no id in the dictionary of %s:%s",
					qualifiedName, property.Name, dictionary.Ontology.Name, dictionary.Ontology.Version)
			}

			bound.Properties[property.Name] = Property{
				ID:         propertyID,
				Name:       property.Name,
				DataType:   property.Type,
				ItemsType:  property.Items,
				EnumValues: property.Values,
				Nullable:   property.Nullable,
				Unique:     property.Unique,
			}
			bound.propertiesByID[propertyID] = bound.Properties[property.Name]
		}

		primaryKey, ok := bound.Properties[objectType.PrimaryKey]
		if !ok {
			return nil, fmt.Errorf("object type %s declares primary key %q which is not one of its properties",
				qualifiedName, objectType.PrimaryKey)
		}
		bound.PrimaryKey = primaryKey

		binding.types[qualifiedName] = bound
		binding.typesByID[typeID] = bound
	}

	for i := range compiled.LinkTypes {
		linkType := &compiled.LinkTypes[i]
		qualifiedName := linkType.QualifiedName()

		linkID, ok := dictionary.Links[qualifiedName]
		if !ok {
			return nil, fmt.Errorf("link type %s has no id in the dictionary of %s:%s",
				qualifiedName, dictionary.Ontology.Name, dictionary.Ontology.Version)
		}
		sourceType, ok := binding.types[linkType.Source.QualifiedName()]
		if !ok {
			return nil, fmt.Errorf("link type %s references source object type %s which is not in this ontology",
				qualifiedName, linkType.Source.QualifiedName())
		}
		targetType, ok := binding.types[linkType.Target.QualifiedName()]
		if !ok {
			return nil, fmt.Errorf("link type %s references target object type %s which is not in this ontology",
				qualifiedName, linkType.Target.QualifiedName())
		}

		bound := LinkType{
			ID:             linkID,
			QualifiedName:  qualifiedName,
			SourceTypeID:   sourceType.ID,
			TargetTypeID:   targetType.ID,
			Cardinality:    linkType.Cardinality,
			ForwardName:    linkType.ForwardName,
			ReverseName:    linkType.ReverseName,
			OnSourceDelete: linkType.OnSourceDelete,
		}
		binding.links[qualifiedName] = bound
		binding.linksByID[linkID] = bound
		binding.outgoing[sourceType.ID] = append(binding.outgoing[sourceType.ID], bound)

		binding.traversals[traversalKey(sourceType.QualifiedName, linkType.ForwardName)] = Traversal{
			Link:       bound,
			Forward:    true,
			FromTypeID: sourceType.ID,
			ToTypeID:   targetType.ID,
		}
		binding.traversals[traversalKey(targetType.QualifiedName, linkType.ReverseName)] = Traversal{
			Link:       bound,
			Forward:    false,
			FromTypeID: targetType.ID,
			ToTypeID:   sourceType.ID,
		}
	}

	return binding, nil
}

// BindVersion binds a store to a specific published ontology version.
func BindVersion(ctx context.Context, reg registry.Interface, name, version string) (*Binding, error) {
	compiled, err := reg.GetSnapshot(ctx, name, version)
	if err != nil {
		return nil, err
	}
	dictionary, err := reg.Dictionary(ctx, name, version)
	if err != nil {
		return nil, err
	}
	return NewBinding(compiled, dictionary)
}

// BindTag binds a store to whatever version an environment tag points at, which
// is how a service selects its ontology.
func BindTag(ctx context.Context, reg registry.Interface, name, tag string) (*Binding, error) {
	resolved, err := reg.Resolve(ctx, name, tag)
	if err != nil {
		return nil, err
	}
	return BindVersion(ctx, reg, name, resolved.Version)
}

// Ontology returns the metadata of the bound version.
func (b *Binding) Ontology() registry.Ontology {
	return b.ontology
}

// ObjectType returns the bound object type with the given qualified name.
func (b *Binding) ObjectType(qualifiedName string) (*ObjectType, error) {
	objectType, ok := b.types[qualifiedName]
	if !ok {
		return nil, fmt.Errorf("%q is not defined in %s:%s: %w",
			qualifiedName, b.ontology.Name, b.ontology.Version, ErrUnknownType)
	}
	return objectType, nil
}

// ObjectTypeByID returns the bound object type with the given catalog id. An
// unknown id means the row was written under an ontology this binding does not
// cover.
func (b *Binding) ObjectTypeByID(typeID int32) (*ObjectType, error) {
	objectType, ok := b.typesByID[typeID]
	if !ok {
		return nil, fmt.Errorf("object type id %d is not defined in %s:%s: %w",
			typeID, b.ontology.Name, b.ontology.Version, ErrUnknownType)
	}
	return objectType, nil
}

// LinkType returns the bound link type with the given qualified name.
func (b *Binding) LinkType(qualifiedName string) (LinkType, error) {
	linkType, ok := b.links[qualifiedName]
	if !ok {
		return LinkType{}, fmt.Errorf("%q is not defined in %s:%s: %w",
			qualifiedName, b.ontology.Name, b.ontology.Version, ErrUnknownLink)
	}
	return linkType, nil
}

// LinkTypeByID returns the bound link type with the given catalog id.
func (b *Binding) LinkTypeByID(linkID int32) (LinkType, error) {
	linkType, ok := b.linksByID[linkID]
	if !ok {
		return LinkType{}, fmt.Errorf("link type id %d is not defined in %s:%s: %w",
			linkID, b.ontology.Name, b.ontology.Version, ErrUnknownLink)
	}
	return linkType, nil
}

// OutgoingLinks returns the link types an object of this type can be the source
// of. Deleting an object walks these, because a link's delete policy is declared
// from the source's point of view.
func (b *Binding) OutgoingLinks(typeID int32) []LinkType {
	return b.outgoing[typeID]
}

// Traversal returns the traversal of the given name reachable from an object
// type. Forward and reverse traversals resolve through the same call, because to
// a caller holding an object they are the same thing: a named edge to follow.
func (b *Binding) Traversal(typeQualifiedName, traversalName string) (Traversal, error) {
	traversal, ok := b.traversals[traversalKey(typeQualifiedName, traversalName)]
	if !ok {
		return Traversal{}, fmt.Errorf("%s has no traversal %q in %s:%s: %w",
			typeQualifiedName, traversalName, b.ontology.Name, b.ontology.Version, ErrUnknownLink)
	}
	return traversal, nil
}

// ObjectTypeNames returns the qualified names of every bound object type, sorted.
func (b *Binding) ObjectTypeNames() []string {
	names := make([]string, 0, len(b.types))
	for name := range b.types {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// LinkTypeNames returns the qualified names of every bound link type, sorted.
func (b *Binding) LinkTypeNames() []string {
	names := make([]string, 0, len(b.links))
	for name := range b.links {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func traversalKey(typeQualifiedName, traversalName string) string {
	return typeQualifiedName + "." + traversalName
}
