<p align="center"><img src="docs/logo.svg" width="160" alt="harnessly"></p>

# harnessly

The open core of a workspace where people and coding agents (Claude Code, Codex) work side by side: terminals,
conversations, documents and boards in one place, served from a cluster to the machines the agents run on.

This repository starts the core over: it carries none of the code it replaces, and parts move here one at a time. It
holds two programs, both single static Go binaries — the machine agent (`cmd/agent`) and the runtime (`cmd/runtime`)
— and a probe for the agent (`cmd/agentprobe`). The cluster side and the web pages live elsewhere.

## Machine agent

`cmd/agent` runs on every machine next to a person's runtime, one process per Unix account. It holds a single
WebSocket link to the cluster — the cluster connects to it on `/v1`, or it dials the cluster itself — and over that link
it serves:

- the transcripts Claude Code and Codex keep for the account (list, read by whole lines, change events that carry the
  appended lines);
- the runtime's state files (workspaces, layouts, settings), versioned writes from the cluster included;
- the runtime's status and sync sockets, relayed;
- tunnels: TCP connections to the runtime and the disk service on loopback, carried as binary frames, so nothing has
  to reach into the machine;
- loopback routes for the account's own CLIs and conversations (`/docs/*`, `/state/*`, `/automations/report`),
  guarded by a token in a 0600 file.

It is one static binary with no runtime dependencies.

```sh
CGO_ENABLED=0 go build -trimpath -o agent ./cmd/agent
AGENT_PORT=28052 AGENT_SECRET_FILE=/path/to/secret ./agent
```

### Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `AGENT_PORT` | — (required) | loopback port of the agent |
| `AGENT_HOST` | `127.0.0.1` | address to listen on |
| `AGENT_SECRET_FILE` | — | file with the pair's session secret; else `authSecret` from `AGENT_CONFIG` |
| `AGENT_DATA_DIR` | `~/.harnessly` | the runtime's data directory |
| `AGENT_CONFIG` | `$AGENT_DATA_DIR/config.json` | runtime config holding `authSecret` |
| `AGENT_CLUSTER_URL`, `AGENT_RUNTIME_ID` | — | with both set the agent dials the cluster itself |
| `AGENT_AUDIENCE`, `AGENT_DIAL_AUDIENCE` | `agent`, `ingest` | token audiences for each direction |
| `RUNTIME_PORT`, `FILES_PORT` | `0` | the runtime's API and the disk service on loopback; `0` — not here |
| `AGENT_RUNTIME_API` | `/api/agent` | prefix of the runtime routes the agent calls |
| `AGENT_RUNTIME_STATUS`, `AGENT_RUNTIME_SYNC` | `/api/status`, `/api/sync` | the runtime's sockets |
| `AGENT_RUNTIME_STATE_CHANGED` | `$AGENT_RUNTIME_API/state-changed` | where the runtime hears about a layout the cluster wrote |
| `AGENT_RUNTIME_COOKIE` | `session-token` | session cookie name the runtime checks |
| `AGENT_RUNTIME_CLI_HEADERS` | `x-cli-token` | headers carrying the runtime's `cli-token` |
| `AGENT_LOCAL_TOKEN_HEADER` | `x-agent-local-token` | header of the loopback routes' token |
| `AGENT_REPORT_PORT_ENV` | `AGENT_REPORT_PORT` | variable an automation conversation finds the agent's port in |
| `AGENT_EXTRA_STATE_FILES` | — | more root files of the data directory to keep in step, comma-separated |
| `AGENT_SCAN_MS` | `30000` | full transcript scan, the safety net under the file watcher |
| `AGENT_VERSION` | build version | version the agent reports |

## Runtime

`cmd/runtime` is the per-person runtime of a machine: the HTTP and WebSocket API the workspace's pages, the agent CLIs'
hooks and the machine agent call. It is being rewritten here one slice of routes at a time from an earlier Node.js
implementation, called the *previous runtime* throughout the code; that implementation is not part of this repository.
The process runs in one of two modes:

- **Gate** (`RUNTIME_GATE_UPSTREAM` set): it holds the runtime's port and answers what the slices switched on for the
  machine cover; everything else — HTTP, streams and WebSocket upgrades — goes to the previous runtime on an inner
  port, so callers keep one port. A previous runtime that is down looks down: the gate drops the connection instead of
  answering for it.
