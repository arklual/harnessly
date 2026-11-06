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
- **Apps' secrets.** A secret an app asks for in its manifest is kept here too, under `app/<id>/<name>`. The slash keeps it out of the safe's list and out of `harnessly secret run` and `file`; the person puts it in from the app's «О приложении», and the runtime reveals it only to fill `${secret:<name>}` in the app's MCP server config at a launch (see [apps](apps.md#manifest)).
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

## Keeping the safe on a server

By default the safe is the file above and needs nothing. A runtime can instead keep the values on a Vault-compatible
server — [OpenBao](https://openbao.org) or HashiCorp Vault, the same API — so that they live where the organisation
already audits and rotates secrets, survive the machine and are shared by the person's machines.

```sh
RUNTIME_SECRETS_MODE=vault
RUNTIME_SECRETS_VAULT_ADDR=https://openbao.example.com:8200
RUNTIME_SECRETS_VAULT_MOUNT=secret                  # the KV engine's mount (v1 or v2, found by itself)
RUNTIME_SECRETS_VAULT_PREFIX=harnessly/users/alice  # the folder of this person's secrets: <prefix>/<name>
RUNTIME_SECRETS_VAULT_AUTH=approle                  # token-file | approle | kubernetes | jwt
RUNTIME_SECRETS_VAULT_ROLE_ID=…
RUNTIME_SECRETS_VAULT_SECRET_ID_FILE=/run/harnessly/secret-id
```

| Setting | Meaning |
| --- | --- |
| `RUNTIME_SECRETS_MODE` | `local` (default), `vault`, `hub` ([the hub's safe](#the-hubs-safe), no server settings on the machine) or `exec` ([any other store](#any-other-store)) |
| `RUNTIME_SECRETS_VAULT_ADDR` | the server's base URL |
| `RUNTIME_SECRETS_VAULT_MOUNT` | the KV mount, `secret` by default; its version is read from `sys/internal/ui/mounts`, else `…_KV_VERSION` (1 or 2), else 2 |
| `RUNTIME_SECRETS_VAULT_PREFIX` | the folder of the safe's secrets in the mount, `harnessly` by default |
| `RUNTIME_SECRETS_VAULT_NAMESPACE` | sent as `X-Vault-Namespace` (Vault Enterprise, OpenBao namespaces) |
| `RUNTIME_SECRETS_VAULT_CACERT` | a PEM file of a private CA |
| `RUNTIME_SECRETS_VAULT_AUTH` | how the runtime logs in, below |
| `RUNTIME_SECRETS_VAULT_AUTH_MOUNT` | the auth method's mount when it is not the method's name |
| `RUNTIME_SECRETS_REF_PREFIXES` | folders `vault:` references may read, comma-separated; `*` for anything the login may read; empty: no references |

The ways in, each reading its credential from a file at every login, so a credential renewed in the file is picked
up:

| `…_AUTH` | Reads | Fits |
| --- | --- | --- |
| `token-file` (default) | `…_TOKEN_FILE`: a token, kept fresh by `bao agent` / `vault agent` | any server, the simplest |
| `approle` | `…_ROLE_ID`, `…_SECRET_ID_FILE` | a service account on a VM |
| `kubernetes` | `…_ROLE`, `…_JWT_FILE` (the pod's service account token by default) | a runtime in a cluster |
| `jwt` | `…_ROLE`, `…_JWT_FILE`: an OIDC token the server trusts | a per-person login (see below) |

A token that stops working (403) is replaced by a new login once; then the call is refused as `denied`. A server
that does not answer is `unavailable`. A setting that does not work leaves the safe refusing new secrets as
`unavailable` and the page saying why: a runtime asked to use a server never quietly keeps values on the machine.

**What lies where.** The server holds `<prefix>/<name>` with the value under the key `value`. When it was put,
which conversations used it, and its limits (the projects whose chats may use it, the sites it may be typed into) stay
on the machine in `<data dir>/secrets/remote-ledger.json`, without values. A secret moved from the file takes its
limits along; losing the ledger loses them, and the secret is then open to every chat of the machine, as one put before
limits existed. Nothing
of the value touches the machine's disk; it is in the runtime's memory for the one request that hands it to a command.

**The secrets of the file** keep working after the switch: a name is looked up on the server first, then in the file;
`local:<name>` names the file's one outright. Settings → Safe shows where each secret lives. New secrets go to the
server, and a secret of the file moves there when it gets a new value.

**References.** An administrator's secret already on the server is used without copying it into the safe:

```sh
harnessly secret run --env DEPLOY_TOKEN=vault:shared/ci/deploy#token -- ./deploy.sh
```

`vault:<path>#<key>` reads the key at that path of the mount (`#value` when left out). References are read only and
only under `RUNTIME_SECRETS_REF_PREFIXES`, on top of what the server's policy allows the login.

### Two ways to lay it out

- **One account for the machine** (`approle`, `kubernetes`, `token-file`): the runtime's account may read and write
  its own prefix (and read the reference folders). Fits a machine of one person, or an organisation that gives out
  service accounts but not personal Vault access. The policy:

  ```hcl
  path "secret/data/harnessly/users/alice/*"     { capabilities = ["create", "read", "update", "delete"] }
  path "secret/metadata/harnessly/users/alice/*" { capabilities = ["list", "read", "delete"] }
  path "secret/data/shared/ci/*"                 { capabilities = ["read"] }   # references
  ```

- **A login per person** (`jwt`): the server trusts an OIDC issuer and a templated policy gives each person their own
  folder, `secret/data/harnessly/users/{{identity.entity.aliases.<accessor>.name}}/*`. The token file is whatever puts
  the person's short-lived token there.

- **The hub's account for every machine** (`RUNTIME_SECRETS_MODE=hub`): only the hub logs in, below.

### Compatibility

The client speaks a small subset that OpenBao and HashiCorp Vault answer alike (see the header of
`internal/secrets/vaultkv`): KV v1 and v2, the four logins, namespaces. CI runs it against both, in dev mode, KV v1
and v2 and AppRole with a narrow policy (`VAULTKV_TEST_ADDR`, `VAULTKV_TEST_ROOT_TOKEN`). Another secret store plugs
in through a command, [below](#any-other-store), or in code as a `secrets.Provider`.

## The hub's safe

A machine connected to a hub can keep its safe on the hub's server without any account of its own: the hub holds the
one service account, each machine reaches only its owner's folder through it. This is for an organisation that gives
service accounts but not personal Vault access, and for machines that should carry no server credential at all.

```sh
# the runtime of the machine
RUNTIME_SECRETS_MODE=hub
```

```sh
# the hub
HUB_SECRETS_VAULT_ADDR=https://openbao.example.com:8200
HUB_SECRETS_VAULT_MOUNT=secret
HUB_SECRETS_VAULT_AUTH=kubernetes          # token-file | approle | kubernetes | jwt, as for the runtime
HUB_SECRETS_VAULT_ROLE=harnessly-hub
HUB_SECRETS_VAULT_PREFIX=harnessly         # the hub's folder in the mount, harnessly by default
HUB_SECRETS_REF_PREFIXES=shared/ci         # what vault: references may read; empty: none
```

The hub takes every `HUB_SECRETS_VAULT_*` setting the runtime takes under `RUNTIME_SECRETS_VAULT_*`; without
`HUB_SECRETS_VAULT_ADDR` it keeps no safe. It is switched on per organisation by the feature flag `secrets.vault`
(Settings of the hub → Features); a machine of an organisation without it is answered `not_enabled` and its safe
refuses new secrets as `unavailable`.

A machine of a closed cluster that a hub only observes (`AGENT_HUB_OBSERVE_FILE`, [hub/machine.md](hub/machine.md))
keeps its safe at home: the observe link serves the safe only when the machine's operator opts in with
`AGENT_HUB_SAFE=on` in the agent's environment. Without it the safe in hub mode refuses as `unavailable`.

**The way of a request.** The runtime asks its machine agent on the loopback (`POST /secrets/hub`, with the agent's
local token). The agent signs the request with the machine's Ed25519 key over the hub's host and the SHA-256 of the
body (domain `harnessly-agent-secrets-v1`, a nonce used once) and posts it to the hub's `POST /agent/secrets`. The hub
checks the signature and that the machine is neither revoked nor switched off, then reads or writes:

```text
<prefix>/users/<owner id>/<name>   the secrets of the person who registered the machine, the value under "value"
```

The folder comes from the hub's own record of the machine, never from the request: a name is one segment of letters,
digits, `.`, `_` and `-`, so a machine cannot leave its owner's folder. Two machines of one person share the folder;
two people never do. A value read comes back sealed to the machine's X25519 key (in parts of 8 KiB, each bound to the
nonce of the request and to its place), so it is in the clear only in the hub's memory and in the runtime's.

**References** (`vault:<path>#<key>`) go through the hub as well and are read only under `HUB_SECRETS_REF_PREFIXES`,
never under `<prefix>/users`, whatever the prefixes say.

**The journal.** Every read, write, removal and reference is an event of the space's audit journal (`secret.read`,
`secret.write`, `secret.delete`, `secret.ref`): who, which machine, which name, whether it was refused. Never a value.

The hub's policy on the server:

```hcl
path "secret/data/harnessly/users/*"     { capabilities = ["create", "read", "update", "delete"] }
path "secret/metadata/harnessly/users/*" { capabilities = ["list", "read", "delete"] }
path "secret/data/shared/ci/*"           { capabilities = ["read"] }   # references
```

## Any other store

A store harnessly does not speak (a cloud's secret manager, 1Password, `pass`, a company service) plugs in through a
command of your own, without a new build:

```sh
RUNTIME_SECRETS_MODE=exec
RUNTIME_SECRETS_EXEC=/opt/harnessly/provider-acme --region eu   # run as is, split at spaces, no shell
RUNTIME_SECRETS_EXEC_PREFIX=harnessly                           # the safe's folder, harnessly by default
RUNTIME_SECRETS_REF_PREFIXES=shared/ci                          # what vault: references may read
```

The runtime runs the command once per call, writes one request as JSON to its stdin and reads one answer from its
stdout, within 20 seconds. The protocol is `harnessly.secrets-provider/v1`:

| Request (`"protocol":"harnessly.secrets-provider/v1"` in each) | Answer |
| --- | --- |
| `{"op":"read","path":"harnessly/gh-token"}` | `{"data":{"value":"…"}}` |
| `{"op":"write","path":"harnessly/gh-token","data":{"value":"…"}}` | `{}` |
| `{"op":"delete","path":"harnessly/gh-token"}` | `{}` |
| `{"op":"list","path":"harnessly"}` | `{"keys":["gh-token","team/"]}`, a folder ending in `/` |
| any, refused | `{"error":{"code":"not_found","message":"…"}}`: `not_found`, `denied`, `unavailable`, `too_large`, `failed` |

Everything else works as with a server: the ledger, the secrets of the file, references (`vault:<path>#<key>` reads
`path` and takes `key` from its data). The page shows the command's name only, never its arguments. Its stderr is
never shown or logged, as it may hold what the store said about a value; the error's `message` is shown, so a
provider never puts a value there. A command that cannot be run, prints something that is not an answer or exits
without one is refused as `unavailable` or `failed`, and the value is not kept on the machine instead.

A provider is a few lines in any language. A sketch over a folder of files, for a test:

```python
#!/usr/bin/env python3
import json, os, sys
req = json.load(sys.stdin)
root = os.environ.get("PROVIDER_DIR", "/var/lib/provider")
path = os.path.join(root, req["path"])
def answer(v): json.dump(v, sys.stdout)
if req["op"] == "read":
    if not os.path.exists(path): answer({"error": {"code": "not_found"}})
    else: answer({"data": json.load(open(path))})
elif req["op"] == "write":
    os.makedirs(os.path.dirname(path), exist_ok=True); json.dump(req["data"], open(path, "w")); answer({})
elif req["op"] == "delete":
    if os.path.exists(path): os.remove(path)
    answer({})
elif req["op"] == "list":
    keys = [n + "/" if os.path.isdir(os.path.join(path, n)) else n for n in os.listdir(path)] if os.path.isdir(path) else []
    answer({"keys": keys})
```

The command runs with the runtime's environment and as its user, so it reads its own credentials (a token file, a
workload identity) the way the store wants. The package `internal/secrets/execkv` is the runtime's side.

## Rotation

Replace in Settings → Safe (the uses are kept), or an agent asks with `--replace` when the kept value stopped working.
Removing a secret is immediate; a file already handed out lives until its time is up.

## Next steps

- Per-person tokens the hub issues for the `jwt` login, so a person's machine logs in to the server as the person
  without a Vault account of their own; folders a space shares in the hub's safe.
- Moving the file's secrets to the server in one go (today a secret moves when it gets a new value).
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

| How input arrives | Tool protection | How it ends |
| --- | --- | --- |
| Safe filling | Exact field origin, atomic input, masks and a chat-wide seal | New main/field document or removal of the tracked field; history reset must succeed |
| Login request | Fresh person-controlled tab; return scans fields, resets history and seals populated login fields | A handoff seal needs a new main document |
| Taking control | Agent commands refuse; return scans secret-like text fields and resets history | Ordinary content remains visible to snapshots; a handoff seal needs a new main document |
| Typing without takeover | Lazy field scan before the next chat tool; evaluate is blocked on the typed document and its live opener component | A different main document releases the evaluate guard, including navigation to the same address |

Remaining limitations:

- GET forms put values in the current URL and Chrome history. A manually submitted value can reach chrome_tabs
  before any scan collected it for masking; resetting navigation history does not scrub that URL or the history database.
- The allowed site receives the value and can store or send it. Its DOM copies may remain visible in snapshots or
  other permitted output, including arbitrary transformations that byte masking cannot recognize.
- Ignoring the Scripted warning can expose manual input to an agent-installed hook already running on that page.
- A tool already executing when the person types the first character may have passed its checks. Typing does not
  automatically take control; use Take control when working alongside a running agent.
- An agent-installed Service Worker can survive a fresh tab on an origin where the agent can publish code. Granting
  a secret to such an origin gives the agent access through the site itself.
- Before the wall, shell access and the same Unix account can bypass tool guards: secret run can retrieve a
  project-authorized entry; the safe key, browser files and private CDP remain accessible to that account.

Use `chrome_fill_secret {id, secret}` for an existing entry, or `chrome_request_secret {name, origin, why}` to open
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
receive safe input; the panel always warns the person, even while the agent holds control. `chrome_open` creates an
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
isolated worlds using native getters. A nonempty password input is secret. Only text, email, tel, url, search and
number inputs are otherwise classified: their computed `-webkit-text-security` is not none, their autocomplete includes
current-password, new-password, one-time-code,
cc-number or cc-csc, or their name, id, aria-label or placeholder matches `pass|pwd|парол|secret|otp|token|\bpin\b|cvv|cvc`
(case insensitive). Such fields seal the document at any length; only values of at least four characters enter the
mask, so partially typed single letters do not corrupt unrelated output. During a login request, any other nonempty
text, email, tel, url, search or number input also seals the document, but its value never leaves the page for masking.
All other input types are ignored before reading their values, including hidden tokens, checkbox values and buttons.
The scan never compares value with defaultValue: controlled frameworks can synchronize both for a shown password.
An ordinary field without these hints, left outside a login request, stays visible to snapshots. This classification
policy does not claim to recognize arbitrary passwords or custom editors.

Keyboard, text, paste and screen-keyboard input persist `PersonInput` before dispatch, even without taking control.
Each dispatch also reads the main frame's loader through Page.getFrameTree, with a 500 ms limit, and persists
PersonDocument only when it changes. A failed read records an unknown-document marker without preventing input.
Before evaluate, every live tab in the target's opener component is checked. A matching loader refuses JavaScript
without setting Scripted; an unknown marker is anchored to the current loader and also refuses. The marker survives
runtime restart. pushState does not change the loader; navigating to the same address does. Mouse input does not set
this marker, and independent tabs are unaffected. This guard applies only to evaluate: without a separate seal,
snapshots, screenshots, console, network and actions stay available. It also covers site copies after a field clears
and human typing in custom editors outside input elements.
Before the next chat tool, every dirty agent-controlled tab receives the same history reset and scan. A successful
scan clears the flag; an incomplete scan keeps it and seals the tab, refusing the tool. A failed history reset or
registry write during an explicit handoff keeps person control and the login pending. The runtime persists the seal
before returning control and settling login completion. These untracked seals require main-document navigation.
Mask values stay only in memory. Registry URL/title restoration is disabled for secret-touched tabs so page metadata
does not persist secret bytes in `tabs.json`. Cookies remain in the browser profile, never in the safe.

For CAPTCHA, 2FA and account-provider sign-in, `chrome_request_login {url, why}` opens a person-controlled tab and a
login card. The browser panel offers Done and Cancel. The call waits up to 55 seconds, then returns `waiting`; repeat it
to continue the same request. Returning control also means Done; closing the tab cancels. Other tabs remain usable.
The response contains only status and current URL. Login cookies are shared by chats authorized for that browser profile.

Byte masking covers literal, JSON, URL/form, component, common base64 and HTML forms, not arbitrary transformations.
Mask values live only for the runtime's lifetime. Disabling autocomplete prevents the tested text-form restoration
and autofill writes; it does not prevent site storage or GET URLs. Stage 5 adds a process/filesystem boundary, but does
not make a malicious or agent-writable allowed origin safe or recover manually submitted values missed by a scan.
Site-origin approval adds a site without replacing the value; explicit --replace requests rotation. An accessible
unscoped entry can receive a site approval even from a chat without a project.
