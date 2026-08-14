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
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/loader"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/util/gitutil"
)

const publishExample = `  # Publish the current schema as version 1.3.0
  fab schema publish --version 1.3.0

  # Publish a mutable draft while iterating
  fab schema publish --version 1.4.0-rc1 --draft

  # Record an explicit commit instead of the current HEAD
  fab schema publish --version 1.3.0 --git-ref 179a9f6`

// PublishOptions is the configuration of a `fab schema publish` invocation.
type PublishOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// Version is the version to publish.
	Version string
	// GitRef records the commit the snapshot was compiled from. It defaults to
	// the HEAD of the foundry's git work tree.
	GitRef string
	// Draft publishes a mutable draft rather than an immutable version.
	Draft bool
	// Output selects the output format.
	Output string

	foundryRoot   string
	loaderOptions loader.Options
}

// NewPublishOptions returns PublishOptions with defaults.
func NewPublishOptions(streams genericiooptions.IOStreams) *PublishOptions {
	return &PublishOptions{IOStreams: streams}
}

// NewCmdPublish returns the `fab schema publish` command.
func NewCmdPublish(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewPublishOptions(streams)

	cmd := &cobra.Command{
		Use:   "publish --version VERSION",
		Short: "Compile the schema and publish it to the registry",
		Long: "Compile every active layer's schema documents and store the result in the\n" +
			"registry as an immutable version.\n\n" +
			"Publishing is idempotent for identical content, so re-running a release\n" +
			"pipeline is safe. Publishing different content under a version that already\n" +
			"exists is refused: other environments and generated clients are pinned to it.\n" +
			"Use --draft while iterating; a draft may be replaced in place and cannot be\n" +
			"tagged.",
		Example: publishExample,
		Args:    cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().StringVar(&o.Version, "version", o.Version, "Version to publish, e.g. 1.3.0.")
	cmd.Flags().StringVar(&o.GitRef, "git-ref", o.GitRef,
		"Commit to record as the provenance of this version. Defaults to the foundry's git HEAD.")
	cmd.Flags().BoolVar(&o.Draft, "draft", o.Draft, "Publish as a mutable draft.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml|digest.")

	return cmd
}

// Complete resolves everything the command needs from flags and the factory.
func (o *PublishOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if err := cmdutil.RequireNoArguments(cmd, args); err != nil {
		return err
	}

	root, err := f.FoundryRoot()
	if err != nil {
		return err
	}
	o.foundryRoot = root

	loaderOptions, err := f.LoaderOptions()
	if err != nil {
		return err
	}
	o.loaderOptions = loaderOptions

	if err := o.registryAccess.complete(cmdutil.Context(cmd), f); err != nil {
		return err
	}

	if o.GitRef == "" {
		o.GitRef = gitutil.HeadRef(cmdutil.Context(cmd), root)
	}
	return nil
}

// Validate checks the options themselves, before any work is done.
func (o *PublishOptions) Validate() error {
	if o.Version == "" {
		return errors.New("--version is required")
	}
	return validateOntologyOutputFormat(o.Output)
}

// Run compiles the schema and publishes it.
func (o *PublishOptions) Run(ctx context.Context) error {
	sources, err := loader.LoadFoundry(o.loaderOptions)
	if err != nil {
		return err
	}

	compiled, err := compiler.Compile(sources)
	if err != nil {
		return err
	}

	published, err := o.Client.Publish(ctx, registry.PublishRequest{
		Name:     o.Name,
		Version:  o.Version,
		Snapshot: compiled,
		GitRef:   o.GitRef,
		Draft:    o.Draft,
	})
	if err != nil {
		return err
	}

	return printOntologyResult(o.Out, o.Output, published,
		fmt.Sprintf("Published %s:%s (%s)", published.Name, published.Version, published.Status))
}
