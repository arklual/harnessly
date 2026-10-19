# Slices

A **slice** is a named group of the runtime's routes that moves to Go as a whole. In [gate
mode](architecture.md#gate-and-standalone) each slice is switched on per machine; in standalone mode all of them are on
and no shadow runs.

## Switching slices

Which slices are on is read from `state-mode.json` in the data directory (`AGENT_DATA_DIR`, default `~/.harnessly`):

```json
{"goSlices": ["fsread", "machines-shadow", "terminal"]}
```

The machine agent writes the file and the gate re-reads it every second, so a slice is switched on or off without a
restart. The sockets a slice takes over (status, sync, terminal) are handed over on the switch.

## The shadow

A slice can first run as `<slice>-shadow`. The previous runtime still answers; the Go answer is computed next to it
(for read-only routes only) and every difference is logged as `shadow answer differs`, with the place where the two
answers first part. JSON is compared as data; files by status, headers and bytes. The counts go to the periodic
`shadow answers compared` log line.

Every `RUNTIME_GATE_COUNT_SECONDS` (default 600) the gate also logs each path still handed to the previous runtime,
with its count and the average and longest time it took, the gate's own hop included. Together the two logs say when a
slice is safe to switch on and what is left to move.

The usual order on a machine is shadow, then the slice, then standalone once every slice has been on for a while.

## The slices

| Slice | What Go answers |
| --- | --- |
| `core` | the runtime core: the tab state keeper, the layout and workspace stores, the status and sync sockets, prompt delivery, the agents' hooks, web push sending and the idle sleep of agents. Two keepers of one machine's tabs would fight over its files, so it runs only while the slice is on |
| `core-shadow` | (while `core` is off) a Go keeper next to the previous runtime's, reading the same layouts and tmux and fed copies of the same hooks, writing and typing nothing; it logs where its tab states and the previous runtime's status stream stay apart |
| `state` | the settings files (the cluster serves them from its database first; this runtime is their fallback and writer), the automation routes, the little routes nothing else took, and the previous runtime's dead routes, answered as it answered them on a machine without pages |
| `terminal` | the terminal socket (`/api/terminal?session=`): a pty running `tmux attach`, in the binary protocol the page speaks |
| `fsread` | read-only answers about the machine and its files (GET only): peers, a tab's working folder, the agents in a tab, the conversations of a folder, the tools on the machine, slash commands and skills, launch choices, the editor's listing and files, changed files and their diffs, the file viewer with downloads and video ranges, the «@»-context picker, memory notes and uploaded attachments |
| `fswrite` | disk writes without agent state: uploads into a conversation and their clean-up, an editor save, syncing a tab's repository with its upstream, stopping a heavy process from the load panel; never shadowed |
| `machines` | the machine snapshot the machine agent polls (load, memory, provider limits with a forecast, release), the machine load panel, `/api/health`, `/api/manifest` and the web push subscription routes |
| `launch` | starting and resuming agents inside the running core: conversations, automation runs, document comments, eval runs, side chats, the Codex launch lines, and the resume of working agents at startup and after a tmux reset |
| `transcripts` | answers computed from the agents' conversation files: links and work paths of a conversation, message counts, the usage report, the statistics pages, and conversation snapshots with their read-only links |
| `timeline` | the timeline socket, the status line sink, a conversation's economics and subagents, and the turn checkpoints (a git snapshot of a tab's folder when an agent's turn ends) |
| `design` | the design library: listing, its loopback server, board edits, imports and new projects, checks and pictures in a headless browser, claims, history, and dialogs with agents |
| `preview` | the reverse proxy behind the pane's browser, with its DevTools record, the agents' DevTools routes and MCP server, and the ports and component lookup of the pane |
| `services` | routes that call external services with the person's tokens: GitLab pushes, merge requests and diffs, link previews, tracker cards and avatars, site icons, the organizer file |
| `voice` | dictation over `/api/voice` (ffmpeg to the machine's speech recognition service) and the in-tab engine's model files |

Some routes of a slice need the core and answer only while it runs; a slice whose configuration is missing (say, the
snapshot routes without the `RUNTIME_SHARE_*` variables) leaves those routes to the previous runtime.
