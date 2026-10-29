#!/usr/bin/env bash
# Local end-to-end check of the hub with a real machine agent and runtime, without Docker or root.
#
#   tools/hub-e2e.sh                       # builds into a temp folder, runs, cleans up
#   E2E_KEEP=1 tools/hub-e2e.sh            # keep the temp folder (logs, databases) and print where it is
#   E2E_SHOTS_DIR=/some/dir tools/hub-e2e.sh   # also take screenshots of the UI there (needs web/dist-hub and Chrome)
#
# What it does, in order: starts the hub (SQLite, email codes to its log) -> signs a person up over HTTP -> makes a space
# -> asks for a registration code -> registers a machine agent with it under a throwaway HOME (Ed25519 key) -> runs the
# machine's runtime and agent -> checks the machine is online with its preflight (disk, memory) -> runs the agent
# endpoints (status, provider check, proxy, sealed key and secret, Git key, console login) -> opens the machine's page
# through the hub -> tries the local policy and the kill switch -> a second person joins by invitation and is held to
# member rights -> the local machine is claimed -> the machine is revoked -> the UI is served -> (optionally)
# screenshots. Everything it starts is stopped on exit. Ports: E2E_HUB_PORT (18510), E2E_RUNTIME_PORT (18511),
# E2E_AGENT_PORT (18512); the screenshots' second machine uses the runtime and agent ports plus 10.
#
# E2E_DATABASE_URL=postgres://user@host:port/emptydb?sslmode=disable runs the hub on that (empty) Postgres database
# instead of SQLite; pg_dump (on PATH or in /usr/lib/postgresql/*/bin) then also searches the database for the secrets.
#
# Needs: go, curl, jq, tmux. For the UI: web/dist-hub (pnpm build:hub; this script runs it when pnpm exists and the folder
# is missing) and, for the machine's page, web/dist (pnpm build) so the runtime is built with the page in it.
# No call leaves this machine: the provider check goes through a proxy that does not exist. E2E_REAL_PROVIDER=1 lets it
# also try the real servers of Anthropic once.

# The services are started in subshells that export their own environment: that is the point.
# shellcheck disable=SC2030,SC2031
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HUB_PORT=${E2E_HUB_PORT:-18510}
RT_PORT=${E2E_RUNTIME_PORT:-18511}
AG_PORT=${E2E_AGENT_PORT:-18512}
HUB="http://127.0.0.1:$HUB_PORT"
SHOTS_DIR=${E2E_SHOTS_DIR:-}
DB_URL=${E2E_DATABASE_URL:-}
PNPM=${PNPM:-pnpm}

for tool in go curl jq tmux; do
	command -v "$tool" >/dev/null || { echo "hub-e2e: $tool is needed" >&2; exit 2; }
done

T=$(mktemp -d "${TMPDIR:-/tmp}/hub-e2e.XXXXXX")
PIDS=()
FAILED=0
STEP=0

say() { printf '%s\n' "$*"; }
step() { STEP=$((STEP + 1)); printf '\n== %d. %s\n' "$STEP" "$*"; }
ok() { printf '  ok    %s\n' "$*"; }
bad() { printf '  FAIL  %s\n' "$*"; FAILED=$((FAILED + 1)); }
die() { printf '  FATAL %s\n' "$*" >&2; exit 1; }

# check "what" <command...>: the command succeeds.
check() {
	local what=$1
	shift
	if "$@" >/dev/null 2>&1; then ok "$what"; else bad "$what"; fi
}

# expect "what" got want
expect() {
	if [ "$2" = "$3" ]; then ok "$1 ($2)"; else bad "$1: got '$2', want '$3'"; fi
}

cleanup() {
	local code=$?
	trap - EXIT
	set +e
	# The runtime's tmux server lives in the throwaway TMUX_TMPDIR; nothing of the host's is touched.
	for sock in "$T"/tmux/tmux-*/*; do
		[ -S "$sock" ] && tmux -S "$sock" kill-server 2>/dev/null
	done
	for pid in "${PIDS[@]:-}"; do
		[ -n "$pid" ] && kill "$pid" 2>/dev/null
	done
	for pid in "${PIDS[@]:-}"; do
		[ -n "$pid" ] && wait "$pid" 2>/dev/null
	done
	if [ "$code" -ne 0 ] || [ "$FAILED" -ne 0 ]; then
		say ""
		say "hub-e2e: FAILED ($FAILED check(s) failed, exit code $code)"
		if [ "${E2E_KEEP:-0}" != 1 ] && [ -f "$T/hub.log" ]; then
			say "--- hub log (tail)"
			tail -n 20 "$T/hub.log" | sed -E 's/ code=[0-9]+/ code=******/'
		fi
		if [ "${E2E_KEEP:-0}" = 1 ]; then say "kept: $T"; else rm -rf "$T"; fi
		exit 1
	fi
	if [ "${E2E_KEEP:-0}" = 1 ]; then say "kept: $T"; else rm -rf "$T"; fi
	say ""
	say "hub-e2e: all checks passed"
}
trap cleanup EXIT

# --- helpers over the hub's API ------------------------------------------------------------------------------------

