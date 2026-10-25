# internal/wire — the v2 contract

`schema/frames.json` is the source of truth. `types.go` (Go) and `web/app/next/protocol/v2.ts` (TS) mirror it;
the fixtures in `testdata/` are read by both:

- `go test ./internal/wire/...` checks every fixture against the schema (a small checker in `schema_test.go`
  that fails on any keyword it does not know), decodes it through the Go types, re-encodes it and compares, and
  checks that every closed list and codec table equals the schema's.
- `pnpm -C web test` parses every fixture with the strict parser of `v2.ts`, compares the result with the
  fixture, and checks every TS closed list against the schema. The TS decoder tables are tied to those lists by
  `satisfies Record<Kind, …>`, so a kind missing on either side fails typecheck.

No generator: a change starts in the schema, then both mirrors, then a fixture; the tests name what is out of step.

A fixture is one document: a socket frame as sent, a command exchange `{op, body, result}`, or an HTTP answer
`{http, response}`. File names: `frame.*`, `snapshot.*`, `ev.*`, `cmd.*`, `cmd.refused.<code>`, `http.*`.

In Go a union with a few fields (`AgentState`, `MessageState`, `GateState`, `Entry`, `CmdResult`) is a flat struct
whose JSON carries exactly the fields of its kind — not `omitempty`, so `false`, `0` and `null` survive. Frames,
events and commands are interfaces over one struct per kind. Decoders are strict (unknown kind or field is an
error) and run `Validate()`.

## Stage 0 decisions

Where the spec was silent or two readings were possible, the simpler one:

1. **Results name their channel.** `accepted`/`queued` carry `ch` beside `seq`: the page waits for that seq on
   that channel (a send shows on `conv:<id>`, a rename on `convs`).
2. **Conversation-scoped commands carry `conversationId`** (`cancel`, `retry`, `promote`, `answer`, `decide`,
   `bg.stop`): the command goes straight to the conversation's actor, no global index of client, question or
   task ids.
3. **Every body has `opId`**; for `send` it must equal `clientId`.
4. `conversation.restore` takes only `id`; `force` is `delete`'s. `conversation.create` takes `projectId` or
   `cwd` (exactly one) and `model: Model | null` (null: the agent's default). `conversation.move` takes
   `projectId: string | null` (null: out of any project).
5. **`MessageState` on the wire has no `draft`, `uploading`, `submitting`**: they live on the page
   (`LocalMessageState` in TS). `MessageFailure` stays one closed list, page-side codes included.
6. `queued.position` starts at 1 (the head).
   `Conversation.lastPromptAt` is when the person last wrote to the chat (`createdAt` until then): the last real
   human message, not the agent's lines, meta lines, compaction summaries, hook or task notifications, tool results
   or reminders. A send from a client sets it at once. The sidebar orders and groups by it, so the agent's work
   never moves a chat.
7. `step` carries `counts: {toolCount, stepCount} | null` — the turn's counters after the entry, null outside a
   turn. `conversation_seen` carries `rev`, like `conversation_state`.
8. **Snapshots.** `seq` is the last seq the snapshot includes, 0 before any event; event seqs start at 1. The
   `machine` channel's data is `{machine}`. `conv:<id>`: `turns` are full `Turn` records (≤ 33, oldest first);
   `entries` hold the steps of the last three and, of each older one, the entry that opened it (the turn's own id:
   the person's message, a teammate's note) and its answer entry, so a folded turn always renders its question and
   its `answerEntryId`. The turns page has the same shape: `{turns, entries, beforeTurnId}`.
9. `PUT /api/v2/uploads` answers an `AttachmentRef`, not only `{id}`.
10. **Events are bound to their channel** (schema, Go and TS): machine events only on `machine`, and so on.
11. Invariants beyond the shape: a `failed` turn has a `failure` and no other turn does; `turn_finished` is
    `done`/`interrupted`, `turn_failed` is `failed`; `permission_requested` is kind `tool`, `plan_proposed` kind
    `plan`; a `compaction` event carries a compaction entry; `afterSeq` needs `epoch`.
12. Every conversation id matches `^tab-[A-Za-z0-9_-]+$`, not only in channel names. `Limits` are plain numbers;
    `wire.DefaultLimits` holds today's values.
13. Go: `AgentState.TurnID` is `*string` (the spec's sketch had `string`) because `working.turnId` is nullable.
14. `convo.Service` does not serve the `machine` channel: reach and load come from elsewhere (the agent link,
    `internal/machines`).

## Stage 0 decisions after §11–12 (no lags, no unclear states)

15. **Every wait on the runtime has a term on the wire**: `wait: {slowAt, deadlineAt}` on `starting`,
    `queued` (null for `agent-busy`/`behind`, which wait on the agent, not on us), `typing`, `in-input` and the
    gate's `answering`. Past `slowAt` the page shows «дольше обычного» (`waitPhase` in v2.ts); by `deadlineAt` the
    runtime has moved the state on — it does not wait longer. The page derives nothing: both instants come from
    the runtime. Terms: `wire.WaitSlowMs` 3 s, typing 12 s, confirm 15 s, answer 10 s, launch 150 s
    (slow at 15 s), queued TTL 15 min as before.
16. **`in-input → failed{unconfirmed}` after 15 s**, not the 30 s of §4.3, to fit «ошибка за 10–15 с».
    The exception is a message typed into a working agent (mode `steer`): Claude holds it in its own queue
    until the agent's next step (a Bash call or a long answer: minutes), and its prompt-submit hook fires at the Enter,
    not when the agent reads it. It stays `in-input` until its transcript entry arrives (`accepted` → `in-turn`), with
    `deadlineAt` 30 min away (`outbox.SteerDeadline`); the page says «agent reads it after the current step», not
    «not confirmed». Steers go to the agent in the order they were sent, ahead of `now` and queued messages.
17. **Every command ends.** `limits` gives `slowMs` (3 s), `cmdBudgetMs` (10 s: the runtime answers every command
    within it, `refused{timeout}` at worst) and `cmdTimeoutMs` (12 s: the page gives up and offers retry or
    cancel). On the page a command ends in `CmdOutcome` = the result, or `failed{timeout|network|http}`; a retry
    reuses the opId. New refuse code `timeout`; `RetryableRefuseCodes` (`machine-offline`, `rate-limited`,
    `internal`, `timeout`) get «Повторить», the rest only «Отменить».
18. **Stable entry ids are part of the contract**: every entry id must be `c:<uuid>#<k>`, `x:<offset>#<k>` or
    `live:<turnId>`, and every turn id one of the first two — schema patterns, `ValidEntryID`/`ValidTurnID` and the
    TS parser refuse anything else, so a random id cannot reach the feed. Builders: `ClaudeEntryID`,
    `OffsetEntryID`, `LiveEntryID`.
19. **A `peer` entry is what other agents' sessions wrote** (Claude Code's `<teammate-message>`, which reaches the
    agent as a user line): `peers` holds each message of the line — `from`, `kind` (`message`, `done` for an idle
    notice with a result, `idle` for one without, `event` for any other JSON notice, its type in `event`), `color`,
    `summary`, `sentAt` and the body as `md`. A background subagent of the session writing to it (`<agent-message from=<hex id>>`) has `agentId` and `model`, and `from` is its task as the Agent call named it (empty when unknown). The harness's lead-in and the paragraph after the messages are not
    carried. It opens a turn like a user entry but is not the person's (no client id, never the outbox's) and is not
    counted in `stepCount` of the turn it opens. The body is the sender's words: the page draws it as Markdown text, never as markup.
20. **The harness's own lines are `notice` entries with code `system-note`**, never `user`: a `<task-notification>`
    of any shape (a goal check-in has only `<summary>`), `<system-reminder>` blocks alone, a line under
    `[SYSTEM NOTIFICATION - NOT USER INPUT]`. `detail` is the note's first line, then its other fields one a line. Like
    a `peer` entry it opens a turn when none runs and is not counted in that turn's `stepCount`.

## Message states: one road

A message goes one way, and every state is one event, sent only when the state really changes:

```
queued{position, reason} → typing → in-input → accepted → in-turn → done
                  ↘ failed{code, retryable} (from any step before in-turn)      ↘ cancelled (person, while queued)
```

