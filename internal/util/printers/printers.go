// Package printers renders command output. Human-readable tables and
// machine-readable encodings live here so that commands contain no formatting
// logic and every command formats the same way.
package printers

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"sigs.k8s.io/yaml"
)

// Supported output formats.
const (
	// FormatTable is the default human-readable output.
	FormatTable = ""
	// FormatJSON prints indented JSON.
	FormatJSON = "json"
	// FormatYAML prints YAML.
	FormatYAML = "yaml"
)

// SupportedFormats lists the output formats accepted by `-o`.
func SupportedFormats() []string {
	return []string{FormatJSON, FormatYAML}
}

// ValidateFormat reports whether format is one of the supported values.
func ValidateFormat(format string) error {
	switch format {
	case FormatTable, FormatJSON, FormatYAML:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be one of %s",
			format, strings.Join(SupportedFormats(), "|"))
	}
}

// JSON writes value as indented JSON.
func JSON(w io.Writer, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// YAML writes value as YAML.
func YAML(w io.Writer, value interface{}) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, string(data))
	return err
}

// ShortDigest abbreviates a digest for table output, keeping the algorithm
// prefix so it is never mistaken for a git ref.
func ShortDigest(digest string) string {
	const keep = 12
	algorithm, hex, found := strings.Cut(digest, ":")
	if !found || len(hex) <= keep {
		return digest
	}
	return algorithm + ":" + hex[:keep]
}

// ShortRef abbreviates a git SHA for table output, leaving anything that is not
// a full SHA -- a tag or a branch name -- untouched.
func ShortRef(ref string) string {
	const shaLength = 40
	const keep = 7
	if len(ref) != shaLength {
		return ref
	}
	return ref[:keep]
}

func orNone(value string) string {
	if value == "" {
		return "<none>"
	}
	return value
}
