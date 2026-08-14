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

package objectstore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	objectstoreapi "github.com/GiorgosAlexakis/fab/pkg/objectstore/api"
	objectstorepostgres "github.com/GiorgosAlexakis/fab/pkg/objectstore/postgres"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

// serverFixture is an object store served over HTTP against a registry served
// over HTTP: the deployment docker compose brings up, without the containers.
type serverFixture struct {
	baseURL  string
	registry registry.Interface
	ctx      context.Context
}

func newServerFixture(t *testing.T) *serverFixture {
	t.Helper()

	pool := framework.NewDatabase(t)
	ctx := context.Background()

	client, _ := framework.NewRegistryServer(t, pool)
	if _, err := objectstorepostgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating the object store: %v", err)
	}
	if _, err := client.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("publishing the ontology: %v", err)
	}
	if _, err := client.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("tagging the ontology: %v", err)
	}

	return &serverFixture{
		baseURL:  framework.NewObjectStoreServer(t, pool, client, ontologyName, "prod"),
		registry: client,
		ctx:      ctx,
	}
}

func TestServerWritesReadsAndTraverses(t *testing.T) {
	f := newServerFixture(t)

	// The store reports which ontology version it is serving requests against.
	var info objectstoreapi.OntologyInfo
	f.get(t, "/v1/ontology", &info)
	if info.Ontology.Version != "1.0.0" {
		t.Errorf("bound version = %q, want 1.0.0", info.Ontology.Version)
	}
	if len(info.ObjectTypes) == 0 || len(info.LinkTypes) == 0 {
		t.Errorf("bound ontology reports %d object types and %d link types, want both non-empty",
			len(info.ObjectTypes), len(info.LinkTypes))
	}

	// Create a customer and two orders.
	var customer objectstore.Object
	f.put(t, "/v1/objects/app/Customer", objectstoreapi.PutRequest{
		Set: map[string]interface{}{
			"id": "CUST-1", "email": "a@corp.com", "tier": "pro", "tags": []string{"vip"},
		},
	}, &customer)
	if customer.PrimaryKey != "CUST-1" {
		t.Fatalf("created object primary key = %q, want CUST-1", customer.PrimaryKey)
	}
	if customer.Properties["tier"] != "pro" {
		t.Errorf("tier = %v, want pro", customer.Properties["tier"])
	}

	for _, id := range []string{"ORD-1", "ORD-2"} {
		var order objectstore.Object
		f.put(t, "/v1/objects/app/Order", objectstoreapi.PutRequest{
			Set: map[string]interface{}{"id": id, "total": "10.50"},
		}, &order)
		f.link(t, http.MethodPut, "/v1/links/app/CustomerOrders", objectstoreapi.LinkBody{
			Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
			Target: objectstore.Ref{Type: orderType, PrimaryKey: id},
		}, http.StatusNoContent)
	}

	// Read it back by primary key.
	var fetched objectstore.Object
	f.get(t, "/v1/objects/app/Customer/CUST-1", &fetched)
	if fetched.Properties["email"] != "a@corp.com" {
		t.Errorf("email = %v, want a@corp.com", fetched.Properties["email"])
	}

	// Filter by a property, with the total alongside the page.
	var list objectstoreapi.ObjectList
	f.get(t, "/v1/objects/app/Customer?tier=pro&total", &list)
	if len(list.Items) != 1 || list.Items[0].PrimaryKey != "CUST-1" {
		t.Errorf("filtered list returned %d items, want CUST-1", len(list.Items))
	}
	if list.Total == nil || *list.Total != 1 {
		t.Errorf("filtered total = %v, want 1", list.Total)
	}

	// A filter on a property the ontology does not define is a client error, not
	// a silent full scan.
	f.expectStatus(t, http.MethodGet, "/v1/objects/app/Customer?nickname=vip", nil, http.StatusBadRequest)

	// Traverse the link in both directions.
	var orders objectstoreapi.ObjectList
	f.get(t, "/v1/objects/app/Customer/CUST-1/links/customer_orders", &orders)
	if len(orders.Items) != 2 {
		t.Errorf("customer has %d orders over the API, want 2", len(orders.Items))
	}
	var owners objectstoreapi.ObjectList
	f.get(t, "/v1/objects/app/Order/ORD-1/links/customer", &owners)
	if len(owners.Items) != 1 || owners.Items[0].PrimaryKey != "CUST-1" {
		t.Errorf("order ORD-1 resolves to %d customers, want CUST-1", len(owners.Items))
	}

	// CustomerOrders restricts deletion while orders are linked.
	f.expectStatus(t, http.MethodDelete, "/v1/objects/app/Customer/CUST-1", nil, http.StatusConflict)

	f.link(t, http.MethodDelete, "/v1/links/app/CustomerOrders", objectstoreapi.LinkBody{
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}, http.StatusNoContent)
	f.link(t, http.MethodDelete, "/v1/links/app/CustomerOrders", objectstoreapi.LinkBody{
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-2"},
	}, http.StatusNoContent)

	f.expectStatus(t, http.MethodDelete, "/v1/objects/app/Customer/CUST-1", nil, http.StatusNoContent)
	f.expectStatus(t, http.MethodGet, "/v1/objects/app/Customer/CUST-1", nil, http.StatusNotFound)
}

