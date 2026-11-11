<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/brand/harnessly-wordmark-dark.svg">
    <img src="docs/brand/harnessly-wordmark-light.svg" alt="harnessly" width="420">
  </picture>
</p>

<p align="center">
  <strong>Run Claude Code and Codex on your own servers. They keep working with the laptop closed,<br>
  you answer them from your phone, and you see what each one is doing and what it costs.</strong>
</p>

<p align="center">
  <a href="https://github.com/ly-solutions/harnessly/actions/workflows/ci.yml"><img alt="CI" src="https://img.shields.io/github/actions/workflow/status/ly-solutions/harnessly/ci.yml?branch=main&label=ci"></a>
  <a href="https://github.com/ly-solutions/harnessly/actions/workflows/lint.yml"><img alt="Lint" src="https://img.shields.io/github/actions/workflow/status/ly-solutions/harnessly/lint.yml?branch=main&label=lint"></a>
  <a href="https://pkg.go.dev/github.com/ly-solutions/harnessly"><img alt="Go Reference" src="https://pkg.go.dev/badge/github.com/ly-solutions/harnessly.svg"></a>
  <a href="https://goreportcard.com/report/github.com/ly-solutions/harnessly"><img alt="Go Report Card" src="https://goreportcard.com/badge/github.com/ly-solutions/harnessly"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/ly-solutions/harnessly"><img alt="OpenSSF Scorecard" src="https://api.scorecard.dev/projects/github.com/ly-solutions/harnessly/badge"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ly-solutions/harnessly"></a>
  <a href="https://github.com/ly-solutions/harnessly/releases"><img alt="Release" src="https://img.shields.io/github/v/release/ly-solutions/harnessly?sort=semver"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/github/license/ly-solutions/harnessly"></a>
</p>

<p align="center">
  <a href="#features">Features</a> ·
  <a href="#quick-start">Quick start</a> ·
  <a href="#apps-and-plugins">Apps</a> ·
  <a href="#self-hosted-hub">Hub</a> ·
  <a href="#releases-and-verification">Releases</a> ·
  <a href="docs/comparison.md">Compared</a> ·
  <a href="docs/README.md">Docs</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

<p align="center">
  <img src="https://raw.githubusercontent.com/ly-solutions/harnessly/assets/demo.gif" alt="Three Claude Code agents work on one server; one asks a question and gets the answer from a phone; then the statistics page shows what they did and what it cost" width="960">
</p>

```sh
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh
```

One command for Linux and macOS: the latest signed release, checked, started as a service of your account on
<http://127.0.0.1:8022>. Then open the page and choose your password. [Other ways](#quick-start) ·
[how it compares](docs/comparison.md).

---

harnessly is a workspace where people and coding agents work side by side. Claude Code and Codex run as the real CLIs in
tmux panes on your machines, next to your repositories and tools, and keep running when nobody has a page open.
harnessly knows what each agent is doing and serves it through one HTTP and WebSocket API and a web UI. It is a Go
runtime plus a machine agent on the user's machine (small static binaries, no cgo), a web UI compiled into the runtime,
and an optional hub for teams: sign-in, spaces, and machines connected through one address.

## Features

### Agents and chats

- Claude Code, Codex and plain shells in tabs on the runtime's own tmux server. A terminal socket attaches a pty to any
  of them.
- Live tab states (`busy`, `needs-input`, `ready-for-review`, ...), derived from the agent's hooks, status line,
  transcript, screen and process tree and pushed to every client.
- Conversations: a timeline over the agents' transcripts (turns, tool runs, plans, questions), subagents, token and
  cost economics, turn checkpoints as git snapshots. The composer takes slash commands, files, context and dictation.
- Prompt delivery through a per-tab queue; launch and resume of agents after a restart; Claude agents idle for too long
  are stopped to free memory and resumed by the next prompt.
- Usage statistics and provider limits with a forecast, from the transcripts.
- Agents can start, brief and read other agents in new tabs with the `harnessly` command line tool
  (`harnessly tab ...`, `harnessly peers`).

### Files, changes, terminal and browser pane

The right pane of a conversation holds a terminal, a file editor, changed files and diffs, a browser for the machine's
dev servers (a reverse proxy with a DevTools record that agents read through an MCP server), documents and boards
([integrations](docs/integrations.md)), GitHub and GitLab, subagents and the session's cost. Agents can open a tab of
the pane for you.

### Sinopia Design

A design space for prototypes, sites and presentations. Every design is a plain folder on your machine, with its page
model, history and exports (PDF, PNG, PowerPoint, one offline HTML file for a deck). Designs work right after install, with an API
key or any agent: boards run on the engine built into harnessly. If you have a claude.ai account, you can also add
Anthropic's engine (optional, Settings → the design section has the command):

