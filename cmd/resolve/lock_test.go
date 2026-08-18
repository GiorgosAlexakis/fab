package resolve

import (
	"strings"
	"testing"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
)

func TestLockRoundTrips(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.3"},
		layerSpec{name: "meta-core", version: "1.0.1", dependsOn: []string{"meta-elo"}},
	)

	resolution, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-core"))
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}

	lock, err := NewLock(root, resolution)
	if err != nil {
		t.Fatalf("NewLock() failed: %v", err)
	}
	if err := cmdutil.SaveLock(root, lock); err != nil {
		t.Fatalf("SaveLock() failed: %v", err)
	}

	reloaded, err := cmdutil.LoadLock(root)
	if err != nil {
		t.Fatalf("LoadLock() failed: %v", err)
	}
	if len(reloaded.Locked) != 2 {
		t.Fatalf("got %d locked layers, want 2", len(reloaded.Locked))
	}
	if reloaded.Locked[0].Name != "meta-elo" || reloaded.Locked[1].Name != "meta-core" {
		t.Errorf("lock does not preserve build order: %v", lockNames(reloaded))
	}
	if !strings.HasPrefix(reloaded.Locked[0].Digest, "sha256:") {
		t.Errorf("digest = %q", reloaded.Locked[0].Digest)
	}

	changes, err := DiffLock(reloaded, root, resolution)
	if err != nil {
		t.Fatalf("Diff() failed: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("a freshly written lock should be current, got %v", changes)
	}
}

func TestDiffReportsAddedRemovedAndChangedLayers(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-core", version: "1.0.0", dependsOn: []string{"meta-elo"}},
	)
	resolution, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-core"))
	if err != nil {
		t.Fatal(err)
	}

	stale := &foundryv1.Lock{Locked: []foundryv1.LockedLayer{
		{Name: "meta-elo", Version: "0.9.0"},
		{Name: "meta-gone", Version: "1.0.0"},
	}}

	changes, err := DiffLock(stale, root, resolution)
	if err != nil {
		t.Fatalf("Diff() failed: %v", err)
	}

	joined := strings.Join(changes, "\n")
	for _, want := range []string{"~ meta-elo 0.9.0 -> 1.0.0", "+ meta-core 1.0.0", "- meta-gone 1.0.0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Diff() = %v, want a line %q", changes, want)
		}
	}
}

// A layer edited in place without a version bump is exactly the drift a digest
// is there to catch.
func TestDiffNoticesAnEditWithoutAVersionBump(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})
	resolution, err := resolve(t, root, foundryFor(t, "meta-elo"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := NewLock(root, resolution)
	if err != nil {
		t.Fatal(err)
	}

	lock.Locked[0].Digest = "sha256:0000"

	changes, err := DiffLock(lock, root, resolution)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || !strings.Contains(changes[0], "without a version bump") {
		t.Errorf("Diff() = %v, want a digest change", changes)
	}
}

// The merge order decides which layer can reference which types, so a reordering
// is a change even when the layer set is identical.
func TestDiffNoticesAReorder(t *testing.T) {
	root := writeLayers(t,
		layerSpec{name: "meta-elo", version: "1.0.0"},
		layerSpec{name: "meta-core", version: "1.0.0", dependsOn: []string{"meta-elo"}},
	)
	resolution, err := resolve(t, root, foundryFor(t, "meta-elo", "meta-core"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := NewLock(root, resolution)
	if err != nil {
		t.Fatal(err)
	}

	lock.Locked[0], lock.Locked[1] = lock.Locked[1], lock.Locked[0]

	changes, err := DiffLock(lock, root, resolution)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) == 0 || !strings.Contains(strings.Join(changes, "\n"), "build order") {
		t.Errorf("Diff() = %v, want a build order change", changes)
	}
}
