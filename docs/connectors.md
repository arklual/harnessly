# Connectors: how to write one

A connector connects the person's mail, calendar, folder, chat bot, feed or incoming webhook to harnessly. Every
connector follows one contract, `internal/connectors`, and adds three things to what harnessly already has:

1. **A way to connect.** A `Spec` with the fields of the form; the secret ones are kept in the machine's safe.
2. **Tools for the agent.** Reads, free (`harnessly connector read`), and actions outward on the person's behalf
   (`harnessly connector act`), which run only after the person's yes on the card of the chat.
3. **A source of events.** A `Poller` or a `Watcher` the runtime runs with a cursor kept on the machine. Its events are
   short envelopes without text, delivered to the hub's rules and to the inbox in the formats they take today.

The reference connector is RSS/Atom: `internal/connectors/rss` (≈350 lines with the parser). Copy it.

## The contract in one screen

```go
package imap

import "github.com/ly-solutions/harnessly/internal/connectors"

type mailConn struct{}

func init() { connectors.Register(mailConn{}) }

func (mailConn) Spec() connectors.Spec {
	return connectors.Spec{
		ID:      "imap",                // stable, [a-z][a-z0-9-]*; names the connector in every event
		Kind:    connectors.Mail,       // mail | calendar | files | chat | feed | webhook: the catalog's group
		Title:   connectors.T("Mail (IMAP)", "Почта (IMAP)"),
		Summary: connectors.T("Any mailbox with IMAP", "Любой ящик с IMAP"),
		Fields: []connectors.Field{
			{ID: "email", Type: connectors.FieldEmail, Label: connectors.T("Address", "Адрес")},
			{ID: "password", Type: connectors.FieldPassword, Secret: true,
				Label: connectors.T("App password", "Пароль приложения")},
		},
		Events:  []string{"mail.received"},
		Tools:   []connectors.Tool{{Name: "message", Summary: "one letter with its text",
			Args: []connectors.Arg{{Name: "id", Required: true}}}},
		Actions: []connectors.Action{{Name: "send", Summary: "send a letter",
			Title: connectors.T("Send the letter", "Отправить письмо"),
			Args:  []connectors.Arg{{Name: "to", Required: true}, {Name: "subject"}, {Name: "text", Required: true}}}},
		Inbox: "mail", // the inbox's source kind, or "" to keep its events out of the inbox
	}
}

// Check is required: try the values against the service, say who they sign in as. Its error is shown to the person.
func (mailConn) Check(ctx context.Context, in connectors.Instance) (connectors.Account, error)

// Reader, when Spec.Tools is not empty.
func (mailConn) Read(ctx context.Context, in connectors.Instance, tool string, args map[string]string) (connectors.Result, error)

// Actor, when Spec.Actions is not empty. Called only after the person's yes, with exactly the proposed args.
func (mailConn) Act(ctx context.Context, in connectors.Instance, action string, args map[string]string) (connectors.Result, error)

// A source, when Spec.Events is not empty: a Poller (asked every Interval)…
func (mailConn) Poll(ctx context.Context, in connectors.Instance, from connectors.Cursor) ([]connectors.Event, connectors.Cursor, error)
func (mailConn) Interval(in connectors.Instance) time.Duration
// …or a Watcher (runs until ctx ends: IMAP IDLE, a bot's long poll, a folder watcher).
func (mailConn) Watch(ctx context.Context, in connectors.Instance, from connectors.Cursor, emit connectors.Emit) error
```

Then import the package for its `init` in `cmd/runtime/routes_connectors.go` (next to `_ ".../connectors/rss"`). That is
all: the catalog in Settings → Connections draws the card and the form from the `Spec`, the runtime keeps the secrets,
runs the source, delivers the events and serves the tools. `Register` panics on a broken `Spec` (a password field that
is not secret, tools without `Read`, events without a source), so the first test of your package sees the mistake.

### More of the form: two ways, discovered choices, a refused action

- **`Field.When{Field, Values}`** shows a field only while a choice field has one of the values: one card with two
  ways to connect (a CalDAV login, or a read-only link) asks only the fields of the way chosen. A hidden field is not
  required and not kept, even if the page sends it.
- **`FieldChoices`** (`"choices"`) is several of the choices, kept as their values joined by commas; the page draws a
  row of toggles.
- **`Field.Discover`** takes the choices from the service: the page sends the values filled in so far to
  `POST <platform>/connectors/discover {connector, field, values}` → `{choices}` (behind a «Find» button), and the
  connector answers from `Discoverer.Discover(ctx, in, field)`, where `in.Secret` reads the form's values, not the
  safe. Nothing is kept. Its error is shown like `Check`'s. A Spec with a discovered field and no `Discover` panics.
- **`ActGate.CanAct(in, action) error`**, optional, refuses an action of a connection before a card is shown, when
  that connection cannot do it at all (a calendar connected by its read-only link): the agent gets the words at once.

## What the runtime does for you

