# The person's connections: mail, trackers, any service

Your app's views can show data from the services the person connected in **Settings → Connections**: Yandex Mail, a
tracker, a calendar, a bank's API, any MCP server or local server. They are the same connections the agents reach through
`harnessly-hub`, and the machine signs every request in as the person. Your app has no key, no sign-in screen and no
server of its own. A widget in the sidebar that shows the open issues of Yandex Tracker is thirty lines.

| You want | Use |
|---|---|
| Data of a service the person already connected, in a view | `hl.connections` (this page) |
| The agent to use a service in the chat | nothing: the agent has the hub already |
| Your own backend, with its own tools | an MCP server in `.mcp.json` ([Backend](03-backend.md)); it may itself be the person's connection (`"connection": "linear"`) |

## 1. Say what you need

In `harnessly/app.json`, the right and the list:

```json
{
  "targets": ["side"],
  "permissions": ["connections.read"],
  "connections": [
    { "id": "yandex-tracker", "access": "read", "reason": "Shows your open issues" },
    { "category": "mail", "reason": "Counts unread mail" }
  ]
}
```

- `id` is a catalog entry of Settings → Connections, or a service of the person's own (`custom-<slug>`, the id on its page and in
  `hl.connections.list()`: the company's internal API, a self-made MCP server, anything not in the catalog; see below);
  `category` is its group (`mail`, `calendar`, `tasks`, `docs`, `code`,
  `chat`, `files`, …) and stands for every connection of the person in it.
- `access` is `read` (the default) or `all`. `all` needs the right `connections.act` as well, and is for an app that
  creates or sends something.
- `reason` is the line the person reads on the card that asks for their agreement. Write what the app does with it.

The install screen lists the rights in the person's words. The connections themselves are agreed to one by one, below.

### The person's own services

A service that is in no catalog gets an id of its own, `custom-<slug>` (`custom-acme-orders`), the moment the person adds
it: by an agent ("connect our orders API, here is the spec"), by the form in **Settings → Connections → Your own service**,
or by pasting an MCP address. The id is stable (the slug is made once, from the name; a rename does not change it), so an
app can name it: `{ "id": "custom-acme-orders", "access": "read", "reason": "Shows today's orders" }`. Such a service reads
exactly like a catalog one: the same `list()` statuses, `api()` for an API, `tools()` and `call()` for an MCP server, the
same agreement of the person for each app, the same reads and the same yes for a change. Its group is the one the person
(or the agent) chose, so `{ "category": "crm" }` finds it too. The person may not have connected it yet when the app is
installed: the app is "waiting for sign-in" until the id exists. An app cannot make a service of its own: only the person
and their agent do, and the keys never leave the machine's safe.

## 2. Ask, then read

```js
const hl = await harnessly.connect({ protocol: 2 });
const tracker = (await hl.connections.list()).find((c) => c.id === 'yandex-tracker');
```

`list()` says where each named connection stands, so the view can say it in words before any call:

| `status` | Means | What the view offers |
|---|---|---|
| `ready` | connected, signed in, agreed to | read it |
| `needs_consent` | the person has not agreed to it for this app | a button: `hl.connections.request(id)` opens the host's card |
| `not_connected` | the person has no such connection (a group with none has `id: ''`) | «Подключите в Настройки → Подключения» |
| `needs_signin` | it waits for a sign-in or new keys | the same page |
| `off` | the person turned it off | the same page |
| `unavailable` | the service does not answer now | try later |

```js
if (tracker.status === 'needs_consent') {
  const { granted } = await hl.connections.request('yandex-tracker'); // from a click, not on load
}
const issues = await hl.connections.api('yandex-tracker', {
  method: 'POST', path: '/issues/_search', query: { perPage: 10 }, body: { query: 'Assignee: me() Resolution: empty()' },
}); // { status, ok, contentType, body } with a JSON body parsed

const tools = await hl.connections.tools('linear'); // an MCP connection: [{ name, description, inputSchema, readOnly }]
const out = await hl.connections.call('linear', 'list_issues', { assignee: 'me' }); // the raw MCP result
hl.on('connections', refresh); // an agreement, a sign-in, a turn on or off, anywhere
```

`conn` is the catalog id or the `slug` that `list()` gives. A call that the connection cannot take now fails with a
`HarnesslyError` (or, with `protocol: 2`, answers a refusal):

| Code | When | Do |
|---|---|---|
| `auth_required` | not connected, off, or waiting for a sign-in; `error.signIn.connection` names it | say so, point to Settings → Connections |
| `auth_expired` | the sign-in ran out | the same |
| `permission_denied` | the manifest does not name it, the person has not agreed, a change with read access only, or the person said no | for the agreement, `request` |
| `rate_limited` | more than 20 calls in 10 s to one connection | wait |
| `quota_exceeded` | an answer over 512 KiB | ask for less (a filter, a page size) |
| `upstream_error` | the service answered with an error or not at all | try later |

## 3. What is read, what needs a yes

With `access: read`, a view can call a tool the server marks `readOnlyHint`, make a GET (or HEAD), and use the searches the
catalog lists as reads of an API (Yandex Tracker's `POST /issues/_search`). `tools()` says which tools are `readOnly`; a
tool with no mark is taken as one that may change something.

A change needs `access: all` in the manifest, the right `connections.act`, the person's agreement at that level, and their
yes **to that very call**: the host shows a dialog with the connection, the tool (or `METHOD path`) and the arguments, and
only «ОК» sends it. Your code just calls `hl.connections.call(...)` and waits; a «no» is `permission_denied`. This is the
same promise as `harnessly connector act` makes the agent: nothing leaves in the person's name that they did not see.

## 4. What the machine guarantees

- **The key is not yours.** `list()` has no address, header or key; an answer and an error are scrubbed of the
  connection's keys (`[key]`); a view never signs in.
- **Per connection, per app.** The person agrees to each connection for each app and can take it back in the app's
  «О приложении» → «Настройка». An update does not widen it.
- **Nothing hidden.** Each call is in the app's journal: which connection, the tool or `METHOD path`, never the arguments.
- **Data, not instructions.** What a service returns is text from outside. Show it as text; do not send it to the agent as
  if the person wrote it.

The full contract (the route, the limits, the model of the agreement) is in [Apps](../apps.md), «The person's connections».
A working app: [`apps/tracker-side`](../apps/tracker-side).

## What does not exist

- A card made by `harnessly widget create` (the agent's own widget) has no manifest, so it cannot name a connection: make an
  app with a `side` view, as [`apps/tracker-side`](../apps/tracker-side) does.
- A tool of a server that carries no `readOnlyHint` is a change, even if it only reads: use `access: all`, or an API
  connection for the read.
- Subscriptions (a push when a mail arrives): ask again on `hl.on('connections')`, on `visibility`, or on a timer.
