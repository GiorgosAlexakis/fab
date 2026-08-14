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

// Package object implements the `fab object` command group: the data plane
// verbs, which read and write object instances through an ontology.
package object

import (
	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

// NewCmdObject returns the `fab object` command group.
func NewCmdObject(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "object",
		Short: "Read and write object instances",
		Long: "Read and write the object instances in the object store.\n\n" +
			"Every command in this group binds to one published ontology version, selected by\n" +
			"--ontology-tag or --ontology-version, and validates what it writes against it.\n" +
			"Types, properties and links are the ones that version declares; nothing else can\n" +
			"be written.",
		Run: cmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmd.AddCommand(
		NewCmdCreate(f, streams),
		NewCmdApply(f, streams),
		NewCmdGet(f, streams),
		NewCmdList(f, streams),
		NewCmdDelete(f, streams),
		NewCmdLink(f, streams),
		NewCmdUnlink(f, streams),
		NewCmdTraverse(f, streams),
	)

	return cmd
}
