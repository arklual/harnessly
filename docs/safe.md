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
without replacing the encrypted value. Creating a secret from a project chat automatically restricts it to that project;
this changes the old globally available default for newly created entries. Existing entries keep their scopes.
Settings → Safe edits sites and project IDs independently of value rotation.

Origins are exact scheme/host/port tuples: HTTPS, or HTTP only on `localhost`, `127.0.0.1` and `[::1]`. Default ports and
host case are canonicalized. Paths, credentials, queries, wildcard domains and HTTP on other hosts are refused. A
subdomain, another port or another scheme does not inherit permission. Use
`harnessly secret ask NAME --origin https://example.com --why "sign in"` to request a website entry outside the chat.

## Browser sign-in

Use `browser_fill_secret {id, secret}` for an existing entry, or `browser_request_secret {name, origin, why}` to open
the same safe card without a shell. Entries without allowed origins cannot be filled. The runtime checks the field's
`securityOrigin` from `Page.getFrameTree`, not JavaScript and not the top-level URL. Consequently, an allowed iframe
embedded on a different site is permitted deliberately; a foreign iframe on an allowed site is refused.

Only visible, enabled, writable `input` elements of type password, text, email, tel or number are eligible, with a
nonzero rectangle that intersects the area below/right of the page's top-left edge. One isolated-world renderer task
checks its origin and field, disables autocomplete, focuses and verifies the shadow-root focus chain, selects, and
uses native `execCommand('insertText')`. Values are passed only as CDP arguments. A partial/sanitized insertion is
cleared in that same task and refused; no alternate input path is used. Embedding frames must be visible, but moving
focus in an embedding document cannot redirect this document's insertion. Controlled input handlers receive native
input events. The safe records the chat and field origin in `UsedBy`. TOTP is calculated locally using RFC 6238.

An agent evaluation permanently marks the tab and its live opener-connected component `Scripted`. Such tabs cannot
receive safe input; the panel always warns the person, even while the agent holds control. `browser_open` creates an
independent browsing context group. Popups inherit their opener's chat, control and scripted state, including late discovery.
An evaluation is refused while any live opener-connected tab is person-controlled, even if the target popup still
belongs to the agent. Independent tabs remain usable. This prevents a new sibling evaluation from reading a personal
page before handoff; scripts installed before takeover remain subject to the warning and limitations below.

The runtime persists a provisional seal and reserves masks before sending the value. Known rejection restores the
previous seal and removes that attempt's masks; an uncertain CDP failure remains sealed. Tool calls within a chat
and returns of person control share a conversation gate through seal checking, dispatch and output. Taking control
does not wait for that gate: the next agent CDP call refuses. Returning control cancels the current chat tool first,
then waits for its gate before inspecting; the agent receives an interruption error and can retry. Seal release
also compares the checked generation before clearing tracking. While any tab of the chat is sealed, evaluate, console,
network and screenshots refuse on every tab of that chat. Other chats remain usable. Snapshots of a tracked sealed tab omit all editable values and their text
children. Control/Meta/Alt shortcuts and Shift+Insert/Delete are refused before dispatch; ordinary keys still work.

A new main document, a new field document, or removal of every tracked field can release a seal. Empty fields do not.
Before release, `Page.resetNavigationHistory` must succeed and console/network/request recordings are cleared.
Back/forward cannot restore the old input document. Read tools run only after this check. A seal without reliable
field references (person handoff or restart) also refuses snapshots and needs a new main document. Failure to read
an initial document marker cannot be mistaken for proof of navigation.

Returning any person-controlled tab to the agent first resets history and scans all frames and shadow roots in
isolated worlds using native getters. A nonempty input is secret if its type is password, its computed
`-webkit-text-security` is not none, its autocomplete includes current-password, new-password, one-time-code,
cc-number or cc-csc, or its name, id, aria-label or placeholder matches `pass|pwd|парол|secret|otp|token|\bpin\b|cvv|cvc`
(case insensitive). Such fields seal the document at any length; only values of at least four characters enter the
mask, so partially typed single letters do not corrupt unrelated output. During a login request, any other nonempty
text, email, tel, url, search or number input also seals the document, but its value never leaves the page for masking.
An ordinary field without these hints, left after ordinary person control rather than a login request, remains readable:
the person has chosen to leave that content to the agent. This is an explicit classification policy, not a claim to
recognize arbitrary passwords.