```sh
harnessly design engine fetch
harnessly design engine status      # what the machine has: engine, browser, ffmpeg
```

Details: [docs/design-engine.md](docs/design-engine.md), [docs/design-pages.md](docs/design-pages.md).

### Apps and plugins

Extend harnessly without forking it. An app is a Claude Code plugin plus one manifest (`harnessly/app.json`): you write
it once and turn it on where you work, in a chat, a tab of the pane, a full screen of its own or a Sinopia document.
A plugin is an app without views.

- **Plugins** change how the agent works where they are on: instructions, skills, slash commands, MCP servers. No code
  runs.
- **Views** are static pages of your package: a tab of the pane, a card in the chat, the app's own screen, a panel or a
  check in a Sinopia document. They talk to the host through a small SDK (`chat.read`, `pane.open`, `storage`,
  `sinopia.*`, ...).
- **Backend** is yours, not the host's: a remote MCP server, or, for a trusted app, a local program.
- **Safe by default.** Every call needs a permission named in the manifest, every view runs in a sandboxed frame on an
  origin of its own, and an app declares its trust tier: `declarative`, `sandboxed` or `trusted`.
- **Sharing.** Versions and channels (dev, beta, stable), the team's and the public catalog, links, and evals that
  gate an app's way to stable.

```sh
harnessly app new acme.cashback --template pane-chat   # start from a template
harnessly app dev ./cashback                            # install the folder itself while you write it
harnessly app validate ./cashback --strict              # every problem names its field
harnessly app publish ./cashback --channel beta         # a beta channel; evals gate the way to stable
harnessly app docs                                      # the whole guide, built into the binary
```

Start with [the guide](docs/build-apps/README.md) ([in Russian](docs/ru/build-apps/README.md)): eight chapters from an
empty folder to a published app. Look up a field, an SDK call or a limit in [the reference](docs/apps.md). Five
complete examples in [docs/apps](docs/apps):

| Example | What it shows |
| --- | --- |
| [mr-review](docs/apps/mr-review) | a plugin only: reviews a merge request in the chat, reads the diff, edits and pushes nothing |
| [cashback](docs/apps/cashback) | a view in the pane and a card on a Sinopia site: the person picks, the agent sees the choice |
| [standup](docs/apps/standup) | the [worked example](docs/build-apps/08-worked-example.md): the agent collects updates, a view edits them, a local program keeps them |
| [deck-lint](docs/apps/deck-lint) | a check in Sinopia presentations: marks slides with too much text and blocks sharing until they are fixed |
| [moodboard](docs/apps/moodboard) | a new Sinopia document type built entirely from the app SDK, with a schema and its migration |

### The safe and connections

- [The safe](docs/safe.md) keeps tokens, passwords and keys for the agents. A secret never travels through the chat:
  the agent asks by name, the person puts it in, and the agent runs a command with it, the output masked.
- [Connections](docs/connections.md): Jira, Confluence, YouTrack, Linear, GitLab, GitHub and wikis, connected once by a
  pasted link. Agents read them with `harnessly issues` and `harnessly pages`.
- [MCP servers](docs/mcp-connections.md) connected in Settings reach every Claude Code and Codex chat; the runtime
  keeps the credentials and signs the requests.

### Voice input

