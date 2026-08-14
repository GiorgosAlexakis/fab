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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
	"github.com/GiorgosAlexakis/fab/pkg/foundry"
)

// layerSpec is a compact description of a layer for tests: name, version and the
// layers it depends on, written as "meta-core@>=1.0.0, <2.0.0".
type layerSpec struct {
	name      string
	version   string
	dependsOn []string
	origin    layerv1.Origin
}

func writeLayers(t *testing.T, specs ...layerSpec) string {
	t.Helper()

	root := t.TempDir()
	layersDir := filepath.Join(root, DefaultLayersDir)

	for _, spec := range specs {
		dir := filepath.Join(layersDir, spec.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating %s: %v", dir, err)
		}

		manifest := fmt.Sprintf("apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: %s\n  version: %s\n",
			spec.name, spec.version)
		if spec.origin != "" {
			manifest += fmt.Sprintf("  origin: %s\n", spec.origin)
		}
		if len(spec.dependsOn) > 0 {
			manifest += "spec:\n  dependsOn:\n"
			for _, dependency := range spec.dependsOn {
				name, constraint, found := strings.Cut(dependency, "@")
				if !found {
					constraint = ">=0.0.1, <2.0.0"
				}
				manifest += fmt.Sprintf("    - name: %s\n      version: %q\n", name, constraint)
			}
		}

		path := filepath.Join(dir, layerv1.FileName)
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return root
}

func configFor(names ...string) *foundry.Config {
	config := &foundry.Config{Metadata: foundry.Metadata{Name: "acme-corp"}}
	for _, name := range names {
		config.Spec.Layers = append(config.Spec.Layers, foundry.LayerSelector{Name: name})
	}
	config.SetDefaults()
	return config
}

func resolve(t *testing.T, root string, config *foundry.Config) (*Resolution, error) {
	t.Helper()

	discovered, err := Discover(filepath.Join(root, DefaultLayersDir))
	if err != nil {
		return nil, err
	}
	return Resolve(config, discovered)
}

func TestResolveOrdersDependenciesFirst(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-auth", version: "1.2.0", dependsOn: []string{"meta-elo", "meta-core"}},
		layerSpec{name: "meta-core", version: "1.0.1", dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-elo", version: "1.0.3"},
	)

	resolution, err := resolve(t, root, configFor("meta-auth", "meta-core", "meta-elo"))
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}

	want := []string{"meta-elo", "meta-core", "meta-auth"}
	if got := resolution.Names(); !equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

// Independent layers must come out in a stable order, or the compiled ontology
// digest would change between runs on the same input.
func TestResolveIsDeterministicForIndependentLayers(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-storage", version: "1.0.0", dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-billing", version: "1.0.0", dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-auth", version: "1.0.0", dependsOn: []string{"meta-elo"}},
	)
	config := configFor("meta-elo", "meta-storage", "meta-billing", "meta-auth")

	want := []string{"meta-elo", "meta-auth", "meta-billing", "meta-storage"}
	for attempt := 0; attempt < 20; attempt++ {
		resolution, err := resolve(t, root, config)
		if err != nil {
			t.Fatalf("Resolve() failed: %v", err)
		}
		if got := resolution.Names(); !equal(got, want) {
			t.Fatalf("attempt %d: order = %v, want %v", attempt, got, want)
		}
	}
}

func TestResolveRejectsAMissingDependency(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-auth", version: "1.0.0", dependsOn: []string{"meta-elo", "meta-core"}},
	)

	// meta-core is not on disk at all.
	_, err := resolve(t, root, configFor("meta-elo", "meta-auth"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency that is not active")
	}
	if !strings.Contains(err.Error(), "add \"meta-core\" to spec.layers in foundry.yaml") {
		t.Errorf("error should say how to fix it, got: %v", err)
	}
}

// A layer present on disk but absent from foundry.yaml is not active. foundry.yaml
// stays the single place that decides what is in the stack.
func TestResolveRejectsAnUndeclaredDependency(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-core", version: "1.0.0", dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-auth", version: "1.0.0", dependsOn: []string{"meta-elo", "meta-core"}},
	)

	_, err := resolve(t, root, configFor("meta-elo", "meta-auth"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency that foundry.yaml does not declare")
	}
	if !strings.Contains(err.Error(), "meta-core") {
		t.Errorf("error should name meta-core, got: %v", err)
	}
}

func TestResolveRejectsAnUnknownLayer(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	_, err := resolve(t, root, configFor("meta-elo", "meta-ghost"))
	if err == nil {
		t.Fatal("Resolve() should reject a layer that is not in the layers directory")
	}
	if !strings.Contains(err.Error(), "meta-ghost") {
		t.Errorf("error should name meta-ghost, got: %v", err)
	}
}

