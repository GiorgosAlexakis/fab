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

package versions

import "testing"

func TestParseVersion(t *testing.T) {
	for _, test := range []struct {
		text    string
		wantErr bool
	}{
		{text: "1.2.0"},
		{text: "0.0.1"},
		{text: "1.2.0-rc.1"},
		{text: "v1.2.0", wantErr: true},
		{text: "1.2", wantErr: true},
		{text: "", wantErr: true},
		{text: "latest", wantErr: true},
	} {
		_, err := ParseVersion(test.text)
		if test.wantErr && err == nil {
			t.Errorf("ParseVersion(%q) succeeded, want an error", test.text)
		}
		if !test.wantErr && err != nil {
			t.Errorf("ParseVersion(%q): %v", test.text, err)
		}
	}
}

func TestRangeIncludes(t *testing.T) {
	for _, test := range []struct {
		spec    string
		version string
		want    bool
	}{
		{spec: ">=1.0.0, <2.0.0", version: "1.0.0", want: true},
		{spec: ">=1.0.0, <2.0.0", version: "1.9.9", want: true},
		{spec: ">=1.0.0, <2.0.0", version: "2.0.0", want: false},
		{spec: ">=1.0.0, <2.0.0", version: "0.9.0", want: false},
		{spec: ">=1.2.0", version: "3.0.0", want: true},
		{spec: "1.2.0", version: "1.2.0", want: true},
	} {
		versionRange, err := ParseRange(test.spec)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", test.spec, err)
		}
		version, err := ParseVersion(test.version)
		if err != nil {
			t.Fatalf("ParseVersion(%q): %v", test.version, err)
		}
		if got := versionRange.Includes(version); got != test.want {
			t.Errorf("ParseRange(%q).Includes(%s) = %v, want %v",
				test.spec, test.version, got, test.want)
		}
	}
}

func TestRangeHasUpperBound(t *testing.T) {
	for _, test := range []struct {
		spec string
		want bool
	}{
		{spec: ">=1.0.0, <2.0.0", want: true},
		{spec: "<2.0.0", want: true},
		{spec: "1.2.0", want: true},
		{spec: "~1.2.0", want: true},
		{spec: "^1.2.0", want: true},
		{spec: ">=1.0.0", want: false},
		{spec: ">1.0.0", want: false},
	} {
		versionRange, err := ParseRange(test.spec)
		if err != nil {
			t.Fatalf("ParseRange(%q): %v", test.spec, err)
		}
		if got := versionRange.HasUpperBound(); got != test.want {
			t.Errorf("ParseRange(%q).HasUpperBound() = %v, want %v", test.spec, got, test.want)
		}
	}
}

func TestParseRangeRejectsGarbage(t *testing.T) {
	for _, spec := range []string{"", "   ", "not a range", ">=", "1.0.0 || garbage"} {
		if _, err := ParseRange(spec); err == nil {
			t.Errorf("ParseRange(%q) succeeded, want an error", spec)
		}
	}
}

func TestVersionCompare(t *testing.T) {
	older, err := ParseVersion("1.2.0")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := ParseVersion("1.10.0")
	if err != nil {
		t.Fatal(err)
	}
	if older.Compare(newer) != -1 {
		t.Errorf("1.2.0 should sort before 1.10.0")
	}
	if newer.Major() != 1 {
		t.Errorf("Major() = %d, want 1", newer.Major())
	}
}
