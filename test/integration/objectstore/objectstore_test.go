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

// Package objectstore_test exercises the object store against a live
// PostgreSQL, through a real ontology published in a real registry.
package objectstore_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	objectstorepostgres "github.com/GiorgosAlexakis/fab/pkg/objectstore/postgres"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

const ontologyName = "acme-corp"

// Types and links of the sample ontology, named once so a typo fails in one
// place rather than in every test.
const (
	customerType = "app/Customer"
	orderType    = "app/Order"
	userType     = "meta-core/User"

	customerOrders  = "app/CustomerOrders"
	customerAccount = "app/CustomerAccount"
)

// fixture is a migrated registry and object store sharing one database, with the
// sample ontology published as 1.0.0 and tagged prod.
type fixture struct {
	pool     *pgxpool.Pool
	registry *registrypostgres.Store
	store    *objectstorepostgres.Store
	ctx      context.Context
}

func newFixture(t *testing.T) *fixture {
	t.Helper()

	pool := framework.NewDatabase(t)
	ctx := context.Background()

	if _, err := registrypostgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating the registry: %v", err)
	}
	if _, err := objectstorepostgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating the object store: %v", err)
	}

	reg := registrypostgres.New(pool)
	if _, err := reg.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("publishing the ontology: %v", err)
	}
	if _, err := reg.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("tagging the ontology: %v", err)
	}

	binding, err := objectstore.BindTag(ctx, reg, ontologyName, "prod")
	if err != nil {
		t.Fatalf("binding the object store to prod: %v", err)
	}

	return &fixture{pool: pool, registry: reg, store: objectstorepostgres.New(pool, binding), ctx: ctx}
}

// bind returns a second store bound to another version of the ontology, which is
// how a service picks up a schema change.
func (f *fixture) bind(t *testing.T, version string) *objectstorepostgres.Store {
	t.Helper()

	binding, err := objectstore.BindVersion(f.ctx, f.registry, ontologyName, version)
	if err != nil {
		t.Fatalf("binding to %s: %v", version, err)
	}
	return objectstorepostgres.New(f.pool, binding)
}

// publish stores a modified snapshot as a new version.
func (f *fixture) publish(t *testing.T, version string, compiled *snapshot.Snapshot) {
	t.Helper()

	if _, err := f.registry.Publish(f.ctx, registry.PublishRequest{
		Name: ontologyName, Version: version, Snapshot: compiled,
	}); err != nil {
		t.Fatalf("publishing %s: %v", version, err)
	}
}

func (f *fixture) createCustomer(t *testing.T, primaryKey string, values map[string]interface{}) *objectstore.Object {
	t.Helper()

	set := map[string]interface{}{"id": primaryKey}
	for name, value := range values {
		set[name] = value
	}

	created, err := f.store.Put(f.ctx, objectstore.PutRequest{Type: customerType, Set: set})
	if err != nil {
		t.Fatalf("creating %s/%s: %v", customerType, primaryKey, err)
	}
	return created
}

func (f *fixture) createOrder(t *testing.T, primaryKey string) *objectstore.Object {
	t.Helper()

	created, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: orderType,
		Set:  map[string]interface{}{"id": primaryKey},
	})
	if err != nil {
		t.Fatalf("creating %s/%s: %v", orderType, primaryKey, err)
	}
	return created
}

func (f *fixture) createUser(t *testing.T, primaryKey, email string) *objectstore.Object {
	t.Helper()

	created, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: userType,
		Set:  map[string]interface{}{"id": primaryKey, "email": email},
	})
	if err != nil {
		t.Fatalf("creating %s/%s: %v", userType, primaryKey, err)
	}
	return created
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := framework.NewDatabase(t)
	ctx := context.Background()

	applied, err := objectstorepostgres.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("first migration applied nothing")
	}

	again, err := objectstorepostgres.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second migration applied %d migrations, want none", len(again))
	}
}