CODE=0
BODY=""
# call <jar> <METHOD> <path> [json]: sets CODE and BODY. The cookie jar is read and updated.
call() {
	local jar=$1 method=$2 path=$3 body=${4-}
	local args=(-sS -o "$T/body" -w '%{http_code}' -b "$jar" -c "$jar" -X "$method" "$HUB$path")
	if [ -n "$body" ]; then args+=(-H 'Content-Type: application/json' --data "$body"); fi
	CODE=$(curl "${args[@]}") || CODE=000
	BODY=$(cat "$T/body")
}
j() { jq -r "$1" <<<"$BODY"; }

# code_for <email>: the sign-in code the hub wrote to its log (no SMTP is configured: that is the DEV path).
code_for() {
	grep 'DEV email code' "$T/hub.log" | grep "to=$1 " | tail -n 1 | sed -E 's/.* code=([0-9]+).*/\1/'
}

# sign_up <jar> <email> <name>: the whole email flow, then the person's name.
sign_up() {
	local jar=$1 email=$2 name=$3
	call "$jar" POST /api/auth/email/start "{\"email\":\"$email\",\"intent\":\"signup\"}"
	[ "$CODE" = 200 ] || die "email/start for $email answered $CODE: $BODY"
	local code
	code=$(code_for "$email")
	[ -n "$code" ] || die "no code for $email in the hub log"
	call "$jar" POST /api/auth/email/verify "{\"email\":\"$email\",\"code\":\"$code\"}"
	[ "$CODE" = 200 ] || die "email/verify answered $CODE: $BODY"
	call "$jar" PATCH /api/me "{\"name\":\"$name\"}"
}

# wait_for "what" <seconds> <command...>: polls until the command succeeds.
wait_for() {
	local what=$1 secs=$2
	shift 2
	local i
	for ((i = 0; i < secs * 4; i++)); do
		if "$@" >/dev/null 2>&1; then ok "$what"; return 0; fi
		sleep 0.25
	done
	bad "$what (not within ${secs}s)"
	return 1
}

machine_is() { # machine_is <jar> <status>
	call "$1" GET "/api/machines/$MID"
	[ "$(j .machine.status)" = "$2" ]
}

# --- 1. build ------------------------------------------------------------------------------------------------------

step "Build the hub, the agent and the runtime (static, no cgo)"
mkdir -p "$T/bin"
cd "$ROOT"
export CGO_ENABLED=0
go build -o "$T/bin/hub" ./cmd/hub
go build -o "$T/bin/harnessly-agent" ./cmd/agent
if [ -f web/dist/index.html ]; then
	go build -tags webui -o "$T/bin/harnessly-runtime" ./cmd/runtime
	HAVE_PAGE=1
	ok "built with the runtime's page"
else
	go build -o "$T/bin/harnessly-runtime" ./cmd/runtime
	HAVE_PAGE=0
	say "  note  web/dist is missing: the runtime has no page, so the page of the machine is not checked (pnpm build)"
fi
WEB_DIR=""
if [ -f web/dist-hub/index.html ]; then
	WEB_DIR="$ROOT/web/dist-hub"
elif command -v "$PNPM" >/dev/null && [ -d web/node_modules ]; then
	say "  building the hub UI (pnpm build:hub)"
	if (cd web && npm_config_manage_package_manager_versions=false nice -n 10 "$PNPM" build:hub >"$T/web-build.log" 2>&1); then
		WEB_DIR="$ROOT/web/dist-hub"
	else
		say "  note  pnpm build:hub failed, see $T/web-build.log"
	fi
fi
if [ -n "$WEB_DIR" ]; then ok "hub UI: $WEB_DIR"; else say "  note  no hub UI (web/dist-hub): the UI steps are skipped"; fi

# What a machine downloads: the installer and, named by system, the two programs.
case "$(uname -s)" in Linux) OS=linux ;; Darwin) OS=darwin ;; *) die "unsupported system" ;; esac
case "$(uname -m)" in x86_64 | amd64) ARCH=amd64 ;; aarch64 | arm64) ARCH=arm64 ;; *) die "unsupported architecture" ;; esac
mkdir -p "$T/dist"
cp deploy/install/install.sh "$T/dist/install.sh"
cp "$T/bin/harnessly-agent" "$T/dist/agent-$OS-$ARCH"
cp "$T/bin/harnessly-runtime" "$T/dist/runtime-$OS-$ARCH"

# --- 2. hub --------------------------------------------------------------------------------------------------------

step "Start the hub on $HUB (SQLite, email codes to the log)"
mkdir -p "$T/hub"
(
	export HUB_LISTEN="127.0.0.1:$HUB_PORT" HUB_PUBLIC_URL="$HUB" HUB_DATA_DIR="$T/hub" HUB_DIST_DIR="$T/dist"
	export HUB_LOCAL_CODE_FILE="$T/hub/local-code" HUB_LOG_LEVEL=info HUB_EDITION=self-hosted HUB_SIGNUP=open
	if [ -n "$DB_URL" ]; then export HUB_DATABASE_URL="$DB_URL"; fi
	if [ -n "$WEB_DIR" ]; then export HUB_WEB_DIR="$WEB_DIR"; fi
	exec "$T/bin/hub" serve
) >"$T/hub.log" 2>&1 &
PIDS+=($!)
wait_for "the hub answers /healthz" 20 curl -fsS "$HUB/healthz" || die "the hub did not start: $(tail -n 5 "$T/hub.log")"
check "/readyz says the database answers" curl -fsS "$HUB/readyz"
call "$T/none" GET /api/config
expect "/api/config: email codes go to the log" "$(j .auth.email.mode)" log
expect "/api/config: no OAuth provider is configured, none is shown" "$(j '[.auth.github,.auth.yandex]|any')" false
expect "/api/config: runtime mode" "$(j .runtimeMode)" path
if [ -z "$DB_URL" ]; then
	check "the SQLite file is private (0600)" test "$(stat -c %a "$T/hub/hub.db")" = 600
