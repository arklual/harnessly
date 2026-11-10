# The store

The store keeps the main copy of people's data — first the Sinopia library — in the hub, and a machine holds a working
copy of it. A file is a chain of revisions in a database; its contents are blobs addressed by their sha256. An edit on
one machine reaches the other machines of the space in seconds, a machine that was off catches up when it is back, and
a write made on a stale copy is kept beside the file instead of being lost.

There is always a store. A runtime with no hub keeps one inside its own process, SQLite and a folder in its data
folder, with the same model, so a hub connected later can take it over (`hub store migrate`).

## Collections

Every space has its own collections; a machine reaches only the ones of the space it belongs to.

| Collection | What it holds |
| --- | --- |
| `design` | the Sinopia library: projects, boards, artifacts. Hidden files do not sync, except a project's `.owner.json`, the shelves in `.folders/`, a project's published files for guests (`.media/published/<file>`: the deck's PDF, a video's MP4) and its PDF mark (`.media/pdf-auto`) |
| `uploads` | files people put into a chat, pictures and attachments |
| `files` | any other file an agent or a person keeps |
| `handoff` | what one machine passes to another |
| `apps` | the files of a space's apps: their storage areas under `storage/` (docs/apps.md); a machine sees only its own person's user and chat areas; the team's apps (`team/`), the guest links (`guests/<person>/`) and their packages (`bundles/`) |

`design` takes only the files of a library (no executables, no tool configuration). The others take any relative path.
A collection is a room of the store named `<collection>-<space id>`.

## How a machine reaches it

1. The runtime reads the hub's `GET /api/capabilities`. Without the capability `store` (an older hub, or
   `HUB_STORE=off`) it keeps its data as before and asks again every ten minutes.
2. It asks for a session with `POST /agent/store`, signed with the machine's own Ed25519 key, the same key it dials the
   hub with (`machine.json`). Nobody hands out a token: a machine that is revoked, switched off or moved to another
   space loses the store within thirty seconds, with its session still in hand.
3. With the session (an hour; a new one is asked before it runs out, or on a 401) it speaks the store's API under
   `/store/v1/collections/<collection>/`.

| Method and path | What it does |
| --- | --- |
| `HEAD`, `GET`, `PUT blobs/<sha256>` | a blob: is it there, read it, put it (the body must hash to its name) |
| `POST commit` | a new revision of a path on top of the revision the writer started from (`base`); a stale base answers `409` with the current revision |
| `GET changes?since=<rev>&wait=<s>` | every path changed after a revision; with `wait`, held until something changes (a long poll) |
| `GET current?path=<p>` | the newest revision of a path |
| `GET history` | the revisions of a path |
| `GET`, `POST leases`, `POST leases/release` | who edits which board now |
| `GET health` | the store answers |

| `GET snapshot?prefix=<folder>/&at=<rev>` | a folder as it was at a revision: the newest revision of each path not newer than `at` |

A blob read by its hash is the same bytes forever: it is served with `ETag` and an immutable cache.

## Who gets which project

The `design` collection is the space's library, but a machine gets only the projects its person — the one its runtime
runs for — has a role in ([design access](design-access.md)): the files of a personal project never reach a machine of
someone with no role in it. A project the hub has no row for (made before access existed, a kit) is open to all.

- `changes`, `current`, `history` and `snapshot` leave out what the machine does not see; `changes` names that in
  `scope`. When the scope changes (a role given or taken), the machine reads the room again from the start: a project it
  was given arrives whole; the files of one it lost are removed when they were not edited there (an edit stays). A
  library several runtimes of one machine share (`RUNTIME_DESIGN_SHARED_DIR`) keeps them, since another runtime's
  person may still have a role.
- A write to a project the person may only view, or has no role in, is refused (`403`); the runtime does not send that
  file again until it changes, and a newer revision from the hub puts the local edit aside instead of losing it.
- A project's `.owner.json` reaching the store before the hub knows the project records it, as the runtime's `design`
  event would: the person the file names owns it when they may open the machine that sent it, otherwise the machine's
  person. Its rows then count on every machine of the space, whichever machine recorded it.
