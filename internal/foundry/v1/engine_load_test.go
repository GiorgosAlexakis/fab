package v1

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	root := writeFoundry(t, `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0"
    - name: meta-marine
      version: "~2.3.0"
`)

	f, err := NewEngine(root).Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if f.Metadata.Name != "acme-corp" {
		t.Errorf("metadata.name = %q, want acme-corp", f.Metadata.Name)
	}
	if len(f.Spec.Layers) != 2 {
		t.Fatalf("got %d layers, want 2", len(f.Spec.Layers))
	}
	if f.Spec.Layers[1].Name != "meta-marine" || f.Spec.Layers[1].Version != "~2.3.0" {
		t.Errorf("second layer = %+v", f.Spec.Layers[1])
	}
}

// TestLoadIgnoresUnknownKeys guards the compatibility promise: foundry.yaml will
// grow keys, and an older fab binary must keep working rather than refuse to
// read the file.
func TestLoadIgnoresUnknownKeys(t *testing.T) {
	root := writeFoundry(t, `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  bsp: aws-eks
  adapters:
    payments: stripe
  layers:
    - name: meta-core
      version: ">=1.0.0"
      overrides:
        - schema/objects/user.yaml
`)

	f, err := NewEngine(root).Load()
	if err != nil {
		t.Fatalf("Load() failed on a file with newer keys: %v", err)
	}
	if f.Metadata.Name != "acme-corp" {
		t.Errorf("metadata.name = %q, want acme-corp", f.Metadata.Name)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := NewEngine(t.TempDir()).Load()
	if !errors.Is(err, ErrFoundryNotFound) {
		t.Fatalf("Load() error = %v, want ErrFoundryNotFound", err)
	}
}

func TestSaveRoundTripsAnEdit(t *testing.T) {
	root := writeFoundry(t, `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
`)
	e := NewEngine(root)

	f, err := e.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if err := f.AddLayer("meta-core", ">=1.0.0, <2.0.0"); err != nil {
		t.Fatalf("AddLayer() failed: %v", err)
	}
	if err := e.Save(f); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	reloaded, err := e.Load()
	if err != nil {
		t.Fatalf("Load() failed after Save(): %v", err)
	}
	if got := reloaded.LayerNames(); len(got) != 2 || got[1] != "meta-core" {
		t.Fatalf("layers = %v, want meta-elo then meta-core", got)
	}
	if spec, _ := reloaded.Selects("meta-core"); spec != ">=1.0.0, <2.0.0" {
		t.Errorf("meta-core range = %q, want >=1.0.0, <2.0.0", spec)
	}
	if err := reloaded.Validate(); err != nil {
		t.Errorf("the saved file does not validate: %v", err)
	}
}

// Save is the last gate before a file fab wrote lands on disk. A foundry.yaml the
// FDE did not type is the hardest one for them to debug.
func TestSaveRefusesAnInvalidFoundry(t *testing.T) {
	root := t.TempDir()

	if err := NewEngine(root).Save(NewFoundry("Acme Corp")); err == nil {
		t.Fatal("Save() accepted a foundry whose name is not a DNS subdomain")
	}
	if _, err := os.Stat(filepath.Join(root, FoundryFileName)); !os.IsNotExist(err) {
		t.Errorf("Save() wrote a file it rejected: %v", err)
	}
}

// A rejected save leaves the file that was there untouched, rather than half
// written or truncated.
func TestSaveLeavesTheOriginalAloneWhenItRefuses(t *testing.T) {
	original := `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
`
	root := writeFoundry(t, original)
	e := NewEngine(root)

	f, err := e.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	f.Metadata.Name = ""

	if err := e.Save(f); err == nil {
		t.Fatal("Save() accepted a foundry without a name")
	}
	if got := read(t, filepath.Join(root, FoundryFileName)); got != original {
		t.Errorf("the original file changed:\n%s", got)
	}
}

// Init checks the document before it lays anything out, so a name that has to be
// fixed does not leave a half-created foundry to clean up.
func TestInitRefusesANameThatIsNotADNSSubdomain(t *testing.T) {
	root := t.TempDir()

	err := NewEngine(root).Init(InitOptions{
		Name:              "Acme Corp",
		FoundationVersion: DefaultFoundationVersion,
	})
	if err == nil {
		t.Fatal("Init() accepted a name that cannot be a DNS subdomain")
	}
	if _, statErr := os.Stat(filepath.Join(root, LayersDir)); !os.IsNotExist(statErr) {
		t.Errorf("Init() laid out a foundry it rejected: %v", statErr)
	}
}
