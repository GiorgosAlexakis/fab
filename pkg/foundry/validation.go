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
	"k8s.io/apimachinery/pkg/util/sets"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/GiorgosAlexakis/fab/pkg/util/versions"
)

// Validate checks foundry.yaml on its own terms.
//
// Whether the declared layers exist, and whether their dependencies can be
// satisfied, is the resolver's question: it needs the layer manifests, which
// this package does not read.
func Validate(config *Config) field.ErrorList {
	var allErrs field.ErrorList

	apiVersionPath := field.NewPath("apiVersion")
	if config.APIVersion != APIVersion {
		allErrs = append(allErrs, field.NotSupported(apiVersionPath, config.APIVersion, []string{APIVersion}))
	}
	if config.Kind != Kind {
		allErrs = append(allErrs, field.Invalid(field.NewPath("kind"), config.Kind, "must be "+Kind))
	}

	namePath := field.NewPath("metadata", "name")
	if config.Metadata.Name == "" {
		allErrs = append(allErrs, field.Required(namePath,
			"the foundry name is the ontology name every published version is stored under"))
	} else {
		for _, msg := range utilvalidation.IsDNS1123Subdomain(config.Metadata.Name) {
			allErrs = append(allErrs, field.Invalid(namePath, config.Metadata.Name, msg))
		}
	}

	allErrs = append(allErrs, validateLayers(config.Spec.Layers, field.NewPath("spec", "layers"))...)
	return allErrs
}

func validateLayers(selectors []LayerSelector, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	declared := sets.New[string]()
	for i := range selectors {
		selector := &selectors[i]
		selectorPath := fldPath.Index(i)

		namePath := selectorPath.Child("name")
		switch {
		case selector.Name == "":
			allErrs = append(allErrs, field.Required(namePath, ""))
		default:
			for _, msg := range utilvalidation.IsDNS1123Label(selector.Name) {
				allErrs = append(allErrs, field.Invalid(namePath, selector.Name, msg))
			}
		}

		// A selector may leave the version out: the official layers are released
		// as a set, so pinning each of them individually is optional.
		if selector.Version != "" {
			allErrs = append(allErrs, versions.ValidateRange(selector.Version, selectorPath.Child("version"))...)
		}

		if declared.Has(selector.Name) {
			allErrs = append(allErrs, field.Duplicate(namePath, selector.Name))
		}
		declared.Insert(selector.Name)
	}

	return allErrs
}
