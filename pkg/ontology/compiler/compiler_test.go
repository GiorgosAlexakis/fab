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

package compiler

import (
	"strings"
	"testing"

	ontologyv1 "github.com/GiorgosAlexakis/fab/pkg/apis/ontology/v1"
	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
)

func objectType(layer, name string, properties ...ontologyv1.Property) *ontologyv1.ObjectType {
	if len(properties) == 0 {
		properties = []ontologyv1.Property{{Name: "id", Type: ontologyv1.PropertyTypeString}}
	}
	return &ontologyv1.ObjectType{
		Metadata: ontologyv1.ObjectMeta{Name: name, Layer: layer},
		Spec: ontologyv1.ObjectTypeSpec{
			PrimaryKey: properties[0].Name,
			Properties: properties,
		},
	}
}

func linkType(layer, name string, source, target ontologyv1.TypeReference, mutate func(*ontologyv1.LinkType)) *ontologyv1.LinkType {
	link := &ontologyv1.LinkType{
		Metadata: ontologyv1.ObjectMeta{Name: name, Layer: layer},
		Spec: ontologyv1.LinkTypeSpec{
			Source:      source,
			Target:      target,
			Cardinality: ontologyv1.CardinalityOneToMany,
		},
	}
	if mutate != nil {
		mutate(link)
	}
	return link
}

func typeRef(layer, name string) ontologyv1.TypeReference {
	return ontologyv1.TypeReference{Layer: layer, Type: name}
}

// coreLayer is a stand-in for meta-core: types other layers link to.
func coreLayer() LayerSource {
	return LayerSource{
		Layer: "meta-core",
		Documents: []Document{
			{Source: "layers/meta-core/schema/objects/user.yaml", Object: objectType("meta-core", "User",
				ontologyv1.Property{Name: "id", Type: ontologyv1.PropertyTypeString},
				ontologyv1.Property{Name: "email", Type: ontologyv1.PropertyTypeString},
			)},
			{Source: "layers/meta-core/schema/objects/organization.yaml", Object: objectType("meta-core", "Organization")},
		},
	}
}

func TestCompileMergesLayers(t *testing.T) {
	appLayer := LayerSource{
		Layer: "app",
		Documents: []Document{
			{Source: "schema/objects/customer.yaml", Object: objectType("app", "Customer")},
			{Source: "schema/objects/order.yaml", Object: objectType("app", "Order")},
			{Source: "schema/links/customer_orders.yaml", Object: linkType("app", "CustomerOrders",
				typeRef("app", "Customer"), typeRef("app", "Order"), nil)},
		},
	}

	compiled, err := Compile([]LayerSource{coreLayer(), appLayer})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	if want := []string{"meta-core", "app"}; !equalStrings(compiled.Layers, want) {
		t.Errorf("Layers = %v, want %v (merge order must be preserved)", compiled.Layers, want)
	}
	if len(compiled.ObjectTypes) != 4 {
		t.Errorf("got %d object types, want 4", len(compiled.ObjectTypes))
	}
	if len(compiled.LinkTypes) != 1 {
		t.Errorf("got %d link types, want 1", len(compiled.LinkTypes))
	}

	// Normalize sorts by qualified name so that file order cannot leak into the
	// digest.
	wantOrder := []string{"app/Customer", "app/Order", "meta-core/Organization", "meta-core/User"}
	var gotOrder []string
	for i := range compiled.ObjectTypes {
		gotOrder = append(gotOrder, compiled.ObjectTypes[i].QualifiedName())
	}
	if !equalStrings(gotOrder, wantOrder) {
		t.Errorf("object type order = %v, want %v", gotOrder, wantOrder)
	}

	link, ok := compiled.LinkType("app", "CustomerOrders")
	if !ok {
		t.Fatal("compiled snapshot is missing app/CustomerOrders")
	}
	if link.ForwardName != "customer_orders" || link.ReverseName != "customer" {
		t.Errorf("traversal names = %q/%q, want customer_orders/customer", link.ForwardName, link.ReverseName)
	}
	if link.OnSourceDelete != string(ontologyv1.DeletePolicyRestrict) {
		t.Errorf("OnSourceDelete = %q, want restrict", link.OnSourceDelete)
	}
}

