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

// Package apiserver holds the HTTP plumbing the internal ontology servers
// share: JSON encoding, the error body their clients decode, and a server
// lifecycle that shuts down without dropping requests.
//
// These servers are internal infrastructure, not fab services. A fab service is
// declared in a layer, generated from proto and packaged into an assembly; the
// registry and the object store are the substrate those services run against, so
// they are plain Go binaries with a plain JSON API.
package apiserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// MaxRequestBodyBytes bounds a request body. A published ontology is the largest
// thing any of these servers accept, and 8 MiB is far above a realistic one.
const MaxRequestBodyBytes = 8 << 20

// Status is the body of every error response. Clients match on Reason, which is
// stable, rather than on Message, which is for humans.
type Status struct {
	// Reason is the machine-readable cause, e.g. "NotFound".
	Reason string `json:"reason"`
	// Message describes the failure.
	Message string `json:"message"`
}

// Error renders a Status as an error, so a client can return it as one.
func (s Status) Error() string {
	if s.Message == "" {
		return s.Reason
	}
	return s.Message
}

// WriteJSON writes a value as the response body.
func WriteJSON(w http.ResponseWriter, code int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if body == nil {
		return
	}
	// The header is already written, so a failure here cannot become a response
	// code. Encoding a value the handler built is not expected to fail.
	_ = json.NewEncoder(w).Encode(body)
}

// WriteError writes an error response carrying a machine-readable reason.
func WriteError(w http.ResponseWriter, code int, reason string, err error) {
	message := ""
	if err != nil {
		message = err.Error()
	}
	WriteJSON(w, code, Status{Reason: reason, Message: message})
}

// DecodeJSON reads a JSON request body into a value. Unknown fields are
// rejected: a client sending a field this server does not understand is a
// version skew that should surface as an error rather than as a silent default.
func DecodeJSON(r *http.Request, into interface{}) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, MaxRequestBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("reading the request body: %w", err)
	}
	return nil
}

// QueryInt reads a non-negative integer query parameter, returning fallback when
// it is absent.
func QueryInt(r *http.Request, name string, fallback int) (int, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %q", name, raw)
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must not be negative: %d", name, value)
	}
	return value, nil
}

// QueryBool reads a boolean query parameter. A bare parameter with no value,
// e.g. "?count", reads as true.
func QueryBool(r *http.Request, name string) (bool, error) {
	query := r.URL.Query()
	if !query.Has(name) {
		return false, nil
	}
	raw := query.Get(name)
	if raw == "" {
		return true, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean: %q", name, raw)
	}
	return value, nil
}

// StatusFromError returns the Status a server sent, if err came from a client
// that decoded one.
func StatusFromError(err error) (Status, bool) {
	var status Status
	if errors.As(err, &status) {
		return status, true
	}
	return Status{}, false
}
