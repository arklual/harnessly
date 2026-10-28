# The safe

The safe keeps the tokens, passwords and keys a person hands to the agents of a machine, so that a secret never
travels through a conversation: not typed into the chat, not into a `!` command, not into the agent's context.

## How it works

```
agent: harnessly secret ask github-token --why "push the release branch"
   │  POST /api/cli/secrets/ask (cli-token)            the agent waits (up to 100 s, then "waiting": ask again)
   ▼
runtime ── GET <platform>/secret-asks ──▶ the person's page polls it every 3 s and opens «Положить в сейф»
   ▲                                       (a password field, the agent's reason, the conversation's title)
   │  PUT <platform>/secrets/github-token {value}      page session, same origin only
   ▼
safe.json (sealed) ── the ask settles ──▶ agent prints "ready: github-token"

agent: harnessly secret run --env GITHUB_TOKEN=github-token -- gh pr create --fill
   │  POST /api/cli/secrets/use → the value goes into the child's environment only;
   ▼  the child's stdout/stderr come back with the value replaced by ••••••
```

- **Storage.** `<data dir>/secrets/safe.json` (0600) holds each value sealed with XChaCha20-Poly1305 under a 32-byte
  installation key, with the secret's name bound in as associated data. The key is a separate file, by default
  `$XDG_STATE_HOME/harnessly/safe.key` (`~/.local/state/harnessly/safe.key`, 0600 in a 0700 folder): outside the
  runtime's data folder, which every agent reads for its CLI token, and outside every workspace.
  `RUNTIME_SECRETS_KEY_FILE` moves it (for example onto a volume only the runtime mounts). The page's file viewer refuses
  `*.key` files like `.ssh`. A self-hosted runtime needs
  nothing else: no Vault, no network.
- **Ways in.** Only the person's page puts a value: Settings → Safe (add, replace, remove) or the ask dialog. These
  routes take a page session and refuse a request from another site.
- **Ways out.** Only two CLI routes return a value, both under the runtime's cli-token: `use` (for
  `harnessly secret run`, which puts it into the child's environment or a temporary 0600 file it removes after the
  command) and `file` (the runtime writes a 0600 file under `/dev/shm/harnessly-safe-<uid>/`, removed after 15 minutes
  by default, 2 hours at most, and on the runtime's restart). There is no command that prints a value.
- **Never carried:** the lists (page and CLI), the asks, the log lines (names, conversations and outcomes only), the
  page's state after a send (the field is cleared the moment the value leaves).
- **The agents are told** in their system prompt (a block after the workspace prompt) to ask with `secret ask`, to use
  `secret run`/`secret file`, never to ask for a secret in the chat and never to print one.

## The password vault is something else

`internal/vault` (`POST /api/cli/vault`) is a different, older feature: an agent asks the person's page to log in on
a website with the browser's password manager, and only a status comes back. Its page side (`/api/platform/vault`)
lives in the cluster, shows only with `RUNTIME_WEB_FEATURES=vault` (off by default), and its requests travel over the
sync socket, which the new interface no longer keeps — that is the "switched off" safe. The secrets safe does not depend
on it or on that flag: it is on in every runtime, and its page polls. It reuses the vault's per-key rate limiter.

## Threat model

What it protects against:

- a secret landing in the conversation, the transcript JSONL, the model's context, the runtime's log or a screenshot
  of the chat: the value never passes through any of them, and a command run with `secret run` has the value masked in
  its output, so `printenv`, `curl -v` or an error message that echoes a token does not bring it back into the context;
- a secret lying in the clear on disk: the safe's file is sealed; a backup or a copy of the data folder without the key
  file reveals nothing;
- another site's page using the person's session to put or remove secrets.

What it does **not** protect against, by design of v1:

- **an agent that wants the value.** The agents run as the same Unix user as the runtime and hold the cli-token: one
  that decides to can read the value (`secret run -- sh -c 'echo $T | base64'`, or reading the key file). The safe
  keeps honest agents from leaking secrets by accident; it is not a sandbox against a hostile one. Real isolation needs
  the runtime (and the key) under a different user than the agents, or a broker that performs the authenticated
  request itself — see the next steps;
- masking is byte-exact: a value transformed by the command (base64, URL-encoded, split across lines) is not masked;
  values shorter than 4 bytes are not masked at all;
- root on the machine, or someone with the person's page session.

## Rotation

Replace in Settings → Safe (the uses are kept), or an agent asks with `--replace` when the kept value stopped working.
Removing a secret is immediate; a file already handed out lives until its time is up.

## Next steps (not in v1)

- Sync between a person's machines through the hub: the hub keeps the values in OpenBao under the
  person's path (`kv/harnessly/<user>/<name>`), the machine fetches one by name over the machine agent's link when a
  command uses it, and Settings → Safe shows where each secret lives. The local safe stays the only store of a
  self-hosted runtime.
- Telegram notification of a waiting ask (the shared bot) when the page is closed.
- The runtime under its own user with the key readable only by it, and per-secret scopes (which conversations or
  folders may use a secret), with an approval prompt for a first use.
