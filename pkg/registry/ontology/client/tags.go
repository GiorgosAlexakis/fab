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

// Resolve returns the version a tag points at.
func (c *Client) Resolve(ctx context.Context, name, tag string) (*registry.Ontology, error) {
	resolved := &registry.Ontology{}
	if err := c.do(ctx, http.MethodGet, tagPath(name, tag), nil, resolved); err != nil {
		return nil, err
	}
	return resolved, nil
}

// ResolveSnapshot returns the compiled ontology a tag points at.
func (c *Client) ResolveSnapshot(ctx context.Context, name, tag string) (*snapshot.Snapshot, error) {
	compiled := &snapshot.Snapshot{}
	if err := c.do(ctx, http.MethodGet, tagPath(name, tag)+"/snapshot", nil, compiled); err != nil {
		return nil, err
	}
	return compiled, nil
}

// ResolveDictionary returns the stable identities of the version a tag points at.
func (c *Client) ResolveDictionary(ctx context.Context, name, tag string) (*registry.Dictionary, error) {
	dictionary := &registry.Dictionary{}
	if err := c.do(ctx, http.MethodGet, tagPath(name, tag)+"/dictionary", nil, dictionary); err != nil {
		return nil, err
	}
	return dictionary, nil
}

// Tag points a tag at a published version.
func (c *Client) Tag(ctx context.Context, name, tag, version string) (*registry.Ontology, error) {
	tagged := &registry.Ontology{}
	body := api.TagRequest{Version: version}
	if err := c.do(ctx, http.MethodPut, tagPath(name, tag), body, tagged); err != nil {
		return nil, err
	}
	return tagged, nil
}

// Promote points toTag at whatever fromTag currently points at.
func (c *Client) Promote(ctx context.Context, name, fromTag, toTag string) (*registry.Ontology, error) {
	promoted := &registry.Ontology{}
	body := api.PromoteRequest{From: fromTag}
	if err := c.do(ctx, http.MethodPost, tagPath(name, toTag)+"/promote", body, promoted); err != nil {
		return nil, err
	}
	return promoted, nil
}

// Rollback returns a tag to the version it pointed at before its last move.
func (c *Client) Rollback(ctx context.Context, name, tag string) (*registry.Ontology, error) {
	rolledBack := &registry.Ontology{}
	if err := c.do(ctx, http.MethodPost, tagPath(name, tag)+"/rollback", nil, rolledBack); err != nil {
		return nil, err
	}
	return rolledBack, nil
}
