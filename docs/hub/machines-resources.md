# Machine resources, load and operations

What the hub knows of the server a machine runs on, how loaded it is, and what an administrator can do to it from the
hub: change its size, restart the runner or the whole machine, and keep a list of who may open it. All of it ships dark:
nothing below changes while the organization's feature `machine.resources` is off.

The wire to a cloud provider is in [provider.md](provider.md), the list of machines in [analytics.md](analytics.md), the
agent's side in [machine.md](machine.md).

## Switches

| Switch | Default | What it does |
| --- | --- | --- |
| organization feature `machine.resources` | off | The gate of everything on this page. Off: `…/resources` and the other endpoints below answer 404 `not_enabled`, the rows of the machines list carry `null` in `cloud`, `hardware`, `load`, `week`, `managed`, `activeOps` is `[]`, `restart` keeps the old behaviour (see below), and the hub stores no load samples |
| `HUB_MACHINE_RESOURCES` | `off` | `on`: the feature is on for every organization, whatever its flags say (a self-hosted hub of one team). The same as `HUB_FEATURES_FORCE=machine.resources` |
| `HUB_PROVIDER` | `none`, or `http` when `HUB_PROVIDER_URL` is set | Which provider answers for the servers. `none`: the hub only shows what the machines said of themselves |
| `HUB_PROVIDER_URL`, `HUB_PROVIDER_TOKEN_FILE` | | Address and bearer token file of the provider service ([provider.md](provider.md)) |
| `HUB_ACCESS_ENFORCE` | `off` | `off`, `warn` or `on`: whether a machine's access list is kept ([Access](#access)) |

The gate is the organizations' flags (`features.Store`: `HUB_FEATURES_FORCE`, the organization's own choice,
`HUB_FEATURES_DEFAULT`), asked with the key of the organization (`store.OrgKey`), or'ed with `HUB_MACHINE_RESOURCES`;
`api.Options.Features` replaces it in tests. An answer is kept for 30 seconds on the busy paths.

## Resources

`GET /api/machines/{id}/resources` (view of the tenant, that is an administrator of it) answers what the hub holds:

```json
{
  "machineId": "m_1",
  "managed": true,
  "provider": { "name": "http", "supported": true },
  "providerError": null,
  "current": { "cpu": 4, "ramGB": 16, "diskGB": 160 },
  "price": { "amount": 1900000, "currency": "RUB", "period": "month" },
  "plans": [
    { "id": "p8", "name": "8 / 32", "cpu": 8, "ramGB": 32, "diskGB": 320, "price": { "amount": 3600000, "currency": "RUB", "period": "month" }, "current": false }
  ],
  "hardware": { "cores": 4, "ramBytes": 17179869184, "diskBytes": 171798691840, "model": "Example CPU", "laptop": false, "os": "linux", "arch": "amd64", "updatedAt": "2026-11-02T10:00:00Z" },
  "consoleUrl": "https://console.example.org/m_1",
  "activeOps": [],
  "updatedAt": "2026-11-02T10:00:00Z"
}
```

- `amount` is in minor units of `currency` (kopecks, cents). A price the provider did not give is `null`: the hub never
  makes one up, and the screen shows a dash.
- Under `HUB_PROVIDER=none` the answer is 200 with `provider.supported: false`; `current`, `price`, `consoleUrl` are
  `null`, `plans` is `[]`, and `hardware` is what the agent reported.
- A provider that is there but did not answer gives 200 with `providerError` set (a short code) and the numbers the hub
  already had; the page stays usable.
- `hardware` is what the agent said of itself (`hardware` in the `hello` message, [machine.md](machine.md));
  `null` for a machine that said nothing. Its numbers stay `null` when unknown, never zero.

`POST /api/machines/{id}/resources/quote` (manager) `{ cpu, ramGB, diskGB }` asks the provider for the price of a size
without doing anything:

```json
{ "machineId": "m_1", "want": { "cpu": 8, "ramGB": 32, "diskGB": 320 },
  "price": { "amount": 3600000, "currency": "RUB", "period": "month" },
  "current": { "amount": 1900000, "currency": "RUB", "period": "month" },
  "delta": { "amount": 1700000, "currency": "RUB", "period": "month" } }
```

`delta` is set only when both prices are in one currency.

`POST /api/machines/{id}/resize` (manager) `{ cpu, ramGB, diskGB }` starts the change and answers 202 `{ op: Op }`. The
size must be a sensible one (`cpu` 1..1024, `ramGB` 1..16384, `diskGB` 1..1048576, else 400 `invalid_request` with
`details.field`). The disk can only grow.