else
	ok "database: Postgres at ${DB_URL#*@}"
fi

# --- 3. sign up ---------------------------------------------------------------------------------------------------

step "Sign up by email code, make a space"
OWNER="$T/jar-owner"
: >"$OWNER"
call "$OWNER" GET /api/me
expect "/api/me signed out" "$CODE" 401
call "$OWNER" POST /api/auth/email/start '{"email":"anna@example.com","intent":"signup"}'
expect "a code is asked for" "$CODE" 200
call "$OWNER" POST /api/auth/email/verify '{"email":"anna@example.com","code":"000000"}'
expect "a wrong code is refused" "$(j .error)" invalid_code
call "$OWNER" POST /api/auth/email/verify "{\"email\":\"anna@example.com\",\"code\":\"$(code_for anna@example.com)\"}"
expect "the right code signs in" "$CODE" 200
expect "a new account is told so" "$(j .isNew)" true
expect "the first account has nowhere to go but a new space" "$(j .next)" /new
call "$OWNER" PATCH /api/me '{"name":"Анна"}'
call "$OWNER" GET /api/me
expect "the first account is the instance admin" "$(j .user.isAdmin)" true
call "$OWNER" POST /api/spaces '{"name":"Тестовое пространство"}'
expect "space created" "$CODE" 201
SID=$(j .space.id)
SLUG=$(j .space.slug)
expect "the slug is made from the name" "$SLUG" testovoe-prostranstvo
CROSS=$(curl -sS -o /dev/null -w '%{http_code}' -b "$OWNER" -X POST -H 'Content-Type: application/json' -H 'Origin: https://evil.example' --data '{"name":"evil"}' "$HUB/api/spaces")
expect "a request from another site is refused" "$CROSS" 403

# Watch the space's event stream for the rest of the run.
curl -sN --max-time 600 -b "$OWNER" "$HUB/api/spaces/$SID/events" >"$T/events.txt" 2>/dev/null &
PIDS+=($!)

# --- 4. registration code -----------------------------------------------------------------------------------------

step "Ask for a registration code"
call "$OWNER" POST "/api/spaces/$SID/machines/codes" '{"profile":"full"}'
expect "full access without the confirmation is refused" "$CODE" 400
call "$OWNER" POST "/api/spaces/$SID/machines/codes" '{"profile":"container"}'
expect "the container profile is off while the hub has no machine image" "$CODE" 422
call "$OWNER" POST "/api/spaces/$SID/machines/codes" '{"profile":"full","confirmFull":true}'
expect "code created" "$CODE" 201
REG_CODE=$(j .code)
expect "the command to copy" "$(j .command)" "curl -fsSL $HUB/install | sh -s -- --token $REG_CODE --profile full"
check "the registration code is not in the hub's log" bash -c "! grep -q '$REG_CODE' '$T/hub.log'"
curl -fsS "$HUB/install" >"$T/install.sh"
check "/install is the installer with the hub's address on top" grep -q "^HARNESSLY_HUB_URL='$HUB'" "$T/install.sh"
check "the installer parses (sh -n)" sh -n "$T/install.sh"
check "/dl/manifest.json lists what a machine downloads" bash -c "curl -fsS '$HUB/dl/manifest.json' | jq -e '.files[\"agent-$OS-$ARCH\"].sha256 | length == 64'"
check "the downloaded agent is the one in the manifest" bash -c "test \"\$(curl -fsS '$HUB/dl/agent-$OS-$ARCH' | sha256sum | cut -d' ' -f1)\" = \"\$(curl -fsS '$HUB/dl/manifest.json' | jq -r '.files[\"agent-$OS-$ARCH\"].sha256')\""

# --- 5. register the machine --------------------------------------------------------------------------------------

step "Register a machine agent with the code (throwaway HOME)"
MHOME="$T/machine"
mkdir -p "$MHOME/work" "$T/tmux"
chmod 700 "$T/tmux"
machine_env() {
	env HOME="$MHOME" TMUX_TMPDIR="$T/tmux" AGENT_DATA_DIR="$MHOME/.harnessly" AGENT_POLICY_FILE="$MHOME/policy.toml" \
		AGENT_WORK_DIR="$MHOME/work" "$@"
}
machine_env "$T/bin/harnessly-agent" register --hub "$HUB" --token "$REG_CODE" --profile full --via command --name e2e-box >"$T/register.out" 2>&1 \
	|| die "register failed: $(cat "$T/register.out")"
