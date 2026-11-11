# Several hub replicas

`HUB_REPLICAS=on` lets several hub processes share one Postgres and serve the same machines. Without the flag the hub
behaves as before: one process, the links of the machines in its memory, nothing written to the directory below.

## What it does

A machine dials one replica, and its WebSocket lives in that process. The replicas keep a directory in Postgres,
the table `machine_links`:

| column | meaning |
|---|---|
| `machine_id` | the machine (one row each, never deleted, so the fence only grows) |
| `owner`, `addr` | the replica that holds the link and the address the others reach it at |
| `fence` | the fencing token: greater by one every time a replica takes the machine over |
| `lease_until` | the owner must renew before this time; the lease lasts 15 s and is renewed every 5 s |

- **Take-over.** A replica that accepts a link writes `owner`, `addr`, a new `lease_until` and `fence = fence + 1` in
  one statement and keeps the returned token with the link. A machine that redials another replica simply takes the row
  over; the old owner finds out at its next renewal (the renewal is `UPDATE … WHERE owner = me AND fence = mine`),
  closes its link with code 4011, and the agent redials normally.
- **Fencing.** Every write a replica makes for a link (hello, last seen, machine info, an event's effects) carries the
  link's owner and token and is applied only if the directory still says so, in the same SQL statement. A replica that
  was paused, or cut from the database, and still holds the agent's socket can therefore write nothing after another
  replica took the machine over: its writes fail with `store.ErrFenced`. If it cannot renew for a whole lease it
  closes the link by itself.
- **A killed replica.** Its links end, the agents redial (through the ingress) and land on a live replica; the dead
  owner's lease expires within 15 s, so no request is sent to it for longer than that, and a clean shutdown releases
  the rows at once.
- **A request for a machine.** `Registry.Get` returns the local link when this replica holds it, otherwise a stand-in
  for the link of the owner named by the directory (looked up at most once a second per machine). `Call` on it is a
  `POST /internal/replica/v1/call` to the owner, which runs the call on the real link and returns the result or the
  agent's own error; `OpenConn` (the tunnel to the machine's runtime, used by the proxy) is a WebSocket to
  `/internal/replica/v1/tunnel` that the owner pipes into a tunnel of the link. `Revoke` and `Disable` of a machine held
  elsewhere reach the owner through `/internal/replica/v1/close`. `Online` is the union of the local links and the live
  rows of the directory.
- Every request of the hop carries the shared secret in `X-Replica-Secret` (checked in constant time) and the fencing
  token the caller saw; an owner that no longer holds that token answers «gone», and the caller treats the machine as
  offline.

## Why an internal hop and not LISTEN/NOTIFY

Both were considered. NOTIFY carries small messages only, one way, without delivery to a replica that is not listening
at that moment, and cannot carry a tunnel, which is a stream of bytes (the proxy opens one for every request to a
machine). Using it would still need a second path for the answers and another for the tunnels. An HTTP request from
replica to replica needs one more thing, an address the replicas reach each other at, and a Kubernetes pod always has one
(`HUB_REPLICA_ADDR`, the pod IP in the chart). The hop is simple, observable with ordinary tools and keeps the agent's
own answers and errors intact.

## Configuration

| variable | |
|---|---|
| `HUB_REPLICAS` | `on` / `off` (default `off`) |
| `HUB_REPLICA_LISTEN` | where this replica serves the others, a listener of its own (default `0.0.0.0:8041`); must not be `HUB_LISTEN` |
| `HUB_REPLICA_ADDR` | required with the flag: where the other replicas reach that listener, e.g. `http://10.0.0.5:8041` |
| `HUB_REPLICA_SECRET` | required with the flag, at least 16 characters, the same on every replica |
| `HUB_REPLICA_ID` | optional name of the replica (random by default) |
| `HUB_SHUTDOWN_GRACE` | how long a stop may take in all (default `20s`); keep it under the time the process is given to stop |
| `HUB_DRAIN_SETTLE` | how long a stopping replica stays not ready and serving before it touches a link (default `5s` with the flag, `0` without) |
| `HUB_DRAIN_LINKS` | the window its machines are asked to dial again over (default `10s` with the flag, `0` without) |

