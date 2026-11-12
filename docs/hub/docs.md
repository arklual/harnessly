# Documents and boards

The hub keeps what people say about a document: the threads, the comments, who may see and edit it, what each person
has read. The documents live in the **artifacts** service and the boards in the **boards** service; the hub talks to
both with one bearer token. Agents publish with `doc.publish` and change access with `doc.acl`; the web reads
`/api/platform/docs`.

It is off by default (`HUB_DOCS=off`). A hub without it is complete: the document screens say "not set up" and name the
missing setting, and an agent's publish gets the same answer instead of a 404.

## On a clean install

`compose.hub.yaml` alone starts a hub and no documents: the feature needs the **artifacts** service, which is a program of
its own with its own image and storage, not part of the hub, so the hub's compose file cannot switch it on by itself.
By hand there are two things, and the overlay does both:

1. a token in `.env`: `DOCS_TOKEN` (16 characters or more; the default in the file is for a trial on one host only);
2. the overlay in the command: `docker compose -f compose.hub.yaml -f compose.docs.yaml up -d --build`. It sets
   `HUB_DOCS=on`, `HUB_ARTIFACTS_URL` and `HUB_ARTIFACTS_TOKEN` on the hub and starts the artifacts service. There is
   no published artifacts image yet, so the first start builds it from its repository: the host needs Docker's build and
   access to that repository.

Nothing else is needed for documents; boards stay optional (see below). Until then the page of a document shows the
hub's own sentence, which names the missing setting (`HUB_DOCS`, `HUB_ARTIFACTS_URL` or `HUB_ARTIFACTS_TOKEN`), and
`GET /api/platform/docs/_status` says the same without signing in.

## Settings

