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

const listExample = `  # List customers
  fab object list app/Customer

  # List the enterprise ones
  fab object list app/Customer --filter tier=enterprise

  # Count them instead
  fab object list app/Customer --filter tier=enterprise --count

  # Page through them
  fab object list app/Customer --limit 50 --offset 50`

// ListOptions is the configuration of a `fab object list` invocation.
type ListOptions struct {
	genericiooptions.IOStreams
	storeAccess

	// Type is the qualified object type name.
	Type string
	// Filter holds property=value equality filters, ANDed together.
	Filter []string
	// Limit bounds how many objects are returned.
	Limit int
	// Offset skips the first Offset objects.
	Offset int
	// Count reports how many objects match instead of listing them.
	Count bool
	// Output selects the output format.
	Output string
}

// NewListOptions returns ListOptions with defaults.
func NewListOptions(streams genericiooptions.IOStreams) *ListOptions {
	return &ListOptions{IOStreams: streams}
}

// NewCmdList returns the `fab object list` command.
func NewCmdList(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewListOptions(streams)

	cmd := &cobra.Command{
		Use:     "list TYPE",
		Aliases: []string{"ls"},
		Short:   "List the objects of one type",
		Long: "List the objects of one object type, optionally filtered by property value.\n\n" +
			"Filters are equality only and are ANDed together. Filtering on a property the\n" +
			"ontology marks indexed is served by an index; filtering on any other property\n" +
			"works but scans, so declare the ones you query on as indexed.",
		Example: listExample,
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	o.addFlags(cmd.Flags())
	cmd.Flags().StringArrayVar(&o.Filter, "filter", o.Filter,
		"Equality filter, as property=value. Repeat to require several.")
	cmd.Flags().IntVar(&o.Limit, "limit", o.Limit,
		fmt.Sprintf("Maximum number of objects to return. Defaults to %d.", objectstore.DefaultQueryLimit))
	cmd.Flags().IntVar(&o.Offset, "offset", o.Offset, "Number of objects to skip.")
	cmd.Flags().BoolVar(&o.Count, "count", o.Count,
		"Print how many objects match instead of listing them. Ignores --limit and --offset.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a table.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *ListOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return cmdutil.UsageErrorf(cmd, "an object type is required")
	}
	o.Type = args[0]
	return o.storeAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves.
func (o *ListOptions) Validate() error {
	if o.Limit < 0 {
		return fmt.Errorf("--limit cannot be negative")
	}
	if o.Offset < 0 {
		return fmt.Errorf("--offset cannot be negative")
	}
	return validateObjectOutputFormat(o.Output)
}

// Run queries the store and prints the result.
func (o *ListOptions) Run(ctx context.Context) error {
	objectType, err := o.objectType(o.Type)
	if err != nil {
		return err
	}

	filters, err := o.filters(objectType)
	if err != nil {
		return err
	}

	query := objectstore.Query{
		Type:    objectType.QualifiedName,
		Filters: filters,
		Limit:   o.Limit,
		Offset:  o.Offset,
	}

	if o.Count {
		count, err := o.Store.Count(ctx, query)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(o.Out, count)
		return err
	}

	objects, err := o.Store.List(ctx, query)
	if err != nil {
		return err
	}
	return printObjects(o.Out, o.Output, objectType, objects)
}

// filters turns the --filter flags into typed query filters.
func (o *ListOptions) filters(objectType *objectstore.ObjectType) ([]objectstore.Filter, error) {
	values, err := parseAssignments(objectType, o.Filter)
	if err != nil {
		return nil, err
	}

	filters := make([]objectstore.Filter, 0, len(values))
	for _, name := range propertyNames(objectType) {
		value, ok := values[name]
		if !ok {
			continue
		}
		filters = append(filters, objectstore.Filter{Property: name, Value: value})
	}
	return filters, nil
}
