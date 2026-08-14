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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

const deleteExample = `  # Delete a customer
  fab object delete app/Customer CUST-1`

// DeleteOptions is the configuration of a `fab object delete` invocation.
type DeleteOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Type is the qualified object type name.
	Type string
	// PrimaryKey identifies the object.
	PrimaryKey string
}

// NewDeleteOptions returns DeleteOptions with defaults.
func NewDeleteOptions(streams genericiooptions.IOStreams) *DeleteOptions {
	return &DeleteOptions{IOStreams: streams}
}

// NewCmdDelete returns the `fab object delete` command.
func NewCmdDelete(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewDeleteOptions(streams)

	cmd := &cobra.Command{
		Use:   "delete TYPE PRIMARY_KEY",
		Short: "Delete an object",
		Long: "Delete one object together with its property values.\n\n" +
			"What happens to the objects it links to is the delete policy each link type\n" +
			"declares: restrict, the default, refuses while a link exists; cascade deletes the\n" +
			"objects on the far end; detach removes the links and leaves them.",
		Example: deleteExample,
		Args:    cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *DeleteOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return cmdutil.UsageErrorf(cmd, "an object type and a primary key are required")
	}
	o.Type, o.PrimaryKey = args[0], args[1]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Run deletes the object.
func (o *DeleteOptions) Run(ctx context.Context) error {
	objectType, err := o.objectType(o.Type)
	if err != nil {
		return err
	}

	ref := objectstore.Ref{Type: objectType.QualifiedName, PrimaryKey: o.PrimaryKey}
	if err := o.Store.Delete(ctx, ref); err != nil {
		return err
	}

	_, err = fmt.Fprintf(o.Out, "%s deleted\n", ref)
	return err
}
