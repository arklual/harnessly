#!/bin/sh
# The example pack, installed and run with one script (docs/extensions/build-your-pack.md).
#
#   ext-pack.sh install  --prefix DIR [--machine-adapter BIN] [--harnessly BIN] [--hub-url URL] [--vault-addr URL]
#   ext-pack.sh start|stop|status --prefix DIR
#   ext-pack.sh rollback --prefix DIR      # the previous install of the pack comes back
#   ext-pack.sh uninstall --prefix DIR
#
# `install` copies the adapters, makes a token file for each (0600), and writes what you point the hub and the runtime
# at: hub.env, runtime.env and extensions.json. It never changes the hub or the runtime. Installing over an existing
# prefix keeps the old files in <prefix>/previous, which is what `rollback` puts back; data and tokens stay.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
cmd=${1:-}
[ $# -gt 0 ] && shift
prefix="" machine_bin="" harnessly="" hub_url="http://127.0.0.1:8080" vault=""
p_machine=${PACK_PORT_MACHINE:-18099} p_memory=${PACK_PORT_MEMORY:-18081} p_conn=${PACK_PORT_CONNECTION:-18082}
while [ $# -gt 0 ]; do
  case $1 in
    --prefix) prefix=$2; shift 2 ;;
    --machine-adapter) machine_bin=$2; shift 2 ;;
    --harnessly) harnessly=$2; shift 2 ;;
    --hub-url) hub_url=$2; shift 2 ;;
    --vault-addr) vault=$2; shift 2 ;;
    *) echo "ext-pack: unknown flag $1" >&2; exit 2 ;;
  esac
done
[ -n "$prefix" ] || { echo "ext-pack: --prefix DIR is needed" >&2; exit 2; }
node=${NODE:-node}
run="$prefix/run" log="$prefix/log"

