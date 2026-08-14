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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
)

// LockFileName is the name of the resolved, pinned layer set.
const LockFileName = "foundry.lock"

// ErrNoLock reports that a foundry has never been resolved.
var ErrNoLock = errors.New("no foundry.lock")

// Lock is the generated, committed record of a resolution. It is never
// hand-edited: `fab resolve` writes it and every other environment reads it, so
// that a build somewhere else composes the same layers in the same order.
type Lock struct {
	// APIVersion is the versioned schema of this file.
	APIVersion string `json:"apiVersion,omitempty"`
	// Kind is always Lock.
	Kind string `json:"kind,omitempty"`
	// Bundle pins where the upstream layers came from.
	Bundle *Bundle `json:"bundle,omitempty"`
	// Locked is the resolved layer set in build order.
	Locked []LockedLayer `json:"locked"`
}

// LockKind is the kind a foundry.lock document carries.
const LockKind = "Lock"

// Bundle pins the upstream layer bundle a foundry's official layers came from.
//
// The official layers are released as one repository at one commit rather than
// N independently versioned ones, so a single SHA pins all of them as a
// compatible set. This is the same trade kas makes for Yocto: fewer moving parts
// in exchange for no mix-and-match between layers that are developed together.
//
// fab does not fetch layers yet, so nothing populates this. It is part of the
// file format because the fetcher will write it and `fab resolve` rewrites the
// lock on every run: without somewhere to put it, resolving would delete the pin
// that makes upstream layers reproducible.
type Bundle struct {
	// URL is the bundle repository.
	URL string `json:"url,omitempty"`
	// Ref is the human-readable ref that was requested, e.g. v1.2.0.
	Ref string `json:"ref,omitempty"`
	// GitRef is the exact commit the ref resolved to. This is the pin.
	GitRef string `json:"gitRef,omitempty"`
	// Digest is the content digest of the fetched bundle.
	Digest string `json:"digest,omitempty"`
	// Layers are the bundle layers that were checked out, which is only the
	// ones foundry.yaml activates.
	Layers []string `json:"layers,omitempty"`
}

// LockedLayer is one pinned layer.
type LockedLayer struct {
	// Name is the layer name.
	Name string `json:"name"`
	// Version is the exact version that was resolved, not a range.
	Version string `json:"version"`
	// Origin records whether the layer is upstream or company-owned.
	Origin layerv1.Origin `json:"origin,omitempty"`
	// Digest is the content digest of the layer's manifest. It detects an
	// upstream layer that changed underneath a pinned version.
	Digest string `json:"digest,omitempty"`
	// DependsOn are the layer's dependencies, recorded so that reading the lock
	// alone explains the order.
	DependsOn []string `json:"dependsOn,omitempty"`
}

// NewLock records a resolution.
func NewLock(resolution *Resolution) (*Lock, error) {
	lock := &Lock{APIVersion: layerv1.SchemeGroupVersion.String(), Kind: LockKind}

	for _, layer := range resolution.Ordered {
		digest, err := manifestDigest(layer)
		if err != nil {
			return nil, err
		}

		dependencies := make([]string, 0, len(layer.Manifest.Spec.DependsOn))
		for _, dependency := range layer.Manifest.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}
		sort.Strings(dependencies)

		lock.Locked = append(lock.Locked, LockedLayer{
			Name:      layer.Name(),
			Version:   layer.Version(),
			Origin:    layer.Origin(),
			Digest:    digest,
			DependsOn: dependencies,
		})
	}

	return lock, nil
}

// LoadLock reads the foundry.lock in root. It returns an error wrapping ErrNoLock
// when the file is absent.
func LoadLock(root string) (*Lock, error) {
	path := filepath.Join(root, LockFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrNoLock)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	lock := &Lock{}
	if err := yaml.Unmarshal(data, lock); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return lock, nil
}

// SaveLock writes lock to the foundry.lock in root.
func SaveLock(root string, lock *Lock) error {
	data, err := yaml.Marshal(lock)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", LockFileName, err)
	}

	header := "# Generated by `fab resolve`. Commit this file; do not edit it.\n"
	path := filepath.Join(root, LockFileName)
	if err := os.WriteFile(path, append([]byte(header), data...), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Diff reports how a lock differs from a resolution, as lines to show the user.
// An empty result means the lock is current.
func (l *Lock) Diff(resolution *Resolution) ([]string, error) {
	fresh, err := NewLock(resolution)
	if err != nil {
		return nil, err
	}

	var changes []string

	locked := make(map[string]LockedLayer, len(l.Locked))
	for _, entry := range l.Locked {
		locked[entry.Name] = entry
	}
	resolved := make(map[string]LockedLayer, len(fresh.Locked))
	for _, entry := range fresh.Locked {
		resolved[entry.Name] = entry
	}

	for _, entry := range fresh.Locked {
		previous, ok := locked[entry.Name]
		switch {
		case !ok:
			changes = append(changes, fmt.Sprintf("+ %s %s", entry.Name, entry.Version))
		case previous.Version != entry.Version:
			changes = append(changes, fmt.Sprintf("~ %s %s -> %s", entry.Name, previous.Version, entry.Version))
		case previous.Digest != entry.Digest:
			changes = append(changes, fmt.Sprintf("~ %s %s changed without a version bump",
				entry.Name, entry.Version))
		}
	}

	var removed []string
	for _, entry := range l.Locked {
		if _, ok := resolved[entry.Name]; !ok {
			removed = append(removed, fmt.Sprintf("- %s %s", entry.Name, entry.Version))
		}
	}
	sort.Strings(removed)
	changes = append(changes, removed...)

	if order := orderDiff(l, fresh); order != "" {
		changes = append(changes, order)
	}
	return changes, nil
}

// orderDiff reports a changed build order even when the layer set is identical,
// because the merge order decides which layer sees which types.
func orderDiff(previous, fresh *Lock) string {
	if len(previous.Locked) != len(fresh.Locked) {
		return ""
	}
	for i := range fresh.Locked {
		if previous.Locked[i].Name != fresh.Locked[i].Name {
			return fmt.Sprintf("~ build order %s -> %s",
				strings.Join(lockNames(previous), " "), strings.Join(lockNames(fresh), " "))
		}
	}
	return ""
}

func lockNames(lock *Lock) []string {
	result := make([]string, 0, len(lock.Locked))
	for _, entry := range lock.Locked {
		result = append(result, entry.Name)
	}
	return result
}

// manifestDigest hashes a layer's manifest as it sits on disk.
//
// Hashing the file rather than the parsed struct is deliberate: the point is to
// notice that an upstream layer was edited, and a comment or a key order change
// is still an edit somebody should see.
func manifestDigest(layer Layer) (string, error) {
	path := filepath.Join(layer.Dir, layerv1.FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
