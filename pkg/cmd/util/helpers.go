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

package util

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

// DefaultErrorExitCode is the exit code used when a command fails.
const DefaultErrorExitCode = 1

// fatalErrHandler is indirected so tests can observe fatal errors instead of
// exiting the test binary.
var fatalErrHandler = fatal

// CheckErr prints a user-friendly message for err and exits non-zero. It is the
// single place commands surface failures, so that error formatting is uniform.
func CheckErr(err error) {
	checkErr(err, fatalErrHandler)
}

// checkErr formats err and hands the message to handleErr.
func checkErr(err error, handleErr func(string, int)) {
	if err == nil {
		return
	}

	// A schema tree usually has more than one problem. Report all of them:
	// making the user rerun the command per error is the worst possible loop.
	if aggregate, ok := err.(utilerrors.Aggregate); ok {
		errs := utilerrors.Flatten(aggregate).Errors()
		messages := make([]string, 0, len(errs))
		for _, nested := range errs {
			messages = append(messages, nested.Error())
		}
		if len(messages) == 1 {
			handleErr(messages[0], DefaultErrorExitCode)
			return
		}
		handleErr(fmt.Sprintf("%d problems found:\n  %s", len(messages), strings.Join(messages, "\n  ")),
			DefaultErrorExitCode)
		return
	}

	handleErr(err.Error(), DefaultErrorExitCode)
}

func fatal(message string, code int) {
	if message != "" {
		if !strings.HasSuffix(message, "\n") {
			message += "\n"
		}
		fmt.Fprint(os.Stderr, "error: "+message)
	}
	os.Exit(code)
}

// UsageErrorf returns an error that points the user at the command's help.
func UsageErrorf(cmd *cobra.Command, format string, args ...interface{}) error {
	message := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s\nSee '%s --help' for usage", message, cmd.CommandPath())
}

// DefaultSubCommandRun prints the help of a command that only groups
// subcommands, so that `fab schema` on its own lists what it can do.
func DefaultSubCommandRun(out io.Writer) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		cmd.SetOut(out)
		cmd.SetErr(out)
		CheckErr(cmd.Help())
	}
}

// RequireNoArguments fails when a command that takes no positional arguments is
// given some, rather than ignoring them.
func RequireNoArguments(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return UsageErrorf(cmd, "unknown command %q", strings.Join(args, " "))
	}
	return nil
}
