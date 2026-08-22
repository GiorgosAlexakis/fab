package v1

import (
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

const LockKind = "Lock"

type Lock struct {
	APIVersion string        `json:"apiVersion,omitempty"`
	Kind       string        `json:"kind,omitempty"`
	Bundle     *Bundle       `json:"bundle,omitempty"`
	Locked     []LockedLayer `json:"locked"`
}

type Bundle struct {
	URL    string   `json:"url,omitempty"`
	Ref    string   `json:"ref,omitempty"`
	GitRef string   `json:"gitRef,omitempty"`
	Layers []string `json:"layers,omitempty"`
}

type LockedLayer struct {
	Name      string         `json:"name"`
	Version   string         `json:"version"`
	Origin    layerv1.Origin `json:"origin,omitempty"`
	Digest    string         `json:"digest,omitempty"`
	DependsOn []string       `json:"dependsOn,omitempty"`
}
