# Getting started

This page runs harnessly on one machine, from source, without anything else around it: the runtime standalone, a
few tabs driven through its API, then the machine agent next to it.

## Requirements

- Linux or macOS, amd64 or arm64.
- Go, the version named in [`go.mod`](../go.mod), to build from source.
- `tmux` on `PATH`: every tab is a tmux session.
- `curl` and `jq` for the examples.
- Optional: the `claude` (Claude Code) and `codex` CLIs, signed in, to run agents; `git` for the diff and checkpoint
  routes; `ffmpeg` for dictation.

## Build

```sh
make build
```

This builds static binaries without cgo into `bin/`: `runtime`, `agent` and `agentprobe`, with the version from
`git describe` stamped in. `go build ./cmd/...` works too. Release archives for linux and darwin are attached to the
[GitHub releases](https://github.com/ly-solutions/harnessly/releases) once a version is tagged.

## Run the runtime standalone

With `RUNTIME_GATE_UPSTREAM` unset the runtime is [standalone](architecture.md#gate-and-standalone): it is the whole
runtime and every slice is on. Give it a throwaway home and its own tmux server for a first look:

```sh
export DEMO_HOME=$(mktemp -d)
HOME=$DEMO_HOME RUNTIME_GATE_LISTEN=127.0.0.1:4321 RUNTIME_TMUX_SOCKET=harnessly-demo \
  ./bin/runtime > "$DEMO_HOME/runtime.log" 2>&1 &
sleep 1
curl -s 127.0.0.1:4321/api/health
```

The log is JSON lines. On the first start the runtime takes its single-instance lock and writes its files into the
data directory, `$HOME/.harnessly` (`AGENT_DATA_DIR`):

| File | What |
| --- | --- |
| `config.json` | the runtime config, holding `authSecret`, the secret the runtime and its machine agent share |
| `cli-token` | the token CLIs and scripts send in the `x-cli-token` header |
| `port` | the port the runtime answers on, read by the hook scripts |
| `hooks.json`, `status-hook.sh`, `statusline.sh`, `codex-hook.sh` | the settings and scripts the agents report through |
| `preview-mcp.json` | the MCP server agents are given |
| `workspaces.json`, `workspaces/<id>/layout.json` | workspaces and their panes and tabs |

A second runtime on the same data directory refuses to start.

## Drive it through the API

Routes under `/api/cli/` take the `cli-token`; the page routes take it too, or a session cookie signed with the
config's secret.

```sh
TOKEN=$(cat "$DEMO_HOME/.harnessly/cli-token")
API=(-s -H "x-cli-token: $TOKEN" -H "content-type: application/json")
mkdir "$DEMO_HOME/project"

# a workspace on a folder; it comes with a first pane and tab
WS=$(curl "${API[@]}" 127.0.0.1:4321/api/workspace \
  -d "{\"name\":\"demo\",\"directory\":\"$DEMO_HOME/project\"}" | jq -r .id)

# the workspaces and the tabs the CLI routes see
curl "${API[@]}" 127.0.0.1:4321/api/cli/workspaces | jq .
curl "${API[@]}" "127.0.0.1:4321/api/cli/tabs?workspaceId=$WS" | jq .

# a terminal tab: type into it, read its screen, ask for its state
TAB=$(curl "${API[@]}" 127.0.0.1:4321/api/cli/tabs -d "{\"workspaceId\":\"$WS\",\"name\":\"shell\"}" | jq -r .tabId)
sleep 1                          # let the shell start
curl "${API[@]}" "127.0.0.1:4321/api/cli/tabs/$TAB/send?workspaceId=$WS" -d '{"content":"ls -la\n"}'
sleep 1
curl "${API[@]}" "127.0.0.1:4321/api/cli/tabs/$TAB/result?workspaceId=$WS" | jq -r .content
curl "${API[@]}" "127.0.0.1:4321/api/cli/tabs/$TAB/status?workspaceId=$WS" | jq .

# close it; its tmux session ends
curl "${API[@]}" -X DELETE "127.0.0.1:4321/api/cli/tabs/$TAB?workspaceId=$WS"
```

Every tab is a session on the runtime's tmux server, so `tmux -L harnessly-demo ls` lists them and
`tmux -L harnessly-demo attach -t <sessionName>` joins one.

## Run an agent

Agents need their CLIs signed in, and Claude Code and Codex keep their logins under the home, so run the runtime with
your own `HOME` for this; stop the throwaway one first. Its data then lives in `~/.harnessly` and its tabs on the tmux
server `harnessly`:

```sh
RUNTIME_GATE_LISTEN=127.0.0.1:4321 ./bin/runtime > runtime.log 2>&1 &
TOKEN=$(cat ~/.harnessly/cli-token)
API=(-s -H "x-cli-token: $TOKEN" -H "content-type: application/json")
WS=$(curl "${API[@]}" 127.0.0.1:4321/api/workspace -d "{\"name\":\"work\",\"directory\":\"$PWD\"}" | jq -r .id)
```

Then open a `claude-code` or `codex-cli` tab with a prompt (and optionally `model` and `effort`):

```sh
curl "${API[@]}" 127.0.0.1:4321/api/cli/tabs \
  -d "{\"workspaceId\":\"$WS\",\"panelType\":\"claude-code\",\"prompt\":\"Summarise this repository\"}"
```

The runtime starts the agent with its hook settings, so the agent reports back: the tab's `cliState` moves to `busy`
while the turn runs and to `ready-for-review` when it ends, and `agentSessionId` names the conversation. Pages follow
the same states live over the `/api/status` socket, and the `/api/timeline` socket streams the conversation itself.
After a restart of the runtime, agents that were working are resumed.

## Connect the machine agent

The machine agent serves the account to the cluster. It reads the shared secret from the runtime's config in the same
data directory (or from `AGENT_SECRET_FILE`):

```sh
HOME=$DEMO_HOME AGENT_PORT=4322 RUNTIME_PORT=4321 ./bin/agent > "$DEMO_HOME/agent.log" 2>&1 &
sleep 1
curl -s 127.0.0.1:4322/healthz
```

The cluster connects to `ws://<host>:4322/v1` with a bearer token signed with that secret, or the agent dials the
cluster itself when `AGENT_CLUSTER_URL` and `AGENT_RUNTIME_ID` are set. `bin/agentprobe` plays the cluster's part for
testing (see [architecture](architecture.md#checking-an-agent)).

## Gate mode

To move an existing installation over gradually, run the runtime in front of the previous runtime instead:
`RUNTIME_GATE_UPSTREAM` is the previous runtime's inner `host:port` (say `127.0.0.1:8023`, while the gate holds the
port callers know), and `state-mode.json` in the data directory says
which [slices](slices.md) Go answers on this machine, shadowed first.

## Clean up

```sh
kill %1 %2
tmux -L harnessly-demo kill-server >/dev/null 2>&1
rm -rf "$DEMO_HOME"
```

## Next

- [Configuration](configuration.md) lists every variable.
- [Architecture](architecture.md) explains what runs where.