The replicas need Postgres (`HUB_DATABASE_URL`): SQLite is a file of one process. They also need the same
`HUB_SECRET_KEY` (the stored SSH keys are sealed with it) and the same configuration. The replicas talk at
`/internal/replica/` on `HUB_REPLICA_LISTEN` only: the public listener answers 404 there. Let nothing but the replicas
reach that port (a NetworkPolicy, a firewall): the network is the first lock. The second is a signature on every
request, an HMAC with `HUB_REPLICA_SECRET` over the method, the path, the time and the body, so the secret itself does
not travel and a request kept for more than half a minute is refused. All replicas must run a build that signs: do not
roll from an older one with the flag on.

The Helm chart does all of it: with `database.postgres.enabled` or `database.external` and `replicaCount` above 1 (the
default is 2) it sets `strategy: RollingUpdate` with `maxUnavailable: 0`, passes the pod IP and a secret it generates
once, opens the replicas' port on a Service of its own with a NetworkPolicy for the hub's pods only, and leaves the data volume out. Without a shared database it stays one replica with `Recreate`, as before. `strategy.type` names the
strategy outright; the chart refuses `RollingUpdate` on the SQLite file or with one replica, `HUB_REPLICAS` turned off
in `extraEnv` under several replicas, and a `terminationGracePeriodSeconds` too short for the drain below. The hub's
pods prefer nodes apart from each other and from the bundled Postgres (`antiAffinity`, `required: true` to insist).

## A replica that stops

On SIGTERM a replica does not cut what it serves:

1. `/readyz` answers 503 (`/healthz` stays 200) and a machine that dials is answered 409, which an agent reads as
   «dial again» and does within a second. Requests and the links it holds go on for `HUB_DRAIN_SETTLE` (5 s), the time
   the Service and the ingress need to stop sending it requests.
2. It asks its machines to dial again one at a time, spread over `HUB_DRAIN_LINKS` (10 s) with a random pause between
   two of them, so they reach the other replica one by one and not as a herd.
3. It ends its loops and the event streams (a page's stream reconnects by itself) and waits for the requests under
   way, all within `HUB_SHUTDOWN_GRACE` (20 s; the chart sets it to `terminationGracePeriodSeconds` less 5).

Both waits are 0 without `HUB_REPLICAS`: a single process has nowhere to send its machines and stops at once.

## What crosses between replicas

- **Event streams.** What one replica tells its streams (a space's events, the workspace page's pokes, an
  organization's events, and the kicks of a person who left) it also sends over Postgres `LISTEN/NOTIFY`, on a channel
  of the hub's schema, and the others give it to their streams. Nothing is kept: a notification a replica did not hear
  is lost, which costs the page a refetch, as a lost stream does. An event longer than 7900 bytes stays with its
  replica (`harnessly_hub_events_relay_dropped_total`). The end of a person's streams (they left a space, their role
  changed) is sent before any event and tried again when the database did not take it; it is still a notification, so
  a replica that was not listening at that moment keeps the stream until the page reconnects. `LISTEN` needs a connection that stays: behind a pooler in
  transaction mode it uses `HUB_DB_MIGRATE_URL`, and without one the streams hear only their own replica (the hub
  says so at the start).
- **Cursors.** The history cursor of a channel moves only from the value its transaction read; the transaction that
  lost to another replica's is run again from its read (`store.TxRetry`, four tries). The cursor of a machine's spend
  never moves back within an epoch.
- **A link the directory did not take.** The entry is tried three times; a link that still has none is closed, and the
  agent dials again.

## What is still in the process

The abuse limits count per replica (the real ceiling is the number of replicas times the limit), and the caches are
per replica. A machine's last reported state (`machines.info`) is read, changed and written by the replica that holds
its link, without a version.

## Migration

`0022_machine_links.sql` only adds a table. The previous revision of the hub ignores it, so a rolling upgrade from it works.
