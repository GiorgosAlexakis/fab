package sync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

// SyncOptions is what a sync needs: where the foundry is and which bundle its
// upstream layers come from.
type SyncOptions struct {
	// Root is the foundry root.
	Root string
	// BundleURL overrides the bundle repository. Empty means the URL the lock
	// already pins, else the official bundle.
	BundleURL string
	// BundleRef overrides the ref to fetch. Empty means the ref the lock already
	// pins, else the default branch of the official bundle.
	BundleRef string
}

// SyncResult is what a sync did.
type SyncResult struct {
	// Bundle is the pin to record in foundry.lock. It is nil when the foundry
	// activates no upstream layers.
	Bundle *foundryv1.Bundle
	// Linked are the layers now reached through the cache.
	Linked []string
	// Local are the company-owned layers, which a sync never touches.
	Local []string
	// Fetched reports whether the bundle had to be downloaded.
	Fetched bool
}

// Sync makes every layer foundry.yaml declares available under layers/.
//
// Upstream layers live in the local cache and are reached through a symlink, so
// that the only copy of an upstream layer is the one the bundle pin describes.
// Company-owned layers are real directories in the foundry repo and are never
// touched. From every other command's point of view the two are identical, which
// is what lets a company extend an upstream layer without forking it.
func Sync(options SyncOptions) (*SyncResult, error) {
	manifest, err := foundryv1.NewEngine(options.Root).Load()
	if err != nil {
		if errors.Is(err, foundryv1.ErrFoundryNotFound) {
			return nil, fmt.Errorf("%w: run `fab init` to create one, or pass --root", err)
		}
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w",
			filepath.Join(options.Root, cmdutil.FoundryFileName), err)
	}

	layersDir := filepath.Join(options.Root, cmdutil.LayersDir)
	result := &SyncResult{}

	var upstream []string
	for _, name := range manifest.LayerNames() {
		owned, err := isLocal(filepath.Join(layersDir, name))
		if err != nil {
			return nil, err
		}
		if owned {
			result.Local = append(result.Local, name)
			continue
		}
		upstream = append(upstream, name)
	}
	if len(upstream) == 0 {
		return result, nil
	}
	sort.Strings(upstream)

	if options.BundleURL == "" && options.BundleRef == "" {
		// A foundry whose pin is already checked out and linked needs nothing.
		// This is what makes a sync in CI, or after a clone where the cache
		// survived, cost nothing and work offline.
		if pin := satisfiedPin(options.Root, layersDir, upstream); pin != nil {
			result.Bundle = pin
			result.Linked = upstream
			return result, nil
		}
	}

	url, ref := options.BundleURL, options.BundleRef
	if url == "" || ref == "" {
		lockedURL, lockedRef := pinnedBundle(options.Root)
		if url == "" {
			url = lockedURL
		}
		if ref == "" {
			ref = lockedRef
		}
	}

	checkout, err := fetchBundle(options.Root, url, ref, upstream)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(layersDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating %s: %w", layersDir, err)
	}
	for _, name := range upstream {
		if err := link(filepath.Join(layersDir, name), checkout.layerDir(name)); err != nil {
			return nil, err
		}
	}

	result.Linked = upstream
	result.Fetched = true
	result.Bundle = &foundryv1.Bundle{URL: checkout.URL, Ref: checkout.Ref, GitRef: checkout.GitRef, Layers: upstream}
	return result, nil
}

// isLocal reports whether a layer directory is company-owned, and so not the
// bundle's to manage.
//
// The manifest's origin is what decides it, not whether the path happens to be a
// symlink today: a layer that says it is upstream belongs in the cache, and one
// that says it is local belongs to whoever wrote it.
func isLocal(dir string) (bool, error) {
	info, err := os.Lstat(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("reading %s: %w", dir, err)
	case info.Mode()&fs.ModeSymlink != 0:
		return false, nil
	}

	layer, err := cmdutil.LoadLayer(dir)
	if err != nil {
		return false, err
	}
	return layer.Metadata.Origin == layerv1.OriginLocal, nil
}

// link points path at target through a relative symlink.
//
// The link is relative so that the foundry can be cloned, moved or mounted
// somewhere else without every layer breaking.
func link(path, target string) error {
	if _, err := os.Stat(target); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("%s: %w", filepath.Base(path), ErrLayerNotInBundle)
		}
		return fmt.Errorf("reading %s: %w", target, err)
	}

	relative, err := filepath.Rel(filepath.Dir(path), target)
	if err != nil {
		return fmt.Errorf("linking %s: %w", path, err)
	}

	if existing, err := os.Readlink(path); err == nil {
		if existing == relative {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("replacing %s: %w", path, err)
		}
	} else if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}

	if err := os.Symlink(relative, path); err != nil {
		return fmt.Errorf("linking %s: %w", path, err)
	}
	return nil
}

// satisfiedPin returns the pin a foundry is already synced to, or nil when there
// is work to do. Every wanted layer must be linked into the cache directory the
// pin names, so adding a layer or changing the pin is never mistaken for a foundry
// that is already in order.
func satisfiedPin(root, layersDir string, wanted []string) *foundryv1.Bundle {
	lock, err := cmdutil.LoadLock(root)
	if err != nil || lock.Bundle == nil || lock.Bundle.GitRef == "" {
		return nil
	}

	cache := filepath.Join(root, CacheDir, cacheName(lock.Bundle.GitRef))
	if _, err := os.Stat(cache); err != nil {
		return nil
	}

	for _, name := range wanted {
		link := filepath.Join(layersDir, name)
		if info, err := os.Lstat(link); err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return nil
		}
		resolved, err := filepath.EvalSymlinks(link)
		if err != nil {
			return nil
		}
		expected, err := filepath.EvalSymlinks(filepath.Join(cache, bundleLayersDir, name))
		if err != nil || resolved != expected {
			return nil
		}
	}

	pinned := *lock.Bundle
	pinned.Layers = wanted
	return &pinned
}

// pinnedBundle returns the bundle a foundry is already pinned to, falling back to
// the official bundle. A foundry that has been synced once keeps fetching the same
// ref, so that a sync is reproducible without anyone passing a flag.
func pinnedBundle(root string) (url, ref string) {
	url, ref = DefaultBundleURL, DefaultBundleRef

	lock, err := cmdutil.LoadLock(root)
	if err != nil || lock.Bundle == nil {
		return url, ref
	}
	if lock.Bundle.URL != "" {
		url = lock.Bundle.URL
	}
	if lock.Bundle.Ref != "" {
		ref = lock.Bundle.Ref
	}
	return url, ref
}
