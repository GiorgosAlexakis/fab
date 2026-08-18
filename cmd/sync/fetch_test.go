package sync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GiorgosAlexakis/fab/cmd/resolve"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

// writeLayerBundle builds a git repository shaped like the upstream layer bundle, so
// that a sync can be exercised without reaching the network.
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

func writeLayerBundle(t *testing.T, specs ...layerSpec) string {
	t.Helper()

	bundle := writeLayers(t, specs...)
	for _, args := range [][]string{
		{"init", "--quiet", "--initial-branch", DefaultBundleRef},
		{"config", "user.email", "layers@test"},
		{"config", "user.name", "layers test"},
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

// writeConsumer builds a foundry that declares layers but has none on disk, which
// is what a foundry looks like after `fab init` or a fresh clone.
func writeConsumer(t *testing.T, names ...string) string {
	t.Helper()

	root := t.TempDir()
	document := "apiVersion: fab/v1\nkind: Foundry\nmetadata:\n  name: acme-corp\nspec:\n  layers:\n"
	for _, name := range names {
		document += "    - name: " + name + "\n"
	}
	if err := os.WriteFile(filepath.Join(root, "foundry.yaml"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestSyncLinksUpstreamLayersIntoTheCache(t *testing.T) {
	bundle := writeLayerBundle(t,
		layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream},
		layerSpec{name: "meta-core", version: "1.0.0", origin: layerv1.OriginUpstream,
			dependsOn: []string{"meta-elo"}},
		layerSpec{name: "meta-billing", version: "1.0.0", origin: layerv1.OriginUpstream},
	)
	root := writeConsumer(t, "meta-elo", "meta-core")

	result, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef})
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}
	if !result.Fetched {
		t.Error("the first sync of a foundry should fetch the bundle")
	}
	if !equal(result.Linked, []string{"meta-core", "meta-elo"}) {
		t.Errorf("linked = %v, want meta-core and meta-elo", result.Linked)
	}
	if result.Bundle == nil || result.Bundle.GitRef == "" {
		t.Fatalf("sync should return the pin it fetched, got %+v", result.Bundle)
	}

	// A layer is reached through a relative symlink into the cache, so the foundry
	// can be moved or cloned without every layer breaking.
	link := filepath.Join(root, cmdutil.LayersDir, "meta-elo")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("%s is not a symlink: %v", link, err)
	}
	if filepath.IsAbs(target) {
		t.Errorf("symlink target %q should be relative", target)
	}
	if !strings.Contains(target, CacheDir) {
		t.Errorf("symlink target %q should point into %s", target, CacheDir)
	}

	if _, err := os.Stat(filepath.Join(root, cmdutil.LayersDir, "meta-billing")); err == nil {
		t.Error("an undeclared layer should not be linked")
	}
	cached := filepath.Join(root, CacheDir, cacheName(result.Bundle.GitRef), bundleLayersDir, "meta-billing")
	if _, err := os.Stat(cached); err == nil {
		t.Error("an undeclared layer should not be checked out of the bundle")
	}

	resolved, err := resolve.ResolveFoundry(root)
	if err != nil {
		t.Fatalf("ResolveFoundry() failed after a sync: %v", err)
	}
	if got := resolved.Resolution.Names(); !equal(got, []string{"meta-elo", "meta-core"}) {
		t.Errorf("build order = %v, want meta-elo then meta-core", got)
	}
}

// A second sync of an unchanged pin does nothing, which is what makes it cheap to
// run in CI and usable without a network.
func TestSyncIsIdempotent(t *testing.T) {
	bundle := writeLayerBundle(t, layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream})
	root := writeConsumer(t, "meta-elo")

	first, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef})
	if err != nil {
		t.Fatal(err)
	}

	// The pin is only reused once it has been written down, which `fab sync` does
	// through the lock.
	lock := &foundryv1.Lock{Bundle: first.Bundle}
	if err := cmdutil.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}

	second, err := Sync(SyncOptions{Root: root})
	if err != nil {
		t.Fatalf("the second Sync() failed: %v", err)
	}
	if second.Fetched {
		t.Error("a foundry already synced to its pin should not fetch again")
	}
	if second.Bundle == nil || second.Bundle.GitRef != first.Bundle.GitRef {
		t.Errorf("the second sync changed the pin: %+v", second.Bundle)
	}
}

