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
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

const createExample = `  # Create a customer
  fab object create app/Customer --set id=CUST-1 --set email=ada@example.com --set tier=pro

  # Values of every ontology type are given as text and parsed against the schema
  fab object create app/Order --set id=ORD-1 --set total=99.50 --set placed_at=2024-03-01T10:00:00Z`

const applyExample = `  # Update one property, leaving the rest of the object alone
  fab object apply app/Customer --set id=CUST-1 --set tier=enterprise

  # Clear a nullable property
  fab object apply app/Customer --set id=CUST-1 --remove phone`

// PutOptions is the configuration of a `fab object create` or
// `fab object apply` invocation.
type PutOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Type is the qualified object type name.
	Type string
	// Set holds property=value assignments.
	Set []string
	// Remove names properties whose values should be cleared.
	Remove []string
	// CreateOnly fails when the object already exists, which is the difference
	// between create and apply.
	CreateOnly bool
	// Output selects the output format.
	Output string
}

// NewPutOptions returns PutOptions with defaults.
func NewPutOptions(streams genericiooptions.IOStreams) *PutOptions {
	return &PutOptions{IOStreams: streams}
}

// NewCmdCreate returns the `fab object create` command.
func NewCmdCreate(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewPutOptions(streams)
	o.CreateOnly = true

	cmd := &cobra.Command{
		Use:   "create TYPE --set property=value",
		Short: "Create an object",
		Long: "Create one object instance of an object type in the bound ontology.\n\n" +
			"Every non-nullable property, including the primary key, must be set: a partially\n" +
			"populated object is not a valid instance of its type. Creating an object whose\n" +
			"primary key is taken fails; use `fab object apply` to update one.",
		Example: createExample,
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())
	cmd.Flags().StringArrayVar(&o.Set, "set", o.Set,
		"Property value, as property=value. Repeat for each property.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a one-line confirmation.")

	return cmd
}

// NewCmdApply returns the `fab object apply` command.
func NewCmdApply(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewPutOptions(streams)

	cmd := &cobra.Command{
		Use:   "apply TYPE --set property=value",
		Short: "Create or update an object",
		Long: "Create an object, or update one that already exists.\n\n" +
			"The write merges: the properties named are written and every other property is\n" +
			"left as it was, so two writers touching different properties of the same object\n" +
			"do not overwrite each other. Clearing a value is therefore explicit, via --remove.",
		Example: applyExample,
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())
	cmd.Flags().StringArrayVar(&o.Set, "set", o.Set,
		"Property value, as property=value. Repeat for each property.")
	cmd.Flags().StringArrayVar(&o.Remove, "remove", o.Remove,
		"Property whose value should be cleared. Repeat for each property.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a one-line confirmation.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *PutOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "an object type is required")
	}
	o.Type = args[0]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before anything is written.
func (o *PutOptions) Validate() error {
	if len(o.Set) == 0 {
		return errors.New("at least one --set property=value is required")
	}
	return validateObjectOutputFormat(o.Output)
}

// Run writes the object and reports it.
func (o *PutOptions) Run(ctx context.Context) error {
	objectType, err := o.objectType(o.Type)
	if err != nil {
		return err
	}

	values, err := parseAssignments(objectType, o.Set)
	if err != nil {
		return err
	}

	stored, err := o.Store.Put(ctx, objectstore.PutRequest{
		Type:       objectType.QualifiedName,
		Set:        values,
		Remove:     o.Remove,
		CreateOnly: o.CreateOnly,
	})
	if err != nil {
		return err
	}

	if o.Output == printers.FormatTable {
		_, err := fmt.Fprintf(o.Out, "%s %s\n", stored.Ref(), o.verb())
		return err
	}
	return printObject(o.Out, o.Output, stored)
}

// verb is what the confirmation line reports, so that create and apply say what
// they did rather than what they are.
func (o *PutOptions) verb() string {
	if o.CreateOnly {
		return "created"
	}
	return "applied"
}
