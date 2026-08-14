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

// Package snapshot defines the compiled ontology: the merged, validated,
// cross-referenced result of every active layer's schema documents.
//
// A snapshot is the only thing downstream consumers read. The registry
// persists it, the OSDK generator reads it, the MCP server serves it, and the
// object store resolves property identities against it. None of them parse
// YAML.
package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Snapshot is a complete, self-contained ontology. There are no diffs and no
// parent references: a snapshot is everything the active layers declare.
type Snapshot struct {
	// Layers are the contributing layers in merge order, which is the
	// topological order of the layer dependency graph.
	Layers []string `json:"layers"`
	// ObjectTypes are the merged object types, sorted by qualified name.
	ObjectTypes []ObjectType `json:"objectTypes"`
	// LinkTypes are the merged link types, sorted by qualified name.
	LinkTypes []LinkType `json:"linkTypes"`
}

// ObjectType is a compiled object type.
type ObjectType struct {
	// Layer is the layer that owns this object type.
	Layer string `json:"layer"`
	// Name is the PascalCase type name.
	Name string `json:"name"`
	// Description is documentation carried through to generated clients.
	Description string `json:"description,omitempty"`
	// PrimaryKey names the identifying property.
	PrimaryKey string `json:"primaryKey"`
	// Properties are this type's properties, sorted by name.
	Properties []Property `json:"properties"`
}

// Property is a compiled property.
type Property struct {
	// Name is the snake_case property name.
	Name string `json:"name"`
	// Type is the ontology property type.
	Type string `json:"type"`
	// Description is documentation carried through to generated clients.
	Description string `json:"description,omitempty"`
	// Items is the element type for array properties.
	Items string `json:"items,omitempty"`
	// Values are the permitted values for enum properties, in declared order.
	Values []string `json:"values,omitempty"`
	// Nullable reports whether the property may be absent.
	Nullable bool `json:"nullable"`
	// Unique reports whether values must be unique across instances.
	Unique bool `json:"unique"`
	// Indexed reports whether the storage layer should index this property.
	Indexed bool `json:"indexed"`
}

// TypeRef refers to an object type by layer and name.
type TypeRef struct {
	// Layer owns the referenced object type.
	Layer string `json:"layer"`
	// Type is the referenced object type name.
	Type string `json:"type"`
}

// LinkType is a compiled link type. Both endpoints are guaranteed to resolve
// to an object type in the same snapshot.
type LinkType struct {
	// Layer is the layer that owns this link type.
	Layer string `json:"layer"`
	// Name is the PascalCase link type name.
	Name string `json:"name"`
	// Description is documentation carried through to generated clients.
	Description string `json:"description,omitempty"`
	// Source is the object type the link points from.
	Source TypeRef `json:"source"`
	// Target is the object type the link points to.
	Target TypeRef `json:"target"`
	// Cardinality constrains both ends of the link.
	Cardinality string `json:"cardinality"`
	// ForwardName is the traversal name from source to target.
	ForwardName string `json:"forwardName"`
	// ReverseName is the traversal name from target to source.
	ReverseName string `json:"reverseName"`
	// OnSourceDelete is the policy applied to targets when a source is deleted.
	OnSourceDelete string `json:"onSourceDelete"`
}

// QualifiedName joins a layer and type name into the ontology-wide identifier
// for that type, e.g. "meta-core/User".
func QualifiedName(layer, name string) string {
	return layer + "/" + name
}

// QualifiedName returns the ontology-wide identifier of this object type.
func (o *ObjectType) QualifiedName() string { return QualifiedName(o.Layer, o.Name) }

// QualifiedName returns the ontology-wide identifier of this link type.
func (l *LinkType) QualifiedName() string { return QualifiedName(l.Layer, l.Name) }

// QualifiedName returns the ontology-wide identifier of the referenced type.
func (r TypeRef) QualifiedName() string { return QualifiedName(r.Layer, r.Type) }

// ObjectType returns the object type with the given layer and name.
func (s *Snapshot) ObjectType(layer, name string) (*ObjectType, bool) {
	for i := range s.ObjectTypes {
		if s.ObjectTypes[i].Layer == layer && s.ObjectTypes[i].Name == name {
			return &s.ObjectTypes[i], true
		}
	}
	return nil, false
}

// LinkType returns the link type with the given layer and name.
func (s *Snapshot) LinkType(layer, name string) (*LinkType, bool) {
	for i := range s.LinkTypes {
		if s.LinkTypes[i].Layer == layer && s.LinkTypes[i].Name == name {
			return &s.LinkTypes[i], true
		}
	}
	return nil, false
}

// Property returns the named property of an object type.
func (o *ObjectType) Property(name string) (*Property, bool) {
	for i := range o.Properties {
		if o.Properties[i].Name == name {
			return &o.Properties[i], true
		}
	}
	return nil, false
}

// Normalize sorts a snapshot into canonical form. Layer order is preserved
// because it is the merge order; everything else is sorted so that reordering
// YAML files or properties within a file does not change the digest.
func (s *Snapshot) Normalize() {
	sort.SliceStable(s.ObjectTypes, func(i, j int) bool {
		return s.ObjectTypes[i].QualifiedName() < s.ObjectTypes[j].QualifiedName()
	})
	sort.SliceStable(s.LinkTypes, func(i, j int) bool {
		return s.LinkTypes[i].QualifiedName() < s.LinkTypes[j].QualifiedName()
	})
	for i := range s.ObjectTypes {
		properties := s.ObjectTypes[i].Properties
		sort.SliceStable(properties, func(a, b int) bool {
			return properties[a].Name < properties[b].Name
		})
	}
}

// CanonicalJSON returns the byte form the digest is computed over. Struct
// field order is fixed by the type definitions and no Go maps are involved, so
// the encoding is deterministic across processes and releases.
func (s *Snapshot) CanonicalJSON() ([]byte, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshaling snapshot: %w", err)
	}
	return data, nil
}

// Digest returns the content digest of the snapshot as "sha256:<hex>". It is
// the identity of the ontology content: the registry stores it, the sstate
// cache keys on it, and `fab schema publish` refuses to overwrite a published
// version whose digest differs.
func (s *Snapshot) Digest() (string, error) {
	data, err := s.CanonicalJSON()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