// TestPutAndGetRoundTrip covers one value of every property type the ontology
// offers: the store has no column types to lean on, so encoding is the only
// thing keeping the data typed.
func TestPutAndGetRoundTrip(t *testing.T) {
	f := newFixture(t)

	placedAt := time.Date(2024, 3, 1, 10, 30, 0, 0, time.UTC)
	created, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set: map[string]interface{}{
			"id":             "CUST-1",
			"email":          "ada@example.com",
			"tier":           "pro",
			"lifetime_value": "1099.50",
			"tags":           []string{"vip", "eu"},
			"preferences":    map[string]interface{}{"theme": "dark", "digest": true},
		},
	})
	if err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if created.ObjectID == 0 {
		t.Error("the created object has no id")
	}

	fetched, err := f.store.Get(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"})
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}

	want := map[string]interface{}{
		"id":    "CUST-1",
		"email": "ada@example.com",
		"tier":  "pro",
		// A decimal survives as text: it exists because binary floating point
		// is the wrong representation for money.
		"lifetime_value": "1099.50",
		"tags":           []interface{}{"vip", "eu"},
		"preferences":    map[string]interface{}{"theme": "dark", "digest": true},
	}
	if !reflect.DeepEqual(fetched.Properties, want) {
		t.Errorf("properties = %#v, want %#v", fetched.Properties, want)
	}

	byID, err := f.store.GetByID(f.ctx, created.ObjectID)
	if err != nil {
		t.Fatalf("GetByID() failed: %v", err)
	}
	if byID.PrimaryKey != "CUST-1" || byID.Type != customerType {
		t.Errorf("GetByID() returned %s, want %s", byID.Ref(), fetched.Ref())
	}

	// A timestamp comes back as a time.Time, which no round trip through JSON
	// text may shift.
	order, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: orderType,
		Set:  map[string]interface{}{"id": "ORD-1", "placed_at": placedAt, "total": "99.99"},
	})
	if err != nil {
		t.Fatalf("Put() of an order failed: %v", err)
	}
	stored, ok := order.Properties["placed_at"].(time.Time)
	if !ok {
		t.Fatalf("placed_at came back as %T, want time.Time", order.Properties["placed_at"])
	}
	if !stored.Equal(placedAt) {
		t.Errorf("placed_at = %s, want %s", stored, placedAt)
	}
}

// TestPutMergesAndRemoves is the reason writes are not whole-object replacements:
// two writers touching different properties must not overwrite each other.
func TestPutMergesAndRemoves(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", map[string]interface{}{
		"email": "ada@example.com",
		"tier":  "free",
	})

	updated, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-1", "tier": "enterprise"},
	})
	if err != nil {
		t.Fatalf("Put() failed: %v", err)
	}
	if updated.Properties["tier"] != "enterprise" {
		t.Errorf("tier = %v, want enterprise", updated.Properties["tier"])
	}
	if updated.Properties["email"] != "ada@example.com" {
		t.Errorf("email = %v after updating another property, want it untouched",
			updated.Properties["email"])
	}

	cleared, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type:   customerType,
		Set:    map[string]interface{}{"id": "CUST-1"},
		Remove: []string{"tier"},
	})
	if err != nil {
		t.Fatalf("Put() with a removal failed: %v", err)
	}
	if _, present := cleared.Properties["tier"]; present {
		t.Errorf("tier is still present after being removed: %v", cleared.Properties["tier"])
	}

	// The primary key is identity, not a value to be cleared.
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type:   customerType,
		Set:    map[string]interface{}{"id": "CUST-1"},
		Remove: []string{"id"},
	}); !errors.Is(err, objectstore.ErrRequiredProperty) {
		t.Errorf("removing the primary key: error = %v, want ErrRequiredProperty", err)
	}
}

func TestCreateOnlyRefusesAnExistingObject(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", nil)

	_, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type:       customerType,
		Set:        map[string]interface{}{"id": "CUST-1"},
		CreateOnly: true,
	})
	if !errors.Is(err, objectstore.ErrAlreadyExists) {
		t.Errorf("Put(CreateOnly) error = %v, want ErrAlreadyExists", err)
	}
}

func TestCreateRequiresThePrimaryKey(t *testing.T) {
	f := newFixture(t)

	_, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"tier": "pro"},
	})
	if !errors.Is(err, objectstore.ErrRequiredProperty) {
		t.Errorf("Put() without a primary key: error = %v, want ErrRequiredProperty", err)
	}
}

