package util

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
	utilerrors "github.com/GiorgosAlexakis/fab/internal/util/errors"
)

var ErrNoManifest = errors.New("no layer.yaml")

var ErrDanglingLayer = errors.New("layer symlink points at a missing directory")

func ManifestPath(root, name string) string {
	return filepath.Join(root, LayersDir, name, ManifestFileName)
}

func Discover(root string) ([]*layerv1.Layer, error) {
	layersDir := filepath.Join(root, LayersDir)

	entries, err := os.ReadDir(layersDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", layersDir, err)
	}

	var discovered []*layerv1.Layer
	var problems []error
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		dir := filepath.Join(layersDir, entry.Name())
		linked := entry.Type()&fs.ModeSymlink != 0

		info, err := os.Stat(dir)
		if err != nil {
			problems = append(problems, describeUnreadable(dir, linked, err))
			continue
		}
		if !info.IsDir() {
			continue
		}

		layer, err := LoadLayer(dir)
		if err != nil {
			problems = append(problems, err)
			continue
		}
		discovered = append(discovered, layer)
	}

	if len(problems) > 0 {
		return nil, utilerrors.NewAggregate(problems)
	}

	sort.Slice(discovered, func(i, j int) bool {
		return discovered[i].Metadata.Name < discovered[j].Metadata.Name
	})
	return discovered, nil
}

func describeUnreadable(dir string, linked bool, err error) error {
	if !linked || !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", dir, err)
	}

	target, readErr := os.Readlink(dir)
	if readErr != nil {
		return fmt.Errorf("%s: %w", dir, ErrDanglingLayer)
	}
	return fmt.Errorf("%s: %w: %s does not exist, so the upstream layer cache is "+
		"missing or out of date", dir, ErrDanglingLayer, target)
}

func LoadLayer(dir string) (*layerv1.Layer, error) {
	path := filepath.Join(dir, ManifestFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w: every layer directory needs one", path, ErrNoManifest)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	manifest := &layerv1.Layer{}
	if err := yaml.UnmarshalStrict(data, manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	layerv1.SetDefaults_Layer(manifest)

	if err := manifest.Validate(); err != nil {
		return nil, fmt.Errorf("%s is not valid: %w", path, err)
	}

	if base := filepath.Base(dir); manifest.Metadata.Name != base {
		return nil, fmt.Errorf("%s: metadata.name is %q but the layer directory is %q",
			path, manifest.Metadata.Name, base)
	}

	return manifest, nil
}
