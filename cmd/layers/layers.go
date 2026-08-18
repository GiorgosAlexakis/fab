package layers

import (
	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/cmd/resolve"
	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
	"github.com/GiorgosAlexakis/fab/internal/util/printers"
)

const layersLong = `List the layers this foundry activates, in build order.

Build order is the useful order: a layer is only built after everything it depends
on, so it can rely on what came before it and nothing else.

A layer that is in the layers directory but not activated in foundry.yaml is not
listed. foundry.yaml is the single place that decides what the stack is made of.`

type Options struct {
	genericiooptions.IOStreams

	Output string

	foundry *resolve.ResolvedFoundry
}

func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

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

func (o *Options) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	root, err := f.FoundryRoot()
	if err != nil {
		return err
	}
	resolved, err := resolve.ResolveFoundry(root)
	if err != nil {
		return err
	}
	o.foundry = resolved
	return nil
}

func (o *Options) Validate() error {
	return printers.ValidateFormat(o.Output)
}

func (o *Options) Run() error {
	lock, err := resolve.NewLock(o.foundry.Root, o.foundry.Resolution)
	if err != nil {
		return err
	}

	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, lock)
	case printers.FormatYAML:
		return printers.YAML(o.Out, lock)
	}
	return printers.LayerList(o.Out, o.foundry.Resolution.Ordered)
}