| Status, code | When |
| --- | --- |
| 404 `not_enabled` | the feature is off for the organization |
| 422 `not_supported` | `HUB_PROVIDER=none`, or the provider does not do it |
| 409 `not_managed` | the machine is not managed for `resources` ([Managed](#managed-capabilities)) |
| 409 `current_unknown` | the hub does not know the size the machine is on |
| 409 `no_change` | the size asked is the size it is |
| 422 `disk_shrink` | `diskGB` is below the current one (`details.current`, `details.wanted`) |
| 409 `op_busy` | an operation is already running on the machine |
| 404 `provider_not_found`, 504 `provider_timeout`, 502 `provider_error` | what the provider answered |

## Operations

An operation is one thing done to a machine, stored in `machine_ops` (migration `0013`). **One unfinished operation per
machine**: a second request gets 409 `op_busy`.

```json
{ "id": "op_1", "machineId": "m_1", "kind": "resize", "status": "running",
  "params": { "cpu": 8, "ramGB": 32, "diskGB": 320 }, "error": null, "createdBy": "u_1",
  "createdAt": "2026-11-02T10:00:00Z", "updatedAt": "2026-11-02T10:00:05Z", "finishedAt": null, "chatsNow": null }
```

- `kind`: `resize`, `reboot`, `power_off`, `power_on` (the provider), `restart_runner`, `restart_machine` and `drain` (the
  agent, the last one together with the provider).
- `status`: `pending`, `running`, `done`, `failed`, `cancelled`. The first two are unfinished.
- `chatsNow`: for a restart that waits for the chats, how many are still running; `null` otherwise.
- `error` is a short text, at most 500 characters.

`GET /api/machines/{id}/ops` (view) lists the last 50, newest first, `?active=1` only the unfinished:
`{ machineId, ops: Op[] }`.
`POST /api/machines/{id}/ops/{opId}/cancel` (manager) cancels an operation that waits for chats (`restart_*` with
`when=after_chats` or `drain`); 200 `{ op }`, 409 `not_cancellable` for anything else, 409 `op_finished` when it is over,
422 `drain_unsupported` when the agent is too old to be asked.

### Move to another tenant

`POST /api/machines/{id}/move` `{ tenantId, teamId?, dry_run? }` (manage on both tenants). The row stays (id, key,
`created_at`); `space_id` changes, the team is reset (or set to `teamId` of the target) and `machine_access` is cleared.
`dry_run` answers `{ plan }` with who loses access and `limits` (`fromRules`, `toRules`: the limit rules of each tenant
and its teams; new spend counts against the target's). The number of machines never blocks a move. Moving to the same tenant is a no-op. See [orgs.md](orgs.md).

### Restart

`POST /api/machines/{id}/restart` `{ what, when }` (manager; the endpoint is the organization's, so 404 with `HUB_ORGS`
off):

| `what` | Meaning |
| --- | --- |
| `runner` | restart the runtime on the machine: the agent operation `runtime.restart` |
| `machine` (also `vm`) | restart the server: through the provider (`reboot`), or, when `when` is not `now`, first wait for the chats |

| `when` | Meaning |
| --- | --- |
| `now` (default) | at once |
| `after_chats` | wait until no chat runs, then restart |
| `drain` | stop taking new chats, wait for the running ones, then restart |

Answers 202 `{ jobId, op }`; `jobId` is the id of the operation. 423 `machine_disabled` for a machine switched off, 422
`drain_unsupported` for an agent without the `drain` feature. **With the feature off** the old contract holds: `what` is
`runner` or `vm`, `when` is `now` or `after_chats`, `runner` is `runtime.restart` and `vm` answers 501
`not_implemented`.

While a machine drains, the agent refuses the routes that start a chat on it (its route filter, the machine's side);
running chats go on and a viewer can still look at them. Only chat-starting routes are refused, and a chat started
straight on the machine is not stopped. `drain` needs an agent that announced the `drain` feature.

Limits worth knowing:

- A `restart_machine` that waits for chats is written off (`failed`) if the hub restarts in the meantime; it is not
  resumed.
- A link lost during a drain is only cleaned up by the stale reaper: a restart that waits more than 24 hours, or a call
  that never reached the provider for 5 minutes, is failed.
- The hub asks a provider about an operation until it finishes, for at most 30 minutes or 8 failed answers in a row.
- Counting chats is best effort: the number comes from the runtime when it answers and from the hub's own sessions
  otherwise.

## Subscription windows

An agent with a runtime next door announces the feature `limits` in `hello`. The hub, with the resources side wired,
subscribes it to the event `limits.windows` (`link.subscribe`), and the agent sends
`{ windows: { providers: { claude|codex: { fiveHour?: { usedPct, resetsAt? }, sevenDay?: { … } } }, at } }` every
60 seconds: `usedPct` is 0..100, `resetsAt` is Unix seconds, and a window the machine has no number for is left out. The
numbers are the runtime's own (`GET /machines`, the provider limits of this machine).

The hub keeps only the last reading of each window per machine (table `machine_provider_limits`, written only where the
organization has the feature on, deleted with the machine's space). Efficiency reads it: the person's `limit.weekUsedPct`
and `limit.resetsAt` on the personal screen are the fullest `seven_day` window of Claude among the machines the person
made, from a reading no older than two hours; with none, both are `null`.

## Load

The agent sends `metrics.sample` events (CPU, memory and disk percentages and the number of chats; any may be missing)
when it announced the `metrics` feature. The hub keeps the last reading for the list (`load`, a reading older than 3 minutes is dropped) and five-minute buckets (`machine_metrics`) for the chart:

`GET /api/machines/{id}/metrics?range=1h|6h|24h|7d` (view; `24h` by default) answers

```json
{ "machineId": "m_1", "range": "24h", "stepSeconds": 300, "from": "…", "to": "…",
  "points": [ { "at": "…", "cpuAvg": 12.4, "cpuMax": 40.1, "ramAvg": 55, "ramMax": 61.2, "diskAvg": 48, "diskMax": 48, "chatsMax": 3, "samples": 12 } ] }
```

Buckets are kept for 7 days. Samples are stored only when the gate is wired and the organization's feature is on. A bucket holds the percentages of
the samples it got; a percentage nobody sent is not counted as zero.

## Managed capabilities

`managed` says which parts of a machine the hub controls, as opposed to the machine's own settings:
`{ budget, env, resources, access }`, each a boolean (default `false`). `PUT /api/machines/{id}/managed`
`{ capability: 'budget'|'env'|'resources'|'access', managed: boolean }` (manager) sets one and answers
`{ machineId, managed }`; journal entry `machine.managed`. `resize` needs `resources` to be managed (409 `not_managed`).

## Access

A machine's access list is `access: [{ kind: 'user' | 'team', id }]` (set by `PATCH /api/machines/{id}`, which exists with
`HUB_ORGS=on`). **An empty list means the whole tenant.** `HUB_ACCESS_ENFORCE` says what the list does:

| Value | Behaviour |
| --- | --- |
| `off` (default) | stored and shown, not enforced: opening and the proxy follow the tenant's membership |
| `warn` | enforced as `on` would, but only logged (`machine access would be refused`); nobody is refused |
| `on` | a person not allowed gets 403 `machine_access` from `…/open` and from every request through the proxy |

Who passes: an instance administrator, a manager of the tenant (owner or administrator grant), the person who connected
the machine, anyone on an empty list, a person listed by `user`, and a member of a listed `team`.

**The owner's rule** (rollout switch `own-work`, keyed by the person who opens and the machine; see
[Own work](own-work.md)). Opening a machine is reading its owner's work, so with the switch on for a person only the
person who connected the machine and the people and teams it lists let them in: an administrator's or a manager's role
and an empty list no longer do. The role still *sees* and manages the machine (lists, detail, live events, operations);
the refusal of opening is 403 `own_work` when a role or an empty list would have let the person in before, and
`machine_access` otherwise. Peer messages ([Peers](peers.md)) follow the rule without the switch: the receiving runtime
asks its owner itself.

With `on` the list also decides who *sees* the machine: a person it does not let in gets the machine in no list (the
tenant's machines, the organization's inventory, the fleet), `GET /api/machines/{id}` answers 404 as for a machine that
does not exist, and the live events of the machine (`machine`, `machine.op`, `job`, `login`) are checked per person at
delivery, so an open stream stops getting them as soon as the person is taken off the list. `off` and `warn` show every
machine to every member, as before.

Things to know:

- It applies to members of the tenant. Someone outside the tenant is refused by the membership check before this one (an
  instance administrator who is not in the tenant gets 403 `forbidden`).
- The proxy keeps an answer for 5 seconds per person and machine, so a person taken off the list is refused from the
  next request after that. **A stream that is already open is not cut**: it ends when its owner closes it.

## Live events

Events of the organization's stream (`machine.op` is new): `machine` (the row changed, as before), `machineRemoved`,
`members`, and

```json
{ "event": "machine.op", "machineId": "m_1", "op": { "…": "Op" } }
```

at every change of an operation. A kind `machine.status` is declared for a later step and not sent.

## Tests

`Service.SetPollEvery(d)` sets the pause between two asks of the provider about an operation; it is for tests only. The
`provider.Memory` provider (`internal/hub/provider`) answers from a table and records the size at the moment of the
call; with it a test runs a resize from request to `done` without a network.
