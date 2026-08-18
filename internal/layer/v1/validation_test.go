package v1

import (
	"strings"
	"testing"
)

func validLayer() *Layer {
	layer := &Layer{
		Metadata: LayerMetadata{Name: "meta-auth", Version: "1.2.0"},
		Spec: LayerSpec{
			DependsOn: []Dependency{
				{Name: "meta-elo", Version: ">=1.0.0, <2.0.0"},
				{Name: "meta-core", Version: ">=1.0.0, <2.0.0"},
			},
		},
	}
	SetDefaults_Layer(layer)
	return layer
}

func TestValidateLayerAcceptsAValidManifest(t *testing.T) {
	if err := validLayer().Validate(); err != nil {
		t.Fatalf("unexpected errors: %v", err)
	}
}

func TestValidateLayerRejectsBadManifests(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Layer)
		wantErr string
	}{
		{
			name:    "no name",
			mutate:  func(l *Layer) { l.Metadata.Name = "" },
			wantErr: "metadata.name",
		},
		{
			name:    "name is not usable as a directory name",
			mutate:  func(l *Layer) { l.Metadata.Name = "Meta_Auth" },
			wantErr: "metadata.name",
		},
		{
			name:    "no version",
			mutate:  func(l *Layer) { l.Metadata.Version = "" },
			wantErr: "metadata.version",
		},
		{
			name:    "version is a range",
			mutate:  func(l *Layer) { l.Metadata.Version = ">=1.0.0" },
			wantErr: "metadata.version",
		},
		{
			name:    "version carries a v prefix",
			mutate:  func(l *Layer) { l.Metadata.Version = "v1.2.0" },
			wantErr: "metadata.version",
		},
		{
			name:    "unknown origin",
			mutate:  func(l *Layer) { l.Metadata.Origin = "vendored" },
			wantErr: "metadata.origin",
		},
		{
			name:    "wrong kind",
			mutate:  func(l *Layer) { l.Kind = "ObjectType" },
			wantErr: "kind",
		},
		{
			name:    "wrong apiVersion",
			mutate:  func(l *Layer) { l.APIVersion = "fab/v2" },
			wantErr: "apiVersion",
		},
		{
			name: "dependency without an upper bound",
			mutate: func(l *Layer) {
				l.Spec.DependsOn[0].Version = ">=1.0.0"
			},
			wantErr: "spec.dependsOn[0].version",
		},
		{
			name: "dependency on itself",
			mutate: func(l *Layer) {
				l.Spec.DependsOn[0].Name = "meta-auth"
			},
			wantErr: "cannot depend on itself",
		},
		{
			name: "duplicate dependency",
			mutate: func(l *Layer) {
				l.Spec.DependsOn[1].Name = "meta-elo"
			},
			wantErr: "spec.dependsOn[1].name",
		},
		{
			name: "dependency without a version",
			mutate: func(l *Layer) {
				l.Spec.DependsOn[0].Version = ""
			},
			wantErr: "spec.dependsOn[0].version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			layer := validLayer()
			test.mutate(layer)

			err := layer.Validate()
			if err == nil {
				t.Fatalf("expected an error mentioning %q", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("errors %v do not mention %q", err, test.wantErr)
			}
		})
	}
}
