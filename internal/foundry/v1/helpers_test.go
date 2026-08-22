package v1

import (
	"errors"
	"testing"
)

func validFoundry(t *testing.T) *Foundry {
	t.Helper()

	f := NewFoundry("acme-corp")
	for _, name := range []string{"meta-elo", "meta-core"} {
		if err := f.AddLayer(name, ">=1.0.0, <2.0.0"); err != nil {
			t.Fatalf("AddLayer(%s) failed: %v", name, err)
		}
	}
	return f
}

func TestAddLayerRejectsALayerThatIsAlreadyDeclared(t *testing.T) {
	f := validFoundry(t)

	err := f.AddLayer("meta-elo", ">=1.0.0, <2.0.0")
	if !errors.Is(err, ErrLayerAlreadyDeclared) {
		t.Fatalf("AddLayer() error = %v, want ErrLayerAlreadyDeclared", err)
	}
	if got := len(f.Spec.Layers); got != 2 {
		t.Errorf("the rejected layer was added anyway: %d layers, want 2", got)
	}
}

func TestSelects(t *testing.T) {
	f := validFoundry(t)

	if spec, ok := f.Selects("meta-core"); !ok || spec != ">=1.0.0, <2.0.0" {
		t.Errorf("Selects(meta-core) = %q, %v", spec, ok)
	}
	if _, ok := f.Selects("meta-absent"); ok {
		t.Error("Selects(meta-absent) should not find anything")
	}
	if got, want := len(f.LayerNames()), 2; got != want {
		t.Errorf("LayerNames() returned %d names, want %d", got, want)
	}
}
