# Spend from the machines: the contract

Part of the [hub contract](api.md). Go types: `internal/hubproto/usage.go`; envelopes: `internal/hubproto/testdata/usage.json`
(a golden test makes them from the types). What the hub does with the facts — money, limits, KPI — is in
[analytics.md](analytics.md) and [limits.md](limits.md).

## Principle

The text of conversations stays on the machine. The hub gets **facts without text**: model calls with tokens, sessions
with numbers, measurements of rate-limit windows and balances, who started a session and with which account. A field
that could carry text (a prompt, a title, a path, a folder) is not in any fact, and a test fixes the field list.

## The outbox and the cursor

- The machine keeps an outbox (`~/.harnessly/usage-outbox`, next to the other outboxes) of records, each with a
  monotonic `seq` from 1 within an **epoch**. The epoch is a random id made with the outbox; a wiped data folder makes a
  new epoch, and the hub, seeing a new epoch, reads it from the start.
- The hub pulls: the agent sends `usage.available{epoch, seq}` after new records (at most every 10 s) and once after each
  connect. The hub calls `usage.read{epoch, after, max}` from its cursor (`ingest_cursors`, per machine and stream),
  writes the batch in one transaction, moves the cursor, then sends `usage.ack{epoch, seq}`. The agent may drop records up
  to an acknowledged seq. While the machine is offline the cursor stands still.
- A new machine starts at `after = 0`: the outbox is first filled from the facts the runtime's report already keeps
  (400 days, `internal/usagereport`), so the first reads are the backfill. Batches are at most `max` records
  (`1 ≤ max ≤ 1000`).
- The same `usage.read` (same epoch and `after`) returns the same records until they are acknowledged. A record never
  changes once it has a seq.
- The hub pulls over the machine's own link, so it needs no credential of the runtime (the report endpoint
  `/api/platform/stats` stays a page-session endpoint for the person).
- `hello.features` contains `usage` when the agent answers these operations; the hub does not call them otherwise.

| `op` / `event` | Params | `result` |
| --- | --- | --- |
| event `usage.available` | `{ epoch, seq }` — the last seq written | |
| `usage.read` | `{ epoch, after, max }` (`epoch: ""` for a first read) | `{ epoch, records: UsageRecord[], last, more }` — records with `seq > after` in order; `last` = the last record's seq (`after` when empty); when `epoch` differs from the asked one the batch starts at the new epoch's beginning |
| `usage.ack` | `{ epoch, seq }` | `{}` |

## Records

```ts
type UsageRecord = {
  seq: number; kind: 'call' | 'session' | 'window' | 'balance';
  at: number;                 // when the machine wrote it, epoch ms, the machine's clock
  call?: UsageCallFact; session?: UsageSessionFact; window?: UsageWindowFact; balance?: UsageBalanceFact;
};

type UsageCallFact = {
  provider: 'claude' | 'codex';  // the agent CLI that made the call
  msgKey: string;                // the provider's own id of the message: Claude message id / request id, Codex response id
  sessionKey: string;
  ts: number;                    // epoch ms UTC
  model: string;                 // as the transcript names it
  baseUrlHost: string;           // where the call went: 'api.anthropic.com', 'api.kimi.com', …; '' = the CLI's default
  input: number;                 // tokens not read from the cache (Codex: its input minus its cached input)
  output: number; cacheRead: number; cacheWrite5m: number; cacheWrite1h: number;
};

type UsageSessionFact = {
  provider: 'claude' | 'codex';
  sessionKey: string;
  parentKey: string;             // '' for a top-level session; a subagent's parent session
  startedAt: number; endedAt: number | null;
  origin: 'live' | 'eval' | 'automation';
  subject: string;               // email of the person who started it (from runtime.session); '' unknown
  accountRef: string;            // see below; '' unknown
  authMode: 'subscription' | 'api_key' | '';
  baseUrlHost: string;
  attribution: 'exact' | 'guessed';
  metrics?: Record<string, number>;   // the machine's scanner (analytics.md, «KPI»); absent until it ran
  mrKeys?: string[];                  // merge requests the session mentioned, host/project/!iid (host lower case), up to 200; analytics.md, «Merged MRs». The hub ignores unknown fields, so a machine may send it before the hub knows it
};

type UsageWindowFact = {
  provider: string; accountRef: string;
  name: string;                  // 'five_hour', 'seven_day', 'seven_day_opus', …
  scope: string;                 // 'all' | 'model:<family>'
  lengthSec: number; unit: 'pct' | 'tokens' | 'requests' | 'credits';
  usedPct: number | null; used: number | null; limit: number | null; resetsAt: number | null;
  measuredAt: number;
  source: string;                // 'statusline' | 'oauth-usage' | 'wham-usage' | 'codexbar' | …
  confidence: 'official' | 'undocumented' | 'estimated';
};

type UsageBalanceFact = {
  provider: string; accountRef: string;
  currency: string;              // 'USD' | 'CNY' | 'RUB' | 'credits'
  available: number | null; spendCap: number | null; spentInPeriod: number | null;
  measuredAt: number; source: string;
};
```

A session is sent again whenever it changes (new calls, its end, its metrics); the hub keeps the newest by seq. A window
is a measurement; the hub keeps the freshest by `measuredAt`, and a measurement with `usedPct: null` after a reset does
not erase the window: the UI says «ждём замер».

## Rules

**Dedup.** A call is unique by `(org, provider, msgKey)` (`usage_calls`). Message ids are global, so a resumed or forked
session (which copies the earlier history with the original ids), a chat moved to another machine and a repeated batch
all add nothing. A call seen first from one machine stays with the session and person it came with first.

