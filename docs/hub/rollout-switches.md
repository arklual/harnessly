# Rollout switches

A rollout switch turns a feature on or off for a **person on a machine** without a release (the wave that moves
the gateway's switches into the hub). It is finer than an organization's flag (`internal/hub/features`): the same feature
can be on for one person on one machine, for a person everywhere, for a machine, or for everyone.

## Table

`rollout_switches` (migration `0042`, additive only):

| column | meaning |
|---|---|
| `feature` | the name of the path or feature |
| `person_email` | a lower-case email, or `*` for anyone |
| `machine_id` | a machine id, or `*` for any |
| `enabled` | on or off |
| `note`, `updated_by`, `updated_at` | who and why (Unix ms) |

Primary key `(feature, person_email, machine_id)`.

## Which row decides

For a feature, a person and a machine, the rows that match are the exact pair and the `*` ones. The most specific wins:
the pair, then the person (`email, *`), then the machine (`*, id`), then `(*, *)`. **No row means off**, so a new feature
ships dark and is turned on pair by pair, then with one `(*, *)` row. A row with `enabled = false` turns it off again for
its scope, whatever the wider rows say.

## In code

`internal/hub/switches`: `Gate.On(ctx, feature, person, machine)`; `Store.ByPrefix(ctx, prefix, person, machine)` is the
sorted names after `prefix` of the features that are on (several variants of one thing, `<prefix><name>`); `Set`,
`Remove` and `List` change and read the rows. Answers are cached for 5 seconds (`switches.CacheTTL`): a change made by
this process is seen at once, one made by another replica within that time. A table that cannot be read means off.
`switches.Static` is a fixed gate for tests of other packages.

The instance's own domains and keys never belong in the rows or the code: they come from the configuration.

## Known switches

- `preview-slot-host`: preview slots on an origin of their own ([preview-slots.md](preview-slots.md)).
- `prompt-launch`: a conversation started from outside with `HUB_LAUNCH_TOKEN`, per person and machine ([automations.md](automations.md)).
- `hub-board-events`: the hub takes the comment events of the boards this person's machines drew and hands the comments to the conversation ([docs.md](docs.md)); off by default, the events then go on to `HUB_BOARD_EVENTS_FORWARD_URL`.
- `hub-eval`: the hub's scheduler of the evaluation queues and starts the runs of this person's schedules ([eval.md](eval.md)); off by default.
- `hub-design-home`: Sinopia's home in one answer, `GET <platform>/design/home` from the machine, per person and machine. Off (default): the hub answers 404 `home_off` and the page asks part by part; a runtime with no hub always serves it. On: the hub asks the machine and its store at once and gives the machine's answer when it comes within 800 ms, otherwise the store's copy of the library (`parts.storeLibrary`, `from: "store"`) so the page draws at once and puts the machine's library over it later; header `X-Harnessly-Home-From: machine|store|none`, 503 `home_unavailable` when neither has it. The same switch opens `GET <platform>/design/store-library/changes?since=<rev>`: a long poll (up to 25 s) that answers `{"rev"}` once the space's library in the store moves past `since`; the page reads the library again on it instead of every 5 s while an agent draws.
- `hub-search`: ⌘K from the hub's database, `GET <platform>/search?q=` ([search.md](search.md)), per person and machine. Off (default): the hub answers 404 `search_off` and the page asks the library of the machine, as before; a runtime with no hub always serves it.
- `hub-apps-catalog`: the apps' document types and their documents from the hub's database, `GET <platform>/apps/kinds` and `/apps/kinds/docs` ([search.md](search.md)), per person and machine. The types are read from the packages of the space's team in the store, the documents are the projects the person has a role in (machines closed to them, HRN-1411, leave theirs out) whose kind is an app's type. Off (default): the hub passes both to the machine, as before. The team's catalog (`/apps/team`, with the machine's checks and the person's mutes) stays the machine's. A runtime with no hub serves both itself.
- `hub-notices`: the person's notices, what they read or took away and their notification switches live in the hub's database, one set for all their machines and devices, with a stream that tells every device of a read ([notices.md](notices.md)), per person and machine. Off (default): the machine's own store answers, as it does without a hub.
- `hub-design-owner`: the hub owns the design projects' records: title, kind, preview, kit, team and the machine of the working copy are columns of `design_projects`, the machine's snapshot no longer overwrites a newer hub edit, and a machine that is off does not remove its projects from the library (`GET <platform>/design/store-library` lists them from the hub's rows). Per person and machine. Off (default): the library lists only what the store has files of, as before; a runtime with no hub always keeps its own records ([store.md](store.md)).
- `hub-stats`: the statistics screen reads the hub's `GET /api/me/stats` for everything, per person (the web asks `GET /api/me/switches`). Off (default): the screen reads the machine's own `/stats`; a runtime with no hub always counts for itself. The hub fills hours of the day, projects, task keys, interrupts, refusals and compactions from the session metrics and session facts (`session_facts`) the machines send.
- `hub-projects`: the sidebar «Проекты» reads the hub's registry `GET /api/me/projects`, per person (the web asks `GET /api/me/switches`; off: the endpoint answers 404 `projects_off`). The machines send the registry (`projects` event: a full list on connect, then create, rename, path change, delete) into `machine_projects`. The list holds the projects of the machines the person may open by the proxy's rule (`machaccess` with the owner's own-work rule): a machine closed to the person gives nothing; a machine that is not on keeps its projects, each with `asleep: true` and the label «машина спит». Off (default): the sidebar reads the runtime; a runtime with no hub always does.
- `connections-shared`: the person's machine keeps the list of their connections (Settings → Connections, no credentials) in the hub's safe as `sys/connections` of their folder, and takes from it the connections made or removed on their other machines (docs/connections.md). Per person and machine (the owner's email, the machine id). Off (default): the hub refuses that record (403 `safe_denied`) and each machine keeps its own list, as before.
- `client-api`: the person connects their own apps (a launcher, a menu-bar counter) with a personal token and the apps read and write their chats through `/api/client/v1` ([client-api.md](client-api.md)); per person, machine `*`. Off: the consent page refuses and the tokens already given stop working.
- `tbot-join`: a machine of this person asks the hub for the join token of its Teleport identity, and the person approves it on the screen «Машины» ([tbot-join.md](tbot-join.md)); per person (the bot's owner) and machine. Off (default): the hub refuses the request with `off` and keeps nothing. Needs `HUB_TBOT_JOIN=on` too.

## API

Only an administrator of the installation (a switch belongs to the installation, not to an organization):

- `GET /api/admin/switches[?prefix=…]` → `{"switches": [{feature, person, machine, enabled, note, updatedBy, updatedAt}]}`
- `PUT /api/admin/switches` with `{feature, person?, machine?, enabled, note?}` → 204 (empty person or machine is `*`)
- `DELETE /api/admin/switches?feature=…&person=…&machine=…` → 204
