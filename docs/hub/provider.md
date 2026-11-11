# The machine provider

The hub does not talk to any cloud directly. It speaks one small HTTP protocol to a **provider service** that you run
next to it and that knows your cloud. The service decides what a machine's «plan» is; the hub only asks. Without a
provider (`HUB_PROVIDER=none`) the hub still shows what machines report of themselves, and every action on a server
answers 422 `not_supported`.

Set `HUB_PROVIDER_URL=https://provider.internal.example.org` (the base address, without a trailing path) and, if the
service wants a token, `HUB_PROVIDER_TOKEN_FILE=/run/secrets/provider-token`. The hub reads the token file again when
it changes on disk, so a rotated token needs no restart. If the file is named and cannot be read at start, the hub does
not start.

Everything is JSON. Every request carries `Authorization: Bearer <token>` (when there is a token) and
`Accept: application/json`.

## Naming a machine

`{ref}` in the paths is the provider's own id of the server (`external_id` of the machine, when the hub has one) and the
hub's machine id otherwise. It is URL-escaped.

## Calls

| Call | Request | Answer |
| --- | --- | --- |
| `GET /machines/{ref}/plans` | | `{ "plans": [ { "id", "name", "cpu", "ramGB", "diskGB", "price"?: Price, "current"?: true } ] }` |
| `POST /machines/{ref}/quote` | `{ "cpu", "ramGB", "diskGB" }` | `{ "price": Price }` |
| `POST /machines/{ref}/resize` | `{ "cpu", "ramGB", "diskGB" }` | `{ "opId": "…" }` |
| `POST /machines/{ref}/reboot` | `{}` | `{ "opId": "…" }` |
| `POST /machines/{ref}/power-off` | `{}` | `{ "opId": "…" }` |
| `POST /machines/{ref}/power-on` | `{}` | `{ "opId": "…" }` |
| `GET /ops/{opId}` | | `{ "state": "pending"\|"running"\|"done"\|"failed", "error"?: "…" }` |
| `GET /machines/{ref}/console` | | `{ "url": "https://…" }` |

`Price` is `{ "amount": 1900000, "currency": "RUB", "period": "month" }`: `amount` in minor units of the currency;
`period` is `month` when left out. Mark the plan the server is on with `"current": true`; the hub learns the current size
from it. A price you do not know is left out, not zero.

The calls that change something answer as soon as the provider took the job and give `opId`; the hub then asks
`GET /ops/{opId}` until the state is `done` or `failed` (a state not in the list is an error).

## Errors

A status outside 2xx is an error. Send `{ "error": "<code>", "message": "<text>" }` when you can.

| Answer | Meaning for the hub |
| --- | --- |
| 501, or `error: "not_supported"` | this call is not available: the hub says 422 `not_supported` |
| 404 | the provider does not know the machine: 404 `provider_not_found` |
| other 4xx | not retried: 502 `provider_error`, with `details.providerCode` and `details.providerMessage` |
| 5xx, a broken connection | retried (below), then 502 `provider_error` |
| no answer in time | 504 `provider_timeout` |

A console address must be an `http(s)` link, else the answer is rejected.

## Retries and idempotency

One attempt waits at most 15 seconds. A 5xx and a broken connection are tried 3 times in all, with a wait of 300 ms
doubling each time. A call that changes something (`resize`, `reboot`, `power-off`, `power-on`) carries an
`Idempotency-Key` header, **the same on every attempt of one request**; the service must answer a repeat with the same
`opId` and not do the work twice. The key is also the hub's operation id, so a request resent after a restart of the hub
is recognized too.

## The in-memory provider

`provider.Memory` implements the same interface from a table, for tests and for trying the screens: plans and sizes are
set by the test, a job finishes as the test says, and the new size is recorded at the moment of the call.

# Protocol v1: the machine adapter

The protocol above is **v0**. It stays as it is and is still the default. Protocol **v1** adds what v0 lacks: the
adapter describes itself, makes a server, takes over an existing one and reports its state. An adapter is a separate
process next to an unmodified Hub; it is the way to put a cloud behind the Hub without changing the Hub.

## Choosing the protocol

`HUB_PROVIDER_PROTOCOL` (with `HUB_PROVIDER=http`):

| Value | Behaviour |
| --- | --- |
| `v0` (default) | exactly as before: no `describe`, only the calls of v0 |
| `auto` | the Hub asks `GET /v1/describe`; an answer with `protocol: "harnessly.machine-provider/v1"` means v1, «unknown call» (404, 405, 501) means v0 |
| `v1` | the adapter must speak v1; a v0 service is refused |

