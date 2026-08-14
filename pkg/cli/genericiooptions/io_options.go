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

// Package genericiooptions provides the IO streams every command writes
// through, so that no command reaches for os.Stdout directly and every command
// is testable with buffers.
package genericiooptions

import (
	"bytes"
	"io"
)

// IOStreams is a set of input and output streams handed to a command.
type IOStreams struct {
	// In is the reader a command reads input from.
	In io.Reader
	// Out is the writer a command writes results to.
	Out io.Writer
	// ErrOut is the writer a command writes warnings and errors to.
	ErrOut io.Writer
}

// NewTestIOStreams returns IOStreams backed by buffers, for use in tests.
func NewTestIOStreams() (IOStreams, *bytes.Buffer, *bytes.Buffer, *bytes.Buffer) {
	in := &bytes.Buffer{}
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}

	return IOStreams{In: in, Out: out, ErrOut: errOut}, in, out, errOut
}
