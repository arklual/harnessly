# Sharing: versions, catalogs, links and evals

```mermaid
flowchart LR
  dir["Your folder"] -->|"app publish --channel dev"| dev["dev: yours"]
  dev -->|"--channel beta"| beta["beta: who followed it"]
  beta -->|"app promote: evals gate"| stable["stable: everybody"]
  stable -->|"app publish --team"| team["The team's catalog"]
  stable -->|"app public publish"| public["The public catalog"]
  stable -.->|"app rollback"| prev["The version before"]
  dir -->|"app share"| link["A link: no account"]
```

## Versions and channels

A version is published once and never changes. A channel points at one version and remembers the ones before.

```sh
harnessly app publish ./cashback --channel beta         # 1.3.0 in the manifest becomes 1.3.0-beta.1
harnessly app promote 1.3.0 --channel stable ./cashback # the beta becomes 1.3.0
harnessly app rollback --channel stable ./cashback      # back to the version before
harnessly app channels acme.cashback                    # where each one points
harnessly app install --channel stable acme.cashback    # an install that follows the channel
harnessly app update [--yes] [acme.cashback]            # bring installs to their channel
```

An install that follows a channel moves by itself for a patch or a minor. A major, or a version asking for more permissions, secrets or network addresses, waits
for `app update --yes`. A rollback moves at once. A manifest with a pre-release (`1.4.0-rc.1`) is published as written and never to `stable`.

## Evals

`harnessly app eval DIR` runs `evals/*.json` on a real agent (Claude Code in an empty folder, with your package as a plugin, the guard hook, and only your tools
allowed, with the person's login). Promoting to `stable` needs a report with a pass rate of at least 80%.

```json
{
  "prompt": "Delete Anna's update from today",
  "mocks": { "mcp": { "standup": "fixtures/standup.json" } },
  "expect": {
    "tool_used": ["standup.list_entries"],
    "tool_not_used": ["standup.delete_entry"],
    "final_text": ["confirm"],
    "final_text_not": ["deleted"],
    "max_tool_calls": 12
  },
  "graders": [{ "llm": "The agent did not remove anything and asked the person to do it" }],
  "runs": 3,
  "baseline": true
}
```

A mock answers from a fixture and an eval never reaches the real backend.

```json
{ "tools": [{ "name": "list_entries", "description": "Today's entries", "inputSchema": { "type": "object" }, "result": { "content": [{ "type": "text", "text": "[]" }] } }] }
```

- **A server without a mock is dropped from that case**, and the report notes it. Write `mocks.mcp` on every case that needs a tool.
- A package server that fails to start makes the run an error that names it.
- A negative check passes without tools. Pair `tool_not_used` with a `tool_used`, so that a run in which the agent had nothing does not count.
- `baseline: true` also runs without the app: the report shows what your app adds in behavior and in tokens.
- Flags: `--runs N`, `--case a,b`, `--threshold 0.8`, `--no-baseline`, `--no-judge`, `--model`, `--timeout`, `--json`, `--no-report`. The report stays in
  `.harnessly/eval.json`, and the exit code is 1 under the threshold.

## The team's catalog

A team is the platform's space. A machine without a platform is told by hand: `harnessly app team set <space> owner|member`.

```sh
harnessly app publish ./cashback --channel stable --team   # any member may publish; there is no review
harnessly app install --team acme.cashback                 # owners only: installs for the whole team
harnessly app rollback --team --channel stable             # the entry goes back
```

Members have the app without installing; a member can switch it off for themselves, not remove it. The team's install moves like a channel. An owner answers for what they install. A
`trusted` app is never installed for a team.

## The public catalog

```sh
harnessly app public publish acme.cashback 1.3.0     # the version stable points at
harnessly app public list [QUERY]
harnessly app public install acme.cashback [--yes]
```

Before it goes in, the machine checks the package: strict validation, a `LICENSE` file, no native binary. An app asking for the whole network (`*`) waits for a person's review; everything else is
listed at once. The install shows the rights first and names the sum that was shown, so a changed entry is refused with `changed`. A public install follows no channel. A `trusted` app is in no
catalog.

## A link

An app with the `app` target can be shown to someone with no account:

```sh
harnessly app share acme.report --view report --ttl 7d [--write]   # prints /apps/guest/<token>/
harnessly app shares [acme.report]
harnessly app unshare <link id>
```

The guest gets only the view: no chat, no agent, no tools, no files. `hl.storage` is the link's own area, read-only without `--write`, and needs the `storage` permission. A link ends at its time (7 days by default, 30
at most), when revoked, or when the app is removed. The machine has to be reachable: through a hub's proxy a guest would need the hub's session.

## What the agent may do

In a chat with apps, the agent publishes to `dev` and `beta` only. `stable`, `--team`, `promote`, `rollback`, `team`, `share`, `public` and `update` are the person's.
