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

// Package registry_test exercises the ontology registry against a live
// PostgreSQL.
package registry_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	"github.com/GiorgosAlexakis/fab/pkg/storage/migrate"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

const ontologyName = "acme-corp"

// newStore returns a migrated registry on a schema of its own.
func newStore(t *testing.T) (*registrypostgres.Store, context.Context) {
	t.Helper()

	pool := framework.NewDatabase(t)
	ctx := context.Background()

	if _, err := registrypostgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrating the registry: %v", err)
	}
	return registrypostgres.New(pool), ctx
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool := framework.NewDatabase(t)
	ctx := context.Background()

	applied, err := registrypostgres.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("first migration failed: %v", err)
	}
	if len(applied) == 0 {
		t.Fatal("first migration applied nothing")
	}

	again, err := registrypostgres.Migrate(ctx, pool)
	if err != nil {
		t.Fatalf("second migration failed: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("second migration applied %d migrations, want none", len(again))
	}

	migrations, err := registrypostgres.Migrations()
	if err != nil {
		t.Fatalf("loading migrations: %v", err)
	}
	pending, err := migrate.Pending(ctx, pool, registrypostgres.Component, migrations)
	if err != nil {
		t.Fatalf("listing pending migrations: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("%d migrations still pending after migrating", len(pending))
	}
}

func TestPublishRoundTrip(t *testing.T) {
	store, ctx := newStore(t)
	compiled := framework.SampleOntology(t)

	published, err := store.Publish(ctx, registry.PublishRequest{
		Name:     ontologyName,
		Version:  "1.0.0",
		Snapshot: compiled,
		GitRef:   "179a9f6",
	})
	if err != nil {
		t.Fatalf("Publish() failed: %v", err)
	}

	wantDigest, err := compiled.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	if published.Digest != wantDigest {
		t.Errorf("published digest = %s, want %s", published.Digest, wantDigest)
	}
	if published.Status != registry.StatusPublished {
		t.Errorf("status = %s, want published", published.Status)
	}
	if published.PublishedAt == nil {
		t.Error("publishedAt is nil for a published version")
	}
	if published.GitRef != "179a9f6" {
		t.Errorf("gitRef = %q, want 179a9f6", published.GitRef)
	}
	if !reflect.DeepEqual(published.Layers, compiled.Layers) {
		t.Errorf("layers = %v, want %v", published.Layers, compiled.Layers)
	}

	// The registry stores normalized rows, not a copy of the document. The
	// round trip is what proves the rows carry the whole ontology.
	stored, err := store.GetSnapshot(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("GetSnapshot() failed: %v", err)
	}
	if !reflect.DeepEqual(stored, compiled) {
		t.Errorf("stored snapshot differs from the published one:\n got: %+v\nwant: %+v", stored, compiled)
	}
}

func TestPublishIsIdempotentForIdenticalContent(t *testing.T) {
	store, ctx := newStore(t)
	compiled := framework.SampleOntology(t)

	first, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: compiled,
	})
	if err != nil {
		t.Fatalf("first Publish() failed: %v", err)
	}

	second, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: compiled,
	})
	if err != nil {
		t.Fatalf("re-publishing identical content failed: %v", err)
	}
	if !first.CreatedAt.Equal(second.CreatedAt) {
		t.Error("re-publishing identical content rewrote the version")
	}

	versions, err := store.List(ctx, ontologyName)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("got %d versions, want 1", len(versions))
	}
}

func TestPublishRefusesToChangeAPublishedVersion(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("Publish() failed: %v", err)
	}

	changed := withExtraProperty(t)
	_, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: changed,
	})
	if !errors.Is(err, registry.ErrAlreadyExists) {
		t.Fatalf("Publish() error = %v, want ErrAlreadyExists", err)
	}
}

func TestDraftsAreReplaceableAndUntaggable(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0-rc1", Snapshot: framework.SampleOntology(t), Draft: true,
	}); err != nil {
		t.Fatalf("publishing a draft failed: %v", err)
	}

	if _, err := store.Tag(ctx, ontologyName, "staging", "1.1.0-rc1"); !errors.Is(err, registry.ErrNotPublished) {
		t.Fatalf("tagging a draft: error = %v, want ErrNotPublished", err)
	}

	changed := withExtraProperty(t)
	replaced, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0-rc1", Snapshot: changed, Draft: true,
	})
	if err != nil {
		t.Fatalf("replacing a draft failed: %v", err)
	}

	wantDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	if replaced.Digest != wantDigest {
		t.Errorf("digest after replacing the draft = %s, want %s", replaced.Digest, wantDigest)
	}
	if replaced.PublishedAt != nil {
		t.Error("a draft has a publishedAt timestamp")
	}

	stored, err := store.GetSnapshot(ctx, ontologyName, "1.1.0-rc1")
	if err != nil {
		t.Fatalf("GetSnapshot() failed: %v", err)
	}
	if !reflect.DeepEqual(stored, changed) {
		t.Error("replacing a draft left rows from the previous content")
	}
}