The choice is made on the first call, not at the start of the Hub (an adapter that is down while the Hub starts does not
stop it), and is made again every 10 minutes, so a new version of the adapter is noticed without a restart.

## Calls

All v1 paths start with `/v1`, carry the same bearer token, retries and `Idempotency-Key` rules as v0, and answer errors
as in the table above.

| Call | Request | Answer |
| --- | --- | --- |
| `GET /v1/describe` | | `{ "protocol", "name", "version", "capabilities": [...], "config_schema"?: JSON Schema }` |
| `POST /v1/machines` | `{ "name", "config"?: {...}, "bootstrap"?: { "hubUrl", "code" } }` | 202 `{ "op", "machine"? }` |
| `POST /v1/machines/{id}/attach` | `{ "hubMachine", "name"? }` | state |
| `GET /v1/machines/{id}` | | `{ "id", "status", "cpu"?, "ramGB"?, "diskGB"?, "plan"?, "price"? }` |
| `POST /v1/machines/{id}/power` | `{ "action": "on"\|"off"\|"reboot" }` | `{ "op" }` |
| `POST /v1/machines/{id}/resize` | `{ "cpu", "ramGB", "diskGB" }` | `{ "op" }` |
| `GET /v1/machines/{id}/plans`, `POST /v1/machines/{id}/quote` | as in v0 | as in v0 |
| `GET /v1/machines/{id}/console` | | `{ "url" }` |
| `GET /v1/ops/{op}` | | `{ "state": "pending"\|"running"\|"done"\|"failed", "error"?, "machine"? }` |

`capabilities` lists what the adapter can do: `create`, `attach`, `state`, `power`, `resize`, `console`, `plans`. A call
whose capability is not listed is not sent: the Hub answers 422 `not_supported`. Without `plans` the resources screen
shows the one plan the machine is on, taken from its state. `config_schema` describes the `config` of create; the Hub
passes `config` on as it came and does not read it. `bootstrap` is what the new server's agent needs to register with the
Hub; the adapter starts the agent with it, the Hub never reaches into the machine.

## What the Hub does with it (behind the switch)

With `HUB_PROVIDER_PROTOCOL` other than `v0` the Hub mounts, for the owner of a space:

- `POST /api/spaces/{spaceId}/machines/provision` `{ name, config }` → 202 `{ op, machine }`: issues a registration code
  and calls create with it; `GET …/provision/{opId}` follows the operation;
- `POST /api/machines/{machineId}/provider/attach` `{ externalId }`: calls attach and records `externalId` as the
  machine's id at the provider;
- `GET /api/machines/{machineId}/provider`, `POST …/provider/power` `{ action }`, `GET …/provider/ops/{opId}`.

The Hub records the server it asked for with the space that asked (`provider_servers`) and the operations it started
(`provider_ops`). A machine is attached only to a server of its own space (anything else answers 404
`server_not_found`, and the adapter is not called), a server to one machine (409 `server_attached`), and an operation is
read only by the space that started it, or through the machine it was started on. A server the Hub did not make is taken
over by an administrator of the installation: their attach records it for the space of the machine.

On v0 these answer 404 as before.

## The example adapter and the update scenario

`examples/machine-adapter` is a stand-in cloud, a separate process that speaks v1 and v0 on one port and can keep its
servers in a file (`-state`). `tools/ci/adapter-upgrade.sh` (CI job `adapter-upgrade`, test
`internal/hub/upgradecheck`) builds the Hub at the base of the merge request (N) and the tree under test (N+1), starts
the adapter, creates and attaches a machine through N, resizes and restarts it, replaces N with N+1 on the same
database, and checks that the machine is in place, N+1 chose v1 by describe, and the operations still go. Neither the
Hub nor the adapter is edited in between.

## What this showed (limits)

- An adapter that speaks only v1 does not work with a Hub released before v1: such a Hub knows no `describe`. To outlive
  an update the adapter keeps v0 until the last old Hub is gone, as the example does.
- v1 is used only after `HUB_PROVIDER_PROTOCOL=auto` (or `v1`) is set: the update alone does not switch an install.
- The resources screens still need an organization with the resources feature (`HUB_ORGS`, `HUB_MACHINE_RESOURCES=on`)
  and the machine marked `managed`.
- The Hub does not yet link a registered machine to its server by the registration code: attach is explicit.
- A Hub released before v1 has no create call, so create in the scenario is made at the adapter directly.
