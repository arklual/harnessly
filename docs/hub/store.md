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
| `design` | the Sinopia library: projects, boards, artifacts. Hidden files do not sync, except a project's `.owner.json` and the shelves in `.folders/` |
| `uploads` | files people put into a chat, pictures and attachments |
| `files` | any other file an agent or a person keeps |
| `handoff` | what one machine passes to another |
| `apps` | the files of a space's apps |

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

A blob read by its hash is the same bytes forever: it is served with `ETag` and an immutable cache.

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
| `auto` (default) | a hub's machine (`machine.json` in the data folder) syncs with its hub's store; a runtime paired with a cluster's agent (`AGENT_SECRET_FILE` or `AGENT_CLUSTER_URL`) leaves its data as it is; a runtime on its own keeps a store inside |
| `hub` | only the hub's store; a machine that is not a hub's syncs nothing |
| `embedded` | the store inside the process, whatever else there is |
| `off` | nothing syncs |

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
| `HUB_STORE_S3_BUCKET` | `HUB_S3_BUCKET` | another bucket of the same S3 for the blobs |
| `HUB_STORE_S3_PREFIX` | `<HUB_S3_PREFIX>store/` | where in the bucket the blobs are; a blob is `<prefix>rooms/<room>/blobs/<sha[:2]>/<sha>` |
| `HUB_STORE_LEGACY_SPACE` | not set | a space's slug: its library is the room `design` itself, the one a design-sync center kept |

`hub check-config` prints where the store is.

## An existing design-sync center

A hub opens the database and the bucket of a design-sync center as they are, without moving a file: point
`HUB_STORE_DATABASE_URL` at its Postgres, `HUB_STORE_S3_BUCKET` and `HUB_STORE_S3_PREFIX` at its blobs, and name the
space whose library it was in `HUB_STORE_LEGACY_SPACE`. The center keeps working beside the hub; the leases of its rooms
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
