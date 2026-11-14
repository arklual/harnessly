# Search from the database (⌘K)

HRN-1420, stage 3 of «the hub is the source of truth». ⌘K used to read the library of the machine; now the hub (and a
runtime without a hub) answers from an index in the database, so a machine that is off or slow does not stop it.

## Route

`GET <platform api>/search?q=<text>[&limit=N]` answers `{"hits":[{"kind","id","parent","title","updatedAt"}]}`.
Kinds: `design` (a project of the library), `app` (an app of the team), `kind` and `doc` (document types of apps and
their documents). On the hub it sits under any machine of the space the person may open and is behind the rollout
switch `hub-search` (off: 404 `search_off`; the web then asks the machine as before). A runtime without a hub always
serves it.

## Rights

The same as the library: a design is found only if the person has a role in the project (design access, with the team
level counted only for people who may open the machine the project is recorded on), so the project of a
private machine is not found by those who cannot open it. Apps are open to the members of the space.

## Index

`internal/searchindex` is one package with a storage interface. The hub keeps the table `search_docs` (migration 0070)
in its own database (Postgres: `pg_trgm` and trigram indexes are made at start when the role may; SQLite: `LIKE`),
the runtime keeps a file next to its data. The match is on normalized text (lowercase, ё=е, only letters and digits); all
words of the question must be found, a title match ranks first, newer wins. 10 000 documents answer in well under 400 ms
(`TestTenThousandUnder400ms`).

It is updated when the store is written, not when someone searches: `filestore.Server.ObserveRooms` hears every commit,
`internal/hub/searchsync` marks the project (or the team file of the apps), and two seconds later reads it once with the
code the library uses. At the start the hub builds the index of a space that has none yet.

## Apps' types and documents, and records

- Behind `hub-apps-catalog` the hub answers `GET <platform>/apps/kinds` and `/apps/kinds/docs` from its database
  (`designaccess/apps_catalog.go`): the types come from the packages of the space's team in the apps collection
  (`appsview.View.Kinds`), the documents are the recorded projects the person has a role in whose kind is not one of
  Sinopia's own. Off, the hub leaves both routes to the machine.
- The records of systems, videos and decks (collections `systems`, `videos`, `decks`) are indexed as designs by
  their title and kind even when the design collection holds no files of the entity (`searchsync.Sync.Record`).
