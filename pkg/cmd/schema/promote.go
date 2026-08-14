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

// PromoteOptions is the configuration of a `fab schema promote` invocation.
type PromoteOptions struct {
	genericiooptions.IOStreams
	registryAccess

	// FromTag is the tag whose version is being promoted.
	FromTag string
	// ToTag is the tag being moved.
	ToTag string
	// Output selects the output format.
	Output string
}

// NewPromoteOptions returns PromoteOptions with defaults.
func NewPromoteOptions(streams genericiooptions.IOStreams) *PromoteOptions {
	return &PromoteOptions{IOStreams: streams}
}

// NewCmdPromote returns the `fab schema promote` command.
func NewCmdPromote(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	o := NewPromoteOptions(streams)

	cmd := &cobra.Command{
		Use:   "promote FROM_TAG TO_TAG",
		Short: "Move a tag to whatever another tag points at",
		Long: "Point TO_TAG at the version FROM_TAG currently resolves to.\n\n" +
			"This is the rollout step: the version that has been validated in staging\n" +
			"becomes the version prod resolves, in one atomic swap.",
		Example: "  fab schema promote staging prod",
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
func (o *PromoteOptions) Complete(f cmdutil.Factory, cmd *cobra.Command, args []string) error {
	if len(args) != 2 {
		return cmdutil.UsageErrorf(cmd, "FROM_TAG and TO_TAG are both required")
	}
	o.FromTag = args[0]
	o.ToTag = args[1]
	return o.registryAccess.complete(cmdutil.Context(cmd), f)
}

// Validate checks the options themselves, before any work is done.
func (o *PromoteOptions) Validate() error {
	return validateOntologyOutputFormat(o.Output)
}

// Run performs the promotion.
func (o *PromoteOptions) Run(ctx context.Context) error {
	promoted, err := o.Client.Promote(ctx, o.Name, o.FromTag, o.ToTag)
	if err != nil {
		return err
	}

	return printOntologyResult(o.Out, o.Output, promoted,
		fmt.Sprintf("Promoted %s:%s from %q to %q", promoted.Name, promoted.Version, o.FromTag, o.ToTag))
}
