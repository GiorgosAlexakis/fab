// Package v1 contains the Layer API type: the layer.yaml manifest at the root of
// every layer.
//
// The import path is group then version: internal/layer/v1. A later layer/v2 is
// a package beside this one rather than an edit to it. Documents still carry
// apiVersion fab/v1: that is the wire vocabulary, and every kind fab reads
// shares it.
package v1

const GroupName = "fab"

const Version = "v1"

const APIVersion = GroupName + "/" + Version

const LayerKind = "Layer"

type Layer struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Metadata   LayerMetadata `json:"metadata"`
	Spec       LayerSpec     `json:"spec,omitempty"`
}

type Origin string

const (
	OriginUpstream Origin = "upstream"
	OriginLocal    Origin = "local"
)

type LayerMetadata struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Origin      Origin `json:"origin,omitempty"`
	Description string `json:"description,omitempty"`
}

type LayerSpec struct {
	DependsOn []Dependency `json:"dependsOn,omitempty"`
}

type Dependency struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}
