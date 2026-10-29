# Deploying harnessly

harnessly is two static binaries: `runtime` (one per person: the web page, the API, tmux and the agents) and
`agent` (one per Unix account, only for linking machines to a cluster). A single person on a single host needs
only the runtime.

## Quick start

Four ways, the shortest first. All of them end the same way: open the page, sign in once, choose your password.

### Quick start with the installer

```sh
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh
```

For one person on one Linux or macOS machine, as an ordinary account (it uses `sudo` only to install missing
packages). [`install.sh`](../install.sh):

- downloads the latest release for the system into `~/.local/bin` as `harnessly-runtime`, `harnessly-agent` and
  `harnessly`, after checking the archive against `checksums.txt`; when `cosign` is installed it also checks the
  signature of `checksums.txt`, as in [the release binary](#quick-start-with-a-release-binary) below;
- installs `tmux` and `git` with `apt`, `dnf`, `yum`, `zypper`, `pacman`, `apk` or `brew` when they are missing
  (`--no-deps` only checks);
- writes the settings to `~/.config/harnessly/env` and starts the runtime as a systemd user service with
  lingering on Linux, a launchd agent on macOS, or a background process when neither is there, on `127.0.0.1:8022`;
- waits for `/api/health` and prints the address, and on a machine reached over SSH the `ssh -L` command for your
  computer.

```sh
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- --port 8030
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- --version 0.16.16
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- --uninstall
```

Running it again updates to the latest release. `--uninstall` stops the service and removes the programs; the data in
`~/.harnessly`, the settings file and the tmux sessions stay. To link machines to a team hub, use the installer the hub
serves instead ([connecting a machine](hub/machine.md)).

### Quick start with Docker

```sh
docker run -d --name harnessly -p 127.0.0.1:8022:8022 -v harnessly-home:/home/agent ghcr.io/ly-solutions/harnessly:latest
docker logs harnessly | grep -i password
```

Open <http://127.0.0.1:8022>, enter the `Initial password` the second command printed, and choose your own password
when the page asks. Workspaces, the runtime's data and the agents' logins live on the `harnessly-home` volume. The image
has no agent CLIs: use `ghcr.io/ly-solutions/harnessly:with-agents` for Claude Code and Codex, or see
[what the agents need](#what-the-agents-need). Tags: `X.Y.Z` (pin this in production), `X.Y`, `latest`, and the same
with a `-with-agents` suffix (`with-agents` is the latest of those). The images are for linux/amd64 and linux/arm64.

Verify the image (needs [cosign](https://docs.sigstore.dev/cosign/installation/) or the [GitHub CLI](https://cli.github.com/)):

```sh
cosign verify ghcr.io/ly-solutions/harnessly:latest \
  --certificate-identity-regexp '^https://github.com/ly-solutions/harnessly/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/ly-solutions/harnessly:latest --repo ly-solutions/harnessly
```

### Quick start with a release binary

Needs `tmux` and `git` on the machine. Every [release](https://github.com/ly-solutions/harnessly/releases) has an archive
per system, `harnessly_<version>_<os>_<arch>.tar.gz` for `linux` and `darwin` on `amd64` and `arm64`, with the
`runtime` (the page is compiled in), the `agent` and the `harnessly` command line tool, plus `checksums.txt` and its
signature `checksums.txt.sigstore.json`.

```sh
VERSION=$(curl -fsSL https://api.github.com/repos/ly-solutions/harnessly/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
OS=linux ARCH=amd64                     # OS: linux or darwin; ARCH: amd64 or arm64
BASE=https://github.com/ly-solutions/harnessly/releases/download/v$VERSION
ARCHIVE=harnessly_${VERSION}_${OS}_${ARCH}.tar.gz
curl -fsSLO "$BASE/$ARCHIVE" -O "$BASE/checksums.txt" -O "$BASE/checksums.txt.sigstore.json"

# 1. checksums.txt was signed by this repository's release workflow (cosign), keyless:
cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/ly-solutions/harnessly/.github/workflows/release.yml@refs/tags/v$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
# 2. the archive is the one in checksums.txt (on macOS: shasum -a 256 -c -):
grep " $ARCHIVE\$" checksums.txt | sha256sum -c -
# 3. optional, the build provenance GitHub recorded for the archive:
gh attestation verify "$ARCHIVE" --repo ly-solutions/harnessly

tar xzf "$ARCHIVE"
RUNTIME_GATE_LISTEN=127.0.0.1:8022 RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ ./runtime
```

Open <http://127.0.0.1:8022> and choose your password. A runtime that listens on `127.0.0.1` leaves the setup open to
whoever reaches the port first, which on your own machine is you; see [the first visit](#the-first-visit) before you
listen on anything else. To keep it running, use [systemd](#systemd-one-host-one-account).

### Quick start from source

```sh
git clone https://github.com/ly-solutions/harnessly.git && cd harnessly
make run                        # builds web/ and the runtime with the page, serves http://127.0.0.1:8022
```

Needs Go (the version in [`go.mod`](../go.mod)), Node.js 22 with pnpm (corepack), tmux and git; more under
[plain commands](#plain-commands).

### Updating

- **Docker**: pull the new image and recreate the container. The home volume stays, so the password, workspaces and
  logins stay too:

  ```sh
  docker pull ghcr.io/ly-solutions/harnessly:latest
  docker rm -f harnessly
  docker run -d --name harnessly -p 127.0.0.1:8022:8022 -v harnessly-home:/home/agent ghcr.io/ly-solutions/harnessly:latest
  ```

  With a pinned tag, put the new version in both places. With compose: `docker compose pull && docker compose up -d`.
- **Installer**: run the same command again.
- **Binary**: download and verify the new archive as above, replace `runtime` (and `agent`, `harnessly` if you use them)
  and restart it. The data in `~/.harnessly` is kept. Under [systemd](#systemd-one-host-one-account) the restart keeps
  tmux and the agents running.
- **From source**: `git pull && make run` (or `make install` and restart the service).

The runtime's `/api/health` answers with the version it runs, and the release notes list what changed.

## All the ways to run

| Way | For | What you get |
| --- | --- | --- |
| [Plain commands](#plain-commands) | a laptop or a dev box, quick tries | `make run`, or a release binary |
| [systemd](#systemd-one-host-one-account) | a host you keep running, one service per Unix account | a user service that survives logouts and keeps agents through restarts |
| [Docker](#docker) | an isolated runtime on any host | one container, the home folder on a volume |
| [docker compose](#docker-compose-the-whole-workspace) | the full workspace on one host | harnessly, boards and artifacts (MinIO optional) built and started with one `docker compose up -d --build` |
| [A public server](#a-public-server) | compose behind Caddy on a VPS | three hostnames, automatic TLS, a firewall that lets in 22, 80 and 443 |
| [Helm](#kubernetes-helm) | Kubernetes | a StatefulSet per person, the home folder on a PVC |
| [Argo CD](#argo-cd) | GitOps | an Application per person, or an app of apps for the whole workspace |

Whatever the way, the runtime runs *standalone* (`RUNTIME_GATE_UPSTREAM` empty), keeps its state in the home folder
(`~/.harnessly` for its data, the workspaces wherever `RUNTIME_WORKSPACE_DIR` points) and asks for a password on the
first visit of the page ([read how that is protected](#the-first-visit) before the host is reachable by anyone else).
Every variable is in [configuration](configuration.md).

## What the agents need

The runtime starts the real CLIs, so they must be installed where it runs and logged in as the account it runs as:

- **Claude Code**: `npm install -g @anthropic-ai/claude-code`, or Anthropic's native installer
  (`curl -fsSL https://claude.ai/install.sh | bash`, which puts `claude` in `~/.local/bin`);
- **Codex**: `npm install -g @openai/codex`.

Log in once from a terminal tab of the page, or provide `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` in the runtime's
environment. Shells in tabs are login shells, so the account's `PATH` (version managers included) applies. The machine
panel shows which tools the runtime found.

- **Claude Code**: run `claude` and type `/login`. It prints a URL: open it in any browser and paste the code you get
  back into the terminal if it asks.
- **Codex on a remote host** (a VPS, a container, anything you do not sit in front of): run
  `codex login --device-auth`, or start `codex` and pick option 2, "Sign in with Device Code", in its own menu. Both
  print a URL and a one-time code: open the URL in a browser on any machine and enter the code. A plain `codex login`
  waits for a browser callback on `127.0.0.1:1455` of the same host, which a remote browser cannot reach, so it never
  completes there. The host must reach `api.openai.com` and `chatgpt.com`; on a locked-down network allow both.
  The login is stored under `~/.codex`, on the home volume in Docker.

Codex conversations start through a small Node.js script the runtime writes into its data folder, so `node` (20 or
newer) must be on the account's `PATH`. The `with-agents` image has it, as does anything that installed Codex from npm;
with a standalone Codex binary and no Node.js, install Node.js too. In the Docker image Codex runs without its own sandbox, which cannot start in a container (`RUNTIME_CODEX_SANDBOX`,
[integrations](integrations.md#agent-skills)). Claude Code needs nothing besides itself. Neither
needs any environment variable besides the ones above: the runtime picks the data folder, the launcher's location and
the variable that carries the token to Codex's MCP server on its own (`RUNTIME_SCRIPT_DATA_DIRS`,
`RUNTIME_MCP_TOKEN_ENV` and `RUNTIME_MCP_SERVER` in [configuration](configuration.md) only rename them).

## The first visit

The page asks for a password the first time it is opened. Until one is chosen, anyone who reaches the page could choose
it, so a runtime that listens beyond this machine locks the setup behind an **initial password**:

- **Nothing set, listening on all interfaces** (a container, `RUNTIME_GATE_LISTEN=0.0.0.0:8022`, a server): the runtime
  makes a one-time initial password at start and prints it once, in a block that contains the line
  `Initial password: XXXX-XXXX-XXXX-XXXX-XXXX` (`docker logs harnessly | grep -i password`, `journalctl -u harnessly`).
  The page asks for it before it shows the setup and refuses to set up for anyone who has not signed in with it. You
  then choose your own password; that one is stored, and the initial one stops working. The initial password is kept
  in memory only: if the runtime restarts before the setup is done, it prints a new one.
- **`INIT_PASSWORD` set** (at least 4 characters): that is the initial password, in any listen mode, and nothing is
  generated. Use it when you want to know the password beforehand (`INIT_PASSWORD=$(openssl rand -hex 16)`). It is in
  `.env` for compose ([`.env.example`](../.env.example)), in `~/.config/harnessly/env` for systemd and under `extraEnv`
  in the Helm chart. It is not read again once a password exists.
- **Listening on loopback only** (`RUNTIME_GATE_LISTEN=127.0.0.1:…`, which is what `make run` does): nothing is
  generated and the first visitor chooses the password, as before. On your own machine, or behind an SSH tunnel, that is
  you. If a reverse proxy on the same host exposes that port to others, set `INIT_PASSWORD` or
  `RUNTIME_GENERATE_INIT_PASSWORD=1` (the runtime cannot tell proxied clients from local ones).

`RUNTIME_GENERATE_INIT_PASSWORD=0` turns the generation off (the first visitor chooses, as before). There is no
password form in the page yet: to choose a new password later, stop the runtime, delete `authPassword` and `authSecret`
from `config.json` in its data folder (`~/.harnessly`) and start it again; the setup then runs again.

The setup stores `networkAccess` in the runtime's `config.json` (the page chooses `localhost`; `tailscale` and `all`
exist). It is not a firewall and not the listen address (`RUNTIME_GATE_LISTEN` is): it decides which remote addresses
may post the agents' status hooks (`/api/status/hook`). With `localhost` only processes on the machine itself can,
which is where the agents run, so leave it. `HOST`/`RUNTIME_ACCESS_HOST` override it with an explicit rule.

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
[artifacts](https://github.com/ly-solutions/artifacts). **There are no published images yet: you build them.** The
first start builds all three (harnessly from this checkout, boards and artifacts from their GitHub repositories),
which takes a few minutes and needs Docker with Compose v2:

```sh
cp .env.example .env            # optional on your own machine: your own tokens, ports and URLs
docker compose up -d --build    # or: make compose-up
```

Run it again with `--build` after `git pull` to get a new version; without `--build` compose keeps the images it
built. (`HARNESSLY_PULL_POLICY=missing` plus `HARNESSLY_IMAGE`, `BOARDS_IMAGE` and `ARTIFACTS_IMAGE` make it use
images from a registry you name instead.)

Open http://localhost:8022, set the password, start an agent: it gets `BOARDS_URL`, `ARTIFACTS_URL` and their tokens,
the boards and documents it creates appear as chips in the conversation and open in the side pane, and the pane lists
the conversation's documents. boards is on http://localhost:8081 and artifacts on http://localhost:8082 as well. Use
the `with-agents` target (`HARNESSLY_TARGET=with-agents` in `.env`) so Claude Code and Codex are in the image.

How it is wired:

- boards and artifacts share harnessly's network namespace (`network_mode: service:harnessly`), like the containers
  of a pod. The runtime and the agents in its tabs reach them at `http://localhost:<port>`, and all ports are
  published by the `harnessly` service, on 127.0.0.1 only: a reverse proxy on the host is what makes them public.
- The addresses people's browsers use are `HARNESSLY_PUBLIC_URL`, `BOARDS_PUBLIC_URL` and `ARTIFACTS_PUBLIC_URL`
  (default `http://localhost:<port>`, no trailing slash). They go to the services as their public URLs, to the
  runtime as `RUNTIME_BOARDS_URL` / `RUNTIME_ARTIFACTS_URL` (so agents post links that open in a browser), and the
  page's origin becomes what boards and artifacts allow in `frame-ancestors` and CORS.
- boards and artifacts use tokens from `.env` (placeholders by default; never leave them on a server). The runtime
  signs you in to boards with a hand-off; the document pane asks for the catalog's sign-in once: paste
  `ARTIFACTS_VIEWER_TOKEN` (or the editor token) in the window it opens.
- **Storage** is on Docker volumes (the `fs` drivers of boards and artifacts: one replica each, which is what this
  file runs). For S3 through a bundled MinIO, set `COMPOSE_PROFILES=minio` and `HARNESSLY_STORAGE=s3` in `.env`; MinIO
  is behind the `minio` profile and does not start otherwise. Any other S3 store works with `HARNESSLY_STORAGE=s3`
  and the `BOARDS_S3_*` / `ARTIFACTS_S3_*` variables in [`.env.example`](../.env.example). Switching drivers does not
  move data: boards and documents made under one stay in it. (Before this setting the file always used MinIO; to keep
  the data of such a stack, set the two variables above.)
- `INIT_PASSWORD` is passed through; see [the first visit](#the-first-visit).

`docker compose down` keeps the volumes (`home`, `boards-data`, `artifacts-data`, `minio-data`); `down -v` removes
them. For anything beyond your own machine follow [a public server](#a-public-server) — or use Kubernetes.

## A public server

One VPS, one domain, compose behind [Caddy](https://caddyserver.com), which gets and renews TLS certificates by itself.

**1. Three hostnames under one registrable domain, all pointing at the server** (A/AAAA records):

| Name | Serves | Behind Caddy |
| --- | --- | --- |
| `example.com` | the harnessly page | `127.0.0.1:8022` |
| `boards.example.com` | boards | `127.0.0.1:8081` |
| `docs.example.com` | artifacts | `127.0.0.1:8082` |

They must share the registrable domain: browsers send boards' and artifacts' session cookies to the frames in the page
only when the page is on the same site ([integrations](integrations.md)). For a quick try with no domain, use
`*.sslip.io`: `203-0-113-7.sslip.io`, `boards.203-0-113-7.sslip.io` and `docs.203-0-113-7.sslip.io` all resolve to
203.0.113.7, and because sslip.io is on the public suffix list the three still count as one site (and Let's Encrypt
rate-limits each address on its own). Use a domain of yours for anything you keep.

**2. Firewall: let in 22, 80 and 443 only.**

```sh
sudo ufw default deny incoming
sudo ufw allow 22/tcp && sudo ufw allow 80/tcp && sudo ufw allow 443/tcp
sudo ufw enable
```

compose binds every port to 127.0.0.1, so nothing else is reachable from outside; mind that Docker publishes a port
that is not bound to 127.0.0.1 straight through ufw. 80 is for Caddy's certificate challenge and the redirect.

**3. Caddy** (`apt install caddy`, or the [official repository](https://caddyserver.com/docs/install)), in
`/etc/caddy/Caddyfile`, then `sudo systemctl reload caddy`:

```caddyfile
example.com {
	reverse_proxy 127.0.0.1:8022
}

boards.example.com {
	reverse_proxy 127.0.0.1:8081
}

docs.example.com {
	reverse_proxy 127.0.0.1:8082
}
```

`reverse_proxy` carries WebSockets (terminals, conversations) as they are and sends `X-Forwarded-Proto: https`, which
makes the page's cookie `Secure`; there is nothing else to configure.

**4. `.env`** next to `compose.yaml`:

```sh
cp .env.example .env
{
  echo "HARNESSLY_PUBLIC_URL=https://example.com"
  echo "BOARDS_PUBLIC_URL=https://boards.example.com"
  echo "ARTIFACTS_PUBLIC_URL=https://docs.example.com"
  echo "HARNESSLY_TARGET=with-agents"
  echo "INIT_PASSWORD=$(openssl rand -hex 16)"
  for v in BOARDS_RUNTIME_TOKEN BOARDS_AGENT_TOKEN BOARDS_SESSION_SECRET ARTIFACTS_EDITOR_TOKEN \
           ARTIFACTS_VIEWER_TOKEN ARTIFACTS_SECRET MINIO_ROOT_PASSWORD; do echo "$v=$(openssl rand -hex 32)"; done
} >> .env
chmod 600 .env
grep '^INIT_PASSWORD' .env       # the password the page asks for on the first visit
```

A later line of a file wins, so these override the empty entries of `.env.example`. Do not keep the placeholder tokens
on a server: they are in this repository.

**5. Build and start, then log the agents in.**

```sh
docker compose up -d --build
```

Open `https://example.com`, enter `INIT_PASSWORD`, choose your password, and log Codex in with
`codex login --device-auth` in a terminal tab ([what the agents need](#what-the-agents-need)). Agents reach boards and
documents at their public URLs, so the server must be able to open its own hostnames (it can if DNS points at it; ufw
lets the containers in through 443).

Without Docker the proxy part is the same: run the runtime on `127.0.0.1:8022` ([systemd](#systemd-one-host-one-account))
and give the page's hostname to Caddy.

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

No image is published yet, so build one (`docker build --target with-agents`), push it to a registry your cluster
reads and set `image.repository` and `image.tag` to it. The [chart's README](../deploy/helm/harnessly/README.md)
lists every value. Run one release per person; boards and
artifacts are installed from their own charts.

## Argo CD

- [deploy/argocd/application.yaml](../deploy/argocd/application.yaml): one person's harnessly from the chart.
- [deploy/argocd/workspace.yaml](../deploy/argocd/workspace.yaml): an app of apps over
  [deploy/argocd/workspace](../deploy/argocd/workspace) — boards, artifacts and a harnessly per person, each from its
  own repository's chart. Copy the folder into your GitOps repository, set hosts, buckets and Secret names, and add a
  `harnessly-<name>.yaml` per person.

Secrets stay out of Git in both: create them with External Secrets, Sealed Secrets or SOPS.

## The preview without a gateway

The right pane's browser shows a dev server or a site through the runtime's preview proxy. Behind a gateway the gateway
gives each page its own origin; a standalone runtime does it itself, with 8 slot listeners next to its own port:

| Slot | Default port (runtime on `8022`) | Shows |
| --- | --- | --- |
| 1–6 | `8023`–`8028` | dev server ports; a port the page links to opens under its slot as `/__tk/p/<port>/` |
| 7–8 | `8029`–`8030` | sites typed as a URL |

Opening a preview (`POST /api/preview/open`, the page does it when you type a port) needs the page's session and hands
out a one-minute ticket for one slot. The slot trades the ticket for its own cookie and from then on proxies to the
target: the dev server sees neither the page's cookies nor the slot's. The page itself refuses any request whose
`Origin` or `Referer` is a slot, terminal and status sockets included, so a previewed page cannot drive the agents.

**On one machine** nothing is to configure: open `http://localhost:8022`, type `5173` (or whatever your dev server
listens on). `RUNTIME_PREVIEW_LISTEN=127.0.0.1:9100` moves the slots to `9100`–`9107`; `RUNTIME_PREVIEW_LISTEN=off`
turns the preview off. Slot ports, like the page's port, must be reachable at the same number from the browser: the
slot's address is the page's host with the slot's port.

**docker compose** publishes `8023`–`8030` on 127.0.0.1 next to `8022`. To move them, set both `HARNESSLY_PREVIEW_PORT`
(the first) and `HARNESSLY_PREVIEW_PORTS` (the range) in `.env`, for example `9100` and `9100-9107`. The dev server runs
in the container (a terminal tab), so the preview reaches it at its port there.

**Over https** a page cannot frame a plain-http slot, so the runtime refuses to open one ("set
RUNTIME_PREVIEW_PUBLIC_ORIGINS") until it knows the slots' https addresses. Give each slot a hostname and list them in
slot order:

```caddyfile
p1.example-preview.com {
	reverse_proxy 127.0.0.1:8023
}
# ... p2 to p7 the same way ...
p8.example-preview.com {
	reverse_proxy 127.0.0.1:8030
}
```

```sh
RUNTIME_PREVIEW_PUBLIC_ORIGINS=https://p1.example-preview.com,https://p2.example-preview.com,...,https://p8.example-preview.com
```

Caddy gets a certificate per hostname (or use one wildcard). Prefer a registrable domain of their own for the slots, as
above: on `localhost` and on subdomains of the page's domain the page and the slots are one site, and a previewed page
can set a cookie the page also receives (it cannot read the page's cookies or call it, but it can sign you out). With
compose, keep the published slot ports on 127.0.0.1 and let only Caddy reach them.

The Helm chart does not route the slots yet: the pod listens on `8023`–`8030`, and a Service port and an Ingress host
per slot, with `RUNTIME_PREVIEW_PUBLIC_ORIGINS` in `extraEnv`, are yours to add.

## Updates

A standalone runtime built from a release looks for a newer one: at start and then every 12 hours it makes one
`GET https://api.github.com/repos/ly-solutions/harnessly/releases/latest` (unauthenticated, 10 seconds at most, with
`If-None-Match`, through `HTTPS_PROXY` if you set it) and compares the tag with its own version. It sends nothing
else: no identifier, no usage, no version of yours beyond the standard request headers. A failure is logged at debug
level and changes nothing. When a newer release exists, the page shows a quiet "new version" notice with a link to
its notes; closing it hides that version until the next one comes out.

The check is skipped, and the notice never appears, for a `dev` build (`make run` from a clone), for a runtime a
[hub](hub/machine.md) carries (the hub serves its machines' binaries), and when you set `RUNTIME_UPDATE_CHECK=0`.
`RUNTIME_UPDATE_CHECK_URL` points the check at a mirror that answers like the GitHub API.

A standalone runtime does not update itself (a machine of a hub does, see [hub/machine.md](hub/machine.md#upgrade-move-and-remove)):
how you update is how you installed. Settings → General → Updates shows the version, the newer release and this table.

| Installed with | Update |
| --- | --- |
| a release binary | download the new archive from the [releases page](https://github.com/ly-solutions/harnessly/releases), replace `runtime`, restart it |
| `make install install-systemd` | `git pull`, `make install`, `systemctl --user restart harnessly` (the agents keep working through the restart) |
| `docker compose` | `git pull`, `docker compose up -d --build` |
| Docker | `git pull`, `docker build -t harnessly .`, then run the container again on the same volume |
| Helm | set the new image tag in the chart's values and `helm upgrade` |

## Behind a proxy

The page uses WebSockets (terminals, conversations, the status and sync sockets): give them a long read timeout
(`proxy_read_timeout 3600s` in nginx). To serve the page under a path, set `RUNTIME_APP_BASE_PATH=/agents`; the API
answers under that path too, so one proxy prefix covers both. Boards and artifacts must allow the page's origin in
their `frame-ancestors`; host all three under one registrable domain so their sign-in cookies reach the panes
([integrations](integrations.md)).
