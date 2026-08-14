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

// Package layers discovers the layers in a foundry and resolves them into a
// build order.
//
// A layer is a directory under layers/ with a layer.yaml at its root. Whether
// that directory is company-owned or a symlink into the upstream cache makes no
// difference here: both are read the same way, which is what lets an FDE extend
// an upstream layer without forking it.
package layers

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/yaml"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
	layervalidation "github.com/GiorgosAlexakis/fab/pkg/apis/layer/validation"
)

// DefaultLayersDir is the directory holding the active layers, relative to the
// foundry root.
const DefaultLayersDir = "layers"

// ErrNoManifest reports that a directory under layers/ has no layer.yaml.
var ErrNoManifest = errors.New("no layer.yaml")

// ErrDanglingLayer reports a layers/ symlink whose target does not exist, which
// is what a foundry looks like when the upstream layer cache has not been
// populated or has been cleaned.
var ErrDanglingLayer = errors.New("layer symlink points at a missing directory")

// Layer is a discovered layer: its manifest and where it was found.
type Layer struct {
	// Manifest is the decoded, defaulted layer.yaml.
	Manifest *layerv1.Layer
	// Dir is the layer directory.
	Dir string
	// Path is Dir relative to the foundry root, for error messages.
	Path string
	// Linked reports whether layers/<name> is a symlink, which is how an
	// upstream layer is made available out of .fab/cache.
	Linked bool
}

// Name returns the layer name.
func (l Layer) Name() string { return l.Manifest.Metadata.Name }

// Version returns the layer's own version.
func (l Layer) Version() string { return l.Manifest.Metadata.Version }

// Origin returns whether the layer is upstream or company-owned.
func (l Layer) Origin() layerv1.Origin { return l.Manifest.Metadata.Origin }

// Discover reads the manifest of every layer directory under layersDir.
//
// A directory without a layer.yaml is an error rather than something to skip: it
// is almost always a half-finished layer or a bad symlink, and silently ignoring
// it produces a confusing "unknown layer" much later.
//
// Problems are collected rather than returned one at a time. An unpopulated layer
// cache leaves every upstream symlink dangling at once, and reporting them one
// per run would take as many runs as there are layers to find that out.
func Discover(layersDir string) ([]Layer, error) {
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// A foundry that activates no layers is legal: the company's own
			// schema directory is enough to compile an ontology.
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", layersDir, err)
	}

	var discovered []Layer
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
		layer.Linked = linked
		discovered = append(discovered, *layer)
	}

	if len(problems) > 0 {
		return nil, utilerrors.NewAggregate(problems)
	}

	sort.Slice(discovered, func(i, j int) bool { return discovered[i].Name() < discovered[j].Name() })
	return discovered, nil
}

// describeUnreadable explains why a layers/ entry could not be read. A dangling
// symlink gets its own message because it means something specific: the layer is
// upstream and its cache is missing, not that the layer was deleted.
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

// LoadLayer reads and validates the manifest of the layer in dir.
func LoadLayer(dir string) (*Layer, error) {
	path := filepath.Join(dir, layerv1.FileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w: every layer directory needs one", path, ErrNoManifest)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	manifest := &layerv1.Layer{}
	// Strict decoding: a misspelled key in a manifest silently changes the
	// dependency graph, which is the last place to be lenient.
	if err := yaml.UnmarshalStrict(data, manifest); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	layerv1.SetDefaults_Layer(manifest)

	if errs := layervalidation.ValidateLayer(manifest); len(errs) > 0 {
		return nil, fmt.Errorf("%s is not valid: %w", path, errs.ToAggregate())
	}

	// The directory name is how every other manifest refers to this layer, and
	// how fab finds its schema. A manifest that disagrees with it would make
	// `dependsOn: meta-auth` ambiguous.
	if base := filepath.Base(dir); manifest.Metadata.Name != base {
		return nil, fmt.Errorf("%s: metadata.name is %q but the layer directory is %q",
			path, manifest.Metadata.Name, base)
	}

	return &Layer{Manifest: manifest, Dir: dir, Path: dir}, nil
}

// index maps layer names to layers.
func index(discovered []Layer) map[string]Layer {
	byName := make(map[string]Layer, len(discovered))
	for _, layer := range discovered {
		byName[layer.Name()] = layer
	}
	return byName
}

// names returns the sorted names of the given layers, for error messages.
func names(discovered []Layer) []string {
	result := make([]string, 0, len(discovered))
	for _, layer := range discovered {
		result = append(result, layer.Name())
	}
	sort.Strings(result)
	return result
}