- **Standalone** (`RUNTIME_GATE_UPSTREAM` empty): there is no previous runtime and this process is the whole runtime
  (see [Standalone](#standalone)). This is the mode to try it in:

```sh
go build -o runtime ./cmd/runtime
HOME=$(mktemp -d) RUNTIME_GATE_LISTEN=127.0.0.1:8022 RUNTIME_TMUX_SOCKET=demo ./runtime &
curl -s 127.0.0.1:8022/api/health   # {"app":"harnessly","version":"0.0.0-development","commit":null}
```

Terminals and agent tabs need `tmux` on `PATH`; the agents themselves are the `claude` and `codex` CLIs.

Where the answers were the previous runtime's, they are kept byte for byte (status codes, JSON key order, headers,
ETags), because the callers parse them; the golden tests in `testdata` pin that down (see
[CONTRIBUTING.md](CONTRIBUTING.md)).

### Slices and the shadow

A slice is a named group of routes that moves to Go as a whole. Which slices are on is read from `state-mode.json` in
the data directory — `{"goSlices": ["fsread", "machines-shadow", …]}`, written by the machine agent — and re-read every
second, so a slice is switched on or off without a restart. A slice can first run as `<slice>-shadow`: the previous
runtime still answers, the Go answer is computed next to it (for read-only routes only) and every difference is logged
("shadow answer differs": JSON compared as data, files by status, headers and bytes), with the counts in the periodic
log ("shadow answers compared"). Every `RUNTIME_GATE_COUNT_SECONDS` the gate also logs each path still handed to the
previous runtime with its count and the average and longest time it took, the gate's own hop included.

| Slice | What Go answers |
| --- | --- |
| `core` | the runtime core: the tab state keeper, the layout and workspace stores, the status and sync sockets, prompt delivery, the agents' hooks, web push sending and the idle sleep of agents. Two keepers of one machine's tabs would fight over its files, so it runs only while the slice is on |
| `core-shadow` | (while `core` is off) a Go keeper next to the previous runtime's, reading the same layouts and tmux and fed copies of the same hooks, writing and typing nothing; it logs where its tab states and the previous runtime's status stream stay apart |
| `state` | the settings files (the cluster serves them from its database first; this runtime is their fallback and writer), the automation routes, the little routes nothing else took, and the previous runtime's dead routes, answered as it answered them on a machine without pages |
| `terminal` | the terminal socket (`/api/terminal?session=`): a pty running `tmux attach`, in the binary protocol the page speaks |
| `fsread` | read-only answers about the machine and its files (GET only): peers, a tab's working folder, the agents in a tab, the conversations of a folder, the tools on the machine, slash commands and skills, launch choices, the editor's listing and files, changed files and their diffs, the file viewer with downloads and video ranges, the «@»-context picker, memory notes and uploaded attachments |
| `fswrite` | disk writes without agent state: uploads into a conversation and their clean-up, an editor save, syncing a tab's repository with its upstream, stopping a heavy process from the load panel; never shadowed |
| `machines` | the machine snapshot the machine agent polls (load, memory, provider limits with a forecast, release), the machine load panel, `/api/health`, `/api/manifest` and the web push subscription routes |
| `launch` | starting and resuming agents inside the running core: conversations, automation runs, document comments, eval runs, side chats, the Codex launch lines, and the resume of working agents at startup and after a tmux reset |
| `transcripts` | answers computed from the agents' conversation files: links and work paths of a conversation, message counts, the usage report, the statistics pages, and conversation snapshots with their read-only links |
| `timeline` | the timeline socket, the status line sink, a conversation's economics and subagents, and the turn checkpoints (a git snapshot of a tab's folder when an agent's turn ends) |
| `design` | the design library: listing, its loopback server, board edits, imports and new projects, checks and pictures in a headless browser, claims, history, and dialogs with agents |
| `preview` | the reverse proxy behind the pane's browser, with its DevTools record, the agents' DevTools routes and MCP server, and the ports and component lookup of the pane |
| `services` | routes that call external services with the person's tokens: GitLab pushes, merge requests and diffs, link previews, tracker cards and avatars, site icons, the organizer file |
| `voice` | dictation over `/api/voice` (ffmpeg to the machine's speech recognition service) and the in-tab engine's model files |

Some routes of a slice need the core and answer only while it runs; a slice whose configuration is missing (say, the
snapshot routes without the `RUNTIME_SHARE_*` variables) leaves those routes to the previous runtime.

### Configuration

Everything that names a deployment — paths, header and cookie names, hosts, URL prefixes — comes from the environment;
the defaults are neutral. Unset values mostly mean "not here": the feature is off or stays with the previous runtime.

#### Gate and process

| Variable | Meaning |
| --- | --- |
| `RUNTIME_GATE_LISTEN` | the runtime's port, e.g. `127.0.0.1:8022` |
| `RUNTIME_GATE_UPSTREAM` | the previous runtime on its inner port; empty runs the runtime standalone (below) |
| `RUNTIME_GATE_RECORD` | a file that gets every request through the gate as a JSON line, credentials left out (unset: none) |
| `RUNTIME_GATE_COUNT_SECONDS` | how often the paths still handed to the previous runtime are logged (default 600) |
| `AGENT_DATA_DIR` | the data directory holding `state-mode.json` (default `~/.harnessly`) |
| `AGENT_VERSION` | version the gate reports |
| `AGENT_CONFIG`, `AGENT_SECRET_FILE` | where the pair's secret is, to check page session cookies |
| `RUNTIME_SESSION_COOKIE` | session cookie name (`session-token`) |
| `RUNTIME_CLI_TOKEN_HEADERS` | headers carrying the runtime's `cli-token` between the gate and the previous runtime |
| `RUNTIME_OLD_INTERNAL_API` | prefix of the previous runtime's internal routes the slices call |
| `RUNTIME_PLATFORM_API` | prefix of the routes the platform's own pages call (default `/api/platform`) |
| `RUNTIME_ACCESS_HOST` | the previous runtime's `HOST`: who may call the hook and agent routes (unset: the config's `networkAccess`; `HOST` itself is still read) |
| `HOST` | the hook route's network access rule, as the previous runtime read it (`localhost`, `tailscale`, `lan`, `all` or CIDRs, comma-separated); unset takes `networkAccess` of the runtime config (`localhost`, `tailscale` or `all`, default `all`) |
| `RUNTIME_AGENT_TOKEN_HEADER` | header of the machine agent's loopback routes, for committing state to the cluster |
| `RUNTIME_RELEASE_FILE` | the release file (`{"version", "commit"}`) the snapshot and `/api/health` report (unset: none; health then says `0.0.0-development`) |
| `RUNTIME_HEALTH_APP` | the app name `/api/health` reports (default `harnessly`) |
| `RUNTIME_MANIFEST_FILE` | the installable app's manifest served at `/api/manifest` (unset: the previous runtime's) |
| `RUNTIME_API_GUIDE_FILE` | the CLI's API guide served at `/api/cli/api-guide` (default `runtime-static/api-guide.md` beside `RUNTIME_RELEASE_FILE`; no file: the previous runtime's) |
| `RUNTIME_REACT_SCAN_FILE` | the render inspector script a developer's page may ask for at `/api/react-scan` (unset: none) |
| `RUNTIME_LAYOUT_KEY` | the runtime config's field holding the workspace's column widths (default `layout`) |
| `RUNTIME_OWNER_EMAIL` | the owner of automations when the runtime config names none (`automationOwnerEmail`) |

