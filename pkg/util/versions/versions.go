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
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// Version is a parsed semantic version.
type Version struct {
	inner *semver.Version
}

// Range is a parsed version range: a set of comma-separated comparators that
// must all hold, e.g. ">=1.0.0, <2.0.0".
type Range struct {
	text  string
	inner *semver.Constraints
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

// ValidateVersion validates an exact version as a field of a document.
func ValidateVersion(text string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if text == "" {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}
	if _, err := ParseVersion(text); err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, text, err.Error()))
	}
	return allErrs
}

// ValidateRange validates a version range as a field of a document, and insists
// on an upper bound.
func ValidateRange(text string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if text == "" {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}

	parsed, err := ParseRange(text)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, text, err.Error()))
		return allErrs
	}
	if !parsed.HasUpperBound() {
		// An open-ended range is a promise the author cannot keep: it claims
		// compatibility with major versions that do not exist yet. This is the
		// failure Yocto's LAYERSERIES_COMPAT exists to prevent.
		allErrs = append(allErrs, field.Invalid(fldPath, text,
			`must bound the major version it was tested against, e.g. ">=1.0.0, <2.0.0"`))
	}
	return allErrs
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
