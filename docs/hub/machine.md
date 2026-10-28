# A machine of a harnessly hub

A machine is a computer (or a container) that runs two programs: the **runtime**, which serves the web page and starts
terminals and coding agents, and the **agent** (`harnessly-agent`), which holds one outgoing, signed WebSocket to the
hub. The hub never connects in: it asks the machine for things over that link and carries a person's browser to the
runtime through it. How the link, the operations and the files are specified is in [api.md](api.md); the reasons are in
[architecture.md](architecture.md). This page is what a person who runs a machine needs to know.

- [Connect a machine](#connect-a-machine)
- [Profiles](#profiles)
- [What the hub can and cannot do to the machine](#what-the-hub-can-and-cannot-do-to-the-machine)
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
choice outside the operations above, never touches an account other than its own, and never stores or logs a key or
secret anywhere but the 0600 env files. `env.deliver` takes only names that look like secrets (`…_TOKEN`, `…_KEY`,
`…_PASSWORD`…) and refuses anything that would change how a shell, git, a pager, a package manager or an interpreter
behaves, and every address (`…_URL`, `…_HOST`) ([api.md](api.md)).

## Files

| Path | Mode | What |
| --- | --- | --- |
| `$AGENT_DATA_DIR/machine.json` | 0600 | the machine's id, the hub's address and the two private keys (Ed25519 for signing, X25519 for sealed keys). Whoever has it is the machine: do not copy it |
| `$AGENT_DATA_DIR/agent-env/all.env`, `claude.env`, `codex.env` | 0600 | `KEY='value'` lines for agent launches: keys (`claude.env`, `codex.env`), secrets and the proxy (`all.env` or an agent's own file) |
| `$AGENT_DATA_DIR/agent-env/proxy.json` | 0600 | the proxy with its credentials |
| `$AGENT_DATA_DIR/agent-audit.log` | 0600 | one JSON line per change the hub made, never a secret |
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
| `AGENT_AUTH=hs256` | run the closed-cluster way, without registering |

To apply a policy to a container machine, mount a file at `/etc/harnessly/policy.toml`.

## Upgrade, move and remove

- **Updates by themselves**: the runtime compares its programs (`harnessly-runtime`, `harnessly-agent`, the `harnessly`
  command) with the hub's `/dl/manifest.json` two minutes after a start and then every hour. Where it may write the
  folder they are in (the `full` profile, `~/.local/bin`) it downloads what differs, checks it against the manifest's
  SHA-256, puts it in place with a rename and restarts; tmux and the agents in it keep working. Settings → General →
  Updates shows the state and has «Update now». `RUNTIME_AUTO_UPDATE=0` in `runtime.env` leaves it to the button. The
  `dir` profile (`/usr/local/bin`, read-only to the services) and a container cannot update themselves: Settings shows
  the command below.
- **Upgrade by hand**: `curl -fsSL https://hub.example.com/install | sh -s -- --update` (`sudo sh` for `dir`): no code,
  the profile it was installed with, the same machine. Running the install command again works too. It
  replaces the programs, keeps the policy, the ports and the machine, and restarts the services. A machine that is
  registered with this hub (a `machine.json` of the same hub and profile) stays one machine in the hub: nothing is sent
  to the hub and no second record appears. A running agent, tmux and what is in it survive the runtime's restart.
- **Move or re-register** a machine: run the install command with a new code and `--re-register` (or
  `harnessly-agent register --force …` and a restart). The old record stays in the hub until you remove it there.
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
