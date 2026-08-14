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

	"github.com/GiorgosAlexakis/fab/pkg/foundry"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
)

const (
	// FoundryConfigFile marks the root of a foundry.
	FoundryConfigFile = foundry.ConfigFileName
	// FoundryRootEnvVar overrides foundry discovery.
	FoundryRootEnvVar = "FAB_ROOT"
	// RegistryURLEnvVar holds the ontology registry connection string.
	RegistryURLEnvVar = "FAB_REGISTRY_URL"
	// ObjectStoreURLEnvVar holds the object store connection string.
	ObjectStoreURLEnvVar = "FAB_OBJECT_STORE_URL"
	// OntologyNameEnvVar overrides the ontology name from foundry.yaml.
	OntologyNameEnvVar = "FAB_ONTOLOGY_NAME"
	// OntologyTagEnvVar names the environment tag data plane commands bind to.
	OntologyTagEnvVar = "FAB_ONTOLOGY_TAG"
)

// ConfigFlags composes the flags every command shares: where the foundry is and
// how to reach the runtime stores. Commands never read these fields directly;
// they go through a Factory.
type ConfigFlags struct {
	// Root is the foundry root. Empty means "discover it".
	Root *string
	// SchemaDir is the company schema directory, relative to the root.
	SchemaDir *string
	// LayersDir is the layers directory, relative to the root.
	LayersDir *string
	// AppLayer is the layer name for documents in SchemaDir.
	AppLayer *string
	// RegistryURL is the PostgreSQL URL of the ontology registry.
	RegistryURL *string
	// ObjectStoreURL is the PostgreSQL URL of the object store. Empty means
	// "the same database as the registry".
	ObjectStoreURL *string
	// OntologyName overrides the name from foundry.yaml.
	OntologyName *string
	// OntologyTag is the environment tag data plane commands bind to.
	OntologyTag *string
}

// NewConfigFlags returns ConfigFlags with the standard foundry layout.
func NewConfigFlags() *ConfigFlags {
	return &ConfigFlags{
		Root:           stringptr(""),
		SchemaDir:      stringptr(loader.DefaultSchemaDir),
		LayersDir:      stringptr(loader.DefaultLayersDir),
		AppLayer:       stringptr(loader.DefaultAppLayer),
		RegistryURL:    stringptr(""),
		ObjectStoreURL: stringptr(""),
		OntologyName:   stringptr(""),
		OntologyTag:    stringptr(""),
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
	if f.RegistryURL != nil {
		flags.StringVar(f.RegistryURL, "registry-url", *f.RegistryURL,
			fmt.Sprintf("PostgreSQL URL of the ontology registry. Defaults to $%s.", RegistryURLEnvVar))
	}
	if f.ObjectStoreURL != nil {
		flags.StringVar(f.ObjectStoreURL, "object-store-url", *f.ObjectStoreURL,
			fmt.Sprintf("PostgreSQL URL of the object store. Defaults to $%s, else the registry URL.",
				ObjectStoreURLEnvVar))
	}
	if f.OntologyName != nil {
		flags.StringVar(f.OntologyName, "ontology-name", *f.OntologyName,
			fmt.Sprintf("Ontology name. Defaults to $%s, else metadata.name from %s.",
				OntologyNameEnvVar, FoundryConfigFile))
	}
	if f.OntologyTag != nil {
		flags.StringVar(f.OntologyTag, "ontology-tag", *f.OntologyTag,
			fmt.Sprintf("Environment tag whose ontology version object commands bind to. Defaults to $%s.",
				OntologyTagEnvVar))
	}
}

// ToRegistryURL resolves the registry connection string from the flag or the
// environment.
func (f *ConfigFlags) ToRegistryURL() (string, error) {
	if f.RegistryURL != nil && *f.RegistryURL != "" {
		return *f.RegistryURL, nil
	}
	if fromEnv := os.Getenv(RegistryURLEnvVar); fromEnv != "" {
		return fromEnv, nil
	}
	return "", fmt.Errorf("no ontology registry configured: pass --registry-url or set $%s", RegistryURLEnvVar)
}

// ToObjectStoreURL resolves the object store connection string, falling back to
// the registry URL: the two planes are separable, but one database is the common
// deployment.
func (f *ConfigFlags) ToObjectStoreURL() (string, error) {
	if f.ObjectStoreURL != nil && *f.ObjectStoreURL != "" {
		return *f.ObjectStoreURL, nil
	}
	if fromEnv := os.Getenv(ObjectStoreURLEnvVar); fromEnv != "" {
		return fromEnv, nil
	}
	url, err := f.ToRegistryURL()
	if err != nil {
		return "", fmt.Errorf("no object store configured: pass --object-store-url, set $%s, "+
			"or configure the registry, which the object store defaults to", ObjectStoreURLEnvVar)
	}
	return url, nil
}

// ToOntologyTag resolves the environment tag data plane commands bind to. There
// is no default: which ontology version a write is validated against is not
// something to guess at.
func (f *ConfigFlags) ToOntologyTag() (string, error) {
	if f.OntologyTag != nil && *f.OntologyTag != "" {
		return *f.OntologyTag, nil
	}
	if fromEnv := os.Getenv(OntologyTagEnvVar); fromEnv != "" {
		return fromEnv, nil
	}
	return "", fmt.Errorf("no ontology tag: pass --ontology-tag or set $%s", OntologyTagEnvVar)
}

// ToOntologyName resolves the ontology name, in precedence order: the
// --ontology-name flag, $FAB_ONTOLOGY_NAME, and metadata.name in foundry.yaml.
func (f *ConfigFlags) ToOntologyName() (string, error) {
	if f.OntologyName != nil && *f.OntologyName != "" {
		return *f.OntologyName, nil
	}
	if fromEnv := os.Getenv(OntologyNameEnvVar); fromEnv != "" {
		return fromEnv, nil
	}

	root, err := f.FoundryRoot()
	if err != nil {
		return "", err
	}
	name, err := foundry.OntologyName(root)
	if err != nil {
		return "", err
	}
	if name == "" {
		return "", fmt.Errorf(
			"no ontology name: pass --ontology-name, set $%s, or set metadata.name in %s",
			OntologyNameEnvVar, filepath.Join(root, FoundryConfigFile))
	}
	return name, nil
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
