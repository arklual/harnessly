# MCP servers as connections

A person connects remote MCP servers (Atlassian, Linear, Notion, Sentry, GitHub, …) once, in **Settings → Connections →
MCP servers**, and every Claude Code and Codex chat started after that has their tools. The runtime keeps the
credentials and signs every request; an agent never holds the service's token.

The code is `internal/mcpconn` (storage, probing, the OAuth client, the catalog), `cmd/runtime/routes_mcp.go` (the page's
routes, the sign-in callback and the agents' proxy) and `cmd/runtime/mcp_agents.go` (what a launch is given).

## Connecting

The page offers a catalog of well-known servers and a field for any MCP URL (streamable HTTP or the legacy HTTP+SSE
transport). Connecting always probes the URL first (`initialize` without credentials), so a catalog entry is only a
starting point. The probe finds how the server signs in:

| Sign-in | Found by | What the person does |
| --- | --- | --- |
| `none` | `initialize` answers without credentials | nothing: the server is connected at once |
| `oauth` | a 401 with OAuth metadata: `WWW-Authenticate: Bearer resource_metadata=…` (RFC 9728), the `.well-known` protected resource document, or the MCP 2025-03-26 fallback on the server's origin | **Sign in** opens the service's own sign-in in a popup; it comes back to harnessly and the row turns connected |
| `header` | credentials required and no OAuth metadata, or OAuth without dynamic client registration | pastes a token; the header name defaults to `Authorization`, a bare token gets `Bearer ` |

The OAuth client registers itself dynamically (RFC 7591) as a public client named `harnessly`, uses PKCE S256, sends the
server's canonical URL as `resource` (RFC 8707) and refreshes the access token a minute before it expires. A refresh the
service refuses puts the server back to "Sign-in expired".

## Where it is kept

`<data>/mcp-connections.json` (0600) holds the servers: id, name, slug, URL, transport, sign-in kind, header name,
whether agents get it, the last state and the tools `tools/list` named. Credentials are in the safe as `mcp-<id>`: the
typed header value, or the OAuth client and tokens. Client registrations are cached per authorization server and
redirect URI; a client secret, when a service hands one out, is in the safe too.

## Routes

The page's routes need the page session and the same origin (`sameorigin`), like the other connections:

| Route | Answer |
| --- | --- |
| `GET <platform>/mcp` | `{servers, catalog}` |
| `POST <platform>/mcp/probe {url}` | `{probe: {url, transport, auth, name?, oauthUnavailable?, headerName?}}` |
| `POST <platform>/mcp {url, name?, catalogId?, transport?, header?}` | `{server, authStart}` (`null` when connected at once); an OAuth server waits in state `signin` |
| `POST <platform>/mcp/<id>/signin {origin, returnTo?}` | `{authUrl}` |
| `GET <platform>/mcp/callback?code&state[&error]` | the small page that ends a sign-in (below) |
| `PUT <platform>/mcp/<id>/header {name, value}` | `{server}`, after a check with the new credential |
| `POST <platform>/mcp/<id>/check` | `{server}` with its tools read again |
| `PATCH <platform>/mcp/<id> {name?, enabled?}` | `{server}` |
| `DELETE <platform>/mcp/<id>` | 204 |

Errors are `{error: reason}`: `bad-request` 400, `not-found` 404, `signin-required` 409, `denied` and `oauth-failed`
424, `unreachable` 502. None is a 401 or 403, which the page reads as its own session.

### The sign-in's way back

The page sends `location.origin`; the runtime takes it only when it is the origin the request came from (the `Origin`
header, else the host asked directly or through a proxy's `X-Forwarded-Host`). The redirect URI is that origin, the
prefix the browser put before `/api` (the app base path, `RUNTIME_APP_BASE_PATH`, `/m/<machine>` in a hub, when the
request came under it) and `<platform>/mcp/callback`.

The callback is a top-level navigation from the service's site, so it is not refused as cross-site; it needs the page
session (the cookie is `SameSite=Lax` and comes with it), and the state of a sign-in this runtime started (once, within
10 minutes). It answers a page without outside resources (CSP `default-src 'none'`, its script and style by a nonce)
that posts `{type: "harnessly-mcp", id, ok, reason}` to `window.opener` at the origin the sign-in started from and
closes; opened without an opener, it says how it went and links back.

## For agents

Every enabled server that is not waiting for a sign-in goes to each Claude Code and Codex launch (and resume) as one
more MCP server named `mcp-<slug>`, at the runtime's proxy on loopback:

- Claude Code gets one more `--mcp-config <data>/mcp/servers-<hash>.json` (0600, by content) with the runtime's own
  token header, and a copy of its settings whose `permissions.allow` adds `mcp__mcp-<slug>`: the tools are
  `mcp__mcp-<slug>__<tool>` and run without asking.
- Codex gets `-c mcp_servers.mcp-<slug>.url=…` and `-c mcp_servers.mcp-<slug>.env_http_headers={"x-cli-token"=…}`,
  the same token variable as the runtime's own MCP server.
- In a chat with apps the agent runs behind the operating system's wall, where the data folder and the person's token
  read as nothing. Its config is the same servers with the launch's own agent token (written next to the apps' other
  launch files), which the proxy takes as it takes the runtime's token. The app guard decides these tools like any other.
- The prompt's connections block says the servers are there as tools and to send the person to Settings → Connections
  for a missing one.

The list and state are read at every launch: a server connected while a chat runs reaches that chat when it starts
next.

### The proxy

`ANY /api/cli/mcp-proxy/<id>` takes the runtime's cli-token or a launch's agent token and forwards to the server's URL:

- Only the transport's headers pass (`Accept`, `Content-Type`, `Mcp-Session-Id`, `MCP-Protocol-Version`,
  `Last-Event-ID` up; `Content-Type`, `Mcp-Session-Id`, `MCP-Protocol-Version`, `Retry-After` down). The agent's token
  and cookies never go upstream; the server's sign-in challenge never comes back.
