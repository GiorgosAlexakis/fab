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

package objectstore

import (
	"errors"
	"reflect"
	"testing"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
)

// Catalog ids the fixtures below are bound with. They are arbitrary, which is
// the point: nothing in the store may assume ids are dense or ordered.
const (
	customerTypeID = 11
	orderTypeID    = 12
	customerIDProp = 101
	customerTier   = 102
	orderIDProp    = 201
	orderTotal     = 202
	linkID         = 301
)

func testSnapshot() *snapshot.Snapshot {
	return &snapshot.Snapshot{
		Layers: []string{"app"},
		ObjectTypes: []snapshot.ObjectType{{
			Layer:      "app",
			Name:       "Customer",
			PrimaryKey: "id",
			Properties: []snapshot.Property{
				{Name: "id", Type: string(ontologyv1.PropertyTypeString)},
				{Name: "tier", Type: string(ontologyv1.PropertyTypeEnum),
					Values: []string{"free", "pro"}, Nullable: true, Indexed: true},
			},
		}, {
			Layer:      "app",
			Name:       "Order",
			PrimaryKey: "id",
			Properties: []snapshot.Property{
				{Name: "id", Type: string(ontologyv1.PropertyTypeString)},
				{Name: "total", Type: string(ontologyv1.PropertyTypeDecimal), Nullable: true},
			},
		}},
		LinkTypes: []snapshot.LinkType{{
			Layer:          "app",
			Name:           "CustomerOrders",
			Source:         snapshot.TypeRef{Layer: "app", Type: "Customer"},
			Target:         snapshot.TypeRef{Layer: "app", Type: "Order"},
			Cardinality:    string(ontologyv1.CardinalityOneToMany),
			ForwardName:    "customer_orders",
			ReverseName:    "customer",
			OnSourceDelete: string(ontologyv1.DeletePolicyCascade),
		}},
	}
}

func testDictionary() *registry.Dictionary {
	return &registry.Dictionary{
		Ontology: registry.Ontology{Name: "acme-corp", Version: "1.0.0"},
		Types: map[string]int32{
			"app/Customer": customerTypeID,
			"app/Order":    orderTypeID,
		},
		TypeNames: map[int32]string{
			customerTypeID: "app/Customer",
			orderTypeID:    "app/Order",
		},
		Properties: map[int32]map[string]int32{
			customerTypeID: {"id": customerIDProp, "tier": customerTier},
			orderTypeID:    {"id": orderIDProp, "total": orderTotal},
		},
		PrimaryKeys: map[int32]int32{
			customerTypeID: customerIDProp,
			orderTypeID:    orderIDProp,
		},
		Links: map[string]int32{"app/CustomerOrders": linkID},
	}
}

func testBinding(t *testing.T) *Binding {
	t.Helper()

	binding, err := NewBinding(testSnapshot(), testDictionary())
	if err != nil {
		t.Fatalf("NewBinding() failed: %v", err)
	}
	return binding
}

func TestBindingResolvesTypesAndProperties(t *testing.T) {
	binding := testBinding(t)

	customer, err := binding.ObjectType("app/Customer")
	if err != nil {
		t.Fatalf("ObjectType() failed: %v", err)
	}
	if customer.ID != customerTypeID {
		t.Errorf("app/Customer id = %d, want %d", customer.ID, customerTypeID)
	}
	if customer.PrimaryKey.Name != "id" || customer.PrimaryKey.ID != customerIDProp {
		t.Errorf("primary key = %s/%d, want id/%d",
			customer.PrimaryKey.Name, customer.PrimaryKey.ID, customerIDProp)
	}
	if customer.PrimaryKey.Nullable {
		t.Error("the primary key is bound as nullable")
	}

	tier, ok := customer.Property("tier")
	if !ok {
		t.Fatal("tier is missing from the bound type")
	}
	if tier.ID != customerTier {
		t.Errorf("tier id = %d, want %d", tier.ID, customerTier)
	}
	if !reflect.DeepEqual(tier.EnumValues, []string{"free", "pro"}) {
		t.Errorf("tier enum values = %v, want [free pro]", tier.EnumValues)
	}

	// Reads arrive as ids and have to find their way back to a name.
	byID, ok := customer.PropertyByID(customerTier)
	if !ok || byID.Name != "tier" {
		t.Errorf("PropertyByID(%d) = %+v, want tier", customerTier, byID)
	}
	if _, ok := customer.PropertyByID(9999); ok {
		t.Error("PropertyByID() resolved an id the ontology does not define")
	}

	if names := binding.ObjectTypeNames(); !reflect.DeepEqual(names, []string{"app/Customer", "app/Order"}) {
		t.Errorf("ObjectTypeNames() = %v, want [app/Customer app/Order]", names)
	}
}

