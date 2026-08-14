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

const traverseExample = `  # The orders of a customer, following the link forwards
  fab object traverse app/Customer CUST-1 customer_orders

  # The customer of an order, following the same link backwards
  fab object traverse app/Order ORD-1 customer`

// TraverseOptions is the configuration of a `fab object traverse` invocation.
type TraverseOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Type is the qualified object type name to start from.
	Type string
	// PrimaryKey identifies the object to start from.
	PrimaryKey string
	// Traversal is the traversal name on that type.
	Traversal string
	// Limit bounds how many objects are returned.
	Limit int
	// Offset skips the first Offset objects.
	Offset int
	// Output selects the output format.
	Output string
}

// NewTraverseOptions returns TraverseOptions with defaults.
func NewTraverseOptions(streams genericiooptions.IOStreams) *TraverseOptions {
	return &TraverseOptions{IOStreams: streams}
}

// NewCmdTraverse returns the `fab object traverse` command.
func NewCmdTraverse(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewTraverseOptions(streams)

	cmd := &cobra.Command{
		Use:   "traverse TYPE PRIMARY_KEY TRAVERSAL",
		Short: "Follow a link from one object",
		Long: "Return the objects reachable from one object along a named traversal.\n\n" +
			"A traversal is one direction of a link type: the link's forwardName when the\n" +
			"object is its source, or its reverseName when the object is its target. Both are\n" +
			"served by the same link rows, so a relationship is queryable from either end\n" +
			"without a second table or a reverse index maintained by hand.",
		Example: traverseExample,
		Args:    cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())
	cmd.Flags().IntVar(&o.Limit, "limit", o.Limit,
		fmt.Sprintf("Maximum number of objects to return. Defaults to %d.", objectstore.DefaultQueryLimit))
	cmd.Flags().IntVar(&o.Offset, "offset", o.Offset, "Number of objects to skip.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a table.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *TraverseOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return cmdutil.UsageErrorf(cmd, "an object type, a primary key and a traversal name are required")
	}
	o.Type, o.PrimaryKey, o.Traversal = args[0], args[1], args[2]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves.
func (o *TraverseOptions) Validate() error {
	if o.Limit < 0 {
		return fmt.Errorf("--limit cannot be negative")
	}
	if o.Offset < 0 {
		return fmt.Errorf("--offset cannot be negative")
	}
	return validateObjectOutputFormat(o.Output)
}

// Run follows the traversal and prints what it reaches.
func (o *TraverseOptions) Run(ctx context.Context) error {
	objectType, err := o.objectType(o.Type)
	if err != nil {
		return err
	}
	traversal, err := o.Binding.Traversal(objectType.QualifiedName, o.Traversal)
	if err != nil {
		return err
	}
	reached, err := o.Binding.ObjectTypeByID(traversal.ToTypeID)
	if err != nil {
		return err
	}

	objects, err := o.Store.Traverse(ctx, objectstore.TraverseRequest{
		From:      objectstore.Ref{Type: objectType.QualifiedName, PrimaryKey: o.PrimaryKey},
		Traversal: o.Traversal,
		Limit:     o.Limit,
		Offset:    o.Offset,
	})
	if err != nil {
		return err
	}
	return printObjects(o.Out, o.Output, reached, objects)
}
