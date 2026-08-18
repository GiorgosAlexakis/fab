package resolve

import (
	"fmt"
	"sort"
	"strings"

	cmdutil "github.com/GiorgosAlexakis/fab/cmd/util"
	foundryv1 "github.com/GiorgosAlexakis/fab/internal/foundry/v1"
	layerv1 "github.com/GiorgosAlexakis/fab/internal/layer/v1"
	utilerrors "github.com/GiorgosAlexakis/fab/internal/util/errors"
	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

type Resolution struct {
	Ordered []*layerv1.Layer
}

func (r *Resolution) Names() []string {
	result := make([]string, 0, len(r.Ordered))
	for _, layer := range r.Ordered {
		result = append(result, layer.Metadata.Name)
	}
	return result
}

func (r *Resolution) Lookup(name string) (*layerv1.Layer, bool) {
	for _, layer := range r.Ordered {
		if layer.Metadata.Name == name {
			return layer, true
		}
	}
	return nil, false
}

func Resolve(declared *foundryv1.Foundry, discovered []*layerv1.Layer) (*Resolution, error) {
	if _, ok := declared.Selects(cmdutil.FoundationLayer); !ok {
		return nil, fmt.Errorf(
			"%s does not activate %q: it is the foundation layer every other layer is built on, "+
				"so add it to spec.layers", cmdutil.FoundryFileName, cmdutil.FoundationLayer)
	}

	byName := index(discovered)

	active, err := activeLayers(declared, byName, discovered)
	if err != nil {
		return nil, err
	}

	var problems []error
	problems = append(problems, checkSelectors(declared, active)...)
	problems = append(problems, checkDependencies(active)...)
	if len(problems) > 0 {
		return nil, utilerrors.NewAggregate(problems)
	}

	ordered, err := sortTopologically(active)
	if err != nil {
		return nil, err
	}
	return &Resolution{Ordered: ordered}, nil
}

func activeLayers(declared *foundryv1.Foundry, byName map[string]*layerv1.Layer,
	discovered []*layerv1.Layer) (map[string]*layerv1.Layer, error) {
	active := make(map[string]*layerv1.Layer)

	var problems []error
	for _, name := range declared.LayerNames() {
		layer, ok := byName[name]
		if !ok {
			problems = append(problems, fmt.Errorf(
				"%s activates layer %q, which is not in the layers directory (found: %s)",
				cmdutil.FoundryFileName, name, describe(names(discovered))))
			continue
		}
		active[name] = layer
	}
	if len(problems) > 0 {
		return nil, utilerrors.NewAggregate(problems)
	}
	return active, nil
}

func checkSelectors(declared *foundryv1.Foundry, active map[string]*layerv1.Layer) []error {
	var problems []error

	for _, layer := range sorted(active) {
		spec, ok := declared.Selects(layer.Metadata.Name)
		if !ok || spec == "" {
			continue
		}

		allowed, err := versions.ParseRange(spec)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: layer %q: %w",
				cmdutil.FoundryFileName, layer.Metadata.Name, err))
			continue
		}
		version, err := versions.ParseVersion(layer.Metadata.Version)
		if err != nil {
			problems = append(problems, fmt.Errorf("%s: %w", relativeManifest(layer.Metadata.Name), err))
			continue
		}
		if !allowed.Includes(version) {
			problems = append(problems, fmt.Errorf(
				"%s activates layer %q at %q but %s is version %s",
				cmdutil.FoundryFileName, layer.Metadata.Name, spec, relativeManifest(layer.Metadata.Name), layer.Metadata.Version))
		}
	}

	return problems
}

func checkDependencies(active map[string]*layerv1.Layer) []error {
	var problems []error

	for _, layer := range sorted(active) {
		for _, dependency := range layer.Spec.DependsOn {
			target, ok := active[dependency.Name]
			if !ok {
				problems = append(problems, fmt.Errorf(
					"layer %q depends on %q, which is not active: add %q to spec.layers in %s",
					layer.Metadata.Name, dependency.Name, dependency.Name, cmdutil.FoundryFileName))
				continue
			}

			allowed, err := versions.ParseRange(dependency.Version)
			if err != nil {
				problems = append(problems, fmt.Errorf("%s: dependency on %q: %w",
					relativeManifest(layer.Metadata.Name), dependency.Name, err))
				continue
			}
			version, err := versions.ParseVersion(target.Metadata.Version)
			if err != nil {
				problems = append(problems, fmt.Errorf("%s: %w", relativeManifest(target.Metadata.Name), err))
				continue
			}

			if !allowed.Includes(version) {
				problems = append(problems, fmt.Errorf(
					"layer %q was tested against %s %q but %s %s is active",
					layer.Metadata.Name, dependency.Name, dependency.Version,
					dependency.Name, target.Metadata.Version))
			}
		}
	}

	return problems
}

func sortTopologically(active map[string]*layerv1.Layer) ([]*layerv1.Layer, error) {
	remaining := make(map[string][]string, len(active))
	for name, layer := range active {
		dependencies := make([]string, 0, len(layer.Spec.DependsOn))
		for _, dependency := range layer.Spec.DependsOn {
			dependencies = append(dependencies, dependency.Name)
		}
		remaining[name] = dependencies
	}

	ordered := make([]*layerv1.Layer, 0, len(active))
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

func index(discovered []*layerv1.Layer) map[string]*layerv1.Layer {
	byName := make(map[string]*layerv1.Layer, len(discovered))
	for _, layer := range discovered {
		byName[layer.Metadata.Name] = layer
	}
	return byName
}

func names(discovered []*layerv1.Layer) []string {
	result := make([]string, 0, len(discovered))
	for _, layer := range discovered {
		result = append(result, layer.Metadata.Name)
	}
	sort.Strings(result)
	return result
}

func sorted(active map[string]*layerv1.Layer) []*layerv1.Layer {
	result := make([]*layerv1.Layer, 0, len(active))
	for _, layer := range active {
		result = append(result, layer)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Metadata.Name < result[j].Metadata.Name })
	return result
}

func relativeManifest(name string) string {
	return cmdutil.LayersDir + "/" + name + "/" + cmdutil.ManifestFileName
}

func describe(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
