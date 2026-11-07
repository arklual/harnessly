# Apps: reference

<!-- Generated from internal/apps/surface.go and contributions.go (UPDATE_SURFACE=1 go test ./internal/apps -run TestSurfaceFilesAreCurrent); do not edit. -->

Everything an app can use, from the one table the manifest check and the bridge read. An agent gets the same with `harnessly app catalog` or the host tool `catalog`.

## Targets

| Target | What it is |
| --- | --- |
| `chat` | The chat: the agent profile, /-commands of the palette, cards and widgets in the feed. |
| `pane` | A tab of the right pane beside the chat. |
| `app` | A full screen of its own, opened from the apps page or by link. |
| `settings` | A page in the settings. |
| `onboarding` | A step of the first run. |
| `object-action` | A button at an object (a machine, a chat) that opens the view. |
| `hub-settings` | A page in the settings of a Hub. |
| `branding` | The product's name and accent; declared values, no view. |
| `sinopia.<kind>` | The editor of one Sinopia document type: panels, selection items, checks, bindings. An alias of the screen of `harnessly.sinopia`. |
| `sinopia.*` | The editor of every Sinopia document type. An alias of the screen of `harnessly.sinopia`. |

## Contributions

The host's own kinds, then the points the builtin apps declare.

| Kind | Declared in | Shows in | What it is |
| --- | --- | --- | --- |
| `view` | `views[]` | `pane`, `app`, `chat`, `settings`, `onboarding`, `object-action`, `hub-settings`, `sinopia.<kind>` | A page of the app in a frame. |
| `chat.command` | `chat.commands[]` | `chat` | A /-command of the chat's palette. |
| `chat.card` | `chat.cards[]` | `chat` | A view of the app the chat's feed shows as a card. |
| `chat.starter` | `chat.starters[]` | `chat` | A line an empty chat offers under its field; a click puts it into the field. |
| `chat.button` | `chat.buttons[]` | `chat` | A button at the chat's field: runs one of the app's commands or opens its view in the pane. |
| `chat.link` | `chat.links[]` | `chat` | A kind of address the app shows a card for under a message that has such a link. |
| `chat.mention` | `chat.mentions` | `chat` | What «@» in the chat's field finds in the app: a tool of its MCP server, asked with what is typed. |
| `agent` | `agent` | `chat`, `pane`, `sinopia.<kind>` | The agent profile: instructions, skills, MCP servers, tool rules, validators. |
| `branding` | `branding` | `branding` | The product's name and accent. |
| `sinopia.kind` | `sinopia.kinds[]` | `sinopia.<kind>` | A Sinopia document type of the app's own. A point of `harnessly.sinopia`. |
| `sinopia.panel` | `sinopia.panels[]` | `sinopia.<kind>` | A view to the right of the editor. A point of `harnessly.sinopia`. |
| `sinopia.selection` | `sinopia.selection[]` | `sinopia.<kind>` | An item of the selection menu. A point of `harnessly.sinopia`. |
| `sinopia.check` | `sinopia.checks[]` | `sinopia.<kind>` | A check of the document. A point of `harnessly.sinopia`. |
| `sinopia.export` | `sinopia.export[]` | `sinopia.<kind>` | An export entry of the document. A point of `harnessly.sinopia`. |
| `sinopia.import` | `sinopia.import[]` | `sinopia.<kind>` | An import entry of the document. A point of `harnessly.sinopia`. |

## Builtin apps

Apps of the host's own: they run in the host's page and reach the host through the same bridge, with rights from the same list. Their points take contributions from any app.

| App | Rights | Points | Aliases |
| --- | --- | --- | --- |
| `harnessly.sinopia` (Sinopia) | `chat.prompt`, `tools.page`, `context.publish` | `sinopia.kind`, `sinopia.panel`, `sinopia.selection`, `sinopia.check`, `sinopia.export`, `sinopia.import` | `sinopia.<kind>`, `sinopia.*` |

## Permissions

Every right has a class of risk. The class forbids nothing: it orders the install screen, from critical down, and a high or critical right is shown with what it can do to the person and journals what it does.

