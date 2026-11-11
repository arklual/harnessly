# Replication of a machine's data over the link

A machine owns several kinds of data the hub keeps a copy of (the register is `internal/hub/store/owners.go`,
[data-owners.md](data-owners.md)). This page is how each of them reaches the hub, and the one protocol they move to.
The map in code is `repl.Kinds` (`internal/hub/repl/kinds.go`); a test fails when a kind the register gives to the
machine has no transport there.

## Map: kind → how it travels now

| Kind (register) | Transport | Wire | Cursor / ack | Retry | When the hub is behind |
|---|---|---|---|---|---|
| chat history, live events (`history.events`) | **batches** (old: event) | `repl.batch` + `repl.ack`; inside, `history.ack` of the channel | the channel's epoch and seq (`history_cursors`) + the batch number | go-back-N from the oldest unacknowledged batch, pause 1 s doubling to 1 min | the agent holds the batch (window 16); old agents: frame dropped from the bulk queue, `history.start` again |
| chat history, backfill (`history.backfill`) | **batches** (old: event) | `repl.batch` + `repl.ack` | the walk cursor of each chat (`history.start.pending`) + the batch number | same as above; old agents: none (a page not stored was lost) | the backfill goroutine waits for room up to 1 min; old agents: dropped |
| spend (`usage`) | pull | `usage.available` → `usage.read` → `usage.ack` | `ingest_cursors` (epoch, seq) | a failed pull is tried again after the same pause (1 s doubling to 1 min) | the outbox waits on the machine |
| what a machine reports about itself (`metrics.sample`, `env.state`, `env.run`, `restart`) | **batches** (old: event) | `repl.batch` + `repl.ack`, data = the old event frame | the last report replaces the one before | go-back-N on a lost batch | events wait on the machine (256 per kind, the oldest dropped past it); old agents: the event queue, a full queue closes the link |
| documents' events (`doc.publish`, `doc.acl`, `docs.bound`, `docs.reply`) | **batches** (old: event) | same; `doc.result` answers publish and acl as before | the request id | go-back-N on a lost batch | same |
| designs (`design`) | **batches** (old: event) | same; `design.access` back | none | go-back-N on a lost batch | same |
| automation results (`automations.result`) | **batches** (old: not sent to a hub link at all) | same; the hub checks the run's one-use token | the run id | go-back-N on a lost batch | same |
| agent memory | HTTP | `/agent/memory/pull`, `/agent/memory/put` signed by the machine | per note revision | the machine's own timer | — |
| chat attachments | HTTP | the page puts the bytes into the hub (`/uploads/blobs`, `/uploads/refs`) | sha256 | the page | — |
| the machine's `hello` | answer to a call | `hello` | replaced on every link | the next link | — |

## The protocol (`hubproto.FeatureRepl`)

1. The agent says `repl` in its `hello` features.
2. The hub answers `repl.start {window, kinds}` before anything of the kinds is asked for (`history.start`, `env.set`, `design.access`). From then on the agent sends each named kind as
   `{"event":"repl.batch","kind","seq","data"}`, where `seq` counts the kind's batches on this link from 1 and `data`
   is the payload of the old event as it was (`HistoryEvents`, `HistoryBackfill`).
3. The hub handles the batches of a link in order. It stores batch `seq = done+1` and answers
   `repl.ack {kind, seq}`; a batch stored before is acknowledged again; a batch after a hole is not stored (the order
   holds, the agent sends from the hole when its ack does not come). A batch the hub could not store gets
   `repl.ack {kind, seq: done, retry: true}`; a batch that can never be stored is acknowledged and skipped.
4. The agent keeps at most `window` batches of a kind unacknowledged. Live events never wait (they run under the
   replica's lock; the channel's own `history.ack` sends them again); a page of the backfill waits for room up to a
   minute. Unacknowledged batches go again from the oldest after 10 s, and after the hub's retry with a pause of
   1 s doubling up to 1 min.
5. A new link, or a new `repl.start`, numbers every kind from 1 again.

With the window, a compliant agent never fills the hub's bulk queue, so nothing is dropped there. If a batch is
dropped anyway, the hub does not call `history.start` again for that link: the hole is sent again by the agent.

## Compatibility

The protocol is chosen per link by the features in `hello`:

- new agent, new hub: batches;
- new agent, old hub: no `repl.start` comes, the agent sends `history.events` and `history.backfill` as before;
- old agent, new hub: the agent does not say `repl`, the hub takes the old events (bulk queue, drop, `history.start`
  after a drop) as before.

## Lag

`harnessly_hub_repl_lag_seconds{kind}` is the age of the oldest data of a kind the hub was sent (or told of) and has
not stored yet: for batches, since the oldest batch not stored was read; for old history frames, the age of the oldest
frame of the bulk queue; for spend, the longest wait of a machine's outbox. Every kind of the map with a lag is written,
at 0 while nothing waits. `harnessly_hub_repl_batches_total{kind,result}` counts batches by `stored`, `retry`,
`duplicate`, `gap` and `bad`.

The older gauges (`history_lag_seconds`, `usage_lag_seconds`) stay until the dashboards move.

## Events as batches

An event moved to batches keeps its frame: the batch's data is the old event as it was, and the hub hands it to the
same handler as the event (`machines.Service.dispatch`). Its senders cannot be asked to send again, so a batch of an
event is acknowledged once handled; what it gains is the window held on the machine and the resend of a frame lost on
the way. The windows of all the kinds together (13 × 16) stay below the hub's bulk queue (256), checked by a test.

## Not moved

- Agent memory travels over HTTP, not the link: it moves to a pull by the hub like the spend (a task of its own).
- `job`, `login`, `agents`, `preflight`, `policy`: state of the link itself, replaced by the next one; left as events.
