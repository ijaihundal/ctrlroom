SHELL := /bin/bash
MODULE := github.com/ijaihundal/ctrlroom
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell git log -1 --format=%cI 2>/dev/null || echo unknown)
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION) -X $(MODULE)/internal/version.Commit=$(COMMIT) -X $(MODULE)/internal/version.Date=$(DATE)

.PHONY: all build run test test-race lint fmt tidy vet clean web web-dev web-build help

all: build

build: ## Build the server binary
	CGO_ENABLED=1 go build -ldflags "$(LDFLAGS)" -o bin/ctrlroom ./cmd/server

run: ## Run the server (dev mode)
	CGO_ENABLED=1 go run -ldflags "$(LDFLAGS)" ./cmd/server

test: ## Run all tests
	CGO_ENABLED=1 go test ./...

test-race: ## Run tests with race detector
	CGO_ENABLED=1 go test -race ./...

test-integration: ## Run integration tests (require real agent binaries)
	CGO_ENABLED=1 go test -tags integration ./...

lint: ## Run golangci-lint
	golangci-lint run

fmt: ## Format code
	go fmt ./...
	golangci-lint run --fix

tidy: ## Tidy module dependencies
	go mod tidy

vet: ## Run go vet
	go vet ./...

web: web-build

web-build: ## Build the frontend
	cd web && npm ci && npm run build

web-dev: ## Run frontend dev server (with backend proxy)
	cd web && npm install && npm run dev

clean: ## Remove build artifacts
	rm -rf bin/ web/dist/ web/node_modules/ coverage.txt

coverage: ## Generate test coverage report
	CGO_ENABLED=1 go test -race -coverprofile=coverage.txt -covermode=atomic ./...
	go tool cover -func=coverage.txt | tail -1

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
