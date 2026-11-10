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
| `cache_write_1h` | Часовые записи кэша | the share of cache writes asked to live an hour (twice the price of a five-minute write) | Σ `cacheWrite1hTokens` ÷ Σ (`cacheWrite5mTokens` + `cacheWrite1hTokens`) | `cacheWrite5mTokens`, `cacheWrite1hTokens` | share, down |
| `mr_survival` | Доживаемость MR | merged MRs that stayed 14 days without a revert, of those old enough to tell | Σ `mrsSurvived` ÷ Σ `mrsMature` | `mrsMature`, `mrsSurvived` (set by the hub, below) | share, up |
| `waiting_p90` | Ожидание, p90 | how long the agent waits for the person in the slowest tenth of the waits | Σ (`waitP90Ms` × `waits`) ÷ Σ `waits` over sessions with `waitP90Ms` | `waitP90Ms`, `waits` | ms, down |
| `skills_in_use` | Скиллы в ходу | of the skills offered, how many are used | Σ `skillsLoaded` ÷ Σ `skillsListed` | `skillsListed`, `skillsLoaded` | share, up |

`metrics` keys (numbers): `turns`, `agentMs`, `waitMs`, `waits`, `activeIntervals` (the session's working intervals,
encoded as a count here and kept as pairs on the machine), `toolCalls`, `toolErrors`, `carryTokens`, `firstCtx`,
`inputTokens`, `cacheReadTokens`, `cacheWriteTokens`, `skillsListed`, `skillsLoaded`, `mrs`, `mrsMerged`, `firstMrMs`,
`waitingSince` (optional, epoch ms: since when the agent waits for its person), and since HRN-786 `cacheWrite5mTokens` and
`cacheWrite1hTokens` (`cacheWriteTokens` split by the lifetime asked for), `waitP90Ms` (the 90th percentile, nearest rank,
of the session's waits; absent without a wait) and, set by the hub, `mrsMature` and `mrsSurvived`. A session of an older
machine lacks the first three; the KPI built on them count only the sessions that have them, so a period of old sessions
shows «данных пока нет», never a zero.

**Survival of merged MRs (`mrsMature`, `mrsSurvived`).** Over the merged requests that count for a session (above), the
hub counts the ones merged at least 14 days ago (`mrsMature`) and, of them, the ones with no revert merged in the same
project within those 14 days (`mrsSurvived`). A revert is a merged request titled `Revert "<title>"` (as GitLab writes it)
in the same project as the request titled `<title>`. The numbers move with time, so they are computed on every read and
are not stored.

**Merged MRs (`mrsMerged`, `firstMrMs`).** The machine's scanner sends only the keys of the merge requests a session
mentioned (`mrKeys`, `host/project/!iid`, host in lower case; found in pr-link records, in `curl`/`glab`/push output of
Bash calls and in the answer of the MCP `save_merge_request`; `mrs` is their count). The hub does not trust a machine for
«merged»: it polls GitLab itself (`HUB_MR_SOURCE_GITLAB_*`, [api.md](api.md)) every 10 minutes for merged requests
(`merged_mrs`: key, merged and created time, title, author, project, link), starting 60 days back and later a day before
the newest merge, and joins them with the keys of the sessions (`agent_session_mrs`; a subagent's keys count for its
main session):

- a merged MR counts once, for the earliest-started main session that mentioned it: `mrsMerged` is the number of merged
  MRs a session owns;
- `firstMrMs` is the earliest creation time among the merged MRs the session mentioned that were created after the
  session began, minus its start; left out when there is none;
- the join is made when a session is read (`Store.sessions`) and replaces what a machine sent as long as the hub holds
  any merged MR; with no source configured, the fields a machine sends stay as they are.

An MR that was not merged, or is outside the polled groups, does not count. Keys are sent again once after an update of
the machine; a hub of an older revision ignores them.

## Signals («Требует внимания»)

A signal lives while its condition holds, deduplicated by `rule + subject`, can be snoozed, and is addressed to the lowest
level that can act on it; after 48 hours without action it is raised one level (`escalated: true`). A lead sees their
team's signals; a member only their own.

| `rule` | Object (dedup key) | When | Severity | `action.kind`, `target` |
| --- | --- | --- | --- | --- |
| `budget_runs_out` | scope | at the pace of the last 7 days the budget ends before the period does | warn | `raise_limit`, the person or `scopeKind:scopeId` |
| `over_80` | person or team | passed 80% of a limit | warn (critical at 100%) | `raise_limit`, same |
| `machine_offline` | machine | not heard from for `machineOfflineAfter` = 15 min | warn; critical from `machineOfflineCritical` = 24 h | `open_machine`, the machine id |
| `machine_idle` | machine | online, no top-level session for a week | info | `open_machine`, the machine id |
| `invite_pending` | invitation | an invitation by email waits `invitePendingAfter` = 3 days | info; warn from `inviteWarnAfter` = 7 days | `resend_invite`, the invitation id |
| `window_90` | account | an organization-funded account's freshest window (measured within `windowFresh` = 24 h, not yet reset) is at `windowPct` = 90% or more | warn; critical from `windowCriticalPct` = 98% | `move_account` to the account of the same provider and kind with the lowest use under `windowSpare` = 70% (the signal stays without the move when none is spare), the target account id |
| `tool_errors_up` | organization | the `tool_errors` KPI is «worse» over the last `toolErrorsWeek` = 7 whole days against the 7 before them | warn | `open_kpi`, `tool_errors` |
| `unknown_account` | account | a machine of the organization used an account nobody registered (`Discovered`); the detail carries the month's list money | warn | `register_account`, the account id |
| `model_no_price` | model | the hourly rollup of the last `unpricedWindow` = 7 days holds calls of the model without a price (`Unpriced > 0`); no money is guessed | warn | `open_prices`, the model |
| `invoice_mismatch` | account | the estimate differs from the provider's bill by more than 5% | warn | `open_kpi` (money), `money` |
| `chat_waiting` | chat | a chat waits for its person longer than `chatWaitingAfter` = 1 hour (session metric `waitingSince`) | info; warn from `chatWaitingWarn` = 4 h | `open_chat`, the session key |
| `limit_request` | person | a person asked for more («Попросить больше») | warn | `raise_limit`, same |

`action` is `{ kind, label, target, … }`: `target` is the one thing the action is about (a person, an account, a
machine, an invitation, a KPI, a chat, a model; a `raise_limit` of a team or a tenant without a person has
`team:<id>` or `tenant:<id>`), so a button can go to it without knowing the variant; the other fields are the variant's
own.

As built:

- The nine rules after `over_80` and `budget_runs_out` are about the organization as a whole and are found only by
  the background checker, not by a screen read: the screens show what the checker kept. The checker runs for the
  organizations with the `ui.hx` flag on ([api.md](api.md), «Features»): every 5 minutes (`checkEvery`) and, after an
  event that can change a rule (a limit set, a machine going offline), 10 seconds after it (`triggerAfter`; events
  within it share one run).
- One open signal per rule × object. A signal whose condition no longer holds is resolved by the checker; a
  rule with no data to look at (no machines, no windows) finds nothing. A snoozed signal stays hidden until its time
  or until its severity rises.
- `invoice_mismatch` is defined but finds nothing: the hub holds no provider invoices to compare the estimate with.
  The rule starts to work when an invoice source is written; nothing is guessed meanwhile.
- `chat_waiting` needs the session metric `waitingSince` (epoch ms; the moment the agent began to wait for its
  person). The machine's scanner does not set it yet, so until it does the rule finds nothing; a chat that is ended does
  not count. The signal is addressed to the person of the session.
- `tool_errors_up` uses the sessions-based `tool_errors` KPI and does not need the daily per-tool table; the table
  can add «which tool» to the detail when it exists.
- New signals are announced on the organization's stream as `signal.new` ([api.md](api.md), «Events»).

## Today and efficiency

`GET …/overview` carries two more blocks, both counted from the same sessions and hourly rows as the rest of the screen
. `today` is «Сегодня», `efficiency` is «Эффективность». Fixtures: `overview-admin.json`, `overview-lead.json`.

```
"today": {"day": "2026-10-31", "spend": {"paid": {"usd": 86.4, "shown": 7041.6}, "list": {"usd": 104.2, "shown": 8492.3}, "tokens": 18400000}, "active": {"people": 3, "of": 4}, "tasks": {"closed": 5, "inWork": 2}, "sparks": {"spend": [{"day": "2026-10-18", "value": 52.1}, {"day": "2026-10-19", "value": 61.0}, {"day": "2026-10-20", "value": 48.3}, {"day": "2026-10-21", "value": 0}, {"day": "2026-10-22", "value": 0}, {"day": "2026-10-23", "value": 35.7}, {"day": "2026-10-24", "value": 72.4}, {"day": "2026-10-25", "value": 66.9}, {"day": "2026-10-26", "value": 58.2}, {"day": "2026-10-27", "value": 71.0}, {"day": "2026-10-28", "value": 64.4}, {"day": "2026-10-29", "value": 80.3}, {"day": "2026-10-30", "value": 95.1}, {"day": "2026-10-31", "value": 86.4}], "active": [{"day": "2026-10-18", "value": 3}, {"day": "2026-10-19", "value": 4}, {"day": "2026-10-20", "value": 4}, {"day": "2026-10-21", "value": 0}, {"day": "2026-10-22", "value": 0}, {"day": "2026-10-23", "value": 2}, {"day": "2026-10-24", "value": 4}, {"day": "2026-10-25", "value": 4}, {"day": "2026-10-26", "value": 3}, {"day": "2026-10-27", "value": 4}, {"day": "2026-10-28", "value": 4}, {"day": "2026-10-29", "value": 4}, {"day": "2026-10-30", "value": 4}, {"day": "2026-10-31", "value": 3}], "tasks": [{"day": "2026-10-18", "value": 3}, {"day": "2026-10-19", "value": 4}, {"day": "2026-10-20", "value": 2}, {"day": "2026-10-21", "value": 0}, {"day": "2026-10-22", "value": 0}, {"day": "2026-10-23", "value": 1}, {"day": "2026-10-24", "value": 5}, {"day": "2026-10-25", "value": 4}, {"day": "2026-10-26", "value": 3}, {"day": "2026-10-27", "value": 6}, {"day": "2026-10-28", "value": 4}, {"day": "2026-10-29", "value": 5}, {"day": "2026-10-30", "value": 7}, {"day": "2026-10-31", "value": 5}]}}
```

- `day` is today in the organization's zone. `spend.paid` / `spend.list` are what the organization's accounts paid /
  listed since midnight of that zone (the same figure as `usage.tick.spendToday`); `spend.tokens` are the input, output
  and cache tokens of all funding of the scope (as `tokensToday`).
- `active.people` are the people with a live top-level session that overlaps today; `active.of` are the people of the
  scope (a team: its members; otherwise the members of the tenants under the scope).
- `tasks.closed` are the sessions that **ended** today with `mrs ≥ 1` (a closed task); `tasks.inWork` are the sessions
  still running (no end) and updated in the last 15 minutes (the same ones `usage.tick.chatsNow` counts).
- `sparks` are the last 14 days, oldest first, today included, one point per day of the zone (a day with nothing is a
  `0`, not a hole). `spend` is paid dollars. There is no strip for the subscriptions tile: the hub keeps no history of
  a window's percent.

```
"efficiency": {
  "teams": [
    {
      "key": "m2platformteamaaaaaa",
      "label": "Платформа",
      "people": 3,
      "closed": 18,
      "perPersonMonth": 6.0,
      "reached": {
        "value": 0.71,
        "n": 64
      }
    },
    {
      "key": "m2productteamaaaaaaa",
      "label": "Продукт",
      "people": 1,
      "closed": 3,
      "perPersonMonth": 3.0,
      "reached": {
        "value": null,
        "n": 4
      }
    }
  ],
  "reached": {
    "value": 0.68,
    "n": 68
  }
}
```

- `teams` are the scope's teams (and «Без команды» with `key: ""` when sessions belong to none), the most tasks per
  person first; rows without a `perPersonMonth` go last.
- `closed` are the team's sessions that ended in the period with `mrs ≥ 1`. `perPersonMonth = closed ÷ people ÷ months`,
  rounded to 0.1: `months` is the number of calendar months for a period of whole months and `days ÷ 30` for any other
  period. It is `null` with no people.
- `reached` is the completion KPI over the sessions started in the period: the share of those with `turns ≥ 3` that made
  an MR. `value` is `null` below 5 sessions (`n` is how many there are), never a made-up number.

Also on the overview and the spend answers:

- `tokens.total` (a field of `Tokens`) is input + output + cache read + cache write.
- `spendBy.totals` is the sum of the rows as `{ list, paid, tokens }`, so a header does not add rows up in the browser:
  `{"list": {"usd": 1610, "shown": 131215.0}, "paid": {"usd": 1380, "shown": 112470.0}, "tokens": 693910000}`.

## People and the week

`GET /api/orgs/{org}/people?scope=&period=` (money) is the lead's «Расход по людям». Fixture: `people-lead.json`.
It needs a team or tenant scope to be useful, but the organization scope answers too (all its people).

```
{
  "scope": {
    "kind": "team",
    "id": "m2platformteamaaaaaa"
  },
  "period": {
    "from": "2026-10-01",
    "to": "2026-11-01",
    "tz": "Europe/Moscow"
  },
  "currency": {
    "code": "RUB",
    "perUsd": 81.5,
    "rateDate": "2026-10-01",
    "source": "cbr"
  },
  "total": {
    "list": {
      "usd": 1190,
      "shown": 96985.0
    },
    "paid": {
      "usd": 980,
      "shown": 79870.0
    },
    "limit": {
      "ruleId": "l2teamplatformaaaaa",
      "amount": {
        "usd": 1500,
        "shown": 122250.0
      },
      "pct": 0.653,
      "status": "ok",
      "setBy": {
        "userId": "u2annaaaaaaaaaaaaaaa",
        "name": "Анна"
      }
    },
    "tone": "ok"
  },
  "week": {
    "from": "2026-10-25",
    "to": "2026-11-01",
    "tz": "Europe/Moscow"
  },
  "people": [
    {
      "userId": "u2annaaaaaaaaaaaaaaa",
      "name": "Анна",
      "role": "owner",
      "list": {
        "usd": 520,
        "shown": 42380.0
      },
      "paid": {
        "usd": 430,
        "shown": 35045.0
      },
      "share": 0.287,
      "limit": null,
      "hot": false,
      "chats": {
        "n": 14,
        "running": 1
      },
      "mrs": {
        "n": 6,
        "merged": 5
      }
    },
    {
      "userId": "u2borisaaaaaaaaaaaaa",
      "name": "Борис",
      "role": "member",
      "list": {
        "usd": 410,
        "shown": 33415.0
      },
      "paid": {
        "usd": 330,
        "shown": 26895.0
      },
      "share": 0.22,
      "limit": null,
      "hot": false,
      "chats": {
        "n": 11,
        "running": 0
      },
      "mrs": {
        "n": 4,
        "merged": 4
      }
    },
    {
      "userId": "u2veraaaaaaaaaaaaaaa",
      "name": "Вера",
      "role": "member",
      "list": {
        "usd": 260,
        "shown": 21190.0
      },
      "paid": {
        "usd": 220,
        "shown": 17930.0
      },
      "share": 0.147,
      "limit": {
        "ruleId": "l2veralimitaaaaaaaa",
        "amount": {
          "usd": 250,
          "shown": 20375.0
        },
        "pct": 0.88,
        "status": "warn",
        "setBy": {
          "userId": "u2annaaaaaaaaaaaaaaa",
          "name": "Анна"
        }
      },
      "hot": true,
      "chats": {
        "n": 9,
        "running": 2
      },
      "mrs": {
        "n": 3,
        "merged": 2
      }
    }
  ]
}
```

- `people` are the members of the scope and everybody who spent in it, the largest `paid` first, then by name.
  Spend that has no known user is not a row but stays in `total`.
- `list` / `paid` are the person's money in the period (the organization's accounts only, as in `…/spend?by=person`).
- `share` is `paid ÷ the scope's own limit` (3 decimals); `null` when the scope has no limit. `limit` is the person's own
  monthly limit with its use (`LimitBar`), `null` when they have none; `hot` is `limit.pct ≥ 0.8`.
