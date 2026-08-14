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

// Layer is the manifest at the root of a layer: what it is, what it needs and
// what schema it contributes.
//
// The manifest is what makes a directory a layer. Resolving the layer graph reads
// only manifests, so a layer's schema, code and infrastructure are never touched
// to work out the merge order.
type Layer struct {
	// APIVersion is the versioned schema of this document. Always fab/v1.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is always Layer.
	Kind string `json:"kind,omitempty"`
	// Metadata identifies the layer.
	Metadata LayerMeta `json:"metadata"`
	// Spec declares what the layer needs and provides.
	Spec LayerSpec `json:"spec,omitempty"`
}

// Origin records who owns a layer, which decides whether fab may upgrade it.
type Origin string

const (
	// OriginUpstream is a fab-provided layer, made available under layers/ out of
	// the local cache. It is never edited in place and never forked: extend it
	// with an aspect, a hook or a wrapping layer.
	OriginUpstream Origin = "upstream"
	// OriginLocal is a company-owned layer living in the foundry repo. fab never
	// upgrades it, because there is no upstream to upgrade it from.
	OriginLocal Origin = "local"
)

// LayerMeta identifies a layer.
type LayerMeta struct {
	// Name is the layer name, e.g. meta-auth. It must match the directory the
	// manifest was loaded from.
	Name string `json:"name"`
	// Version is the layer's own semantic version. Other layers depend on it
	// through a range, so this is the number those ranges are matched against.
	Version string `json:"version"`
	// Origin records whether the layer is fab-provided or company-owned. It
	// defaults to local, because a layer fab did not put there is yours.
	Origin Origin `json:"origin,omitempty"`
	// Description documents the layer for `fab layers` and generated docs.
	Description string `json:"description,omitempty"`
}

// LayerSpec declares a layer's dependencies and contributions.
type LayerSpec struct {
	// DependsOn are the layers this one needs, each at a version range. A
	// dependency must be satisfied by an active layer; fab does not activate a
	// layer just because something depends on it.
	DependsOn []Dependency `json:"dependsOn,omitempty"`
	// Provides declares what the layer contributes. It is a manifest of intent
	// that fab checks the tree against.
	Provides Provides `json:"provides,omitempty"`
}

// Dependency requires another layer within a version range.
type Dependency struct {
	// Name is the required layer's name.
	Name string `json:"name"`
	// Version is a semantic version range, e.g. ">=1.0.0, <2.0.0".
	//
	// The upper bound is the point of it. Without one, a breaking major release
	// of a dependency silently breaks this layer.
	Version string `json:"version"`
}

// Provides declares a layer's contributions.
type Provides struct {
	// Schema is the ontology this layer contributes.
	Schema *SchemaProvides `json:"schema,omitempty"`
}

// SchemaProvides names the ontology documents a layer ships. Kinds fab does not
// compile yet are still declarable, so a layer written against a later phase of
// the ontology parses today rather than failing to load.
type SchemaProvides struct {
	// Objects are the object type names.
	Objects []string `json:"objects,omitempty"`
	// Links are the link type names.
	Links []string `json:"links,omitempty"`
	// Aspects are the aspect names. Phase 2.
	Aspects []string `json:"aspects,omitempty"`
	// Interfaces are the interface names. Phase 2.
	Interfaces []string `json:"interfaces,omitempty"`
	// Actions are the action type names. Phase 3.
	Actions []string `json:"actions,omitempty"`
}