| Variable | Meaning |
| --- | --- |
| `HUB_DOCS` | `on` turns the feature on. |
| `HUB_DOCS_LEGACY_HOSTS` | Comma-separated hosts that answer the old document address `/a/<slug>` with a permanent redirect to `<public URL>/agents/docs/<slug>`. Nothing is served there: the page decides, by sign-in and the document's own access. Empty: none. |
| `HUB_ARTIFACTS_URL`, `HUB_ARTIFACTS_TOKEN` | The artifacts service and its editor token. Without them documents answer `docs_not_configured`. |
| `HUB_BOARDS_URL`, `HUB_BOARDS_TOKEN` | The boards service. Optional: without them the hub offers documents only. |
| `HUB_ARTIFACTS_ACCESS_TOKEN` | The documents service's own secret for asking the hub what a person may do with documents (`POST /api/platform/docs/_rights`, below). A secret of its own, not `HUB_ARTIFACTS_TOKEN`. Unset: the route answers 404. |
| `HUB_BOARD_EVENTS_SECRET` | The secret the boards service sends with each comment event of a board (`POST /api/platform/docs/_board-events`, "Comments on a board" below). Unset: the route answers 404. |
| `HUB_BOARD_EVENTS_FORWARD_URL` | Where an event goes that the hub does not take (board unknown to the hub, or the owner's `hub-board-events` switch off), with the same body and secret. Unset: such an event is answered and dropped. |
| `HUB_BOARD_COMMAND` | The name of the boards tool the agent is told to answer a comment with. Unset: `board`. |
| `HUB_BOARDS_PUBLIC_URL`, `HUB_ARTIFACTS_PUBLIC_URL` | The services' addresses as machines and browsers reach them, `https://`. Unset: the same as `HUB_BOARDS_URL` / `HUB_ARTIFACTS_URL`. |

The public addresses go to the machines with the environment's apps, as `${install:boardsUrl}` and
`${install:artifactsUrl}` of their packages ([Your own server](../build-apps/03-backend.md#secrets-once-more)); the hub
itself keeps calling `HUB_BOARDS_URL` / `HUB_ARTIFACTS_URL`, which may be an address inside the cluster. An installation
without an address has no such app.

### Rights for the documents service

The documents service knows the person from the platform's token (`sub`), and the hub keeps who has access. It asks:

```
POST /api/platform/docs/_rights
Authorization: Bearer <HUB_ARTIFACTS_ACCESS_TOKEN>

{"sub": "<sub>", "slugs": ["a", "b"]}
→ {"rights": {"a": "edit", "b": "none"}}
```

Up to 200 slugs. Each right is `none`, `view`, `comment` or `edit`, by the same rules as the person's own page (the
owner and the administrators read as `edit`). A document the hub has no record of, one out of the person's reach and
every document of a person the hub does not know are `none`. A missing or wrong token is 401. The service may keep an
answer for up to 10 seconds.

`GET /api/platform/docs/_status` tells what is set up and why not, without signing anything.

### When a service is down

Every hub process asks the documents and the boards service for `GET /readyz` every 30 seconds (3 second timeout)
and keeps the answer; `_status` has it as `services: {documents|boards: {up, since, checkedAt}}`, and the scrape as
`harnessly_hub_app_service_up{service}`. The page asks `_status`, never the service: while a service is down its app
shows «сейчас не открывается» with «Повторить» in place of the view, and opens the view by itself once the hub sees
the service again. A service down for 5 minutes gets one mail to the administrators of the installation
(`HUB_ADMIN_EMAILS` and the accounts marked admin), and one more when it is back; of several replicas only the holder
of a lease mails. `hub doctor` checks the same (`HUB-E002`). The machine's proxy of an app's MCP server answers a call
to a server that does not answer, or answers 502/503/504, with a tool result marked as an error: «the service is
unavailable, try again later», not a transport's error.

### Documents that belong to no organization

On a hub with several organizations, a document or a board that nothing ties to one is closed to everybody but its
author and the administrators, and the hub says how many there are when it starts. It is one the hub has no record of
in a space (it was published straight to the documents service, or it is a board) whose named author is not an
address a member of some space has proved. The migration that gave documents a space could not place these: it
moves the hub's own records, and these have none.

`POST /api/platform/docs/_adopt` (an administrator of the installation; 404 for anyone else) places them in a space:

```json
{ "spaceId": "<space>", "authors": ["", "Display Name"], "apply": false }
```

It takes the documents whose author is in `authors` exactly as the services name it (`""` is a document that names
nobody), and no others: an author's name is something somebody typed, so the hub does not place a document on its own
guess. Without `"apply": true` it is a dry run: nothing is written. The answer is `{ spaceId, applied, adopted:
[{ slug, kind, author }], left: [{ author, count, member }], leftCount }`; `left` is what still belongs to no
organization, counted by author, to decide what to list next, and `member` says the author goes by the account
address of a member of the space (a hint: the address is not proved to be theirs). A placed document the hub had no record of stays
readable by the people of its space, as it was meant to be, and has no owner; a document that already has a space is
never moved.

## Run it

- **docker compose:** `docker compose -f compose.hub.yaml -f compose.docs.yaml up -d --build` adds the artifacts service
  (built from its repository the first time). Set `DOCS_TOKEN` in `.env`. Boards are the `boards` profile, because no
  boards image is published yet: build one, set `BOARDS_IMAGE`, add `--profile boards`.
- **Helm:** `--set docs.enabled=true` runs artifacts in the release (one replica, files on a volume) and makes the
  token. `docs.boards.bundled=true` plus `docs.boards.image.repository` adds boards. For services you run yourself set
  `docs.artifacts.bundled=false`, `docs.artifacts.url` and `docs.existingSecret` (a Secret with the key `token`).

Why it is **off by default** in the chart: it is two more services with a token to keep, and a hub that only runs agents
does not need them. The chart refuses to render when it is on but cannot work (no artifacts service, boards bundled
without an image, a service of yours without its token).

## Rights

The owner of a document (the person whose machine published it) and the hub's administrators can do everything. The people
of the document's organization get the level the owner gave the team: none, view, comment or edit. A document's record
names its space (`doc_meta.space_id`): the space of the machine that published it. The organization is the one of that
space; a record from before records named a space, whose space could not be told (no machine, and an owner in no space
or in several), names none and goes by the machine that published it or, when the hub does not know the machine, by
the organizations of its owner, until its owner publishes it from a machine again. Every query of the records carries
a condition on the space, and a machine does not publish over a document of another space;
people of other organizations get nothing from the team level, only what a share by name or a guest link gives. A
board, and a document the hub has no record of, is viewable by the people of its author's organizations, when the
author the service names is an address a person of the hub has proved (an account that merely signs in under the
address, with a provider that does not vouch for it, is not the author). What nothing ties to an organization is viewable by
everybody of the hub's only organization; on a hub with several it is closed to all but its author and the
administrators, and the hub's log names such documents once at the start, so that an administrator shares them.
`GET <platform api>/docs/people?q=` finds the people to name in a comment: among those the person shares a space with
(an administrator of the installation finds anybody), by two characters or more, 120 searches a minute per person.
Agent visibility maps to levels: `private` none, `team` or `view` view, `comment`, `edit`, and
`public` view with a public link.

## Comments reach the conversation

A document published from a conversation is bound to it: after the publish the agent sends `docs.bound` with the tab,
session and working directory (a board is bound the same way, with `kind` `board`: see "Comments on a board"). When a person comments, the hub hands the
comment to that conversation through the machine with `docs.deliver`, with the quoted place, the last comments of the
thread and how to answer. The agent answers with the `reply` and `resolve` commands of its document tool, which reach the hub as `docs.reply`
and show in the thread as "Агент".

A comment the machine could not take (offline, timed out) is tried again every minute; after a day it is marked
undelivered and the thread shows why (`deliveryError`). A delivered comment carries `deliveredAt`.

On a machine linked to the hub as an observer the cluster keeps its documents unless the operator sets
`AGENT_HUB_DOCS=on` there.

## Comments on a board

The boards service tells the hub about every change of a board's comments: it posts the event to
`<platform api>/docs/_board-events` with the secret in the `X-Boards-Webhook-Secret` header (`HUB_BOARD_EVENTS_SECRET`; without
it the route answers 404). The body is `{event, slug, thread: {id, resolved?, anchor?: {elementId, label, x, y}}, message?: {id, text,
author, authorEmail, authorKind}}` with `event` one of `thread.created`, `thread.replied`, `thread.resolved`,
`thread.reopened`, `thread.removed`. A wrong secret answers 403, a body that is not JSON or lacks `event`, `slug` (lower case
letters, digits and dashes, 3 to 121) or `thread.id` answers 400, and an event dealt with answers `202 {"outcome": "applied" |
"ignored" | "forwarded"}`.

The hub takes the event only for a board it knows the conversation of: when the agent publishes a board it sends `docs.bound`
with `kind` `board`, and the hub keeps the slug, the machine, the owner and the conversation in `board_bindings` (never the
board's key). It takes it only while the rollout switch `hub-board-events` is on for the owner of the machine
([rollout-switches.md](rollout-switches.md)). Then the thread and its comments are mirrored into `doc_threads` and
`doc_comments` by the ids the boards service gave (`external_id`: the same event twice changes nothing), `thread.resolved` and
`thread.reopened` set the status, `thread.removed` deletes the thread. A comment of a person goes to the bound conversation with
`docs.deliver` like a comment on a document, in the words the old gateway used: the board, the shape the thread sits on, the
earlier comments and how to answer with `<command> comment <slug> --reply <thread>` or `--resolve <thread>` (`HUB_BOARD_COMMAND`, default `board`); the agent's answer
goes to the boards service itself, which sends the event back (`authorKind` `agent`) and the hub shows it in the thread. A guest who
left a name only is "гость" in the thread and the person's name when the author has an address.

An event the hub does not take (the board is unknown, or the owner's switch is off) is passed on to `HUB_BOARD_EVENTS_FORWARD_URL`
with the same body and secret when it is set, and dropped when it is not: the boards service does not retry. This lets the
address of the webhook move to the hub first and the boards move over one person at a time.

The machine must serve documents to send the binding: always on a machine's own hub link, on an observer link only with
`AGENT_HUB_DOCS=on`.

## What it does not do yet

- Editing a block in place (`/source/block`) answers 501 `not_supported`; save the whole source instead.
- A comment written on a board in the hub's own page is not sent to the boards service; the boards service is where boards are commented.
- Documents are shared by the whole installation, not separated by space.
- A public link to a board is not supported.
- Several replicas of the bundled services: they keep files on one volume. Use S3 storage for artifacts and run it
  yourself (`docs.artifacts.url`).

## The installer

`harnessly install` (branch `agent/742-installer`) sets up a machine, not a hub, so it needs nothing from this
page. When it grows a hub mode, the hub's compose files are `compose.hub.yaml` plus `compose.docs.yaml`.
