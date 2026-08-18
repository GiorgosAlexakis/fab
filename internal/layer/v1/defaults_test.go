package v1

import "testing"

func TestSetDefaultsLayer(t *testing.T) {
	layer := &Layer{Metadata: LayerMetadata{Name: "meta-marine", Version: "0.1.0"}}
	SetDefaults_Layer(layer)

	if layer.APIVersion != "fab/v1" {
		t.Errorf("APIVersion = %q, want fab/v1", layer.APIVersion)
	}
	if layer.Kind != LayerKind {
		t.Errorf("Kind = %q, want %s", layer.Kind, LayerKind)
	}
	if layer.Metadata.Origin != OriginLocal {
		t.Errorf("Origin = %q, want local", layer.Metadata.Origin)
	}
}
