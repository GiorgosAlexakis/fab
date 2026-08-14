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

package v1

// TypeMeta describes the kind and API version of an individual ontology
// document. Every document in schema/ carries it.
type TypeMeta struct {
	// APIVersion is the versioned schema of this representation. Always fab/v1
	// for the kinds in this package.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is the ontology document kind, e.g. ObjectType.
	Kind string `json:"kind,omitempty"`
}

// ObjectMeta is metadata common to every ontology document.
type ObjectMeta struct {
	// Name is the document name, unique within a layer for a given kind.
	// Object type and link type names are PascalCase, e.g. Customer,
	// CustomerOrders.
	Name string `json:"name"`
	// Layer is the layer that owns this document. It is defaulted from the
	// directory the document was loaded from; when set explicitly it must
	// agree with that directory.
	Layer string `json:"layer,omitempty"`
	// Description is free-form documentation surfaced by `fab schema` output,
	// generated clients and the MCP server.
	Description string `json:"description,omitempty"`
}

// PropertyType is the ontology-level type of a property. It is deliberately
// smaller than the SQL or proto type systems: the compiler maps these onto
// storage and wire types, not the other way round.
type PropertyType string

const (
	// PropertyTypeString is a variable-length UTF-8 string.
	PropertyTypeString PropertyType = "string"
	// PropertyTypeBoolean is a true/false value.
	PropertyTypeBoolean PropertyType = "boolean"
	// PropertyTypeInteger is a signed 32-bit integer.
	PropertyTypeInteger PropertyType = "integer"
	// PropertyTypeLong is a signed 64-bit integer.
	PropertyTypeLong PropertyType = "long"
	// PropertyTypeDouble is an IEEE-754 double.
	PropertyTypeDouble PropertyType = "double"
	// PropertyTypeDecimal is an arbitrary-precision decimal, carried as a
	// string on the wire to avoid float rounding.
	PropertyTypeDecimal PropertyType = "decimal"
	// PropertyTypeTimestamp is an instant with timezone (RFC 3339).
	PropertyTypeTimestamp PropertyType = "timestamp"
	// PropertyTypeDate is a calendar date without time or zone.
	PropertyTypeDate PropertyType = "date"
	// PropertyTypeJSON is an opaque JSON document. The ontology makes no
	// promises about its contents; it is not queryable by property path.
	PropertyTypeJSON PropertyType = "json"
	// PropertyTypeEnum is a closed set of string values listed in
	// Property.Values.
	PropertyTypeEnum PropertyType = "enum"
	// PropertyTypeArray is a repeated scalar whose element type is named by
	// Property.Items.
	PropertyTypeArray PropertyType = "array"
)

// Property is a single field on an object type.
type Property struct {
	// Name is the property name in snake_case, unique within its object type.
	Name string `json:"name"`
	// Type is the ontology type of this property.
	Type PropertyType `json:"type"`
	// Description documents the property for generated clients.
	Description string `json:"description,omitempty"`
	// Items is the element type. Required when Type is array, forbidden
	// otherwise. Only scalar element types are supported.
	Items PropertyType `json:"items,omitempty"`
	// Values enumerates the permitted values. Required when Type is enum,
	// forbidden otherwise.
	Values []string `json:"values,omitempty"`
	// Nullable reports whether the property may be absent. Defaults to true
	// for every property except the primary key, which is never nullable.
	Nullable *bool `json:"nullable,omitempty"`
	// Unique constrains this property to be unique across instances of the
	// object type.
	Unique bool `json:"unique,omitempty"`
	// Indexed requests an index for filtering on this property.
	Indexed bool `json:"indexed,omitempty"`
}

// ObjectType is a business entity: a named set of properties with a primary
// key, owned by exactly one layer.
type ObjectType struct {
	TypeMeta `json:",inline"`
	// Metadata identifies and documents this object type.
	Metadata ObjectMeta `json:"metadata"`
	// Spec is the object type definition.
	Spec ObjectTypeSpec `json:"spec"`
}

// ObjectTypeSpec is the definition of an object type.
type ObjectTypeSpec struct {
	// PrimaryKey names the property that identifies an instance. It must be a
	// declared, non-nullable scalar property.
	PrimaryKey string `json:"primaryKey"`
	// Properties are the fields of this object type. Declaration order is not
	// significant: the compiler sorts properties by name so that reordering
	// YAML does not change the ontology digest.
	Properties []Property `json:"properties"`
}