#### tmux, tabs and agents

| Variable | Meaning |
| --- | --- |
| `RUNTIME_TMUX_SOCKET`, `RUNTIME_TMUX_LEGACY_SOCKET` | the runtime's tmux servers (`-L`; default `harnessly`, no legacy one) |
| `RUNTIME_SESSION_PREFIX`, `RUNTIME_LEGACY_SESSION_PREFIX` | tab session name prefixes (default `hs-`); a legacy one may live on either server |
| `RUNTIME_TMUX_CONF` | the tmux.conf a new tmux server reads and is re-read after each new session (none: tmux defaults) |
| `RUNTIME_PANE_BIN_DIR` | folder put first on a new pane shell's `PATH` (pane-side wrappers) |
| `RUNTIME_AGENT_NICE` | niceness of a tmux server the runtime starts, 0–19, `0` = off (default 10) |
| `RUNTIME_TAB_META_KEY` | the layout tab field holding the platform's own metadata (launch choice, mode) |
| `RUNTIME_CLAUDE_LAUNCHER` | the managed Claude launcher (an absolute path) typed instead of `claude`; unset types `claude` |
| `RUNTIME_LAUNCH_DEFAULTS_KEY` | key of the runtime config (`AGENT_CONFIG`) holding the remembered model and effort per agent CLI |
| `RUNTIME_AGENT_SLEEP_IDLE_MIN`, `RUNTIME_AGENT_SLEEP_PRESSURE_MIN` | minutes an idle Claude may stay before it is put to sleep (default 360) and the shorter limit under memory pressure (default 20); `0` turns a rule off |
| `RUNTIME_AGENT_RESUME_RECENT_MIN` | at startup, resume conversations active this recently too (default 0, only those mid-turn); the startup resume is the core's with the `launch` slice on, the previous runtime's otherwise |
| `RUNTIME_START_ENV_PREFIX` | prefix of the variables a started conversation may be given (`<prefix>NAME='value'` before its launch line; default `HARNESS_`) |
| `RUNTIME_FIRST_WORKSPACE_NAME` | the workspace made when a conversation starts on a machine that has none (default `Workspace`) |
| `RUNTIME_EVAL_RUNNER`, `RUNTIME_NODE_BIN` | the eval runner script an eval run starts detached, and the Node that runs it (default `node`); no script, no eval runs (501) |
| `RUNTIME_SIDE_CHAT_CONFIG` | the machine's runtime config naming the Claude binary (`claude_binary`) and login folder (`auth_directory`) a side chat runs (unset: `claude`, the default login) |
| `RUNTIME_MCP_SERVER`, `RUNTIME_MCP_PATH`, `RUNTIME_MCP_TOKEN_ENV` | the runtime's MCP server as Codex is told about it: its name, its route (default `/api/cli/preview/mcp`, on the gate's port) and the variable the Codex launcher puts the CLI token in |
| `RUNTIME_SCRIPT_DATA_DIRS` | home-relative data folders the Codex launcher looks for, comma-separated; with it and `RUNTIME_MCP_TOKEN_ENV` the core writes a missing launcher itself |
| `RUNTIME_CODEXBAR_BIN` | the codexbar binary the core polls for Claude's usage limits (unset: none) |
| `CLAUDE_CONFIG_DIR`, `CODEX_HOME` | the agent CLIs' own folders, `~/.claude` and `~/.codex` by default |

