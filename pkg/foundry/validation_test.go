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

package foundry

import (
	"strings"
	"testing"
)

func validConfig() *Config {
	config := &Config{
		Metadata: Metadata{Name: "acme-corp"},
		Spec: Spec{
			Layers: []LayerSelector{
				{Name: "meta-elo", Version: ">=1.0.0, <2.0.0"},
				{Name: "meta-core", Version: ">=1.0.0, <2.0.0"},
			},
		},
	}
	config.SetDefaults()
	return config
}

func TestValidateAcceptsAValidConfig(t *testing.T) {
	if errs := Validate(validConfig()); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	for _, test := range []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{
			name:    "no name",
			mutate:  func(c *Config) { c.Metadata.Name = "" },
			wantErr: "metadata.name",
		},
		{
			name:    "name is not a DNS subdomain",
			mutate:  func(c *Config) { c.Metadata.Name = "Acme Corp" },
			wantErr: "metadata.name",
		},
		{
			name:    "wrong kind",
			mutate:  func(c *Config) { c.Kind = "Layer" },
			wantErr: "kind",
		},
		{
			name:    "wrong apiVersion",
			mutate:  func(c *Config) { c.APIVersion = "fab/v2" },
			wantErr: "apiVersion",
		},
		{
			name:    "layer without a name",
			mutate:  func(c *Config) { c.Spec.Layers[0].Name = "" },
			wantErr: "spec.layers[0].name",
		},
		{
			name:    "layer range without an upper bound",
			mutate:  func(c *Config) { c.Spec.Layers[0].Version = ">=1.0.0" },
			wantErr: "spec.layers[0].version",
		},
		{
			name:    "layer range that is not a range",
			mutate:  func(c *Config) { c.Spec.Layers[0].Version = "latest" },
			wantErr: "spec.layers[0].version",
		},
		{
			name:    "duplicate layer",
			mutate:  func(c *Config) { c.Spec.Layers[1].Name = "meta-elo" },
			wantErr: "spec.layers[1].name",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			config := validConfig()
			test.mutate(config)

			errs := Validate(config)
			if len(errs) == 0 {
				t.Fatalf("expected an error mentioning %q", test.wantErr)
			}
			if !strings.Contains(errs.ToAggregate().Error(), test.wantErr) {
				t.Errorf("errors %v do not mention %q", errs, test.wantErr)
			}
		})
	}
}

// A layer selector without a version is legal: the official layers are released
// as a set, so pinning each of them individually is optional.
func TestValidateAllowsLayersWithoutAVersionRange(t *testing.T) {
	config := validConfig()
	config.Spec.Layers[0].Version = ""

	if errs := Validate(config); len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
}

func TestSetDefaults(t *testing.T) {
	config := &Config{Metadata: Metadata{Name: "acme-corp"}}
	config.SetDefaults()

	if config.APIVersion != APIVersion || config.Kind != Kind {
		t.Errorf("apiVersion/kind = %q/%q, want %q/%q", config.APIVersion, config.Kind, APIVersion, Kind)
	}
}

func TestSelects(t *testing.T) {
	config := validConfig()

	if spec, ok := config.Selects("meta-core"); !ok || spec != ">=1.0.0, <2.0.0" {
		t.Errorf("Selects(meta-core) = %q, %v", spec, ok)
	}
	if _, ok := config.Selects("meta-absent"); ok {
		t.Error("Selects(meta-absent) should not find anything")
	}
	if got, want := len(config.LayerNames()), 2; got != want {
		t.Errorf("LayerNames() returned %d names, want %d", got, want)
	}
}