func TestCompileCrossLayerLink(t *testing.T) {
	billingLayer := LayerSource{
		Layer: "meta-billing",
		Documents: []Document{
			{Source: "layers/meta-billing/schema/objects/subscription.yaml",
				Object: objectType("meta-billing", "Subscription")},
			{Source: "layers/meta-billing/schema/links/org_subscription.yaml",
				Object: linkType("meta-billing", "OrganizationSubscription",
					typeRef("meta-core", "Organization"), typeRef("meta-billing", "Subscription"),
					func(link *ontologyv1.LinkType) {
						link.Spec.Cardinality = ontologyv1.CardinalityOneToOne
					})},
		},
	}

	if _, err := Compile([]LayerSource{coreLayer(), billingLayer}); err != nil {
		t.Fatalf("Compile() failed on a valid cross-layer link: %v", err)
	}
}

func TestCompileErrors(t *testing.T) {
	testCases := []struct {
		name        string
		layers      []LayerSource
		wantMessage string
	}{
		{
			name: "a layer may not define a type in another layer's namespace",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/user.yaml", Object: objectType("meta-core", "User")},
				},
			}},
			wantMessage: "metadata.layer is \"meta-core\"",
		},
		{
			name: "duplicate object type",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/customer.yaml", Object: objectType("app", "Customer")},
					{Source: "schema/objects/customer_copy.yaml", Object: objectType("app", "Customer")},
				},
			}},
			wantMessage: "already defined in schema/objects/customer.yaml",
		},
		{
			name: "duplicate layer",
			layers: []LayerSource{
				{Layer: "app", Documents: []Document{{Source: "a.yaml", Object: objectType("app", "Customer")}}},
				{Layer: "app", Documents: []Document{{Source: "b.yaml", Object: objectType("app", "Order")}}},
			},
			wantMessage: "declared more than once",
		},
		{
			name: "link to a type from an inactive layer",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/customer.yaml", Object: objectType("app", "Customer")},
					{Source: "schema/links/customer_sessions.yaml", Object: linkType("app", "CustomerSessions",
						typeRef("app", "Customer"), typeRef("meta-auth", "Session"), nil)},
				},
			}},
			wantMessage: "unknown target object type meta-auth/Session",
		},
		{
			name: "forward traversal collides with a property",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/customer.yaml", Object: objectType("app", "Customer",
						ontologyv1.Property{Name: "id", Type: ontologyv1.PropertyTypeString},
						ontologyv1.Property{Name: "customer_orders", Type: ontologyv1.PropertyTypeString},
					)},
					{Source: "schema/objects/order.yaml", Object: objectType("app", "Order")},
					{Source: "schema/links/customer_orders.yaml", Object: linkType("app", "CustomerOrders",
						typeRef("app", "Customer"), typeRef("app", "Order"), nil)},
				},
			}},
			wantMessage: "forwardName \"customer_orders\" on app/Customer collides with a property",
		},
		{
			name: "two links claim the same traversal name",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/customer.yaml", Object: objectType("app", "Customer")},
					{Source: "schema/objects/order.yaml", Object: objectType("app", "Order")},
					{Source: "schema/objects/invoice.yaml", Object: objectType("app", "Invoice")},
					{Source: "schema/links/customer_orders.yaml", Object: linkType("app", "CustomerOrders",
						typeRef("app", "Customer"), typeRef("app", "Order"), nil)},
					{Source: "schema/links/customer_invoices.yaml", Object: linkType("app", "CustomerInvoices",
						typeRef("app", "Customer"), typeRef("app", "Invoice"),
						func(link *ontologyv1.LinkType) {
							link.Spec.ForwardName = "customer_orders"
						})},
				},
			}},
			wantMessage: "collides with forward traversal of link app/CustomerOrders",
		},
		{
			name: "invalid document",
			layers: []LayerSource{{
				Layer: "app",
				Documents: []Document{
					{Source: "schema/objects/customer.yaml", Object: objectType("app", "customer")},
				},
			}},
			wantMessage: "metadata.name",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := Compile(testCase.layers)
			if err == nil {
				t.Fatalf("Compile() succeeded, want an error mentioning %q", testCase.wantMessage)
			}
			if !strings.Contains(err.Error(), testCase.wantMessage) {
				t.Fatalf("Compile() error = %v, want it to mention %q", err, testCase.wantMessage)
			}
		})
	}
}

