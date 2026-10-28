#!/bin/sh
# Entrypoint of the machine image (docs/hub/machine.md): registers the machine with its hub when it is not registered
# yet, then runs the runtime and the agent side by side and ends when either of them does.
#
#   AGENT_HUB_URL                 the hub's public URL (needed to register)
#   AGENT_REGISTER_CODE           the one-time registration code, hx_…
#   AGENT_REGISTER_CODE_FILE      or a file holding it (it is read each try, so a hub may renew it)
#   AGENT_REGISTER_PROFILE        container (default), dir or full
#   AGENT_REGISTER_VIA            container (default), local (the hub's local machine) or command
#   AGENT_REGISTER_NAME           the name the machine shows in the hub (default: the container's host name)
#   AGENT_REGISTER_WAIT           seconds to keep trying while the hub or the code is not there yet (default 120)
#   MACHINE_RUNTIME_PORT          the runtime's loopback port (default 18022)
#   MACHINE_AGENT_PORT            the agent's loopback port (default 18023)
#
# Neither port is published: the hub reaches the runtime through the agent's outgoing link. With AGENT_AUTH=hs256 the
# machine runs the closed-cluster way, without registering.
set -eu

AGENT_BIN=${HARNESSLY_AGENT_BIN:-harnessly-agent}
RUNTIME_BIN=${HARNESSLY_RUNTIME_BIN:-harnessly-runtime}

say() { printf 'harnessly-machine: %s\n' "$*" >&2; }
die() {
	say "$*"
	exit 1
}

# json_field FILE KEY prints the first string value of KEY in a JSON file machine.json-shaped (one level, no escapes).
json_field() {
	sed -n 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

register() {
	hub=${AGENT_HUB_URL:-}
	[ -n "$hub" ] || die "this machine is not registered and AGENT_HUB_URL is not set"
	if [ -z "${AGENT_REGISTER_CODE:-}" ] && [ -z "${AGENT_REGISTER_CODE_FILE:-}" ]; then
		die "this machine is not registered: set AGENT_REGISTER_CODE or AGENT_REGISTER_CODE_FILE (a code from the hub)"
	fi
	wait_for=${AGENT_REGISTER_WAIT:-120}
	case $wait_for in '' | *[!0-9]*) die "AGENT_REGISTER_WAIT is not a number of seconds" ;; esac
	deadline=$(($(date +%s) + wait_for))
	while :; do
		set -- register --hub "$hub" --profile "${AGENT_REGISTER_PROFILE:-container}" --via "${AGENT_REGISTER_VIA:-container}"
		[ -z "${AGENT_REGISTER_NAME:-}" ] || set -- "$@" --name "$AGENT_REGISTER_NAME"
		ready=1
		if [ -n "${AGENT_REGISTER_CODE_FILE:-}" ]; then
			if [ -r "$AGENT_REGISTER_CODE_FILE" ]; then
				set -- "$@" --code-file "$AGENT_REGISTER_CODE_FILE"
			else
				ready=0
				say "waiting for the registration code file $AGENT_REGISTER_CODE_FILE"
			fi
		else
			set -- "$@" --token "$AGENT_REGISTER_CODE"
		fi
		if [ "$ready" = 1 ] && "$AGENT_BIN" "$@"; then
			return 0
		fi
		[ "$(date +%s)" -lt "$deadline" ] || die "could not register with $hub"
		sleep 3
	done
}

stop_children() {
	for pid in "$@"; do
		kill -TERM "$pid" 2>/dev/null || true
	done
	n=0
	while [ "$n" -lt 20 ]; do
		alive=0
		for pid in "$@"; do
			if kill -0 "$pid" 2>/dev/null; then alive=1; fi
		done
		[ "$alive" = 1 ] || return 0
		n=$((n + 1))
		sleep 0.5
	done
	for pid in "$@"; do
		kill -KILL "$pid" 2>/dev/null || true
	done
}

main() {
	: "${HOME:?HOME is not set}"
	data=${AGENT_DATA_DIR:-$HOME/.harnessly}
	machine=${AGENT_MACHINE_FILE:-$data/machine.json}
	export AGENT_DATA_DIR="$data"

	if [ "${AGENT_AUTH:-auto}" != hs256 ]; then
		[ -f "$machine" ] || register
		base=$(json_field "$machine" runtimeBasePath)
		hub=$(json_field "$machine" hubUrl)
		case $base in /*) ;; *) die "$machine has no runtimeBasePath" ;; esac
		case $base in *[!A-Za-z0-9/_.-]*) die "$machine has an odd runtimeBasePath" ;; esac
		export RUNTIME_APP_BASE_PATH="$base"
		export RUNTIME_HUB_URL="${RUNTIME_HUB_URL:-$hub}"
	fi

	rport=${MACHINE_RUNTIME_PORT:-18022}
	aport=${MACHINE_AGENT_PORT:-18023}
	export RUNTIME_GATE_LISTEN="127.0.0.1:$rport" RUNTIME_GATE_UPSTREAM='' RUNTIME_PREVIEW_LISTEN=off
	export RUNTIME_PORT="$rport" AGENT_PORT="$aport"
	export RUNTIME_WORKSPACE_DIR="${RUNTIME_WORKSPACE_DIR:-$HOME/work}"
	mkdir -p "$RUNTIME_WORKSPACE_DIR"

	"$RUNTIME_BIN" &
	rpid=$!
	"$AGENT_BIN" &
	apid=$!
	trap 'stop_children "$rpid" "$apid"; exit 143' TERM INT
	while kill -0 "$rpid" 2>/dev/null && kill -0 "$apid" 2>/dev/null; do
		sleep 1 &
		wait $! || true
	done
	if kill -0 "$rpid" 2>/dev/null; then
		who=agent
		wait "$apid" && code=0 || code=$?
	else
		who=runtime
		wait "$rpid" && code=0 || code=$?
	fi
	say "the $who ended (status $code), stopping the machine"
	stop_children "$rpid" "$apid"
	exit "$code"
}

main "$@"
