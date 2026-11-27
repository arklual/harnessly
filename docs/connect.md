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

1. Download `harnessly-connect_<os>_<arch>` from the `connect-v*` release and start `connect` (`connect.exe`).
2. In harnessly run `harnessly connect code`: a one-time code, good for 30 minutes.
3. In Connect's window put the harnessly address and the code. Connect stays in the tray and starts at log-in.

Without a desktop (a Linux server) use `connect pair --url https://… hx_…`, then `connect run` or
`connect autostart on` (a systemd user service).

The tray shows the state and how many of the agents' commands run. **Отключить** kills every command of the agents
and drops the link; **Подключить** brings it back without a new code. The journal (commands and every file the agents
changed, 50 MB, then rotated) opens from the tray.

Windows needs Git for Windows: the agent's shell there is Git Bash, as for Claude Code itself.

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