MF="$MHOME/.harnessly/machine.json"
check "machine.json is private (0600)" test "$(stat -c %a "$MF")" = 600
MID=$(jq -r .machineId "$MF")
BASE=$(jq -r .runtimeBasePath "$MF")
expect "the base path is /m/<id>" "$BASE" "/m/$MID"
expect "the dial address is the hub's, as ws" "$(jq -r .dialUrl "$MF")" "ws://127.0.0.1:$HUB_PORT/agent/v1"
check "the machine has an Ed25519 key (a 32-byte seed)" bash -c "test \"\$(jq -r .ed25519 '$MF' | base64 -d | wc -c)\" = 32"
check "the registration code is not left on the machine" bash -c "! grep -rq '$REG_CODE' '$MHOME'"
if machine_env "$T/bin/harnessly-agent" register --hub "$HUB" --token "$REG_CODE" --profile full --via command --force >"$T/register2.out" 2>&1; then
	bad "the same code registered a second machine"
else
	ok "a used code does not register again"
fi
call "$OWNER" GET "/api/machines/$MID"
expect "the machine is known, still pending" "$(j .machine.status)" pending
expect "it carries the hostname asked for" "$(j .machine.hostname)" e2e-box
expect "its profile" "$(j .machine.profile)" full

# --- 6. run the runtime and the agent -----------------------------------------------------------------------------

step "Run the machine's runtime and agent"
(
	export HOME="$MHOME" TMUX_TMPDIR="$T/tmux" AGENT_DATA_DIR="$MHOME/.harnessly" AGENT_POLICY_FILE="$MHOME/policy.toml"
	export RUNTIME_GATE_LISTEN="127.0.0.1:$RT_PORT" RUNTIME_GATE_UPSTREAM='' RUNTIME_APP_BASE_PATH="$BASE" RUNTIME_HUB_URL="$HUB"
	export RUNTIME_PREVIEW_LISTEN=off RUNTIME_WORKSPACE_DIR="$MHOME/work"
	exec "$T/bin/harnessly-runtime"
) >"$T/runtime.log" 2>&1 &
PIDS+=($!)
(
	export HOME="$MHOME" TMUX_TMPDIR="$T/tmux" AGENT_DATA_DIR="$MHOME/.harnessly" AGENT_POLICY_FILE="$MHOME/policy.toml"
	export AGENT_PORT="$AG_PORT" RUNTIME_PORT="$RT_PORT" AGENT_WORK_DIR="$MHOME/work"
	exec "$T/bin/harnessly-agent"
) >"$T/agent.log" 2>&1 &
AGENT_PID=$!
PIDS+=("$AGENT_PID")
wait_for "the machine shows online in the hub" 40 machine_is "$OWNER" online || die "never online; agent log: $(tail -n 5 "$T/agent.log")"
check "the agent says it is linked (harnessly-agent status)" machine_env "$T/bin/harnessly-agent" status
call "$OWNER" GET "/api/machines/$MID"
expect "the hub knows the agent's version" "$([ -n "$(j .machine.agentVersion)" ] && echo yes)" yes
expect "the machine has an X25519 key for sealed secrets (32 bytes)" "$(j .machine.encKey | base64 -d | wc -c | tr -d ' ')" 32
wait_for "the preflight arrived" 30 bash -c "curl -fsS -b '$OWNER' '$HUB/api/machines/$MID' | jq -e '.machine.preflight.disk.free > 0'" || true
call "$OWNER" GET "/api/machines/$MID"
PF=$(j .machine.preflight)
expect "preflight: memory total is reported" "$(jq '.memTotal > 0' <<<"$PF")" true
expect "preflight: free disk is reported" "$(jq '.disk.free > 0' <<<"$PF")" true
expect "preflight: cpus" "$(jq '.cpus > 0' <<<"$PF")" true
say "  info  $(jq -r '"\(.osName) · \(.cpus) cores · \(.memTotal/1073741824|floor) GB memory · \(.disk.free/1073741824|floor) GB free · checks: \([.checks[]|"\(.id)=\(.level)"]|join(" "))"' <<<"$PF")"
call "$OWNER" POST "/api/machines/$MID/preflight" '{}'
expect "the preflight can be run again" "$CODE" 200
call "$OWNER" GET "/api/spaces/$SID/machines"
expect "the space lists the machine" "$(j '.machines|length')" 1

# --- 7. the agents' status ----------------------------------------------------------------------------------------

step "Agent status endpoints"
call "$OWNER" GET "/api/machines/$MID/agents?refresh=1"
expect "status answers" "$CODE" 200
expect "claude, codex and gemini" "$(j '[.agents[].provider]|join(",")')" claude,codex,gemini
expect "gemini is «Скоро»" "$(j '.agents[]|select(.provider=="gemini").soon')" true
say "  info  claude installed=$(j '.agents[]|select(.provider=="claude").installed') login=$(j '.agents[]|select(.provider=="claude").login'); codex installed=$(j '.agents[]|select(.provider=="codex").installed') login=$(j '.agents[]|select(.provider=="codex").login')"
call "$OWNER" POST "/api/machines/$MID/agents/gemini/check" '{}'
expect "gemini is not implemented (provider_off)" "$(j .error)" provider_off

# --- 8. provider check, proxy, keys, secrets ----------------------------------------------------------------------

