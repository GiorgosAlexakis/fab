package layer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmdtesting "github.com/GiorgosAlexakis/fab/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// writeFoundry builds a foundry that declares only meta-elo but has meta-core
// sitting in layers/, ready to be activated.
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
  version: 1.2.3
  origin: upstream
`)
	return root
}

func add(t *testing.T, root, name, version string) (string, error) {
	t.Helper()

	streams, _, _, errOut := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewAddOptions(streams)
	o.Version = version

	cmd := NewCmdLayerAdd(factory, streams)
	if err := o.Complete(factory, cmd, []string{name}); err != nil {
		return errOut.String(), err
	}
	if err := o.Validate(); err != nil {
		return errOut.String(), err
	}

	err := o.Run()
	return errOut.String(), err
}

func TestAddDerivesTheVersionRange(t *testing.T) {
	root := writeFoundry(t)

	errOut, err := add(t, root, "meta-core", "")
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	saved, err := foundryv1.NewEngine(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	spec, declared := saved.Selects("meta-core")
	if !declared {
		t.Fatalf("meta-core was not declared: %v", saved.LayerNames())
	}
	// The layer is at 1.2.3, so the range runs from there to the next major.
	if spec != ">=1.2.3, <2.0.0" {
		t.Errorf("range = %q, want >=1.2.3, <2.0.0", spec)
	}
	if !strings.Contains(errOut, "meta-core") {
		t.Errorf("expected a confirmation on stderr, got: %q", errOut)
	}
}

func TestAddWithAnExplicitRange(t *testing.T) {
	root := writeFoundry(t)

	if _, err := add(t, root, "meta-core", ">=1.0.0, <2.0.0"); err != nil {
		t.Fatalf("add failed: %v", err)
	}

	saved, err := foundryv1.NewEngine(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if spec, _ := saved.Selects("meta-core"); spec != ">=1.0.0, <2.0.0" {
		t.Errorf("range = %q, want the range that was passed", spec)
	}
}

func TestAddRejectsARangeWithoutAnUpperBound(t *testing.T) {
	_, err := add(t, writeFoundry(t), "meta-core", ">=1.0.0")
	if err == nil {
		t.Fatal("add should reject a range with no upper bound")
	}
	if !strings.Contains(err.Error(), "upper bound") && !strings.Contains(err.Error(), "bound") {
		t.Errorf("error should explain the missing upper bound, got: %v", err)
	}
}

func TestAddRejectsALayerThatIsNotThere(t *testing.T) {
	_, err := add(t, writeFoundry(t), "meta-billing", "")
	if err == nil {
		t.Fatal("add should reject a layer it cannot find or pin")
	}
	// The available layers are listed, because the usual cause is a typo.
	for _, want := range []string{"meta-billing", "meta-core", "meta-elo", "fab sync"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q, got: %v", want, err)
		}
	}
}

// A layer the bundle provides but this foundry has never fetched is activated by
// naming the range, because there is nothing on disk to derive one from.
func TestAddALayerThatIsNotFetchedYet(t *testing.T) {
	root := writeFoundry(t)

	errOut, err := add(t, root, "meta-billing", ">=1.0.0, <2.0.0")
	if err != nil {
		t.Fatalf("add failed: %v", err)
	}

	saved, err := foundryv1.NewEngine(root).Load()
	if err != nil {
		t.Fatal(err)
	}
	if spec, declared := saved.Selects("meta-billing"); !declared || spec != ">=1.0.0, <2.0.0" {
		t.Errorf("meta-billing = %q, %v; want the range that was passed", spec, declared)
	}
	if !strings.Contains(errOut, "fab sync") {
		t.Errorf("stderr should say to fetch it, got: %q", errOut)
	}
}

func TestAddRejectsALayerThatIsAlreadyActive(t *testing.T) {
	_, err := add(t, writeFoundry(t), "meta-elo", "")
	if err == nil {
		t.Fatal("add should reject a layer that is already declared")
	}
	if !strings.Contains(err.Error(), "already declared") {
		t.Errorf("error should say the layer is already declared, got: %v", err)
	}
}

func TestAddWithoutAFoundryExplainsWhatToDo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.2.3
`)

	_, err := add(t, root, "meta-core", "")
	if err == nil {
		t.Fatal("add should fail without a foundry.yaml")
	}
	if !strings.Contains(err.Error(), "fab init") {
		t.Errorf("error should point at `fab init`, got: %v", err)
	}
}