#### Prompt delivery

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PROMPT_EDITOR` | the prompt editor script (full path): new Claudes get it as `VISUAL`/`EDITOR`, and a running Claude started with it takes long prompts through it (unset: no editor, long prompts go as a paste; the earlier `RUNTIME_PROMPT_EDITOR_NAME`, a name under `$AGENT_DATA_DIR/bin/`, is still read) |
| `RUNTIME_PROMPT_EDITOR_LEGACY` | older paths of that script whose `VISUAL` still counts, comma-separated |
| `RUNTIME_PROMPT_HANDOFF_DIR` | the folder the editor script takes its hand-off files from (default `$AGENT_DATA_DIR/prompt-handoff`) |
| `RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET` | the tmux server whose panes' hand-off files carry no server name (default `RUNTIME_TMUX_LEGACY_SOCKET`) |
| `RUNTIME_PROMPT_HUMAN_EDITOR_VAR` | the variable the editor script finds the person's own editor in, set in front of every Claude launch (the earlier `RUNTIME_HUMAN_EDITOR_VAR` is still read) |

#### Folders and files

| Variable | Meaning |
| --- | --- |
| `RUNTIME_WORKSPACE_DIR` | the workspace folder (home by default): a root the viewer and the «@»-context picker may open, and under which (or a workspace's folder) a tab may look up commands; when set, new conversations start there and an explicit launch folder must lie inside it (unset: a workspace's own folder, no explicit folders) |
| `RUNTIME_UPLOADS_DIR` | uploaded attachments (default `$AGENT_DATA_DIR/uploads`) |
| `RUNTIME_UPLOAD_HEADER_PREFIXES` | prefixes of an upload's headers (`<prefix>filename` and the like), the current one first, comma-separated (default `x-harnessly-`) |
| `RUNTIME_SESSION_MEMORY_DIRS` | folders whose `<entry>/memory` hold synced copies of memory notes, comma-separated (Claude Code's `projects` is always read) |
| `RUNTIME_SECRET_CONFIG_DIRS` | home-relative folders holding credentials (`.config/<app>`), refused by the viewer like `.ssh`, comma-separated |
| `RUNTIME_DATA_DIR_LABEL` | how messages name the data folder (default `~/` and its path under the home) |
| `RUNTIME_CHECKPOINT_NAMESPACE`, `RUNTIME_CHECKPOINT_TMP_TAG` | the turn checkpoints' git refs (`refs/<ns>/checkpoints/`, `refs/<ns>/saved/`) and author (`<ns>@localhost`), default `harnessly`; and the tag of the files a rewind stages (`<file>.<tag>-<hex>.tmp`, default `hs`) |

#### Links and external services

| Variable | Meaning |
| --- | --- |
| `RUNTIME_STAND_DOMAIN`, `RUNTIME_DOCS_HOST`, `RUNTIME_PLATFORM_HOST`, `RUNTIME_DOCS_READER_PATH` | what an agent may open in its right pane: feature stands, published documents and boards, the platform's reader |
| `RUNTIME_GITLAB_HOSTS`, `RUNTIME_TASK_KEY_RE`, `RUNTIME_TASK_EXAMPLES` | GitLab servers (comma-separated) and the tracker's task keys a pane tab may show |
| `RUNTIME_TRACKER_URL`, `RUNTIME_TRACKER_HOSTS`, `RUNTIME_TRACKER_KEYS` | the issue tracker's origin, the hosts its issue links use (comma-separated) and its project keys (comma-separated; unset: the alternatives of `RUNTIME_TASK_KEY_RE` when it is the plain `^(KEY\|KEY)-…` form), by which the conversation sources tell issues apart |
| `RUNTIME_ICON_USER_AGENT` | the User-Agent of site icon requests |
| `RUNTIME_ORGANIZER_FILE` | the person's organizer (projects, which project a conversation belongs to, pins), a JSON file (unset: those routes stay with the previous runtime) |
| `GITLAB_SELF_TOKEN`, `GITLAB_TOKEN`, `YOUTRACK_TOKEN` | the person's tokens the `services` slice calls with: the first of `RUNTIME_GITLAB_HOSTS`, any other GitLab, the tracker; when unset, read from a file (`gitlab-self-token`; `gitlab-token` or `glab-token`; `youtrack-token`) in one of the `RUNTIME_SECRET_CONFIG_DIRS` of the home |

#### Usage and statistics

| Variable | Meaning |
| --- | --- |
| `RUNTIME_USAGE_CACHE_FILE`, `RUNTIME_USAGE_TIMEZONE`, `RUNTIME_USAGE_OWNER` | the usage report's facts file under `$AGENT_DATA_DIR/stats` (default `usage-v2.json`; the previous runtime's name to share its file), the zone name the report gives its days (default `UTC`; the days themselves follow `RUNTIME_USAGE_TZ_OFFSET_MINUTES`) and the person a session without an e-mail `sub` stands for (default `user@host`) |
| `RUNTIME_USAGE_TZ_OFFSET_MINUTES` | the zone (minutes east of UTC) whose weekend the limits forecast learns the person's usual hours by (default 0) |
| `RUNTIME_PROJECTS_ROOT` | the folder name under which the usage report names projects by their repository (unset: by folder) |
| `RUNTIME_STATS_CWD_REWRITES` | more home-relative project folder prefixes the statistics pages shorten, `prefix=to` comma-separated |

#### Conversation snapshots

| Variable | Meaning |
| --- | --- |
| `RUNTIME_SHARE_BASE_HEADER`, `RUNTIME_SHARE_TOKEN_HEADER`, `RUNTIME_SHARE_TOKEN_SALT`, `RUNTIME_SHARE_TOKEN_AUDIENCE`, `RUNTIME_SHARE_FILE_PREFIX` | snapshot links: the header the platform gives the links' address in, the header of the share gateway's read token, the token key's HKDF salt (keyed by the pair's secret) and audience, and the start of a downloaded file's name; with `RUNTIME_APP_NAME` (the page's brand) all set, the snapshot routes are the slice's, else the previous runtime's |
| `RUNTIME_SHARE_DROP_HOST`, `RUNTIME_PROFILE_HOMES` | links to the platform itself (this host suffix under `RUNTIME_APP_BASE_PATH`) a snapshot page drops, and the folder of per-person profiles whose homes a snapshot shortens to `~` |

#### Machine and web push

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PUSH_SUBJECT`, `RUNTIME_PUSH_TAG_PREFIX` | the web push VAPID subject (a reachable `https:` or `mailto:` URL) and the start of a notification's tag (`<prefix><tab id>`); the core sends no push without both |
| `RUNTIME_APP_NAME` | the app's name in the notifications the `machines` slice sends (a test push, a new release); without it, the push subject and the tag prefix those two routes stay with the previous runtime |
| `RUNTIME_MACHINE_NAME_PREFIX` | cut from the start of the host name to name the machine in its snapshot |
| `RUNTIME_RELEASE_ROOT` | where the platform is installed: the machine load counts anything run from there as the platform's |
| `RUNTIME_RESERVED_PORTS` | the platform's own ports, comma-separated, never listed in the machine load (the gate's and the previous runtime's are added) |

