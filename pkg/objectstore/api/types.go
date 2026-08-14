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

// Package api is the wire contract of the object store server: the request and
// response bodies, and the mapping between object store errors and HTTP.
package api

import (
	"errors"
	"net/http"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Reasons an object store request can fail.
const (
	ReasonNotFound         = "NotFound"
	ReasonAlreadyExists    = "AlreadyExists"
	ReasonConflict         = "Conflict"
	ReasonUnknownType      = "UnknownType"
	ReasonUnknownProperty  = "UnknownProperty"
	ReasonUnknownLink      = "UnknownLink"
	ReasonInvalidValue     = "InvalidValue"
	ReasonCardinality      = "Cardinality"
	ReasonTypeMismatch     = "TypeMismatch"
	ReasonLinked           = "Linked"
	ReasonRequiredProperty = "RequiredProperty"
	ReasonInvalid          = "Invalid"
	ReasonInternal         = "Internal"
)

// PutRequest is the body of a write. The object type is in the path.
type PutRequest struct {
	// Set holds the property values to write, keyed by property name.
	Set map[string]interface{} `json:"set"`
	// Remove names properties whose values should be deleted.
	Remove []string `json:"remove,omitempty"`
	// CreateOnly fails rather than updating when the object already exists.
	CreateOnly bool `json:"createOnly,omitempty"`
}

// LinkBody is the body of a link or unlink request. The link type is in the path.
type LinkBody struct {
	// Source is the object the link points from.
	Source objectstore.Ref `json:"source"`
	// Target is the object the link points to.
	Target objectstore.Ref `json:"target"`
}

// ObjectList is the response of a list or traverse request.
type ObjectList struct {
	// Items are the matching objects.
	Items []objectstore.Object `json:"items"`
	// Total is how many objects match in total, ignoring paging. It is only set
	// when the request asked for it.
	Total *int64 `json:"total,omitempty"`
}

// OntologyInfo describes the ontology version a request would be served with.
// It is what an operator reads to find out which schema the store is enforcing,
// and what a client reads to check it is talking to the version it expects.
type OntologyInfo struct {
	// Ontology is the bound version's metadata.
	Ontology registry.Ontology `json:"ontology"`
	// ObjectTypes are the qualified object type names it defines.
	ObjectTypes []string `json:"objectTypes"`
	// LinkTypes are the qualified link type names it defines.
	LinkTypes []string `json:"linkTypes"`
}

// StatusForError maps an object store error onto an HTTP code and a reason.
//
// The mapping is what makes the store's rules visible to a client: a uniqueness
// violation and a cardinality violation are both conflicts, but a client
// retrying blindly needs to know which one it hit.
func StatusForError(err error) (int, string) {
	switch {
	case errors.Is(err, objectstore.ErrNotFound):
		return http.StatusNotFound, ReasonNotFound
	case errors.Is(err, objectstore.ErrAlreadyExists):
		return http.StatusConflict, ReasonAlreadyExists
	case errors.Is(err, objectstore.ErrConflict):
		return http.StatusConflict, ReasonConflict
	case errors.Is(err, objectstore.ErrCardinality):
		return http.StatusConflict, ReasonCardinality
	case errors.Is(err, objectstore.ErrLinked):
		return http.StatusConflict, ReasonLinked
	case errors.Is(err, objectstore.ErrUnknownType):
		return http.StatusBadRequest, ReasonUnknownType
	case errors.Is(err, objectstore.ErrUnknownProperty):
		return http.StatusBadRequest, ReasonUnknownProperty
	case errors.Is(err, objectstore.ErrUnknownLink):
		return http.StatusBadRequest, ReasonUnknownLink
	case errors.Is(err, objectstore.ErrInvalidValue):
		return http.StatusBadRequest, ReasonInvalidValue
	case errors.Is(err, objectstore.ErrTypeMismatch):
		return http.StatusBadRequest, ReasonTypeMismatch
	case errors.Is(err, objectstore.ErrRequiredProperty):
		return http.StatusBadRequest, ReasonRequiredProperty
	case errors.Is(err, registry.ErrNotFound):
		// The requested ontology version or tag does not exist, so there is no
		// schema to serve the request against.
		return http.StatusBadRequest, ReasonInvalid
	default:
		return http.StatusInternalServerError, ReasonInternal
	}
}