func TestTagPromoteAndRollback(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("publishing 1.0.0 failed: %v", err)
	}
	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0", Snapshot: withExtraProperty(t),
	}); err != nil {
		t.Fatalf("publishing 1.1.0 failed: %v", err)
	}

	if _, err := store.Tag(ctx, ontologyName, "staging", "1.0.0"); err != nil {
		t.Fatalf("Tag() failed: %v", err)
	}
	resolved, err := store.Resolve(ctx, ontologyName, "staging")
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if resolved.Version != "1.0.0" {
		t.Errorf("staging resolves to %s, want 1.0.0", resolved.Version)
	}
	if len(resolved.Tags) != 1 || resolved.Tags[0] != "staging" {
		t.Errorf("tags = %v, want [staging]", resolved.Tags)
	}

	if _, err := store.Promote(ctx, ontologyName, "staging", "prod"); err != nil {
		t.Fatalf("Promote() failed: %v", err)
	}
	if _, err := store.Rollback(ctx, ontologyName, "prod"); !errors.Is(err, registry.ErrNoPreviousVersion) {
		t.Fatalf("rolling back a tag that never moved: error = %v, want ErrNoPreviousVersion", err)
	}

	// Move staging forward, promote it, then roll prod back.
	if _, err := store.Tag(ctx, ontologyName, "staging", "1.1.0"); err != nil {
		t.Fatalf("re-tagging staging failed: %v", err)
	}
	promoted, err := store.Promote(ctx, ontologyName, "staging", "prod")
	if err != nil {
		t.Fatalf("Promote() failed: %v", err)
	}
	if promoted.Version != "1.1.0" {
		t.Errorf("prod now resolves to %s, want 1.1.0", promoted.Version)
	}

	rolledBack, err := store.Rollback(ctx, ontologyName, "prod")
	if err != nil {
		t.Fatalf("Rollback() failed: %v", err)
	}
	if rolledBack.Version != "1.0.0" {
		t.Errorf("prod after rollback resolves to %s, want 1.0.0", rolledBack.Version)
	}

	// A tagged snapshot must be fetchable by tag, not just by version.
	byTag, err := store.ResolveSnapshot(ctx, ontologyName, "prod")
	if err != nil {
		t.Fatalf("ResolveSnapshot() failed: %v", err)
	}
	byVersion, err := store.GetSnapshot(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("GetSnapshot() failed: %v", err)
	}
	if !reflect.DeepEqual(byTag, byVersion) {
		t.Error("resolving prod and fetching 1.0.0 returned different ontologies")
	}
}

func TestDeprecate(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("Publish() failed: %v", err)
	}
	if _, err := store.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("Tag() failed: %v", err)
	}

	deprecated, err := store.Deprecate(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("Deprecate() failed: %v", err)
	}
	if deprecated.Status != registry.StatusDeprecated {
		t.Errorf("status = %s, want deprecated", deprecated.Status)
	}

	// A deprecated version must stay readable: services pinned to it keep
	// running.
	if _, err := store.GetSnapshot(ctx, ontologyName, "1.0.0"); err != nil {
		t.Errorf("a deprecated version is no longer readable: %v", err)
	}
	if _, err := store.Resolve(ctx, ontologyName, "prod"); err != nil {
		t.Errorf("a tag pointing at a deprecated version no longer resolves: %v", err)
	}
}

