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

// Package foundry reads foundry.yaml, the file an FDE edits to select layers
// and adapters.
//
// Only the fields the ontology work needs are read today: the ontology name and
// the declared layer names. Layer resolution, adapter selection and BSP
// selection are the layers and build work, and will extend this type rather
// than introduce a second parser.
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
	// Layers are the activated layers.
	Layers []LayerSelector `json:"layers,omitempty"`
}

// LayerSelector activates one layer at a version range.
type LayerSelector struct {
	// Name is the layer name.
	Name string `json:"name,omitempty"`
	// Version is a semantic version range.
	Version string `json:"version,omitempty"`
}

// ErrNotFound reports that a directory holds no foundry.yaml.
var ErrNotFound = errors.New("no foundry.yaml")

// Load reads the foundry.yaml in root. It returns an error wrapping ErrNotFound
// when the file is absent, so callers can fall back to flags.
//
// Decoding is lenient: this file will grow keys that older fab binaries do not
// know about, and refusing to read it would make every layer addition a CLI
// upgrade.
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
	return config, nil
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
