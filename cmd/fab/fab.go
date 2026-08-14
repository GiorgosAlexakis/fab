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

// Command fab is the FAB command line interface.
package main

import (
	"fmt"
	"os"

	"github.com/GiorgosAlexakis/fab/pkg/cmd"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
)

func main() {
	command := cmd.NewDefaultFabCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(cmdutil.DefaultErrorExitCode)
	}
}
