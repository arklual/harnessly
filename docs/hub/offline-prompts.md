# Messages to a machine that is off

A message a person writes in the workspace (`/agents/`) to a machine that is off waits in the hub (table
`offline_prompts`, migration `0033_offline_prompts.sql`), survives closing the tab, shows on their other devices, and
the hub hands it to the machine's runtime when the machine connects. The API is the cluster gateway's, under the page's
platform API (`internal/hub/proxy/outbox.go`):

| Request | Answer |
|---|---|
| `POST offline-prompts?machine=<id>` `{tabId, text, send}` | `201 {item}`; the same `clientId` again returns the kept item |
| `GET offline-prompts?machine=<id>&tab=<tab>` | `{items}`, oldest first; delivered ones stay listed 10 more minutes |
| `DELETE offline-prompts?machine=<id>&client=<clientId>` (or `&id=`) | `{item}`; only `queued`/`failed` can be taken back, else `409` |

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
