// Package resolve implements `fab resolve`, which turns the layers foundry.yaml
// declares into a pinned, reproducible build order.
package resolve

import (
	"fmt"

	"github.com/spf13/cobra"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
	"github.com/GiorgosAlexakis/fab/internal/util/printers"
)

const resolveLong = `Resolve the layers foundry.yaml declares into a build order and pin it in foundry.lock.

Resolution is where every cross-layer promise is checked: that the foundation layer
is activated, that each declared layer exists, that each dependency is active and
inside the version window it was tested against, and that the graph is acyclic.

Commit foundry.lock. It is what makes another environment compose the same layers
in the same order.`

const resolveExample = `  # Resolve the layer graph and write foundry.lock
  fab resolve

  # Validate the graph and the lock without writing anything
  fab resolve --check`

// Options is the configuration of a `fab resolve` invocation.
type Options struct {
	genericiooptions.IOStreams

	// Check validates without writing foundry.lock, and fails when the lock on
	// disk does not match what resolution produced. It is the CI mode: a stale
	// lock is a broken build waiting to happen.
	Check bool
	// Output selects the output format.
	Output string

	foundry *ResolvedFoundry
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

// NewCmdResolve returns the `fab resolve` command.
func NewCmdResolve(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:     "resolve",
		Short:   "Resolve the layer graph and pin it in foundry.lock",
		Long:    resolveLong,
		Example: resolveExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().BoolVar(&o.Check, "check", o.Check,
		"Validate the layer graph and foundry.lock without writing anything.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		fmt.Sprintf("Output format. One of: %v. Defaults to a summary table.", printers.SupportedFormats()))

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *Options) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	root, err := f.FoundryRoot()
	if err != nil {
		return err
	}
	resolved, err := ResolveFoundry(root)
	if err != nil {
		return err
	}
	o.foundry = resolved
	return nil
}

// Validate checks the options themselves, before any work is done.
func (o *Options) Validate() error {
	return printers.ValidateFormat(o.Output)
}

// Run validates the resolved graph and writes or checks the lock.
func (o *Options) Run() error {
	if o.Check {
		if err := CheckLock(o.foundry); err != nil {
			return err
		}
		lock, err := NewLock(o.foundry.Root, o.foundry.Resolution)
		if err != nil {
			return err
		}
		existing, err := cmdutil.LoadLock(o.foundry.Root)
		if err != nil {
			return err
		}
		lock.Bundle = existing.Bundle
		return o.print(lock, fmt.Sprintf("%s is up to date.\n", cmdutil.LockFileName))
	}

	lock, err := WriteLock(o.foundry, nil)
	if err != nil {
		return err
	}
	return o.print(lock, fmt.Sprintf("Wrote %s with %d layers.\n", cmdutil.LockFileName, len(lock.Locked)))
}

func (o *Options) print(lock *foundryv1.Lock, message string) error {
	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, lock)
	case printers.FormatYAML:
		return printers.YAML(o.Out, lock)
	}

	if err := printers.LockSummary(o.Out, lock); err != nil {
		return err
	}
	fmt.Fprint(o.ErrOut, message)
	return nil
}
