package initialize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

// inTempDir moves the test into an empty working directory, because fab init
// creates the foundry under the directory it is run from and takes no say in
// where that is. The working directory belongs to the process rather than to
// the test, so nothing in this file may run in parallel.
func inTempDir(t *testing.T) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(previous) })
}

// initFoundry drives the command the way cobra does, so that what the flags are
// wired to is what the test exercises. What the created foundry looks like on
// disk is the engine's test to make; these are about the command.
func initFoundry(t *testing.T, name, version string) (string, error) {
	t.Helper()

	streams, _, _, _ := genericiooptions.NewTestIOStreams()

	o := NewOptions(streams)
	if version != "" {
		o.Version = version
	}

	if err := o.Complete(NewCmdInit(streams), []string{name}); err != nil {
		return o.root, err
	}
	if err := o.Validate(); err != nil {
		return o.root, err
	}
	return o.root, o.Run()
}

func read(t *testing.T, root string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, cmdutil.FoundryFileName))
	if err != nil {
		t.Fatalf("the new foundry has no readable %s: %v", cmdutil.FoundryFileName, err)
	}
	return string(data)
}

func TestRunCreatesAFoundryNamedAfterIt(t *testing.T) {
	inTempDir(t)

	root, err := initFoundry(t, "acme-corp", "")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}
	if filepath.Base(root) != "acme-corp" {
		t.Errorf("root = %q, want a directory named after the foundry", root)
	}

	created := read(t, root)
	if !strings.Contains(created, "name: acme-corp") {
		t.Errorf("%s is not named after the foundry:\n%s", cmdutil.FoundryFileName, created)
	}
	// The foundation layer is activated explicitly, like any other layer.
	if !strings.Contains(created, "name: "+cmdutil.FoundationLayer) {
		t.Errorf("a new foundry should activate %s:\n%s", cmdutil.FoundationLayer, created)
	}
}

// The flag reaches the engine, which writes it into both the manifest it
// scaffolds and the range the foundry activates that manifest at.
func TestRunScaffoldsTheVersionTheFlagNames(t *testing.T) {
	inTempDir(t)

	root, err := initFoundry(t, "acme-corp", "0.2.0")
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if created := read(t, root); !strings.Contains(created, ">=0.2.0, <1.0.0") {
		t.Errorf("--version did not reach %s:\n%s", cmdutil.FoundryFileName, created)
	}

	manifest, err := os.ReadFile(
		filepath.Join(root, cmdutil.LayersDir, cmdutil.FoundationLayer, cmdutil.ManifestFileName))
	if err != nil {
		t.Fatalf("reading the scaffolded manifest: %v", err)
	}
	if !strings.Contains(string(manifest), "version: 0.2.0") {
		t.Errorf("--version did not reach the scaffolded manifest:\n%s", manifest)
	}
}

// --version names the version the manifest is scaffolded at, so a range has
// nowhere to go. Catching it here rather than in the engine keeps the flag in
// the message.
func TestValidateRejectsAVersionThatIsNotExact(t *testing.T) {
	inTempDir(t)

	for _, version := range []string{">=0.1.0, <1.0.0", "v0.1.0", "0.1", "latest"} {
		_, err := initFoundry(t, "acme-corp", version)
		if err == nil {
			t.Errorf("init accepted --version %q", version)
			continue
		}
		if !strings.Contains(err.Error(), "--version") {
			t.Errorf("--version %q: error should name the flag, got: %v", version, err)
		}
	}
}

// A foundry is created, never merged into one that is already there.
func TestRunRefusesToOverwriteAFoundry(t *testing.T) {
	inTempDir(t)

	if _, err := initFoundry(t, "acme-corp", ""); err != nil {
		t.Fatal(err)
	}

	_, err := initFoundry(t, "acme-corp", "")
	if err == nil {
		t.Fatal("init should refuse to overwrite an existing foundry")
	}
	if !strings.Contains(err.Error(), cmdutil.FoundryFileName) {
		t.Errorf("error should name the file in the way, got: %v", err)
	}
}
