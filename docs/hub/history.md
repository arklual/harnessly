# Chats in the hub

The hub keeps every person's chats — the list and each chat's feed — so the workspace opens them whole while the
machine they ran on is off, rebooting or gone. The machine stays what runs the chat: sending a prompt, the live turn,
the terminal and the preview still need it. Reading does not (everything a person looks at is read from the
center).

## How a chat gets here

1. The machine's agent says `history` in its `hello` (a machine of the hub with a runtime next to it; an observe link of
   a cluster's machine only when the operator opted in, see below).
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

## A chat deleted on its machine

The machine owns its chats and the hub's rows are a copy ([data-owners.md](data-owners.md)). When a chat is deleted on
the machine, the runtime's `conversation_removed` marks the copy (`history_conversations.deleted_at`): the chat leaves
the list, the feed and the search at once, and its rows stay for `HUB_HISTORY_DELETED_DAYS` (30 by default; `0` keeps
them for good). After that a background sweep removes the chat's entries and turns, then its row and the cursor of its
channel. A chat that is not marked is never touched, and a chat the machine sends again loses the mark.

The sweep is gentle on purpose, the first pass over an old database included: a statement removes at most 500 rows,
statements are 200 ms apart, and a pass stops at 20 000 rows or 50 chats; passes are ten minutes apart, the first one
two minutes after the start. With several replicas one of them holds the job (`job_leases`, the lease outlives the
time between passes) and the others skip their passes until it stops coming. `harnessly_hub_history_chats_purged_total`,
`harnessly_hub_history_rows_purged_total` and `harnessly_hub_history_purge_errors_total` show it.

The mark comes only from the live event. A delete the hub never got as an event (the runtime restarted before the hub
had read its `convs` channel that far) leaves the copy unmarked and listed until the chat list is reconciled; that
reconciliation is not built yet.

## A machine the hub only observes

A machine run by a closed cluster is linked to the hub as an observer and keeps handing its chats to the cluster. The
operator hands them to the hub as well with `AGENT_HUB_HISTORY=on` in the agent's environment. Without the
variable nothing changes: the observe link offers no `history` and answers `history.start` as unsupported. The kill
switch turns it off like the rest of the link.

The hub's copy is a second consumer of the runtime's replica, and the two never share a thing (a replica has state: a
cursor, an ack and a baseline per channel, and a new `start` throws that state away). The agent runs a replica of its own
for the hub, on its own socket to the runtime (`/api/v2/replica` serves every connection apart, each with its own
`hello` and chat list), and its frames go to the hub link only. The cluster's `v2.start`, `v2.ack` and its stream are
never touched by the hub's `history.start`, `history.ack`, a hole in the hub's channel or a hub link that went away,
and the other way round. The cost is a second reader on the runtime: its events are published once more, and the hub's
backfill reads the pages of the chats again at the backfill's pace.

The hub takes it as from any machine with the feature: the owner is the person who added the machine, and the chats are
read as those of a machine of theirs (`GET history/conversations`), though the machine itself «only observes» and
does not open.

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

## Work of a chat

The «Changes» tab shows the git work of a chat and of the chats it delegated (delegate_task), on any machine, open or
closed. The work is registered, not guessed from the chat's text:

- **On the machine.** The runtime's observer (`internal/chatwork`) follows the chats' feeds: a file a write tool wrote
  registers its worktree; a folder a shell command named becomes work when the command changed git (a commit, a new
  worktree or branch, a push) and the chat committed there; at the end of each turn every worktree of the chat is
  looked at again (head, branch, uncommitted changes, the commits made since the chat began, the origin, a merge
  request a push or `glab mr create` printed). The records live in `<data>/chatwork.json`.
- **Old chats.** The first time the tab asks (`POST <platform>/work {conversationId}`), the transcripts of the chat and
  of its delegated chats on that machine are looked through once for the same signs; what they show is registered as
  `scan` work.
- **To the hub.** The replica socket hands the records to the agent (`{"k":"work"}`: all of them when it opens, then
  each change); the agent forwards them as `chatwork` events and sends everything it keeps again when a link subscribes.
  The hub keeps the newest record of each chat's worktree in `chat_work`, under the machine's owner.
- **Reading.** `GET <platform api>/history/conversations/<id>/work` is the person's work of the chat and of the chats
  delegated from it at any depth (by `history_conversations.parent_id`, filled from the records, and by the work's own
  parent), on the person's machines. The page merges it with the machine's own answer. A worktree's diff is read on its
  machine (`POST <platform>/changes {paths, sinceMs}`); when the machine is off or the worktree is gone and the branch
  is pushed, `POST <platform>/work/remote {remote, branch}` reads the branch against its base from the code host through
  the connection on the remote's host.

The draft remarks of the «Ревью кода» app are kept here too (`review_notes`):
`GET …/history/conversations/<id>/review-notes` lists the person's remarks on the chat's work, `PUT …/review-notes/<note>`
keeps one (new or changed, at most 500 a chat) and `DELETE` forgets it. They are read and written under the person's id
only, so nobody else sees them; sending them to the agent is the page's job, one message into the chat's queue.
