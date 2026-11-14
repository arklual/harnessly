# Notices in a database

The notices at the top of the window (a chat waits for an answer, a turn is done, a failure, a limit), what the person
read or took away, and the switches of Settings → Notifications live in a database, not in the page's localStorage.
The page keeps localStorage only as a cache for the first paint and for a dropped connection.

One package, `internal/notices`, with one storage interface (`notices.Store`) and one implementation over SQL
(`notices.SQL`) for both sides:

- **hub**: Postgres, tables `notice_items` and `notice_settings` (migration `0068_notices.sql`), behind the rollout
  switch `hub-notices` ([rollout-switches.md](rollout-switches.md)). The person is the hub user, so every machine and
  every device of the person sees one set. Off: the hub passes the requests to the machine.
- **runtime**: a SQLite file `notices.db` in the data folder, or the Postgres of `RUNTIME_STORE_DB` when that names one.
  Always on: a runtime with no hub keeps its notices through a restart. The person is named by the page (the `person`
  query parameter), as the page names it for its cache.

## The page's routes (`<platform>` = `/api/<name>`)

| Route | |
|---|---|
| `GET <platform>/notifications` | `{items, reads, settings}`: the unread items, the latest reads, the switches at a revision |
| `POST <platform>/notifications/items` | `{key, chatId, at, doc, from}` a new notice; a repeat of a key changes nothing |
| `POST <platform>/notifications/read` | `{keys}` or `{chatId}` or `{all:true}`, `from`: read or taken away |
| `PUT <platform>/notifications/settings` | `{doc, rev, from}` the switches over the revision read (409 when it moved on) |
| `GET <platform>/notifications/stream` | `text/event-stream`: `hello`, then `new`, `read`, `settings` events of the person |

A read on one device is a `read` event on the stream of every device of the person; `from` is the sender's device id so
it can skip its own echo.

## The slot of questions (agents and apps)

The smart notice's slot (`internal/noticeslot`) writes every open question into the runtime's store as it opens and
marks it answered, dismissed or expired as it ends; what a restart left open is marked expired at the next start (the
call that waited for it is gone with the process). `GET <platform>/notices/stream` tells the page when the slot changed,
so the page does not poll `/notices` every second and a half while the stream lives; it asks every 30 s as a
fallback and at the old pace while the stream is down.

## The page

The page (`web/app/next/stores/notices-sync.ts`) asks one path, `<platform>/notifications`, and whoever answers (the hub with the switch on, the runtime otherwise) is the same to it. It loads the state, sends a new notice (`/items`) and a notice taken away (`/read`), saves the switches (`/settings`), and applies the person's stream (`/stream`) from the other devices, skipping its own echo by the device id. localStorage stays a cache for the first paint.

The smart notice's slot has its own stream, `<platform>/notices/stream` (event `slot`): while it is open the page looks at the slot once in 30 seconds instead of every 1.5 seconds; with no stream the calm poll stays as before. The runtime keeps the slot's open questions in the same store under the reserved person `_slot`; a restart ends what a gone call asked. `RUNTIME_STORE_DB` (a `postgres://` address) puts the runtime's store in Postgres, otherwise it is `notices.db` (SQLite) in the data directory.

## News of the hub in the bell

A request for access to a design and the decision on it still go by mail, and now also into the bell: `designaccess` calls `noticehub.Service.Ring`, which puts a notice of kind `access` into the hub notices of the same people (the owner, the managers, the editors when editors may share; the decision goes to the one who asked). The key is stable (`access-request:<request id>`, `access-decision:<request id>`), so a repeat never shows twice; the notice has no chat and carries a `link` to the page of the project's requests. It is put only for a person with the switch `hub-notices` on for that machine; for the others it is the mail alone, as before.