// A layer added to foundry.yaml after the first sync is missing from layers/, so
// the pin is no longer satisfied even though it has not changed.
func TestSyncLinksALayerAddedAfterTheFirstSync(t *testing.T) {
	bundle := writeLayerBundle(t,
		layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream},
		layerSpec{name: "meta-core", version: "1.0.0", origin: layerv1.OriginUpstream},
	)
	root := writeConsumer(t, "meta-elo")

	first, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef})
	if err != nil {
		t.Fatal(err)
	}
	if err := cmdutil.SaveLock(root, &foundryv1.Lock{Bundle: first.Bundle}); err != nil {
		t.Fatal(err)
	}

	document := "apiVersion: fab/v1\nkind: Foundry\nmetadata:\n  name: acme-corp\n" +
		"spec:\n  layers:\n    - name: meta-elo\n    - name: meta-core\n"
	if err := os.WriteFile(filepath.Join(root, "foundry.yaml"), []byte(document), 0o600); err != nil {
		t.Fatal(err)
	}

	second, err := Sync(SyncOptions{Root: root})
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}
	if !second.Fetched {
		t.Error("a newly activated layer should be fetched")
	}
	if !equal(second.Linked, []string{"meta-core", "meta-elo"}) {
		t.Errorf("linked = %v, want both layers", second.Linked)
	}
}

// A company-owned layer is the company's, not the bundle's. Sync must not fetch
// it, replace it, or fail because the bundle has never heard of it.
func TestSyncLeavesLocalLayersAlone(t *testing.T) {
	bundle := writeLayerBundle(t, layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream})
	root := writeConsumer(t, "meta-elo", "meta-marine")

	local := filepath.Join(root, cmdutil.LayersDir, "meta-marine")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-marine\n  version: 0.1.0\n" +
		"  origin: local\nspec:\n  dependsOn:\n    - name: meta-elo\n      version: \">=0.1.0, <1.0.0\"\n"
	if err := os.WriteFile(filepath.Join(local, cmdutil.ManifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef})
	if err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}
	if !equal(result.Local, []string{"meta-marine"}) {
		t.Errorf("local = %v, want meta-marine", result.Local)
	}
	if !equal(result.Linked, []string{"meta-elo"}) {
		t.Errorf("linked = %v, want only meta-elo", result.Linked)
	}

	info, err := os.Lstat(local)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Error("a company-owned layer should still be a real directory")
	}
}

// An upstream layer that was checked in as a real directory belongs in the cache:
// the pin, not the working tree, is what says what an upstream layer contains.
func TestSyncReplacesAScaffoldedUpstreamLayer(t *testing.T) {
	bundle := writeLayerBundle(t, layerSpec{name: "meta-elo", version: "0.2.0", origin: layerv1.OriginUpstream})
	root := writeConsumer(t, "meta-elo")

	scaffolded := filepath.Join(root, cmdutil.LayersDir, "meta-elo")
	if err := os.MkdirAll(scaffolded, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "apiVersion: fab/v1\nkind: Layer\nmetadata:\n  name: meta-elo\n  version: 0.1.0\n  origin: upstream\n"
	if err := os.WriteFile(filepath.Join(scaffolded, cmdutil.ManifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef}); err != nil {
		t.Fatalf("Sync() failed: %v", err)
	}

	if _, err := os.Readlink(scaffolded); err != nil {
		t.Fatalf("the scaffolded layer should have become a symlink: %v", err)
	}
	layer, err := cmdutil.LoadLayer(scaffolded)
	if err != nil {
		t.Fatal(err)
	}
	if layer.Metadata.Version != "0.2.0" {
		t.Errorf("linked version = %q, want the bundle's 0.2.0", layer.Metadata.Version)
	}
}

func TestSyncReportsALayerTheBundleDoesNotCarry(t *testing.T) {
	bundle := writeLayerBundle(t, layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream})
	root := writeConsumer(t, "meta-elo", "meta-ghost")

	_, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: DefaultBundleRef})
	if !errors.Is(err, ErrLayerNotInBundle) {
		t.Fatalf("Sync() error = %v, want ErrLayerNotInBundle", err)
	}
	if !strings.Contains(err.Error(), "meta-ghost") {
		t.Errorf("error should name the layer, got: %v", err)
	}
}

func TestSyncReportsAnUnknownRef(t *testing.T) {
	bundle := writeLayerBundle(t, layerSpec{name: "meta-elo", version: "0.1.0", origin: layerv1.OriginUpstream})
	root := writeConsumer(t, "meta-elo")

	_, err := Sync(SyncOptions{Root: root, BundleURL: bundle, BundleRef: "v9.9.9"})
	if err == nil {
		t.Fatal("Sync() should fail on a ref the bundle does not have")
	}
	if !strings.Contains(err.Error(), "v9.9.9") {
		t.Errorf("error should name the ref, got: %v", err)
	}
}

func TestSyncWithoutAFoundry(t *testing.T) {
	_, err := Sync(SyncOptions{Root: t.TempDir()})
	if err == nil {
		t.Fatal("Sync() should fail without a foundry.yaml")
	}
	if !strings.Contains(err.Error(), "fab init") {
		t.Errorf("error should point at `fab init`, got: %v", err)
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
