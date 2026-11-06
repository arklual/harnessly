# Connection adapters

Settings → Connections knows the products built into the runtime (Jira, YouTrack, GitLab, GitHub, …). A product that is
not built in, such as a corporate GitLab with its own sign-in or an in-house tracker, connects through an **adapter**:
a small HTTP service that speaks the connection protocol. No rebuild of the runtime is needed.

Without a declared adapter nothing changes: the built-in products work as before and the adapters are never asked.

## Declare an adapter

Set `RUNTIME_CONNECTION_ADAPTERS_FILE` to a JSON file, the same format as the hub's extensions file
([`internal/hub/ext`](../internal/hub/ext)):

```json
[
  {
    "name": "example-tracker",
    "kind": "connection",
    "url": "http://127.0.0.1:8080",
    "tokenFile": "/etc/harnessly/example-tracker.token",
    "protocol": "harnessly.connection/v1"
  }
]
```

- `tokenFile` holds the bearer token the runtime shows the **adapter**. It is a path, never a value.
- The file has no field for a connection's credentials. The person's token for the product is kept in the runtime's
  safe, as for every connection, and the runtime sends it in the body of each call. The adapter keeps nothing.
- Entries of other protocols are left to their own loaders. An adapter that is silent, speaks another protocol or names
  a kind a built-in product already has is told on stderr and skipped; the other products go on.

The kind of the connection is the adapter's `name` from `GET /v1/describe`. Built-in providers are tried first, so an
adapter never takes a link from one.

## Protocol `harnessly.connection/v1`

`GET /v1/describe` answers the extension frame's description: `protocol`, `name`, `version` and `capabilities`. The
capabilities are `detect` (required), `link`, `issues` and `pages`; a call whose capability is not listed is never sent.
Every other call is `POST` with a JSON body. `404` means "not this" or "no such thing", `501` or `not_supported` means
"I do not do this", `401`/`403` or `{"error":"denied"}` means the credentials are refused.

| Call | Body | Answer |
| --- | --- | --- |
| `/v1/detect` | `{url}` | the detection (`baseUrl`, `deployment`, `methods`, `capabilities`, …), or `404` when the link is not this product |
| `/v1/link` | `{conn, url}` | `{found, kind: "issue"\|"page", key}` |
| `/v1/me` | `{conn, credentials}` | the person: `{login, name, email}` |
| `/v1/projects` | `{conn, credentials}` | `{projects: [{key, name, url}]}` |
| `/v1/issues/search` | `{conn, credentials, query}` | `{issues: [issue]}`; `query` is `{text, mine, open, project, native, limit}` |
| `/v1/issues/get` | `{conn, credentials, key}` | one issue, with the description and the last comments |
| `/v1/pages/search` | `{conn, credentials, query}` | `{pages: [page]}` |
| `/v1/pages/get` | `{conn, credentials, id}` | one page, with the body |

`conn` is `{id, baseUrl, apiUrl, deployment, method, extra}`; `credentials` maps field ids (`token`, `email`, …) to the
values the person gave. The shapes of the issue, page, person and detection are the JSON of `Issue`, `Page`, `Person`
and `Detection` in [`internal/connections/model.go`](../internal/connections/model.go).

`/v1/link` is asked with a two-second wait and its answer is kept for a minute: the core wants a link read without a
slow network, so a slow adapter reads as "not a link".

## What the contract covers, and what not

The contract is `connections.Provider` with `IssueSource` and `PageSource`; the built-in products implement it as they
did, and the adapter is one more implementation. Merge requests (the `code` capability) have no source interface in the
core yet, so an adapter may list `code` in its detection and the connection keeps it, but the core asks nothing of it.

## Example

[`examples/connection-adapter-ts`](../examples/connection-adapter-ts) is a tracker on `tracker.example.com` in
dependency-free TypeScript (Node 22+). Run it and declare it:

```sh
ADAPTER_TOKEN=secret PORT=8080 node --experimental-strip-types examples/connection-adapter-ts/adapter.ts
RUNTIME_CONNECTION_ADAPTERS_FILE=examples/connection-adapter-ts/adapters.json harnessly runtime
```