A microphone in the message field. The recording is turned into text on the machine by a built-in
[whisper.cpp](https://github.com/ggml-org/whisper.cpp) server; nothing is sent to an outside service. The model is
downloaded once, on the first dictation (`base` or `small`, by processor count). Needs `ffmpeg` (included in the Docker
images) and `https://` or `localhost`. Turn it off with `RUNTIME_VOICE_ENGINE=off`. See [docs/voice.md](docs/voice.md).

### Ask a human

An agent can ask a person a question or request an approval, from the shell:

```sh
harnessly ask people                                  # who may be tagged, who may approve
harnessly ask --to NAME --option A --option B QUESTION...
harnessly ask approve --to NAME --link URL WHAT...
```

The question goes to a Telegram chat through a bot you linked, with the person tagged and answer buttons; the person
answers in Telegram or on the page. `harnessly ask` waits for the answer (exit 4 when it does not come in time; for
`approve`, exit 0 means approved). Bots, contacts and approvers are set in Settings, Notifications, in the People
section. Agents can also send plain messages with `harnessly notify send`.

### Notifications and the update notice

- Web push when a turn ends or an agent asks for input; Telegram bots as above.
- A standalone runtime built from a release checks the project's GitHub releases at start and every 12 hours and shows
  a quiet "new version" notice with a link to the notes. It follows the `stable` channel unless
  `RUNTIME_UPDATE_CHANNEL=latest` ([update channels](docs/self-host/update-channels.md)). The check sends nothing but the standard request headers;
  `RUNTIME_UPDATE_CHECK=0` turns it off. See [Updates](docs/deploy.md#updates).

### Hub

For a team, the hub is the one address people sign in at.

- Spaces and invitations (by email or by link); `HUB_SIGNUP=invite` limits sign-up to administrators and invited people.
- Sign-in by email code, passkey, GitHub or Yandex ID (and any OIDC provider). A way without its settings is not offered.
- Machines connect outbound with one command, so they need no open port. In subdomain mode (`HUB_RUNTIME_DOMAIN`) a
  machine opens at `<id>.<machines domain>`, one origin per machine; otherwise at `/m/<id>/` on the hub.

## Quick start

### One command

```sh
curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh
```

The installer puts the latest release into `~/.local/bin`, checks the archive against the release's `checksums.txt`
(and its sigstore signature when `cosign` is installed), installs `tmux` and `git` when they are missing, and starts
the runtime as a systemd user service on Linux or a launchd agent on macOS, listening on `127.0.0.1:8022` only. Run it
again to update. Options go after `sh -s --`: `--version X.Y.Z`, `--port N`, `--no-service`, `--uninstall` (keeps
your data in `~/.harnessly`); `--help` lists them all. On a remote server it prints the `ssh -L` command that brings
the page to your computer. Details: [docs/deploy.md](docs/deploy.md#quick-start-with-the-installer).

### Docker

```sh
docker run -d --name harnessly -p 127.0.0.1:8022:8022 -v harnessly-home:/home/agent ghcr.io/ly-solutions/harnessly:latest
docker logs harnessly | grep -i password
```

Open <http://127.0.0.1:8022>, enter the `Initial password` the second command printed, and choose your own. Workspaces,
data and the agents' logins live on the `harnessly-home` volume. The image has no agent CLIs; use
`ghcr.io/ly-solutions/harnessly:with-agents` for Claude Code and Codex. Tags: `X.Y.Z` (pin this in production), `X.Y`,
`latest`, and the same with a `-with-agents` suffix. Images are for linux/amd64 and linux/arm64.

### Release binary

Needs `tmux` and `git`. Each [release](https://github.com/ly-solutions/harnessly/releases) has
`harnessly_<version>_<os>_<arch>.tar.gz` for `linux` and `darwin` on `amd64` and `arm64`, with the `runtime` (the page
is compiled in), the `agent` and the `harnessly` command line tool.

```sh
VERSION=$(curl -fsSL https://api.github.com/repos/ly-solutions/harnessly/releases/latest | sed -n 's/.*"tag_name": *"v\([^"]*\)".*/\1/p')
OS=linux ARCH=amd64                     # OS: linux or darwin; ARCH: amd64 or arm64
BASE=https://github.com/ly-solutions/harnessly/releases/download/v$VERSION
ARCHIVE=harnessly_${VERSION}_${OS}_${ARCH}.tar.gz
curl -fsSLO "$BASE/$ARCHIVE" -O "$BASE/checksums.txt" -O "$BASE/checksums.txt.sigstore.json"
# verify first, see "Releases and verification" below
tar xzf "$ARCHIVE"
RUNTIME_GATE_LISTEN=127.0.0.1:8022 RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ ./runtime
```

Open <http://127.0.0.1:8022> and choose your password. Read [the first visit](docs/deploy.md#the-first-visit) before
you listen on anything other than `127.0.0.1`. To keep it running, use
[systemd](docs/deploy.md#systemd-one-host-one-account).

### From source

```sh
git clone https://github.com/ly-solutions/harnessly.git && cd harnessly
make run        # builds web/ and the runtime with the page, serves http://127.0.0.1:8022
```

Needs Go (the version in [`go.mod`](go.mod)), Node.js 22 with pnpm, tmux and git. `docker compose up -d --build` starts
the whole workspace with boards and artifacts on <http://localhost:8022>; Helm and Argo CD examples are in
[docs/deploy.md](docs/deploy.md). To drive the runtime's HTTP API by hand against a throwaway home, see
[Getting started](docs/getting-started.md).

Agents need their CLIs installed and logged in where the runtime runs (`npm install -g @anthropic-ai/claude-code`,
`npm install -g @openai/codex`); see [what the agents need](docs/deploy.md#what-the-agents-need).

## Self-hosted hub

```sh
git clone https://github.com/ly-solutions/harnessly && cd harnessly
docker compose -f compose.hub.yaml up -d --build      # the hub and one machine, http://hub.localhost:8040
```

Only `HUB_PUBLIC_URL` is required. The hub image is built locally from this repository. Without a mail server the
sign-in code is in the hub's log, marked `DEV`. Connect other machines with the installer the hub serves:

```sh
curl -fsSL https://hub.example.com/install | sudo sh -s -- --token <code> --profile dir
```

Profiles are `dir`, `container` and `full`. Caddy with automatic TLS, systemd, Helm and the subdomain mode:
[docs/hub/deploy.md](docs/hub/deploy.md); connecting a machine: [docs/hub/machine.md](docs/hub/machine.md).

## Releases and verification

A pushed `vX.Y.Z` tag runs the [release workflow](.github/workflows/release.yml):

- GoReleaser builds the archives for linux and darwin (amd64, arm64) and makes a GitHub release with `checksums.txt`
  and its keyless cosign signature `checksums.txt.sigstore.json`. The archives get a build provenance attestation.
- A multi-arch image (linux/amd64, linux/arm64) is pushed to `ghcr.io/ly-solutions/harnessly`, signed keyless with
  cosign and attested.
- Nothing is signed with a key kept anywhere: signatures and attestations use the run's GitHub OIDC identity.

Verify an archive (needs [cosign](https://docs.sigstore.dev/cosign/installation/) and, for provenance, the
[GitHub CLI](https://cli.github.com/)):

```sh
cosign verify-blob checksums.txt --bundle checksums.txt.sigstore.json \
  --certificate-identity "https://github.com/ly-solutions/harnessly/.github/workflows/release.yml@refs/tags/v$VERSION" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
grep " $ARCHIVE\$" checksums.txt | sha256sum -c -          # on macOS: shasum -a 256 -c -
gh attestation verify "$ARCHIVE" --repo ly-solutions/harnessly
```

Verify the image:

```sh
cosign verify ghcr.io/ly-solutions/harnessly:latest \
  --certificate-identity-regexp '^https://github.com/ly-solutions/harnessly/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/ly-solutions/harnessly:latest --repo ly-solutions/harnessly
```

## Docs

| | |
| --- | --- |
| [Compared](docs/comparison.md) | harnessly next to Happy, Paseo, T3 Code, Orca and Claude Code Remote Control |
| [Getting started](docs/getting-started.md) | build, run standalone, open tabs, run agents, connect the machine agent |
| [Deploy](docs/deploy.md) | quick start, verification, updating, systemd, Docker, compose, Helm, Argo CD |
| [Configuration](docs/configuration.md) | every `RUNTIME_*` and `AGENT_*` variable |
| [Build apps and plugins](docs/build-apps/README.md) | the guide: from an empty folder to a published app, views and backend, with diagrams and examples ([in Russian](docs/ru/build-apps/README.md)) |
| [Apps reference](docs/apps.md) | every manifest field, SDK call, limit and security layer of apps |
| [Example apps](docs/apps) | mr-review, cashback, standup, deck-lint, moodboard: complete packages to copy from |
| [The safe](docs/safe.md) | secrets for the agents without the chat: storage, the ask flow, the threat model |
| [Connections](docs/connections.md) | trackers, wikis and code hosts by a pasted link; the agents' `harnessly issues` |
| [MCP servers](docs/mcp-connections.md) | remote MCP servers from Settings: OAuth sign-in, the proxy, how the agents get them |
| [Voice input](docs/voice.md) | the microphone, the built-in recognition, how to change or turn it off |
| [Board engine](docs/design-engine.md) | the built-in engine, the optional vendor engine, moving upstream changes over |
| [Render kit](docs/render-kit.md) | pinned Chrome, ffmpeg and fonts; `harnessly doctor render` |
| [Design pages](docs/design-pages.md) | the page model of a design project and the page commands |
| [Integrations](docs/integrations.md) | boards, artifacts and the agent skills |
| [Architecture](docs/architecture.md) | the runtime gate, the core, tmux, the machine agent, the repository layout |
| [Slices](docs/slices.md) | the slices of routes and switching them per machine |
| [The hub](docs/hub/deploy.md) | [deploy](docs/hub/deploy.md), [connecting a machine](docs/hub/machine.md), [architecture](docs/hub/architecture.md), [API](docs/hub/api.md) |
| [Changelog](CHANGELOG.md) | what changed in each version |
| [Contributing](CONTRIBUTING.md) | development setup, tests, commit convention; [Security](SECURITY.md), [Code of Conduct](CODE_OF_CONDUCT.md) |

harnessly is young: versions are `0.x` and a minor version may change the API.

## License

[Apache License 2.0](LICENSE). Third-party licences: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
