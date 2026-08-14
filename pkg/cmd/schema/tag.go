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
	"fmt"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

// TagOptions is the configuration of a `fab schema tag` invocation.
type TagOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// Tag is the environment tag to move.
	Tag string
	// Version is the version the tag should point at.
	Version string
	// Output selects the output format.
	Output string
}

// NewTagOptions returns TagOptions with defaults.
func NewTagOptions(streams genericiooptions.IOStreams) *TagOptions {
	return &TagOptions{IOStreams: streams}
}

// NewCmdTag returns the `fab schema tag` command.
func NewCmdTag(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewTagOptions(streams)

	cmd := &cobra.Command{
		Use:   "tag TAG VERSION",
		Short: "Point an environment tag at a version",
		Long: "Point an environment tag such as dev, staging or prod at a published version.\n\n" +
			"Tags are how running services select an ontology: they resolve a tag, not a\n" +
			"version, so promoting is what rolls a change out. A draft cannot be tagged.",
		Example: "  fab schema tag staging 1.3.0",
		Args:    cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run(cmdutil.Context(cmd)))
		},
	}

	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml|digest.")

	return cmd
}

// Complete resolves everything the command needs from arguments and the factory.
func (o *TagOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return cmdutil.UsageErrorf(cmd, "TAG and VERSION are both required")
	}
	o.Tag = args[0]
	o.Version = args[1]
	return o.registryAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before any work is done.
func (o *TagOptions) Validate() error {
	return validateOntologyOutputFormat(o.Output)
}

// Run moves the tag.
func (o *TagOptions) Run(ctx context.Context) error {
	tagged, err := o.Client.Tag(ctx, o.Name, o.Tag, o.Version)
	if err != nil {
		return err
	}

	return printOntologyResult(o.Out, o.Output, tagged,
		fmt.Sprintf("Tag %q now points at %s:%s", o.Tag, tagged.Name, tagged.Version))
}
