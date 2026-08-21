SHELL := /usr/bin/env bash

GO ?= go
BIN_DIR ?= bin

BINARIES ?= fab

GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
GIT_TREE_STATE ?= $(shell test -n "$$(git status --porcelain 2>/dev/null)" && echo dirty || echo clean)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo v0.0.0-dev)

VERSION_PKG := github.com/GiorgosAlexakis/fab/internal/version
LDFLAGS := -X $(VERSION_PKG).gitVersion=$(VERSION) \
	-X $(VERSION_PKG).gitCommit=$(GIT_COMMIT) \
	-X $(VERSION_PKG).gitTreeState=$(GIT_TREE_STATE) \
	-X $(VERSION_PKG).buildDate=$(BUILD_DATE)

.PHONY: all
all: verify build test

.PHONY: build
build: ## Build the CLI into $(BIN_DIR).
	for binary in $(BINARIES); do \
		$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/$$binary ./cmd/$$binary || exit 1; \
	done

.PHONY: install
install: ## Install the CLI into $(GOBIN), or $(GOPATH)/bin when GOBIN is unset.
	for binary in $(BINARIES); do \
		$(GO) install -ldflags '$(LDFLAGS)' ./cmd/$$binary || exit 1; \
	done

.PHONY: test
test: ## Run unit tests.
	$(GO) test ./... $(TESTFLAGS)

.PHONY: verify
verify: vet ## Run all static checks.

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: clean
clean:
	rm -rf $(BIN_DIR)

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
