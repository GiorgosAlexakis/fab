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

// Package objectstore defines the object store: the data plane that holds object
// instances, their current property values and the links between them.
//
// The store is generic over the ontology. It has no table per object type and no
// column per property; it writes the stable catalog ids the registry allocates.
// That is what makes publishing a new ontology version a metadata operation
// rather than a migration of the data plane.
package objectstore

import (
	"context"
	"errors"
	"time"
)

// Errors returned by an object store. Callers match on these rather than on
// strings.
var (
	// ErrNotFound is returned when an object does not exist.
	ErrNotFound = errors.New("object not found")
	// ErrAlreadyExists is returned when creating an object whose primary key is
	// taken.
	ErrAlreadyExists = errors.New("object already exists")
	// ErrConflict is returned when a write would violate a uniqueness
	// constraint declared in the ontology.
	ErrConflict = errors.New("conflicts with an existing object")
	// ErrUnknownType is returned when a request names an object type that the
	// bound ontology does not define.
	ErrUnknownType = errors.New("unknown object type")
	// ErrUnknownProperty is returned when a request names a property that the
	// bound ontology does not define on that type.
	ErrUnknownProperty = errors.New("unknown property")
	// ErrUnknownLink is returned when a request names a link type or traversal
	// that the bound ontology does not define.
	ErrUnknownLink = errors.New("unknown link type")
	// ErrInvalidValue is returned when a value does not match the declared
	// property type.
	ErrInvalidValue = errors.New("invalid property value")
	// ErrCardinality is returned when a link would violate its declared
	// cardinality.
	ErrCardinality = errors.New("violates link cardinality")
	// ErrTypeMismatch is returned when an object is not of the type the
	// operation requires, e.g. when linking an object to the wrong end of a
	// link type.
	ErrTypeMismatch = errors.New("object is not of the required type")
	// ErrLinked is returned when deleting an object that a link type with the
	// restrict delete policy still connects.
	ErrLinked = errors.New("object still has links")
	// ErrRequiredProperty is returned when a write would leave a non-nullable
	// property unset.
	ErrRequiredProperty = errors.New("required property is missing")
)

// Ref identifies an object by type and primary key, which is how callers outside
// the store refer to objects. Internal object ids are an implementation detail
// of the store and are only meaningful within one database.
type Ref struct {
	// Type is the qualified object type name, e.g. "app/Customer".
	Type string
	// PrimaryKey is the value of the type's primary key property.
	PrimaryKey string
}

// String renders a reference as "app/Customer/CUST-1".
func (r Ref) String() string {
	return r.Type + "/" + r.PrimaryKey
}

// Object is one object instance with its current property values.
type Object struct {
	// ObjectID is the store-internal identity of the object.
	ObjectID int64 `json:"objectId"`
	// Type is the qualified object type name.
	Type string `json:"type"`
	// PrimaryKey is the value of the type's primary key property.
	PrimaryKey string `json:"primaryKey"`
	// Properties are the current values, keyed by property name. Properties
	// that have never been set are absent rather than nil.
	Properties map[string]interface{} `json:"properties"`
	// CreatedAt is when the object was created.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is when any of its properties last changed.
	UpdatedAt time.Time `json:"updatedAt"`
}

// Ref returns the reference identifying this object.
func (o *Object) Ref() Ref {
	return Ref{Type: o.Type, PrimaryKey: o.PrimaryKey}
}

// PutRequest creates or updates one object.
//
// Writes merge: properties named in Set are written and everything else is left
// alone, so two writers updating different properties of the same object do not
// overwrite each other. Removing a value is explicit, via Remove.
type PutRequest struct {
	// Type is the qualified object type name.
	Type string
	// Set holds the property values to write, keyed by property name. It must
	// include the primary key property when creating the object.
	Set map[string]interface{}
	// Remove names properties whose values should be deleted. A non-nullable
	// property cannot be removed.
	Remove []string
	// CreateOnly fails rather than updating when the object already exists.
	CreateOnly bool
}

// Filter matches objects whose property equals a value.
type Filter struct {
	// Property is the property name to match on.
	Property string
	// Value is the value it must equal.
	Value interface{}
}

// Query selects objects of one type.
type Query struct {
	// Type is the qualified object type name.
	Type string
	// Filters are ANDed together. Every filter property should be indexed in
	// the ontology; the store does not refuse unindexed filters, but they scan.
	Filters []Filter
	// Limit bounds the number of objects returned. Zero means DefaultQueryLimit.
	Limit int
	// Offset skips the first Offset objects, ordered by object id.
	Offset int
}

// DefaultQueryLimit bounds an unbounded query, so that a missing Limit cannot
// pull an entire object type into memory.
const DefaultQueryLimit = 100

// MaxQueryLimit is the largest page a single query may return.
const MaxQueryLimit = 10000

// LinkRequest connects or disconnects two objects.
type LinkRequest struct {
	// Link is the qualified link type name, e.g. "app/CustomerOrders".
	Link string
	// Source is the object the link points from.
	Source Ref
	// Target is the object the link points to.
	Target Ref
}

// TraverseRequest walks a link from one object.
type TraverseRequest struct {
	// From is the object to start at.
	From Ref
	// Traversal is the traversal name on From's type: either a link's
	// forwardName, when From is the link's source, or its reverseName, when From
	// is the link's target.
	Traversal string
	// Limit bounds the number of objects returned. Zero means
	// DefaultQueryLimit.
	Limit int
	// Offset skips the first Offset objects, ordered by object id.
	Offset int
}

// Interface is the object store.
type Interface interface {
	// Put creates or updates an object and returns its current state.
	Put(ctx context.Context, request PutRequest) (*Object, error)

	// Get returns one object by type and primary key.
	Get(ctx context.Context, ref Ref) (*Object, error)

	// GetByID returns one object by its store-internal id.
	GetByID(ctx context.Context, objectID int64) (*Object, error)

	// Delete removes an object together with its properties and links.
	//
	// What happens to the objects it points at is the delete policy each link
	// type declares: restrict refuses while a link exists, cascade deletes
	// them, detach and set_null leave them. Links pointing at the object are
	// always removed, because a policy is declared only from the source's side.
	Delete(ctx context.Context, ref Ref) error

	// List returns the objects of one type matching a query.
	List(ctx context.Context, query Query) ([]Object, error)

	// Count returns how many objects match a query, ignoring Limit and Offset.
	Count(ctx context.Context, query Query) (int64, error)

	// Link connects two objects. Linking an already linked pair is a no-op.
	Link(ctx context.Context, request LinkRequest) error

	// Unlink disconnects two objects. Unlinking an unlinked pair is a no-op.
	Unlink(ctx context.Context, request LinkRequest) error

	// Traverse returns the objects reachable from one object along a traversal.
	Traverse(ctx context.Context, request TraverseRequest) ([]Object, error)
}
