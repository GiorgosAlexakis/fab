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

package v1

import "testing"

func TestSetDefaultsObjectType(t *testing.T) {
	obj := &ObjectType{
		Metadata: ObjectMeta{Name: "Customer", Layer: "app"},
		Spec: ObjectTypeSpec{
			PrimaryKey: "id",
			Properties: []Property{
				{Name: "id", Type: PropertyTypeString},
				{Name: "email", Type: PropertyTypeString, Unique: true},
				{Name: "notes", Type: PropertyTypeString},
			},
		},
	}

	SetObjectDefaults(obj)

	if obj.APIVersion != "fab/v1" {
		t.Errorf("APIVersion = %q, want fab/v1", obj.APIVersion)
	}
	if obj.Kind != ObjectTypeKind {
		t.Errorf("Kind = %q, want %q", obj.Kind, ObjectTypeKind)
	}

	primaryKey := obj.Spec.Properties[0]
	if primaryKey.IsNullable() {
		t.Error("primary key defaulted to nullable, want non-nullable")
	}
	if !primaryKey.Unique || !primaryKey.Indexed {
		t.Errorf("primary key unique=%v indexed=%v, want both true", primaryKey.Unique, primaryKey.Indexed)
	}

	if !obj.Spec.Properties[1].Indexed {
		t.Error("unique property was not defaulted to indexed")
	}

	if !obj.Spec.Properties[2].IsNullable() {
		t.Error("ordinary property defaulted to non-nullable, want nullable")
	}
}

func TestSetDefaultsObjectTypeKeepsExplicitNullable(t *testing.T) {
	nonNullable := false
	obj := &ObjectType{
		Metadata: ObjectMeta{Name: "Customer", Layer: "app"},
		Spec: ObjectTypeSpec{
			PrimaryKey: "id",
			Properties: []Property{
				{Name: "id", Type: PropertyTypeString},
				{Name: "email", Type: PropertyTypeString, Nullable: &nonNullable},
			},
		},
	}

	SetObjectDefaults(obj)

	if obj.Spec.Properties[1].IsNullable() {
		t.Error("explicit nullable=false was overwritten")
	}
}

func TestNewUnknownKind(t *testing.T) {
	if _, err := New("Aspect"); err == nil {
		t.Fatal("New(\"Aspect\") succeeded, want an error naming the supported kinds")
	}
}
