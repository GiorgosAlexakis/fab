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

// Package version implements `fab version`.
package version

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/GiorgosAlexakis/fab/pkg/cli/genericiooptions"
	cmdutil "github.com/GiorgosAlexakis/fab/pkg/cmd/util"
	"github.com/GiorgosAlexakis/fab/pkg/version"
)

// Options is the configuration of a `fab version` invocation.
type Options struct {
	genericiooptions.IOStreams

	// Short prints just the version string.
	Short bool
	// Output selects a machine-readable form: json or yaml.
	Output string
}

// NewOptions returns Options with defaults.
func NewOptions(streams genericiooptions.IOStreams) *Options {
	return &Options{IOStreams: streams}
}

// NewCmdVersion returns the `fab version` command.
func NewCmdVersion(streams genericiooptions.IOStreams) *cobra.Command {
	o := NewOptions(streams)

	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print the fab version",
		Long:  "Print the version, commit and build metadata of this fab binary.",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Validate())
			cmdutil.CheckErr(o.Run())
		},
	}

	cmd.Flags().BoolVar(&o.Short, "short", o.Short, "Print just the version string.")
	cmd.Flags().StringVarP(&o.Output, "output", "o", o.Output, "Output format. One of: json|yaml.")

	return cmd
}

// Validate checks the flag combination.
func (o *Options) Validate() error {
	switch o.Output {
	case "", "json", "yaml":
		return nil
	default:
		return fmt.Errorf("invalid output format %q: must be json or yaml", o.Output)
	}
}

// Run prints the version information.
func (o *Options) Run() error {
	info := version.Get()

	switch o.Output {
	case "json":
		data, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(o.Out, string(data))
	case "yaml":
		data, err := yaml.Marshal(info)
		if err != nil {
			return err
		}
		fmt.Fprint(o.Out, string(data))
	default:
		if o.Short {
			fmt.Fprintln(o.Out, info.GitVersion)
			return nil
		}
		fmt.Fprintf(o.Out, "fab version: %s (commit %s, %s, built %s, %s %s)\n",
			info.GitVersion, info.GitCommit, info.GitTreeState, info.BuildDate, info.GoVersion, info.Platform)
	}

	return nil
}