step "Provider check, proxy, sealed key and secret"
if [ "${E2E_REAL_PROVIDER:-0}" = 1 ]; then
	call "$OWNER" POST "/api/machines/$MID/agents/claude/check" '{}'
	V=$(j .check.verdict)
	case "$V" in ok | network | rejected) ok "the check on the real servers answered: $V" ;; *) bad "unexpected verdict '$V': $BODY" ;; esac
fi
call "$OWNER" POST "/api/machines/$MID/agents/claude/check" '{"proxyUrl":"http://127.0.0.1:9"}'
expect "a check through a proxy that is not there" "$(j .check.verdict)" network
expect "…said so as coming through the proxy" "$(j .check.via)" proxy
expect "…and lists the hosts it tried" "$(j '.check.hosts|length > 0')" true
call "$OWNER" PUT "/api/machines/$MID/proxy" '{"scope":"all","url":"http://user:proxypass@127.0.0.1:9"}'
expect "the proxy is saved" "$CODE" 200
expect "the answer shows the host" "$(j .proxy.all.host)" 127.0.0.1:9
check "…and no credentials" bash -c "! grep -q proxypass <<<'$BODY'"
call "$OWNER" PUT "/api/machines/$MID/proxy" '{"scope":"codex","url":"socks5://127.0.0.1:9"}'
expect "a SOCKS proxy gets a local HTTP bridge" "$(j '.proxy.codex.bridge|test("^127\\.0\\.0\\.1:[0-9]+$")')" true
call "$OWNER" PUT "/api/machines/$MID/proxy" '{"scope":"all","url":"ftp://x"}'
expect "a proxy of another kind is refused" "$CODE" 400
check "the proxy is in the env file, 0600" bash -c "test \"\$(stat -c %a '$MHOME/.harnessly/agent-env/all.env')\" = 600 && grep -q HTTPS_PROXY '$MHOME/.harnessly/agent-env/all.env'"

call "$OWNER" GET "/api/machines/$MID"
ENC=$(j .machine.encKey)
# seal <purpose> <value>: the envelope the page would make.
seal() { printf %s "$2" | go run ./tools/hub-e2e/seal "$ENC" "$MID" "$1"; }
KEYVAL="e2e-fake-claude-key-$(date +%s)-$RANDOM"
call "$OWNER" POST "/api/machines/$MID/agents/claude/key" "{\"envelope\":$(seal key:claude "$KEYVAL")}"
expect "a sealed API key is delivered" "$CODE" 200
expect "…saved on the machine" "$(j .check.saved)" true
expect "…and seen by a login shell" "$(j .check.present)" true
V=$(j .check.verdict)
case "$V" in rejected | network | accepted) ok "the key was checked: $V" ;; *) bad "unexpected key verdict '$V'" ;; esac
check "the key is in claude.env, 0600" bash -c "test \"\$(stat -c %a '$MHOME/.harnessly/agent-env/claude.env')\" = 600 && grep -q ANTHROPIC_API_KEY '$MHOME/.harnessly/agent-env/claude.env'"
call "$OWNER" POST "/api/machines/$MID/agents/claude/key" "{\"envelope\":$(seal key:codex "$KEYVAL")}"
expect "an envelope made for another purpose does not open" "$CODE" 400
call "$OWNER" POST "/api/machines/$MID/agents/claude/key/verify" '{}'
expect "the key can be checked again" "$CODE" 200

SECRET="e2e-fake-secret-$(date +%s)-$RANDOM"
call "$OWNER" POST "/api/machines/$MID/env" "{\"name\":\"GH_TOKEN\",\"envelope\":$(seal env:GH_TOKEN "$SECRET")}"
expect "a secret is delivered" "$CODE" 200
expect "…and only its name comes back" "$(j '.env|join(",")')" GH_TOKEN
call "$OWNER" POST "/api/machines/$MID/env" "{\"name\":\"GIT_SSH_COMMAND\",\"envelope\":$(seal env:GIT_SSH_COMMAND x)}"
expect "a variable that would run code (GIT_SSH_COMMAND) is refused" "$CODE" 400
call "$OWNER" POST "/api/machines/$MID/env" "{\"name\":\"DEBUG\",\"envelope\":$(seal env:DEBUG 1)}"
expect "a name that does not look like a secret is refused" "$CODE" 400
call "$OWNER" GET "/api/machines/$MID"
expect "the machine lists the secret's name" "$(j '.machine.env|join(",")')" GH_TOKEN
call "$OWNER" POST "/api/machines/$MID/gitkey" '{}'
expect "a Git key is made" "$(j '.publicKey|startswith("ssh-ed25519 ")')" true

