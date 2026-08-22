package versions

import "testing"

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
