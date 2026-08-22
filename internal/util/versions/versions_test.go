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

func TestCompatibleRange(t *testing.T) {
	for _, test := range []struct {
		version string
		want    string
	}{
		{version: "1.2.0", want: ">=1.2.0, <2.0.0"},
		{version: "0.1.0", want: ">=0.1.0, <1.0.0"},
		{version: "2.0.0", want: ">=2.0.0, <3.0.0"},
	} {
		got, err := CompatibleRange(test.version)
		if err != nil {
			t.Errorf("CompatibleRange(%q): %v", test.version, err)
			continue
		}
		if got != test.want {
			t.Errorf("CompatibleRange(%q) = %q, want %q", test.version, got, test.want)
		}
	}

	// A range has no single version to widen from, so it is not a version.
	for _, text := range []string{">=1.0.0, <2.0.0", "~1.2.0", "v1.2.0", ""} {
		if _, err := CompatibleRange(text); err == nil {
			t.Errorf("CompatibleRange(%q) succeeded, want an error", text)
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
