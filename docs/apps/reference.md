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
| `sinopia.<kind>` | The editor of one Sinopia document type: panels, selection items, checks, bindings. |
| `sinopia.*` | The editor of every Sinopia document type. |

## Contributions

| Kind | Declared in | Shows in | What it is |
| --- | --- | --- | --- |
| `view` | `views[]` | `pane`, `app`, `chat`, `settings`, `onboarding`, `object-action`, `hub-settings`, `sinopia.<kind>` | A page of the app in a frame. |
| `chat.command` | `chat.commands[]` | `chat` | A /-command of the chat's palette. |
| `chat.card` | `chat.cards[]` | `chat` | A view of the app the chat's feed shows as a card. |
| `agent` | `agent` | `chat`, `pane`, `sinopia.<kind>` | The agent profile: instructions, skills, MCP servers, tool rules, validators. |
| `branding` | `branding` | `branding` | The product's name and accent. |
| `sinopia.kind` | `sinopia.kinds[]` | `sinopia.<kind>` | A Sinopia document type of the app's own. |
| `sinopia.panel` | `sinopia.panels[]` | `sinopia.<kind>` | A view to the right of the editor. |
| `sinopia.selection` | `sinopia.selection[]` | `sinopia.<kind>` | An item of the selection menu. |
| `sinopia.check` | `sinopia.checks[]` | `sinopia.<kind>` | A check of the document. |
| `sinopia.export` | `sinopia.export[]` | `sinopia.<kind>` | An export entry of the document. |
| `sinopia.import` | `sinopia.import[]` | `sinopia.<kind>` | An import entry of the document. |

## Permissions

| Right | What it opens | Methods |
| --- | --- | --- |
| `chat.prompt` | Send a message to the agent of the chat (hl.chat.send). | `ui/message` |
| `chat.read` | Read the chat's messages. | `harnessly/chat.read` |
| `chat.card` | Put a card of the app in the chat's feed. | `harnessly/chat.card` |
| `agents.read` | List the agents of the workspace. | `harnessly/agents.list` |
| `pane.open` | Open a view of the app in the right pane. | `harnessly/pane.open` |
| `storage` | Keep data of the app per chat, per document and per person. | `harnessly/storage.delete`, `harnessly/storage.get`, `harnessly/storage.keys`, `harnessly/storage.set` |
| `storage.space.write` | Write the storage area shared by the whole space. | — |
| `files.read:<mask>` | Read workspace files matching the mask. | `harnessly/files.read` |
| `files.write:<mask>` | Write workspace files matching the mask. | `harnessly/files.write` |
| `secrets:<name>` | Have the machine put a secret the person typed into the app's server calls. | — |
| `notify` | Show the person a notification. | `harnessly/notify` |
| `tools.page` | Offer tools from the view's page that the agent of the chat calls (hl.tools.register, navigator.modelContext.registerTool). | `harnessly/tools.register`, `harnessly/tools.result` |
| `context.publish` | Publish what the view shows for the agent of the chat to read (hl.publish). | `harnessly/context.publish` |
| `sinopia.read` | Read the Sinopia document the view is shown in. | `harnessly/sinopia.document`, `harnessly/sinopia.fields`, `harnessly/sinopia.mark`, `harnessly/sinopia.node`, `harnessly/sinopia.query`, `harnessly/sinopia.selection` |
| `sinopia.write` | Change the Sinopia document: patches, fields, new boards. | `harnessly/sinopia.boards.add`, `harnessly/sinopia.patch`, `harnessly/sinopia.setFields` |
| `sinopia.comment` | Comment on the Sinopia document. | `harnessly/sinopia.comment` |
| `sinopia.kind` | Add Sinopia document types of the app's own. | — |
| `sinopia.export` | Render the document to a file. Refused for now: not supported yet. | `harnessly/sinopia.render` |
| `ai` | Call a model on the person's behalf. Refused for now: not supported yet. | — |
| `users.read` | Read the people of the space. Refused for now: not supported yet. | — |

## Bridge methods

| Method | Right | SDK | What it does |
| --- | --- | --- | --- |
| `ui/initialize` | — | — | The view's handshake: the host answers its context. |
| `ui/notifications/initialized` | — | — | The view says it is ready. |
| `harnessly/debug.log` | — | — | An error or console.error of the view (the SDK sends it), for the debug section of the app's inspector. |
| `ui/notifications/size-changed` | — | `hl.resize(height)` | The view asks for a height. |
| `ui/open-link` | — | — | Open an address in a new tab. |
| `tools/call` | — | `hl.tools.call(name, args)` | Call a tool of the app's own MCP server. |
| `ui/message` | `chat.prompt` | `hl.chat.send(text)` | A message to the agent of this chat, signed with the app's name. |
| `harnessly/chat.read` | `chat.read` | `hl.chat.read({last})` | The chat's last messages. |
| `harnessly/chat.card` | `chat.card` | `hl.chat.card(view, data)` | A card of the app in the feed. |
| `harnessly/pane.open` | `pane.open` | `hl.pane.open(target)` | Open a view of the app in the right pane. |
| `harnessly/agents.list` | `agents.read` | `hl.agents.list()` | The agents of the workspace. |
| `harnessly/storage.get` | `storage` | `hl.storage.<scope>.get(key)` | Read a key of the app's storage. |
| `harnessly/storage.set` | `storage` | `hl.storage.<scope>.set(key, value, {ifVersion})` | Write a key; ifVersion writes only over that version. |
| `harnessly/storage.delete` | `storage` | `hl.storage.<scope>.delete(key)` | Remove a key. |
| `harnessly/storage.keys` | `storage` | `hl.storage.<scope>.keys()` | The keys of an area. |
| `harnessly/settings.get` | — | `hl.settings.get()` | The app's settings as the person saved them. |
| `harnessly/notify` | `notify` | `hl.notify(text)` | A notification to the person. |
| `harnessly/files.read` | `files.read` | `hl.files.read(path)` | Read a workspace file inside the app's mask. |
| `harnessly/files.write` | `files.write` | `hl.files.write(path, text)` | Write a workspace file inside the app's mask. |
| `harnessly/tools.register` | `tools.page` | `hl.tools.register(tool, run)` | Offer the view's tools to the agent; the whole list each time. |
| `harnessly/tools.result` | `tools.page` | — | The view's answer to an agent's call of its tool (the SDK sends it). |
| `harnessly/context.publish` | `context.publish` | `hl.publish(data)` | What the view shows, for the agent to read with page_tools. |
| `harnessly/ui.toast` | — | `hl.ui.toast(text)` | A short message at the bottom of the page. |
| `harnessly/ui.confirm` | — | `hl.ui.confirm(text)` | A yes/no question in the host's own dialog. |
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

The server `harnessly-app`, in a chat with apps on.

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
| `sinopia_*` | Read and change the Sinopia document beside the chat. |
