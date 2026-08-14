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

package layers

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/GiorgosAlexakis/fab/pkg/foundry"
)

// Foundry is a foundry's composition: what it declares and what that resolves to.
type Foundry struct {
	// Root is the foundry root.
	Root string
	// Config is the parsed foundry.yaml. A directory without one gets an empty
	// config, so that a bare schema directory still compiles.
	Config *foundry.Config
	// Resolution is the active layers in build order.
	Resolution *Resolution
}

// ResolveFoundry reads foundry.yaml and the layer manifests under root, and
// resolves them into a build order.
//
// layersDir is relative to root; it defaults to layers/.
func ResolveFoundry(root, layersDir string) (*Foundry, error) {
	if layersDir == "" {
		layersDir = DefaultLayersDir
	}

	config, err := foundry.Load(root)
	switch {
	case err == nil:
		if errs := foundry.Validate(config); len(errs) > 0 {
			return nil, fmt.Errorf("%s is not valid: %w",
				filepath.Join(root, foundry.ConfigFileName), errs.ToAggregate())
		}
	case errors.Is(err, foundry.ErrNotFound):
		// `fab schema validate` in a bare directory with a schema/ folder is a
		// useful thing to be able to do, and it needs no layers.
		config = &foundry.Config{}
		config.SetDefaults()
	default:
		return nil, err
	}

	discovered, err := Discover(filepath.Join(root, layersDir))
	if err != nil {
		return nil, err
	}

	resolution, err := Resolve(config, discovered)
	if err != nil {
		return nil, err
	}
	return &Foundry{Root: root, Config: config, Resolution: resolution}, nil
}