PGDUMP=$(command -v pg_dump || true)
if [ -z "$PGDUMP" ]; then
	for candidate in /usr/lib/postgresql/*/bin/pg_dump; do
		if [ -x "$candidate" ]; then PGDUMP=$candidate; fi
	done
fi
if [ -n "$DB_URL" ] && [ -n "$PGDUMP" ]; then
	"$PGDUMP" --data-only "$DB_URL" >"$T/hub/pg-dump.sql" 2>/dev/null || bad "pg_dump failed"
fi
check "the hub stored neither the key nor the secret (database, data folder, log)" bash -c "! grep -rqa -e '$KEYVAL' -e '$SECRET' '$T/hub' '$T/hub.log'"
check "the machine's logs do not hold them either" bash -c "! grep -rqa -e '$KEYVAL' -e '$SECRET' '$T/agent.log' '$T/runtime.log' '$MHOME/.harnessly/agent-audit.log'"
check "the audit log on the machine names the changes" bash -c "grep -q 'env.deliver' '$MHOME/.harnessly/agent-audit.log' && grep -q 'key.deliver' '$MHOME/.harnessly/agent-audit.log'"
call "$OWNER" DELETE "/api/machines/$MID/env/GH_TOKEN"
expect "a secret can be removed" "$(j '.env|length')" 0
call "$OWNER" DELETE "/api/machines/$MID/proxy?scope=all"
call "$OWNER" DELETE "/api/machines/$MID/proxy?scope=codex"
expect "the proxy can be cleared" "$(j '[.proxy.all,.proxy.claude,.proxy.codex]|map(select(.!=null))|length')" 0

# --- 9. console login ---------------------------------------------------------------------------------------------

step "Login in a console tab (the runtime makes the page, the hub relays it)"
call "$OWNER" POST "/api/machines/$MID/agents/codex/login" '{"method":"console"}'
expect "the login starts" "$CODE" 202
LID=$(j .login.id)
wait_for "the console URL arrives on the event stream" 20 grep -q '"consoleUrl":"/m/' "$T/events.txt" || true
CONSOLE=$(grep -o '"consoleUrl":"[^"]*"' "$T/events.txt" | head -n 1 | cut -d'"' -f4 || true)
if [ -n "$CONSOLE" ]; then
	check "it is a page under the machine's base path" bash -c "[[ '$CONSOLE' == $BASE/console/* ]]"
	CONSOLE_CODE=$(curl -sS -o "$T/console.html" -w '%{http_code}' -b "$OWNER" "$HUB$CONSOLE")
	expect "the console page opens through the hub" "$CONSOLE_CODE" 200
else
	bad "no consoleUrl on the event stream"
fi
call "$OWNER" POST "/api/machines/$MID/agents/codex/login" '{"method":"console"}'
expect "a second login on the machine is refused while one runs" "$CODE" 409
call "$OWNER" DELETE "/api/machines/$MID/logins/$LID"
expect "a login can be cancelled" "$CODE" 204

# --- 10. the machine's page through the hub -----------------------------------------------------------------------

step "Open the machine through the hub"
call "$OWNER" GET "/api/machines/$MID/open"
expect "the open address (path mode)" "$(j .url)" "$BASE/"
UNAUTH=$(curl -sS -o /dev/null -D "$T/h.txt" -w '%{http_code}' "$HUB$BASE/")
expect "without a session the hub sends to its sign-in" "$UNAUTH" 302
check "…with the way back" grep -qi "^location: /signin?next=" "$T/h.txt"
call "$OWNER" GET "$BASE/api/auth/setup"
expect "the runtime offers no password setup behind a hub" "$(j .needsSetup)" false
SETUP_POST=$(curl -sS -o /dev/null -w '%{http_code}' -b "$OWNER" -X POST -H 'Content-Type: application/json' --data '{"password":"x"}' "$HUB$BASE/api/auth/setup")
expect "…and refuses to set one" "$SETUP_POST" 400
if [ "$HAVE_PAGE" = 1 ]; then
	PAGE=$(curl -sS -o "$T/page.html" -w '%{http_code}' -b "$OWNER" -H 'Accept: text/html' "$HUB$BASE/")
	expect "the machine's page is served" "$PAGE" 200
	check "…it knows the hub (hubUrl in its config)" grep -q "hubUrl" "$T/page.html"
fi
SET_COOKIES=$(curl -sS -o /dev/null -D - -b "$OWNER" "$HUB$BASE/api/auth/setup" | grep -ci '^set-cookie:' || true)
expect "no cookie of the runtime reaches the browser" "$SET_COOKIES" 0

# --- 11. policy and the kill switch -------------------------------------------------------------------------------

step "The machine's own say: policy.toml and the kill switch"
printf 'terminal = false\n' >"$MHOME/policy.toml"
wait_for "the hub hears that the terminal is off" 30 bash -c "curl -fsS -b '$OWNER' '$HUB/api/machines/$MID' | jq -e '.machine.policy.terminal == false'" || true
DENIED=$(curl -sS -o /dev/null -w '%{http_code}' -b "$OWNER" "$HUB$BASE/api/v2/terminal")
expect "a shell route is refused by the machine" "$DENIED" 403
call "$OWNER" POST "/api/machines/$MID/agents/claude/login" '{"method":"console"}'
expect "a console login is refused too (it is a terminal)" "$(j .error)" machine_policy
rm -f "$MHOME/policy.toml"
# The agent's own account could write that file, so it only narrows on its own; opening up takes a person's SIGHUP.
sleep 3
check "without a SIGHUP the terminal stays off" bash -c "curl -fsS -b '$OWNER' '$HUB/api/machines/$MID' | jq -e '.machine.policy.terminal == false'"
kill -HUP "$AGENT_PID"
wait_for "after a SIGHUP the policy is back to the defaults" 30 bash -c "curl -fsS -b '$OWNER' '$HUB/api/machines/$MID' | jq -e '.machine.policy.terminal == true'" || true
machine_env "$T/bin/harnessly-agent" off >/dev/null
wait_for "the kill switch takes the machine offline" 40 machine_is "$OWNER" offline || true
call "$OWNER" POST "/api/machines/$MID/preflight" '{}'
expect "an offline machine answers machine_offline" "$(j .error)" machine_offline
OFFLINE_PAGE=$(curl -sS -o "$T/offline.html" -w '%{http_code}' -b "$OWNER" -H 'Accept: text/html' "$HUB$BASE/")
expect "…and its page says so (502)" "$OFFLINE_PAGE" 502
machine_env "$T/bin/harnessly-agent" on >/dev/null
wait_for "switched on again, the machine is back" 90 machine_is "$OWNER" online || true

# --- 12. a second person -----------------------------------------------------------------------------------------

step "A second person joins by invitation and has a member's rights"
call "$OWNER" POST "/api/spaces/$SID/invites/link" '{}'
expect "an invitation link is made" "$CODE" 201
TOKEN=$(j .invite.url | sed 's#.*/invite/##')
MEMBER="$T/jar-member"
: >"$MEMBER"
call "$MEMBER" GET "/api/invites/$TOKEN"
expect "the invitation can be read before signing in" "$(j .valid)" true
sign_up "$MEMBER" bob@example.com Боб
call "$MEMBER" POST "/api/invites/$TOKEN/accept" '{}'
expect "the invitation is accepted" "$CODE" 200
call "$MEMBER" GET "/api/spaces/$SID/machines"
expect "a member sees the machines" "$(j '.machines|length')" 1
call "$MEMBER" POST "/api/machines/$MID/agents/claude/check" '{}'
expect "…but cannot run an agent operation" "$CODE" 403
call "$MEMBER" POST "/api/spaces/$SID/machines/codes" '{"profile":"dir"}'
expect "…nor ask for a registration code" "$CODE" 403
call "$MEMBER" GET "$BASE/api/auth/setup"
expect "…but can open the machine" "$CODE" 200
call "$MEMBER" PATCH "/api/spaces/$SID" '{"name":"mine now"}'
expect "…and cannot rename the space" "$CODE" 403
OUTSIDER="$T/jar-outsider"
: >"$OUTSIDER"
sign_up "$OUTSIDER" eve@example.com Ева
call "$OUTSIDER" GET "/api/machines/$MID"
check "someone outside the space cannot see the machine" test "$CODE" = 403 -o "$CODE" = 404
OUT_PAGE=$(curl -sS -o /dev/null -w '%{http_code}' -b "$OUTSIDER" -H 'Accept: text/html' "$HUB$BASE/")
check "…or open its page" test "$OUT_PAGE" = 403 -o "$OUT_PAGE" = 404
BOB_ID=$(curl -fsS -b "$OWNER" "$HUB/api/spaces/$SID/members" | jq -r '.members[]|select(.email=="bob@example.com").userId')
call "$OWNER" DELETE "/api/spaces/$SID/members/$BOB_ID"
expect "a member can be removed" "$CODE" 204
call "$MEMBER" GET "/api/spaces/$SID/machines"
expect "…and then sees nothing" "$CODE" 403

