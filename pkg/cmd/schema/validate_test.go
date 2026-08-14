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

package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
)

// newFakeFactory points commands at a foundry in testdata.
func newFakeFactory(root string) cmdutil.Factory {
	return cmdtesting.NewTestFactory(root)
}

func TestValidateRunSummary(t *testing.T) {
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	factory := newFakeFactory("testdata/foundry")

	o := NewValidateOptions(streams)
	cmd := NewCmdValidate(factory, streams)

	if err := o.Complete(factory, cmd, nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	output := out.String()
	for _, want := range []string{"LAYER", "meta-core", "app", "4 object types", "3 link types", "digest: sha256:"} {
		if !strings.Contains(output, want) {
			t.Errorf("summary output is missing %q:\n%s", want, output)
		}
	}
}

func TestValidateRunJSON(t *testing.T) {
	streams, _, out, _ := genericiooptions.NewTestIOStreams()
	factory := newFakeFactory("testdata/foundry")

	o := NewValidateOptions(streams)
	o.Output = "json"
	if err := o.Complete(factory, NewCmdValidate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	var compiled snapshot.Snapshot
	if err := json.Unmarshal(out.Bytes(), &compiled); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out.String())
	}

	if want := []string{"meta-core", "app"}; len(compiled.Layers) != 2 ||
		compiled.Layers[0] != want[0] || compiled.Layers[1] != want[1] {
		t.Errorf("Layers = %v, want %v", compiled.Layers, want)
	}

	customer, ok := compiled.ObjectType("app", "Customer")
	if !ok {
		t.Fatal("compiled ontology is missing app/Customer")
	}
	tier, ok := customer.Property("tier")
	if !ok {
		t.Fatal("app/Customer is missing the tier property")
	}
	if len(tier.Values) != 3 {
		t.Errorf("tier values = %v, want three enum values", tier.Values)
	}

	account, ok := compiled.LinkType("app", "CustomerAccount")
	if !ok {
		t.Fatal("compiled ontology is missing app/CustomerAccount")
	}
	if account.Target.Layer != "meta-core" || account.Target.Type != "User" {
		t.Errorf("CustomerAccount target = %s, want meta-core/User", account.Target.QualifiedName())
	}
}

func TestValidateRunDigestIsStable(t *testing.T) {
	factory := newFakeFactory("testdata/foundry")

	digests := make([]string, 2)
	for i := range digests {
		streams, _, out, _ := genericiooptions.NewTestIOStreams()
		o := NewValidateOptions(streams)
		o.Output = "digest"
		if err := o.Complete(factory, NewCmdValidate(factory, streams), nil); err != nil {
			t.Fatalf("Complete() failed: %v", err)
		}
		if err := o.Run(); err != nil {
			t.Fatalf("Run() failed: %v", err)
		}
		digests[i] = strings.TrimSpace(out.String())
	}

	if !strings.HasPrefix(digests[0], "sha256:") {
		t.Errorf("digest = %q, want a sha256: prefix", digests[0])
	}
	if digests[0] != digests[1] {
		t.Errorf("digest is not stable across runs: %q != %q", digests[0], digests[1])
	}
}

func TestValidateRunReportsUnresolvedReference(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	factory := newFakeFactory("testdata/invalid-foundry")

	o := NewValidateOptions(streams)
	if err := o.Complete(factory, NewCmdValidate(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}

	err := o.Run()
	if err == nil {
		t.Fatal("Run() succeeded on a foundry with a dangling link, want an error")
	}
	if !strings.Contains(err.Error(), "meta-auth/Session") {
		t.Errorf("error = %v, want it to name the unresolved type", err)
	}
	if !strings.Contains(err.Error(), "layer \"meta-auth\" is not active") {
		t.Errorf("error = %v, want it to explain that the layer is not active", err)
	}
}

func TestValidateRejectsUnknownOutputFormat(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	o := NewValidateOptions(streams)
	o.Output = "table-ish"
	if err := o.Validate(); err == nil {
		t.Fatal("Validate() accepted an unknown output format")
	}
}

func TestValidateRejectsPositionalArguments(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	factory := newFakeFactory("testdata/foundry")

	o := NewValidateOptions(streams)
	if err := o.Complete(factory, NewCmdValidate(factory, streams), []string{"customer"}); err == nil {
		t.Fatal("Complete() accepted a positional argument")
	}
}
