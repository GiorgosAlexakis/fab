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
// point a command at a foundry on disk and at a registry of its choosing
// without going through flag parsing or environment variables.
package cmdtesting

import (
	"context"
	"errors"

	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// TestFactory is a Factory whose dependencies are set directly by a test.
type TestFactory struct {
	// Root is the foundry root commands should load schema documents from.
	Root string
	// Name is the ontology name commands should publish under.
	Name string
	// Tag is the environment tag object commands bind to.
	Tag string
	// RegistryClient is returned by Registry. A nil value makes Registry fail,
	// which is what a test that must not touch the registry wants.
	RegistryClient registry.Interface
	// DB is returned by RegistryDB and, unless ObjectStore is set, by
	// ObjectStoreDB.
	DB storage.Beginner
	// ObjectStoreDatabase is returned by ObjectStoreDB when the test puts the
	// two planes in different databases.
	ObjectStoreDatabase storage.Beginner
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

// WithOntologyTag sets the environment tag object commands will bind to.
func (f *TestFactory) WithOntologyTag(tag string) *TestFactory {
	f.Tag = tag
	return f
}

// WithRegistry sets the registry commands will talk to.
func (f *TestFactory) WithRegistry(client registry.Interface) *TestFactory {
	f.RegistryClient = client
	return f
}

// WithRegistryDB sets the database handle commands will use.
func (f *TestFactory) WithRegistryDB(db storage.Beginner) *TestFactory {
	f.DB = db
	return f
}

// WithObjectStoreDB points the object store at a database of its own.
func (f *TestFactory) WithObjectStoreDB(db storage.Beginner) *TestFactory {
	f.ObjectStoreDatabase = db
	return f
}

// FoundryRoot returns the configured root.
func (f *TestFactory) FoundryRoot() (string, error) {
	if f.Root == "" {
		return "", errors.New("no foundry root configured in the test factory")
	}
	return f.Root, nil
}

// LoaderOptions returns loader options for the configured root.
func (f *TestFactory) LoaderOptions() (loader.Options, error) {
	root, err := f.FoundryRoot()
	if err != nil {
		return loader.Options{}, err
	}
	options := loader.Options{Root: root}
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

// Registry returns the configured registry.
func (f *TestFactory) Registry(context.Context) (registry.Interface, error) {
	if f.RegistryClient == nil {
		return nil, errors.New("no registry configured in the test factory")
	}
	return f.RegistryClient, nil
}

// OntologyTag returns the configured environment tag.
func (f *TestFactory) OntologyTag() (string, error) {
	if f.Tag == "" {
		return "", errors.New("no ontology tag configured in the test factory")
	}
	return f.Tag, nil
}

// RegistryDB returns the configured database handle.
func (f *TestFactory) RegistryDB(context.Context) (storage.Beginner, error) {
	if f.DB == nil {
		return nil, errors.New("no registry database configured in the test factory")
	}
	return f.DB, nil
}

// ObjectStoreDB returns the object store handle, defaulting to the registry's:
// one database is the common deployment, and a test that needs two says so.
func (f *TestFactory) ObjectStoreDB(ctx context.Context) (storage.Beginner, error) {
	if f.ObjectStoreDatabase != nil {
		return f.ObjectStoreDatabase, nil
	}
	return f.RegistryDB(ctx)
}

// Close is a no-op: the test owns the lifetime of anything it configured.
func (f *TestFactory) Close() {}