func TestCompileCollectsAllErrors(t *testing.T) {
	layers := []LayerSource{{
		Layer: "app",
		Documents: []Document{
			{Source: "schema/objects/customer.yaml", Object: objectType("app", "customer")},
			{Source: "schema/objects/order.yaml", Object: objectType("app", "order")},
		},
	}}

	_, err := Compile(layers)
	if err == nil {
		t.Fatal("Compile() succeeded, want errors for both documents")
	}
	for _, want := range []string{"customer.yaml", "order.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

// TestCompileDigestIsOrderIndependent is the guarantee the sstate cache and
// `fab schema publish` both rely on: the same schema content compiles to the
// same digest regardless of how the documents were arranged on disk.
func TestCompileDigestIsOrderIndependent(t *testing.T) {
	properties := []ontologyv1.Property{
		{Name: "id", Type: ontologyv1.PropertyTypeString},
		{Name: "email", Type: ontologyv1.PropertyTypeString},
		{Name: "tier", Type: ontologyv1.PropertyTypeEnum, Values: []string{"free", "pro"}},
	}
	reordered := []ontologyv1.Property{properties[0], properties[2], properties[1]}

	first, err := Compile([]LayerSource{{
		Layer: "app",
		Documents: []Document{
			{Source: "a.yaml", Object: objectType("app", "Customer", properties...)},
			{Source: "b.yaml", Object: objectType("app", "Order")},
		},
	}})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	second, err := Compile([]LayerSource{{
		Layer: "app",
		Documents: []Document{
			{Source: "b.yaml", Object: objectType("app", "Order")},
			{Source: "a.yaml", Object: objectType("app", "Customer", reordered...)},
		},
	}})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	firstDigest, err := first.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	secondDigest, err := second.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	if firstDigest != secondDigest {
		t.Errorf("digest changed with document order: %s != %s", firstDigest, secondDigest)
	}
}

func TestCompileDigestChangesWithContent(t *testing.T) {
	base, err := Compile([]LayerSource{{
		Layer:     "app",
		Documents: []Document{{Source: "a.yaml", Object: objectType("app", "Customer")}},
	}})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}
	changed, err := Compile([]LayerSource{{
		Layer: "app",
		Documents: []Document{{Source: "a.yaml", Object: objectType("app", "Customer",
			ontologyv1.Property{Name: "id", Type: ontologyv1.PropertyTypeString},
			ontologyv1.Property{Name: "email", Type: ontologyv1.PropertyTypeString},
		)}},
	}})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	baseDigest, err := base.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	changedDigest, err := changed.Digest()
	if err != nil {
		t.Fatalf("Digest() failed: %v", err)
	}
	if baseDigest == changedDigest {
		t.Error("adding a property did not change the digest")
	}
}

func TestSnapshotLookups(t *testing.T) {
	compiled, err := Compile([]LayerSource{coreLayer()})
	if err != nil {
		t.Fatalf("Compile() failed: %v", err)
	}

	user, ok := compiled.ObjectType("meta-core", "User")
	if !ok {
		t.Fatal("ObjectType(meta-core, User) not found")
	}
	if _, ok := user.Property("email"); !ok {
		t.Error("Property(email) not found on meta-core/User")
	}
	if _, ok := user.Property("nope"); ok {
		t.Error("Property(nope) unexpectedly found")
	}
	if got := user.QualifiedName(); got != snapshot.QualifiedName("meta-core", "User") {
		t.Errorf("QualifiedName() = %q", got)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
