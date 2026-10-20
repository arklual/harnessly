# Deploying harnessly

harnessly is two static binaries: `runtime` (one per person: the web page, the API, tmux and the agents) and
`agent` (one per Unix account, only for linking machines to a cluster). A single person on a single host needs
only the runtime. Pick the way that fits:

| Way | For | What you get |
| --- | --- | --- |
| [Plain commands](#plain-commands) | a laptop or a dev box, quick tries | `make run`, or a release binary |
| [systemd](#systemd-one-host-one-account) | a host you keep running, one service per Unix account | a user service that survives logouts and keeps agents through restarts |
| [Docker](#docker) | an isolated runtime on any host | one container, the home folder on a volume |
| [docker compose](#docker-compose-the-whole-workspace) | the full workspace on one host | harnessly, boards, artifacts and MinIO with one `docker compose up` |
| [Helm](#kubernetes-helm) | Kubernetes | a StatefulSet per person, the home folder on a PVC |
| [Argo CD](#argo-cd) | GitOps | an Application per person, or an app of apps for the whole workspace |

Whatever the way, the runtime runs *standalone* (`RUNTIME_GATE_UPSTREAM` empty), keeps its state in the home folder
(`~/.harnessly` for its data, the workspaces wherever `RUNTIME_WORKSPACE_DIR` points) and asks for a password on the
first visit of the page. Every variable is in [configuration](configuration.md).

## What the agents need

The runtime starts the real CLIs, so they must be installed where it runs and logged in as the account it runs as:

- **Claude Code**: `npm install -g @anthropic-ai/claude-code`, or Anthropic's native installer
  (`curl -fsSL https://claude.ai/install.sh | bash`, which puts `claude` in `~/.local/bin`);
- **Codex**: `npm install -g @openai/codex`.

Log in once from a terminal tab of the page (`claude`, then `/login`; `codex login`), or provide `ANTHROPIC_API_KEY` /
`OPENAI_API_KEY` in the runtime's environment. Shells in tabs are login shells, so the account's `PATH` (version
managers included) applies. The machine panel shows which tools the runtime found.

## Plain commands

```sh
git clone https://github.com/ly-solutions/harnessly.git && cd harnessly
make run                        # builds web/ and the runtime with the page, serves http://127.0.0.1:8022
```

`make run` needs Go, Node.js 22 with pnpm (corepack), tmux and git. Without Node.js, take a release binary instead:
the archives on the [releases page](https://github.com/ly-solutions/harnessly/releases) carry a runtime with the page
compiled in.

```sh
RUNTIME_GATE_LISTEN=127.0.0.1:8022 RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ ./runtime
```

`make build` builds the binaries *without* the page (the runtime then serves only its API, or a page from
`RUNTIME_WEB_DIR`); `make build-webui` builds them the way releases do. Keep the listener on loopback and reach it
through SSH (`ssh -L 8022:127.0.0.1:8022 host`) or a TLS proxy.

## systemd: one host, one account

This is how a shared machine runs harnessly: one runtime per Unix account, as that account's systemd user service.

```sh
make install install-systemd    # ~/.local/bin/harnessly-runtime, ~/.config/systemd/user/harnessly.service,
                                # ~/.config/harnessly/env (fill it in)
systemctl --user enable --now harnessly
loginctl enable-linger "$USER"  # keep it running without a login session
journalctl --user -u harnessly -f
```

The unit ([deploy/systemd/harnessly.service](../deploy/systemd/harnessly.service)) uses `KillMode=process`: stopping
or upgrading the service stops only the runtime, while tmux and the agents keep working; the next runtime picks
them up and resumes agents that were busy. Give every account its own `RUNTIME_GATE_LISTEN` port.

## Docker

The [Dockerfile](../Dockerfile) builds the page and the binaries and ships them on Debian slim with tmux, git, bash,
curl, ripgrep and a non-root user `agent` (uid 10001). `/home/agent` is a volume: workspaces, the runtime's data and
the agents' logins live there.

```sh
docker build -t harnessly .                                  # or: make image
docker run -d --name harnessly -p 127.0.0.1:8022:8022 -v harnessly-home:/home/agent harnessly
```

The default image has no agent CLIs. Three ways to add them:

1. **Build the `with-agents` target**, which installs both from their npm packages (with Node.js):
   `docker build --target with-agents -t harnessly .` (`make image TARGET=with-agents`). Pin versions with
   `--build-arg CLAUDE_CODE_VERSION=… --build-arg CODEX_VERSION=…`.
2. **Install them into the home volume** from a terminal tab of the page:
   `curl -fsSL https://claude.ai/install.sh | bash` (Claude Code's native installer, into `~/.local/bin`). Installs
   survive container upgrades because they live on the volume.
3. **Mount host binaries** that run on Debian (glibc), read-only:
   `-v "$(command -v claude)":/usr/local/bin/claude:ro`. Self-contained binaries (Claude Code's native build, Codex's)
   work; an npm-installed CLI also needs Node.js in the image, so prefer 1.

Logins are made inside the container (they end up in the volume) or come from API keys passed with `-e`.

To run the container with a read-only root (only the volume and `/tmp` writable), add `--read-only --tmpfs /tmp`.

## docker compose: the whole workspace

[compose.yaml](../compose.yaml) runs harnessly with [boards](https://github.com/ly-solutions/boards) and
[artifacts](https://github.com/ly-solutions/artifacts), and MinIO as their S3 storage (the buckets are created on
start):

```sh
cp .env.example .env            # optional: your own tokens and ports
docker compose up --build       # or: make compose-up
```

Open http://localhost:8022, set the password, start an agent: it gets `BOARDS_URL`, `ARTIFACTS_URL` and their tokens,
the boards and documents it creates appear as chips in the conversation and open in the side pane, and the pane lists
the conversation's documents. boards is on http://localhost:8081 and artifacts on http://localhost:8082 as well.

How it is wired:

- boards, artifacts and MinIO share harnessly's network namespace (`network_mode: service:harnessly`), like the
  containers of a pod. The browser, the runtime and the agents in its tabs therefore reach every service at the same
  `http://localhost:<port>` address, and links agents post open in the browser unchanged. All four ports are
  published by the `harnessly` service, on 127.0.0.1 only.
- boards and artifacts allow `http://localhost:8022` to frame them and use tokens from `.env` (placeholders by
  default). The runtime signs you in to boards with a hand-off; the document pane asks for the catalog's sign-in once:
  paste `ARTIFACTS_VIEWER_TOKEN` (or the editor token) in the window it opens.
- boards and artifacts are built from their repositories (`BOARDS_SOURCE`, `ARTIFACTS_SOURCE` name local checkouts
  instead), or pulled when `BOARDS_IMAGE` / `ARTIFACTS_IMAGE` name published images.
- `HARNESSLY_TARGET=with-agents` builds harnessly with Claude Code and Codex.

`docker compose down` keeps the volumes (`home`, `minio-data`); `down -v` removes them. For anything beyond your own
machine, set real tokens and put TLS in front — or use Kubernetes.

## Kubernetes: Helm

[deploy/helm/harnessly](../deploy/helm/harnessly) runs one person's harnessly as a StatefulSet with one replica, the
home folder on a PersistentVolumeClaim, probes on `/api/health`, a non-root, read-only-root security context, a
Service and an optional Ingress. Tokens come from a Secret (`existingSecret`).

```sh
helm install ada deploy/helm/harnessly \
  --set ownerName=Ada \
  --set integrations.boards.url=https://boards.example.com \
  --set integrations.artifacts.url=https://artifacts.example.com \
  --set existingSecret=harnessly-ada \
  --set ingress.enabled=true --set 'ingress.hosts[0].host=ada.work.example.com' \
  --set 'ingress.hosts[0].paths[0].path=/'
```

The [chart's README](../deploy/helm/harnessly/README.md) lists every value. Run one release per person; boards and
artifacts are installed from their own charts.

## Argo CD

- [deploy/argocd/application.yaml](../deploy/argocd/application.yaml): one person's harnessly from the chart.
- [deploy/argocd/workspace.yaml](../deploy/argocd/workspace.yaml): an app of apps over
  [deploy/argocd/workspace](../deploy/argocd/workspace) — boards, artifacts and a harnessly per person, each from its
  own repository's chart. Copy the folder into your GitOps repository, set hosts, buckets and Secret names, and add a
  `harnessly-<name>.yaml` per person.

Secrets stay out of Git in both: create them with External Secrets, Sealed Secrets or SOPS.

## Behind a proxy

The page uses WebSockets (terminals, conversations, the status and sync sockets): give them a long read timeout
(`proxy_read_timeout 3600s` in nginx). To serve the page under a path, set `RUNTIME_APP_BASE_PATH=/agents`; the API
answers under that path too, so one proxy prefix covers both. Boards and artifacts must allow the page's origin in
their `frame-ancestors`; host all three under one registrable domain so their sign-in cookies reach the panes
([integrations](integrations.md)).