- **queued.** `position` 1 is the head. The head's `reason` is the agent's state alone (`agent-busy`, `launching`,
  `resuming`, `blocked-menu`, `blocked-text`, `paused`); the others are `behind`. A message in flight (typing, in-input)
  or a steer typed ahead of it does not change the head's reason, and a message that waited on `agent-busy` and is
  pushed down by one that jumped ahead («now») keeps `agent-busy` — it never goes `agent-busy → behind → agent-busy`.
- **typing → in-input.** The text is typed into the agent's box; the wait terms (`wait.slowAt`, `wait.deadlineAt`) are the
  runtime's. A steer (`mode: steer`) is read by the agent only at its next step: it stays `in-input` up to 30 min and the
  line is free for the next one.
- **accepted.** The agent has it: the hook said so (`entryId: null`) and then the transcript named the entry
  (`entryId`), the second event only adds the name. **in-turn:** the turn it opened (a queued message opens a new
  turn), or the running turn it was steered into — the entry is then a `user` entry among that turn's steps.
- **failed** is the runtime's by its deadlines or a refusal on the merits; the page only shows it. The page has no
  «not confirmed» of its own: a message the runtime has not answered is not failed, see «The road is not a refusal».
- Where the page draws it: while the agent was busy at the send (or the message is a steer), in the stack over the field
  from the first moment until `accepted`; otherwise in the feed on its way. At `accepted` it is the feed's: the turn's
  prompt, or a steered message among the steps. The `user` entry carries the client id, which is how the page tells a
  message it holds from the entry that is the same message, and why one never stands twice.
- Delivery to a free agent is event-driven: the send command pumps the queue at once; a screen that still shows the
  last turn's spinner while the state says idle is looked at again every 100 ms (40 looks), then every 1.5 s.

## References: designs, documents, boards, files, MRs, tasks

A message can point the agent at places instead of carrying files: `send.refs` (and `Message.refs`, `Draft.refs`,
`draft.set.refs`), at most 10 `MessageRef {kind, id, title, target}` of kind `design | doc | board | file | mr | task`.
Nothing is uploaded; `id` names the place for the page (a design's id, a document's slug, a path, an MR's or a task's
URL or key), `title` is the chip, `target` the URL or path the agent opens (`""` for a design).

- **The runtime writes them into the words.** `Message.text` is the person's text, a blank line and one block
  (internal/attach), the same block the agent reads:
  ```
  <attached>
  Прикреплено к сообщению:
  <ref kind="design" id="landing" title="Лендинг"/> Дизайн «Лендинг» из библиотеки (…): папка …, доски — в …/project. …
  <ref kind="doc" id="pay" title="Разбор оплаты"/> Документ «Разбор оплаты» — https://… (прочитай его, прежде чем отвечать).
  </attached>
  ```
  The header is in the runtime's language; every place is one line led by its `<ref …/>` tag, whose attributes are
  escaped (`&amp; &lt; &gt; &quot;`), and nothing in a place's words can close the block or start a line of it.
- **A design** is named by the machine's library: its folder, its boards and the rules file a design dialog gets; the
  conversation is linked to the design (`designly.json` `attachedDesigns`), so `designly/library` lists it among the
  design's `convs`. A design the library has not is `refused{not-found}`; a machine without a library `unsupported`.
- **The page reads the block back**: a bubble and a queued row show the person's text and a chip per `<ref>`, never the
  block. A repeat of the `clientId` is the same message: the block is not appended twice.
- The first message of `conversation.create` takes no references yet.

## The road is not a refusal

Who leads a message's delivery — this runtime, or a cluster's outbox in front of machines — says where it is by an
event (`prompt_state`, `message_upsert`); the page never decides that a message failed or went.

- **page-side `submitting`** lasts until the first such event or a refusal on the merits. It has no deadline and no red.
- **Of the road, not a refusal** (the message stays `submitting`, the bubble says «waiting for the machine», without red):
  no connection, a socket that dropped, a restart of the runtime or the machine, a gateway's 502/503/any status with no
  `CmdResult` in the body, an answer that did not come in `cmdTimeoutMs`, and the refusals `machine-offline`,
  `timeout`, `internal`, `rate-limited` (`RETRYABLE_REFUSE_CODES`) — they say «not now», not «no».
- **Sent again by itself**, with the same `clientId` as `opId`: 1, 2, 4, 8, 15, 30 s (30 s from then on), at once when the
  machine's socket is live again, after F5 too (the outbox of IndexedDB keeps the message). The runtime answers a repeat
  with the answer it gave (kept 24 h and across restarts), so a repeat never types twice; `timeout` and `internal` are
  not kept as an answer, so the repeat is tried afresh (`convo.TestASendRepeatedByThePageIsTheSameMessage`).
- **Refused on the merits** (`bad-request`, `text-too-long`, `queue-full`, `attachment-*`, `agent-gone`, `not-found`,
  `forbidden`, …): `failed{refused}`, red, the reason in the person's words (`next.refuse.<code>`), Delete only — the same
  `clientId` would get the same no.
- **Delete** of a message the runtime has not shown is the page's own and works offline; a `cancel` for the same
  `clientId` is sent besides, in case a lost answer hid that the runtime has it.
- A cluster that leads the delivery uses the same road: it answers the command (`accepted`/`queued`) once it has taken
  the message into its outbox, and from then on `queued{reason}` / `typing` / … come as events, whoever types.

## Closed questions

19. **Machine channel.** `wire.MachineSource` (`source.go`) serves it: `Snapshot(ctx, load)` and
    `Subscribe(load, fn)` with `machine_status` and, while asked, `machine_load`. `convo` does not know it.
20. **Device.** Every HTTP request carries `X-Harnessly-Device: <deviceId>` (`wire.DeviceHeader`, `DEVICE_HEADER`);
    the socket carries it in `hello.device`. The page makes the id once and keeps it in localStorage.
21. **Snapshot and seq — a protocol invariant.** The hub subscribes the channel before it takes the snapshot, so
    no event falls between them. The snapshot carries the last seq it includes; the page drops events with
    seq ≤ that seq. Every event applies idempotently by id and rev: an event already reflected changes nothing.
22. **Turns page.** `limit` defaults to 30 (`TurnsPageDefault`), at most 100 (`TurnsPageMax`); below 1 or above
    100 the page answers `refused{bad-request}`. A page that cannot answer sends `{r: "refused", code}` like a
    command (`Refusal`, `parseRefusal`).
23. **«Сейчас».** There is no event that clears `now`: the page sets it to null on `turn_finished` and
    `turn_failed` of that turn.
24. **conversation.create with first.** The accepted result's `id` is the new conversation's id. The page
    subscribes to `conv:<id>` with its epoch and `afterSeq: 0` (`frame.client.sub-new-conv`); the ring holds a new
    channel from its first event, so the first message's `queued{launching}` and later states are not lost.
25. **Gate ids.** `Permission` has `turnId: string | null` like `Question`. Question and permission ids share one
    space within a conversation, told apart by prefix — `q:<toolUseId>`, `p:<toolUseId>` or
    `p:<tabId>:<eventSeq>` — guaranteed by the runtime and checked everywhere an id of either is taken.
26. **answers[].text** is the person's own answer instead of a choice: an item has either `choices` (≥ 1) or
    `text` with `choices: []`. Both or neither is `refused{bad-request}`.

## Stage B — the hub, the socket, commands and pages

27. **Routes** (`pages.go`, under the same base path as Legacy, `/agents` or none, and the same page session):
    `GET /api/v2/hello`, `GET /api/v2/socket`, `POST /api/v2/cmd/<op>`, `PUT /api/v2/uploads?name=<file>` (the
    body is the file, `Content-Type` its type; the answer is an `AttachmentRef`), `GET /api/v2/conversations/<id>/
    turns|turns/<turnId>/entries|entries/<entryId>/output`. Without a session: 401 with `{r: "refused", code:
    "bad-request"}`.
28. **HTTP status of a refusal.** A command always answers 200 with a `CmdResult` (refusals included). A page or
    an upload answers its refusal with a status: 400 `bad-request`, 404 `not-found`, 413 `attachment-too-large`,
    415 `attachment-type`, 503 `timeout`, 500 `internal`.
29. **opId.** The same opId with the same body is the same command: while it runs a repeat waits for it, after it
    the repeat gets the stored result (24 h, 5000 ops, kept in `<data>/v2/ops.json`). The same opId with another
    body is `refused{conflict}`. A command past `cmdBudgetMs` is `refused{timeout}` and is not stored, so a retry
    runs again.
