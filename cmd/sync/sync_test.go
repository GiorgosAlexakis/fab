package sync

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cmdtesting "github.com/GiorgosAlexakis/fab/cmd/testing"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
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

// writeBundle builds a git repository shaped like the upstream layer bundle, so
// that a sync can be exercised without reaching the network.
func writeBundle(t *testing.T) string {
	t.Helper()

	bundle := t.TempDir()
	writeFile(t, filepath.Join(bundle, "layers", "meta-elo", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-elo
  version: 0.1.0
  origin: upstream
`)
	writeFile(t, filepath.Join(bundle, "layers", "meta-core", "layer.yaml"), `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.0
  origin: upstream
spec:
  dependsOn:
    - name: meta-elo
      version: ">=0.1.0, <1.0.0"
`)

	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch", DefaultBundleRef},
		{"config", "user.email", "sync@test"},
		{"config", "user.name", "sync test"},
		{"add", "."},
		{"commit", "--quiet", "-m", "the bundle"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = bundle
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}
	return bundle
}

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
    - name: meta-core
      version: ">=1.0.0, <2.0.0"
`)
	return root
}

func sync(t *testing.T, root, bundle, output string) (string, string, error) {
	t.Helper()

	streams, _, out, errOut := genericiooptions.NewTestIOStreams()
	factory := cmdutil.Factory(cmdtesting.NewTestFactory(root))

	o := NewOptions(streams)
	o.BundleURL = bundle
	o.Output = output
	if bundle != "" {
		o.BundleRef = DefaultBundleRef
	}

	if err := o.Complete(factory, NewCmdSync(factory, streams), nil); err != nil {
		return out.String(), errOut.String(), err
	}
	if err := o.Validate(); err != nil {
		return out.String(), errOut.String(), err
	}

	err := o.Run()
	return out.String(), errOut.String(), err
}

func TestRunFetchesLinksAndPins(t *testing.T) {
	root := writeFoundry(t)

	out, errOut, err := sync(t, root, writeBundle(t), "")
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	for _, want := range []string{"BUNDLE:", "COMMIT:", "LAYER", "meta-elo", "meta-core"} {
		if !strings.Contains(out, want) {
			t.Errorf("output is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "Linked 2 upstream layers") {
		t.Errorf("stderr should report what was linked, got: %q", errOut)
	}

	lock, err := cmdutil.LoadLock(root)
	if err != nil {
		t.Fatalf("LoadLock() failed: %v", err)
	}
	if lock.Bundle == nil || lock.Bundle.GitRef == "" {
		t.Fatalf("the lock should pin the bundle, got %+v", lock.Bundle)
	}
	if len(lock.Locked) != 2 || lock.Locked[0].Name != "meta-elo" {
		t.Errorf("locked layers = %+v, want meta-elo then meta-core", lock.Locked)
	}
}

// The second sync reuses the pin the first one wrote, so it needs no flags and no
// network.
func TestRunIsIdempotent(t *testing.T) {
	root := writeFoundry(t)
	if _, _, err := sync(t, root, writeBundle(t), ""); err != nil {
		t.Fatal(err)
	}

	_, errOut, err := sync(t, root, "", "")
	if err != nil {
		t.Fatalf("the second sync failed: %v", err)
	}
	if !strings.Contains(errOut, "Already synced") {
		t.Errorf("stderr should report that there was nothing to do, got: %q", errOut)
	}
}

func TestRunJSON(t *testing.T) {
	out, _, err := sync(t, writeFoundry(t), writeBundle(t), "json")
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if !strings.Contains(out, `"gitRef"`) {
		t.Errorf("JSON output should carry the bundle pin:\n%s", out)
	}
}

func TestRunWithoutAFoundry(t *testing.T) {
	_, _, err := sync(t, t.TempDir(), writeBundle(t), "")
	if err == nil {
		t.Fatal("sync should fail without a foundry.yaml")
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
