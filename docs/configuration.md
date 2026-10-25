# Configuration

Both programs are configured only through the environment. Everything that names a deployment (paths, header and
cookie names, hosts, URL prefixes) is a variable with a neutral default, so the code never knows which deployment it
runs in. Unset values mostly mean "not here": the feature is off, or in [gate mode](architecture.md#gate-and-standalone)
it stays with the previous runtime.

- [Machine agent](#machine-agent)
- [Runtime](#runtime): [gate and process](#gate-and-process), [tmux, tabs and agents](#tmux-tabs-and-agents),
  [prompt delivery](#prompt-delivery), [folders and files](#folders-and-files),
  [links and external services](#links-and-external-services), [usage and statistics](#usage-and-statistics),
  [conversation snapshots](#conversation-snapshots), [machine and web push](#machine-and-web-push),
  [design library](#design-library), [preview proxy](#preview-proxy), [voice](#voice)
- [Standalone runtime](#standalone-runtime)

## Machine agent

`cmd/agent`, one process per Unix account.

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

`cmd/runtime`, one per person on a machine. The variables are grouped by what they touch.

### Gate and process

| Variable | Meaning |
| --- | --- |
| `RUNTIME_GATE_LISTEN` | the runtime's port, e.g. `127.0.0.1:8022` |
| `RUNTIME_GATE_UPSTREAM` | the previous runtime on its inner port; empty runs the runtime [standalone](#standalone-runtime) |
| `RUNTIME_GATE_COUNT_SECONDS` | how often the paths still handed to the previous runtime are logged (default 600) |
| `AGENT_DATA_DIR` | the data directory holding `state-mode.json` (default `~/.harnessly`) |
| `AGENT_VERSION` | version the gate reports |
| `AGENT_CONFIG`, `AGENT_SECRET_FILE` | where the pair's secret is, to check page session cookies |
| `RUNTIME_SESSION_COOKIE` | session cookie name (`session-token`) |
| `RUNTIME_CLI_TOKEN_HEADERS` | headers carrying the runtime's `cli-token` between the gate and the previous runtime |
| `RUNTIME_PLATFORM_API` | prefix of the routes the platform's own pages call (default `/api/platform`) |
| `RUNTIME_ACCESS_HOST` | the previous runtime's `HOST`: who may call the hook and agent routes (unset: the config's `networkAccess`; `HOST` itself is still read) |
| `INIT_PASSWORD` | an initial password (4 characters or more) that guards the first-run setup: while no password is set, the page asks for it before it lets anyone choose one, and sign-in with it works until the real one is chosen. Unset, whoever opens the page first chooses the password ([deploy](deploy.md#the-first-visit)) |
| `HOST` | the hook route's network access rule, as the previous runtime read it (`localhost`, `tailscale`, `lan`, `all` or CIDRs, comma-separated); unset takes `networkAccess` of the runtime config (`localhost`, `tailscale` or `all`, default `all`) |
| `RUNTIME_AGENT_TOKEN_HEADER` | header of the machine agent's loopback routes, for committing state to the cluster |
| `RUNTIME_RELEASE_FILE` | the release file (`{"version", "commit"}`) the snapshot and `/api/health` report (unset: none; health then says `0.0.0-development`) |
| `RUNTIME_LOCALE` | the language of every text the runtime shows people (error bodies, notifications, the shared snapshot page, the command catalog, the design library pages and the agent prompts it writes): `en` (default) or `ru`, the previous runtime's texts byte for byte |
| `RUNTIME_HEALTH_APP` | the app name `/api/health` reports (default `harnessly`) |
| `RUNTIME_MANIFEST_FILE` | the installable app's manifest served at `/api/manifest` (unset: the previous runtime's) |
| `RUNTIME_API_GUIDE_FILE` | the CLI's API guide served at `/api/cli/api-guide` (default `runtime-static/api-guide.md` beside `RUNTIME_RELEASE_FILE`; no file: the previous runtime's) |
| `RUNTIME_REACT_SCAN_FILE` | the render inspector script a developer's page may ask for at `/api/react-scan` (unset: none) |
| `RUNTIME_LAYOUT_KEY` | the runtime config's field holding the workspace's column widths (default `layout`) |
| `RUNTIME_OWNER_EMAIL` | the owner of automations when the runtime config names none (`automationOwnerEmail`) |

### tmux, tabs and agents

| Variable | Meaning |
| --- | --- |
| `RUNTIME_TMUX_SOCKET`, `RUNTIME_TMUX_LEGACY_SOCKET` | the runtime's tmux servers (`-L`; default `harnessly`, no legacy one) |
| `RUNTIME_SESSION_PREFIX`, `RUNTIME_LEGACY_SESSION_PREFIX` | tab session name prefixes (default `hs-`); a legacy one may live on either server |
| `RUNTIME_TMUX_CONF` | the tmux.conf a new tmux server reads and is re-read after each new session (set but empty: tmux defaults; unset: none in front of the previous runtime, and standalone `tmux.conf` in the data directory, written with the status bar off when missing) |
| `RUNTIME_PANE_BIN_DIR` | folder put first on a new pane shell's `PATH` (pane-side wrappers; default `RUNTIME_AGENT_BIN_DIR` while the [agent skills](integrations.md#agent-skills) are on). Debian-style login shells rebuild `PATH` from `/etc/profile`, which keeps `~/.local/bin` when it exists but drops this folder |
| `RUNTIME_AGENT_NICE` | niceness of a tmux server the runtime starts, 0–19, `0` = off (default 10) |
| `RUNTIME_FOLDER_TRUST` | `ask` leaves Claude's and Codex's "trust this folder?" dialog to the person in the terminal; unset, the runtime answers yes for the folder the conversation was started in (the dialog is not shown on the page, and the agent would wait at it unseen) |
| `RUNTIME_TAB_META_KEY` | the layout tab field holding the platform's own metadata (launch choice, mode) |
| `RUNTIME_CLAUDE_LAUNCHER` | the managed Claude launcher (an absolute path) typed instead of `claude`; unset types `claude` |
| `RUNTIME_LAUNCH_DEFAULTS_KEY` | key of the runtime config (`AGENT_CONFIG`) holding the remembered model and effort per agent CLI |
| `RUNTIME_AGENT_SLEEP_IDLE_MIN`, `RUNTIME_AGENT_SLEEP_PRESSURE_MIN` | minutes an idle Claude may stay before it is put to sleep (default 360) and the shorter limit under memory pressure (default 20); `0` turns a rule off |
| `RUNTIME_AGENT_RESUME_RECENT_MIN` | at startup, resume conversations active this recently too (default 0, only those mid-turn); the startup resume is the core's with the `launch` slice on, the previous runtime's otherwise |
| `RUNTIME_START_ENV_PREFIX` | prefix of the variables a started conversation may be given (`<prefix>NAME='value'` before its launch line; default `HARNESS_`) |
| `RUNTIME_FIRST_WORKSPACE_NAME` | the workspace made when a conversation starts on a machine that has none (default `Workspace`) |
| `RUNTIME_EVAL_RUNNER`, `RUNTIME_NODE_BIN` | the eval runner script an eval run starts detached, and the Node that runs it (default `node`); no script, no eval runs (501) |
| `RUNTIME_SIDE_CHAT_CONFIG` | the machine's runtime config naming the Claude binary (`claude_binary`) and login folder (`auth_directory`) a side chat runs (unset: `claude`, the default login) |
| `RUNTIME_MCP_SERVER`, `RUNTIME_MCP_PATH`, `RUNTIME_MCP_TOKEN_ENV` | the runtime's MCP server as Codex is told about it: its name, its route (default `/api/cli/preview/mcp`, on the gate's port) and the variable the Codex launcher puts the CLI token in (the server is called `harnessly` and the variable `HARNESSLY_CLI_TOKEN` unless set) |
| `RUNTIME_SCRIPT_DATA_DIRS` | data folders the Codex launcher looks for the runtime in, home-relative or absolute, comma-separated; default the runtime's own data folder (`.harnessly`). A standalone runtime writes the launcher itself with the defaults, so Codex needs neither this nor `RUNTIME_MCP_TOKEN_ENV`; in front of a previous runtime, with both set, the core writes a missing launcher |
| `RUNTIME_CODEX_SANDBOX` | `container`: start Codex with `sandbox_mode="danger-full-access"` and `approval_policy="never"` because the container is the sandbox (Codex's own cannot start without user namespaces); `codex` (default; `container` in the Docker image): leave them to Codex ([integrations](integrations.md#agent-skills)) |
| `RUNTIME_CODEXBAR_BIN` | the codexbar binary the core polls for Claude's usage limits (unset: none) |
| `CLAUDE_CONFIG_DIR`, `CODEX_HOME` | the agent CLIs' own folders, `~/.claude` and `~/.codex` by default |

### Prompt delivery

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PROMPT_EDITOR` | the prompt editor script (full path): new Claudes get it as `VISUAL`/`EDITOR`, and a running Claude started with it takes long prompts through it (unset: no editor, long prompts go as a paste; the earlier `RUNTIME_PROMPT_EDITOR_NAME`, a name under `$AGENT_DATA_DIR/bin/`, is still read) |
| `RUNTIME_PROMPT_EDITOR_LEGACY` | older paths of that script whose `VISUAL` still counts, comma-separated |
| `RUNTIME_PROMPT_HANDOFF_DIR` | the folder the editor script takes its hand-off files from (default `$AGENT_DATA_DIR/prompt-handoff`) |
| `RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET` | the tmux server whose panes' hand-off files carry no server name (default `RUNTIME_TMUX_LEGACY_SOCKET`) |
| `RUNTIME_PROMPT_HUMAN_EDITOR_VAR` | the variable the editor script finds the person's own editor in, set in front of every Claude launch (the earlier `RUNTIME_HUMAN_EDITOR_VAR` is still read) |

### Folders and files

| Variable | Meaning |
| --- | --- |
| `RUNTIME_WORKSPACE_DIR` | the workspace folder (home by default): a root the viewer and the «@»-context picker may open, and under which (or a workspace's folder) a tab may look up commands; when set, new conversations start there and an explicit launch folder must lie inside it (unset: a workspace's own folder, no explicit folders) |
| `RUNTIME_UPLOADS_DIR` | uploaded attachments (default `$AGENT_DATA_DIR/uploads`) |
| `RUNTIME_UPLOAD_HEADER_PREFIXES` | prefixes of an upload's headers (`<prefix>filename` and the like), the current one first, comma-separated (default `x-harnessly-`) |
| `RUNTIME_SESSION_MEMORY_DIRS` | folders whose `<entry>/memory` hold synced copies of memory notes, comma-separated (Claude Code's `projects` is always read) |
| `RUNTIME_SECRET_CONFIG_DIRS` | home-relative folders holding credentials (`.config/<app>`), refused by the viewer like `.ssh`, comma-separated |
| `RUNTIME_DATA_DIR_LABEL` | how messages name the data folder (default `~/` and its path under the home) |
| `RUNTIME_CHECKPOINT_NAMESPACE`, `RUNTIME_CHECKPOINT_TMP_TAG` | the turn checkpoints' git refs (`refs/<ns>/checkpoints/`, `refs/<ns>/saved/`) and author (`<ns>@localhost`), default `harnessly`; and the tag of the files a rewind stages (`<file>.<tag>-<hex>.tmp`, default `hs`) |

### Links and external services

| Variable | Meaning |
| --- | --- |
| `RUNTIME_STAND_DOMAIN`, `RUNTIME_DOCS_HOST`, `RUNTIME_PLATFORM_HOST`, `RUNTIME_DOCS_READER_PATH` | what an agent may open in its right pane: feature stands, published documents and boards, the platform's reader ; a turn's cards for the stands and published pages it made are told by the same hosts |
| `RUNTIME_BOARDS_CLI` | the boards CLI an agent reads a board with (`<cli> read <link>`), named on a board a message points at (unset: only the link) |
| `RUNTIME_GITLAB_HOSTS`, `RUNTIME_TASK_KEY_RE`, `RUNTIME_TASK_EXAMPLES` | GitLab servers (comma-separated) and the tracker's task keys a pane tab may show; a `!12` in a chat opens a merge request only of these servers, which are asked with the tokens below whether it exists |
| `RUNTIME_GITLAB_API_ORIGIN` | for tests: the origin the GitLab API is read from instead of `https://<host>` |
| `RUNTIME_TRACKER_URL`, `RUNTIME_TRACKER_HOSTS`, `RUNTIME_TRACKER_KEYS` | the issue tracker's origin, the hosts its issue links use (comma-separated) and its project keys (comma-separated; unset: the alternatives of `RUNTIME_TASK_KEY_RE` when it is the plain `^(KEY\|KEY)-…` form), by which the conversation sources tell issues apart |
| `RUNTIME_ICON_USER_AGENT` | the User-Agent of site icon requests |
| `RUNTIME_ORGANIZER_FILE` | the person's organizer (projects, which project a conversation belongs to, pins), a JSON file (unset: those routes stay with the previous runtime) |
| `GITLAB_SELF_TOKEN`, `GITLAB_TOKEN`, `YOUTRACK_TOKEN` | the person's tokens the `services` slice calls with: the first of `RUNTIME_GITLAB_HOSTS`, any other GitLab, the tracker; when unset, read from a file (`gitlab-self-token`; `gitlab-token` or `glab-token`; `youtrack-token`) in one of the `RUNTIME_SECRET_CONFIG_DIRS` of the home |

### Usage and statistics

| Variable | Meaning |
| --- | --- |
| `RUNTIME_USAGE_CACHE_FILE`, `RUNTIME_USAGE_TIMEZONE`, `RUNTIME_USAGE_OWNER` | the usage report's facts file under `$AGENT_DATA_DIR/stats` (default `usage-v2.json`; the previous runtime's name to share its file), the zone name the report gives its days (default `UTC`; the days themselves follow `RUNTIME_USAGE_TZ_OFFSET_MINUTES`) and the person a session without an e-mail `sub` stands for (default `user@host`) |
| `RUNTIME_USAGE_TZ_OFFSET_MINUTES` | the zone (minutes east of UTC) whose weekend the limits forecast learns the person's usual hours by (default 0) |
| `RUNTIME_PROJECTS_ROOT` | the folder name under which the usage report names projects by their repository (unset: by folder) |
| `RUNTIME_STATS_CWD_REWRITES` | more home-relative project folder prefixes the statistics pages shorten, `prefix=to` comma-separated |

### Conversation snapshots

| Variable | Meaning |
| --- | --- |
| `RUNTIME_SHARE_BASE_HEADER`, `RUNTIME_SHARE_TOKEN_HEADER`, `RUNTIME_SHARE_TOKEN_SALT`, `RUNTIME_SHARE_TOKEN_AUDIENCE`, `RUNTIME_SHARE_FILE_PREFIX` | snapshot links: the header the platform gives the links' address in, the header of the share gateway's read token, the token key's HKDF salt (keyed by the pair's secret) and audience, and the start of a downloaded file's name; with `RUNTIME_APP_NAME` (the page's brand) all set, the snapshot routes are the slice's, else the previous runtime's |
| `RUNTIME_SHARE_DROP_HOST`, `RUNTIME_PROFILE_HOMES` | links to the platform itself (this host suffix under `RUNTIME_APP_BASE_PATH`) a snapshot page drops, and the folder of per-person profiles whose homes a snapshot shortens to `~` |

### Machine and web push

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PUSH_SUBJECT`, `RUNTIME_PUSH_TAG_PREFIX` | the web push VAPID subject (a reachable `https:` or `mailto:` URL) and the start of a notification's tag (`<prefix><tab id>`); the core sends no push without both |
| `RUNTIME_APP_NAME` | the app's name in the notifications the `machines` slice sends (a test push, a new release); without it, the push subject and the tag prefix those two routes stay with the previous runtime |
| `RUNTIME_MACHINE_NAME_PREFIX` | cut from the start of the host name to name the machine in its snapshot |
| `RUNTIME_RELEASE_ROOT` | where the platform is installed: the machine load counts anything run from there as the platform's |
| `RUNTIME_RESERVED_PORTS` | the platform's own ports, comma-separated, never listed in the machine load (the gate's and the previous runtime's are added) |

### Design library

| Variable | Meaning |
| --- | --- |
| `RUNTIME_DESIGN_LIBRARY_DIR` | the library folder; unset: `design-artifacts` under `RUNTIME_WORKSPACE_DIR`, else `RUNTIME_WORKSPACE`, else the workspace root of `RUNTIME_WORKSPACE_CONFIG` (default `RUNTIME_SIDE_CHAT_CONFIG`), else the data directory |
| `RUNTIME_WORKSPACE`, `RUNTIME_WORKSPACE_CONFIG` | the fallbacks above for finding the library |
| `RUNTIME_DESIGN_HISTORY_DIR` | where the boards' private history repositories live (default the data directory) |
| `RUNTIME_DESIGN_KITS_DIR` | where UI kits are installed by version (`<dir>/<id>/<version>/`), served by the library server at `/kit/<id>/<version>/<file>`; default `design-kits` in the data folder. Install with `harnessly design kit install` |
| `RUNTIME_DESIGN_KIT_ORIGINS` | comma-separated origins (`https://host[:port]`) of pages that show boards: they may read kit files across origins, frame the library server's pages (its `frame-ancestors` names them besides itself and loopback) and talk to the board script; the library server's own loopback origins always may, any other origin gets no CORS header. Boards shown through a preview slot get the slot's own `frame-ancestors`. Default empty |
| `RUNTIME_DESIGN_RELEASES_DIR` | a checkout of a design-upstream mirror: its `releases.json` lists the releases of the board format (Claude Design) with their engines and status; a project with `meta/` gets its release's engine (`"release"` in `meta/project.json`, default `legacy`) as `support.js`, new projects start on the newest stable one, and the panel offers newer stable releases. Unset: every project is served its own `support.js` as before |
| `RUNTIME_DESIGN_LIBRARY_PORT` | the library server's preferred loopback port |
| `RUNTIME_DESIGN_CHROME` | the headless browser a check drives (unset: the usual places) |
| `RUNTIME_CLI_NAME` | the command agents are told to run (`<cli> design check …`, the DevTools hints; default `harnessly`) |
| `RUNTIME_DESIGN_TAG`, `RUNTIME_DESIGN_HISTORY_EMAIL` | what this runtime writes into shared places (the history's author and markers, a check's temporary profile; default `harnessly-design`) and the history's author address (default `design@localhost`) |
| `RUNTIME_DESIGN_SHARED_DIR` | the team's shared library on the machines, named in the library's rules file (default `/srv/design`) |
| `RUNTIME_DESIGN_KIT_GLOBAL`, `RUNTIME_DESIGN_PAGES_GLOBAL`, `RUNTIME_DESIGN_ARGS_GLOBAL` | the window globals of a project's UI kit, its page renderer and the renderer's arguments (default `DesignKit`, `DesignPages`, `__DESIGN_ARGS`) |
| `RUNTIME_DESIGN_CANVAS_KEY` | the key of a project's `project/canvas.json` under which the runtime keeps its page data (ids, routes); default `harnessly` (see [design pages](design-pages.md)) |
| `RUNTIME_DESIGN_ENGINE_FILE` | the board engine (`support.js`) a new project gets when the library has no `.template/`; unset: the copy shipped with the runtime |
| `RUNTIME_PEOPLE` | names a claim shows, by the local part of the owner's address: `local=Name,local2=Name2` |

### Preview proxy

| Variable | Meaning |
| --- | --- |
| `RUNTIME_PREVIEW_PROXY_PATH` | the proxy's own path (default `/agents$RUNTIME_PLATFORM_API/preview/proxy`) |
| `RUNTIME_PREVIEW_CLAIM_HEADER`, `RUNTIME_PREVIEW_CLAIM_AUDIENCE` | the header carrying the gateway's claim, signed with the pair's secret, and its audience (default `x-preview-claim`, `preview-proxy`) |
| `RUNTIME_PREVIEW_PRIVATE_HEADER_PREFIX` | headers only the gateway sets, never passed upstream (default `x-preview-`) |
| `RUNTIME_PREVIEW_SIGN_IN_HOSTS`, `RUNTIME_PREVIEW_STAND_SESSION_HEADER` | where a feature stand (`RUNTIME_STAND_DOMAIN`) sends a person without a session, comma-separated, and the header carrying the person's stand session |
| `RUNTIME_PREVIEW_SELF_IPS` | the machine's public addresses no interface carries (behind NAT), comma-separated |
| `RUNTIME_PREVIEW_BRIDGE_FILE` | the page bridge script injected into previewed pages (unset: a standalone runtime takes `preview-bridge.js` of its web build, `RUNTIME_WEB_DIR` or the embedded one; a gate sends those requests to the previous runtime) |
| `RUNTIME_PREVIEW_LISTEN` | standalone only: `host:port` of the first of the 8 preview slot listeners, the other 7 take the next ports (default: the gate's host and the 8 ports after its port, so `8023`–`8030` next to `8022`); `off` turns the preview off. See [the preview without a gateway](deploy.md#the-preview-without-a-gateway) |
| `RUNTIME_PREVIEW_PUBLIC_ORIGINS` | standalone only: the 8 origins a reverse proxy serves the slots at, comma-separated, in slot order (`https://p1.example.com,...`); needed when the page is opened over https |
| `RUNTIME_WORKTREES_ROOT` | the git worktrees folder whose dev servers the pane lists (default `~/.worktrees`) |

### Voice

| Variable | Meaning |
| --- | --- |
| `RUNTIME_VOICE_SOCKET` | the machine's speech recognition service, a Unix socket (default `/run/harnessly-voice/asr.sock`) |
| `RUNTIME_VOICE_FFMPEG_BIN` | ffmpeg's absolute path (unset: the usual places) |

### Environment of the process

The `fsread` preflight builds the login shell's environment from the gate's own (`HOME`, `USER`, `SHELL`, `PATH`,
`LANG`, `LC_*` and the like) and Codex keeps sessions in local-day folders, so the gate should start with the
environment and time zone the person's shell has.

## Standalone runtime

These apply when `RUNTIME_GATE_UPSTREAM` is empty and the runtime does the start-up work itself (see
[architecture](architecture.md#gate-and-standalone)).

| Variable | Meaning |
| --- | --- |
| `RUNTIME_LOCK_FILE` | the single-instance lock, a path or a name in the data directory (default `runtime.lock`) |
| `RUNTIME_MCP_ALLOW` | the permissions the hook settings grant, comma-separated (default `mcp__$RUNTIME_MCP_SERVER`) |
| `RUNTIME_PROMPT_EDITOR_TITLE` | the name the prompt editor script's comment gives it |
| `RUNTIME_WORKSPACE_PROMPTS_FILE` | the agents' per-workspace prompts, `{"claude": …, "codex": …}` with `{{workspaceId}}` (default the release's `runtime-static/workspace-prompts.json`; none: the built-in prompts, which tell both agents about the `harnessly` command and, when configured, the `artifacts` and `boards` commands) |
| `RUNTIME_CODEX_LAUNCHER_FILE` | the Codex launcher script as the platform ships it (default the release's `runtime-static/codex-launcher.js`; none: a neutral one) |
| `RUNTIME_TELEMETRY_URL`, `RUNTIME_TELEMETRY_KEY` | a PostHog-compatible batch endpoint and project key a released runtime reports corrected tab states to (unset: logged only) |
| `RUNTIME_TELEMETRY_PROPERTIES`, `RUNTIME_TELEMETRY_ANONYMOUS_ID`, `RUNTIME_TELEMETRY_MISMATCH_EVENT` | the properties every event starts with (a JSON object), who the events are about without an owner e-mail (default `runtime`), the event's name (default `platform_status_mismatch`) |
| `RUNTIME_PLUGIN_RELOAD_MARK` | the plugin reconciler's mark file (`{"plugins": ms, "skills": ms}`); unset: no reloads |

### Web page

The browser page in `web/` (see the [README](../README.md#the-web-page)). A standalone runtime serves it at
`RUNTIME_APP_BASE_PATH` (default `/agents`; `/` redirects there) and fills that base and the API prefixes into the page.
It answers its API under that base too (`<base>/api/…` besides `/api/…`), so one proxy prefix covers both. In front of
the previous runtime the pages are someone else's and the gate leaves those paths alone.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_WEB` | `on` serves the page in front of the previous runtime too, `off` never; unset: standalone only |
| `RUNTIME_WEB_DIR` | a built page (`web/dist`) to serve instead of the one compiled in (`-tags webui`); without either there is no page; its files go out gzipped to browsers that accept it |
Every page variable below is read the same way by `runtime web export <dir>`: it writes the page this runtime would serve
into `<dir>` (its files and `index.html` with the base and the settings filled in), so a proxy in front of the runtimes
can serve the same build while the machine behind it is down.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_APP_NAME` | the product name the page shows in its titles and brand (default `harnessly`) |
| `RUNTIME_DESIGN_NAME` | the design tool's name in the page's texts: sidebar, pane, tab, hints (default `Designly`) |
| `RUNTIME_APP_MARK_URL` | the URL of the product mark the page shows in place of the harnessly one and gives the browser as the tab icon (default: the harnessly mark) |
| `RUNTIME_LOCALE` | the page's and the API's language: `en` (default) or `ru` |
| `RUNTIME_WEB_FEATURES` | optional parts of the page, comma separated: `chats` (plain chats through an OpenWebUI next to the runtime), `efficiency` (the team report), `vault` (the password safe), `design` (Claude Design boards); none by default. Unknown names, including the retired `next-ui`, are ignored |
| `RUNTIME_WEB_COPY_FILE` | a JSON file of product copy the page uses instead of its defaults (welcome line, suggested first prompts) |
| `RUNTIME_PEOPLE` | `login=Name` pairs, comma separated: display names of the people of this installation (greeting, statistics, mentions) |
| `RUNTIME_PANE_HOSTS` | extra hosts whose pages the side pane's browser tab may open |
| `RUNTIME_SPEED_POSTHOG_HOST`, `RUNTIME_SPEED_POSTHOG_KEY` | a PostHog project the page reports its own speed to (load, long tasks); unset: nothing is sent |

The runtime writes these into the page as `<meta name="hl-config">` when it serves `index.html`, together with the
service addresses below, the tracker and GitLab settings, `RUNTIME_LAYOUT_KEY` and `RUNTIME_LAUNCH_DEFAULTS_KEY`. No token
ever reaches the page.

### Boards and artifacts

The sibling services [boards](https://github.com/ly-solutions/boards) (whiteboards) and
[artifacts](https://github.com/ly-solutions/artifacts) (documents); see [integrations](integrations.md). With an address
set, every tab this runtime starts (Claude Code, Codex, a shell; resumed ones too) gets the service's variables in its
shell, and the page shows a board and a document pane. Tokens stay on the machine: the page asks the runtime for a
board sign-in hand-off and for a conversation's documents. In front of the previous runtime nothing changes until an
address is set, and only tabs this runtime starts get the variables.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_BOARDS_URL` | the boards server's origin, e.g. `https://boards.example.com`; agents get it as `BOARDS_URL` |
| `RUNTIME_BOARDS_TOKEN` | an API token of the boards server (one of its `BOARDS_API_TOKENS`): the runtime mints sign-in hand-offs with it (`POST /b/api/handoff`); unset, the pane loads the board directly and boards signs people in its own way |
| `RUNTIME_BOARDS_AGENT_TOKEN` | the token agents get as `BOARDS_TOKEN` (default `RUNTIME_BOARDS_TOKEN`); give agents their own so it can be revoked alone |
| `RUNTIME_ARTIFACTS_URL` | the catalog's origin, e.g. `https://artifacts.example.com`; agents get it as `ARTIFACTS_URL` and the tab's id as `ARTIFACTS_SESSION` |
| `RUNTIME_ARTIFACTS_TOKEN` | the editor token agents publish with (`ARTIFACTS_TOKEN`) |
| `RUNTIME_ARTIFACTS_READ_TOKEN` | the viewer token the runtime lists a conversation's documents with (default `RUNTIME_ARTIFACTS_TOKEN`) |
| `RUNTIME_ARTIFACTS_SHARE_TOKEN` | a sharer token for the reader's public-link switch (default `RUNTIME_ARTIFACTS_TOKEN`) |
| `RUNTIME_ARTIFACTS_AUTHOR` | who agents publish as (`ARTIFACTS_AUTHOR`); default `<RUNTIME_OWNER_NAME> (agent)` when that is set |
| `RUNTIME_OWNER_NAME` | the person's display name, for board hand-offs and the default author; the hand-off's e-mail is the session's, else `RUNTIME_OWNER_EMAIL` |

#### Agent skills

A standalone runtime installs the [agent skills](integrations.md#agent-skills) for Claude Code and Codex when it starts:
the `harnessly` skill always, `boards` with `RUNTIME_BOARDS_URL`, `artifacts` with `RUNTIME_ARTIFACTS_URL`.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_AGENT_SKILLS` | `off` (also `0`, `false`, `no`, `none`) installs nothing, loads no skills plugin into Claude and adds no `PATH` folder; anything else, or unset, installs them |
| `RUNTIME_AGENT_BIN_DIR` | where the `artifacts`, `boards` and `harnessly` launchers go (default `~/.local/bin`, which login shells put on the `PATH` when it exists) |
| `CODEX_HOME` | Codex's folder (default `~/.codex`); its user skills go to `$CODEX_HOME/skills/harnessly/` |

### GitHub

Issues and pull requests of GitHub (or GitHub Enterprise) for the page: an issue key `owner/repo#12` opens as a task
tab, issue and pull request links show as chips with their state.

| Variable | Meaning |
| --- | --- |
| `RUNTIME_GITHUB_TOKEN` | a token with read access to the repositories (public repositories work without one, rate limited) |
| `RUNTIME_GITHUB_HOST` | the web host, default `github.com`; for GitHub Enterprise its host (the API is then `https://<host>/api/v3`) |
| `RUNTIME_GITHUB_API` | the REST API root when it is elsewhere |
| `RUNTIME_GITHUB_REPOS` | repositories the page may read, `owner/repo` or `owner/*`, comma separated; empty allows any |
