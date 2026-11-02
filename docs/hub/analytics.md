# Overview, money, KPI, signals, changes: the contract

Part of the [hub contract](api.md). Rights: [orgs.md](orgs.md); what machines send: [usage.md](usage.md); limits:
[limits.md](limits.md). TS types: `web/app/hub-api/analytics.ts` and `common.ts`; mock fixtures for the three roles
(organization admin, team lead, member): `web/app/hub-api/fixtures/analytics/*.json`, checked by
`web/app/hub-api/contracts.test.ts`.

All endpoints are under `/api/orgs/{org}` unless they start with `/api/me`, need a session and follow `HUB_ORGS`
([orgs.md](orgs.md#flag)). `scope` is `kind:id`; the scope must be one the person may view (`403 forbidden_scope`
otherwise), and money needs `Capabilities.money` on it.

## Money and periods

- Amounts are kept in USD (integer micro-dollars) and sent as `Money = { usd, shown }`: `shown` is in the organization's
  currency at the month's rate. Every answer with money carries `currency: { code, perUsd, rateDate, source }`; the rate
  is the Central Bank of Russia's on the 1st of the month, an admin may correct it (`source: 'manual'`); `'none'` while no
  rate is known (`shown` = `usd`).
- Two numbers per amount: **`list`** — the estimate at list prices (`prices`, by version); **`paid`** — what it costs:
  on a key, the list price (reconciled daily with the provider's bill where an admin key exists); on a subscription, the
  account's `monthlyFee` split between the people in proportion to their `list` on it in the month (preliminary until the
  month closes, fixed on the 1st). A call of a model without a price adds tokens and `unpricedCalls`, never money.
- **Who pays.** Organization budgets, limits and spend count only `funding: 'org'`. Personal accounts (a person's own
  subscription or key, and any account the hub has not been told about) are not in the organization's money; an admin
  sees their activity and KPI, not their money. The person sees their own money in `/api/me/usage`.
- **Periods.** `period=month|week|day` (the current one) or `from`/`to` (`YYYY-MM-DD`, `to` exclusive), cut in
  `orgs.timezone`. The comparison period defaults to the same length right before (`cmpFrom`/`cmpTo` move it).
- A **session** belongs to one team: the machine's team when the person is in it, otherwise the person's primary team in
  that tenant. Subagents' money goes to their parent session; `eval` and `automation` sessions count in money and never
  in KPI.

## KPI

Twelve, each a function of `agent_sessions.metrics` (the machine's scanner fills them, `usage.md`) over the live,
top-level sessions of the scope and period. `n` is the sessions behind a value; a value of fewer than 5 sessions is
`null` with `verdict: 'too_few'`. A difference smaller than the noise (±2σ of the weekly values of the 8 weeks before)
is `verdict: 'noise'`, shown as «не отличить от шума».

| `kpi` | Name | One phrase | Formula | `metrics` fields | Unit, better |
| --- | --- | --- | --- | --- | --- |
| `money_per_mr` | Деньги на MR | what one merged MR cost | Σ `paid` of the sessions (with their subagents) ÷ Σ `mrsMerged` | `mrsMerged`; money from `usage_calls` | usd, down |
| `completion` | Доходимость | how many sessions end in an MR | sessions with `mrs ≥ 1` ÷ sessions with `turns ≥ 3` | `mrs`, `turns` | share, up |
| `time_to_mr` | Время до MR | from the start of a session to its first MR | median of `firstMrMs` over sessions with `mrs ≥ 1` | `firstMrMs` | ms, down |
| `mrs_per_person` | MR на человека | merged MRs per active person per week | Σ `mrsMerged` ÷ active people ÷ weeks of the period | `mrsMerged` | count, up |
| `autonomy` | Автономность | how much of the work the agent does on its own | Σ `agentMs` ÷ Σ (`agentMs` + `waitMs`) | `agentMs`, `waitMs` | share, up |
| `waiting_human` | Ждёт человека | how long the agent waits for the person after it stops | median of `waitMs ÷ waits` over sessions with `waits ≥ 1` | `waitMs`, `waits` | ms, down |
| `parallelism` | Параллельность | how many sessions a person runs at once | Σ `agentMs` of the person's sessions ÷ the union of their working intervals, averaged over people | `agentMs`, `activeIntervals` | count, up |
| `tool_errors` | Ошибки инструментов | how often a tool call fails, hooks, blocks and gates aside | Σ `toolErrors` ÷ Σ `toolCalls` | `toolCalls`, `toolErrors` | share, down |
| `reread_context` | Перечитанный контекст | the share of input that is context carried again | Σ `carryTokens` ÷ Σ (`inputTokens` + `cacheReadTokens` + `cacheWriteTokens`) | `carryTokens`, `inputTokens`, `cacheReadTokens`, `cacheWriteTokens` | share, down |
| `session_start_ctx` | Старт сессии | how much context a session starts with | median of `firstCtx` | `firstCtx` | tokens, down |
| `cache_hit` | Кэш | the share of input read from the cache | Σ `cacheReadTokens` ÷ Σ (`inputTokens` + `cacheReadTokens` + `cacheWriteTokens`) | `cacheReadTokens`, `inputTokens`, `cacheWriteTokens` | share, up |
| `skills_in_use` | Скиллы в ходу | of the skills offered, how many are used | Σ `skillsLoaded` ÷ Σ `skillsListed` | `skillsListed`, `skillsLoaded` | share, up |

`metrics` keys (numbers): `turns`, `agentMs`, `waitMs`, `waits`, `activeIntervals` (the session's working intervals,
encoded as a count here and kept as pairs on the machine), `toolCalls`, `toolErrors`, `carryTokens`, `firstCtx`,
`inputTokens`, `cacheReadTokens`, `cacheWriteTokens`, `skillsListed`, `skillsLoaded`, `mrs`, `mrsMerged`, `firstMrMs`.
Until the machine's scanner fills the MR fields, an installation may fill them from its own merge-request source.

## Signals («Требует внимания»)

A signal lives while its condition holds, deduplicated by `rule + subject`, can be snoozed, and is addressed to the lowest
level that can act on it; after 48 hours without action it is raised one level (`escalated: true`). A lead sees their
team's signals; a member only their own.

| `rule` | When | `action.kind` |
| --- | --- | --- |
| `budget_runs_out` | at the pace of the last 7 days the budget ends before the period does | `raise_limit` |
| `over_80` | a person or a team passed 80% of a limit | `raise_limit` |
| `window_90` | a shared account's window is at 90% or more before its reset | `move_account` («Перекинуть на №N») |
| `tool_errors_up` | `tool_errors` rose above the noise | `open_kpi` |
| `invite_pending` | an invitation waits 3 days | `resend_invite` |
| `machine_offline`, `machine_idle` | a machine is offline, or idle for a week | `open_machine` |
| `unknown_account` | a machine of the organization used an account nobody registered | `register_account` |
| `model_no_price` | calls of a model without a price | `open_prices` |
| `invoice_mismatch` | the estimate differs from the provider's bill by more than 5% | `open_kpi` (money) |
| `chat_waiting` | a chat waits for its person for more than an hour | `open_chat` |
| `limit_request` | a person asked for more («Попросить больше») | `raise_limit` |

## Endpoints

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET …/overview?scope=&period=` | view (money parts need money) | | `OverviewResponse`: spend against the limit with the forecast, four tiles, «Требует внимания», spend by teams (org, tenant) or by people (team), subscriptions |
| `GET …/signals?scope=&all=1` | view | | `SignalsResponse` — the person's open signals in the scope; `all=1` adds the snoozed |
| `POST …/signals/{id}/snooze` | the addressee or above | `{ until }` (≤ 30 days) | `{ signal: Signal }`; journal `signal.snooze` |
| `GET …/spend?scope=&by=team\|person\|model\|account\|tenant&period=` | money | | `SpendResponse` — each row with its limit and forecast; sums of rows equal the total |
| `GET …/subscriptions?scope=` | money | | `SubscriptionsResponse` — each subscription's fee split between people, its windows |
| `GET …/kpis?scope=&from=&to=&cmpFrom=&cmpTo=&series=team` | view | | `KpisResponse` — all twelve with the comparison; `series=team` adds them per team («Команды рядом») |
| `GET …/kpis/{kpi}/series?scope=&from=&to=&by=day\|week&split=team` | view | | `KpiSeriesResponse` — points, splits per team, markers of changes |
| `GET …/changes?scope=&from=&to=` | view | | `ChangesResponse` — the scope's changes and its ancestors' (they touch it too) |
| `POST …/changes` | manage of the scope | `{ scope, title, at }` | 201 `{ change: Change }`; journal `change.create` |
| `GET …/changes/{id}/effect?scope=` | view | | `EffectResponse` — 7 days before against 7 after, live sessions, every KPI; `overlaps` lists other changes in the windows (then `verdict: 'mixed'`) |
| `GET …/waste?scope=&period=` | money | | `WasteResponse` — «Куда уходят деньги впустую» |

### Machines

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET …/machines?scope=&problems=1` | view | | `MachinesResponse` — the scope's machines, those with problems first; `problems=1` only those |
| `PATCH /api/machines/{id}` | manage of its tenant (team: the team's lead may change `access`) | `{ name?, teamId?, access? }` | `{ machine: HubMachine }`; journal `machine.team`, `machine.access` |
| `POST /api/machines/{id}/restart` | manage | `{ what: 'runner' \| 'vm', when: 'now' \| 'after_chats' }` | 202 `{ jobId }`; `runner` is the agent operation `runtime.restart`, `vm` goes to the cloud (Selectel) |
| `POST /api/machines/{id}/disable` | manage | `{}` | `{ machine }` — the link is closed, the key kept; the machine cannot be opened |
| `POST /api/machines/{id}/enable` | manage | `{}` | `{ machine }` — it comes back with the same files |

The existing `PATCH /api/machines/{id}` `{ name }` keeps working as it does.

### The person: `/api/me/*`

Every signed-in person, in any organization or none; only their own sessions.

| Method, path | Request | Response |
| --- | --- | --- |
| `GET /api/me/usage?period=&org=` | | `MeUsageResponse` — their money on organization and personal accounts, by day, model and account; `accountedUntil` is «на сервере учтено до …» |
| `GET /api/me/sessions?from=&to=&before=&limit=50` | | `MeSessionsResponse` — their sessions with price and result, no text |
| `GET /api/me/kpis?from=&to=&cmpFrom=&cmpTo=` | | `KpisResponse` with `scope: 'me'` |
| `GET /api/me/limits` | | [limits.md](limits.md) |
| `POST /api/me/limit-requests` | `{ amountUsd, reason, scope }` | 201 `LimitRequestResponse` — a `limit_request` signal to those who may raise it |

The agent app's «Управление» screen keeps its local history (`/api/platform/stats` of the runtime) and adds these with
the «на сервере учтено до …» line.

As built (HRN-590):

- Days are cut in the zone of `?org=`, of the person's only organization, or `Europe/Moscow`.
- `paid` is computed at read time from the hourly rollup: a subscription's fee × the row's `list` ÷ everyone's `list` on
  that account in the calendar month; a key or prepaid balance is its `list`; an account the hub does not know is 0.
- `accountedUntil` is the latest ack the hub gave on the `usage` stream of the machines that ran the person's sessions;
  `null` until one has been read.
- `parallelism` takes each session's span from start to end (the working intervals stay on the machine).
- `/api/me/sessions` pages newest first: `next` is the start of the page's last session, sent back as `before`.
- The endpoints follow `HUB_ORGS` like the rest of the organizations' API (404 when off, instance admins only in
  `shadow`).

As built (HRN-613): the organization's `…/overview`, `…/spend`, `…/kpis`, `…/changes` and `…/waste`, and the rate.

As built (HRN-613, machines):

- `…/machines` lists the machines of the scope's tenants (a team scope: the machines in that team), those with problems
  first, then by name. A row is computed from what the hub holds: `status` from the link, `lastActiveAt` and
  `chatsRunning` from the top-level `live` sessions of the machine (running: not ended and moved in the last 15
  minutes), `people` from those sessions of the last 30 days, `access` from the new `machine_access` table (migration
  `0010_machines.sql`; empty means the whole tenant).
- Problems: `disabled` (alone, when the machine is switched off), `offline` (seen once, no link now), `idle_week` (no
  chat for 7 days; not for a machine that never connected), `agent_old` (below `HUB_MIN_AGENT_VERSION`), `disk_low`
  (the last preflight reported less than 10% or 5 GiB free), `login_expired` (an installed agent reports
  `logged_out`).
- `cloud` is always `null` and `currency` is only the organization's: no cloud provider is connected to the hub, so
  there is no flavor and no price.
- `access` is stored and shown, not enforced: opening and the proxy still follow the tenant's membership.
- `PATCH` with `teamId` or `access` answers `{ machine: HubMachine }`; a name alone keeps the old answer (the machine as
  the space sees it). The team and every entry of `access` must belong to the machine's tenant. A lead of the machine's
  team may send `access` alone. Journal: `machine.team`, `machine.access`, and `machine.disable`, `machine.enable`.
- `disable` closes the link with code 4003 (the agent waits and dials again) and the hub answers its dial with 423
  `machine_disabled` until `enable`; the key and the files stay. A switched-off machine answers 423 to `open` and to
  `restart` too.
- `restart` with `what: 'runner'` sends the agent operation `runtime.restart` `{ jobId, when }` and answers 202
  `{ jobId }`; an agent that does not know it gives 422 `restart_unsupported`. `what: 'vm'` answers 501
  `not_implemented` («Перезапуск ВМ пока недоступен»): the hub has no cloud provider to ask.
- All of it follows `HUB_ORGS` (404 when off, instance administrators only in `shadow`).

- **Rate.** The organization's `currency` (default `RUB`) is shown at the Central Bank's rate on the 1st of the period's
  first month, read once from `HUB_RATES_URL` (default `https://www.cbr.ru/scripts/XML_daily.asp`, `off` never fetches)
  and kept in `currency_rates`; a `manual` row there wins over the fetched one and is never overwritten. A month in the
  future, or a rate that cannot be fetched (tried again after 10 minutes), is shown in dollars with `source: 'none'`.
  The same rate is in `/api/me/*` and `…/limits`.
- **Who.** `…/overview` needs view of the scope, `…/spend` money; both are a manager's (org or tenant admin, the
  team's lead). `scope` is `kind:id`, the organization by default.
- **Money** counts the hourly rollup of `funding: 'org'` in the scope (a tenant by the session's tenant, a team by its
  team). `forecast` is `paid` plus the pace of `paid` over the last 7 days for the period's remaining days; `null` for a
  period that has ended. Rows: `team` («Без команды» for sessions without one), `person`, `model`, `account`, `tenant`;
  sorted by `paid`. `guessedShare` is 0 for now.
- **Limit bars** are monthly money rules (not `forbid`): the total the scope's own rule (no person, account or model);
  a team or tenant row that scope's own rule; a person the most used personal rule in the scope or under it; an account
  the most used account rule in the scope; a model none. `setBy` is the person who set it.
- **Tiles** take the live top-level sessions of the scope that started in the period (any funding) and the same for the
  comparison period: `activePeople` distinct people, `sessions`, `mrs` Σ `mrsMerged`; `moneyPerMr` is Σ `paid` of the
  organization's sessions ÷ their `mrsMerged`, `null` without MRs.
- **«Требует внимания» and signals** are kept in `org_signals` (migration 0009, additive), one row per `rule + key`
  (`over_80`: the limit rule's id, `budget_runs_out`: the scope, `limit_request`: person + scope). The overview's
  attention block and `GET …/signals` run the same code: it computes the signals of the scope from the limits as they
  stand, upserts them (a known one keeps `firstAt` and its snooze, `lastAt` is now) and resolves those whose condition no
  longer holds (`resolved_at`; if the condition returns it is a new signal with a new `firstAt`). A signal that got
  worse (to `critical`) loses its snooze. Signals are about now: they are always computed from the current month, in the
  organization's zone, whatever period the overview shows, so a closed month shows the same signals as this one. The
  overview takes the not snoozed ones, `critical` first, at most 6.
  - **Computed rules.** `over_80` (every rule of the scope or under it at 80% or more, `critical` when it stops or is
    spent; a personal rule is addressed to its person, a team's or tenant's to the managers of its scope) and
    `budget_runs_out` (the scope's own limit is below the forecast; computed, and resolved, only for the scope that is
    being looked at, as the forecast is its own), and `limit_request` (below). The other rules of the table
    (`window_90`, `tool_errors_up`, `invite_pending`, `machine_offline`, `machine_idle`, `unknown_account`,
    `model_no_price`, `invoice_mismatch`, `chat_waiting`) are not computed yet and never appear.
  - **Who sees what.** A signal's `scope` is where it is dealt with (a personal limit's: the scope of the rule). `GET
    …/signals?scope=&all=1` shows the open signals of the scope **and under it** to a manager of the scope (view, as
    everywhere in this section); a member of the scope (view is refused) gets the same list cut to the signals about them
    or addressed to them, not 403 (a contract deviation: the contract says «a member only their own»); somebody outside
    the scope still gets 403. `all=1` adds the snoozed ones (`snoozedUntil` set).
  - **Escalation** is computed on read, not stored: a signal that is not snoozed and has waited 48 hours since
    `max(firstAt, last snooze)` has `escalated: true`, and escalated ones go before the others of their severity. Since
    a manager sees the whole subtree, «raised one level» means the signal gets to the level above through this flag and
    the order, not through a second row.
  - **`POST …/signals/{id}/snooze`** (the addressee, or manage above the signal's scope) takes `until` as RFC 3339 or a
    day (its midnight in the organization's zone); it must be in the future and at most 30 days ahead, otherwise 400 with
    `details.field = 'until'`. It returns `{ signal }` and journals `signal.snooze` (target kind `signal`, labeled by the
    signal's title; before `snoozedUntil`, after the rule, title and the new `snoozedUntil`) in the signal's scope. A
    resolved signal is 404.
  - **`POST /api/me/limit-requests`** (`{ amountUsd, reason, scope }`; the person must belong to the scope: be in the
    team, in the tenant, in the organization) takes `amountUsd` > 0 and a `reason` of 1–500 characters (400 otherwise),
    makes (or renews, one per person and scope: the new sum and reason, `firstAt` kept, the snooze lifted) a `limit_request`
    signal, `warn`, `action: raise_limit` with the scope and `userId` = the person, and answers 201 `{ request: { id,
    status: 'sent', to } }` with `id` the signal's. `to` is the lowest level that can raise the person's limit: a team's
    lead(s), else the tenant's admins and owners, else the organization's admins; the person never asks themselves.
    No such person is 409 `no_recipient`. The request is resolved when a money limit is set in its scope for the person
    (or the scope's own) after the request was last made. There is no other state: a request is not «approved» or
    «declined», it is answered by the limit.
- The overview's `subscriptions` is the list of `…/subscriptions` for the same scope and period.
- `accountedUntil` is the latest ack on the `usage` stream of the machines of the scope's sessions.
- **`…/subscriptions`** (money) lists the organization's subscription accounts (`funding: 'org'`, kind `subscription`),
  highest `monthlyFee` first, then by label. An organization sees all of them; a tenant or a team the ones that belong
  or are granted to it, above it or in it (`account_grants`), and any it has spent through in the period. An account with
  no use in the period is there with `shares: []`. `shares` are the people of the scope who spent on the account in the
  period: `share` is their `list` ÷ everyone's `list` on the account in the period (4 decimals; in a team the shares add
  up to less than 1), `amount` is the `paid` that `…/spend?by=person` counts for them on this account (the fee × the
  row's `list` ÷ everyone's, hour by hour), so the amounts of everyone add up to the fee for a whole month; biggest
  amount first. `preliminary` is true while the last month of the period is open. `windows` are all the account's
  measured windows (`account_windows`, [usage.md](usage.md)); `usedPct` is as measured (or `used` ÷ `limit` for a
  provider that counts in units), `waiting` is true once `resetsAt` has passed with no new measurement (the old value is
  kept), `forecastFullAt` is when the window fills at the pace it has had since it began, `null` if it would not before
  the reset. `windowPct` is the person's part of the shortest still-open window of all models: its `usedPct` × their
  share of the account's `list` over that window's hours (percent, «примерно», as `window_pct` in [limits.md](limits.md)),
  `null` with no such window or no use in its hours. The overview fills its `subscriptions` the same way.
- **`…/kpis`** (view) are the twelve KPI of `/api/me/kpis` over the scope's live top-level sessions (any funding), with
  `scope` the `{kind, id}` object; `money_per_mr` counts only the organization's sessions, their `paid` ÷ their
  `mrsMerged`, as `moneyPerMr` does. `series=team` adds them per team of the scope, sorted by name; without it `series`
  is `null`.
- `GET …/kpis/{kpi}/series` — one KPI by `day` (default) or `week` (7-day buckets from `from`, the last cut at `to`);
  a bucket below 5 sessions is `null`. `split=team` adds a line per team. `markers` are the changes of the scope's
  chain in the period; a marker's `verdict` is that change's effect on this KPI (`mixed` when another change falls in
  its windows). Unknown KPI — 404.
- **Changes** are kept in `org_changes` (migration 0008, additive). `GET …/changes` (view) lists the scope's changes and
  its ancestors', oldest first, `from`/`to` bounding `at`. `POST …/changes` (manage of the body's `scope`, the
  organization when it is left out) takes `at` as a day (its midnight in the organization's zone) or RFC 3339, not later
  than a day from now, and a title of 1–200 characters; it journals `change.create` with the title as the target's label.
  Only `kind: 'manual'` is made; detected changes (versions, models, skills, policies) are not yet. `…/changes/{id}/effect`
  (view; `scope` the change's own by default, else one the change touches) compares the 7 days before the change's day
  with the 7 days from it, by the KPI of `…/kpis` (`before` = `cmpValue`, `after` = `value`); any other change of the
  scope's chain in those 14 days goes to `overlaps` and turns each verdict but `too_few` into `mixed`.
- **`…/waste`** (money) is where the organization's list money went to waste, over the live top-level sessions of the
  scope that started in the period with `funding: 'org'` only; each session counts at its list money (subagents'
  calls included). Three kinds: `reread_context` is Σ list × min(1, `carryTokens` ÷ (`inputTokens` + `cacheReadTokens` +
  `cacheWriteTokens`)); `failed_tools` is Σ list × min(1, `toolErrors` ÷ `toolCalls`); `abandoned_sessions` is the whole
  list of the ended sessions with 3 turns or more and no MR (the `completion` KPI's rule). `sessions` is how many
  contributed, `share` the item's part of the scope's whole list spend (0 when it is 0, 3 decimals). Items of zero are
  left out, the rest are sorted by amount; `items` is `[]` when none. `long_waits` and `oversized_model` are not
  computed yet and are not in the answer.
