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
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
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
	// enumValueRegexp matches enum values, which are data rather than
	// identifiers and so allow mixed case: free, pro, ENTERPRISE.
	enumValueRegexp = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

	// reservedPropertyNames are the names the object store and generated
	// clients use for row identity. A property may not shadow them.
	//
	// Note that created_at and updated_at are NOT reserved: meta-core's
	// Auditable interface declares them as ordinary ontology properties.
	reservedPropertyNames = sets.New[string]("object_id", "object_type")

	supportedPropertyTypes = []ontologyv1.PropertyType{
		ontologyv1.PropertyTypeString,
		ontologyv1.PropertyTypeBoolean,
		ontologyv1.PropertyTypeInteger,
		ontologyv1.PropertyTypeLong,
		ontologyv1.PropertyTypeDouble,
		ontologyv1.PropertyTypeDecimal,
		ontologyv1.PropertyTypeTimestamp,
		ontologyv1.PropertyTypeDate,
		ontologyv1.PropertyTypeJSON,
		ontologyv1.PropertyTypeEnum,
		ontologyv1.PropertyTypeArray,
	}

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

// ValidateObject validates a defaulted ontology document of any known kind.
func ValidateObject(obj ontologyv1.Object) field.ErrorList {
	switch typed := obj.(type) {
	case *ontologyv1.ObjectType:
		return ValidateObjectType(typed)
	case *ontologyv1.LinkType:
		return ValidateLinkType(typed)
	default:
		return field.ErrorList{field.InternalError(field.NewPath("kind"),
			fmt.Errorf("no validation registered for %T", obj))}
	}
}

// ValidateObjectType validates a single object type document.
func ValidateObjectType(obj *ontologyv1.ObjectType) field.ErrorList {
	allErrs := validateTypeMeta(&obj.TypeMeta, ontologyv1.ObjectTypeKind)
	allErrs = append(allErrs, validateObjectMeta(&obj.Metadata, field.NewPath("metadata"))...)

	specPath := field.NewPath("spec")
	propertiesPath := specPath.Child("properties")

	if len(obj.Spec.Properties) == 0 {
		allErrs = append(allErrs, field.Required(propertiesPath, "an object type must declare at least one property"))
	}

	names := sets.New[string]()
	for i := range obj.Spec.Properties {
		property := &obj.Spec.Properties[i]
		propertyPath := propertiesPath.Index(i)
		allErrs = append(allErrs, ValidateProperty(property, propertyPath)...)
		if names.Has(property.Name) {
			allErrs = append(allErrs, field.Duplicate(propertyPath.Child("name"), property.Name))
		}
		names.Insert(property.Name)
	}

	allErrs = append(allErrs, validatePrimaryKey(obj, specPath.Child("primaryKey"))...)
	return allErrs
}

// ValidateProperty validates a single property of an object type.
func ValidateProperty(property *ontologyv1.Property, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	namePath := fldPath.Child("name")
	switch {
	case property.Name == "":
		allErrs = append(allErrs, field.Required(namePath, ""))
	case !snakeCaseRegexp.MatchString(property.Name):
		allErrs = append(allErrs, field.Invalid(namePath, property.Name,
			"must be snake_case: lowercase alphanumerics separated by single underscores"))
	case len(property.Name) > maxNameLength:
		allErrs = append(allErrs, field.TooLong(namePath, property.Name, maxNameLength))
	case reservedPropertyNames.Has(property.Name):
		allErrs = append(allErrs, field.Invalid(namePath, property.Name,
			fmt.Sprintf("is reserved by the object store: %s", strings.Join(sets.List(reservedPropertyNames), ", "))))
	}

	if len(property.Description) > maxDescriptionLength {
		allErrs = append(allErrs, field.TooLong(fldPath.Child("description"), "", maxDescriptionLength))
	}

	typePath := fldPath.Child("type")
	if property.Type == "" {
		allErrs = append(allErrs, field.Required(typePath, ""))
		return allErrs
	}
	if !isSupportedPropertyType(property.Type) {
		allErrs = append(allErrs, field.NotSupported(typePath, property.Type, supportedPropertyTypes))
		return allErrs
	}

	allErrs = append(allErrs, validateItems(property, fldPath.Child("items"))...)
	allErrs = append(allErrs, validateEnumValues(property, fldPath.Child("values"))...)

	// Uniqueness and indexes are storage constraints on a single comparable
	// value; composite and opaque types have no such value.
	if !property.Type.IsScalar() {
		if property.Unique {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("unique"), property.Unique,
				fmt.Sprintf("only scalar properties can be unique, %s is not scalar", property.Type)))
		}
		if property.Indexed && property.Type != ontologyv1.PropertyTypeEnum {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("indexed"), property.Indexed,
				fmt.Sprintf("only scalar and enum properties can be indexed, %s is neither", property.Type)))
		}
	}

	return allErrs
}

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

