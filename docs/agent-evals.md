# Agent evals: does the environment make the agent behave

`harnessly eval` checks that harnessly itself, with nothing else, makes an agent do the right thing: a Claude Code or
Codex chat with no memory, no plugins of the person and no CLAUDE.md of theirs, only harnessly's system context, MCP
tools, skills and CLI, on weak and strong models. «Draw a cat» must put a picture in the feed on the first turn; «make a
board» must open that board in the right pane; a password is asked through the safe, never in the chat.

It is built as a product from the start: scenarios are data, environments are configuration, and runs scale out over
disposable stands. Later the same tool checks the quality of an agent environment and its skills on a customer's own
machines and services.

Outside a checkout, `harnessly eval list` and `run` use the catalog included in the CLI build. In a checkout the local `evals/agent` takes precedence. Use `--dir PATH` for a custom catalog. Run `harnessly eval doctor` on the execution host to query its Landlock ABI. If the host cannot provide it, provision a compatible Linux runner for agent evals. Other integrations can run independently.

## The pieces

| Piece | What it is |
| --- | --- |
| **Stand** | A disposable harnessly: its own home, its own runtime on its own port and tmux socket, one workspace, empty memory, no plugins, logins copied without refresh tokens, `env -i`, permissions bypassed (nobody is there to grant one). Not connected to a hub, so nothing reaches anybody's sidebar, search or notifications. Confined by Landlock (`harnessly eval sandbox`, no privileges needed): the runtime and every agent it starts read the system and the tools and read and write only the stand's folder; homes, `/tmp` and other stands are out of reach. Without it an agent that cannot find a project searches the whole machine and writes into what it finds; a machine without Landlock refuses to run stands, and there is no flag around it. One stand per run, N in parallel. Pictures are drawn by a fake generator (`RUNTIME_IMAGEGEN_BIN`), so a run costs only the agent. |
| **Scenario** | A YAML file: setup (files, a design project), the person's words (a canonical prompt plus stored variants), the agents it applies to, and expectations. |
| **Facts** | What a run left, read after the turn: tool calls with arguments and errors from the transcript (subagents included), the right pane's intents (`pane-intents.json`), what the runtime lists as made by the person's chat with its subagents and delegated chats (`GET …/made`, the source of «Сделано за ход» and «Документы»), picture jobs, the workspace's files, the final words, tokens and cost. Facts are saved, so a run is judged again without running it. |
| **Judge** | Expectations over facts, never over the wording alone: `called` / `not_called` (a tool, regexps over its arguments; `even_failed: true` counts a call that failed too: a question to the person times out in a run nobody answers, and with `not_called` the attempt alone fails), `pane` (tab and target), `made` (kind, ref and who made it: the chat, a subagent or a subchat; `absent: true` for none), `images`, `file`, `text_not`, `any_of`, `max_calls`, `check` (a shell command run in the workspace after the run, inside the stand's sandbox: a hidden test of the code the agents wrote; it sees `EVAL_STARTED_AT` and `EVAL_INTERRUPT_AT`). An optional `judge:` rubric goes to a cheap model only where no fact can tell. |
| **Matrix** | scenario × variant × model × setup × repeat. Budget in runs and dollars, parallel stands, infrastructure failures (stand down, launch failed, API 5xx/401, timeout with no answer) retried apart from the model's failures and never counted, a cache by fingerprint (scenario, variant, model, setup, harnessly and CLI version). |
| **Report** | Scenario × model: passes/runs with the 95% Wilson interval, the diff to the last run of the same setup (improved / regressed / same within noise), and for each failure the expectation that failed in words and the transcript. Markdown and JSON; a page in harnessly comes next. |

## Noise and honesty

- Variants of a prompt (paraphrase, typos, Russian/English, short/long) are generated once by a cheap model from the
  canonical intent (`harnessly eval variants`) and stored in the scenario with the rest of it: regenerating them per run
  moves results by itself.
- A cell's number is the pass rate with a Wilson interval. A change counts only when its interval clears the noise
  threshold, which is the larger of the A/A spread (one setup run twice), the interval and 10 points; otherwise «same».
  Setups are compared paired (same scenarios and variants): paired bootstrap and McNemar, not overlapping intervals.
- Screening runs 3 repeats; a cell between 20% and 95%, or one compared across setups, gets more runs within budget.
- The held-out set lives outside this public repository and is run, never read, when instructions are tuned: an edit is
  kept only if the working set improves and the held-out set does not fall.
- The main number is pass^k (all k runs passed): an environment that works one time in two is broken. pass@k is shown
  for diagnosis.

## Analogues

No tool on the market runs the real Claude Code and Codex CLIs against a scenario as data and judges by the facts of the
runtime and of mocked services across a matrix of models and setups. What we take:

- **`claude plugin eval`**: the shape of a case (prompt, graders: tool used, file, regexp, judge), a run without the
  environment as the baseline, mocked MCP servers whose calls are checked. Closed, Claude Code only, no statistics.
- **Inspect AI** (with `inspect_swe`): the data model Task / Sample / Scorer, sandboxes, a proxy between the CLI and the
  model (later: our model gateway for weak/strong models and budget). Python; a reference, not a dependency.
- **τ-bench / τ²-bench, MCPMark**: success is the state of the environment, not the text; the user simulated by a model
  (with the warning that changing the simulator moves results, hence stored variants); pass^k.
- **Harbor / Terminal-Bench**: a task as a folder of instruction, environment and tests; real CLI agents; parallel
  runners.
- **Braintrust / LangSmith**: the report as a diff to the previous experiment with improved / regressed / tie per case.
- **SWE-bench**: a held-out set that the tuned thing never sees; contamination ends a benchmark.
- **promptfoo**: declarative tests and a matrix of providers; Node, SDK-based (not the real CLI).

What we write ourselves: the stand (the subject under test is harnessly itself, its runtime and its pane), the facts of
the runtime, the expectations, the statistics, budget and report. A customer's version differs from a model benchmark:
the model is fixed and the environment varies; scenarios and mocks come from the customer's processes and APIs; it runs
inside their perimeter and keeps transcripts there; weak and strong models sit in one table to answer «can this team use
the cheap model with this environment»; and every CLI or model update is a release gate.

## Waves

1. Stand, runner, report; scenarios for the pane (board, document, merge request, task), pictures, files, documents,
   tracker and GitLab on mocks, delegation, the safe, editing the open board; Claude Haiku/Sonnet/Opus/Fable, Codex,
   Kimi.
2. Widgets in the chat, sidebars, subchats and subagents, notifications, the vault, architecture boards, Sinopia edge
   cases; a page of the report; paired comparison of setups; a simulated page for «what the person has open».

## Commands

```
harnessly eval list   [--dir evals/agent]
harnessly eval run    --models haiku,opus [--scenarios a,b] [--repeats 3] [--parallel 4] [--max-runs 150] [--max-usd 20]
harnessly eval report [--run <dir>] [--against <dir>]
harnessly eval variants <scenario.yaml> [--n 6] [--write]
```

A setup is a file (`evals/agent/setups/default.yaml`): the runtime's settings (`RUNTIME_*`: the hosts of GitLab, the
tracker, documents and stands, the task keys) and the agents' environment (a provider's base URL for a model that is not
Anthropic's). A scenario may add runtime settings of its own. Logins live in `<root>/login/` as copies without refresh
tokens (`claude-credentials.json`, `claude-account.json`, `codex-auth.json`).

