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

Pages: `overview`, `first-plugin`, `views`, `backend`, `sinopia`, `agents`, `debugging`, `sharing`, `worked-example`, `reference`.

## The loop

```sh
harnessly app new acme.thing --template chat       # or pane-chat, with a view
harnessly app validate ./thing --strict
harnessly app dev ./thing                          # installs the folder itself
harnessly open app:acme.thing                      # shows its view to the person
harnessly app eval ./thing                         # scenarios from evals/ on a real agent
```

After you change `harnessly/app.json`, run `app dev` again. Files in `ui/` take effect when the view reloads. Instructions, skills and `.mcp.json` reach the agent at its next launch.

## Four things that catch everyone

1. **Work in a chat with no apps on.** In a chat with any app on, the source of every installed app is read-only, and `app dev`, `install` and `enable` are refused. If
   `HARNESSLY_CLI_TOKEN_FILE` is set in your environment, you are in one: say so to the person.
2. **Views are classic scripts** in a sandbox with an opaque origin: no modules, no `localStorage`, no `fetch` of your own files. Load `/__hl/app-sdk.js`, then use `harnessly.connect()`.
3. **There is no server run by the host.** `hl.tools.call` reaches only a public `https://` MCP server. A local `command` server needs `trust: trusted`, serves the agent only, and shares data with a view through workspace files. Read the page `backend` before choosing.
4. **`allow` rules grant nothing**, `Edit` does not cover `Write`, and manifest `secrets` and `settings` do nothing yet.

Say what you checked and what you could not. A view's look needs a browser: ask the person to look, or use a guest link with Playwright (page `debugging`).