- Fresh credentials come from the service at every request. A 401 from the server refreshes the sign-in once and sends
  the buffered request again (bodies up to 4 MiB); a second 401 answers 409 `signin-required`, not a 401 the agent's
  CLI would try to answer with a sign-in of its own.
- Event streams are copied as they come, flushed at every write.
- The legacy SSE transport: the `endpoint` event of the stream is rewritten to `/api/cli/mcp-proxy/<id>/message?to=…`,
  and a POST there goes to the announced endpoint, only on the server's own origin.
- Redirects are not followed, there is no overall timeout (a stream lasts as long as the agent keeps it), and cloud
  metadata addresses (169.254.0.0/16) are refused. Log lines name the server id and the status only.
- A server turned off for agents answers 404.

## Local servers, APIs and the hub

The catalog has three kinds of entries (`internal/mcpconn/catalog*.go`, 336 in all, in 23 groups):

- **Remote MCP servers** (`transport: http|sse`): the vendor's own hosted server, signed in with OAuth (dynamic client
  registration), a typed header or nothing. `TestRealRemoteServers` (`HARNESSLY_E2E_REMOTE=1`) probes every one as the
  page does. Not here, and why: Figma's and Slack's hosted servers admit only their own list of clients; Google's and
  Microsoft's (Gmail, Drive, Graph) have no client registration; T-Invest's needs the Russian root CA.
- **Local servers** (`transport: stdio`): an open-source MCP server the runtime starts itself, `npx -y <pkg>@<version>`
  or `uvx <pkg>==<version>` (uv is installed into `~/.local/bin` on first need). The person types the entry's fields
  (`env`); they are kept in the safe as one credential `mcp-<id>` and given to the process as environment variables,
  with nothing else of the runtime's environment. One process serves every chat of the machine: the runtime makes the
  handshake once, answers the agents' `initialize` from it, gives every request an id of its own, refuses the server's
  requests to the client (sampling, roots) and stops a process idle for 15 minutes. `PUT <platform>/mcp/<id>/env`
  changes the keys (an empty one keeps its value) and restarts the process. `Server.problem` says why one does not start
  (no npx, the package does not run, the keys were refused). `TestRealLocalServers` (`HARNESSLY_E2E_MCP=all|<ids>`)
  starts every one with made-up keys.
- **APIs** (`transport: api`): a service with no MCP server worth running (Moscow Exchange ISS, Steam, T-Invest,
  Tochka, Yandex Music/Disk/Metrica/Webmaster/Direct/Tracker, Selectel, Figma REST, Higgsfield, Telegram Bot, VK,
  Mattermost, amoCRM, Bitrix24, exchanges, arXiv, OpenAlex, PubMed…). The entry has a base address (it may hold a
  `{FIELD}`: a self-hosted Mattermost, a Bitrix24 webhook), where the keys go (`apiHeaders`, `apiQuery`, `{BASIC:A:B}`
  for basic auth), a `checkPath` asked when the keys are typed, and a `guide` of the main paths for the agent. A path
  never leaves the base; an answer never carries a key back.

