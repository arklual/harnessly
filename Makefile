# Developer tasks. CI runs the same targets, so `make fmt-check vet lint test-race` before a push is what CI checks.

GO            ?= go
GOLANGCI_LINT ?= golangci-lint
BIN           ?= bin
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS       := -s -w -X harnessly/internal/agent.BuildVersion=$(or $(VERSION),dev)
CROSS         := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.DEFAULT_GOAL := help
.PHONY: help build test test-race lint fmt fmt-check vet cover cross clean

help: ## Show the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  \033[1m%-10s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build agent, runtime and agentprobe into ./bin (static, no cgo)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/ ./cmd/...

test: ## Run the tests
	$(GO) test -count=1 ./...

test-race: ## Run the tests with the race detector
	$(GO) test -race -count=1 ./...

lint: ## Run golangci-lint (.golangci.yml)
	$(GOLANGCI_LINT) run ./...

fmt: ## Format the code
	gofmt -w cmd internal

fmt-check: ## Fail when a file is not gofmt-ed
	@unformatted=$$(gofmt -l cmd internal); if [ -n "$$unformatted" ]; then echo "not gofmt-ed:"; echo "$$unformatted"; exit 1; fi

vet: ## Run go vet
	$(GO) vet ./...

cover: ## Run the tests with coverage into coverage.out and print the total
	$(GO) test -count=1 -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -n 1

cross: ## Compile every package for linux and darwin, amd64 and arm64
	@for target in $(CROSS); do \
		echo "go build $$target"; \
		CGO_ENABLED=0 GOOS=$${target%/*} GOARCH=$${target#*/} $(GO) build -o /dev/null ./... || exit 1; \
	done

clean: ## Remove build outputs
	rm -rf $(BIN) dist coverage.out
