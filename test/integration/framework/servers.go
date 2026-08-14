//go:build integration

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

package framework

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore/server"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	registryclient "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/client"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	registryserver "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/server"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// NewRegistryServer serves a migrated registry over HTTP and returns a client
// for it, so a test can exercise the path a real caller takes: client, HTTP,
// server, PostgreSQL.
func NewRegistryServer(t *testing.T, db storage.Beginner) (registry.Interface, string) {
	t.Helper()

	ctx := context.Background()
	if _, err := registrypostgres.Migrate(ctx, db); err != nil {
		t.Fatalf("migrating the registry: %v", err)
	}

	httpServer := httptest.NewServer(registryserver.New(registrypostgres.New(db), nil))
	t.Cleanup(httpServer.Close)

	client, err := registryclient.New(httpServer.URL)
	if err != nil {
		t.Fatalf("building a registry client for %s: %v", httpServer.URL, err)
	}
	return client.WithHTTPClient(httpServer.Client()), httpServer.URL
}

// NewObjectStoreServer serves the object store over HTTP against the ontology
// version the given tag points at, and returns its base URL.
func NewObjectStoreServer(t *testing.T, db storage.Beginner, reg registry.Interface,
	ontologyName, tag string) string {
	t.Helper()

	// A short TTL keeps the test honest about rebinding without making it slow:
	// promoting a tag has to become visible without restarting the server.
	resolver, err := server.NewPostgresResolver(db, reg, ontologyName, tag, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("building the object store resolver: %v", err)
	}

	httpServer := httptest.NewServer(server.New(resolver, nil))
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}
