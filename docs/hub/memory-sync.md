# Your agent memory on all your machines

Claude Code keeps what it learns about a project in memory notes:
`<CLAUDE_CONFIG_DIR or ~/.claude>/projects/<entry>/memory/*.md`. Each machine keeps its own copy. With memory sync on,
a note written on one of your machines reaches your other machines through the hub. It reaches only machines that belong
to you, never anyone else's.

It is off by default. Both sides have to turn it on:

| Where | Setting |
| --- | --- |
| hub | `HUB_MEMORY_SYNC=on` serves `POST /agent/memory/pull` and `/agent/memory/put` (otherwise 404) |
| runtime | `RUNTIME_MEMORY_SYNC_URL=http://127.0.0.1:<agent port>/memory`, the memory relay of the machine's agent |
| agent with an observe link | `AGENT_HUB_MEMORY=on` (a machine linked by its own cluster takes no part without it) |

The agent's kill switch turns it off with everything else the hub reaches.

## What syncs

- **Only memory notes.** These are the `*.md` files of a project's `memory/` folder: letters, digits, `.`, `-` and `_`,
  no leading dot, at most 256 KiB each. Nothing else of the project or of Claude Code's settings syncs, and neither do
  transcripts.
- **Project identity.** A project is identified by its git remote (`origin`, else the first remote), normalized:
  `git@example.com:acme/app.git` and `https://example.com/acme/app` are both `git:example.com/acme/app`. The project's
  folder can differ from machine to machine.
  A folder without a remote does not sync, unless its person asks for it in one of two ways. A file
  `.harnessly-memory-project` in the folder, whose first line is a key (letters, digits, `.`, `_`, `-`, at most 64), names
  the project `dir:<key>`: folders of different machines with the same key are one project, and the key wins over a
  remote. With `RUNTIME_MEMORY_LOCAL_FOLDERS=on` on the runtime, a folder with neither is the project `dir:<its absolute
  path>`: machines that keep the folder at the same path share it (a path with a space in it does not sync).
- **Owner.** A note belongs to the person who owns the machine that sends it (the machine's `created_by`), and only
  while that person still has a role in the machine's space. The request is signed with the machine's key under its own
  domain (`harnessly-agent-memory-v1`) and names no person; a field that tries to is ignored. Two people with machines
  in the same space have separate memories. A revoked machine, a disabled one, or one whose person left the space gets
  403. Its former owner's memory is out of its reach.
- **Limits.** At most 1000 notes per project, 200 projects per pull, and 240 requests a minute per machine.

## When it syncs

- Before a Claude conversation starts, the runtime syncs that conversation's project, for 5 s at most. A slow or
  unreachable hub never stops the start: the conversation starts with the notes the machine has. A sync replaces a
  file in one rename, so a session never reads half a note.
- `harnessly memory sync` syncs the project of the current folder (`--cwd DIR` for another one, `--all` for every
  project synced before). Run it again after a failure; that is always safe. `harnessly memory` shows the last sync:
  what was pulled, pushed and deleted, plus conflicts and errors. The report is kept in
  `<data dir>/v2/memory-sync.json`. The same command works as a launcher hook (for example a Claude Code `SessionStart`
  hook running `harnessly memory sync`).

## How a sync decides

Each memory folder keeps `.harnessly-sync.json`: the project id and, for each note, the version it last saw (a hash of
the text). The hub writes a note only when it is still at the version the writer saw. Otherwise it answers with the
newer note and keeps it.

| Here | In the hub | What happens |
| --- | --- | --- |
| unchanged | changed | pulled |
| changed | unchanged | pushed |
| new | none | pushed |
| none | present | pulled |
| deleted here | unchanged | deleted in the hub (a tombstone, so the other machines learn of it) |
| unchanged | deleted | moved to `.harnessly-sync-trash/` |
| changed | deleted | pushed again (the edit wins) |
| changed | changed | **conflict** |

On a conflict the local note stays as it is. The other version is written beside it as
`<name>.conflict-<version>.md`, and the report names the machine it came from. Merge them into the note and delete the
copy; the next sync pushes the merged note. Conflict copies never sync. Any failure (the hub offline, a refusal, a full
disk) is reported, and the local notes it touched stay as they were.

## Another way to the hub

The runtime talks to the memory through two calls, so another service can take the agent's place:
`POST <url>/pull {projects}` → `{notes}` and `POST <url>/put {project, name, body | delete, base}` → `{note}` or
`{conflict}` (types in `internal/hubproto/memory.go`, client in `internal/memsync`). The runtime signs each request with
its secret under the audience `harnessly-memory-relay`.
