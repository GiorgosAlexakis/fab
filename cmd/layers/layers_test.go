package layers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"

	cmdtesting "github.com/GiorgosAlexakis/fab/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

// writeFoundry builds a foundry whose layer graph forces a build order that
// alphabetical ordering would get wrong: meta-storage sorts last but must be built
// before meta-core.
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
    - name: meta-storage
    - name: meta-core
`)

	writeFile(t, filepath.Join(root, "layers", "meta-elo", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-elo
  version: 0.1.0
  origin: upstream
`)
	writeFile(t, filepath.Join(root, "layers", "meta-storage", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-storage
  version: 1.1.0
  origin: upstream
spec:
  dependsOn:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
`)
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
spec:
  dependsOn:
    - name: meta-storage
      version: ">=1.0.0, <2.0.0"
`)
	return root
}

func run(t *testing.T, root, output string) (string, string) {
	t.Helper()

	streams, _, out, errOut := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewOptions(streams)
	o.Output = output

	if err := o.Complete(factory, NewCmdLayers(factory, streams), nil); err != nil {
		t.Fatalf("Complete() failed: %v", err)
	}
	if err := o.Validate(); err != nil {
		t.Fatalf("Validate() failed: %v", err)
	}
	if err := o.Run(); err != nil {
		t.Fatalf("Run() failed: %v", err)
	}
	return out.String(), errOut.String()
}

func TestRunListsLayersInBuildOrder(t *testing.T) {
	out, _ := run(t, writeFoundry(t), "")

	for _, want := range []string{"LAYER", "VERSION", "ORIGIN", "DEPENDS ON", "0.1.0", "upstream", "local"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}

	wantOrder := []string{"meta-elo", "meta-storage", "meta-core"}
	if got := layerOrder(out, wantOrder); !equal(got, wantOrder) {
		t.Errorf("layers listed as %v, want %v (dependency order, not alphabetical)", got, wantOrder)
	}
}

func TestRunJSON(t *testing.T) {
	out, _ := run(t, writeFoundry(t), "json")

	lock := &foundryv1.Lock{}
	if err := json.Unmarshal([]byte(out), lock); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(lock.Locked) != 3 {
		t.Fatalf("got %d layers, want 3", len(lock.Locked))
	}
	if lock.Locked[0].Name != "meta-elo" || lock.Locked[2].Name != "meta-core" {
		t.Errorf("JSON output is not in build order: %+v", lock.Locked)
	}
	if lock.Locked[0].Digest == "" {
		t.Error("JSON output should carry a digest per layer")
	}
}

// A layer sitting in layers/ that foundry.yaml does not activate is not part of
// the stack, and must not be listed as though it were.
func TestRunListsOnlyActiveLayers(t *testing.T) {
	root := writeFoundry(t)
	writeFile(t, filepath.Join(root, "layers", "meta-billing", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-billing
  version: 1.0.0
`)

	out, _ := run(t, root, "")
	if strings.Contains(out, "meta-billing") {
		t.Errorf("an inactive layer should not be listed:\n%s", out)
	}
}

func TestCompleteReportsABrokenGraph(t *testing.T) {
	root := writeFoundry(t)

	// Point meta-core at a version of meta-storage that is not active.
	writeFile(t, filepath.Join(root, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
spec:
  dependsOn:
    - name: meta-storage
      version: ">=2.0.0, <3.0.0"
`)

	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewOptions(streams)
	err := o.Complete(factory, NewCmdLayers(factory, streams), nil)
	if err == nil {
		t.Fatal("Complete() should fail when the layer graph does not resolve")
	}
	if !strings.Contains(err.Error(), "tested against") {
		t.Errorf("error should explain the compatibility window, got: %v", err)
	}
}

func TestCompleteWithoutAFoundryExplainsWhatToDo(t *testing.T) {
	streams, _, _, _ := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(t.TempDir()))

	o := NewOptions(streams)
	err := o.Complete(factory, NewCmdLayers(factory, streams), nil)
	if err == nil {
		t.Fatal("Complete() should fail without a foundry.yaml")
	}
	if !strings.Contains(err.Error(), "fab init") {
		t.Errorf("error should point at `fab init`, got: %v", err)
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

// layerOrder returns the wanted names in the order they appear in the output.
func layerOrder(output string, want []string) []string {
	var order []string
	for _, line := range strings.Split(output, "\n") {
		for _, name := range want {
			if strings.HasPrefix(line, name) {
				order = append(order, name)
			}
		}
	}
	return order
}

func equal(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
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
