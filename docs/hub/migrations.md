# Database migrations, rollback and the schema check

The hub keeps its data in SQLite (one file) or PostgreSQL (`HUB_DATABASE_URL`). The embedded migrations
(`internal/hub/store/migrations/NNNN_name.sql`) run when a hub starts, `hub seed` and `hub machine-code` included.

## What happens at start

1. The hub takes a lock, so that replicas which start together migrate **once**: the others wait, find the work done and go on.
2. It looks at `schema_migrations`. If the database holds migrations this binary does not know, see
   [a database newer than the binary](#a-database-newer-than-the-binary).
3. It applies the migrations not applied yet, each in its own transaction, together with its row in `schema_migrations`.
4. It compares the real schema (tables, columns with types and `NOT NULL`, primary keys, indexes) with the snapshot of
   this version, kept in the repository ([`internal/hub/store/schema/`](../../internal/hub/store/schema)). Extra tables,
   columns and indexes are fine; a missing or different one is **schema drift**: someone changed the schema by hand, or a
   migration file was edited after it had been applied.

`HUB_DB_CHECK` says what the hub does about the two problems below:

| `HUB_DB_CHECK` | Newer database, drift |
| --- | --- |
| `warn` (default) | writes a warning with the code to the log and starts, as hubs did before the checks existed |
| `strict` | refuses to start: prints `CODE: text` and exits with the code's exit status |
| `off` | does not look |

| Code | Exit status | Meaning |
| --- | --- | --- |
| `DB_NEWER_THAN_BINARY` | 70 | the database was migrated by a newer hub |
| `DB_SCHEMA_DRIFT` | 71 | the schema differs from the one the migrations produce |
| `DB_SCHEMA_MISMATCH` | 72 | `HUB_DB_SCHEMA` is set but the connections do not use it (a pooler dropped `search_path`) |
| `DB_MIGRATE_LOCK_TIMEOUT` | 73 | another replica held the migration lock for two minutes (nothing is migrating: look for the session that holds the advisory lock in `pg_locks`) |

Exit status 2 is a bad configuration, 1 anything else. `strict` is the right setting for a production hub: a supervisor
(Kubernetes, systemd) restarts a crashed hub, and the code in the log says that restarting will not help.

## A database newer than the binary

**Rolling the binary back does not roll the data back.** The newer hub has applied its migrations; the older one does not
know the tables and columns they made. With `HUB_DB_CHECK=strict` the older hub refuses to start with
`DB_NEWER_THAN_BINARY` and names the migrations. Your options:

- start the newer hub version again (a rollback of the binary alone is not available once it migrated);
- or restore the database from the backup made **before** the upgrade, then start the older hub. Make that backup
  every time before you upgrade a hub;
- or, if the newer migrations are marked compatible (below), the older hub starts.

### expand / contract: migrations an older hub survives

A release must keep working on the database of the next one for as long as a rollback is wanted. Write migrations in
two steps, in two releases:

1. **Expand** (release N): add what the new code needs and nothing that the old code reads — a new table, a nullable
   column or one with a default, an index. The old code ignores them. Mark the file with its first line:

   ```sql
   -- compat: previous
   ALTER TABLE spaces ADD COLUMN archived_at BIGINT;
   ```

   `compat: previous` is a promise that the **previous** release runs correctly on the database after this migration.
   An older hub that finds only such migrations it does not know starts (and logs them).
2. **Contract** (release N+1 or later, once nobody rolls back to N-1): drop the old column, add `NOT NULL` or a
   constraint the old code would break, rename. These are **not** marked, and a hub older than them refuses to start.

Between the two, backfill data in the application or in a migration of release N+1, never in the same step that
makes the old code fail. Do not edit a migration file after a release; add a new one (the drift check would report the edit).
The marker is stored in `schema_migrations.compat_prev` when the migration is applied.

## PostgreSQL settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `HUB_DB_SCHEMA` | the connection's `search_path` (usually `public`) | The Postgres schema for the hub's tables. Created if missing. Lower-case letters, digits, `_` |
| `HUB_DB_POOLER` | not set | `transaction`: a connection pooler in transaction mode (PgBouncer) sits in front of the database |
| `HUB_DB_MIGRATE_URL` | not set | A direct (not pooled) `postgres://` URL used for the migrations only |
| `HUB_DB_CHECK` | `warn` | `warn`, `strict` or `off`, see above |

None of them changes a hub that does not set it.

### Choosing the lock

Migrations run under a PostgreSQL advisory lock, one number per `HUB_DB_SCHEMA`.

- Direct connection (the default, and with `HUB_DB_MIGRATE_URL`): a **session** lock is held on one connection for the
  whole run. The wait is bounded (two minutes), then `DB_MIGRATE_LOCK_TIMEOUT`.
- `HUB_DB_POOLER=transaction` without a direct URL: a session lock cannot be held, because PgBouncer gives every
  transaction a different server connection (the lock would stay on a connection that is handed to someone else, and
  the unlock would go to another). The hub therefore takes a **transaction** lock (`pg_advisory_xact_lock`) at the start
  of each migration step, in the same transaction as the step, and re-checks `schema_migrations` after getting it. It
  is released by the commit, on any pooler. This is the default for pooled setups because it needs nothing else.
- `HUB_DB_POOLER=transaction` with `HUB_DB_MIGRATE_URL`: the migrations use the direct URL and a session lock; the
  pooled URL serves the running hub. Prefer this when a migration is long (an index on a big table): one connection
  holds the lock through the whole run, and migrations may use statements that need a session (`CREATE INDEX CONCURRENTLY`
  cannot run in a transaction at all; keep such a step out of the embedded files).

### PgBouncer in transaction mode

Set `HUB_DB_POOLER=transaction`. The hub then speaks the simple protocol (no prepared statements, no statement cache),
which is what a transaction pool needs. In PgBouncer: `pool_mode = transaction`; PgBouncer 1.21+ can also keep prepared
statements with `max_prepared_statements`, but the hub does not depend on it.

With `HUB_DB_SCHEMA` the hub sends `search_path` as a connection parameter. PgBouncer passes it on only with
`track_extra_parameters = IntervalStyle, search_path` (1.22+) and with `search_path` **not** in `ignore_startup_parameters`.
Otherwise set it on the role: `ALTER ROLE hub SET search_path = hub`. The role setting is the reliable way: Postgres
applies it to every server connection, whatever the pooler does with startup parameters, and PgBouncer keeps a pool per
role. A tracked parameter is fine while all clients of one role use the same schema (one install = one schema); do not
point clients with different schemas at the same role. The hub checks it before it creates a table; if the
connections do not use the schema it exits with `DB_SCHEMA_MISMATCH` and has created nothing.

## For developers

- A new migration: add `NNNN_name.sql`, then run `make hub-schema` (with `HUB_TEST_POSTGRES_URL` set to refresh the
  Postgres snapshot too) and commit the changed `schema/*.json`. `TestSchemaSnapshot*` fails until you do.
- The tests that need PostgreSQL read `HUB_TEST_POSTGRES_URL`; the pooler ones also `HUB_TEST_PGBOUNCER_URL` (a PgBouncer in
  transaction mode with `track_extra_parameters = IntervalStyle, search_path`) and `HUB_TEST_PGBOUNCER_PLAIN_URL` (one with
  `search_path` in `ignore_startup_parameters`). The tests make a login role per test (`ALTER ROLE … SET search_path`). Without the variables they skip; the `hub-db` CI job starts all three.
