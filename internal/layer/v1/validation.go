package v1

import (
	"fmt"

	utilerrors "github.com/GiorgosAlexakis/fab/internal/util/errors"
	"github.com/GiorgosAlexakis/fab/internal/util/validation"
	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

// maxDescriptionLength bounds documentation strings.
const maxDescriptionLength = 4096

// Validate checks a defaulted layer manifest.
//
// It reports every problem it finds rather than the first. A manifest someone is
// editing usually has more than one, and making them rerun the command per
// problem is the worst possible loop.
//
// Each message opens with the path of the field it belongs to, e.g.
// spec.dependsOn[1].name, so that a report of several problems still says where
// each one is.
func (layer *Layer) Validate() error {
	var problems []error

	if layer.APIVersion != APIVersion {
		problems = append(problems,
			fmt.Errorf("apiVersion: must be %q, got %q", APIVersion, layer.APIVersion))
	}
	if layer.Kind != LayerKind {
		problems = append(problems,
			fmt.Errorf("kind: must be %q, got %q", LayerKind, layer.Kind))
	}

	for _, problem := range validation.NameProblems(layer.Metadata.Name) {
		problems = append(problems, fmt.Errorf("metadata.name: %s", problem))
	}
	if _, err := versions.ParseVersion(layer.Metadata.Version); err != nil {
		problems = append(problems, fmt.Errorf("metadata.version: %w", err))
	}
	switch layer.Metadata.Origin {
	case OriginUpstream, OriginLocal:
	default:
		problems = append(problems, fmt.Errorf("metadata.origin: must be %q or %q, got %q",
			OriginUpstream, OriginLocal, layer.Metadata.Origin))
	}
	if len(layer.Metadata.Description) > maxDescriptionLength {
		problems = append(problems,
			fmt.Errorf("metadata.description: must be at most %d bytes", maxDescriptionLength))
	}

	seen := make(map[string]struct{}, len(layer.Spec.DependsOn))
	for i, dependency := range layer.Spec.DependsOn {
		path := fmt.Sprintf("spec.dependsOn[%d]", i)

		for _, problem := range validation.NameProblems(dependency.Name) {
			problems = append(problems, fmt.Errorf("%s.name: %s", path, problem))
		}
		if err := versions.ValidateRange(dependency.Version); err != nil {
			problems = append(problems, fmt.Errorf("%s.version: %w", path, err))
		}

		// The empty name is already reported as missing, and reporting it again
		// as a self-dependency would be noise.
		if dependency.Name != "" && dependency.Name == layer.Metadata.Name {
			problems = append(problems, fmt.Errorf("%s.name: a layer cannot depend on itself", path))
		}
		if _, repeated := seen[dependency.Name]; repeated {
			problems = append(problems,
				fmt.Errorf("%s.name: %q is declared more than once", path, dependency.Name))
		}
		seen[dependency.Name] = struct{}{}
	}

	return utilerrors.NewAggregate(problems)
}
