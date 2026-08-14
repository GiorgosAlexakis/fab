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
	"fmt"
	"net/http"
	"net/url"
	"sort"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore"
	"github.com/GiorgosAlexakis/fab/pkg/objectstore/api"
)

// reservedQueryParams are the query parameters that configure the request rather
// than filter it. Everything else is read as a property filter.
var reservedQueryParams = map[string]bool{
	"tag":     true,
	"version": true,
	"limit":   true,
	"offset":  true,
	"total":   true,
}

// list returns the objects of one type matching the query parameters.
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store(w, r)
	if !ok {
		return
	}

	query, err := parseQuery(r, store, qualifiedName(r, "type"))
	if err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}

	objects, err := store.List(r.Context(), query)
	if err != nil {
		writeObjectStoreError(w, err)
		return
	}

	list := api.ObjectList{Items: objects}
	withTotal, err := apiserver.QueryBool(r, "total")
	if err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}
	if withTotal {
		total, err := store.Count(r.Context(), query)
		if err != nil {
			writeObjectStoreError(w, err)
			return
		}
		list.Total = &total
	}
	apiserver.WriteJSON(w, http.StatusOK, list)
}

// traverse follows a named link from one object.
func (s *Server) traverse(w http.ResponseWriter, r *http.Request) {
	store, ok := s.store(w, r)
	if !ok {
		return
	}

	limit, err := apiserver.QueryInt(r, "limit", 0)
	if err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}
	offset, err := apiserver.QueryInt(r, "offset", 0)
	if err != nil {
		apiserver.WriteError(w, http.StatusBadRequest, api.ReasonInvalid, err)
		return
	}

	objects, err := store.Traverse(r.Context(), objectstore.TraverseRequest{
		From:      objectRef(r),
		Traversal: r.PathValue("traversal"),
		Limit:     limit,
		Offset:    offset,
	})
	if err != nil {
		writeObjectStoreError(w, err)
		return
	}
	apiserver.WriteJSON(w, http.StatusOK, api.ObjectList{Items: objects})
}

// parseQuery builds a query from the URL. Filter values arrive as strings, so
// they are parsed against the property's declared type: a filter on an integer
// property has to compare as an integer, not as the string a URL carries.
func parseQuery(r *http.Request, store BoundStore, typeName string) (objectstore.Query, error) {
	limit, err := apiserver.QueryInt(r, "limit", 0)
	if err != nil {
		return objectstore.Query{}, err
	}
	offset, err := apiserver.QueryInt(r, "offset", 0)
	if err != nil {
		return objectstore.Query{}, err
	}

	objectType, err := store.Binding().ObjectType(typeName)
	if err != nil {
		return objectstore.Query{}, err
	}

	filters, err := parseFilters(r.URL.Query(), objectType)
	if err != nil {
		return objectstore.Query{}, err
	}

	return objectstore.Query{Type: typeName, Filters: filters, Limit: limit, Offset: offset}, nil
}

// parseFilters reads the property filters out of a query string, in a stable
// order so that two identical requests produce the same SQL.
func parseFilters(query url.Values, objectType *objectstore.ObjectType) ([]objectstore.Filter, error) {
	names := make([]string, 0, len(query))
	for name := range query {
		if !reservedQueryParams[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	filters := make([]objectstore.Filter, 0, len(names))
	for _, name := range names {
		property, ok := objectType.Property(name)
		if !ok {
			return nil, fmt.Errorf("%s has no property %q: %w",
				objectType.QualifiedName, name, objectstore.ErrUnknownProperty)
		}
		value, err := objectstore.ParseValue(property, query.Get(name))
		if err != nil {
			return nil, err
		}
		filters = append(filters, objectstore.Filter{Property: name, Value: value})
	}
	return filters, nil
}
