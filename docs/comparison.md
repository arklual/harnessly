# harnessly and similar tools

This page lists what harnessly and five similar tools have, taken from their own READMEs and docs. A `?` means the
project's public docs do not say, and we did not guess.

Pick harnessly when your agents should run on a server you control and you want to reach them from a browser: Claude
Code and Codex run as the real CLIs in tmux, keep running when nobody has a page open, and an optional self-hosted hub
gives a team sign-in, spaces and machines behind one address, with no inbound port on the machine. Pick something else
when it fits better. If you want a phone remote for one Claude Code session on your laptop, the built-in Remote Control
is enough and needs nothing to install. If you want native desktop and mobile apps, many parallel worktrees, or agents
other than Claude Code and Codex, look at Orca, Paseo, T3 Code or Happy. harnessly has none of those apps and runs only
Claude Code, Codex and plain shells.

## Table

| | harnessly | Happy | Paseo | T3 Code | Orca | Claude Code Remote Control |
| --- | --- | --- | --- | --- | --- | --- |
| License | Apache-2.0 | MIT | Apache-2.0 | MIT | MIT | Proprietary, not open source |
| Where the agent runs | Your machine or server, in tmux | Your machine | Your machine or server (a daemon) | Your machine or server (`t3`) | Your machine; a remote box over SSH or `orca serve` (beta) | Your machine |
| Agents | Claude Code, Codex, plain shells | Claude, Codex, Grok, Gemini (via Antigravity), other ACP agents (beta) | Claude Code, Codex, Copilot, OpenCode, Pi, Antigravity, Muse Code | Claude Code, Codex, Cursor, Grok Build, OpenCode, Antigravity, Pi, Muse Code | Any CLI agent: Claude Code, Codex, OpenCode, Pi and others | Claude Code only |
| Clients | Web UI served by the runtime; web push. No native desktop or mobile app | iOS and Android apps, web, desktop app | Desktop app, iOS and Android apps, web, CLI | Desktop app, iOS and Android apps, web (app.t3.codes and local) | Desktop app, iOS and Android companion apps. Web UI: ? | claude.ai/code in a browser, Claude iOS and Android apps, Claude Desktop |
| Self-hostable server or relay | Yes: the runtime alone, or the optional hub | Yes: relay server in the repo (Docker). A hosted relay is the default | Yes: daemon (npm or Docker with web UI). Relay is optional; running your own: ? | Yes: `t3 serve`. T3 Connect relay is run by the project | Yes: `orca serve` (beta). Orca Relay for phone pairing: self-hosting ? | No. Traffic goes through the Anthropic API |
| Reaching a remote machine | With the hub the machine dials out; no inbound port. Without it, you expose the runtime port yourself (LAN, VPN, reverse proxy) | Through the relay. Inbound port: ? | Relay, or direct TCP, Tailscale, SSH. Inbound port: ? | T3 Connect (account, no router forwarding), or LAN, Tailscale, SSH | Orca Relay (needs sign-in), or LAN, Tailscale, SSH | Outbound HTTPS only; no inbound port |
| Several people | Hub: accounts, spaces, owner and member roles, invites. The runtime is single-person: members who open a machine see the same tabs | Invite someone into a session; a team server is described on the site (details: ?) | ? (one daemon, optional password) | Guest links (beta): a guest follows one thread, you approve each message. No accounts or spaces documented | ? (several paired devices per server, each with its own token) | Sessions belong to your account; on Team and Enterprise an owner switches it on. Sharing a session: ? |
| Terminal | Yes, in the browser | In the original CLI, in your terminal. In-app terminal: ? | Yes | Yes | Yes | ? (the web and phone view shows the conversation) |
| Diff, files, forges | Changed files and diffs, file editor, per-turn git checkpoints, GitHub and GitLab panel | ? | Diffs, file editing, pull requests | Per-turn diffs, branch comparison, pull requests on GitHub, GitLab, Forgejo, Gitea, Bitbucket, Azure DevOps | Diffs with line comments sent back to the agent, file tree, GitHub and Linear | Diff pane in a git repository; `@` file completion |
| Account with the vendor | No | ? | No | Only for T3 Connect | Only for Orca Relay | claude.ai subscription (Pro, Max, Team, Enterprise); API keys are not supported |

What harnessly has that the table does not show: an on-machine speech-to-text microphone (whisper.cpp), a secrets safe
so tokens do not pass through the chat, `harnessly ask` to put a question or an approval to a person in Telegram, a
browser pane for the machine's dev servers, boards and documents beside the conversation, and a command that lets one
agent start and read another in a new tab. Usage and provider limits are shown per runtime, from that machine's
transcripts. Team-wide per-person cost reports are not part of the open core.

What harnessly lacks, besides the apps and agents above: Windows is not supported (release binaries are for Linux and
macOS, and agents need tmux), the hub runs as one replica, and versions are `0.x`, so a minor version may change the API.

## Notes

**Happy** ([slopus/happy](https://github.com/slopus/happy)). Mobile, web and desktop clients for Claude, Codex and
other agents, with end-to-end encrypted sync through a relay. The relay server is open source and runs in one Docker
container; the default is the project's hosted relay. It is built around the phone as a remote and around sharing a
session with another person.

**Paseo** ([getpaseo/paseo](https://github.com/getpaseo/paseo)). A daemon on your machine with desktop, mobile, web and
CLI clients, parallel agents in separate worktrees, terminals, diffs and a built-in browser. It supports seven agents
and has a TypeScript SDK and plugins. The docs describe a single-user setup.

**T3 Code** ([pingdotgg/t3code](https://github.com/pingdotgg/t3code)). A desktop, web and mobile control surface for
agents installed on your machine, with usage totals across providers and many forge integrations. Remote access uses
T3 Connect (free, up to 3 machines per account) or LAN, Tailscale and SSH. The README says it is early and mostly not
accepting outside contributions.

**Orca** ([stablyai/orca](https://github.com/stablyai/orca)). A desktop app that runs any CLI agent in its own git
worktree, with a built-in browser, diff annotation, GitHub and Linear, and a phone companion. A headless `orca serve`
is in beta; the docs describe it with desktop and mobile clients and say not to forward its port to the public
internet.

**Claude Code Remote Control** ([docs](https://code.claude.com/docs/en/remote-control)). A built-in feature that opens
a Claude Code session running on your machine in claude.ai/code or the Claude app. `claude remote-control` runs a
server mode with up to 32 concurrent sessions by default. While connected, the session transcript is stored on
Anthropic's servers. Claude Code only, and it needs a claude.ai subscription.

---

Checked on 2026-10-29 against each project's public README and docs. Corrections are welcome: please open an issue.
