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

Nothing else of the runtime's environment reaches the panes: a tab's shell starts from an allow-list (`HOME`, `PATH`,
`LANG`, …) plus these. In front of a previous runtime (gate mode), only tabs the Go runtime starts get them.

### Agent skills

The variables are what the [agent skills](https://github.com/ly-solutions/agent-skills) read. Install them into Claude
Code on the machine (or into the image) once:

```text
/plugin marketplace add ly-solutions/agent-skills
/plugin install boards@ly-solutions
/plugin install artifacts@ly-solutions
```

With them an agent reads a board from its link as text, draws diagrams from a node and edge graph, answers board
comments, searches the catalog before writing a document, publishes new versions under the same slug, and shares only
when asked. Every document it publishes records `ARTIFACTS_SESSION`, so the pane can list a conversation's documents.
The `artifacts` CLI the skill uses is `npm install -g @ly-solutions/artifacts`.

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
