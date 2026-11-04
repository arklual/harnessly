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

What it does **not** protect against:

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

## Next steps

- Sync between a person's machines through the hub: the hub keeps the values in OpenBao under the
  person's path (`kv/harnessly/<user>/<name>`), the machine fetches one by name over the machine agent's link when a
  command uses it, and Settings → Safe shows where each secret lives. The local safe stays the only store of a
  self-hosted runtime.
- Telegram notification of a waiting ask (the shared bot) when the page is closed.
- The runtime under its own user with the key readable only by it (stage 5). Project scopes and access cards are
  implemented locally; they do not isolate agents that can read the same Unix files.

## Website origins and project access

Each entry can carry public `Origins` and `Projects` lists. Old `safe.json` files remain readable: empty lists retain
legacy CLI access. A nonempty project list restricts `secret list`, `secret run`, `secret file` and browser filling.
The runtime resolves the chat from the calling connection and reads its current project; an unknown connection or a
claimed session belonging to another chat cannot use a scoped entry. A foreign entry and an absent entry have the same
use refusal. An ask for an existing foreign entry displays an access grant, never a value field. Approving adds access
without replacing the encrypted value. Settings → Safe edits sites and project IDs independently of value rotation.

Origins are exact scheme/host/port tuples: HTTPS, or HTTP only on `localhost`, `127.0.0.1` and `[::1]`. Default ports and
host case are canonicalized. Paths, credentials, queries, wildcard domains and HTTP on other hosts are refused. A
subdomain, another port or another scheme does not inherit permission. Use
`harnessly secret ask NAME --origin https://example.com --why "sign in"` to request a website entry outside the chat.

## Browser sign-in

Use `browser_fill_secret {id, secret}` for an existing entry, or `browser_request_secret {name, origin, why}` to open
the same safe card without a shell. Entries without allowed origins cannot be filled. The runtime checks the field's
`securityOrigin` from `Page.getFrameTree`, not JavaScript and not the top-level URL. Consequently, an allowed iframe
embedded on a different site is permitted deliberately; a foreign iframe on an allowed site is refused.

Only visible, enabled, writable `input` elements of type password, text, email, tel or number are eligible. The runtime
focuses the field, checks the active element in an isolated world (including shadow roots and every embedding frame),
then uses `Input.insertText`.
A focus handler that redirects input is refused. The safe records the chat and frame origin in `UsedBy`. A TOTP URI
(`otpauth://totp/...`) produces the current code locally using RFC 6238, including SHA-1, SHA-256 and SHA-512; neither the
URI nor the code appears in a successful tool response.

Before insertion, the value joins in-memory masking and the tab is sealed. Evaluate, console, network and screenshot
tools refuse while sealed. Clicks, typing, keys and masked accessibility snapshots remain available. A new main-frame
document, or removal/clearing of every filled field, releases the seal. Seal checks execute private-world DOM code only;
a refused evaluation never executes the agent's page expression. A durable document marker survives runtime restarts.
After a restart, lost node references require navigation to release the seal; snapshots also fail closed until then.
The runtime does not persist masking values. Registry URL/title restoration is disabled for tabs that received a secret,
so page-controlled metadata cannot write that secret into `tabs.json`. Cookies remain in the browser profile, not the safe.

For CAPTCHA, 2FA and account-provider sign-in, `browser_request_login {url, why}` opens a person-controlled tab and a
login card. The browser panel offers Done and Cancel. The call waits up to 55 seconds, then returns `waiting`; repeat it
to continue the same request. Returning control also means Done; closing the tab cancels. Other tabs remain usable.
The response contains only status and current URL. Login cookies are shared by chats authorized for that browser profile.

### Remaining browser threats

The seal restricts specific reading tools; it is **not** an information-flow boundary against a hostile website or an
agent deliberately extracting secrets. In particular:

- Masking is byte-based (with common JSON/URL escaping also covered), not semantic. A site can expose base64, character
  codes, fragments, transformed DOM text or visual pixels. Accessibility snapshots remain allowed while sealed, so
  transformed text in the DOM can escape. Once unsealed, screenshots can show copies rendered outside password fields.
- Sealing applies to one tab. A same-origin popup with an `opener` reference can read the sealed opener through
  its own evaluation tool, and encoding defeats byte masking. An agent can also install page listeners before the
  first fill. Keeping other tabs usable is not equivalent to isolating JavaScript window references.
- Clearing/removing an input says nothing about other copies: the site can retain the value in JavaScript, another
  frame, storage, a response or its server. Unsealing after this change or after navigation is a usability rule, not
  proof that all copies disappeared. Encoded console/network history may remain readable after unsealing.
- A page can move focus or navigate asynchronously between CDP commands. The final isolated-world focus and origin
  checks reduce this window but do not make validation and `Input.insertText` atomic. The allowed site receives input
  events and can copy or transmit the value. Browser input is not a transaction spanning all renderer tasks.
- The agent's shell and cli-token can still obtain an allowed entry through `secret run` and transform it to bypass
  output masking. Project restrictions apply to those API reads, but before stage 5 a same-user agent can also read the
  safe key, browser profile or private CDP socket directly. Hiding a token or sealing a tool does not isolate Unix files.
- Masking values live only for the runtime's lifetime. A persisted seal fails closed on restart, but previously
  unsealed pages and their stored copies are not a durable redaction system. The browser itself may persist what a site
  receives as input; the safe cannot prevent that.

Stage 5's process/filesystem boundary is separate work. It will not, by itself, make a malicious allowed site trustworthy.
