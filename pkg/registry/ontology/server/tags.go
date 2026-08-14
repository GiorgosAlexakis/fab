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
	"github.com/GiorgosAlexakis/fab/pkg/registry/ontology/api"
)

// resolve returns the version a tag points at. This is the request a running
// service makes at startup, and the one it makes again when a promotion moves
// the tag under it.
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	resolved, err := s.registry.Resolve(r.Context(), r.PathValue("name"), r.PathValue("tag"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, resolved)
}

// resolveSnapshot returns the compiled ontology a tag points at.
func (s *Server) resolveSnapshot(w http.ResponseWriter, r *http.Request) {
	compiled, err := s.registry.ResolveSnapshot(r.Context(), r.PathValue("name"), r.PathValue("tag"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, compiled)
}

// resolveDictionary returns the stable identities of the version a tag points
// at, which is what the object store binds itself with.
func (s *Server) resolveDictionary(w http.ResponseWriter, r *http.Request) {
	dictionary, err := s.registry.ResolveDictionary(r.Context(), r.PathValue("name"), r.PathValue("tag"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, dictionary)
}

// tag points a tag at a published version.
func (s *Server) tag(w http.ResponseWriter, r *http.Request) {
	var request api.TagRequest
	if err := apiserver.DecodeJSON(r, &request); err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}
	if request.Version == "" {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("version is required"))
		return
	}

	tagged, err := s.registry.Tag(r.Context(), r.PathValue("name"), r.PathValue("tag"), request.Version)
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, tagged)
}

// promote points the tag in the path at whatever the source tag points at.
func (s *Server) promote(w http.ResponseWriter, r *http.Request) {
	var request api.PromoteRequest
	if err := apiserver.DecodeJSON(r, &request); err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}
	if request.From == "" {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid,
			errors.New("from is required"))
		return
	}

	promoted, err := s.registry.Promote(r.Context(), r.PathValue("name"), request.From, r.PathValue("tag"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, promoted)
}

// rollback returns a tag to the version it pointed at before its last move.
func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	rolledBack, err := s.registry.Rollback(r.Context(), r.PathValue("name"), r.PathValue("tag"))
	if err != nil {
		writeRegistryError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, rolledBack)
}
