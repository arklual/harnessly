# Chats in the hub

The hub keeps every person's chats — the list and each chat's feed — so the workspace opens them whole while the
machine they ran on is off, rebooting or gone. The machine stays what runs the chat: sending a prompt, the live turn,
the terminal and the preview still need it. Reading does not (HRN-877: everything a person looks at is read from the
center).

## How a chat gets here

1. The machine's agent says `history` in its `hello` (a machine of the hub with a runtime next to it; an observe link of
   a cluster's machine does not — the cluster keeps that copy).
2. The hub subscribes it to `history.events` and `history.backfill` and tells it where its copy stands
   (`history.start`): the last seq stored of each channel, the revision of every chat it has, and the chats it does not
   hold whole yet (`pending`), each with the turn its walk goes on before.
3. The agent reads the runtime's replica socket (`/api/v2/replica`) and forwards every event of the conversation
   channels (`convs`, `conv:<id>`) as `history.events`; the hub stores a frame and answers how far the channel is
   stored (`history.ack`). A frame after a hole is not applied: the agent resends from there or starts a new baseline.
4. What the agent no longer has, and every chat the hub lacks, goes as `history.backfill`: the chat list, then the
   pages of turns. A chat is walked from its newest page to its first turn; each page says where it was read from
   (`history.from`), the hub moves the chat's cursor on, and the page with no older turn marks the chat whole. A walk
   cut short (the link went, the runtime restarted) goes on from the cursor next time.

Nothing is sent before the hub's `history.start`. The owner of what a machine sends is the person who added the machine,
never anything a frame says. `HUB_HISTORY=off` switches it off: the hub subscribes to nothing and the workspace asks the
home machine, as before.

## Secrets

Nothing is stored with a secret in it: every event is cut with the rules of the cluster's gateway
(the line redaction of the gateway) before it is read — the same token shapes, the same placeholders
(`[скрыто: <kind>]`), the same order (`internal/hub/history/redact.go`, its test holds the gateway's cases).

## Tables

`history_conversations` (a chat per machine: its record, revision, the cursor of its walk), `history_turns`,
`history_entries` (the wire's turns and entries as JSON) and `history_cursors` (a channel's last seq per machine). Both
SQLite and Postgres; migration `0028_history.sql`.

## What the workspace reads

| Path (under the workspace's platform API, `/agents/api/<platform>/`) | What it answers |
| --- | --- |
| `GET history/conversations` | `enabled`, the person's machines with `online`/`offline`, their chats (newest prompt first) |
| `GET history/conversations/<id>?before=&limit=` | a page of a chat: up to 33 turns (100 at most), oldest first, the steps of the newest three |
| `GET history/conversations/<id>/turns/<turn>/entries` | the steps of one folded turn |

Only chats of machines the person may open and that the person owns are served. A machine the person only
administers (a space owner sees every machine of the space) is not theirs: its chats stay out of the list and the
search, and the list names it under `others` (`id`, `name`, `state`) so the page can offer it. Picking it shows that
machine's own list alone, the way an admin inspects a machine; the search goes only to a machine of the person's own.
The sidebar (projects and favorites) answers `enabled: false` to a person without a machine of their own. Older hubs answer these paths from the
home machine; a newer agent with an older hub sends nothing (the hub never asks).

## Attachments

What a person attaches goes to the machine as before (the agent needs the copy there), and the page puts the same bytes
into the space's `uploads` collection of the hub's store by their sha256 and names them with the machine's upload id
(`PUT uploads/blobs/<sha256>`, then `PUT uploads/refs/<up_…>`; table `upload_refs`, migration `0029_upload_refs.sql`).
A ref is the person's own: what one person names never decides what another reads. The chat reads
`GET uploads/files/<up_…>` from the hub, and an upload the hub has not got from the machine (a 307).

A file the agent hands over (`deliver_file`, `harnessly deliver`) is put by the runtime of a hub's machine into the
space's `files` collection by its sha256, up to 64 MB, in the background. The chat's card saves it from
`GET uploads/delivered/<sha256>?path=&type=&name=&download=1`; one the hub has not got is redirected to the machine's
own `file-preview` of the path, where the machine checks the person again. The collection is the space's machines', so
the hub serves a file only when one of the person's own chats on that machine shows its sha256; anything else gets the
same redirect.

Bytes are read by their hash only by the person who put them (the hub hashes every body itself, also when it has the
bytes already) (`upload_owners`, migration `0030_upload_owners.sql`),
and a ref names only bytes of one's own: the collections are the space's machines', not the person's.

**A machine that is off.** What is attached goes into the hub only, as `hub:<sha256>`; the message waits in the outbox,
and when the machine is back the page reads the bytes from the hub (which gives them only to the person who put them)
and uploads them to the machine as any other file, before the message goes. The machine never reads the space's
collection for a person: what it would read there is not checked against who asks.

**The pane.** A file the pane opens while the machine is not live is read from
`GET uploads/delivered/by-path?machine=&path=`: the person's own turns say which handed-over file is at that path; any
other is sent on to the machine.

The page is told whether to do this by `centralUploads` in its settings (true when the hub has a store).
