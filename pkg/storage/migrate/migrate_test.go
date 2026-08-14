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

package migrate

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestLoad(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0002_add_tags.sql":        &fstest.MapFile{Data: []byte("CREATE TABLE tags ();")},
		"migrations/0001_create_registry.sql": &fstest.MapFile{Data: []byte("CREATE TABLE ontologies ();")},
		"migrations/README.md":                &fstest.MapFile{Data: []byte("not a migration")},
	}

	migrations, err := Load(fsys, "migrations")
	if err == nil {
		t.Fatal("Load() accepted a non-migration file in the migrations directory")
	}
	if !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("Load() error = %v, want it to name the offending file", err)
	}

	delete(fsys, "migrations/README.md")

	migrations, err = Load(fsys, "migrations")
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(migrations) != 2 {
		t.Fatalf("got %d migrations, want 2", len(migrations))
	}
	if migrations[0].Version != 1 || migrations[1].Version != 2 {
		t.Errorf("migrations are not ordered by version: %d then %d",
			migrations[0].Version, migrations[1].Version)
	}
	if migrations[0].Name != "create_registry" {
		t.Errorf("first migration name = %q, want create_registry", migrations[0].Name)
	}
	if !strings.HasPrefix(migrations[0].Checksum, "sha256:") {
		t.Errorf("checksum = %q, want a sha256 prefix", migrations[0].Checksum)
	}
	if migrations[0].Checksum == migrations[1].Checksum {
		t.Error("migrations with different bodies have the same checksum")
	}
}

func TestLoadRejectsDuplicateVersions(t *testing.T) {
	fsys := fstest.MapFS{
		"migrations/0001_create_registry.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		"migrations/0001_also_first.sql":      &fstest.MapFile{Data: []byte("SELECT 2;")},
	}

	_, err := Load(fsys, "migrations")
	if err == nil {
		t.Fatal("Load() accepted two migrations with the same version")
	}
	if !strings.Contains(err.Error(), "share version 1") {
		t.Errorf("Load() error = %v, want it to report the shared version", err)
	}
}

func TestLoadMissingDirectory(t *testing.T) {
	if _, err := Load(fstest.MapFS{}, "migrations"); err == nil {
		t.Fatal("Load() succeeded on a missing migrations directory")
	}
}
