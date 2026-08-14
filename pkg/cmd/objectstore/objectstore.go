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

// Package objectstore implements the `fab objectstore` command group, which
// operates on the object store database itself rather than on the objects in it.
package objectstore

import (
	"github.com/spf13/cobra"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

// NewCmdObjectStore returns the `fab objectstore` command group.
func NewCmdObjectStore(f cmdutil.Factory, streams genericiooptions.IOStreams) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "objectstore",
		Aliases: []string{"object-store"},
		Short:   "Administer the object store",
		Long: "Administer the object store: the data plane that holds object instances,\n" +
			"their current property values and the links between them.",
		Run: cmdutil.DefaultSubCommandRun(streams.ErrOut),
	}

	cmd.AddCommand(NewCmdMigrate(f, streams))

	return cmd
}
