# Backend: where your code runs

**The host runs no server for an app.** There is no process it starts, supervises or proxies. An app that needs code beyond the browser
has three places to put it, and each reaches different things. Choose by who has to call it.

```mermaid
flowchart TB
  subgraph browser["The person's browser"]
    view["View (sandboxed)"]
  end
  subgraph machine["The machine"]
    runtime["Runtime"]
    agent["Agent (Claude Code), in the wall"]
    stdio["Local MCP server: a child of the agent"]
    files[("The chat's folder")]
  end
  subgraph internet["Your server, public https"]
    remote["Remote MCP server, HTTP API"]
  end
  view -->|"hl.files"| runtime
  runtime --> files
  view -->|"hl.tools.call"| runtime
  runtime -->|"the machine calls: public https only"| remote
  view -->|"fetch, with network.connect"| remote
  agent -->|"tools"| stdio
  agent -->|"tools"| remote
  stdio --> files
```

## Choose

| You need | Do this | Tier |
| --- | --- | --- |
| The agent to use a public API | A remote server in `.mcp.json` | `declarative` |
| A view to call that same server | `hl.tools.call('server.tool', args)`, public https, no secrets | `declarative` or `sandboxed` |
| A view to call your own API directly, with tokens of its own | `network.connect` and `fetch` | `sandboxed` |
| Pictures or fonts from a CDN | `network.resources` | `sandboxed` |
| A whole page from your server | `entry: "https://…"` with `network.frames`. It has no bridge and no SDK session | `sandboxed` |
| The agent to run a local program | A `command` server in `.mcp.json` | `trusted` |
| Data shared by that program and a view | Files of the chat's folder | `trusted`, `files.*` |
| A value stored per person, seen by a view and the agent | `hl.storage.chat` and `app.data_*`. Each key has a version; pass `ifVersion` to avoid overwriting the other side (`conflict`). The view hears the agent's writes by the `storage` event. See [Views](02-views.md#versions) | any |
| A secret for the agent | `harnessly secret`, see [Agents](05-agents.md#secrets) | any |
| A secret per install, for your server | Manifest `secrets` and `${secret:name}` in the `headers` of `.mcp.json`; see [Secrets, once more](#secrets-once-more) | `declarative` or `sandboxed` (remote), `trusted` (local, in `env`) |
| Choices the person makes once for the app | Manifest `settings`, read with `hl.settings.get()` | any |

## A remote MCP server

```json
{
  "mcpServers": {
    "bank": {
      "type": "http",
      "url": "https://bank.example.com/mcp",
      "headers": { "x-app": "cashback" }
    }
  }
}
```

The agent gets its tools from Claude Code at launch. A view reaches the same tools:

```js
const res = await hl.tools.call('bank.get_categories', { month: '2026-11' });
if (res.isError) throw new Error(res.content?.[0]?.text);
```

What `hl.tools.call` does:

- **The machine makes the call, not the browser**, so it refuses to be a way into the person's network. It resolves the name and checks the address it really
  dials: `https://` and a public address. Loopback, private ranges, link-local and carrier-grade NAT (`100.64.0.0/10`) are `network_denied`; another scheme is
  `host_not_allowed`, a `command` server `policy_blocked`. No proxy, no redirects.
- **Internal hosts** are opened by the admin of the deployment, never by an app: `RUNTIME_APPS_INTERNAL_HOSTS=gitlab.corp,*.svc` on the machine lets those names
  resolve to private, loopback or shared addresses and use plain `http://` too; link-local (the cloud's metadata) stays closed. `RUNTIME_APPS_CA_FILE` adds the
  organization's root certificates. A certificate the machine does not trust is `tls_untrusted`; a server that answers an HTTP error or no JSON-RPC is `upstream_error`.
- Each call is a new connection: `initialize`, `notifications/initialized`, `tools/call`. 30 s overall, a 4 MB answer, JSON or an event stream.
- **A header with `${secret:name}` is filled by the machine** from the app's secret of that name; the view never holds the value. Any other header containing `${` is not sent. Literal headers are sent, and anyone holding the package reads them, so keep tokens out of them. If a secret has no value yet, the call is refused and the app waits for it. In the agent's use of the same server, `${VAR}` is Claude Code's expansion from its environment, and `${secret:name}` is filled by the host.
- **A server can be the person's own connection.** `{"type": "http", "connection": "linear"}` (an id from the catalog in Settings → Connections) takes the address and the sign-in from the connection the person made there, so the agent and `hl.tools.call` work as the person, not through an install's secret. Only a remote server (`http` or `sse`) may name a connection, and then without `url`, `headers` or `command`; an unknown id fails validation. The package, the guard's snapshot and the agent's environment never hold the person's token: the agent gets the address of the runtime's proxy (`/api/cli/apps/mcp/<app>/<server>`) with the launch's token, and the proxy signs each call in from the machine's safe and renews the token itself. Connections live on each person's machine, so two people on one app go each under themselves. Without the connection, or before the sign-in, the app is «ждёт вход»: the chat starts, the server is left out, a view's `hl.tools.call` is refused, and «О приложении» offers «Подключить» (`waitingConnections` in the app's description). The app uses a connection only after the person agrees to it for that app (`harnessly app connect <id> <connection>`, `--off` takes it back; the agent cannot run it); an update that names a new connection waits for its own agreement. A view reaches Streamable HTTP connections only.
- You get the raw MCP result. `isError: true` is not an error of the call: check it.
- Failures arrive as a word only: `host_unavailable` for unreachable and timeout, `policy_blocked` for refusals and a JSON-RPC error.

A minimal server, no dependencies, is in [`standup/remote-server-example/server.mjs`](../apps/standup/remote-server-example/server.mjs). It was run against a hand-written
client and Claude Code. It speaks stateless Streamable HTTP, one JSON-RPC message per POST:

```text
POST /mcp  initialize                  200 application/json, protocolVersion, capabilities.tools
POST /mcp  notifications/initialized   202, no body
POST /mcp  tools/list                  200, the tools with inputSchema
POST /mcp  tools/call                  200, { content: [{ type: "text", text }], structuredContent? }
GET  /mcp                              405 (no event stream is required)
```

## A view calling your API with fetch

```json
"trust": "sandboxed",
"network": { "connect": ["api.example.com"] }
```

The browser makes this call, so the machine's address rules do not apply, and a person's internal host works if their browser can reach it. The frame's origin is opaque, and this was tested in Chromium:

- Every request carries **`Origin: null`**. Without CORS headers the fetch fails.
- `Access-Control-Allow-Origin: *`, or echoing `null`, works. `Authorization` and JSON bodies cause a preflight: answer `OPTIONS` too, and put the header on the real response as well.
- `credentials: 'include'` fails with `*`. It works with `null` echoed, `Access-Control-Allow-Credentials: true` and a cookie with `SameSite=None; Secure`, but browsers that block third-party cookies drop it (Safari and Chrome's blocking were not tested).

**Use a bearer token.** Let the person sign in on your site, have them paste or receive a token, and keep it in `hl.storage.user`. Then only your app's views can read it.

```js
const token = await hl.storage.user.get('token');
const res = await fetch('https://api.example.com/standup', { headers: { Authorization: 'Bearer ' + token } });
```

Allowing `Origin: null` means any sandboxed page may call you, so the token has to be what authorizes a request, not the origin.

## A local program

```json
{
  "mcpServers": {
    "standup": { "command": "node", "args": ["${CLAUDE_PLUGIN_ROOT}/backend/server.mjs"] }
  }
}
```

This needs `"trust": "trusted"`. The trusted tier is installed by hand by each person, and is in no catalog. All of the following was observed with a real agent:

- **Claude Code starts it**, as a child of the agent, for the life of that launch. harnessly neither starts nor supervises it, so it does not run when no agent does. When it crashes, its tools are gone until the agent restarts.
- `${CLAUDE_PLUGIN_ROOT}` expands to the package's folder.
- **Its working folder is the chat's folder.** So `./standup/` in the server is `standup/` in `hl.files`. That folder is the one the chat was started in: chats started in one folder share these files, and a chat in another folder or a worktree has its own.
- **It runs inside the wall** in a chat with apps. It can write the chat's folder and cannot write its own package. The app's storage and the safe's files are not there.
- It inherits the agent's environment, including `HARNESSLY_CLI_TOKEN_FILE`, the launch's confined token: what the agent may do through the runtime, such as `harnessly secret run`, your program and its dependencies may too. Vet them as you would the agent.
- **A view cannot call it**: `hl.tools.call` is `policy_blocked`, and it has no port. Share data through files:

```mermaid
sequenceDiagram
  participant A as Agent
  participant S as Local server
  participant F as standup/2026-11-03.json
  participant V as View
  A->>S: save_entry(Anna, ...)
  S->>F: write
  V->>F: hl.files.read (permission files.read:standup/*.json)
  V->>F: hl.files.write after the person edits
  V->>A: hl.chat.send("An entry was edited, read them again")
  A->>S: list_entries
  S->>F: read: updatedBy person
```

- `hl.files.read` of a file that does not exist rejects with `invalid_params`, as does text it cannot read. Treat that as «no data yet».
- **Put no file content into `hl.chat.send`.** The agent reads that message as the person's words, so a name from a file could give it instructions. Send fixed text, and have it read the data through a tool.
- **The rules belong in the server.** `instructions.md` asks; the program decides. In the example, `save_entry` refuses to empty an entry, and refuses an entry the person edited unless it is given the `updatedAt` the agent read.
- Neither side locks. If both write in the same instant, the last one wins: read just before you write, and keep each write small.

A local server could open a port of its own and be listed in `network.connect`, but it lives only while the agent does, so a view would find it gone. Prefer files.

## Cards from a tool

A tool of your own server can name a card:

```json
{ "_meta": { "ui": { "resourceUri": "ui://acme.thing/card" } }, "data": { "date": "2026-11-03" } }
```

The whole JSON may run up to 64 KB, on one line or several. The view reads `data` as `hl.context.data`. To change a card later, or to
show one without a tool call, use a [widget](02-views.md#widgets-in-the-chat).

## Secrets, once more

| Where | Today |
| --- | --- |
| Manifest `secrets` + `permissions: ["secrets:name"]` | The person puts the value into the machine's safe from «О приложении» (`app/<id>/<name>`). Neither a view nor the agent can read it |
| A header with `${secret:name}`, agent's use and `hl.tools.call` | The machine writes the value into the header when the chat starts (or the call is made). A new value needs no reinstall: it is read at the next launch |
| `${secret:name}` in `env` of a local server | Same, for a `trusted` app only. Not in `args` (a command line shows in the process list) and not in the `url` |
| A required secret without a value | The app is «ждёт секрет» in «О приложении». The chat starts; the servers that need the secret are left out |
| A header with `${VAR}`, agent's use | Claude Code expands it from the agent's environment |
| A header with `${VAR}`, `hl.tools.call` | The header is not sent |
| The agent | `harnessly secret run --env VAR=name -- command` |
| A view | A token the person gave it, in `hl.storage.user` |
| A local server | It inherits the agent's environment, and may run `harnessly secret run` like the agent. Not tested from inside the runtime's wall |

For an app with a secret, Claude loads a copy of the package folder made for the launch (links to the real files and its own `.mcp.json`); the installed package is never changed. **A remote server gets no value in any file**: its entry points at the runtime's proxy (`/api/cli/apps/mcp/<app>/<server>`) with the launch's token, and the proxy, for an app on in that chat only, adds the package's headers with the secrets and calls the real https address under the rules of `hl.tools.call` (streams pass through; a missing secret answers 409). A local server of a `trusted` app takes the value in `env`, so that file holds it: it lives in a folder of this launch outside the registry, which the wall opens for this chat only, and the runtime removes it once the launch's agent is gone.

### Settings

```json
"settings": {"type": "object", "required": ["region"], "properties": {
  "region": {"type": "string", "title": "Region", "enum": ["eu", "us"], "default": "eu"},
  "limit": {"type": "integer", "minimum": 1, "maximum": 50, "default": 10}}}
```

The form is in «О приложении»; a view reads `await hl.settings.get()` and the agent calls the host tool `settings_get`. Types other than `string`, `number`, `integer` and `boolean` are refused by the validator.

Next: [the worked example](08-worked-example.md).