- `total.tone` is the colour of the scope's own limit: `ok`, `warn` from 75% and `risk` from 90%; `null` without a limit.
- `chats` and `mrs` are the person's last `week` (7 days of the zone, today included): chats started, those still
  running (not ended, moved in the last 15 minutes), MRs made (`mrs`) and merged (`mrsMerged`).
- `role` is the person's role in the scope's tenant, `""` when they have none there. People invited but not yet entered
  are not listed.

`GET /api/me/week?org=` is the member's overview tiles «Чатов за неделю» and «MR за неделю». Fixture:
`me-week-member.json`. The person's own live top-level sessions of the last 7 days; `org` only chooses the zone.

```
{
  "week": {
    "from": "2026-10-25",
    "to": "2026-11-01",
    "tz": "Europe/Moscow"
  },
  "chats": {
    "n": 25,
    "running": 3
  },
  "mrs": {
    "n": 5,
    "merged": 4
  }
}
```

`GET /api/me/kpis/{kpi}/series?org=&from=&to=&by=day|week` is the person's own KPI chart, with the shape of
`…/kpis/{kpi}/series` without `split`. Fixture: `me-kpi-series-member.json`. The sessions are all the person's own, so
the chart agrees with `/api/me/kpis`. Unknown KPI — 404, `by` other than `day`/`week` — 400.

