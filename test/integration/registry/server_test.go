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

package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

// TestServerRoundTrip publishes and reads an ontology through the registry API,
// checking that what comes back over HTTP is what the store holds.
func TestServerRoundTrip(t *testing.T) {
	client, _ := framework.NewRegistryServer(t, framework.NewDatabase(t))
	ctx := context.Background()
	compiled := framework.SampleOntology(t)

	published, err := client.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: compiled, GitRef: "179a9f6",
	})
	if err != nil {
		t.Fatalf("publishing through the API: %v", err)
	}

	digest, err := compiled.Digest()
	if err != nil {
		t.Fatalf("digesting the compiled ontology: %v", err)
	}
	if published.Digest != digest {
		t.Errorf("published digest = %s, want %s", published.Digest, digest)
	}
	if published.Status != registry.StatusPublished {
		t.Errorf("published status = %s, want %s", published.Status, registry.StatusPublished)
	}
	if published.GitRef != "179a9f6" {
		t.Errorf("published gitRef = %q, want 179a9f6", published.GitRef)
	}

	// The snapshot has to survive JSON in both directions: the OSDK generator
	// and the object store read it back and expect the same ontology.
	fetched, err := client.GetSnapshot(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("fetching the snapshot: %v", err)
	}
	if !reflect.DeepEqual(fetched, compiled) {
		t.Error("the snapshot fetched over HTTP differs from the one published")
	}

	dictionary, err := client.Dictionary(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("fetching the dictionary: %v", err)
	}
	typeID, ok := dictionary.TypeID("app/Customer")
	if !ok {
		t.Fatal("the dictionary has no id for app/Customer")
	}
	if _, ok := dictionary.PropertyID(typeID, "email"); !ok {
		t.Error("the dictionary has no id for app/Customer.email")
	}
	if _, ok := dictionary.Links["app/CustomerOrders"]; !ok {
		t.Error("the dictionary has no id for app/CustomerOrders")
	}

	versions, err := client.List(ctx, ontologyName)
	if err != nil {
		t.Fatalf("listing versions: %v", err)
	}
	if len(versions) != 1 || versions[0].Version != "1.0.0" {
		t.Errorf("list returned %+v, want one version 1.0.0", versions)
	}
}

// TestServerErrorsSurviveTheWire checks that a caller holding a client reacts to
// registry errors exactly as one holding the store would.
func TestServerErrorsSurviveTheWire(t *testing.T) {
	client, baseURL := framework.NewRegistryServer(t, framework.NewDatabase(t))
	ctx := context.Background()

	if _, err := client.Get(ctx, ontologyName, "9.9.9"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("getting an unknown version returned %v, want ErrNotFound", err)
	}
	if _, err := client.Resolve(ctx, ontologyName, "prod"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("resolving an unset tag returned %v, want ErrNotFound", err)
	}

	compiled := framework.SampleOntology(t)
	if _, err := client.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: compiled,
	}); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	// Republishing different content under a published version is refused, and
	// the reason has to reach the client as ErrAlreadyExists rather than as a
	// generic failure: a release pipeline branches on it.
	edited := framework.SampleOntology(t)
	edited.ObjectTypes[0].Properties[0].Description = "changed"
	if _, err := client.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: edited,
	}); !errors.Is(err, registry.ErrAlreadyExists) {
		t.Errorf("republishing different content returned %v, want ErrAlreadyExists", err)
	}

	// A draft cannot be tagged, and a tag that has never moved cannot roll back.
	if _, err := client.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0", Snapshot: edited, Draft: true,
	}); err != nil {
		t.Fatalf("publishing a draft: %v", err)
	}
	if _, err := client.Tag(ctx, ontologyName, "prod", "1.1.0"); !errors.Is(err, registry.ErrNotPublished) {
		t.Errorf("tagging a draft returned %v, want ErrNotPublished", err)
	}
	if _, err := client.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("tagging prod: %v", err)
	}
	if _, err := client.Rollback(ctx, ontologyName, "prod"); !errors.Is(err, registry.ErrNoPreviousVersion) {
		t.Errorf("rolling back a tag that never moved returned %v, want ErrNoPreviousVersion", err)
	}

	// Health endpoints are what compose and orchestrators wait on.
	response, err := http.Get(baseURL + "/healthz")
	if err != nil {
		t.Fatalf("getting /healthz: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		t.Errorf("/healthz returned %d, want 200", response.StatusCode)
	}

	// A malformed request is rejected before it reaches the store.
	bad, err := http.Post(baseURL+"/v1/ontologies/"+ontologyName+"/versions",
		"application/json", strings.NewReader(`{"version":"2.0.0"}`))
	if err != nil {
		t.Fatalf("posting a publish request without a snapshot: %v", err)
	}
	defer func() { _ = bad.Body.Close() }()
	if bad.StatusCode != http.StatusBadRequest {
		t.Errorf("publishing without a snapshot returned %d, want 400", bad.StatusCode)
	}
	var status struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(bad.Body).Decode(&status); err != nil {
		t.Fatalf("decoding the error body: %v", err)
	}
	if status.Reason != "Invalid" {
		t.Errorf("error reason = %q, want Invalid", status.Reason)
	}
}
