# Hub contract

Everything the hub, the machine agent and the hub UI agree on. When code and this file disagree one of them is wrong:
change both in the same commit. Background: [architecture.md](architecture.md); the machine side in
[machine.md](machine.md); running a hub in [deploy.md](deploy.md).

Conventions:

- JSON in and out, UTF-8, `camelCase` keys. Times are RFC 3339 strings in UTC (`"2026-10-26T09:00:00Z"`) on the hub
  API, epoch milliseconds (`number`) on the agent link. Sizes are bytes (`number`).
- Ids are 20 characters `[a-z2-7]`. `jobId` and `loginId` are 32 lowercase hex characters made by the hub.
- `?` in a type means optional (may be absent); `| null` means present and maybe null.
- Human texts in errors are Russian (the UI may show them as is); the UI decides by `error`, never by `message`.

## Hub HTTP API

### Errors

Every non-2xx answer of `/api/*` and `/agent/*` is

```json
{ "error": "invalid_code", "message": "Неверный код", "details": { "attemptsLeft": 2 } }
```

| HTTP | `error` | When |
| --- | --- | --- |
| 400 | `invalid_request` | malformed JSON, missing or bad field (`details.field`) |
| 400 | `invalid_code` | wrong email code (`details.attemptsLeft`) |
| 401 | `unauthenticated` | no or expired hub session |
| 403 | `forbidden` | signed in, not allowed (not a member, member not owner) |
| 403 | `cross_site` | a change request failed the same-origin check or is not `application/json`; also a request to `/m/<machine>/…` that a page of another origin made (anything but the navigation of a whole page) or a frame of another origin |
| 403 | `signup_closed` | `HUB_SIGNUP=invite` and no account or invite for this email (an invitation addressed to an email counts only once that address is proved) |
| 403 | `first_account_unverified` | no administrator is named and the first account has not proved its email: sign in with the code from the mail |
| 403 | `machine_policy` | the machine's local policy refused the operation (`details.op`) |
| 403 | `machine_untrusted` | path mode (no `HUB_RUNTIME_DOMAIN`): the machine was not connected by an instance administrator, so it may not run on the hub's origin (`/m/<id>/…`, `GET /api/machines/<id>/open`) |
| 404 | `not_found` | |
| 409 | `conflict` | slug taken, last owner, last sign-in method, a login already running (`details.reason`) |
| 409 | `account_exists` | an unverified-email provider (Yandex) met an existing account |
| 410 | `expired` | code, challenge, invite or registration code expired or used up |
| 422 | `provider_off` | the auth provider or connect way is not configured |
| 429 | `rate_limited` | `details.retryAfter` seconds; also `Retry-After` header |
| 502 | `machine_offline` | the machine has no link right now |
| 502 | `machine_error` | the agent answered an error (`details.agentError`, its text) |
| 504 | `machine_timeout` | the agent did not answer in time (15 s unless stated) |
| 500 | `internal` | |

### Shared types

```ts
type User = { id: string; email: string; name: string; isAdmin: boolean; createdAt: string };
type Role = 'owner' | 'member';
type SpaceRef = { id: string; slug: string; name: string; role: Role };
type Space = SpaceRef & { createdAt: string; onboardingStep: OnboardingStep };
type OnboardingStep = 'machines' | 'agents' | 'access' | 'done';
type Profile = 'dir' | 'container' | 'full';
type Provider = 'claude' | 'codex' | 'gemini';

type Machine = {
  id: string;
  spaceId: string | null;
  name: string;               // defaults to hostname; owner may rename
  hostname: string;
  os: 'linux' | 'darwin';
  arch: 'amd64' | 'arm64';
  unixUser: string;
  profile: Profile;
  via: 'command' | 'ssh' | 'local' | 'container';
  status: 'pending' | 'online' | 'offline';
  connectedAt: string | null;
  lastSeenAt: string | null;
  agentVersion: string;
  encKey: string;             // X25519 public key, base64 (std, padded), 32 bytes
  policy: Policy | null;      // last reported by the agent
  preflight: PreflightReport | null;
  agents: AgentStatus[];      // claude, codex always; gemini always as { provider: 'gemini', soon: true, ... }
  proxy: ProxySummary;
  env: string[];              // names of delivered secrets, sorted
  createdAt: string;
};
```

`Policy`, `PreflightReport`, `AgentStatus`, `ProviderCheck`, `ProxySummary`, `Job`, `Login` are the agent link types
below, passed through by the hub unchanged except times converted to RFC 3339 strings.

### Public

| Method, path | Request | 200 response |
| --- | --- | --- |
| `GET /healthz` | | `{"ok":true,"version":"…"}` |
| `GET /readyz` | | `{"ok":true}` once the database answers and migrations ran; 503 otherwise |
| `GET /api/config` | | `Config` |

```ts
type Config = {
  version: string;
  publicUrl: string;                  // HUB_PUBLIC_URL, no trailing slash
  edition: 'self-hosted' | 'cloud';
  signup: 'open' | 'invite';
  auth: {
    email: { mode: 'smtp' | 'log' };  // always on; 'log' = code goes to the hub log
    passkey: boolean;
    github: boolean;
    yandex: boolean;
    oidc: { id: string; name: string }[];
  };
  connect: {
    local: { available: boolean };    // a registered, unclaimed local machine exists (self-hosted only)
    command: true;
    ssh: { available: boolean; publicKey: null }; // publicKey is always null: the key is the space's, GET /api/spaces/{id}/machines/ssh
    teleport: false;
  };
  profiles: { dir: true; container: boolean; full: true };
  runtimeMode: 'path' | 'subdomain';
  termsUrl: string | null;            // HUB_TERMS_URL; the sign-up line about terms shows only when set
};
```

### Auth

After a successful sign-in every flow answers `SignedIn` and sets the session cookie:

```ts
type SignedIn = { user: User; isNew: boolean; next: string }; // next: '/new' (no space yet), '/s/<slug>' or the asked `next`
```

| Method, path | Request | Response | Errors |
| --- | --- | --- | --- |
| `POST /api/auth/email/start` | `{ email, intent: 'signin' \| 'signup' }` | 200 `{ resendAfter: 60, expiresIn: 600 }` — same answer whether the account exists or not | 400, 403 `signup_closed`, 429 |
| `POST /api/auth/email/verify` | `{ email, code }` (6 digits) | 200 `SignedIn` | 400 `invalid_code`, 410 `expired` (TTL 10 min or 5 wrong tries), 429 |
| `POST /api/auth/passkey/login/begin` | `{}` | 200 `{ challengeId, publicKey }` — `publicKey` is `PublicKeyCredentialRequestOptions` JSON (base64url buffers, as go-webauthn emits), discoverable credentials, `userVerification: 'preferred'` | 422 `provider_off` |
| `POST /api/auth/passkey/login/finish` | `{ challengeId, credential }` — `PublicKeyCredential.toJSON()` | 200 `SignedIn` | 400, 410, 401 `unauthenticated` (unknown credential) |
| `GET /api/auth/oauth/{provider}/start?intent=signin\|signup\|link&next=/path` | | 302 to the provider; sets a short cookie bound to the state (`__Host-hl_oauth`, Path=/, on https; `hl_oauth` without https) | 422 `provider_off` (as a redirect to `/signin?error=provider_off`) |
| `GET /api/auth/oauth/{provider}/callback` | provider's `code`, `state` | 302 to `next` (default `/new` or `/s/<slug>`), session cookie set | 302 to `/signin?error=<code>` with `account_exists`, `expired`, `signup_closed`, `oauth_failed` |
| `POST /api/auth/logout` | `{}` | 204, cookie cleared | |

