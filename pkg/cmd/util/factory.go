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

// Package util holds the plumbing shared by every fab command: the Factory
// that turns flags into working objects, and the error handling every command
// funnels through.
package util

import (
	"context"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	registryclient "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/client"
)

// Factory provides commands with everything they need that depends on flags or
// the environment. Commands take a Factory rather than concrete types so that
// tests can substitute fakes, and so that a command never has to know how the
// foundry was located or how the registry is reached.
type Factory interface {
	// FoundryRoot returns the resolved foundry root directory.
	FoundryRoot() (string, error)
	// LoaderOptions returns the schema loader configuration for the foundry.
	LoaderOptions() (loader.Options, error)
	// OntologyName returns the name published versions are stored under.
	OntologyName() (string, error)
	// Registry returns a client for the ontology registry server.
	Registry(ctx context.Context) (registry.Interface, error)
}

// FoundryLocator is the part of the flag set a Factory is built from. It is
// satisfied by genericclioptions.ConfigFlags.
type FoundryLocator interface {
	// FoundryRoot resolves the foundry root from flags and environment.
	FoundryRoot() (string, error)
	// LoaderOptions builds the loader configuration from flags.
	LoaderOptions() (loader.Options, error)
	// ToRegistryURL resolves the base URL of the registry server.
	ToRegistryURL() (string, error)
	// ToOntologyName resolves the ontology name.
	ToOntologyName() (string, error)
}

type factoryImpl struct {
	locator FoundryLocator
}

// NewFactory returns a Factory backed by the given flags.
func NewFactory(locator FoundryLocator) Factory {
	return &factoryImpl{locator: locator}
}

func (f *factoryImpl) FoundryRoot() (string, error) {
	return f.locator.FoundryRoot()
}

func (f *factoryImpl) LoaderOptions() (loader.Options, error) {
	return f.locator.LoaderOptions()
}

func (f *factoryImpl) OntologyName() (string, error) {
	return f.locator.ToOntologyName()
}

// Registry returns a client for the registry server. The CLI never connects to
// the registry database: the server owns the schema, so a stale fab binary
// cannot write rows a newer server would not recognise.
func (f *factoryImpl) Registry(context.Context) (registry.Interface, error) {
	url, err := f.locator.ToRegistryURL()
	if err != nil {
		return nil, err
	}
	return registryclient.New(url)
}
