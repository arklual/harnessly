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

| `HUB_ENV_APPS` | `on` (the default): every organization has the apps «Доски» and «Документы» while the hub has the address of their service. `off`: no built-in apps ([Built-in apps](environment.md#built-in-apps)). |
| `HUB_BOARDS_APP_REPO`, `HUB_BOARDS_APP_BRANCH`, `HUB_ARTIFACTS_APP_REPO`, `HUB_ARTIFACTS_APP_BRANCH` | Where the machines take the two packages from, for a mirror in a closed network: an https address of a repository with the folder `harnessly-app`, and a branch or a tag. Unset: the services' public repositories, `main`. |

The public addresses go to the machines with the environment's apps, as `${install:boardsUrl}` and
`${install:artifactsUrl}` of their packages ([Your own server](../build-apps/03-backend.md#secrets-once-more)); the hub
itself keeps calling `HUB_BOARDS_URL` / `HUB_ARTIFACTS_URL`, which may be an address inside the cluster. An installation
without an address has no such app; with one, every organization has the app with no setting up.

### Rights for the documents service

The documents service knows the person from the platform's token (`sub`), and the hub keeps who has access. The
boards service asks the same with its own secret (`HUB_BOARD_EVENTS_SECRET`) and, for a person signed in on the boards
site, `email` in place of `sub`: only an address a person of the hub has proved names them. It asks:

```
POST /api/platform/docs/_rights
Authorization: Bearer <HUB_ARTIFACTS_ACCESS_TOKEN>

{"sub": "<sub>", "slugs": ["a", "b"]}
→ {"rights": {"a": "edit", "b": "none"}}
```

The answer also names in `known` the slugs the hub keeps a record of: the boards service applies the hub's word to those
and keeps its own rules for the rest. Up to 200 slugs. Each right is `none`, `view`, `comment` or `edit`, by the same rules as the person's own page (the
owner and the administrators read as `edit`). A document the hub has no record of, one out of the person's reach and
every document of a person the hub does not know are `none`. A missing or wrong token is 401. The service may keep an
answer for up to 10 seconds.

A link without sign-in to a board opens on the boards service, `<HUB_BOARDS_PUBLIC_URL>/b/<slug>?g=<token>`, while the
hub has `HUB_BOARD_EVENTS_SECRET` and the public address; the service asks what the link allows:

```
POST /api/platform/docs/_guest
Authorization: Bearer <HUB_BOARD_EVENTS_SECRET or HUB_ARTIFACTS_ACCESS_TOKEN>

{"token": "<the link's token>", "slug": "<board>", "open": true}
→ {"slug", "rights": "view" | "comment", "forWhom", "linkId", "expiresAt"}
```

A token nobody made, one for another slug, one switched off or run out and a board in the trash are `404`. `open: true`
counts an opening of the link. A guest with `comment` comments on the board under the name the link was made for; with
`view` they only read. A document's link stays the hub's page, `/d/<token>`.

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

## Make, copy, rename and the trash

The hub owns the records of the installation's documents and boards, so documents and boards are made, copied, renamed
and put into the trash through it (`internal/hub/docs/manage.go`); the hub then asks the documents or boards
service with its own token. The rights are the boards' ones:

| Route | Body | Who | Answer |
| --- | --- | --- | --- |
| `POST <p>` | `{kind?: "artifact" \| "board", title?, markdown? \| html? \| template?, spaceId?}` | anybody in the space | `201 {doc}` |
| `POST <p>/{slug}/duplicate` | `{title?, spaceId?}` (may be empty) | who may read it | `201 {doc}` |
| `PATCH <p>/{slug}` | `{title}` (2 to 200 characters) | who may edit it | `200 {doc}` |
| `DELETE <p>/{slug}` | none | the owner or an administrator | `200 {slug, kind, trashedAt, trashedBy}` |
| `POST <p>/{slug}/restore` | none | the owner or an administrator | `200 {doc}` |
| `GET <p>/_trash` | none | the owner (an administrator sees all) | `200 {docs: [{slug, kind, title, owner, trashedAt, trashedBy}]}` |

`<p>` is `<platform api>/docs` (`/api/platform/docs`). `{doc}` is the record the list shows (`slug, kind, title, url,
owner, openThreads, lastActivityAt, …`). A new document or board belongs to whoever made it, sits in `spaceId` or in the
person's only space (several spaces and no `spaceId`: `400 space_required` naming them) and the space edits it. A
document is made from `html`, from `markdown`, from a `template` (`meeting`, `plan`, `review`, the app's three) or, with
none, as an empty page with its title; a board from the boards service's `template`. A copy takes the current version
of a document or the scene of a board as it is now and gets the title `<title> (копия)` unless one is given. A thing in
the trash opens for nobody, a guest link included, and leaves every list. The services close it themselves too: a board
through `POST /b/api/boards/{slug}/trash|restore`, a document through `POST <documents>/api/v1/artifacts/{slug}/trash
{trashed, by}` with the hub's editor token, after which `/a/{slug}`, `/embed/{slug}` and the API answer `404` to all
but that token. The documents service may not answer: the hub hides the document anyway, answers
`serviceConfirmed: false`, writes the failure to its log and keeps `doc_meta.trash_sync` set; the leader sends the current
state again at start and every minute (`trashsync.go`) until the service confirms it. Nothing is deleted for good: the
trash keeps everything until it is taken out.

The agent of the owner's machine reaches the same trash through its sharing route (`POST /docs/acl` of the machine,
`route` ending in `/trash`, body `{trashed: true|false}`): the machine's owner must own the document.

The documents and boards apps call the same routes on a person's behalf: `Authorization: Bearer <HUB_ARTIFACTS_ACCESS_TOKEN
or HUB_BOARD_EVENTS_SECRET>` and `X-Harnessly-Token: <the person's platform token>` — the `harnessly-app+jwt` the hub
signed for that service (audience: the documents' or boards' origin), alive. The secret alone names nobody; a token for
another audience, an expired one or one of a person switched off is `401`; a service acts with the person's own rights,
never as an administrator. Putting a document into the trash revokes its public link; taking it out does not bring the
link back.

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

## Mentions on a board

A comment on a board that calls a person by `@Name` rings that person's bell in harnessly, whoever drew the
board and whatever the owner's `hub-board-events` switch. The boards service names the people a message calls in
`message.mentions`: `{name, agent: true}` for `@harnessly`, `{name, id}` for a person of the board. It keeps an id only
for the people who were on the board signed in (opened it, edited it, commented), so a mention never shows a board to
someone who has not had it; a name typed by hand that is not one of them stays text. The id is:

- `hub:<user id>` for a person signed in from the harnessly app view (`board_signin`): their id in this hub;
- `mail:<hex>` for a person signed in any other way: HMAC-SHA256 of the lower-case e-mail keyed with the webhook secret
  (`HUB_BOARD_EVENTS_SECRET`, the same as the boards service's). The hub finds its own user with that e-mail; nobody
  without the secret can read the id, and no e-mail travels for a mention.

The hub tells each person once per message (`mention:<slug>:<message id>`), never the author (`message.authorId`), not
the agent, not a person the hub does not know or has switched off, at most ten a message, and only for
`thread.created` and `thread.replied`. The notice is of kind `access` with an `@` icon, no chat: «Упоминание на доске
«<title>»», the author and the comment's first 140 characters, and a link inside the page,
`/apps/harnessly.boards?o=<slug>&th=<thread>`, which opens the board in the boards app with the thread's card open. It
is put only for a person with the switch `hub-notices` on for every machine (`*`); without it there is nothing to show
it in yet. An older boards service sent plain names in `mentions`: they name nobody.

## @harnessly on a board

A person who calls `@harnessly` in a comment (`mentions` has `{agent: true}`, or an older service left `@harnessly` in
the words) gets the agent with the board closed (`internal/hub/docs/board_call.go`):

- a board no conversation drew (made in the boards app) is bound to the caller's machine seen last, neither revoked nor
  switched off; the caller is `message.authorId` (`hub:`/`mail:` as for mentions) or `authorEmail` of a hub user. A
  caller the hub does not know, or one without a machine, binds nothing, and the event goes as before;
- the binding names no chat yet (`tab_id` and `session_name` `board-call`): the machine starts a conversation for the
  first comment and the hub keeps the id it answers (`tabId`), so the next comments go to the same chat. A board bound
  this way is taken by the hub without the `hub-board-events` switch, replies without `@harnessly` too;
- the comment goes as any comment on a bound board: typed into the chat, queued while it works, tried every minute
  while the machine is away, given up after a day. While it waits the caller's bell says so once
  (`wait:<comment id>`, «Ждёт машину · «<title>»»);
- the agent answers in the thread through the boards command or the app's tools, as the person; its answer rings the
  bell of the person the board is bound to (`agent:<slug>:<message id>`, «Агент ответил на доске «<title>»»), with the
  link that opens the thread.

The documents service sends its comments to the same endpoint in the same shape with `"kind": "doc"`
(`ARTIFACTS_HUB_EVENTS_URL`, the same secret): the hub keeps a document's threads and binding under the key
`doc/<slug>`, tells the agent to answer with the documents' tools (`doc_reply`, `quick_apply`, `doc_edit`), and links the
bell to `/apps/harnessly.docs?o=<slug>&th=<thread>`. A document of the hub's own (`/docs/<slug>`) called with
`@harnessly` is bound the same way, to its machine or the caller's.

## What it does not do yet

- Editing a block in place (`/source/block`) answers 501 `not_supported`; save the whole source instead.
- A comment written on a board in the hub's own page is not sent to the boards service; the boards service is where boards are commented.
- Documents are shared by the whole installation, not separated by space.
- Several replicas of the bundled services: they keep files on one volume. Use S3 storage for artifacts and run it
  yourself (`docs.artifacts.url`).

## The installer

`harnessly install` (branch `agent/742-installer`) sets up a machine, not a hub, so it needs nothing from this
page. When it grows a hub mode, the hub's compose files are `compose.hub.yaml` plus `compose.docs.yaml`.