// TestUniquePropertyIsEnforced is the part of the ontology that a generic
// property table cannot express with a unique index, so the store has to carry
// it.
func TestUniquePropertyIsEnforced(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", map[string]interface{}{"email": "ada@example.com"})

	_, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-2", "email": "ada@example.com"},
	})
	if !errors.Is(err, objectstore.ErrConflict) {
		t.Fatalf("reusing a unique value: error = %v, want ErrConflict", err)
	}

	// An object may keep writing its own value, and may change it.
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-1", "email": "ada@example.com"},
	}); err != nil {
		t.Errorf("rewriting an object's own unique value failed: %v", err)
	}
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-1", "email": "ada@lovelace.example"},
	}); err != nil {
		t.Errorf("changing a unique value failed: %v", err)
	}

	// The old value is free again, and so is a deleted object's.
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-2", "email": "ada@example.com"},
	}); err != nil {
		t.Errorf("reusing a value that was changed away from failed: %v", err)
	}
	if err := f.store.Delete(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-2"}); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-3", "email": "ada@example.com"},
	}); err != nil {
		t.Errorf("reusing a deleted object's unique value failed: %v", err)
	}
}

func TestWritesAreValidatedAgainstTheOntology(t *testing.T) {
	f := newFixture(t)

	tests := []struct {
		name    string
		request objectstore.PutRequest
		want    error
	}{{
		name:    "an unknown object type",
		request: objectstore.PutRequest{Type: "app/Invoice", Set: map[string]interface{}{"id": "INV-1"}},
		want:    objectstore.ErrUnknownType,
	}, {
		name: "an unknown property",
		request: objectstore.PutRequest{Type: customerType,
			Set: map[string]interface{}{"id": "CUST-1", "nickname": "Ada"}},
		want: objectstore.ErrUnknownProperty,
	}, {
		name: "a value outside an enum",
		request: objectstore.PutRequest{Type: customerType,
			Set: map[string]interface{}{"id": "CUST-1", "tier": "platinum"}},
		want: objectstore.ErrInvalidValue,
	}, {
		name: "a value of the wrong type",
		request: objectstore.PutRequest{Type: customerType,
			Set: map[string]interface{}{"id": "CUST-1", "email": 42}},
		want: objectstore.ErrInvalidValue,
	}, {
		name: "a float where a decimal is declared",
		request: objectstore.PutRequest{Type: customerType,
			Set: map[string]interface{}{"id": "CUST-1", "lifetime_value": 10.5}},
		want: objectstore.ErrInvalidValue,
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := f.store.Put(f.ctx, test.request); !errors.Is(err, test.want) {
				t.Errorf("Put() error = %v, want %v", err, test.want)
			}
		})
	}

	// A rejected write must leave nothing behind.
	if _, err := f.store.Get(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"}); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("a rejected write created an object: %v", err)
	}
}

func TestGetAndDeleteOfAMissingObject(t *testing.T) {
	f := newFixture(t)

	ref := objectstore.Ref{Type: customerType, PrimaryKey: "CUST-404"}
	if _, err := f.store.Get(f.ctx, ref); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
	if err := f.store.Delete(f.ctx, ref); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("Delete() error = %v, want ErrNotFound", err)
	}
	if _, err := f.store.GetByID(f.ctx, 999999); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("GetByID() error = %v, want ErrNotFound", err)
	}
}

