# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[Semantic Versioning](https://semver.org/spec/v2.0.0.html); while it is in `0.x`, a minor version may change the API.

## [Unreleased]

### Added

- The machine's own projects and favorites go into its person's sidebar on the hub by the move (kind
  `sidebar`, `POST /store/migration/v1/sidebar`): merged as the page merges a machine, without the page open; the
  journal counts it verified once the sidebar lists the machine. A hub with its history off keeps it skipped
  (`MIG-NOT-KEPT`) and is asked again each pass.
- A runtime under a hub with a store moves by itself what an older version kept only on its disk (HRN-883,
  `docs/migrations/0001-data-to-hub.md`): the pictures of its chats, the files its agents handed over (found by their
  records in the transcripts, read at most 32 MB/s) and the handoff notes go into the store by sha256, each checked
  there after the upload. The journal is kept on the disk (`<data>/migrations/`) and in the hub; a restart goes on
  where it stopped, nothing on the disk is deleted. A failed upload is tried again by itself; a file that cannot be
  read waits for `harnessly migrate retry|skip`. `harnessly migrate status|retry|skip|verify [--json]` read and steer
  it; `RUNTIME_DATA_MOVE=off` turns it off.
- The hub keeps the journal of the move of the machines' own data into its store (`docs/migrations`): one row
  per machine × kind × object with its state, error code and attempts (migration 0034). A machine reports with its
  store session (`POST /store/migration/v1/report`); a person who may open the machine reads how far it is and asks to
  retry or skip the failed objects (`/api/machines/<id>/migration`, `…/retry`, `…/skip`). A verified upload becomes its
  person's in the hub, as one the page put there. `hub doctor` lists every machine's move and, for each failed code,
  its cause and fix from `docs/migrations/0001-data-to-hub.md`, which the binary carries (`harnessly docs`, self-host).
  The capability `migration` says the hub has the journal.

### Fixed