#### Design library

| Variable | Meaning |
| --- | --- |
| `RUNTIME_DESIGN_LIBRARY_DIR` | the library folder; unset: `design-artifacts` under `RUNTIME_WORKSPACE_DIR`, else `RUNTIME_WORKSPACE`, else the workspace root of `RUNTIME_WORKSPACE_CONFIG` (default `RUNTIME_SIDE_CHAT_CONFIG`), else the data directory |
| `RUNTIME_WORKSPACE`, `RUNTIME_WORKSPACE_CONFIG` | the fallbacks above for finding the library |
| `RUNTIME_DESIGN_HISTORY_DIR` | where the boards' private history repositories live (default the data directory) |
| `RUNTIME_DESIGN_LIBRARY_PORT` | the library server's preferred loopback port |
| `RUNTIME_DESIGN_CHROME` | the headless browser a check drives (unset: the usual places) |
| `RUNTIME_CLI_NAME` | the command agents are told to run (`<cli> design check …`, the DevTools hints; default `harnessly`) |
| `RUNTIME_DESIGN_TAG`, `RUNTIME_DESIGN_HISTORY_EMAIL` | what this runtime writes into shared places (the history's author and markers, a check's temporary profile; default `harnessly-design`) and the history's author address (default `design@localhost`) |
| `RUNTIME_DESIGN_SHARED_DIR` | the team's shared library on the machines, named in the library's rules file (default `/srv/design`) |
| `RUNTIME_DESIGN_KIT_GLOBAL`, `RUNTIME_DESIGN_PAGES_GLOBAL`, `RUNTIME_DESIGN_ARGS_GLOBAL` | the window globals of a project's UI kit, its page renderer and the renderer's arguments (default `DesignKit`, `DesignPages`, `__DESIGN_ARGS`) |
| `RUNTIME_PEOPLE` | names a claim shows, by the local part of the owner's address: `local=Name,local2=Name2` |