func validatePrimaryKey(obj *ontologyv1.ObjectType, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if obj.Spec.PrimaryKey == "" {
		allErrs = append(allErrs, field.Required(fldPath, ""))
		return allErrs
	}

	var primaryKey *ontologyv1.Property
	for i := range obj.Spec.Properties {
		if obj.Spec.Properties[i].Name == obj.Spec.PrimaryKey {
			primaryKey = &obj.Spec.Properties[i]
			break
		}
	}
	if primaryKey == nil {
		allErrs = append(allErrs, field.Invalid(fldPath, obj.Spec.PrimaryKey,
			"must name a property declared in spec.properties"))
		return allErrs
	}

	if !primaryKey.Type.IsScalar() {
		allErrs = append(allErrs, field.Invalid(fldPath, obj.Spec.PrimaryKey,
			fmt.Sprintf("primary key must be a scalar property, %q is %s", primaryKey.Name, primaryKey.Type)))
	}
	if primaryKey.IsNullable() {
		allErrs = append(allErrs, field.Invalid(fldPath, obj.Spec.PrimaryKey,
			fmt.Sprintf("primary key %q must not be nullable", primaryKey.Name)))
	}

	return allErrs
}

func validateItems(property *ontologyv1.Property, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if property.Type != ontologyv1.PropertyTypeArray {
		if property.Items != "" {
			allErrs = append(allErrs, field.Invalid(fldPath, property.Items,
				fmt.Sprintf("may only be set when type is %s", ontologyv1.PropertyTypeArray)))
		}
		return allErrs
	}

	switch {
	case property.Items == "":
		allErrs = append(allErrs, field.Required(fldPath,
			fmt.Sprintf("required when type is %s", ontologyv1.PropertyTypeArray)))
	case !isSupportedPropertyType(property.Items):
		allErrs = append(allErrs, field.NotSupported(fldPath, property.Items, supportedPropertyTypes))
	case !property.Items.IsScalar():
		allErrs = append(allErrs, field.Invalid(fldPath, property.Items,
			"array element type must be scalar; nested arrays, enums and json elements are not supported"))
	}

	return allErrs
}

func validateEnumValues(property *ontologyv1.Property, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if property.Type != ontologyv1.PropertyTypeEnum {
		if len(property.Values) > 0 {
			allErrs = append(allErrs, field.Invalid(fldPath, property.Values,
				fmt.Sprintf("may only be set when type is %s", ontologyv1.PropertyTypeEnum)))
		}
		return allErrs
	}

	if len(property.Values) == 0 {
		allErrs = append(allErrs, field.Required(fldPath,
			fmt.Sprintf("required when type is %s", ontologyv1.PropertyTypeEnum)))
		return allErrs
	}

	seen := sets.New[string]()
	for i, value := range property.Values {
		valuePath := fldPath.Index(i)
		switch {
		case value == "":
			allErrs = append(allErrs, field.Required(valuePath, ""))
		case !enumValueRegexp.MatchString(value):
			allErrs = append(allErrs, field.Invalid(valuePath, value,
				"must start with an alphanumeric and contain only alphanumerics, underscores and dashes"))
		case len(value) > maxNameLength:
			allErrs = append(allErrs, field.TooLong(valuePath, value, maxNameLength))
		}
		if seen.Has(value) {
			allErrs = append(allErrs, field.Duplicate(valuePath, value))
		}
		seen.Insert(value)
	}

	return allErrs
}

func isSupportedPropertyType(propertyType ontologyv1.PropertyType) bool {
	for _, supported := range supportedPropertyTypes {
		if propertyType == supported {
			return true
		}
	}
	return false
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
