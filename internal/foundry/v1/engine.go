package v1

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

// The names a foundry is laid out with. Init creates this layout and every later
// command reads it, so the names are written once here rather than again per
// command.
const (
	// FoundryFileName is the Foundry document at the root of a foundry.
	FoundryFileName = "foundry.yaml"
	// LayersDir holds one directory per activated layer, each of them either a
	// layer written in this foundry or a symlink into the fetched cache.
	LayersDir = "layers"
	// ManifestFileName is the Layer document at the root of a layer.
	ManifestFileName = "layer.yaml"
	// LockFileName pins what `fab sync` fetched.
	LockFileName = "foundry.lock"
	// FoundationLayer is the layer every other layer is built on. A new foundry
	// activates it and nothing else.
	FoundationLayer = "meta-elo"
)

// DefaultFoundationVersion is the version of the foundation layer a new foundry
// is scaffolded against when the caller names none. `fab sync` replaces the
// scaffolded manifest with the copy the bundle pin describes.
const DefaultFoundationVersion = "0.1.0"

// ErrFoundryNotFound is what Load reports when there is no foundry to read.
var ErrFoundryNotFound = errors.New("no " + FoundryFileName)

// Engine creates and stores a foundry.
//
// It is an interface so that a foundry kept somewhere other than a directory
// does not have to pretend to be one. NewEngine returns the only implementation
// there is today, which keeps a foundry as files on this machine.
type Engine interface {
	// Init creates a foundry that is ready to be resolved before anything has
	// been fetched. It does not overwrite: the caller checks first.
	Init(opts InitOptions) error
	// Save writes the foundry document, replacing the one that is there.
	Save(f *Foundry) error
	// Load reads the foundry document. It reports a foundry that is not there
	// as an error wrapping ErrFoundryNotFound, so callers can say what to do
	// about it rather than repeating the check.
	Load() (*Foundry, error)
}

// InitOptions is what a new foundry is created from.
type InitOptions struct {
	// Name is the foundry's name, which is the company it belongs to.
	Name string
	// FoundationVersion is the exact version of the foundation layer the new
	// foundry is scaffolded against, e.g. "0.2.0". Init widens it into the range
	// the foundry activates the layer at.
	FoundationVersion string
}

// foundationTemplate is the scaffolded manifest of the foundation layer.
const foundationTemplate = `apiVersion: fab/v1
kind: Layer
metadata:
  name: %s
  version: %s
  origin: upstream
  description: >-
    The foundation layer every other layer is built on. This copy is a placeholder
    so that a new foundry resolves before it has fetched anything; ` + "`fab sync`" + `
    replaces it with a link into the layer cache.
spec: {}
`

// gitignore is what a foundry must not commit. The cache is fetched, not authored:
// it is reproducible from the pin in foundry.lock.
const gitignore = `# Fetched by ` + "`fab sync`" + `, reproducible from foundry.lock.
.fab/
`

// engine keeps a foundry as files under a directory on this machine.
type engine struct {
	// root is the directory the foundry document sits at the top of.
	root string
}

// NewEngine returns an Engine for the foundry rooted at root. The directory does
// not have to exist yet: Init creates it.
func NewEngine(root string) Engine {
	return &engine{root: root}
}

// Init lays out a new foundry: the foundation layer, the gitignore, and the
// foundry document that activates the layer.
//
// The foundation layer is scaffolded rather than fetched so that a foundry
// resolves the moment it is created, before it has network access or a cache.
func (e *engine) Init(opts InitOptions) error {
	declared, err := versions.CompatibleRange(opts.FoundationVersion)
	if err != nil {
		return fmt.Errorf("the version of %s to scaffold against: %w", FoundationLayer, err)
	}

	created := NewFoundry(opts.Name)
	if err := created.AddLayer(FoundationLayer, declared); err != nil {
		return err
	}
	// Checked before anything is written, so that a name the FDE has to go back
	// and fix does not leave half a foundry on disk behind it.
	if err := created.Validate(); err != nil {
		return err
	}

	foundationDir := filepath.Join(e.root, LayersDir, FoundationLayer)
	if err := os.MkdirAll(foundationDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", foundationDir, err)
	}

	files := map[string]string{
		filepath.Join(foundationDir, ManifestFileName): fmt.Sprintf(foundationTemplate,
			FoundationLayer, opts.FoundationVersion),
		filepath.Join(e.root, ".gitignore"): gitignore,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}

	return e.Save(created)
}

// Save encodes the foundry and replaces the file that is there.
//
// What the struct does not model is not written back: an older fab binary can
// read a foundry.yaml carrying keys it does not know, but saving one rewrites
// the file as the keys this binary knows.
func (e *engine) Save(f *Foundry) error {
	SetDefaults_Foundry(f)
	if err := f.Validate(); err != nil {
		return fmt.Errorf("refusing to write an invalid %s: %w", FoundryFileName, err)
	}

	data, err := yaml.Marshal(f)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", FoundryFileName, err)
	}

	path := filepath.Join(e.root, FoundryFileName)
	file, err := os.CreateTemp(e.root, FoundryFileName+".*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer os.Remove(file.Name())

	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(file.Name(), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}

	// Renaming over the original means a failure part-way through leaves the
	// foundry's only hand-edited file intact rather than truncated.
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Load reads the foundry document.
//
// Decoding is lenient: this file grows keys that older fab binaries do not know
// about, and refusing to read it would make every new key a forced CLI upgrade.
// Saving is not lenient in the same way, which is what Save documents.
func (e *engine) Load() (*Foundry, error) {
	path := filepath.Join(e.root, FoundryFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%s: %w", path, ErrFoundryNotFound)
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	f := &Foundry{}
	if err := yaml.Unmarshal(data, f); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	SetDefaults_Foundry(f)
	return f, nil
}
