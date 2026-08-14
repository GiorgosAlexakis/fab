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
	"strings"
	"testing"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
)

// validObjectType returns a document that passes validation, which each test
// case then breaks in exactly one way.
func validObjectType(mutate func(obj *ontologyv1.ObjectType)) *ontologyv1.ObjectType {
	obj := &ontologyv1.ObjectType{
		Metadata: ontologyv1.ObjectMeta{Name: "Customer", Layer: "app"},
		Spec: ontologyv1.ObjectTypeSpec{
			PrimaryKey: "id",
			Properties: []ontologyv1.Property{
				{Name: "id", Type: ontologyv1.PropertyTypeString},
				{Name: "email", Type: ontologyv1.PropertyTypeString, Unique: true},
				{Name: "tier", Type: ontologyv1.PropertyTypeEnum, Values: []string{"free", "pro"}},
			},
		},
	}
	if mutate != nil {
		mutate(obj)
	}
	ontologyv1.SetObjectDefaults(obj)
	return obj
}

func TestValidateObjectType(t *testing.T) {
	testCases := []struct {
		name      string
		obj       *ontologyv1.ObjectType
		wantField string
	}{
		{
			name: "valid",
			obj:  validObjectType(nil),
		},
		{
			name:      "name is not PascalCase",
			obj:       validObjectType(func(obj *ontologyv1.ObjectType) { obj.Metadata.Name = "customer" }),
			wantField: "metadata.name",
		},
		{
			name:      "layer is not a DNS label",
			obj:       validObjectType(func(obj *ontologyv1.ObjectType) { obj.Metadata.Layer = "Meta_Core" }),
			wantField: "metadata.layer",
		},
		{
			// Defaulting fills an empty kind, so the reachable failure is a
			// document whose kind disagrees with its content.
			name:      "kind does not match the document",
			obj:       validObjectType(func(obj *ontologyv1.ObjectType) { obj.Kind = "Aspect" }),
			wantField: "kind",
		},
		{
			name:      "wrong api version",
			obj:       validObjectType(func(obj *ontologyv1.ObjectType) { obj.APIVersion = "fab/v2" }),
			wantField: "apiVersion",
		},
		{
			name: "no properties",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties = nil
			}),
			wantField: "spec.properties",
		},
		{
			name: "property name is not snake_case",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Name = "emailAddress"
			}),
			wantField: "spec.properties[1].name",
		},
		{
			name: "property name is reserved",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Name = "object_id"
			}),
			wantField: "spec.properties[1].name",
		},
		{
			name: "duplicate property",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[2].Name = "email"
			}),
			wantField: "spec.properties[2].name",
		},
		{
			name: "unknown property type",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Type = "uuid"
			}),
			wantField: "spec.properties[1].type",
		},
		{
			name: "enum without values",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[2].Values = nil
			}),
			wantField: "spec.properties[2].values",
		},
		{
			name: "duplicate enum value",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[2].Values = []string{"free", "free"}
			}),
			wantField: "spec.properties[2].values[1]",
		},
		{
			name: "values on a non-enum property",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Values = []string{"free"}
			}),
			wantField: "spec.properties[1].values",
		},
		{
			name: "array without items",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Type = ontologyv1.PropertyTypeArray
			}),
			wantField: "spec.properties[1].items",
		},
		{
			name: "array of arrays",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Type = ontologyv1.PropertyTypeArray
				obj.Spec.Properties[1].Items = ontologyv1.PropertyTypeArray
			}),
			wantField: "spec.properties[1].items",
		},
		{
			name: "items on a non-array property",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Items = ontologyv1.PropertyTypeString
			}),
			wantField: "spec.properties[1].items",
		},
		{
			name: "unique json property",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[1].Type = ontologyv1.PropertyTypeJSON
			}),
			wantField: "spec.properties[1].unique",
		},
		{
			name: "missing primary key",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.PrimaryKey = ""
			}),
			wantField: "spec.primaryKey",
		},
		{
			name: "primary key names an undeclared property",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.PrimaryKey = "customer_id"
			}),
			wantField: "spec.primaryKey",
		},
		{
			name: "nullable primary key",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				nullable := true
				obj.Spec.Properties[0].Nullable = &nullable
			}),
			wantField: "spec.primaryKey",
		},
		{
			name: "json primary key",
			obj: validObjectType(func(obj *ontologyv1.ObjectType) {
				obj.Spec.Properties[0].Type = ontologyv1.PropertyTypeJSON
			}),
			wantField: "spec.primaryKey",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			errs := ValidateObjectType(testCase.obj)
			assertFieldError(t, errs.ToAggregate(), testCase.wantField)
		})
	}
}

// assertFieldError requires that err mentions wantField, or that err is nil
// when wantField is empty.
func assertFieldError(t *testing.T, err error, wantField string) {
	t.Helper()

	if wantField == "" {
		if err != nil {
			t.Fatalf("expected no validation errors, got: %v", err)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected a validation error on %s, got none", wantField)
	}
	if !strings.Contains(err.Error(), wantField) {
		t.Fatalf("expected a validation error on %s, got: %v", wantField, err)
	}
}