`{provider}` is `github`, `yandex` or `oidc-<id>` for each id in `HUB_OIDC_PROVIDERS`. The callback URL to register
with a provider is `<HUB_PUBLIC_URL>/api/auth/oauth/<provider>/callback`. `intent=link` requires a session and links the
identity to the current user (`next` defaults to `/settings`).

Provider details the hub follows: GitHub — scopes `read:user user:email`, the primary verified email from
`/user/emails`, subject = numeric user id. Yandex — `https://oauth.yandex.ru/authorize`, token at
`https://oauth.yandex.ru/token`, profile from `https://login.yandex.ru/info?format=json` (`id`, `default_email`,
`real_name`), email treated as unverified. OIDC — discovery from the issuer, PKCE S256, `nonce`, scopes `openid email
profile` unless `HUB_OIDC_<ID>_SCOPES`, `email_verified` honoured.

### Me

| Method, path | Request | Response |
| --- | --- | --- |
| `GET /api/me` | | 200 `{ user: User, identities: { provider: string; email: string }[], passkeys: Passkey[], spaces: SpaceRef[] }`; 401 when signed out (the UI's "am I signed in" check) |
| `PATCH /api/me` | `{ name }` | 200 `{ user }` |
| `POST /api/me/passkeys/begin` | `{}` | 200 `{ challengeId, publicKey }` (`PublicKeyCredentialCreationOptions`, resident key required) |
| `POST /api/me/passkeys/finish` | `{ challengeId, credential, name }` | 200 `{ passkey: Passkey }` |
| `DELETE /api/me/passkeys/{id}` | | 204; 409 `conflict` (`last_method`) |
| `DELETE /api/me/identities/{provider}` | | 204; 409 `conflict` (`last_method`) |

`type Passkey = { id: string; name: string; createdAt: string; lastUsedAt: string | null }`

### Spaces and invites

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `POST /api/spaces` | any | `{ name }` (1–60 chars) | 201 `{ space: Space }`; slug made from the name (transliterated, `[a-z0-9-]{3,40}`, suffix `-2`… when taken) |
| `GET /api/spaces/{spaceId}` | member | | `{ space: Space }` |
| `GET /api/spaces/by-slug/{slug}` | member | | `{ space: Space }` |
| `PATCH /api/spaces/{spaceId}` | owner | `{ name?, onboardingStep? }` | `{ space: Space }` |
| `GET /api/spaces/{spaceId}/members` | member | | `{ members: { userId, email, name, role, joinedAt }[] }` |
| `PATCH /api/spaces/{spaceId}/members/{userId}` | owner | `{ role }` | `{ member }`; 409 `last_owner` |
| `DELETE /api/spaces/{spaceId}/members/{userId}` | owner, or self | | 204; 409 `last_owner` |
| `POST /api/spaces/{spaceId}/invites` | owner | `{ emails: string[] }` (≤ 20) | 201 `{ invites: Invite[] }` |
| `POST /api/spaces/{spaceId}/invites/link` | owner | `{ maxUses?: number, days?: 1..7 }` | 201 `{ invite: Invite }`; returns the current active link invite if one exists and the body is empty |
| `GET /api/spaces/{spaceId}/invites` | owner | | `{ invites: Invite[] }` (active ones; `url` absent: only hashes are kept) |
| `DELETE /api/spaces/{spaceId}/invites/{inviteId}` | owner | | 204 |
| `GET /api/invites/{token}` | anyone | | `{ spaceName, inviterName, kind, email: string \| null, valid: boolean }` (email masked `r***@company.ru`) |
| `POST /api/invites/{token}/accept` | signed in | `{}` | `{ space: SpaceRef }`; 403 `forbidden` (email mismatch), 410 `expired` |

```ts
type Invite = {
  id: string; kind: 'email' | 'link'; email: string | null; role: Role;
  url?: string;          // only in the answer that created it: `${publicUrl}/invite/${token}`
  sent?: boolean;        // email invites: false when there is no SMTP (the link is also in the hub log)
  maxUses: number | null; uses: number; expiresAt: string; createdAt: string;
};
```

### Space events (SSE)

`GET /api/spaces/{spaceId}/events` (member) — `text/event-stream`, a comment `: ping` every 20 s, no replay
(`Last-Event-ID` ignored): on (re)connect the UI refetches what it shows. Each event is `event: <type>` and one
`data:` line of JSON:

| `event` | `data` | Sent when |
| --- | --- | --- |
| `machine` | `{ machine: Machine }` | registered, online/offline, renamed, preflight or agents or proxy or policy changed, claimed |
| `machineRemoved` | `{ machineId }` | revoked |
| `job` | `{ machineId, job: Job }` | every step change of an install job (agent or SSH) |
| `login` | `{ machineId, login: Login }` | every state change of an agent login |
| `members` | `{}` | members or invites changed (refetch) |

Owners receive `job` and `login`; members receive `machine`, `machineRemoved`, `members`.

### Machines

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET /api/spaces/{spaceId}/machines` | member | | `{ machines: Machine[] }` |
| `POST /api/spaces/{spaceId}/machines/codes` | owner | `{ profile: Profile, confirmFull?: boolean }` | 201 `{ code, expiresAt, profile, command }`; 400 when `profile=full` without `confirmFull: true`; 422 `provider_off` for `container` without `HUB_CONTAINER_IMAGE` |
| `POST /api/spaces/{spaceId}/machines/local` | owner who is instance admin | `{}` | 200 `{ machine: Machine }`; 404 when no unclaimed local machine; 403 not admin |
| `GET /api/spaces/{spaceId}/machines/ssh` | owner | | 200 `{ publicKey }` — `ssh-ed25519 AAAA… harnessly`, **this space's own** key (made on first use, one per space, the private half sealed in the database); only the space's owners see it; 422 `provider_off` when `HUB_SSH=off` |
| `POST /api/spaces/{spaceId}/machines/ssh` | owner | `{ host, port?: 22, user, profile: Profile, confirmFull?: boolean, hostKeyFingerprint?: 'SHA256:<43 base64 chars>' }` | 202 `{ jobId }`; progress as `job` events, kind `ssh-install`. Without `hostKeyFingerprint` the job only shakes hands and fails with `host_key_unconfirmed` carrying the server's fingerprint; send the request again with it (400 `invalid_request`, `details.field` = `hostKeyFingerprint`, when malformed). Non-public addresses fail with `ssh_blocked` |
| `GET /api/machines/{machineId}` | member | | `{ machine: Machine }` |
| `PATCH /api/machines/{machineId}` | owner | `{ name }` | `{ machine: Machine }` |
| `DELETE /api/machines/{machineId}` | owner | | 204 (revoked, link closed with 4010) |
| `GET /api/machines/{machineId}/open` | member | `?redirect=1` answers 302 instead | `{ url }` — path mode: `/m/<id>/` (403 `machine_untrusted` unless an instance administrator connected the machine); subdomain mode: `https://<id>.<HUB_RUNTIME_DOMAIN>/m/<id>/?hl_ticket=<t>` (60 s, single use) |
| `POST /api/machines/{machineId}/preflight` | owner | `{}` | `{ preflight: PreflightReport }` (runs it again) |

`command` is exactly `curl -fsSL <publicUrl>/install | sh -s -- --token <code> --profile <profile>`, with `sudo sh` in place
of `sh` for the `dir` profile (it needs root); the UI shows and copies it as is.

### Agents on a machine (owner only)

All of these relay one agent operation and answer 502/504 per [errors](#errors) when the machine is away or fails.

| Method, path | Request | Response | Agent op |
| --- | --- | --- | --- |
| `GET /api/machines/{id}/agents?refresh=1` | | `{ agents: AgentStatus[] }` (cached without `refresh`) | `agents.status` |
| `POST /api/machines/{id}/agents/{provider}/install` | `{ from?: 'provider' }` | 202 `{ jobId }` | `agents.install` |
| `POST /api/machines/{id}/agents/{provider}/check` | `{ proxyUrl?: string }` (check through this proxy without saving it) | `{ check: ProviderCheck }` (timeout 30 s) | `provider.check` |
| `POST /api/machines/{id}/agents/{provider}/login` | `{ method: 'link' \| 'console' }` | 202 `{ login: Login }` (state `starting`); 409 `conflict` (`login_running`) | `login.start` |
| `POST /api/machines/{id}/logins/{loginId}/code` | `{ code }` | 204 | `login.code` |
| `DELETE /api/machines/{id}/logins/{loginId}` | | 204 | `login.cancel` |
| `POST /api/machines/{id}/agents/{provider}/key` | `{ envelope: Envelope }` | `{ check: KeyCheck }` | `key.deliver` |
| `POST /api/machines/{id}/agents/{provider}/key/verify` | `{}` | `{ check: KeyCheck }` | `key.verify` |
| `GET /api/machines/{id}/proxy` | | `{ proxy: ProxySummary }` | `proxy.get` |
| `PUT /api/machines/{id}/proxy` | `{ scope: 'all' \| 'claude' \| 'codex', url }` | `{ proxy: ProxySummary }` | `proxy.set` |
| `DELETE /api/machines/{id}/proxy?scope=all\|claude\|codex` | | `{ proxy: ProxySummary }` | `proxy.clear` |
| `POST /api/machines/{id}/env` | `{ name, envelope: Envelope }` | `{ env: string[] }` | `env.deliver` |
| `DELETE /api/machines/{id}/env/{name}` | | `{ env: string[] }` | `env.remove` |
| `POST /api/machines/{id}/gitkey` | `{}` | `{ publicKey }` | `gitkey.ensure` |

`provider=gemini` answers 422 `provider_off` everywhere. The «Сохранить и проверить» button of the proxy form is
`PUT …/proxy` then `POST …/agents/{provider}/check`; after an `ok` the UI starts the install again with
`from: 'provider'`.

The hub never logs request bodies of `…/key`, `…/env`, `…/proxy`, `…/logins/*/code`, nor stores them.

### Runtime proxy

| Path | What |
| --- | --- |
| `/m/{machineId}/*` (path mode, on the hub origin) | needs the hub session and membership, and a machine an instance administrator connected (else 403 `machine_untrusted`); otherwise 302 to `/signin?next=…` |
| `https://{machineId}.{HUB_RUNTIME_DOMAIN}/m/{machineId}/*` (subdomain mode) | needs the `__Host-hl_run` cookie of that host (`hl_run` without https); `?hl_ticket=` trades a ticket for it (then 302 without the ticket); without either, 302 to `<publicUrl>/api/machines/<id>/open?redirect=1` |

Everything below the base path goes to the runtime unchanged (HTTP and WebSocket). The hub answers itself only:
`POST /m/{id}/api/auth/logout` → 204 (the runtime page's sign-out is the hub's), and when the machine is offline an
HTML page «Машина не на связи» with status 502.

### Machine endpoints (no cookie)

| Method, path | Request | Response |
| --- | --- | --- |
| `GET /install` | | `text/x-shellscript`: `deploy/install/install.sh` (from `HUB_DIST_DIR`) with `HARNESSLY_HUB_URL` set to the public URL and, when `HUB_CONTAINER_IMAGE` is set, `HARNESSLY_IMAGE` set to it, on the lines right after the shebang |
| `GET /dl/manifest.json` | | `{ version, files: { "<name>": { sha256, size } } }`, names `runtime-<os>-<arch>`, `agent-<os>-<arch>` for linux/darwin × amd64/arm64 |
| `GET /dl/{name}` | | the binary (`application/octet-stream`), from `HUB_DIST_DIR` |
| `POST /agent/register` | `RegisterRequest` | 201 `RegisterResponse`; 410 `expired`, 400, 429 |
| `GET /agent/v1` | WebSocket upgrade with the signed `Authorization` header | 101; 401 bad signature/unknown key/old time/replayed nonce; 403 revoked; 426 agent older than `HUB_MIN_AGENT_VERSION` |
| `DELETE /agent/v1` | the machine removes itself: an `Authorization` header made like the dial's, signed under the domain `harnessly-agent-unlink-v1` (a dial's signature does not do) | 204 (revoked as `DELETE /api/machines/{id}` does: link closed with 4010, `machineRemoved` event); 401 bad signature/unknown key/old time/replayed nonce; 403 already revoked |

```ts
type RegisterRequest = {
  code: string;                 // hx_…
  publicKey: string;            // Ed25519, base64 std, 32 bytes
  encKey: string;               // X25519, base64 std, 32 bytes
  hostname: string; os: 'linux' | 'darwin'; arch: 'amd64' | 'arm64'; unixUser: string;
  profile: Profile; via: 'command' | 'ssh' | 'local' | 'container';
  agentVersion: string;
};
type RegisterResponse = {
  machineId: string;
  dialUrl: string;              // wss://<host>/agent/v1 (ws:// when the public URL is http)
  runtimeBasePath: string;      // /m/<machineId>
  hubUrl: string;               // public URL
  spaceName: string | null;     // null for the local machine before it is claimed
};
```

The code's profile wins over the request's when they differ (the hub records the code's). A local code
(`HUB_LOCAL_CODE_FILE`) registers with `spaceId = null`, `via = local`, and is renewed by the hub when used or
expired, so the local machine can re-register after its volume was wiped.

## Agent link, protocol 2

### Dial and signature

The agent dials `dialUrl` from `machine.json` with these headers:

```
Authorization: Harnessly-Ed25519 m=<machineId>, t=<unix seconds>, n=<nonce>, s=<signature>
X-Harnessly-Protocol: 2
X-Harnessly-Agent-Version: <version>
```

`nonce` is 16 random bytes, base64url without padding. `signature` is Ed25519 over the bytes of the five lines below
joined by `\n` (no trailing newline), base64url without padding:

```
harnessly-agent-dial-v1
<machineId>
<t>
<nonce>
<host>
```

`host` is the host (and `:port` if not default) of the dial URL, lower-case, as the agent dialed it; the hub compares
with its public URL's host. The hub accepts `|now - t| ≤ 120 s`, a nonce not seen in the last 5 minutes for that
machine, a key that matches the machine and no `revoked_at`.

Both sides use `internal/hubproto` (owned by AGENT): `DialMessage(machineID, t, nonce, host) []byte`,
`SignDial(priv, …) (header string)`, `ParseDialHeader(string) (DialAuth, error)`, `VerifyDial(pub, DialAuth, host, now)`.

Close codes from the hub: `4003` switched off (existing; agent waits a minute), `4010` revoked (agent stops dialing
until restart and logs `machine revoked by the hub`), `4011` replaced by a newer link of the same machine (agent
redials normally), `4012` protocol too old (agent waits an hour).

### Frames

Unchanged from v1: a request `{"op": "...", "id": <number|string>, ...params}` gets exactly one
`{"id": …, "ok": true, "result": …}` or `{"id": …, "ok": false, "error": "<code>: <text>"}`; events are
`{"event": "...", ...}`; tunnel bytes are binary frames (kind byte, 16-byte tunnel id, payload).

Protocol 2 error strings start with a code and a colon. Codes and the hub's HTTP mapping:

| Agent code | Meaning | Hub answers |
| --- | --- | --- |
| `policy_denied` | the local policy switched the operation off | 403 `machine_policy` |
| `unsupported` | unknown op or provider (gemini) | 422 `provider_off` |
| `busy` | a login or install for that provider is already running | 409 `conflict` |
| `not_found` | unknown `loginId`/`jobId`/name | 404 |
| `invalid` | bad parameter (envelope does not open, bad env name, bad proxy URL) | 400 `invalid_request` |
| `failed` | the operation ran and failed (text says why) | 502 `machine_error` |

### Operations (hub → agent)

| `op` | Params | `result` |
| --- | --- | --- |
| `hello` | | `Hello` |
| `link.subscribe` | `{ events: string[] }` | `{}` — this link gets only these events from now on; a link that never sent it gets all (v1 behaviour). The hub sends `["job","login","agents","preflight","policy"]` |
| `preflight.run` | | `PreflightReport` (≤ 20 s) |
| `agents.status` | `{ providers?: Provider[] }` | `{ agents: AgentStatus[] }` (claude and codex, probed in parallel, ≤ 25 s each; the hub waits 35 s) |
| `agents.install` | `{ jobId, provider, from?: 'provider' }` | `{ jobId }` at once; then `job` events until `state` is `done` or `failed` |
| `provider.check` | `{ provider, proxyUrl?: string }` | `ProviderCheck` (≤ 25 s) |
| `proxy.get` | | `ProxySummary` |
| `proxy.set` | `{ scope: 'all' \| 'claude' \| 'codex', url }` | `ProxySummary` |
| `proxy.clear` | `{ scope }` | `ProxySummary` |
| `login.start` | `{ loginId, provider, method: 'link' \| 'console' }` | `Login` (state `starting`); then `login` events |
| `login.code` | `{ loginId, code }` | `{}` — Claude link login only |
| `login.cancel` | `{ loginId }` | `{}` |
| `key.deliver` | `{ provider, envelope: Envelope }` | `KeyCheck` (after saving) |
| `key.verify` | `{ provider }` | `KeyCheck` |
| `env.deliver` | `{ name, envelope: Envelope }` | `{ env: string[] }` |
| `env.remove` | `{ name }` | `{ env: string[] }` |
| `gitkey.ensure` | | `{ publicKey }` — `ssh-ed25519 … harnessly@<hostname>`; made once at `~/.ssh/harnessly_ed25519`, with a `Host *` `IdentityFile` entry added to `~/.ssh/config` |
| `runtime.session` | `{ subject, name? }` (subject = the person's email) | `{ cookieName, token, expiresAt }` — a page session signed with the pair secret, 12 h |
| `tunnel.open` | v1, unchanged; target `runtime` only for the hub (passes the policy's route filter) | v1 |

The v1 operations (`transcripts.*`, `state.*`, `conversations.start`, `docs.*`, `design.*`, `eval.*`, `status.send`)
stay for the closed cluster. The hub does not call them; on a hub link they answer `policy_denied`.

### Events (agent → hub)

| `event` | Fields | When |
| --- | --- | --- |
| `job` | `{ job: Job }` | each step change of an install |
| `login` | `{ login: Login }` | each state change of a login |
| `agents` | `{ agents: AgentStatus[] }` | after an install, login, key change, and when a periodic check (every 5 min) differs |
| `preflight` | `{ report: PreflightReport }` | after connect and every 30 min |
| `policy` | `{ policy: Policy }` | the policy file or the kill switch changed (the kill switch then closes the link) |

### Types

```ts
type Hello = {
  protocol: 2;
  agentVersion: string; user: string; home: string; hostname: string;
  features: string[];          // v1 list plus: "preflight","agents","login","provider-check","proxy","keys","env","gitkey","runtime-session", and "tunnel" when the agent has a runtime port
  machineId: string;
  os: 'linux' | 'darwin'; arch: 'amd64' | 'arm64';
  profile: Profile;
  policy: Policy;
};

type Policy = {
  terminal: boolean;           // runtime terminal socket and shell tabs reachable through the tunnel: /api/terminal, /api/v2/terminal, /api/v2/conversations/<id>/terminals…, and the login console (/console/…, /api/console/…); also, with it off, the chat types only into a running agent, refuses `!` (shell mode) lines and does not approve tool calls
  agentLogin: boolean;
  keys: boolean;               // key.deliver
  env: boolean;                // env.deliver, gitkey.ensure
  install: boolean;            // agents.install
  runtimeSession: boolean;     // runtime.session and tunnel.open (off = nobody opens the runtime through the hub)
  denyRoutes: string[];        // runtime path prefixes after the base path, e.g. ["/api/terminal"]
  source: string;              // the file it was read from, or "default"
};

type Check = { id: string; level: 'ok' | 'warn' | 'fail'; message: string };

type PreflightReport = {
  at: number;
  osName: string;              // "Ubuntu 24.04", "macOS 15.1" (from /etc/os-release PRETTY_NAME or sw_vers)
  os: 'linux' | 'darwin'; arch: 'amd64' | 'arm64';
  cpus: number;
  memTotal: number; memAvailable: number;
  disk: { path: string; total: number; free: number };  // the work folder's filesystem
  tools: {
    tmux: { installed: boolean; version: string | null; compatible: boolean };
    git: { installed: boolean; version: string | null };
    node: { installed: boolean; version: string | null };   // Codex needs ≥ 20
    claude: { installed: boolean; version: string | null };
    codex: { installed: boolean; version: string | null };
  };
  checks: Check[];
};
```

`checks` ids and levels: `disk` (free < 3 GB fail, < 10 GB warn), `memory` (total < 2 GB fail, available < 1 GB warn),
`tmux` (missing or < 2.9 fail), `git` (missing warn), `node` (missing or < 20 warn: "Codex не поставить без Node.js
20+"), `clock` (off by more than 60 s against the hub's `Date` header, warn). The UI line «на связи · Ubuntu 22.04 ·
32 ядра · 64 ГБ памяти · 412 ГБ свободно» is built from `osName`, `cpus`, `memTotal`, `disk.free`.

```ts
type AgentStatus = {
  provider: Provider;
  soon?: true;                 // gemini only (added by the hub; the agent never reports gemini)
  installed: boolean;
  version: string | null;
  login: 'logged_in' | 'logged_out' | 'unknown';
  authMode: 'subscription' | 'api_key' | 'cloud' | null;
  overriddenBy: string | null; // e.g. "ANTHROPIC_API_KEY": a key in the environment beats the subscription login
  access: ProviderCheck | null;// last provider check, if any
};
```

Status comes from `claude auth status` (JSON, exit code) and `codex login status` (exit code; mode from its text), run
through the account's login shell with the agent env files sourced.

```ts
type ProviderCheck = {
  provider: Provider;
  verdict: 'ok' | 'network' | 'rejected';
  at: number;
  via: 'direct' | 'proxy';
  host: string;                // the host the verdict is about: the first that failed, else the API host
  status: number | null;       // HTTP status when the server answered
  detail: string | null;       // network: the error ("timeout", "connection refused", "certificate …"); rejected: first 300 chars of the body
  hosts: { host: string; dns: boolean; tcp: boolean; tls: boolean; ms: number; error: string | null }[];
};
```

Hosts: Claude — `api.anthropic.com`, `claude.ai`, `platform.claude.com`; Codex — `api.openai.com`, `chatgpt.com`,
`auth.openai.com`. The light request is `GET https://api.anthropic.com/v1/models` / `GET https://api.openai.com/v1/models`
without a key: a 401 with the provider's JSON error is `ok` (reachable and answering); any other non-2xx is
`rejected`; no answer is `network`. 8 s per host.

```ts
type ProxySummary = {
  all: ProxyEntry | null; claude: ProxyEntry | null; codex: ProxyEntry | null;
};
type ProxyEntry = {
  scheme: 'http' | 'https' | 'socks5';
  host: string;                // host:port, never credentials
  bridge: string | null;       // "127.0.0.1:<port>" when a SOCKS bridge runs
};
```

Proxy URLs accepted: `http://`, `https://`, `socks5://`, `socks5h://`, optional `user:pass@`. The effective proxy of an
agent is its own scope, else `all`.

```ts
type Job = {
  id: string;
  kind: 'agent-install' | 'ssh-install';
  provider: Provider | null;
  state: 'running' | 'done' | 'failed';
  steps: { id: string; state: 'wait' | 'run' | 'ok' | 'fail'; detail: string | null }[];
  error: { code: string; message: string } | null;
  check: ProviderCheck | null; // set when the provider step failed
};
```

Step ids, in order (the UI owns the labels, copied from the mockup):

| kind | step ids | UI labels |
| --- | --- | --- |
| `agent-install` | `system`, `provider`, `download`, `install`, `verify` | «Проверяем систему», «Проверяем, что серверы {vendor} отвечают», «Скачиваем {name}», «Ставим на {machine}», «Проверяем запуск» |
| `ssh-install` | `ssh`, `preflight`, `install`, `online` | «Связь по SSH», «Проверяем диск и память», «Ставим агента harnessly», «Машина на связи» |

`ssh-install` jobs are the hub's own (it emits them on the SSE stream); `detail` of the `ssh` step carries the host
key fingerprint `SHA256:…`. `from: 'provider'` starts an install at the provider step (the earlier ones are `ok`).

Error codes of the `ssh` step: `host_key_unconfirmed` (the request carried no `hostKeyFingerprint`: nothing was run and
no key was offered; the `ssh` step `detail` and the message carry the server's key type and fingerprint to show the owner,
who compares it with the server's own and repeats the request with it), `host_key_mismatch` (the server showed another key
than the confirmed one; nothing was run), `ssh_blocked` (the address is not on the public internet: private, loopback,
link-local, shared, documentation and reserved ranges, and IPv6 forms that carry such an IPv4 address; `HUB_SSH_ALLOW_PRIVATE=on`
lets a self-hosted hub through, the cloud edition never), `ssh_unreachable` (refused, timed out, unknown name or no SSH:
one message on purpose), `ssh_auth` (the server did not take the space's key). The name is resolved once, every answer
must be public, and the connection goes to the address that was checked.

```ts
type Login = {
  id: string;
  provider: Provider;
  method: 'link' | 'console';
  state: 'starting' | 'url' | 'waiting' | 'verifying' | 'done' | 'failed' | 'expired' | 'cancelled';
  url: string | null;          // link: the provider page to open
  userCode: string | null;     // Codex device code to enter on that page ("K7QF-2M9P")
  needsCode: boolean;          // Claude: the UI shows «вставьте код» and posts it with login.code
  consoleUrl: string | null;   // console: a console page of the runtime under its base path, e.g. "/m/<id>/console/<hex16>"
  expiresAt: number | null;    // Claude 10 min, Codex 15 min from start
  error: string | null;        // failed: human text; e.g. link not found on screen in 10 s → "Не нашли ссылку входа, войдите в консоли"
  status: AgentStatus | null;  // on done
};
```

A console is a text page the runtime serves (`POST <base>/api/agent/terminal-tab {command}`, machine agent only, opens a
300x40 tmux session running the login command and answers `{ tabId, url }`): it needs the signed-in person's runtime
session, shows the screen as text with its links clickable and takes typed text and a short list of keys. The UI opens
`consoleUrl` like any page under the base path (a new browser tab, with the ticket in subdomain mode). It is a terminal,
so `login.start` with `method: "console"` answers `policy_denied` when the policy has `terminal = false`; a console
older than two hours is closed when the next one is opened.

The hub does not pass on what it cannot vouch for, because the machine that wrote it may be hostile: `url` reaches the
UI only when it is `https` with no credentials or port and its host is exactly one of the provider's sign-in pages
(Claude: `claude.ai`, `console.anthropic.com`, `platform.claude.com`; Codex: `auth.openai.com`); `consoleUrl` only when it
is `/m/<this machine's id>/console/<16 hex>` (or `/m/<id>/?tab=<id>`); `userCode` only as letters, digits and dashes (up to
32); `error` is cut at 500 characters. A login that was waiting for a link it lost this way is sent as `failed` with a fixed
text («Машина прислала ссылку входа не на страницу поставщика — мы её скрыли…»). The UI checks again before it opens
anything.

State flow — Claude link: `starting` → `url` (`needsCode`) → after `login.code` `verifying` → `done` | `failed`.
Codex link: `starting` → `url` (with `userCode`) → `waiting` → `done` | `expired`. Console: `starting` → `waiting`
(`consoleUrl`) → `done` when the status turns `logged_in` (polled every 3 s, 15 min) | `expired`. One login per machine
at a time; a new `login.start` while one runs answers `busy`.

```ts
type KeyCheck = {
  provider: Provider;
  present: boolean;            // the variable is visible to a login shell of the account
  verdict: 'accepted' | 'rejected' | 'network' | 'absent';
  status: number | null;       // provider's HTTP status for the key check
  saved?: boolean;             // key.deliver: written to the env file
};
```

The key check is `GET /v1/models` with the key (`x-api-key` + `anthropic-version: 2023-06-01` for Anthropic, `Bearer`
for OpenAI), through the agent's proxy: 200 accepted, 401/403 rejected. Variable names: Claude `ANTHROPIC_API_KEY`,
Codex `OPENAI_API_KEY`.

### Sealed envelope (browser → machine)

The browser seals a key or a secret to the machine's X25519 key (`Machine.encKey`); the hub passes the JSON through.

```ts
type Envelope = { v: 1; epk: string; nonce: string; ct: string }; // base64 std, padded
```

1. Make an ephemeral X25519 key pair; `epk` is its raw 32-byte public key.
2. `shared = X25519(ephemeralPrivate, encKey)`.
3. `key = HKDF-SHA256(ikm = shared, salt = epk ‖ encKey, info = "harnessly-envelope-v1", length = 32)`.
4. `nonce` = 12 random bytes; `ct = AES-256-GCM(key, nonce, plaintext, aad)` (tag appended, as WebCrypto does).
5. `aad = "harnessly:" + machineId + ":" + purpose`, where `purpose` is `key:<provider>` for `key.deliver` and
   `env:<NAME>` for `env.deliver`. An envelope made for one machine or purpose does not open as another.
6. Plaintext: the key or the secret's value, UTF-8, ≤ 8 KiB.

Browser: WebCrypto `X25519` (`generateKey`, `deriveBits`), `HKDF`, `AES-GCM`. Machine: `crypto/ecdh`, `crypto/hkdf`,
`crypto/cipher`. `internal/hubproto` has `Seal`/`Open` and test vectors in `internal/hubproto/testdata/envelope.json`
(`[{ machineId, purpose, encPrivate, encKey, ephemeralPrivate, nonce, plaintext, envelope }]`) that the web test
(`web/app/hub/lib/seal.test.ts`) must reproduce with the same ephemeral key and nonce.

The hub distributes `encKey`, so a hub that is itself compromised could hand out its own key; sealing protects against
storage and logs, not against a malicious hub. The UI text («Ключ зашифруется в браузере и уйдёт прямо на машину. У
нас он не сохраняется.») is true under that reading.

Env names accepted by `env.deliver` are an allowlist, one rule for both sides (`hubproto.CheckEnvName`; the hub checks
first and answers 400 `invalid_request` with `details.field = "name"`, the machine checks again). The env files are
sourced into every agent launch, so a hub that could set `GIT_SSH_COMMAND`, `PYTHONPATH` or `PAGER` would run code on the
machine past the policy's `terminal = false`. A name is accepted when it matches `^[A-Z_][A-Z0-9_]{0,63}$`, ends in
`_TOKEN`, `_KEY`, `_SECRET`, `_PASSWORD`, `_PAT`, `_DSN` or `_USER` (`GH_TOKEN`, `GITLAB_TOKEN` pass) and is not `PATH`,
`HOME`, `SHELL`, `USER`, `LOGNAME`, `PWD`, `IFS`, `TMPDIR`, `*_PROXY`, `BASH_ENV`, `ENV`, `PROMPT_COMMAND`, `PS1`, `PS4`,
`ZDOTDIR`, `NODE_OPTIONS`, `NODE_PATH`, `NODE_EXTRA_CA_CERTS`, `JAVA_TOOL_OPTIONS`, `_JAVA_OPTIONS`, `GCONV_PATH`,
`BROWSER`, `PAGER`, `SSH_ASKPASS`, `EDITOR`, `VISUAL`, `GOPROXY`, `GOFLAGS`, `HF_ENDPOINT`, `GH_HOST`, a name containing
`_CA_`, or one with the prefixes `LD_`, `DYLD_`, `BASH_FUNC_`, `GIT_`, `LESS`, `SSL_CERT_`, `PYTHON`, `PERL`, `RUBY`,
`PIP_`, `UV_`, `DOCKER_`, `NPM_CONFIG_`, `CARGO_`, `CONDA_`, `ANTHROPIC_`, `OPENAI_` (keys go through `key.deliver`),
`HARNESSLY_`, `RUNTIME_`, `AGENT_`, `CODEX_`, `CLAUDE_`. An address is not a secret: `_URL` and `_HOST` names (`GITLAB_HOST`,
`PIP_INDEX_URL`, a registry mirror) are not accepted, because a variable that points a package manager or a CLI at another
server makes the machine fetch and run what that server serves. `env.remove` takes any well-formed name except the proxy
variables.

## The machine side

### Files

| Path | Mode | What |
| --- | --- | --- |
| `$AGENT_DATA_DIR/machine.json` | 0600 | `{ machineId, hubUrl, dialUrl, runtimeBasePath, profile, ed25519: <seed b64>, x25519: <private b64>, registeredAt }` |
| `$AGENT_DATA_DIR/agent-env/all.env`, `claude.env`, `codex.env` | 0600 | `KEY='value'` lines (single-quoted, `'` escaped as `'\''`): keys (`claude.env`/`codex.env`), secrets (`all.env`), proxy variables (scope's file) |
| `$AGENT_DATA_DIR/agent-env/proxy.json` | 0600 | the proxy settings with credentials |
| `$AGENT_DATA_DIR/agent-audit.log` | 0600 | one JSON line per changing operation, no secrets |
| `$AGENT_DATA_DIR/agent-link.json` | 0600 | what the running agent tells `harnessly-agent status` and the installer: linked or not, why; rewritten every 20 s (a file older than 60 s is an agent that is gone). No secret in it |
| `/etc/harnessly/agent.env` or `~/.config/harnessly/agent.env` | 0644 / 0600 | the folders the `harnessly-agent` commands use, so `sudo harnessly-agent status` looks where the service does |
| `/etc/harnessly/policy.toml` or `~/.config/harnessly/policy.toml` | root 0644 / user 0600 | the local policy |
| `disabled` next to the policy file, or `$AGENT_DATA_DIR/disabled` | | kill switch |

`AGENT_DATA_DIR` defaults to `~/.harnessly`, the runtime's data folder. **Launch lines source the env files**: the
runtime prefixes every Claude and Codex launch, its side chat and chat titles (and the agent prefixes every login and status command) with
`set -a; . <dir>/all.env; . <dir>/<provider>.env; set +a;` for the files that exist, so keys, secrets and the proxy
reach the agent CLI and nothing else needs the person's shell profile.

### policy.toml

```toml
# Read at start, on SIGHUP and when the file changes. Missing file = these defaults.
terminal = true
agent_login = true
keys = true
env = true
install = true
runtime_session = true
deny_routes = []   # e.g. ["/api/terminal", "/api/fs/write"]
```

A file the agent's account can write (the `full` profile's) is trusted only to narrow while the agent runs: a change that
allows more waits for `SIGHUP` or a restart. A root-owned file applies every change at once. With `terminal = false` the
route filter allows only a list of write routes and refuses the rest (and `POST /api/v2/cmd/decide`, the approval of a tool call, even though the chat is on that list), and the runtime, which the agent tells with `POST <base>/api/agent/policy`, types chat text only into a pane where an agent runs and refuses `!` lines; it never lets the runtime's file routes write the
policy, the kill switch, `machine.json`, the audit log or the agent's env files. See
[machine.md](machine.md#the-local-policy).

### Commands

The `cmd/agent` binary is installed as `harnessly-agent` (and `cmd/runtime` as `harnessly-runtime`), in
`/usr/local/bin` for profile `dir`, `~/.local/bin` for `full`, `/usr/local/bin` inside the container image.

| Command | What |
| --- | --- |
| `harnessly-agent register --hub <url> --token <hx_…> --profile <p> [--via command\|ssh\|local\|container] [--name <host name>] [--force\|--reuse]` | registers and writes `machine.json`; exits non-zero with the hub's message on failure. `--name` is the host name the machine reports (`RegisterRequest.hostname`); `--force` replaces an existing `machine.json`; `--reuse` keeps a valid one of the same hub and profile and sends nothing |
| `harnessly-agent unlink` / `gitkey-remove` | remove the machine from its hub (`DELETE /agent/v1`, signed) / take away the git key and the `~/.ssh/config` block the agent made |
| `harnessly-agent register --code-file <path> …` | same, the code read from a file (the local machine in compose) |
| `harnessly-agent off` / `on` / `status` | kill switch and a one-line state (`linked to <hub> as <machineId>`, `off`, `not registered`) |
| `harnessly-agent policy` / `version` | print the policy in force / the version |
| `harnessly-agent` (no arguments) | runs: with `machine.json` dials the hub with Ed25519; without it, v1 behaviour from the environment |

### install.sh

`curl -fsSL <hub>/install | sh -s -- --token <code> --profile dir|container|full [--name <n>] [--dry-run] [--re-register] [--uninstall [--purge]]` (`sudo sh` for `dir`)

POSIX `sh`, everything inside `main()` called on the last line. Steps: detect OS and arch → refuse Windows and
unsupported arch → (dir, container) check root or `sudo -n` → download binaries from `<hub>/dl/` and check SHA-256
against `manifest.json` → set up the profile (user, folders, units, policy file; or the container) → `harnessly-agent register`
→ start services → wait up to 60 s for the agent to report `linked` → print «Done: the machine will appear in harnessly.» (in Russian when the locale is). It
writes a manifest of everything it made that `--uninstall` removes: `/etc/harnessly/install-manifest` for `dir` (root-owned:
root runs `--uninstall` and must not trust a file the service user can replace), `~/.harnessly/install-manifest-container`
for `container`, `~/.harnessly/install-manifest` for `full`. `--uninstall` keeps the work folder and the `dir` user's home
unless `--purge`. `--dry-run` prints the plan without changing anything. The container profile needs the machine image
(`HARNESSLY_IMAGE`, set by the hub's `/install`, or `--image`); it registers from a one-off container and starts the
long-running one with `machine.json` already in its volume, so no code is left in its settings. Messages are English, or
Russian by `LANG`/`LC_ALL`/`HARNESSLY_LANG`; the line «Готово: машина появится в harnessly» is always printed.
`HARNESSLY_INSTALL_ROOT` puts every path under a folder (tests and image building; empty in real use). The `dir` runtime
unit has no `PrivateTmp` (the tmux server and the agents outlive the runtime process; tmux's socket is in
`/var/lib/harnessly/tmux`); the agent unit has it.

Ports on the machine: the runtime on `127.0.0.1:18022`, the agent on `127.0.0.1:18023` (next free if taken; written
into the units). Runtime environment: `RUNTIME_GATE_LISTEN`, `RUNTIME_GATE_UPSTREAM=` (standalone),
`RUNTIME_APP_BASE_PATH=<runtimeBasePath>`, `RUNTIME_HUB_URL=<hubUrl>`, `RUNTIME_PREVIEW_LISTEN=off`,
`RUNTIME_WORKSPACE_DIR` (dir: `/srv/harnessly/work`; full: `~/work`). Agent environment: `AGENT_PORT`,
`RUNTIME_PORT`. In Ed25519 mode the agent puts `runtimeBasePath` in front of the runtime routes it calls itself
(`AGENT_RUNTIME_API`, `AGENT_RUNTIME_STATUS`, `AGENT_RUNTIME_SYNC`, `AGENT_RUNTIME_STATE_CHANGED` defaults become
`/m/<id>/api/agent`, `/m/<id>/api/status`, …), since the runtime serves its API under its base path. Tunnelled
requests already carry the base path and pass the route filter unchanged, `X-Forwarded-*` included.

## Configuration

### Hub

| Variable | Default | What |
| --- | --- | --- |
| `HUB_LISTEN` | `0.0.0.0:8040` | HTTP listen address (TLS is the reverse proxy's) |
| `HUB_PUBLIC_URL` | required | the address people and machines use, e.g. `https://hub.example.com` |
| `HUB_DATABASE_URL` | `sqlite://$HUB_DATA_DIR/hub.db` | `sqlite:///abs/path.db` or `postgres://user:pass@host/db?sslmode=…` |
| `HUB_DATA_DIR` | `/var/lib/harnessly-hub` | SQLite file, the `secret_key` file (unless `HUB_SECRET_KEY` is set) |
| `HUB_EDITION` | `self-hosted` | or `cloud` (needs `HUB_RUNTIME_DOMAIN`; no SSH to private addresses) |
| `HUB_SIGNUP` | `open` (Helm chart: `invite`) | or `invite` |
| `HUB_ADMIN_EMAILS` | | comma-separated; an address counts when its owner proved it; empty = the first account is the admin, if it proved its email |
| `HUB_TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` (compose: the network's gateway; Helm: none) | CIDRs whose `X-Forwarded-For`/`-Proto` are believed: as exact as the proxy |
| `HUB_SMTP_URL` | | `smtp://user:pass@host:587` (STARTTLS) or `smtps://…:465`; empty = codes and invite links go to the log, which the hub allows on plain http only: an https hub and the cloud edition refuse to start without it (`check-config` exits 2) |
| `HUB_MAIL_LOG` | | `allow`: accept codes and invite links in the log on an https hub or in the cloud edition; the hub warns at every start |
| `HUB_MAIL_FROM` | `harnessly <no-reply@<host>>` | |
| `HUB_PASSKEY` | `on` | `off` hides passkeys |
| `HUB_RP_ID` | public URL's host | WebAuthn RP ID |
| `HUB_RP_NAME` | `harnessly` | |
| `HUB_GITHUB_CLIENT_ID`, `HUB_GITHUB_CLIENT_SECRET` | | GitHub OAuth app |
| `HUB_GITHUB_URL` | `https://github.com` | GitHub Enterprise base |
| `HUB_YANDEX_CLIENT_ID`, `HUB_YANDEX_CLIENT_SECRET` | | Yandex ID app |
| `HUB_OIDC_PROVIDERS` | | comma-separated ids `[a-z0-9]+`; each needs `HUB_OIDC_<ID>_ISSUER`, `_CLIENT_ID`, `_CLIENT_SECRET`, optional `_NAME` (button text), `_SCOPES` |
| `HUB_RUNTIME_DOMAIN` | | e.g. `run.example.net`: subdomain mode, a domain that is not on the hub's registrable domain (a parent of the hub's host is refused; a same-site one is a warning, and the cloud edition refuses it unless `HUB_ALLOW_SAME_SITE_RUNTIME=1`); empty: path mode (only machines an instance administrator connected are opened; refused by the cloud edition) |
| `HUB_ALLOW_SAME_SITE_RUNTIME` | `0` | `1` accepts, in the cloud edition, a `HUB_RUNTIME_DOMAIN` on the hub's registrable domain (one site: the `__Host-` cookies and the cross-site checks narrow what a machine's script can do there, they do not remove it) |
| `HUB_DIST_DIR` | `/opt/harnessly/dist` | binaries and `manifest.json` served under `/dl/` |
| `HUB_CONTAINER_IMAGE` | | image for the container profile; empty hides it |
| `HUB_LOCAL_CODE_FILE` | | when set, the hub keeps a local registration code in this file (self-hosted «Эта машина»), mode `0600` in a `0700` folder |
| `HUB_LOCAL_CODE_GROUP` | | numeric group id that may read that file instead (`0640`, folder `0750`) |
| `HUB_SSH` | `on` | `off` hides «по SSH» |
| `HUB_SSH_ALLOW_PRIVATE` | `off` | `on` lets the SSH way reach private, loopback and link-local addresses (self-hosted only; the cloud edition ignores it) |
| `HUB_SECRET_KEY` | | at least 32 characters: seals the spaces' SSH keys; empty: a `secret_key` file in `HUB_DATA_DIR` |
| `HUB_MIN_AGENT_VERSION` | | refuse older agents (426 / 4012) |
| `HUB_TERMS_URL` | | shown under the sign-up |
| `HUB_WEB_DIR` | | serve the UI from this folder instead of the embedded files (development) |
| `HUB_LOG_LEVEL` | `info` | |

Secrets (`*_SECRET`, `HUB_SMTP_URL`, `HUB_DATABASE_URL`, `HUB_SECRET_KEY`) may also be given as `<NAME>_FILE` pointing to a file.

### Agent (new)

| Variable | Default | What |
| --- | --- | --- |
| `AGENT_AUTH` | `auto` | `auto`: Ed25519 when `machine.json` exists, else HS256 (v1); `ed25519`, `hs256` force one |
| `AGENT_MACHINE_FILE` | `$AGENT_DATA_DIR/machine.json` | |
| `AGENT_POLICY_FILE` | `/etc/harnessly/policy.toml` if it exists, else `~/.config/harnessly/policy.toml` | |
| `AGENT_ENV_DIR` | `$AGENT_DATA_DIR/agent-env` | |

All v1 variables keep their meaning. In Ed25519 mode `AGENT_CLUSTER_URL` and `AGENT_RUNTIME_ID` are ignored, and the
agent starts even while the runtime has not written its secret yet (`runtime.session` answers `failed` until then).

### Runtime (new)

| Variable | What |
| --- | --- |
| `RUNTIME_HUB_URL` | the hub's public URL; goes into the page config as `hubUrl`. With it set a hub signs the people in: `GET /api/auth/setup` answers `{ needsSetup: false, requiresAuth: false, hostEnvLocked: false }` and `POST` answers 400, because a first-run password would replace the secret the agent signs page sessions with. The page offers no password setup or login (`hubUrl` is the cue) |

## UI routes

The hub serves `web/dist-hub/index.html` for each of these paths and the files under `/assets/`. In the table, «calls»
lists the endpoints a screen uses; every screen first calls `GET /api/config` and `GET /api/me` (401 = signed out).

| Route | Screen (mockup step) | Calls |
| --- | --- | --- |
| `/` | redirect: signed out → `/signin`; no space → `/new`; else last space `/s/<slug>` (or its setup step if `onboardingStep ≠ done`) | |
| `/signin`, `/signup` | «Аккаунт»: «Вход в harnessly» / «Создайте аккаунт»; passkey, GitHub, Яндекс, OIDC buttons as configured; email → «Отправили код на …» → six-digit code; `?error=` shown | `auth/email/start`, `auth/email/verify`, `auth/passkey/login/*`, `auth/oauth/*/start` |
| `/invite/<token>` | invitation: space and inviter, «Войти и присоединиться» | `GET /api/invites/<token>`, then sign-in, then `POST …/accept` |
| `/new` | «Пространство»: «Как назовём пространство?», invites by email and «Скопировать ссылку» | `POST /api/spaces`, `POST …/invites`, `POST …/invites/link`, `PATCH /api/spaces/<id>` (`onboardingStep: 'machines'`) |
| `/s/<slug>/setup/machines` | «Машины»: «Эта машина» / «Свой сервер» / «по SSH» / Teleport (disabled) / «Наша ВМ» (disabled); profile choice; command; machines appearing | `GET …/machines`, `POST …/machines/codes`, `POST …/machines/local`, `POST …/machines/ssh`, SSE |
| `/s/<slug>/setup/agents` | «Агенты»: per machine tabs, Claude Code / Codex / Gemini «Скоро»; install plan; provider failure and proxy form; login by link / console / key | `GET /api/machines/<id>/agents`, `…/install`, `…/check`, `…/login`, `…/logins/<id>/code`, `…/key`, `…/key/verify`, `…/proxy`, SSE |
| `/s/<slug>/setup/access` | «Доступы»: GitHub, GitLab, SSH-ключ для Git, Секреты; «Пропустить» | `POST /api/machines/<id>/env` (each machine), `POST /api/machines/<id>/gitkey` |
| `/s/<slug>/setup/done` | «Всё готово»: summary, «Открыть первый чат» | `PATCH /api/spaces/<id>` (`onboardingStep: 'done'`), `GET /api/machines/<first>/open` |
| `/s/<slug>` | space home: machines with status and «Открыть», «Добавить машину» | `GET …/machines`, SSE, `GET /api/machines/<id>/open` |
| `/s/<slug>/settings` | members and invites, machines (rename, remove, agents), space name | members/invites/machines endpoints |
| `/settings` | the person: name, passkeys, linked sign-ins, sign out | `/api/me*`, `auth/oauth/*/start?intent=link`, `auth/logout` |

The «Аккаунт» step is `/signup` or `/signin`; «Пространство» is `/new`; the step bar (Шаг N из 6) is drawn from the
route. The runtime page (`/m/<id>/…`) is not part of the hub UI: it is the runtime's page; with `hubUrl` in its config
its name at the top of the sidebar links back to the hub (`<hubUrl>/`), and its sign-out posts to its own
`POST <base>/api/auth/logout` (the hub answers it, see Runtime proxy, and works in both runtime modes without a
cross-origin post) and then goes to `<hubUrl>/signin`. When its session is gone (the hub answered the page's first request
with a redirect to its sign-in) it goes to `<hubUrl>/signin?next=<path>`; `/m/…` is a valid `next`, taken with a full page
load.

The profile choice and the full-access confirmation come before the command, not after a machine connects as the
mockup draws it.
