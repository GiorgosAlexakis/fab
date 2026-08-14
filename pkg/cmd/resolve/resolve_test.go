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

package resolve

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/layers"
)

const userYAML = `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: User
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
`

// writeFoundry builds a foundry with one upstream layer and a company schema that
// links across the layer boundary.
func writeFoundry(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foundry.yaml"), `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
`)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
  origin: upstream
spec:
  provides:
    schema:
      objects: [User]
`)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "schema", "objects", "user.yaml"), userYAML)

	writeFile(t, filepath.Join(root, "schema", "objects", "order.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Order
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
`)
	writeFile(t, filepath.Join(root, "schema", "links", "user_orders.yaml"), `apiVersion: fab/v1
kind: LinkType
metadata:
  name: UserOrders
spec:
  source:
    layer: meta-core
    type: User
  target:
    type: Order
  cardinality: one_to_many
`)
	return root
}

func newOptions(t *testing.T, root string) (*Options, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()

	streams, _, out, errOut := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewOptions(streams)
	if err := o.Complete(factory, NewCmdResolve(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	return o, out, errOut
}

func TestRunWritesTheLock(t *testing.T) {
	root := writeFoundry(t)

	o, out, errOut := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	if !strings.Contains(out.String(), "meta-core") {
		t.Errorf("output should list the resolved layers:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "Wrote foundry.lock") {
		t.Errorf("stderr should confirm the write, got: %q", errOut.String())
	}

	lock, err := layers.LoadLock(root)
	if err != nil {
		t.Fatalf("LoadLock() failed: %v", err)
	}
	if len(lock.Locked) != 1 || lock.Locked[0].Name != "meta-core" {
		t.Errorf("lock = %+v, want one meta-core entry", lock.Locked)
	}
	if lock.Locked[0].Version != "1.0.1" {
		t.Errorf("locked version = %q, want the exact version 1.0.1", lock.Locked[0].Version)
	}
}

// Resolution has nothing to say about where the upstream layers were fetched
// from, so rewriting the lock must leave that pin alone rather than drop it.
func TestRunKeepsTheBundlePin(t *testing.T) {
	root := writeFoundry(t)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}

	pinned, err := layers.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	pinned.Bundle = &layers.Bundle{
		URL:    "https://github.com/fab-oss/foundry.git",
		Ref:    "v1.2.0",
		GitRef: "a3f8c21d9e4b6f0123456789abcdef0123456789",
		Layers: []string{"meta-core"},
	}
	if err := layers.SaveLock(root, pinned); err != nil {
		t.Fatal(err)
	}

	again, _, _ := newOptions(t, root)
	if err := again.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	reloaded, err := layers.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Bundle == nil {
		t.Fatal("re-resolving dropped the bundle pin")
	}
	if reloaded.Bundle.GitRef != pinned.Bundle.GitRef {
		t.Errorf("bundle gitRef = %q, want %q", reloaded.Bundle.GitRef, pinned.Bundle.GitRef)
	}
}

// --check is the CI mode: it must fail on a foundry that was never resolved.
func TestCheckRequiresALock(t *testing.T) {
	o, _, _ := newOptions(t, writeFoundry(t))
	o.Check = true

	err := o.Run()
	if err == nil {
		t.Fatal("--check should fail when foundry.lock does not exist")
	}
	if !strings.Contains(err.Error(), "fab resolve") {
		t.Errorf("error should say how to fix it, got: %v", err)
	}
}

func TestCheckAcceptsACurrentLock(t *testing.T) {
	root := writeFoundry(t)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	checked, _, errOut := newOptions(t, root)
	checked.Check = true
	if err := checked.Run(); err != nil {
		t.Fatalf("--check failed on the lock it had just written: %v", err)
	}
	if !strings.Contains(errOut.String(), "up to date") {
		t.Errorf("stderr should confirm the lock is current, got: %q", errOut.String())
	}
}

func TestCheckRejectsAStaleLock(t *testing.T) {
	root := writeFoundry(t)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}

	// A version bump on a layer, as an upstream release would deliver it.
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.1.0
  origin: upstream
spec:
  provides:
    schema:
      objects: [User]
`)

	checked, _, _ := newOptions(t, root)
	checked.Check = true

	err := checked.Run()
	if err == nil {
		t.Fatal("--check should fail on a stale lock")
	}
	if !strings.Contains(err.Error(), "1.0.1 -> 1.1.0") {
		t.Errorf("error should show what changed, got: %v", err)
	}
}

// Resolution is where a broken cross-layer reference is caught, because it is the
// last point at which the active layer set is known.
func TestRunRejectsABrokenCrossLayerReference(t *testing.T) {
	root := writeFoundry(t)

	writeFile(t, filepath.Join(root, "schema", "links", "user_orders.yaml"), `apiVersion: fab/v1
kind: LinkType
metadata:
  name: UserOrders
spec:
  source:
    layer: meta-absent
    type: User
  target:
    type: Order
  cardinality: one_to_many
`)

	o, _, _ := newOptions(t, root)
	err := o.Run()
	if err == nil {
		t.Fatal("Run() should reject a reference into a layer that is not active")
	}
	if !strings.Contains(err.Error(), "meta-absent") {
		t.Errorf("error should name the missing layer, got: %v", err)
	}
}

// A manifest that disagrees with the tree it ships is a bug in the layer.
func TestRunRejectsAManifestThatOverDeclares(t *testing.T) {
	root := writeFoundry(t)

	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
  origin: upstream
spec:
  provides:
    schema:
      objects: [User, Organization]
`)

	o, _, _ := newOptions(t, root)
	err := o.Run()
	if err == nil {
		t.Fatal("Run() should reject a manifest that declares a type it does not ship")
	}
	if !strings.Contains(err.Error(), "Organization") {
		t.Errorf("error should name the undelivered type, got: %v", err)
	}
}

// A foundry can be resolved before any schema exists: the layer graph is
// meaningful on its own.
func TestRunWithoutSchema(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foundry.yaml"), `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-core
`)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
`)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed on a foundry with no schema: %v", err)
	}
	if _, err := layers.LoadLock(root); err != nil {
		t.Errorf("LoadLock() failed: %v", err)
	}
}

func TestValidateRejectsAnUnknownFormat(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	o := NewOptions(streams)
	o.Output = "toml"
	if err := o.Validate(); err == nil {
		t.Fatal("Validate() should reject an unsupported output format")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
