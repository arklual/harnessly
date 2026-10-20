# Boards, documents and agent skills

harnessly is where people and coding agents work side by side: terminals, conversations, documents and boards in one
place. Two sibling services hold the documents and the boards, each with its own storage and releases:

- [boards](https://github.com/ly-solutions/boards): Excalidraw whiteboards that people draw and agents read as text
  and edit through an API;
- [artifacts](https://github.com/ly-solutions/artifacts): versioned documents agents publish, find and share.

harnessly embeds both next to the conversation and hands their addresses to the agents. Each service documents its
side of the contract in `docs/integrations/harnessly.md` ([boards](https://github.com/ly-solutions/boards/blob/main/docs/integrations/harnessly.md),
[artifacts](https://github.com/ly-solutions/artifacts/blob/main/docs/integrations/harnessly.md)); this page is
harnessly's side.

```
 browser ── harnessly page ──┬── side pane: <iframe> boards editor  (signed in by a one-time hand-off)
                             ├── side pane: <iframe> artifacts /embed/<slug>
                             └── runtime API ── /api/integrations…  ──▶ boards  POST /b/api/handoff        (runtime token)
                                                                    └─▶ artifacts GET /api/v1/artifacts?session=  (viewer token)
 agent in a tab ── BOARDS_URL, BOARDS_TOKEN, ARTIFACTS_URL, ARTIFACTS_TOKEN, ARTIFACTS_SESSION ──▶ boards, artifacts
               └─ harnessly open … / tab … ──▶ runtime /api/cli  ──▶ the right pane, other tabs
```

## Configure

On the runtime ([all variables](configuration.md#boards-and-artifacts)):

```sh
RUNTIME_BOARDS_URL=https://boards.example.com          # as browsers reach it
RUNTIME_BOARDS_TOKEN=…                                 # one of boards' BOARDS_API_TOKENS: mints sign-in hand-offs
RUNTIME_BOARDS_AGENT_TOKEN=…                           # what agents get as BOARDS_TOKEN (default: the runtime's)
RUNTIME_ARTIFACTS_URL=https://artifacts.example.com
RUNTIME_ARTIFACTS_TOKEN=…                              # editor token: agents publish with it
RUNTIME_ARTIFACTS_READ_TOKEN=…                         # viewer token: the runtime lists a conversation's documents
RUNTIME_OWNER_NAME=Ada                                 # who boards shows; agents publish as "Ada (agent)"
```

On boards and artifacts, allow the page's origin to embed them:

```sh
BOARDS_FRAME_ANCESTORS="'self' https://work.example.com"
BOARDS_CORS_ORIGINS=https://work.example.com
ARTIFACTS_FRAME_ANCESTORS=https://work.example.com
```

Host the three under one registrable domain (`work.example.com`, `boards.example.com`, `artifacts.example.com`):
browsers send the services' session cookies to frames only when the framing page is on the same site. On different
sites, set `BOARDS_COOKIE_SAMESITE=none` and `ARTIFACTS_COOKIE_SAMESITE=none` (https only) and expect browsers that
block third-party cookies to ask for a sign-in in the pane every time.

The [compose file](../compose.yaml), the [Helm chart](../deploy/helm/harnessly) and the
[Argo CD app of apps](../deploy/argocd/workspace.yaml) come wired this way ([deploy](deploy.md)).

## What agents get

Every tab the runtime starts or resumes — Claude Code, Codex and plain shells — has in its shell:

| Variable | Value |
| --- | --- |
| `BOARDS_URL` | `RUNTIME_BOARDS_URL` |
| `BOARDS_TOKEN` | `RUNTIME_BOARDS_AGENT_TOKEN`, else `RUNTIME_BOARDS_TOKEN` |
| `ARTIFACTS_URL` | `RUNTIME_ARTIFACTS_URL` |
| `ARTIFACTS_TOKEN` | `RUNTIME_ARTIFACTS_TOKEN` |
| `ARTIFACTS_SESSION` | the tab's id: the conversation the documents belong to |
| `ARTIFACTS_AUTHOR` | `RUNTIME_ARTIFACTS_AUTHOR`, else "`<RUNTIME_OWNER_NAME>` (agent)" |
| `HARNESSLY_DATA_DIR` | the runtime's data folder (the `harnessly` command reads its port and token there); set whether or not a service is configured |

Nothing else of the runtime's environment reaches the panes: a tab's shell starts from an allow-list (`HOME`, `PATH`,
`LANG`, …) plus these. In front of a previous runtime (gate mode), only tabs the Go runtime starts get them.

### Agent skills

A standalone runtime installs the skills for both agents when it starts; there is nothing to do by hand and nothing to
set up inside a tab. Which skills follow which service is configured:

| Skill | When | What the agent learns |
| --- | --- | --- |
| `harnessly` | always | the [`harnessly` command](#the-harnessly-command): show a document, board, file or dev server in the right pane, start and read another agent in a new tab |
| `boards` | `RUNTIME_BOARDS_URL` | read a board from its link as text, draw a diagram from a node and edge graph, edit shapes, answer comments (the `boards` command) |
| `artifacts` | `RUNTIME_ARTIFACTS_URL` | search the catalog before writing, publish, publish a new version under the same slug, share only when asked (the `artifacts` command) |

Where they go, all in folders named for harnessly, so nothing of your own is touched:

| Agent | Location | How it is loaded |
| --- | --- | --- |
| Claude Code | a plugin in `<data folder>/agent-skills/claude` (skills show as `harnessly:boards` …) | the runtime starts every Claude with `--plugin-dir` for it |
| Codex | `~/.codex/skills/harnessly/<skill>/SKILL.md` (`$CODEX_HOME/skills/harnessly`) | Codex reads its user skills by itself |
| both | `artifacts` (a Node.js client, needs Node.js), `boards` (bash and curl) and a link to `harnessly` in `~/.local/bin` (`RUNTIME_AGENT_BIN_DIR`) | login shells put `~/.local/bin` on the `PATH` when it exists |

Each folder keeps a manifest of the files harnessly wrote. A new release replaces them when they are unchanged; a
file that is not harnessly's, or that you edited, is left as it is (the runtime logs it); a skill whose service you
removed is deleted. Starting again with nothing new writes nothing. `RUNTIME_AGENT_SKILLS=off` turns all of it off.

The skills are the files of [ly-solutions/agent-skills](https://github.com/ly-solutions/agent-skills) and
[ly-solutions/artifacts](https://github.com/ly-solutions/artifacts) at the commits pinned in `skills/SOURCES`, compiled
into the runtime, so an offline install has them. `skills/sync.sh` updates the copy and `make skills-check` verifies it
(see `skills/README.md`). Claude Code outside harnessly can install the same plugins from the marketplace:

```text
/plugin marketplace add ly-solutions/agent-skills
/plugin install harnessly@ly-solutions
```

Every agent also gets a short workspace prompt (Claude as a system-prompt file, Codex as developer instructions): where
it is, the `harnessly` command, and the `artifacts` and `boards` commands when those services are set. A deployment can
replace it with its own `RUNTIME_WORKSPACE_PROMPTS_FILE`.

Two things about Codex in a container:

- Codex drops every variable named like a secret (`KEY`, `SECRET`, `TOKEN`) from the commands it runs. The runtime
  starts it with `shell_environment_policy` set so that `BOARDS_TOKEN` and `ARTIFACTS_TOKEN` stay, and only the
  runtime's own CLI token is left out. (`harnessly` reads that token from the data folder, not from its environment.)
- Codex's own sandbox (bubblewrap) needs user namespaces, which a default Docker container does not allow: every
  command then fails with "No permissions to create a new namespace". Inside such a container the container is the
  sandbox, so the Docker image sets `RUNTIME_CODEX_SANDBOX=container`: the runtime starts Codex with
  `-c sandbox_mode="danger-full-access" -c approval_policy="never"` (Codex does not ask before running commands).
  Everywhere else the default is `codex`, which leaves Codex's sandbox and approvals alone. Set the variable to either
  value to override the image, for instance `RUNTIME_CODEX_SANDBOX=codex` on a container that does allow user
  namespaces. `dangerouslySkipPermissions` in the runtime's settings (Codex then runs with `--yolo`) says the same and
  wins, and `sandbox_mode` / `approval_policy` in `~/.codex/config.toml` apply under `codex`.

### The `harnessly` command

The command is installed next to the runtime (in the image: `/usr/local/bin/harnessly`; from a release or `make install`:
beside `harnessly-runtime`) and finds the runtime by itself: its port and token are files in the data folder, whose
place every pane gets as `HARNESSLY_DATA_DIR`. It talks to the runtime's `/api/cli` routes; `harnessly api-guide`
prints them for what the command does not cover. Run `harnessly --help` for all of it.

```text
harnessly open <link|file|port|kind>   show it in the person's right pane (documents, boards, files, a dev server, a merge request)
harnessly panel show|hide              the whole pane
harnessly status                       what was asked of the pane, what the person has seen
harnessly workspaces                   the workspaces
harnessly tab list|create|send|status|result|close
                                       tabs; `tab create -t claude-code|codex-cli --prompt TEXT` starts another agent
```

`open` works with the page closed: the request waits with the conversation. Documents and boards open when their address
is on the servers you configured (`RUNTIME_ARTIFACTS_URL`, `RUNTIME_BOARDS_URL`), a dev server by its port; an address
the pane may not open is refused with the reason. `--json` prints the runtime's answer, `-w` picks the workspace.

## In the page

- **Chips.** A board link (`<boards>/b/<slug>?k=<key>`) or a document link (`<artifacts>/a/<slug>`, or the page's own
  reader `<base>/docs/<slug>`) in an agent's reply or a person's message is a chip; clicking it opens it in the right
  pane. GitHub issue and pull request links, tracker task keys and GitLab merge requests are chips too.
- **Board tab.** Before loading a board, the page asks the runtime for a one-time sign-in link
  (`POST /api/integrations/boards/sign-in`, which calls boards' `POST /b/api/handoff` with the runtime's token for the
  signed-in person) and frames that: boards sets its own session and lands on the board's live editor. Without
  `RUNTIME_BOARDS_TOKEN` the pane loads the board directly and boards signs people in its own way.
- **Document reader.** `<platform>/docs/…` serves the reader from the catalog: the list of documents with open
  threads and unread comments, one document, its versions and their diff, and a short-lived read link
  (`GET /api/v1/artifacts/{slug}/access` with the viewer token) the reader frames, so no token reaches the browser.
  The public-link switch uses the sharer token. Comment threads are kept by the runtime in `doc-threads.json` in its
  data directory.
- **Artifacts tab.** The documents of the current conversation (`GET /api/integrations/artifacts/documents?session=…`).

## Not done yet

- boards' comment webhook and artifacts' change webhook are not received yet: the document list polls, and board
  comments addressed to an agent are not routed to it.
- There is no conversation URL for `ARTIFACTS_SESSION_URL` yet, so the catalog cannot link a document back to its
  conversation.
- The pane has no board picker or "new board" button; agents create boards, people paste links.
- Selecting text in the reader to start a thread needs the reader's selection bridge inside the document frame,
  which the catalog's read view does not carry yet; threads can be read and answered.
