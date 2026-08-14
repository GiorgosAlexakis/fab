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
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

var (
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
)

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
