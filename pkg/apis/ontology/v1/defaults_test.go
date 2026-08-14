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

func TestSetDefaultsLinkType(t *testing.T) {
	obj := &LinkType{
		Metadata: ObjectMeta{Name: "CustomerOrders", Layer: "app"},
		Spec: LinkTypeSpec{
			Source:      TypeReference{Type: "Customer"},
			Target:      TypeReference{Type: "Order"},
			Cardinality: CardinalityOneToMany,
		},
	}

	SetObjectDefaults(obj)

	if obj.Spec.Source.Layer != "app" || obj.Spec.Target.Layer != "app" {
		t.Errorf("endpoint layers = %q/%q, want app/app", obj.Spec.Source.Layer, obj.Spec.Target.Layer)
	}
	if obj.Spec.ForwardName != "customer_orders" {
		t.Errorf("ForwardName = %q, want customer_orders", obj.Spec.ForwardName)
	}
	if obj.Spec.ReverseName != "customer" {
		t.Errorf("ReverseName = %q, want customer", obj.Spec.ReverseName)
	}
	if obj.Spec.OnSourceDelete != DeletePolicyRestrict {
		t.Errorf("OnSourceDelete = %q, want restrict", obj.Spec.OnSourceDelete)
	}
}

func TestSetDefaultsLinkTypeKeepsCrossLayerEndpoint(t *testing.T) {
	obj := &LinkType{
		Metadata: ObjectMeta{Name: "OrganizationSubscription", Layer: "meta-billing"},
		Spec: LinkTypeSpec{
			Source:      TypeReference{Layer: "meta-core", Type: "Organization"},
			Target:      TypeReference{Type: "Subscription"},
			Cardinality: CardinalityOneToOne,
		},
	}

	SetObjectDefaults(obj)

	if obj.Spec.Source.Layer != "meta-core" {
		t.Errorf("Source.Layer = %q, want meta-core", obj.Spec.Source.Layer)
	}
	if obj.Spec.Target.Layer != "meta-billing" {
		t.Errorf("Target.Layer = %q, want meta-billing", obj.Spec.Target.Layer)
	}
}

func TestNewUnknownKind(t *testing.T) {
	if _, err := New("Aspect"); err == nil {
		t.Fatal("New(\"Aspect\") succeeded, want an error naming the supported kinds")
	}
}
