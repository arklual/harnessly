# Working in harnessly as an agent

You run in a tab of harnessly: a person works in a chat with you, on a real machine, in a project folder. A right pane
beside the conversation shows what you make. Other tabs run terminals and other agents. This section is written for
you, the agent; the person reads it too, to know what you know.

## What you are given

- **The workspace prompt**: who the person is talking to, the workspace id, the `harnessly` command, how your answers
  render. It comes with every chat.
- **The `harnessly` command** on your PATH. It finds the runtime by itself; there is nothing to log in to.
- **Skills**: documents and boards, the workspace, writing apps; the ones the person turned on in the settings.
- **The person's connections**: their tracker, wiki and code host through `harnessly issues`, `harnessly pages`, and
  the MCP servers they connected, already signed in.
- **The safe**: secrets by name, never by value (`harnessly secret`).

## The command, in short

```bash
harnessly docs search <words>        # this documentation and the team's: read before guessing
harnessly open <link|file|port>      # show it to the person in the right pane
harnessly deliver <file>             # hand a file the person asked for: a card in the chat
harnessly tab create -t codex-cli --prompt "<the whole task>"   # start another agent
harnessly peers                      # other agents' chats you may write to
harnessly secret run --env VAR=NAME -- cmd   # run a command with a secret from the safe
harnessly issues --mine --open       # the person's tasks in their tracker
harnessly mrs HRN-796                # merge requests that name a task, from the person's GitLab
harnessly --help                     # everything, with examples
```

## Rules of the house

1. **Show, do not describe.** A document you published, a board you drew, a dev server you started: open it in the
   pane (`harnessly open`) or name its address in the answer, which opens it once.
2. **Deliver only what was asked for.** `harnessly deliver` is for a file the person wants to receive, not for every
   file you touched.
3. **Secrets go through the safe.** Never ask for a token in the chat, never print one, never write one into a file.
   `harnessly secret ask NAME --why "…"` gives the person a card to put it in.
4. **Start another agent the platform's way.** `harnessly tab create --prompt` gives it the hooks and the prompt; a
   `claude` typed into a shell does not.
5. **Ask the docs first.** How harnessly works and how this team has set things up is written down: `harnessly docs
   search`. When you found out something the docs lacked, offer to [add it](docs.md).

## How your answers render

A fenced block marked `mermaid` becomes a diagram; one marked `html` becomes a sandboxed page with a toggle to its
source. A link to a document or a board alone on its line is a card. A task key, a merge request or a site in the
text is a chip the person clicks to open in the pane.
