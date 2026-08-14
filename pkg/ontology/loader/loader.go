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

// Package loader reads ontology YAML documents off disk and decodes them into
// fab/v1 API objects.
//
// The loader knows about files, directories and layers. It does not validate
// anything beyond "this decodes into a kind I recognise" -- validation is the
// validation package's job and cross-document checks are the compiler's.
package loader

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
)

// Defaults for the foundry layout described in elo_layers.md §10.
const (
	// DefaultSchemaDir is the directory holding a layer's or a company's
	// schema documents, relative to the layer root or the foundry root.
	DefaultSchemaDir = "schema"
	// DefaultLayersDir is the directory holding the active layers, relative to
	// the foundry root. Upstream layers are symlinks into .fab/cache.
	DefaultLayersDir = "layers"
	// DefaultAppLayer is the layer name given to the company's own schema
	// documents in the foundry root.
	DefaultAppLayer = "app"
)

// Options configures loading a whole foundry.
type Options struct {
	// Root is the foundry root: the directory holding foundry.yaml.
	Root string
	// SchemaDir is the company schema directory relative to Root.
	SchemaDir string
	// LayersDir is the layers directory relative to Root.
	LayersDir string
	// AppLayer is the layer name for documents in SchemaDir.
	AppLayer string
}

// SetDefaults fills in the standard foundry layout for any unset field.
func (o *Options) SetDefaults() {
	if o.SchemaDir == "" {
		o.SchemaDir = DefaultSchemaDir
	}
	if o.LayersDir == "" {
		o.LayersDir = DefaultLayersDir
	}
	if o.AppLayer == "" {
		o.AppLayer = DefaultAppLayer
	}
}

// LoadFoundry loads every layer's schema documents from a foundry tree, plus
// the company's own schema directory.
//
// Layers are returned in alphabetical order with the app layer last. That is a
// stand-in for real ordering: once layer.yaml dependency resolution exists, the
// resolver supplies the topological merge order and this ordering goes away.
func LoadFoundry(opts Options) ([]compiler.LayerSource, error) {
	opts.SetDefaults()

	var sources []compiler.LayerSource

	layerNames, err := discoverLayers(filepath.Join(opts.Root, opts.LayersDir))
	if err != nil {
		return nil, err
	}
	for _, layerName := range layerNames {
		schemaDir := filepath.Join(opts.Root, opts.LayersDir, layerName, opts.SchemaDir)
		source, err := LoadLayerDir(schemaDir, layerName)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				// A layer that ships only code or only infrastructure is
				// legal; the schema directory is additive.
				continue
			}
			return nil, err
		}
		sources = append(sources, *source)
	}

	appSchemaDir := filepath.Join(opts.Root, opts.SchemaDir)
	appSource, err := LoadLayerDir(appSchemaDir, opts.AppLayer)
	switch {
	case err == nil:
		sources = append(sources, *appSource)
	case errors.Is(err, fs.ErrNotExist):
		// A foundry that only activates layers has no company schema yet.
	default:
		return nil, err
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no schema documents found in %s: expected %s/ or %s/*/%s/",
			opts.Root, opts.SchemaDir, opts.LayersDir, opts.SchemaDir)
	}
	return sources, nil
}

// LoadLayerDir loads every ontology document under dir as contributions of the
// named layer. It returns an error wrapping fs.ErrNotExist if dir is absent.
func LoadLayerDir(dir, layer string) (*compiler.LayerSource, error) {
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s: not a directory", dir)
	}
	return LoadLayerFS(os.DirFS(dir), ".", layer, dir)
}

// LoadLayerFS loads documents from root within fsys. displayPath prefixes
// error messages so they name a path the user recognises.
func LoadLayerFS(fsys fs.FS, root, layer, displayPath string) (*compiler.LayerSource, error) {
	source := &compiler.LayerSource{Layer: layer}

	var filePaths []string
	err := fs.WalkDir(fsys, root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if isYAML(filePath) {
			filePaths = append(filePaths, filePath)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", displayPath, err)
	}

	// Walk order is already lexical, but sorting makes the guarantee explicit:
	// the compiled snapshot must not depend on filesystem iteration order.
	sort.Strings(filePaths)

	for _, filePath := range filePaths {
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", displaySource(displayPath, filePath), err)
		}
		documents, err := decodeDocuments(data, displaySource(displayPath, filePath))
		if err != nil {
			return nil, err
		}
		source.Documents = append(source.Documents, documents...)
	}

	return source, nil
}

// decodeDocuments splits a YAML stream and decodes each document into the API
// type registered for its kind.
func decodeDocuments(data []byte, source string) ([]compiler.Document, error) {
	var documents []compiler.Document

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for index := 0; ; index++ {
		raw, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", source, err)
		}
		if isEmptyDocument(raw) {
			continue
		}

		documentSource := source
		if index > 0 {
			// Only disambiguate when a file really holds several documents, so
			// the common single-document case stays a plain path.
			documentSource = fmt.Sprintf("%s[%d]", source, index)
		}

		object, err := decodeDocument(raw, documentSource)
		if err != nil {
			return nil, err
		}
		documents = append(documents, compiler.Document{Source: documentSource, Object: object})
	}

	return documents, nil
}

// isEmptyDocument reports whether a document carries no content. A file that
// holds only comments, or a stream with a trailing "---", produces documents
// that are not zero bytes but decode to nothing.
func isEmptyDocument(raw []byte) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	var content interface{}
	if err := yaml.Unmarshal(raw, &content); err != nil {
		// Leave malformed YAML to the decoder, which reports it with the file
		// name attached.
		return false
	}
	return content == nil
}

func decodeDocument(raw []byte, source string) (ontologyv1.Object, error) {
	var typeMeta ontologyv1.TypeMeta
	if err := yaml.Unmarshal(raw, &typeMeta); err != nil {
		return nil, fmt.Errorf("%s: not a valid YAML document: %w", source, err)
	}
	if typeMeta.Kind == "" {
		return nil, fmt.Errorf("%s: kind is required, expected one of %v",
			source, ontologyv1.KnownKinds())
	}

	object, err := ontologyv1.New(typeMeta.Kind)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	// Strict decoding turns a misspelled field into an error instead of a
	// silently ignored line. A schema is a contract; a typo in it must not be
	// discovered three environments later.
	if err := yaml.UnmarshalStrict(raw, object); err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	return object, nil
}

// discoverLayers lists the layer directories under layersDir. Entries are
// resolved through symlinks because upstream layers are symlinks into
// .fab/cache, as described in elo_build.md §7.
func discoverLayers(layersDir string) ([]string, error) {
	entries, err := os.ReadDir(layersDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", layersDir, err)
	}

	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		info, err := os.Stat(filepath.Join(layersDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("resolving %s: %w", filepath.Join(layersDir, entry.Name()), err)
		}
		if info.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func isYAML(filePath string) bool {
	switch strings.ToLower(path.Ext(filePath)) {
	case ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func displaySource(displayPath, filePath string) string {
	if filePath == "." {
		return displayPath
	}
	return filepath.Join(displayPath, filePath)
}
