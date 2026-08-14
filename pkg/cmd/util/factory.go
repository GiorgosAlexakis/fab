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
	"sync"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
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
	// OntologyTag returns the environment tag data plane commands bind to.
	OntologyTag() (string, error)
	// Registry returns a connected ontology registry. The connection is opened
	// on first use and shared by every command in the process.
	Registry(ctx context.Context) (registry.Interface, error)
	// RegistryDB returns the registry's database handle, for commands that
	// operate on the schema itself rather than on its contents.
	RegistryDB(ctx context.Context) (storage.Beginner, error)
	// ObjectStoreDB returns the object store's database handle. It may be the
	// same database as the registry, or a different one.
	ObjectStoreDB(ctx context.Context) (storage.Beginner, error)
	// Close releases any connections the factory opened.
	Close()
}

// FoundryLocator is the part of the flag set a Factory is built from. It is
// satisfied by genericclioptions.ConfigFlags.
type FoundryLocator interface {
	// FoundryRoot resolves the foundry root from flags and environment.
	FoundryRoot() (string, error)
	// LoaderOptions builds the loader configuration from flags.
	LoaderOptions() (loader.Options, error)
	// ToRegistryURL resolves the registry connection string.
	ToRegistryURL() (string, error)
	// ToObjectStoreURL resolves the object store connection string.
	ToObjectStoreURL() (string, error)
	// ToOntologyName resolves the ontology name.
	ToOntologyName() (string, error)
	// ToOntologyTag resolves the environment tag to bind to.
	ToOntologyTag() (string, error)
}

type factoryImpl struct {
	locator FoundryLocator

	// Pools are opened lazily and keyed by URL, so that a command needing the
	// registry twice does not open two pools, and so that the common
	// single-database deployment shares one pool between the two planes.
	mutex sync.Mutex
	pools map[string]*storage.Pool
}

// NewFactory returns a Factory backed by the given flags.
func NewFactory(locator FoundryLocator) Factory {
	return &factoryImpl{locator: locator, pools: map[string]*storage.Pool{}}
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

func (f *factoryImpl) OntologyTag() (string, error) {
	return f.locator.ToOntologyTag()
}

func (f *factoryImpl) Registry(ctx context.Context) (registry.Interface, error) {
	db, err := f.RegistryDB(ctx)
	if err != nil {
		return nil, err
	}
	return registrypostgres.New(db), nil
}

func (f *factoryImpl) RegistryDB(ctx context.Context) (storage.Beginner, error) {
	url, err := f.locator.ToRegistryURL()
	if err != nil {
		return nil, err
	}
	return f.poolFor(ctx, url)
}

func (f *factoryImpl) ObjectStoreDB(ctx context.Context) (storage.Beginner, error) {
	url, err := f.locator.ToObjectStoreURL()
	if err != nil {
		return nil, err
	}
	return f.poolFor(ctx, url)
}

func (f *factoryImpl) poolFor(ctx context.Context, url string) (storage.Beginner, error) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if pool, ok := f.pools[url]; ok {
		return pool, nil
	}
	pool, err := storage.Open(ctx, url)
	if err != nil {
		return nil, err
	}
	f.pools[url] = pool
	return pool, nil
}

func (f *factoryImpl) Close() {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	for url, pool := range f.pools {
		pool.Close()
		delete(f.pools, url)
	}
}
