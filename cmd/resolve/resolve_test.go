package resolve

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"

	cmdtesting "github.com/GiorgosAlexakis/fab/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

// writeFoundry builds a foundry with the foundation layer and one layer that
// depends on it.
func writeFoundry(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foundry.yaml"), `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
`)
	writeFile(t, filepath.Join(root, "layers", "meta-elo", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-elo
  version: 0.1.0
  origin: upstream
`)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
  origin: upstream
spec:
  dependsOn:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
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

	lock, err := cmdutil.LoadLock(root)
	if err != nil {
		t.Fatalf("LoadLock() failed: %v", err)
	}
	if len(lock.Locked) != 2 {
		t.Fatalf("lock = %+v, want two entries", lock.Locked)
	}
	// The lock is in build order, and pins exact versions rather than the ranges
	// foundry.yaml asked for.
	if lock.Locked[0].Name != "meta-elo" || lock.Locked[1].Name != "meta-core" {
		t.Errorf("lock is not in build order: %+v", lock.Locked)
	}
	if lock.Locked[1].Version != "1.0.1" {
		t.Errorf("locked version = %q, want the exact version 1.0.1", lock.Locked[1].Version)
	}
	if lock.Locked[1].DependsOn[0] != "meta-elo" {
		t.Errorf("the lock should record dependencies, got %+v", lock.Locked[1])
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

	pinned, err := cmdutil.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	pinned.Bundle = &foundryv1.Bundle{
		URL:    "https://github.com/fab-oss/foundry.git",
		Ref:    "v1.2.0",
		GitRef: "a3f8c21d9e4b6f0123456789abcdef0123456789",
		Layers: []string{"meta-core", "meta-elo"},
	}
	if err := cmdutil.SaveLock(root, pinned); err != nil {
		t.Fatal(err)
	}

	again, _, _ := newOptions(t, root)
	if err := again.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	reloaded, err := cmdutil.LoadLock(root)
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

func TestCheckLeavesTheLockAlone(t *testing.T) {
	root := writeFoundry(t)
	path := filepath.Join(root, cmdutil.LockFileName)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Unix(0, 0), time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}

	checked, _, _ := newOptions(t, root)
	checked.Check = true
	if err := checked.Run(); err != nil {
		t.Fatal(err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(time.Unix(0, 0)) {
		t.Error("--check rewrote foundry.lock")
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(before) {
		t.Error("--check changed the contents of foundry.lock")
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
  dependsOn:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
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

// A layer that is not activated is not in the lock, however much of it is on disk.
func TestRunLocksOnlyActiveLayers(t *testing.T) {
	root := writeFoundry(t)
	writeFile(t, filepath.Join(root, "layers", "meta-billing", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-billing
  version: 1.0.0
`)

	o, _, _ := newOptions(t, root)
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}

	lock, err := cmdutil.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range lock.Locked {
		if entry.Name == "meta-billing" {
			t.Errorf("an inactive layer was locked: %+v", lock.Locked)
		}
	}
}

func TestCompleteReportsAnUnsatisfiedDependency(t *testing.T) {
	root := writeFoundry(t)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
  origin: upstream
spec:
  dependsOn:
    - name: meta-billing
      version: ">=1.0.0, <2.0.0"
`)

	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewOptions(streams)
	err := o.Complete(factory, NewCmdResolve(factory, streams), nil)
	if err == nil {
		t.Fatal("Complete() should fail when a dependency is not active")
	}
	if !strings.Contains(err.Error(), "meta-billing") {
		t.Errorf("error should name the missing layer, got: %v", err)
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
