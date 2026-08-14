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

const linkExample = `  # Attach an order to a customer
  fab object link app/CustomerOrders CUST-1 ORD-1`

const unlinkExample = `  # Detach an order from a customer
  fab object unlink app/CustomerOrders CUST-1 ORD-1`

// LinkOptions is the configuration of a `fab object link` or
// `fab object unlink` invocation.
type LinkOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Link is the qualified link type name.
	Link string
	// SourcePrimaryKey identifies the object the link points from.
	SourcePrimaryKey string
	// TargetPrimaryKey identifies the object the link points to.
	TargetPrimaryKey string
	// Unlink disconnects the pair instead of connecting it, which is the whole
	// difference between the two commands sharing these options.
	Unlink bool
}

// NewLinkOptions returns LinkOptions with defaults.
func NewLinkOptions(streams genericiooptions.IOStreams) *LinkOptions {
	return &LinkOptions{IOStreams: streams}
}

// NewCmdLink returns the `fab object link` command.
func NewCmdLink(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewLinkOptions(streams)

	cmd := &cobra.Command{
		Use:   "link LINK_TYPE SOURCE_PRIMARY_KEY TARGET_PRIMARY_KEY",
		Short: "Link two objects",
		Long: "Connect two objects over a link type.\n\n" +
			"The link type names both ends, so only the primary keys are given here. The link\n" +
			"is a row of its own rather than a foreign key column, which is what makes it\n" +
			"traversable from either end and what lets its cardinality be enforced on both.\n" +
			"Linking an already linked pair is a no-op.",
		Example: linkExample,
		Args:    cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())

	return cmd
}

// NewCmdUnlink returns the `fab object unlink` command.
func NewCmdUnlink(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewLinkOptions(streams)
	o.Unlink = true

	cmd := &cobra.Command{
		Use:   "unlink LINK_TYPE SOURCE_PRIMARY_KEY TARGET_PRIMARY_KEY",
		Short: "Unlink two objects",
		Long: "Disconnect two objects over a link type.\n\n" +
			"Only the link is removed; both objects survive. Unlinking a pair that is not\n" +
			"linked is a no-op.",
		Example: unlinkExample,
		Args:    cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *LinkOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 3 {
		return cmdutil.UsageErrorf(cmd, "a link type and the two primary keys are required")
	}
	o.Link, o.SourcePrimaryKey, o.TargetPrimaryKey = args[0], args[1], args[2]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Run links or unlinks the two objects.
func (o *LinkOptions) Run(ctx context.Context) error {
	linkType, err := o.linkType(o.Link)
	if err != nil {
		return err
	}
	sourceType, err := o.Binding.ObjectTypeByID(linkType.SourceTypeID)
	if err != nil {
		return err
	}
	targetType, err := o.Binding.ObjectTypeByID(linkType.TargetTypeID)
	if err != nil {
		return err
	}

	request := objectstore.LinkRequest{
		Link:   linkType.QualifiedName,
		Source: objectstore.Ref{Type: sourceType.QualifiedName, PrimaryKey: o.SourcePrimaryKey},
		Target: objectstore.Ref{Type: targetType.QualifiedName, PrimaryKey: o.TargetPrimaryKey},
	}

	verb := "linked"
	if o.Unlink {
		if err := o.Store.Unlink(ctx, request); err != nil {
			return err
		}
		verb = "unlinked"
	} else if err := o.Store.Link(ctx, request); err != nil {
		return err
	}

	_, err = fmt.Fprintf(o.Out, "%s %s %s over %s\n",
		request.Source, verb, request.Target, linkType.QualifiedName)
	return err
}
