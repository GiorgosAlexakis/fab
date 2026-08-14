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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
)

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
	if err := os.WriteFile(filepath.Join(cached, layerv1.FileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(root, DefaultLayersDir, name)
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

	discovered, err := Discover(filepath.Join(root, DefaultLayersDir))
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("found %d layers, want 2", len(discovered))
	}

	byName := map[string]Layer{}
	for _, layer := range discovered {
		byName[layer.Name()] = layer
	}

	linked, ok := byName["meta-elo"]
	if !ok {
		t.Fatal("the linked layer was not discovered")
	}
	if !linked.Linked {
		t.Error("a layer reached through a symlink should be reported as linked")
	}
	if linked.Version() != "1.0.3" || linked.Origin() != layerv1.OriginUpstream {
		t.Errorf("linked layer = %s/%s, want 1.0.3/upstream", linked.Version(), linked.Origin())
	}
	if local := byName["meta-marine"]; local.Linked {
		t.Error("a real directory should not be reported as linked")
	}
}

// This is what a company foundry looks like when it has just been cloned: the
// symlinks are committed but the cache they point into is gitignored, so every
// upstream layer is dangling until the cache is populated.
func TestDiscoverReportsAnUnpopulatedCache(t *testing.T) {
	root := t.TempDir()
	layersDir := filepath.Join(root, DefaultLayersDir)
	if err := os.MkdirAll(layersDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"meta-elo", "meta-core"} {
		target := filepath.Join("..", ".fab", "cache", "foundry-a3f8c21", "layers", name)
		if err := os.Symlink(target, filepath.Join(layersDir, name)); err != nil {
			t.Fatal(err)
		}
	}

	_, err := Discover(layersDir)
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
	discovered, err := Discover(filepath.Join(t.TempDir(), DefaultLayersDir))
	if err != nil {
		t.Fatalf("Discover() failed: %v", err)
	}
	if len(discovered) != 0 {
		t.Errorf("found %d layers, want none", len(discovered))
	}
}