// Cardinality constrains how many instances may participate on each side of a
// link type.
type Cardinality string

const (
	// CardinalityOneToOne links at most one source to at most one target.
	CardinalityOneToOne Cardinality = "one_to_one"
	// CardinalityOneToMany links one source to many targets.
	CardinalityOneToMany Cardinality = "one_to_many"
	// CardinalityManyToOne links many sources to one target.
	CardinalityManyToOne Cardinality = "many_to_one"
	// CardinalityManyToMany links many sources to many targets.
	CardinalityManyToMany Cardinality = "many_to_many"
)

// DeletePolicy is the behavior applied to linked objects when the object on
// the other side of a link is deleted.
type DeletePolicy string

const (
	// DeletePolicyRestrict fails the delete while linked objects exist. This
	// is the default: it never destroys data implicitly.
	DeletePolicyRestrict DeletePolicy = "restrict"
	// DeletePolicyCascade deletes the linked target objects.
	DeletePolicyCascade DeletePolicy = "cascade"
	// DeletePolicySetNull clears the link reference on the target objects.
	DeletePolicySetNull DeletePolicy = "set_null"
	// DeletePolicyDetach removes the link but leaves the target objects.
	DeletePolicyDetach DeletePolicy = "detach"
)

// TypeReference points at an object type, possibly in another layer.
type TypeReference struct {
	// Layer is the layer that owns the referenced object type. It defaults to
	// the layer of the referencing document.
	Layer string `json:"layer,omitempty"`
	// Type is the referenced object type name.
	Type string `json:"type"`
}

// LinkType is a first-class typed relationship between two object types. It is
// not a foreign key: it is a named, directional, cardinality-constrained edge
// that is queryable in both directions.
type LinkType struct {
	TypeMeta `json:",inline"`
	// Metadata identifies and documents this link type.
	Metadata ObjectMeta `json:"metadata"`
	// Spec is the link type definition.
	Spec LinkTypeSpec `json:"spec"`
}

// LinkTypeSpec is the definition of a link type.
type LinkTypeSpec struct {
	// Source is the object type the link points from.
	Source TypeReference `json:"source"`
	// Target is the object type the link points to.
	Target TypeReference `json:"target"`
	// Cardinality constrains both ends of the link.
	Cardinality Cardinality `json:"cardinality"`
	// ForwardName is the traversal name from source to target. It defaults to
	// the snake_case form of the link type name.
	ForwardName string `json:"forwardName,omitempty"`
	// ReverseName is the traversal name from target back to source. It
	// defaults to the snake_case form of the source type name.
	ReverseName string `json:"reverseName,omitempty"`
	// OnSourceDelete is the policy applied to target objects when a source
	// object is deleted. Defaults to restrict.
	OnSourceDelete DeletePolicy `json:"onSourceDelete,omitempty"`
}

// GetObjectKind returns the document's kind and API version.
func (t *TypeMeta) GetObjectKind() *TypeMeta { return t }

// GetObjectMeta returns the object type's metadata.
func (o *ObjectType) GetObjectMeta() *ObjectMeta { return &o.Metadata }

// GetObjectMeta returns the link type's metadata.
func (l *LinkType) GetObjectMeta() *ObjectMeta { return &l.Metadata }

// Object is implemented by every ontology document kind in this package.
type Object interface {
	// GetObjectKind returns the kind and API version of the document.
	GetObjectKind() *TypeMeta
	// GetObjectMeta returns the name, owning layer and description.
	GetObjectMeta() *ObjectMeta
}

var (
	_ Object = &ObjectType{}
	_ Object = &LinkType{}
)

// IsScalar reports whether t is a scalar type, i.e. one that can serve as a
// primary key, an array element or an indexable value.
func (t PropertyType) IsScalar() bool {
	switch t {
	case PropertyTypeString, PropertyTypeBoolean, PropertyTypeInteger,
		PropertyTypeLong, PropertyTypeDouble, PropertyTypeDecimal,
		PropertyTypeTimestamp, PropertyTypeDate:
		return true
	default:
		return false
	}
}

// IsNullable reports the effective nullability of the property. Call it after
// defaulting; an undefaulted nil Nullable reads as nullable.
func (p *Property) IsNullable() bool {
	return p.Nullable == nil || *p.Nullable
}
