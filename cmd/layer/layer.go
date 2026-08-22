// Package layer implements `fab layer`, the commands that change which layers a
// foundry activates.
package layer

import (
	"github.com/spf13/cobra"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	"github.com/GiorgosAlexakis/fab/internal/util/genericiooptions"
)

// NewCmdLayer returns the `fab layer` command group.
func NewCmdLayer(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "layer",
		Short: "Change which layers this foundry activates",
		Long: "Change which layers this foundry activates.\n\n" +
			"These commands edit foundry.yaml for you, so that activating a layer does not\n" +
			"mean remembering the version range to write.",
		Run: cmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmd.AddCommand(NewCmdLayerAdd(f, streams))

	return cmd
}