# --- 13. the local machine ("Эта машина") -------------------------------------------------------------------------

step "The local machine: registered from the hub's code file, claimed by the instance admin"
LOCAL_HOME="$T/local"
mkdir -p "$LOCAL_HOME"
wait_for "the hub keeps a local code in its file" 10 test -s "$T/hub/local-code" || true
if HOME="$LOCAL_HOME" "$T/bin/harnessly-agent" register --hub "$HUB" --code-file "$T/hub/local-code" --profile container --via local \
	--machine-file "$LOCAL_HOME/machine.json" >"$T/register-local.out" 2>&1; then
	ok "registered with the code file"
else
	bad "local register failed: $(cat "$T/register-local.out")"
fi
call "$OWNER" GET /api/config
expect "/api/config: a local machine is waiting" "$(j .connect.local.available)" true
call "$OUTSIDER" POST "/api/spaces/$SID/machines/local" '{}'
check "someone who is not the admin cannot claim it" test "$CODE" = 403 -o "$CODE" = 401 -o "$CODE" = 404
call "$OWNER" POST "/api/spaces/$SID/machines/local" '{}'
expect "the admin claims it" "$CODE" 200
expect "…it is via local" "$(j .machine.via)" local

# --- 14. revoke --------------------------------------------------------------------------------------------------

step "Revoke the machine"
call "$OWNER" DELETE "/api/machines/$MID"
expect "the machine is removed" "$CODE" 204
wait_for "the agent learns it was revoked" 30 grep -q 'machine revoked by the hub' "$T/agent.log" || true
call "$OWNER" GET "/api/machines/$MID"
expect "the hub no longer knows it" "$CODE" 404
check "its events reached the stream (machine, machineRemoved)" bash -c "grep -q '^event: machine\$' '$T/events.txt' && grep -q '^event: machineRemoved\$' '$T/events.txt'"
check "no registration code or key ever reached the hub's log" bash -c "! grep -q -e 'hx_' -e '$KEYVAL' -e '$SECRET' '$T/hub.log'"

