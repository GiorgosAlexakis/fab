// Package initialize implements `fab init`, which creates a foundry.
package initialize

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

const initLong = `Create a foundry.

A foundry is your repository, not a fork of fab. It holds the file that says which
layers your stack is built from, the lock that pins them, and the layers you write
yourself. fab is installed once and used from every foundry.

The new foundry activates the foundation layer and nothing else. Run fab sync next:
it fetches the upstream layer bundle into the local cache and links the activated
layers into place.`

const initExample = `  # Create a foundry in ./acme-corp
  fab init acme-corp

  # Scaffold against a specific version of the foundation layer
  fab init acme-corp --version 0.2.0`

// Options is the configuration of a `fab init` invocation.
type Options struct {
	genericiooptions.IOStreams

	// Version is the exact version of the foundation layer the new foundry is
	// scaffolded against, e.g. "0.2.0".
	Version string

	name string
	root string
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams, Version: foundryv1.DefaultFoundationVersion}
}

// NewCmdInit returns the `fab init` command.
func NewCmdInit(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:     "init NAME",
		Short:   "Create a foundry",
		Long:    initLong,
		Example: initExample,
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().StringVar(&o.Version, "version", o.Version,
		"Version of the foundation layer to scaffold against, e.g. 0.2.0.")

	return cmd
}

// Complete resolves everything the command needs from flags and arguments.
func (o *Options) Complete(cmd *cobra.Command, args []string) error {
	o.name = args[0]

	// The foundry is created under the directory fab is run from, in one named
	// after it.
	root, err := filepath.Abs(o.name)
	if err != nil {
		return err
	}
	o.root = root
	return nil
}

// Validate checks the flags and the target directory before anything is written.
func (o *Options) Validate() error {
	// An exact version, not a range: it is written into the scaffolded manifest
	// as the version that manifest is at. The engine widens it into the range
	// the foundry activates the layer at.
	if _, err := versions.ParseVersion(o.Version); err != nil {
		return fmt.Errorf("--version: %w", err)
	}

	// A foundry is created, never merged into something that is already there:
	// overwriting a foundry.yaml would silently discard a layer selection.
	if _, err := os.Stat(filepath.Join(o.root, cmdutil.FoundryFileName)); err == nil {
		return fmt.Errorf("%s already holds a %s", o.root, cmdutil.FoundryFileName)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("reading %s: %w", o.root, err)
	}
	return nil
}

// Run creates the foundry.
func (o *Options) Run() error {
	created := foundryv1.NewEngine(o.root)
	if err := created.Init(foundryv1.InitOptions{
		Name:              o.name,
		FoundationVersion: o.Version,
	}); err != nil {
		return err
	}

	fmt.Fprintf(o.ErrOut, "Created the %s foundry in %s.\nRun `fab sync` to fetch the upstream layers.\n",
		o.name, o.root)
	return nil
}