// The upper bound in a version range is the whole reason it exists: a breaking
// major release of a dependency must fail resolution, not the build after it.
func TestResolveEnforcesTheCompatibilityWindow(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "2.0.0"},
		layerSpec{name: "meta-auth", version: "1.0.0", dependsOn: []string{"meta-elo@>=1.0.0, <2.0.0"}},
	)

	_, err := resolve(t, root, configFor("meta-elo", "meta-auth"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency outside its compatibility window")
	}
	if !strings.Contains(err.Error(), "tested against") {
		t.Errorf("error should explain the window, got: %v", err)
	}
}

func TestResolveEnforcesTheFoundrySelector(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.5.0"})

	config := configFor()
	config.Spec.Layers = []foundry.LayerSelector{{Name: "meta-elo", Version: ">=2.0.0, <3.0.0"}}

	_, err := resolve(t, root, config)
	if err == nil {
		t.Fatal("Resolve() should reject a layer outside the range foundry.yaml pinned")
	}
	if !strings.Contains(err.Error(), "1.5.0") {
		t.Errorf("error should name the actual version, got: %v", err)
	}
}

func TestResolveDetectsACycle(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-alpha", version: "1.0.0", dependsOn: []string{"meta-beta"}},
		layerSpec{name: "meta-beta", version: "1.0.0", dependsOn: []string{"meta-alpha"}},
	)

	_, err := resolve(t, root, configFor("meta-alpha", "meta-beta"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Errorf("error should mention a cycle, got: %v", err)
	}
}

// Every problem is reported at once, so that fixing a foundry is one pass rather
// than one error at a time.
func TestResolveReportsEveryProblemAtOnce(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "2.0.0"},
		layerSpec{name: "meta-auth", version: "1.0.0", dependsOn: []string{"meta-elo@>=1.0.0, <2.0.0"}},
		layerSpec{name: "meta-billing", version: "1.0.0", dependsOn: []string{"meta-core@>=1.0.0, <2.0.0"}},
	)

	_, err := resolve(t, root, configFor("meta-elo", "meta-auth", "meta-billing"))
	if err == nil {
		t.Fatal("Resolve() should fail")
	}
	message := err.Error()
	if !strings.Contains(message, "meta-core") || !strings.Contains(message, "tested against") {
		t.Errorf("error should report both problems, got: %v", err)
	}
}

func TestResolveWithNoLayers(t *testing.T) {
	root := t.TempDir()

	resolution, err := resolve(t, root, configFor())
	if err != nil {
		t.Fatalf("Resolve() failed for a foundry with no layers: %v", err)
	}
	if len(resolution.Ordered) != 0 {
		t.Errorf("expected no layers, got %v", resolution.Names())
	}
}

func TestDiscoverRejectsADirectoryWithoutAManifest(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})
	if err := os.MkdirAll(filepath.Join(root, DefaultLayersDir, "meta-half-done"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(filepath.Join(root, DefaultLayersDir))
	if err == nil {
		t.Fatal("Discover() should reject a layer directory with no layer.yaml")
	}
	if !strings.Contains(err.Error(), "meta-half-done") {
		t.Errorf("error should name the directory, got: %v", err)
	}
}

func TestLoadLayerRejectsANameThatDisagreesWithTheDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DefaultLayersDir, "meta-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-authentication\n  version: 1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, layerv1.FileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLayer(dir)
	if err == nil {
		t.Fatal("LoadLayer() should reject a manifest whose name is not the directory name")
	}
}

func TestLoadLayerRejectsAnUnknownField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, DefaultLayersDir, "meta-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-auth\n  version: 1.0.0\nspec:\n  dependsUpon: []\n"
	if err := os.WriteFile(filepath.Join(dir, layerv1.FileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadLayer(dir); err == nil {
		t.Fatal("LoadLayer() should reject an unknown field")
	}
}

func TestDiscoverReadsOriginAndVersion(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.3", origin: layerv1.OriginUpstream},
		layerSpec{name: "meta-marine", version: "0.1.0"},
	)

	discovered, err := Discover(filepath.Join(root, DefaultLayersDir))
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("got %d layers, want 2", len(discovered))
	}
	if discovered[0].Origin() != layerv1.OriginUpstream {
		t.Errorf("meta-elo origin = %q, want upstream", discovered[0].Origin())
	}
	if discovered[1].Origin() != layerv1.OriginLocal {
		t.Errorf("meta-marine origin = %q, want local by default", discovered[1].Origin())
	}
	if discovered[0].Version() != "1.0.3" {
		t.Errorf("meta-elo version = %q", discovered[0].Version())
	}
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
