package resolve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
)

func NewLock(root string, resolution *Resolution) (*foundryv1.Lock, error) {
	lock := &foundryv1.Lock{APIVersion: foundryv1.APIVersion, Kind: foundryv1.LockKind}

	for _, layer := range resolution.Ordered {
		digest, err := manifestDigest(root, layer)
		if err != nil {
			return nil, err
		}

		dependencies := make([]string, 0, len(layer.Spec.DependsOn))
		for _, dependency := range layer.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}
		sort.Strings(dependencies)

		lock.Locked = append(lock.Locked, foundryv1.LockedLayer{
			Name:      layer.Metadata.Name,
			Version:   layer.Metadata.Version,
			Origin:    layer.Metadata.Origin,
			Digest:    digest,
			DependsOn: dependencies,
		})
	}

	return lock, nil
}

func DiffLock(lock *foundryv1.Lock, root string, resolution *Resolution) ([]string, error) {
	fresh, err := NewLock(root, resolution)
	if err != nil {
		return nil, err
	}

	var changes []string

	locked := make(map[string]foundryv1.LockedLayer, len(lock.Locked))
	for _, entry := range lock.Locked {
		locked[entry.Name] = entry
	}
	resolved := make(map[string]foundryv1.LockedLayer, len(fresh.Locked))
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
	for _, entry := range lock.Locked {
		if _, ok := resolved[entry.Name]; !ok {
			removed = append(removed, fmt.Sprintf("- %s %s", entry.Name, entry.Version))
		}
	}
	sort.Strings(removed)
	changes = append(changes, removed...)

	if order := orderDiff(lock, fresh); order != "" {
		changes = append(changes, order)
	}
	return changes, nil
}

func orderDiff(previous, fresh *foundryv1.Lock) string {
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

func lockNames(lock *foundryv1.Lock) []string {
	result := make([]string, 0, len(lock.Locked))
	for _, entry := range lock.Locked {
		result = append(result, entry.Name)
	}
	return result
}

func manifestDigest(root string, layer *layerv1.Layer) (string, error) {
	path := cmdutil.ManifestPath(root, layer.Metadata.Name)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