token() { # a token file that is made once and kept
  if [ ! -s "$1" ]; then (umask 077; head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n' >"$1"); fi
}

install_pack() {
  command -v "$node" >/dev/null || { echo "ext-pack: node 22 or newer is needed for the TypeScript adapters" >&2; exit 1; }
  mkdir -p "$prefix/secrets" "$prefix/data" "$prefix/bin"
  chmod 700 "$prefix/secrets"
  if [ -f "$prefix/pack.json" ]; then # the previous install, for rollback
    rm -rf "$prefix/previous"; mkdir -p "$prefix/previous"
    for f in pack.json adapters bin ui-app hub.env runtime.env extensions.json; do
      [ -e "$prefix/$f" ] && mv "$prefix/$f" "$prefix/previous/$f"
    done
    mkdir -p "$prefix/bin"
  fi
  cp "$here/pack.json" "$prefix/pack.json"
  cp -r "$here/adapters" "$prefix/adapters"
  cp -r "$here/ui-app" "$prefix/ui-app"
  rm -f "$prefix/adapters/adapters.test.ts"
  if [ -n "$machine_bin" ]; then
    cp "$machine_bin" "$prefix/bin/machine-adapter"
  elif [ -d "$here/../machine-adapter" ] && command -v go >/dev/null; then
    (cd "$here/../.." && go build -o "$prefix/bin/machine-adapter" ./examples/machine-adapter)
  else
    echo "ext-pack: no machine adapter binary: pass --machine-adapter BIN (build it from examples/machine-adapter)" >&2
    exit 1
  fi
  chmod 755 "$prefix/bin/machine-adapter"
  for t in machine memory connection; do token "$prefix/secrets/$t.token"; done

  cat >"$prefix/extensions.json" <<JSON
[
  {"name": "example-corp-memory", "kind": "memory", "url": "http://127.0.0.1:$p_memory",
   "tokenFile": "$prefix/secrets/memory.token", "protocol": "harnessly.memory/v1",
   "branding": {"name": "Example Corp Agents", "accent": "#2f6fed", "supportUrl": "https://support.example.com"}},
  {"name": "example-tracker", "kind": "connection", "url": "http://127.0.0.1:$p_conn",
   "tokenFile": "$prefix/secrets/connection.token", "protocol": "harnessly.connection/v1"}
]
JSON
  cat >"$prefix/hub.env" <<ENV
# Add to the hub's environment (docs/hub/provider.md, docs/hub/extensions.md).
HUB_PROVIDER=http
HUB_PROVIDER_URL=http://127.0.0.1:$p_machine
HUB_PROVIDER_TOKEN_FILE=$prefix/secrets/machine.token
HUB_PROVIDER_PROTOCOL=auto
HUB_EXTENSIONS=on
HUB_EXTENSIONS_FILE=$prefix/extensions.json
ENV
  {
    echo "# Add to the runtime's environment (docs/connection-adapters.md, docs/memory-adapters.md, docs/safe.md)."
    echo "RUNTIME_CONNECTION_ADAPTERS_FILE=$prefix/extensions.json"
    echo "RUNTIME_MEMORY_ADAPTERS_FILE=$prefix/extensions.json"
    echo "HARNESSLY_UI_SLOTS=on"
    if [ -n "$vault" ]; then
      echo "RUNTIME_SECRETS_MODE=vault"
      echo "RUNTIME_SECRETS_VAULT_ADDR=$vault"
      echo "RUNTIME_SECRETS_VAULT_AUTH=token-file"
      echo "RUNTIME_SECRETS_VAULT_TOKEN_FILE=$prefix/secrets/vault.token"
    fi
  } >"$prefix/runtime.env"

  if [ -n "$harnessly" ]; then
    "$harnessly" app install "$prefix/ui-app"
  else
    echo "ext-pack: the interface extension is not installed (no --harnessly): harnessly app install $prefix/ui-app"
  fi
  echo "ext-pack: installed example-corp-pack $(sed -n 's/.*"version": "\([^"]*\)".*/\1/p' "$prefix/pack.json" | head -1) in $prefix"
}

pidof_() { [ -f "$run/$1.pid" ] && kill -0 "$(cat "$run/$1.pid")" 2>/dev/null && cat "$run/$1.pid"; }

launch() { # name, then the command
  n=$1; shift
  if pidof_ "$n" >/dev/null; then return 0; fi
  mkdir -p "$run" "$log"
  "$@" >>"$log/$n.log" 2>&1 &
  echo $! >"$run/$n.pid"
}

start_pack() {
  [ -f "$prefix/pack.json" ] || { echo "ext-pack: not installed in $prefix" >&2; exit 1; }
  launch machine "$prefix/bin/machine-adapter" -listen "127.0.0.1:$p_machine" -token-file "$prefix/secrets/machine.token" -state "$prefix/data/cloud.json"
  launch memory env PORT="$p_memory" ADAPTER_TOKEN="$(cat "$prefix/secrets/memory.token")" MEMORY_FILE="$prefix/data/memory.json" \
    "$node" --experimental-strip-types "$prefix/adapters/memory.ts"
  launch connection env PORT="$p_conn" ADAPTER_TOKEN="$(cat "$prefix/secrets/connection.token")" \
    "$node" --experimental-strip-types "$prefix/adapters/connection.ts"
  # ready when each answers describe
  for spec in "machine:$p_machine" "memory:$p_memory" "connection:$p_conn"; do
    n=${spec%%:*} port=${spec##*:}
    i=0
    until curl -fsS -H "Authorization: Bearer $(cat "$prefix/secrets/$n.token")" "http://127.0.0.1:$port/v1/describe" >/dev/null 2>&1; do
      i=$((i + 1)); [ $i -le 100 ] || { echo "ext-pack: $n did not answer, see $log/$n.log" >&2; exit 1; }
      sleep 0.1
    done
  done
}

stop_pack() {
  for n in machine memory connection; do
    if pid=$(pidof_ $n); then kill "$pid" 2>/dev/null || true; fi
    rm -f "$run/$n.pid"
  done
}

status_pack() {
  bad=0
  for n in machine memory connection; do
    if pid=$(pidof_ $n); then echo "ok       $n (pid $pid)"; else echo "stopped  $n"; bad=1; fi
  done
  return $bad
}

case $cmd in
  install) install_pack ;;
  start) start_pack ;;
  stop) stop_pack ;;
  status) status_pack ;;
  rollback)
    [ -d "$prefix/previous" ] || { echo "ext-pack: nothing to roll back to" >&2; exit 1; }
    was_up=0; status_pack >/dev/null 2>&1 && was_up=1; stop_pack
    rm -rf "$prefix/next"; mkdir -p "$prefix/next"
    for f in pack.json adapters bin ui-app hub.env runtime.env extensions.json; do [ -e "$prefix/$f" ] && mv "$prefix/$f" "$prefix/next/$f"; done
    for f in pack.json adapters bin ui-app hub.env runtime.env extensions.json; do [ -e "$prefix/previous/$f" ] && mv "$prefix/previous/$f" "$prefix/$f"; done
    rm -rf "$prefix/previous"; mv "$prefix/next" "$prefix/previous"
    [ $was_up = 1 ] && start_pack
    echo "ext-pack: rolled back to $(sed -n 's/.*"version": "\([^"]*\)".*/\1/p' "$prefix/pack.json" | head -1)"
    ;;
  uninstall)
    stop_pack
    [ -n "$harnessly" ] && "$harnessly" app remove example-corp.workspace || true
    rm -rf "$prefix/pack.json" "$prefix/adapters" "$prefix/bin" "$prefix/ui-app" "$prefix/hub.env" "$prefix/runtime.env" "$prefix/extensions.json" "$prefix/previous" "$prefix/run"
    echo "ext-pack: removed; tokens and data stay in $prefix/secrets and $prefix/data"
    ;;
  *) echo "usage: ext-pack.sh install|start|stop|status|rollback|uninstall --prefix DIR" >&2; exit 2 ;;
esac
