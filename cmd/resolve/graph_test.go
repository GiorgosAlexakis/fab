package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

type layerSpec struct {
	name      string
	version   string
	dependsOn []string
	origin    layerv1.Origin
}

func writeLayers(t *testing.T, specs ...layerSpec) string {
	t.Helper()

	root := t.TempDir()
	layersDir := filepath.Join(root, cmdutil.LayersDir)

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

		path := filepath.Join(dir, cmdutil.ManifestFileName)
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return root
}

func foundryFor(t *testing.T, names ...string) *foundryv1.Foundry {
	t.Helper()

	declared := foundryv1.NewFoundry("acme-corp")
	for _, name := range names {
		if err := declared.AddLayer(name, ""); err != nil {
			t.Fatalf("AddLayer(%s) failed: %v", name, err)
		}
	}
	return declared
}

func resolve(t *testing.T, root string, declared *foundryv1.Foundry) (*Resolution, error) {
	t.Helper()

	discovered, err := cmdutil.Discover(root)
	if err != nil {
		return nil, err
	}
	return Resolve(declared, discovered)
}

func TestResolveOrdersDependenciesFirst(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-auth", version: "1.2.0", dependsOn: []string{"meta-elo", "meta-core"}},
		layerSpec{name: "meta-core", version: "1.0.1", dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-elo", version: "1.0.3"},
	)

	resolution, err := resolve(t, root, foundryFor(t, "meta-auth", "meta-core", "meta-elo"))
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
	declared := foundryFor(t, "meta-elo", "meta-storage", "meta-billing", "meta-auth")

	want := []string{"meta-elo", "meta-auth", "meta-billing", "meta-storage"}
	for attempt := 0; attempt < 20; attempt++ {
		resolution, err := resolve(t, root, declared)
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
	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-auth"))
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

	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-auth"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency that foundry.yaml does not declare")
	}
	if !strings.Contains(err.Error(), "meta-core") {
		t.Errorf("error should name meta-core, got: %v", err)
	}
}

func TestResolveRejectsAnUnknownLayer(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-ghost"))
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

	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-auth"))
	if err == nil {
		t.Fatal("Resolve() should reject a dependency outside its compatibility window")
	}
	if !strings.Contains(err.Error(), "tested against") {
		t.Errorf("error should explain the window, got: %v", err)
	}
}

func TestResolveEnforcesTheFoundrySelector(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.5.0"})

	declared := foundryv1.NewFoundry("acme-corp")
	if err := declared.AddLayer("meta-elo", ">=2.0.0, <3.0.0"); err != nil {
		t.Fatal(err)
	}

	_, err := resolve(t, root, declared)
	if err == nil {
		t.Fatal("Resolve() should reject a layer outside the range foundry.yaml pinned")
	}
	if !strings.Contains(err.Error(), "1.5.0") {
		t.Errorf("error should name the actual version, got: %v", err)
	}
}

func TestResolveDetectsACycle(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-alpha", version: "1.0.0", dependsOn: []string{"meta-beta"}},
		layerSpec{name: "meta-beta", version: "1.0.0", dependsOn: []string{"meta-alpha"}},
	)

	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-alpha", "meta-beta"))
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

	_, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-auth", "meta-billing"))
	if err == nil {
		t.Fatal("Resolve() should fail")
	}
	message := err.Error()
	if !strings.Contains(message, "meta-core") || !strings.Contains(message, "tested against") {
		t.Errorf("error should report both problems, got: %v", err)
	}
}

// The foundation layer is not activated implicitly. Everything else is built on
// it, and a foundry.yaml that lists every layer except the mandatory one would be
// a worse file to read.
func TestResolveRequiresTheFoundationLayer(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-core", version: "1.0.0", dependsOn: []string{"meta-elo"}},
	)

	_, err := resolve(t, root, foundryFor(t, "meta-core"))
	if err == nil {
		t.Fatal("Resolve() should reject a foundry that does not activate meta-elo")
	}
	if !strings.Contains(err.Error(), cmdutil.FoundationLayer) {
		t.Errorf("error should name the foundation layer, got: %v", err)
	}
}

func TestResolveWithOnlyTheFoundationLayer(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	resolution, err := resolve(t, root, foundryFor(t, "meta-elo"))
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if got := resolution.Names(); !equal(got, []string{"meta-elo"}) {
		t.Errorf("order = %v, want just meta-elo", got)
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
