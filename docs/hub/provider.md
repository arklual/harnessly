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
