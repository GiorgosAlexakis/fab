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
