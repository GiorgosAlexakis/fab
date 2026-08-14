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
	"os"
	"path/filepath"
	"strings"
	"testing"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
)

// writeDeclaringLayer builds a foundry whose meta-core layer declares two object
// types and one link type.
func writeDeclaringLayer(t *testing.T) string {
	t.Helper()

	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	dir := filepath.Join(root, DefaultLayersDir, "meta-core")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-core
  version: 1.0.1
  origin: upstream
spec:
  dependsOn:
    - name: meta-elo
      version: ">=1.0.0, <2.0.0"
  provides:
    schema:
      objects: [Organization, User]
      links: [OrganizationUsers]
`
	if err := os.WriteFile(filepath.Join(dir, layerv1.FileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCheckProvides(t *testing.T) {
	root := writeDeclaringLayer(t)

	resolution, err := resolve(t, root, configFor("meta-elo", "meta-core"))
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}

	matching := map[string]Contributions{
		"meta-core": {
			Objects: []string{"Organization", "User"},
			Links:   []string{"OrganizationUsers"},
		},
	}
	if err := CheckProvides(resolution, matching); err != nil {
		t.Errorf("CheckProvides() failed on a matching manifest: %v", err)
	}
}

func TestCheckProvidesReportsDisagreementWithTheTree(t *testing.T) {
	root := writeDeclaringLayer(t)

	resolution, err := resolve(t, root, configFor("meta-elo", "meta-core"))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name    string
		actual  Contributions
		wantErr string
	}{
		{
			name:    "a declared type is missing from the tree",
			actual:  Contributions{Objects: []string{"Organization"}, Links: []string{"OrganizationUsers"}},
			wantErr: "declares object types it does not ship: User",
		},
		{
			name: "the tree ships a type the manifest hides",
			actual: Contributions{
				Objects: []string{"Organization", "User", "ApiKey"},
				Links:   []string{"OrganizationUsers"},
			},
			wantErr: "ships object types it does not declare: ApiKey",
		},
		{
			name:    "a declared link is missing",
			actual:  Contributions{Objects: []string{"Organization", "User"}},
			wantErr: "declares link types it does not ship: OrganizationUsers",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := CheckProvides(resolution, map[string]Contributions{"meta-core": test.actual})
			if err == nil {
				t.Fatalf("CheckProvides() should report %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("error = %v, want it to mention %q", err, test.wantErr)
			}
		})
	}
}

// A layer that does not declare provides.schema is not making a claim, so there
// is nothing to contradict.
func TestCheckProvidesIgnoresLayersThatDeclareNothing(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	resolution, err := resolve(t, root, configFor("meta-elo"))
	if err != nil {
		t.Fatal(err)
	}

	actual := map[string]Contributions{"meta-elo": {Objects: []string{"Anything"}}}
	if err := CheckProvides(resolution, actual); err != nil {
		t.Errorf("CheckProvides() failed: %v", err)
	}
}

// Kinds the compiler does not support yet may be declared without being shipped:
// a layer written against a later ontology phase must still resolve today.
func TestCheckProvidesIgnoresUncompiledKinds(t *testing.T) {
	root := writeLayers(t, layerSpec{name: "meta-elo", version: "1.0.0"})

	dir := filepath.Join(root, DefaultLayersDir, "meta-elo")
	manifest := `apiVersion: fab/v1
kind: Layer
metadata:
  name: meta-elo
  version: 1.0.0
spec:
  provides:
    schema:
      aspects: [UserAuthAspect]
      actions: [Login]
`
	if err := os.WriteFile(filepath.Join(dir, layerv1.FileName), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	resolution, err := resolve(t, root, configFor("meta-elo"))
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckProvides(resolution, map[string]Contributions{}); err != nil {
		t.Errorf("CheckProvides() failed: %v", err)
	}
}