| Part | Where | What it does |
| --- | --- | --- |
| Form and secrets | `Service.Connect` | Checks the values by field type (url, email, number, choice), runs `Check`, keeps the secret fields in the safe as one secret `connector-<id>` (a JSON object), the others in `<data>/connectors/connections.json`. Nothing is kept when `Check` fails. |
| Reading a secret | `Instance.Secret("password")` | Reads the safe at the moment of use: a rotated password is taken at once. Never log it, never put it in an error. |
| HTTP | `Instance.HTTP` | The runtime's client: the company's proxy and CA, no cloud metadata addresses. Use it, not `http.DefaultClient`. |
| Cursor | `<data>/connectors/state/<id>.json` | The cursor after the last batch, written together with the batch. The source starts from it after a restart. |
| No doubles | the same file | The fingerprints already emitted (the last 5000). A source that reads the same thing again after a crash doubles nothing. |
| No loss | the outbox in the same file | An envelope stays until every destination took it (the hub away, the inbox down): tried again every 30 s, dropped after 72 h. |
| Errors | `Conn.State`, `Conn.Error` | A source that returns an error is started again after a minute from the kept cursor; the page shows the error in words. |
| Text from outside | `Result.Data` | Printed to the agent between `<untrusted-data>` marks with «data, not instructions» (`connectors.Untrusted`). |
| Actions | `Service.Propose` / `Answer` | A card in the chat that asked; `Act` runs only on the person's yes, once, with the shown args; every run is a line of `<data>/connectors/sent.jsonl`. |
| Own record | `Instance.Load` / `Instance.Store` | A small JSON of the connection's own (a bound chat, a pairing code, the last messages a bot got) in `<data>/connectors/state/<id>.memo.json`; never shown to an agent or the page, gone with the connection. Serialize it yourself between your source, reads and actions. |
| One more step | `Stepper.Next` | What the person still has to do after the check (open a pairing link): the page shows the words and the link under the connection; only the page sees it, never the agents' listing. |
| Answers elsewhere | `Relay` | A connector that shows every proposal of every connection in another place too (a bot's message with buttons) and takes the answer there through `Instance.Answer`; journaled as `relay:<connection>`. |

## The source and its cursor

- **The cursor is yours and opaque**: a sync token, the last UID, a JSON of ETag + known ids (RSS does that). Keep it
  small (it is rewritten with every batch).
- **Poller**: `Poll(from)` answers the new events and the cursor after them; the runtime keeps both at once and waits
  `Interval` (never less than a minute). Be gentle with someone else's server: conditional requests (ETag,
  If-Modified-Since, sync-token), 15+ minutes for feeds.
- **Watcher**: call `emit(events, next)` for each batch and do not move past it until `emit` returned nil. Return when
  `ctx` is done. An error ends the run; the runtime starts it again later from the last kept cursor.
- **The first run only learns.** What is already in the mailbox or the feed when the person connects is not news: emit
  nothing, keep a cursor (RSS: the first poll keeps the known ids).
- **`Event.ID` is the thing's own stable id** (Message-ID, guid, UID with UIDVALIDITY, path+mtime). The runtime makes the
  fingerprint `<connector>:<connection>:<id>` and delivers each once.

## The envelope

```go
connectors.Event{
	Type:   "mail.received",          // <domain>.<what>, one of Spec.Events
	ID:     "<CAF…@mail.example.com>", // stable id at the source
	At:     sentAt,                   // when it happened; zero is "now"
	Actor:  "Ann <ann@example.com>",  // who did it
	Title:  "Contract for Friday",    // subject or headline, cut at 200 runes. NEVER the body
	URL:    "",                       // a web link the person opens, if there is one
	Thread: threadID,                 // groups a conversation: the inbox's item, the hub's wait key
	Ref:    "message id=<CAF…>",      // the read that shows the whole thing: "<tool> key=value…"
	Mine:   false,                    // the person did it themselves
	Facts:  map[string]string{"hasAttachment": "true"}, // ≤ 12 short facts a rule can filter on, no text
}
```

There is no field for the body or an excerpt, on purpose: the text of a letter or a message never leaves the machine
in an event. The agent reads it on the machine with the read the event names.

Event types in use and planned: `mail.received`, `calendar.changed` (with `change: created|moved|updated|cancelled`), `calendar.starting`, `calendar.day`,
`calendar.ended`, `chat.message`, `chat.mention`, `feed.item`, `webhook.received`, `file.created`, `file.changed`,
`note.created`, `note.task`.

## Where events go

Both destinations get the envelope in the format they take today; there is no second format.

**The hub's rules** (`docs/hub/automations.md`). The runtime hands the event to the machine agent (`POST
/automations/event` on the loopback), which sends `automations.event` on its hub link: the same `{source, fingerprint,
event}` as `POST /automations/hook`, with the event under `data` because `event` names the frame. The hub queues runs
of the **machine owner's** rules only. Without text: the event is

```json
{"type": "feed.item", "connector": "rss", "connection": "cx1a2b3c", "account": "Example blog", "id": "…",
 "at": "2026-11-15T10:00:00Z", "actor": "Ann", "thread": "…", "mine": true,
 "ref": "harnessly connector read cx1a2b3c item id=…", "facts": {"hasAttachment": "true"}}
