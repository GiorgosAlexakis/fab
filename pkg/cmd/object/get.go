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

package object

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

const getExample = `  # Show one customer
  fab object get app/Customer CUST-1

  # Fetch it as JSON
  fab object get app/Customer CUST-1 -o json`

// GetOptions is the configuration of a `fab object get` invocation.
type GetOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Type is the qualified object type name.
	Type string
	// PrimaryKey identifies the object.
	PrimaryKey string
	// Output selects the output format.
	Output string
}

// NewGetOptions returns GetOptions with defaults.
func NewGetOptions(streams genericiooptions.IOStreams) *GetOptions {
	return &GetOptions{IOStreams: streams}
}

// NewCmdGet returns the `fab object get` command.
func NewCmdGet(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewGetOptions(streams)

	cmd := &cobra.Command{
		Use:   "get TYPE PRIMARY_KEY",
		Short: "Show one object",
		Long: "Show one object instance and its current property values.\n\n" +
			"Only the properties the bound ontology version declares are shown. A value written\n" +
			"under a property that a later version dropped stays in the store but is invisible\n" +
			"here, because this version has no name for it.",
		Example: getExample,
		Args:    cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a summary.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *GetOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return cmdutil.UsageErrorf(cmd, "an object type and a primary key are required")
	}
	o.Type, o.PrimaryKey = args[0], args[1]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves.
func (o *GetOptions) Validate() error {
	return validateObjectOutputFormat(o.Output)
}

// Run fetches the object and prints it.
func (o *GetOptions) Run(ctx context.Context) error {
	objectType, err := o.objectType(o.Type)
	if err != nil {
		return err
	}

	stored, err := o.Store.Get(ctx, objectstore.Ref{
		Type:       objectType.QualifiedName,
		PrimaryKey: o.PrimaryKey,
	})
	if err != nil {
		return err
	}
	return printObject(o.Out, o.Output, stored)
}