Markers (here and in the organization's series) carry what the chart draws:

```
"markers": [
  {
    "changeId": "c2subagentsaaaaaaaaa",
    "at": "2026-10-15T09:00:00Z",
    "title": "Субагенты на Sonnet",
    "layer": "team",
    "author": {
      "userId": "u2annaaaaaaaaaaaaaaa",
      "name": "Анна"
    },
    "icon": "edit",
    "verdict": "better"
  },
  {
    "changeId": "c2claudeversionaaaaa",
    "at": "2026-10-22T03:10:00Z",
    "title": "Claude Code 2.4.1 → 2.5.0",
    "layer": "org",
    "author": null,
    "icon": "rocket",
    "verdict": "noise"
  }
]
```

- `layer` is where the change applies: `org`, `tenant` or `team`; `author` is who made it (`null` for a detected one);
  `icon` is `edit` (a manual change), `rocket` (a new version of a tool), `announce` (any other detected change: a model, skill or policy); `verdict` is the
  change's effect on this KPI as before.
- Markers of `/api/me/…` come only with `?org=`: a change belongs to an organization, so without it `markers` is `[]`.
  With it they are the organization's changes plus those of the tenants and teams the person's own sessions in the period
  belong to.

## Endpoints

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET …/overview?scope=&period=` | view (money parts need money) | | `OverviewResponse`: spend against the limit with the forecast, four tiles, «Требует внимания», spend by teams (org, tenant) or by people (team), subscriptions |
| `GET …/signals?scope=&all=1` | view | | `SignalsResponse` — the person's open signals in the scope; `all=1` adds the snoozed |
| `POST …/signals/{id}/snooze` | the addressee or above | `{ until }` (≤ 30 days) | `{ signal: Signal }`; journal `signal.snooze` |
| `DELETE …/signals/{id}/snooze` | the addressee or above | | `{ signal: Signal }` back in the list; journal `signal.unsnooze` |
| `GET …/spend?scope=&by=team\|person\|model\|account\|tenant&period=` | money | | `SpendResponse` — each row with its limit and forecast; sums of rows equal the total |
| `GET …/subscriptions?scope=` | money | | `SubscriptionsResponse` — each subscription's fee split between people, its windows |
| `GET …/limit-windows?scope=&from=&to=` | money | | `LimitWindowsResponse` — the history of the windows of the subscriptions the scope reaches ([below](#limit-windows-hrn-786)) |
| `GET …/kpis?scope=&from=&to=&cmpFrom=&cmpTo=&series=team` | view | | `KpisResponse` — all fifteen with the comparison; `series=team` adds them per team («Команды рядом») |
| `GET …/kpis/{kpi}/series?scope=&from=&to=&by=day\|week&split=team` | view | | `KpiSeriesResponse` — points, splits per team, markers of changes |
| `GET …/changes?scope=&from=&to=` | view | | `ChangesResponse` — the scope's changes and its ancestors' (they touch it too) |
| `POST …/changes` | manage of the scope | `{ scope, title, at }` | 201 `{ change: Change }`; journal `change.create` |
| `GET …/changes/{id}/effect?scope=` | view | | `EffectResponse` — 7 days before against 7 after, live sessions, every KPI; `overlaps` lists other changes in the windows (then `verdict: 'mixed'`) |
| `GET …/changes/effects?scope=&from=&to=` | view | | `EffectsResponse` — `{ effects: EffectResponse[] }`: the effect of every change `…/changes` gives for the same scope and range, in one request (the screen asks this, not one `…/effect` per change) |
| `GET …/waste?scope=&period=` | money | | `WasteResponse` — «Куда уходят деньги впустую» |
| `GET …/tools?scope=&from=&to=` (or `period=`) | view | | `ToolsResponse` — MCP servers, skills and built-in tools the agents called: calls, errors, people and teams, calls by day, the last call; at most 400 days |
| `GET …/people?scope=&period=` | money | | `PeopleResponse` — «Расход по людям» of a team or tenant: each person's money, share, personal limit, week's chats and MRs ([below](#people-and-the-week)) |

### Machines

| Method, path | Who | Request | Response |
| --- | --- | --- | --- |
| `GET …/machines?scope=&problems=1` | view | | `MachinesResponse` — the scope's machines, those with problems first; `problems=1` only those |
| `PATCH /api/machines/{id}` | manage of its tenant (team: the team's lead may change `access`) | `{ name?, teamId?, access? }` | `{ machine: HubMachine }`; journal `machine.team`, `machine.access` |
| `POST /api/machines/{id}/restart` | manage | `{ what: 'runner' \| 'machine' \| 'vm', when: 'now' \| 'drain' \| 'after_chats' }` | 202 `{ jobId, op }`; `runner` is the agent operation `runtime.restart`; `machine` (also `vm`) restarts the server through the provider, and `drain` stops new chats first. Without the `machine.resources` feature only `runner`/`vm` and `now`/`after_chats` work, and `vm` answers 501. See [machines-resources.md](machines-resources.md) |
| `GET /api/machines/{id}/resources`, `POST …/resources/quote`, `POST …/resize`, `GET …/ops`, `POST …/ops/{opId}/cancel`, `GET …/metrics`, `PUT …/managed` | view / manage | | the machine's server, load and operations, only with the `machine.resources` feature (404 `not_enabled` otherwise); [machines-resources.md](machines-resources.md) |
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
| `GET /api/me/kpis/{kpi}/series?org=&from=&to=&by=day\|week` | | `KpiSeriesResponse` — the person's own KPI over time, with the changes of `?org=` as markers ([below](#people-and-the-week)) |
| `GET /api/me/week?org=` | | `MeWeek` — their chats and MRs of the last 7 days |
| `GET /api/me/limits` | | [limits.md](limits.md) |
| `POST /api/me/limit-requests` | `{ amountUsd, reason, scope }` | 201 `LimitRequestResponse` — a `limit_request` signal to those who may raise it |

The agent app's «Управление» screen keeps its local history (`/api/platform/stats` of the runtime) and adds these with
the «на сервере учтено до …» line.

As built:

- Days are cut in the zone of `?org=`, of the person's only organization, or `Europe/Moscow`.
- `paid` is computed at read time from the hourly rollup: a subscription's fee × the row's `list` ÷ everyone's `list` on
  that account in the calendar month; a key or prepaid balance is its `list`; an account the hub does not know is 0.
- `accountedUntil` is the latest ack the hub gave on the `usage` stream of the machines that ran the person's sessions;
  `null` until one has been read.
- `parallelism` takes each session's span from start to end (the working intervals stay on the machine).
- `/api/me/sessions` pages newest first: `next` is the start of the page's last session, sent back as `before`.
- The endpoints follow `HUB_ORGS` like the rest of the organizations' API (404 when off, instance admins only in
  `shadow`).

As built: the organization's `…/overview`, `…/spend`, `…/kpis`, `…/changes` and `…/waste`, and the rate.

As built (machines):

- `…/machines` lists the machines of the scope's tenants (a team scope: the machines in that team), those with problems
  first, then by name. A row is computed from what the hub holds: `status` from the link, `lastActiveAt` and
  `chatsRunning` from the top-level `live` sessions of the machine (running: not ended and moved in the last 15
  minutes), `people` from those sessions of the last 30 days, `access` from the new `machine_access` table (migration
  `0010_machines.sql`; empty means the whole tenant). `opensRuntime` is false when the machine's last policy refused
  runtime sessions (a machine the hub only watches): the page hides «Открыть» and `…/open` answers 409 `observe_only`
 .
- Problems: `disabled` (alone, when the machine is switched off), `offline` (seen once, no link now), `idle_week` (no
  chat for 7 days; not for a machine that never connected), `agent_old` (below `HUB_MIN_AGENT_VERSION`), `disk_low`
  (the last preflight reported less than 10% or 5 GiB free), `login_expired` (an installed agent reports
  `logged_out`).
- `cloud` is `null` unless the organization has the `machine.resources` feature on and the hub knows the server of the
  machine (from a provider, [provider.md](provider.md)); then it is `{ provider, plan, cpu, ramGB, diskGB, price,
  externalId }`, each part `null` when unknown, and `price` is `{ amount, currency, period }` with `amount` in minor
  units. The hub never makes a price up. With the feature on the row also carries `hardware` (what the machine said of
  itself), `load` (`{ cpuPct, ramPct, diskPct, at }`, the last reading, dropped after 3 minutes), `week` (`{ chats, calls,
  unpriced, spend }` of the last seven days, `spend` a `Money` of the priced calls, `null` when none was priced),
  `managed` (`{ budget, env, resources, access }`) and `activeOps` (the unfinished operations). With it off these are
  `null` and `[]`.
- `access` is stored and shown. Whether it is kept when someone opens the machine is `HUB_ACCESS_ENFORCE`: `off`
  (default) follows the tenant's membership only, `warn` logs who would be refused, `on` answers 403 `machine_access`
  ([machines-resources.md](machines-resources.md#access)).
- `PATCH` with `teamId` or `access` answers `{ machine: HubMachine }`; a name alone keeps the old answer (the machine as
  the space sees it). The team and every entry of `access` must belong to the machine's tenant. A lead of the machine's
  team may send `access` alone. Journal: `machine.team`, `machine.access`, and `machine.disable`, `machine.enable`.
- `disable` closes the link with code 4003 (the agent waits and dials again) and the hub answers its dial with 423
  `machine_disabled` until `enable`; the key and the files stay. A switched-off machine answers 423 to `open` and to
  `restart` too.
- `restart` with `what: 'runner'` sends the agent operation `runtime.restart` `{ jobId, when }` and answers 202
  `{ jobId }`; an agent that does not know it gives 422 `restart_unsupported`. `what: 'vm'` answers 501
  `not_implemented` («Перезапуск ВМ пока недоступен») while the organization's `machine.resources` feature is off; with
  it on, `vm` is `machine`: a reboot through the provider (422 `not_supported` without one), and `when: 'drain'` is
  accepted. The answer then also carries `op`, and the restart is a stored operation ([machines-resources.md](machines-resources.md#operations)).
- All of it follows `HUB_ORGS` (404 when off, instance administrators only in `shadow`).

- **Rate.** The organization's `currency` (default `RUB`) is shown at the Central Bank's rate on the 1st of the period's
  first month, read once from `HUB_RATES_URL` (default `https://www.cbr.ru/scripts/XML_daily.asp`, `off` never fetches)
  and kept in `currency_rates`; a `manual` row there wins over the fetched one and is never overwritten. A month in the
  future, or a rate that cannot be fetched (tried again after 10 minutes), is shown in dollars with `source: 'none'`.
  The same rate is in `/api/me/*` and `…/limits`.
- **Who.** `…/overview` needs view of the scope, `…/spend` money; both are a manager's (org or tenant admin, the
  team's lead). `scope` is `kind:id`, the organization by default.
- **Money** counts the hourly rollup of `funding: 'org'` in the scope (a tenant by the session's tenant, a team by its
  team). `forecast` is `paid` plus the pace of `paid` over the last 7 days for the period's remaining days (what a limit
  is checked against); `forecastList` is the same at API prices, shown next to the list spend; both `null` for a
  period that has ended. Rows: `team` («Без команды» for sessions without one), `person`, `model`, `account`, `tenant`;
  sorted by `paid`. `guessedShare` is 0 for now.