```

and the hub drops `title`, `excerpt`, `text`, `body`, `content`, `snippet` if a machine sends them anyway. A rule
takes it by `source` = the connector's kind (`mail`, `calendar`, `feed`, `chat`, `files`, `webhook`) and filters on
these fields, e.g. `{"trigger": {"source": "feed", "filter": {"connection": "cx1a2b3c"}}}`; a wait rule keys on
`thread`. The run's agent reads the thing with `ref`. An older hub that does not subscribe `automations.event` gets
nothing; the events wait in the outbox.

**The inbox** (`harnessly-inbox`, `POST /api/ingest`), only for a connector whose `Spec.Inbox` names a kind the inbox
takes today (`mail`, `calendar`, `telegram`, and since harnessly-inbox 0.10.12 `webhook`, `feed`, `folder`). The runtime finds the inbox as the installed app whose start-page view
is a day feed signed in by the machine (`feedSecret`, `docs/apps/day-feed.md`) and posts to `/api/ingest` of the feed's
origin with that token: `{source: {kind, label: account}, signals: [{fingerprint, itemKey, kind: type, actor, title,
url, at, mine}]}` — without `excerpt`. A new kind for the inbox needs the inbox to list it in its `PushKinds` first; until then leave `Inbox` empty. The
incoming webhook (`webhook`), RSS (`feed`) and folder (`folder`) send theirs since harnessly-inbox 0.10.12. The Python collectors of the inbox are not touched by this.

## Tools and actions

- **Reads** are free: `harnessly connector read <connection> <tool> key=value…`. Return what came from outside in
  `Result.Data` (with `From`: "mail from ann@example.com"), your own words in `Result.Text`. The runtime marks `Data`
  as untrusted; never put outside text into `Text`.
- **Actions outward** (send a letter or a message, create or answer a meeting, delete) are `Spec.Actions`. The agent
  runs `harnessly connector act <connection> <action> key=value… --why "…"`; the runtime keeps the proposal and shows a
  card in that chat with the action's `Title` and every argument as it will be used. The person's yes (a page request
  with their session: no agent can make it) runs `Act` with exactly those args, once; no answer in 30 minutes expires
  it. Every run is a line of the journal of sends: when, which connection, which action, the args, which chat asked,
  who confirmed, the outcome (`GET <platform>/connector-sends`). There is no auto-send yet; when it comes, it will be
  the person's switch per chat or address, never a tool's.
- **`Action.Direct`** is the one exception: an action that reaches only the person themselves (a push to their own
  bound chat) runs at once, without the card, and is journaled as `direct`. The connector makes sure it cannot reach
  anyone else and limits how often it runs. Anything to another person is never Direct.
- A tool may write only where nothing leaves the person's own account and nobody else sees it: a draft kept in the
  mailbox's Drafts folder is a read (`imap draft`), a letter sent is an action. Marking read or moving a letter is an
  action too: it changes what the person sees.
- A send to someone who is not a participant of the thread, an attachment from another letter, a link outside the
  thread: put it in the args, so the card shows it.

## More of the contract

Added compatibly; a connector that does not use them changes nothing.

- **`Field.Generate`**: a secret the runtime makes itself (a webhook's key). The form does not ask it; `Connect` fills it
  with 32 random bytes (hex) into the safe like any secret field.
- **Shown once**: the answer of `Connect` (and of `Reissue`) carries `Conn.Shown`, `[{label, value}]`, which is never
  kept: the page shows it in the connection's row with «Copy» and «Saved». A connector says what to show with
  `Revealer.Reveal(in)` (the webhook: the address and the secret); without it, the generated fields are shown.
- **Reissue**: `Service.Reissue(id)`, `POST <platform>/connectors/<id>/reissue`, makes new values of the generated
  fields; the old ones stop working at once. The row has «Reissue» for a connector with a generated field.
- **`Instance.Dir`**: the connection's own folder, `<data>/connectors/data/<id>`, for what a connector keeps beyond the
  cursor (a webhook's bodies, a folder's snapshot and journal). Made by the connector, removed with the connection.
- **`Receiver`**: a source that is pushed to (`Receive(ctx, in, Incoming{Header, Body, Path}) ([]Event, error)`)
  instead of asked. `Service.Receive` keeps its events as a source's batch (repeats dropped, outbox, delivery).
  `ErrUnauthorized` is the sender's 401 without words, `ErrDuplicate` a repeat already taken (200, nothing emitted).

## Incoming webhook (`internal/connectors/webhook`)

**Connect.** Settings → Connections → Webhooks → «Incoming webhook»: the source (GitHub, GitLab, Grafana, Alertmanager,
Sentry, «n8n, Zapier, Make or any JSON»), for Grafana, Alertmanager and any JSON how it proves itself (a header, or
the secret in the address), for any JSON the title and link fields (`data.title`), for Sentry its integration's
Client Secret. The page shows once the address and the secret to give the service; «Reissue» makes a new secret,
«Disconnect» revokes the address. The machine must be linked to a hub: the address is the hub's.

**Registered by itself (GitHub, GitLab; HRN-1727).** The form also takes where to register (`owner/repo`,
`group/project`, `group:<path>` on GitLab, `org:<name>` on GitHub), the service's address for an own GitLab or GitHub
Enterprise, and a token that may add webhooks there (GitLab: Maintainer of the project, Owner of a group; GitHub:
`admin:repo_hook`, `admin:org_hook`). Given them, the connection puts its address at the service itself with the
events the preset reads (GitLab: push, merge requests, pipelines, notes, issues, releases; GitHub: pull requests and
their reviews, issues and comments, check suites, workflow runs, push, releases), signed with its secret; once an hour
it looks for the webhook there and puts it back if it is gone; removing the connection takes it off. A refused token is
said on the page (the source's error), never the token. Without a token the address is given by hand, as before.

**The address and where the body is.** `https://<hub>/api/platform/automations/hook/m/<machine>/<connection>` (with
`/<secret>` when the secret is in the address): the hub's automation hook, next to `/hook`, not a second receiver of
its own. The hub keeps nothing of the request and does not know the secret: it checks the shape of the address,
limits the size (1 MiB, else 413) and the rate (60 a minute per address, 30 at once, else 429), and passes the body
and the headers the presets read over the machine's link (`connectors.webhook`, served while the machine lets the hub
reach the runtime for automations). The body lives in the hub's memory for the time of that call only and is never
logged; the machine away is 503, and the sender tries again. On the machine the runtime checks the proof by the secret
from the safe, in constant time — GitHub `X-Hub-Signature-256`, GitLab `X-Gitlab-Token`, Grafana
`X-Grafana-Alerting-Signature` (with its timestamp header, ±5 min) or a bearer token, Alertmanager a bearer token
(`http_config.authorization`), Sentry `Sentry-Hook-Signature` with `Sentry-Hook-Timestamp` (±5 min), any JSON an
HMAC-SHA256 in `X-Signature-256` / `X-Signature` / `X-Webhook-Signature` (hex or base64, `sha256=` allowed) or a bearer
token, or the secret in the path. A wrong one is 401 without details, the same as an unknown connection. A repeat (the
service's delivery id — `X-GitHub-Delivery`, `X-Gitlab-Event-UUID`, `Request-ID`, `Idempotency-Key` — or the body's
hash, within 72 h) is 200 and nothing more. The body is kept under the connection's folder (`index.jsonl` and
`bodies/<key>.json`, the newest 200, at most 30 days), with the headers that say what it is and never the signature.