# --- 15. the UI --------------------------------------------------------------------------------------------------

step "The hub UI"
if [ -n "$WEB_DIR" ]; then
	curl -sS -o "$T/index.html" -D "$T/index.h" "$HUB/" || true
	check "/ is the page" grep -qi '^content-type: text/html' "$T/index.h"
	check "…with a content security policy" grep -qi '^content-security-policy:' "$T/index.h"
	ASSETS=$(grep -oE '(src|href)="/assets/[^"]+"' "$T/index.html" | cut -d'"' -f2 | sort -u || true)
	if [ -n "$ASSETS" ]; then ok "the page names its assets"; else bad "no assets in the page"; fi
	for a in $ASSETS; do
		A=$(curl -sS -o "$T/asset" -w '%{http_code} %{size_download}' "$HUB$a")
		case "$A" in "200 "*) if [ "${A#* }" -gt 0 ]; then ok "$a loads (${A#* } bytes)"; else bad "$a is empty"; fi ;; *) bad "$a: $A" ;; esac
	done
	for route in /signin /signup /new /invite/x "/s/$SLUG/setup/machines" "/s/$SLUG/setup/agents" "/s/$SLUG/setup/access" "/s/$SLUG/setup/done" "/s/$SLUG" "/s/$SLUG/settings" /settings; do
		R=$(curl -sS -o "$T/route.html" -w '%{http_code} %{content_type}' "$HUB$route")
		case "$R" in "200 text/html"*) ok "$route serves the page" ;; *) bad "$route: $R" ;; esac
	done
	expect "a missing asset is a 404, not the page" "$(curl -sS -o /dev/null -w '%{http_code}' "$HUB/assets/nope.js")" 404
else
	say "  skip  no hub UI built"
fi

# --- 16. screenshots ---------------------------------------------------------------------------------------------

if [ -n "$SHOTS_DIR" ] && [ -n "$WEB_DIR" ]; then
	step "Screenshots into $SHOTS_DIR"
	# A new machine for the pages that list one: the previous one was revoked above.
	call "$OWNER" POST "/api/spaces/$SID/machines/codes" '{"profile":"full","confirmFull":true}'
	CODE2=$(j .code)
	SHOT_HOME="$T/machine2"
	mkdir -p "$SHOT_HOME/work"
	env HOME="$SHOT_HOME" AGENT_DATA_DIR="$SHOT_HOME/.harnessly" AGENT_POLICY_FILE="$SHOT_HOME/policy.toml" \
		"$T/bin/harnessly-agent" register --hub "$HUB" --token "$CODE2" --profile full --via command --name second-box >/dev/null
	MID2=$(jq -r .machineId "$SHOT_HOME/.harnessly/machine.json")
	BASE2=$(jq -r .runtimeBasePath "$SHOT_HOME/.harnessly/machine.json")
	RT2=$((RT_PORT + 10))
	AG2=$((AG_PORT + 10))
	(
		export HOME="$SHOT_HOME" TMUX_TMPDIR="$T/tmux" AGENT_DATA_DIR="$SHOT_HOME/.harnessly" AGENT_POLICY_FILE="$SHOT_HOME/policy.toml"
		export RUNTIME_GATE_LISTEN="127.0.0.1:$RT2" RUNTIME_GATE_UPSTREAM='' RUNTIME_APP_BASE_PATH="$BASE2" RUNTIME_HUB_URL="$HUB"
		export RUNTIME_PREVIEW_LISTEN=off RUNTIME_WORKSPACE_DIR="$SHOT_HOME/work"
		exec "$T/bin/harnessly-runtime"
	) >"$T/runtime2.log" 2>&1 &
	PIDS+=($!)
	(
		export HOME="$SHOT_HOME" TMUX_TMPDIR="$T/tmux" AGENT_DATA_DIR="$SHOT_HOME/.harnessly" AGENT_POLICY_FILE="$SHOT_HOME/policy.toml"
		export AGENT_PORT="$AG2" RUNTIME_PORT="$RT2" AGENT_WORK_DIR="$SHOT_HOME/work"
		exec "$T/bin/harnessly-agent"
	) >"$T/agent2.log" 2>&1 &
	PIDS+=($!)
	MID="$MID2"
	wait_for "the second machine is online" 40 machine_is "$OWNER" online || true
	COOKIE_NAME=$(awk '$6 ~ /^(__Host-)?hl_/ && $6 !~ /hl_(oauth|run)$/ {print $6}' "$OWNER" | head -n 1)
	COOKIE_VALUE=$(awk '$6 ~ /^(__Host-)?hl_/ && $6 !~ /hl_(oauth|run)$/ {print $7}' "$OWNER" | head -n 1)
	PAGE_ID=""
	if [ "$HAVE_PAGE" = 1 ]; then PAGE_ID="$MID2"; fi
	if node "$ROOT/tools/hub-e2e/shots.mjs" "$HUB" "$SHOTS_DIR" "$COOKIE_NAME" "$COOKIE_VALUE" "$SLUG" "$PAGE_ID" 2>&1 | sed 's/^/  /'; then
		ok "screenshots taken, no console errors"
	else
		bad "the browser saw errors (above)"
	fi
fi
