# Hub extensions

Corporate things (credential, document, connection and memory providers) plug into the hub as **extension packages**
without a fork: each is a separate process, an *adapter*, that the hub calls over HTTP. Updating the hub is installing a
new core version; the adapters stay as they are. The machine provider protocol ([provider.md](provider.md)) is the first
user of this frame and keeps its own calls on top of it.

Everything here is behind a flag. With `HUB_EXTENSIONS` off (the default) nothing is read, called or shown.

## Registering adapters

```
HUB_EXTENSIONS=on
HUB_EXTENSIONS_FILE=/etc/harnessly/extensions.json
```

The file is a JSON array:

```json
[
  {"name": "acme-vault", "kind": "credentials", "url": "http://vault-adapter.internal:8080",
   "token_file": "/run/secrets/acme-vault-token", "protocol": "harnessly.credentials/v1", "timeout_seconds": 15}
]
```

| Field | Meaning |
|---|---|
| `name` | unique, `[a-z0-9-]`, up to 64 characters; shown as `extension.<name>` |
| `kind` | what the adapter provides (`credentials`, `documents`, `connections`, `memory`, …) |
| `url` | base address of the adapter |
| `token_file` | file with the bearer token; read again when it changes, so a rotation needs no restart |
| `protocol` | the protocol the hub expects, `harnessly.<kind>/v<major>` |
| `timeout_seconds` | per attempt; default 15 |

A bad file stops the hub at start with the reason, instead of running half-configured.

## Views and branding in the Hub

With `HARNESSLY_UI_SLOTS=on` an entry may also declare a page of the Hub's settings and the Hub's name and accent:

```json
{"name": "example-corp", "kind": "connections", "url": "http://adapter.internal:8080",
 "token_file": "/run/secrets/example-corp-token", "protocol": "harnessly.connections/v1",
 "ui": [{"id": "main", "slot": "hub-settings", "label": "Example Corp", "url": "https://ext.example.com/hub/settings"}],
 "branding": {"name": "Example Corp Agents", "accent": "#2f6fed", "supportUrl": "https://support.example.com"}}
```

| Field | Meaning |
|---|---|
| `ui[].id`, `ui[].slot` | unique id; the only slot is `hub-settings` |
| `ui[].label`, `ui[].icon` | the heading of the page, 1–60 characters |
| `ui[].url` | `https` address without credentials; the Hub frames it in a sandbox (`allow-scripts`, no `allow-same-origin`) and adds its origin to `frame-src` of its page |
| `branding.name`, `accent`, `supportUrl` | the page title, an accent as `#rrggbb`, an `https` or `mailto` address |

The Hub serves the result at `GET /api/ext/ui` (404 while the flag is off) and shows the views under «Расширения» for an
administrator or lead. A view that does not answer within eight seconds, or throws, is replaced by a row with a retry.

## The frame

- **Handshake.** `GET /v1/describe` answers `{protocol, name, version, capabilities, config_schema}`. A protocol other
  than the expected one is a `protocol_mismatch`; the hub never guesses.
- **Timeouts and retries.** Each attempt has its timeout; a 5xx or a broken connection is retried up to 3 times with a
  doubling pause (300 ms to start). A 4xx is the adapter's answer and is not retried.
- **Idempotency.** Every call that changes something carries `Idempotency-Key`, the same on every attempt. The adapter
  answers a repeat with the first result and does not do the work twice (`Idempotent-Replayed: true`).
- **Circuit breaker.** After 5 failures in a row the hub stops calling the adapter for 30 s, then lets one probe
  through. A 4xx, «not supported» and «not found» are not failures.
- **Health.** The hub asks describe every 30 s: `ok`, `protocol_mismatch`, `unreachable` or `pending` (not yet asked).

## Seeing the state

`hub doctor` prints the configuration check and one line per adapter, and exits 1 if any is not `ok` (2 if the
configuration cannot be read):

```
configuration  ok
extensions     1 adapter
OK       acme-vault [credentials] http://vault-adapter.internal:8080 acme 2.0.0 circuit=closed
```

`GET /api/capabilities` lists each adapter as `extension.<name>`: `on` when healthy, `needs_config` otherwise. The
endpoint is open, so it carries a fixed reason per state and never the address or the raw error; `hub doctor` says more.

## Writing an adapter

- Go: the `extsdk` package is a skeleton with describe, the token check and the idempotency replay; add your calls with
  `Handle`.
- TypeScript: [`examples/ext-adapter-ts`](../../examples/ext-adapter-ts) is the same with no dependencies.

The error body is `{"error": "<code>", "message": "<text>"}`; 501 or `not_supported` means the call is not available.

See also [capabilities](../capabilities.md).
