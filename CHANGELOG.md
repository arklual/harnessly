# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); while it is in `0.x`, a minor version may change the API.

## [Unreleased]

### Added

- Agents get the workspace out of the box. A standalone runtime installs agent skills for both Claude Code and Codex
  when it starts: `harnessly` always, `boards` with `RUNTIME_BOARDS_URL`, `artifacts` with `RUNTIME_ARTIFACTS_URL`.
  Claude loads them as a plugin with `--plugin-dir`, Codex from `~/.codex/skills/harnessly`, and the `artifacts`,
  `boards` and `harnessly` launchers go to `~/.local/bin`. Files are tracked in a manifest per folder: yours are never
  touched, a release replaces only unchanged ones, a removed service's skill is deleted. `RUNTIME_AGENT_SKILLS=off`
  disables it; `RUNTIME_AGENT_BIN_DIR` moves the launchers. The skills are vendored in `skills/` at the commits pinned
  in `skills/SOURCES` (`skills/sync.sh`, `make skills-check`), so an offline install has them.
- The `harnessly` command (`cmd/harnessly`, in the image on the `PATH` and in the release archives): `open` shows a
  document, board, file, dev server or merge request in the person's right pane, `panel`, `status`, `workspaces`,
  `tab list|create|send|status|result|close` (start another Claude Code or Codex with a prompt), `api-guide`; with
  `--json`. It finds the runtime by itself from the data folder, which every pane now gets as `HARNESSLY_DATA_DIR`.
- Built-in workspace prompts for both agents when no `RUNTIME_WORKSPACE_PROMPTS_FILE` is set (the open install had
  none): where the agent is, the `harnessly` command, and the `artifacts` and `boards` commands when configured.
- `GET /api/cli/api-guide` answers with a built-in guide of the `/api/cli` routes when the deployment names none.
- `open artifacts` accepts links to separate document and board servers (`RUNTIME_ARTIFACTS_URL`,
  `RUNTIME_BOARDS_URL`), and plain-http ones on this machine; the reader path follows `RUNTIME_APP_BASE_PATH`.

- Boards and artifacts integrations. `RUNTIME_BOARDS_URL` / `RUNTIME_ARTIFACTS_URL` with their tokens give every
  tab the runtime starts or resumes `BOARDS_URL`, `BOARDS_TOKEN`, `ARTIFACTS_URL`, `ARTIFACTS_TOKEN`,
  `ARTIFACTS_SESSION` (the tab's id) and `ARTIFACTS_AUTHOR`. New routes for the page: `GET /api/integrations`,
  `POST /api/integrations/boards/sign-in` (a one-time boards hand-off) and
  `GET /api/integrations/artifacts/documents?session=`. In gate mode they exist only once a service is configured.
- The web page has a side pane with Board and Document tabs: the boards editor signed in through the hand-off, the
  artifacts embed view following the page's theme, and the current conversation's documents. Board and document links
  in conversations become chips that open there.
- Ways to run: a multi-stage `Dockerfile` (with a `with-agents` target that adds Claude Code and Codex from npm), a
  `compose.yaml` with boards, artifacts and MinIO wired together, the Helm chart `deploy/helm/harnessly`, Argo CD
  examples (one Application, and an app of apps for the whole workspace), a systemd user unit and `make run`,
  `install`, `install-systemd`, `image`, `compose-up` targets. `docs/deploy.md` and `docs/integrations.md` describe
  them.
- `tmux.Config.PaneEnv` and `Tmux.TabOf`: extra variables for a new session's pane shell, keyed by its tab.
- GitHub workflows: tests with the race detector, a build for linux and darwin on amd64 and arm64, golangci-lint,
  CodeQL, OpenSSF Scorecard, gitleaks, and releases with GoReleaser on version tags.
- A Makefile with the developer targets (`build`, `test`, `test-race`, `lint`, `fmt`, `vet`, `cover`, `cross`,
  `clean`), an `.editorconfig` and a golangci-lint configuration the code passes.
- Documentation in `docs/`: getting started, architecture, slices and configuration; a code of conduct, issue and
  pull request templates, CODEOWNERS and Dependabot.
- Brand assets: wordmarks for light and dark backgrounds, a square avatar and the social preview.
- `tools/publish-tree.sh` exports one commit as a fresh single-commit repository, refusing when it finds a forbidden
  word or a secret.
- A gitleaks configuration with narrow allowlists for test fixtures.

- `POST /api/cli/tabs/<tab>/send` takes `keys`, an array of tmux key names (`C-c`, `Escape`, `Enter`, `Up`, …) pressed
  after the text or alone: control characters in `content` were never typed.
- `INIT_PASSWORD` is handled by the page: it asks for the initial password first, then offers the setup. compose,
  `.env.example` and the systemd env example carry it.
- `compose.yaml` takes the public addresses (`HARNESSLY_PUBLIC_URL`, `BOARDS_PUBLIC_URL`, `ARTIFACTS_PUBLIC_URL`) and
  derives the frame and CORS origins from them; MinIO is behind the `minio` profile. `docs/deploy.md` has a section for
  a public server: three hostnames, Caddy with automatic TLS, ufw, and `*.sslip.io` for a quick try.

### Changed

- `RUNTIME_CODEX_SANDBOX=container`, the Docker image's default, starts Codex without its own sandbox (which cannot start
  in a container): `sandbox_mode="danger-full-access"`, `approval_policy="never"`. Elsewhere Codex keeps its own.
- A standalone runtime writes the Codex launcher with defaults (the data folder, `HARNESSLY_CLI_TOKEN`), so Codex
  conversations start in Docker, compose, Helm and systemd without `RUNTIME_SCRIPT_DATA_DIRS` and
  `RUNTIME_MCP_TOKEN_ENV`. A conversation that cannot start answers `500` with the reason instead of an empty error.