#### Preview proxy

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PREVIEW_PROXY_PATH` | the proxy's own path (default `/agents$RUNTIME_PLATFORM_API/preview/proxy`) |
| `RUNTIME_PREVIEW_CLAIM_HEADER`, `RUNTIME_PREVIEW_CLAIM_AUDIENCE` | the header carrying the gateway's claim, signed with the pair's secret, and its audience (default `x-preview-claim`, `preview-proxy`) |
| `RUNTIME_PREVIEW_PRIVATE_HEADER_PREFIX` | headers only the gateway sets, never passed upstream (default `x-preview-`) |
| `RUNTIME_PREVIEW_SIGN_IN_HOSTS`, `RUNTIME_PREVIEW_STAND_SESSION_HEADER` | where a feature stand (`RUNTIME_STAND_DOMAIN`) sends a person without a session, comma-separated, and the header carrying the person's stand session |
| `RUNTIME_PREVIEW_SELF_IPS` | the machine's public addresses no interface carries (behind NAT), comma-separated |
| `RUNTIME_PREVIEW_BRIDGE_FILE` | the page bridge script injected into previewed pages (unset: those requests go to the previous runtime) |
| `RUNTIME_WORKTREES_ROOT` | the git worktrees folder whose dev servers the pane lists (default `~/.worktrees`) |

#### Voice

| Variable | Meaning |
| --- | --- |
| `RUNTIME_VOICE_SOCKET` | the machine's speech recognition service, a Unix socket (default `/run/harnessly-voice/asr.sock`) |
| `RUNTIME_VOICE_FFMPEG_BIN` | ffmpeg's absolute path (unset: the usual places) |

The `fsread` preflight builds the login shell's environment from the gate's own (`HOME`, `USER`, `SHELL`, `PATH`,
`LANG`, `LC_*` and the like) and Codex keeps sessions in local-day folders, so the gate should start with the
environment and time zone the person's shell has.

### Standalone

With `RUNTIME_GATE_UPSTREAM` empty there is no previous runtime: every slice is on, whatever `state-mode.json` says (no
shadow runs), and a request no route takes gets the previous runtime's answer to a route it did not have (404
`{"error":"Not found"}`), or 503 `{"error":"Service unavailable"}` for one of its routes not answered here (or not yet:
the core still starting), as a machine that is down; `/api/health` reports this process. What the previous runtime did
on start and stop is then the gate's, byte for byte, one writer per file: it takes the single-instance lock (and gives up
while a runtime answering on the lock's port holds it), makes the config and the pair's secret when missing, keeps or
makes the `cli-token`, writes the `port` file, the hook, status line and Codex hook scripts, the `--settings` and MCP
files, the prompt editor and the Codex launcher, re-reads the tmux config, writes each workspace's agent prompts, and
cleans up old uploads and the status line figures of gone conversations; at stop the port file and the lock go. What
the core told the previous runtime (the workspace prompts, a corrected tab state) it then does itself, and it carries a
change of the person's Claude plugins into idle Claude tabs.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_LOCK_FILE` | the single-instance lock, a path or a name in the data directory (default `runtime.lock`) |
| `RUNTIME_MCP_ALLOW` | the permissions the hook settings grant, comma-separated (default `mcp__$RUNTIME_MCP_SERVER`) |
| `RUNTIME_PROMPT_EDITOR_TITLE` | the name the prompt editor script's comment gives it |
| `RUNTIME_WORKSPACE_PROMPTS_FILE` | the agents' per-workspace prompts, `{"claude": …, "codex": …}` with `{{workspaceId}}` (default the release's `runtime-static/workspace-prompts.json`; none: no prompts) |
| `RUNTIME_CODEX_LAUNCHER_FILE` | the Codex launcher script as the platform ships it (default the release's `runtime-static/codex-launcher.js`; none: a neutral one) |
| `RUNTIME_TELEMETRY_URL`, `RUNTIME_TELEMETRY_KEY` | a PostHog-compatible batch endpoint and project key a released runtime reports corrected tab states to (unset: logged only) |
| `RUNTIME_TELEMETRY_PROPERTIES`, `RUNTIME_TELEMETRY_ANONYMOUS_ID`, `RUNTIME_TELEMETRY_MISMATCH_EVENT` | the properties every event starts with (a JSON object), who the events are about without an owner e-mail (default `runtime`), the event's name (default `platform_status_mismatch`) |
| `RUNTIME_PLUGIN_RELOAD_MARK` | the plugin reconciler's mark file (`{"plugins": ms, "skills": ms}`); unset: no reloads |

### Checking an agent

`cmd/agentprobe` talks to an agent the way the cluster does: `-mode compat` prints every answer of a fixed script so
two agents can be diffed, `-mode bench` measures listing, reading and event latency, and `-mode runtime` / `-mode
ingest` stand in for the runtime and the cluster.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md); security issues: [SECURITY.md](SECURITY.md).

## License

Apache License 2.0, see [LICENSE](LICENSE).
