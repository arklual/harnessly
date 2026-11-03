# Connections: trackers, wikis and code hosts

A person connects their own tools once, in **Settings → Connections**, by pasting any link of them: an issue, a page, the
start page. The runtime finds out which product it is, where it runs and which version, asks for the right credentials,
checks them, and from then on the tracker app, link previews, task cards and the agents read that tool through one
common format. No agent ever holds the token.

The code is `internal/connections` (the core and one file per product) and `cmd/runtime/routes_connections.go` (the
routes). Remote MCP servers are connected in the same section and given to the agents as tools: see
[MCP servers as connections](mcp-connections.md).

## Products

| Product | Kind | Reads | Where it runs |
| --- | --- | --- | --- |
| Jira | `jira` | issues | Atlassian Cloud (REST v3), Server / Data Center 7.x–11.x (REST v2), any domain and context path (`/jira`) |
| Confluence | `confluence` | pages | Atlassian Cloud (under `/wiki`), Server / Data Center; login and password before 7.9, tokens after |
| YouTrack | `youtrack` | issues | YouTrack Cloud (`*.youtrack.cloud`, `*.myjetbrains.com`) and self-hosted, any context path (`/youtrack`) |
| Linear | `linear` | issues | linear.app |
| GitLab | `gitlab` | issues, code | gitlab.com and self-managed (the token page moves at GitLab 16) |
| GitHub | `github` | issues, code | github.com, GitHub Enterprise Cloud (`*.ghe.com`) and Server (API under `/api/v3`) |
| Gitea / Forgejo | `gitea` | issues, code | self-hosted |
| Redmine | `redmine` | issues | self-hosted (API key) |
| Yandex Tracker | `yandex-tracker` | issues | tracker.yandex.ru, with the organization id |
| Notion | `notion` | pages | notion.so (an integration token) |
| MediaWiki, Outline, BookStack, Wiki.js | `mediawiki`, `outline`, `bookstack`, `wikijs` | pages | self-hosted or their clouds |

Issues of every product come in one shape: key (`ABC-12`, `group/project#12`, `owner/repo#12`), title, status with a
category (`todo`, `doing`, `done`, `unknown`), type, priority, project, assignee, reporter, parent, labels, dates,
description as plain text or Markdown and the last comments. Pages: id, title, space, excerpt, author, date and body.

## How a link is recognised

