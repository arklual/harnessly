---
name: apps
description: Write, check, debug or publish a harnessly app or plugin: instructions and tool rules for an agent, a view in the right pane, a card in the chat, a full screen of its own, a backend through MCP, a panel, check or document type in Sinopia. Use when the person asks to make a plugin, an app or an extension for harnessly, when you see harnessly/app.json or a `harnessly app` command, or when an app view or tool misbehaves.
---

# Writing a harnessly app

An app is a folder: a Claude Code plugin plus the manifest `harnessly/app.json`. A plugin is an app with no views. Its guide is built into
the `harnessly` binary. Read it before you write anything, because it records what really works, and several manifest fields pass the
check and do nothing.

```sh
harnessly app docs                  # the pages of the guide; needs no runtime and no network
harnessly app docs overview         # what to build and which page to read next
harnessly app docs export ./hl-docs # the pages and the example packages in a folder, to search and copy
```

Pages: `overview`, `first-plugin`, `views`, `backend`, `sinopia`, `agents`, `debugging`, `sharing`, `worked-example`, `compatibility`,
`reference`.

## The loop

```sh
harnessly app new acme.thing --template chat       # or pane-chat, with a view
harnessly app validate ./thing --strict
harnessly app dev ./thing                          # installs the folder itself
harnessly open app:acme.thing                      # shows its view to the person
harnessly app eval ./thing                         # scenarios from evals/ on a real agent
```

After you change `harnessly/app.json`, run `app dev` again. Files in `ui/` take effect when the view reloads. Instructions, skills and `.mcp.json` reach the agent at its next launch.

## Rights, classes and consent

Every right in `permissions` has a class: low, medium, high or critical. The class forbids nothing. It orders the install screen
(dangerous first, under a warning that apps are not checked), and a high or critical right shows what it can do to the person. An update
that asks for more runs only after the person agrees to what it adds. Each call of a high or critical right goes into the app's journal
(its inspector), and one made while the agent uses the view asks the person first. Ask only for what the app uses. The class of each
right is in `harnessly app catalog` and the page `reference`.

**Trust circles** (`trust` in the manifest, required). `declarative`: text only (instructions, skills, commands, remote MCP servers,
static views); no scripts, so no `network.connect` or resources. `sandboxed`: its views run scripts in the sandboxed frame and reach the
host only through the rights below. `trusted`: also local scripts, validators and local `command` MCP servers; ask for it only when the
app cannot work without. Nothing here is a ban by origin: any app may ask for any right, and the circle is shown on the install screen.

**What the person sees.** One line that apps are not checked, then the rights in plain words, the most dangerous first, each high or
critical one with what it can do. An update shows only the new rights, marked «новое»; without a click the old version stays. The
agent cannot grant rights to itself: install, update and enable are the person's click in the host. The app's inspector keeps the
journal of high and critical calls, and one button turns the app off. Write the manifest so that this screen reads true: no right
you do not use, and a `description` that says why each dangerous one is needed.

## What a view can reach

| Need | SDK | Right (class) |
| --- | --- | --- |
| Workspace files | `hl.files.list/search/watch/pick`, `read/write` | `files.read:<mask>`, `files.write:<mask>`, `files.watch` (medium) |
| Git of the chat's folder | `hl.git.status/diff/log` | `git.read` (medium) |
| What happens in the chat | `hl.events.on('turn'\|'tool'\|'message', fn)` | `events:<kind>` (medium) |
| A command, a dev server | `hl.terminal.run`, `hl.process.start/stop/list/output`, `hl.preview.open(port)` | `terminal.run[:<mask>]`, `process[:<mask>]` (high) |
| A terminal of its own, the chat's terminals | `hl.terminal.open` (a PTY), `hl.terminal.read` | `terminal.pty` (critical), `terminal.read` (high) |
| The agent's browser | `hl.browser.read`, `hl.browser.drive` | `browser.read`, `browser.drive` (high) |
| Stop or add to the agent's tool calls | `hl.hooks.on('tool', fn)` | `hooks.agent`, `hooks.strict` (critical) |
| Run without the view open | a hidden view in every chat the app is on in | `background` (critical) |
| A model, an agent | `hl.ai.complete`, `hl.agents.run` | `ai`, `agents.run` (high), within the manifest `budget` of the day |
| The safe, write only | `hl.secrets.put(name, value, {shared})` | `secrets.write` (medium, its own keys); `secrets.write:<name>` (high, the person's secret) |
| A notice, the person's keys | `hl.ui.toast`, `hl.keys.on/press/hints` | none |

Commands and processes run in the agent's sandbox. A mask such as `terminal.run:npm test*` runs only the commands it matches, without
the shell. `hooks.agent` sees each tool call before it runs. A view that does not answer in 5 s lets the call through, and with
`hooks.strict` it stops it instead. `budget: {usdPerDay, runsPerDay}` defaults to $0.50 and 3 runs, at most $5 and 20; past it a call
rejects with `quota_exceeded` and the model is not asked.

The chat itself: `chat.starters` (lines in an empty chat), `chat.buttons` (icons at the field, a command or a view) and `chat.links` (a
card under a matching link). Search (⌘K) finds the app's commands and the app by name with nothing to declare. All of this is in the
page `views`.

**Compatibility.** Check what the host can do, not its version: `hl.has('files.read')` is false on an older host or without the right.
With `harnessly.connect({protocol: 2})`, a refusal comes back as a value (`hl.refused(answer)`) instead of an exception. Set `"harnessly":
">=…"` to the lowest version you need. The SDK only grows, so an app keeps working. See the page `compatibility`.

## Four things that catch everyone

1. **Work in a chat with no apps on.** In a chat with any app on, the source of every installed app is read-only, and `app dev`, `install` and `enable` are refused. If
   `HARNESSLY_CLI_TOKEN_FILE` is set in your environment, you are in one: say so to the person.
2. **Views are classic scripts** in a sandbox with an opaque origin: no modules, no `localStorage`, no `fetch` of your own files. Load `/__hl/app-sdk.js`, then use `harnessly.connect()`.
3. **There is no server run by the host.** `hl.tools.call` reaches only a public `https://` MCP server. A local `command` server needs `trust: trusted`, serves the agent only, and shares data with a view through workspace files. Read the page `backend` before choosing.
4. **`allow` rules grant nothing** and `Edit` does not cover `Write`. Manifest `secrets` are for the app's server: the person types
   them in «О приложении» and the machine fills `${secret:name}` into `.mcp.json` headers; nothing reads them back.

Say what you checked and what you could not. A view's look needs a browser: ask the person to look, or use a guest link with Playwright (page `debugging`).
