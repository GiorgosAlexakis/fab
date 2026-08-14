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

// Package genericclioptions holds the flags that are common to every fab
// command: where the foundry is and how its directories are laid out.
package genericclioptions

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
)

const (
	// FoundryConfigFile marks the root of a foundry.
	FoundryConfigFile = "foundry.yaml"
	// FoundryRootEnvVar overrides foundry discovery.
	FoundryRootEnvVar = "FAB_ROOT"
)

// ConfigFlags composes the foundry location flags. Commands never read these
// fields directly; they go through a Factory.
type ConfigFlags struct {
	// Root is the foundry root. Empty means "discover it".
	Root *string
	// SchemaDir is the company schema directory, relative to the root.
	SchemaDir *string
	// LayersDir is the layers directory, relative to the root.
	LayersDir *string
	// AppLayer is the layer name for documents in SchemaDir.
	AppLayer *string
}

// NewConfigFlags returns ConfigFlags with the standard foundry layout.
func NewConfigFlags() *ConfigFlags {
	return &ConfigFlags{
		Root:      stringptr(""),
		SchemaDir: stringptr(loader.DefaultSchemaDir),
		LayersDir: stringptr(loader.DefaultLayersDir),
		AppLayer:  stringptr(loader.DefaultAppLayer),
	}
}

// AddFlags binds the foundry flags to the given flag set.
func (f *ConfigFlags) AddFlags(flags *pflag.FlagSet) {
	if f.Root != nil {
		flags.StringVar(f.Root, "root", *f.Root,
			fmt.Sprintf("Path to the foundry root. Defaults to $%s, else the nearest ancestor directory containing %s.",
				FoundryRootEnvVar, FoundryConfigFile))
	}
	if f.SchemaDir != nil {
		flags.StringVar(f.SchemaDir, "schema-dir", *f.SchemaDir,
			"Directory holding the company's schema documents, relative to the foundry root.")
	}
	if f.LayersDir != nil {
		flags.StringVar(f.LayersDir, "layers-dir", *f.LayersDir,
			"Directory holding the active layers, relative to the foundry root.")
	}
	if f.AppLayer != nil {
		flags.StringVar(f.AppLayer, "app-layer", *f.AppLayer,
			"Layer name assigned to the documents in --schema-dir.")
	}
}

// FoundryRoot resolves the foundry root, in precedence order: the --root flag,
// $FAB_ROOT, the nearest ancestor of the working directory containing
// foundry.yaml, and finally the working directory itself.
//
// The last fallback exists so that `fab schema validate --schema-dir ...` works
// in a bare directory, before `fab init` has produced a foundry.yaml.
func (f *ConfigFlags) FoundryRoot() (string, error) {
	if f.Root != nil && *f.Root != "" {
		return filepath.Abs(*f.Root)
	}
	if fromEnv := os.Getenv(FoundryRootEnvVar); fromEnv != "" {
		return filepath.Abs(fromEnv)
	}

	workingDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("determining the working directory: %w", err)
	}
	if root, found := findFoundryRoot(workingDir); found {
		return root, nil
	}
	return workingDir, nil
}

// LoaderOptions returns the loader configuration for the resolved foundry.
func (f *ConfigFlags) LoaderOptions() (loader.Options, error) {
	root, err := f.FoundryRoot()
	if err != nil {
		return loader.Options{}, err
	}

	options := loader.Options{Root: root}
	if f.SchemaDir != nil {
		options.SchemaDir = *f.SchemaDir
	}
	if f.LayersDir != nil {
		options.LayersDir = *f.LayersDir
	}
	if f.AppLayer != nil {
		options.AppLayer = *f.AppLayer
	}
	options.SetDefaults()
	return options, nil
}

// findFoundryRoot walks up from dir looking for a foundry.yaml.
func findFoundryRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, FoundryConfigFile)); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func stringptr(value string) *string {
	return &value
}