- **Limit bars** are monthly money rules (not `forbid`): the total the scope's own rule (no person, account or model);
  a team or tenant row that scope's own rule; a person the most used personal rule in the scope or under it; an account
  the most used account rule in the scope; a model none. `setBy` is the person who set it.
- **Tiles** take the live top-level sessions of the scope that started in the period (any funding) and the same for the
  comparison period: `activePeople` distinct people, `sessions`, `mrs` Σ `mrsMerged`; `moneyPerMr` is Σ `list` (API prices) of
  the organization's sessions ÷ their `mrsMerged`, `null` without MRs.
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
  - **`DELETE …/signals/{id}/snooze`** (the same people) takes the snooze back: `snoozedUntil` becomes null, the signal
    is listed again and the journal gets `signal.unsnooze` (before the old `snoozedUntil`, after null). The screen
    offers it as «Вернуть» right after a snooze.
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
- **`…/kpis`** (view) are the fifteen KPI of `/api/me/kpis` over the scope's live top-level sessions (any funding), with
  `scope` the `{kind, id}` object; `money_per_mr` counts only the organization's sessions, their `list` ÷ their
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
  `…/changes/effects` gives the same for each change of `…/changes` on the same scope and range, oldest first, reading
  the sessions once for the union of the windows.
- **`…/waste`** (money) is where the organization's list money went to waste, over the live top-level sessions of the
  scope that started in the period with `funding: 'org'` only; each session counts at its list money (subagents'
  calls included). Exactly four kinds, each `{kind, money, share, sessions, explain}` (plus `title`, `hint`, and `amount`,
  the first version's name for `money`, kept equal to it): `abandoned_sessions` is the whole list of the ended sessions
  with 3 turns or more and no MR (the `completion` KPI's rule); `retries_after_errors` is Σ list × min(1, `toolErrors` ÷
  `toolCalls`), an estimate (the machines do not measure what a retry cost); `long_context` is the list money of the
  calls whose context (input + cache read + cache write) was 250 000 tokens or more, taken from the usage calls and
  capped by the session's list; `extra_skills` is Σ list × the part of the input the start context of unused skills
  takes (`firstCtx` × (`skillsListed` − `skillsLoaded`) ÷ `skillsListed` × `turns`, over input + cache read + cache
  write, at most 1), an upper estimate. `explain` says in words how the number was counted and says «оценка» when it is
  one. `sessions` is how many contributed, `share` the item's part of the scope's whole list spend (0 when it is 0, 3
  decimals). A session can be in several kinds, so the items are not added up. Items of zero are left out, the rest are
  sorted by amount; `items` is `[]` when none. The first version's `reread_context`, `failed_tools`, `long_waits` and
  `oversized_model` are gone.


## Limit windows

`GET /api/orgs/{org}/limit-windows?scope=&from=&to=` (money; the period as in `…/spend`) answers
`LimitWindowsResponse = { scope, period, accounts: AccountWindows[] }`. An account is a subscription of the organization
(`funding: 'org'`, kind `subscription`) that belongs or is granted to the scope, above it or in it; one with no cycle in
the period is left out. `AccountWindows = { accountId, label, provider, plan, windows: WindowHistory[] }`, the longest
window first. `WindowHistory = { name, scope, lengthSec, cycles: { endsAt, peakPct, measures }[], avgPeakPct, fullCycles }`:
a cycle is one stretch of a window between two resets, listed when it ends in the period, oldest first; `peakPct` is the
highest use measured in it, `measures` the number of measurements, `avgPeakPct` the mean of the peaks, `fullCycles` the
number of cycles that reached 95%.

The hub writes a cycle on every measurement of a window that has a use and a reset time (table `account_window_cycles`,
migration 0016, additive): the cycle is the reset time rounded to ten minutes, its `peakPct` the highest use seen, an
older measurement counts too, one without a use does not. Cycles exist from the first measurement after the hub took the
migration; earlier periods have none, and the screen says so.
