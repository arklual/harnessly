# kitbench

Repeatable before/after bench of UI kits for Sinopia Design. Runs on the build machine only.

    python3 tools/kitbench/kitbench.py all --bin <dir with harnessly and runtime> --out <run dir> [--models haiku,sonnet]
    python3 tools/kitbench/kitbench.py report --out <run dir> [--against <earlier run dir>]

- `kits`: part A. `harnessly design kit add npm:<pkg>` and `kit check --render --levels` for React and Angular kits.
- `agent`: part B. Per kit, a project that remembers the kit and five plain "draw screen X" prompts that do not name the
  kit, run as `harnessly eval` scenarios in sandboxed stands. Metrics: share of kit elements, `design check --strict`
  findings, screenshot check, a haiku judge score (1-10).
- `report`: `report.md` and `agent-summary.json`; with `--against` a before/after table.

A kit that is not public: `KITBENCH_EXPORT_KITS=name=archive.tar.gz` (a `kit export` archive next to the run dir).

Needs `~/agent-evals` (tools, login copy, setups/chrome.yaml). Stands need a short `--root` (tmux socket path).
