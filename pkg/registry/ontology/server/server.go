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

// Package server serves the ontology registry over HTTP.
//
// It is a thin layer over registry.Interface: the routes are the interface's
// methods, and the handlers do no work of their own beyond decoding a request
// and mapping an error onto a status code. Everything that decides what a
// publish or a promotion means lives in the registry implementation, so the
// same rules apply whether it is reached in process or over the network.
package server

import (
	"context"
	"errors"
	"net/http"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/registry/ontology/api"
)

// Server routes registry requests to a registry implementation.
type Server struct {
	registry registry.Interface
	ready    func(ctx context.Context) error
	mux      *http.ServeMux
}

// New returns a server backed by the given registry. The ready function reports
// whether the backing store can be reached, and may be nil.
func New(reg registry.Interface, ready func(ctx context.Context) error) *Server {
	s := &Server{registry: reg, ready: ready, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	apiserver.RegisterHealth(s.mux, s.ready)

	s.mux.HandleFunc("POST /v1/ontologies/{name}/versions", s.publish)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/versions", s.list)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/versions/{version}", s.get)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/versions/{version}/snapshot", s.getSnapshot)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/versions/{version}/dictionary", s.getDictionary)
	s.mux.HandleFunc("POST /v1/ontologies/{name}/versions/{version}/deprecate", s.deprecate)

	s.mux.HandleFunc("GET /v1/ontologies/{name}/tags/{tag}", s.resolve)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/tags/{tag}/snapshot", s.resolveSnapshot)
	s.mux.HandleFunc("GET /v1/ontologies/{name}/tags/{tag}/dictionary", s.resolveDictionary)
	s.mux.HandleFunc("PUT /v1/ontologies/{name}/tags/{tag}", s.tag)
	s.mux.HandleFunc("POST /v1/ontologies/{name}/tags/{tag}/promote", s.promote)
	s.mux.HandleFunc("POST /v1/ontologies/{name}/tags/{tag}/rollback", s.rollback)
}

// publish stores a compiled snapshot as a version.
func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	var request api.PublishRequest
	if err := apiserver.DecodeJSON(r, &request); err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}
	if request.Version == "" {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("version is required"))
		return
	}
	if request.Snapshot == nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("snapshot is required"))
		return
	}

	published, err := s.registry.Publish(r.Context(), registry.PublishRequest{
		Name:     r.PathValue("name"),
		Version:  request.Version,
		Snapshot: request.Snapshot,
		GitRef:   request.GitRef,
		Draft:    request.Draft,
	})
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, published)
}

// list returns every version of an ontology.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	versions, err := s.registry.List(r.Context(), r.PathValue("name"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, api.VersionList{Items: versions})
}

// get returns the metadata of one version.
func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	found, err := s.registry.Get(r.Context(), r.PathValue("name"), r.PathValue("version"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, found)
}

// getSnapshot returns the compiled ontology of one version.
func (s *Server) getSnapshot(w http.ResponseWriter, r *http.Request) {
	compiled, err := s.registry.GetSnapshot(r.Context(), r.PathValue("name"), r.PathValue("version"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, compiled)
}

// getDictionary returns the stable identities of one version.
func (s *Server) getDictionary(w http.ResponseWriter, r *http.Request) {
	dictionary, err := s.registry.Dictionary(r.Context(), r.PathValue("name"), r.PathValue("version"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, dictionary)
}

// deprecate marks a published version as no longer recommended.
func (s *Server) deprecate(w http.ResponseWriter, r *http.Request) {
	deprecated, err := s.registry.Deprecate(r.Context(), r.PathValue("name"), r.PathValue("version"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, deprecated)
}

// writeRegistryError maps a registry error onto a status code and a reason.
func writeRegistryError(w http.ResponseWriter, err error) {
	code, reason := api.StatusForError(err)
	apiserver.WriteError(w, code, reason, err)
}
