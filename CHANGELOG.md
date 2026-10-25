# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); while it is in `0.x`, a minor version may change the API.

## [Unreleased]

### Added

- Designly: «Ссылки» is one screen with one form. «Кто откроет» chooses between a link for anyone without sign-in and an
  invite for the signed-in; one list shows both kinds, each marked, and a made link is copied at once. A link without
  sign-in carries the look the owner gave the design (device, browser bars, fit, lock, page) so a guest opens it the
  same way.
- Designly: a link without sign-in. «Настройки ссылок» of the share dialog has a «Ссылка без входа» block above the invite
  links: a name, «Может смотреть» or «Может комментировать», a term (1 hour to 30 days, or none); making a link copies
  it at once, and a menu extends it by 7 days, changes what it allows or revokes it. The runtime answers the gateway
  at `design-public/<project>` (the board list, the project files, `support.js` of the project's release) for a
  60-second token only; hidden files, paths out of the project and symlinks out of it are not served.

### Fixed

- A message with a picture Claude does not show as «[Image #1]» in 8 s (5 s before) no longer fails try after try: it goes
  with the picture's path as text, path and text under one Enter, and Claude reads the file by its path.
- A picture's path no longer stays in the agent's input line. Claude can read a big picture after the typist gave up on
  it, and the path then landed as plain text, holding up the next message as «text in the terminal's input line». A
  box that holds only picture paths the harness pasted is cleared before the next message. A try whose text stays in
  the box is typed again, backing off, up to 10 tries (6 before) before the message fails, Enter is pressed again up
  to 10 times for a message the agent did not take, and a Stop whose Escape did not go in is pressed again.
- A turn waiting on a slow model is no longer marked done. 15 s of silence with the box up and no spinner read ended
  the turn while Claude was still waiting for the reply (96 s seen), so Stop was refused as «not working» and a model
  switch hit the box. A Claude turn the transcript keeps open now ends on silence only when the screen says it ended
  (its summary «✻ Brewed for 8s», or the stop mark), or after 10 minutes of silence; the Stop hook and the transcript
  end it as before.
- A message typed into the terminal no longer stays there unsent. A long line typed under load, its Enter taken into
  the text as a newline as the agent's step ended, read as sent because the box looked empty before the agent drew it;
  the message then showed «in the agent's queue» for half an hour while its text sat in the input line, and every
  message after it waited on that text until it expired. The typist now counts a message sent only once the box stayed
  free of it for 600 ms, a message typed into the turn is looked for in the box while it waits, and its own text there
  gets its Enter again (or fails it, the box cleared); a message held by someone else's text says «the terminal's
  input line holds text». A steered message the person pulled back by stopping the turn (Claude returns it to the box
  for editing) is not Entered again: it shows «back in the terminal's input line», to retry or remove.
- A message sent while the agent works is never lost and does not jump: it stands once in the stack over the
  field — «sending» at once, with a pencil (back into the field) and a bin — until the agent has it, then once in the
  feed at its place; a message typed into the turn (⌃↵) stands in the stack «the agent reads it after the current step»
  and, once read, among the turn's steps and under its fold (it used to vanish when the agent read it). The live
  indicator is never over the person's last message, «not confirmed» no longer shows for a message that only waited in
  the queue, a queued message keeps its reason instead of swinging between «agent busy» and «behind», and a message to
  a free agent whose screen still draws the last turn's spinner is typed within 300 ms of the screen freeing, not 1.5 s.
- A message sent during a rollout (the runtime or the machine restarting, a gateway's 502/503, a lost answer, a dropped
  socket) is no longer «not sent: the machine refused it»: the road failing is not a refusal. The bubble
  waits, quietly and without red («waiting for the machine»), and goes by itself with the same client id — after 1, 2, 4 …
  30 s, at once when the socket is back, after a reload too — and the runtime answers a repeat with what it answered
  before, so it never lands twice. Red «not sent» with its reason in words («the message is too long», «the queue is
  full») is only for what the runtime refused on the merits; the page has no «not confirmed» of its own any more.
- A queued message is delivered or fails visibly, never waits forever: a picture reference the agent's input box
  wrapped midway (`[Image` / `#104]`) counts as pasted, a retry reads the whole box and finishes its own pictures
  instead of taking them for someone else's draft, failed tries back off and give up after six with the reason on the
  message, and Retry and Delete work on it. Every wait of the queue goes to the log.
- The composer shows the runtime's queue and background tasks from the status socket, and one place picks the machine
  of a runtime call, so a conversation drawn without a frame gets its Stop and send.
- The frame of a fleet machine (`?machine=<id>` in its address) sends its API calls and sockets to that machine too;
  behind a gateway that routes by the parameter they went to the default machine, and a conversation of another machine
  did not open.

### Added

- Chats branch and hand over. «Ответвить» in a chat's menu, or the fork mark under a finished answer, opens a new chat
  with the history up to that turn (Claude's session is copied and resumed; Codex forks its whole session at the last
  turn, or starts from an excerpt); the new chat says where it branched off and links back. «Влить обратно» has the
  fork's agent write a short result, editable before it goes, and sends it into the original chat's queue. A model of
  the other agent in the field's picker hands the chat over: a preview of an excerpt (the first request, the latest
  turns, key command results, within the new model's window) or of a note the agent writes, editable, starts a new
  chat in the same folder; the old chat stays as it is, and the new one shows «Передача контекста · A → B».

- Going on from an earlier message in the new interface. A message of the person's offers «Edit and continue from
  here» on hover (a long press on the phone) and becomes an editor in place; sending asks «Conversation only» or
  «Conversation and files» — the folder back to the snapshot taken after the turn before, with the files that would
  change listed, disabled when there is none. The turns from that message on fold into «Rewound N turns», which opens
  to show them read only, and «Undo» takes the rewind back for 10 s. The runtime forks the agent's transcript before
  the message into a new session (Claude Code and Codex both resume it as their own) and resumes the chat there, so
  the old branch stays whole; an agent at work is stopped first, and the first message of a chat has nothing to go on
  from.
- The agent's answer draws what it writes: a `mermaid` block is a diagram (themed, a click opens it full size, the
  source copies), an `html` block is a page in a sandboxed frame that has no access to the page, its cookies or the
  network, with a toggle to its source and a full-size view, and a link to the installation's own document or board,
  alone in its paragraph, is a card. The agents' built-in prompts say so. The diagram renderer loads with the first
  diagram, not with the page.
- Chats that stop on the agent's usage limit go on by themselves when it resets, and chats a restart cut short go on
  once their agent is back. The feed ends with a calm card «Лимит Claude исчерпан · продолжу автоматически в
  18:00 (через 2 ч 14 мин)» with «Продолжить сейчас» and «Не продолжать»; what the person sends meanwhile waits for the
  reset («Отправится после сброса лимита в 18:00») and goes first. The sidebar shows a quiet clock on held chats, and the
  feed marks where the work went on («Продолжил после сброса лимита», «Прервано перезапуском · продолжил
  автоматически»). Settings → General has both switches, on by default (command `limit`, feature `auto-continue`).
- Designly: who sees a design. A new design is personal; the owner opens it to the team (view, comment
  or edit), gives a person a role for an hour, a day, a week, a month, to a date or for good, takes it back one by one,
  makes a link for a logged-in colleague (up to 7 days, a number of uses) and sees the access log. A colleague without
  access asks for it from the page and the owner gets a push to allow for a term or refuse. The platform keeps the
  rights; the runtime gets a snapshot per person and hides closed projects everywhere — the library, its pages and
  files, the CLI and the agent's tools answer as if the project were not there. Projects made before stay open to the
  team; an ownerless one can be taken. Without the platform nothing changes.
- Boards load faster: React and Babel are served by the runtime instead of unpkg, the design's heavy files get
  hashed addresses cached for good, and a page switch inside a design moves the frame without a new slot login.

- Chat terminals can be ended and stop by themselves when idle. «Завершить терминал» in the chat's menus
  (sidebar, header «…», the phone sheet) asks first, then kills the chat's tmux session and everything under it; the
  chat and its history stay and the next message resumes the agent (command `terminal.stop`, feature `terminal-stop`).
  The runtime also stops a terminal that has stood idle for the window of «Останавливать простаивающие терминалы»
  (off, 1 h, 2 h by default, 6 h; `GET/PUT /api/v2/runtime-settings`): only when the agent is idle with no background
  work, gate, queue or viewer, nothing but the agent and its helpers runs in the pane, the pane's screen has not changed
  and the tree has used next to no CPU. The feed says «Терминал остановлен: 2 ч без работы»; chats without a running terminal
  are dimmed in the sidebar and the rail.
- The «More → Apps» screen is a left list of the connected apps with the chosen one open as a page on the right, and it
  scales to many apps: the tracker page lists all the epics and tasks assigned to the person (the list used to
  keep only the task-card keys, 8 epics and the stale ones of anyone), an epic opens into its subtasks with progress, and
  both are searched, filtered by status, tag, type and epic, sorted and grouped; the GitLab page shows the pipeline,
  approvals, draft, conflicts, open threads, author, assignee, labels and how long a request stands still, with filters by
  state, author, assignee, project, pipeline and condition and a search; each page has its own settings kept in the
  browser, and «To chat» still takes rows into a new chat as mentions. Wire: the feature `apps-hub`; the sections are
  `epics`/`tasks` and `mrs`, and a row carries the fields above (see internal/wire/README.md, item 47).
- The messages waiting in the queue over the field can be put in another order: the grip on the left of a row
  carries it (the mouse, or a finger after it rests; ↑ ↓ with the grip focused move it a place at a time), a line shows
  where it would land, and the order changes when the runtime has said so — the other rows then slide to their places.
  Wire: `queue.move {opId, conversationId, clientId, beforeClientId | null}` and the feature `queue-move` (only a queued
  message moves; a place across a message sent «now» is `refused{conflict}`, a message already typed `refused{too-late}`);
  a runtime without it draws no grip. A long queue now scrolls inside itself instead of pushing the chat off the screen.
- The right pane's browser works without a gateway: a standalone runtime opens the preview itself
  (`POST /api/preview/open`) in 8 slot listeners next to its port, each an origin of its own, and proxies a dev server or
  a site there; the page refuses requests coming from a slot. `RUNTIME_PREVIEW_LISTEN` moves or turns off the slots,
  `RUNTIME_PREVIEW_PUBLIC_ORIGINS` names them behind an https proxy; compose and the image publish `8023`–`8030`. A dev
  server that is down now says so instead of "preview not supported".
- `runtime web export <dir>` writes the page this runtime would serve, with this installation's settings filled in, into
  a folder: a gateway in front of the machines can serve the page itself, also while a machine is down.
- `RUNTIME_APP_MARK_URL`: an installation's own product mark. The page draws it in place of the harnessly mark, and the
  runtime puts it into the tab icon and `RUNTIME_APP_NAME` into the title and the installed app's name.
- The web page catches up with the agents page it was split from: tool calls in the feed expand to their result, stop
  mark and line diff; cards for plans, questions, Codex steps and runtime notices; the agent's task checklist above the
  feed; the reader's place held while new rows arrive, new rows marked, model switches shown; message history and quick
  prompts in the composer, send-now with Ctrl/Cmd+Enter, a slash command from the welcome screen; a message the runtime
  takes out of the queue stays as a pending row; one Annotate action with a comment per quoted fragment; board comments
  to a chat wait in its queue; the new-task machine outlives a reload; the document reader takes opaque version ids; a
  service worker, a manifest and an update check by the entry's hash.

- Design projects get a page model (`docs/design-pages.md`): `meta/project.json` and one `meta/pages/<ULID>.json` per
  page, with a permanent id. When `meta/` is there it is the truth; `project/canvas.json` (the design tool's v3 index)
  and `artifact.json` are built from it, and a project without it is read as before. `harnessly design page
  list|create|rename|remove` (`/api/cli/design/pages`), board claims keyed by the page id (a claim under the old key
  still holds), `harnessly design migrate [--dry-run]` for a whole library (same listing, no board or asset touched, the
  same ids on every run), and `harnessly hook design-guard`, a Claude Code hook that refuses hand edits of
  `project/canvas.json`. The library rules file is v6.
- A new design project no longer needs `<library>/.template/`: the runtime ships a board template and the board engine
  (`RUNTIME_DESIGN_ENGINE_FILE` overrides it), and every new project starts with `meta/`.
- UI kits by version for the design library. A project's boards no longer carry their own 6 MB copy of the kit:
  a kit is installed once as `<kits>/<id>/<version>/` and the library server answers `/kit/<id>/<version>/<file>` with
  immutable cache headers (the same path rules as project files: no `..`, no links out of the version's folder), so a
  board loads `/kit/acme-ds/0.16.0/bundle.r18.js` and a new kit version is a new address. `GET /__lib/kits` lists the
  installed kits and versions (`?kit=<id>&version=<v>` the files of one). `RUNTIME_DESIGN_KITS_DIR` names the folder
  (default `design-kits` in the data folder). `harnessly design kit install <id> <version> <tarball-or-url>` unpacks a
  tarball all or nothing (links, devices and paths outside the kit are refused; the same tarball again does nothing,
  other content under an installed version needs `--force`) and `harnessly design kit list` shows what is installed.
  Both work on the folder, the runtime does not have to be up. Kit files are readable across origins only from the
  library server's own loopback origins and the hosts in `RUNTIME_DESIGN_KIT_ORIGINS` (the origin is echoed back with
  `Vary: Origin`; any other origin gets no CORS header).

- Releases of the board format. With `RUNTIME_DESIGN_RELEASES_DIR` (a checkout of a design-upstream
  mirror of Claude Design releases), a project with `meta/` is served the engine of its release (`"release"` in
  `meta/project.json`, `legacy` when it names none) as `project/support.js`, checked against the mirror's sha256; the
  boards and the project's own copy are never changed, and the engine's version goes into the board's address so a
  browser drops the old one. A project without `meta/`, or on a release the mirror lacks, is served its own copy. A new
  project starts on the newest stable release (or the library's choice). `GET /design/library/releases[?artifact=]`
  lists the releases and the project's, `POST {artifact, release}` moves a project to a stable release (through the
  page model's `SetRelease`; a project without `meta/` is asked to migrate first), `POST {newProjects}` sets the
  release new projects start on; the library listing carries each project's `release` with the `upgrade` on offer.
  Without the variable nothing changes.

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

- Platform API calls follow the installation's prefix (`RUNTIME_PLATFORM_API`) instead of a fixed `/api/platform`.
- Inline code in messages no longer shows the typography plugin's backticks next to its chip.

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
