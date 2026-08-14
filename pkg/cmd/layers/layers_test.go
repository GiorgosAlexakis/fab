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

package layers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdtesting "github.com/GiorgosAlexakis/fab/pkg/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/layers"
)

// writeFoundry builds a foundry whose layer graph forces a merge order that
// alphabetical ordering would get wrong: meta-storage sorts last but must be
// merged before meta-core.
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
      version: ">=1.0.0, <2.0.0"
    - name: meta-storage
    - name: meta-core
`)

	writeFile(t, filepath.Join(root, "layers", "meta-elo", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-elo
  version: 1.0.3
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
      version: ">=1.0.0, <2.0.0"
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

	for _, want := range []string{"LAYER", "VERSION", "ORIGIN", "DEPENDS ON", "1.0.3", "upstream", "local"} {
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

	lock := &layers.Lock{}
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

func TestRunWithoutLayers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "foundry.yaml"), `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
`)

	out, errOut := run(t, root, "")
	if out != "" {
		t.Errorf("expected no table, got:\n%s", out)
	}
	if !strings.Contains(errOut, "activates no layers") {
		t.Errorf("expected an explanation on stderr, got: %q", errOut)
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
