package v1

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFoundry(t *testing.T, content string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, FoundryFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", FoundryFileName, err)
	}
	return root
}

func read(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestInitCreatesAFoundry(t *testing.T) {
	root := t.TempDir()

	if err := NewEngine(root).Init(InitOptions{
		Name:              "acme-corp",
		FoundationVersion: DefaultFoundationVersion,
	}); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	saved := read(t, filepath.Join(root, FoundryFileName))
	for _, want := range []string{"name: acme-corp", "name: " + FoundationLayer} {
		if !strings.Contains(saved, want) {
			t.Errorf("%s is missing %q:\n%s", FoundryFileName, want, saved)
		}
	}

	// The foundation layer is scaffolded so that a new foundry resolves before
	// it has fetched anything.
	manifest := read(t, filepath.Join(root, LayersDir, FoundationLayer, ManifestFileName))
	if !strings.Contains(manifest, "name: "+FoundationLayer) {
		t.Errorf("the scaffolded manifest is not %s:\n%s", FoundationLayer, manifest)
	}

	if gitignored := read(t, filepath.Join(root, ".gitignore")); !strings.Contains(gitignored, ".fab/") {
		t.Errorf(".gitignore should exclude the cache, got:\n%s", gitignored)
	}
}

// The version the caller picks reaches both files: the manifest is scaffolded at
// that exact version, and the foundry activates it at the major it belongs to.
func TestInitScaffoldsTheChosenFoundationVersion(t *testing.T) {
	root := t.TempDir()

	if err := NewEngine(root).Init(InitOptions{
		Name:              "acme-corp",
		FoundationVersion: "0.2.0",
	}); err != nil {
		t.Fatalf("Init() failed: %v", err)
	}

	manifest := read(t, filepath.Join(root, LayersDir, FoundationLayer, ManifestFileName))
	if !strings.Contains(manifest, "version: 0.2.0") {
		t.Errorf("the scaffolded manifest is not at 0.2.0:\n%s", manifest)
	}

	saved := read(t, filepath.Join(root, FoundryFileName))
	if !strings.Contains(saved, ">=0.2.0, <1.0.0") {
		t.Errorf("%s should activate the layer at the 0.x major:\n%s", FoundryFileName, saved)
	}
}

// Init takes the version the manifest is written at, so a range has nowhere to
// go: there would be no single version to scaffold.
func TestInitRejectsAVersionThatIsNotExact(t *testing.T) {
	for _, version := range []string{">=0.1.0, <1.0.0", "v0.1.0", "0.1", "latest", ""} {
		root := t.TempDir()

		err := NewEngine(root).Init(InitOptions{Name: "acme-corp", FoundationVersion: version})
		if err == nil {
			t.Errorf("Init() accepted %q as a foundation version", version)
			continue
		}
		if _, statErr := os.Stat(filepath.Join(root, FoundryFileName)); !os.IsNotExist(statErr) {
			t.Errorf("Init() wrote a foundry it rejected for version %q", version)
		}
	}
}

// Saving encodes the struct, so what the struct does not model is not written
// back. An older fab binary can read a foundry.yaml with newer keys, but saving
// one rewrites the file as the keys this binary knows.
func TestSaveWritesOnlyWhatTheStructModels(t *testing.T) {
	root := writeFoundry(t, `# The Acme foundry.
apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  bsp: aws
  layers:
    - name: meta-elo # the foundation
      version: ">=0.1.0, <1.0.0"
`)

	f := NewFoundry("acme-corp")
	if err := f.AddLayer(FoundationLayer, ">=0.1.0, <1.0.0"); err != nil {
		t.Fatalf("AddLayer() failed: %v", err)
	}
	if err := NewEngine(root).Save(f); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	saved := read(t, filepath.Join(root, FoundryFileName))
	for _, unwanted := range []string{"# The Acme foundry.", "# the foundation", "bsp: aws"} {
		if strings.Contains(saved, unwanted) {
			t.Errorf("saving kept %q, which the struct does not model:\n%s", unwanted, saved)
		}
	}
	if !strings.Contains(saved, "name: "+FoundationLayer) {
		t.Errorf("saving lost the layer selection:\n%s", saved)
	}
}
