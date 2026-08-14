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

package schema

import (
	"context"
	"fmt"
	"io"

	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// registryAccess is embedded by every schema command that talks to the
// registry. It carries the two things all of them resolve: which ontology, and
// which registry.
type registryAccess struct {
	// Name is the ontology name.
	Name string
	// Client is the connected registry.
	Client registry.Interface
}

// complete resolves the ontology name and opens the registry connection.
func (a *registryAccess) complete(ctx context.Context, f cmdutil.Factory) error {
	name, err := f.OntologyName()
	if err != nil {
		return err
	}
	a.Name = name

	client, err := f.Registry(ctx)
	if err != nil {
		return err
	}
	a.Client = client
	return nil
}

// printOntologyResult reports the outcome of a command that moved something.
// In the default format it prints the one-line confirmation the caller supplies;
// with -o it prints the resulting version instead, for scripts to consume.
func printOntologyResult(out io.Writer, format string, item *registry.Ontology, confirmation string) error {
	switch format {
	case printers.FormatJSON:
		return printers.JSON(out, item)
	case printers.FormatYAML:
		return printers.YAML(out, item)
	case printers.FormatDigest:
		_, err := fmt.Fprintln(out, item.Digest)
		return err
	default:
		_, err := fmt.Fprintln(out, confirmation)
		return err
	}
}

// validateOntologyOutputFormat accepts the formats that make sense for a single
// version's metadata.
func validateOntologyOutputFormat(format string) error {
	switch format {
	case printers.FormatTable, printers.FormatJSON, printers.FormatYAML, printers.FormatDigest:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be one of json|yaml|digest", format)
	}
}
