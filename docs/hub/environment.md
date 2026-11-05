# Environment and catalog

What the chats of an organization have at hand: **skills**, **MCP servers**, **repositories** and **documents**. The
catalog says what exists; the layers of the organization (organization, tenant, team) say who gets which item; a
machine receives the result for its team. Part of the hub contract: [api.md](api.md); organizations and roles:
[orgs.md](orgs.md); spend facts: [usage.md](usage.md). TS types and fixtures: `web/app/hub-api/env.ts`,
`web/app/hub-api/fixtures/env/`.

Everything here ships dark. Nothing changes for an organization until the flag is on (see [Flags](#flags)).

## Concepts

- **Catalog item** — a skill, an MCP server, a repository or a document: `id`, `kind` (`skill`, `mcp`, `repo`, `doc`),
  `name`, `hint`, `source` (JSON, see below). Items are the organization's own, or built-in (`builtIn`, no organization,
  every organization sees them, nobody changes them). A removed item is archived, not deleted: entries that used it stop
  putting it in, and its past calls stay.
- **Entry** — one item put into one layer: `entryId`, the layer (`org`, `tenant`, `team`) and its scope id, `enabled`,
  `pinned`, an audience. An item has at most one entry per layer.
- **Layer** — the organization (`scopeId` is the organization id), a tenant (the space id), a team (the team id).
- **Audience** — who an entry reaches inside its layer: `tenant` (everybody the layer reaches), `teams` (only people of
  the listed teams; ids of teams of the layer) or `off` (nobody, the entry is kept but takes no part). A team entry reaches
  the team only.
- **Pinned** — nobody in a lower layer may switch the item off, neither an admin of a tenant or team nor the person.
- **Resolver** — an item that several layers put in is **one** item, taken from the highest layer (organization beats
  tenant beats team); it is pinned when any layer pinned it. An entry that is `enabled: false` in a lower layer switches
  an item off for that layer's people unless a higher layer pinned it (then the switch is refused with `locked`).
- **Personal switch** — a person switches an unpinned item off for themselves in a tenant (`PUT /api/me/env/{itemId}`).
  It is theirs alone; see [What is not done](#what-is-not-done) for the limits.

### `source` by kind

A source holds the text or the address of the item and **nothing secret**. Whatever is not listed is refused (400
`invalid_request`, `details.field` is `source.<key>`).

| Kind | Keys |
| --- | --- |
| `skill` | `body` (the SKILL.md text, ≤ 48 KiB), `ref` (an http or https address) |
| `mcp` | `transport` (`http`, `sse`, `stdio`), `url` (http or https, no login in it), `command`, `args` (≤ 32 strings), `secretRefs` (≤ 16 names of secrets, `[A-Za-z_][A-Za-z0-9_]*`; the machine looks them up by name) |
| `repo` | `url` (https or ssh), `branch` |
| `doc` | `url` (http or https), `text` (≤ 48 KiB) |

The whole source is at most 64 KiB. Names are up to 80 characters, hints up to 200, without control characters.

## Rights

Reading the environment and changing a layer follow the roles of [orgs.md](orgs.md): a person manages the layers they
manage (`authz.Manage`): the organization's admins every layer, a tenant's admin the tenant and its teams, a team lead their
team. `GET /env` shows a person the layers they may look at and marks every row `locked` with a `lockedReason` when they
cannot change it. The catalog is read by anyone who manages something in the organization (to add from it) and written
by organization admins only. `/api/me/env` is the person's own.

## Endpoints

All under `/api/orgs/{org}` (id or slug) need a session and answer like the rest of the organizations' API: `404
not_found` for an organization the person has nothing in, `404 not_enabled` while the flag is off. Change requests are
JSON with the same-origin check. Every change is written to the organization's journal (`org_changes`, kind `env`) and the
audit trail, and tells the watchers of the organization's events with `env.changed` `{scope, entryId, rev}`.

### `GET /api/orgs/{org}/env?scope=<tenant id or slug>`

The environment screen. `scope` limits it to one tenant (and its teams); without it all tenants the person may look at.

```ts
type EnvResponse = {
  summary: {
    total: number;                       // enabled entries over all layers
    byKind: { skill: number; mcp: number; repo: number; doc: number };
    callsWeek: number | null;            // null: no tool facts yet, not zero
    top: { id: string; name: string }[]; // the most called items of the week
    unused: number | null;
    pinnedByOrg: number;
  };
  layers: EnvLayer[];                    // the organization, then the tenants, then the teams
  teams: { id: string; name: string; tenantId: string }[];   // for the audience pickers
  // How far the list got: the screen's working machines (not revoked or disabled; of the team when `team` is set),
  // those that reported what they hold, those whose hash is the one the hub computes for them now.
  // A machine whose list is empty counts as current without a report: the hub sends no first empty list.
  delivery: { machines: number; reported: number; current: number; lastAt: string | null };
};
type EnvLayer = {
  layer: 'org' | 'tenant' | 'team'; scopeId: string; tenantId: string;  // tenantId "" on the organization layer
  title: string; subtitle: string; locked: boolean;           // locked: the person may not change this layer
  callsWeek: number | null; counts: KindCounts; items: EnvEntry[];
};
type EnvEntry = {
  entryId: string; itemId: string; kind: Kind; name: string; hint: string;
  audience: { mode: 'tenant' | 'teams' | 'off'; teams: string[] };
  pinned: boolean; locked: boolean; lockedReason: 'pinned_by_org' | 'pinned_by_tenant' | 'no_rights' | null;
  enabled: boolean;
  callsWeek: number | null; spark14: number[];         // 14 UTC days, the oldest first
  lastUsedAt: string | null;
  unused: boolean | null;                               // null: unknown
  // Enabled rows only: what the machines holding the current list said about the item; null: none reported it.
  // A machine with an older list is not counted. `message` is the first reason a machine gave.
  delivery: { ok: number; pending: number; error: number; message: string | null } | null;
};
```

A real response is `web/app/hub-api/fixtures/env/env-admin.json`.

### `GET /api/orgs/{org}/env/items/{entryId}`

The card of an entry: `{entryId, itemId, kind, name, hint, layer, scopeId, enabled, callsWeek, people, perDay,
bars14: {day, calls}[14], lastUsedAt, unused, audience, pinned, locked, lockedReason}`. `people` is the number of
people who called it in the week, `perDay` the week's calls divided by seven, one decimal. A person who may not look at
the entry's layer gets 403, one outside the organization 404. Fixture: `fixtures/env/card.json`.

### `POST /api/orgs/{org}/env/entries` → 201

`{layer, scopeId, itemId, audience?: {mode, teams?}, enabled?: boolean (default true), pinned?: boolean}` — puts an item
in a layer. Answers the new row as `EnvEntry` (the row of the screen). Requires the right to manage the layer. A team
layer takes the audience `tenant` (the whole team) or `off` only. 409 `exists` when the layer already has the item; 409 `locked` when it is created `enabled: false`
under a pin; 400 for an archived or unknown `itemId` (`details.field` `itemId`), a bad layer or an audience that names
teams outside the layer.

### `PATCH /api/orgs/{org}/env/entries/{entryId}` → 200

`{enabled?, audience?, pinned?}`; absent keys stay. Answers the changed `EnvEntry`. Only the layer's managers; switching
off (`enabled: false`) an item a higher layer pinned is refused with 409 `locked`. A change that changes nothing writes
nothing.

### `DELETE /api/orgs/{org}/env/entries/{entryId}` → 204

Takes the item out of the layer.

### Catalog

| Method and path | Who | Answer |
| --- | --- | --- |
| `GET /api/orgs/{org}/catalog?kind=&layer=&scopeId=` | anyone who manages something in the organization | `{ items: CatalogItem[] }`; `layer` and `scopeId` set `added` (the layer has the item) |
| `POST /api/orgs/{org}/catalog` `{kind, name, hint?, source?}` | organization admins | 201 `{ item }` |
| `PATCH /api/orgs/{org}/catalog/{itemId}` `{name?, hint?, source?, archived?}` | organization admins | 200 `{ item }` |
| `DELETE /api/orgs/{org}/catalog/{itemId}` | organization admins | 204 — archives |

`CatalogItem = {id, kind, name, hint, source: object, builtIn, archived, added, createdAt}`. 409 `name_taken` when another
live item of the kind has the name (case aside); 409 `built_in` for a built-in item. The kind of an item never changes.
Fixture: `fixtures/env/catalog.json`.

### `GET /api/me/env?space=<tenant id or slug>`

The person's own environment in a tenant (`space` optional when they have one): `{space: {id, slug, name}, org:
{id, name} | null, items: MeItem[]}` with `MeItem = {itemId, entryId, kind, name, hint, layer, scopeId, layerTitle,
pinned, off, canToggle}`. `canToggle` is false for a pinned item. Fixture: `fixtures/env/me.json`.

### `PUT /api/me/env/{itemId}?space=…`

`{off: boolean}` (`space` may also be in the body) → 200 `MeItem`. 409 `locked` for a pinned item, 404 for an item the
person does not have. Recorded in the audit trail only; it changes nothing of the organization.

## Calls of the tools

The usage feed ([usage.md](usage.md)) writes a `tool` record per session, UTC day, tool kind and tool name as the
session stands: `{provider, sessionKey, day, toolKind, toolName, calls, errors}`. `toolKind` is `skill` (the name of the
skill), `mcp` (the name of the MCP server), `bash` or `other` (the tool's own name). A call the person refused is not a
call; a call whose result has not come yet is not counted until it does. The record carries no text of a conversation
or tool input. A later record of the same (session, day, kind, name) replaces the earlier one; the agent never lowers a
total it wrote. An old hub skips the unknown kind.

The hub stores the day's totals with the session's person and team (the machine's creator, guessed, when the session is
unknown) and finds the catalog item by name when a screen is read: a skill by `name` (a «plugin:skill» call also matches
a bare `skill` name), an MCP server by its server name; Bash and the built-in tools belong to no item. So a skill added
to the catalog later still gets the calls made before. Window: 7 days for `callsWeek` and `people`, 14 for the sparkline,
30 for `unused` (nothing called for 30 days **and** the entry is older than that). **No tool facts at all means `null`**, not zero: the
screens show «нет данных».

## Machines

A machine gets the list of its team: what the layers put in for the machine's tenant and team, without personal switches
(a machine serves a team, not one person). The hub computes it, sorts it by kind, name and id, and hashes it
(`hubproto.EnvHash`, SHA-256 over the canonical JSON; the order of items or of keys does not matter).

- `hello.features` has `environment`; `hello` carries `envRev` and `envHash` (what the agent holds).
- `env.set` (hub → agent) `{rev, hash, items: [{itemId, kind, name, layer, pinned, source}]}` replaces the whole list. The
  answer is an `EnvState`: `{rev, hash, applied: [{itemId, status: 'ok' | 'error' | 'pending', message?}]}`. `hash` is the
  one of the list the agent holds.
- `env.state` (agent → hub, event) `{"event":"env.state","state":{…EnvState}}` after connect and when the outcome of
  applying changed.
- The hub sends when the machine's hash differs from the computed one, per online machine with the feature and only
  while the `env` flag is on. `rev` grows by one with each changed list. It does not send a first empty list, and it
  sends again on every pass while the agent reports another hash than the hub's. The agent's last state is kept per
  machine (`env_state`).

The agent keeps the list in `environment.json` in its data folder (atomic write, `0600`). An identical hash only bumps the
rev; a list whose hash does not match its items is refused (`invalid`). An observe link (`AGENT_HUB_OBSERVE_FILE`)
accepts `env.set` too and keeps the list in its own folder (`<data>/hub/`); its budget, tunnel and policy stay closed.
Item ids must match `[A-Za-z0-9_-]{1,64}`, at most 500 items, a source at most 256 KiB.

The agent writes the list as a Claude plugin `hub-environment` in `<link folder>/environment/claude-plugin`
(`hubproto.EnvPluginDir`): a skill with a body becomes `skills/<itemId>/SKILL.md`; a document, a repository or a reference
becomes a reference skill that tells the chat where to look; an MCP server goes to `.mcp.json`, its secrets as
`${NAME:-}` from the machine's environment. An MCP server whose secret the machine does not have, or an HTTP server with
more than one secret, is `pending` with the reason; a bad id or an unknown kind is `error`. An empty list removes the
plugin. The runtime passes `--plugin-dir` for every such folder that exists to each new Claude chat; the list reaches a
running chat with its next start. The screen shows this as «Доехало до машин» and, per row, what did not go in.

## Storage

Migration `0012_environment.sql`, additive only (SQLite and Postgres; the previous binary ignores the tables):
`catalog_items`, `env_entries` (unique per organization, layer, scope and item), `env_person_off`, `env_state` (per
machine: what was sent and what the agent reported), `tool_usage_day`.

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `HUB_ENV_FEATURES` | empty | comma list of `env` (the layers, `/me/env`, the machines' lists), `catalog` (the catalog endpoints) or `all`; empty: both off, the endpoints answer `404 not_enabled`, no machine is sent anything |

The endpoints also follow `HUB_ORGS`. Per-organization switches are not in the base yet (there is no `org_features`), so
the flag is for the whole hub. `HUB_USAGE_INGEST=on` is needed for the numbers.

## What is not done

- A member's own additions from the catalog (not only switching off) have no storage.
- Personal switches are not applied to a machine's list; machines get the team's list.
- Tool names of sessions already in an agent's facts cache appear after their transcript is parsed again.
- Codex chats do not get the list.
- Turning the flag off leaves the machines with their last list.
- The applied status is shown summed over the screen's machines; there is no per-machine view.
- The flag is not in `GET /api/config`.
