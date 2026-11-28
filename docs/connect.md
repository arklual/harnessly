# Harnessly Connect

Connect is an app on a person's own computer (macOS, Linux, Windows). With it, the agents of their harnessly can work
with that computer's files, shell and programs while Claude Code itself keeps running on the server under the person's
subscription. The laptop dials the server, so it needs no open port and works behind NAT.

```
laptop                                   server (runtime)
Harnessly Connect  ◄── WebSocket ─────── claude (Claude Code), in a view where /Users/masha/proj is the laptop's
  runs the agent's Bash                    Bash      → connect-bash → the laptop
  answers file operations                  Read/Edit → FUSE mount   → the laptop
  harnessly shim                           hooks, MCP, memory stay on the server
```

## On the laptop

In harnessly run `harnessly connect code`. It prints one command per system, with a one-time code inside, good for
30 minutes and for one computer:

```
macOS, Linux (терминал):
  curl -fsSL https://harnessly.example.com/connect/install/hx_… | sh

Windows (PowerShell):
  irm https://harnessly.example.com/connect/install/hx_….ps1 | iex
```

The command downloads the newest `connect-v*` release for the system, checks it against the release's
`checksums.txt`, puts Connect in the person's own folder (`~/.local/bin/harnessly-connect`,
`%LOCALAPPDATA%\Programs\HarnesslyConnect`), pairs it, turns autostart on and starts it. No administrator rights are
needed. On Windows it also installs Git for Windows with winget when there is no Git Bash. Run again with a new code,
it replaces the Connect already there.

The runtime builds the command from its own address: `RUNTIME_CONNECT_URL` (the address the laptop reaches it at),
else the host the request came to. From the CLI on the server itself give it: `harnessly connect code --url https://…`.
`RUNTIME_CONNECT_RELEASES` names another GitHub repository of releases, `RUNTIME_CONNECT_DOWNLOAD` a folder (URL) of
release files to use instead.

By hand: download `harnessly-connect_<os>_<arch>` from the release, start `connect` (`connect.exe`) and put the
harnessly address and the code in its window. Without a desktop (a Linux server): `connect pair --url https://… hx_…`,
then `connect run` or `connect autostart on` (a systemd user service).

The tray shows the state and how many of the agents' commands run. **Отключить** kills every command of the agents
and drops the link; **Подключить** brings it back without a new code. The journal (commands and every file the agents
changed, 50 MB, then rotated) opens from the tray.

Windows needs Git for Windows: the agent's shell there is Git Bash, as for Claude Code itself.

## One chat on the laptop

Any Claude chat can be switched to a computer and back, keeping its history: **«…» → «Где работает…»** in the chat's
menu, or `harnessly connect chat <chat|current> <device> <dir on it>` (`… server` puts it back,
`harnessly connect chat current` says where it runs). The runtime checks the folder on the computer, and the chat's
agent starts again there once its turn is over: its Bash and files are then the computer's, and the agent is told
that the paths of its earlier turns are the server's. Back on the server it goes on in its own folder. A computer that
is offline cannot be picked; a chat whose own folder is a device folder (`folder add`) runs there anyway.

Claude Code keeps a session under the folder it ran in: the launcher links the chat's one transcript into the new
folder's project (`~/.claude/projects/<folder>/<session>.jsonl`), so `--resume` finds it and the runtime keeps reading
the same file.

## On the server

Connect needs, next to the runtime: `connect-bash` and `connect-chat` in one folder (the linux release archive has
them), `bwrap` (bubblewrap), FUSE (`/dev/fuse`, `fusermount3`), and the person's `systemd --user`.

```sh
ln -s /path/to/connect-chat ~/.local/bin/claude-connect
# runtime.env
RUNTIME_CLAUDE_LAUNCHER=/home/me/.local/bin/claude-connect
```

Then a folder of the laptop becomes a folder chats start in:

```sh
harnessly connect folder add MacBook /Users/masha/proj      # Windows: /c/Users/masha/proj
harnessly connect folders | devices | revoke MacBook | folder rm /Users/masha/proj
```

Open that folder as a project in harnessly; every chat in it (new, resumed, forked) runs on the laptop. Every other
chat starts the plain `claude`, as before.

## How it works

- **Link.** The device has an Ed25519 key made at pairing; every dial is signed (`Harnessly-Ed25519`, the hub's own
  scheme) and the runtime keeps the public key in `<data>/connect/devices.json`. One WebSocket carries everything.
- **Commands.** Claude Code runs its Bash tool through `CLAUDE_CODE_SHELL` (`connect-bash`); hooks and MCP servers do
  not go through it. Each command runs on the device with the device's login environment and the agent's own
  variables. Claude Code's shell snapshot and the file its commands write the working directory into are carried to the
  device and back. Signals (a timeout, an interrupt) reach the command's process group.
- **Survival.** The device holds the processes and their output; a link that drops (sleep, Wi-Fi) comes back and the
  output arrives from where the runtime stopped, once and in order. A device that restarted says so and the command
  fails with that reason.
- **Files.** `connect-chat` mounts the device folder with FUSE and shows it at the device's own path in a bubblewrap
  view of the server where everything else stays where it is, then starts Claude there. The runtime runs with
  `NoNewPrivileges`, which stops the setuid `fusermount`, and Ubuntu's AppArmor gives bubblewrap's namespaces no right
  to mount, so the FUSE server runs in a transient `systemd-run --user` unit; bubblewrap only binds the mounted folder.
- **The harnessly command on the device** is Connect itself under that name, first on the commands' `PATH`. It asks the
  runtime to run the command for the chat whose command is running. `harnessly secret run` runs the command on the
  device: the values come for that one command, go into its environment only and are masked in its output.
- **Windows.** Paths are Git Bash's (`/c/Users/…`) everywhere the agent looks; `C:\…` in a Windows program's output is
  rewritten to that form, and the agent is told so in its system prompt.

## Limits

- Claude Code only; Codex and Antigravity chats in a device folder are not supported yet.
- `harnessly open ./file` and other commands that hand the runtime a path name the device's path, which the runtime
  itself cannot read.
- File operations cross the link: a big `grep -r` through the mount is slow — run it with Bash, on the device.
- Releases are not signed with Apple's or Microsoft's certificates: macOS asks to open the app from the context menu
  the first time, Windows shows SmartScreen.
