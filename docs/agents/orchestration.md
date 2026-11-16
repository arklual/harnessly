# A team of delegated chats

A chat hands work to another agent with `delegate_task` (the `harnessly` MCP server) or `harnessly tab create --prompt`:
a nested chat the person sees, on any provider and model of the machine. Several such chats under one chat are a
team, with what agent teams of other tools have and the chats stay visible: the person can open each, watch it and
take it over.

| What | Tools | What happens |
|---|---|---|
| Messages mid-turn | `send_to_conversation` (id `parent` or a sibling's id), `list_children` | the message reaches the other agent at its next step |
| Broadcast | `broadcast_children` | one message to every delegated chat at work; `deliver: now` interrupts their turns; free ones get it with their next turn |
| Wait for all | `wait_conversation` id `all` | one call until every delegated chat has answered |
| Task list | `task_add`, `task_list`, `task_claim`, `task_done`, `task_release` | shared tasks with an order, claimed under a lock |
| Checks | `check` of `task_add` | «done» only when the command exits 0; otherwise the task goes back with the output |
| Next task | (automatic) | a chat that finishes empty-handed gets the next free task |
| Plan first | `plan_first` of `delegate_task`, `submit_plan`, `approve_plan` | no edits until the parent approves the plan |
| Coordinator | `coordinate_mode` | the chat hands work out; its own edits and test runs are refused |

## A lean start

A delegated Claude Code chat gets a whole task and starts lean: without the person's memory index (auto memory off)
and without the person's plugins that bring neither hooks nor language servers (only MCP servers, skills, agents).
The project's CLAUDE.md, the platform's own tools and skills and every plugin with hooks (guards of pushes and
releases) stay. Instead of the memory index the chat gets a digest of it in its system prompt, built at every start:
the lines marked 🔴 (rules the person holds every chat to) and the lines of a section about access and tools, up to
about 1.5 thousand tokens. The smart notice tools (`notice_*`) are denied: asking the person is the leading chat's job. In a profile with many plugins this took the start from 63 to 40 thousand tokens. `full_context: true`
of `delegate_task` starts a chat with everything, for a task that needs the person's notes or one of their MCP servers;
`RUNTIME_DELEGATE_FULL_CONTEXT=1` turns lean starts off for the machine.

## Messages inside the team

The parent writes to the chats it delegated, as before. A delegated chat writes to its parent (`id: "parent"`) and to
the chats delegated beside it: `list_children` in a delegated chat names its parent and its siblings. The message goes
into the receiver's running turn and is read at its next step (`deliver: "now"` hands the running tool to the
background so it is read at once; `queue` waits for the turn to end). It ends with the sender's id, so the other agent
can answer. A sibling's message is news, not work from the parent: a `wait_conversation` of the parent is not reset by it.

Messages within a team are paced so two agents cannot spin answering each other: at most 6 to one chat and 20 to the
whole team in 10 minutes; past that the send is refused with `too-often`.

`broadcast_children` sends one text to every delegated chat at once. A chat at work gets it in its running turn; with
`deliver: now` (a stop) its turn is interrupted as by Escape — the command it runs in the foreground is stopped, not
handed to the background — and the message starts its next turn, so nothing more happens before it is read. A free
one (its turn ended) is not woken: the message is kept and goes into its next turn when that starts; `wake: true` starts
the free ones on it now. A stopped chat (closed, or stopped for standing idle) is skipped. The answer names each.

`wait_conversation` with id `all` waits for every chat the caller delegated and answers each one's result in one
call: a lead does not pay for its whole context once per chat and per poll.

## The task list

The team's list belongs to the chat that delegates (the lead); its delegated chats share it. Each task has an id (`t1`,
`t2`…), a title, the whole task in `detail`, the tasks it waits for (`after`) and an optional `check`.

- `task_claim` takes the named task, or without an id the first free one whose `after` tasks are done. A claim is a lock:
  two chats never hold one task, and a chat holds one task at a time. The lead may claim for a chat of its own (`for`).
- `task_done` reports a held task with its result. Without a check it is done at once. With one, the command runs in the
  folder of the reporting chat (`sh -c`, up to 20 minutes): exit 0 makes the task done and the answer names the next
  free task; any other code keeps the task with the chat and returns the end of the output. A check slower than the
  call (8 minutes, 45 seconds for Codex) answers at once, and its result comes as a message.
  A check runs as the machine, so only a chat that runs without asking for permissions (bypass) may set one, and it
  runs only when the reporting chat does too; it gets a plain environment (PATH, HOME, the language), no secrets.
- `task_release` gives a task back, with why.
- A delegated chat that claimed a task once and ends its turn with no task in hand is given the next free task and told
  so; its parent is woken only when nothing is left for it.

The person sees the list in the plan of the lead's chat (the pane's «План»), under the agent's own plan: done, in work
with the chat that holds it, waiting for another task.

The list lives in `delegation-tasks.json` beside the runtime's book of delegations (`<data>/v2/`).

## Plan first

`delegate_task` with `plan_first: true` starts the chat in the plan role: it may read and look around, but every edit
(`Write`, `Edit`, `apply_patch`…) and every shell command that writes is refused by its PreToolUse hook with words that
tell it to send the plan. The chat calls `submit_plan` and ends its turn with `[[question]]`; the plan reaches the parent
as a message. The parent calls `approve_plan` with `approve: true` (the edits are allowed from then on, and the chat is
told to go on) or `approve: false` and `feedback` (the chat changes the plan and sends it again).

## The coordinator

`coordinate_mode` with `on: true` makes the chat a coordinator: it delegates, keeps the list, reads and checks, but its
edits and its builds, test suites and linters (`go test`, `vitest`, `tsc`, `eslint`, `pnpm build`, `make`…) are refused
with a hint to hand them out. The agent turns it on when the person says so («только координируй», «сам не правь»), and
off when the person asks it to do the work itself.

## How the roles are held

The runtime keeps each chat's role (plan, coordinator) in its book and marks the agent's tmux session with a file under
`<data>/chat-role/`. The PreToolUse hook every chat already has reads it: `harnessly hook voice-guard` for Claude Code,
`harnessly hook role-guard` for Codex. No mark, or one that cannot be read, lets every call go. Read-only shell commands
(`git log`, `grep`, `sed -n '10,40p'`, `cat`…) pass in the plan role; a redirect into a file, a command inside the line
(`$(…)`, backticks, `xargs`), `find -delete` and anything unknown do not. A plan-role chat cannot switch itself to the
coordinator or back.
