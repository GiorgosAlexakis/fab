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
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

// ListOptions is the configuration of a `fab schema list` invocation.
type ListOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// Output selects the output format.
	Output string
}

// NewListOptions returns ListOptions with defaults.
func NewListOptions(streams genericiooptions.IOStreams) *ListOptions {
	return &ListOptions{IOStreams: streams}
}

// NewCmdList returns the `fab schema list` command.
func NewCmdList(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewListOptions(streams)

	cmd := &cobra.Command{
		Use:     "list",
		Short:   "List the published ontology versions",
		Long:    "List every version of this ontology in the registry, newest first, with the tags pointing at each.",
		Example: "  fab schema list",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml.")

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *ListOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}
	return o.registryAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before any work is done.
func (o *ListOptions) Validate() error {
	switch o.Output {
	case printers.FormatTable, printers.FormatJSON, printers.FormatYAML:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be json or yaml", o.Output)
	}
}

// Run lists the versions.
func (o *ListOptions) Run(ctx context.Context) error {
	versions, err := o.Client.List(ctx, o.Name)
	if err != nil {
		return err
	}

	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, versions)
	case printers.FormatYAML:
		return printers.YAML(o.Out, versions)
	}

	if len(versions) == 0 {
		fmt.Fprintf(o.ErrOut, "No versions of ontology %q have been published.\n", o.Name)
		return nil
	}
	return printers.OntologyList(o.Out, versions)
}
