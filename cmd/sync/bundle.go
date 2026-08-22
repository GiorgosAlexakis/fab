package sync

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultBundleURL is the repository the official layers are released from.
//
// The official layers are one repository rather than one per layer, because they
// are developed and released as a compatible set. This is the model kas uses for
// Yocto, and the reason foundry.lock pins a single commit for all of them.
const DefaultBundleURL = "https://github.com/GiorgosAlexakis/fab.git"

// DefaultBundleRef is the ref fetched when a foundry has never been synced and
// none was asked for.
const DefaultBundleRef = "main"

// CacheDir is where fetched bundles are unpacked, relative to the foundry root.
// It is gitignored: a foundry commits the symlinks and the lock, never the cache.
const CacheDir = ".fab/cache"

// bundleLayersDir is the directory the layers live in inside a bundle.
const bundleLayersDir = "layers"

// shortRefLength is how much of a commit SHA names a cache directory. It is long
// enough to be unambiguous and short enough to read in a symlink target.
const shortRefLength = 12

// ErrLayerNotInBundle reports a declared layer the bundle does not carry.
var ErrLayerNotInBundle = errors.New("the bundle has no such layer")

// bundleCheckout is a bundle fetched into the local cache.
type bundleCheckout struct {
	// URL is the repository it came from.
	URL string
	// Ref is the ref that was asked for.
	Ref string
	// GitRef is the commit the ref resolved to.
	GitRef string
	// Dir is the cache directory holding the checkout.
	Dir string
}

// layerDir returns where a layer sits inside the checkout.
func (b bundleCheckout) layerDir(name string) string {
	return filepath.Join(b.Dir, bundleLayersDir, name)
}

// resolveRef asks the remote which commit a ref points at.
//
// Resolution is separate from fetching so that a cache directory can be named
// after the commit before anything is downloaded, which is what lets a repeated
// sync of an unchanged pin do no work.
func resolveRef(url, ref string) (string, error) {
	output, err := git("", "ls-remote", url, ref)
	if err != nil {
		return "", err
	}

	line := strings.TrimSpace(output)
	if line == "" {
		return "", fmt.Errorf("the bundle at %s has no ref %q", url, ref)
	}

	sha, _, found := strings.Cut(strings.Split(line, "\n")[0], "\t")
	if !found || sha == "" {
		return "", fmt.Errorf("could not read the commit of %q from %s", ref, url)
	}
	return sha, nil
}

// fetchBundle makes the bundle at ref available in root's cache and returns the
// checkout. A cache directory for the resolved commit that already exists is
// reused rather than fetched again.
//
// Only the declared layers are checked out. A bundle carries every official layer,
// and a foundry has no use for the ones it does not activate.
func fetchBundle(root, url, ref string, wanted []string) (bundleCheckout, error) {
	sha, err := resolveRef(url, ref)
	if err != nil {
		return bundleCheckout{}, err
	}

	checkout := bundleCheckout{URL: url, Ref: ref, GitRef: sha,
		Dir: filepath.Join(root, CacheDir, cacheName(sha))}

	if _, err := os.Stat(checkout.Dir); err == nil {
		// The commit is already cached, but a layer activated since it was fetched
		// will not have been checked out of it.
		return checkout, extendCheckout(checkout, wanted)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return bundleCheckout{}, fmt.Errorf("reading %s: %w", checkout.Dir, err)
	}

	if err := os.MkdirAll(filepath.Dir(checkout.Dir), 0o755); err != nil {
		return bundleCheckout{}, fmt.Errorf("creating %s: %w", filepath.Dir(checkout.Dir), err)
	}

	// The checkout is assembled under a temporary name and renamed into place, so
	// that an interrupted fetch cannot leave a half-populated cache directory
	// that the next run would mistake for a complete one.
	staging, err := os.MkdirTemp(filepath.Dir(checkout.Dir), ".staging-*")
	if err != nil {
		return bundleCheckout{}, fmt.Errorf("creating a staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	if err := checkoutLayers(staging, url, ref, wanted); err != nil {
		return bundleCheckout{}, err
	}
	if err := os.Rename(staging, checkout.Dir); err != nil {
		return bundleCheckout{}, fmt.Errorf("populating %s: %w", checkout.Dir, err)
	}
	return checkout, nil
}

// extendCheckout adds layers to a cached checkout that does not hold them yet.
//
// The layers already there are kept, so that activating one layer does not take
// another out of a cache a second foundry may be sharing a symlink into.
func extendCheckout(checkout bundleCheckout, wanted []string) error {
	present, err := os.ReadDir(filepath.Join(checkout.Dir, bundleLayersDir))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", checkout.Dir, err)
	}

	paths := make(map[string]bool, len(present)+len(wanted))
	for _, entry := range present {
		paths[filepath.Join(bundleLayersDir, entry.Name())] = true
	}

	missing := false
	for _, name := range wanted {
		path := filepath.Join(bundleLayersDir, name)
		if !paths[path] {
			missing = true
		}
		paths[path] = true
	}
	if !missing {
		return nil
	}

	return sparseCheckout(checkout.Dir, checkout.GitRef, sortedKeys(paths))
}

// sortedKeys returns the keys of a set, in order.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sparseCheckout narrows dir's working tree to paths at the given commit.
func sparseCheckout(dir, commit string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	if _, err := git(dir, append([]string{"sparse-checkout", "set", "--no-cone"}, paths...)...); err != nil {
		return err
	}
	_, err := git(dir, "checkout", "--quiet", commit)
	return err
}

// checkoutLayers shallow-fetches ref into dir and checks out only wanted layers.
func checkoutLayers(dir, url, ref string, wanted []string) error {
	steps := [][]string{
		{"init", "--quiet"},
		{"remote", "add", "origin", url},
		{"config", "core.sparseCheckout", "true"},
		{"fetch", "--quiet", "--depth", "1", "origin", ref},
	}
	for _, step := range steps {
		if _, err := git(dir, step...); err != nil {
			return err
		}
	}

	paths := make([]string, 0, len(wanted))
	for _, name := range wanted {
		paths = append(paths, filepath.Join(bundleLayersDir, name))
	}
	return sparseCheckout(dir, "FETCH_HEAD", paths)
}

// cacheName is the cache directory name for a commit.
func cacheName(sha string) string {
	if len(sha) > shortRefLength {
		sha = sha[:shortRefLength]
	}
	return "foundry-" + sha
}

// git runs a git command, returning its standard output.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stderr strings.Builder
	cmd.Stderr = &stderr

	output, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), message)
	}
	return string(output), nil
}
