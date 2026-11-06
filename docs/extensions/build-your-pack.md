# Build your own extension pack

A company that runs harnessly usually has its own machines, its own place for the agent's memory, its own tracker, its
own secret store and its own look. None of it needs a fork. harnessly has a few interfaces; a company builds an
**extension pack**, a set of small services and one interface package that speak them, and installs it next to
unmodified harnessly. This page is the way from nothing to a pack that passes the acceptance scenario. The names here
are invented: `example-corp`, `example.com`. A working copy is [examples/ext-pack-example](../../examples/ext-pack-example).

## What a pack can hold

| Part             | Interface                                  | Where it runs             | Reference                             |
| ---------------- | ------------------------------------------ | ------------------------- | ------------------------------------- |
| Machines         | `harnessly.machine-provider/v1`            | next to the hub           | [hub/provider.md](../hub/provider.md) |
| Memory store     | `harnessly.memory/v1`                      | next to the runtime       | memory-adapters.md                    |
| Tracker and wiki | `harnessly.connection/v1`                  | next to the runtime       | connection-adapters.md                |
| Secrets          | vault mode of the safe, configuration only | the runtime's environment | [safe.md](../safe.md)                 |
| Interface        | an app of kind `ui` filling slots          | the runtime               | [apps.md](../apps.md)                 |

A pack takes any of them: a company with one tracker builds one adapter. The memory and tracker references ship with
the changes that add those interfaces; the extension frame itself is [hub/extensions.md](../hub/extensions.md).

There is no interface for documents yet. A company that keeps documents elsewhere has the tracker and wiki adapter,
which reads pages, and nothing more.

## The shape of an adapter

An adapter is an HTTP service you run. Whatever it does for the hub or the runtime, it follows the same frame:

- `GET /v1/describe` answers `{"protocol": "harnessly.<kind>/v1", "name": "...", "version": "...", "capabilities": [...]}`.
  The hub or the runtime asks first and refuses an adapter that speaks another major version.
- Every call carries `Authorization: Bearer <token>`. The token is a file (`tokenFile`), read again when it changes, so a
  rotation needs no restart. Without a valid token the adapter answers 401.
- A route the adapter does not do answers `501` with `{"error": "not_supported"}`: say what you cannot do, do not guess.
- A call that changes something carries an `Idempotency-Key`. A repeat of the same key must give the same result and
  the header `Idempotent-Replayed: true`, because the caller retries after a timeout.
- Errors are `{"error": "<code>", "message": "..."}` with the right status; a `409` is a conflict the caller can resolve.

The pack example keeps this in one file, [adapters/lib.ts](../../examples/ext-pack-example/adapters/lib.ts) (under 100
lines); an adapter is that frame plus its routes. Use it, or the Go package `extsdk`, or write the frame in your own
language: it is plain HTTP and JSON.

## Step by step

1. **Pick the first part.** Machines are the usual start, because the hub is useful once it can make a server.
   Copy `examples/machine-adapter`, replace its in-memory "cloud" with calls to your provider's API, keep the routes.
2. **Write the others** from the same frame. The memory store is four calls (`list`, `get`, `put`, `search`); `put` takes
   `ifVersion` and answers `409` when the note changed. The tracker adapter answers `detect` (is this link ours), `link`
   (turn it into a connection), and then reads: `me`, `projects`, issues and pages.
3. **Describe the pack** in `extensions.json`, one entry for each adapter that the hub or the runtime asks:

   ```json
   [
     {
       "name": "example-corp-memory",
       "kind": "memory",
       "url": "http://127.0.0.1:18081",
       "tokenFile": "/opt/example-corp-pack/secrets/memory.token",
       "protocol": "harnessly.memory/v1"
     },
     {
       "name": "example-tracker",
       "kind": "connection",
       "url": "http://127.0.0.1:18082",
       "tokenFile": "/opt/example-corp-pack/secrets/connection.token",
       "protocol": "harnessly.connection/v1"
     }
   ]
   ```

   The hub and each loader of the runtime read the same file and skip entries of other protocols. An entry may carry
   `branding` (name, accent colour, support link) and `ui`.

4. **Point the products at it.** The hub: `HUB_PROVIDER=http`, `HUB_PROVIDER_URL`, `HUB_PROVIDER_TOKEN_FILE`,
   `HUB_PROVIDER_PROTOCOL=auto`, `HUB_EXTENSIONS=on`, `HUB_EXTENSIONS_FILE`. The runtime: `RUNTIME_CONNECTION_ADAPTERS_FILE`,
   `RUNTIME_MEMORY_ADAPTERS_FILE`, and for the safe `RUNTIME_SECRETS_*`. The example installer writes these into
   `hub.env` and `runtime.env`; an operator copies them into the service units. Neither product is patched.
5. **Make the interface package.** An app of kind `ui` fills the slots `settings`, `onboarding`, `object-action`,
   `hub-settings` and `branding` with sandboxed views. It has no agent, no chat and no backend; its settings and
   secrets are kept by harnessly, and it receives a secret only by the `secrets:<name>` right in its manifest.
   `harnessly app validate` checks the package, `harnessly app install` installs it, `disable` switches it off and
   `remove` takes it away (with its data, or without). When the data layout changes, add a `data.migrations` step.
   It runs under the flag `HARNESSLY_UI_SLOTS=on`.
6. **Put a script around it.** One command that installs and one that starts the pack is the line between a demo and
   something a colleague can use. `ext-pack.sh` of the example is about 170 lines of shell; it keeps the previous
   install for `rollback` and leaves tokens and data alone.

## Check that it works

Look at what the hub says about your adapters, before and after a restart:

```sh
hub doctor                       # OK / PROTOCOL_MISMATCH / UNREACHABLE for each adapter
curl -s $HUB/api/capabilities    # "extension.<name>": "on" for each healthy adapter
```

Then the same scenario the release is accepted by (below), against your pack.

## The acceptance scenario

`internal/hub/acceptance` is the scenario a release must pass, and `tools/ci/acceptance-pack.sh` runs it on every
release tag in CI (`acceptance-pack`). It does, with two builds of the hub, N (the previous release) and N+1 (this one):

1. **Clean install:** the pack is installed by its own script and started, then build N of the hub is started on an
   empty database, pointed at the pack.
2. **Sign-in and organization:** a person signs in by an email code and creates an organization and a space.
3. **Machine and agent:** the pack's machine adapter makes a server, the agent on it registers with the code the hub
   gave, the adapter attaches the server to the hub's machine, and a resize goes through. A note is written to the
   pack's memory store.
4. **Update:** the pack is installed over itself (the next version) and the hub is replaced by build N+1 on the same
   database. The machine, the organization and the note are there, the operations go on, and the pack's adapters are `on`
   in `/api/capabilities`.
5. **Rollback:** the pack's previous install comes back and build N starts on the same database. The machine and the
   note are there and the operations still go through. This is why a migration may only add: the old build must read
   the database the new one has touched.

To accept your own pack the same way, replace the script and the adapters in the scenario with yours and keep the
steps. A release of your pack is ready when the five steps pass and `hub doctor` is clean after each.

## Keep it public-safe

A pack for others to copy uses `example.com` addresses and invented names in code, tests and docs. Keep company names,
real domains and real tokens in your own repository, in your secret store, never in the example.
