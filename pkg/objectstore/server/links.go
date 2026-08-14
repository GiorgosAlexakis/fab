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
	"errors"
	"net/http"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore/api"
)

// link connects two objects. It is a PUT because linking an already linked pair
// is a no-op: the request states the edge should exist, not that it is new.
func (s *Server) link(w http.ResponseWriter, r *http.Request) {
	request, ok := s.linkRequest(w, r)
	if !ok {
		return
	}

	store, ok := s.store(w, r)
	if !ok {
		return
	}

	if err := store.Link(r.Context(), request); err != nil {
		writeObjectStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// unlink disconnects two objects, leaving both of them in place.
func (s *Server) unlink(w http.ResponseWriter, r *http.Request) {
	request, ok := s.linkRequest(w, r)
	if !ok {
		return
	}

	store, ok := s.store(w, r)
	if !ok {
		return
	}

	if err := store.Unlink(r.Context(), request); err != nil {
		writeObjectStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// linkRequest decodes the two ends of a link, reporting the failure itself.
func (s *Server) linkRequest(w http.ResponseWriter, r *http.Request) (objectstore.LinkRequest, bool) {
	var body api.LinkBody
	if err := apiserver.DecodeJSON(r, &body); err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return objectstore.LinkRequest{}, false
	}
	if body.Source.Type == "" || body.Source.PrimaryKey == "" {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("source.type and source.primaryKey are required"))
		return objectstore.LinkRequest{}, false
	}
	if body.Target.Type == "" || body.Target.PrimaryKey == "" {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("target.type and target.primaryKey are required"))
		return objectstore.LinkRequest{}, false
	}

	return objectstore.LinkRequest{
		Link:   qualifiedName(r, "link"),
		Source: body.Source,
		Target: body.Target,
	}, true
}
