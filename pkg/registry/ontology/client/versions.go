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

package client

import (
	"context"
	"net/http"

	"github.com/GiorgosAlexakis/fab/pkg/ontology/snapshot"
	registry "github.com/GiorgosAlexakis/fab/pkg/registry/ontology"
	"github.com/GiorgosAlexakis/fab/pkg/registry/ontology/api"
)

// Publish stores a compiled snapshot as a version.
func (c *Client) Publish(ctx context.Context, request registry.PublishRequest) (*registry.Ontology, error) {
	body := api.PublishRequest{
		Version:  request.Version,
		GitRef:   request.GitRef,
		Draft:    request.Draft,
		Snapshot: request.Snapshot,
	}
	published := &registry.Ontology{}
	if err := c.do(ctx, http.MethodPost, ontologyPath(request.Name)+"/versions", body, published); err != nil {
		return nil, err
	}
	return published, nil
}

// Get returns the metadata of one version.
func (c *Client) Get(ctx context.Context, name, version string) (*registry.Ontology, error) {
	found := &registry.Ontology{}
	if err := c.do(ctx, http.MethodGet, versionPath(name, version), nil, found); err != nil {
		return nil, err
	}
	return found, nil
}

// GetSnapshot returns the compiled ontology of one version.
func (c *Client) GetSnapshot(ctx context.Context, name, version string) (*snapshot.Snapshot, error) {
	compiled := &snapshot.Snapshot{}
	if err := c.do(ctx, http.MethodGet, versionPath(name, version)+"/snapshot", nil, compiled); err != nil {
		return nil, err
	}
	return compiled, nil
}

// List returns every version of an ontology, newest first.
func (c *Client) List(ctx context.Context, name string) ([]registry.Ontology, error) {
	list := &api.VersionList{}
	if err := c.do(ctx, http.MethodGet, ontologyPath(name)+"/versions", nil, list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// Deprecate marks a published version as no longer recommended.
func (c *Client) Deprecate(ctx context.Context, name, version string) (*registry.Ontology, error) {
	deprecated := &registry.Ontology{}
	if err := c.do(ctx, http.MethodPost, versionPath(name, version)+"/deprecate", nil, deprecated); err != nil {
		return nil, err
	}
	return deprecated, nil
}

// Dictionary returns the stable type and property identities of a version.
func (c *Client) Dictionary(ctx context.Context, name, version string) (*registry.Dictionary, error) {
	dictionary := &registry.Dictionary{}
	if err := c.do(ctx, http.MethodGet, versionPath(name, version)+"/dictionary", nil, dictionary); err != nil {
		return nil, err
	}
	return dictionary, nil
}
