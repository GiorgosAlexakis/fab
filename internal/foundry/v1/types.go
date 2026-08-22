// Package v1 contains the Foundry API: the foundry.yaml and foundry.lock types,
// and the engine that creates and stores them.
//
// The import path is group then version: internal/foundry/v1. Documents still
// carry apiVersion fab/v1: that is the wire vocabulary, and every kind fab
// reads shares it.
package v1

const GroupName = "fab"

const Version = "v1"

const APIVersion = GroupName + "/" + Version

const FoundryKind = "Foundry"

type Foundry struct {
	APIVersion string      `json:"apiVersion,omitempty"`
	Kind       string      `json:"kind,omitempty"`
	Metadata   Metadata    `json:"metadata,omitempty"`
	Spec       FoundrySpec `json:"spec,omitempty"`
}

type Metadata struct {
	Name string `json:"name,omitempty"`
}

type FoundrySpec struct {
	Layers []LayerSelector `json:"layers,omitempty"`
}

type LayerSelector struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}
