# Routines: recurring agent tasks

A routine is a task of a person that the hub runs again and again: «каждый понедельник присылай аналитику», «каждое
утро дайджест в файл новой версией», «следи за выкатом и пиши, если упал». The person asks in words; the agent in the
chat makes the routine (`harnessly routine create` or the `routine_create` tool); the hub keeps it, starts each run on
one of the person's machines in a new chat, checks the report and tells the person. harnessly only launches and
reports: the work is the agent's.

Under the hood a routine is an automation rule ([automations.md](automations.md)) with `routine` set, almost always
with a schedule trigger. Everything there holds — the queue, the daily cap, the dispatcher, the start over the machine's
link — with these differences:

| | Automation | Routine |
| --- | --- | --- |
| Ends | `awaiting_approval`, the owner approves or rejects | `done` with an outcome, no approval |
| Report | summary, bug, MR | outcome `ok`, `quiet` or `failed`, summary, full text, artifacts, memory, channels reached |
| Checks | none | the routine's deterministic checks, done by the hub |
| Memory | none | `state`: what the agent left for the next run, given to it in the prompt |
| Failures | each on its own | counted in a row (`failStreak`); three pause the routine with one notice |
| Quiet hours, cap | 0–8 Moscow, 3 a day | none, 48 a day (set `limits` to change) |
| Model | `sonnet` | its own, or `HUB_ROUTINES_MODEL` (default `sonnet`) |

The schedule needs the rollout switch `automation-triggers` for the person, like any schedule; the routes need
`HUB_AUTOMATIONS=on`.

## The routine

`routine` in the rule (JSON):

```jsonc
{
  "words": "по будням в 9:00",            // the schedule as the person said it
  "autonomy": "read",                      // read (default) | draft | act — what the run's agent may do outside
  "delivery": {
    "when": "changed",                     // changed (default: ok and failed, not quiet) | always | failure | never
    "channels": ["notice", "telegram", "file"],
    "file": "digest/{date}.md"             // channel file: written as a new version every run; {date} is YYYY-MM-DD
  },
  "checks": [                              // the hub's checks of the report; one failed makes the run failed
    {"kind": "contains", "value": "Риски", "label": "Есть раздел «Риски»"},
    {"kind": "regex", "value": "\\d+ заказ"},
    {"kind": "artifact", "value": "doc"},  // an artifact of the kind (any, when empty)
    {"kind": "file"},                      // an artifact of kind file titled the delivery file of today
    {"kind": "fresh"}                      // today's date (the schedule's zone) in the summary or the text
  ],
  "once": false,                           // one run, then off
  "until": "2026-12-31",                   // off after that day
  "budgetUsd": 1.0,                        // the run's cap the agent is told
  "chatId": "tab-…",                       // the chat the routine was made in
  "team": ""                               // reserved: a team's routine (owner and whose accounts its runs use)
}
```

Channels: the run's chat is always there. `notice` is the bell of harnessly (the hub's notices, switch `hub-notices`),
put by the hub per `when`. `telegram`, `messenger` and `mail` the run's agent sends itself through the person's
connectors, and names in its report; `file` and `doc` it writes and attaches as artifacts.

## The run

Each run starts in a new chat marked `from: {app: "routines", label: "Регулярные", title: <rule name>}`: in
the person's sidebar the chats of the routines' and the triggers' runs stand in one folder, «Регулярные», named after
their rule. Moving a chat to a project or the recent chats takes it out of the folder. A runtime older than the mark
starts the chat without it.

The prompt the hub gives the run's agent says: the routine's name, why it runs (schedule or by hand), that no one is
watching, the autonomy, the task, the memory, the checks, where the result goes, and how to report. The agent writes
the report to a file and posts it to its machine's agent, the token in a header:

```sh
curl -sS -X POST "http://127.0.0.1:$AGENT_REPORT_PORT/automations/report" -H 'content-type: application/json' \
  -H "x-result-token: $HARNESS_AUTOMATION_RESULT_TOKEN" --data @report.json
```

```jsonc
{"runId": "…", "outcome": "ok", "summary": "…", "body": "markdown",
 "artifacts": [{"kind": "doc|file|chart|mr|board|link", "title": "…", "url": "…", "meta": "версия 12"}],
 "state": "the whole memory for the next run", "delivered": ["telegram"], "error": ""}
```

The machine relays it as `automations.result`. The hub: `quiet` and `failed` are taken as said; `ok` passes the
checks or becomes `failed`. The run's `result` keeps the summary, text (64 KiB), artifacts (12), check results,
channels, model and the outcome the agent said. `state` replaces the memory (16 KiB). A routine that reported a
failure, failed to start, waited a day for a machine or sent nothing for 2 hours counts a failure; an `ok` or `quiet`
clears the count. Three in a row: the routine is turned off with `pausedReason` and one notice; turning it on again
clears both. A `once` routine turns off after a run that did not fail; one past `until` turns off.

## Routes

Under `<DesignPlatformAPI>/automations`, besides the rules' own:

- `GET /routines` — the person's routines, each with `next` (the next three starts), `last` (the last ten runs:
  status and outcome) and `stats`; and `defaultModel`.
- `POST /rules/{id}/run` — a run now, outside the schedule (409 `paused` when the routine is off, 429 at the cap).
- `GET /routines/feed?person=<email>&since=<RFC3339>` with `Authorization: Bearer <HUB_LAUNCH_TOKEN>` — for the
  service that builds the person's day (the home screen's brief, the inbox's «Сделал за тебя»): their routines (next
  start, paused) and the runs of all their rules since `since` (36 hours by default), newest first, at most 100. A run
  has `kind`: `routine` (outcome, summary, error, artifacts, the routine page's address in `url`) or `trigger` (a rule
  of an event, a schedule or a wait: `source`, `status`, summary, `mrUrl`; `awaiting_approval` waits for the person's
  ok).

From a machine: `POST /agent/routines`, signed with the machine's key under `harnessly-agent-routines-v1`, body
`{op: list|get|create|update|run|delete|runs, id, body, limit}`; the routines are those of the machine's owner, named
by id or name. The runtime carries it for the page and the command: `<platform>/routines[/{id}[/run|/runs]]` (a page
session) and `/api/cli/routines…` (the CLI token).

## The command and the tools

```sh
harnessly routine create --name "Дайджест дня" --cron "0 9 * * 1-5" --words "по будням в 9:00" \
  --prompt "…" --to notice,telegram,file --file digest/{date}.md --check contains:Риски --check file --check fresh
harnessly routine list | show ID | set ID … | pause ID | resume ID | run ID | runs ID | forget ID | delete ID
```

MCP: `routine_list`, `routine_create`, `routine_update`, `routine_run`, `routine_runs`, `routine_delete`
(`internal/routinetools`). Scenario: `evals/agent/routine-create.yaml`.

## Next

- The run in the same chat every time (`continue` context) — needs the runtime to resume a conversation by id.
- The machine chosen by load, not first connected.
- The agent proposing a routine from repeated questions.
- Team routines: owner, runAs and billing (`team`).
