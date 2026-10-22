# syntax=docker/dockerfile:1
#
# harnessly on one host: the runtime with the web page compiled in, tmux, git and bash, as a non-root user whose
# home (workspaces, the runtime's data, the agents' logins) is a volume.
#
#   docker build -t harnessly .                          # the runtime image, without agent CLIs
#   docker build --target with-agents -t harnessly .     # plus Claude Code and Codex from their npm packages
#
# Claude Code and Codex are not part of the default image: install them with the with-agents target, mount the
# binaries from the host, or install them into the home volume (docs/deploy.md).

ARG GO_VERSION=1.26.5
ARG NODE_VERSION=22
ARG DEBIAN_VERSION=bookworm

FROM node:${NODE_VERSION}-${DEBIAN_VERSION}-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM golang:${GO_VERSION}-${DEBIAN_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY skills ./skills
COPY web/*.go ./web/
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ENV CGO_ENABLED=0
RUN ldflags="-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=${VERSION} -X github.com/ly-solutions/harnessly/internal/hlcli.Version=${VERSION}" \
 && go build -trimpath -tags webui -ldflags "$ldflags" -o /out/runtime ./cmd/runtime \
 && go build -trimpath -ldflags "$ldflags" -o /out/agent ./cmd/agent \
 && go build -trimpath -ldflags "$ldflags" -o /out/harnessly ./cmd/harnessly

FROM debian:${DEBIAN_VERSION}-slim AS base
LABEL org.opencontainers.image.title="harnessly" \
      org.opencontainers.image.description="A workspace for people and coding agents: terminals, conversations, documents and boards in one place" \
      org.opencontainers.image.source="https://github.com/ly-solutions/harnessly" \
      org.opencontainers.image.licenses="Apache-2.0"
RUN apt-get update \
 && apt-get install -y --no-install-recommends tmux git bash ca-certificates curl tini procps openssh-client ripgrep less \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 --user-group --shell /bin/bash agent \
 && mkdir -p /home/agent/work \
 && chown -R agent:agent /home/agent
COPY --from=build /out/runtime /out/agent /out/harnessly /usr/local/bin/
ENV HOME=/home/agent \
    LANG=C.UTF-8 \
    SHELL=/bin/bash \
    RUNTIME_GATE_LISTEN=0.0.0.0:8022 \
    RUNTIME_GATE_UPSTREAM= \
    RUNTIME_WORKSPACE_DIR=/home/agent/work \
    RUNTIME_APP_BASE_PATH=/ \
    RUNTIME_CODEX_SANDBOX=container
USER agent
WORKDIR /home/agent
VOLUME ["/home/agent"]
EXPOSE 8022 8023-8030
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl -fsS http://127.0.0.1:8022/api/health >/dev/null || exit 1
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/runtime"]

# Claude Code and Codex installed from their official npm packages, with the Node.js they need. Agents log in from a
# terminal tab once; the logins live in the home volume.
FROM base AS with-agents
USER root
COPY --from=web /usr/local/bin/node /usr/local/bin/node
COPY --from=web /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
ARG CLAUDE_CODE_VERSION=latest
ARG CODEX_VERSION=latest
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
 && npm cache clean --force
USER agent

FROM base AS runtime
