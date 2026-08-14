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

// Package cmdtesting provides a Factory for command tests, so that a test can
// point a command at a foundry on disk without going through flag parsing or
// environment variables.
package cmdtesting

import (
	"errors"

	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/layers"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
)

// TestFactory is a Factory whose dependencies are set directly by a test.
type TestFactory struct {
	// Root is the foundry root commands should load schema documents from.
	Root string
	// Name is the ontology name commands should publish under.
	Name string
}

var _ cmdutil.Factory = &TestFactory{}

// NewTestFactory returns a factory pointed at a foundry root.
func NewTestFactory(root string) *TestFactory {
	return &TestFactory{Root: root}
}

// WithOntologyName sets the ontology name commands will use.
func (f *TestFactory) WithOntologyName(name string) *TestFactory {
	f.Name = name
	return f
}

// FoundryRoot returns the configured root.
func (f *TestFactory) FoundryRoot() (string, error) {
	if f.Root == "" {
		return "", errors.New("no foundry root configured in the test factory")
	}
	return f.Root, nil
}

// Foundry resolves the layer graph of the configured root.
func (f *TestFactory) Foundry() (*layers.Foundry, error) {
	root, err := f.FoundryRoot()
	if err != nil {
		return nil, err
	}
	return layers.ResolveFoundry(root, "")
}

// LoaderOptions returns loader options for the configured root, with the layers
// the resolver found.
func (f *TestFactory) LoaderOptions() (loader.Options, error) {
	resolved, err := f.Foundry()
	if err != nil {
		return loader.Options{}, err
	}

	options := loader.Options{Root: resolved.Root, Layers: resolved.Resolution.Names()}
	options.SetDefaults()
	return options, nil
}

// OntologyName returns the configured ontology name.
func (f *TestFactory) OntologyName() (string, error) {
	if f.Name == "" {
		return "", errors.New("no ontology name configured in the test factory")
	}
	return f.Name, nil
}