A local server that takes a setting as a command-line argument, not an environment variable (SQLite's file path), puts
`{FIELD}` into its `Command`; the person's value for that field goes in its place, and an empty field drops the argument.
Not here, and why: Playwright, Chrome DevTools and Puppeteer (the agent has its own browser, `browser_*` and `chrome_*`;
a second, empty one would duplicate it, and Puppeteer's reference package is archived); Atlassian's server already
covers Jira and Confluence; HubSpot's and Box's hosted servers admit no client registration (HubSpot is here as the
local server with a private app token, Box's local package does not import); Zendesk has no vendor server, only
community packages with a few hundred weekly downloads.

### A service's page and how to add an entry

Each entry has its own page, `/settings/connections/<id>`, written by hand: a description, three to
five things an agent can do with the service, the numbered steps to connect and official links, in Russian and in
English. It is text for the person; an agent is never given it. The pages are in `internal/mcpconn/service_pages*.go`:
`service_pages.go` holds the type and the first pages, the others one map per group of the catalog.

To add a service:

1. Put the entry in `catalog_new.go` (or the file of its kind): the pinned package or the vendor's address, the
   sign-in, `Blurb` and `BlurbRu` (one sentence each). For a local server start it with made-up keys first:
   `HARNESSLY_E2E_MCP=<id> go test ./internal/mcpconn -run TestRealLocalServers -v`, for a remote one
   `HARNESSLY_E2E_REMOTE=1 … -run TestRealRemoteServers`. An entry that does not start does not go in.
2. Write its page in the `guides…` map of a `service_pages_<group>.go` file under the same id: facts about the service
   and the server's real tools (read the package's tool list, not its marketing), steps that match the entry's fields
   or sign-in, and only official links, each one opened with `curl` (200, or a real page behind a bot check; a link
   you are not sure of stays out).
3. `TestCatalogGuides` fails an entry without a page, a page without steps or links, English text with Russian in it and
   a Russian text that is not Russian. `go test ./internal/mcpconn` runs it.
4. The catalog is part of the agent's surface (`evals/agent/surface.txt`): a new group of services or a change of how
   the hub finds them comes with a scenario in `evals/agent/hub-*.yaml`.

### The hub

Agents get one MCP server for all of it, `harnessly-hub` (`/api/cli/mcp-hub`, `internal/mcpconn/hub.go`), so a
hundred connections do not put thousands of tool descriptions into every chat:

| tool | what it does |
|---|---|
| `search_tools(query)` | ranks the tools of every turned-on connection by the words of the task (Russian or English); names catalog services that are not connected but would fit |
| `connections(query?)` | the connections in one line each; with a query, the matching ones with their tools |
| `tool_schema(connection, tool)` | the tool's input schema, read live (kept 10 minutes) |
| `call(connection, tool, arguments)` | calls it; a failure on the way is tried once more (OAuth token refreshed, local process restarted); an answer over 12 000 characters is saved to a file and only its start comes back with the path |
| `api_request(connection, method, path, query, body)` | a request to an API connection with the person's keys |
| `manage(connection, check|on|off)` | checks a connection again, turns it on or off |

When the turned-on servers have at most 40 tools together, agents also get them as servers of their own
(`mcp__mcp-<slug>__<tool>`), as before; past that, only the hub. The scenarios `evals/agent/hub-*.yaml` check that an
agent finds and uses connections through the hub (`setup.connections` connects keyless catalog entries on the stand).

## For apps

The same connections reach an app's views through `hl.connections` of `@harnessly/app-sdk` (docs/apps.md, «The person's
connections»; the guide `harnessly app docs connections`): `list`, `tools`, `call`, `api` and `request`, over
`POST <platform>/apps/connections` (`cmd/runtime/routes_apps_connections.go`). An app names the catalog entries (or groups,
the catalog's `kind`) it needs in its manifest, the person agrees to each for each app, and what it reads is only what the
server marks `readOnlyHint` and GET requests (`mcpconn.APIReads` also lists the searches of an API that are POSTs, such as
Yandex Tracker's `/issues/_search`); a change goes out only after the person's yes to that call, in the page. An answer is
scrubbed of the connection's keys (`Service.Redact`), at most 512 KiB, 20 calls in 10 seconds per app and connection. The
connections' service tells the apps when it writes (`Service.OnChange`), which is the event `connections`. Connections made
by a pasted address are services of the person's own and are named in a manifest as `custom-<slug>`, below.

## The person's own services

A service that is in no catalog (the company's internal API, a self-made MCP server, a local server of one's choice) is a
connection of the same record as the catalog's, with an id of its own, `custom-<slug>` (`Server.catalogId`; the slug is
made once from the name and made unique, the machine's id of the record stays `m…`). It is kept in `mcp-connections.json`
and its keys in the safe as `mcp-<id>` like the others, so the hub, the proxy, the apps' `hl.connections` and the page
treat it as one of them. A server added by a pasted address gets the same id and an empty description (older records are
given theirs when the file is first read). The description, which the catalog writes by hand in `service_pages.go`, is
`Server.custom`: the group (`category`: any of the catalog's groups, `utility` when none fits), the one-letter mark,
`about`/`aboutRu`, `can`, `steps`, `links`; an API's `guide` is the record's own. `entryOf(srv)` reads a catalog entry or
makes one from the record, which is what the hub's search, the page and the apps' groups use.

| Kind | What it keeps | Check on connecting |
| --- | --- | --- |
| `mcp` | the address, `http` or `sse` (the probe decides), a header name and the one key of it, or OAuth, or nothing | `initialize`, then `tools/list` with the key; OAuth waits for the person's sign-in |
| `api` | the base address (it may hold `{KEY}`), where each key goes (`apiHeaders`, `apiQuery`, `{BASIC:A:B}`), `checkPath`, the guide of the main paths | a GET of `checkPath` with the keys |
| `local` | `npx -y <package>@<exact version> …` or `uvx <package>==<exact version> …` and its keys as environment variables; a Docker image, a script, `latest` are refused | started once, only after the person's yes |

A key is never a value of the record. `keys[]` names the variables (`API_KEY`), `apiHeaders`/`apiQuery` carry only
`{API_KEY}` placeholders (a header whose value is a literal secret is refused), and the value is typed by the person on the
service's page or copied by the runtime from a secret of the safe the key names (`keys[].safe`, which must start with the
service's id: an agent cannot point a service at another secret of the safe).

### How an agent connects one

The hub's `connection_add` (only on a runtime that can show a card; `connection_add {help: true}` prints the steps):

1. The agent finds out the kind from the address, the docs or the OpenAPI file, and writes the record: for an API the
   base address (`servers[0].url`), where the key goes (from the security scheme), a cheap `checkPath`, a guide of the main
   paths; the description in the person's language; the category.
2. It asks for each key with `harnessly secret ask custom-<slug>-<key> --why …` (a card of the safe) and names the secret in
   `keys[].safe`. A key is never an argument, a command line or a message.
3. `dry_run: true` tries it (an MCP address is initialised and its tools listed with the key; an API's check path is asked
   with the key from the safe; a local server is never started) and says what would be made.
4. Without `dry_run` the person gets a card in the agent's own chat, the card of `harnessly connector act`
   (`<platform>/connector-actions`, a proposal of the connectors' service made by `ProposeRun`), listing the kind, the
   address, where each key goes and from which secret, what was tried, the description and, for a local server, the command
   and a warning that it runs someone else's code. Only a yes makes the record (`Service.SaveCustom`); the call waits 90
   seconds and answers "waiting", to be called again with `proposal=<id>`. The same id changes a service made before. The
   card is shown on the page only, never in a chat bot (a one-tap «Send» would not show the command that runs).
5. The answer says what was made and how to use it (`api_request(connection, path)`, `call`, or the sign-in the person still
   has to make). The proposal and its yes are in the journal of sends (`connector-sends`), without any key.

### By hand

Settings → Connections → **Your own service**: «Connect with the agent» opens a new chat with the request written in its
field; «Add by hand» has an MCP address (the probe finds the sign-in), an API (base address, the header and prefix the key
goes in, a check path, the key) and a local server (a command, with a checkbox saying that it runs someone else's code).
The page of a service of one's own is the page of a catalog one with an editor of its name, group and description
(`PATCH <platform>/mcp/<id> {name?, custom?, guide?}`), the keys form, check, turn off and remove. `POST <platform>/mcp/custom
{input, values}` makes or changes one (`input` is `mcpconn.CustomInput`, `values` the keys typed).

The scenarios `evals/agent/own-api-by-spec.yaml` and `own-mcp-by-address.yaml` check the agent's way.
