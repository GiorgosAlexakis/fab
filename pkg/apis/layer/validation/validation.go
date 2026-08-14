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

// Package validation validates a layer manifest in isolation.
//
// Whether a dependency can actually be satisfied is the resolver's question,
// because answering it needs every other manifest. This package only decides
// whether one manifest is well formed.
package validation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	layerv1 "github.com/GiorgosAlexakis/fab/pkg/apis/layer/v1"
	"github.com/GiorgosAlexakis/fab/pkg/util/versions"
)

// maxDescriptionLength bounds documentation strings.
const maxDescriptionLength = 4096

var supportedOrigins = []layerv1.Origin{layerv1.OriginUpstream, layerv1.OriginLocal}

// ValidateLayer validates a defaulted layer manifest.
func ValidateLayer(obj *layerv1.Layer) field.ErrorList {
	var allErrs field.ErrorList

	apiVersionPath := field.NewPath("apiVersion")
	if obj.APIVersion == "" {
		allErrs = append(allErrs, field.Required(apiVersionPath, ""))
	} else if obj.APIVersion != layerv1.SchemeGroupVersion.String() {
		allErrs = append(allErrs, field.NotSupported(apiVersionPath, obj.APIVersion,
			[]string{layerv1.SchemeGroupVersion.String()}))
	}

	kindPath := field.NewPath("kind")
	if obj.Kind == "" {
		allErrs = append(allErrs, field.Required(kindPath, ""))
	} else if obj.Kind != layerv1.LayerKind {
		allErrs = append(allErrs, field.Invalid(kindPath, obj.Kind,
			fmt.Sprintf("must be %s", layerv1.LayerKind)))
	}

	allErrs = append(allErrs, validateMeta(&obj.Metadata, field.NewPath("metadata"))...)
	allErrs = append(allErrs, validateSpec(&obj.Spec, obj.Metadata.Name, field.NewPath("spec"))...)
	return allErrs
}

// ValidateLayerName validates a layer name. Layer names become directory names
// and qualify every type a layer owns, so they follow the rules of a DNS label.
func ValidateLayerName(name string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList
	if name == "" {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}
	for _, msg := range utilvalidation.IsDNS1123Label(name) {
		allErrs = append(allErrs, field.Invalid(fldPath, name, msg))
	}
	return allErrs
}

func validateMeta(meta *layerv1.LayerMeta, fldPath *field.Path) field.ErrorList {
	allErrs := ValidateLayerName(meta.Name, fldPath.Child("name"))
	allErrs = append(allErrs, versions.ValidateVersion(meta.Version, fldPath.Child("version"))...)

	if !isSupportedOrigin(meta.Origin) {
		allErrs = append(allErrs, field.NotSupported(fldPath.Child("origin"), meta.Origin, supportedOrigins))
	}
	if len(meta.Description) > maxDescriptionLength {
		allErrs = append(allErrs, field.TooLong(fldPath.Child("description"), "", maxDescriptionLength))
	}

	return allErrs
}

func validateSpec(spec *layerv1.LayerSpec, layerName string, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	dependsOnPath := fldPath.Child("dependsOn")
	seen := sets.New[string]()
	for i := range spec.DependsOn {
		dependency := &spec.DependsOn[i]
		dependencyPath := dependsOnPath.Index(i)

		allErrs = append(allErrs, ValidateLayerName(dependency.Name, dependencyPath.Child("name"))...)
		allErrs = append(allErrs, versions.ValidateRange(dependency.Version, dependencyPath.Child("version"))...)

		if dependency.Name != "" && dependency.Name == layerName {
			allErrs = append(allErrs, field.Invalid(dependencyPath.Child("name"), dependency.Name,
				"a layer cannot depend on itself"))
		}
		if seen.Has(dependency.Name) {
			allErrs = append(allErrs, field.Duplicate(dependencyPath.Child("name"), dependency.Name))
		}
		seen.Insert(dependency.Name)
	}

	return allErrs
}

func isSupportedOrigin(origin layerv1.Origin) bool {
	for _, supported := range supportedOrigins {
		if origin == supported {
			return true
		}
	}
	return false
}
