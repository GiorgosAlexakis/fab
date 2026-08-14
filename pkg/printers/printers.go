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

// Package printers renders command output. Human-readable tables and
// machine-readable encodings live here so that commands contain no formatting
// logic and every command formats the same way.
package printers

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"k8s.io/apimachinery/pkg/util/duration"
	"sigs.k8s.io/yaml"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Supported output formats.
const (
	// FormatTable is the default human-readable output.
	FormatTable = ""
	// FormatJSON prints indented JSON.
	FormatJSON = "json"
	// FormatYAML prints YAML.
	FormatYAML = "yaml"
	// FormatDigest prints only the ontology digest, for scripts and CI.
	FormatDigest = "digest"
)

// SupportedFormats lists the output formats accepted by `-o`.
func SupportedFormats() []string {
	return []string{FormatJSON, FormatYAML, FormatDigest}
}

// ValidateFormat reports whether format is one of the supported values.
func ValidateFormat(format string) error {
	switch format {
	case FormatTable, FormatJSON, FormatYAML, FormatDigest:
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

// SnapshotSummary writes a per-layer breakdown of a compiled ontology followed
// by its digest.
func SnapshotSummary(w io.Writer, snap *snapshot.Snapshot) error {
	digest, err := snap.Digest()
	if err != nil {
		return err
	}

	objectTypeCounts := map[string]int{}
	for i := range snap.ObjectTypes {
		objectTypeCounts[snap.ObjectTypes[i].Layer]++
	}
	linkTypeCounts := map[string]int{}
	for i := range snap.LinkTypes {
		linkTypeCounts[snap.LinkTypes[i].Layer]++
	}

	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "LAYER\tOBJECT TYPES\tLINK TYPES")
	for _, layer := range snap.Layers {
		fmt.Fprintf(tw, "%s\t%d\t%d\n", layer, objectTypeCounts[layer], linkTypeCounts[layer])
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(w, "\n%d object types, %d link types across %d layers\n",
		len(snap.ObjectTypes), len(snap.LinkTypes), len(snap.Layers))
	fmt.Fprintf(w, "digest: %s\n", digest)
	return nil
}

// OntologyList writes a table of ontology versions, newest first.
func OntologyList(w io.Writer, ontologies []registry.Ontology) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "VERSION\tSTATUS\tTAGS\tDIGEST\tGIT REF\tLAYERS\tAGE")
	for i := range ontologies {
		item := &ontologies[i]
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%d\t%s\n",
			item.Version,
			item.Status,
			orNone(strings.Join(item.Tags, ",")),
			ShortDigest(item.Digest),
			orNone(ShortRef(item.GitRef)),
			len(item.Layers),
			duration.HumanDuration(time.Since(item.CreatedAt)),
		)
	}
	return tw.Flush()
}

// Ontology writes the metadata of one ontology version.
func Ontology(w io.Writer, item *registry.Ontology) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintf(tw, "NAME:\t%s\n", item.Name)
	fmt.Fprintf(tw, "VERSION:\t%s\n", item.Version)
	fmt.Fprintf(tw, "STATUS:\t%s\n", item.Status)
	fmt.Fprintf(tw, "TAGS:\t%s\n", orNone(strings.Join(item.Tags, ",")))
	fmt.Fprintf(tw, "LAYERS:\t%s\n", strings.Join(item.Layers, ", "))
	fmt.Fprintf(tw, "GIT REF:\t%s\n", orNone(item.GitRef))
	fmt.Fprintf(tw, "DIGEST:\t%s\n", item.Digest)
	fmt.Fprintf(tw, "CREATED:\t%s\n", item.CreatedAt.Format(time.RFC3339))
	if item.PublishedAt != nil {
		fmt.Fprintf(tw, "PUBLISHED:\t%s\n", item.PublishedAt.Format(time.RFC3339))
	}
	return tw.Flush()
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

// SnapshotDigest writes only the digest of a compiled ontology.
func SnapshotDigest(w io.Writer, snap *snapshot.Snapshot) error {
	digest, err := snap.Digest()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, digest)
	return err
}
