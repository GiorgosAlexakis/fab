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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

const validateExample = `  # Validate the ontology of the foundry in the current directory
  fab schema validate

  # Validate a foundry elsewhere and print the compiled ontology
  fab schema validate --root ../acme-foundry -o yaml

  # Print only the digest, for a CI cache key
  fab schema validate -o digest`

// ValidateOptions is the configuration of a `fab schema validate` invocation.
type ValidateOptions struct {
	genericiooptions.IOStreams

	// Output selects the output format.
	Output string

	loaderOptions loader.Options
	snapshot      *snapshot.Snapshot
}

// NewValidateOptions returns ValidateOptions with defaults.
func NewValidateOptions(streams genericiooptions.IOStreams) *ValidateOptions {
	return &ValidateOptions{IOStreams: streams}
}

// NewCmdValidate returns the `fab schema validate` command.
func NewCmdValidate(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewValidateOptions(streams)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Compile and validate the ontology",
		Long: "Compile every active layer's schema documents into a single ontology and validate it.\n\n" +
			"Cross-layer references are resolved here: a link that points at a type from an inactive\n" +
			"layer fails validation rather than failing at runtime.",
		Example: validateExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		fmt.Sprintf("Output format. One of: %v. Defaults to a summary table.", printers.SupportedFormats()))

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *ValidateOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	loaderOptions, err := f.LoaderOptions()
	if err != nil {
		return err
	}
	o.loaderOptions = loaderOptions
	return nil
}

// Validate checks the options themselves, before any work is done.
func (o *ValidateOptions) Validate() error {
	return printers.ValidateFormat(o.Output)
}

// Run loads, compiles and prints the ontology.
func (o *ValidateOptions) Run() error {
	sources, err := loader.LoadFoundry(o.loaderOptions)
	if err != nil {
		return err
	}

	compiled, err := compiler.Compile(sources)
	if err != nil {
		return err
	}
	o.snapshot = compiled

	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, compiled)
	case printers.FormatYAML:
		return printers.YAML(o.Out, compiled)
	case printers.FormatDigest:
		return printers.SnapshotDigest(o.Out, compiled)
	default:
		return printers.SnapshotSummary(o.Out, compiled)
	}
}

// Snapshot returns the compiled ontology from the last Run. It exists for tests
// and for commands that embed validation as a first step.
func (o *ValidateOptions) Snapshot() *snapshot.Snapshot {
	return o.snapshot
}
