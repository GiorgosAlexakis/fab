// Package errors joins the several problems a command finds into the one error
// it returns.
//
// Validation reports every field it can rather than stopping at the first, so
// problems arrive as a list while a Go function returns a single error.
// Aggregate carries the list as one error and keeps it readable to whatever
// prints it.
package errors

import (
	stderrors "errors"
	"strings"
)

// Aggregate is an error that stands for several.
type Aggregate interface {
	error

	// Errors returns the problems the aggregate was built from. They are always
	// the leaves: an aggregate never holds another aggregate.
	Errors() []error
}

type aggregate []error

// NewAggregate returns one error standing for all of errs, or nil when there is
// nothing to report.
//
// Nil entries are dropped and nested aggregates are flattened as they go in, so
// that a caller reading Errors() never has to walk a tree. A command collects
// problems from several sources, and the depth they were nested at says nothing
// about the problems themselves.
func NewAggregate(errs []error) Aggregate {
	flattened := flatten(errs, nil)
	if len(flattened) == 0 {
		return nil
	}
	return aggregate(flattened)
}

// flatten appends the leaves of errs to into.
func flatten(errs []error, into []error) []error {
	for _, err := range errs {
		switch typed := err.(type) {
		case nil:
			continue
		case Aggregate:
			into = flatten(typed.Errors(), into)
		default:
			into = append(into, err)
		}
	}
	return into
}

// Errors returns the problems the aggregate was built from.
func (agg aggregate) Errors() []error { return agg }

// Is reports whether any of the problems matches target.
//
// Without it errors.Is would stop at the aggregate, and a caller asking whether
// a run hit a particular sentinel would get no for every run that also hit
// something else. Reporting several problems must not cost the caller the
// ability to recognise one of them.
func (agg aggregate) Is(target error) bool {
	for _, err := range agg {
		if stderrors.Is(err, target) {
			return true
		}
	}
	return false
}

// As finds the first problem that target can be assigned from, for the same
// reason Is exists.
func (agg aggregate) As(target any) bool {
	for _, err := range agg {
		if stderrors.As(err, target) {
			return true
		}
	}
	return false
}

// Error renders a single problem as itself and several as a bracketed list.
//
// Repeated messages are reported once: the same problem often reaches the
// aggregate from more than one source, and the count the user reads should be a
// count of things to fix.
func (agg aggregate) Error() string {
	messages := make([]string, 0, len(agg))
	seen := make(map[string]struct{}, len(agg))
	for _, err := range agg {
		message := err.Error()
		if _, repeated := seen[message]; repeated {
			continue
		}
		seen[message] = struct{}{}
		messages = append(messages, message)
	}

	if len(messages) == 1 {
		return messages[0]
	}
	return "[" + strings.Join(messages, ", ") + "]"
}
