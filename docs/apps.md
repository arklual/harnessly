# Apps

An app is a package you write once and turn on where you work: in a chat, a pane tab, a full screen of its own or a
Sinopia document. A plugin is an app without views. This page is the reference; to learn it step by step, with diagrams and
examples, read [the guide](build-apps/README.md) (`harnessly app docs` on a machine). The format is a Claude Code plugin plus one
manifest of ours:

```text
acme-cashback/
  harnessly/app.json          # the manifest: required
  .claude-plugin/plugin.json  # written by the install from app.json
  instructions.md             # always in the agent's context while the app is on: keep it short
  skills/<name>/SKILL.md      # read by the agent on demand
  commands/*.md               # slash commands
  .mcp.json                   # MCP servers: remote (url); a trusted app may run local ones (command)
  ui/                         # views: static files
```

## Commands

```sh
harnessly app docs                                      # the guide, built into the binary
harnessly app new acme.cashback --template pane-chat   # templates: chat, pane-chat
harnessly app validate ./cashback --strict              # every problem names its field; --strict fails on warnings
harnessly app dev ./cashback                            # install the folder itself while you write it
harnessly app install ./cashback                        # install a copy: later edits do not reach it
harnessly app list
harnessly app enable acme.cashback                      # in the chat this tab runs; disable turns it off
harnessly app remove acme.cashback
harnessly app publish ./cashback --channel beta         # versions, channels, the team, links, evals: see "Sharing"
harnessly app eval ./cashback
```

Installed apps live in `<data>/apps`: the list in `installed.json`, the copies in `packages/<id>/<version>`. A dev
install points at your folder and is never deleted by `remove`.

## Manifest

| Field | Required | Meaning |
| --- | --- | --- |
| `id` | yes | `<publisher>.<name>`, lower case latin letters, digits and `-`; never changes |
| `name`, `description` | yes | What people see |
| `version` | yes | `1.2.0`; a published version never changes |
| `harnessly` | yes | Host versions it works with (apps came in 0.16.36): `>=0.16.36`, `>=0.17`, `^0.17`, `>=0.17 <1`, `0.16 \|\| >=0.18` |
| `trust` | yes | `declarative` (no outbound `network` for views), `sandboxed` (adds `network` and a framed `https://` view), `trusted` (adds local processes; installed by hand). A view's scripts run on every tier |
| `targets` | yes | `chat`, `pane`, `app`, `sinopia.<kind>`, `sinopia.*` |
| `activation` | no | `on`: `document` turns the app on in the documents of its kinds. `manual`, `command:/x`, `view`, `always` are checked and turn nothing on. `openView`: the view `harnessly open app:<id>` shows |
| `permissions` | yes | `chat.prompt`, `chat.read`, `chat.card`, `pane.open`, `storage`, `storage.space.write`, `ai`, `files.read:<mask>`, `files.write:<mask>`, `secrets:<name>`, `sinopia.read\|write\|comment\|export\|kind`, `users.read`, `notify` |
| `network` | no | `connect`, `resources`, `frames`: host names; `*` needs `reason` |
| `secrets` | no | `{name, description}`: checked, not used yet. There is no install prompt and nothing can read a value. An agent uses `harnessly secret` instead |
| `settings` | no | JSON Schema of an object: checked, not used yet. There is no install form |
| `agent` | no | `instructions` and `tools` (`allow`, `deny`, `confirm`) act. `mcp` is what a Sinopia `tool` must name. `skills` is checked against the files (all of `skills/` loads anyway). `context`, `validators`, `flow`, `exclusive` are checked, not used yet |
| `views` | no | `{id, title, entry, targets}`: `entry` is a file of the package or an `https://` address listed in `network.frames` |
| `chat` | no | `commands` (`/name`, a file `commands/name.md`), `cards` (views with the chat target) |

A target with nothing of the app in it is an error, and so is a contribution to a target the app does not list.

## In a chat

