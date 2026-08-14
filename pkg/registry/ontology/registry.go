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

// Package ontology defines the ontology registry: the metadata plane that
// stores versioned ontology snapshots and the environment tags that point at
// them.
//
// The interface here is storage-agnostic on purpose. Everything above it --
// commands, services, the OSDK generator -- depends on this interface, never on
// PostgreSQL, so the storage backend can be replaced without touching them.
package ontology

import (
	"context"
	"errors"
	"time"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
)

// Status is the lifecycle state of an ontology version.
type Status string

const (
	// StatusDraft is a version that is still being iterated on. A draft may be
	// re-published in place and may not be tagged.
	StatusDraft Status = "draft"
	// StatusPublished is an immutable version that environments may point at.
	StatusPublished Status = "published"
	// StatusDeprecated is a published version that should no longer be adopted.
	// It stays readable so that clients pinned to it keep working.
	StatusDeprecated Status = "deprecated"
)

// Errors returned by a registry. Callers match on these rather than on strings.
var (
	// ErrNotFound is returned when a version, tag or ontology does not exist.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists is returned when publishing a version that already
	// exists with different content.
	ErrAlreadyExists = errors.New("already exists")
	// ErrImmutable is returned when modifying a published or deprecated version.
	ErrImmutable = errors.New("published versions are immutable")
	// ErrNotPublished is returned when tagging a draft version.
	ErrNotPublished = errors.New("version is not published")
	// ErrNoPreviousVersion is returned when rolling back a tag that has never
	// moved.
	ErrNoPreviousVersion = errors.New("tag has no previous version")
	// ErrDigestMismatch is returned when stored rows do not reproduce the
	// digest recorded at publish time.
	ErrDigestMismatch = errors.New("stored ontology does not match its digest")
)

// Ontology is the metadata of one ontology version, without its type
// definitions.
type Ontology struct {
	// Name is the ontology name, typically the company name.
	Name string `json:"name"`
	// Version is the semantic version of this snapshot.
	Version string `json:"version"`
	// Status is the lifecycle state.
	Status Status `json:"status"`
	// Digest is the content digest of the compiled snapshot.
	Digest string `json:"digest"`
	// GitRef is the commit the snapshot was compiled from, if known.
	GitRef string `json:"gitRef,omitempty"`
	// Layers are the contributing layers, in merge order.
	Layers []string `json:"layers"`
	// Tags are the environment tags currently pointing at this version.
	Tags []string `json:"tags,omitempty"`
	// CreatedAt is when the version was first written.
	CreatedAt time.Time `json:"createdAt"`
	// PublishedAt is when the version became published, if it has.
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// PublishRequest publishes a compiled snapshot as a named version.
type PublishRequest struct {
	// Name is the ontology name.
	Name string
	// Version is the version to publish.
	Version string
	// Snapshot is the compiled ontology.
	Snapshot *snapshot.Snapshot
	// GitRef is the commit being published, if known.
	GitRef string
	// Draft publishes the version as a mutable draft.
	Draft bool
}

// Dictionary maps ontology names to the stable integer identities the object
// store persists. It is the bridge between the metadata plane and the data
// plane.
type Dictionary struct {
	// Ontology is the version this dictionary was resolved from.
	Ontology Ontology
	// Types maps a qualified type name ("app/Customer") to its catalog id.
	Types map[string]int32
	// TypeNames maps a catalog type id back to its qualified name.
	TypeNames map[int32]string
	// Properties maps a catalog type id to that type's property ids by name.
	Properties map[int32]map[string]int32
	// PropertyNames maps a catalog property id back to its name.
	PropertyNames map[int32]string
	// PropertyTypes maps a catalog property id to its ontology data type.
	PropertyTypes map[int32]string
	// PrimaryKeys maps a catalog type id to the property id of its primary key.
	PrimaryKeys map[int32]int32
}

// TypeID returns the catalog id of a qualified type name.
func (d *Dictionary) TypeID(qualifiedName string) (int32, bool) {
	typeID, ok := d.Types[qualifiedName]
	return typeID, ok
}

// PropertyID returns the catalog id of a property on a type.
func (d *Dictionary) PropertyID(typeID int32, property string) (int32, bool) {
	properties, ok := d.Properties[typeID]
	if !ok {
		return 0, false
	}
	propertyID, ok := properties[property]
	return propertyID, ok
}

// Interface is the ontology registry.
type Interface interface {
	// Publish stores a compiled snapshot as a version. Publishing a version
	// that already exists is an error unless the content is identical, in which
	// case it is a no-op, or unless the existing version is a draft, which is
	// replaced.
	Publish(ctx context.Context, request PublishRequest) (*Ontology, error)

	// Get returns the metadata of one version.
	Get(ctx context.Context, name, version string) (*Ontology, error)

	// GetSnapshot returns the compiled ontology of one version.
	GetSnapshot(ctx context.Context, name, version string) (*snapshot.Snapshot, error)

	// List returns every version of an ontology, newest first.
	List(ctx context.Context, name string) ([]Ontology, error)

	// Resolve returns the version a tag points at.
	Resolve(ctx context.Context, name, tag string) (*Ontology, error)

	// ResolveSnapshot returns the compiled ontology a tag points at.
	ResolveSnapshot(ctx context.Context, name, tag string) (*snapshot.Snapshot, error)

	// Tag points a tag at a published version.
	Tag(ctx context.Context, name, tag, version string) (*Ontology, error)

	// Promote points toTag at whatever fromTag currently points at.
	Promote(ctx context.Context, name, fromTag, toTag string) (*Ontology, error)

	// Rollback returns a tag to the version it pointed at before its last move.
	Rollback(ctx context.Context, name, tag string) (*Ontology, error)

	// Deprecate marks a published version as no longer recommended.
	Deprecate(ctx context.Context, name, version string) (*Ontology, error)

	// Dictionary returns the stable type and property identities of a version.
	Dictionary(ctx context.Context, name, version string) (*Dictionary, error)

	// ResolveDictionary returns the stable identities of the version a tag
	// points at.
	ResolveDictionary(ctx context.Context, name, tag string) (*Dictionary, error)
}
