# Chat resume snapshots

A chat goes on on another machine of the same person while the machine it lives on is off. After each
turn the machine leaves a snapshot of the chat in the hub's store; when the person moves the chat and the source does
not answer, the target continues it from that snapshot instead of asking the source.

The snapshot holds the chat's whole history, secrets included, so only the person's own machines read it: not the web,
not another member of the team, not an administrator of the organization or of the instance. The hub keeps it sealed
and cannot open it.

## What a snapshot is

One file per chat, `<machine id>/<chat id>.json` in the person's room `resume-<user id>` of the collection `resume`:

```json
{ "v": 1, "opId": "r…", "at": 1762780000000, "bundleEnc": { "enc": "aes-256-gcm", "nonce": "…", "ct": "…" } }
```

`bundleEnc` is the bundle a chat move carries (`internal/chatmove`: title, history up to 32 MiB, the repository's
remote and commit), gzipped and sealed with AES-256-GCM under the person's resume key, bound to `opId` and the import
step. `opId` is derived from the machine, the chat and its fence, so continuing from one snapshot twice opens the chat
once. A chat that is working, has no history, was moved away or holds more than 32 MiB of history gets no snapshot;
nothing is cut.

The machine sends a snapshot in the background: a chat whose history grew and stayed still for a whole tick (15 s,
the turn is over) is sealed and put once; an unchanged chat is not sent again, across restarts too
(`<data dir>/v2/chat-resume.json` remembers what the store has). The chats that existed when the switch was turned on
get a snapshot after their next turn. A turn never waits on it.

## The key

The person's resume key is 32 random bytes in their own folder of the hub's safe, `me/chat-resume-key`, made by the
first of their machines that needs it (each writes, then reads back what the safe kept). Only the person's machines
read that folder: the same trust as the Claude profile's room key (`store.md`, «The Claude profile»). The store keeps
ciphertext only; the hub never passes the key in a move. The hub's safe must be on (`secrets.vault`).

## Who reads it

The hub's store lets a request at the `resume` collection only with a machine's session (`POST /agent/store`):

| Who | Answer |
| --- | --- |
| a machine of the person | its own room; writes only under its own id, reads the snapshots of the person's other machines, also of one that is off or revoked |
| a machine of another person naming the person's machine | 403 |
| a machine nobody made | 403 |
| the web, a script with a person's cookie, any request without a machine's session | 403 (not 401: no login lets it in) |

`internal/hub/storehub/resume.go`; the tests are in `resume_test.go` beside it.

## Continuing a chat

`POST /api/v2/chat-move` (api.md, «Moving a chat between machines») with the source off: when the flag is on for both
machines' organizations and both machines were made by the person moving the chat, the hub asks the target's runtime
for the step `resume` (`/api/v2/move/resume`, `{sourceMachine, conversationId, provider, profile, model, withoutFiles}`),
and answers `{targetMachineId, targetName, convId, fromSnapshot: true}`. The target reads the snapshot and the key
itself and imports the chat as a move would. A machine the person only administers is never a side of it. The source
is not sealed: when it is back, its copy of the chat is still there as it was, and the two go on apart.

The runtime answers 404 `no_snapshot` when the switch is off on it or the chat has no snapshot, 400 `bad_bundle` when
the snapshot does not open with the person's key.

## Switches

| Where | Switch | Off |
| --- | --- | --- |
| each machine | `RUNTIME_CHAT_RESUME=hub` | nothing is sent, the step `resume` answers 404 |
| the hub, per organization | flag `chat.resume` (with `runtime.fleet`) | a move from a machine that is off is refused, 409 `machine_offline`, as before |

Turn it on for an organization with `PUT /api/orgs/{org}/features/chat.resume` (an instance administrator) and on the
person's machines with `RUNTIME_CHAT_RESUME=hub` in their environment.

An older hub does not list `resume` in the store's collections: a newer runtime sees that, sends nothing and asks
again every ten minutes. An older runtime never asks for the collection, and a newer hub never asks it for `resume`
unless the flag is on; then it answers 404 and the move says so.

## From the web

A chat of a machine that is off is in the workspace from the hub's history; its menu has «Продолжить на другой ВМ» when
the machine was made by the person looking at it and its organization has `chat.resume`. With the source off:

- `GET /api/v2/chat-move/status` answers `{enabled, moved: null, offline: true}`; `enabled` is that rule and nothing
  more.
- `GET /api/v2/chat-move/targets` does not ask the source. `source` is `{ok: true, project: false, dirty: false,
  busy: false, offline: true, fromSnapshot: true}` (nothing is known of its folder), `targets` are only the person's
  machines that are up and for which the continuation is allowed (made by the person, `chat.resume` on in their
  organization), each probed with `preflight` as before. Without the flag, or for a machine of another person, the
  answer stays `409 machine_offline` / `404`.
- The move itself is `POST /api/v2/chat-move`, as above.

The dialog says that the chat goes on from the last saved snapshot and that uncommitted changes of the folder stay on the
machine that is off. The switch «без локальных файлов» is offered but not required: whether the repository and the commit
are on the target is learned by the target when it imports.

The hub still never reads or hands out a snapshot: the web gets no content of it, only the list of targets.

## Not yet

- A chat that has no snapshot (the machine never ran it with `RUNTIME_CHAT_RESUME=hub`) is offered all the same; the
  target answers `no_snapshot` and the dialog shows it.
- Uncommitted changes of the source stay on it; the chat goes on at the last commit the snapshot names.
