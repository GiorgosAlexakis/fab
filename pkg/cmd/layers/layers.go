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

// Package layers implements `fab layers`, which shows what a foundry is composed
// of.
package layers

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/layers"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

const layersLong = `List the layers this foundry activates, in build order.

Build order is the useful order: schemas are merged in it, so a layer can only
reference types from the layers above it in the list.`

// Options is the configuration of a `fab layers` invocation.
type Options struct {
	genericiooptions.IOStreams

	// Output selects the output format.
	Output string

	foundry *layers.Foundry
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

// NewCmdLayers returns the `fab layers` command.
func NewCmdLayers(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:     "layers",
		Short:   "List the active layers in build order",
		Long:    layersLong,
		Example: "  fab layers",
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml. Defaults to a table.")

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *Options) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	resolved, err := f.Foundry()
	if err != nil {
		return err
	}
	o.foundry = resolved
	return nil
}

// Validate checks the options themselves, before any work is done.
func (o *Options) Validate() error {
	switch o.Output {
	case printers.FormatTable, printers.FormatJSON, printers.FormatYAML:
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be json or yaml", o.Output)
	}
}

// Run prints the active layers.
func (o *Options) Run() error {
	// The lock shape is the right thing to serialise: it is the same layer set,
	// and a script reading `fab layers -o json` and one reading foundry.lock then
	// see identical fields.
	lock, err := layers.NewLock(o.foundry.Resolution)
	if err != nil {
		return err
	}

	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, lock)
	case printers.FormatYAML:
		return printers.YAML(o.Out, lock)
	}

	if len(o.foundry.Resolution.Ordered) == 0 {
		fmt.Fprintf(o.ErrOut, "This foundry activates no layers; only %s/ is compiled.\n",
			"schema")
		return nil
	}
	return printers.LayerList(o.Out, o.foundry.Resolution)
}
