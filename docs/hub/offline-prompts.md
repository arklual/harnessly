# Messages to a machine that is off

A message a person writes in the workspace (`/agents/`) to a machine that is off waits in the hub (table
`offline_prompts`, migration `0033_offline_prompts.sql`), survives closing the tab, shows on their other devices, and
the hub hands it to the machine's runtime when the machine connects. The API is the cluster gateway's, under the page's
platform API (`internal/hub/proxy/outbox.go`):

| Request | Answer |
|---|---|
| `POST offline-prompts?machine=<id>` `{tabId, text, send}` | `201 {item}`; the same `clientId` again returns the kept item |
| `GET offline-prompts?machine=<id>&tab=<tab>` | `{items}`, oldest first; delivered ones stay listed 10 more minutes |
| `DELETE offline-prompts?machine=<id>&client=<clientId>` (or `&id=`) | `{item, outcome}`: `cancelled` — it waited (`queued`/`failed`) and is gone, the hub never hands it over; `delivered` — the machine has it, the row stays. `409 {code: "sending"}` while the hub hands it over (ask again), `404` when the hub keeps no such message |

`send` is the page's own v2 send command (`opId` = `clientId`, `conversationId` = `tabId`, no attachments); anything else
is `400 bad-send`. At most 20 waiting messages per chat and 200 per machine (waiting and failed); delivered and failed items go after a day. Only the owner sees or removes their items.

## Delivery

`Service.RunOutbox` runs every 5 s (`DeliverOutbox` is one pass). It reads only the machines connected to the hub (so a machine that stays off never crowds out another). For a connected machine it marks the oldest message
`sending` and posts the stored `send` to the machine's `/api/v2/cmd/send` with `X-Harnessly-Device: hub-outbox`, one at a
time per machine, stopping for the machine at the first message it did not take:

- `accepted`, `queued`, `refused too-late` — `delivered`;
- `refused machine-offline|timeout|internal|rate-limited`, 5xx, no link — back to `queued`;
- `refused conflict` — `failed` («Этот чат продолжен на другой машине»); other refusals and 4xx — `failed` with the code.

A message left `sending` for 2 minutes (the hub restarted) goes back to `queued`; the machine takes one opId once.

## New chats while no machine answers

When none of the person's machines answers, the start page leaves the new chat's first message with the hub instead of
sending `conversation.create` to a machine (`internal/hub/proxy/outbox_chats.go`, table `offline_chats`). The page shows
it at once as a chat «Ждёт машину» (sidebar row and screen at `/c/queued:<item id>`), on every device of the person's.

| Request | Answer |
|---|---|
| `POST offline-chats?machine=<id>` `{create}` | `201 {item}`; the same `clientId` again returns the kept item |
| `GET offline-chats` | `{items}` of the person, every machine, oldest first; delivered ones stay listed 10 more minutes |
| `DELETE offline-chats?id=<item id>` | `{item}`; only `queued`/`failed` can be taken back, else `409` |

`create` is the page's own v2 `conversation.create`: one chat in a `cwd` or a machine's `projectId`, `opId` = the first
message's `clientId`, nothing attached (attachments are HRN-1037), no `launch`, `parentId`, `title` or `test`; anything
else is `400 bad-create`. At most 20 waiting chats per person.

`Service.RunOutboxChats` runs every 5 s (`DeliverOutboxChats` is one pass): for a connected machine it posts the stored
command to `/api/v2/cmd/conversation.create` (`X-Harnessly-Device: hub-outbox`). `accepted` with the chat's `id` is
`delivered` and the item keeps `convId`; the page that shows the waiting chat opens that chat. `refused
machine-offline|timeout|internal|rate-limited`, 5xx or no link wait for the next pass; other refusals are `failed`. The
machine answers one opId once, so a pass that is retried never makes the chat twice.

## A machine that is on but silent: the last lists

A machine whose link is up can still not answer (a stuck runtime, a full disk). The page then waited the whole header
wait for every list it reads. The rollout switch **`fleet-stale`** ([rollout-switches.md](rollout-switches.md), per
person and machine; no row means off, and off changes nothing) makes the hub answer instead:

- The hub keeps the last good `200 application/json` answer (at most 256 KB, 64 MB all together, 10 minutes) of
  `GET …/api/v2/{hello,agents,providers,apps,conversations}` and `conversations/{id}/{made,tasks}`, under the person, the
  machine, the path and the query. A copy is never shown to anyone else. Long polls and event streams are never kept.
- A machine that refuses or does not answer is marked silent for 20 s. Meanwhile, and while its link is down, such a
  list is answered from the copy; a list with a copy waits 8 s for the machine, not the full header wait.
- The copy carries `X-Harnessly-Stale: <age in seconds>` and `Cache-Control: no-store`; the page shows it as stale.
  There is no copy for a person's first read: the answer is the ordinary one (`machine_offline` or the page).
- Any answer from the machine ends the mark. The person stays on their machine: nothing is ever sent to another one.
