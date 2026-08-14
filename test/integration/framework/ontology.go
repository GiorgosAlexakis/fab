//go:build integration

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

package framework

import (
	"testing"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/compiler"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
)

// CompileOntology compiles the given layers, failing the test if they do not
// compile. Integration tests build their fixtures through the compiler so that
// what they store is always a real snapshot.
func CompileOntology(t *testing.T, layers []compiler.LayerSource) *snapshot.Snapshot {
	t.Helper()

	compiled, err := compiler.Compile(layers)
	if err != nil {
		t.Fatalf("compiling the test ontology: %v", err)
	}
	return compiled
}

// SampleOntology returns a two-layer ontology: a meta-core layer with User, and
// an app layer with Customer and Order plus links between them.
func SampleOntology(t *testing.T) *snapshot.Snapshot {
	t.Helper()

	return CompileOntology(t, []compiler.LayerSource{
		{
			Layer: "meta-core",
			Documents: []compiler.Document{
				{Source: "layers/meta-core/schema/objects/user.yaml", Object: &ontologyv1.ObjectType{
					Metadata: ontologyv1.ObjectMeta{Name: "User", Description: "A person who can sign in."},
					Spec: ontologyv1.ObjectTypeSpec{
						PrimaryKey: "id",
						Properties: []ontologyv1.Property{
							{Name: "id", Type: ontologyv1.PropertyTypeString},
							{Name: "email", Type: ontologyv1.PropertyTypeString, Unique: true},
						},
					},
				}},
			},
		},
		{
			Layer: "app",
			Documents: []compiler.Document{
				{Source: "schema/objects/customer.yaml", Object: &ontologyv1.ObjectType{
					Metadata: ontologyv1.ObjectMeta{Name: "Customer"},
					Spec: ontologyv1.ObjectTypeSpec{
						PrimaryKey: "id",
						Properties: []ontologyv1.Property{
							{Name: "id", Type: ontologyv1.PropertyTypeString},
							{Name: "email", Type: ontologyv1.PropertyTypeString, Unique: true},
							{Name: "tier", Type: ontologyv1.PropertyTypeEnum,
								Values: []string{"free", "pro", "enterprise"}, Indexed: true},
							{Name: "lifetime_value", Type: ontologyv1.PropertyTypeDecimal},
							{Name: "tags", Type: ontologyv1.PropertyTypeArray, Items: ontologyv1.PropertyTypeString},
							{Name: "preferences", Type: ontologyv1.PropertyTypeJSON},
						},
					},
				}},
				{Source: "schema/objects/order.yaml", Object: &ontologyv1.ObjectType{
					Metadata: ontologyv1.ObjectMeta{Name: "Order"},
					Spec: ontologyv1.ObjectTypeSpec{
						PrimaryKey: "id",
						Properties: []ontologyv1.Property{
							{Name: "id", Type: ontologyv1.PropertyTypeString},
							{Name: "placed_at", Type: ontologyv1.PropertyTypeTimestamp, Indexed: true},
							{Name: "total", Type: ontologyv1.PropertyTypeDecimal},
						},
					},
				}},
				{Source: "schema/links/customer_orders.yaml", Object: &ontologyv1.LinkType{
					Metadata: ontologyv1.ObjectMeta{Name: "CustomerOrders"},
					Spec: ontologyv1.LinkTypeSpec{
						Source:      ontologyv1.TypeReference{Type: "Customer"},
						Target:      ontologyv1.TypeReference{Type: "Order"},
						Cardinality: ontologyv1.CardinalityOneToMany,
						ReverseName: "customer",
					},
				}},
				{Source: "schema/links/customer_account.yaml", Object: &ontologyv1.LinkType{
					Metadata: ontologyv1.ObjectMeta{Name: "CustomerAccount"},
					Spec: ontologyv1.LinkTypeSpec{
						Source:         ontologyv1.TypeReference{Type: "Customer"},
						Target:         ontologyv1.TypeReference{Layer: "meta-core", Type: "User"},
						Cardinality:    ontologyv1.CardinalityOneToOne,
						ReverseName:    "customer",
						OnSourceDelete: ontologyv1.DeletePolicyDetach,
					},
				}},
			},
		},
	})
}
