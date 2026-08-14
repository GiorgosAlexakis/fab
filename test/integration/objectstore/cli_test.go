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
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	"github.com/GiorgosAlexakis/fab/pkg/cmd/object"
	objectstorecmd "github.com/GiorgosAlexakis/fab/pkg/cmd/objectstore"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

// newCLIFactory returns a factory wired to the fixture's registry and object
// store, so command tests exercise the whole path from arguments to PostgreSQL.
func newCLIFactory(t *testing.T, f *fixture) *cmdtesting.TestFactory {
	t.Helper()

	return cmdtesting.NewTestFactory(t.TempDir()).
		WithOntologyName(ontologyName).
		WithOntologyTag("prod").
		WithRegistry(f.registry).
		WithRegistryDB(f.pool)
}

func TestObjectStoreMigrateCommand(t *testing.T) {
	pool := framework.NewDatabase(t)
	factory := cmdtesting.NewTestFactory(t.TempDir()).WithRegistryDB(pool)
	ctx := context.Background()

	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	o := objectstorecmd.NewMigrateOptions(streams)
	if err := o.Complete(factory, objectstorecmd.NewCmdMigrate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Run(ctx); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "applied 0001_object_store") {
		t.Errorf("migrate output = %q, want it to report the applied migration", out.String())
	}

	// Deploys run migrations every time, so a second run must do nothing.
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	o = objectstorecmd.NewMigrateOptions(streams)
	if err := o.Complete(factory, objectstorecmd.NewCmdMigrate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Run(ctx); err != nil {
		t.Fatalf("second Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("second migrate output = %q, want it to report no work", out.String())
	}
}

func TestObjectCommandsRoundTrip(t *testing.T) {
	f := newFixture(t)
	factory := newCLIFactory(t, f)

	// create
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	create := object.NewPutOptions(streams)
	create.CreateOnly = true
	create.Set = []string{"id=CUST-1", "email=ada@example.com", "tier=pro", "tags=vip,eu"}
	if err := create.Complete(factory, object.NewCmdCreate(factory, streams), []string{customerType}); err != nil {
		t.Fatalf("create Complete() failed: %v", err)
	}
	if err := create.Validate(); err != nil {
		t.Fatalf("create Validate() failed: %v", err)
	}
	if err := create.Run(f.ctx); err != nil {
		t.Fatalf("create Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "app/Customer/CUST-1") {
		t.Errorf("create output = %q, want it to name the object", out.String())
	}

	// get -o json: the values come back typed, having been parsed out of text
	// against the ontology.
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	get := object.NewGetOptions(streams)
	get.Output = "json"
	if err := get.Complete(factory, object.NewCmdGet(factory, streams),
		[]string{customerType, "CUST-1"}); err != nil {
		t.Fatalf("get Complete() failed: %v", err)
	}
	if err := get.Validate(); err != nil {
		t.Fatalf("get Validate() failed: %v", err)
	}
	if err := get.Run(f.ctx); err != nil {
		t.Fatalf("get Run() failed: %v", err)
	}

	var fetched objectstore.Object
	if err := json.Unmarshal(out.Bytes(), &fetched); err != nil {
		t.Fatalf("get -o json produced %q: %v", out.String(), err)
	}
	if fetched.Properties["tier"] != "pro" {
		t.Errorf("tier = %v, want pro", fetched.Properties["tier"])
	}
	tags, ok := fetched.Properties["tags"].([]interface{})
	if !ok || len(tags) != 2 || tags[0] != "vip" {
		t.Errorf("tags = %#v, want [vip eu]", fetched.Properties["tags"])
	}

	// apply updates one property and leaves the rest alone.
	streams, _, _, _ = genericiooptions.NewTestIOStreams()
	apply := object.NewPutOptions(streams)
	apply.Set = []string{"id=CUST-1", "tier=enterprise"}
	if err := apply.Complete(factory, object.NewCmdApply(factory, streams), []string{customerType}); err != nil {
		t.Fatalf("apply Complete() failed: %v", err)
	}
	if err := apply.Run(f.ctx); err != nil {
		t.Fatalf("apply Run() failed: %v", err)
	}

	// list --filter
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	list := object.NewListOptions(streams)
	list.Filter = []string{"tier=enterprise"}
	if err := list.Complete(factory, object.NewCmdList(factory, streams), []string{customerType}); err != nil {
		t.Fatalf("list Complete() failed: %v", err)
	}
	if err := list.Validate(); err != nil {
		t.Fatalf("list Validate() failed: %v", err)
	}
	if err := list.Run(f.ctx); err != nil {
		t.Fatalf("list Run() failed: %v", err)
	}
	listing := out.String()
	for _, want := range []string{"PRIMARY KEY", "TIER", "CUST-1", "enterprise"} {
		if !strings.Contains(listing, want) {
			t.Errorf("list output is missing %q:\n%s", want, listing)
		}
	}

	// list --count
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	count := object.NewListOptions(streams)
	count.Count = true
	if err := count.Complete(factory, object.NewCmdList(factory, streams), []string{customerType}); err != nil {
		t.Fatalf("count Complete() failed: %v", err)
	}
	if err := count.Run(f.ctx); err != nil {
		t.Fatalf("count Run() failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "1" {
		t.Errorf("list --count = %q, want 1", got)
	}
}

func TestObjectLinkAndTraverseCommands(t *testing.T) {
	f := newFixture(t)
	factory := newCLIFactory(t, f)

	f.createCustomer(t, "CUST-1", nil)
	f.createOrder(t, "ORD-1")

	// link names the link type, so the primary keys are enough: the ontology
	// already says which types the two ends are.
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	link := object.NewLinkOptions(streams)
	if err := link.Complete(factory, object.NewCmdLink(factory, streams),
		[]string{customerOrders, "CUST-1", "ORD-1"}); err != nil {
		t.Fatalf("link Complete() failed: %v", err)
	}
	if err := link.Run(f.ctx); err != nil {
		t.Fatalf("link Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "linked") {
		t.Errorf("link output = %q, want it to report the link", out.String())
	}

	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	traverse := object.NewTraverseOptions(streams)
	if err := traverse.Complete(factory, object.NewCmdTraverse(factory, streams),
		[]string{customerType, "CUST-1", "customer_orders"}); err != nil {
		t.Fatalf("traverse Complete() failed: %v", err)
	}
	if err := traverse.Validate(); err != nil {
		t.Fatalf("traverse Validate() failed: %v", err)
	}
	if err := traverse.Run(f.ctx); err != nil {
		t.Fatalf("traverse Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "ORD-1") {
		t.Errorf("traverse output = %q, want it to list the order", out.String())
	}

	// unlink, then delete: the link type restricts deletes while it holds.
	streams, _, _, _ = genericiooptions.NewTestIOStreams()
	unlink := object.NewLinkOptions(streams)
	unlink.Unlink = true
	if err := unlink.Complete(factory, object.NewCmdUnlink(factory, streams),
		[]string{customerOrders, "CUST-1", "ORD-1"}); err != nil {
		t.Fatalf("unlink Complete() failed: %v", err)
	}
	if err := unlink.Run(f.ctx); err != nil {
		t.Fatalf("unlink Run() failed: %v", err)
	}

	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	remove := object.NewDeleteOptions(streams)
	if err := remove.Complete(factory, object.NewCmdDelete(factory, streams),
		[]string{customerType, "CUST-1"}); err != nil {
		t.Fatalf("delete Complete() failed: %v", err)
	}
	if err := remove.Run(f.ctx); err != nil {
		t.Fatalf("delete Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "deleted") {
		t.Errorf("delete output = %q, want it to confirm the delete", out.String())
	}
}

// TestObjectCommandsReportUnknownNames checks the failure a user is most likely
// to hit: a type or property that the bound ontology does not declare.
func TestObjectCommandsReportUnknownNames(t *testing.T) {
	f := newFixture(t)
	factory := newCLIFactory(t, f)

	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	create := object.NewPutOptions(streams)
	create.Set = []string{"id=INV-1"}
	if err := create.Complete(factory, object.NewCmdCreate(factory, streams),
		[]string{"app/Invoice"}); err != nil {
		t.Fatalf("create Complete() failed: %v", err)
	}
	err := create.Run(f.ctx)
	if err == nil {
		t.Fatal("create accepted a type the ontology does not declare")
	}
	if !strings.Contains(err.Error(), customerType) {
		t.Errorf("error = %q, want it to list the known types", err)
	}

	streams, _, _, _ = genericiooptions.NewTestIOStreams()
	typo := object.NewPutOptions(streams)
	typo.Set = []string{"id=CUST-1", "nickname=Ada"}
	if err := typo.Complete(factory, object.NewCmdCreate(factory, streams),
		[]string{customerType}); err != nil {
		t.Fatalf("create Complete() failed: %v", err)
	}
	if err := typo.Run(f.ctx); err == nil {
		t.Fatal("create accepted a property the ontology does not declare")
	}

	// A malformed assignment is a usage error, not a value error.
	streams, _, _, _ = genericiooptions.NewTestIOStreams()
	malformed := object.NewPutOptions(streams)
	malformed.Set = []string{"id"}
	if err := malformed.Complete(factory, object.NewCmdCreate(factory, streams),
		[]string{customerType}); err != nil {
		t.Fatalf("create Complete() failed: %v", err)
	}
	if err := malformed.Run(f.ctx); err == nil {
		t.Fatal("create accepted a --set without a value")
	}
}