**Events.** `webhook.received`, one per request (one per alert of an Alertmanager or Grafana group; a ping gives none),
with the kind as the fact `kind`: `pr.opened|closed|merged|…`, `review.approved`, `issue.opened|assigned|…`,
`comment.created`, `ci.failed|passed|canceled`, `ci.job_failed`, `push`, `release.published`, `alert.firing|resolved`,
`error.created`, `json`. Facts are identifiers only: `repo`, `number`, `branch`, `base`, `assignee`, `reviewer`,
`alert`, `status`, `severity`, `fingerprint`, `project`, `level`, `type`. `thread` groups one thing (`o/r#7`, an
alert's fingerprint, so a wait rule can key «firing without resolved in 20m»). The title (a PR's title, an alert's
name) stays on the machine: the hub drops it. A rule: `{"trigger": {"source": "webhook", "filter": {"facts":
{"kind": "ci.failed"}}}}` — or by `connection`.

**Reads.** `harnessly connector read <connection> events [limit=20]` — the latest requests: when, kind, title, link
and id; `… event id=<id>` — one whole, the body as the service sent it. Both come as untrusted data. No actions.

## MCP recipe (`internal/connectors/mcprecipe`)

A service connected through MCP that gives no events — only tools — becomes a source by a **recipe**: which
read-only tool to call, with which arguments, where the list is in the answer and which fields of a thing are its id,
time, author, title, link. An agent writes the recipe once, when the service is connected (it reads the server's tools
and calls the one that lists things, then maps its answer); from then on the runtime polls by itself, with no model in
the loop.

```json
{"trigger": "generation.done", "tool": "list_jobs", "args": {"status": "done", "since": "{{cursor}}"},
 "items": "data.jobs", "id": "id", "at": "done_at", "actor": "", "title": "prompt", "url": "link",
 "thread": "", "cursor": "done_at", "every": "5m"}
```

- **Connect.** Settings → Connections → Feeds → «MCP recipe»: the MCP server (a choice of the connected ones) and
  the recipe. The check is a dry run: the tool is called once and the form shows how many things it found and the
  first one — what the events will look like.
- **Only reads.** A tool the server marks destructive never runs; one it does not mark read-only is refused unless
  the person allows it in the form.
- **The answer** is the tool's `structuredContent`, else its first text content read as JSON; paths are dotted
  (`data.items`, `items.0.id`); a time is RFC 3339 or Unix seconds or milliseconds. `{{cursor}}` in a string argument
  is the greatest value of the field `cursor` seen so far ("" on the first poll).
