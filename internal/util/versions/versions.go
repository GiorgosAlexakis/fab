// Package versions parses the semantic versions layers carry and the ranges they
// depend on each other through.
//
// Layer compatibility is the whole point of the range syntax: a dependency
// declares the window of a layer it was tested against, and the resolver refuses
// to build outside that window rather than discovering the breakage later.
package versions

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Version is a parsed semantic version.
type Version struct {
	inner *semver.Version
}

// ParseVersion parses an exact semantic version, e.g. "1.2.0".
//
// A leading "v" is rejected: layer versions are data in a YAML file, not Go
// module paths, and accepting both spellings would make "v1.2.0" and "1.2.0"
// two different keys in the lock file.
func ParseVersion(text string) (Version, error) {
	if text == "" {
		return Version{}, fmt.Errorf("a version is required")
	}
	if strings.HasPrefix(text, "v") || strings.HasPrefix(text, "V") {
		return Version{}, fmt.Errorf("%q must not carry a leading %q: write %s",
			text, text[:1], strings.TrimLeft(text, "vV"))
	}

	parsed, err := semver.StrictNewVersion(text)
	if err != nil {
		return Version{}, fmt.Errorf("%q is not a semantic version: %w", text, err)
	}
	return Version{inner: parsed}, nil
}

// CompatibleRange returns the range to depend on a layer at, given the version it
// is at now: from that version up to, but excluding, the next major.
//
// It is what fab writes when it adds a layer for you. The upper bound is the
// point: the layer has been tested against the major version that exists today,
// and a future breaking release must be adopted deliberately.
func CompatibleRange(text string) (string, error) {
	version, err := ParseVersion(text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(">=%s, <%d.0.0", version, version.Major()+1), nil
}

// String returns the canonical text of the version.
func (v Version) String() string {
	if v.inner == nil {
		return ""
	}
	return v.inner.String()
}

// IsZero reports whether the version is unset.
func (v Version) IsZero() bool { return v.inner == nil }

// Compare orders two versions: -1 if v sorts before other, 0 if equal, 1 if after.
func (v Version) Compare(other Version) int {
	return v.inner.Compare(other.inner)
}

// Major returns the major version.
func (v Version) Major() uint64 { return v.inner.Major() }