- A `claude-code` or `codex-cli` tab made through `POST /api/cli/tabs` starts its agent without a `prompt` too; it used
  to become a plain shell.
- The standalone gate answers a `/api/cli/` request refused for lack of the cli-token with a `403` that names the
  header (a page session cookie never worked there).
- `compose.yaml` builds the images (`docker compose up -d --build`; nothing is published) under local `:local` tags and
  never tries to pull them. boards and artifacts store on volumes by default; set `COMPOSE_PROFILES=minio` and
  `HARNESSLY_STORAGE=s3` to keep using MinIO as before.

- The module path is `github.com/ly-solutions/harnessly` (it was `harnessly`); imports and the version ldflags follow.
- Release runtimes (GoReleaser and the GitLab tag build) carry the web page (the `webui` build tag). In gate mode it
  stays off unless `RUNTIME_WEB=on`, so machines running in front of a previous runtime answer as before. `make build`
  still builds a runtime without the page.
- Links in agent replies open in a new tab.
- The README is rewritten around a quick start that runs the runtime standalone; the configuration tables and the
  slice details moved to `docs/`.
- Golden test data stores fake provider credentials as placeholders built at run time.
- Every text the runtime shows people follows `RUNTIME_LOCALE`: English by default, Russian with `RUNTIME_LOCALE=ru`
  (the previous texts, byte for byte). That covers error bodies, notifications, the shared snapshot page and file, the
  command catalog, the design library pages and the prompts it writes for agents, preview pages and limit messages.

### Fixed

- A first start of Claude Code or Codex in a new folder no longer hangs at «Starting». Both ask whether to trust the
  folder, and the dialog was only in the terminal, so the agent waited there unseen while the messages sent to it
  piled up as pending. The runtime now answers yes for the folder the conversation was started in;
  `RUNTIME_FOLDER_TRUST=ask` leaves the dialog to the person.
- A plain terminal tab opened from the sidebar shows its terminal; it was a blank page.
- Codex in code mode no longer shows a bare `exec`: the timeline entry, the push text and the current-action line name
  the commands, searches and other tools the script calls.
- Titles of board and document chips are looked up by origin, so boards and artifacts served over http on other ports
  of one host (the compose workspace) get them too. `RUNTIME_ARTIFACTS_URL` and `RUNTIME_BOARDS_URL` name those origins.
- Stopping the design library's server also stops its background history and waits for a snapshot under way.
- A pane command a page applied before the runtime started waiting for it is reported as applied.
- Tests no longer depend on the machine's umask, wait for the standalone run's panes to exit, and the timeline golden
  scenario waits for events instead of time.
- Codex dropped `BOARDS_TOKEN` and `ARTIFACTS_TOKEN` from the environment of its commands (it removes variables named
  like secrets), so its agents could not use boards and artifacts. The runtime now starts Codex with
  `shell_environment_policy` that keeps them and leaves only its own CLI token out.

## [0.9.0] - 2026-10-19

The first version that runs a machine without the previous runtime. 0.1.0 to 0.8.0 were internal pre-releases that
built up to it; this entry summarises everything the project holds.

### Added

- **Machine agent** (`cmd/agent`): one process per Unix account with a single WebSocket link to the cluster (wire
  protocol v1), serving the agents' transcripts with change events, the runtime's state files with versioned writes,
  the status and sync sockets relayed, TCP tunnels to the runtime and the disk service, and token-guarded loopback
  routes. `cmd/agentprobe` drives an agent the way the cluster does, for compatibility diffs and benchmarks.
- **Runtime gate** (`cmd/runtime`): holds the runtime's port and answers the slices switched on per machine in
  `state-mode.json`, re-read every second; hands everything else, sockets included, to the previous runtime over
  pooled connections; runs read-only slices in shadow and logs where the answers differ; records traffic and logs
  per-path proxy statistics.
- **Standalone mode**: with no previous runtime the gate is the whole runtime and does its start-up work itself: the
  single-instance lock, the config and shared secret, the `cli-token`, the hook, status line and Codex hook scripts,
  the agents' settings and MCP files, the prompt editor and the Codex launcher.
- **Core**: the tab state keeper fed by hooks, transcripts, screens and the process tree; the layout and workspace
  stores; the status and sync sockets; the prompt queue and delivery into tmux panes; hook routes; web push; idle sleep
  of agents; plugin and skill reloads in idle Claude tabs.
- **tmux control layer** and parsers of the agents' terminal screens; the terminal socket as a pty on `tmux attach`.
- **Claude Code and Codex support**: detection in a pane, launch and resume lines, the process table, background work,
  subscription limits with a forecast, session history and statistics.
- **Slices** of the previous runtime's routes in Go: `fsread` (machine and file reads, git answers, slash commands,
  launch choices), `fswrite` (uploads, editor saves, repository sync), `machines` (machine snapshot, load, health),
  `launch` (conversations, automation and eval runs, side chats), `transcripts` (links, message counts, usage report,
  statistics, conversation snapshots), `timeline` (conversation socket, status line sink, economics, subagents, turn
  checkpoints), `design` (design library with its server, history, imports and headless checks), `preview` (reverse
  proxy with DevTools and an MCP server), `services` (GitLab, link previews, tracker cards, site icons), `voice`
  (dictation) and `state` (settings files, automations and the remaining routes).
- Byte-for-byte compatible answers (status codes, JSON key order, headers, ETags) pinned by golden tests recorded from
  the previous implementation's handlers.

[Unreleased]: https://github.com/ly-solutions/harnessly/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/ly-solutions/harnessly/releases/tag/v0.9.0