- **Events.** `feed.item`, one per thing not seen before, its id `<trigger>:<thing's id>`, the facts `trigger` and
  `server`. The first poll gives none: it learns what is there, so connecting a service does not pour its past into
  the digest. Every `every` (1m–24h, 10m by default). They go to the hub's journal and the inbox (`feed`) like any
  connector's, and each recipe is a trigger of its own in the catalog (`/events` → «Триггеры»).

## Folder (`internal/connectors/folder`)

**Connect.** Settings → Connections → Folders → «Folder»: the path (`~/Notes`; it must exist and be inside the home
folder of the runtime's account after symlinks are resolved), the kind (any folder or an Obsidian vault) and masks of
names (`*.md, *.txt`; an Obsidian vault without one watches `*.md`). More than 20 000 files is refused.

**Events.** `file.changed` with the facts `path` (relative), `change` (`created`, `modified`, `deleted`) and `size`. The
truth is a scan (size, modification time, and for files up to 2 MiB a hash, so a touch without a change gives nothing),
kept as the connection's snapshot; fsnotify only says when to scan, with a 2 s quiet period (never past 15 s) that
joins a burst into one batch, and a scan every minute as the fallback. `.git`, `node_modules` and the like are never
watched; an Obsidian vault skips `.obsidian` and `.trash`; symlinks are not followed. After a restart the first scan
is compared with the kept snapshot: only what changed while the runtime was down comes, once. More than 100 changes
in one batch (a checkout, a sync) is one event with `change: bulk` and `count`. An Obsidian vault also gives
`note.created` for a new note and `note.task` for a new open task with a date (`- [ ] … 📅 2026-11-20`), facts `path`,
`due`, `line` — never the task's words.

**Reads.** `… changes [since=24h|2026-11-15T10:00:00Z] [until=…] [limit=100]` — the journal of changes;
`… file path=<relative>` — the text (512 KiB at most; `..` and symlinks out of the folder are refused); `… search
query=<words>` — matching lines as `path:line: text`. All as untrusted data. No action: writing into the folder is the
agent's ordinary work with files.

## The page

Settings → Connections has a part «Mail, calendars, feeds» drawn from the catalog (`GET <platform>/connectors`):
cards grouped by kind, a card opens the form of its `Spec.Fields` (a choice is a row of buttons, a secret a password
field), the connected ones with «Check» and «Disconnect» and words only when something is wrong. A proposed action is
a card over the field of the chat that asked (`web/app/next/bindings/connector-actions.tsx`), kept in step by a long
wait on `<platform>/connector-actions?rev=`. A new connector needs nothing on the page: its `Spec` is enough.

## The calendar (`caldav`)

`internal/connectors/caldav`, one card «Calendar» with a choice of where the calendar is:

- **Yandex, iCloud, Fastmail** (the CalDAV address is preset), **Nextcloud** or **another CalDAV server** (the person
  gives the address): a login and an app password (not the account's own; the hint says where each service makes
  one). The principal is found at the address or at `/.well-known/caldav`, then the calendar home; «Find» lists the
  calendars with events and the person picks the ones to connect (none picked: all). Redirects and the hrefs of the
  answers are followed only within the site of the address (iCloud answers from `pNN-caldav.icloud.com`). Several
  accounts are several connections.
- **Link .ics (read only)**: a calendar's secret iCal address (Google: «Secret address in iCal format»; Outlook:
  «Publish → ICS»), kept in the safe as a secret field. Every action is refused before a card (`CanAct`) with words
  that say why and what to do instead; the read of one meeting says it too.
- **Settings**: tell about a meeting N minutes before (15), the hour of the day's schedule (8, 0 = none), the time zone
  (empty: the calendar's own zone, else the machine's).

**Events** (`source: "calendar"` for the hub's rules, and the inbox's `calendar` kind):

| Type | When | Facts besides the times |
| --- | --- | --- |
| `calendar.starting` | `remind` minutes before a meeting (not a whole-day one, not cancelled, not declined) | `minutes` |
| `calendar.changed` | a meeting created (`change: created`), moved (`moved`, with `movedFrom`), changed in title, place, link or people (`updated`), cancelled or deleted (`cancelled`) | `change` |
| `calendar.ended` | a meeting ended, for 30 minutes after | |
| `calendar.day` | once a day at the morning hour (for four hours): today's schedule | `date`, `meetings`, `first`, `last`, `busyMinutes`, `allDay`, `calls` |