`harnessly app enable <id>` (or the chat's app list) turns an app on in a Claude Code chat; Codex chats get apps
later. An agent at rest is stopped at once and the next message brings it back with the app; an agent at work keeps
what it had until its terminal stops. The launch then gets:

- the package as a Claude Code plugin (`--plugin-dir`): its skills, commands and `.mcp.json` servers;
- the instructions files of all its apps in the system prompt, under `# Apps on in this chat`;
- a snapshot of the apps' tool rules (`<data>/apps/launch/policy-*.json`) and the chat's own settings plus the
  `harnessly hook app-guard --policy <snapshot>` PreToolUse hook, which decides each tool call by it, locally.

**Turning apps on and off.** In the page: «+» of the field → «Приложение» lists the installed apps that work in a chat and
are not on yet, and a pick turns the app on; the apps that are on stand as a quiet row of names over the field (a name
opens the app as the main thing, the small cross turns it off). The page sends the whole list with the
`conversation.apps` command and shows the change only when the machine has taken it. From a tab, `harnessly app enable`
keeps working, but `harnessly app disable` in a chat that has apps is refused (`409 use-page`): taking an app away from
a chat is the person's, with the page's switch. «Приложения» of the sidebar lists the apps that are on in the person's
chats (each opens in the chat it was last on in) and the way to all the installed ones (`/apps`).

An app gets a chat only where it is on. The runtime refuses to issue a view session for a chat whose `apps` lack the
app (`403 not-on`), so the view has no chat area, and the page does not pass the chat to an app that is off there
(`chat.read`, `chat.card` and `chat.prompt` answer `permission_denied`). On the app's own page in such a chat the
«…» menu offers «Включить в этом чате».

## Views

A view is a page of the package (`views[].entry`, a file under `ui/` or its own folder) that the person sees in a pane
tab or as a card in the chat. The runtime shows it in a sandboxed frame on an origin of its own (a preview slot), never
the page's. The preview tools see the view's own file loads (`preview_network`), not its console or its bridge calls. Only the entry, its folder and `ui/`
are served; nothing else of the package is.

- **Open one** with `harnessly open app:<id>` or `harnessly open app:<id>/<view>` (the app's first pane view when none
  is named). The tab is the `app` kind of the pane; an installed app adds no kind of its own.
- **Security policy.** The frame is sandboxed without `allow-same-origin` (and without `allow-top-navigation`), and the
  view's CSP carries `sandbox allow-scripts allow-forms`, so a view's origin is opaque: it cannot read another app's
  view or any storage of the page, and `localStorage` and cookies are not there. Keep state with `hl.storage`. Its files
  load only under the token of a session the runtime issued for that mount (`/<token>/<app>/…`), on a Host that is
  the loopback server's own. Its CSP comes from `network`: scripts are files of the
  package (no inline scripts, no eval), `connect-src` is `'none'` unless `network.connect` lists addresses, images,
  fonts and media take `network.resources`, nested frames `network.frames`.
- **Theme.** The host passes `--t-*` tokens and the language at the handshake and when they change; the SDK applies
  them to `:root` with `<html data-theme>`, `color-scheme` and `lang`, and has no stylesheet of its own: give defaults. The
  tokens are `--t-bg`, `--t-surface`, `--t-surface-hover`, `--t-text`, `--t-text-secondary`, `--t-text-muted`, `--t-border`,
  `--t-border-strong`, `--t-accent`, `--t-danger`, `--t-success`. A view works in the light and the dark theme and from 360 px wide.

### `@harnessly/app-sdk`

One dependency-free file the runtime serves to every view at `/__hl/app-sdk.js` as a classic script (the same code as an ES module is at `/__hl/app-sdk.mjs`, for a bundler; a module script cannot carry the slot's cookie from an opaque origin). Types: `/__hl/app-sdk.d.ts`:

```html
<script src="/__hl/app-sdk.js"></script>           <!-- a classic script: window.harnessly -->
<script>
  harnessly.connect().then(async (hl) => {         // rejects with host_unavailable outside Harnessly
  hl.context                                        // app, target, view, theme, locale, chat, user, readOnly, data (a card)
  await hl.chat.send('Make a draft')
  await hl.storage.install.set('last', { id: 7 })
  })
</script>
```

| Call | Permission | What it does |
| --- | --- | --- |
| `hl.on(event, fn)` | none | `context`, `theme`, `selection`. `tool-result`, `tool-input`, `flow`, `data`, `visibility` are not sent by the page yet |
| `hl.chat.send(text)` | `chat.prompt` | A message to the agent of this chat; the chat always shows the app's real name before it, on one line |
| `hl.chat.read({last})` | `chat.read` | The latest messages of the chat |
| `hl.chat.card(view, data)` | `chat.card` | A card of the app in the feed |
| `hl.pane.open(target)` | `pane.open` | Opens `app:<id>[/<view>]`; anything else is `invalid_params` |
| `hl.tools.call('server.tool', args)` | none | A tool of the app's own remote MCP server (`.mcp.json`), called by the machine: `https://` on a public address only, 30 s, 4 MB. A `command` server is `policy_blocked`, a header with `${` is not sent. Answers the raw MCP result, `isError` included |
| `hl.storage.<scope>.get/set/delete/keys` | `storage` | Scopes `install`, `user`, `chat`, `space`, `doc`; writing `space` needs `storage.space.write` |
| `hl.flow.*`, `hl.data.*` | none | Not connected in the page yet: `host_unavailable` |
| `hl.ui.toast/confirm`, `hl.notify(text)` | `notify` for the last | A notice, a question, a message to the person |
| `hl.resize(height)` | none | The height of a card (sent by itself for cards) |

**Classic scripts only.** A view's frame has an opaque origin, which sends the slot's cookie with classic scripts, stylesheets
and images but not with module scripts or CORS-mode loads (`fetch` without credentials, fonts), so those come back 401. Load
the SDK with `<script src="/__hl/app-sdk.js">`, ship data as a script that sets a global, and keep the code in classic files.

A refusal is a `HarnesslyError` with a `code`: `permission_denied`, `not_in_target`, `policy_blocked`, `network_denied`,
`rate_limited` (60 calls a second), `host_unavailable`, `invalid_params`.

The wire is MCP Apps: JSON-RPC 2.0 over `postMessage` (`ui/initialize`, `tools/call`, `ui/message`,
`ui/notifications/*`) with the host's own methods under `harnessly/`. The host hears only the frame's window on the
frame's origin, and checks each call against `permissions` and `targets`; the runtime checks storage and tool calls once
more.

The bridge belongs to the document the view server served. The server writes the session's nonce into each page
(`<meta name="hl-nonce">`) and, right after it, `<script src="/__hl/bridge-shim.js">`, before any script of the view. The
shim sends `ui/initialize` with the nonce; only a document that presents it is answered, and the answer carries one end
of a `MessageChannel`. From then on the host accepts calls only on its end and ignores the frame's window altogether, so
a document that replaced the view cannot slip a call in before the host hears its `load`; the host also drops the bridge,
closing the port, on every `load` of the frame after the first. The shim replaces `window.parent` with a stand-in whose
`postMessage` sends on the port and re-dispatches the port's messages as `message` events on `window` (the host's origin,
the stand-in as `source`), and answers the view's own `ui/initialize` from the host's reply. So a standard MCP Apps view
(`App` from `@modelcontextprotocol/ext-apps`, or hand-written `window.parent.postMessage`) works unchanged, and so does
the SDK, which knows nothing of nonce or port. The host context is MCP Apps' (`theme` is `light` or `dark`,
`styles.variables` the tokens, `locale`, `displayMode`); the whole of ours is under `hostContext.harnessly`, which is
what `hl.context` is. A view session lives an hour unused (twelve at the most); a mounted view renews it every ten minutes through
`POST <platform>/apps/session/renew`, which changes nothing but the end. A view at the app's own address (`views[].entry` an `https://` address listed in `network.frames`)
is not served by the host, so it has no nonce; only the navigation rule holds for it. Views may post their forms to
themselves (`form-action 'self'`) and nowhere else.

### Storage