30. **Socket.** `hello` within 10 s or the socket closes; ping every 15 s, a socket silent for 35 s closes; at most
    64 channels; a client 8 MB behind is closed with 1008. Each channel keeps 2000 events or 5 minutes for resume
    (`RingSize`, `RingAge`); a cursor of another epoch or past the ring gets a snapshot.
31. **Budgets.** `FitConvs` keeps the convs snapshot within 72 KB per 100 conversations (snippets, then titles,
    shortened); `FitConv` keeps a conversation snapshot within 256 KB: outputs shortened, older turns folded to
    their answers, the last turn's oldest steps, the oldest turns. The user entry, the answer and the live reply
    of the last turn always stay.
32. **Fake runtime.** `RUNTIME_V2_FAKE=1` (pace `RUNTIME_V2_FAKE_STEP_MS`, default 300) serves the fixtures
    through `wirefake` on a real hub and socket: messages run a scripted turn, markers in the text steer it
    (`[fake:work]`, `[fake:question]`, `[fake:permission]`, `[fake:plan]`, `[fake:fail]`, `[fake:stuck]`,
    `[fake:launch-fail]`), and `POST /api/v2/fake/<action>` steers a test (`wirefake/control.go`: reset, restart,
    drop, delay, turn.finish, turn.fail, ask, agent). Without the fake there is no v2 yet: hello is 404.

## Models

