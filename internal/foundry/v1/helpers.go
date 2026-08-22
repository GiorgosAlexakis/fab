package v1

import (
	"errors"
	"fmt"
)

// ErrLayerAlreadyDeclared is returned by AddLayer. A foundry activates a layer
// once, and silently replacing the range would discard a deliberate pin.
var ErrLayerAlreadyDeclared = errors.New("layer is already declared")

// NewFoundry returns a defaulted foundry with the given name.
func NewFoundry(name string) *Foundry {
	foundry := &Foundry{Metadata: Metadata{Name: name}}
	SetDefaults_Foundry(foundry)
	return foundry
}

// AddLayer activates a layer at a version range, and refuses one that is
// already activated.
func (foundry *Foundry) AddLayer(name, versionRange string) error {
	if _, declared := foundry.Selects(name); declared {
		return fmt.Errorf("%q: %w", name, ErrLayerAlreadyDeclared)
	}
	foundry.Spec.Layers = append(foundry.Spec.Layers, LayerSelector{Name: name, Version: versionRange})
	return nil
}

// LayerNames returns the activated layers in the order they are declared.
func (foundry *Foundry) LayerNames() []string {
	names := make([]string, 0, len(foundry.Spec.Layers))
	for _, selector := range foundry.Spec.Layers {
		names = append(names, selector.Name)
	}
	return names
}

// Selects returns the range a layer is activated at, and whether it is
// activated at all. An activated layer with no range returns an empty string
// and true, which is not the same as not being activated.
func (foundry *Foundry) Selects(name string) (string, bool) {
	for _, selector := range foundry.Spec.Layers {
		if selector.Name == name {
			return selector.Version, true
		}
	}
	return "", false
}
