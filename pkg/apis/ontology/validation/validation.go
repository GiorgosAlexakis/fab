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

// Package validation validates single ontology documents in isolation.
//
// Anything that needs to look at more than one document -- a link that
// references an object type, two layers claiming the same type name -- belongs
// to the compiler, not here.
package validation

import (
	"fmt"
	"regexp"

	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

const (
	// maxNameLength bounds every identifier the ontology emits into a
	// PostgreSQL identifier or a proto field name.
	maxNameLength = 63
	// maxDescriptionLength bounds documentation strings so a snapshot stays a
	// reasonable size.
	maxDescriptionLength = 4096
)

var (
	// typeNameRegexp matches PascalCase type names: Customer, PurchaseOrder.
	typeNameRegexp = regexp.MustCompile(`^[A-Z][A-Za-z0-9]*$`)
	// snakeCaseRegexp matches property and traversal names: email, last_login.
	snakeCaseRegexp = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
)

func validateTypeMeta(meta *ontologyv1.TypeMeta, expectedKind string) field.ErrorList {
	var allErrs field.ErrorList

	apiVersionPath := field.NewPath("apiVersion")
	if meta.APIVersion == "" {
		allErrs = append(allErrs, field.Required(apiVersionPath, ""))
	} else if meta.APIVersion != ontologyv1.SchemeGroupVersion.String() {
		allErrs = append(allErrs, field.NotSupported(apiVersionPath, meta.APIVersion,
			[]string{ontologyv1.SchemeGroupVersion.String()}))
	}

	kindPath := field.NewPath("kind")
	if meta.Kind == "" {
		allErrs = append(allErrs, field.Required(kindPath, ""))
	} else if meta.Kind != expectedKind {
		allErrs = append(allErrs, field.Invalid(kindPath, meta.Kind,
			fmt.Sprintf("must be %s", expectedKind)))
	}

	return allErrs
}

func validateObjectMeta(meta *ontologyv1.ObjectMeta, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	namePath := fldPath.Child("name")
	switch {
	case meta.Name == "":
		allErrs = append(allErrs, field.Required(namePath, ""))
	case !typeNameRegexp.MatchString(meta.Name):
		allErrs = append(allErrs, field.Invalid(namePath, meta.Name,
			"must be PascalCase: an uppercase letter followed by letters and digits"))
	case len(meta.Name) > maxNameLength:
		allErrs = append(allErrs, field.TooLong(namePath, meta.Name, maxNameLength))
	}

	allErrs = append(allErrs, validateLayerName(meta.Layer, fldPath.Child("layer"))...)

	if len(meta.Description) > maxDescriptionLength {
		allErrs = append(allErrs, field.TooLong(fldPath.Child("description"), "", maxDescriptionLength))
	}

	return allErrs
}

// ValidateLayerName validates a layer name. Layer names appear in directory
// paths, package names and lock entries, so they follow the same rules as a
// Kubernetes DNS label.
func ValidateLayerName(layer string, fldPath *field.Path) field.ErrorList {
	return validateLayerName(layer, fldPath)
}

func validateLayerName(layer string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if layer == "" {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}
	for _, msg := range utilvalidation.IsDNS1123Label(layer) {
		allErrs = append(allErrs, field.Invalid(fldPath, layer, msg))
	}
	return allErrs
}

func validateTraversalName(name string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	switch {
	case name == "":
		allErrs = append(allErrs, field.Required(fldPath, ""))
	case !snakeCaseRegexp.MatchString(name):
		allErrs = append(allErrs, field.Invalid(fldPath, name,
			"must be snake_case: lowercase alphanumerics separated by single underscores"))
	case len(name) > maxNameLength:
		allErrs = append(allErrs, field.TooLong(fldPath, name, maxNameLength))
	}
	return allErrs
}
