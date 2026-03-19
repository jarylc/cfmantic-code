SHELL := /bin/bash

GO := go
CGO_ENABLED ?= 1
export CGO_ENABLED
BINARY := bin/cfmantic-code
VERSION ?= $(shell git describe --tags --always --dirty --match 'v*' 2>/dev/null || echo 0.1.0)
BUILD_VERSION := $(patsubst v%,%,$(VERSION))
LDFLAGS := -s -w -X cfmantic-code/internal/config.buildVersion=$(BUILD_VERSION)

.PHONY: help init install-tools mocks generate build run test test-race lint lint-fix fmt vet pre-commit clean

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'

init: install-tools generate ## Install tools and generate all files

install-tools: ## Install Go tooling
	$(GO) get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

mocks: ## Generate mock implementations for interfaces
	$(GO) run github.com/vektra/mockery/v3@latest

generate: mocks ## Generate mocks

build: ## Build the Go binary with CGO
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .

run: build ## Build then run the binary
	./$(BINARY)

test: mocks ## Run all tests
	$(GO) test ./...

test-race: mocks ## Run all tests with the race detector
	$(GO) test -race ./...

lint: install-tools ## Run golangci-lint
	$(GO) tool golangci-lint run ./...

lint-fix: install-tools ## Auto-fix lint and formatting issues
	-$(GO) fix ./...
	-$(GO) tool golangci-lint run --fix ./...

fmt: install-tools ## Format Go sources (gofumpt, goimports)
	$(GO) tool golangci-lint fmt ./...

vet: ## Run go vet
	$(GO) vet ./...

pre-commit: generate fmt lint-fix vet test ## Run all checks: generate, format, lint, vet, and test

clean: ## Remove build artifacts and generated mocks
	rm -rf bin/* internal/mocks/*