func TestDeleteRemovesPropertiesAndLinks(t *testing.T) {
	f := newFixture(t)
	created := f.createCustomer(t, "CUST-1", map[string]interface{}{"email": "ada@example.com"})

	if err := f.store.Delete(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"}); err != nil {
		t.Fatalf("Delete() failed: %v", err)
	}

	var properties int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM object_props WHERE object_id = $1`, created.ObjectID).Scan(&properties); err != nil {
		t.Fatalf("counting the property rows: %v", err)
	}
	if properties != 0 {
		t.Errorf("%d property rows survived the delete", properties)
	}

	var claims int
	if err := f.pool.QueryRow(f.ctx,
		`SELECT count(*) FROM object_prop_unique WHERE object_id = $1`, created.ObjectID).Scan(&claims); err != nil {
		t.Fatalf("counting the unique value claims: %v", err)
	}
	if claims != 0 {
		t.Errorf("%d unique value claims survived the delete", claims)
	}
}

func TestListAndCount(t *testing.T) {
	f := newFixture(t)

	f.createCustomer(t, "CUST-1", map[string]interface{}{"tier": "pro", "email": "a@example.com"})
	f.createCustomer(t, "CUST-2", map[string]interface{}{"tier": "enterprise", "email": "b@example.com"})
	f.createCustomer(t, "CUST-3", map[string]interface{}{"tier": "enterprise", "email": "c@example.com"})

	all, err := f.store.List(f.ctx, objectstore.Query{Type: customerType})
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d customers, want 3", len(all))
	}
	if all[0].PrimaryKey != "CUST-1" || all[2].PrimaryKey != "CUST-3" {
		t.Errorf("List() is not ordered by insertion: %s then %s", all[0].PrimaryKey, all[2].PrimaryKey)
	}
	if all[0].Properties["tier"] != "pro" {
		t.Errorf("listed objects are not hydrated: %#v", all[0].Properties)
	}

	filtered, err := f.store.List(f.ctx, objectstore.Query{
		Type:    customerType,
		Filters: []objectstore.Filter{{Property: "tier", Value: "enterprise"}},
	})
	if err != nil {
		t.Fatalf("filtered List() failed: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("got %d enterprise customers, want 2", len(filtered))
	}

	count, err := f.store.Count(f.ctx, objectstore.Query{
		Type:    customerType,
		Filters: []objectstore.Filter{{Property: "tier", Value: "enterprise"}},
	})
	if err != nil {
		t.Fatalf("Count() failed: %v", err)
	}
	if count != 2 {
		t.Errorf("Count() = %d, want 2", count)
	}

	// Two filters are ANDed, not ORed.
	both, err := f.store.Count(f.ctx, objectstore.Query{
		Type: customerType,
		Filters: []objectstore.Filter{
			{Property: "tier", Value: "enterprise"},
			{Property: "email", Value: "b@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("Count() with two filters failed: %v", err)
	}
	if both != 1 {
		t.Errorf("Count() with two filters = %d, want 1", both)
	}

	page, err := f.store.List(f.ctx, objectstore.Query{Type: customerType, Limit: 2, Offset: 1})
	if err != nil {
		t.Fatalf("paged List() failed: %v", err)
	}
	if len(page) != 2 || page[0].PrimaryKey != "CUST-2" {
		t.Errorf("page = %v, want CUST-2 and CUST-3", primaryKeys(page))
	}

	if _, err := f.store.List(f.ctx, objectstore.Query{
		Type:    customerType,
		Filters: []objectstore.Filter{{Property: "nickname", Value: "Ada"}},
	}); !errors.Is(err, objectstore.ErrUnknownProperty) {
		t.Errorf("filtering on an unknown property: error = %v, want ErrUnknownProperty", err)
	}
	if _, err := f.store.List(f.ctx, objectstore.Query{
		Type:  customerType,
		Limit: objectstore.MaxQueryLimit + 1,
	}); err == nil {
		t.Error("List() accepted a limit beyond the maximum")
	}
}

func TestLinkAndTraverse(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", nil)
	f.createOrder(t, "ORD-1")
	f.createOrder(t, "ORD-2")

	link := objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}
	if err := f.store.Link(f.ctx, link); err != nil {
		t.Fatalf("Link() failed: %v", err)
	}
	// Replaying a link must not fail: a caller retrying a message should not
	// have to check first.
	if err := f.store.Link(f.ctx, link); err != nil {
		t.Fatalf("re-linking the same pair failed: %v", err)
	}

	second := link
	second.Target = objectstore.Ref{Type: orderType, PrimaryKey: "ORD-2"}
	if err := f.store.Link(f.ctx, second); err != nil {
		t.Fatalf("linking a second order failed: %v", err)
	}

	orders, err := f.store.Traverse(f.ctx, objectstore.TraverseRequest{
		From:      objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Traversal: "customer_orders",
	})
	if err != nil {
		t.Fatalf("forward Traverse() failed: %v", err)
	}
	if got := primaryKeys(orders); !reflect.DeepEqual(got, []string{"ORD-1", "ORD-2"}) {
		t.Errorf("customer_orders = %v, want [ORD-1 ORD-2]", got)
	}

	// The same rows serve the reverse direction: a link is queryable from both
	// ends without a second table.
	customers, err := f.store.Traverse(f.ctx, objectstore.TraverseRequest{
		From:      objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
		Traversal: "customer",
	})
	if err != nil {
		t.Fatalf("reverse Traverse() failed: %v", err)
	}
	if got := primaryKeys(customers); !reflect.DeepEqual(got, []string{"CUST-1"}) {
		t.Errorf("customer of ORD-1 = %v, want [CUST-1]", got)
	}

	if err := f.store.Unlink(f.ctx, link); err != nil {
		t.Fatalf("Unlink() failed: %v", err)
	}
	if err := f.store.Unlink(f.ctx, link); err != nil {
		t.Fatalf("unlinking an unlinked pair failed: %v", err)
	}
	remaining, err := f.store.Traverse(f.ctx, objectstore.TraverseRequest{
		From:      objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Traversal: "customer_orders",
	})
	if err != nil {
		t.Fatalf("Traverse() after unlinking failed: %v", err)
	}
	if got := primaryKeys(remaining); !reflect.DeepEqual(got, []string{"ORD-2"}) {
		t.Errorf("customer_orders after unlinking ORD-1 = %v, want [ORD-2]", got)
	}
}

func TestLinkRejectsWhatTheOntologyForbids(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", nil)
	f.createCustomer(t, "CUST-2", nil)
	f.createOrder(t, "ORD-1")
	f.createUser(t, "USER-1", "ada@example.com")

	// one_to_many: an order belongs to one customer, so a second customer
	// cannot claim it.
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); err != nil {
		t.Fatalf("Link() failed: %v", err)
	}
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-2"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); !errors.Is(err, objectstore.ErrCardinality) {
		t.Errorf("linking a second source over a one_to_many link: error = %v, want ErrCardinality", err)
	}

	// one_to_one: a customer has one account.
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerAccount,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: userType, PrimaryKey: "USER-1"},
	}); err != nil {
		t.Fatalf("Link() of an account failed: %v", err)
	}
	f.createUser(t, "USER-2", "grace@example.com")
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerAccount,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: userType, PrimaryKey: "USER-2"},
	}); !errors.Is(err, objectstore.ErrCardinality) {
		t.Errorf("linking a second target over a one_to_one link: error = %v, want ErrCardinality", err)
	}

	// A link is typed: an object on the wrong end is a caller bug, not a
	// missing row.
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); !errors.Is(err, objectstore.ErrTypeMismatch) {
		t.Errorf("linking the wrong type: error = %v, want ErrTypeMismatch", err)
	}

	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-404"},
	}); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("linking a missing object: error = %v, want ErrNotFound", err)
	}

	if _, err := f.store.Traverse(f.ctx, objectstore.TraverseRequest{
		From:      objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Traversal: "invoices",
	}); !errors.Is(err, objectstore.ErrUnknownLink) {
		t.Errorf("traversing an undeclared name: error = %v, want ErrUnknownLink", err)
	}
}

// TestDeletePoliciesAreApplied covers the three policies a link type can declare
// from its source's side.
func TestDeletePoliciesAreApplied(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", nil)
	f.createOrder(t, "ORD-1")
	f.createUser(t, "USER-1", "ada@example.com")

	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); err != nil {
		t.Fatalf("Link() failed: %v", err)
	}

	// CustomerOrders defaults to restrict: deleting the customer must fail
	// while the order hangs off it.
	customer := objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"}
	if err := f.store.Delete(f.ctx, customer); !errors.Is(err, objectstore.ErrLinked) {
		t.Fatalf("deleting a restricted source: error = %v, want ErrLinked", err)
	}
	if _, err := f.store.Get(f.ctx, customer); err != nil {
		t.Errorf("a refused delete removed the object anyway: %v", err)
	}

	if err := f.store.Unlink(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: customer,
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); err != nil {
		t.Fatalf("Unlink() failed: %v", err)
	}

	// CustomerAccount declares detach: the user survives, the link does not.
	if err := f.store.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerAccount,
		Source: customer,
		Target: objectstore.Ref{Type: userType, PrimaryKey: "USER-1"},
	}); err != nil {
		t.Fatalf("Link() of an account failed: %v", err)
	}
	if err := f.store.Delete(f.ctx, customer); err != nil {
		t.Fatalf("Delete() with a detach policy failed: %v", err)
	}
	if _, err := f.store.Get(f.ctx, objectstore.Ref{Type: userType, PrimaryKey: "USER-1"}); err != nil {
		t.Errorf("a detach policy deleted the target: %v", err)
	}

	// cascade takes the far end with it.
	f.publish(t, "1.1.0", withCascadingOrders(t))
	cascading := f.bind(t, "1.1.0")

	if _, err := cascading.Put(f.ctx, objectstore.PutRequest{
		Type: customerType, Set: map[string]interface{}{"id": "CUST-2"},
	}); err != nil {
		t.Fatalf("creating a customer failed: %v", err)
	}
	if err := cascading.Link(f.ctx, objectstore.LinkRequest{
		Link:   customerOrders,
		Source: objectstore.Ref{Type: customerType, PrimaryKey: "CUST-2"},
		Target: objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"},
	}); err != nil {
		t.Fatalf("Link() failed: %v", err)
	}
	if err := cascading.Delete(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-2"}); err != nil {
		t.Fatalf("Delete() with a cascade policy failed: %v", err)
	}
	if _, err := cascading.Get(f.ctx, objectstore.Ref{Type: orderType, PrimaryKey: "ORD-1"}); !errors.Is(err, objectstore.ErrNotFound) {
		t.Errorf("a cascade policy left the target behind: %v", err)
	}
}

// TestRebindingToANewVersionKeepsData is the payoff of storing catalog ids rather
// than names: publishing an ontology version is a metadata change, and no object
// row moves.
func TestRebindingToANewVersionKeepsData(t *testing.T) {
	f := newFixture(t)
	f.createCustomer(t, "CUST-1", map[string]interface{}{
		"email": "ada@example.com",
		"tier":  "pro",
	})

	f.publish(t, "1.1.0", withProperty(t, "phone"))
	upgraded := f.bind(t, "1.1.0")

	// The object written under 1.0.0 reads back unchanged under 1.1.0, and the
	// property the new version added is simply unset.
	fetched, err := upgraded.Get(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"})
	if err != nil {
		t.Fatalf("Get() through the new binding failed: %v", err)
	}
	if fetched.Properties["email"] != "ada@example.com" || fetched.Properties["tier"] != "pro" {
		t.Errorf("properties = %#v, want the values written under 1.0.0", fetched.Properties)
	}
	if _, present := fetched.Properties["phone"]; present {
		t.Error("a property added by a new version has a value on an old object")
	}

	if _, err := upgraded.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-1", "phone": "+30 210 0000000"},
	}); err != nil {
		t.Fatalf("writing the new property failed: %v", err)
	}

	// The old binding has no name for the new property, so it cannot see it --
	// but everything it does know is intact.
	throughOldBinding, err := f.store.Get(f.ctx, objectstore.Ref{Type: customerType, PrimaryKey: "CUST-1"})
	if err != nil {
		t.Fatalf("Get() through the old binding failed: %v", err)
	}
	if _, present := throughOldBinding.Properties["phone"]; present {
		t.Error("the old binding sees a property its ontology version does not declare")
	}
	if throughOldBinding.Properties["email"] != "ada@example.com" {
		t.Errorf("email through the old binding = %v, want it untouched",
			throughOldBinding.Properties["email"])
	}
	if _, err := f.store.Put(f.ctx, objectstore.PutRequest{
		Type: customerType,
		Set:  map[string]interface{}{"id": "CUST-1", "phone": "+30 210 0000000"},
	}); !errors.Is(err, objectstore.ErrUnknownProperty) {
		t.Errorf("writing through the old binding: error = %v, want ErrUnknownProperty", err)
	}
}

// withProperty returns the sample ontology with one more property on
// app/Customer.
func withProperty(t *testing.T, property string) *snapshot.Snapshot {
	t.Helper()

	compiled := framework.SampleOntology(t)
	customer, ok := compiled.ObjectType("app", "Customer")
	if !ok {
		t.Fatal("the sample ontology has no app/Customer")
	}
	customer.Properties = append(customer.Properties, snapshot.Property{
		Name:     property,
		Type:     string(ontologyv1.PropertyTypeString),
		Nullable: true,
	})
	compiled.Normalize()
	return compiled
}

// withCascadingOrders returns the sample ontology with CustomerOrders switched
// from restrict to cascade.
func withCascadingOrders(t *testing.T) *snapshot.Snapshot {
	t.Helper()

	compiled := framework.SampleOntology(t)
	link, ok := compiled.LinkType("app", "CustomerOrders")
	if !ok {
		t.Fatal("the sample ontology has no app/CustomerOrders")
	}
	link.OnSourceDelete = string(ontologyv1.DeletePolicyCascade)
	return compiled
}

func primaryKeys(objects []objectstore.Object) []string {
	keys := make([]string, 0, len(objects))
	for i := range objects {
		keys = append(keys, objects[i].PrimaryKey)
	}
	return keys
}
