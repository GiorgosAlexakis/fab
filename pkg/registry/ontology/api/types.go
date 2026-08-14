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

// Package api is the wire contract of the ontology registry server: the request
// and response bodies, and the mapping between registry errors and HTTP.
//
// Server and client both depend on this package and on nothing of each other, so
// the contract cannot drift between the two.
package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Reasons a registry request can fail. These are the stable part of an error
// response; the message is not.
const (
	ReasonNotFound          = "NotFound"
	ReasonAlreadyExists     = "AlreadyExists"
	ReasonImmutable         = "Immutable"
	ReasonNotPublished      = "NotPublished"
	ReasonNoPreviousVersion = "NoPreviousVersion"
	ReasonDigestMismatch    = "DigestMismatch"
	ReasonInvalid           = "Invalid"
	ReasonInternal          = "Internal"
)

// PublishRequest is the body of a publish request.
type PublishRequest struct {
	// Version is the version to publish.
	Version string `json:"version"`
	// GitRef is the commit the snapshot was compiled from, if known.
	GitRef string `json:"gitRef,omitempty"`
	// Draft publishes a mutable draft rather than an immutable version.
	Draft bool `json:"draft,omitempty"`
	// Snapshot is the compiled ontology.
	Snapshot *snapshot.Snapshot `json:"snapshot"`
}

// TagRequest is the body of a request pointing a tag at a version.
type TagRequest struct {
	// Version is the published version the tag should point at.
	Version string `json:"version"`
}

// PromoteRequest is the body of a promote request. The tag being moved is in
// the path; this is the tag it copies from.
type PromoteRequest struct {
	// From is the tag whose version is being promoted.
	From string `json:"from"`
}

// VersionList is the response of a list request.
type VersionList struct {
	// Items are the versions, newest first.
	Items []registry.Ontology `json:"items"`
}

// sentinels pairs each reason with the registry error it stands for, so an
// error survives the round trip through HTTP and a caller can still match it
// with errors.Is.
var sentinels = map[string]error{
	ReasonNotFound:          registry.ErrNotFound,
	ReasonAlreadyExists:     registry.ErrAlreadyExists,
	ReasonImmutable:         registry.ErrImmutable,
	ReasonNotPublished:      registry.ErrNotPublished,
	ReasonNoPreviousVersion: registry.ErrNoPreviousVersion,
	ReasonDigestMismatch:    registry.ErrDigestMismatch,
}

// StatusForError maps a registry error onto an HTTP code and a reason.
func StatusForError(err error) (int, string) {
	switch {
	case errors.Is(err, registry.ErrNotFound):
		return http.StatusNotFound, ReasonNotFound
	case errors.Is(err, registry.ErrAlreadyExists):
		return http.StatusConflict, ReasonAlreadyExists
	case errors.Is(err, registry.ErrImmutable):
		return http.StatusConflict, ReasonImmutable
	case errors.Is(err, registry.ErrNotPublished):
		return http.StatusConflict, ReasonNotPublished
	case errors.Is(err, registry.ErrNoPreviousVersion):
		return http.StatusConflict, ReasonNoPreviousVersion
	case errors.Is(err, registry.ErrDigestMismatch):
		return http.StatusInternalServerError, ReasonDigestMismatch
	default:
		return http.StatusInternalServerError, ReasonInternal
	}
}

// ErrorFromStatus maps an error response back onto the registry error it stands
// for, so that a command reacting to ErrNotFound behaves the same whether it
// holds a local store or a client.
func ErrorFromStatus(status apiserver.Status) error {
	sentinel, ok := sentinels[status.Reason]
	if !ok {
		return status
	}
	message := strings.TrimSuffix(status.Message, ": "+sentinel.Error())
	if message == "" || message == sentinel.Error() {
		return sentinel
	}
	return fmt.Errorf("%s: %w", message, sentinel)
}
