package util

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
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
	layersDir := filepath.Join(root, LayersDir)

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

		path := filepath.Join(dir, ManifestFileName)
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}
	return root
}

// linkIntoCache makes an upstream layer available the way the layer cache does:
// layers/<name> is a symlink into a gitignored cache directory.
func linkIntoCache(t *testing.T, root, name, version string) {
	t.Helper()

	cached := filepath.Join(root, ".fab", "cache", "foundry-a3f8c21", "layers", name)
	if err := os.MkdirAll(cached, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: " + name +
		"\n  version: " + version + "\n  origin: upstream\n"
	if err := os.WriteFile(filepath.Join(cached, ManifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, LayersDir, name)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(cached, link); err != nil {
		t.Fatal(err)
	}
}

// An upstream layer reached through a symlink is read exactly like a local
// directory, which is what lets a foundry mix the two without fab caring.
func TestDiscoverReadsLinkedLayers(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-marine", version: "0.1.0"})
	linkIntoCache(t, root, "meta-elo", "1.0.3")

	discovered, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("found %d layers, want 2", len(discovered))
	}

	byName := map[string]*layerv1.Layer{}
	for _, layer := range discovered {
		byName[layer.Metadata.Name] = layer
	}

	linked, ok := byName["meta-elo"]
	if !ok {
		t.Fatal("the linked layer was not discovered")
	}
	if linked.Metadata.Version != "1.0.3" || linked.Metadata.Origin != layerv1.OriginUpstream {
		t.Errorf("linked layer = %s/%s, want 1.0.3/upstream", linked.Metadata.Version, linked.Metadata.Origin)
	}
}

// This is what a company foundry looks like when it has just been cloned: the
// symlinks are committed but the cache they point into is gitignored, so every
// upstream layer is dangling until the cache is populated.
func TestDiscoverReportsAnUnpopulatedCache(t *testing.T) {
	root := t.TempDir()
	layersDir := filepath.Join(root, LayersDir)
	if err := os.MkdirAll(layersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"meta-elo", "meta-core"} {
		target := filepath.Join("..", ".fab", "cache", "foundry-a3f8c21", "layers", name)
		if err := os.Symlink(target, filepath.Join(layersDir, name)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Discover(root)
	if err == nil {
		t.Fatal("Discover() should report dangling layer symlinks")
	}
	if !errors.Is(err, ErrDanglingLayer) {
		t.Errorf("error should be an ErrDanglingLayer, got: %v", err)
	}

	// Both are reported: an unpopulated cache breaks every upstream layer at
	// once, and fixing them one run at a time would be tedious.
	message := err.Error()
	for _, want := range []string{"meta-elo", "meta-core", ".fab/cache/foundry-a3f8c21"} {
		if !strings.Contains(message, want) {
			t.Errorf("error should mention %q, got: %v", want, message)
		}
	}
}

// A foundry with no layers directory at all is legal: the company's own schema
// directory is enough to compile an ontology.
func TestDiscoverWithoutALayersDirectory(t *testing.T) {
	discovered, err := Discover(t.TempDir())
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 0 {
		t.Errorf("found %d layers, want none", len(discovered))
	}
}

func TestDiscoverRejectsADirectoryWithoutAManifest(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})
	if err := os.MkdirAll(filepath.Join(root, LayersDir, "meta-half-done"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(root)
	if err == nil {
		t.Fatal("Discover() should reject a layer directory with no layer.yaml")
	}
	if !strings.Contains(err.Error(), "meta-half-done") {
		t.Errorf("error should name the directory, got: %v", err)
	}
}

func TestLoadLayerRejectsANameThatDisagreesWithTheDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, LayersDir, "meta-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-authentication\n  version: 1.0.0\n"
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadLayer(dir)
	if err == nil {
		t.Fatal("LoadLayer() should reject a manifest whose name is not the directory name")
	}
}

func TestLoadLayerRejectsAnUnknownField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, LayersDir, "meta-auth")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-auth\n  version: 1.0.0\nspec:\n  dependsUpon: []\n"
	if err := os.WriteFile(filepath.Join(dir, ManifestFileName), []byte(manifest), 0o600); err != nil {
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

	discovered, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("got %d layers, want 2", len(discovered))
	}
	if discovered[0].Metadata.Origin != layerv1.OriginUpstream {
		t.Errorf("meta-elo origin = %q, want upstream", discovered[0].Metadata.Origin)
	}
	if discovered[1].Metadata.Origin != layerv1.OriginLocal {
		t.Errorf("meta-marine origin = %q, want local by default", discovered[1].Metadata.Origin)
	}
	if discovered[0].Metadata.Version != "1.0.3" {
		t.Errorf("meta-elo version = %q", discovered[0].Metadata.Version)
	}
}
