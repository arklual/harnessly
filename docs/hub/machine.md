# A machine of a harnessly hub

A machine is a computer (or a container) that runs two programs: the **runtime**, which serves the web page and starts
terminals and coding agents, and the **agent** (`harnessly-agent`), which holds one outgoing, signed WebSocket to the
hub. The hub never connects in: it asks the machine for things over that link and carries a person's browser to the
runtime through it. How the link, the operations and the files are specified is in [api.md](api.md); the reasons are in
[architecture.md](architecture.md). This page is what a person who runs a machine needs to know.

- [Connect a machine](#connect-a-machine)
- [Profiles](#profiles)
- [What the hub can and cannot do to the machine](#what-the-hub-can-and-cannot-do-to-the-machine)
- [Machine setup](#machine-setup)
- [Files](#files)
- [The local policy](#the-local-policy)
- [The kill switch and the audit log](#the-kill-switch-and-the-audit-log)
- [Commands](#commands)
- [Agents: sign-in, keys, secrets and the proxy](#agents-sign-in-keys-secrets-and-the-proxy)
- [The container image](#the-container-image)
- [Upgrade, move and remove](#upgrade-move-and-remove)
- [When the machine does not appear](#when-the-machine-does-not-appear)

## Connect a machine

In the hub: **Machines, your own server**, choose a profile, copy the command and run it on the machine:

```sh
curl -fsSL https://hub.example.com/install | sudo sh -s -- --token <code> --profile dir
```

The command of `dir` runs the script with `sudo` (it makes a user and system services); the commands of `full` and
`container` have no `sudo`, because they run as you. After the machine has used the code, the hub's page shows the code
as used and the machine in its place, not the command again.

The code is good for one registration and for a short time. The script is the hub's own `deploy/install/install.sh`;
read it first with `curl -fsSL https://hub.example.com/install | less` if you like: it is one file, POSIX `sh`, and
nothing in it runs until the last line, so a download that stops half way does nothing.

What it does, in order: looks at the system (Linux with systemd, or macOS for `full`; Windows is not supported because
the agents need tmux), checks for root or passwordless `sudo` where the profile needs it, installs `tmux` and `git` when
they are missing (profile `dir` only), downloads `harnessly-agent` and `harnessly-runtime` from the hub and checks their
SHA-256 against the hub's `manifest.json`, sets up the profile, registers the machine (the code goes to the agent in a
private file, never on a command line), writes the services, starts them and waits up to 60 seconds for
`harnessly-agent status` to say the machine is linked. Then it prints «Done: the machine will appear in harnessly.»
(«Готово: машина появится в harnessly.» in Russian).

Options: `--name NAME` (what the machine is called in the hub; default its host name), `--dry-run` (print the plan,
change nothing), `--wait SECONDS`, `--hub URL` and `--image IMAGE` (when the hub did not fill them in), `--re-register` (register
the machine anew even when it is registered already, see [below](#upgrade-move-and-remove)), `--uninstall` (see
[below](#upgrade-move-and-remove)). Messages are English, or Russian when `LANG`, `LC_ALL` or `HARNESSLY_LANG`
starts with `ru`.

The runtime listens on `127.0.0.1:18022` and the agent on `127.0.0.1:18023` (the next free ports when those are taken;
a second run keeps the ports it chose). Nothing listens on a public address: the machine only dials out, to the hub.

## Profiles

Chosen with `--profile`; the choice is part of the command the hub shows.

| Profile | For | What the installer makes |
| --- | --- | --- |
| `dir` | a server you share or do not want agents to roam on | needs root once. The Unix user `harnessly` (no sudo, home `/var/lib/harnessly`), the work folder `/srv/harnessly/work`, two system services `harnessly-runtime` and `harnessly-agent` that run as that user with systemd hardening, the root-owned `/etc/harnessly/policy.toml`. Agents see that user's folders and nothing of other accounts |
| `container` | a machine you want to keep apart completely | runs the hub's machine image with Docker or Podman (the hub has to be configured with one: `HUB_CONTAINER_IMAGE`), the home on a named volume. Nothing but Docker is installed on the host |
| `full` | your own laptop or workstation | services of the account you run the command as: user units of systemd (with `loginctl enable-linger`) or launchd jobs on macOS. Agents see everything that account sees. The hub asks for a confirmation before it shows this command |

### `dir`

| Path | What |
| --- | --- |
| `/usr/local/bin/harnessly-agent`, `harnessly-runtime` | the programs, owned by root |
| `/etc/harnessly/policy.toml` | the [local policy](#the-local-policy), root-owned; an existing one is never overwritten |
| `/etc/harnessly/runtime.env`, `agent.env` | the services' environment: ports, folders, the hub's address, the machine's base path. No secret in them |
| `/etc/systemd/system/harnessly-runtime.service`, `harnessly-agent.service` | the units |
| `/var/lib/harnessly` | the user's home: `.harnessly` (the runtime's data, `machine.json`, the env files), `tmux` (the tmux socket), the agents' own logins |
| `/srv/harnessly/work` | the workspaces |
| `/etc/harnessly/install-manifest` | what the installer made, for `--uninstall`: it marks a folder, the user or the volume for `--purge` only when the installer itself made it |

The runtime unit sets `ProtectSystem=strict` (only the home, the work folder and `/tmp` are writable), `ProtectHome`,
`NoNewPrivileges` (no `sudo` for agents), `CapabilityBoundingSet=` and the kernel protections. It stops without taking
the tmux server and the agents down (`KillMode=process`), so an upgrade does not end a running agent, and for the same
reason it does not use `PrivateTmp`: the tmux socket lives in the user's home and the agents keep one `/tmp`.

### `full`

Everything is in the account's home: programs in `~/.local/bin`, `~/.harnessly`, `~/.config/harnessly` (policy, env
files), `~/work`, units in `~/.config/systemd/user` (the same as [deploy/systemd](../../deploy/systemd)) or plists in
`~/Library/LaunchAgents` (`io.harnessly.runtime`, `io.harnessly.agent`, logs in `~/Library/Logs/harnessly`), and
`~/.harnessly/install-manifest`. The home path may not contain spaces. Run it as the account, not as root.

### `container`

The installer pulls the image, registers the machine from a one-off container that shares the volume (so the code is
never stored in the long-running container's settings), and starts `harnessly-machine` with `--restart unless-stopped`.
Its home is the volume `harnessly-machine-home`. See [the image](#the-container-image).

## What the hub can and cannot do to the machine

The link reaches the runtime, and the runtime starts shells and agents: **whoever controls the hub can run code on
every machine connected to it**. The machine keeps the last word, in three ways ([policy](#the-local-policy),
[kill switch](#the-kill-switch-and-the-audit-log), [audit log](#the-kill-switch-and-the-audit-log)).

What the agent does for the hub, always on the machine's side of the policy:

- reports the system (`hello`, `preflight`: disk, memory, tmux, git, Node.js, the agents installed);
- installs Claude Code and Codex (their own installer and npm, into the account's `~/.local`);
- checks that the machine can reach an agent's provider (DNS, TCP, TLS, one light request without a key) and applies a
  proxy to agent launches;
- signs agents in: by link (Claude: `claude auth login` in a pane of its own, the URL read from the screen; Codex: the
  device code), in a [console](#the-console-page), or with an API key sealed in the browser to the machine's key and
  opened only here;
- stores secrets the hub delivers as environment variables for agent launches;
- creates the machine's SSH key for Git (`gitkey.ensure`, the public half goes back): `~/.ssh/harnessly_ed25519`, offered
  to `github.com`, `gitlab.com`, `bitbucket.org` and `codeberg.org` by a block between `# >>> harnessly >>>` and
  `# <<< harnessly <<<` in `~/.ssh/config` (for a forge of your own, add a `Host` line with the same `IdentityFile`);
- mints a session for a person the hub has signed in, and opens a tunnel from the hub to the runtime.

What it does not do: it takes no command line from the hub, never lets the hub write a file or run a program of its
choice outside the operations above (the one exception is the [machine setup](#machine-setup): the scripts of the
person's own organization's environment list, run by the person's runtime and only under the policy `setup`), never touches an account other than its own, and never stores or logs a key or
secret anywhere but the 0600 env files. `env.deliver` takes only names that look like secrets (`…_TOKEN`, `…_KEY`,
`…_PASSWORD`…) and refuses anything that would change how a shell, git, a pager, a package manager or an interpreter
behaves, and every address (`…_URL`, `…_HOST`) ([api.md](api.md)).

## Machine setup

Besides what the chats load, the organization's environment list can say what the machine is to have: the forges' CLIs
logged in (`access`), repositories cloned (`repo`) and Linux scripts run in order (`script`). What the items mean and
the runs are in [environment.md](environment.md#machine-setup); this is who does it on the machine.

- **The runtime does it, not the agent.** The agent is sandboxed by systemd on the managed profiles (the home is
  read-only, its own folder the only place to write) and must not run a program of anyone's choosing. The runtime runs as
  the person, holds the person's safe (`internal/secrets`) and already runs their commands. So the agent is the courier
  and the runtime (`internal/envsetup`, started by `cmd/runtime`) does the work, one runner for each link folder
  (`$AGENT_DATA_DIR`, `$AGENT_DATA_DIR/hub`, the folder of `AGENT_HUB_OBSERVE_FILE`).
- **Hand-off by files** in the link folder, which survive a restart of either process: `environment.json` (the list the
  agent holds) and `environment-requests/<runId>.json` (a run the hub asked for) go to the runtime;
  `environment-setup.json` (the hash worked on, each item's status, the ledger of scripts run once) and
  `environment-runs/<runId>.json` (a run as it stands, rewritten as steps move, kept 14 days) come back. The agent
  turns the first into the items' status in `env.state` and the second into `env.run` events (re-sent for the last 24
  hours after a connect).
- **When.** A list the runtime has not worked on is applied within seconds (`apply`); `env.run` asks for `update`,
  `check` (changes nothing) or `sandbox` (the list from scratch in a temporary home of its own, removed after the run; also inside bubblewrap
  where a probe shows it works — many machines refuse unprivileged user namespaces, and then the run goes on without it and
  says so in the log of every step). One run at a time. A run a crash cut short is marked «прервано
  перезапуском» and the list is applied again, the ledger sparing what already ran once.
- **Secrets.** A script gets the safe's secrets it names as variables (`npm-token` → `NPM_TOKEN`) and the output is
  masked. A secret that is not in the safe opens the safe's ask on the person's page («Положить в сейф»); the run waits
  and goes on once the secret is there.
- **Policy `setup`** (on when the machine has no policy file; a policy file must say `setup = true`, a file without the key means off). The agent writes its decision into the link
  folder (`environment-allow.json`, `{"setup": bool}`) and the runtime runs **nothing** (no apply, update, check or
  sandbox) unless it says true; a missing file is a no. With it off, `env.run` answers `policy_denied`, `hello` does not
  offer the feature `environment.setup` (so the hub sends no setup items) and the setup items are `pending` «настройка
  машины выключена на этой машине (политика setup)». The kill switch turns it off too.
- **Observe links are off by default.** The machine of a closed cluster is not the hub's to set up on the hub's say. The
  operator opts in on the machine with `AGENT_HUB_SETUP=on` in the agent's environment; without it the observe link
  gets no feature, `env.run` is `policy_denied`, and the runtime is told no. `RUNTIME_ENV_SETUP=off` in the runtime's
  environment switches the runner off on the machine itself (the items then stay «машина ещё настраивается»).
  The hub's safe is opt-in on an observe link the same way, with `AGENT_HUB_SAFE=on` ([safe.md](../safe.md)).
  So are the machine's chats, with `AGENT_HUB_HISTORY=on`: the hub gets a copy of them beside the cluster's, which
  keeps its own ([history.md](history.md)).
- **Limits of what is promised.** Scripts run as the person with a minimal environment; they can do what the person can.
  A script by address is fetched over https only (no redirect to http), a repository over https or ssh only. The sandbox
  is for finding what a fresh machine lacks, not for containing a script: without bubblewrap nothing stops a script
  from leaving the sandbox home. The runtime runs setup programs with lowered CPU and disk priority, one run at a time,
  and clones only with 2 GiB free. A list that is not done (an error, a token missing) is tried again every 30 minutes.
  The runtime's heartbeat `environment-runner.json` (every 30 s) tells the agent a setup is there to offer the feature.

## Base package

Some parts of harnessly need tools on the machine: chats need tmux, voice input needs ffmpeg and a speech model,
design checks and exports need Chromium, PPTX needs LibreOffice and unzip, apps need bubblewrap. The base package is
the list of them, built into harnessly (`internal/basepkg`). The same list feeds the machine checks, the
«Настройка машины» card on the start page (its first stage, «Основа») and the installer.

| Item | Without it |
| --- | --- |
| tmux, git, bash | chats, tabs, the terminal and repositories do not work |
| curl, ssh, python3 | part of the setup scripts fails |
| Node.js and npm | agents cannot be installed or started |
| ffmpeg | no voice input, recording or video |
| bubblewrap | apps (plugins) do not start |
| jq, ripgrep | agents are slower with service answers and code search |
| Chromium | no design checks, no PNG, PDF or MP4 export |
| LibreOffice, unzip | no PPTX import or export |
| speech model (`huggingface.co`, `github.com`) | no voice input |

**Who installs it.**

- `dir`: the installer adds `harnessly-basepkg.service` (root, oneshot), a daily `.timer` and a `.path` unit. The
  service runs right after the machine connects, so nothing waits for the person's first visit. It runs
  `harnessly-agent basepkg install`, which installs only the system packages of the list built into that binary. It
  takes no commands from the hub or the runtime. From its inbox (`<data dir>/basepkg`, written by the runtime) it reads
  only the ids the person skipped and a proxy address, and it writes its state to `/var/lib/harnessly-basepkg`.
  `HARNESSLY_BASEPKG=0` at install leaves the service out.
- `full`: nothing is installed as root. The card shows one command for the machine's package manager
  (`sudo apt-get install …`), with «Скопировать команду» and «Пропустить».
- `container`: the image carries the system tools.
- The speech model is the runtime's own. It is fetched in the background a few minutes after start, when there is
  room and the machine is not busy.

**Care for the machine.** The service runs with `Nice=19` and the idle I/O class, installs one item at a time and
restarts no service. An item is installed only when 2 GiB stay free after it (`waiting_disk` otherwise). Heavy items
(Chromium, LibreOffice, the speech model) wait while the load is above twice the cores (`deferred`). A failure of one
item never stops the others.

**No network.** An item whose install failed on the network is `no_access`, with the address when the package
manager names one. The card offers «Указать прокси» (an `http`, `https` or `socks5` address; a login and password go
in the safe, never in the address) and «Пропустить». The service goes direct first and uses the proxy only after
the direct way failed on the network; the package manager checks the packages' signatures either way. A skipped item is not offered again until the person brings it
back under Settings → General.

Runtime endpoints, all under the page's session:

| Request | What it does |
| --- | --- |
| `GET /api/platform/basepkg` | the items and their status: `ok`, `missing`, `installing`, `no_access`, `waiting_disk`, `deferred`, `skipped`, `error` |
| `POST /api/platform/basepkg/run` | install now (the service's request file and the speech model) |
| `POST /api/platform/basepkg/skip` `{"id": "chrome", "skip": true}` | skip an item, or bring it back with `false` |
| `POST /api/platform/basepkg/proxy` `{"url": "http://proxy:3128"}` | the proxy for the install; `""` removes it |

## Files

| Path | Mode | What |
| --- | --- | --- |
| `$AGENT_DATA_DIR/machine.json` | 0600 | the machine's id, the hub's address and the two private keys (Ed25519 for signing, X25519 for sealed keys). Whoever has it is the machine: do not copy it |
| `$AGENT_DATA_DIR/agent-env/all.env`, `claude.env`, `codex.env` | 0600 | `KEY='value'` lines for agent launches: keys (`claude.env`, `codex.env`), secrets and the proxy (`all.env` or an agent's own file) |
| `$AGENT_DATA_DIR/agent-env/proxy.json` | 0600 | the proxy with its credentials |
| `$AGENT_DATA_DIR/agent-audit.log` | 0600 | one JSON line per change the hub made, never a secret |
| `$AGENT_DATA_DIR/budget.json` | 0600 | the last `budget.set` (the hub's limits, [limits.md](limits.md)); kept over a restart and while the link is down; only a newer version replaces it |
| `$AGENT_DATA_DIR/agent-link.json` | 0600 | what the running agent tells `harnessly-agent status` (linked or not, why); refreshed every 20 s |
| `~/.ssh/harnessly_ed25519`, `.pub`, and the marked block in `~/.ssh/config` | 0600 / 0644 | the machine's git key, made once when the hub asks for it (never over a key that is there) |
| `$AGENT_DATA_DIR/gitkey.json` | 0600 | which of those files the agent made itself (the key, the `.pub`, `~/.ssh/config`, `~/.ssh`), so `--uninstall` takes away only those |
| `/etc/harnessly/policy.toml` or `~/.config/harnessly/policy.toml` | root 0644 / user 0600 | the local policy |
| `disabled` next to the policy file, in `/etc/harnessly` or in `$AGENT_DATA_DIR` | | the kill switch |
| `/etc/harnessly/agent.env` or `~/.config/harnessly/agent.env` | 0644 / 0600 | the folders the `harnessly-agent` commands use, so `sudo harnessly-agent status` looks where the service does |

`AGENT_DATA_DIR` is `~/.harnessly` of the account the services run as (`/var/lib/harnessly/.harnessly` for `dir`).

## The local policy

A TOML file the hub cannot change. It is read at start, on `SIGHUP` and whenever it changes (by content, so a quick
same-size edit counts). A missing file means every capability is on; a file with an error means **everything is off**,
and the agent says why in its log.

```toml
terminal = true          # terminals, the chat page's shell, the login console
agent_login = true       # signing agents in
keys = true              # API keys and secrets delivered from the hub
env = true               # environment variables delivered from the hub
install = true           # installing Claude Code and Codex
runtime_session = true   # the hub opening this machine's page for a person, and the tunnel it needs
setup = true             # the environment list setting the machine up: logins, clones, scripts (env.run)
peers = true             # messages from and to agents of other machines of the hub (peers.md)
deny_routes = []         # route prefixes of the runtime to refuse, e.g. ["/api/terminal", "/api/fs/write"]
```

A refused operation answers `policy_denied`, and the hub shows «запрещено настройками машины». The agent puts the policy
in `hello` and sends it again when it changes, so the hub's page shows what the machine allows.

The runtime is reached through a filter inside the agent, not directly. With `terminal = false` the filter also refuses
every route that opens a shell: `/api/terminal`, `/api/v2/terminal`, `/api/v2/conversations/<id>/terminals`, `/console/…`.
`deny_routes` entries match a path prefix as the runtime sees it (without the machine's base path).

Switching the terminal off is more than closing those sockets, because the runtime has many routes that end in a shell
or in a command. While `terminal = false` the filter works from a list of the routes that may change anything, and
refuses every other write (`POST`, `PUT`, `PATCH`, `DELETE`): the chat and its prompts, uploads, the editor's save,
layout and workspace housekeeping, and the creation of Claude, Codex and browser tabs. Not on the list, so refused: raw
keys into tmux (`/api/tmux/*`), the CLI's tabs, `PATCH /api/config` (the permission switch), the agents' settings (MCP
servers, rules), Tailscale, a first-run password. A new tab or pane must name `claude-code`, `codex-cli` or
`web-browser` and carry no `command`; a new workspace must resume an agent's conversation. The filter reads a body the
way the runtime does (by its `Content-Type`; a name written twice or in another case is refused, not guessed), and
refuses what it cannot be sure of. Reads stay open.

What the filter cannot see is what a pane holds, and the chat's routes type into panes. So the agent also tells the
runtime next door whether the terminal is off (`POST <runtime>/api/agent/policy`, signed with the pair's secret like its
other calls: at start, at once when the policy changes, and every 15 seconds, so a runtime that restarted learns it
again; a hub's runtime counts the terminal as off until it has been told). While it is off the runtime itself refuses:

- **Typing into a pane with no agent in front.** A chat message, a queued message, «send now», a steer and every key the
  chat's typist writes go only into a pane where the agent runs and is in the foreground (not a shell, not a program
  the agent was left for, and not a pane tmux could not describe). A tab whose agent exited, or a shell tab left over
  from before, takes nothing: the text would be read by the shell and run. The queue holds its messages until the agent is
  back, and the chat answers 403 with the reason. Before this, only the slash-command runner checked.
- **Claude's and Codex's own shell mode.** A message that starts with `!` (spaces and invisible characters in front do not
  hide it) is a command to the agent's shell mode, run without asking. It is refused as a message, as a queued message
  (also one queued before the terminal was switched off) and as «send now».
- **Approving a tool call or a plan.** `POST /api/v2/cmd/decide` is refused by the filter, and so is a key of the menu
  mirror (`/api/platform/commands/screen`) while the agent's screen asks to approve something. An agent that asks to run
  a command and is told yes has run it, which is what a terminal is. A message that makes the agent ask is not refused,
  the question just stays open for the owner at the machine.

**`terminal = false` is not a boundary against running code while the agents run in a permissive mode.** An agent started
with its permission checks off (Claude's `--dangerously-skip-permissions` or `bypassPermissions` mode, Codex's `--yolo`)
runs what the conversation asks for: whoever can send it a message can make it run commands, and nothing in the hub or
the machine agent can tell that from work. The switch closes the ways *around*
the agent (a shell, a typed line, a keystroke that approves); it does not turn an agent into something that cannot run a
command. For a machine whose hub is not trusted with commands, keep the agents in their asking modes and leave
the approvals to the person at the machine. Messages that were queued before the switch are delivered as they were,
except the `!` ones above.

The filter also refuses, with the terminal on or off, any write by the editor's save to the policy file, the kill switch,
`machine.json`, the audit log and the agent's env files (`agent-env/`), wherever the path puts them. These are the files
that decide what the hub may do, and the runtime's file routes would otherwise let the hub change them.

Where the file is: `AGENT_POLICY_FILE`, else `/etc/harnessly/policy.toml` when it exists, else
`~/.config/harnessly/policy.toml`. `harnessly-agent policy` prints the policy in force.

### Who can write the file

"A file the hub cannot change" holds only if the account the agent runs as cannot write it either: the hub reaches the
runtime, and the runtime runs as that account. The agent checks the permissions of the file and of every folder on the
way to it.

- **Root-owned, in a folder only root changes** (the `dir` profile's `/etc/harnessly/policy.toml`, a read-only mount in a
  container): the file is the owner's alone and every change of it applies as soon as it is seen.
- **Writable by the agent's account** (the `full` profile's `~/.config/harnessly/policy.toml`): the file is trusted for
  what it takes away and not for what it gives back. While the agent runs, a change that switches something off or adds
  a denied route applies at once; a change that allows **more** than the agent was running with is held, with a warning
  in the log, until `SIGHUP` or a restart, which are the owner's word. A file with an error still closes everything at
  once, and mending it opens only up to what the agent was running with. Deleting the file does not open anything either.
  `harnessly-agent policy` prints a note when the file is of this kind.

For a policy the hub cannot lift, use the `dir` profile or mount a root-owned file.

## The kill switch and the audit log

`harnessly-agent off` writes a file called `disabled` (next to the policy, or in `/etc/harnessly`, or in the data
folder). The agent notices within a second, closes the link and does not dial again until `harnessly-agent on` removes
the file. Stopping the service does the same without the agent: `systemctl stop harnessly-agent` (`--user` for `full`).
The page of a machine that is off shows «Машина не на связи».

Every change the hub makes (a login, a key, a secret, an install, a proxy, a runtime session, a tunnel) is appended to
`agent-audit.log` with its time, never with a value.

## The hub's limits on the machine

The agent answers `budget.set` by writing the list to `budget.json` (a temporary file, then a rename) and answers the
version it holds; a version not newer than that changes nothing. It sends the `budget` feature and `budgetVersion` in
`hello`. The runtime next door reads the file (cached by its mtime; a missing or damaged file is no list, so the
machine does as it did without a hub) and applies it in two places, under a hub only:

- **New chat.** `launch.Starter` asks `Config.Admit` before it makes a tab, and `v2Launcher.Launch` asks the same first,
  so the person is shown the reason and the reset time. Only `stop` in `mode = stop` refuses.
- **Running chat.** `budget-hook.sh`, beside the status hook, runs on every prompt and tool call of Claude Code
  (`UserPromptSubmit`, `PreToolUse` in `hooks.json`) and of Codex (the `-c hooks.*` flags). It does nothing while the file
  is missing or its mode is `off`; otherwise it calls `POST /api/budget/hook`, and the runtime's answer is the hook's
  output: a notice (`systemMessage`, once per chat and entry, and again when the rule's status or period changes), or in
  `stop` mode a refusal of the prompt (`decision: block`) and an end of the turn at a tool call
  (`continue: false`). A runtime that is away never stops a chat.

Code: `internal/budget` (matching, the decision, the file), `cmd/runtime/budget_gate.go` (the wiring),
`internal/agent/budget.go` (the op).

What is not done yet:

- **The person of a chat.** The runtime does not yet know who opened a chat (the person's email reaches the agent in
  `runtime.session`, not the runtime). A chat is taken to be the machine owner's (`RUNTIME_OWNER_EMAIL`), else the only
  person the list names; on a shared machine with entries for several people and no owner set, nothing is applied.
  Next: carry the session's subject into the launch and the tab's metadata, and set `Gate.UserOf`.
- **The account.** Only subscription logins are known (`claude:<uuid>` from `~/.claude.json`, `codex:<id>` from
  `auth.json`); a chat on an API key or another endpoint matches entries for any account only. Replace
  `budget.Accounts` with the usage collector's record of the chat.
- **The model of an Auto launch** is unknown, so an entry that names a model does not match it.
- **A Codex tab is not paused through tmux** (the table in [limits.md](limits.md)); a Codex chat is refused through its
  hooks only, and the shape of a Codex hook's refusal is not yet checked against a Codex CLI of this version. TODO: pause
  the pane through tmux.

## Load, hardware and restart

Agents of this version announce the features `metrics` and `drain` in `hello` (an older agent does not, and the hub then
asks for neither; `drain` also covers `runtime.restart.cancel` and the `restart` event). They add to the protocol, all optional, so an older hub or agent keeps working:

- `hello.hardware` `{ cores, ramBytes, diskBytes, model, laptop, os, arch }` — what the machine is made of; a field it
  could not read is left out.
- the event `metrics.sample` `{ sample: { cpuPct, ramPct, diskPct, chatsNow, at } }` every 60 seconds, and only to a link that named it in `link.subscribe` (a hub that does not subscribe is never sent it); a value that
  could not be read is `null`. The agent reads them from the system (the work folder's filesystem for the disk), not from
  the hub.
- the operation `runtime.restart` `{ jobId, when: 'now'|'after_chats'|'drain' }` answers at once with
  `{ state, chatsNow }`; progress is the event `restart` `{ jobId, state: 'waiting'|'restarting'|'done'|'failed'|'cancelled',
  chatsNow?, error? }`. One restart at a time. `drain` refuses new chats on the machine until none is at work. The
  operation `runtime.restart.cancel` `{ jobId }` takes back a restart that waits. The runtime restarts by exiting, its
  service starts it again, and `done` is said when it answers.

What the hub does with them, and the API, is in [machines-resources.md](machines-resources.md).

## Commands

| Command | What |
| --- | --- |
| `harnessly-agent register --hub URL (--token CODE \| --code-file PATH) --profile P [--via command\|ssh\|local\|container] [--name NAME] [--force \| --reuse]` | registers the machine and writes `machine.json`; the installer runs it for you. `--reuse` keeps a registration with the same hub and profile that is in `machine.json` and sends nothing to the hub |
| `harnessly-agent unlink` | asks the hub, signed with the machine's own key, to remove this machine (`DELETE /agent/v1`); exit 0 when the hub no longer has it. The files stay |
| `harnessly-agent gitkey-remove` | takes the harnessly block out of `~/.ssh/config` and removes the git key files the agent made; a key or a file that was there before stays |
| `harnessly-agent status` | one line, exit 0 only when linked: `linked to <hub> as <id>`, `not linked to <hub> as <id>: <reason>`, `off (<file>)`, `registered as <id> with <hub>, but the agent is not running`, `not registered` |
| `harnessly-agent off` / `on` | the kill switch |
| `harnessly-agent policy` | prints the policy in force |
| `harnessly-agent version` | |
| `harnessly-agent` | runs the agent (what the service does) |

## Agents: sign-in, keys, secrets and the proxy

Agent launches (a terminal tab, a chat's agent, a resume) start with `set -a; . all.env; . <provider>.env; set +a;` for
the env files that exist, so a key, a secret or a proxy the hub delivered reaches Claude Code and Codex and nothing else
needs the person's shell profile. The side chat and the chat titles, which the runtime starts itself, get the same
variables.

- **Sign in by link.** Claude: the agent runs `claude auth login` in a wide pane of its own, finds the URL on the screen
  and sends it to the hub's page; the person signs in and pastes the code back, which the agent types into the pane.
  Codex: the device code and its link. The agent confirms with `claude auth status` / `codex login status`.
- <a id="the-console-page"></a>**Sign in in a console.** The agent asks the runtime for a terminal that runs the login
  command, shown on a page of its own at `<base>/console/<id>` (the output as text with its links clickable, a field to
  type into, a few keys). The chat page has no tab for a shell that belongs to no chat, so this page is the console. The
  agent polls the status until the agent is signed in. It needs `terminal = true`.
  A CLI that waits for the provider's redirect on a loopback port (`http://127.0.0.1:<port>/callback?code=…`) can't get it:
  the browser that ends on that address runs on the person's computer. When a process of the console listens on a
  loopback port, the page shows a field for the address the browser ended on (the "this site can't be reached" page),
  and the runtime makes that one GET to the listener on this machine. Only loopback hosts (`localhost`, `127.0.0.1`,
  `::1`) and only a port a process of that console listens on; once per console; the console belongs to the first
  signed-in person who opened it, anyone else is refused (403); the address is never logged and the listener's answer
  isn't sent back, only its status.
  **The guided sign-in page (GitHub #69).** The console page opens as a sign-in view, not a terminal: the account and the
  machine it signs in on, one **Open sign-in page** action (the first `https` address the CLI printed), and where the
  attempt stands: *preparing*, *waiting for the browser*, *verifying*, *connected*, *failed*, *expired* or *cancelled*,
  with the time left. The terminal (output, input, keys) is under **Troubleshooting**; it opens by itself when the
  sign-in waits on no loopback port (it may want a code typed) or has failed. When a console process listens on a
  loopback port, a collapsed section explains why the browser ends on a page that does not load and takes the pasted
  address, with the limits above. **Connected** is shown only after the machine agent's own status check of the account
  (never for the callback's delivery): the agent tells the runtime the state with `POST /api/agent/console-state`
  (`{"tabId","state","provider","label","expiresAt"}`, machine agent only; states only, never what the CLI printed) and
  the runtime answers whether the person cancelled. **Cancel** (`POST /api/console/<id>/cancel`, the console's owner
  only) closes that console's terminal, and the CLI process and listener with it; no other console is touched, and
  an attempt that ended in a state stays in it (a cancelled one never turns into connected). A sign-in that outlives its
  timeout ends as expired and its terminal is closed.
- <a id="cli-sign-in"></a>**Other CLIs (GitHub #58).** A machine signs in more CLIs than the agents the same console
  way. They are described in `cli-logins.json` of the agent's data folder, or in the file `AGENT_CLI_LOGINS_FILE`
  names; an organization's extension pack writes it, and the agent reads it on each use (no restart, no hub setting):

  ```json
  [{"id": "example-cli", "label": "Example CLI", "login": ["example-cli", "login"],
    "status": ["example-cli", "whoami"], "timeout": 600}]
  ```

  `id`: lower-case letters, digits and dashes, 2 to 32, not `claude`, `codex` or `gemini`. `label`: up to 60
  characters. `login` and `status`: a command as arguments (up to 32, no line breaks), run as they are, nothing from the
  browser is added. `timeout`: seconds a sign-in may take (900 when absent, at most 3600). Up to 16 CLIs; a wrong entry
  is skipped and noted once in the audit log (`cli-logins.load`). Both commands run with the machine's `all.env` and
  `<id>.env`, `BROWSER=echo` and no display, so the CLI prints its address in the console instead of opening a
  browser nobody sees. `status` exits 0 when signed in; its output is thrown away (it may print a token); a missing
  command reads as not installed. The agent reports each CLI next to the agents (`kind: "cli"`, `label`, `installed`,
  `login`), the hub keeps the list per machine, and «Агенты» shows it under «Входы в CLI». The sign-in is the console
  above and nothing else: the hub starts it only for an id the machine reported (404 otherwise), only by `console`
  (400), only for a space owner (403); the loopback callback goes through the console page, once, for its owner only.
- **API key.** Sealed in the browser to the machine's X25519 key, opened here, checked with the provider (a models-list
  request), stored in `claude.env` or `codex.env`. A key already exported on the machine is checked in place.
- **Proxy.** `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` in an env file, for all agents or for one. A `socks5://` proxy gets
  a local HTTP bridge on `127.0.0.1` inside the agent. It applies to agent launches, their sign-in and the provider check,
  never to the agent's link to the hub.

## The container image

The hub's `machine` image holds `harnessly-agent` and `harnessly-runtime` in `/usr/local/bin` and starts
[`deploy/container/entrypoint.sh`](../../deploy/container/entrypoint.sh). The script registers the machine when
`machine.json` is not in the home volume, then runs the runtime and the agent side by side and ends when either does
(restart policy brings the pair back).

| Variable | What |
| --- | --- |
| `AGENT_HUB_URL` | the hub's public URL (to register) |
| `AGENT_REGISTER_CODE` or `AGENT_REGISTER_CODE_FILE` | a one-time code, or a file holding it (read on every try, so a hub that renews it works) |
| `AGENT_REGISTER_PROFILE`, `AGENT_REGISTER_VIA` | default `container` and `container`; the hub's local machine uses `container` and `local` |
| `AGENT_REGISTER_NAME` | the name in the hub |
| `AGENT_REGISTER_WAIT` | seconds to keep trying while the hub or the code is not there yet (default 120) |
| `MACHINE_RUNTIME_PORT`, `MACHINE_AGENT_PORT` | loopback ports (default 18022 and 18023); neither is published |
| `HARNESSLY_SUPERVISE` | `1` runs the runtime and the agent under `harnessly supervise`: a program that ends is started again (waiting 1 s, 2 s, 4 s ... up to 30 s after quick failures), the reason is logged and noted in `supervisor.json` of the data folder, and the machine keeps its id in the hub. Default `0`: the container ends when either program does |
| `AGENT_AUTH=hs256` | run the closed-cluster way, without registering |

To apply a policy to a container machine, mount a file at `/etc/harnessly/policy.toml`.

## Company network, a check before installing, closed networks

Both installers (`install.sh` for a single machine, the hub's `/install` for a hub machine) take the same network options. Without them nothing changes.

| Option | What it does |
|---|---|
| `--check` | Looks and changes nothing: the system, curl/tar, tmux and git, free disk, the port, the service manager, Docker, and whether the network works. A failure says what to give: `--ca-file` for a company CA, `--proxy` or `--mirror` for a closed network, `--offline` for none |
| `--ca-file FILE` | The company CA. Every component gets `SSL_CERT_FILE`, `NODE_EXTRA_CA_CERTS`, `CURL_CA_BUNDLE`, `GIT_SSL_CAINFO` and `REQUESTS_CA_BUNDLE`; the container profile mounts the file read-only |
| `--proxy URL`, `--no-proxy LIST` | `HTTPS_PROXY`, `HTTP_PROXY` and `NO_PROXY` for every component. `127.0.0.1`, `localhost` and `::1` (and the hub's host) always go direct |
| `--mirror URL` | Release files from `URL/vX.Y.Z/…` (and `URL/latest.txt`, `URL/stable.json` for the [stable channel](../self-host/update-channels.md)) instead of github.com. `--npm-registry` and `--pip-index` set the package sources |
| `--offline DIR` | Nothing is downloaded: the programs come from `DIR` (see below) |
| `--docker external\|own\|auto` | Hub installer, container profile. `external` uses only a daemon that already runs; `own` and `auto` start a private `dockerd` under `~/.harnessly/docker` when none runs (root and `dockerd` are needed). A daemon that runs is never touched |
| `--log FILE` | The step journal; by default `install.log` in the data folder (`~/.harnessly`) |

`harnessly install` runs the single-machine installer with a check first and passes every option on. The settings are written once, into a marked block of `~/.config/harnessly/env` (and the `runtime.env` and `agent.env` of the hub installer); running the installer again replaces the block and updates in place.

**Offline bundle.** On a machine with the internet, `scripts/make-offline-bundle.sh X.Y.Z [OUT] [linux_amd64 …]` downloads the release archives, checks them against `checksums.txt` and writes `harnessly-offline-X.Y.Z.tar`; `scripts/make-offline-bundle.sh --hub https://hub.example.com` makes the same from a hub (its `/install` and a copy of `/dl`). Copy the `.tar` over, unpack it and run `sh install.sh --offline .`. tmux and git are not in the bundle, and a hub machine still has to reach its hub to register. The bundle is a build output and is not committed.

## Personal and administered machines

A machine is personal: its owner is the person who registered it, and its chats, history copy, memory and agent
sign-ins are theirs. A space owner sees every machine of the space in the inventory and may write to it, but its chats
do not join the owner's sidebar or search; the page opens on a machine of the person's own.

A machine registered by an admin for somebody else is handed over with `POST /api/machines/{id}/owner`
(`{"userId": "…", "history": "move"|"keep"}`), for a space owner only and to a member of the machine's space. By
default the chats the machine already copied to the hub move to the new owner; `keep` leaves them with the previous
one. The machine's link reconnects so that new chats land with the new owner, and the change is written to the audit
log (`machine_owner_changed`). The machine stays as trusted as it was: on a hub without `HUB_RUNTIME_DOMAIN` the
rule «an administrator connected it» is asked of who connected it (`machines.connected_by`), not of the new owner. The
machine answers `ownerId` in `GET /api/machines/{id}`.

## Upgrade, move and remove

- **Updates by themselves**: the runtime compares its programs (`harnessly-runtime`, `harnessly-agent`, the `harnessly`
  command) with the hub's `/dl/manifest.json` two minutes after a start and then every hour. Where it may write the
  folder they are in (the `full` profile, `~/.local/bin`) it downloads what differs, checks it against the manifest's
  SHA-256, puts it in place with a rename and restarts; tmux and the agents in it keep working. Settings → General →
  Updates shows the state and has «Update now». `RUNTIME_AUTO_UPDATE=0` in `runtime.env` leaves it to the button.
  After an update the machine does not update itself again for a day (`RUNTIME_UPDATE_MIN_GAP`, `24h`; `0` for no
  window): every update restarts the runtime, and a hub that releases several times a day would restart each machine
  as often. «Update now» and the command are not held back. The
  `dir` profile (`/usr/local/bin`, read-only to the services) and a container cannot update themselves: Settings shows
  the command below.
- **Fleet mode** (`RUNTIME_UPDATE_MODE=fleet`, off by default): a machine that takes whole releases from a rollout
  (stage, switch with a health check, rollback, deltas) instead of the hourly pull: [fleet-rollout.md](fleet-rollout.md).
- **Upgrade by hand**: `curl -fsSL https://hub.example.com/install | sh -s -- --update` (`sudo sh` for `dir`): no code,
  the profile it was installed with, the same machine. Running the install command again works too. It
  replaces the programs, keeps the policy, the ports and the machine, and restarts the services. A machine that is
  registered with this hub (a `machine.json` of the same hub and profile) stays one machine in the hub: nothing is sent
  to the hub and no second record appears. A running agent, tmux and what is in it survive the runtime's restart.
- **Move or re-register** a machine: run the install command with a new code and `--re-register` (or
  `harnessly-agent register --force …` and a restart). A machine that registers again in the space it is in keeps its
  id, its name and its chats: it proves to the hub, with the key in its `machine.json`, that it is the same machine, and
  gets new keys. Registered into another space, without its `machine.json`, or after it was removed or switched off in
  the hub, it is a new machine, and the old record stays in the hub until you remove it there.
- **Remove**: `curl -fsSL https://hub.example.com/install | sudo sh -s -- --uninstall` (`sudo` for `dir` only; add
  `--profile P` to pick one when several are installed, `--dry-run` to see the plan). It first asks the hub to forget the
  machine (`harnessly-agent unlink`, signed with the machine's own key, so the machine disappears from the hub's list and
  its key stops working); when the hub cannot be reached it says so, goes on, and you remove the machine in the hub's
  list yourself. Then it stops the services and takes away what the manifest lists: the programs, units, env files,
  policy, `machine.json`, the agents' env files, the git key and the block in `~/.ssh/config` that the agent made.
  **It keeps** the work folder and the home of the `dir` user (your code, the agents' logins) unless you add `--purge`.
  `--purge` removes only what the installer made: the user `harnessly` and its home, `/srv/harnessly` or the work
  folder, the container volume, each only if the installer created it. A folder, a user or a volume that was there
  before is never removed (the manifest says what was made; a manifest of an earlier installer has no such marks, and
  its folders are kept). A `full` install never removes `~/.harnessly` as a whole or `~/work` unless the installer made
  them and you give `--purge`; they are the account's own otherwise.

## When the machine does not appear

Run `harnessly-agent status` on the machine (`sudo harnessly-agent status` for `dir`) and read the line:

| Status | Meaning |
| --- | --- |
| `not registered` | the code was not accepted or the install stopped before registering; get a new command in the hub |
| `registered as …, but the agent is not running` | start it: `systemctl start harnessly-agent` (`--user` for `full`); read `journalctl -u harnessly-agent` |
| `… not linked … : could not reach the hub: …` | the machine cannot reach the hub: DNS, a firewall, a proxy in between; the reason is the network's own |
| `… : the hub did not accept this machine's signature: check the clock, or the machine was removed` | the signature carries the time, so a wrong clock fails it; or the machine was removed in the hub |
| `… : revoked by the hub` | the machine was removed in the hub (close code 4010); register it again |
| `… : the hub needs a newer harnessly-agent` | the hub requires a newer agent (`HUB_MIN_AGENT_VERSION`); run the install command again |
| `… : dialling the hub` | it is trying; wait a few seconds |
| `off (…)` | the kill switch is on; `harnessly-agent on` |
| `machine.json is damaged: …` | do not edit it; register again with `--force` |

Logs: `journalctl -u harnessly-agent -u harnessly-runtime -n 100` (`--user` for `full` on Linux),
`~/Library/Logs/harnessly/*.log` on macOS, `docker logs harnessly-machine` for the container.

## When something on the machine quietly does not work

Settings → General → _Machine check_ lists only what is not in order on the machine, each with the command that puts
it right (and a _Copy_ button). The runtime runs a fixed list of checks; the page names a check by its id and can make
the runtime run nothing else. Two fixes the runtime makes itself, without root, by the _Fix_ button: the `PATH` line
for `~/.local/bin` in the shell's start file, and the hub's update of the machine's programs.

| Check | Seen as | Fix |
| --- | --- | --- |
| tmux 2.9+ | chats do not start | `sudo apt-get install -y tmux` (`brew install tmux`) |
| git | no changes shown, no commits | `sudo apt-get install -y git` |
| Claude Code or Codex | no agent to start | `curl -fsSL https://claude.ai/install.sh \| bash`, `npm install -g @openai/codex` |
| git `user.email` | the agent cannot commit | `git config --global user.name "…" && git config --global user.email …` |
| `~/.local/bin` in the login shell's `PATH` | `harnessly: command not found`, `claude: command not found` in a new terminal | `echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc` (button) |
| the `harnessly` command beside the runtime | agents cannot open tabs or show files (a machine an older hub installed) | `curl -fsSL <hub>/install \| sh -s -- --update` (button, where the machine updates itself) |
| lingering (`full` on Linux) | the machine goes offline when you log out | `sudo loginctl enable-linger $USER` |
| ffmpeg | no microphone | `sudo apt-get install -y ffmpeg` (`brew install ffmpeg`) |
| the built-in recognition can run | the microphone answers with an error | macOS: `brew install whisper-cpp`; a processor without AVX2: your own server (docs/voice.md) |
| the voice model can be downloaded | the first dictation fails | download the model by hand (the command is shown), or your own server |
| room on the data folder's disk (1 GB and the model still to come) | downloads and updates fail | free the disk; the command shows what takes the room |
| https (in the browser) | no microphone button | open the machine through the hub, or put TLS in front |
| the microphone allowed (in the browser) | dictation does not start | allow it in the site settings and reload |

Only the voice checks reach the network, and only when the model or the server binary is still to be downloaded: a
`HEAD` request to `huggingface.co` and `github.com`, kept for ten minutes.

## Disk tab (`/api/fs/*`)

Off by default. Set `RUNTIME_DISK_ROOTS="Label=/abs/path;Other=/abs/other"` on the runtime to turn it on; GET `/api/capabilities` then reports `disk` as on. The id of a root is its label as a slug. Routes: `roots`, `list` (paged, 500 per page), `favorites` (GET/POST/DELETE, kept in `<data>/disk-favorites.json`), `download` (ranges supported), `mkdir`, and resumable `uploads` (create, `HEAD`/`PUT` with `Upload-Offset`, `complete`, `DELETE`; unfinished uploads sit in `<data>/disk-uploads/`).

Only the named roots are reachable: a path with `..`, an absolute path, or a symbolic link that leads out of its root is refused with 403 (`internal/diskfs`).
