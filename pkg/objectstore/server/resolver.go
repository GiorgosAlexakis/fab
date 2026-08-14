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

package server

import (
	"context"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
)

// Selector chooses the ontology version a request is served against.
type Selector struct {
	// Tag is an environment tag, e.g. "prod". Empty means the server default.
	Tag string
	// Version pins an exact version, which wins over Tag.
	Version string
}

// BoundStore is an object store together with the ontology it is bound to.
type BoundStore interface {
	objectstore.Interface
	// Binding returns the ontology version the store reads and writes with.
	Binding() *objectstore.Binding
}

// Resolver returns the store to serve a request with.
//
// The server takes a Resolver rather than a store because the ontology version in
// force is not fixed for the lifetime of the process: a tag moves, and the next
// request has to be served against what it points at now.
type Resolver interface {
	// Store returns an object store bound to the selected ontology version.
	Store(ctx context.Context, selector Selector) (BoundStore, error)
}