Every meeting's envelope: title (the inbox shows it; the hub drops it), the call link as `url`, the organizer's name as
`actor`, the meeting's UID as `thread`, `ref` = `event id=<id>`, and facts `startsAt`, `endsAt`, `allDay`, `calendar`,
`attendees` (how many), `people` (their names, never their addresses), `call` (`zoom`, `meet`, `telemost`, `teams`,
…), `response` (the person's own answer), `recurring`. The place and the description stay on the machine: the agent
reads them with `ref`.

The source is a Poller asked every minute; the server is asked every 5 minutes (an .ics link every 15, conditional):
each calendar's ctag and sync token first, `sync-collection` with the changed objects only when they moved, a full
`calendar-query` of the window (a day back, 15 days ahead) every 6 hours or when the token expired. The window's
meetings are kept in the cursor (repeating ones expanded on the machine: RRULE in the event's own zone, so 10:00 stays
10:00 across a clock change, EXDATE, RDATE, exceptions by RECURRENCE-ID, whole days counted in days, Windows zone names
of Outlook), so «starting», «ended» and the day need no request, and the events already given are kept a day: a
restart doubles nothing. A meeting new to the cursor is told as created when its CREATED is after the last sync, or,
without CREATED, when it is within a week (a series entering the window is not news).

**Reads**: `events from= to= calendar=` (a listing with ids), `free date= days= from=09:00 to=19:00 min=30` (free
windows past the busy meetings: not the cancelled, declined, transparent or whole-day ones), `event id=` (one meeting
whole: people with their answers, organizer, place, link, description; all of it marked as untrusted data).
**Actions** (each by the card, written with `If-Match` on the ETag read just before; the server's scheduling sends
the invitations): `create title= start= end=|minutes= location= attendees= description= calendar=`, `move id= start=
end=` (one time of a repeating meeting: an exception), `cancel id=` (only one's own meeting; one time: an EXDATE),
`reply id= answer=accepted|declined|tentative`.

**A brief before a meeting**: a hub rule `{"trigger": {"source": "calendar", "filter": {"type":
"calendar.starting"}}, "promptTemplate": "A meeting starts soon: {{event}}. Read it by its ref and brief me on the
people and the topic."}`; N is the connection's «tell N minutes before».
## Mail (IMAP)

`internal/connectors/imap` connects any mailbox with IMAP and SMTP over TLS and an app password: Yandex, Mail.ru,
iCloud, Gmail, Fastmail, a server of one's own; several mailboxes at once, each a connection of its own.

- **Connecting.** The address and the app password (the field's hint says where each provider makes one). The servers
  come by the address's domain for the known providers (or the «Servers» buttons), else from the domain's autoconfig
  file (`autoconfig.<domain>`, `/.well-known/autoconfig`) or its SRV records (`_imaps`, `_submissions`,
  `_submission`, followed only to a host of the domain or of a known provider), else from the IMAP and SMTP fields. TLS only: 993 and 465 implicit, 587 STARTTLS. The check signs in
  to IMAP and to SMTP and sends nothing; a refused login says the server's words and what usually helps (the app
  password, mail clients on in the mailbox's settings, for Yandex and Mail.ru also a paid plan for mail clients).
- **Events.** One connection per mailbox runs IDLE on INBOX (NOOP polling where IDLE is missing), leaving it every
  10 minutes to look at the sent folder; a broken connection is tried again after 5 s doubling to 5 min, and after six
  failures in a row the error goes to the page. The cursor is UIDVALIDITY + the last UID of INBOX and of the sent
  folder; the first run only learns, a changed UIDVALIDITY learns again. `mail.received`: actor, subject, thread (the
  root of References), `ref` = `message id=<Message-ID>`, facts `folder`, `hasAttachment`, `unread`, `list` and
  `person` (false for mailings and robots: List-Unsubscribe, List-Id, Precedence bulk/list/junk, Auto-Submitted,
  noreply-like senders). `mail.replied`: the person's letter in the sent folder that answers something, from any
  client (`mine`, the same `thread`). `mail.unanswered`: a letter with `person` true that got no answer in the field's
  N days (3 by default, 0 off; facts `days`, `receivedAt`), counted on the machine from the letter's arrival. A rule
  takes it by type: `{"trigger": {"source": "mail", "filter": {"type": "mail.unanswered"}}}`; a hub wait rule can
  instead start on `mail.received` and stop on `mail.replied` keyed on `thread` (up to 168 h).
- **Reads.** `list` (folder, from, since, before, unread, query, limit), `folders`, `message` (headers, the text from
  text/plain or cleaned html, the attachments numbered), `attachment` (one into `dir`, the chat's folder: never a
  hidden folder like `~/.ssh`, links resolved; 25 MB at most, nothing overwritten), `thread` (by References, across INBOX, sent and archive), `draft` (kept in Drafts with
  In-Reply-To and References when it answers; nothing is sent). A letter is fetched up to 30 MB and read tolerantly:
  any charset, nested multipart, a broken or cut part keep what could be read.
- **Actions** (the card shows every argument): `send` (to, cc, bcc, subject, text), `reply` (id of the letter
  answered, to, cc, subject, text; In-Reply-To and References are taken from that letter), `mark-read` and `archive`
  (ids). A sent letter is put into the sent folder when the server did not do it itself.
- Reads and actions share one more connection per mailbox, kept two minutes after the last use.

## Default rules (`internal/connectors/rules`)

A connection gives use without a rule written by hand: its row in Settings → Connections has «Rules», two to four
switches of ready rules for that connector. They are off until the person switches them on, except a plain reminder
that runs no agent. Each rule is data in `internal/connectors/rules/templates.json` — a trigger, a filter, the agent's
prompt (short, in Russian) or a message to the person — not code per rule; a new one is a JSON entry and a test.

| Connector | Trigger | What it does |
| --- | --- | --- |
| Mail | `mail.unanswered` with `person: true` | The agent reads the thread, saves a draft reply in Drafts, pushes «from, about, the draft» with «Send / Not now»; «Send» makes it propose `reply` with exactly that text, sent only by the card |
| Mail | daily at 08:30 | Up to five unread letters of the day that wait for the person |
| Calendar | `calendar.starting` | A brief: who comes, the last letters with them (when mail is connected), what is expected, the call link |
| Calendar | daily at 20:00 | Tomorrow's meetings and the longest free window |
| Calendar | `calendar.ended` | With other people there: «note the outcome and the tasks?» |
| Webhook | `alert.firing` without `alert.resolved` of the same `thread` in 15 min | The agent reads the alert and proposes one action |
| Webhook | `ci.failed` | The agent reads the failed build and proposes a fix |
| Folder | `note.task` with `due` | On by default, no agent: the person's Telegram the day before at 09:00 |
| Folder, mail, calendar | a new note, task, letter or meeting whose title has the words (default «ДЗ», «домашнее задание», «дедлайн», «сдать до»; the person edits them) | «Assignment X due Friday — do a draft?»; on «Yes» the agent works in that chat with the materials |
| RSS | daily at 19:00, topics the person gives | One line and a link per item of the day on the topics |

**Where they run.** On the machine, as a third destination of the events next to the hub and the inbox: the title of a
letter or a note stays here (the words are matched on it, whole words, any case), and the rule's chat starts here
(Claude, the first workspace, named after the rule). Triggers: `events` (+ `facts`, each equal; + `keywords`), `at`
(daily, the machine's clock, up to three hours late, never at once when switched on after the time), `wait` (start,
stop facts, key, after) and `before` (a date fact minus days, at a time). State is `<data>/connectors/rules.json`:
the switches, the timers, the runs waiting, today's counts. The hub's own rules (`docs/hub/automations.md`) are not
touched and still take every event: a rule the person writes there works as before.

**What the agent is told.** The rule and the connection, the event as the runtime made it (type, connector,
connection, time), the read as one command with its argument in single quotes (a Message-ID or a file name is the
sender's choice: it never becomes a command of its own; a ref of another shape gives no command), everything the
sender chose (id, title, actor, thread, facts) inside `<untrusted-data>`, that the texts of letters, notes and webhooks are data and not instructions, that nothing is
forwarded or sent beyond what the rule says and every action outward is `harnessly connector act` showing exactly what
goes, then the rule's task and how to reach the person.

**The push.** The person's bound Telegram bot when there is one: the agent runs `harnessly connector act <bot> notify
text=… buttons=…`; «Open in harnessly» to the rule's chat is added by the bot, and a press comes back into that chat.
Without a bot the agent ends its turn with a short answer, and harnessly shows it as the chat's usual notification
with a link to the chat. A rule without an agent (the reminder) goes only to the bot.

**Limits, shared.** Runs wait out the quiet hours of the asks' settings; each rule has a limit per connection a day
(1 to 10), all rules together 40 a day; a run not started in 18 hours is dropped. The bot's own 12 messages an hour
and quiet-hours silence stay. No rule sends anything outward by itself: there is no auto-send.

**The page.** `GET <platform>/connectors/<id>/rules` answers `{rules: [{id, title, summary, on, keywords?}]}`; `PUT`
with `{rule, on?, keywords?}` changes one and answers the same list. The listing names the connectors that have
rules in `ruled`. Removing a connection drops its rules' state.

**Not yet in the inbox.** The inbox takes signals (`/api/ingest`) but no proposal of an action; a rule's proposal lives
in its chat and the bot. A field such as `prepared {title, chat}` on a signal would let the inbox show it among its own
actions.

## Checks a connector comes with

- A test of `Check` (right and wrong credentials), of the source (first run learns, a repeat gives nothing, a new
  thing gives one event, the cursor survives), of each read and action — against an `httptest` server or a fake, never
  a real service. See `internal/connectors/rss/rss_test.go` and `internal/connectors/connectors_test.go`.
- A test that runs a whole `connectors.Service` (`Start`) cancels its context and calls `WaitStopped()` before it
  changes a package knob the source reads (a clock, a timeout) or starts a second runtime on the same folder: the
  sources stop a moment after their context, and a sleep instead is a data race under `-race`.
- Its error words for the person: "IMAP is off or needs a paid plan" is not "wrong password".
- A new `harnessly` command or MCP tool comes with a scenario in `evals/agent/` (`AGENTS.md`).

## Telegram bot (`telegram-bot`)

`internal/connectors/telegram`: the person's own bot from @BotFather as their channel with harnessly, on the official
Bot API only (nothing reads the person's chats through their account). The Bot API client is
`internal/notifybots` — the same as the notification bots and the asks.

- **Connect**: the bot's token (in the safe), checked with `getMe`; the account is `@<bot>`. The page then shows a
  link `t.me/<bot>?start=<one-time code>` (10 minutes, a new one when it runs out). The person opens it and presses
  Start: the bot binds their private chat and their Telegram id, answers «Done, bound to harnessly», and the page,
  which reads the list again every 3 s while a step is open, drops the link by itself («Check» does it at once). From then on it reads and answers that chat
  only; a message or a button press from anyone else is ignored, a group someone else adds it to is left at once.
  A bot of its own: Telegram gives one bot's updates to one reader, so a bot already linked for this machine's
  notifications or asks (the same Telegram id) is refused on connect.
- **Source**: a long poll of `getUpdates` (a Watcher, no webhook to the outside; the cursor is the offset). Each
  update's effects (the binding, a press, a message into a chat) are done once: the record keeps the last handled
  update, so a crash re-reads only events, which the runtime drops as delivered.
- **Events**, all `chat.message` without text, `thread` = `tg:<chat id>`, `ref` = `message id=<chat>:<message>`:
  the person's message to the bot (`mine`, facts `chat=private`, `forwarded`, `reply`), a press of a button under a
  push (facts `button`, `notice`), a message in a group the person added the bot to that mentions or answers it
  (privacy mode stays on; facts `chat=group`, `group`). A hub rule on `{"source": "chat", "filter": {"connector":
  "telegram-bot"}}` can start a new agent chat. With «Your messages to the bot» = «Into a chat of harnessly» the
  person's message also goes into the named chat as their own words, prefixed `[Telegram]`; a reply to a bot's message
  goes to the chat that wrote it; a forwarded message comes as `<untrusted-data>`.
- **Reads**: `messages [chat=me|<group>] [limit=]`, `message id=`, `groups` — the last 200 messages the bot got.
- **Actions**: `notify text= [buttons="Yes|Later"]` is Direct — the person's own chat, at once, at most 12 an hour,
  without a sound in the quiet hours of the asks' settings, with «Open in harnessly» to the chat that wrote
  (`<hub>/agents/?c=agent:<machine>:<chat>` from the machine's hub link; no button on a machine without one); a press comes back to the
  chat that wrote. `send chat= text=` writes to a group after the yes on the card.
- **Relay**: every proposal of every connection also comes to the bound chat with «Send / Don't send»: the same args
  as the card (by default who, where and the subject; the whole text only when «Sends to confirm in Telegram» = «The
  whole text»), a one-time button code dropped before the answer runs, the proposal's 30 minutes, the bound chat and
  person only. An answer anywhere closes the message in Telegram.
- Not here: reading the person's own chats (a bot cannot: that is the Telegram account below), Telegram Business.

## Telegram account (`telegram-account`)

`internal/connectors/tgaccount`: the person's own Telegram account through MTProto (`github.com/gotd/td`, pure Go),
so that the agent reads their chats and writes as them. The bot above stays the channel between harnessly and the
person; this is their account itself.

- **Connect**: `api_id` and `api_hash` of the person's own app (my.telegram.org → API development tools), the hash in
  the safe; the check asks Telegram for a sign-in code, which fails at once on a wrong pair. An optional «Cloud
  password» (in the safe) is for an account with two-step verification. The page then shows a QR code (`Step.Image`,
  a PNG data URL; the page draws only `data:image/png`): the person scans it in Telegram on the phone, Settings →
  Devices → Link Desktop Device. The sign-in runs while the page asks for the step (it stops 2 minutes after the page
  is closed, and starts again when it is opened); once signed in, the connection is checked again (`SetRecheck`) and
  its account becomes «Name (@username)».
- **On the machine**: the session (`session.json`, 0600), the updates' state (`updates.json`) and the book of peers
  with their access hashes (`peers.json`) in the connection's folder, gone with it. Removing the connection also
  signs this device out of Telegram (`Forgetter`). A session ended from the phone is forgotten by the source, and the
  page shows a new code.
- **Source**: gotd's updates manager over the account's updates; its state is kept, so a restart catches up with
  `getDifference`. The source waits without an error until the sign-in. Events, `chat.message` without text,
  `thread` = `tg:<marked chat id>`, `ref` = `message chat=<id> id=<message>`, facts `chat=private|group`, `mentioned`,
  `reply`, `forwarded`, `media`, `bot`: by «Events of new messages», private chats and mentions in groups (the
  default), every chat but channels, or none. Channel posts and the person's own Saved Messages are never events.
  Inbox kind `telegram`.
- **Reads**: `chats [limit=] [unread=true]`, `messages chat= [limit=] [before=]`, `message chat= id=`,
  `search q= [chat=] [limit=]`, `me`. A chat is `@username`, a `t.me` link, `+phone`, `me` (Saved Messages), the
  marked id from `chats` (users as they are, groups `-id`, supergroups and channels `-100…`) or a known chat's exact
  title. Reading leaves messages unread. What is read comes as `<untrusted-data>`.
- **Actions**: `send chat= text= [reply=]` writes as the person after their yes on the card (and the bot's «Send /
  Don't send» when a bot is bound): what the agent reads in chats is other people's words, and a message cannot be
  taken back. `note text=` is Direct: the person's own Saved Messages, at once, at most 20 an hour.
- Telegram may limit a new app's account for unusual activity: the connector sends only what the person confirmed,
  and Telegram's «wait N seconds» comes back to the agent as words.
