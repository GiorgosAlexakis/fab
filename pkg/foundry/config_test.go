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

package foundry

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ConfigFileName), []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", ConfigFileName, err)
	}
	return root
}

func TestLoad(t *testing.T) {
	root := writeConfig(t, `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0"
    - name: meta-marine
      version: "~2.3.0"
`)

	config, err := Load(root)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if config.Metadata.Name != "acme-corp" {
		t.Errorf("metadata.name = %q, want acme-corp", config.Metadata.Name)
	}
	if len(config.Spec.Layers) != 2 {
		t.Fatalf("got %d layers, want 2", len(config.Spec.Layers))
	}
	if config.Spec.Layers[1].Name != "meta-marine" || config.Spec.Layers[1].Version != "~2.3.0" {
		t.Errorf("second layer = %+v", config.Spec.Layers[1])
	}
}

// TestLoadIgnoresUnknownKeys guards the compatibility promise: foundry.yaml will
// grow keys, and an older fab binary must keep working rather than refuse to
// read the file.
func TestLoadIgnoresUnknownKeys(t *testing.T) {
	root := writeConfig(t, `apiVersion: fab/v1
kind: Foundry
metadata:
  name: acme-corp
spec:
  bsp: aws-eks
  adapters:
    payments: stripe
  layers:
    - name: meta-core
      version: ">=1.0.0"
      overrides:
        - schema/objects/user.yaml
`)

	config, err := Load(root)
	if err != nil {
		t.Fatalf("Load() failed on a file with newer keys: %v", err)
	}
	if config.Metadata.Name != "acme-corp" {
		t.Errorf("metadata.name = %q, want acme-corp", config.Metadata.Name)
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load() error = %v, want ErrNotFound", err)
	}
}

func TestOntologyName(t *testing.T) {
	root := writeConfig(t, "metadata:\n  name: acme-corp\n")

	name, err := OntologyName(root)
	if err != nil {
		t.Fatalf("OntologyName() failed: %v", err)
	}
	if name != "acme-corp" {
		t.Errorf("OntologyName() = %q, want acme-corp", name)
	}
}

// TestOntologyNameWithoutFoundry keeps `fab schema validate` usable in a bare
// directory: a missing foundry.yaml is not an error, just an absent name.
func TestOntologyNameWithoutFoundry(t *testing.T) {
	name, err := OntologyName(t.TempDir())
	if err != nil {
		t.Fatalf("OntologyName() failed: %v", err)
	}
	if name != "" {
		t.Errorf("OntologyName() = %q, want an empty name", name)
	}
}
