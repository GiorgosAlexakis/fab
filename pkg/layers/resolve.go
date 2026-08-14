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

package layers

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/errors"

	"github.com/GiorgosAlexakis/fab/pkg/foundry"
	"github.com/GiorgosAlexakis/fab/pkg/util/versions"
)

// Resolution is the outcome of resolving a foundry's layers.
type Resolution struct {
	// Ordered is the active layers in topological order: every layer appears
	// after each layer it depends on. Schemas are merged in this order, so a
	// layer can always reference the types of the ones before it.
	Ordered []Layer
}

// Names returns the resolved layer names in build order.
func (r *Resolution) Names() []string {
	result := make([]string, 0, len(r.Ordered))
	for _, layer := range r.Ordered {
		result = append(result, layer.Name())
	}
	return result
}

// Lookup returns a resolved layer by name.
func (r *Resolution) Lookup(name string) (Layer, bool) {
	for _, layer := range r.Ordered {
		if layer.Name() == name {
			return layer, true
		}
	}
	return Layer{}, false
}

// Resolve turns the layers foundry.yaml declares into a build order.
//
// Every check that needs more than one manifest happens here: that a declared
// layer exists, that each dependency is active, that each dependency's version
// falls inside the window it was tested against, and that the graph is acyclic.
// All of it is reported at once, because fixing one missing layer only to be
// told about the next is a bad way to learn what a foundry needs.
func Resolve(config *foundry.Config, discovered []Layer) (*Resolution, error) {
	byName := index(discovered)

	active, err := activeLayers(config, byName, discovered)
	if err != nil {
		return nil, err
	}

	var problems []error
	problems = append(problems, checkSelectors(config, active)...)
	problems = append(problems, checkDependencies(active)...)
	if len(problems) > 0 {
		return nil, errors.NewAggregate(problems)
	}

	ordered, err := sortTopologically(active)
	if err != nil {
		return nil, err
	}
	return &Resolution{Ordered: ordered}, nil
}

// activeLayers returns the declared layers, keyed by name.
func activeLayers(config *foundry.Config, byName map[string]Layer,
	discovered []Layer) (map[string]Layer, error) {
	active := make(map[string]Layer)

	var problems []error
	for _, name := range config.LayerNames() {
		layer, ok := byName[name]
		if !ok {
			problems = append(problems, fmt.Errorf(
				"foundry.yaml activates layer %q, which is not in the layers directory (found: %s)",
				name, describe(names(discovered))))
			continue
		}
		active[name] = layer
	}
	if len(problems) > 0 {
		return nil, errors.NewAggregate(problems)
	}
	return active, nil
}

// checkSelectors reports layers whose version falls outside the range
// foundry.yaml pinned them to.
func checkSelectors(config *foundry.Config, active map[string]Layer) []error {
	var problems []error

	for _, layer := range sorted(active) {
		spec, ok := config.Selects(layer.Name())
		if !ok || spec == "" {
			continue
		}

		allowed, err := versions.ParseRange(spec)
		if err != nil {
			problems = append(problems, fmt.Errorf("foundry.yaml: layer %q: %w", layer.Name(), err))
			continue
		}
		version, err := versions.ParseVersion(layer.Version())
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", layer.Path, err))
			continue
		}
		if !allowed.Includes(version) {
			problems = append(problems, fmt.Errorf(
				"foundry.yaml activates layer %q at %q but %s is version %s",
				layer.Name(), spec, layer.Path, layer.Version()))
		}
	}

	return problems
}

// checkDependencies reports dependencies that are missing from the active set or
// whose version is outside the declared compatibility window.
func checkDependencies(active map[string]Layer) []error {
	var problems []error

	for _, layer := range sorted(active) {
		for _, dependency := range layer.Manifest.Spec.DependsOn {
			target, ok := active[dependency.Name]
			if !ok {
				problems = append(problems, fmt.Errorf(
					"layer %q depends on %q, which is not active: add %q to spec.layers in foundry.yaml",
					layer.Name(), dependency.Name, dependency.Name))
				continue
			}

			allowed, err := versions.ParseRange(dependency.Version)
			if err != nil {
				problems = append(problems, fmt.Errorf("%s: dependency on %q: %w",
					layer.Path, dependency.Name, err))
				continue
			}
			version, err := versions.ParseVersion(target.Version())
			if err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", target.Path, err))
				continue
			}

			if !allowed.Includes(version) {
				// This is the check that earns the upper bound in a range: the
				// layer was tested against a window, and something outside it is
				// active.
				problems = append(problems, fmt.Errorf(
					"layer %q was tested against %s %q but %s %s is active",
					layer.Name(), dependency.Name, dependency.Version,
					dependency.Name, target.Version()))
			}
		}
	}

	return problems
}

// sortTopologically returns the active layers in dependency order, breaking ties
// by name so the order is reproducible.
func sortTopologically(active map[string]Layer) ([]Layer, error) {
	remaining := make(map[string][]string, len(active))
	for name, layer := range active {
		dependencies := make([]string, 0, len(layer.Manifest.Spec.DependsOn))
		for _, dependency := range layer.Manifest.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}
		remaining[name] = dependencies
	}

	ordered := make([]Layer, 0, len(active))
	placed := make(map[string]bool, len(active))

	for len(placed) < len(active) {
		ready := make([]string, 0, len(remaining))
		for name, dependencies := range remaining {
			if placed[name] {
				continue
			}
			if allPlaced(dependencies, placed) {
				ready = append(ready, name)
			}
		}
		if len(ready) == 0 {
			return nil, cycleError(remaining, placed)
		}

		// Sorting the ready set is what makes the build order a function of the
		// manifests alone, rather than of Go's map iteration order.
		sort.Strings(ready)
		for _, name := range ready {
			ordered = append(ordered, active[name])
			placed[name] = true
		}
	}

	return ordered, nil
}

func allPlaced(dependencies []string, placed map[string]bool) bool {
	for _, dependency := range dependencies {
		if !placed[dependency] {
			return false
		}
	}
	return true
}

// cycleError names the layers that could not be ordered. They are exactly the
// ones on or behind a cycle.
func cycleError(remaining map[string][]string, placed map[string]bool) error {
	var stuck []string
	for name := range remaining {
		if !placed[name] {
			stuck = append(stuck, name)
		}
	}
	sort.Strings(stuck)
	return fmt.Errorf("the layer dependency graph has a cycle involving %s", describe(stuck))
}

// sorted returns the active layers in name order, so that every error list this
// package produces is deterministic.
func sorted(active map[string]Layer) []Layer {
	result := make([]Layer, 0, len(active))
	for _, layer := range active {
		result = append(result, layer)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name() < result[j].Name() })
	return result
}

func describe(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
