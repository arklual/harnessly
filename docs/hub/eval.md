# Evaluation of the harness

The evaluation runs golden tasks of a repository in a sandbox on a machine, on a schedule, and gives the
Efficiency screen a verdict per change of the harness: did the tasks still pass, at what cost. It moved from the old
gateway into the hub so the gateway can be switched off. This page describes what the hub holds; the scheduler and the
runner on the machine are separate steps and are described where they land.

## Tables (migration 0062)

| Table | Whose | What |
|---|---|---|
| `eval_arms` | installation | A variant of the harness under test: `spec` (JSON) and its `fingerprint`. |
| `eval_tasks` | installation | A golden task: repository, base commit, prompt, `checks`, the `hidden` tests that grade by exit code, `forbidden` paths, `limits`, `extra`; `held_out` keeps it out of what the agent could have seen. |
| `eval_suites` | installation | A set of tasks (`task_keys`) with `k` repeats and the `arms` to compare. |
| `eval_schedules` | a machine | Days of the week (1 = Monday), the time `at_msk` (Moscow), `every_weeks`, the share of the person's weekly subscription the runs may use (`week_pct`), `enabled`. `owner_user_id` is the person whose subscription pays. |
| `eval_batches` | a machine | One launch of a suite: `reason`, `slot` (unique per schedule, so a slot is never launched twice), `state`, `expires_at`. |
| `eval_runs` | a machine | One task on one arm, with the verdict (`passed`), cost, wall time, tokens, the `diff` of files the agent changed. |
| `eval_checks` | a machine | The checks of one run by name, with exit code and time. |

JSON documents are text, times are Unix milliseconds. Deleting a space removes its machines' schedules, batches, runs and
checks; the catalog is the installation's and stays.

## Moving the old gateway's data

`hub import-llm --history --eval` (or `HUB_LLM_IMPORT_EVAL=1`) reads the old `eval_*` tables in one read-only
transaction and writes the hub's by id with `ON CONFLICT DO NOTHING`; a second run changes nothing, and a dry run (the
default) rolls back, so its numbers are the real ones.

- The catalog (arms, tasks, suites) is taken whole; a row the hub has is kept as it is.
- A schedule and a batch belong to «machine:email» of the old gateway. They go to the hub machine of that person that the
  matched chats are on, and are left, counted in the report, when the hub does not know the email or the machine.
- Schedules keep their `enabled` flag, but nothing runs them until the hub's scheduler is switched on for the person.
- A batch still `queued` or `running` in the old gateway is left with its runs: the old gateway is still running it and it
  expires by itself. Finished batches, their runs and checks move.
- Regression alerts are not here: they live in `eff_regression_alerts`.

## The scheduler

`internal/hub/eval` runs the schedules. It works under the rollout switch `hub-eval` of the schedule's owner (by
email): with the switch off nothing is queued and nothing is started, and a batch already queued waits. One hub replica works
at a time (a lease named `eval-scheduler`); a pass runs every 20 seconds.

- **Slots.** A schedule has days of the week, a time in Moscow time and «every N weeks» counted from a fixed Monday. A slot
  due within the last six hours is queued once (`eval_batches` is unique by schedule and slot); an older one is missed.
- **Guards.** A run is handed to the machine only while the subscription's windows read by the machine are fresh (30 minutes)
  and the week is under 70% and the five hours under 60%; without a fresh reading it waits (fail closed), and the batch note
  tells why. A week's runs of one person are capped at 8% of the window by cost; a batch over the cap is skipped.
- **Link.** The scheduler reaches a machine over the agent link (`HubLink`: `eval.run.start`, `eval.run.poll`,
  `eval.run.stop`) and only if the agent said the feature `eval.run` in its `hello`. A run is claimed, handed over, and polled; a machine silent for 10 minutes fails the run, one past its wall time
  and 20 minutes is stopped. A "subscription limit" skip goes back to the queue after 10 minutes (up to 12 times).
- **Close.** A finished batch is closed (`done`, `partial`, `skipped`), leaves a marker on the efficiency charts, and a smoke
  batch with results for less than half of its runs, or a pass rate below the minimum of the last seven (or a cost per task
  over 1.2× their median), writes an `eff_regression_alerts` row addressed to the owner.

## The runner on a machine

`internal/evalrun` is the machine's side. The agent runs one golden task at a time, in a sandbox, paid from the
machine owner's own subscription, and reports how it went. A run is an agent session on the owner's machine with a copy of
their login, so a machine serves it only when the operator opted in there.

| What | Where |
| --- | --- |
| `AGENT_HUB_EVAL=on` | the agent offers the feature `eval.run`; without it the three operations answer `policy_denied` |
| `AGENT_EVAL_WORKSPACE` | the folder of the person's source checkouts, for tasks with the layout `workspace` (default `<home>/workspace`) |
| `AGENT_EVAL_TOOL_DIRS` | colon-separated folders with tools a run may read (compilers, package stores) |
| `eval.run.start` | `{runId, task, arm}` in, `{}` out; asking again for a known run is not an error; `busy` while another run goes |
| `eval.run.poll` | `{runIds}` (at most 50) in, `{runs: {id: state}}` out; an unknown run is missing; a run that was going when the agent restarted is `failed` |
| `eval.run.stop` | `{runId}` in, `{}` out |

The names differ from the closed cluster's `eval.start`, `eval.poll` and `eval.stop` on purpose: a hub link does not get those.

- **Where it runs.** Linux with bubblewrap only; without it the agent does not offer `eval.run`. The state of a run is kept in
  `<data dir>/eval/runs/<id>/state.json`, so a poll after a restart still answers.
- **The copy.** The task's base commit is exported with `git archive` into a clean folder with no history, then committed so
  that the agent's changes show as a diff. Dependencies are installed on the host from the stores the machine already has
  (the base commit is the catalog's, trusted); the agent's session itself never runs outside the sandbox.
- **The sandbox.** The person's home is replaced by an empty one (`--tmpfs HOME`), the environment is `env -i`, the network
  tools are denied by the eval profile, and the copy and the sandbox home are removed afterwards. Git reads of the
  agent-modified repository happen inside the sandbox, not on the host.
- **The login.** An access-only copy of the owner's Claude login (and of Codex, for a Codex arm) is written into the sandbox
  home: no refresh token ever enters it. A login that ends before the run can is put off (`вход по подписке …`, the hub
  puts the run back in the queue); a machine with no login skips the run.
- **The harness.** An arm with the harness `personal` carries the owner's `CLAUDE.md`, the settings' env keys that tune the
  agent and hold no secret, and their plugins, skills and agents bound read-only; the arm `repo` carries only what the
  repository brings.
- **The verdict.** After the session the task's hidden files are written, the checks run in the same sandbox (10 minutes
  each), and the run passes when no forbidden file was touched, every check passed, and there was at least one check.
