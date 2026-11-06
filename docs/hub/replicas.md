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
| `HUB_REPLICA_ADDR` | required with the flag: where the other replicas reach this one, e.g. `http://10.0.0.5:8040` |
| `HUB_REPLICA_SECRET` | required with the flag, at least 16 characters, the same on every replica |
| `HUB_REPLICA_ID` | optional name of the replica (random by default) |

The replicas need Postgres (`HUB_DATABASE_URL`): SQLite is a file of one process. They also need the same
`HUB_SECRET_KEY` (the stored SSH keys are sealed with it) and the same configuration. Keep `/internal/replica/` off the
public ingress (the secret is the second lock, not the first).

The Helm chart does all of it: with `database.postgres.enabled` or `database.external` and `replicaCount` above 1 (the
default is 2) it sets `strategy: RollingUpdate` with `maxUnavailable: 0`, passes the pod IP and a secret it generates
once, and leaves the data volume out. Without a shared database it stays one replica with `Recreate`, as before.

## What is still in the process

The directory covers the machine links. Other state lives in the process and is per replica: the open event streams of
people (a person sees the events that happened on the replica serving their stream, plus what that replica reads
from the database), the abuse limits (they count per replica, so the real ceiling is the number of replicas times the
limit), and the in-memory caches. A deployment that needs exact limits or cross-replica event streams can pin sign-in
traffic with the ingress; moving these into the database is not part of this change.

## Migration

`0022_machine_links.sql` only adds a table. The previous revision of the hub ignores it, so a rolling upgrade from it works.