Every product looks at the pasted link at once (`connections.Detect`); the first that recognises it wins. A product
claims a link only by its own signature: a known host (atlassian.net, youtrack.cloud, linear.app, github.com, …), its
JSON answer (Jira's `serverInfo`, YouTrack's `/api/config`, GitLab's `/api/v4/version`, …) or its own header — never a
plain page that answers 200. The context path is found by cutting the link before the product's own routes
(`https://h/jira/browse/X-1` → `https://h/jira`). Anonymous requests are shared between products and capped at 12 s per
link; addresses of cloud metadata services (169.254.0.0/16) are refused.

The answer names the sign-in methods in the order the page offers them (an API token with an email for Atlassian
Cloud, a personal access token, a login and password for old Server versions, …), with the address where the token is
created on that very instance.

## Where it is kept

- `connections.json` in the runtime's data folder: product, address, version, sign-in method, the non-secret login, who
  the credentials signed in as, the state of the last check, which connection is primary. No credential.
- The credentials: in [the safe](safe.md) as the secret `conn-<id>`, encrypted with the machine's key. Each read is
  recorded in the safe as a use by «connection <name>».
- Connecting the same instance with the same login again replaces the credentials: that is how a token is rotated.
- Log lines name connection ids, kinds and reasons; never a credential or an address with a login in it.

## The primary connection

Each capability (issues, pages) has one primary connection: the one the person marked, else the first of their own,
else the deployment's. The tracker app and bare task keys (`ABC-12`) use the primary connection with issues; `harnessly
issues` and `harnessly pages` use the primary one unless `--conn` names another.

## The deployment's tracker and GitLab

A deployment that names `RUNTIME_TRACKER_URL` (a YouTrack) gets it listed as the connection `env-tracker`, with the
token the tracker routes already read (`YOUTRACK_TOKEN`, else a `youtrack-token` file in `RUNTIME_SECRET_CONFIG_DIRS`
under the home). The first of `RUNTIME_GITLAB_HOSTS` is listed as `env-gitlab` with `GITLAB_SELF_TOKEN` (or
`gitlab-self-token`). Both are read-only on the page (409 `read-only` to rename or remove), can be made primary, and
the agents read them like any other connection. An instance the person connects themselves hides the deployment's
copy of it.

While `env-tracker` is the primary tracker, the tracker app, task cards (`RUNTIME_TASK_KEY_RE` keys) and the tracker's
link previews keep their own reads, answer for answer. A connection of the person's that is primary is read through
its product instead, in the same answer shapes. Links on any connection (an issue on Jira, a page on Confluence, an
issue on the deployment's GitLab) get a preview whatever is primary.

## Routes

`<platform>` is `RUNTIME_PLATFORM_API` (default `/api/platform`). The page's routes want a page session of the same
origin; the CLI's want the cli-token and only read.

| Route | Answer |
| --- | --- |
| `GET <platform>/connections` | `{connections: Conn[], kinds: Kind[]}` |
| `POST <platform>/connections/detect` `{url}` | `{detection}` |
| `POST <platform>/connections` `{detection, method, credentials, name?}` | `{connection}` after the credentials are checked |
| `POST <platform>/connections/<id>/check` | `{connection}` with the new state |
| `PATCH <platform>/connections/<id>` `{name?, primary?: true}` | `{connection}` |
| `DELETE <platform>/connections/<id>` | 204; the credentials leave the safe |
| `GET <platform>/connections/<id>/projects` | `{projects}` |
| `GET <platform>/connections/<id>/issues?q=&mine=1&open=1&project=&native=&limit=` | `{issues}` |
| `GET <platform>/connections/<id>/issue?key=` | `{issue}` |
| `GET <platform>/connections/<id>/pages?q=&limit=` | `{pages}` |
| `GET <platform>/connections/<id>/page?id=` | `{page}` |
| `GET <platform>/connections/resolve?url=` | `{connection, target: {kind, key}, issue?, page?}`; 404 when no connection owns the link |
| `GET /api/cli/connections`, `/api/cli/connections/resolve`, `/api/cli/connections/<id>/projects\|issues\|issue\|pages\|page` | the same reads for agents |

Refusals are `{error: reason}`: `unknown-product` 422, `denied` 424 (the service refused the credentials — never 401 or
403, which the page reads as its own session), `not-found` 404, `unsupported` and `bad-request` 400, `read-only` 409,
`unreachable` 502.

The existing routes answer connections too: `<platform>/link-meta?url=` gives an issue of a connection as
`{kind: "issue", key, title, state, resolved, assignee, type, parent}` and a page as `{kind: "page", id, title, space,
excerpt}`; `?key=` takes a bare key of the primary tracker. `<platform>/task?key=[&conn=<id>]` gives an issue in the
task card's shape. Both answer once the person connected something of their own, whatever the deployment names. The
page's config carries `connectionHosts`, the hosts of the connections with issues or pages as they are at page load.

## For agents

```
harnessly connections                       what is connected: id, product, address, capabilities, state, primary
harnessly issues --mine --open              my open issues in the primary tracker
harnessly issues --conn ID --project ABC login bug
harnessly issue ABC-12                      one issue with its description and last comments
harnessly issue https://acme.atlassian.net/browse/ABC-12   a link is read by the connection that owns it
harnessly pages onboarding                  search the primary wiki
harnessly page ID|URL                       read one page
```

`--json` prints the runtime's answer. Every agent's prompt says the same next to the safe's block: read trackers and
wikis with these commands, never ask for their tokens in the chat, and when a service is not connected ask the person to
connect it in Settings → Connections. The person's MCP servers reach the agents as
tools of their own ([MCP servers as connections](mcp-connections.md)).
