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
	"k8s.io/apimachinery/pkg/util/validation/field"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

var (
	supportedCardinalities = []ontologyv1.Cardinality{
		ontologyv1.CardinalityOneToOne,
		ontologyv1.CardinalityOneToMany,
		ontologyv1.CardinalityManyToOne,
		ontologyv1.CardinalityManyToMany,
	}

	supportedDeletePolicies = []ontologyv1.DeletePolicy{
		ontologyv1.DeletePolicyRestrict,
		ontologyv1.DeletePolicyCascade,
		ontologyv1.DeletePolicySetNull,
		ontologyv1.DeletePolicyDetach,
	}
)

// ValidateLinkType validates a single link type document. Resolving Source and
// Target against the declared object types is the compiler's job.
func ValidateLinkType(obj *ontologyv1.LinkType) field.ErrorList {
	allErrs := validateTypeMeta(&obj.TypeMeta, ontologyv1.LinkTypeKind)
	allErrs = append(allErrs, validateObjectMeta(&obj.Metadata, field.NewPath("metadata"))...)

	specPath := field.NewPath("spec")
	allErrs = append(allErrs, validateTypeReference(&obj.Spec.Source, specPath.Child("source"))...)
	allErrs = append(allErrs, validateTypeReference(&obj.Spec.Target, specPath.Child("target"))...)

	cardinalityPath := specPath.Child("cardinality")
	if obj.Spec.Cardinality == "" {
		allErrs = append(allErrs, field.Required(cardinalityPath, ""))
	} else if !isSupportedCardinality(obj.Spec.Cardinality) {
		allErrs = append(allErrs, field.NotSupported(cardinalityPath, obj.Spec.Cardinality, supportedCardinalities))
	}

	allErrs = append(allErrs, validateTraversalName(obj.Spec.ForwardName, specPath.Child("forwardName"))...)
	allErrs = append(allErrs, validateTraversalName(obj.Spec.ReverseName, specPath.Child("reverseName"))...)
	if obj.Spec.ForwardName != "" && obj.Spec.ForwardName == obj.Spec.ReverseName {
		allErrs = append(allErrs, field.Invalid(specPath.Child("reverseName"), obj.Spec.ReverseName,
			"forwardName and reverseName must differ: they are the two traversal directions of this link"))
	}

	deletePath := specPath.Child("onSourceDelete")
	switch {
	case obj.Spec.OnSourceDelete == "":
		allErrs = append(allErrs, field.Required(deletePath, ""))
	case !isSupportedDeletePolicy(obj.Spec.OnSourceDelete):
		allErrs = append(allErrs, field.NotSupported(deletePath, obj.Spec.OnSourceDelete, supportedDeletePolicies))
	case obj.Spec.OnSourceDelete == ontologyv1.DeletePolicySetNull &&
		obj.Spec.Cardinality == ontologyv1.CardinalityManyToMany:
		// A many-to-many edge lives in its own link table; there is no single
		// reference to null out. Removing the edge is `detach`.
		allErrs = append(allErrs, field.Invalid(deletePath, obj.Spec.OnSourceDelete,
			"set_null is not valid for many_to_many links; use detach"))
	}

	return allErrs
}

func validateTypeReference(ref *ontologyv1.TypeReference, fldPath *field.Path) field.ErrorList {
	allErrs := validateLayerName(ref.Layer, fldPath.Child("layer"))

	typePath := fldPath.Child("type")
	switch {
	case ref.Type == "":
		allErrs = append(allErrs, field.Required(typePath, ""))
	case !typeNameRegexp.MatchString(ref.Type):
		allErrs = append(allErrs, field.Invalid(typePath, ref.Type,
			"must be PascalCase: an uppercase letter followed by letters and digits"))
	}

	return allErrs
}

func isSupportedCardinality(cardinality ontologyv1.Cardinality) bool {
	for _, supported := range supportedCardinalities {
		if cardinality == supported {
			return true
		}
	}
	return false
}

func isSupportedDeletePolicy(policy ontologyv1.DeletePolicy) bool {
	for _, supported := range supportedDeletePolicies {
		if policy == supported {
			return true
		}
	}
	return false
}
