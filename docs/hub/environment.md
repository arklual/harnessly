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
| `app` | `from` (`repo`), `repo` (the item id of a `repo` of the list) or `repoUrl` (https) + `branch`, `path` (the app's folder in the repository, relative, no `..`), `off` (boolean: off for the whole organization) — see «Apps» |
| `pack` | as `app`: `repo` or `repoUrl` + `branch`, `path` (the pack's folder with `harnessly/pack.json`) — see [packs.md](packs.md); org admins only, flag `env.packs` |

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

## Presets

A **preset** is a named set of one layer's groups, for a person who does not want to switch groups one by one: they pick
it, and its optional groups turn on while the layer's other optional groups turn off. Required groups are not touched.

- **Preset** — `id`, `name` (≤ 40 characters, unique in the layer, compared without case), `hint`, `groupIds` (groups
  of the same layer), `position`. Managers of the layer keep presets; a deleted group leaves the presets that held it.
- Picking a preset is the person's own choice per optional group, the same as `PUT /api/me/env/groups/{groupId}`: each
  change is in the audit log (`env.person.group`), and the person's machines get the new list at once.
- **Active** — the person's current choice of the layer's optional groups (the ones they have items in) is exactly the
  preset's.
- A person sees only presets with at least one group they have items in.

| Method and path | Who | Answer |
| --- | --- | --- |
| `POST /api/orgs/{org}/env/presets` `{layer, scopeId, name, hint?, groupIds?}` | managers of the layer | 201 `{preset: PresetView}`; 409 `name_taken`; 400 `invalid_request` with `details.field` `groupIds` for a group of another layer |
| `PATCH /api/orgs/{org}/env/presets/{presetId}` `{name?, hint?, groupIds?, position?}` | managers of the layer | 200 `{preset}` |
| `DELETE /api/orgs/{org}/env/presets/{presetId}` | managers of the layer | 204 |
| `PUT /api/me/env/presets/{presetId}?space=` | the person | 200 `{presets: MePreset[], groups: MeGroup[]}` as they are now; 404 for a preset none of whose groups holds an item of the person |

`GET /api/orgs/{org}/env` lists every layer's `presets: PresetView[]` (`{id, name, hint, groupIds, position}`);
`GET /api/me/env` has `presets: MePreset[]` (`{id, name, hint, layer, groupIds, active}`).

Example — presets over the groups above, by the API:

```sh
curl -X POST $HUB/api/orgs/acme/env/presets -d '{"layer":"org","scopeId":"<org id>","name":"Бэкендер","groupIds":["<id of Бэкенд>"]}'
curl -X POST $HUB/api/orgs/acme/env/presets -d '{"layer":"org","scopeId":"<org id>","name":"Аналитик","hint":"отчёты без сервисов","groupIds":["<id of Аналитика>"]}'
# a person picks «Аналитик»: Аналитика goes on, Бэкенд off
curl -X PUT "$HUB/api/me/env/presets/<id of Аналитик>?space=product" -d '{}'
```

The same in an env-seed file: `presets:` of an organization, with the groups by name (they must be groups of the same
layer, in `groups:` or already there). The file is idempotent; a preset without `hint:` keeps its hint.

```yaml
orgs:
  - org: acme
    presets:
      - {layer: org, name: Бэкендер, groups: [Бэкенд]}
      - {layer: org, name: Аналитик, hint: отчёты без сервисов, groups: [Аналитика]}
```

`--dry-run` prints `+ preset «…» in …` for a new preset and `~ preset «…» in … (groups)` for a changed one.

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

### Apps

An `app` item puts a harnessly app (the folder with `harnessly/app.json`: a design system, presentation templates) on
everyone the item reaches. Only an organization's admin writes one (a team's or a tenant's is refused, like `plugin`),
and it goes only to a machine whose hello has `environment.apps` (`hubproto.FeatureEnvApps`).

The machine's step comes after the repositories and before Claude's settings and the scripts
(`internal/envsetup/app.go`): it takes the folder as a script from a repository is taken (a repository of the list, or a
clone of `repoUrl` kept for the environment) and installs it in the app registry as an **environment install**
(`internal/apps/envinstall.go`): a copy in `<data>/apps/env/<id>/<version>`, a record in `<data>/apps/env.json`. The same
version from the same files is «уже стоит». A trusted app is never installed this way (the step fails).

- The person cannot remove it. They can turn it off for themselves only when the item is not pinned (a pinned item, or
  an item of a required group); a pinned one says so on «О приложении». Their own install of the same app wins.
- After a full run on the machine itself (not a check, not a sandbox) the environment's apps whose items left the list
  are taken off, the copy with them. Each hub link takes off only its own.
- A new version in the repository comes with the next setup. Boards already made from the app's templates are not
  changed; a strict template keeps them locked as before.
- **Off for the organization**: `source.off: true` (the item's form, «В организации» → «Выключено у всех») turns the
  app off for everybody, pinned or not. The machines get the change with the list, at once and without a setup run;
  the app stays installed with its settings and data, and is not listed: no item in «Приложения», no tools for the
  agent, no views. Turned on again, it is back as it was. The service's own data and links to it are not touched.
- **In the menu**: an app of the environment whose manifest targets `app` stands in the sidebar's «Приложения» for
  everybody, without turning it on in a chat; a person who turned it off for themselves does not see it there.

### Built-in apps

«Доски» and «Документы» are apps of the platform itself: every organization has them with no setting up, as `app`
items of the organization's layer, **while the hub has the address of their service** (`HUB_BOARDS_URL`,
`HUB_ARTIFACTS_URL`; see [Boards](../apps/boards.md) and [Documents](../apps/documents.md)). A hub without the address
of a service has no item for it. They are on for everybody and not pinned.

| Item | Item id | App id | Needs | Package by default |
| --- | --- | --- | --- | --- |
| Доски | `builtin-app-boards` | `harnessly.boards` | `HUB_BOARDS_URL` | `https://github.com/ly-solutions/boards`, branch `main`, folder `harnessly-app` |
| Документы | `builtin-app-docs` | `harnessly.docs` | `HUB_ARTIFACTS_URL` | `https://github.com/ly-solutions/artifacts`, branch `main`, folder `harnessly-app` |

Like «Рекомендованное», the items and their entries (`builtin-entry-app-…`) live in the code
(`internal/hub/env/builtinapps.go`), not in the database; an entry shows `lockedReason: "built_in"`, and a row of
`GET …/env` has `builtIn: true` and `off`.

- **Where the package comes from.** `HUB_BOARDS_APP_REPO` / `HUB_BOARDS_APP_BRANCH` and `HUB_ARTIFACTS_APP_REPO` /
  `HUB_ARTIFACTS_APP_BRANCH` name another repository (https) and a branch or a tag of it; the folder is always
  `harnessly-app`. A closed network names its mirrors here ([Closed network](../self-host/closed-network.md)). The
  default is the branch `main` of the service's own repository, not a tag: the package's version is in its manifest,
  a machine installs it anew only when the version or the files change, and the package has to match the service the
  installation runs. Name a tag in `…_APP_BRANCH` to hold a version.
- **New versions reach the machines by themselves**. A machine done with its list looks at the branch of
  every `app` item of a repository outside the list every ten minutes (`git ls-remote`, the scripts' and packs'
  branches every half an hour as before); a branch that moved starts an apply, which installs the app again from the
  new commit. Each run's step of an app carries `version`, the manifest's version now on the machine; the row of the
  app in `GET …/env` has `appVersion` and `appUpdatedAt` (when a machine last put that version on, from the runs of
  the last two weeks), and «Окружение» shows them in the item's sheet. The page reads the machine's apps every three
  minutes while a view of one is on screen: a new version starts the view anew, and the view opens what it had open
  (`data.object`), so an open board stays open.
- **A person** turns an app off for themselves on their machine («Приложения», or the settings), as any app of the
  environment that is not pinned. The hub's personal switch (`PUT /api/me/env/{itemId}`) answers 409 `built_in` for a
  platform's item, and `GET /api/me/env` lists it with `canToggle: false`.
- **The organization** turns one off for everybody with the same «Выключено у всех» as its own apps:
  `PATCH /api/orgs/{org}/catalog/builtin-app-boards` with `{"source": {"off": true}}` (an organization's admin; in
  «Окружение», the item's card). It is the one thing an organization changes in the item: a name, a hint or an archive
  in the request answers 409 `built_in`. The choice is kept as a row of `org_features` (`env.app.boards`,
  `env.app.docs`; no row means on), which is not one of the hub's flags and is not listed among them. The machines get
  the item with `source.off`, so the app stays installed with its data and is not there for anybody.
- **The hub** leaves them out everywhere with `HUB_ENV_APPS=off`; an organization then puts the apps in with `app`
  items of its own.
- **No doubles.** When the list of a person or a machine has an `app` item of the organization's own for the same
  package (the same repository address, compared without case, a trailing `/` or `.git`, and the same folder), the
  platform's item is left out: the organization's wins, with its pin, its audience and its switch. On the machine one
  app is one install: the organization's own item of the same app id takes the install over from the platform's item
  (and the platform's gives way to it), and an item that replaced another keeps the install, its settings and the
  person's switch.
- **Without «Окружение».** The machines of an organization whose `env` flag is off get the platform's apps and
  nothing else of the environment; such an organization has no screen to turn them off, only the hub's switch.
  A machine of a space that is in no organization gets no list, as before.
- **Older agents.** An item goes only to a machine whose hello has `environment.apps`; the list of any other machine,
  and its hash, are what they were.

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

**The rule for apps** (`Org.appsConsent`). An organization's admin switches «Не спрашивать разрешений у
приложений» on the organization's screen (`PATCH /api/orgs/{org}` `{appsConsent: 'auto' | 'ask'}`, a person's session
only: no machine's or agent's token reaches the orgs API). The rule rides on `env.set` as `appsConsent: 'auto'` to an
agent whose `hello.features` has `environment.apps-consent`; then `hash` is `hubproto.EnvSetHash(EnvHash(items),
'auto')`, so a change of the rule reaches the machine the way a change of the list does. The rule `ask` is not sent and
leaves the hash of the items, so an older agent (which neither has the feature nor knows the field) and a new one
without the rule go on asking. The agent keeps `appsConsent` in `environment.json` beside the list (the file's `hash`
stays the items', as the setup reads it); the runtime reads it from every link folder. When one link says `auto`, an
install, an update or a dev edit that would wait for the person gets its rights at once (the journal line
`consent.policy` with the version), and what waited is granted when the rule comes on. Turned off, it takes nothing
back; new asks wait for the person again. A machine without a hub has no such file and asks.

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

| `provider` | `api` (`anthropic` or `openai`), `baseUrl`, `models` (the model ids offered; empty: the machine asks the provider), `key` (the secret with the provider's key; empty: none), `env` (plain variables of a launch, `NAME=value`), `secretEnv` (secret variables, `NAME=<secret>`), `preset` |

A script that calls `brew` is refused (400, `details.field` `source.body` or `source.run`): the machines are Linux.

### Model providers

An item of kind `provider` gives every machine of its audience a model provider: the person finds it in Settings →
«Провайдеры» beside the accounts they added themselves, marked «из окружения», and picks its models for the background
tasks; with the Anthropic API it is also a launch profile of Claude Code chats, and `env` / `secretEnv` are added to
the launch. The person cannot change or remove it on the machine (which account of a provider is the main one stays
their choice); it leaves with its item.

**The source names its secrets and holds none.** `key` and the right side of a `secretEnv` line are either the name
of a secret of the person's safe (everyone puts their own; the machine opens the safe's ask and the step waits,
`pending`), or a reference `vault:<path>#<key>` to a secret the organization keeps on the safe's server: nobody types
it, the machine reads it through its safe ([docs/safe.md](../safe.md), «References»; the folders a reference may read
are `HUB_SECRETS_REF_PREFIXES` / `RUNTIME_SECRETS_REF_PREFIXES`). A reference that cannot be read is the step's error,
said at the setup and not at the first launch.

**A secret goes only to the server the person allowed it for.** A secret of the person's safe is bound to sites
(`origins`). The machine uses a named secret for a provider only when the person put it, or allowed the one they
have, for that provider's server: the ask the machine opens names the server, and until it is answered the provider is
kept without the secret (the step is `pending`). An item cannot therefore send a token the person keeps for something
else to an address of its own, and a new address of the item needs the person's word again. The same holds for an
account an agent writes with `harnessly settings`: it names only secrets allowed for that very server (`harnessly
secret ask NAME --origin https://<host>`), never another account's key, and never a `vault:` reference. A variable that is a credential by its name (`…KEY`, `…TOKEN`,
`…SECRET`, `…PASSWORD`) is refused in `env` (400, `source.env`): it goes to `secretEnv` with a secret named. The
variables a launch sets itself (`PATH`, `HOME`, `CLAUDE_CONFIG_DIR`, the `HARNESSLY_`, `RUNTIME_` families
and the like) and the ones that make a program run somebody's code (`NODE_OPTIONS`, `LD_PRELOAD`, `GIT_*`, `PYTHON*`,
`EDITOR` and the like) are refused in both. So are the variables that would send the launch's requests, and with them the key,
to another server than `baseUrl` or take away the check of who answers: `ANTHROPIC_BASE_URL` of another server, the
proxy variables, `NODE_TLS_REJECT_UNAUTHORIZED`, the switches to another cloud. The paths of certificates
(`NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`) are taken. Values of secrets are in no answer of the hub or of the machine, in no journal and
in no log: the registry keeps the names.

A deployment names more families of its own in the runtime's `RUNTIME_LAUNCH_ENV_RESERVED` (prefixes, comma-separated;
empty says there are none). A hub's machine without it says so in its log at start and in the machine checks.

The hub sends the kind only to a machine whose `hello` has `environment.providers` (and the machine setup); an older
machine gets the list without it. Only an organization admin writes such an item (as an `access`).

The person's own accounts are set up on the machine: on the Settings screen, or by an agent with `harnessly settings`
(the key `providers`: the list in JSON to read; an account in the same form as the source, with a `name`, to write).
An agent names secrets the same way and asks the person for a missing one with `harnessly secret ask`; outside the
Settings window its change waits for the person's confirmation on a card.

#### Proposals of an agent

An agent may propose a provider for the whole organization, with `harnessly settings` and the key
`environment.providers` (a machine of a hub only): `{"name", "hint"?, …the source}` adds the provider of that name or
changes it, `{"name", "remove": true}` takes it out of the environment. The machine's link proves the machine, not a
person — whoever drives a chat on an admin's machine is not that admin — so a machine changes nothing: the proposal
travels as the event `env.propose` (`hubproto.EnvProposal`), the hub checks its shape with the rules of a source and
keeps it (`env_proposals`), and answers with the op `env.proposed` (`hubproto.EnvProposalResult`): `202`, it waits, or
a refusal with its words. The machine asks twice: first with `check` (nothing is kept), so that a wrong value is
refused before any card, then for the proposal itself, after the person confirmed it on the card; the agent is told
«proposed, nothing is changed yet». No value of a secret is carried: a key and a secret variable are named.

An organization admin decides it on the environment screen, in the block «Предложения агентов», under their own
session: `GET /api/orgs/{org}/env/proposals` lists the waiting ones — what would change (the source as proposed, and the
one of that name the organization has now), the machine, who connected it and the chat it came from —
`POST …/env/proposals/{id}/apply` makes the change through the handlers of the screen (the admin's rights, the same
checks, the same journal, whose line names the admin as the author and says it was an agent's proposal, from which chat
and machine), `POST …/env/proposals/{id}/decline` drops it. Nobody but an organization admin sees or decides them; being
an administrator of the installation does not count here. A proposal waits seven days; the same machine proposing the
same name again replaces its waiting one; a machine keeps at most five waiting and an organization twenty.

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
  It opens the database with the serving hub's settings (`HUB_DATABASE_URL`, `HUB_DB_SCHEMA`, `HUB_DB_POOLER`,
  `HUB_DB_MIGRATE_URL`, `HUB_DB_CHECK`). `--dry-run` only connects: it never migrates, and refuses a database that is
  behind the binary.

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
| `HUB_ENV_APPS` | `on` | `off` leaves the platform's apps «Доски» and «Документы» out of every organization (see «Built-in apps») |
| `HUB_BOARDS_APP_REPO`, `HUB_BOARDS_APP_BRANCH` | the boards service's public repository, `main` | where the machines take the «Доски» package from: an https address of a repository with the folder `harnessly-app`, and a branch or a tag |
| `HUB_ARTIFACTS_APP_REPO`, `HUB_ARTIFACTS_APP_BRANCH` | the documents service's public repository, `main` | the same for «Документы» |
| `HUB_ENV_FEATURES` | empty | comma list of `env` (the layers, `/me/env`, the machines' lists), `catalog` (the catalog endpoints) or `all`; empty: both off, the endpoints answer `404 not_enabled`, no machine is sent anything |

The endpoints also follow `HUB_ORGS`. Per-organization switches are not in the base yet (there is no `org_features`), so
the flag is for the whole hub. `HUB_USAGE_INGEST=on` is needed for the numbers.

## What is not done

- A member's own additions from the catalog (not only switching off) have no storage.
- Personal switches are not applied to a machine's list; machines get the team's list.
- The hub's personal switch does not take a platform's item (409 `built_in`): its storage is by catalog item, and the
  platform's items are not in the catalog. A person turns a platform's app off on their machine.
- The hub compares an organization's own app with a platform's by the package's place, not by the app id (it does not
  read manifests); two packages of one app id from different places meet on the machine, where the organization's wins.
- Tool names of sessions already in an agent's facts cache appear after their transcript is parsed again.
- Codex chats do not get the list, nor «Рекомендованное» (Codex's `~/.codex/config.toml` is not written).
- Turning the flag off leaves the machines with their last list.
- The applied status is shown summed over the screen's machines; there is no per-machine view.
- The flag is not in `GET /api/config`.
