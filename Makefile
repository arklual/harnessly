# Developer tasks. CI runs the same targets, so `make fmt-check vet lint test-race` before a push is what CI checks.

GO            ?= go
GOLANGCI_LINT ?= golangci-lint
BIN           ?= bin
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS       := -s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=$(or $(VERSION),dev)
CROSS         := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.DEFAULT_GOAL := help
.PHONY: help build test test-race lint fmt fmt-check vet cover cross clean web web-check web-browser-test runtime-web build-webui test-webui release-snapshot run install install-systemd image compose-up compose-down

help: ## Show the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  \033[1m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build agent, runtime and agentprobe into ./bin (static, no cgo; the runtime without the page)
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

PNPM ?= pnpm

web/node_modules: web/package.json web/pnpm-lock.yaml
	cd web && $(PNPM) install --frozen-lockfile
	@touch $@

web: web/node_modules ## Build the web page into web/dist
	cd web && $(PNPM) build

web-check: web/node_modules ## Lint, type-check, unit-test and build the web page (what CI runs for it)
	cd web && $(PNPM) lint && $(PNPM) typecheck && $(PNPM) test && $(PNPM) build

runtime-web: web ## Build bin/runtime with the web page compiled in (the webui tag)
	CGO_ENABLED=0 $(GO) build -trimpath -tags webui -ldflags "$(LDFLAGS)" -o $(BIN)/runtime ./cmd/runtime

build-webui: build runtime-web ## Build agent, agentprobe and a runtime with the page: what releases ship

test-webui: web ## Check the page-carrying runtime keeps the page off in front of a previous runtime
	$(GO) test -tags webui -count=1 -run TestWebUIBuild ./cmd/runtime

release-snapshot: ## Build the release archives locally into dist/ with GoReleaser (needs goreleaser, Node.js, pnpm)
	goreleaser release --snapshot --clean

web-browser-test: web ## Drive the page in headless Chrome against a standalone runtime (E2E_BOARDS_DIR, E2E_ARTIFACTS_DIR: also the side pane)
	cd web && $(PNPM) e2e

# --- running it -----------------------------------------------------------------------------------------------------

PREFIX       ?= $(HOME)/.local
LISTEN       ?= 127.0.0.1:8022
IMAGE        ?= ghcr.io/ly-solutions/harnessly:latest
SYSTEMD_USER ?= $(HOME)/.config/systemd/user

run: runtime-web ## Run the runtime with the page in the foreground on LISTEN (127.0.0.1:8022), data in ~/.harnessly
	RUNTIME_GATE_LISTEN=$(LISTEN) RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ $(BIN)/runtime

install: build-webui ## Install harnessly-runtime and harnessly-agent into PREFIX/bin (~/.local/bin)
	install -Dm755 $(BIN)/runtime $(DESTDIR)$(PREFIX)/bin/harnessly-runtime
	install -Dm755 $(BIN)/agent $(DESTDIR)$(PREFIX)/bin/harnessly-agent

install-systemd: ## Install the systemd user service and an env file to fill in (then: systemctl --user enable --now harnessly)
	install -Dm644 deploy/systemd/harnessly.service $(SYSTEMD_USER)/harnessly.service
	@if [ ! -f $(HOME)/.config/harnessly/env ]; then install -Dm600 deploy/systemd/env.example $(HOME)/.config/harnessly/env; fi
	systemctl --user daemon-reload

image: ## Build the container image (TARGET=with-agents adds Claude Code and Codex)
	docker build --build-arg VERSION=$(VERSION) $(if $(TARGET),--target $(TARGET)) -t $(IMAGE) .

compose-up: ## Start harnessly, boards, artifacts and MinIO with docker compose (http://localhost:8022)
	docker compose up --build -d

compose-down: ## Stop the compose workspace (volumes stay)
	docker compose down
