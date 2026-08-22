package layer

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

const addLong = `Activate a layer in foundry.yaml.

Adding a layer that is in the layers directory pins it to a range fab derives from
the version it is at now: from that version up to the next major. A layer the bundle
has but this foundry has never fetched can be activated by passing the range
yourself; run fab sync afterwards to fetch it.

foundry.yaml is read, changed and written back through the same type fab validates,
so the file is rewritten rather than patched in place: comments and keys this fab
does not know about are not carried over.`

const addExample = `  # Activate a layer, pinning it to the major version it is at now
  fab layer add meta-core

  # Activate a layer at a range you choose
  fab layer add meta-core --version ">=1.2.0, <2.0.0"`

type AddOptions struct {
	genericiooptions.IOStreams

	Version string

	name       string
	root       string
	layer      *layerv1.Layer
	discovered []*layerv1.Layer
}

func NewAddOptions(streams genericiooptions.IOStreams) *AddOptions {
	return &AddOptions{IOStreams: streams}
}

func NewCmdLayerAdd(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewAddOptions(streams)

	cmd := &cobra.Command{
		Use:     "add NAME",
		Short:   "Activate a layer in foundry.yaml",
		Long:    addLong,
		Example: addExample,
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.Version, "version", o.Version,
		`Version range to declare the layer at, e.g. ">=1.0.0, <2.0.0". Defaults to the layer's current major.`)

	return cmd
}

func (o *AddOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	o.name = args[0]

	root, err := f.FoundryRoot()
	if err != nil {
		return err
	}
	o.root = root

	discovered, err := f.Layers()
	if err != nil {
		return err
	}
	o.discovered = discovered

	for _, layer := range discovered {
		if layer.Metadata.Name == o.name {
			o.layer = layer
			break
		}
	}
	return nil
}

func (o *AddOptions) Validate() error {
	if o.Version != "" {
		if err := versions.ValidateRange(o.Version); err != nil {
			return fmt.Errorf("--version: %w", err)
		}
	}

	if o.layer == nil && o.Version == "" {
		return fmt.Errorf(
			"no layer %q in %s/ (found: %s): run `fab sync` to fetch it, "+
				"or pass --version to activate a layer the bundle provides",
			o.name, cmdutil.LayersDir, describe(o.discovered))
	}
	return nil
}

func (o *AddOptions) Run() error {
	declared := o.Version
	if declared == "" {
		derived, err := versions.CompatibleRange(o.layer.Metadata.Version)
		if err != nil {
			return fmt.Errorf("%s: %w", cmdutil.ManifestPath(o.root, o.name), err)
		}
		declared = derived
	}

	foundry := foundryv1.NewEngine(o.root)

	f, err := foundry.Load()
	if err != nil {
		if errors.Is(err, foundryv1.ErrFoundryNotFound) {
			return fmt.Errorf("%w: run `fab init` first, or pass --root", err)
		}
		return err
	}
	if err := f.AddLayer(o.name, declared); err != nil {
		return err
	}
	if err := foundry.Save(f); err != nil {
		return err
	}

	fmt.Fprintf(o.ErrOut, "Activated %s in %s at %q.\n", o.name, cmdutil.FoundryFileName, declared)
	if o.layer == nil {
		fmt.Fprintf(o.ErrOut, "Run `fab sync` to fetch it, then `fab resolve`.\n")
		return nil
	}
	fmt.Fprintf(o.ErrOut, "Run `fab resolve` to pin it in %s.\n", cmdutil.LockFileName)
	return nil
}

func describe(discovered []*layerv1.Layer) string {
	if len(discovered) == 0 {
		return "none"
	}

	names := make([]string, 0, len(discovered))
	for _, layer := range discovered {
		names = append(names, layer.Metadata.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
