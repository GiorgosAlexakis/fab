package v1

import (
	"strings"
	"testing"
)

func TestValidateAcceptsAValidFoundry(t *testing.T) {
	if err := validFoundry(t).Validate(); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
}

func TestValidateRejectsBadFoundries(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Foundry)
		wantErr string
	}{
		{
			name:    "no name",
			mutate:  func(f *Foundry) { f.Metadata.Name = "" },
			wantErr: "metadata.name",
		},
		{
			name:    "name is not usable as a directory name",
			mutate:  func(f *Foundry) { f.Metadata.Name = "Acme Corp" },
			wantErr: "metadata.name",
		},
		{
			name:    "name carries a dot",
			mutate:  func(f *Foundry) { f.Metadata.Name = "acme.corp" },
			wantErr: "metadata.name",
		},
		{
			name:    "wrong kind",
			mutate:  func(f *Foundry) { f.Kind = "Layer" },
			wantErr: "kind",
		},
		{
			name:    "wrong apiVersion",
			mutate:  func(f *Foundry) { f.APIVersion = "fab/v2" },
			wantErr: "apiVersion",
		},
		{
			name:    "layer without a name",
			mutate:  func(f *Foundry) { f.Spec.Layers[0].Name = "" },
			wantErr: "spec.layers[0].name",
		},
		{
			name:    "layer range without an upper bound",
			mutate:  func(f *Foundry) { f.Spec.Layers[0].Version = ">=1.0.0" },
			wantErr: "spec.layers[0].version",
		},
		{
			name:    "layer range that is not a range",
			mutate:  func(f *Foundry) { f.Spec.Layers[0].Version = "latest" },
			wantErr: "spec.layers[0].version",
		},
		{
			name:    "duplicate layer",
			mutate:  func(f *Foundry) { f.Spec.Layers[1].Name = "meta-elo" },
			wantErr: "spec.layers[1].name",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			f := validFoundry(t)
			test.mutate(f)

			err := f.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("errors %v do not mention %q", err, test.wantErr)
			}
		})
	}
}

// A layer selector without a version is legal: the official layers are released
// as a set, so pinning each of them individually is optional.
func TestValidateAllowsLayersWithoutAVersionRange(t *testing.T) {
	f := validFoundry(t)
	f.Spec.Layers[0].Version = ""

	if err := f.Validate(); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
}

func TestNewIsValidOnceNamed(t *testing.T) {
	f := NewFoundry("acme-corp")

	if f.APIVersion != APIVersion || f.Kind != FoundryKind {
		t.Errorf("apiVersion/kind = %q/%q, want %q/%q", f.APIVersion, f.Kind, APIVersion, FoundryKind)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("a new foundry does not validate: %v", err)
	}
}