33. **Model choice.** `machine.agents[].models` lists what the CLI takes (`{id, label, hint, efforts, defaultEffort}`;
    `hint` is one short line on what the model is for, `""` when the CLI says nothing honest; the efforts are
    `low | medium | high | xhigh | max | ultra` (`ultra` is Codex's, gpt-5.6-sol and terra), those the model takes, the rest of the CLI's levels left out; `defaultEffort` is
    what the model runs on when none is asked for: Codex's cache; for Claude CLAUDE_CODE_EFFORT_LEVEL, then the
    `effortLevel` saved in its user settings, then the model's own default from Claude Code's bundled model catalog
    (a project's settings can still override it for one conversation); Haiku has no depth to choose, `[]` and `null`; the first model is the one a new conversation offers; `[]` when the machine does not
    know them) and `canSwitchModel` says whether a running conversation takes the `model` command. A runtime that
    serves it announces the `model` feature. `conversation.create` carries `model` (the agent starts on it);
    `model {id, model, effort}` switches a running one: Claude gets `/model <id>` (and `/effort <level>`) typed into
    its box, and the answer is `accepted` on `convs` with the record's `rev` once the agent said «Set model to»
    (the «Switch model?» question is answered yes: choosing it was the consent). An agent that is not at its prompt —
    in a turn, asking, not started (stopped, gone, still starting) — is never refused for it: the answer is `accepted`
    at once with the change on the record as `pendingModel {id, effort, ultracode?, since, applying}`, and the runtime applies it
    at the first moment the agent is at its prompt, before the next queued message is typed (an agent not started: right
    after the launch a message brings about, before that message). The latest ask replaces the one waiting; asking for
    the model the agent runs on takes it back. An applied change moves `model` and adds `{at, from, to}` to `modelLog`
    (the last 20), which the feed draws where it happened. An effort the model does not take is
    `refused{option-unknown}`, a Codex conversation `refused{unsupported}`; the record keeps the old model on any
    refusal. A queued change the agent did not take is logged too, as `{at, from, to, refused: <code>}` with the record
    left on `from`: the feed says «модель не сменилась» and why.

33a. **Ultracode.** Claude Code's ultracode (2.1.310+) is a mode beside the effort, not a level of it: standing dynamic
    workflows with subagents on every task, at any effort. `models[].ultracode: true` marks the models that take it
    (Claude's Fable, Opus, Sonnet; not Haiku, not Codex). `Model`, `pendingModel` and `model` carry an optional
    `ultracode` boolean (absent is off; in `model`, absent keeps the mode the conversation runs). A switch types
    `/model <id>`, then `/effort <level>`, then `/effort ultracode on|off`, and is applied when Claude says «Ultracode
    on»/«Ultracode off»; a model that does not take it is `refused{option-unknown}`. A conversation started on ultracode
    gets it from `--settings` (the `ultracode` key: Claude Code never saves it), and one resumed gets it typed again,
    since a new process starts without it. Effort `ultracode` of before is read as `{effort: xhigh, ultracode: true}`:
    what `--effort ultracode` ran.

## Background, closing, projects

34. **Background.** `conv:<id>` snapshots list `background` (a Bash command, a monitor or a subagent: kind, title,
    status, `stoppable`); a runtime that serves it announces the `background` feature. A task is listed while it
    runs and leaves the list (`background_removed`) when it ends. `bg.stop {conversationId, taskId}` stops a command
    (SIGTERM, SIGKILL after 3 s) and answers `accepted` on `conv:<id>`; a task already stopped is `accepted` again, a
    subagent or an unknown id is `refused{unsupported}` / `refused{not-found}`.
    **Shots.** The same snapshot lists `shots` (newest first, at most 30: `id`, `name`, `at`, and `agent`, the subagent's
    description, absent for the agent itself): the pictures the agent and its subagents read or a tool saved, taken from
    the transcripts. A change goes out whole as `shots_set {shots}`. `GET /api/v2/conversations/{id}/shot?id=<id>` sends
    the bytes of a listed picture only (PNG, JPEG, GIF or WebP by its first bytes, at most 12 MB); any other id, a path in
    its place, a file gone or not a picture is a 404 `not-found`.
    **Pictures.** The same snapshot lists `pictures` (at most 60, the latest named last: `id`, `text`): the pictures the
    conversation's own messages (the person's and the agent's answers) name by an absolute path, `text` being the path
    as the message writes it. A path is listed only while it is a plain PNG, JPEG, GIF or WebP file of at most 12 MB
    that, links followed, lies inside the conversation's folder, a Claude scratchpad (`/tmp/claude-<uid>`) or the
    uploads; a change goes out whole as `pictures_set {pictures}`. Its bytes come from the same `shot?id=<id>`, which
    checks the roots, the links and the first bytes again at every request; the page never sends a path.
35. **Deleting closes the pane.** `conversation.delete` ends the conversation's tmux session and takes its tab out of
    the layout; a deleted conversation restored later is `gone`, as its tab is not there any more.
36. **Projects.** A folder of the machine becomes a project with `project.create {path}` (an absolute path to a
    directory; else `refused{not-found}`). The answer is `accepted` on `convs` at the seq of `project_upsert
    {project}`, with the project's `id`; the same folder again is the same project, published again. The `convs`
    snapshot carries `projects` (oldest first) and `defaultCwd`, the folder a conversation starts in when the
    person names none — `conversation.create` takes `projectId` or `cwd`, never both, so the page offers
    `defaultCwd` as the first choice. Projects are kept by the runtime across restarts.
37. **Folders for a project.** `GET /api/v2/dirs` answers `[{path, name, git, branch, empty}]`: the folders one level
    below the machine's default folder (`defaultCwd`) and the git folders one level below those that are not git
    themselves; hidden folders and build outputs are left out, git folders come first, then by name, at most 200.
    `branch` is the checked out branch, `null` for a plain folder or a detached head. `project.create` takes an optional
    `name` (one line, up to 120 characters); without it the project is called by its folder.
38. **Sessions from a terminal.** `GET /api/v2/sessions` answers `[{provider, sessionId, title, cwd, updatedAt, turns}]`:
    the sessions Claude Code and Codex kept on the machine, in any folder, that no conversation of the runtime holds
    (nor a fork answering a side question), the most recently active first, at most 50. `title` is the agent's own name
    for the session, else the first thing the person asked, else the folder's name; sessions nobody has written in are
    left out; Codex's are those of the last 30 days. `conversation.resume` then has a second form,
    `{opId, provider, sessionId, cwd}` (the first, `{opId, id}`, resumes a stopped conversation; mixing them is
    bad-request): it opens a new conversation on that session — the agent starts as `claude --resume <id>` /
    `codex resume <id>` in `cwd`, the conversation shows the session's history and is titled by it — and answers
    `accepted` with its `id`, like `conversation.create`. `cwd` must be the session's own folder (bad-request
    otherwise), an unknown session or a gone folder is not-found, and a session a conversation already holds answers
    with that conversation's id and starts nothing.
39. **Terminals.** The feature `terminal` is announced by a runtime that serves terminals (the live one and the fake). A conversation has its agent's pane and any number of shells of its own (at most 8).
    - `GET /api/v2/terminal?conversation=<id>[&terminal=<tid>][&cols=&rows=&clientId=]` is a WebSocket. Without
      `terminal` it attaches to the conversation's agent pane, with full access (keystrokes go to the agent, as in the
      Legacy page; a person typing there while a v2 message is being typed is the person's doing); with `terminal`, to
      that shell. A conversation, a terminal or a pane that is not there is answered 404 before any upgrade, a malformed
      id 400. `clientId` is the page's: one socket per `clientId` and terminal, a page that opens the same terminal again
      replaces its old socket.
    - The socket speaks the Legacy terminal protocol, binary frames of one byte of type and a payload. Page to runtime:
      `0x00` keystrokes (UTF-8), `0x02` resize (uint16 cols, uint16 rows, big endian), `0x03` heartbeat (every 30 s, the
      runtime answers `0x03`; 90 s without one closes the socket). Runtime to page: `0x01` output (UTF-8, whole
      characters), `0x03` heartbeat answer. The screen of the pane arrives first, as the output a terminal would draw to
      catch up. The page keeps the socket by reconnecting when it closes; nothing is lost, tmux holds the session.
    - `GET /api/v2/conversations/{id}/terminals` lists the own terminals `[{id, createdAt}]`, oldest first;
      `POST` makes one (a shell in the conversation's folder) and answers `{id, createdAt}`; `DELETE .../{terminalId}`
      ends it (204). A ninth is 409. The shells are tmux sessions, so they outlive a restart of the runtime; deleting the
      conversation ends them, and so does closing them. There is no push for the list: the page reads it when it opens.
40. **Voice.** The feature `voice` is announced by a runtime whose machine can recognize speech (ffmpeg and the recognition
    service's socket, checked once at start; the fake always). `POST /api/v2/voice?lang=ru|en|auto` (`ru` without it)
    takes a whole recording as the body (webm/opus, mp4/aac, wav: whatever ffmpeg reads; up to 20 MB, 2 minutes to be
    recognized) and answers `{text}`, `""` for silence. It is the same decoder and the same recognition service as the
    Legacy page's dictation socket. An empty body or an unknown `lang` is bad-request, a recording over 20 MB
    attachment-too-large, a machine that cannot recognize now unsupported, a recording that failed to decode or
    recognize internal, one that took over 2 minutes timeout. The text is for the composer's field; nothing is sent.
41. **Other origins.** The socket (`/api/v2/socket`), the terminal socket, `POST /api/v2/voice` and the writes to
    `.../terminals`, `PATCH /api/v2/me/settings` and `PATCH /api/v2/agent-settings/<provider>` (item 48) are refused 403
    `forbidden` when a page of another origin asks: the browser sends the session cookie
    to any site, and these open a shell or take a recording. `Sec-Fetch-Site`, when the request has it, decides alone
    (`same-origin` and `none` pass; `same-site`, a sibling subdomain, does not); without it an `Origin` must name the
    host asked (or the `X-Forwarded-Host` a proxy in front was asked as). A request with neither is no browser's and is
    let through to its own credentials. The rule is `internal/sameorigin`; the Legacy sockets (the terminal, the
    dictation, the timeline, the status and sync hubs) take it too. The agent's own socket to the cluster is
    authenticated by a bearer token, not a cookie, and is left as it is.
42. **Designly.** The feature `designly` is announced by a runtime that can start agents (the fake always). The machine's
    design library, over four pages:
    - `POST /api/v2/designly/create` takes `{title, brief, kind: site|slides|ds, provider, model|null, effort|null,
      designSystem|null}`, makes a project of the library from its template, opens a v2 conversation in the project's
      folder (the agent, model and depth as asked; its first message is the new-project task, the pages hint, the kind
      and the design system; Claude also gets the library's rules as its system prompt, another agent in the message)
      and answers `{id, title, conv}`; the conversation carries the project's name. Refusals are
      `{r: "refused", code, message, projectId}` with a message worded for the person: 422 `bad-request` (an empty
      name or brief, a name or brief too long, an unknown design system), 409 `conflict` (the id is taken), 500
      `internal`; with `projectId` set the project was made but the conversation did not start.
    - `GET /api/v2/designly/library` answers `{port, root, artifacts}`: `port` is the library server's on the machine
      (the boards are served there), and each artifact `{id, title, updatedAt, kind, pinned, cover, projectPath,
      boards: [{title, path}], convs: [conversation ids], touchedBy?: [conversation ids], owner?: {name, me}}`;
      `owner` is who made the project (`me`: the person asking), read from `<project>/.owner.json` that a create
      writes beside `meta/`; it is absent for a project made before owners were recorded, and none is guessed.
      `convs` are the v2 conversations whose folder is the project's, so a card shows the state of its chat from the
      conversation's own channel. `touchedBy` are the other conversations, of any folder, that worked on the project:
      their agent wrote a file under its folder (Write/Edit/MultiEdit/NotebookEdit, apply_patch, a shell command that
      writes there) or named it to the design CLI (`open design <id>`, `design <cmd> <id>`), the latest activity
      first. The runtime reads it from the transcripts (an index per transcript that grows with it, kept in
      `v2/designly-chats.json`, the transcripts never read before read behind at idle priority), so the chats from
      before the link count too; an older runtime leaves it out.
    - `GET /api/v2/designly/systems` answers `[{id, label, desc}]`: the design systems the library's template carries
      (`project/_ds/*`, named by the folder without its uuid, labelled by its README's heading), the projects of kind
      `ds`, and the UI kits installed on the machine (`kit:<id>`). `designSystem` of a create is one of these ids.
    - `POST /api/v2/designly/pin` takes and answers `{id, pinned}`; a project that is not in the library is 404.
    The kind of a project and the pins are kept by the runtime in its data folder (`v2/designly.json`), not in the
    project: a project made elsewhere is a `site`, unpinned. Create and pin are refused to other origins (item 41).
43. **Side chat.** `POST /api/v2/conversations/{id}/side-chat` takes `{question, excerpts?, history?, model?}` (the
    Legacy side chat's body without `tabId`; the conversation is the tab) and answers a server-sent stream: `data:
    {"type":"delta","text"}` frames, then one `{"type":"done","text","costUsd"?}` or `{"type":"error","code","message"}`,
    and a `: ping` comment every 15 s. The answer is a fork of the conversation's Claude session in its folder, never
    saved; closing the stream stops it; one question runs per conversation. A question that cannot start is a JSON
    `{error, code}` with a status, not a stream: 400 `bad-request`, 404 (the conversation is not here, or the machine
    has no side chat), 409 `no-session` (the agent has no saved session yet) or `busy` (the previous question still
    runs), 422 `unsupported` (not a Claude conversation). Refused to other origins (item 41). The fake answers a Claude
    conversation in a few deltas; a question with "busy" in it is refused `busy`, one with "fail" ends in an `error` frame.
44. **Files.** The folder of a conversation (its `cwd`) can be browsed beside the chat:
    - `GET /api/v2/conversations/{id}/files?path=<folder>` answers `{path, entries: [{name, path, kind: file|directory}],
      truncated}`; `path` is relative to the folder and empty for the folder itself; entries are folders first, then
      files, by name; links and special files are not listed; at most 2000 entries are looked at (`truncated`).
    - `GET /api/v2/conversations/{id}/file?path=<file>` answers `{path, content, revision, bytes}`: UTF-8 text of at most
      1 MB; `revision` is what a save must name. A picture or other binary is not read here: the page opens it by its
      absolute path (the platform's file preview).
    - `PUT /api/v2/conversations/{id}/file?path=<file>` with `{content, revision}` saves the whole text and answers the
      file as it is now (a new `revision`). The file is replaced whole (a temporary file beside it, renamed over, the
      mode kept); a file that changed since `revision` was read is refused 409 `conflict`; a file the person cannot
      write is 403. Same-origin only (item 41); the body is at most 6 MB (1 MB of text, every byte escaped).
    A path that leaves the folder by name or through a link, or is absolute, is refused. Refusals are `{r: "refused",
    code, message}` with the message worded for the person: 400 `bad-request` (no path for a file, a path over 4096
    bytes or with a NUL), 403 `forbidden`, 404 `not-found` (the conversation or the path is gone, a file where a
    folder was asked), 409 `conflict` (a save over a file that changed), 415 `unsupported` (not text, not UTF-8, over 1 MB, not a regular file), 500 `internal`; 404 as
    well where the machine has no files. The fake serves one small project to every conversation (`src/`, `docs/`,
    `README.md`, `package.json`); the paths `forbidden*` and `binary*` are refused as above, `readonly*` cannot be saved; a
    save is kept in memory for the conversation.
45. **Tasks.** `GET /api/v2/conversations/{id}/tasks` answers the plan the agent keeps while it works: `[{id, subject,
    status: pending|in_progress|completed|blocked}]`, in the order the tasks were made, `[]` for an agent that keeps none.
    Claude's TaskCreate/TaskUpdate and Codex's plan updates are not steps of a turn (the feed holds no entry for them),
    so the page asks again when the turn moves (a step, a finished turn). The id is the agent's own: a number for
    Claude, the line's text for Codex. A TaskUpdate that names a task made before the transcript the runtime has read
    (older than its last 2000 entries) is not in the list, and one without a status sets `pending`. The fake gives the
    first conversation a plan of four tasks, the others none.
46. **Machine tool.** The feature `machine` is announced by a runtime that serves the Machine tool of the page's right pane
    (the live runtime and the fake). The page asks again every 2 s while the tab is on screen; nothing is pushed.
    - `GET /api/v2/machine/stats` answers `{at, cpu, cores, mem, net, disk, history, procs}`. `cpu` and `cores[]` are the busy
      share (0…1) of the processor and of each core since the reading before (`/proc/stat`, iowait counts as idle); `mem` is
      `{used, total}` in bytes (what a new program cannot have: total − MemAvailable); `net` is `{rx, tx}` bytes per second
      over the machine's own links (loopback, bridges and container pairs are left out, they carry the same bytes twice).
    - `history` is the last hour, one `{at, cpu, mem, rx, tx}` per two minutes (at most 30, oldest first), the last being the
      newest reading, so the chart ends where the figures beside it are. The runtime reads `/proc` every 5 s into a ring of
      an hour in memory; a restart starts it empty, and a stretch the runtime was not running has no point.
    - `disk` is the filesystem of the runtime user's home: `{path, total, free, parts, measuredAt, partial}`. The filesystem
      says how much is used, not whose it is, so the runtime adds up the folders it knows by name in the background (every
      10 minutes, at low priority, within 45 s): `workspace` (the workspace folder; when that is the home folder, its hidden
      entries are left out), `caches` (the tools' caches: `.cache`, `.npm`, `.pnpm-store`, `.gradle`, `.m2`, `.cargo`, `.bun`,
      `go/pkg`), `agents` (`.claude`, `.codex` and the runtime's own data folder); `other` is the used part none of them
      accounts for (Docker images, system files, everything else) and `free` is what is available. Sizes are blocks on disk,
      on the same filesystem, links not followed, a file with several names once. `measuredAt` is null until the first walk
      ends (all that is used is then `other`); `partial` says the last walk ran out of time, so the named parts are lower
      bounds.
    - `procs` are the heaviest processes of the runtime's user by memory and processor together, the heaviest first (the
      Legacy panel's list, `internal/machines`): `{pid, startTime, name, command, cpu, rss, kind, port, stuck, guard}`.
      `cpu` is the share of one core, like top (a busy multi-threaded process passes 100); `rss` is bytes; `command` has
      secrets hidden and is cut at 160 characters; `name` is the program and what it runs ("node · vite"). `kind` is
      `service | agent | dev | other`; `guard` is true for what is shown and never stopped: the runtime and everything above
      or under it, tmux, a service of the platform, an agent of a chat and its tools. `stuck`: a dev server on `port` that did
      not answer for three looks in a row.
    - `POST /api/v2/machine/processes/stop` with `{pid, startTime}` (as listed) sends the process SIGTERM, and SIGKILL when it
      is still there 5 s later, and answers `{stopped: true, signal}`. Everything is checked again on a fresh read of `/proc`:
      it is the process the page was shown (the start time is its, the pid may have been reused), it is the runtime user's
      (real and effective uid), it is not guarded, and it is among the heavy processes the list offers now. A refusal is
      `{r: "refused", code, reason, message}` with the status of its code: 404 `not-found` / `gone`, 409 `conflict` /
      `changed`, 403 `forbidden` / `foreign`, `protected`, `not-stoppable` or `not-listed`, 503 `timeout` / `busy` (the scan
      did not end in time), 500 `internal` / `failed`; a body that is not `{pid ≥ 2, startTime: digits}` is 400. A stop is
      refused to other origins (item 41): it ends a process. A machine without the tool answers 404 to both.
    The fake serves eight cores on slow waves, an hour of history, a disk of 128 GB and five processes (two guarded); a stop
    ends a process until `POST /api/v2/fake/reset`.
47. **Apps.** The feature `apps` is announced by a runtime that serves the Apps screen of «More»; `apps-hub` by one that
    serves the screen's present shape (a left list of apps, each a page of its own), and a page that needs it tells an older
    runtime to be updated instead of failing to decode. `GET /api/v2/apps` answers `{at, staleDays, apps}`; the page asks
    when the screen opens and again every minute while it is on screen. It reads and changes nothing, so it is not refused
    to other origins. `apps` has the tracker and GitLab, in the order of the list; each is `{id, state, reason, host, me,
    sections}`:
    - `state` is `ready` (the sections are the app's answer, any of them may be empty), `off` (the machine is not set up
      for it: `reason` `not-configured` when no address is named, `no-token` when the person's token is not on the machine)
      or `failed` (it is set up and did not answer: `reason` `denied` when the service refused the token, `unreachable`
      otherwise). `reason` is null exactly when `state` is `ready`, and an app that is not ready has no sections. `host` is the
      service read (null while none is named); `me` is the person's login there (null when not read).
    - The tracker's `sections` are `epics` (every epic assigned to the person, any space, unresolved ones and those resolved in
      the last 30 days) and `tasks` (every other issue assigned to the person, the same window). Nothing is filtered by who
      made it or by the task-card key, so other spaces (`OPS-…`) are there; an epic is told by its `Type`. GitLab's only
      section is `mrs`: the open merge requests (any author, up to 200) and the last merged and closed ones. The page does
      the search, filters and sorting over them. A merge request's pipeline and approvals are read per request for the first
      40 open ones; a failed read leaves them null. The tokens are the ones the task card and the merge request pane read
      (`YOUTRACK_TOKEN`, `GITLAB_SELF_TOKEN`), and the first of `RUNTIME_GITLAB_HOSTS` is the GitLab read; an answer that is
      ready is kept for 30 s, one that is not is asked again at once.
    - A row is `{key, id, title, url, state, flag, tone, project, person, target, createdAt, updatedAt, closedAt, author,
      assignees, reviewers, me, labels, priority, kind, parent, progress, children, mr}`. `key` is unique on the screen,
      `id` is what the person reads (`ABC-2628`, `!502`), `url` opens it in the service. `state` is the service's own name
      (the tracker's, in the space's own language; a merge request's `opened`, `merged`, `closed`); `person` is the assignee
      or the author as the service names them; `labels` are the tracker's tags or the merge request's labels; `me` says in
      which roles the person stands on it (`author`, `assignee`, `reviewer`). A task has a `kind` (its `Type`), a `priority`
      and a `parent` `{id, title, url}` (its epic); an epic has `progress` `{done, total}` over its subtasks and `children`
      `{id, title, url, state, done, person}` (at most 60). A merge request has a `flag` (`review`, `conflict`, `ci-failed`,
      `no-reviewer`, `open`), its `project`, `target`, and `mr` `{source, draft, conflicts, needsRebase, ready, unresolved,
      comments, pipeline, approvals}`: `pipeline` is `success|failed|running|pending|canceled|skipped|manual` or null,
      `approvals` is `{given, required}` or null. `tone` is how the status is coloured. Titles are one line, without
      control characters, cut at 300; every list is an array, never null.
    The fake gives both apps their rows and, on `POST /api/v2/fake/apps` `{app, state}`, turns one of them `off` (without a
    token), `failed` (unreachable) or `ready` again; `POST /api/v2/fake/reset` makes both ready.
48. **Settings.** The feature `settings` is announced by a runtime that serves the Settings screen's agent files (the live
    runtime and the fake). `GET /api/v2/agent-settings/<claude|codex>` answers the CLI's settings file on the machine
    (`~/.claude/settings.json`, `~/.codex/config.toml`; `$CLAUDE_CONFIG_DIR` and `$CODEX_HOME` move them) as
    `{provider, path, rev, account, options, rules, env, mcp}`; any other provider is 404, a file that does not read is 500
    `internal`, a missing file is a CLI with nothing set. `PATCH` with `{rev, patch}` changes it and answers the same record;
    `rev` is the file's content hash the page read, a file that moved since is 409 `conflict` and nothing is written. The
    write is pessimistic: the page shows a value when this answer carries it. Both are read and written whole by the
    runtime, never by the page, and a write is refused to other origins (item 41).
    - `options` lists every option the CLI takes, in a fixed order, each `{key, value}` with `value` null while the file does
      not set it (the CLI's own default then applies). The keys and their values (`AgentOptionSpecs` in `agentsettings.go`):
      Claude Code `model`, `effort` (`low…max`), `baseUrl` (`env.ANTHROPIC_BASE_URL`), `fallbackModel`, `thinking`
      (`alwaysThinkingEnabled`), `outputStyle`, `permissionMode` (`permissions.defaultMode`: `default`, `acceptEdits`, `plan`;
      a file may also hold `bypassPermissions`, `auto` or `dontAsk`, which are shown and not written), `sandbox`
      (`sandbox.enabled`), `projectMcp` (`enableAllProjectMcpServers`), `compactWindow` (`autoCompactWindow`, 100000…1000000
      tokens), `memory` (`autoMemoryEnabled`), `keepDays` (`cleanupPeriodDays`), `hooks` (the inverse of `disableAllHooks`: true
      while the hooks run), `coauthor` (`includeCoAuthoredBy`), `updates` (`autoUpdatesChannel`: `stable`, `latest`); Codex
      `model`, `effort` (`low…xhigh`), `baseUrl` (`openai_base_url`), `summary` (`model_reasoning_summary`), `fast`
      (`service_tier` is `fast`), `sandboxMode` (`sandbox_mode`), `network` (`sandbox_workspace_write.network_access`),
      `approval` (`approval_policy`), `search` (`web_search`), `history` (`history.persistence` is not `none`). A value the
      file holds that is not one of these is read as unset.
    - `rules` (Claude Code only, null for Codex) are `permissions.allow`, `ask` and `deny`; `env` (Claude Code only, empty for
      Codex) is the settings' `env` without `ANTHROPIC_BASE_URL`, which is the option `baseUrl`. A variable whose name has
      `KEY`, `TOKEN`, `SECRET` or `PASS` comes as bullets and its last four characters (`secret: true`), never whole.
    - `mcp` lists the servers the CLI has for the person, not those of a project or a plugin: Claude Code's `mcpServers` of
      `~/.claude.json`, Codex's `[mcp_servers.*]`. `target` is the program's name or the address's host (no arguments, no
      path, no key). `enabled` is false when Claude Code has a deny rule `mcp__<name>` or Codex has `enabled = false`.
    - `account` is how the CLI is signed in on this machine: `subscription` (Claude Code's `.credentials.json`, Codex's ChatGPT
      login), `key` (an API key in the settings' `env` or the environment), or not signed in. `plan` and `who` come from the
      files the CLI keeps beside its sign-in (`oauthAccount`, the id token's claims); a token or a key never leaves.
    - A patch names what it changes: `options` (a null value unsets the option), `rules` (the three lists whole), `envSet`,
      `envUnset`, `mcp` `{name, enabled}` (Claude Code adds or removes the deny rule, Codex sets or removes `enabled`). What
      it does not name keeps its place and its bytes: JSON keeps the order of its keys and the keys nobody models; TOML is
      edited line by line, comments and tables kept, and read again with a parser before it is written — a file this cannot
      change safely (a dotted key, a key twice) is 400 `bad-request`, as are a key the CLI does not take, a value out of its
      set, an address that is not http(s), rules for Codex, and an empty patch. Skills are not here: the page reads them
      from the command catalog (`/api/<platform>/commands`).
    The fake keeps both files in memory, revisions included; `POST /api/v2/fake/reset` puts them back.
49. **Open.** `GET /api/v2/conversations/{id}/open?ref=<ref>` answers where a chip or a link of the conversation opens in
    the right pane: `{tab, target}`, `tab` one of `task` (a tracker key, or a task's page on `RUNTIME_TRACKER_HOSTS`;
    `target` is the key), `gitlab` (a merge request or a commit on `RUNTIME_GITLAB_HOSTS`; the link), `artifacts` (a
    document or a board of the portal; the reader's path or the board's frame) and `browser` (any other http(s) address).
    The rules are the agent's own `open` command (`internal/panel`), so a click and the command open the same tab. `!12`
    is the merge request of the GitLab project the conversation's folder is a checkout of (the first of its remotes on a
    GitLab server of the deployment). What opens nothing (an unknown key, `!12` outside a GitLab checkout, a link that is
    not http(s)) is 404 `not-found`, as is a conversation that is not here; an empty `ref` or one over 512 bytes is 400. It
    reads and changes nothing, so it is not refused to other origins. The fake answers by the same rules from the
    runtime's environment, with no checkout behind any conversation.

    A merge request is named `!12`, or with its project in front, as the text names it: `harnessly !80`,
    `group/sub/project !12` (one space; the ref is the word or path before the number and the number). The project is
    looked for in this order, and the first step that finds the merge request answers: (1) with a project named, the
    projects the chat knows (the ones its merge request links, its folder's checkout, the repositories one level below
    its folder, by their path, their last segment or their folder's name; a path with a slash that matches none is asked
    of the first GitLab of the deployment as it stands, and a bare name is looked up in that GitLab's project search, the
    projects whose last segment it is) whose GitLab has that merge request; a name that no project has is a word of the
    sentence and is dropped; (2) the merge request links the agent and the person wrote in the
    chat (`!12` in a link of the text; what tools printed and links to other servers than `RUNTIME_GITLAB_HOSTS` are not
    read), the latest first; (3) the GitLab project of the folder's checkout; (4) when the folder is not a checkout,
    the GitLab projects of the repositories one level below it. Steps (1), (3), (4) ask the GitLab, with the token the
    merge request tab reads with, whether the merge request exists (at most 10 requests at once, 6 s each, 8 s for the
    whole ref; yes is kept for 30 minutes, no for 2 minutes), so a ref that answers is a merge request that opens. A ref that fits several
    projects answers with `choices` (`tab`, `target`, `label` the project, `detail` the merge request's title or null),
    2 to 8, the best first; `tab` and `target` are the first of them. Of the repositories under a folder (the first 64 by name) the
    merge requests changed last come first.
50. **Open, many at once.** `GET /api/v2/conversations/{id}/open-refs?ref=<ref>&ref=<ref>…` (1 to 32 refs, each as for
    `open`) answers `{refs: [{ref, open}]}`, every ref in the order asked; `open` is what `open` would answer, or null
    where it would answer 404 (so nothing is an error here but the conversation not being here, 404, and a missing or
    oversized query, 400). The page asks it for the references of the messages on screen and draws as something to click
    only the ones that open.
51. **The session's goal.** `goal {opId, id, text}` sets the `/goal` of a Claude conversation: the runtime queues the
    command message `/goal <text>` (an empty `text` queues `/goal clear`), so it goes into the agent's box at the
    first moment the agent can take it, like any queued command. The answer is `queued` on `conv:<id>`; the new goal
    shows on the record (`goal`) when the transcript says it. `text` is one line of at most 2000 characters (else
    `refused{bad-request}`); a Codex conversation is `refused{unsupported}`.
52. **Background work.** A conversation record carries `bgAgents` (absent when 0): how many pieces of background work the
    agent waits for — subagents, commands, monitors and its own scheduled wakeup together (the name is from when only
    subagents counted; a client that knows nothing else still draws the chat as at work). `bgWait` (absent when 0) says
    what it is made of: `agents`, `teammates`, `workflows`, `commands`, `monitors` (each absent when 0), `command` (the title, when one command
    runs) and `wakeupAt` (epoch ms, absent when none pends; a wakeup whose time has come is no longer waited for). A
    `BackgroundTask` of kind `command` that is a Monitor carries `monitor: true`; a wakeup is no task. A named teammate
    (an Agent call with a `name`, Claude Code's agent teams) is a `subagent` with `teammate: true`: its `title` is the
    name, its `role` what it was started for, its id the agent id that names its transcript (so it opens in the pane as
    a subagent does). It is `running` from its spawn or from the lead's next `SendMessage` to it (`startedAt` is when
    that stretch began), `idle` from its `idle_notification` (`endedAt` is then; an idle notice written before the last
    message sent to it is about the work before it) and `done` once it shut down (`shutdown_approved` or the system's
    `teammate_terminated`) or its agent is gone; only a teammate is ever `idle`. A teammate at work counts in `bgWait`
    as `teammates` (and in `bgAgents`); an idle one waits for a message and nothing waits for it. A dynamic workflow
    (Claude Code's `Workflow` tool: a script that starts subagents in the background, phase by phase) is a task of kind
    `workflow`, its id the task id the launch named; only it carries `workflow`: the `name` and `description` of the
    script's `meta`, the `toolUseId` of the Workflow call (its tool entry in the feed, null when unknown) and its
    `phases` in the script's order, each with the `agents` its journal started in it so far — `id` (names the agent's
    transcript), `label`, `status` (never `idle`; one left at work by a run that ended is `stopped`), `startedAt`,
    `endedAt`, `model`, `last` (its last step) and `result` (the first words of what it returned), the unknown ones
    null. It is `running` until its task notification or its final state says how it ended, listed as long as an
    ended subagent would be, and one at work counts in `bgWait` as `workflows` (and in `bgAgents`). All of it goes only to
    a page whose socket address names the cap (`/api/v2/socket?caps=workflows`, a comma list; not in `hello`, which an
    older runtime would refuse): to any other page a workflow's `background_upsert` is a `background_removed` of its id
    (same seq), a snapshot leaves workflow tasks out, and `bgWait.workflows` is taken out of `bgAgents`. A chat whose
    prompt is free but waits for any of it is still at work; the sidebar, the rail and the top tab draw it so and say
    what it waits for, and it becomes finished (and unread, with a notification) only when all of it is over.
53. **Reordering the queue.** `queue.move {opId, conversationId, clientId, beforeClientId}` puts the queued message
    `clientId` right before the queued message `beforeClientId` (`null`: at the end of the line). The effect shows as
    a `queue_changed` on `conv:<id>` (the new `items` order, `rev` up) and the messages' `position`s that follow; the
    answer is `accepted`. Only a queued message moves: one being typed, waiting in the agent's input or past that is
    `refused{too-late}`, an unknown `clientId` is `refused{not-found}`. The order keeps what the line always had: the
    messages sent «now» or into the turn stand ahead of the ones that merely wait, so a place that would put a message
    across them, or before a message that has left the line meanwhile, is `refused{conflict}`; a move that changes
    nothing is `accepted` at once. A runtime that has the command announces the feature `queue-move` in `hello`; the page
    draws no drag handle without it.
54. **Machine health.** `machine.health` (absent where a source does not judge it, as a gateway's own record) is the
    machine's verdict on its load: `{level, since, issues}`, `level` one of `ok`, `warn`, `critical` and the worst of
    `issues`, `since` the epoch ms it came to that level, `issues` `[{metric, level}]` with `metric` one of `disk`, `mem`,
    `cpu` and `level` `warn` or `critical`, worst first and then in that order (empty when ok). The runtime judges it
    every 5 s (`internal/machines/health.go`, one table of lines): disk of the data folder warn under 10 % or 10 GiB free,
    critical under 5 % or 3 GiB; memory (MemAvailable) warn from 85 %, critical from 95 %, or a swap-in of 50 / 500
    pages a second; CPU warn only, busy 90 % over the last two minutes, never one sample. A level is left only past a
    margin (disk 20 % above the line, memory 3 points below, swap-in at half, CPU under 80 %). A change goes out as a
    `machine_status` carrying `health` to every subscriber of the machine channel, with load or without: the page holds
    the channel of every machine (without load, the open one with it) so the other machines' dots are live. A
    `machine_status` without `health` leaves the health the page holds. The page draws what the runtime said and keeps no
    lines of its own: the machine's dot and the header's machine amber or red, a reason under each line of the card past
    its line, and for `critical` a banner at the top of the sidebar and a row in the notification centre.
55. **A decision the page could not deliver.** An open gate may carry `refused` (absent otherwise), one of
    `menu-unmatched`. The runtime presses an option only when the menu on the agent's screen is the one the person
    decided on, read by the strict rules of `internal/typist/gates.go`. When the same call still asks but the screen
    cannot be matched (an unknown dialog layout, a cut call), no key goes in and the gate opens again with
    `refused: "menu-unmatched"`. The card then says «decide in the terminal» and offers to open the chat's terminal on
    the agent's pane. The next state of the gate drops the field.
56. **Stopping a chat's terminal.** `terminal.stop {opId, id, force?}` ends the conversation's terminal: the agent and
    everything under its pane get SIGTERM, what stays SIGKILL after 3 s, then the tmux session goes. The conversation
    and its history stay: the record goes out as a `conversation_upsert` on `convs` with `state` `stopped`
    (`resumable: true`) and `terminalStop: {at, by: "person"}`, and the answer is `accepted` from that seq. The next
    message resumes the agent on its session the way any stopped agent resumes (the resume makes the session again in
    the tab's folder); once the agent is up again, `terminalStop` is gone from the record. A terminal that does not run
    (`stopped`, `gone`, `launch-failed`, `starting`, or one being stopped) is `refused{agent-gone}`; a working or asking
    agent without `force: true` is `refused{agent-working}`; a session the runtime does not own (the one it runs in
    itself, one of no tab) and a conversation with no agent session to resume on are `refused{unsupported}`; a stop
    that failed is `refused{internal}` and the agent keeps running. A runtime that has the command announces
    `terminal-stop` in `hello`; the page offers «Завершить терминал» only then.
57. **Idle terminals stop by themselves.** Every 5 minutes the runtime looks at its chats and stops the terminal of one
    that stood idle for the whole window, as `terminal.stop` does, with `terminalStop: {at, by: "idle", idleMs}`. Idle is
    every one of these at every look of the window: the agent at its prompt (`idle`, `ready`) with no gate, nothing
    queued, typed or waiting to be applied; no background work (`bgAgents`, a running background task, a scheduled
    wakeup); nobody holding `conv:<id>` or attached to the pane; nothing in the pane's process tree but the agent, its
    helpers (what it started outside a tool shell: MCP servers, the status line) and shells — a tool shell of the agent
    (Claude's `-c … shell-snapshots … eval`, Codex's `bash -lc`) or any other process in the pane is work; the tree's
    processor time under 2 % of a core for the helpers and 5 % for the agent between two looks; the agent's screen the
    same (digits aside); no activity on the record (a prompt, a turn's end, the state's change, a read) after the
    window began. Any of them failing starts the window again. The window is `GET /api/v2/runtime-settings`
    `{idleStop: "off" | "1h" | "2h" | "6h"}` (`2h` until set), changed by `PUT` of the same object and kept in
    `<data>/v2/runtime-settings.json`; a bad value is 400 `bad-request`. The runtime logs each stop with the
    conversation, how long it stood idle and the memory freed.
58. **Naming a chat anew.** `conversation.retitle {opId, id}` asks the cheap model of the chat's agent to name it again
    from its first message and the agent's latest answer. The answer comes when the title is made (the record's `title`,
    `titleSource: "agent"`, replaces even a title the person typed: they asked); a title that takes longer than 8 s
    is answered `accepted` all the same and arrives with the record when it is made. A title that could not be made is
    `refused{internal}`; a chat already being named is `refused{conflict}`, one with no answer of the agent yet `refused{bad-request}`.
59. **One prompt, several models.** `conversation.create` may carry `worktree: true` and `launch {id, prompt}`. `launch` marks
    the conversation as one of those a prompt started on different models (the same `id` and `prompt`, at most 200
    characters, on each; it shows on the record as `launch`). `worktree` starts it in a git worktree of its own: the
    runtime adds `<repo>/.worktrees/<name>` on a new branch `agent/<name>` off the HEAD of the repository the folder is in
    (kept out of `git status` by the repository's exclude file) and the record's `cwd` is the matching folder in it. A
    folder that is no repository, or has no commit yet, is used as it is; a worktree that could not be made is
    `refused{internal}`.
60. **Forks, handoffs and merges.** Features `fork` and `handoff`. `conversation.fork {opId, id, atTurn?}` opens a new
    conversation that branches off after a finished turn (`atTurn`, the last finished one when absent; a live turn is
    refused `agent-working`, an unknown one `not-found`). Claude forks its own session: the runtime copies the
    transcript up to the cut under a new session id and resumes the copy, so the new chat has the history up to there.
    Codex forks with its CLI, which takes the whole session: at the last turn of an agent at rest; anywhere else the
    fork starts with an excerpt up to the turn as its first message. `conversation.handoff {opId, id, provider, model,
    mode: "excerpt" | "note", text}` opens a conversation on another agent or model in the same folder and project
    whose first message is `text` (at most 90 000 characters), the context the person approved. Both answer `accepted`
    on `convs` with the new id; the source is not changed. The new record carries `origin {k: "fork" | "handoff",
    fromId, fromTitle, fromProvider, fromModel, atTurn?, via: "session" | "excerpt" | "note", at}`.
    `conversation.merge {opId, id, text}` sends a fork's result into the queue of the conversation it came from, as a
    queued message on that conversation's channel; its result's `id` is that conversation (`unsupported` for a
    conversation with no origin). The texts travel in blocks the page draws as cards: a handoff's first message is
    `<handoff from title agent model mode [kind="fork"]>` with instructions for the agent and the text, a merge is
    `<fork-result from title agent>`. `POST /conversations/{id}/handoff-draft {mode: "excerpt" | "note" | "result",
    provider?, model?}` answers `{mode, text, tokens, turns}`: an excerpt (the first request, the newest turns whole,
    the results of key commands, within 16k tokens and a quarter of the target model's window, no reasoning, tool
    state or attachments), or a note the agent writes from a throwaway fork of its session with no tools that change
    anything (Claude through the side chat's fork, Codex by `codex exec fork --ephemeral` read-only); `result` is a
    fork's note for its merge. A note takes up to 3 minutes; one is written at a time per conversation (`conflict`).
61. **Held on the usage limit, going on at the reset.** A turn whose last line is the agent's notice of its plan's usage
    limit — Claude's refusal (`error: "rate_limit"`, the reset in `quotaLimits.resetsAt`; the words «You've hit your
    session limit · resets 11:50pm (UTC)» when it has none), Codex's «You've hit your usage limit … try again at 6:05 PM»
    in its transcript or, the last thing above its composer, on its screen — ends `failed{rate-limit}` with `retryAt`
    the reset, and the record gets `limited: {provider, window: "session" | "weekly" | "other", at, resetsAt,
    continueAt}`. `resetsAt` is the reset the notice names, else the one of the provider's exhausted window in the
    limits it last reported, else null. The queue is paused `limited`; its messages have no term while it is (they were
    sent for after the reset). At `continueAt` — `resetsAt` and 22–90 s of the conversation's own jitter, at least 5 min
    from the hold when `resetsAt` was already past — the hold ends and the work goes on: the head of the queue if the
    person queued something, else the runtime's own message «Продолжай с того места, где остановился» (`auto: "limit"`
    on the message); either is noted in `autoContinues: [{at, why: "limit", clientId, own?}]` (the last 20; `own` when
    the message was the runtime's). `continueAt` is null when the runtime will not go on by itself: the runtime setting
    `limitContinue` is `off`, no reset time is known, or the person said no; such a hold ends quietly at `resetsAt` and
    the jitter's end, and what was queued goes then. `limit {opId, id, action}` decides: `now` goes on at once (the
    queue's head, else the same words as the person's own message, not `auto`, and no `autoContinues` entry), `cancel`
    drops `continueAt`, `auto` sets it again; a conversation not held is `refused{not-found}`. `queue.resume` of a queue
    paused `limited` is `limit now`. A new turn the agent takes by itself (the person typed into its terminal) ends the
    hold. The record and the pause outlive a restart.
62. **A restart that cut a turn short.** A conversation that was `working` when the runtime went down and whose agent is
    not running when it comes back (no tmux session, or a shell in the pane) gets the runtime's message «Продолжай —
    работа была прервана перезапуском» (`auto: "restart"`) once, noted in `autoContinues` with `why: "restart"`, `own`.
    The queue brings the agent back the usual way: a stopped one is resumed on its session; a lost terminal waits for
    the startup's resume of the tabs that were at work, and after 45 s without it the conversation resumes its agent
    itself. An agent still running (tmux outlived the runtime) and a conversation that was not at work are left as they
    are. The runtime setting `restartContinue: "off"` turns it off. Both switches are in `GET/PUT
    /api/v2/runtime-settings` (`"on"` until set; a PUT that leaves one out keeps it). A runtime that does all this
    announces `auto-continue`.
63. **Delegated chats.** `conversation.create` may carry `parentId` (a conversation of this machine; an unknown one is
    `refused{not_found}`) and `title` (at most 200 characters, the name from the start): an agent's `delegate_task`
    starts a chat so, and the record carries `parentId` for as long as it lives. `RuntimeSettings.delegation`
    `{maxChildren 1–10, anyChat}` bounds what agents may delegate; a PUT without it keeps the saved one. Both go only to
    a page that names the cap (`/api/v2/socket?caps=delegation`, and `?caps=delegation` on `/api/v2/runtime-settings`):
    any other page gets the record without `parentId` (an event's older form is encoded with the same seq) and the
    settings without `delegation`.
64. **Backend profiles (Kimi, GLM, Grok).** A profile is another vendor's models behind one of the agent CLIs, not a new
    `Provider`: a Kimi or GLM chat is `provider: claude`, a Grok chat `provider: codex`. A profile id is
    `^[a-z][a-z0-9-]{0,31}$` and the runtime owns the list. `conversation.create` and `conversation.handoff` may carry
    `profile` beside `model` (a model of the profile; `model.profile` in a command is refused), and the record carries
    `model.profile` for as long as it lives; it cannot be changed in a chat (a handoff starts another one).
    `MachineAgent.profiles` lists `{id, label, vendor, configured, models}` under the CLI the profile runs through;
    a page draws only the `configured` ones. All of it goes only to a page that names the cap
    (`/api/v2/socket?caps=profiles`, and `?caps=profiles` on `/api/v2/machines`): any other page gets the agents
    without `profiles` and the models without `profile` (an event's older form is encoded with the same seq, a
    snapshot is cut the same way).
    The Settings screen reads and writes the keys over HTTP, same origin only for a write: `GET /api/v2/providers` →
    `{profiles: [{id, label, vendor, via, host, hint, key: {configured, tail}, models}]}` (every preset of the
    runtime); `PUT /api/v2/providers/<id>/key` `{key}` → `{configured, tail}` (the key is 8–512 printable characters
    without a space); `DELETE /api/v2/providers/<id>/key` → `{configured: false, tail: null}`;
    `POST /api/v2/providers/<id>/test` → `{ok, reason, status, ms}`, one minimal request to the vendor with the saved
    key. An unknown id is `not-found`, a bad key `bad-request`. The key never comes back: `tail` is «…» and its last
    four characters. The key is not logged, not on the wire and not in a tab's metadata.
65. **A message does not hang.** A typed message (not steered, not carried by a launch) that the agent has not taken
    within `ConfirmDeadline` (30 s) — no prompt-submit, no transcript entry — is not failed: the transcript is asked
    once more, and without its entry the message goes back to the head of the queue as `queued{position: 1, reason:
    "stalled", wait: null}`. Its text is taken out of the agent's input box at the next look (a late Enter must not
    send it behind the person's back), it is never typed again by itself, and the messages behind it wait: something
    beyond this message is wrong. `retry` types it again (attempts+1, the transcript asked first), `promote` does the
    same at its new place, `cancel` drops it; an entry with its text read later accepts it. A steered message is
    Claude's to read at its next step and keeps its 30 min. A message left `typing` 30 s with no try on its way stalls
    the same way. The stall goes only to a page that names the cap (`/api/v2/socket?caps=msgstatus`, and
    `?caps=msgstatus` on the status page): any other page gets the same rev as `failed{unconfirmed, retryable: true,
    at}` and a queue without its id.
    `GET /api/v2/conversations/{id}/messages?ids=a,b` (1–20 client ids, none twice) answers `{messages: [...]}`, one per
    id in order: `{clientId, k: "known", message, handed?: {at, where, entryId}}` — `where` is `input-box` (typed, not
    taken), `agent-queue` (a steer in the agent's own queue), `agent` (its hook took it), `transcript` (`entryId`), and
    `handed` is absent for a message not typed (queued, failed, cancelled); `{clientId, k: "unknown"}` for an id the
    runtime never had; `{clientId, k: "expired", at}` for one whose record it let go (after a day; the ids are kept 7
    days, at most 500 a conversation). A `send` with an id the runtime still has is accepted with its whole record
    again (`message_upsert`), so a page that lost the events takes the true state; one of an expired id is
    `refused{too-late}` — it was delivered, and is never typed twice.