Per app, under `<data>/apps/storage/<id>/<scope>/`. An area of scope `chat` is the chat the view is mounted in (the
runtime reads it from the view's session, a call cannot name another); `doc` is refused until a view is bound to a
document. Limits: `install` 5 MB, `user` 1 MB, `chat` 256 KB per chat and 20 MB over all chats, `space` 10 MB, `doc` 100
KB per document and 20 MB in all. A write past a limit is refused whole.

### Cards in the chat

A tool of the app's own MCP server names a view of the app and the data to draw it with:

```json
{"_meta":{"ui":{"resourceUri":"ui://acme.cashback/card"}},"data":{"percent":5}}
```

The feed draws the app's view `card` (a view with the `chat` target) under that tool step, through the same bridge; the data is `hl.context.data`. The feed reads this
from the agent's transcript, where a result is the first line of its text, cut at 100 characters: so the answer's `text` has to be this one line, under 100 characters, and a card
with more to show reads it from a file or storage. "Open in the pane" opens the app's pane view. Only a tool of the app's own server may name the app's view.

### The app as the main thing

`/apps/<id>?chat=<conv>` shows the app's `app`-target view on the whole page and the chat it works in beside it. The
chat stands in one of three places, chosen with the switch in the page's head and kept for the viewer: a column at the
left, a window over the app (moved by its head, grown by its corner), or only its field at the foot with the agent's last
line over it. The pane's «во всю ширину» on an app tab opens this page; its back arrow returns to the chat. Without
`?chat=` the app stands alone. A view for this target is declared with `targets: ["app"]` (alongside `pane`, if the
same page serves both). The page's «…» has «О приложении» (the rights, targets and version), «Отладка» for a dev install
and «Отключить в этом чате».

### Host calls beyond storage and tools

These are done by the page, after the bridge has checked the call against `permissions` and `targets`:

| Call | What the page does |
| --- | --- |
| `hl.chat.read({last})` | The latest messages of the chat as `{role, text}`, text only (up to 200) |
| `hl.chat.card(view, data)` | Puts a card of the app's `chat` view (`data` up to 20 KB) at the end of the chat's feed, under the app's name. The feed is the agent's transcript and cannot be written into, so a posted card stays at the tail and is kept in this browser, not on the machine |
| `hl.ui.confirm(text)` | A dialog titled with the app's real name; resolves a boolean, `true` only for its OK |
| `hl.notify(text)`, `hl.ui.toast(text)` | The platform's notice, under the app's name |

### Files of the workspace

`hl.files.read(path)` and `hl.files.write(path, text)` reach the files of the folder the chat works in, nothing else. The app
holds them with a mask per right: `files.read:docs/**`, `files.write:out/*.md`. A mask is a path of the workspace where `*`
stands for a run of characters inside a name and `**` for any number of folders; it may not be absolute, start with `~` or
contain `..`, and `files.write:**` over the whole workspace is a warning at validate. A file moves as text, up to 4 MB.

The runtime judges a path by where it really leads, on every call:

- `..`, an absolute path, `~` and a NUL are refused as written; the path is then cleaned, every symlink is followed (a file
  that does not exist yet is judged by its nearest folder that does), and the result has to lie in the workspace **and** match
  a mask. A link inside the workspace to a secret, to another place, or out of it does not open the door.
- Secrets are out whatever a mask says, matched without regard to case (`.GIT/config` is `.git/config` on a case-insensitive
  disk): `.env`, `.env.*`, `.envrc`, `.git/` (its config holds remotes with credentials, its hooks run code), `.ssh/`, `.aws/`,
  `.gnupg/`, `.kube/`, `.docker/`, `.npmrc`, `.netrc`, `.pypirc`, `*.pem`, `*.key`, `id_rsa`-style keys.
- Out of reach too: the host's data folder (the apps registry, the `cli-token`), and every installed app's package folder and
  source folder (a dev install's), whichever app owns it and wherever it lies, even inside the workspace. They are the same set
  the agent's guard protects (`Registry.ProtectedDirs`), compared as written and as resolved.
- **Writes** refuse agent and tool configuration (`.claude`, `.mcp.json`, `.vscode`, `.idea`, `.harnessly`, `.github`,
  `CLAUDE.md`, `AGENTS.md`: code or prompts in disguise; they can be read) and every dot-file or dot-folder that no mask names
  with a dot of its own. `files.write:docs/**` and `files.write:**` never reach `docs/.cache/x`; `files.write:.notes/**` does
  reach `.notes/`.
- The read or write itself goes through the workspace's root (`os.Root`), which does not follow a link out of it, so a link
  swapped in between the check and the open is refused too.
- The workspace is the chat's own folder as the machine records it; the call names none. A view in no chat has no workspace,
  and an app that is not on in the chat gets nothing.

A refusal is `permission_denied` and does not repeat the host's paths.

### App Inspector

Not a tab of its own: the page's «О приложении» shows the app's rights, targets and version, and for a dev install
(`harnessly app dev`) «Отладка» adds the view's bridge calls (method, the right it needed, `ok` or the error code, no
arguments or data), the keys the app keeps in each storage scope, and a reload. In the pane the same sits under the view
behind a small «Отладка» button of the tab, shown for dev installs only.

## Tool policy

`agent.tools` has three lists of rules: `allow`, `deny`, `confirm` (the person confirms the call in the chat). A rule
is a built-in tool (`Edit`), a built-in tool with a mask on its input (`Bash(git push*)`, Claude's `Bash(rm:*)` works
too), a tool of an MCP server (`bank-cashback.create`, `bank-cashback.*`), the host's `app.*` tools, `mcp:*` for every
MCP tool or `*`.

**`allow` grants nothing**: the hook answers only for `deny` and `confirm`, so `allow` only beats a broader rule, and a call it wins is left to the
chat's own permissions. `Edit(<mask>)` is matched against `file_path` exactly as Claude wrote it, normally absolute (`Edit(*/docs/*)`), and `Edit` does
not cover `Write`, `MultiEdit` or `NotebookEdit`.

Within one app the most specific rule wins: an exact name beats a mask, a longer mask beats a shorter one, `mcp:*` and
`*` lose to everything; on a tie `deny` beats `confirm`, `confirm` beats `allow`. Across the apps of a chat a `deny` of
any app wins, then a `confirm`. A call no rule names is left to the chat's own permissions.

A `Bash(...)` rule is matched against each simple command of the line (`a && b`, `a; b`, a subshell, with variable
assignments and the program's path cut off). A line the guard cannot see into (`eval`, `sh -c`, `xargs`) is confirmed
by the person instead of passing a deny silently, and an `allow` holds only when every command matches. An app's
allow reaches only its own MCP servers. The rules are a guardrail for the agent, not a sandbox: the agent runs as the
same user as the runtime, and a determined one has ways around any check of its command lines.

The guard holds the rules the launch was given: editing a manifest, or running `harnessly app enable|disable|install|
dev|remove` (and their aliases) from inside the chat, is refused, and so is writing into `<data>/apps` or an app's
folder. A snapshot the guard cannot read, or one whose content no longer matches the hash in its name, refuses every
call.

## Host tools for the agent

A chat with apps gets one more MCP server, `harnessly-app` (the runtime's own endpoint, added to the launch only when the
chat has apps). Its tools answer for the apps on in that chat and no others; `app` names one and may be left out when only
one app is on. Claude writes tool names with `_`, the tool rules with `.`: `mcp__harnessly-app__data_get` is `app.data.get`, `sinopia_set_fields` is `app.sinopia.set.fields`.

| Tool (rule) | What it does |
| --- | --- |
| `open` (`app.open`) | Opens the app's view for the pane (`view`, the first one when omitted) in the person's pane |
| `data_get`, `data_set` (`app.data.get`, `app.data.set`) | The app's `chat` storage of this chat by `key`: the same data its view reads with `hl.storage.chat`, with the same limits |
| `ask` (`app.ask`) | Puts a choice (`question`, 2 to 8 `options`) to the person in the app and waits up to ten minutes; answers with the option or `cancelled` |

The host's choice is drawn over the app's view with the app's name and answered through `GET <platform>/apps/asks?conversation=`
and `POST <platform>/apps/asks/answer`; an agent that stopped waiting leaves no choice behind.

## Sinopia

An app that lists a target `sinopia.<kind>` (`site`, `slides`, `video`, `ds`; `sinopia.*` is all of them) works in the documents of
that kind, and with `"activation": {"on": ["document"]}` it is on in every one of them. A `sinopia` object in the manifest says what
it puts into the editor; every contribution may be narrowed with `kinds`, and one for a kind the app does not target is an error.
The document types an app adds are `sinopia.kinds`, below.

```json
"sinopia": {
  "panels": [{ "id": "lint", "view": "panel", "title": "Check" }],
  "selection": [{ "id": "shorten", "title": "Shorten", "agent": "Cut the text in half" }],
  "bindings": { "schema": "schema/cashback.json", "inspector": "inspector", "lockUnbound": true },
  "checks": [{ "id": "deck", "on": ["save", "share"], "blockShare": "error", "rules": [ … ] }],
  "templates": ["templates/cashback-card"]
}
```

| Contribution | What the editor does |
| --- | --- |
| `panels` | A view of the app (its `targets` include the document's) beside the editor, on the right, for the kinds named. Several panels become tabs; the person can hide the panel. On a narrow window it is a sheet over the lower part |
| `selection` | An item of the bar that shows while elements are picked. `agent`: a task for the chat linked to the document, with the elements as an `<annotation>`; `tool`: a tool `<server>.<tool>` of the app's own MCP server, called with `{document, nodes}` |
| `bindings` | Elements of a board with `data-hl-field="<name>"` belong to the app. A click on one opens `inspector` (a view of the app) on that field: the element is put into the selection first (`hl.sinopia.selection()`), an inspector already open is told by the `selection` event `{document, field, nodes}`. `data-hl-attr="<attribute>"` makes the field set that attribute, else it is the element's words. `schema` is a JSON Schema (type, enum, minimum/maximum, minLength/maxLength, pattern) that every written value must meet. `lockUnbound` refuses hand edits of the elements that carry no binding |
| `checks` | Run when the document is saved (the boards changed) and when it is shared or published (`on`). A finding is a mark on the element: the board outlines it and shows the note. `blockShare` (`error` or `warning`) is the level from which a finding stops the share: the share and publish buttons ask the checks first and, if one blocks, show what it found instead of opening |
| `templates` | Folders of the package with a `board.dc.html`; `hl.sinopia.boards.add({template})` adds a board made from one |

A check is one of two kinds. `rules` are evaluated by the host and need no server: `{id, select, level, message, fix?}` plus one
limit, `maxWords` or `maxChars` (the element's own text, not its block children's) or `minFontPx` (its inline `font-size`);
`message` may say `{n}` and `{limit}`. `tool` names a tool `<server>.<tool>` of the app's MCP server, called with
`{trigger, document, nodes}` and answering `{findings: [{node, level, message, rule?, fix?}]}`. `when` expressions are not
evaluated yet.

The Document API is the same for a view (`hl.sinopia.*`, over the bridge) and the agent (the tools `sinopia_*` of
`harnessly-app`, rule `app.sinopia.*`). It reads and writes the boards of a document that has `meta/` and `.dc.html` boards
(`<x-dc>`); another document answers `invalid_params`. A node is an element of a board with the id `<page id>:<number>` (the
number is its `data-dc-tpl`) and a fingerprint `fp` that a change may present to be refused if the element moved on.

| Method (SDK) / tool | Right | What it does |
| --- | --- | --- |
| `document()` / `sinopia_document` | `sinopia.read` | Kind, title, boards, revision |
| `selection()` / `sinopia_selection` | `sinopia.read` | What the person picked |
| `node(id)`, `query(selector)` / `sinopia_node`, `sinopia_query` | `sinopia.read` | Elements; a selector is type, `#id`, `.class`, `[attr]` (also `^= $= *= ~=`), a space and `>`, commas; at most 500 come back; `text` is clipped (300 characters), `words` counts the element's own words whole |
| `fields()` / `sinopia_fields` | `sinopia.read` | The bound fields with their values |
| `mark(nodes, {note, level, replace})` / `sinopia_mark` | `sinopia.read` | Marks on the board (`error`, `warning`, `info`) |
| `patch(ops)` / `sinopia_patch` | `sinopia.write` | `setText`, `setStyle`, `setAttr`, `insert`, `remove`, `setField`: all or none, one history entry. A board that another chat holds a claim on is refused (`policy_blocked`) |
| `setFields(patch)` / `sinopia_set_fields` | `sinopia.write` | Bound fields, checked against the schema |
| `boards.add({template, title, data})` / `sinopia_boards_add` | `sinopia.write` | A new board from a template; `data` goes into its bound elements |
| `comment(node, text)` / `sinopia_comment` | `sinopia.comment` | A note on an element. From a view it also goes to the linked chat as an `<annotation>` |
| `render()` / `sinopia_render` | `sinopia.export` | Not available yet: `host_unavailable` |

Limits worth knowing: the app's view shows in a document of its own kind only; text set into a board is escaped and an inserted
element is cleaned (no scripts, no event handlers, `style` only through `setStyle`); `setAttr` refuses `on*`, `srcdoc`,
`data-dc-*`, `{{ }}` and `javascript:`. The share gate is the editor's: a link given by the gateway or a public address is not
stopped by it (the runtime reports `blocked` from `POST <platform>/apps/sinopia/checks`).

A board marks the bound elements and the findings only when the page tells it to, and talks to the page over its own messages
(`tk:hl`, `tk:hl-marks`, `tk:hl-click`; a picked element carries its `data-hl-*` in `tk:picked`). The page hears only a board
that said hello from the slot's origin.

### Document types of your own

An app can add a type of Sinopia document next to site, slides, video and ds: a moodboard, a form, a map. It needs the
permission `sinopia.kind`, a target `sinopia.<type>`, and a view that edits the document.

```json
"targets": ["sinopia.acme.moodboard"],
"permissions": ["sinopia.kind", "sinopia.read", "storage"],
"views": [{ "id": "editor", "entry": "ui/editor.html", "targets": ["sinopia.acme.moodboard"] }],
"sinopia": { "kinds": [{
  "id": "acme.moodboard", "title": "Мудборд", "icon": "ui/moodboard.svg", "editor": "editor",
  "schema": "schema/moodboard.json", "schemaVersion": 2, "dataKey": "board", "migrations": { "1": "migrations/1-to-2.json" }
}] }
```

- **The name is the app's own.** A type is the app's id, or the id with up to two more segments (`acme.moodboard`,
  `acme.moodboard.board`); another name is a validate error. Two installed apps cannot add one name (the second install is
  refused with `kind-taken`). The host's types have no dot, so nothing an app adds can be taken for them, and `sinopia.*` in
  another app's targets does not reach an added type: an app is in it only by naming it.
- **The host owns the project**: its folder, history, claims, links, comments and rights. `POST <platform>/apps/kinds/create`
  makes it (`{kind, title}`) and records its type in `<data>/apps/kind-docs.json`; the library's list leaves such a project out
  of the sites, decks and videos. The Document API tells its type (`hl.sinopia.document()` → `kind`).
- **The app owns the editor** (the view named in `editor`, shown with target `sinopia.<type>` and the document) and the files of
  its format, through the SDK: `hl.storage.doc` keeps data bound to the document, `hl.sinopia.*` reads it, `hl.files.*` the
  workspace within its masks. `GET <platform>/apps/kinds` lists the types of the apps that are on; Designly's start shows a
  tile for each next to Site, Presentation and Video, and a click makes the document (named by the brief typed so far, else
  by the type's title) and opens it at once. The library lists such documents with the others (`GET <platform>/apps/kinds/docs`:
  title, type and date, no pin or delete) — in the full library and in the Designly tab of a chat's right pane alike — and a
  click opens `/designly/doc/<id>` (in the pane, in the pane): the page calls `kinds/open` and shows the app's editor view in a
  frame with the target `sinopia.<type>` and the document. A document that may only be read still shows the editor, in
  **read-only mode**: `kinds/open` answers `readOnly: true` with a `code` (`newer`, `failed`, `no-room`, `unreadable`,
  `no-package`, or `view-only` when the person's role in the project is below edit; such a document is not migrated), the
  frame carries `hl.context.readOnly === true`, and a line above it says why. A view reads `hl.context.readOnly` (also in the
  `context` event) and offers no way to change the document; as a safeguard the host refuses, with `permission_denied`,
  `hl.storage.doc.set/delete` and `hl.sinopia.patch/setFields/boards.add` while the document is open for reading. An app that
  is not installed shows «Нужно приложение» with the way to the apps.
- **An old document is migrated when it is opened.** `dataKey` names the key of `hl.storage.doc` that holds the document. The
  host records the format version beside it (the key `@schema`, written when the document is made) and, in `kinds/open`, runs
  the steps from that version to the app's. The old value is kept under `<dataKey>@v<N>` in the same write as the new one,
  so the migration is all or nothing, and `POST <platform>/apps/kinds/revert` (`{document, to}`) puts it back. A document that
  does not fit a step, is of a newer version than the app, or has no room for the old copy is not touched: `kinds/open`
  answers `readOnly: true` with the `reason`. A document with no recorded version is left as it is. The copy lives in the
  app's storage, not in the project's file history.
- **Without the app** the document stays: `POST <platform>/apps/kinds/open` answers `needsApp: true` with the type, and the
  page shows it read-only with the note that it needs the app.
- **The format has versions.** `schemaVersion` is the current one; `migrations["1"]` is a JSON Patch (RFC 6902: add, remove,
  replace, move, copy, test) that takes a document from version 1 to 2, and there is a step for every version before the
  current (validate checks that the chain is whole and each file is a patch). `Migrate` applies the steps in turn, all or
  nothing, so a document that does not fit leaves the old one as it was.
- `guest` is not supported yet: a manifest that has it is refused at validate, so no field promises a view that is not there.

[`apps/moodboard`](apps/moodboard) is such a type made from the SDK alone: an editor view that keeps the cards with
`hl.storage.doc`, a JSON Schema, and a migration from version 1 (`notes`) to 2 (`cards`).

## Sharing

### Versions and channels

A version is published once and never changes; its number is never used again. A channel is a pointer at one of the
versions, with the ones it pointed at before.

```sh
harnessly app publish ./cashback --channel beta       # 1.3.0 in the manifest -> 1.3.0-beta.1 (next free N)
harnessly app promote 1.3.0 --channel stable ./cashback   # the beta becomes 1.3.0, a version of its own
harnessly app rollback --channel stable ./cashback     # the channel points at the version before
harnessly app channels acme.cashback                   # where dev, beta and stable point
harnessly app install --channel stable acme.cashback   # an install that follows the channel
harnessly app update [--yes] [acme.cashback]           # bring the installs to what their channel points at
```

`dev` is the author's, `beta` is for those who followed it, `stable` is for everybody. A manifest that says `1.3.0` goes to
`dev` and `beta` as `1.3.0-dev.N` / `1.3.0-beta.N` and to `stable` as `1.3.0`; a manifest that already says a pre-release
(`1.4.0-rc.1`) is published as written and never to `stable`. Published versions live in `<data>/apps/published/<id>/v/<version>/`,
the pointers in `channels.json`.

An install that follows a channel moves with it: a patch or a minor at once; a new major, or a version that asks for
more permissions, secrets or network addresses, waits for `app update --yes`. A move back (a rollback) is made at once.
Promoting to `stable` asks the evals: a package with `evals/*.json` needs a report (`harnessly app eval` leaves it in
`.harnessly/eval.json`) whose pass rate is at least 80 % (`--threshold` changes the bar for one promotion).

The agent of a chat with apps on publishes to `dev` and `beta` only; `stable`, `--team`, `promote`, `rollback`, `team`,
`share` and `update` are refused to it by the guard (the person runs them).

### The team's catalog

A team is the platform's space. The platform tells the machine which space it is in and the person's role in it
(owner or member): the op `apps.team` carries the snapshot (role, the catalog with the archives of its entries, the team's
installs) to `<apps>/team.json`, the way `design.access` does for the design library; `apps.team.snapshot` reads it back
from a machine (the one an owner installed on) for the platform to send to the others. A machine without the platform
is told by hand: `harnessly app team set <space> owner|member`.

- `harnessly app publish --team` (or `app team publish <id> <version> --channel C`) puts a published version in the
  catalog, one entry per app and channel, with the version before it kept for a rollback. Any member may; there is no review,
  the owner who installs an app answers for it.
- `harnessly app install --team <id> [--channel stable]` installs the catalog's app for the whole team. Owners only.
  Members have the app without installing: the registry merges the team's installs in; a member's own install of the
  same app wins. On `/apps` the catalog is under the list: «Установить для команды» for an owner, «Выключить у меня» for
  a member (the app stays installed for the team; a member cannot remove it). The change waits for the machine.
- The team's install follows its catalog entry like a channel does: a patch or a minor reaches the machines at once, a
  major or new rights wait for the owner to install again. `harnessly app rollback --team --channel C` points the entry
  back.
- An app of the `trusted` tier is never installed for a team, from a catalog or any other way: it runs code on the
  machine, so each person installs it by hand. The runtime refuses it, a snapshot that carries one is applied without it,
  and the page does not offer it.
- Packages travel as gzip tars (at most 16 MB); the receiving machine checks the entry's SHA-256, unpacks only plain
  files inside the folder, and checks the package as any other (the same id and version as the entry says) before it
  is used.

### The public catalog

The apps their authors offer to everybody: anyone browses it and installs for themselves, no team needed. It is the team's
catalog made wider, on the same machinery (a package as a gzip tar with its SHA-256, the channels), and the same line holds.

- `harnessly app public publish <id> <version>` puts the version that stable points at into it. Before it goes in, the
  machine checks the package: the strict check (a warning is an error), a `LICENSE` file, and no native binary (ELF, Mach-O,
  PE) in a package that is not trusted. A script with the execute bit is text and passes.
- An app that asks for the whole network (`network` with `*`) is stored as `pending` and is not listed until a person at the
  platform approves it (`POST <platform>/apps/public/review`, the machine agent's relay only). Everything else is listed at once:
  the checks are the machine's, and the author answers for the rest.
- **The trusted tier is in no catalog**, team or public: `public publish` refuses it, the list never shows one, and a snapshot
  that carries one is applied without it.
- `harnessly app public [list [QUERY]]` and the «Публичный каталог» section under the team's on `/apps` (a search; the rights
  are shown before the install, then «Установить с этими правами») list the approved entries. The install names the archive's sum
  that was shown: an entry that changed in between is refused (`changed`) and nothing is installed unseen. The install is the
  person's own, removable like any other, and follows no channel.
- The platform sends a machine the reviewed list (`POST <platform>/apps/public/sync`, with the archives) and reads this machine's
  entries to review (`.../snapshot`, `.../pending`); the machine keeps them in `<data>/apps/public.json` and `public/`, so a
  platform that is down does not empty the catalog. The agent of a chat does not run `app public`.

Not built: the platform's side of the list (the relay that sends `sync` and takes `snapshot` is not in this repo), ratings and
categories, and updates of a public install (it is installed at the version shown; a new one is installed again).

### A standalone app by link

An app with the `app` target can be shown to someone with no account and no machine:

```sh
harnessly app share acme.report --view report --ttl 7d       # prints /apps/guest/<token>/ ; --write lets the guest write
harnessly app shares [acme.report]
harnessly app unshare <link id>
```

The guest opens `<machine address>/apps/guest/<token>/` and gets only the app's view: no chat, no agent (guest AI is
off; the host refuses `ui/message`, `tools/call`, the chat, pane, flow, notify and document calls with
`permission_denied`), the theme and the wire method `ui/open-link` (https; the SDK has no call for it, and the app's normal page does not answer it). Its `hl.storage.*` is the link's own area (every scope names it) and
needs the `storage` permission; a link reads only unless it was made with `--write`. The view runs in the same sandbox
(opaque origin, the CSP of its `network`). A link ends at its time (7 days by default, 30 at most), when it is revoked, or
when the app is uninstalled. Only the token's hash is kept. The page itself carries no token for the view to read.

The runtime answers these routes without a session (the token is the authority); how far a guest can reach the machine is
the deployment's: through the hub's proxy a guest would need the hub session, so a public link is for a machine the
guest can reach directly, or for the platform's gateway to relay. Data a guest sees beyond the link's storage comes from
the app's own server, through `network.connect` or a framed view.

### Evals

`harnessly app eval [dir]` runs the scenarios in `evals/*.json` on a real agent and judges what the agent did:

```json
{
  "prompt": "Заведи кешбэк 5% на АЗС с 1 ноября",
  "mocks": { "mcp": { "bank-cashback": "fixtures/bank.json" } },
  "expect": {
    "tool_used": ["bank-cashback.get_categories"],
    "tool_not_used": ["bank-cashback.create"],
    "final_text": ["согласовать"],
    "final_text_not": ["завёл"],
    "max_tool_calls": 12
  },
  "graders": [{ "llm": "The agent showed a preview and asked for approval; it did not create the cashback itself" }],
  "runs": 3,
  "baseline": true
}
```

- Tools are named `<server>.<tool>` (an MCP tool of the app's package or a mock), or the agent's own name (`Bash`).
- `mocks.mcp` maps a server of the package's `.mcp.json` to a fixture, `{"tools": [{"name", "description", "inputSchema",
  "result"}]}`, a file of the package. The eval runs a copy of the package whose `.mcp.json` has only the mocked servers,
  each answering from its fixture (`harnessly app mock-mcp <fixture>`, a stdio MCP server): an eval never reaches the
  app's real backend. **A server of the package with no mock in the scenario is dropped, and the agent runs without its
  tools**: put `mocks.mcp` on every scenario that needs them (the report says in a `note:` when a server has no mock, or a
  mock names a server the package does not have). If a mocked server does not come up in the run, the run is an error
  that says which server, not a failed expectation.
- The agent is Claude Code in print mode, in an empty folder, with the app's package as a plugin, the chat's app-guard
  hook, only the app's tools allowed, nothing of the person's own settings or plugins, no session kept. It uses the
  person's own login; the person's claude.ai connectors are switched off (`--strict-mcp-config` is not used: it would hide
  the plugin's own servers too). `--agent codex` is not supported yet.
- A scenario runs `runs` times (default 3); a run passes when all `expect` holds and every `llm` grader (a second
  print-mode call that answers PASS or FAIL as its first word, asked once more if it does not settle or contradicts itself;
  `--no-judge` leaves them unjudged and the report says so) agrees.
- `baseline: true` also runs the prompt without the app: the report shows the pass rate without it and what the app adds
  to the input tokens of a run.
- The report is printed and left in `.harnessly/eval.json` (`--no-report` skips it, `--json` prints it as JSON). The exit
  code is 1 when the pass rate is under `--threshold` (default 0.8). Other flags: `--runs N`, `--case a,b`, `--no-baseline`,
  `--model`, `--timeout`.

## What keeps the agent out of the apps

The agent of a chat with apps runs as the person, so a rule in a hook is a guardrail against its mistakes, not a wall. What
holds now, in layers:

- **The app-guard hook** judges every call by a snapshot taken at launch. Edit, Write, MultiEdit and NotebookEdit are judged by
  where the path **really leads** (the path made absolute, symlinks followed), so a link from the chat's folder into
  `<data>/apps` or an installed package is that folder; a Bash line naming the registry or a package, written either as the
  launch has it or as it really is, is refused. `harnessly app enable|disable|install|remove|promote|rollback|team|public|share|…`
  are the person's; the agent publishes to dev and beta only.
- **Claude's own deny rules** are written into the chat's settings: `Edit(//<registry>/**)` for the registry and each app's
  package (Edit's rule covers Write, MultiEdit and NotebookEdit), so the file tools are refused by Claude itself, not only by
  the hook.
- **Off from the page only.** `POST /api/cli/apps/enable` with `on: false` from the tab of a chat that has apps is refused
  (`use-page`); the page's switch (`conversation.apps`) does it.
- **The source of every app is out of reach.** The registry and the package and source folder of every installed app are read-only in a chat with any app,
  dev installs included, not only the apps on in that chat. So an agent writes an app in a chat with no apps on.

- **A token of the launch, not the person's.** The agent's CLI calls the runtime with a token of its own launch,
  `agent.<launch>.<expMs>.<hmac>` signed with the runtime's `agent-token-key` and good for 30 days (a resume issues a new one),
  read from the private file `HARNESSLY_CLI_TOKEN_FILE` names. With it, the routes that change the apps (`install`, `remove`,
  `enable`, `promote`, `rollback`, `follow`, `sync`, `team/*`, `share`, `unshare`, `public/install`, `public/publish`, and
  `publish` past dev and beta) answer 403 `agent_cannot_change_apps`, however the request was sent; the rest of the CLI
  (tabs, `open`, notifications, reading the apps) takes it as the cli-token. A forged or expired agent token is no token.

- **The operating system's wall.** The agent's CLI runs in a mount namespace of its own (bubblewrap): the machine bound as it
  is, and over it the registry and every installed package and source folder bound **read-only**, in the form they really
  have (a link to them leads into the bind). Any write there from the agent's process tree, whatever program makes it (a
  redirect, `cd` and a relative path, a script it wrote, python, `mv` of a package), fails in the kernel with «Read-only
  file system»; the chat's own folder and the rest of the machine stay as they were, and the runtime, outside the namespace,
  still installs and updates the apps. The launch line is `VAR=… bwrap --dev-bind / / --ro-bind <dir> <dir> … -- <runtime>
  sandbox-exec -- claude …`; `sandbox-exec` gives the terminal's foreground to the CLI before it becomes it, so the pane's
  foreground program is still `claude` and Ctrl-C reaches it, and the runtime looks through bwrap when it finds the agent
  under the pane. A bind keeps its folder, not the path to it, so every folder above a bound one that the agent could rename
  (its parent is writable: `~/.harnessly`, the folders above an author's source) is bound onto itself, writable as before:
  a mount point cannot be renamed or removed, and the path the runtime reads stays the folder it trusts. The person's
  session bus and user systemd would run a command outside the wall, so inside it the bus socket has `/dev/null` over it,
  `$XDG_RUNTIME_DIR/systemd` is an empty tmpfs and `DBUS_SESSION_BUS_ADDRESS` is unset (the ssh and gpg agents' sockets
  stay). `internal/apps/boundary_test.go` runs a real child through the wall on the machine it is on.

Where the wall cannot be made (no `bwrap`, user namespaces refused by the kernel or by AppArmor, a container), the chat still
starts, guarded by the hook and the deny rules alone, and this is never silent: the runtime logs a warning at every such launch,
«Проверка машины» shows `apps_boundary` failing with what bwrap said and the command that puts it right, and the app's «О
приложении» says the agent is not walled off. A refusal was the other choice; it would take every chat with an app down on
such a machine for a wall the person cannot put up from the page, while the layers above still hold.

What a machine needs: the `bubblewrap` package, and unprivileged user namespaces allowed for it: where AppArmor restricts them
(`kernel.apparmor_restrict_unprivileged_userns=1`, Ubuntu's default) the profile `/etc/apparmor.d/bwrap-userns-restrict`
loaded (Ubuntu 26.04's apparmor package carries it); elsewhere `user.max_user_namespaces` above 0 (and Debian's `kernel.unprivileged_userns_clone=1`).
The runtime's unit must not set `RestrictNamespaces=`, `NoNewPrivileges=` is fine, and `ProtectHome=`/`ReadOnlyPaths=` of the
unit already apply inside.

`/etc/ssh/ssh_config.d` is empty inside: ssh refuses an included file it cannot see as root's, and in the namespace root's files belong to nobody. So ssh and git over ssh work in the wall by the person's `~/.ssh/config`, without the system's drop-ins.

The wall also takes from the agent what would let it pass for the person, or have its code run outside later:

- **The runtime's API.** A launch's token is refused on every route except a named list (`confineAgentToken` in
  `cmd/runtime/apps_agent.go`): what the agent's CLI, hooks and MCP servers call. A terminal, a new tab or agent, input into a
  pane, a peer's mailbox, the browser tab and the delegation tools run outside the wall, so they answer `403
  agent_outside_the_wall`, and a route added later is shut until it is named.
- **The data folder.** Inside it is a tmpfs with only what a launch reads (`internal/apps/seal.go`): the session key and
  password (`config.json`), the safe, the machine's token, the runtime's state and the scripts other chats and the tmux
  server run do not exist there. The folder is a mount point, so it cannot be renamed; the runtime resolves a link on the way to
  it once, at its start, because a link in the home folder cannot be pinned.
- **Start-up files, read-only:** shell profiles, `~/.ssh` (except `known_hosts`), `~/.config/systemd`, `.gitconfig`, tmux's and
  vim's, the settings, hooks, plugins, skills and instructions of Claude Code and Codex, and every folder under the home folder on the
  PATH. Those not there yet are made empty first (a file the agent could create is as good as one it could change);
  `.bash_profile`, when made, reads what a login bash read before it. So in a chat with apps `npm i -g` into the home folder and
  changing these files fail with «Read-only file system».
- **Servers that run things:** the tmux server, the session bus and user systemd, the Docker and Podman sockets.
- **Other processes.** `sandbox-exec` puts the agent in a Landlock domain before it becomes the CLI
  (`internal/apps/harden_linux.go`): ptrace-level access out of the domain is refused (`/proc/<pid>/root`, `mem`,
  `process_vm_writev`) on any kernel since 5.13, and signals out of it and connections to outside abstract sockets since 6.12. A
  seccomp filter refuses TIOCSTI and TIOCLINUX, whatever the kernel's `dev.tty.legacy_tiocsti` says.
- **`sudo` and setuid programs do not raise rights** (bwrap sets no_new_privs), which also keeps the agent from remounting the
  binds. This is the wall, not an omission: root would take it down. What needs root runs in a chat without apps or in the
  person's terminal.

A login to the same account over ssh would be outside the wall. The agent cannot add a key (`~/.ssh` is read-only), but a key
already authorised works: «Проверка машины» shows `apps_boundary` failing where ssh from the machine to itself logs in, with the
command that refuses such logins in sshd. The check also reports a kernel without Landlock or its scopes.

What stays open, and cannot be closed by this wall:

- **Code the agent writes in the workspace is run by whoever runs it next.** A chat works by changing the project: `Makefile`,
  `package.json` scripts, `.git/hooks`, a project's `.claude/settings.json`. Another chat or the person running them runs them
  outside the wall. Review what a chat with apps changed, as any change.
- **Other programs that read a file of the person's and act on it** when the person opens them. The list above covers what acts
  without the person doing anything else; it is not every program.
- **What the agent is allowed by design:** it runs as the person, so it reads the person's files and tokens outside the data
  folder (`~/.config`, `~/.claude`), uses the safe by name and has the network. The wall does not stop a leak; an app's `network` is
  a rule for its views, not for the agent.
- **A chat without apps and a Codex chat have no wall.** Apps are on in Claude Code chats only, so no app's instructions reach those
  agents, and they can change the registry as the person can. The wall keeps an app's chat from reaching out, not the rest of the
  machine from reaching in.

A dev install whose source folder is missing when the chat starts has no bind (bwrap cannot bind what is not there); the hook
still refuses its being made.

## Examples

- [`apps/mr-review`](apps/mr-review): review without edits, a comment in the MR only after the person confirms; `evals/no-edits.json` is a check to copy for `harnessly app eval`.
- [`apps/cashback`](apps/cashback): a pane and full-screen view with the month's picks in the chat's storage; the agent reads and writes them with `app.data.*`, shows the view with `app.open` and confirms changes with `app.ask`. In a site it adds a card template whose elements carry `data-hl-field`, and an inspector view that opens on a click on one of them and writes the value back with `hl.sinopia.setFields`.
- [`apps/moodboard`](apps/moodboard): a Sinopia document type of its own (`acme.moodboard`) built from the SDK alone: its editor view, a JSON Schema and a migration between two versions of the format.
- [`apps/standup`](apps/standup): a `trusted` app with a local stdio server, the workspace's files as the shared store, a pane and full-screen view, a card from a tool's result, and the worked example of [the guide](build-apps/08-worked-example.md).
- [`apps/deck-lint`](apps/deck-lint): «Валидатор презентаций», on in presentations only (`sinopia.slides`): word and font-size rules that mark the slides' blocks and stop a share while one is an error, a panel with the longest texts, a selection item that asks the agent to shorten the text.

## State

What ships now: the manifest and its check, the registry, `harnessly app`, the per-chat list of apps
(`conversation.apps`, with the page's switch), what a Claude Code chat's agent gets from them (plugin, instructions, tool
policy, the `harnessly-app` tools), views in the pane, as cards in the chat and as the app's own page with the chat
beside it, the SDK, the bridge, the storage, the host calls and the inspector, and in Sinopia the Document API, panels, the
selection bar, checks with marks and the share gate, and bindings, and sharing: immutable versions on dev, beta and
stable with rollback and evals as the gate to stable, the team's catalog with installs for the team, standalone apps by
link, and `harnessly app eval`, and the files of the workspace within an app's masks, the document types an app adds, and
the public catalog. The process comes next.

Not built yet: the hub's own catalog and storage of packages (the team's and the public list come from the platform's relay,
the `apps.team` op and its public counterpart, and are not sent by this repo's hub), the wall for a Codex agent (apps are on in
Claude Code chats only), `app fork` and `app export`, `app eval init`, evals on
Codex, and the check of a published version against the requests the evals made.
