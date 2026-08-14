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

package loader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
)

const customerYAML = `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Customer
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
    - name: email
      type: string
      unique: true
`

const multiDocumentYAML = `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Order
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
---
apiVersion: fab/v1
kind: LinkType
metadata:
  name: CustomerOrders
spec:
  source:
    type: Customer
  target:
    type: Order
  cardinality: one_to_many
`

func TestLoadLayerFS(t *testing.T) {
	fsys := fstest.MapFS{
		"objects/customer.yaml":     &fstest.MapFile{Data: []byte(customerYAML)},
		"objects/order.yaml":        &fstest.MapFile{Data: []byte(multiDocumentYAML)},
		"objects/notes.md":          &fstest.MapFile{Data: []byte("# not a schema document")},
		"objects/.hidden.yaml.swp":  &fstest.MapFile{Data: []byte("garbage")},
		"objects/empty_stream.yaml": &fstest.MapFile{Data: []byte("# only a comment\n---\n")},
	}

	source, err := LoadLayerFS(fsys, ".", "app", "schema")
	if err != nil {
		t.Fatalf("LoadLayerFS() failed: %v", err)
	}

	if source.Layer != "app" {
		t.Errorf("Layer = %q, want app", source.Layer)
	}
	if len(source.Documents) != 3 {
		t.Fatalf("got %d documents, want 3: %+v", len(source.Documents), documentSources(source.Documents))
	}

	wantSources := []string{
		filepath.Join("schema", "objects/customer.yaml"),
		filepath.Join("schema", "objects/order.yaml"),
		filepath.Join("schema", "objects/order.yaml") + "[1]",
	}
	got := documentSources(source.Documents)
	for i := range wantSources {
		if got[i] != wantSources[i] {
			t.Errorf("document %d source = %q, want %q", i, got[i], wantSources[i])
		}
	}

	if _, ok := source.Documents[0].Object.(*ontologyv1.ObjectType); !ok {
		t.Errorf("document 0 decoded as %T, want *v1.ObjectType", source.Documents[0].Object)
	}
	if _, ok := source.Documents[2].Object.(*ontologyv1.LinkType); !ok {
		t.Errorf("document 2 decoded as %T, want *v1.LinkType", source.Documents[2].Object)
	}
}

func TestLoadLayerFSErrors(t *testing.T) {
	testCases := []struct {
		name        string
		content     string
		wantMessage string
	}{
		{
			name:        "missing kind",
			content:     "apiVersion: fab/v1\nmetadata:\n  name: Customer\n",
			wantMessage: "kind is required",
		},
		{
			name:        "kind from a later phase",
			content:     "apiVersion: fab/v1\nkind: Aspect\nmetadata:\n  name: UserAuthAspect\n",
			wantMessage: "unsupported kind \"Aspect\"",
		},
		{
			name: "misspelled field is rejected, not ignored",
			content: `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Customer
spec:
  primaryKeys: id
  properties:
    - name: id
      type: string
`,
			wantMessage: "unknown field \"primaryKeys\"",
		},
		{
			name:        "not valid YAML",
			content:     "apiVersion: fab/v1\nkind: [ObjectType\n",
			wantMessage: "schema/objects/broken.yaml",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := fstest.MapFS{
				"objects/broken.yaml": &fstest.MapFile{Data: []byte(testCase.content)},
			}
			_, err := LoadLayerFS(fsys, ".", "app", "schema")
			if err == nil {
				t.Fatalf("LoadLayerFS() succeeded, want an error mentioning %q", testCase.wantMessage)
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Fatalf("LoadLayerFS() error = %v, want it to mention %q", err, testCase.wantMessage)
			}
			if !strings.Contains(err.Error(), "broken.yaml") {
				t.Errorf("error does not name the offending file: %v", err)
			}
		})
	}
}

func TestLoadFoundry(t *testing.T) {
	root := t.TempDir()

	// An upstream layer, as `fab sync` leaves it: the layer body lives in the
	// gitignored cache and layers/<name> is a symlink into it.
	cachedLayer := filepath.Join(root, ".fab", "cache", "foundry-a3f8c21", "layers", "meta-core")
	writeFile(t, filepath.Join(cachedLayer, "schema", "objects", "user.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: User
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
`)
	mustMkdirAll(t, filepath.Join(root, "layers"))
	if err := os.Symlink(cachedLayer, filepath.Join(root, "layers", "meta-core")); err != nil {
		t.Fatalf("symlinking the cached layer: %v", err)
	}

	// A local layer, checked into the foundry.
	writeFile(t, filepath.Join(root, "layers", "meta-marine", "schema", "objects", "vessel.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Vessel
spec:
  primaryKey: imo_number
  properties:
    - name: imo_number
      type: string
`)

	// A layer that ships code but no schema is legal and must be skipped.
	mustMkdirAll(t, filepath.Join(root, "layers", "meta-devops", "packages"))

	// The company's own schema.
	writeFile(t, filepath.Join(root, "schema", "objects", "customer.yaml"), customerYAML)

	sources, err := LoadFoundry(Options{Root: root})
	if err != nil {
		t.Fatalf("LoadFoundry() failed: %v", err)
	}

	wantLayers := []string{"meta-core", "meta-marine", "app"}
	if len(sources) != len(wantLayers) {
		t.Fatalf("got %d layers, want %d: %+v", len(sources), len(wantLayers), sources)
	}
	for i, want := range wantLayers {
		if sources[i].Layer != want {
			t.Errorf("layer %d = %q, want %q (layers sort alphabetically with app last)", i, sources[i].Layer, want)
		}
		if len(sources[i].Documents) != 1 {
			t.Errorf("layer %q has %d documents, want 1", sources[i].Layer, len(sources[i].Documents))
		}
	}
}

func TestLoadFoundryWithoutSchema(t *testing.T) {
	root := t.TempDir()

	_, err := LoadFoundry(Options{Root: root})
	if err == nil {
		t.Fatal("LoadFoundry() succeeded on an empty directory, want an error")
	}
	if !strings.Contains(err.Error(), "no schema documents found") {
		t.Errorf("error = %v, want it to explain that no schema documents were found", err)
	}
}

func TestLoadLayerDirMissing(t *testing.T) {
	_, err := LoadLayerDir(filepath.Join(t.TempDir(), "absent"), "app")
	if !os.IsNotExist(err) {
		t.Fatalf("LoadLayerDir() error = %v, want a not-exist error so callers can skip schema-less layers", err)
	}
}

func documentSources(documents []compiler.Document) []string {
	sources := make([]string, 0, len(documents))
	for _, document := range documents {
		sources = append(sources, document.Source)
	}
	return sources
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	mustMkdirAll(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("creating %s: %v", path, err)
	}
}
