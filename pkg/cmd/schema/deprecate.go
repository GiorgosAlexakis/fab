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

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

// DeprecateOptions is the configuration of a `fab schema deprecate` invocation.
type DeprecateOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// Version is the version to deprecate.
	Version string
	// Output selects the output format.
	Output string
}

// NewDeprecateOptions returns DeprecateOptions with defaults.
func NewDeprecateOptions(streams genericiooptions.IOStreams) *DeprecateOptions {
	return &DeprecateOptions{IOStreams: streams}
}

// NewCmdDeprecate returns the `fab schema deprecate` command.
func NewCmdDeprecate(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewDeprecateOptions(streams)

	cmd := &cobra.Command{
		Use:   "deprecate VERSION",
		Short: "Mark a published version as no longer recommended",
		Long: "Mark a published version as deprecated.\n\n" +
			"A deprecated version stays readable and tagged services keep working: clients\n" +
			"pinned to it must not break. Deprecation is a signal to stop adopting it.",
		Example: "  fab schema deprecate 1.2.0",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml|digest.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *DeprecateOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "VERSION is required")
	}
	o.Version = args[0]
	return o.registryAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before any work is done.
func (o *DeprecateOptions) Validate() error {
	return validateOntologyOutputFormat(o.Output)
}

// Run deprecates the version.
func (o *DeprecateOptions) Run(ctx context.Context) error {
	deprecated, err := o.Client.Deprecate(ctx, o.Name, o.Version)
	if err != nil {
		return err
	}

	return printOntologyResult(o.Out, o.Output, deprecated,
		fmt.Sprintf("Deprecated %s:%s", deprecated.Name, deprecated.Version))
}
