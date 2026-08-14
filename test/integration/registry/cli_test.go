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
	"github.com/GiorgosAlexakis/fab/pkg/cmd/schema"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/test/integration/framework"
)

// newCLIFixture returns a factory wired to a registry server and a foundry on
// disk, so command tests exercise the whole path a user takes: YAML, the
// registry API, PostgreSQL.
func newCLIFixture(t *testing.T) (*cmdtesting.TestFactory, registry.Interface, string, context.Context) {
	t.Helper()

	pool := framework.NewDatabase(t)
	client, _ := framework.NewRegistryServer(t, pool)
	ctx := context.Background()

	root := framework.WriteFoundry(t, ontologyName)
	factory := cmdtesting.NewTestFactory(root).
		WithOntologyName(ontologyName).
		WithRegistry(client)

	return factory, client, root, ctx
}

func TestSchemaPublishAndPromoteCommands(t *testing.T) {
	factory, client, root, ctx := newCLIFixture(t)

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

	// prod now resolves to what 1.0.0 was published with
	resolved, err := client.ResolveSnapshot(ctx, ontologyName, "prod")
	if err != nil {
		t.Fatalf("resolving prod: %v", err)
	}
	resolvedDigest, err := resolved.Digest()
	if err != nil {
		t.Fatalf("digesting the resolved snapshot: %v", err)
	}
	if resolvedDigest != digest {
		t.Errorf("prod resolves to digest %s, want %s", resolvedDigest, digest)
	}
}

func TestSchemaRollbackCommand(t *testing.T) {
	factory, client, root, ctx := newCLIFixture(t)

	publish(t, factory, ctx, "1.0.0", false)
	framework.AddPropertyToCustomer(t, root, "phone")
	publish(t, factory, ctx, "1.1.0", false)

	if _, err := client.Tag(ctx, ontologyName, "prod", "1.0.0"); err != nil {
		t.Fatalf("tagging prod: %v", err)
	}
	if _, err := client.Tag(ctx, ontologyName, "prod", "1.1.0"); err != nil {
		t.Fatalf("moving prod: %v", err)
	}

	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	rollbackOptions := schema.NewRollbackOptions(streams)
	if err := rollbackOptions.Complete(factory, schema.NewCmdRollback(factory, streams),
		[]string{"prod"}); err != nil {
		t.Fatalf("rollback Complete() failed: %v", err)
	}
	if err := rollbackOptions.Run(ctx); err != nil {
		t.Fatalf("rollback Run() failed: %v", err)
	}
	if !strings.Contains(out.String(), "1.0.0") {
		t.Errorf("rollback output = %q, want it to name the restored version", out.String())
	}

	resolved, err := client.Resolve(ctx, ontologyName, "prod")
	if err != nil {
		t.Fatalf("resolving prod: %v", err)
	}
	if resolved.Version != "1.0.0" {
		t.Errorf("prod points at %s after rollback, want 1.0.0", resolved.Version)
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