func TestNotFound(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Get(ctx, ontologyName, "9.9.9"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
	if _, err := store.Resolve(ctx, ontologyName, "prod"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("Resolve() error = %v, want ErrNotFound", err)
	}
	if _, err := store.Tag(ctx, ontologyName, "prod", "9.9.9"); !errors.Is(err, registry.ErrNotFound) {
		t.Errorf("Tag() error = %v, want ErrNotFound", err)
	}
	versions, err := store.List(ctx, "no-such-ontology")
	if err != nil {
		t.Errorf("List() of an unknown ontology failed: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("List() of an unknown ontology returned %d versions", len(versions))
	}
}

// TestPropertyIdentityIsStableAcrossVersions is the guarantee the object store
// depends on: a property keeps its prop_id for as long as its name is unchanged,
// across every ontology version, so no object row has to be rewritten when the
// schema evolves.
func TestPropertyIdentityIsStableAcrossVersions(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("publishing 1.0.0 failed: %v", err)
	}
	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.1.0", Snapshot: withExtraProperty(t),
	}); err != nil {
		t.Fatalf("publishing 1.1.0 failed: %v", err)
	}

	first, err := store.Dictionary(ctx, ontologyName, "1.0.0")
	if err != nil {
		t.Fatalf("Dictionary(1.0.0) failed: %v", err)
	}
	second, err := store.Dictionary(ctx, ontologyName, "1.1.0")
	if err != nil {
		t.Fatalf("Dictionary(1.1.0) failed: %v", err)
	}

	customerID, ok := first.TypeID("app/Customer")
	if !ok {
		t.Fatal("app/Customer is missing from the dictionary")
	}
	if secondID, ok := second.TypeID("app/Customer"); !ok || secondID != customerID {
		t.Errorf("app/Customer type id changed between versions: %d then %d", customerID, secondID)
	}

	for _, property := range []string{"id", "email", "tier"} {
		firstPropertyID, ok := first.PropertyID(customerID, property)
		if !ok {
			t.Fatalf("property %q is missing from the 1.0.0 dictionary", property)
		}
		secondPropertyID, ok := second.PropertyID(customerID, property)
		if !ok {
			t.Fatalf("property %q is missing from the 1.1.0 dictionary", property)
		}
		if firstPropertyID != secondPropertyID {
			t.Errorf("property %q changed id between versions: %d then %d",
				property, firstPropertyID, secondPropertyID)
		}
	}

	// The property added in 1.1.0 exists only there, and its id does not
	// collide with anything.
	if _, ok := first.PropertyID(customerID, "phone"); ok {
		t.Error("1.0.0 knows about a property that was added in 1.1.0")
	}
	phoneID, ok := second.PropertyID(customerID, "phone")
	if !ok {
		t.Fatal("the property added in 1.1.0 is missing from its dictionary")
	}
	for property, propertyID := range second.Properties[customerID] {
		if property != "phone" && propertyID == phoneID {
			t.Errorf("property %q shares an id with phone", property)
		}
	}

	if got := second.PrimaryKeys[customerID]; got != second.Properties[customerID]["id"] {
		t.Errorf("primary key of app/Customer = %d, want the id property %d",
			got, second.Properties[customerID]["id"])
	}
	if got := second.PropertyTypes[phoneID]; got != "string" {
		t.Errorf("data type of phone = %q, want string", got)
	}
}

func TestResolveDictionary(t *testing.T) {
	store, ctx := newStore(t)

	if _, err := store.Publish(ctx, registry.PublishRequest{
		Name: ontologyName, Version: "1.0.0", Snapshot: framework.SampleOntology(t),
	}); err != nil {
		t.Fatalf("Publish() failed: %v", err)
	}
	if _, err := store.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("Tag() failed: %v", err)
	}

	dictionary, err := store.ResolveDictionary(ctx, ontologyName, "prod")
	if err != nil {
		t.Fatalf("ResolveDictionary() failed: %v", err)
	}
	if dictionary.Ontology.Version != "1.0.0" {
		t.Errorf("dictionary resolved from version %s, want 1.0.0", dictionary.Ontology.Version)
	}
	if _, ok := dictionary.TypeID("meta-core/User"); !ok {
		t.Error("meta-core/User is missing from the resolved dictionary")
	}
}

func TestListOrdersNewestFirst(t *testing.T) {
	store, ctx := newStore(t)

	for _, version := range []string{"1.0.0", "1.1.0", "1.2.0"} {
		compiled := framework.SampleOntology(t)
		if version != "1.0.0" {
			compiled = withExtraPropertyNamed(t, "field_"+version)
		}
		if _, err := store.Publish(ctx, registry.PublishRequest{
			Name: ontologyName, Version: version, Snapshot: compiled,
		}); err != nil {
			t.Fatalf("publishing %s failed: %v", version, err)
		}
	}

	versions, err := store.List(ctx, ontologyName)
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("got %d versions, want 3", len(versions))
	}
	if versions[0].Version != "1.2.0" {
		t.Errorf("first listed version = %s, want the newest, 1.2.0", versions[0].Version)
	}
}

// withExtraProperty returns the sample ontology with one more property on
// app/Customer, which is the smallest schema change that alters the digest.
func withExtraProperty(t *testing.T) *snapshot.Snapshot {
	t.Helper()
	return withExtraPropertyNamed(t, "phone")
}

func withExtraPropertyNamed(t *testing.T, property string) *snapshot.Snapshot {
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

// compile-time assertion that the framework helpers keep returning what these
// tests expect.
var _ = []compiler.LayerSource(nil)
