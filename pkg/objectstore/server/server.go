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

// Package server serves the object store over HTTP.
//
// Every request is served against one ontology version: the one the request
// selects with ?tag= or ?version=, or the server's default tag. The version is
// resolved from the registry, never from the request body, so a client cannot
// write values the ontology does not allow by claiming a schema of its own.
package server

import (
	"context"
	"net/http"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore/api"
)

// Server routes object store requests to a resolved store.
type Server struct {
	resolver Resolver
	ready    func(ctx context.Context) error
	mux      *http.ServeMux
}

// New returns a server that resolves a store per request. The ready function
// reports whether the backing store can be reached, and may be nil.
func New(resolver Resolver, ready func(ctx context.Context) error) *Server {
	s := &Server{resolver: resolver, ready: ready, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	apiserver.RegisterHealth(s.mux, s.ready)

	s.mux.HandleFunc("GET /v1/ontology", s.ontology)

	s.mux.HandleFunc("POST /v1/objects/{layer}/{type}", s.put)
	s.mux.HandleFunc("GET /v1/objects/{layer}/{type}", s.list)
	s.mux.HandleFunc("GET /v1/objects/{layer}/{type}/{primaryKey}", s.get)
	s.mux.HandleFunc("DELETE /v1/objects/{layer}/{type}/{primaryKey}", s.delete)
	s.mux.HandleFunc("GET /v1/objects/{layer}/{type}/{primaryKey}/links/{traversal}", s.traverse)

	s.mux.HandleFunc("PUT /v1/links/{layer}/{link}", s.link)
	s.mux.HandleFunc("DELETE /v1/links/{layer}/{link}", s.unlink)
}

// ontology reports which ontology version a request would be served with.
func (s *Server) ontology(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store(w, r)
	if !ok {
		return
	}
	binding := store.Binding()
	apiserver.WriteJSON(w, http.StatusOK, api.OntologyInfo{
		Ontology:    binding.Ontology(),
		ObjectTypes: binding.ObjectTypeNames(),
		LinkTypes:   binding.LinkTypeNames(),
	})
}

// put creates or updates an object.
func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	var request api.PutRequest
	if err := apiserver.DecodeJSON(r, &request); err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}

	store, ok := s.store(w, r)
	if !ok {
		return
	}

	object, err := store.Put(r.Context(), objectstore.PutRequest{
		Type:       qualifiedName(r, "type"),
		Set:        request.Set,
		Remove:     request.Remove,
		CreateOnly: request.CreateOnly,
	})
	if err != nil {
		writeObjectStoreError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, object)
}

// get returns one object by type and primary key.
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store(w, r)
	if !ok {
		return
	}

	object, err := store.Get(r.Context(), objectRef(r))
	if err != nil {
		writeObjectStoreError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, object)
}

// delete removes an object, applying the delete policies of its link types.
func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store(w, r)
	if !ok {
		return
	}

	if err := store.Delete(r.Context(), objectRef(r)); err != nil {
		writeObjectStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// store resolves the store for this request, reporting the failure itself so
// that handlers stay linear.
func (s *Server) store(w http.ResponseWriter, r *http.Request) (BoundStore, bool) {
	query := r.URL.Query()
	store, err := s.resolver.Store(r.Context(), Selector{
		Tag:     query.Get("tag"),
		Version: query.Get("version"),
	})
	if err != nil {
		writeObjectStoreError(w, err)
		return nil, false
	}
	return store, true
}

// qualifiedName rebuilds a layer-qualified name from the path, which carries the
// layer and the name as separate segments.
func qualifiedName(r *http.Request, nameSegment string) string {
	return r.PathValue("layer") + "/" + r.PathValue(nameSegment)
}

// objectRef reads the object a request addresses.
func objectRef(r *http.Request) objectstore.Ref {
	return objectstore.Ref{Type: qualifiedName(r, "type"), PrimaryKey: r.PathValue("primaryKey")}
}

// writeObjectStoreError maps an object store error onto a status code and reason.
func writeObjectStoreError(w http.ResponseWriter, err error) {
	code, reason := api.StatusForError(err)
	apiserver.WriteError(w, code, reason, err)
}
