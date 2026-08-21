// Package cmd assembles the fab command tree.
//
// Every command follows the same shape: a NewCmdXxx constructor takes a Factory
// and IOStreams, and an Options struct implements Complete, Validate and Run.
// A cobra callback never holds the work, so a command can be tested without a process.
package cmd

import (
	"os"

	"github.com/spf13/cobra"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/cmd/version"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

// Command group ids used to organise `fab --help`.
const (
	groupOther = "other"
)

// FabOptions is the configuration of the root command.
type FabOptions struct {
	genericiooptions.IOStreams

	// Arguments are the process arguments, including the program name.
	Arguments []string
}

// NewDefaultFabCommand returns the fab command tree wired to the process
// streams and arguments.
func NewDefaultFabCommand() *cobra.Command {
	return NewFabCommand(FabOptions{
		IOStreams: genericiooptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr},
		Arguments: os.Args,
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

	cmd.AddGroup(
		&cobra.Group{ID: groupOther, Title: "Other Commands:"},
	)

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
