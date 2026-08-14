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
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	registrycmd "github.com/GiorgosAlexakis/fab/pkg/cmd/registry"
	"github.com/GiorgosAlexakis/fab/pkg/cmd/schema"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	registrypostgres "github.com/GiorgosAlexakis/fab/pkg/registry/ontology/postgres"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

// newCLIFixture returns a factory wired to a real registry and a foundry on
// disk, so command tests exercise the whole path from YAML to PostgreSQL.
func newCLIFixture(t *testing.T) (*cmdtesting.TestFactory, string, context.Context) {
	t.Helper()

	pool := framework.NewDatabase(t)
	ctx := context.Background()

	root := framework.WriteFoundry(t, ontologyName)
	factory := cmdtesting.NewTestFactory(root).
		WithOntologyName(ontologyName).
		WithRegistryDB(pool)

	return factory, root, ctx
}

func TestRegistryMigrateCommand(t *testing.T) {
	factory, _, ctx := newCLIFixture(t)

	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	o := registrycmd.NewMigrateOptions(streams)
	if err := o.Complete(factory, registrycmd.NewCmdMigrate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Run(ctx); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "applied 0001_registry") {
		t.Errorf("migrate output = %q, want it to report the applied migration", out.String())
	}

	// A second run must be a no-op, because CI pipelines run migrations on
	// every deploy.
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	o = registrycmd.NewMigrateOptions(streams)
	if err := o.Complete(factory, registrycmd.NewCmdMigrate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Run(ctx); err != nil {
		t.Fatalf("second Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "up to date") {
		t.Errorf("second migrate output = %q, want it to report no work", out.String())
	}
}

func TestSchemaPublishAndPromoteCommands(t *testing.T) {
	factory, root, ctx := newCLIFixture(t)
	migrateRegistry(t, factory, ctx)

	// Wire the factory's registry client now that the schema exists.
	db, err := factory.RegistryDB(ctx)
	if err != nil {
		t.Fatalf("getting the registry database: %v", err)
	}
	factory.WithRegistry(registrypostgres.New(db))

	digest := publish(t, factory, ctx, "1.0.0", false)

	framework.AddPropertyToCustomer(t, root, "phone")
	secondDigest := publish(t, factory, ctx, "1.1.0", false)
	if digest == secondDigest {
		t.Error("publishing an edited schema produced the same digest")
	}

	// list
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	listOptions := schema.NewListOptions(streams)
	if err := listOptions.Complete(factory, schema.NewCmdList(factory, streams), nil); err != nil {
		t.Fatalf("list Complete() failed: %v", err)
	}
	if err := listOptions.Run(ctx); err != nil {
		t.Fatalf("list Run() failed: %v", err)
	}
	listing := out.String()
	for _, want := range []string{"VERSION", "1.0.0", "1.1.0", "published"} {
		if !strings.Contains(listing, want) {
			t.Errorf("list output is missing %q:\n%s", want, listing)
		}
	}

	// tag staging 1.0.0
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	tagOptions := schema.NewTagOptions(streams)
	if err := tagOptions.Complete(factory, schema.NewCmdTag(factory, streams), []string{"staging", "1.0.0"}); err != nil {
		t.Fatalf("tag Complete() failed: %v", err)
	}
	if err := tagOptions.Run(ctx); err != nil {
		t.Fatalf("tag Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "staging") {
		t.Errorf("tag output = %q, want it to name the tag", out.String())
	}

	// promote staging prod
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	promoteOptions := schema.NewPromoteOptions(streams)
	if err := promoteOptions.Complete(factory, schema.NewCmdPromote(factory, streams),
		[]string{"staging", "prod"}); err != nil {
		t.Fatalf("promote Complete() failed: %v", err)
	}
	if err := promoteOptions.Run(ctx); err != nil {
		t.Fatalf("promote Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "1.0.0") {
		t.Errorf("promote output = %q, want it to name the promoted version", out.String())
	}

	// get --tag prod -o digest must return what 1.0.0 was published with
	streams, _, out, _ = genericiooptions.NewTestIOStreams()
	getOptions := schema.NewGetOptions(streams)
	getOptions.Tag = "prod"
	getOptions.Output = "digest"
	if err := getOptions.Complete(factory, schema.NewCmdGet(factory, streams), nil); err != nil {
		t.Fatalf("get Complete() failed: %v", err)
	}
	if err := getOptions.Validate(); err != nil {
		t.Fatalf("get Validate() failed: %v", err)
	}
	if err := getOptions.Run(ctx); err != nil {
		t.Fatalf("get Run() failed: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != digest {
		t.Errorf("prod resolves to digest %s, want %s", got, digest)
	}
}

func TestSchemaGetRequiresASelector(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	o := schema.NewGetOptions(streams)
	if err := o.Validate(); err == nil {
		t.Fatal("get Validate() accepted neither --version nor --tag")
	}

	o.Version = "1.0.0"
	o.Tag = "prod"
	if err := o.Validate(); err == nil {
		t.Fatal("get Validate() accepted both --version and --tag")
	}
}

func TestSchemaPublishRequiresAVersion(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	o := schema.NewPublishOptions(streams)
	if err := o.Validate(); err == nil {
		t.Fatal("publish Validate() accepted a missing --version")
	}
}

// publish runs `fab schema publish --version <version>` and returns the digest
// it reported.
func publish(t *testing.T, factory cmdutil.Factory, ctx context.Context, version string, draft bool) string {
	t.Helper()

	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	o := schema.NewPublishOptions(streams)
	o.Version = version
	o.Draft = draft
	o.Output = "digest"

	if err := o.Complete(factory, schema.NewCmdPublish(factory, streams), nil); err != nil {
		t.Fatalf("publish Complete() failed: %v", err)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("publish Validate() failed: %v", err)
	}
	if err := o.Run(ctx); err != nil {
		t.Fatalf("publish Run() failed: %v", err)
	}

	digest := strings.TrimSpace(out.String())
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("publish reported %q, want a sha256 digest", digest)
	}
	return digest
}

func migrateRegistry(t *testing.T, factory cmdutil.Factory, ctx context.Context) {
	t.Helper()

	db, err := factory.RegistryDB(ctx)
	if err != nil {
		t.Fatalf("getting the registry database: %v", err)
	}
	if _, err := registrypostgres.Migrate(ctx, db); err != nil {
		t.Fatalf("migrating the registry: %v", err)
	}
}
