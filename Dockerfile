# syntax=docker/dockerfile:1
#
# harnessly on one host: the runtime with the web page compiled in, tmux, git and bash, as a non-root user whose
# home (workspaces, the runtime's data, the agents' logins) is a volume.
#
#   docker build -t harnessly .                          # the runtime image, without agent CLIs
#   docker build --target with-agents -t harnessly .     # plus Claude Code and Codex from their npm packages
#   docker build --target hub -t harnessly-hub .         # the hub (docs/hub/deploy.md): sign-in, spaces, machines
#   docker build --target machine -t harnessly-machine . # a machine for a hub: registers at start, runs runtime and agent
#
# Claude Code and Codex are not part of the default image: install them with the with-agents target, mount the
# binaries from the host, or install them into the home volume (docs/deploy.md).

ARG GO_VERSION=1.26.5
ARG NODE_VERSION=22
ARG DEBIAN_VERSION=bookworm

# The page is the same for every platform: both web stages and the Go builds run on the builder's own platform, and the
# Go builds cross-compile for the target (a multi-arch image is not built under emulation).
FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-${DEBIAN_VERSION}-slim AS web
WORKDIR /src/web
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${DEBIAN_VERSION} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY skills ./skills
COPY deploy ./deploy
COPY web/*.go ./web/
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN ldflags="-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=${VERSION} -X github.com/ly-solutions/harnessly/internal/hlcli.Version=${VERSION}" \
 && go build -trimpath -tags webui -ldflags "$ldflags" -o /out/runtime ./cmd/runtime \
 && go build -trimpath -ldflags "$ldflags" -o /out/agent ./cmd/agent \
 && go build -trimpath -ldflags "$ldflags" -o /out/harnessly ./cmd/harnessly

# --- voice input: whisper.cpp's server --------------------------------------------------------------------------------
# The speech recognition of the runtime's microphone (internal/voice/engine). Built on Debian 11 (glibc 2.31) so that
# the same binary also runs on the older distributions the release workflow `whisper-server.yml` publishes it for;
# libstdc++ is linked in, there is no OpenMP, and on amd64 the instruction set is AVX2 (the engine checks the processor
# before it uses it). `docker build --target whisper-server -o out .` leaves the binary in out/. whisper.cpp is MIT.
FROM debian:bullseye-slim AS whisper
ARG TARGETARCH
ARG WHISPER_VERSION=v1.9.5
ARG WHISPER_COMMIT=d1be6fde11ac6e0407606b4e42fe72d34add8037
# Debian 11 is out of support: its packages are on archive.debian.org now.
RUN printf 'deb http://archive.debian.org/debian bullseye main\n' > /etc/apt/sources.list \
 && rm -f /etc/apt/sources.list.d/* \
 && apt-get -o Acquire::Check-Valid-Until=false update \
 && apt-get install -y --no-install-recommends build-essential cmake git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch "${WHISPER_VERSION}" https://github.com/ggml-org/whisper.cpp /whisper \
 && test "$(git -C /whisper rev-parse HEAD)" = "${WHISPER_COMMIT}"
RUN cd /whisper \
 && simd="" \
 && if [ "${TARGETARCH}" = "amd64" ]; then simd="-DGGML_AVX=ON -DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_F16C=ON"; fi \
 && cmake -B build -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DGGML_NATIVE=OFF -DGGML_OPENMP=OFF \
      -DWHISPER_BUILD_TESTS=OFF -DWHISPER_SDL2=OFF -DCMAKE_EXE_LINKER_FLAGS="-static-libstdc++ -static-libgcc" $simd \
 && cmake --build build -j"$(nproc)" --target whisper-server \
 && install -D -s build/bin/whisper-server /out/whisper-server

FROM scratch AS whisper-server
COPY --from=whisper /out/whisper-server /whisper-server

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
# --- design tools (optional) ----------------------------------------------------------------------------------------
# The design library's checks, pictures and PDF/PPTX exports drive a headless Chromium; video exports and recordings use
# ffmpeg. They are in the image by default; --build-arg DESIGN_TOOLS=0 leaves them out (about 400 MB less). The board
# engine is never in the image: each machine fetches it with its owner's own account (docs/design-engine.md).
ARG DESIGN_TOOLS=1
RUN if [ "$DESIGN_TOOLS" = 1 ]; then \
      apt-get update \
   && apt-get install -y --no-install-recommends chromium ffmpeg fonts-liberation fonts-noto-color-emoji \
   && rm -rf /var/lib/apt/lists/*; \
    fi
# A container gives an unprivileged user no namespaces for Chromium's own sandbox: the container is the boundary.
ENV RUNTIME_DESIGN_CHROME_ARGS=--no-sandbox
# --- end of design tools ---------------------------------------------------------------------------------------------
COPY --from=build /out/runtime /out/agent /out/harnessly /usr/local/bin/
# Voice input: ffmpeg decodes what the browser records, whisper-server recognizes it. The speech model itself is
# fetched by the runtime on the first dictation into the home volume (docs/deploy.md, "Voice input").
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg \
 && rm -rf /var/lib/apt/lists/*
COPY --from=whisper /out/whisper-server /usr/local/bin/whisper-server
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

# Node.js for the image's own platform (the web stage runs on the builder's).
FROM node:${NODE_VERSION}-${DEBIAN_VERSION}-slim AS node-target

# Claude Code and Codex installed from their official npm packages, with the Node.js they need. Agents log in from a
# terminal tab once; the logins live in the home volume.
FROM base AS with-agents
USER root
COPY --from=node-target /usr/local/bin/node /usr/local/bin/node
COPY --from=node-target /usr/local/lib/node_modules/npm /usr/local/lib/node_modules/npm
ARG CLAUDE_CODE_VERSION=latest
ARG CODEX_VERSION=latest
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx \
 && npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" "@openai/codex@${CODEX_VERSION}" \
 && npm cache clean --force
USER agent

# --- the hub ------------------------------------------------------------------------------------------------------
# The hub with its page compiled in, and what it serves to machines: the runtime (with its page) and the agent for the
# four systems a machine can be, and the installer. /dl/manifest.json is made by the hub from the files it finds.
FROM build AS build-hub
ARG VERSION=dev
# `pnpm build` of the web stage builds the hub page too (web/dist-hub): one install and one type check for both.
COPY --from=web /src/web/dist-hub ./web/dist-hub
# The hub and the four systems a machine can be build side by side: one after another they took four minutes.
RUN set -e; \
    hubflags="-s -w -X github.com/ly-solutions/harnessly/internal/hub/buildinfo.Version=${VERSION}"; \
    ldflags="-s -w -X github.com/ly-solutions/harnessly/internal/agent.BuildVersion=${VERSION} -X github.com/ly-solutions/harnessly/internal/hlcli.Version=${VERSION}"; \
    mkdir -p /out/dist; \
    go build -trimpath -tags hubui -ldflags "$hubflags" -o /out/hub ./cmd/hub & pids="$!"; \
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
      os=${target%/*}; arch=${target#*/}; \
      ( GOOS=$os GOARCH=$arch go build -trimpath -tags webui -ldflags "$ldflags" -o /out/dist/runtime-$os-$arch ./cmd/runtime \
        && GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$ldflags" -o /out/dist/agent-$os-$arch ./cmd/agent ) & pids="$pids $!"; \
    done; \
    for p in $pids; do wait "$p"; done; \
    install -m 0755 deploy/install/install.sh /out/dist/install.sh

# The hub: one static binary, CA certificates for the sign-in providers and the mail server, a user of its own, and the
# data folder (SQLite, the SSH key) on a volume. Nothing else runs in it.
FROM debian:${DEBIAN_VERSION}-slim AS hub
LABEL org.opencontainers.image.title="harnessly hub" \
      org.opencontainers.image.description="The hub of harnessly: sign-in, spaces and invitations, and the machines people connect, reached through one address" \
      org.opencontainers.image.source="https://github.com/ly-solutions/harnessly" \
      org.opencontainers.image.licenses="Apache-2.0"
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --uid 10002 --user-group --no-create-home --shell /usr/sbin/nologin hub \
 && mkdir -p /var/lib/harnessly-hub /run/harnessly \
 && chown hub:hub /var/lib/harnessly-hub /run/harnessly
COPY --from=build-hub /out/hub /usr/local/bin/hub
COPY --from=build-hub /out/dist /opt/harnessly/dist
ENV HUB_LISTEN=0.0.0.0:8040 \
    HUB_DATA_DIR=/var/lib/harnessly-hub \
    HUB_DIST_DIR=/opt/harnessly/dist
USER hub
WORKDIR /var/lib/harnessly-hub
VOLUME ["/var/lib/harnessly-hub"]
EXPOSE 8040
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 \
  CMD ["/usr/local/bin/hub", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/hub"]
CMD ["serve"]

# A machine for a hub: the with-agents image, the runtime and the agent under the names the entrypoint runs, and the
# entrypoint, which registers the machine with its hub (AGENT_HUB_URL and AGENT_REGISTER_CODE or
# AGENT_REGISTER_CODE_FILE) when it is not registered, then runs both. Nothing is published: the hub reaches the runtime
# through the agent's outgoing link. The home folder (logins, workspaces, the machine's key) is the volume.
FROM with-agents AS machine
USER root
COPY --from=build /out/agent /usr/local/bin/harnessly-agent
COPY --from=build /out/runtime /usr/local/bin/harnessly-runtime
COPY --chmod=0755 deploy/container/entrypoint.sh /usr/local/bin/harnessly-machine
USER agent
HEALTHCHECK NONE
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/harnessly-machine"]

# The default target (the last stage): the runtime image.
FROM base AS runtime
