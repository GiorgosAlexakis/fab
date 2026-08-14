# Copyright The FAB Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

SHELL := /usr/bin/env bash

GO ?= go
BIN_DIR ?= bin

# The CLI, plus the internal ontology registry server.
BINARIES ?= fab ontology-registry

GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_TREE_STATE ?= $(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo dirty || echo clean)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0-dev)

VERSION_PKG := github.com/GiorgosAlexakis/fab/pkg/version
LDFLAGS := -X $(VERSION_PKG).gitVersion=$(VERSION) \
	-X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).gitTreeState=$(GIT_TREE_STATE) \
	-X $(VERSION_PKG).buildDate=$(BUILD_DATE)

.PHONY: all
all: verify build test

.PHONY: build
build: ## Build the CLI and the registry server into $(BIN_DIR).
	for binary in $(BINARIES); do \
		$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$$binary ./cmd/$$binary || exit 1; \
	done

.PHONY: test
test: ## Run unit tests.
	$(GO) test ./... $(TESTFLAGS)

.PHONY: test-integration
test-integration: ## Run integration tests against a live PostgreSQL (needs FAB_TEST_POSTGRES_URL).
	$(GO) test -tags=integration -count=1 ./test/integration/... $(TESTFLAGS)

.PHONY: verify
verify: verify-gofmt verify-boilerplate vet ## Run all static checks.

.PHONY: verify-gofmt
verify-gofmt:
	./hack/verify-gofmt.sh

.PHONY: verify-boilerplate
verify-boilerplate:
	./hack/verify-boilerplate.sh

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
