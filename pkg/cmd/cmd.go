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

// Package cmd assembles the fab command tree.
//
// Every command follows the same shape: a NewCmdXxx constructor takes a Factory
// and IOStreams, and an Options struct implements Complete, Validate and Run.
// Business logic lives in pkg/, never in a cobra callback.
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericclioptions"
	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	layerscmd "github.com/GiorgosAlexakis/fab/pkg/cmd/layers"
	"github.com/GiorgosAlexakis/fab/pkg/cmd/resolve"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/cmd/version"
)

// Command group ids used to organise `fab --help`.
const (
	groupComposition = "composition"
	groupOther       = "other"
)

// FabOptions is the configuration of the root command.
type FabOptions struct {
	genericiooptions.IOStreams

	// Arguments are the process arguments, including the program name.
	Arguments []string
	// ConfigFlags are the persistent flags shared by all commands.
	ConfigFlags *genericclioptions.ConfigFlags
}

// NewDefaultFabCommand returns the fab command tree wired to the process
// streams and arguments.
func NewDefaultFabCommand() *cobra.Command {
	return NewFabCommand(FabOptions{
		IOStreams:   genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
		Arguments:   os.Args,
		ConfigFlags: genericclioptions.NewConfigFlags(),
	})
}

// NewFabCommand returns the root command of the fab CLI.
func NewFabCommand(o FabOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fab",
		Short: "Compose, build and operate a company stack",
		Long: "fab composes domain layers into a deployable company stack.\n\n" +
			"The ontology is the centre of that stack: schema documents describe the business\n" +
			"domain, and fab compiles them into a versioned ontology that services, generated\n" +
			"clients and the object store all bind against.",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run:           cmdutil.DefaultSubCommandRun(o.ErrOut),
	}

	if o.ConfigFlags == nil {
		o.ConfigFlags = genericclioptions.NewConfigFlags()
	}
	o.ConfigFlags.AddFlags(cmd.PersistentFlags())

	factory := cmdutil.NewFactory(o.ConfigFlags)

	cmd.AddGroup(
		&cobra.Group{ID: groupComposition, Title: "Composition Commands:"},
		&cobra.Group{ID: groupOther, Title: "Other Commands:"},
	)

	resolveCmd := resolve.NewCmdResolve(factory, o.IOStreams)
	resolveCmd.GroupID = groupComposition
	cmd.AddCommand(resolveCmd)

	layersCmd := layerscmd.NewCmdLayers(factory, o.IOStreams)
	layersCmd.GroupID = groupComposition
	cmd.AddCommand(layersCmd)

	versionCmd := version.NewCmdVersion(o.IOStreams)
	versionCmd.GroupID = groupOther
	cmd.AddCommand(versionCmd)

	cmd.SetArgs(commandArguments(o.Arguments))
	cmd.SetIn(o.In)
	cmd.SetOut(o.Out)
	cmd.SetErr(o.ErrOut)

	return cmd
}

// commandArguments strips the program name from the process arguments.
func commandArguments(arguments []string) []string {
	if len(arguments) <= 1 {
		return []string{}
	}
	return arguments[1:]
}