Scenarios that export a Sinopia Design page need a browser in the stand: put Chrome (or chrome-headless-shell, with a
wrapper that sets its libraries) in a `--tools` folder and name it in the setup's `RUNTIME_DESIGN_CHROME` (runtime and
agent environment both, with `RUNTIME_DESIGN_CHROME_ARGS: --no-sandbox` under Landlock). Without it the agent calls
`harnessly design export` rightly and the export fails, which is the stand's fault, not the agent's. Pick a
`--port-base` whose next ports are free: a stand that cannot bind its port is an infrastructure failure.

A scenario about a UI kit installed on the machine names kit folders in `setup.kits` (each with its
`design-system.json` naming the kit's id and version, as `harnessly design kit init` makes one): the stand installs them as
`design kit install` does, so the `kit_*` tools and `design new --kit` see them. Fixtures are neutral kits
(`fixtures/kits/acme-ui`), never a real company's.

## Access that is already there

`access-*`: an agent asked the person for a key that the team's vault had and a skill described. The
fixture `fixtures/access` gives the stand a mocked team infrastructure: skills of the project (`.claude/skills` and
`.agents/skills`: analytics, the team vault, the servers), and on `PATH` a `kubectl` that answers only with the
machine's kubeconfig and reads only the vault in `vault-0`, a `tsh` that answers only with the machine's identity file
(`tsh status` says «Not logged in», as a person's login does), and the `flags` and `grafana` CLIs that need the keys.
Three scenarios expect the result without asking the person (`not_called` with `even_failed`): a flag key the service's
skill points to in the vault, a Grafana token only the vault's skill lists, a disk on a server through the machine's
identity. The control `access-secret-nowhere` expects the ask through the safe for a key that is nowhere. The report
counts a card of the safe among the asks and shows the share of blind asks: the person asked before any skill was
opened.

## Teamwork scenarios

Scenarios where one chat hands work to others (`evals/agent/orch-*.yaml`) need three things more:

- **The person's memory**: `setup.memory` (name → text, `MEMORY.md` and notes) is put into the memory folder of the stand's
  workspace project, as earlier chats left it: `widget-offer-after-no` / `-after-yes` seed what the person answered to the
  offer of a progress widget. Claude only: Codex has no such memory.
- **The workspace as a project**: `setup.workspace` copies fixture folders into it and `setup.run` runs a script there
  (`git init` and a first commit), so a check can ask `git diff` what changed.
- **Waiting for the helpers**: a chat that delegates ends its turn and is woken by its chats' answers. With `settle_s`
  the run goes on until no agent of the stand (subagents, delegated chats in their own folders, teammates) wrote to its
  transcript for that many seconds and the person's chat is not working; the facts and tokens include every session.
- **The person speaking mid-work**: `interrupt: {after_s, text}` types a message into the chat while something still
  works («stop, main is frozen»); a check reads `EVAL_INTERRUPT_AT` to find commits and edits made after it.

The ways to work are compared as setups over the same scenarios: `setups/orch-subagents.yaml`,
`orch-subchats.yaml`, `orch-teams.yaml` (agent teams, the control). A setup's `prompt_suffix` is added to the person's
words to name the way, and `agent_env` switches a CLI feature on. The report adds a table of cost and teamwork per cell:
work time and tokens (medians with their spread over repeats), dollars, helpers started (subagents / delegated chats /
teammates), files edited from more than one session, identical edits, the times the person was needed (a question tool,
a turn ending on input), edits of the lead chat itself, REPL calls and timeouts.

**Code Mode** of Claude Code (`setups/code-mode.yaml`): `CLAUDE_CODE_EXPERIMENTAL_CODE_MODE=1` plus the
consent the opt-in dialog stores (`codeModeCaveatsAcceptedV1` and `codeModeAutoModeCaveatAcceptedV1` in
`~/.claude.json`, set by the setup's `claude_config`) starts the chat with the REPL tool and no dialog. It is refused
in `-p`, with agent teams, with OpenTelemetry on, and on API keys or other providers. The calls a REPL script makes go
through PreToolUse and PostToolUse hooks one by one with their own tool names (a deny stops that call only), and the
transcript records them as ordinary tool calls, so expectations judge them as usual.

## New features

A change of what agents are told or given changes what they do, so it is measured like one. The agent's surface is
listed in `evals/agent/surface.txt` (globs): the workspace prompt, the skills, the MCP tools, the `harnessly` commands.
Each scenario names the paths it checks in `covers:`.

- **In the same commit**: a change of the surface brings a new scenario or changes one (its words, expectations or
  covers). `harnessly eval affected [--base REF]` lists the changed surface, the scenarios whose covers it touches and
  the scenarios changed with it; `--names` prints them for `eval run --scenarios`.
- **A draft to start from**: `harnessly eval suggest [--base REF] [--write]` gives the diff of the surface to a cheap
  model (haiku) with one scenario as the example, and writes the YAML it answers when it reads as a scenario and no file
  of that id exists. It is a draft: check that its expectations are facts the change makes true, and add covers.
- **CI**: `agent-evals-coverage` runs `eval affected --check` on every push to main that touches code or scenarios and
  turns yellow (exit 3, allowed to fail) when the surface changed without a scenario. A warning, not a block, while the
  set is small.
- **Smoke run**: `agent-evals-smoke` runs the affected scenarios once each on haiku (at most $2), only when asked —
  `[eval]` in the commit message or `AGENT_EVAL_SMOKE=1` on a pipeline — and only where a runner can host stands:
  Landlock, the agents' CLIs (`AGENT_EVAL_TOOLS`), a login copy without refresh tokens (`AGENT_EVAL_LOGIN`), named by
  `AGENT_EVAL_RUNNER_TAG`. Without that variable the job is not created; run the same by hand on the build machine.
- **Proof of a fix**: the affected scenarios before and after, on a cheap and a strong model, ≤14 runs per cell
  (variants and repeats), the same stand for both arms.

Codex in a stand: the login copy has an empty `refresh_token` (Codex refuses an auth.json without the field, and the
real one would be rotated) and a fresh `last_refresh`; Codex runs without its own sandbox
(`RUNTIME_CODEX_SANDBOX=container`), since Landlock is the boundary and its bubblewrap cannot start inside it. Its code
mode puts tool calls in a script (`tools.exec_command({cmd: …})`); each is judged as a call of its own.

Models that are not Anthropic's run in Claude Code through the setup's agent environment: `ANTHROPIC_BASE_URL`,
`ANTHROPIC_AUTH_TOKEN` and the `ANTHROPIC_DEFAULT_*_MODEL` names, with `--models sonnet` (the runtime launches only the
models of its catalog; the setup maps sonnet to the provider's model). Keep such a setup file private (0600) and out of
the repository.

## Blender scenes

`blender-scene-*` check that a scene the agent saves with the blender-lab skill shows up in Sinopia without the person asking.
`fixtures/blender-lab/mock/blender` is a stand-in for headless Blender (a fake `bpy` that accepts any call and writes the
.blend and the PNGs), so a run costs only the agent. `mock/variant` is the setup under test: `before` (the skill as it was),
`text` (the step in the skill's workflow, run by hand), `wrap` (the `blender` wrapper that runs `design scene3d add` after
a save), `both` (the committed default). Measured with the runtime without the `scene3d` paragraph of the workspace prompt, as
production had it: before 5/36 (dev), 0/9 (held out); text only 16/18 and 8/9; both 18/18 and 9/9.