- An owner file records a project only when its folder, in any letter case, holds nothing else yet: an owner file over
  a filled folder takes nothing over. Later writes to a recorded project's `.owner.json` need its owner; a top folder that
  is not plain ASCII is not written (project ids are ASCII; a look-alike letter could pass for another project's).
- One runtime of a machine syncs a folder at a time (`<folder>/.store-sync.lock`); the others wait. A library the
  runtimes of several people share (`RUNTIME_DESIGN_SHARED_DIR`) is synced by each of them with its own person's
  projects (a lock of its own per runtime): the folder holds what any of the machine's people has a role in.

## History, versions and pins

Under a hub the Sinopia library has no git history beside it: a board's versions, the return to one and the version a
guest link is pinned to are the `design` collection's revisions in the center, whichever machine made them and whether
the owner's machine is on (`internal/design/revisions.go`).

- A version id is the revision number as 40 hex digits (`%040x`), so the hub's and the gateway's links and `--at` take it
  like a commit; a version pinned in git before keeps opening from the old repository while it is there.
- `history?prefix=<project>/` lists a project's newest revisions; `snapshot?prefix=&at=` is the project as it was at a
  revision, which a pinned guest link is unpacked from.
- Returning to a version writes its contents over the board; the sync sends them as a new revision, so the history only
  grows.
- In the store's library an owner file without a person makes no one its owner by the Unix user (every machine may run
  as the same one): the hub's record of the project decides.
- An embedded store or no store keeps the git history as before.

## A write on a stale copy

The hub takes a write only on top of the revision the writer started from. When two machines change the same file, the
second to arrive does not overwrite the first: the hub's version takes the file's place, and the losing contents go to
`.conflicts/<path>.<machine>.<time>` in the library with a notice beside them. The runtime tells the agent that wrote
them; a small edit of a board is only kept for that agent to re-apply, a bigger one is also saved as a project of its
own, `<project>-variant-…`.

## The runtime

`RUNTIME_STORE` picks where the runtime's store is ([configuration](../configuration.md#design-library)):

| Value | What happens |
| --- | --- |
| `auto` (default) | a hub's machine syncs with its hub's store: `machine.json` in the data folder, or the observe link of an agent that also serves a cluster (`AGENT_HUB_OBSERVE_FILE`, `<data folder>/hub/machine.json`); a runtime paired with a cluster's agent and no hub leaves its data as it is; a runtime on its own keeps a store inside |
| `hub` | only the hub's store; a machine that is not a hub's syncs nothing |
| `embedded` | the store inside the process, whatever else there is |
| `off` | nothing syncs |

`RUNTIME_STORE_LIBRARY=off` keeps the store linked but leaves the Sinopia library out of it (and its history in git):
for a machine whose library another sync still keeps, such as the design-sync agent of a team's `/srv`, until that one
is retired, so two syncs of one folder never run side by side.

The store inside the runtime lives in `<data folder>/store`; what the sync knows of each folder is in
`<data folder>/store-sync`. The library folder can move (another workspace): it is synced from where it is now.

## Drivers

| | Revisions | Contents |
| --- | --- | --- |
| By default | SQLite in `HUB_STORE_DIR` (`<HUB_DATA_DIR>/store`) | the same folder |
| A hub on Postgres | the hub's database, in tables of their own (`filestore_rooms`, schemas `filestore_r_*`); through `HUB_DB_MIGRATE_URL` when it is set, since the store's schemas need a session `search_path` that a pooler in transaction mode drops | |
| `HUB_STORAGE=s3` | | the hub's bucket under `HUB_STORE_S3_PREFIX` (default `<HUB_S3_PREFIX>store/`) |

The store reuses the hub's database and bucket settings and has none of its own unless asked:

| Variable | Default | Meaning |
| --- | --- | --- |
| `HUB_STORE` | `on` | `off`: no store, no capability, no routes; machines keep their data as before |
| `HUB_STORE_DIR` | `<HUB_DATA_DIR>/store` | the SQLite databases and, without S3, the blobs |
| `HUB_STORE_DATABASE_URL` | the hub's database | another database for the revisions, `sqlite:///folder` or `postgres://…`. A Postgres named here is read in the layout of a design-sync center (`ds_rooms`, `r_*`) |
| `HUB_STORE_S3_ENDPOINT` | not set | a bucket of the store's own, with `HUB_STORE_S3_REGION`, `HUB_STORE_S3_BUCKET`, `HUB_STORE_S3_PREFIX`, `HUB_STORE_S3_ACCESS_KEY`, `HUB_STORE_S3_SECRET_KEY` (path style): a design-sync center's blobs, while the hub's other files stay on its disk |
| `HUB_STORE_S3_BUCKET` | `HUB_S3_BUCKET` | another bucket of the same S3 for the blobs |
| `HUB_STORE_S3_PREFIX` | `<HUB_S3_PREFIX>store/` | where in the bucket the blobs are; a blob is `<prefix>rooms/<room>/blobs/<sha[:2]>/<sha>` |
| `HUB_STORE_LEGACY_SPACE` | not set | a space's slug: its library is the room `design` itself, the one a design-sync center kept |
| `HUB_STORE_PROFILE_ROOMS` | not set | `<user id>=profile-sergey,<user id>=profile-rsk`: the rooms a design-sync center kept people's Claude profiles in, bound by user id (not by email, which an account may merely claim); the person's machines sync those instead of `profile-<user id>` |

`hub check-config` prints where the store is.

## The Claude profile

A person's Claude Code profile is a collection of its own, `profile`, and one room per person whatever space the
machine is in: `profile-<user id>`, or the design-sync room `HUB_STORE_PROFILE_ROOMS` binds to their user id. Only the
machines the person made open it; a machine nobody made opens none. The room holds `~/.claude/settings.json`,
`CLAUDE.md`, `keybindings.json` and the `skills`, `agents`, `commands`, `rules`, `output-styles` and `hooks` folders;
never a secret: `.gitconfig`, glab's config and the `RUNTIME_SECRET_CONFIG_DIRS` folders stay out (`.config` is never
walked), as do files that
look like keys (`.env`, `*.pem`, `*.key`), and the settings' environment variables named or valued like a token stay on
each machine. Secrets go through the hub's safe.

The contents are sealed on the machine (XChaCha20-Poly1305, the path bound in) with the room key, which the hub's safe
keeps in the person's folder as `profile-room-key`: the hub's store never sees a profile in clear. A design-sync room
keeps its key: the first machine of the person that still has design-sync's identity
(`agent-*/identity.age` in the folder `RUNTIME_PROFILE_STATE_DIR` names) opens the key wrapped for it (`GET …/collections/profile/keys`)
and puts it in the safe; a room nobody keyed and that holds nothing gets a new key.

A runtime syncs the profile with `RUNTIME_PROFILE_SYNC=hub` (the machine's profile sets it with `profileSync: hub`, and
host-activate then stops design-sync's `design-sync-profile-*` units). It installs and updates the plugins the settings
enable with `claude plugin …` and has the open Claude tabs reload them.

## An existing design-sync center

A hub opens the database and the bucket of a design-sync center as they are, without moving a file: point
`HUB_STORE_DATABASE_URL` at its Postgres, `HUB_STORE_S3_*` at its blobs (`HUB_STORE_S3_ENDPOINT` and the center's keys
when the hub keeps its own files on disk), and name the space whose library it was in `HUB_STORE_LEGACY_SPACE`. The
schema (`ds_rooms`, `r_*`, the same tables and the `design_sync` notify channel) and the blob keys are design-sync 0.7's. The center keeps working beside the hub; the leases of its rooms
are the same. The center's own tokens and members are not read: a machine reaches the store with its hub key.

## Backup, collection, moving

```sh
hub store backup --dir /backup/store        # a copy a hub (or migrate) can start from; blobs are copied by hash
hub store gc --keep 20 --days 30 --dry-run  # count what old revisions and unreferenced blobs would go
hub store gc --keep 20 --days 30            # and delete them (a blob is kept a day after nothing refers to it)
hub store migrate --from /path/to/store --rooms design --into-room design-<space id>
```

`backup` takes a consistent snapshot of the SQLite databases while the hub keeps writing, then the blobs; with Postgres,
back the database up as you do the hub's ([migrations](migrations.md)) and the bucket with the bucket's own tools.

`migrate` copies another store — the one a runtime kept inside itself (`<data folder>/store`), or another hub's — into
this hub's, revision by revision, blobs by hash, and checks the copy. `--from-db` and `--from-dir` name a source whose
database and files are apart. Running it again copies only what is new.