- The hub says its release instead of `dev`: the image of a merge request, promoted to the release without a rebuild, carries `dev-<commit>`, and `HUB_VERSION` (the Helm chart sets it from the image's tag) names the release.
- A space owner's sidebar no longer merges the chats of every machine of the space: the list, the search and the page's
  first machine are the person's own machines; an administered machine is named under `others` and shows its own list
  only when picked. `POST /api/machines/{id}/owner` hands a machine registered by an admin to the member it is for,
  with its copied chats (#57).
- A link of a connection or of the documents provider opens natively before the remote browser is considered: an issue
  in the task tab through its own connection (two trackers with one key on different hosts no longer mix; the key keeps
  its case and hyphens), a wiki page in a safe reader, a document or board by the provider's embed address. A
  documents adapter may declare the new `resolve` capability to tell documents from boards; one without it behaves as
  before. A refused sign-in shows a "Connect" action and an unembeddable provider "Open original link", never a
  silently started Chrome. Click, context menu and `harnessly open` agree (GitHub #60).
- The home screen's machine setup card takes clicks again: it sat in the greeting, which lets the pointer through to
  the composer, and inherited that, so its stages, «Подробнее», «Повторить» and the safe step did nothing. The card and
  the into-project chip take the pointer while shown; a card fading out lets it through again. A browser test presses
  each with the mouse (GitHub #59).
- Personal settings (`/api/v2/me`, `/api/v2/me/settings`) are kept by the hub per signed-in user, with their own
  revision and the account's e-mail and name; they were forwarded to one runtime's single settings file, so users who
  shared a runtime overwrote each other's settings, including the main machine. The hub answers them on every route
  (the fleet, the workspace, a machine's own address), also with the machines off, and refuses a main machine the user
  may not open. A runtime's old settings file is not copied into any account; a runtime on its own is unchanged
  (GitHub #61).
- The agent asks the runtime for its chat list again (after 30 s, doubling up to 5 min) while none came: a runtime
  too busy to make the list in its budget answered nothing, and the backfill to the cluster waited for the next link
  — chats stayed not caught up for an hour and more on a loaded machine.
- A chat whose backfill a busy runtime cut (a page not given) goes on from where it stopped a minute later, the wait
  doubling up to 15 min, while the link stays; it waited for the next link.
- The chats the cluster holds whole without the steps of every turn (`v2.start.steps`, the cluster's migration 0054) are
  walked again from their steps cursor with all the steps, after the other walks; their pages carry `steps.from` and
  move only that cursor.
- The hub's store waits out a bucket busy with the same blob (409 OperationAborted, «a conflicting conditional
  operation is currently in progress»: two machines putting the same file at once) and finds the blob there or puts it
  again; it answered 500, and the machine's sync of that file failed.
- A chat opened on a machine that is on, whose chats the cluster keeps, is drawn from the cluster's copy, and the
  machine's channel is asked after it from the copy's newest entry (a snapshot "after"), not for its whole tail; past
  3 s without the copy it is asked as before. The copy's newest page is asked once for the feed and the channel.
  The snapshot after an entry carries every turn from that entry's own on, as it is now, so the turn's state (running or
  idle) reaches the page whatever the cursor; only a runtime that says so (feature `snapshot-after`) is asked that way.
- A chat backfilled to the cluster brings the steps of every turn, not only of its newest three: a walk reads pages
  of 20 turns with their steps (`…/turns?steps=all`). A folded turn the copy holds fewer steps of than the turn counts
  (chats backfilled before) is asked from the machine when it is there; it opened with its answer only.
- The agent gives a page of a long chat 90 s to come whole from the runtime (it was 15 s: the first page of a chat with a
  transcript of tens of megabytes could be cut there, and its backfill stopped on it); a cut answer is logged as
  cut, not as «runtime answered 200».
- A chat's backfill to the cluster goes on from its cursor after the runtime restarts: a page asked before a turn
  older than the tail the runtime holds now reads the transcript back to that turn; it answered 404, and the chat's
  backfill stopped there for good.
- The runtime's read back to an old backfill cursor builds the chat's feed again only when what it read has doubled,
  not after each 4 MB of the transcript: a walk deep into a transcript of 100 MB was quadratic and held the chat's
  actor for minutes.

### Added

- A person's Claude profile syncs through the hub's store (`RUNTIME_PROFILE_SYNC=hub`, `docs/hub/store.md`):
  one room per person (`profile-<user id>`, or design-sync's room bound to the user id by `HUB_STORE_PROFILE_ROOMS`), sealed on the machine
  with the room key the hub's safe keeps; design-sync's key is taken over from its identity once. Secrets never go in:
  `.gitconfig`, glab's config, the `RUNTIME_SECRET_CONFIG_DIRS` folders and the settings' token-like environment
  variables stay on the machine; `RUNTIME_PROFILE_LOCAL_KEYS` keeps more settings keys local, `RUNTIME_PROFILE_STATE_DIR`
  names the sync's state folder. The runtime installs the plugins the settings enable, as design-sync's profile agents did.
- Your Claude Code memory can follow you to all your machines (opt-in: `HUB_MEMORY_SYNC=on` on the hub,
  `RUNTIME_MEMORY_SYNC_URL` on the runtime). Only the notes of a project's `memory/` folder sync, under the project's
  git remote, and only between machines of the same person. A Claude conversation syncs its project before it starts.
  A note changed on two machines keeps both versions. `harnessly memory [sync]` shows the last sync and retries it
  (docs/hub/memory-sync.md).
- Other CLIs of a machine sign in from the hub's page like the agents do in the console (GitHub #58): the machine
  describes them in `cli-logins.json` of the agent's data folder (or `AGENT_CLI_LOGINS_FILE`) with an id, a label, a
  fixed login command, a status command and a timeout; an extension pack writes the file, the core and the hub need no
  change. The agent reports them as `clis` next to the agents, «Агенты» lists them under «Входы в CLI», and the sign-in
  runs only in the console with `BROWSER=echo`, so the CLI prints its address and its loopback callback comes back
  through the protected console (single use, its owner only). The hub starts a CLI only by an id the machine reported,
  for a space owner.

- The search over the chats, the sidebar's projects and favorites, and the turns a rewind left behind open from the
  cluster's and the hub's copy, with the machine off too: the hub serves `…/history/search`, `…/history/sidebar` (one
  document per person, written on its revision) and `…/history/conversations/<id>/rewinds/<n>/turns`, as the gateway
  does. A rewind's live `feed_reset` marks the turns it left behind, so the copy's feed no longer shows them; an undone
  rewind brings them back.
- The hub keeps the messages written to a machine that is off, like the cluster's gateway (`…/offline-prompts`): they
  outlive the tab, show on the person's other devices, and the hub hands each one to the machine as the page's own send
  command once it is back, in order and once per opId.
- A message written to a machine that is off waits in the cluster's gateway too, as the very send command the page
  sends the machine: it outlives the tab, shows on the person's other devices and goes by itself when the machine is
  back; the machine takes one opId once, so the agent gets it once whoever comes first. A hub without the gateway's
  queue keeps the page's own outbox, as before.
- A tool's whole output is in the cluster's and the hub's copy: the agent reads it from the runtime apart from the
  stream (up to 1 MiB) for every tool it sees cut, pages keep carrying its first 4 KB, and «show all» reads it from
  `…/history/conversations/<id>/entries/<entry>/output` — with the machine off too.
- Heavy checks share the machine: launchers named `go`, `vitest`, `playwright`, `tsc`, `eslint`, `npx`, `npm` and `pnpm` on the agents'
  `PATH` run builds, tests, type checks and linters through `harnessly heavy`, which gives them one of the machine's
  few slots (by its cores), runs them gently and, when the machine stays busy, tells the agent to go on without the check
  (exit 75). `harnessly heavy stats` counts the calls that waited and were refused; the workspace prompt asks agents
  to check once, at the end, only what they changed (`RUNTIME_HEAVY_LAUNCHERS`).
- The fleet's workspace on a standalone hub (`runtime.fleet`): the hub carries the runtime's page and serves it at
  `/agents/` with the home machine's settings; `/agents/api/v2/…` is the fleet, every other API call goes to
  `?machine=<id>` or the home machine, so Sinopia and the platform API work across machines too. The hub page's
  «Рабочее пространство» opens it. Only machines the hub trusts are in it, in either mode. See `docs/hub/api.md`.
- `harnessly upgrade plan|apply|rollback`: a signed release manifest (`/dl/release.json`, Ed25519 against a pinned key)
  ties the hub, runtime, agent, command line and web page to one version and lists the extension protocol ranges. `plan`
  shows what changes and which registered extensions the new core cannot speak to; `apply` replaces the programs
  together, restarts, checks and puts the old ones back on failure; the data is not touched. `tools/relmanifest` makes
  and signs the manifest. The automatic update by `/dl/manifest.json` is unchanged.
- Model profiles without a rebuild (`RUNTIME_PROFILES_FILE`, `RUNTIME_PROFILES_ADAPTER_URL`, `RUNTIME_PROFILES_SECRET_CMD`, off by
  default): a file or an adapter adds profiles to the built-in presets or replaces one by id; address, API format, key as
  a reference to a safe secret, limits and prices. See `docs/model-profiles.md`.
- The hub behind a reverse proxy and under a path: `HUB_PUBLIC_URL=https://example.com/harnessly` (and `HUB_AGENT_URL`). Routes, links,
  cookies (`Path`, `__Secure-` name under https), the web client and the machines' dial follow the path; the hub answers
  with or without the prefix. Without a path nothing changes. See `docs/hub/proxy.md`.
- `hub doctor` checks the database and its migrations, the sites' S3 bucket, OpenID providers, the public address, the
  proxy settings and the clock; problems and startup errors carry a code (`HUB-…`), what to do and a link
  (`docs/hub/doctor.md`). `--offline` skips what needs the public address.
- UI slots for apps (`HARNESSLY_UI_SLOTS=on`, off by default): view targets `settings`, `onboarding`, `object-action` and
  `hub-settings`, an app `branding` block, `hl.slot.done()`, a Settings → Extensions page and machine buttons drawn from
  packages in the existing sandboxed frame; a failing view is held by a boundary. The hub's own page is declared but not
  drawn yet. Example: `examples/ext-ui-slots`.
- Connection adapters (`RUNTIME_CONNECTION_ADAPTERS_FILE`, off by default): a product that is not built in, such as a
  corporate GitLab or an in-house tracker, connects through an HTTP adapter of the extension frame
  (`harnessly.connection/v1`: detect, link, me, projects, issue search and card, pages) without a rebuild. The
  credentials come from the runtime's safe and are not in the declaring file; built-in products are unchanged. Example
  in `examples/connection-adapter-ts`, docs in `docs/connection-adapters.md`.
- Memory store contract (`internal/memstore`: list, get, put with a version check, text search) and memory adapters
  (`RUNTIME_MEMORY_ADAPTERS_FILE`, off by default): a team's notes in a wiki or a database connect through an HTTP
  adapter of the extension frame (`harnessly.memory/v1`) and are read over the files, which stay the default and are
  used when the adapter is silent or does not have a note. Example in `examples/memory-adapter-ts`, docs in
  `docs/memory-adapters.md`.
- Documents and boards provider (`RUNTIME_DOC_ADAPTERS_FILE`, off by default): a contract for reading, publishing,
  access, comments, embed and boards; the built-in artifacts and boards stay the default, an external service plugs in
  as an adapter of the extension frame (`harnessly.docs/v1`), with a TypeScript example and docs/doc-adapters.md.
- An example extension pack (`examples/ext-pack-example`: machine adapter, memory store, tracker, secrets settings and an
  interface extension, installed by one script), a guide to building your own (`docs/extensions/build-your-pack.md`) and
  the acceptance scenario of a release, run in CI on release tags: clean install, sign-in, organization, machine, agent,
  update to the next version and rollback.
- Extension frame of the hub (`HUB_EXTENSIONS`, `HUB_EXTENSIONS_FILE`, off by default): adapters registered by config
  with a token file and an expected protocol, describe handshake, timeouts, retries with an idempotency key, a circuit
  breaker, their state in `hub doctor` and `/api/capabilities`, a Go SDK (`extsdk`) and a TypeScript example. The machine
  provider protocol now runs on this frame with the same behaviour.
- Machine adapter protocol v1 (`GET /v1/describe`, create, attach, state, power, resize), chosen by describe with
  `HUB_PROVIDER_PROTOCOL=auto`; the default `v0` is the behaviour as before. An example adapter in
  `examples/machine-adapter` and a scenario that updates the hub under a running adapter.
- Hub: `HUB_AUTH_EMAIL=off` signs people in only through OpenID Connect, GitHub, Yandex or the gateway: no SMTP, no mailed
  codes, invitations as links; the sign-in screen hides the email form. `HUB_STORAGE=s3` with `HUB_S3_*` keeps the hub's
  files in one bucket, and `hub storage migrate` moves an existing hub's files there. Both are off by default.
- The lively hub behind the `ui.hx.live` feature: one motion system on every screen (cascade, count-up numbers,
  one skeleton, hover and press), analytics periods of 24 hours, 7 and 30 days or any dates kept in the address,
  sortable and searchable tables, and the «Инструменты» screen over `GET /api/orgs/{org}/tools` — which MCP servers
  and skills the agents call, how often, with errors and who calls.
- Installer for a company network: `--check` looks at the machine before installing and changes nothing, `--ca-file`,
  `--proxy`, `--no-proxy`, `--mirror`, `--npm-registry` and `--pip-index` reach every component (`SSL_CERT_FILE`,
  `NODE_EXTRA_CA_CERTS`, `NO_PROXY`, and the container profile's mounts), the hub installer tells a Docker daemon that
  already runs from one it can start of its own (`--docker external|own|auto`), `--offline DIR` installs from a bundle
  that `scripts/make-offline-bundle.sh` builds, every step goes to a journal, and running again changes nothing that is
  already right. `harnessly install` runs it with the check first.
- Several hub replicas (`HUB_REPLICAS=on`, off by default): the machines' links get an owner with a lease and a fencing
  token in a table in Postgres, a request for a machine on another replica reaches its owner over an internal hop (calls
  and tunnels), a replica that lost its lease writes nothing, and a killed replica's machines reconnect to a live one. The
  Helm chart runs 2 replicas with a rolling update on a shared Postgres and stays one replica on SQLite
  ([docs/hub/replicas.md](docs/hub/replicas.md)).
- Documents and boards next to the hub (HUB_DOCS, off by default): `doc.publish` and `doc.acl` from agents, `/api/platform/docs`
  with threads, comments and rights kept in the hub, and "not set up" with the reason on the screens instead of a 404.
  `compose.docs.yaml` and the chart's `docs.*` run the artifacts and boards services beside the hub; see docs/hub/docs.md.
- MCP servers in Settings → Connections: a catalog of well-known remote MCP servers (Atlassian, Linear, Notion, Sentry,
  GitHub, Stripe, …) and any MCP URL, streamable HTTP or legacy SSE. The runtime finds how a server signs in: the
  service's own sign-in in a popup (OAuth with dynamic registration and PKCE), a pasted token, or nothing. Every Claude
  Code and Codex chat started afterwards gets the enabled servers' tools as `mcp-<name>` through the runtime's proxy,
  which adds the credentials and refreshes them; agents never hold the service's token.
- Connections: Settings → Connections takes any link of the person's tracker, wiki or code host (Jira and Confluence in
  the cloud or on Server / Data Center, YouTrack, Linear, GitLab, GitHub, Gitea, Redmine, Yandex Tracker, Notion and
  self-hosted wikis, on any domain and path), finds the product and its version, checks the credentials and keeps them
  in the safe. The tracker app, task cards and link previews read the primary connection in the same shapes as the
  deployment's tracker, which is listed as a connection itself; agents read issues and pages with `harnessly
  connections`, `issues`, `issue`, `pages` and `page`, never with a token of their own.
- Sinopia video: scenes run on one time contract. `tkVideo.motion` puts GSAP (with its plugins and `tk.js` presets, served by the library at `/__lib/motion/`) on the scene's time, `tkVideo.register` adds any other engine. The check now finds overlapping tweens, engine and script errors, outside addresses and motion past the scene, walks the scene in time, and compares frames with a tolerance. `design video frame --sheet` gives a contact sheet. Final renders take PNG frames at CRF 16, preset slow, tagged BT.709.
- Sinopia video: the rules are read on demand with `design video guide [topic]`, and a chat working in a video project gets `sinopia_view_*`, `sinopia_frame`, `sinopia_check` and `sinopia_video_render`; other chats carry none of this. If the person just moved the player, the agent's command becomes a button.
- Dictation: Settings → Voice input has «My words» (a personal dictionary kept with the account: a word, optionally how it
  is heard; the runtime puts it over every partial and final text of any model, and finds a word by sound when no
  variant is given) and a choice of the recognition model: the machine's own, OpenAI gpt-4o-transcribe (Realtime API) or
  Yandex SpeechKit v3 (streaming). Their keys are saved in Models and keys; a model without a key cannot be picked, and the
  audio leaves the machine only for the picked cloud model.

- Dictation: the recognition model «ChatGPT plan (unofficial)» recognizes on the plan the machine's Codex CLI is signed in
  with, whole after the stop, through an undocumented endpoint (docs/voice.md says what that costs).

- «Сразу» (⌃↵ in the field, ↑ on a queued message) while several messages wait sends them all as one message now, in
  the queue's order, a blank line between them, every attachment kept; the queue is empty after it. Commands («/…»,
  «! …») still go alone.

- No machine is picked in the sidebar any more while the cloud keeps the list: the chats, the projects and the favorites
  are the person's whichever machine the page goes through, and the machine card at the bottom is the open chat's
  machine (the default one on the start page) with its load. A new chat starts on the default machine; another is
  picked in the model's popover («ВМ …» under the depth), which brings that machine's agents and models. The start
  page's machine pill is gone.

- A chat of another machine than the picked one is written to as it is, on its own machine: no bar over the field, no
  «Продолжить здесь». The row is the chat's title alone; where the chat lives is the quiet first line of its menu, with
  «Продолжить на другой ВМ» right under it.

- The sidebar is the person's, not a machine's: the projects (folders) and the favorites' layout are one document the
  cloud keeps per person (`GET/PUT …/history/sidebar`, written on its revision, a conflict taken and made again), the
  same whichever machine is picked; a project of the person stands for projects of the machines and gets its folder on a
  machine when a chat moves in or starts there. Each machine's own projects and favorites are merged in by name on first
  sight, nothing lost. The start page picks the machine of a new chat (the default one is the machine picked in the
  sidebar). Projects of two machines with the same id no longer take each other's place on the page.

- Video: the model behind the «Видео» type — state with scenes on a time line (moments read from `data-tk-at` / `data-tk-out` /
  `data-build-in`, automatic length), settings, a scene's length and transition, comments on a moment or a stretch.
- Video: rendering to MP4 — scenes become cached clips (ffmpeg, one tab pair, paused under load), joined without re-encoding, subtitles burned in on a second pass, published as `video.mp4` / `video-9x16.mp4` with `chapters.json` and `video.json`.
- Design video: `harnessly design video get|scene|frame|check|render|comments` for agents, the «Video (kind video)» section of the library guide (v9) and the first-message lines of a new video.
- Design, Video: the live player — two scene frames swapped at scene boundaries, play/pause, ±5/10 s, frame step, scene and tenth jumps, I/O range, live or MP4 source, subtitle overlay.
- Design: the Video shell and create screen — bar, stage and timeline slots, a panel with the chat and comments, style, render of MP4 per format with a current/outdated mark and download, and a create screen with formats, length and subtitles.
- Video: a guest link to a video plays the published MP4 in 16:9 or 9:16 with chapters, a comment strip and range select; the link index says `deck.type: video` with its formats and length.
- Releases: a `vX.Y.Z` tag publishes archives for linux and macOS (amd64, arm64) with `checksums.txt` signed keyless with cosign
  (`checksums.txt.sigstore.json`) and a build provenance attestation, and a multi-arch container image
  `ghcr.io/ly-solutions/harnessly` (`X.Y.Z`, `X.Y`, `latest`, and `-with-agents`), signed and attested. `docs/deploy.md` starts
  with a quick start (Docker, release binary, source), how to verify and how to update.
- Design: the contracts of videos (projects of the kind `video`, a deck folder with `meta/video.json`): the video's
  JSON shapes and files (`meta/video.json`, `meta/scenes`, `.media/video`, the time fields of a comment), the
  `/design/video/*` and `/api/cli/design/video/*` routes (answering 501 until their parts land), `video` settings in
  `POST /api/v2/designly/create`, the Video tile and its create route, and the page's video readers, calls, scene
  messages, store and texts. A video still opens in the site view.
- Design: the video's engine. A video's board gets `/__lib/video-runtime.js` before its engine: the scene runs on its own
  clock (build-ins by `data-tk-at`, the board's animations seeked, a seeded `Math.random`), talks to the player by
  `tk:video-*` messages and to headless Chrome by `window.tkVideo`. `GET /design/video/frame` and `design video frame`
  draw a cached JPEG of a scene at a time (draft or full size), `design video check` lints the boards' scripts and finds
  moments past a scene's end, text that overflows, blocks off screen, small text in 9:16 and frames that do not repeat
 .
- Presentations: sharing a deck never waits for its PDF. The link is made and can be copied at once; the runtime keeps
  the guests' PDF (`POST /design/deck/pdf`, a marker in `.media/pdf-auto`), builds it in the background, again a few
  seconds after the deck stops changing, and publishes each build the usual way. The share sheet says how the PDF is
  doing in one quiet line, and an open guest page offers the PDF as soon as it is there.
- Telegram notifications through one shared bot: Settings → Notifications → «Connect Telegram» opens the bot with a
  one-time code, Start links the person's chat, and agents write there with `harnessly notify send "<text>"` (no
  `--bot`). The person has one instruction of when agents write, the same on all their machines; /stop in the bot or
  «Disconnect» unlinks. The cloud's bot is reached through the machine agent (`POST /notify/call`, event `notify.call`,
  op `notify.result`) and its token never reaches the machine; a self-hosted runtime may have its own installation bot
  (`RUNTIME_NOTIFY_BOT_TOKEN`). Bots of the person's own by token are offered only while there is no shared bot,
  unless `RUNTIME_NOTIFY_OWN_BOTS` is `on` (or `off`).
- Design: the model of a presentation: settings in `meta/deck.json` with `deck-theme.css` written from them, speaker notes and
  team and guest comments one file each, the deck, settings, notes, dictation-tidy, comments and company-template
  (`.pptx` colours, fonts, layouts) routes, the deck lines of the agent's create prompt and guide, and
  `harnessly design notes get|set`.
- Presentations: exports of a deck as PDF (a page per slide), PNG pictures, PowerPoint (editable text, speaker notes) and
  one offline HTML file, built as jobs with progress, one at a time per machine
- Design: the deck's left panel (chat, notes, comments; docked, floating or a phone sheet), notes by voice, comments with pins,
  a picked place and a held finger, guest comments beside the team's, and the slide as context of the chat.
- Design: the contracts of presentations (projects of the kind `slides`): the deck's JSON shapes and files
  (`meta/deck.json`, `meta/notes`, `meta/comments`, `.media/`, which the history leaves out), the `/design/deck/*`
  routes (answering 501 until their parts land), `deck` settings and `attachmentIds` in `POST /api/v2/designly/create`,
  and the page's deck readers, calls, board messages, store and texts. A deck still opens in the site view.
- Design: a deck's recordings (chunks appended by sequence, events, finish, the transcript with pauses and fillers,
  the montage, retakes where the last take wins) and the MP4 render of a recording with the camera circle, subtitles
  and chapters, published for guests.
- Design: hand edits of a slide are written into its board: `POST /design/deck/patch` finds the element by the engine's
  `data-dc-tpl` (the same HTML parse, held against Chrome by a parity test), checks the text the page saw (409 when the
  board changed, 422 for text the code fills), replaces only that text or `style` (a move is `translate` in `cqw`,
  "back in place" removes it) and saves a history version named "Manual edit" («Правка вручную»). The board script
  gets the deck bridge: in-place editing of plain texts, block hover, dragging free blocks with snapping, hit tests,
  steps, the pointer, a touch hold that picks a place, and the deck's keys.
- Design: a hand edit of a block's style, place or reset carries the fingerprint the board gave for it (source tag,
  `style` and words) and lands only on that element, else it is refused as changed; a block inside its own size
  container moves in `px` (`unit`), the rest in `cqw`. The Chrome parity test checks the fingerprints too.
- Design: the Presentation tile of Designly's start opens a create screen (`/designly/new/slides`): the brief, files that
  come along, a company .pptx whose colours, fonts and layouts are read as the deck's style, and the settings (length
  5–45 min, slides, tone, prompter text) as a popover, or a bottom sheet on a phone.
- Design: a deck's share popover (who opens it, how a link opens, comments, PDF, views) and its guest page: slides, full
  screen or the recording with chapters, comments at a place on a slide, views reported per slide; the public index
  carries `kind` and `deck`, and `__media/<file>` serves the published video and PDF with Range.
- Design: a presentation opens in its own editor: the bar, the slide strip, the slide fitted to the stage, a frame and
  a grip to move a block (with guides and a way back), the text toolbar, the notes as subtitles, the style and «More»
  menus with downloads, and on a phone a dock to ask the agent and a sheet to edit a text above the slide.
- Design: the show, record, done and montage scenes of a presentation (page side): the presenter's panel with the clock
  and the pace, the prompter that follows the voice, the recorder that uploads 5-second chunks and events, the finished
  recording's dialog and the transcript montage.
- «Что нового»: after a release the page shows a card at the top once — two or three points in the person's words,
  each with an icon, «Понятно» or Esc puts it away for good (remembered in this browser). It waits while «Вышла новая
  версия» is up. A release adds its entry to `web/app/next/features/whats-new/releases.ts` and its texts to
  `app/locales/<lang>/next-whats-new.json`; `?dev=whatsnew` shows the card.
- Telegram notifications: in Settings → Notifications the person links any number of Telegram bots, each by its
  @BotFather token, with a name and an instruction of when an agent should use it. A chat (private, group or channel)
  is bound by a one-time code the person sends the bot with Start; the runtime finds it in the bot's updates. Agents
  see the bound bots and their instructions in the system prompt of every Claude Code and Codex launch, list them with
  `harnessly notify` and send with `harnessly notify send --bot NAME TEXT` (markdown-ish text as Telegram HTML, long
  text split, at most 20 messages a minute per bot). The runtime keeps the tokens (`v2/notify-bots.json`, 0600) and
  makes every call to Telegram; no answer to the page or the agents carries a token.
- Design: the preview of the owner's own design records a timeline in the background (pages, clicks, scrolling, viewport,
  tweaks, console errors, failed or slow requests, long tasks, dropped frames, web vitals) and the runtime keeps the last
  six hours per design (at most 20000 events or 4 MB). Agents read it on demand with `design timeline` and
  `design moment`; nothing is put into their context, nothing a person types is recorded, and guests record nothing.
- A handoff can send the complete conversation history without putting it in the browser or command line. The runtime
  streams it into a private provider-neutral file, gives the new agent a short first prompt that points to it, and
  keeps that handoff ahead of messages sent while the new agent starts. Source account, model, session and machine
  metadata do not cross the handoff boundary; private artifacts are removed after use and expire after a crash.
- A non-retryable provider quota response now ends the turn immediately instead of leaving the CLI in a retry loop.
  The chat shows one durable notice and waits for the person to choose another available model or profile; it never
  resends the prompt or switches an account by itself. Transient 429 responses remain retryable.
- Runtime: a message is never left waiting for ever. A typed message the agent does not take within 30 s goes back to
  the head of the queue as `queued{stalled}` (its text taken out of the agent's input box) and waits for the person's
  retry instead of a red failure or a bubble that spins; a steered message, held in Claude's own queue, is not touched.
  `GET /api/v2/conversations/{id}/messages?ids=…` says what the runtime knows of each client id (known with when and
  where it was handed, unknown, expired); a `send` of an id it already has gives its record back. A page reads the
  stall with `caps=msgstatus`; any other page gets it as `failed{unconfirmed}`.

- Designly: a guest link opens the same viewer the owner has (bar, devices, sizes, zoom, browser and status bars, board
  settings) read-only, in the view the owner shared. A guest with the right to comment picks a block and comments on it.
  The page is a second entry of the web build (`guest.html`); the runtime serves its files and the boards under
  `design-public/<project>/` without a session, with the board bridge for picking and live settings.
- Designly: «Ссылки» is one screen with one form. «Кто откроет» chooses between a link for anyone without sign-in and an
  invite for the signed-in; one list shows both kinds, each marked, and a made link is copied at once. A link without
  sign-in carries the look the owner gave the design (device, browser bars, fit, lock, page) so a guest opens it the
  same way.
- Designly: a link without sign-in. «Настройки ссылок» of the share dialog has a «Ссылка без входа» block above the invite
  links: a name, «Может смотреть» or «Может комментировать», a term (1 hour to 30 days, or none); making a link copies
  it at once, and a menu extends it by 7 days, changes what it allows or revokes it. The runtime answers the gateway
  at `design-public/<project>` (the board list, the project files, `support.js` of the project's release) for a
  60-second token only; hidden files, paths out of the project and symlinks out of it are not served.
- The hub (`cmd/hub`, `docs/hub/`): sign-in for a team by email code, passkeys, GitHub, Yandex ID or any OpenID Connect
  provider, spaces with invitations, and machines that connect outbound with a one-time code and are known by their
  Ed25519 key. It serves the onboarding UI, the machine installer and the machine binaries, and carries each machine's
  page over the agent's tunnel. SQLite by default, Postgres by a DSN. Deploy with `compose.hub.yaml`, the Helm chart in
  `deploy/helm/harnessly-hub` or the systemd unit; see `docs/hub/deploy.md`.
- On the machine: `harnessly-agent register`, `status`, `off`, `on` and `policy`; a local `policy.toml` and a kill
  switch the hub cannot change; installs of Claude Code and Codex, sign-in to them by link, in a console or with a key
  sealed in the browser, a provider network check and an optional proxy for agent launches; `deploy/install/install.sh`
  with the `dir`, `container` and `full` profiles (`docs/hub/machine.md`).

### Changed

- The agent's Chrome is held to rules: it goes out only through an egress proxy shared with the pane's live
  browser, which refuses private networks, link-local and the machine's own addresses (by the address a name resolves
  to) and on `localhost` opens only the machine's dev servers, never the runtime's port; `file:`, `chrome:`,
  `javascript:` and `data:` pages are refused. It closes after 3 minutes idle by default and keeps session cookies
  across, runs at most 3 per machine (`RUNTIME_BROWSER_MAX_HOSTS`) with 8 tabs each (`RUNTIME_BROWSER_MAX_TABS`, also
  the renderer bound) under `nice 10`, downloads into its profile up to 100 MiB a file
  (`RUNTIME_BROWSER_DOWNLOAD_LIMIT`), is replaced after an update (build in `Host.hello`) and closed by
  `RUNTIME_BROWSER=off`. See `docs/configuration.md`.
- A runtime that listens beyond loopback and has no password and no `INIT_PASSWORD` makes a one-time initial password at
  start and prints it once (`Initial password: …`), so the first visitor of a public host can no longer choose the
  password. `RUNTIME_GENERATE_INIT_PASSWORD=1|0` forces it on or off; a runtime on `127.0.0.1` behaves as before.
- The Docker image builds the page and the Go binaries on the builder's platform and cross-compiles, so a multi-arch image
  is not built under emulation.
- A completed goal leaves the composer but remains in the conversation history, where it can be continued explicitly.
- Public Designly boards open in a clean guest tab and load versioned local vendor assets with ETag, gzip and private
  caching instead of repeatedly transferring the full vendor payload.
- The interface opens faster on a phone and typing in it no longer stutters. It comes as one file instead of about 80
  (124 requests at start become 42, which counts on HTTP/1.1), loads a single language, and the terminal, the right
  pane's tabs, sign-in and docs load when first opened. On an emulated iPhone 14 the home screen is ready in about 4 s
  instead of 5.9 s and a chat in 6.6 s instead of 8.4 s; the main-thread work while typing is halved.
  The guest page of a design loads 10 files instead of 36 and none of the interface's, and its buttons get the styles
  they were missing.
- Enter pressed on the start page before the list of machines arrives waits and sends instead of being dropped.

### Fixed

- A fresh device opening `/agents/` while the home machine's runtime hangs (its agent keeping the link) no longer stays
  blank for up to 30 s: the hub waits on that machine's page settings at most 2 s and not again for 30 s, the person's
  other machine that is on answers for them meanwhile, and the page draws the machines without waiting for its
  `me` past 3 s.
- A chat opens whole while its machine is starting, hung or switched off: the feed, older turns and a turn's steps are
  read from the hub's (the cluster's) copy first, not only once the machine is called offline; the machine answers
  only for what the copy does not have yet. A chat the copy does not hold back to its first turn says so (`complete`),
  so its oldest page asks the machine for the rest instead of ending the feed early.
- A restart no longer loses the runtime's `RUNTIME_*` settings. `install.sh` keeps the `RUNTIME_*` variables
  it was given in `~/.config/harnessly/env` (before, only the first start, which inherited them from the installer's
  shell, had them) and passes them to the launchd job; the hub machine's installer keeps the lines added to
  `runtime.env` and `agent.env` instead of rewriting the files. `RUNTIME_PREVIEW_DOMAIN` (and `RUNTIME_PREVIEW_ID`)
  name the preview slots' origins `https://s<n>-<id>.<domain>` when no explicit list is given, the same on every start.
  `harnessly doctor preview` and the machine check `preview_dns` say when those names do not resolve (NXDOMAIN).
- A new Sinopia project takes a brief of up to 20,000 characters, not 4,000: a dictated brief ran past the old bound
  and the person saw a bare «brief: too long». A longer one is refused in their words, with a hint to attach the text as
  a file.

- A message with several pictures no longer goes to the agent cut in two: no Enter is pressed while Claude still reads a
  paste («Pasting…»), nor while its box stops short of the message's end; the paths of a paste Claude reads late are
  not pasted a second time. A message of only «…/up_….pn» went in, the other paths left in the box.

- What is left in Claude's box of our own messages — their text with a picture path Claude read late, a part an Enter
  did not take — no longer stops the queue as «в поле ввода терминала есть текст»: the message itself gets its Enter
  (or, cut short, is cleared and typed whole), an earlier one's rest is cleared. A person's draft is still never
  touched; a block by it ends after 10 minutes from the first look, however often the agent was busy between looks,
  the message failed with «Повторить».

- A Stop no longer pauses the queue for good: the pause ends when the person sends a message or a turn that began after
  the Stop ends; «Продолжить» stands on the first paused row even when a failed one is above it, and a failed message
  the runtime queued again is asked about, so the strip shows what the runtime holds.

- A message the agent took no longer stays in the queue over the field: a chat whose channel the machine refused (its
  snapshot past the time budget on a loaded machine answers not-found) is asked for again instead of being let go, so its
  frames come back; the page also asks the runtime about queued messages whose frames it missed.
- A bash-mode line («! cmd») sent from the field is taken by its command entry in the transcript (no prompt-submit hook
  fires for it): it is no longer «не дошло» with Повторить after it ran.
- A send while the draft's write had no answer yet no longer brings the sent words and files back into the field from
  the write's own late echo — the second send made two copies of one message.
- Telegram notifications: a chat's title (set by whoever made the chat) reaches the agent as one quoted line of at most
  64 characters, so it cannot add lines of its own to the agent's instructions.
- A long link in the agent's reply stays inside the chat column: its chip is never wider than the column and the address
  ends with an ellipsis.
- Designly: «Обзор» (all devices side by side) is on the canvas like a single device: a drag of the empty area, the
  wheel, a trackpad, Space or the middle button pan it, ⌘/Ctrl + wheel, a pinch and the zoom controls zoom it. It keeps
  room at the bottom for a guest's list of comments, and that list sits inside the page in the window's frame instead
  of on its edge.
- A conversation keeps loading its transcript and older pages from its origin machine when another machine is selected;
  switching the execution view no longer replaces existing Codex history with a false “not on this machine” error.
- A required first handoff prompt keeps its private priority without pretending to be a steered message, so it remains
  first after a failed launch while the visible queue and timeline preserve their normal FIFO order.

- A message with a picture Claude does not show as «[Image #1]» in 8 s (5 s before) no longer fails try after try: it goes
  with the picture's path as text, path and text under one Enter, and Claude reads the file by its path.
- A picture's path no longer stays in the agent's input line. Claude can read a big picture after the typist gave up on
  it, and the path then landed as plain text, holding up the next message as «text in the terminal's input line». A
  box that holds only picture paths the harness pasted is cleared before the next message. A try whose text stays in
  the box is typed again, backing off, up to 10 tries (6 before) before the message fails, Enter is pressed again up
  to 10 times for a message the agent did not take, and a Stop whose Escape did not go in is pressed again.
- A turn waiting on a slow model is no longer marked done. 15 s of silence with the box up and no spinner read ended
  the turn while Claude was still waiting for the reply (96 s seen), so Stop was refused as «not working» and a model
  switch hit the box. A Claude turn the transcript keeps open now ends on silence only when the screen says it ended
  (its summary «✻ Brewed for 8s», or the stop mark), or after 10 minutes of silence; the Stop hook and the transcript
  end it as before.
- A message typed into the terminal no longer stays there unsent. A long line typed under load, its Enter taken into
  the text as a newline as the agent's step ended, read as sent because the box looked empty before the agent drew it;
  the message then showed «in the agent's queue» for half an hour while its text sat in the input line, and every
  message after it waited on that text until it expired. The typist now counts a message sent only once the box stayed
  free of it for 600 ms, a message typed into the turn is looked for in the box while it waits, and its own text there
  gets its Enter again (or fails it, the box cleared); a message held by someone else's text says «the terminal's
  input line holds text». A steered message the person pulled back by stopping the turn (Claude returns it to the box
  for editing) is not Entered again: it shows «back in the terminal's input line», to retry or remove.
- A message sent while the agent works is never lost and does not jump: it stands once in the stack over the
  field — «sending» at once, with a pencil (back into the field) and a bin — until the agent has it, then once in the
  feed at its place; a message typed into the turn (⌃↵) stands in the stack «the agent reads it after the current step»
  and, once read, among the turn's steps and under its fold (it used to vanish when the agent read it). The live
  indicator is never over the person's last message, «not confirmed» no longer shows for a message that only waited in
  the queue, a queued message keeps its reason instead of swinging between «agent busy» and «behind», and a message to
  a free agent whose screen still draws the last turn's spinner is typed within 300 ms of the screen freeing, not 1.5 s.
- A message sent during a rollout (the runtime or the machine restarting, a gateway's 502/503, a lost answer, a dropped
  socket) is no longer «not sent: the machine refused it»: the road failing is not a refusal. The bubble
  waits, quietly and without red («waiting for the machine»), and goes by itself with the same client id — after 1, 2, 4 …
  30 s, at once when the socket is back, after a reload too — and the runtime answers a repeat with what it answered
  before, so it never lands twice. Red «not sent» with its reason in words («the message is too long», «the queue is
  full») is only for what the runtime refused on the merits; the page has no «not confirmed» of its own any more.
- A queued message is delivered or fails visibly, never waits forever: a picture reference the agent's input box
  wrapped midway (`[Image` / `#104]`) counts as pasted, a retry reads the whole box and finishes its own pictures
  instead of taking them for someone else's draft, failed tries back off and give up after six with the reason on the
  message, and Retry and Delete work on it. Every wait of the queue goes to the log.
- The composer shows the runtime's queue and background tasks from the status socket, and one place picks the machine
  of a runtime call, so a conversation drawn without a frame gets its Stop and send.
- The frame of a fleet machine (`?machine=<id>` in its address) sends its API calls and sockets to that machine too;
  behind a gateway that routes by the parameter they went to the default machine, and a conversation of another machine
  did not open.

### Added

- Chats branch and hand over. «Ответвить» in a chat's menu, or the fork mark under a finished answer, opens a new chat
  with the history up to that turn (Claude's session is copied and resumed; Codex forks its whole session at the last
  turn, or starts from an excerpt); the new chat says where it branched off and links back. «Влить обратно» has the
  fork's agent write a short result, editable before it goes, and sends it into the original chat's queue. A model of
  the other agent in the field's picker hands the chat over: a preview of an excerpt (the first request, the latest
  turns, key command results, within the new model's window) or of a note the agent writes, editable, starts a new
  chat in the same folder; the old chat stays as it is, and the new one shows «Передача контекста · A → B».

- Going on from an earlier message in the new interface. A message of the person's offers «Edit and continue from
  here» on hover (a long press on the phone) and becomes an editor in place; sending asks «Conversation only» or
  «Conversation and files» — the folder back to the snapshot taken after the turn before, with the files that would
  change listed, disabled when there is none. The turns from that message on fold into «Rewound N turns», which opens
  to show them read only, and «Undo» takes the rewind back for 10 s. The runtime forks the agent's transcript before
  the message into a new session (Claude Code and Codex both resume it as their own) and resumes the chat there, so
  the old branch stays whole; an agent at work is stopped first, and the first message of a chat has nothing to go on
  from.
- The agent's answer draws what it writes: a `mermaid` block is a diagram (themed, a click opens it full size, the
  source copies), an `html` block is a page in a sandboxed frame that has no access to the page, its cookies or the
  network, with a toggle to its source and a full-size view, and a link to the installation's own document or board,
  alone in its paragraph, is a card. The agents' built-in prompts say so. The diagram renderer loads with the first
  diagram, not with the page.
- Chats that stop on the agent's usage limit go on by themselves when it resets, and chats a restart cut short go on
  once their agent is back. The feed ends with a calm card «Лимит Claude исчерпан · продолжу автоматически в
  18:00 (через 2 ч 14 мин)» with «Продолжить сейчас» and «Не продолжать»; what the person sends meanwhile waits for the
  reset («Отправится после сброса лимита в 18:00») and goes first. The sidebar shows a quiet clock on held chats, and the
  feed marks where the work went on («Продолжил после сброса лимита», «Прервано перезапуском · продолжил
  автоматически»). Settings → General has both switches, on by default (command `limit`, feature `auto-continue`).
- Designly: who sees a design. A new design is personal; the owner opens it to the team (view, comment
  or edit), gives a person a role for an hour, a day, a week, a month, to a date or for good, takes it back one by one,
  makes a link for a logged-in colleague (up to 7 days, a number of uses) and sees the access log. A colleague without
  access asks for it from the page and the owner gets a push to allow for a term or refuse. The platform keeps the
  rights; the runtime gets a snapshot per person and hides closed projects everywhere — the library, its pages and
  files, the CLI and the agent's tools answer as if the project were not there. Projects made before stay open to the
  team; an ownerless one can be taken. Without the platform nothing changes.
- Boards load faster: React and Babel are served by the runtime instead of unpkg, the design's heavy files get
  hashed addresses cached for good, and a page switch inside a design moves the frame without a new slot login.

- Chat terminals can be ended and stop by themselves when idle. «Завершить терминал» in the chat's menus
  (sidebar, header «…», the phone sheet) asks first, then kills the chat's tmux session and everything under it; the
  chat and its history stay and the next message resumes the agent (command `terminal.stop`, feature `terminal-stop`).
  The runtime also stops a terminal that has stood idle for the window of «Останавливать простаивающие терминалы»
  (off, 1 h, 2 h by default, 6 h; `GET/PUT /api/v2/runtime-settings`): only when the agent is idle with no background
  work, gate, queue or viewer, nothing but the agent and its helpers runs in the pane, the pane's screen has not changed
  and the tree has used next to no CPU. The feed says «Терминал остановлен: 2 ч без работы»; chats without a running terminal
  are dimmed in the sidebar and the rail.
- The «More → Apps» screen is a left list of the connected apps with the chosen one open as a page on the right, and it
  scales to many apps: the tracker page lists all the epics and tasks assigned to the person (the list used to
  keep only the task-card keys, 8 epics and the stale ones of anyone), an epic opens into its subtasks with progress, and
  both are searched, filtered by status, tag, type and epic, sorted and grouped; the GitLab page shows the pipeline,
  approvals, draft, conflicts, open threads, author, assignee, labels and how long a request stands still, with filters by
  state, author, assignee, project, pipeline and condition and a search; each page has its own settings kept in the
  browser, and «To chat» still takes rows into a new chat as mentions. Wire: the feature `apps-hub`; the sections are
  `epics`/`tasks` and `mrs`, and a row carries the fields above (see internal/wire/README.md, item 47).
- The messages waiting in the queue over the field can be put in another order: the grip on the left of a row
  carries it (the mouse, or a finger after it rests; ↑ ↓ with the grip focused move it a place at a time), a line shows
  where it would land, and the order changes when the runtime has said so — the other rows then slide to their places.
  Wire: `queue.move {opId, conversationId, clientId, beforeClientId | null}` and the feature `queue-move` (only a queued
  message moves; a place across a message sent «now» is `refused{conflict}`, a message already typed `refused{too-late}`);
  a runtime without it draws no grip. A long queue now scrolls inside itself instead of pushing the chat off the screen.
- The right pane's browser works without a gateway: a standalone runtime opens the preview itself
  (`POST /api/preview/open`) in 8 slot listeners next to its port, each an origin of its own, and proxies a dev server or
  a site there; the page refuses requests coming from a slot. `RUNTIME_PREVIEW_LISTEN` moves or turns off the slots,
  `RUNTIME_PREVIEW_PUBLIC_ORIGINS` names them behind an https proxy; compose and the image publish `8023`–`8030`. A dev
  server that is down now says so instead of "preview not supported".
- `runtime web export <dir>` writes the page this runtime would serve, with this installation's settings filled in, into
  a folder: a gateway in front of the machines can serve the page itself, also while a machine is down.
- `RUNTIME_APP_MARK_URL`: an installation's own product mark. The page draws it in place of the harnessly mark, and the
  runtime puts it into the tab icon and `RUNTIME_APP_NAME` into the title and the installed app's name.
- The web page catches up with the agents page it was split from: tool calls in the feed expand to their result, stop
  mark and line diff; cards for plans, questions, Codex steps and runtime notices; the agent's task checklist above the
  feed; the reader's place held while new rows arrive, new rows marked, model switches shown; message history and quick
  prompts in the composer, send-now with Ctrl/Cmd+Enter, a slash command from the welcome screen; a message the runtime
  takes out of the queue stays as a pending row; one Annotate action with a comment per quoted fragment; board comments
  to a chat wait in its queue; the new-task machine outlives a reload; the document reader takes opaque version ids; a
  service worker, a manifest and an update check by the entry's hash.

- Design projects get a page model (`docs/design-pages.md`): `meta/project.json` and one `meta/pages/<ULID>.json` per
  page, with a permanent id. When `meta/` is there it is the truth; `project/canvas.json` (the design tool's v3 index)
  and `artifact.json` are built from it, and a project without it is read as before. `harnessly design page
  list|create|rename|remove` (`/api/cli/design/pages`), board claims keyed by the page id (a claim under the old key
  still holds), `harnessly design migrate [--dry-run]` for a whole library (same listing, no board or asset touched, the
  same ids on every run), and `harnessly hook design-guard`, a Claude Code hook that refuses hand edits of
  `project/canvas.json`. The library rules file is v6.
- A new design project no longer needs `<library>/.template/`: the runtime ships a board template and the board engine
  (`RUNTIME_DESIGN_ENGINE_FILE` overrides it), and every new project starts with `meta/`.
- UI kits by version for the design library. A project's boards no longer carry their own 6 MB copy of the kit:
  a kit is installed once as `<kits>/<id>/<version>/` and the library server answers `/kit/<id>/<version>/<file>` with
  immutable cache headers (the same path rules as project files: no `..`, no links out of the version's folder), so a
  board loads `/kit/acme-ds/0.16.0/bundle.r18.js` and a new kit version is a new address. `GET /__lib/kits` lists the
  installed kits and versions (`?kit=<id>&version=<v>` the files of one). `RUNTIME_DESIGN_KITS_DIR` names the folder
  (default `design-kits` in the data folder). `harnessly design kit install <id> <version> <tarball-or-url>` unpacks a
  tarball all or nothing (links, devices and paths outside the kit are refused; the same tarball again does nothing,
  other content under an installed version needs `--force`) and `harnessly design kit list` shows what is installed.
  Both work on the folder, the runtime does not have to be up. Kit files are readable across origins only from the
  library server's own loopback origins and the hosts in `RUNTIME_DESIGN_KIT_ORIGINS` (the origin is echoed back with
  `Vary: Origin`; any other origin gets no CORS header).

- Releases of the board format. With `RUNTIME_DESIGN_RELEASES_DIR` (a checkout of a design-upstream
  mirror of Claude Design releases), a project with `meta/` is served the engine of its release (`"release"` in
  `meta/project.json`, `legacy` when it names none) as `project/support.js`, checked against the mirror's sha256; the
  boards and the project's own copy are never changed, and the engine's version goes into the board's address so a
  browser drops the old one. A project without `meta/`, or on a release the mirror lacks, is served its own copy. A new
  project starts on the newest stable release (or the library's choice). `GET /design/library/releases[?artifact=]`
  lists the releases and the project's, `POST {artifact, release}` moves a project to a stable release (through the
  page model's `SetRelease`; a project without `meta/` is asked to migrate first), `POST {newProjects}` sets the
  release new projects start on; the library listing carries each project's `release` with the `upgrade` on offer.
  Without the variable nothing changes.

- Agents get the workspace out of the box. A standalone runtime installs agent skills for both Claude Code and Codex
  when it starts: `harnessly` always, `boards` with `RUNTIME_BOARDS_URL`, `artifacts` with `RUNTIME_ARTIFACTS_URL`.
  Claude loads them as a plugin with `--plugin-dir`, Codex from `~/.codex/skills/harnessly`, and the `artifacts`,
  `boards` and `harnessly` launchers go to `~/.local/bin`. Files are tracked in a manifest per folder: yours are never
  touched, a release replaces only unchanged ones, a removed service's skill is deleted. `RUNTIME_AGENT_SKILLS=off`
  disables it; `RUNTIME_AGENT_BIN_DIR` moves the launchers. The skills are vendored in `skills/` at the commits pinned
  in `skills/SOURCES` (`skills/sync.sh`, `make skills-check`), so an offline install has them.
- The `harnessly` command (`cmd/harnessly`, in the image on the `PATH` and in the release archives): `open` shows a
  document, board, file, dev server or merge request in the person's right pane, `panel`, `status`, `workspaces`,
  `tab list|create|send|status|result|close` (start another Claude Code or Codex with a prompt), `api-guide`; with
  `--json`. It finds the runtime by itself from the data folder, which every pane now gets as `HARNESSLY_DATA_DIR`.
- Built-in workspace prompts for both agents when no `RUNTIME_WORKSPACE_PROMPTS_FILE` is set (the open install had
  none): where the agent is, the `harnessly` command, and the `artifacts` and `boards` commands when configured.
- `GET /api/cli/api-guide` answers with a built-in guide of the `/api/cli` routes when the deployment names none.
- `open artifacts` accepts links to separate document and board servers (`RUNTIME_ARTIFACTS_URL`,
  `RUNTIME_BOARDS_URL`), and plain-http ones on this machine; the reader path follows `RUNTIME_APP_BASE_PATH`.

- Boards and artifacts integrations. `RUNTIME_BOARDS_URL` / `RUNTIME_ARTIFACTS_URL` with their tokens give every
  tab the runtime starts or resumes `BOARDS_URL`, `BOARDS_TOKEN`, `ARTIFACTS_URL`, `ARTIFACTS_TOKEN`,
  `ARTIFACTS_SESSION` (the tab's id) and `ARTIFACTS_AUTHOR`. New routes for the page: `GET /api/integrations`,
  `POST /api/integrations/boards/sign-in` (a one-time boards hand-off) and
  `GET /api/integrations/artifacts/documents?session=`. In gate mode they exist only once a service is configured.
- The web page has a side pane with Board and Document tabs: the boards editor signed in through the hand-off, the
  artifacts embed view following the page's theme, and the current conversation's documents. Board and document links
  in conversations become chips that open there.
- Ways to run: a multi-stage `Dockerfile` (with a `with-agents` target that adds Claude Code and Codex from npm), a
  `compose.yaml` with boards, artifacts and MinIO wired together, the Helm chart `deploy/helm/harnessly`, Argo CD
  examples (one Application, and an app of apps for the whole workspace), a systemd user unit and `make run`,
  `install`, `install-systemd`, `image`, `compose-up` targets. `docs/deploy.md` and `docs/integrations.md` describe
  them.
- `tmux.Config.PaneEnv` and `Tmux.TabOf`: extra variables for a new session's pane shell, keyed by its tab.
- GitHub workflows: tests with the race detector, a build for linux and darwin on amd64 and arm64, golangci-lint,
  CodeQL, OpenSSF Scorecard, gitleaks, and releases with GoReleaser on version tags.
- A Makefile with the developer targets (`build`, `test`, `test-race`, `lint`, `fmt`, `vet`, `cover`, `cross`,
  `clean`), an `.editorconfig` and a golangci-lint configuration the code passes.
- Documentation in `docs/`: getting started, architecture, slices and configuration; a code of conduct, issue and
  pull request templates, CODEOWNERS and Dependabot.
- Brand assets: wordmarks for light and dark backgrounds, a square avatar and the social preview.
- `tools/publish-tree.sh` exports one commit as a fresh single-commit repository, refusing when it finds a forbidden
  word or a secret.
- A gitleaks configuration with narrow allowlists for test fixtures.

- `POST /api/cli/tabs/<tab>/send` takes `keys`, an array of tmux key names (`C-c`, `Escape`, `Enter`, `Up`, …) pressed
  after the text or alone: control characters in `content` were never typed.
- `INIT_PASSWORD` is handled by the page: it asks for the initial password first, then offers the setup. compose,
  `.env.example` and the systemd env example carry it.
- `compose.yaml` takes the public addresses (`HARNESSLY_PUBLIC_URL`, `BOARDS_PUBLIC_URL`, `ARTIFACTS_PUBLIC_URL`) and
  derives the frame and CORS origins from them; MinIO is behind the `minio` profile. `docs/deploy.md` has a section for
  a public server: three hostnames, Caddy with automatic TLS, ufw, and `*.sslip.io` for a quick try.

### Changed

- `RUNTIME_CODEX_SANDBOX=container`, the Docker image's default, starts Codex without its own sandbox (which cannot start
  in a container): `sandbox_mode="danger-full-access"`, `approval_policy="never"`. Elsewhere Codex keeps its own.
- A standalone runtime writes the Codex launcher with defaults (the data folder, `HARNESSLY_CLI_TOKEN`), so Codex
  conversations start in Docker, compose, Helm and systemd without `RUNTIME_SCRIPT_DATA_DIRS` and
  `RUNTIME_MCP_TOKEN_ENV`. A conversation that cannot start answers `500` with the reason instead of an empty error.
- A `claude-code` or `codex-cli` tab made through `POST /api/cli/tabs` starts its agent without a `prompt` too; it used
  to become a plain shell.
- The standalone gate answers a `/api/cli/` request refused for lack of the cli-token with a `403` that names the
  header (a page session cookie never worked there).
- `compose.yaml` builds the images (`docker compose up -d --build`; nothing is published) under local `:local` tags and
  never tries to pull them. boards and artifacts store on volumes by default; set `COMPOSE_PROFILES=minio` and
  `HARNESSLY_STORAGE=s3` to keep using MinIO as before.

- The module path is `github.com/ly-solutions/harnessly` (it was `harnessly`); imports and the version ldflags follow.
- Release runtimes (GoReleaser and the GitLab tag build) carry the web page (the `webui` build tag). In gate mode it
  stays off unless `RUNTIME_WEB=on`, so machines running in front of a previous runtime answer as before. `make build`
  still builds a runtime without the page.
- Links in agent replies open in a new tab.
- The README is rewritten around a quick start that runs the runtime standalone; the configuration tables and the
  slice details moved to `docs/`.
- Golden test data stores fake provider credentials as placeholders built at run time.
- Every text the runtime shows people follows `RUNTIME_LOCALE`: English by default, Russian with `RUNTIME_LOCALE=ru`
  (the previous texts, byte for byte). That covers error bodies, notifications, the shared snapshot page and file, the
  command catalog, the design library pages and the prompts it writes for agents, preview pages and limit messages.

### Fixed

- Platform API calls follow the installation's prefix (`RUNTIME_PLATFORM_API`) instead of a fixed `/api/platform`.
- Inline code in messages no longer shows the typography plugin's backticks next to its chip.

- A first start of Claude Code or Codex in a new folder no longer hangs at «Starting». Both ask whether to trust the
  folder, and the dialog was only in the terminal, so the agent waited there unseen while the messages sent to it
  piled up as pending. The runtime now answers yes for the folder the conversation was started in;
  `RUNTIME_FOLDER_TRUST=ask` leaves the dialog to the person.
- A plain terminal tab opened from the sidebar shows its terminal; it was a blank page.
- Codex in code mode no longer shows a bare `exec`: the timeline entry, the push text and the current-action line name
  the commands, searches and other tools the script calls.
- Titles of board and document chips are looked up by origin, so boards and artifacts served over http on other ports
  of one host (the compose workspace) get them too. `RUNTIME_ARTIFACTS_URL` and `RUNTIME_BOARDS_URL` name those origins.
- Stopping the design library's server also stops its background history and waits for a snapshot under way.
- A pane command a page applied before the runtime started waiting for it is reported as applied.
- Tests no longer depend on the machine's umask, wait for the standalone run's panes to exit, and the timeline golden
  scenario waits for events instead of time.
- Codex dropped `BOARDS_TOKEN` and `ARTIFACTS_TOKEN` from the environment of its commands (it removes variables named
  like secrets), so its agents could not use boards and artifacts. The runtime now starts Codex with
  `shell_environment_policy` that keeps them and leaves only its own CLI token out.

## [0.9.0] - 2026-10-19

The first version that runs a machine without the previous runtime. 0.1.0 to 0.8.0 were internal pre-releases that
built up to it; this entry summarises everything the project holds.

### Added

- **Machine agent** (`cmd/agent`): one process per Unix account with a single WebSocket link to the cluster (wire
  protocol v1), serving the agents' transcripts with change events, the runtime's state files with versioned writes,
  the status and sync sockets relayed, TCP tunnels to the runtime and the disk service, and token-guarded loopback
  routes. `cmd/agentprobe` drives an agent the way the cluster does, for compatibility diffs and benchmarks.
- **Runtime gate** (`cmd/runtime`): holds the runtime's port and answers the slices switched on per machine in
  `state-mode.json`, re-read every second; hands everything else, sockets included, to the previous runtime over
  pooled connections; runs read-only slices in shadow and logs where the answers differ; records traffic and logs
  per-path proxy statistics.
- **Standalone mode**: with no previous runtime the gate is the whole runtime and does its start-up work itself: the
  single-instance lock, the config and shared secret, the `cli-token`, the hook, status line and Codex hook scripts,
  the agents' settings and MCP files, the prompt editor and the Codex launcher.
- **Core**: the tab state keeper fed by hooks, transcripts, screens and the process tree; the layout and workspace
  stores; the status and sync sockets; the prompt queue and delivery into tmux panes; hook routes; web push; idle sleep
  of agents; plugin and skill reloads in idle Claude tabs.
- **tmux control layer** and parsers of the agents' terminal screens; the terminal socket as a pty on `tmux attach`.
- **Claude Code and Codex support**: detection in a pane, launch and resume lines, the process table, background work,
  subscription limits with a forecast, session history and statistics.
- **Slices** of the previous runtime's routes in Go: `fsread` (machine and file reads, git answers, slash commands,
  launch choices), `fswrite` (uploads, editor saves, repository sync), `machines` (machine snapshot, load, health),
  `launch` (conversations, automation and eval runs, side chats), `transcripts` (links, message counts, usage report,
  statistics, conversation snapshots), `timeline` (conversation socket, status line sink, economics, subagents, turn
  checkpoints), `design` (design library with its server, history, imports and headless checks), `preview` (reverse
  proxy with DevTools and an MCP server), `services` (GitLab, link previews, tracker cards, site icons), `voice`
  (dictation) and `state` (settings files, automations and the remaining routes).
- Byte-for-byte compatible answers (status codes, JSON key order, headers, ETags) pinned by golden tests recorded from
  the previous implementation's handlers.

[Unreleased]: https://github.com/ly-solutions/harnessly/compare/v0.9.0...HEAD
[0.9.0]: https://github.com/ly-solutions/harnessly/releases/tag/v0.9.0
