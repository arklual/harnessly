# Moving the old gateway's data into the hub

`hub import-llm` moves what the old gateway's database (`llm`) holds into the hub, so that the old gateway can be closed
(parent HRN-121). It runs from git as a Job or by hand on the hub's pod; nothing is moved by hand with `psql`.

- llm is **only read**: one read-only transaction, the DSN comes from the environment (`HUB_LLM_IMPORT_URL`, from a secret),
  never from the command line of a person.
- The hub gets only rows it lacks (`ON CONFLICT DO NOTHING`): its own rows win and a second run changes nothing.
- Without `--apply` it is a dry run: the report says what would move, nothing is written (the run is still noted).
- Each run leaves a row in `llm_import_runs` (migration `0057`) with the report, so a run can be checked later.

## What it moves

| From llm | To the hub | Rule |
|---|---|---|
| `rollout_switches` | `rollout_switches` | Only the features the hub's paths read (`llmimport.DefaultFeatures`, or `--features`), only for the people named in `--persons` (a row for anyone only with `--everyone`). A switch for a machine moves only if `--machines name=<hub machine id>` names it. |
| `resource_access_log` (documents) | `resource_access_log` (`doc`) | Rows of a document the hub has an access row for; the row's id is `llm-<id>`, so a row is taken once. |
| `ai_voice_takes` | `voice_takes` | Takes of the people named in `--persons` (`--everyone`: all) whose email the hub knows as a user. A take the hub has (same person and id) is left as it is: its text may have been edited there. The audio moves as base64 when it is there and not over 8 MiB; otherwise the take moves with its text only. Run it again before the cut-over to catch the takes dictated since. |
| `eff_pulse` | `eff_pulse` | Weekly answers of the efficiency screen of the people named in `--persons` (`--everyone`: all) whose email the hub knows. An answer the hub has for the same person and week is left as it is. |

## The chat step (`--history`)

Under `--history` (`HUB_LLM_IMPORT_HISTORY=1`) the import also moves what the people read and wrote in the old gateway.
No new tables: it writes the hub's own, by key, `ON CONFLICT DO NOTHING`, so a line the hub has is never overwritten.

| From llm | To the hub | Rule |
|---|---|---|
| `ai_turns`, `ai_entries` | `history_turns`, `history_entries` | A chat of llm goes into the hub chat with the same id, if there is exactly one and it is the same person's (deleted chats of llm too). No such chat, another person's, or several: the chat is left and counted in the report. Lines are read one chat at a time, each chat is its own transaction. |
| `ai_sidebars` | `history_sidebars` | By person; the hub's sidebar wins. |
| `offline_prompts` | `offline_prompts` | By id; the hub machine is the one the person's matched chats are on. |
| `doc_bindings` + `resources` | `doc_meta` | Documents and boards the hub has no record of (content stays in the artifacts catalog). In the space of the machine they were published from; with no known machine they go without a space and the hub gives it on the next publish. |
| `ai_voice_takes`, `eff_pulse` | `voice_takes`, `eff_pulse` | With `--voice-everyone` (`HUB_LLM_IMPORT_VOICE_EVERYONE=1`): of every person, not only `--persons`. |
| `eval_arms`, `eval_tasks`, `eval_suites`, `eval_schedules`, `eval_batches`, `eval_runs`, `eval_checks` | the same names, `eval_*` | With `--eval` (`HUB_LLM_IMPORT_EVAL=1`, needs `--history`): see [eval.md](eval.md). By id; a schedule and a batch go to the hub machine of its owner's matched chats. |

A dry run does the writes and rolls each transaction back, so its numbers are the real ones. Apps (`apps_team`,
`apps_public`) have no table in the hub and are not moved.

The gateway's own switches (`runtime-go-*`, streams, shadows, `no-frame`, …) are not moved: they end with the gateway.

## The rule step (`--automations`)

Under `--automations` (`HUB_LLM_IMPORT_AUTOMATIONS=1`) the import also moves the automation rules of the old gateway
(`automation_rules`) that are **switched on** there; a rule that is off is not moved, nothing would run it. It needs
`--automations-owner <email>` (`HUB_LLM_IMPORT_AUTOMATIONS_OWNER`): the hub person who gets the rules. A rule runs on its
owner's machine, and the gateway's owner email may not sign in to the hub, so all moved rules become that person's. The
person must have signed in to the hub once, otherwise the run stops before it writes anything.

- The key is (owner, name), `ON CONFLICT DO NOTHING`: a rule the person already has is kept as it is, a second run adds nothing.
- The machine is empty by default: the hub runs a rule on the first connected machine of its owner
  (`docs/hub/automations.md`). `--automations-machine <hub machine id>` (`HUB_LLM_IMPORT_AUTOMATIONS_MACHINE`) pins the rules
  to one machine; it must be a live machine of the owner.
- Trigger, prompt, model and limits are copied as they are: both gateways read the same keys.
- The report lists each rule read, where it went and whether it was added or kept. A dry run rolls the write back.

## What it only checks

The report compares keys: people (email), document share links (token hash), documents (slug) and conversations that are
not deleted, with the hub's `identities`/`users`, `resource_guest_links`, `doc_meta` and `history_conversations`.
«missing in the hub» is the number llm has and the hub has not; it moves nothing.

## Not here

Designs: `hub design import-gateway`. The rest of efficiency (`eff_*` but the weekly answers): its own task. Apps and the machines' state: their own tasks.

## Variables

| Variable | Meaning |
|---|---|
| `HUB_LLM_IMPORT_URL` | llm's Postgres address (read-only user). |
| `HUB_LLM_IMPORT_PERSONS` | the people whose switches move (comma-separated emails). |
| `HUB_LLM_IMPORT_MACHINES` | `llm machine name=hub machine id`, comma-separated. |
| `HUB_LLM_IMPORT_FEATURES` | the switches to move; empty is the default list. |
| `HUB_LLM_IMPORT_AUTOMATIONS` | `1`: also move the automation rules that are on. |
| `HUB_LLM_IMPORT_AUTOMATIONS_OWNER` | the hub person's email who gets them. |
| `HUB_LLM_IMPORT_AUTOMATIONS_MACHINE` | a hub machine id to pin them to; empty: any connected machine of the owner. |