| Right | Class | What it opens | Methods |
| --- | --- | --- | --- |
| `chat.prompt` | medium | Send a message to the agent of the chat (hl.chat.send). | `ui/message` |
| `chat.read` | medium | Read the chat's messages. | `harnessly/chat.read` |
| `chat.card` | medium | Put a card of the app in the chat's feed. | `harnessly/chat.card` |
| `agents.read` | medium | List the agents of the workspace. | `harnessly/agents.list` |
| `pane.open` | low | Open a view of the app in the right pane. | `harnessly/pane.open` |
| `storage` | low | Keep data of the app per chat, per document and per person. | `harnessly/storage.delete`, `harnessly/storage.get`, `harnessly/storage.keys`, `harnessly/storage.set` |
| `storage.space.write` | medium | Write the storage area shared by the whole space. | — |
| `files.read:<mask>` | medium | Read workspace files matching the mask. | `harnessly/files.list`, `harnessly/files.read`, `harnessly/files.search` |
| `files.write:<mask>` | medium | Write workspace files matching the mask. | `harnessly/files.write` |
| `files.watch` | medium | Hear which files under the files.read masks change (hl.files.watch); the names, not the contents. | `harnessly/files.unwatch`, `harnessly/files.watch` |
| `git.read` | medium | See git status, diffs and history of the chat's folder (secret files left out). | `harnessly/git.diff`, `harnessly/git.log`, `harnessly/git.status` |
| `events:<kind>` | medium | Hear what happens in the chat: turn (the agent's turn ended), tool (the agent called a tool), message (a message came). | `harnessly/events.subscribe` |
| `secrets:<name>` | high | Have the machine put a secret the person typed into the app's server calls. | — |
| `secrets.write[:<name>]` | medium, high with <name> | Put a value into the safe, never read it back (hl.secrets.put). Alone: the app's own secrets, those it holds secrets:<name> for; with a name (secrets.write:GITHUB_TOKEN): that secret of the person's safe, the one the agent uses. | `harnessly/secrets.put` |
| `terminal.run[:<command>]` | high | Run a command in the chat's folder inside the agent's sandbox and get its exit code and output (hl.terminal.run). Alone: any command, through the shell; with a mask (terminal.run:npm test*, * is any run of characters but an option): only the commands it matches, without the shell; an option (a word starting with -) is matched only where the mask writes it. | `harnessly/terminal.run` |
| `process[:<command>]` | high | Start and stop long commands in the chat's folder inside the agent's sandbox (a dev server, a watcher), read their output, and open a local port in the right pane's browser (hl.process, hl.preview.open). Alone: any command, through the shell; with a mask (process:npm run dev*): only the commands it matches, as terminal.run's masks. | `harnessly/preview.open`, `harnessly/process.list`, `harnessly/process.output`, `harnessly/process.start`, `harnessly/process.stop` |
| `terminal.pty` | critical | Open a shell on a terminal of its own in the chat's folder inside the agent's sandbox, type into it and get what it prints (hl.terminal.open): a whole terminal, as the right pane's own. 4 per app in a chat. | `harnessly/terminal.close`, `harnessly/terminal.open`, `harnessly/terminal.resize`, `harnessly/terminal.write` |
| `terminal.read` | high | Read what the chat's terminals show: the agent's and the chat's own shells, as text (hl.terminal.read). | `harnessly/terminal.read` |
| `browser.read` | high | See the agent's browser of the chat: its tabs, a page's text and elements, a picture of it (hl.browser.read). | `harnessly/browser.read` |
| `browser.drive` | high | Act in the agent's browser of the chat: open and close tabs, go to an address, click, type, press keys, pick options (hl.browser.drive). Passwords, files and scripts stay out. | `harnessly/browser.drive` |
| `hooks.agent` | critical | Hear each tool call of the chat's agent before it runs, and stop it or add context for the agent (hl.hooks.on). The call is never changed. A view that does not answer in 5 s lets it through, and the journal says so. | `harnessly/hooks.result`, `harnessly/hooks.subscribe` |
| `hooks.strict` | critical | Strict mode of hooks.agent: a call the app's view did not answer in time, or with no view open, is stopped instead of let through. | — |
| `background` | critical | Run a view of the app hidden in the page while the app is on in a chat, without the person opening it: its hooks and events work in every such chat. | — |
| `notify` | low | Show the person a notification. | `harnessly/notify` |
| `tools.page` | medium | Offer tools from the view's page that the agent of the chat calls (hl.tools.register, navigator.modelContext.registerTool). | `harnessly/tools.register`, `harnessly/tools.result` |
| `context.publish` | medium | Publish what the view shows for the agent of the chat to read (hl.publish). | `harnessly/context.publish` |
| `sinopia.read` | medium | Read the Sinopia document the view is shown in. | `harnessly/sinopia.document`, `harnessly/sinopia.fields`, `harnessly/sinopia.mark`, `harnessly/sinopia.node`, `harnessly/sinopia.query`, `harnessly/sinopia.selection` |
| `sinopia.write` | medium | Change the Sinopia document: patches, fields, new boards. | `harnessly/sinopia.boards.add`, `harnessly/sinopia.patch`, `harnessly/sinopia.setFields` |
| `sinopia.comment` | low | Comment on the Sinopia document. | `harnessly/sinopia.comment` |
| `sinopia.kind` | low | Add Sinopia document types of the app's own. | — |
| `sinopia.export` | low | Render the document to a file. Refused for now: not supported yet. | `harnessly/sinopia.render` |
| `ai` | high | Call a model on the person's behalf, within the app's budget of the day (manifest `budget.usdPerDay`, $0.50 by default, $5 at most). | `harnessly/ai.complete` |
| `agents.run` | high | Start a task in a new chat under the app's chat, within the app's budget of the day (manifest `budget.runsPerDay`, 3 by default, 20 at most). | `harnessly/agents.run` |
| `users.read` | medium | Read the people of the space. Refused for now: not supported yet. | — |

## Bridge methods

| Method | Right | SDK | What it does |
| --- | --- | --- | --- |
| `ui/initialize` | — | — | The view's handshake: the host answers its context. |
| `ui/notifications/initialized` | — | — | The view says it is ready. |
| `harnessly/protocol` | — | `connect({protocol})` | The view names the version of the bridge's protocol it is written for (above 1); the host answers the one it speaks with it. |
| `harnessly/debug.log` | — | — | An error or console.error of the view (the SDK sends it), for the debug section of the app's inspector. |
| `ui/notifications/size-changed` | — | `hl.resize(height)` | The view asks for a height. |
| `ui/open-link` | — | — | Open an address in a new tab. |
| `tools/call` | — | `hl.tools.call(name, args)` | Call a tool of the app's own MCP server. |
| `ui/message` | `chat.prompt` | `hl.chat.send(text)` | A message to the agent of this chat, signed with the app's name. |
| `harnessly/chat.read` | `chat.read` | `hl.chat.read({last})` | The chat's last messages. |
| `harnessly/chat.card` | `chat.card` | `hl.chat.card(view, data)` | A card of the app in the feed. |
| `harnessly/pane.open` | `pane.open` | `hl.pane.open(target)` | Open a view of the app in the right pane. |
| `harnessly/agents.list` | `agents.read` | `hl.agents.list()` | The agents of the workspace. |
| `harnessly/agents.run` | `agents.run` | `hl.agents.run(prompt, {title})` | Start a task in a new chat under this one: {id}. The person sees it in the sidebar; counts against budget.runsPerDay. |
| `harnessly/ai.complete` | `ai` | `hl.ai.complete(prompt, {system, maxTokens, model})` | One answer of a model, no tools: {text, usd, spentToday, budget}. model is fast (default) or smart; refused as over_budget when the day's budget is spent. |
| `harnessly/secrets.put` | `secrets.write` | `hl.secrets.put(name, value, {shared})` | Put a value into the safe: {created}. The app's own secret by default; shared: true puts the person's secret of that name (secrets.write:<name>). The value is never given back. |
| `harnessly/storage.get` | `storage` | `hl.storage.<scope>.get(key)` | Read a key of the app's storage. |
| `harnessly/storage.set` | `storage` | `hl.storage.<scope>.set(key, value, {ifVersion})` | Write a key; ifVersion writes only over that version. |
| `harnessly/storage.delete` | `storage` | `hl.storage.<scope>.delete(key)` | Remove a key. |
| `harnessly/storage.keys` | `storage` | `hl.storage.<scope>.keys()` | The keys of an area. |
| `harnessly/settings.get` | — | `hl.settings.get()` | The app's settings as the person saved them. |
| `harnessly/notify` | `notify` | `hl.notify(text)` | A notification to the person. |
| `harnessly/files.read` | `files.read` | `hl.files.read(path)` | Read a workspace file inside the app's mask. |
| `harnessly/files.write` | `files.write` | `hl.files.write(path, text)` | Write a workspace file inside the app's mask. |
| `harnessly/files.list` | `files.read` | `hl.files.list(folder)` | The files and folders of a folder that the app's masks reach. |
| `harnessly/files.search` | `files.read` | `hl.files.search(text, {glob, case, limit})` | Lines that hold a text in the files the app may read; with a glob alone, the files by name. |
| `harnessly/files.watch` | `files.watch` | `hl.files.watch(onChange)` | Start hearing which files under the masks change: harnessly/notifications/files-changed {paths}. |
| `harnessly/files.unwatch` | `files.watch` | `the function hl.files.watch returns` | Stop hearing file changes. |
| `harnessly/files.pick` | — | `hl.files.pick({accept})` | The person picks a file of the chat's folder in the host's dialog; the view gets its path and text once, no mask needed. null when the person closes it. |
| `harnessly/git.status` | `git.read` | `hl.git.status()` | The branch and the changed files of the chat's folder. |
| `harnessly/git.diff` | `git.read` | `hl.git.diff({path, staged, ref})` | The unified diff of the chat's folder or a path in it. |
| `harnessly/git.log` | `git.read` | `hl.git.log({limit, path})` | The last commits. |
| `harnessly/terminal.run` | `terminal.run` | `hl.terminal.run(command, {cwd, timeout})` | Run a command in the chat's folder (or cwd in it) inside the agent's sandbox: {code, output, cut, timedOut, ms}. Timeout in seconds, 120 by default, 600 at most; 1 MiB of output. |
| `harnessly/process.start` | `process` | `hl.process.start(command, {cwd})` | Start a long command in the chat's folder inside the agent's sandbox: {id, command, running}. It runs until stopped, until the app is turned off in the chat, or until the runtime stops; 4 per app in a chat. |
| `harnessly/process.stop` | `process` | `hl.process.stop(id)` | Stop a process the app started (SIGTERM, then SIGKILL after 3 s). |
| `harnessly/process.list` | `process` | `hl.process.list()` | The app's processes in this chat, running and recently ended, with their exit codes. |
| `harnessly/process.output` | `process` | `hl.process.output(id, {since})` | A process's output from an offset: {text, next, running, code, lost}; the machine keeps its last 256 KiB. |
| `harnessly/preview.open` | `process` | `hl.preview.open(port, {path})` | Open http://localhost:<port><path> in the right pane's browser, where the person sees the app's dev server. |
| `harnessly/hooks.subscribe` | `hooks.agent` | `hl.hooks.on('tool', fn)` | Hear the agent's tool calls before they run: harnessly/notifications/hook {id, tool, input, cut} (cut: the input was too big to send, 60 KiB); fn answers {decision: 'deny', reason} to stop it, {context} to add a line for the agent, or nothing to let it through. |
| `harnessly/hooks.result` | `hooks.agent` | — | The view's word on a call it heard (the SDK sends it): {id, decision, reason, context}. |
| `harnessly/browser.read` | `browser.read` | `hl.browser.read(tool, args)` | A look at the agent's browser of the chat: tool is tabs, snapshot, screenshot or wait_for (the chrome_* tools' arguments); the answer is the tool's content, the page's text marked untrusted. |
| `harnessly/browser.drive` | `browser.drive` | `hl.browser.drive(tool, args)` | An action in the agent's browser of the chat: tool is open, navigate, close_tab, resize, click, hover, type, press_key, select or dialog; refs come from a snapshot. No password, file or script. |
| `harnessly/terminal.open` | `terminal.pty` | `hl.terminal.open({cols, rows, cwd})` | Open a shell on a terminal of its own in the chat's folder (or cwd in it) inside the agent's sandbox: {id}. What it prints comes as harnessly/notifications/terminal-output {id, data} (bytes), its end as harnessly/notifications/terminal-exit {id, code}. |
| `harnessly/terminal.write` | `terminal.pty` | `terminal.write(data)` | Type into an open terminal: a string or bytes, 64 KiB at most a call. Not counted in the 60 calls a second. |
| `harnessly/terminal.resize` | `terminal.pty` | `terminal.resize(cols, rows)` | Set an open terminal's size. |
| `harnessly/terminal.close` | `terminal.pty` | `terminal.close()` | End the shell of a terminal and everything it started. |
| `harnessly/terminal.read` | `terminal.read` | `hl.terminal.read({terminal, lines})` | What the chat's terminals show, as text: {terminals: [{id, text}]}; id "agent" is the agent's own, the others the chat's shells; lines of history, 200 by default, 2000 at most. |
| `harnessly/events.subscribe` | `events` | `hl.events.on(kind, fn)` | Hear the chat's events of the kinds the app holds events:<kind> for: harnessly/notifications/event {kind, at, …}. |
| `harnessly/tools.register` | `tools.page` | `hl.tools.register(tool, run)` | Offer the view's tools to the agent; the whole list each time. |
| `harnessly/tools.result` | `tools.page` | — | The view's answer to an agent's call of its tool (the SDK sends it). |
| `harnessly/context.publish` | `context.publish` | `hl.publish(data)` | What the view shows, for the agent to read with page_tools. |
| `harnessly/ui.toast` | — | `hl.ui.toast(text, {tone, description})` | A short message at the bottom of the page; tone info, success, warning or error, and a second line. |
| `harnessly/ui.confirm` | — | `hl.ui.confirm(text)` | A yes/no question in the host's own dialog. |
| `harnessly/keys.on` | — | `hl.keys.on(scope, handlers)` | Run the host's shortcut commands of a screen (deck, video, site, pictures, files) while the view is up, on the keys the person set for them: harnessly/notifications/key {command, chord}. |
| `harnessly/keys.off` | — | `the function hl.keys.on returns` | Stop running a screen's shortcut commands. |
| `harnessly/keys.press` | — | `hl.keys.press(chord, {typing})` | A key pressed inside the view, run as the host's shortcut would run it; only while the view has the focus. Answers {ran}. |
| `harnessly/keys.hints` | — | `hl.keys.hints(commands)` | The keys the person has for the host's commands, as the host writes them: {hints: {command: label or null}}. |
| `harnessly/slot.done` | — | `hl.slot.done()` | A slot view (an onboarding step) says it is finished. |
| `harnessly/sinopia.document` | `sinopia.read` | `hl.sinopia.document()` | The document the view is shown in. |
| `harnessly/sinopia.selection` | `sinopia.read` | `hl.sinopia.selection()` | What the person selected. |
| `harnessly/sinopia.node` | `sinopia.read` | `hl.sinopia.node(id)` | One node of the document. |
| `harnessly/sinopia.query` | `sinopia.read` | `hl.sinopia.query(selector, options)` | Nodes matching a selector. |
| `harnessly/sinopia.fields` | `sinopia.read` | `hl.sinopia.fields()` | The values of the bound fields. |
| `harnessly/sinopia.mark` | `sinopia.read` | `hl.sinopia.mark(nodes, options)` | Highlight nodes in the editor. |
| `harnessly/sinopia.patch` | `sinopia.write` | `hl.sinopia.patch(ops)` | Change the document. |
| `harnessly/sinopia.setFields` | `sinopia.write` | `hl.sinopia.setFields(patch)` | Write bound fields back. |
| `harnessly/sinopia.boards.add` | `sinopia.write` | `hl.sinopia.boards.add(options)` | Add a board to the document. |
| `harnessly/sinopia.render` | `sinopia.export` | `hl.sinopia.render(options)` | Render the document. |
| `harnessly/sinopia.comment` | `sinopia.comment` | `hl.sinopia.comment(node, text)` | Comment on a node of the document. |

