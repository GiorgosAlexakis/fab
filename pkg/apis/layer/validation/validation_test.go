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

package validation

import (
	"strings"
	"testing"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
)

func validLayer() *layerv1.Layer {
	layer := &layerv1.Layer{
		Metadata: layerv1.LayerMeta{Name: "meta-auth", Version: "1.2.0"},
		Spec: layerv1.LayerSpec{
			DependsOn: []layerv1.Dependency{
				{Name: "meta-elo", Version: ">=1.0.0, <2.0.0"},
				{Name: "meta-core", Version: ">=1.0.0, <2.0.0"},
			},
			Provides: layerv1.Provides{
				Schema: &layerv1.SchemaProvides{Objects: []string{"Session", "Permission"}},
			},
		},
	}
	layerv1.SetDefaults_Layer(layer)
	return layer
}

func TestValidateLayerAcceptsAValidManifest(t *testing.T) {
	if errs := ValidateLayer(validLayer()); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateLayerRejectsBadManifests(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*layerv1.Layer)
		wantErr string
	}{
		{
			name:    "no name",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Name = "" },
			wantErr: "metadata.name",
		},
		{
			name:    "name is not a DNS label",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Name = "Meta_Auth" },
			wantErr: "metadata.name",
		},
		{
			name:    "no version",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Version = "" },
			wantErr: "metadata.version",
		},
		{
			name:    "version is a range",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Version = ">=1.0.0" },
			wantErr: "metadata.version",
		},
		{
			name:    "version carries a v prefix",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Version = "v1.2.0" },
			wantErr: "metadata.version",
		},
		{
			name:    "unknown origin",
			mutate:  func(l *layerv1.Layer) { l.Metadata.Origin = "vendored" },
			wantErr: "metadata.origin",
		},
		{
			name:    "wrong kind",
			mutate:  func(l *layerv1.Layer) { l.Kind = "ObjectType" },
			wantErr: "kind",
		},
		{
			name:    "wrong apiVersion",
			mutate:  func(l *layerv1.Layer) { l.APIVersion = "fab/v2" },
			wantErr: "apiVersion",
		},
		{
			name: "dependency without an upper bound",
			mutate: func(l *layerv1.Layer) {
				l.Spec.DependsOn[0].Version = ">=1.0.0"
			},
			wantErr: "spec.dependsOn[0].version",
		},
		{
			name: "dependency on itself",
			mutate: func(l *layerv1.Layer) {
				l.Spec.DependsOn[0].Name = "meta-auth"
			},
			wantErr: "cannot depend on itself",
		},
		{
			name: "duplicate dependency",
			mutate: func(l *layerv1.Layer) {
				l.Spec.DependsOn[1].Name = "meta-elo"
			},
			wantErr: "spec.dependsOn[1].name",
		},
		{
			name: "dependency without a version",
			mutate: func(l *layerv1.Layer) {
				l.Spec.DependsOn[0].Version = ""
			},
			wantErr: "spec.dependsOn[0].version",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			layer := validLayer()
			test.mutate(layer)

			errs := ValidateLayer(layer)
			if len(errs) == 0 {
				t.Fatalf("expected an error mentioning %q", test.wantErr)
			}
			if !strings.Contains(errs.ToAggregate().Error(), test.wantErr) {
				t.Errorf("errors %v do not mention %q", errs, test.wantErr)
			}
		})
	}
}

func TestSetDefaultsLayer(t *testing.T) {
	layer := &layerv1.Layer{Metadata: layerv1.LayerMeta{Name: "meta-marine", Version: "0.1.0"}}
	layerv1.SetDefaults_Layer(layer)

	if layer.APIVersion != "fab/v1" {
		t.Errorf("APIVersion = %q, want fab/v1", layer.APIVersion)
	}
	if layer.Kind != layerv1.LayerKind {
		t.Errorf("Kind = %q, want %s", layer.Kind, layerv1.LayerKind)
	}
	if layer.Metadata.Origin != layerv1.OriginLocal {
		t.Errorf("Origin = %q, want local", layer.Metadata.Origin)
	}
}