// TestServerRebindsWhenATagMoves checks that promoting a version reaches a
// running store: this is the whole point of resolving the ontology by tag.
func TestServerRebindsWhenATagMoves(t *testing.T) {
	f := newServerFixture(t)

	// A property that does not exist in 1.0.0 is rejected.
	f.expectStatus(t, http.MethodPost, "/v1/objects/app/Customer", objectstoreapi.PutRequest{
		Set: map[string]interface{}{"id": "CUST-2", "phone": "555"},
	}, http.StatusBadRequest)

	if _, err := f.registry.Publish(f.ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0", Snapshot: withProperty(t, "phone"),
	}); err != nil {
		t.Fatalf("publishing 1.1.0: %v", err)
	}
	if _, err := f.registry.Tag(f.ctx, ontologyName, "prod", "1.1.0"); err != nil {
		t.Fatalf("moving prod to 1.1.0: %v", err)
	}

	// A request pinned to the old version still sees the old schema, while the
	// tag catches up once the cached binding expires.
	f.expectStatus(t, http.MethodPost, "/v1/objects/app/Customer?version=1.0.0", objectstoreapi.PutRequest{
		Set: map[string]interface{}{"id": "CUST-3", "phone": "555"},
	}, http.StatusBadRequest)

	var created objectstore.Object
	f.eventually(t, func() bool {
		code := f.send(t, http.MethodPost, "/v1/objects/app/Customer", objectstoreapi.PutRequest{
			Set: map[string]interface{}{"id": "CUST-2", "phone": "555"},
		}, &created)
		return code == http.StatusOK
	}, "the store never picked up ontology 1.1.0")

	if created.Properties["phone"] != "555" {
		t.Errorf("phone = %v after rebinding, want 555", created.Properties["phone"])
	}
}

// get reads a JSON response, failing the test on any error status.
func (f *serverFixture) get(t *testing.T, path string, into interface{}) {
	t.Helper()
	if code := f.send(t, http.MethodGet, path, nil, into); code != http.StatusOK {
		t.Fatalf("GET %s returned %d", path, code)
	}
}

// put writes an object, failing the test on any error status.
func (f *serverFixture) put(t *testing.T, path string, body interface{}, into interface{}) {
	t.Helper()
	if code := f.send(t, http.MethodPost, path, body, into); code != http.StatusOK {
		t.Fatalf("POST %s returned %d", path, code)
	}
}

// link sends a link or unlink request and asserts its status.
func (f *serverFixture) link(t *testing.T, method, path string, body interface{}, want int) {
	t.Helper()
	if code := f.send(t, method, path, body, nil); code != want {
		t.Fatalf("%s %s returned %d, want %d", method, path, code, want)
	}
}

// expectStatus asserts the status of a request whose body the test ignores.
func (f *serverFixture) expectStatus(t *testing.T, method, path string, body interface{}, want int) {
	t.Helper()
	if code := f.send(t, method, path, body, nil); code != want {
		t.Fatalf("%s %s returned %d, want %d", method, path, code, want)
	}
}

// send performs a request and decodes a successful response into out.
func (f *serverFixture) send(t *testing.T, method, path string, body, out interface{}) int {
	t.Helper()

	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encoding the request body: %v", err)
		}
		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(f.ctx, method, f.baseURL+path, payload)
	if err != nil {
		t.Fatalf("building %s %s: %v", method, path, err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = response.Body.Close() }()

	if out != nil && response.StatusCode < http.StatusBadRequest {
		if err := json.NewDecoder(response.Body).Decode(out); err != nil {
			t.Fatalf("decoding the response of %s %s: %v", method, path, err)
		}
	}
	return response.StatusCode
}

// eventually retries until the condition holds or the attempts run out, which is
// how a test waits for a cached ontology binding to expire.
func (f *serverFixture) eventually(t *testing.T, condition func() bool, message string) {
	t.Helper()

	for attempt := 0; attempt < 40; attempt++ {
		if condition() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s after 40 attempts", message)
}
