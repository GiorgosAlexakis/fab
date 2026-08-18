package v1

import (
	"errors"
	"fmt"

	utilerrors "github.com/GiorgosAlexakis/fab/internal/util/errors"
	"github.com/GiorgosAlexakis/fab/internal/util/validation"
	"github.com/GiorgosAlexakis/fab/internal/util/versions"
)

// Validate checks a foundry on its own terms.
//
// It takes the struct rather than the file so that the same check covers what was
// read, what an edit produced and what is about to be written.
//
// Whether the declared layers exist, and whether their dependencies can be
// satisfied, is the resolver's question: it needs the layer manifests, which
// this package does not read.
//
// Every problem is reported, and each message opens with the path of the field
// it belongs to, e.g. spec.layers[1].name.
func (foundry *Foundry) Validate() error {
	var problems []error

	if foundry.APIVersion != APIVersion {
		problems = append(problems,
			fmt.Errorf("apiVersion: must be %q, got %q", APIVersion, foundry.APIVersion))
	}
	if foundry.Kind != FoundryKind {
		problems = append(problems,
			fmt.Errorf("kind: must be %q, got %q", FoundryKind, foundry.Kind))
	}

	if foundry.Metadata.Name == "" {
		problems = append(problems, errors.New(
			"metadata.name: is required, a foundry is named after the company it belongs to, e.g. acme-corp"))
	} else {
		for _, problem := range validation.NameProblems(foundry.Metadata.Name) {
			problems = append(problems, fmt.Errorf("metadata.name: %s", problem))
		}
	}

	declared := make(map[string]struct{}, len(foundry.Spec.Layers))
	for i, selector := range foundry.Spec.Layers {
		path := fmt.Sprintf("spec.layers[%d]", i)

		for _, problem := range validation.NameProblems(selector.Name) {
			problems = append(problems, fmt.Errorf("%s.name: %s", path, problem))
		}

		// A selector may leave the version out: the official layers are released
		// as a set, so pinning each of them individually is optional.
		if selector.Version != "" {
			if err := versions.ValidateRange(selector.Version); err != nil {
				problems = append(problems, fmt.Errorf("%s.version: %w", path, err))
			}
		}

		if _, repeated := declared[selector.Name]; repeated {
			problems = append(problems,
				fmt.Errorf("%s.name: %q is declared more than once", path, selector.Name))
		}
		declared[selector.Name] = struct{}{}
	}

	return utilerrors.NewAggregate(problems)
}