Keyboard, text, paste and screen-keyboard input persist `PersonInput` before dispatch, even without taking control.
Before the next chat tool, every dirty agent-controlled tab receives the same history reset and scan. A successful
scan clears the flag; an incomplete scan keeps it and seals the tab, refusing the tool. A failed history reset or
registry write during an explicit handoff keeps person control and the login pending. The runtime persists the seal
before returning control and settling login completion. These untracked seals require main-document navigation.
Mask values stay only in memory. Registry URL/title restoration is disabled for secret-touched tabs so page metadata
does not persist secret bytes in `tabs.json`. Cookies remain in the browser profile, never in the safe.

For CAPTCHA, 2FA and account-provider sign-in, `browser_request_login {url, why}` opens a person-controlled tab and a
login card. The browser panel offers Done and Cancel. The call waits up to 55 seconds, then returns `waiting`; repeat it
to continue the same request. Returning control also means Done; closing the tab cancels. Other tabs remain usable.
The response contains only status and current URL. Login cookies are shared by chats authorized for that browser profile.

### Remaining browser threats

The tool boundary prevents agent-installed page hooks before filling, reads through sibling tabs while sealed,
clipboard shortcuts, partial editable-value snapshots, and back/forward restoration after release. It does not make
an allowed website trustworthy or isolate processes sharing the same Unix account:

- The allowed site receives the value and may retain or send it. Site-owned copies placed in the DOM can remain
  visible to snapshots in transformations not recognized by byte masking. Literal, JSON, URL/form, component, common
  base64 and HTML forms are masked; arbitrary encodings, fragments and visual representations are not guaranteed.
- An agent-installed Service Worker can survive its original scripted tab if the agent can host a worker script on
  that origin. Granting a secret to an origin where the agent can publish code gives that agent access through the
  website itself. A fresh browsing context group does not clear origin-scoped workers or storage.
- GET forms place values into Chrome URL/history records; masking tool output and resetting the navigation stack do
  not erase the browser's history database. Do not treat profile files as a secret-free archive. Disabling field
  autocomplete prevents the tested text-form restoration/autofill writes, not arbitrary site storage or GET URLs.
- Lazy input inspection runs before a tool, not continuously. A tool already past its seal check when the person
  types the first character may still read or act before the next check. Typing does not automatically take control;
  use Take control when working alongside a running agent. A value submitted and removed before any scan cannot be
  collected for masking or establish a content-based seal, although the next scan still resets history. A site that
  copies and clears the field before the first tool can leave that copy readable through evaluate. A manually
  submitted GET form can likewise expose an unmasked value in the current URL through browser_tabs; resetting
  history does not scrub the current URL. These cases do not require an agent tool to be running during typing.
- Ordinary inputs without secret hints outside a login request are intentionally readable after handoff. Custom
  editors and secrets represented outside input elements are not classified. Short values seal but are not masked.
- Ignoring the scripted-tab warning and entering a password by hand can expose it to an earlier agent-installed
  hook. Manual login requests always open a fresh tab, but taking over an existing scripted tab does not remove hooks.
- After a permitted release, copies retained by the website may be accessible again, subject to output masking.
  Removing a field or navigating is a workflow transition, not proof that the website destroyed all copies.
- The agent's shell and cli-token can obtain an allowed entry through `secret run` and transform it to bypass output
  masking. Project scopes apply to those API reads, but before stage 5 the same Unix user can also read the safe key,
  profile files or private CDP socket directly. Masking values live only for the runtime's lifetime.

Stage 5 supplies a separate process/filesystem boundary. It does not make an agent-writable or malicious allowed
origin safe or recover manual values already missed by a lazy scan. Site-origin approval adds a site without replacing
a value; an explicit `--replace` still requests rotation. An accessible unscoped entry can receive a site approval even from a chat without a project.
