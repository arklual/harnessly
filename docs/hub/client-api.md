# Client API: the person's own apps

A person connects their own app — a launcher extension, a menu-bar counter, a script — to the hub, and the app reads
their chats and writes to them with a personal token. The app never sees the person's password or session: the person
confirms a short code on the hub, the way a TV signs in (the OAuth 2.0 device flow, RFC 8628, in the hub's own JSON).

Behind the rollout switch `client-api` ([rollout-switches.md](rollout-switches.md)), per person (machine `*`). With the
switch off the consent page refuses and every token of the person stops working at once.

Code: `internal/hub/auth/apitoken.go` (connecting, tokens), `internal/hub/proxy/client_api.go` (the API),
`internal/hub/store/api_tokens.go`, migration `0067_api_tokens.sql`.

## Connecting an app

```
app                                  hub                                   person (signed in)
POST /api/auth/device/start  ──────▶ deviceCode, userCode, verifyUrl
open verifyUrl in the browser ───────────────────────────────────────────▶ /connect?code=ABCD-EFGH
                                                                           GET  /api/auth/device?code=…   (what asks)
                                                                           POST /api/auth/device/approve
POST /api/auth/device/token (every `interval` s) ◀── authorization_pending … then {token}
```

- `POST /api/auth/device/start` `{client, device?}` → `{deviceCode, userCode, verifyUrl, interval, expiresIn}`.
  `client` is the app's name the person sees ("Raycast"), `device` the computer ("MacBook"). No sign-in; 10 per 10
  minutes from one address. The code lives 10 minutes.
- `POST /api/auth/device/token` `{deviceCode}` → `{token, tokenId, scopes, expiresAt, user: {id, email, name}}`, or 400
  with `error` `authorization_pending` (ask again in `interval` seconds), `access_denied` (the person refused) or
  `expired_token` (start over). The token is given once; the code is spent.
- `GET /api/auth/device?code=` (signed in) → `{client, device, ip, expiresAt, scopes}`: what the consent page shows.
  404 `client_api_off` while the switch is off for the person.
- `POST /api/auth/device/approve` (signed in) `{code, allow, scopes?}` → 204. `scopes` are `read` and `prompt` (both by
  default); a token without `prompt` only reads.

The token is `hlp_` and 43 characters; the hub keeps only its SHA-256. It works 90 days from its last use (the term
moves on with use, at most once a minute). The person sees their apps and revokes them in the account settings:

- `GET /api/me/tokens` → `{on, tokens: [{id, client, device, scopes, createdAt, lastUsedAt, expiresAt, ip}]}`
- `DELETE /api/me/tokens/{id}` → 204

Signing out everywhere revokes the tokens too; a disabled account's tokens stop working.

## The API

Every call carries `Authorization: Bearer hlp_…`; 401 `token_invalid` means connect again, 403 `scope` that the token
may not do this. 600 calls a minute per token. Only the person's own machines count (not those they reach as an
administrator of a space). Answers are JSON, `Cache-Control: no-store`.

```ts
type Machine = { id: string; name: string; online: boolean; status: string; org: string };

type Chat = {
  id: string;
  machineId: string;
  machineName: string;
  online: boolean;            // the chat's machine is connected now
  title: string;
  project: string;            // the last part of cwd
  projectId: string | null;
  cwd: string;
  provider: string;           // claude, codex, …
  model: string;
  state: string;              // starting, working, waiting-question, waiting-permission, ready, idle, stopped, …
  attention: '' | 'question' | 'permission' | 'plan' | 'unread'; // why it waits for the person
  unread: boolean;            // the last turn ended after the person last read the chat
  pinned: boolean;
  recap: string | null;       // the agent's latest summary of the chat
  snippet: string | null;     // the start of the last answer
  lastTurnStatus: string | null;
  queue: number;              // messages waiting for the agent
  background: number;         // background work the agent waits for
  from?: string;              // the app that started the chat
  delegated: boolean;         // started by another agent
  createdAt: number;          // Unix ms
  lastPromptAt: number;
  activityAt: number;
  url: string;                // the chat on the hub's page
};

type Turn = {
  id: string;
  status: string;
  startedAt: number;
  endedAt: number | null;
  toolCount: number;
  prompt: string;             // the person's message
  answer: string;             // the agent's last answer of the turn, Markdown
};

type Delivery = 'delivered' | 'queued' | 'failed';
```

- `GET /api/client/v1/me` → `{user: {id, email, name}, hub: {url}, scopes, machines: Machine[]}`
- `GET /api/client/v1/chats?filter=all|attention|working&machine=&limit=` → `{chats: Chat[], counts: {all, attention,
  working}}`, newest prompt first; `limit` 1–200 (100). The counts are of all the person's chats, whatever the filter: a
  menu-bar counter reads `counts.attention`. Test chats are left out.
- `GET /api/client/v1/chats/{id}?limit=` → `{chat: Chat, turns: Turn[], complete}`, the last `limit` turns (1–50, 10).
- `POST /api/client/v1/chats/{id}/messages` `{text, mode?: 'queue'|'now', clientId?}` →
  `{delivery, chatId, machineId, clientId, url, error?, queueId?}`. A machine that is on takes it at once (200); one
  that is off gets it when it connects again, as a message written on the page would (202, `queued`, see
  [offline-prompts.md](offline-prompts.md)). `clientId` (`[A-Za-z0-9_-]{1,64}`) makes a retry safe: the machine takes
  one id once.
- `POST /api/client/v1/chats` `{machineId, projectId | cwd, provider?, model?, text, worktree?, clientId?}` →
  `{delivery, machineId, clientId, id?, url?, error?, queueId?}`: a new chat with the first message. `provider` is
  `claude` by default.
- `POST /api/client/v1/chats/{id}/stop` → 204: stops the agent's turn. 502 `machine_offline` while the machine is off.
- `POST /api/client/v1/chats/{id}/read` → 204: marks the chat read. 502 `machine_offline` while the machine is off.
- `GET /api/client/v1/projects?machine=` → `{projects: [{machineId, machineName, online, projectId, cwd, name,
  providers, lastUsedAt, chats}]}`: the folders the person's chats ran in, most recent first — what a "new chat" form
  offers.
- `GET /api/client/v1/machines/{id}/agents` → `{machineId, agents}`: the machine's agents and their models, as the
  machine reports them (the `MachineAgent` of the v2 protocol). 502 `machine_offline` while the machine is off.

Answering an agent's question or permission is not in the API: the app opens `url`.

## Clients

`clients/raycast` is the Raycast extension: several hubs, the chats with their summaries, a reply, a new prompt, a
menu-bar counter of the chats that wait for the person.
