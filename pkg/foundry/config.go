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

// Package foundry reads and writes foundry.yaml, the one file an FDE edits to
// say what their stack is made of.
//
// Only the layer selection is read today, because that is what the ontology
// needs: the layers a foundry activates decide which schema documents are
// merged and in what order. Adapter selection, the cloud target and the MCP
// server are the build and services work, and will extend this type rather than
// introduce a second parser.
package foundry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"
)

// ConfigFileName is the name of the foundry configuration file.
const ConfigFileName = "foundry.yaml"

// Kind is the kind a foundry.yaml document carries.
const Kind = "Foundry"

// APIVersion is the apiVersion a foundry.yaml document carries.
const APIVersion = "fab/v1"

// Config is the subset of foundry.yaml that fab reads today.
type Config struct {
	// APIVersion is the versioned schema of this file.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is always Foundry.
	Kind string `json:"kind,omitempty"`
	// Metadata names the foundry.
	Metadata Metadata `json:"metadata,omitempty"`
	// Spec selects what the foundry is built from.
	Spec Spec `json:"spec,omitempty"`
}

// Metadata names a foundry.
type Metadata struct {
	// Name is the ontology name every published version is stored under. It is
	// typically the company name, e.g. acme-corp.
	Name string `json:"name,omitempty"`
}

// Spec selects the layers a foundry activates.
type Spec struct {
	// Layers are the activated layers, each at a version range.
	Layers []LayerSelector `json:"layers,omitempty"`
}

// LayerSelector activates one layer at a version range.
type LayerSelector struct {
	// Name is the layer name.
	Name string `json:"name,omitempty"`
	// Version is a semantic version range, e.g. ">=1.0.0, <2.0.0".
	Version string `json:"version,omitempty"`
}

// ErrNotFound reports that a directory holds no foundry.yaml.
var ErrNotFound = errors.New("no foundry.yaml")

// Load reads the foundry.yaml in root. It returns an error wrapping ErrNotFound
// when the file is absent, so callers can fall back to flags.
//
// Decoding is lenient: this file grows keys that older fab binaries do not know
// about, and refusing to read it would make every new key a forced CLI upgrade.
func Load(root string) (*Config, error) {
	path := filepath.Join(root, ConfigFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrNotFound)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	config := &Config{}
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	config.SetDefaults()
	return config, nil
}

// SetDefaults fills in the fields fab can infer.
func (c *Config) SetDefaults() {
	if c.APIVersion == "" {
		c.APIVersion = APIVersion
	}
	if c.Kind == "" {
		c.Kind = Kind
	}
}

// LayerNames returns the names of every declared layer.
func (c *Config) LayerNames() []string {
	names := make([]string, 0, len(c.Spec.Layers))
	for _, selector := range c.Spec.Layers {
		names = append(names, selector.Name)
	}
	return names
}

// Selects reports whether a layer is declared, and the range it is declared at.
func (c *Config) Selects(name string) (string, bool) {
	for _, selector := range c.Spec.Layers {
		if selector.Name == name {
			return selector.Version, true
		}
	}
	return "", false
}

// OntologyName returns the ontology name declared in root's foundry.yaml, or
// the empty string when there is no foundry.yaml or it names nothing.
func OntologyName(root string) (string, error) {
	config, err := Load(root)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return config.Metadata.Name, nil
}
