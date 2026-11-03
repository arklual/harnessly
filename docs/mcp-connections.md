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
