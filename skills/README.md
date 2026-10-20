# Vendored agent skills

harnessly installs these skills for the agents it starts (Claude Code and Codex) so a fresh install works with
documents, boards and the workspace at once. They are not written here: they are copied, at pinned commits, from

| Source | What is taken |
| --- | --- |
| [ly-solutions/agent-skills](https://github.com/ly-solutions/agent-skills) | `plugins/harnessly` (the workspace skill), `plugins/boards` (the boards skill and its `boards` helper) |
| [ly-solutions/artifacts](https://github.com/ly-solutions/artifacts) | `skills/artifacts` and the `artifacts` command line client (`bin/artifacts.mjs`, `src/cli.mjs`; Node.js standard library only) |

Both are Apache-2.0 (their `LICENSE` files are vendored next to them).

- `SOURCES` names the repositories and the commits.
- `upstream/` is the copy that is compiled into the runtime. Do not edit it by hand.
- `sync.sh` rewrites `upstream/` and `MANIFEST.sha256` from `SOURCES`; `sync.sh --check` fails when `upstream/` differs
  from the pinned commits (CI runs it). A Go test also checks `upstream/` against `MANIFEST.sha256` without a network.

To take a newer version: change the commit in `SOURCES`, run `skills/sync.sh`, commit the result.
