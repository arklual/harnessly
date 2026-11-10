# Limits and how a machine applies them: the contract

Part of the [hub contract](api.md). Who may set which limit: [orgs.md](orgs.md#rights) (the rights table). TS types:
`web/app/hub-api/limits.ts`; fixtures: `web/app/hub-api/fixtures/limits/*.json`. Go types of the link:
`internal/hubproto/budget.go`, envelopes `internal/hubproto/testdata/budget.json`.

## Rules

A rule limits spend on some axes over a period:

| Axis | Field | Meaning |
| --- | --- | --- |
| scope | `scope` | the organization, a tenant or a team the rule belongs to |
| person | `userId` | set: a personal limit of that person inside the scope (in a team: the personal limit; in a tenant: the personal ceiling); null: the scope's own budget |
| account | `accountId` | set: only spend through that account (an organization's shared limit on it, or a tenant's own narrowing rule) |
| model | `modelPattern` | set: only that model family; with `forbid: true` the model is not allowed at all |
| period | `period` | `month` (calendar month in the organization's time zone), `five_hour`, `seven_day` (the account's windows) |
| unit | `unit` | `money` (USD, `amount` in dollars), `tokens`, `window_pct` (the person's estimated share of an account's window, «примерно») |

Natural key: `(scope, userId, accountId, modelPattern, period, unit)`; `PUT` on the same key replaces the rule. Only
`funding: 'org'` spend counts against rules of an organization, a tenant or a team ([analytics.md](analytics.md#money-and-periods)).

## The tightest one wins

For a person, every rule that applies to a call counts it: their personal limit in the session's team, the team's
budget, their personal ceiling in the tenant, the tenant's budget, the organization's budget, any rule on the account
(the organization's, the tenant's), any rule on the person and the account, and any rule on the model. Each rule has its
own `used` and `pct = used ÷ amount`; its status is `stop` at `pct ≥ 1`, `warn` at `pct ≥ warnPct` (80% unless set),
`ok` below. A `forbid` rule is `stop` at once.

The person's **effective** status for an account and a model is the worst status of the rules that apply, and the
**binding** rule is the one with the highest `pct` (a `forbid` first). Limits are ceilings, not shares: personal limits of
a team may add up to more than its budget (the UI shows «раздано 120%», `handedOutPct`), and then whichever bound is
reached first stops.

**Example: five rules on one person.** Вера is in team «Платформа» of tenant «Acme», October:

| # | Rule | Set by | Used | `pct` | Status |
| --- | --- | --- | --- | --- | --- |
| 1 | personal limit in «Платформа», $100 / month | Борис, the lead | $84 | 0.84 | warn |
| 2 | budget of «Платформа», $1500 / month | Анна, the tenant's admin | $1190 | 0.79 | ok |
| 3 | personal ceiling in «Acme», $150 / month | Анна | $84 | 0.56 | ok |
| 4 | account «Claude Max №2», `window_pct` 90 on `five_hour` (an organization rule for everyone) | Анна, the organization's admin | 92% | 1.02 | stop |
| 5 | model `opus-4-1`, `forbid` (organization) | Анна | | — | stop |

Effective for Вера:

- through «Claude Max №2», any model: **stop**, binding rule 4 — «Окно Claude Max №2 — 92%, сброс в 20:00»;
- through any other account, any model but `opus-4-1`: **warn**, binding rule 1 — «Личный лимит — 84% за октябрь»;
- `opus-4-1` anywhere: **stop**, rule 5 — «Модель opus-4-1 в организации запрещена».

The hub sends exactly these three entries to Вера's machines (`budget.set`). Fixture: `fixtures/limits/vera.json`.

## Enforcement

`HUB_LIMITS_ENFORCE=off|warn|stop` (default `off`): `off` — the hub computes states and shows them, sends nothing to
machines; `warn` — machines notify at `warn` and at `stop` but start chats; `stop` — machines do not start a new chat at
`stop` or with a forbidden model, and stop running ones at their next prompt or tool call. The plan: two weeks of `warn`
on production before `stop`.

The limit is applied on the machine (subscription traffic does not pass any gateway of ours), within 60 s of the change
on the hub; spend past a `stop` is at most one turn. A machine without a link keeps the last list. A hard cut on the
server, for organization keys behind a gateway with per-person virtual keys, is phase 2.

## Endpoints

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET /api/orgs/{org}/limits?scope=&userId=` | view of the scope | | `LimitsResponse`: the rules of the scope and of its ancestors that bind its people, and the effective limit of each person of the scope (or of `userId` only); `handedOutPct` for a team or tenant |
| `PUT /api/orgs/{org}/limits` | by the rights table | `PutLimitRequest` | `{ rule: LimitRule }` — upsert on the natural key; 403 `self_change` (one's own limit, or the budget of a scope one manages oneself), 403 `forbidden_scope`; journal `limit.set` with «было» and «стало» (`self: true` for an organization admin on the organization's limit) |
| `DELETE /api/orgs/{org}/limits/{ruleId}` | as `PUT` for that rule | | 204; journal `limit.remove` |
| `GET /api/me/limits` | the person | | `MeLimitsResponse`: their effective limits and the entries their machines hold |

## On the machine

| `op` | Params | `result` |
| --- | --- | --- |
| `budget.set` | `{ version, mode, issuedAt, entries: BudgetEntry[] }` — the whole list for every person who uses the machine | `{ version }` |

`BudgetEntry = { user (email), account (accountRef, '' any), modelPattern ('' any), status, forbid, pct, resetsAt, reason, ruleId }`.
Only entries that are not `ok` are sent; a person, account or model not in the list is `ok`. The agent ignores a
version not newer than its own. `hello` carries `budgetVersion` (with the `budget` feature), and the hub sends a new
list right after `hello` when the machine's is older, and within 60 s of any change.

What the machine does with an entry, by `mode`:

| Status | `mode = warn` | `mode = stop` |
| --- | --- | --- |
| `warn` | a notice in the chat once per entry version | the same |
| `stop` | a notice; the chat starts | a new chat does not start (the reason and the reset time are shown); a running chat gets the reason at its next prompt or tool call (hook UserPromptSubmit / PreToolUse); a Codex tab is paused through tmux |

## As built (HRN-591)

- Rules live in `limit_rules`. `PUT` upserts on the natural key (scope, person, account, model, period, unit). What each
  machine was last sent lives in `machine_budgets` (version, hash of the list, time).
- States are computed when someone reads them, and by the pusher, from the hourly rollup (`usage_rollup_hour`). Only
  rows paid by the organization (`funding = org`) count. Money is `paid`, the same as in analytics
  ([analytics.md](analytics.md)). Tokens are input + output + cache read and write. Months and days follow the
  organization's time zone, or `Europe/Moscow` for an implicit organization.
- `window_pct` takes the account's last measured window (statusline and the like), so its value is `estimated`. A
  window that has reset since that measurement counts as 0 until the next one. A person's `window_pct` is the window ×
  their share of the account's `list` over the window's hours.
- When a tenant moves to another organization, its rules and its teams' rules are deleted. When a team is deleted, its
  rules go with it.
- With `HUB_LIMITS_ENFORCE=off` the hub keeps no pusher and sends nothing. The endpoints still answer, following
  `HUB_ORGS`.
- The pusher checks every 60 s, after a rule changes, and after a machine's `hello` if the machine has the `budget`
  feature. It sends a list when the list's hash changed or when the machine holds an older version. The version is
  `max(now in ms, last + 1)`. Accounts with no `accountRef` are left out of a machine's list, because a machine cannot
  tell them apart.
- `GET /api/me/limits` shows only the rules that bind the person, for each organization of their spaces (or only
  `?org=`). `machines` lists the version each of their machines holds.