**Subagents.** A subagent's session has `parentKey`. Its calls are money of the parent session (and so of the parent's
person, team and account), but not prompts and not KPI. Subagent and workflow transcripts are read at any depth of the
session folder (`subagents/`, `workflows/wf_*/agent-*.jsonl`).

**Origin.** `eval` and `automation` sessions count in money and limits, never in KPI.

**Who and with what.** The hub opens a runtime with `runtime.session{subject}`, so the runtime knows who opens a tab; the
SessionStart hook records `sessionKey → { subject, accountRef, baseUrlHost }`. Without that record the session is the
machine creator's on the machine's login, `attribution: 'guessed'`; the UI shows the guessed share.

**`accountRef`** — never a key or a token, only what identifies the account:

| Account | `accountRef` |
| --- | --- |
| Claude subscription (OAuth) | `claude:<account uuid>` (from the CLI's account record) |
| Anthropic API key | `key:sha256:<first 16 hex of SHA-256 of the key>` |
| Codex / ChatGPT login | `codex:<account id>` |
| OpenAI API key | `key:sha256:<16 hex>` |
| Another endpoint behind Claude Code or Codex (Kimi, Z.ai, OpenRouter, …) | `key:sha256:<16 hex>` of that endpoint's key; `baseUrlHost` names the provider |

The hash is computed on the machine. A test puts a fake key in the environment and checks it is nowhere in the outbox.

**Price by provider.** The price of a call is looked up by the provider the call went to — from `baseUrlHost`
(`api.kimi.com` → Kimi, `api.z.ai` → Z.ai, `openrouter.ai` → OpenRouter, empty or `api.anthropic.com` → Anthropic) — and
the model. A model with no price row gets **no price** (`list_cost = NULL`, a «модель без цены» signal), never a
neighbour's price and never 0.

**Time.** Every time on the link is epoch milliseconds UTC, and the hub stores UTC. Days and months are cut in the
organization's time zone (`orgs.timezone`, default `Europe/Moscow`) when the hub rolls up; a space without an
organization is cut in `Europe/Moscow`. If the machine's clock differs from the hub's receiving time by more than 5
minutes (`at` against the time the batch arrived), the hub shifts the batch's times by the difference and marks the rows
(`usage_calls.ts_shifted`). Only a clock **ahead** can be told this way (a record «from the future»): a record of a clock
behind looks the same as a backlog written earlier, so such a batch is kept as the machine wrote it.

**Flag.** `HUB_USAGE_INGEST=off|on` (default `off`): with `off` the hub never calls `usage.read`, does not subscribe to
`usage.available`, and the tables stay empty. With `on` the hub seeds the price list on start and pulls each machine whose
`hello` has `usage` on connect and on every `usage.available`, one pull per machine at a time (`internal/hub/ingest`).

**How the hub stores a batch** (one transaction with the cursor; `usage.ack` after the commit):

- A machine nobody claimed (the local machine before an admin takes it) is not read: it has no organization.
- `call` → `usage_calls`, priced by `prices` at its time; a call already there adds nothing.
- `session` → `agent_sessions`. The person is the hub user of `subject`; an unknown or empty `subject` makes it the
  machine creator's, `guessed`. The team is the machine's when the person is in it, otherwise the person's primary team in
  the machine's tenant, otherwise their first one there. On a new epoch the machine's sessions forget their seq, so the
  new outbox's records win.
- `accountRef` of a session, window or balance → the organization's account of that provider and ref; an unknown one is
  made `discovered`, personal, owned by the person it came with (the machine creator for windows and balances), kind
  `api_key` for a `key:` ref and `subscription` otherwise, until an admin registers it (`hub seed`, `accounts:`,
  orgs.md). A session record without `accountRef` keeps the account the session already has.
- `window` and `balance` → `account_windows`, `account_balances` (only a newer measurement replaces).
- A kind the hub does not know is skipped, so an agent newer than the hub never stops the cursor.
- `usage_rollup_hour` (key: the UTC hour start in epoch ms) is summed anew for every hour the batch touched: the hours of
  new calls and of every call of a session that changed. A subagent's call is summed under its parent session's person,
  team, space, account and funding. `paid` is the list price on an `api_key` or `prepaid` account and 0 on a
  subscription (its fee is split when the screens read, analytics.md).

## Where it is kept

`0005_usage.sql`: `usage_calls` (unique `org_key, provider, msg_key`), `agent_sessions`, `accounts`, `account_grants`,
`account_windows`, `account_balances`, `prices`, `usage_rollup_hour`, `ingest_cursors`. Money is integer micro-dollars.
`org_key` is the organization's id, or `space:<id>` for a space of the implicit organization.

The price list (`prices`) has one row per provider, price key and `effective_from` (epoch ms); a call is priced by the row
in effect at its time and keeps that row's `effective_from` as `price_ver`. The key is the Claude family
(`opus-4-6`) for Anthropic, the table's model for OpenAI, the model name for the rest (`internal/hub/usage.PriceKey`).
The hub seeds the rows of `internal/pricing` (the runtime's numbers, so the two agree) and never overwrites a row that is
there. Prices of other providers are added as rows with their own `effective_from` once confirmed; until then their calls
have no price. On start the hub also prices the stored calls that came without a price by the list as it is now
(`usage.PriceUnpriced`) and sums their hours anew, so a model added to the table later gets its past calls too.
