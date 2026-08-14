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

package printers

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/GiorgosAlexakis/fab/pkg/layers"
)

// LayerList writes the active layers in build order.
//
// Build order rather than alphabetical order is the point of the table: it is
// what decides which layer can reference which types.
func LayerList(w io.Writer, resolution *layers.Resolution) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "LAYER\tVERSION\tORIGIN\tDEPENDS ON")
	for _, layer := range resolution.Ordered {
		dependencies := make([]string, 0, len(layer.Manifest.Spec.DependsOn))
		for _, dependency := range layer.Manifest.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}

		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			layer.Name(),
			layer.Version(),
			layer.Origin(),
			orNone(strings.Join(dependencies, ",")),
		)
	}
	return tw.Flush()
}

// LockSummary writes the pinned layer set in build order.
func LockSummary(w io.Writer, lock *layers.Lock) error {
	tw := tabwriter.NewWriter(w, 0, 8, 3, ' ', 0)
	fmt.Fprintln(tw, "LAYER\tVERSION\tORIGIN\tDIGEST")
	for _, entry := range lock.Locked {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
			entry.Name, entry.Version, entry.Origin, ShortDigest(entry.Digest))
	}
	return tw.Flush()
}
