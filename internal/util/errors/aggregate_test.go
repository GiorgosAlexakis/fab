package errors

import (
	stderrors "errors"
	"testing"
)

var errSentinel = stderrors.New("sentinel")

func TestNewAggregateReturnsNilWhenThereIsNothingToReport(t *testing.T) {
	for _, test := range []struct {
		name string
		errs []error
	}{
		{name: "no errors", errs: nil},
		{name: "empty", errs: []error{}},
		{name: "only nils", errs: []error{nil, nil}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := NewAggregate(test.errs); got != nil {
				t.Errorf("NewAggregate() = %v, want nil", got)
			}
		})
	}
}

func TestNewAggregateFlattens(t *testing.T) {
	inner := NewAggregate([]error{stderrors.New("second"), stderrors.New("third")})
	agg := NewAggregate([]error{stderrors.New("first"), nil, inner})

	errs := agg.Errors()
	if len(errs) != 3 {
		t.Fatalf("Errors() returned %d errors, want the 3 leaves: %v", len(errs), errs)
	}
	for _, err := range errs {
		if _, nested := err.(Aggregate); nested {
			t.Errorf("Errors() returned a nested aggregate: %v", err)
		}
	}
}

func TestErrorRendersOneProblemAsItself(t *testing.T) {
	agg := NewAggregate([]error{stderrors.New("only one problem")})

	if got, want := agg.Error(), "only one problem"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrorListsSeveralProblems(t *testing.T) {
	agg := NewAggregate([]error{stderrors.New("first"), stderrors.New("second")})

	if got, want := agg.Error(), "[first, second]"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// The same problem often reaches the aggregate from more than one source.
func TestErrorReportsARepeatedMessageOnce(t *testing.T) {
	agg := NewAggregate([]error{stderrors.New("same"), stderrors.New("same")})

	if got, want := agg.Error(), "same"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// A caller must still be able to recognise a sentinel in a run that found more
// than one problem.
func TestIsSeesThroughTheAggregate(t *testing.T) {
	agg := NewAggregate([]error{stderrors.New("unrelated"), errSentinel})

	if !stderrors.Is(agg, errSentinel) {
		t.Errorf("errors.Is() did not find the sentinel in %v", agg)
	}
	if stderrors.Is(agg, stderrors.New("absent")) {
		t.Error("errors.Is() matched an error the aggregate does not hold")
	}
}

func TestIsSeesThroughAWrappedSentinel(t *testing.T) {
	agg := NewAggregate([]error{stderrors.New("unrelated"), wrap(errSentinel)})

	if !stderrors.Is(agg, errSentinel) {
		t.Errorf("errors.Is() did not unwrap through the aggregate: %v", agg)
	}
}

func wrap(err error) error { return &wrapped{err} }

type wrapped struct{ inner error }

func (w *wrapped) Error() string { return "wrapped: " + w.inner.Error() }
func (w *wrapped) Unwrap() error { return w.inner }
