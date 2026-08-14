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
	"os"
	"path/filepath"
	"testing"
)

// WriteFoundry creates a foundry tree in a temporary directory and returns its
// root. Command-level tests use it so that they exercise the real path from YAML
// on disk to rows in PostgreSQL.
func WriteFoundry(t *testing.T, ontologyName string) string {
	t.Helper()

	root := t.TempDir()

	writeFile(t, filepath.Join(root, "foundry.yaml"), `apiVersion: fab/v1
kind: Foundry
metadata:
  name: `+ontologyName+`
spec:
  layers:
    - name: meta-core
      version: ">=1.0.0"
`)

	writeFile(t, filepath.Join(root, "layers", "meta-core", "schema", "objects", "user.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: User
  description: A person who can sign in.
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
    - name: email
      type: string
      unique: true
`)

	writeFile(t, filepath.Join(root, "schema", "objects", "customer.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Customer
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
    - name: email
      type: string
      unique: true
    - name: tier
      type: enum
      values: [free, pro, enterprise]
      indexed: true
`)

	writeFile(t, filepath.Join(root, "schema", "objects", "order.yaml"), `apiVersion: fab/v1
kind: ObjectType
metadata:
  name: Order
spec:
  primaryKey: id
  properties:
    - name: id
      type: string
    - name: total
      type: decimal
`)

	writeFile(t, filepath.Join(root, "schema", "links", "customer_orders.yaml"), `apiVersion: fab/v1
kind: LinkType
metadata:
  name: CustomerOrders
spec:
  source:
    type: Customer
  target:
    type: Order
  cardinality: one_to_many
  reverseName: customer
`)

	return root
}

// AddPropertyToCustomer appends a property to the app/Customer document, which
// is the smallest edit that changes the compiled digest.
func AddPropertyToCustomer(t *testing.T, root, property string) {
	t.Helper()

	path := filepath.Join(root, "schema", "objects", "customer.yaml")
	existing, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	updated := string(existing) + "    - name: " + property + "\n      type: string\n"
	writeFile(t, path, updated)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
