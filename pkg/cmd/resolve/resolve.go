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

// Package resolve implements `fab resolve`, which turns the layers foundry.yaml
// declares into a pinned, reproducible build order.
package resolve

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/layers"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
)

const resolveLong = `Resolve the layers foundry.yaml declares into a build order and pin it in foundry.lock.

Resolution is where every cross-layer promise is checked: that each declared layer
exists, that each dependency is active and inside the version window it was tested
against, that the graph is acyclic, that each layer ships the schema its manifest
declares, and that every cross-layer type reference resolves.

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

	foundry       *layers.Foundry
	loaderOptions loader.Options
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

	resolved, err := f.Foundry()
	if err != nil {
		return err
	}
	o.foundry = resolved

	loaderOptions, err := f.LoaderOptions()
	if err != nil {
		return err
	}
	o.loaderOptions = loaderOptions
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

// Run validates the resolved graph and writes or checks the lock.
func (o *Options) Run() error {
	if err := o.checkSchema(); err != nil {
		return err
	}

	lock, err := layers.NewLock(o.foundry.Resolution)
	if err != nil {
		return err
	}

	existing, err := layers.LoadLock(o.foundry.Root)
	switch {
	case err == nil:
		// Resolution reads manifests, not the bundle the upstream ones came out
		// of, so it has nothing to say about the pin. Rewriting the lock without
		// carrying it forward would drop it.
		lock.Bundle = existing.Bundle
	case errors.Is(err, layers.ErrNoLock):
		existing = nil
	default:
		return err
	}

	if o.Check {
		return o.compareWithLock(existing, lock)
	}
	if err := layers.SaveLock(o.foundry.Root, lock); err != nil {
		return err
	}
	return o.print(lock, fmt.Sprintf("Wrote %s with %d layers.\n", layers.LockFileName, len(lock.Locked)))
}

// checkSchema compiles the ontology so that resolution fails on a broken
// cross-layer reference, and confirms each layer ships what it declares.
//
// A foundry with no schema at all is still resolvable: the layer graph is
// meaningful before any type exists.
func (o *Options) checkSchema() error {
	sources, err := loader.LoadFoundry(o.loaderOptions)
	if err != nil {
		if strings.Contains(err.Error(), "no schema documents found") {
			return nil
		}
		return err
	}

	compiled, err := compiler.Compile(sources)
	if err != nil {
		return err
	}

	contributions := map[string]layers.Contributions{}
	for i := range compiled.ObjectTypes {
		objectType := &compiled.ObjectTypes[i]
		entry := contributions[objectType.Layer]
		entry.Objects = append(entry.Objects, objectType.Name)
		contributions[objectType.Layer] = entry
	}
	for i := range compiled.LinkTypes {
		linkType := &compiled.LinkTypes[i]
		entry := contributions[linkType.Layer]
		entry.Links = append(entry.Links, linkType.Name)
		contributions[linkType.Layer] = entry
	}

	return layers.CheckProvides(o.foundry.Resolution, contributions)
}

// compareWithLock reports a lock that does not match the resolved graph.
func (o *Options) compareWithLock(existing, fresh *layers.Lock) error {
	if existing == nil {
		return fmt.Errorf("%s does not exist: run `fab resolve` and commit it", layers.LockFileName)
	}

	changes, err := existing.Diff(o.foundry.Resolution)
	if err != nil {
		return err
	}
	if len(changes) > 0 {
		return fmt.Errorf("%s is out of date; run `fab resolve`:\n  %s",
			layers.LockFileName, strings.Join(changes, "\n  "))
	}

	return o.print(fresh, fmt.Sprintf("%s is up to date.\n", layers.LockFileName))
}

func (o *Options) print(lock *layers.Lock, message string) error {
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
