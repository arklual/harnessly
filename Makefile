# Developer tasks. CI runs the same targets, so `make fmt-check vet lint test-race` before a push is what CI checks.

GO            ?= go
GOLANGCI_LINT ?= golangci-lint
BIN           ?= bin
VERSION       ?= $(shell git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev)
LDFLAGS       := -s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=$(or $(VERSION),dev) -X github.com/ly-solutions/harnessly/internal/hlcli.Version=$(or $(VERSION),dev) -X github.com/ly-solutions/harnessly/internal/hub/buildinfo.Version=$(or $(VERSION),dev)
CROSS         := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.DEFAULT_GOAL := help
.PHONY: skills-sync skills-check help build test test-race lint fmt fmt-check vet cover cross clean web web-check web-browser-test runtime-web build-webui test-webui release-snapshot run install install-systemd image compose-up compose-down hub-web hub hub-dist hub-image machine-image hub-compose-up hub-compose-down

help: ## Show the targets
	@awk 'BEGIN {FS = ":.*## "} /^[a-z-]+:.*## / {printf "  \033[1m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

build: ## Build agent, harnessly, runtime, agentprobe and hub into ./bin (static, no cgo; the runtime and the hub without their pages)
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

skills-sync: ## Re-copy the vendored agent skills from the commits pinned in skills/SOURCES
	skills/sync.sh

skills-check: ## Fail when the vendored agent skills differ from the commits pinned in skills/SOURCES (needs a network)
	skills/sync.sh --check

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

build-webui: build runtime-web ## Build agent, harnessly, agentprobe and a runtime with the page: what releases ship

test-webui: web ## Check the page-carrying runtime keeps the page off in front of a previous runtime
	$(GO) test -tags webui -count=1 -run TestWebUIBuild ./cmd/runtime

release-snapshot: ## Build the release archives locally into dist/ with GoReleaser (needs goreleaser, Node.js, pnpm)
	goreleaser release --snapshot --clean

web-browser-test: web ## Drive the page in headless Chrome against a standalone runtime (E2E_BOARDS_DIR, E2E_ARTIFACTS_DIR: also the side pane)
	cd web && $(PNPM) e2e

# --- the hub (docs/hub/deploy.md) -------------------------------------------------------------------------------------

# Where hub-dist puts what the hub serves to machines (HUB_DIST_DIR of the hub), and for which systems.
HUB_DIST      ?= $(BIN)/hub-dist
HUB_TARGETS   ?= $(CROSS)
HUB_IMAGE     ?= ghcr.io/ly-solutions/harnessly-hub:latest
MACHINE_IMAGE ?= ghcr.io/ly-solutions/harnessly-machine:latest

hub-web: web/node_modules ## Build the hub page into web/dist-hub
	cd web && $(PNPM) build:hub

hub: hub-web ## Build bin/hub with the hub page compiled in (the hubui tag)
	CGO_ENABLED=0 $(GO) build -trimpath -tags hubui -ldflags "$(LDFLAGS)" -o $(BIN)/hub ./cmd/hub

hub-dist: web ## Build what a hub serves to its machines into HUB_DIST: runtime-<os>-<arch>, agent-<os>-<arch>, install.sh (HUB_TARGETS=linux/amd64 for one system)
	@mkdir -p $(HUB_DIST)
	@for target in $(HUB_TARGETS); do \
		os=$${target%/*}; arch=$${target#*/}; echo "hub-dist $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -tags webui -ldflags "$(LDFLAGS)" -o $(HUB_DIST)/runtime-$$os-$$arch ./cmd/runtime || exit 1; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(HUB_DIST)/agent-$$os-$$arch ./cmd/agent || exit 1; \
	done
	@if [ -f deploy/install/install.sh ]; then install -m755 deploy/install/install.sh $(HUB_DIST)/install.sh; else echo "deploy/install/install.sh is missing: /install will answer 404 until it is in $(HUB_DIST)"; fi

hub-image: ## Build the hub image (binary, page, and the machine binaries it serves)
	docker build --build-arg VERSION=$(VERSION) --target hub -t $(HUB_IMAGE) .

machine-image: ## Build the machine image (runtime and agent with Claude Code and Codex, registers with a hub at start)
	docker build --build-arg VERSION=$(VERSION) --target machine -t $(MACHINE_IMAGE) .

hub-compose-up: ## Build and start a hub and a machine next to it with docker compose (compose.hub.yaml; http://127.0.0.1:8040)
	docker compose -f compose.hub.yaml up --build -d

hub-compose-down: ## Stop the hub and its machine (volumes stay)
	docker compose -f compose.hub.yaml down

# --- running it -----------------------------------------------------------------------------------------------------

PREFIX       ?= $(HOME)/.local
LISTEN       ?= 127.0.0.1:8022
IMAGE        ?= ghcr.io/ly-solutions/harnessly:latest
SYSTEMD_USER ?= $(HOME)/.config/systemd/user

run: runtime-web ## Run the runtime with the page in the foreground on LISTEN (127.0.0.1:8022), data in ~/.harnessly
	RUNTIME_GATE_LISTEN=$(LISTEN) RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ $(BIN)/runtime

install: build-webui ## Install harnessly-runtime, harnessly-agent and the harnessly command into PREFIX/bin (~/.local/bin)
	install -Dm755 $(BIN)/runtime $(DESTDIR)$(PREFIX)/bin/harnessly-runtime
	install -Dm755 $(BIN)/agent $(DESTDIR)$(PREFIX)/bin/harnessly-agent
	install -Dm755 $(BIN)/harnessly $(DESTDIR)$(PREFIX)/bin/harnessly

install-systemd: ## Install the systemd user service and an env file to fill in (then: systemctl --user enable --now harnessly)
	install -Dm644 deploy/systemd/harnessly.service $(SYSTEMD_USER)/harnessly.service
	@if [ ! -f $(HOME)/.config/harnessly/env ]; then install -Dm600 deploy/systemd/env.example $(HOME)/.config/harnessly/env; fi
	systemctl --user daemon-reload

image: ## Build the container image (TARGET=with-agents adds Claude Code and Codex)
	docker build --build-arg VERSION=$(VERSION) $(if $(TARGET),--target $(TARGET)) -t $(IMAGE) .

compose-up: ## Build and start harnessly, boards and artifacts with docker compose (http://localhost:8022)
	docker compose up --build -d

compose-down: ## Stop the compose workspace (volumes stay)
	docker compose down
