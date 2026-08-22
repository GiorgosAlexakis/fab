package versions

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// Range is a parsed version range: a set of comma-separated comparators that
// must all hold, e.g. ">=1.0.0, <2.0.0".
type Range struct {
	text  string
	inner *semver.Constraints
}

// ParseRange parses a version range.
func ParseRange(text string) (Range, error) {
	if strings.TrimSpace(text) == "" {
		return Range{}, fmt.Errorf("a version range is required")
	}

	parsed, err := semver.NewConstraint(text)
	if err != nil {
		return Range{}, fmt.Errorf("%q is not a version range: %w", text, err)
	}
	return Range{text: text, inner: parsed}, nil
}

// ValidateRange validates a version range and insists on an upper bound.
//
// Parsing is not enough on its own: ">=1.0.0" is a well-formed range and still
// a promise the author cannot keep, because it claims compatibility with major
// versions that do not exist yet. This is the failure Yocto's LAYERSERIES_COMPAT
// exists to prevent.
func ValidateRange(text string) error {
	parsed, err := ParseRange(text)
	if err != nil {
		return err
	}
	if !parsed.HasUpperBound() {
		return errors.New(`must bound the major version it was tested against, e.g. ">=1.0.0, <2.0.0"`)
	}
	return nil
}

// String returns the range as it was written.
func (r Range) String() string { return r.text }

// IsZero reports whether the range is unset.
func (r Range) IsZero() bool { return r.inner == nil }

// Includes reports whether version satisfies the range.
func (r Range) Includes(version Version) bool {
	return r.inner.Check(version.inner)
}

// HasUpperBound reports whether the range excludes some future version.
//
// It is answered by probing rather than by inspecting comparators: a range is
// bounded above exactly when there is a version it rejects, and a major version
// far beyond anything real is a cheap witness. ">=1.0.0" admits it and so is
// unbounded; ">=1.0.0, <2.0.0" rejects it and so is bounded.
func (r Range) HasUpperBound() bool {
	sentinel, err := semver.StrictNewVersion("100000.0.0")
	if err != nil {
		return false
	}
	return !r.inner.Check(sentinel)
}
