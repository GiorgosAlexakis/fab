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
	"errors"
	"sync"
	"time"

	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	objectstorepostgres "github.com/GiorgosAlexakis/fab/pkg/objectstore/postgres"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/storage"
)

// DefaultBindingTTL is how long a resolved ontology version is reused before the
// registry is asked again.
//
// A tag is mutable: promoting a version moves it, and the store has to notice
// without a restart. Re-resolving on every request would make the registry a
// dependency of every write, so the binding is cached for a short while instead.
// The plan's end state is LISTEN/NOTIFY invalidation on the tag table; a TTL is
// the same behaviour with a bounded delay rather than an immediate one.
const DefaultBindingTTL = 30 * time.Second

// PostgresResolver binds the object store database to the ontology versions the
// registry serves.
type PostgresResolver struct {
	db         storage.Beginner
	registry   registry.Interface
	name       string
	defaultTag string
	ttl        time.Duration

	mutex sync.Mutex
	bound map[Selector]*binding
}

var _ Resolver = &PostgresResolver{}

// binding is one cached ontology version.
type binding struct {
	store   BoundStore
	expires time.Time
}

// NewPostgresResolver returns a resolver for one object store database and one
// ontology. The default tag is used by requests that do not select a version.
func NewPostgresResolver(db storage.Beginner, reg registry.Interface, name, defaultTag string,
	ttl time.Duration) (*PostgresResolver, error) {
	if db == nil {
		return nil, errors.New("an object store database is required")
	}
	if reg == nil {
		return nil, errors.New("an ontology registry is required")
	}
	if name == "" {
		return nil, errors.New("an ontology name is required")
	}
	if defaultTag == "" {
		return nil, errors.New("a default ontology tag is required")
	}
	if ttl <= 0 {
		ttl = DefaultBindingTTL
	}

	return &PostgresResolver{
		db:         db,
		registry:   reg,
		name:       name,
		defaultTag: defaultTag,
		ttl:        ttl,
		bound:      map[Selector]*binding{},
	}, nil
}

// Store returns a store bound to the selected ontology version, resolving it
// through the registry when the cached binding is missing or stale.
func (r *PostgresResolver) Store(ctx context.Context, selector Selector) (BoundStore, error) {
	if selector.Version == "" && selector.Tag == "" {
		selector.Tag = r.defaultTag
	}
	if selector.Version != "" {
		selector.Tag = ""
	}

	if cached := r.cached(selector); cached != nil {
		return cached, nil
	}

	bound, err := r.bind(ctx, selector)
	if err != nil {
		return nil, err
	}

	store := objectstorepostgres.New(r.db, bound)
	r.remember(selector, store)
	return store, nil
}

// Ontology reports the version the default tag currently points at, which is
// what readiness depends on: a store whose ontology cannot be resolved cannot
// serve a request.
func (r *PostgresResolver) Ontology(ctx context.Context) (registry.Ontology, error) {
	store, err := r.Store(ctx, Selector{})
	if err != nil {
		return registry.Ontology{}, err
	}
	return store.Binding().Ontology(), nil
}

func (r *PostgresResolver) cached(selector Selector) BoundStore {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	found, ok := r.bound[selector]
	if !ok || time.Now().After(found.expires) {
		return nil
	}
	return found.store
}

func (r *PostgresResolver) remember(selector Selector, store BoundStore) {
	r.mutex.Lock()
	defer r.mutex.Unlock()

	r.bound[selector] = &binding{store: store, expires: time.Now().Add(r.ttl)}
}

func (r *PostgresResolver) bind(ctx context.Context, selector Selector) (*objectstore.Binding, error) {
	if selector.Version != "" {
		return objectstore.BindVersion(ctx, r.registry, r.name, selector.Version)
	}
	return objectstore.BindTag(ctx, r.registry, r.name, selector.Tag)
}