## Host tools of the agent

The server `harnessly-app`, in a chat with apps on. The last ones are the builtin apps' backend tools.

| Tool | What it does |
| --- | --- |
| `open` | Open an app's view in the person's right pane. |
| `data_get` | Read what the app keeps for this chat under a key. |
| `data_set` | Write what the app keeps for this chat under a key. |
| `settings_get` | Read the app's settings. |
| `ask` | Put a choice to the person in the app's view and wait for the answer. |
| `widget_show` | Show an app's chat view in the feed with the data given. |
| `widget_render` | Draw a widget written on the spot in the feed. |
| `page_tools` | The tools and the context the open views of the chat's apps offer from the page. |
| `page_call` | Call a tool an open view offers; the view runs it in the person's page. |
| `catalog` | The places, rights, methods and contribution kinds an app can use. |
| `sinopia_document` | The Sinopia document: its kind, title, boards and revision. Of of `harnessly.sinopia`. |
| `sinopia_selection` | The elements the person picked in the editor now. Of of `harnessly.sinopia`. |
| `sinopia_node` | One element of a board by its id (<page id>:<number>). Of of `harnessly.sinopia`. |
| `sinopia_query` | Elements that match a selector (type, #id, .class, [attr], space, >), in one board or all. Of of `harnessly.sinopia`. |
| `sinopia_fields` | The data fields the app binds to the board's elements, with their values. Of of `harnessly.sinopia`. |
| `sinopia_set_fields` | Set the values of bound fields; the board is rewritten, as one history entry. A value outside the app's schema is refused. Of of `harnessly.sinopia`. |
| `sinopia_patch` | Change the board: ops setText, setStyle, setAttr, insert, remove, setField, all or none, as one history entry. A board another chat holds is refused. Of of `harnessly.sinopia`. |
| `sinopia_mark` | Put a note on elements of the board, which the person sees on it. Of of `harnessly.sinopia`. |
| `sinopia_comment` | Comment an element: the comment shows on the board as a mark. Of of `harnessly.sinopia`. |
| `sinopia_render` | Render a board as PNG, PDF or HTML. Not available yet. Of of `harnessly.sinopia`. |
| `sinopia_boards_add` | Add a board from one of the app's templates, with data for its bound fields. Of of `harnessly.sinopia`. |