func TestBindingResolvesLinksAndTraversals(t *testing.T) {
	binding := testBinding(t)

	linkType, err := binding.LinkType("app/CustomerOrders")
	if err != nil {
		t.Fatalf("LinkType() failed: %v", err)
	}
	if linkType.ID != linkID {
		t.Errorf("link id = %d, want %d", linkType.ID, linkID)
	}
	if linkType.OnSourceDelete != string(ontologyv1.DeletePolicyCascade) {
		t.Errorf("onSourceDelete = %q, want cascade", linkType.OnSourceDelete)
	}

	// The same link is reachable under its forward name from the source and its
	// reverse name from the target: to a caller holding an object, both are just
	// a named edge to follow.
	forward, err := binding.Traversal("app/Customer", "customer_orders")
	if err != nil {
		t.Fatalf("forward Traversal() failed: %v", err)
	}
	if !forward.Forward || forward.FromTypeID != customerTypeID || forward.ToTypeID != orderTypeID {
		t.Errorf("forward traversal = %+v, want customer -> order", forward)
	}

	reverse, err := binding.Traversal("app/Order", "customer")
	if err != nil {
		t.Fatalf("reverse Traversal() failed: %v", err)
	}
	if reverse.Forward || reverse.FromTypeID != orderTypeID || reverse.ToTypeID != customerTypeID {
		t.Errorf("reverse traversal = %+v, want order -> customer", reverse)
	}

	// A traversal only exists on the type that can start it.
	if _, err := binding.Traversal("app/Order", "customer_orders"); !errors.Is(err, ErrUnknownLink) {
		t.Errorf("Traversal() of a forward name from the target: error = %v, want ErrUnknownLink", err)
	}

	outgoing := binding.OutgoingLinks(customerTypeID)
	if len(outgoing) != 1 || outgoing[0].ID != linkID {
		t.Errorf("OutgoingLinks(customer) = %+v, want the one link", outgoing)
	}
	if outgoing := binding.OutgoingLinks(orderTypeID); len(outgoing) != 0 {
		t.Errorf("OutgoingLinks(order) = %+v, want none: order is never the source", outgoing)
	}
}

func TestBindingRejectsUnknownNames(t *testing.T) {
	binding := testBinding(t)

	if _, err := binding.ObjectType("app/Invoice"); !errors.Is(err, ErrUnknownType) {
		t.Errorf("ObjectType() error = %v, want ErrUnknownType", err)
	}
	if _, err := binding.ObjectTypeByID(9999); !errors.Is(err, ErrUnknownType) {
		t.Errorf("ObjectTypeByID() error = %v, want ErrUnknownType", err)
	}
	if _, err := binding.LinkType("app/InvoiceOrders"); !errors.Is(err, ErrUnknownLink) {
		t.Errorf("LinkType() error = %v, want ErrUnknownLink", err)
	}
	if _, err := binding.LinkTypeByID(9999); !errors.Is(err, ErrUnknownLink) {
		t.Errorf("LinkTypeByID() error = %v, want ErrUnknownLink", err)
	}
}

// TestNewBindingRequiresACompleteDictionary is the guard that matters most: a
// store that could not resolve a property to an id would silently drop values.
func TestNewBindingRequiresACompleteDictionary(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*registry.Dictionary)
	}{{
		name: "a type with no id",
		mutate: func(d *registry.Dictionary) {
			delete(d.Types, "app/Order")
		},
	}, {
		name: "a property with no id",
		mutate: func(d *registry.Dictionary) {
			delete(d.Properties[customerTypeID], "tier")
		},
	}, {
		name: "a link with no id",
		mutate: func(d *registry.Dictionary) {
			delete(d.Links, "app/CustomerOrders")
		},
	}}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dictionary := testDictionary()
			test.mutate(dictionary)

			if _, err := NewBinding(testSnapshot(), dictionary); err == nil {
				t.Fatal("NewBinding() accepted a dictionary that does not cover the ontology")
			}
		})
	}
}

func TestNewBindingRequiresBothHalves(t *testing.T) {
	if _, err := NewBinding(nil, testDictionary()); err == nil {
		t.Error("NewBinding() accepted a nil snapshot")
	}
	if _, err := NewBinding(testSnapshot(), nil); err == nil {
		t.Error("NewBinding() accepted a nil dictionary")
	}
}
