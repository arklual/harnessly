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
| `plugin` | `marketplace` (GitHub `owner/repo` or an https git address), `plugin` (`name@marketplace`) — see «Recommended» |
| `claude-settings` | `always`, `ifAbsent` (objects of Claude Code's settings), `claudeMd` (≤ 8 KiB) — see «Recommended» |

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
  delivery: { machines: number; reported: number; current: number; lastAt: string | null } | null;   // null past 200 working machines
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

## Groups

> **Important — read before you put a list together.** A list of more than about five repositories, or with anything
> heavy or rarely used, goes into **groups**. Make **required** only what nobody of the team can work without; put the
> rest into **optional** groups, and what most people do not need into optional groups that start **off**
> (`defaultOn: false`). A machine gets an optional group only when its person has it on, so a team of thirty services
> does not clone thirty repositories onto every laptop.

- **Group** — a named group of one layer's entries: `id`, `name` (≤ 40 characters, unique in the layer), `hint`,
  `required`, `defaultOn` (default true), `position`. An entry is in at most one group of its own layer (`groupId`;
  `""` for none).
- **Required**: the group's items are pinned — nobody below and no person can switch them off.
- **Optional**: each person switches the group on or off for themselves in a tenant; with no choice it is `defaultOn`.
  Items of a group that is off for the person are `off` in `GET /api/me/env` and are **not in the list of the machines
  that are theirs** (`store.Machine.CreatedBy`), so nothing of them is cloned, run or loaded there. What a machine has
  already set up stays (a folder may hold work); it is just no longer kept up.
- An item put in by several layers takes the group of the highest layer that puts it in, as it takes the item.
- Deleting a group leaves its entries in, without a group; the people's choices of it go.

| Method and path | Who | Answer |
| --- | --- | --- |
| `POST /api/orgs/{org}/env/groups` `{layer, scopeId, name, hint?, required?, defaultOn?}` | managers of the layer | 201 `{group: GroupView}`; 409 `name_taken` |
| `PATCH /api/orgs/{org}/env/groups/{groupId}` `{name?, hint?, required?, defaultOn?, position?}` | managers of the layer | 200 `{group}` |
| `DELETE /api/orgs/{org}/env/groups/{groupId}` | managers of the layer | 204 |
| `POST …/env/entries` and `PATCH …/env/entries/{entryId}` with `groupId` | managers of the layer | the group must be of the entry's layer (400 `invalid_request`, `details.field` `groupId`); `""` takes the entry out |
| `PUT /api/me/env/groups/{groupId}?space=` `{on: boolean}` | the person | 200 `MeGroup`; 409 `locked` for a required group, 404 for a group none of their items is in |

`GET /api/orgs/{org}/env` lists every layer's `groups: GroupView[]` and every row's `groupId`; `GET /api/me/env` has
`groups: MeGroup[]` (`{id, name, hint, layer, required, on, canToggle, items}`, only groups the person has items in) and
every item's `groupId`.

Example — a team's code in groups, by the API:

```sh
curl -X POST $HUB/api/orgs/acme/env/groups -d '{"layer":"org","scopeId":"<org id>","name":"Основа","required":true}'
curl -X POST $HUB/api/orgs/acme/env/groups -d '{"layer":"org","scopeId":"<org id>","name":"Бэкенд"}'
curl -X POST $HUB/api/orgs/acme/env/groups -d '{"layer":"org","scopeId":"<org id>","name":"Аналитика","defaultOn":false}'
curl -X PATCH $HUB/api/orgs/acme/env/entries/<entry id> -d '{"groupId":"<id of Бэкенд>"}'
# a person switches «Аналитика» on for themselves
curl -X PUT "$HUB/api/me/env/groups/<id of Аналитика>?space=product" -d '{"on":true}'
```

The same in an env-seed file (`hub env-seed --file`): `groups:` of an organization, and `group:` of an entry, by name.

```yaml
orgs:
  - org: acme
    groups:
      - {layer: org, name: Основа, required: true}
      - {layer: org, name: Бэкенд, hint: сервисы}
      - {layer: org, name: Аналитика, defaultOn: false}
    entries:
      - {layer: org, item: Настройка машины, group: Основа}
      - {layer: org, item: orders, group: Бэкенд}
      - {layer: org, item: reports, group: Аналитика}
```

An entry without `group:` keeps the group it has; a group without `defaultOn:` keeps (or, new, gets) `true`.

## Recommended

«Рекомендованное» is a group of the platform itself, in every organization with no setting up: plugins and settings of
Claude Code an agent works better with, none of which the platform needs. It is **optional and off**: each person turns
it on for themselves (in «Окружение» of the hub, or on their machine's settings), and then their machines put it in in
the background, with the same setup runner as the rest; one item failing does not stop the others. Turned off again,
the group stops being updated and **nothing is undone**: the plugins and settings it put in stay.

| Item | Kind | What it brings |
| --- | --- | --- |
| Superpowers | `plugin` | the way of working: plan, tests, a check before handing in |
| Context7 | `plugin` | up-to-date documentation of libraries |
| Playwright | `plugin` | the agent opens pages and checks them in a browser |
| Chrome DevTools | `plugin` | the network, the console and the speed of a page |
| Экономные настройки Claude | `claude-settings` | subagents on Sonnet, sane Bash timeouts, the rules of economy in CLAUDE.md |

The items, the entries (`builtin-entry-…`) and the group (`id: "recommended"`, `builtIn: true`) live in the code
(`internal/hub/env/recommended.go`), not in the database: nobody changes or removes them in the hub (an entry shows
`lockedReason: "built_in"`; a change of the group answers 409 `built_in`). An organization's admin may put entries of
the organization's own into the group (a plugin of the team with its hooks, say). `HUB_ENV_RECOMMENDED=off` leaves the
group out of every organization of the hub.

**The two kinds.** Both are done by the machine's setup runner (`internal/envsetup/claude.go`) and go only to a machine
whose hello has `environment.claude`, so an older agent never sees them. Both change the person's Claude Code, so only an
organization's admin writes them (a team's or a tenant's item of these kinds is refused).

- `plugin` — when the marketplace is not known on the machine, `claude plugin marketplace add <marketplace>`; then
  `claude plugin install <plugin>`. A plugin already in place is left to Claude Code, which updates its plugins itself.
  No `claude` on the machine: the step fails with that, and the rest go on.
- `claude-settings` — in `~/.claude/settings.json`, the keys of `always` are set every time and the keys of `ifAbsent`
  only where the person has none (`env` variable by variable); nothing else in the file changes, and a file that is not
  JSON is left alone (the step fails). Only these keys are written — `env`, `model`, `effortLevel`,
  `includeCoAuthoredBy`, `cleanupPeriodDays`, `subagentPromptCacheTtl`, `alwaysThinkingEnabled`, `outputStyle`,
  `spinnerTipsEnabled` — and in `env` only `CLAUDE_CODE_SUBAGENT_MODEL`, `BASH_DEFAULT_TIMEOUT_MS`,
  `BASH_MAX_TIMEOUT_MS`, `CLAUDE_CODE_MAX_OUTPUT_TOKENS`, `MAX_THINKING_TOKENS`, `CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY`,
  `DISABLE_ERROR_REPORTING`, `DISABLE_TELEMETRY`, `DISABLE_AUTOUPDATER` (`internal/hubproto/claude.go`): never hooks,
  permissions, an API address or a key. `claudeMd` goes into `~/.claude/CLAUDE.md` between
  `<!-- harnessly:<item id> -->` and `<!-- /harnessly:<item id> -->`: the block is rewritten in place, the rest of the
  file is never touched. A file linked from elsewhere (dotfiles) is written where it really is.

A check run says what would change and changes nothing; a sandbox run writes into the sandbox's own home.

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

## Machine setup

Besides what the chats load, a list can say what the **machine** is to have: repositories cloned, the forges' CLIs
logged in, Linux scripts run in order. Machines are Linux only. Types: `internal/hubproto/envsetup.go`.

### Kinds and their `source`

| Kind | Keys |
| --- | --- |
| `repo` | `url`, `branch` (as before), `dir` (the folder on the machine: `~/…` or relative to the home; empty: `~/code/<name of the repository>`) |
| `access` | `service` (`gitlab` or `github`), `host` (empty: `gitlab.com` / `github.com`), `secret` (the safe's name of the token; empty: `gitlab-token` / `github-token`) |
| `script` | `from` (`text`, `repo`, `file`, `url`); `body` (the commands for `text`, the file's text for `file`, ≤ 48 KiB); `fileName` (for `file`); `repo` (the item id of a `repo` of the list, for `repo`), `repoUrl` + `branch` (a repository outside the list, cloned only to run the script); `path` (the file in the repository); `url` (for `url`, https only); `run` (how to start it, ≤ 4 KiB, lines run in order; empty: `bash <file>`); `rerun` (`once` or `check`); `check` (for `check`: a command, exit 0 means ready); `secrets` (≤ 16 names of the safe, `[A-Za-z0-9][A-Za-z0-9._-]{0,63}`) |

A script that calls `brew` is refused (400, `details.field` `source.body` or `source.run`): the machines are Linux.

### Order

Scripts run in the order of the list: the organization's first, then the tenant's, then the team's, each layer in its
editors' order (`env_entries.position`). `EnvItem.order` carries the place (1, 2, …) of a script; other kinds have none.
A machine without the feature `environment.setup` (the agent offers it only when the policy `setup` allows, and on an
observe link only with `AGENT_HUB_SETUP=on`) gets neither the order nor the `script` and `access` items (an older
agent would refuse a list it cannot hash the same way).

`PUT /api/orgs/{org}/env/order` `{layer, scopeId, entryIds: string[]}` → 200 `{entryIds}`: the scripts of a layer in
their new order (all of the layer's script entries, each once). Manage of the layer. A changed order is a changed list.

### What the machine does

The agent keeps the list as before and the runtime (the person's process, with the safe) does the setup:

1. **Access.** `glab auth status --hostname <host>` / `gh auth status --hostname <host>`; logged in: `ok`. Otherwise
   the token from the safe (`secret`) logs the CLI in (`glab auth login --hostname <host> --stdin`, `gh auth login
   --hostname <host> --with-token`), then `gh auth setup-git` / a git credential helper for the GitLab host so
   `git clone` over https uses it. No token in the safe: `pending` «положите токен <name> в сейф», and the runtime opens
   the safe's ask on the person's page. No CLI: `error` (a script of the list may install it; access is tried again
   after the scripts).
2. **Repositories.** The folder exists and is a clone of the same address: `ok` (never pulled, never reset: it may hold
   the person's work). Missing: `git clone --branch <branch> <url> <dir>`. Exists and is something else: `error`.
3. **Scripts**, in order. A script gets `HOME`, the safe's `secrets` as variables (`npm-token` → `$NPM_TOKEN`), its
   folder as the working directory (the repository's for `repo`, a fresh temporary one otherwise), 15 minutes. `once`
   runs on a machine once per source: a changed script runs again. `check` runs the check first and the script only
   when it fails. A failed script stops the scripts after it (`pending` «ждёт, пока починят <name>»).
4. Access and repositories that failed are tried once more after the scripts.

Output goes to the run's log with the values of the secrets replaced by `••••••`, at most 16 KiB per step (the tail).

### Runs

A run is the machine doing the list: `apply` (the machine took a new list), `update` (asked: apply now, every
script's check again), `check` (only look; nothing changes), `sandbox` (the whole list from scratch in a home of its
own, a temporary folder with HOME, the XDG folders and the git config inside it, wrapped in bubblewrap too where the machine
has a working one; the ledger and the setup file stay as they are, and each step's log opens with a line saying which). Agent ↔ hub:

- `env.run` (hub → agent) `{runId, mode, items?, by?}` → `{runId, accepted: true}` at once. `items` only for `sandbox`
  (a draft that is not saved). Whether a machine serves it, and the setup kinds at all, is the machine's own choice:
  an own link follows the policy `setup` (a policy file without the key means off); an observe link serves `env.run`
  and the setup kinds only when the machine's operator opts in with `AGENT_HUB_SETUP=on`. A machine that serves them
  advertises `environment.setup` in `hello`, and only while its runtime's setup runner is alive (the runtime
  heartbeats; the hub learns a change at the machine's next connect, and meanwhile the machine reports its setup
  items as «настройщик машины не запущен»). `busy` for a runId the machine already has; runs of one runtime go one at a
  time, a later request waits.
- `env.run` (agent → hub, event) `{"event":"env.run","run":EnvRun}` when a step changes and at the end; the same
  `runId` replaces. After a connect the agent sends the runs of the last 24 hours again; the hub upserts.
- `env.state` keeps saying how each item went; for the setup kinds the status is the runtime's for the list held
  (`pending` «машина ещё настраивается» until it worked on that list).

Runs are kept 14 days on the machine and on the hub (`env_runs`). A run the hub holds as `running` that nothing has
updated for 30 minutes is ended by the hub as `error` («машина не ответила»); the hub looks every 5 minutes. The hub
keeps at most 30 runs of their own a machine starts per hour (`apply`), and at most 512 KiB of steps per run (the logs of
the oldest steps are dropped first); a run beyond that is dropped. A run id is kept only for the machine it belongs to,
and a run of the modes `update`, `check` and `sandbox` only if the hub asked that machine for it.

### Endpoints

| Method and path | Who | Answer |
| --- | --- | --- |
| `GET /api/orgs/{org}/env/runs?layer=&scopeId=&errors=1&limit=` | managers of the layer | `layer` and `scopeId` are required (400 `invalid_request` otherwise); `{runs: EnvRunRow[]}` of the machines the layer reaches, newest first, 14 days; `errors=1`: only runs with an error. A machine changes what it offers (the setup on or off) by calling the hub again |
| `GET /api/orgs/{org}/env/runs/{runId}` | as above, or the machine's owner | `{run: EnvRunRow}` with `log` on every step |
| `POST /api/orgs/{org}/env/try` `{layer, scopeId, draft?: {entryId?, kind, name, source}, machineId?}` | managers of the layer | 202 `{runId, machine: {id, name}}`: a sandbox run of the layer's list (a team: the team's list) with the draft added or put in place of `entryId`, on one of the asker's own online machines with `environment.setup` (`machineId` picks; none: 409 `no_machine`) |
| `GET /api/me/env/machines?space=` | the person | `{machines: MeMachine[]}` |
| `POST /api/me/env/run?space=` `{mode: 'update' \| 'check', machineId?}` | the person | 202 `{runs: {runId, machineId}[]}` — on every online machine of theirs in the tenant (or the one) |
| `GET /api/me/env/runs?space=` | the person | `{runs: EnvRunRow[]}` of their machines |
| `POST /api/orgs/{org}/env/apply` `{layer, scopeId, when: 'now' \| 'next'}` | managers of the layer | 200 `{machines, started, waiting}` — see «Apply on the machines» |

```ts
type EnvRunRow = { id: string; machine: { id: string; name: string }; person: { id: string; name: string };
  mode: 'apply' | 'update' | 'check' | 'sandbox'; by: string | null; status: 'running' | 'ok' | 'error' | 'pending';
  startedAt: string; finishedAt: string | null;
  steps: { itemId: string; kind: Kind; name: string; status: string; message: string | null; exit: number | null;
    ms: number; log?: string }[] };
type MeMachine = { id: string; name: string; online: boolean; setup: boolean;   // setup: has environment.setup
  team: { id: string; name: string } | null;   // the team whose list it holds
  current: boolean;          // holds the list the hub computes for it now
  failed: number;            // setup items in error or waiting for the person
  lastRun: EnvRunRow | null };
```

`MeMachine.team` is `{id, name}` of the team whose list the machine holds (null: no team). `failed` counts the setup
items in `error` and those `pending` with the ask «положите токен … в сейф». `GET /api/me/env` gets `outdated: boolean`
(some machine of theirs in the tenant that serves the setup is not current or has a failed item; a machine with
`setup: false` is not counted)
and `machines: MeMachine[]`.

### Apply on the machines

«Применить на машинах» in the editor's head (managers of the layer only) asks every machine the layer reaches for an
`update` run: the list and, with it, the base package (docs/hub/machine.md, «Base package»). Two choices:

- **«Сейчас»** — a machine online with `environment.setup` runs at once (`started`); the others are marked and run when
  they next connect (`waiting`).
- **«При следующем запуске»** — nothing runs now: every machine is marked and runs when it next connects, so nobody's
  work in progress is touched.

The mark is one row per machine (`env_apply_pending`, the latest ask wins). When a marked machine connects, the hub
sends its list first and asks for the run a few seconds later; the mark is taken off when the run is asked, and put
back if the machine refused it. A disabled machine is not counted. The ask is in the layer's history as `env.apply`.

### Who writes the catalog

An organization admin writes any item. A tenant admin or a team lead creates items for their layer: `POST
/api/orgs/{org}/catalog` takes `owner: {layer, scopeId}` (default the organization); the item is changed and archived
by the managers of its owner layer and by organization admins. `CatalogItem` gets `owner: {layer, scopeId} | null`
(null: the organization). The screen's «Редактор команды» is the team's lead.

**Secrets and accesses are the organization admins'.** A script that takes `secrets` from the safe, and every `access`
item, are allowed only in items of the organization (`owner` null): a team lead or a tenant admin could otherwise read
the members' tokens through a script. `POST`/`PATCH /catalog` with an `owner` refuses them with 400, `details.field`
`source.secrets` (or `kind` for an access): «Токены из сейфа и доступы подключает админ организации». The same holds for
`hub env-seed` and for the drafts of `POST /env/try` (a draft of anybody but an organization admin, or of an owned
item, with secrets or of the kind access, is refused).

An item with an owner (`owner` not null) can be carried only by the entries of exactly that layer and scope: `POST
/api/orgs/{org}/env/entries` refuses another one with 400 `itemId` («Этот пункт принадлежит другому слою — скопируйте
его»), and the resolver skips such an entry. An item of the organization (`owner` null) goes into any layer. «Copy from
another team» is a new item (`POST /catalog` with `owner`). Otherwise whoever edits a team's item could change what
runs on the machines of wider layers.

### Details of the answers

- `POST /env/try`: `409 conflict` with `details.reason` `no_machine` when the asker has no online machine with
  `environment.setup` (or the `machineId` is not one of them). The list sent is the layer's own with the draft; a new
  draft script goes last. The run row is stored as `running` at once; the machine's `env.run` events replace it.
- The hub keeps an `env.run` event only for a run of that machine: a run it asked for (`update`, `check`, `sandbox`) must
  already have a row of the same machine, an `apply` run may be new; a run id held by another machine is dropped.
- `POST /api/me/env/run` with no online machine: 409 `no_machine`. `MeMachine.current` is true when the machine reported
  the hash the hub computes for it now (and for a machine whose list is empty, always).
- Script `source.url` is https only. `repoUrl` and the repository's `url` are https or ssh (`ssh://git@host/…` is fine,
  a password never is).
- `hub env-seed --file env.yaml [--dry-run]` moves catalog items (`kind`, `name`, `hint`, `source`, `owner`) and entries
  (`layer`, `tenant`, `team`, `item`, `pinned`, `disabled`, `audience`, `position`) of organizations named by slug, matched
  by (kind, name) and (layer, scope, item); in a script `source.repo` may be the repository item's name. An item without a `hint` in the file keeps the hint it has.

## Storage

Migration `0012_environment.sql`, additive only (SQLite and Postgres; the previous binary ignores the tables):
`catalog_items`, `env_entries` (unique per organization, layer, scope and item), `env_person_off`, `env_state` (per
machine: what was sent and what the agent reported), `tool_usage_day`. Migration `0015_environment_setup.sql`:
`env_entries.position`, `catalog_items.owner_layer` / `owner_scope`, `env_runs` (one row per run: machine, org, mode,
status, the steps with their logs; removed after 14 days).

## Flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `HUB_ENV_RECOMMENDED` | `on` | `off` leaves the platform's group «Рекомендованное» out of every organization (see «Recommended») |
| `HUB_ENV_FEATURES` | empty | comma list of `env` (the layers, `/me/env`, the machines' lists), `catalog` (the catalog endpoints) or `all`; empty: both off, the endpoints answer `404 not_enabled`, no machine is sent anything |

The endpoints also follow `HUB_ORGS`. Per-organization switches are not in the base yet (there is no `org_features`), so
the flag is for the whole hub. `HUB_USAGE_INGEST=on` is needed for the numbers.

## What is not done

- A member's own additions from the catalog (not only switching off) have no storage.
- Personal switches are not applied to a machine's list; machines get the team's list.
- Tool names of sessions already in an agent's facts cache appear after their transcript is parsed again.
- Codex chats do not get the list, nor «Рекомендованное» (Codex's `~/.codex/config.toml` is not written).
- Turning the flag off leaves the machines with their last list.
- The applied status is shown summed over the screen's machines; there is no per-machine view.
- The flag is not in `GET /api/config`.
