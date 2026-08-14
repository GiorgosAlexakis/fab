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

package schema

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	"github.com/GiorgosAlexakis/fab/pkg/printers"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

const getExample = `  # Show what the prod environment is running
  fab schema get --tag prod

  # Fetch a pinned version as YAML
  fab schema get --version 1.3.0 -o yaml`

// GetOptions is the configuration of a `fab schema get` invocation.
type GetOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// Version selects a version directly.
	Version string
	// Tag selects the version an environment tag points at.
	Tag string
	// Output selects the output format.
	Output string
}

// NewGetOptions returns GetOptions with defaults.
func NewGetOptions(streams genericiooptions.IOStreams) *GetOptions {
	return &GetOptions{IOStreams: streams}
}

// NewCmdGet returns the `fab schema get` command.
func NewCmdGet(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewGetOptions(streams)

	cmd := &cobra.Command{
		Use:   "get (--version VERSION | --tag TAG)",
		Short: "Fetch a published ontology from the registry",
		Long: "Fetch a published ontology from the registry by version or by environment tag.\n\n" +
			"The registry stores the ontology as normalized rows, not as a document; this\n" +
			"command rebuilds the snapshot from them and verifies that it still reproduces\n" +
			"the digest recorded at publish time.",
		Example: getExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().StringVar(&o.Version, "version", o.Version, "Version to fetch, e.g. 1.3.0.")
	cmd.Flags().StringVar(&o.Tag, "tag", o.Tag, "Environment tag to resolve, e.g. prod.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output,
		"Output format. One of: json|yaml|digest. Defaults to a summary table.")

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *GetOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}
	return o.registryAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before any work is done.
func (o *GetOptions) Validate() error {
	switch {
	case o.Version == "" && o.Tag == "":
		return errors.New("one of --version or --tag is required")
	case o.Version != "" && o.Tag != "":
		return errors.New("--version and --tag are mutually exclusive")
	}
	return printers.ValidateFormat(o.Output)
}

// Run fetches the ontology and prints it.
func (o *GetOptions) Run(ctx context.Context) error {
	var (
		meta     *registry.Ontology
		compiled *snapshot.Snapshot
		err      error
	)

	if o.Tag != "" {
		meta, err = o.Client.Resolve(ctx, o.Name, o.Tag)
	} else {
		meta, err = o.Client.Get(ctx, o.Name, o.Version)
	}
	if err != nil {
		return err
	}

	compiled, err = o.Client.GetSnapshot(ctx, o.Name, meta.Version)
	if err != nil {
		return err
	}

	switch o.Output {
	case printers.FormatJSON:
		return printers.JSON(o.Out, compiled)
	case printers.FormatYAML:
		return printers.YAML(o.Out, compiled)
	case printers.FormatDigest:
		_, err := fmt.Fprintln(o.Out, meta.Digest)
		return err
	}

	if err := printers.Ontology(o.Out, meta); err != nil {
		return err
	}
	fmt.Fprintln(o.Out)
	return printers.SnapshotSummary(o.Out, compiled)
}
