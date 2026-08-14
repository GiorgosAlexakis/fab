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

// Package client reaches the ontology registry over its HTTP API.
//
// It implements registry.Interface, so a caller cannot tell whether it holds a
// client or the PostgreSQL store: the same methods, the same errors. That is
// what lets the CLI talk to a server while the registry's own tests exercise the
// store directly.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GiorgosAlexakis/fab/pkg/apiserver"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/registry/ontology/api"
)

// DefaultTimeout bounds a single request. Publishing a large ontology is the
// slowest call, and it is still a single transaction.
const DefaultTimeout = 60 * time.Second

// Client is a registry reached over HTTP.
type Client struct {
	baseURL *url.URL
	http    *http.Client
}

var _ registry.Interface = &Client{}

// New returns a client for the registry server at baseURL.
func New(baseURL string) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("the registry URL is required")
	}
	if !strings.Contains(baseURL, "://") {
		baseURL = "http://" + baseURL
	}
	parsed, err := url.Parse(strings.TrimSuffix(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parsing the registry URL %q: %w", baseURL, err)
	}
	return &Client{baseURL: parsed, http: &http.Client{Timeout: DefaultTimeout}}, nil
}

// WithHTTPClient returns a copy of the client using the given HTTP client, which
// is how a test points it at an httptest server.
func (c *Client) WithHTTPClient(httpClient *http.Client) *Client {
	copied := *c
	copied.http = httpClient
	return &copied
}

// do sends a request and decodes the response into out, which may be nil.
func (c *Client) do(ctx context.Context, method, path string, body, out interface{}) error {
	var payload io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding the request body: %w", err)
		}
		payload = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL.String()+path, payload)
	if err != nil {
		return fmt.Errorf("building the request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("reaching the ontology registry at %s: %w", c.baseURL, err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode >= http.StatusBadRequest {
		return api.ErrorFromStatus(decodeStatus(response))
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding the response from %s %s: %w", method, path, err)
	}
	return nil
}

// decodeStatus reads an error body, falling back to the HTTP status when the
// body is not a Status: a proxy or a misrouted request can answer with anything.
func decodeStatus(response *http.Response) apiserver.Status {
	body, err := io.ReadAll(io.LimitReader(response.Body, apiserver.MaxRequestBodyBytes))
	if err != nil {
		return apiserver.Status{Reason: api.ReasonInternal, Message: err.Error()}
	}

	var status apiserver.Status
	if json.Unmarshal(body, &status) == nil && status.Reason != "" {
		return status
	}
	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	return apiserver.Status{Reason: api.ReasonInternal, Message: message}
}

// ontologyPath is the base path of one ontology's resources.
func ontologyPath(name string) string {
	return "/v1/ontologies/" + url.PathEscape(name)
}

// versionPath is the base path of one version's resources.
func versionPath(name, version string) string {
	return ontologyPath(name) + "/versions/" + url.PathEscape(version)
}

// tagPath is the base path of one tag's resources.
func tagPath(name, tag string) string {
	return ontologyPath(name) + "/tags/" + url.PathEscape(tag)
}
