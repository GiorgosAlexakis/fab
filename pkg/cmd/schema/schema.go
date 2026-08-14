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

// Package schema implements the `fab schema` command group, which turns schema
// documents into a compiled ontology.
package schema

import (
	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

// NewCmdSchema returns the `fab schema` command group.
func NewCmdSchema(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Work with the ontology schema",
		Long: "Compile, validate and inspect the ontology assembled from the active layers.\n\n" +
			"The schema documents under schema/ and layers/*/schema/ are the source of truth.\n" +
			"Everything else -- generated clients, migrations, registry snapshots -- is derived from them.",
		Run: cmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmd.AddCommand(NewCmdValidate(f, streams))

	return cmd
}
