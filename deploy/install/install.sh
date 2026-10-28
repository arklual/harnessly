#!/bin/sh
# harnessly machine installer: connects this machine to a harnessly hub.
#
#   curl -fsSL <hub>/install | sh -s -- --token <code> --profile dir|container|full [--name <n>] [--dry-run] [--uninstall]
#
# What it does, what it leaves behind and how to undo it is in docs/hub/machine.md. The hub serves this file with
# HARNESSLY_HUB_URL (and HARNESSLY_IMAGE, when it has a machine image) set on the lines right below this one. Nothing
# runs until main() on the last line, so a download that stops half way does nothing.

usage() {
	cat <<'EOF'
harnessly installer

  curl -fsSL <hub>/install | sh -s -- --token <code> --profile dir|container|full [options]

  --token CODE       the one-time registration code from the hub
  --profile P        dir        a separate Unix user and folder, system services (needs root once)
                     container  the hub's machine image, run with Docker or Podman
                     full       services of this account; agents see everything it sees
  --name NAME        the name the machine shows in the hub (default: the host's name)
  --dry-run          print what would be done and change nothing
  --uninstall        remove what an earlier run installed, and the machine from the hub's list (--purge: also the user,
                     home and work folder the installer made; what was there before is never removed)
  --re-register      register the machine anew; by default a machine already registered with this hub is kept
  --wait SECONDS     how long to wait for the machine to link to the hub (default 60)
  --hub URL          the hub's address (default: the one this script was served by)
  --image IMAGE      the machine image of the container profile (default: the hub's)
EOF
}

# --- messages: English, and Russian when the locale is ----------------------------------------------------------

lang_init() {
	case ${HARNESSLY_LANG:-${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}} in
	ru*) RU=1 ;;
	*) RU=0 ;;
	esac
}

pick() {
	if [ "$RU" = 1 ]; then printf '%s' "$2"; else printf '%s' "$1"; fi
}

say() { printf '%s\n' "$(pick "$1" "$2")"; }
step() {
	STEP=$(pick "$1" "$2")
	printf '==> %s\n' "$STEP"
}
warn() { printf 'harnessly-install: %s\n' "$(pick "$1" "$2")" >&2; }
die() {
	warn "$1" "$2"
	exit 1
}
# fail stops the installation, but only warns on a dry run: the plan goes on and shows what a real run would stop at.
fail() {
	if [ "$DRY" = 1 ]; then
		warn "a real run would stop here: $1" "настоящий запуск остановился бы здесь: $2"
		return 0
	fi
	die "$1" "$2"
}
plan() { printf '  would %s\n' "$1"; }

# --- arguments ----------------------------------------------------------------------------------------------------

parse_args() {
	TOKEN=
	PROFILE=
	NAME=
	DRY=0
	UNINSTALL=0
	PURGE=0
	REREGISTER=0
	WAIT=60
	IMAGE=${HARNESSLY_IMAGE:-}
	HUB_URL=${HARNESSLY_HUB_URL:-}
	while [ $# -gt 0 ]; do
		case $1 in
		--*=*)
			_k=${1%%=*}
			_v=${1#*=}
			shift
			set -- "$_k" "$_v" "$@"
			continue
			;;
		--dry-run) DRY=1 ;;
		--uninstall) UNINSTALL=1 ;;
		--purge) PURGE=1 ;;
		--re-register) REREGISTER=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		--token | --profile | --name | --wait | --hub | --image)
			[ $# -ge 2 ] || die "$1 needs a value" "для $1 нужно значение"
			case $1 in
			--token) TOKEN=$2 ;;
			--profile) PROFILE=$2 ;;
			--name) NAME=$2 ;;
			--wait) WAIT=$2 ;;
			--hub) HUB_URL=$2 ;;
			--image) IMAGE=$2 ;;
			esac
			shift
			;;
		*)
			usage >&2
			die "unknown argument: $1" "неизвестный аргумент: $1"
			;;
		esac
		shift
	done
	while :; do
		case $HUB_URL in */) HUB_URL=${HUB_URL%/} ;; *) break ;; esac
	done
}

validate_args() {
	[ -n "$HUB_URL" ] || die "the hub's address is unknown: run the command from the hub, or give --hub https://..." \
		"адрес хаба неизвестен: запустите команду из хаба или укажите --hub https://..."
	case $HUB_URL in http://* | https://*) ;; *) die "--hub must be an http:// or https:// address" "--hub должен быть адресом http:// или https://" ;; esac
	case $HUB_URL in *[!A-Za-z0-9:/._@%+~=-]*) die "the hub's address has odd characters" "в адресе хаба странные символы" ;; esac
	case $WAIT in '' | *[!0-9]*) die "--wait is a number of seconds" "--wait задаётся числом секунд" ;; esac
	case $NAME in *[[:cntrl:]]*) die "--name has control characters" "в --name есть управляющие символы" ;; esac
	[ "${#NAME}" -le 100 ] || die "--name is longer than 100 characters" "--name длиннее 100 символов"
	case $IMAGE in *[!A-Za-z0-9:/._@+-]*) die "--image has odd characters" "в --image странные символы" ;; esac
	case $PROFILE in
	dir | container | full) ;;
	'') [ "$UNINSTALL" = 1 ] || die "--profile dir, container or full is required" "нужен --profile dir, container или full" ;;
	*) die "--profile must be dir, container or full" "--profile должен быть dir, container или full" ;;
	esac
	if [ "$UNINSTALL" = 0 ]; then
		[ -n "$TOKEN" ] || die "--token is required: copy the command from the hub" "нужен --token: скопируйте команду из хаба"
		[ "${#TOKEN}" -le 512 ] || die "the code is too long" "код слишком длинный"
		case $TOKEN in *[!A-Za-z0-9._~+=/-]*) die "the code has odd characters" "в коде странные символы" ;; esac
	fi
}

# --- platform -----------------------------------------------------------------------------------------------------

detect_platform() {
	case $(uname -s) in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	MINGW* | MSYS* | CYGWIN* | Windows*) die "Windows is not supported: the agents need tmux. Use WSL or a Linux machine." \
		"Windows не поддерживается: агентам нужен tmux. Используйте WSL или Linux-машину." ;;
	*) die "unsupported system: $(uname -s)" "система не поддерживается: $(uname -s)" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "unsupported processor: $(uname -m)" "процессор не поддерживается: $(uname -m)" ;;
	esac
	if [ "$OS" = darwin ] && [ "$PROFILE" != full ] && [ "$UNINSTALL" = 0 ]; then
		die "macOS machines use --profile full" "на macOS подходит только --profile full"
	fi
}

# --- tools: every change goes through these, so a dry run shows it and a root-less run uses sudo -----------------------

# rp is a path under the install root, which is empty except in tests and image builds.
rp() { printf '%s%s' "$ROOT" "$1"; }

# run_priv runs a command as root when the profile needs it.
run_priv() {
	if [ "$PRIV" = 1 ] && [ "$IS_ROOT" != 1 ]; then sudo -n "$@"; else "$@"; fi
}

# X runs a command that changes something.
X() {
	if [ "$DRY" = 1 ]; then
		plan "run: $*"
		return 0
	fi
	run_priv "$@"
}

# mkd makes a folder: mkd PATH MODE [OWNER]. A folder that exists is left as it is; one it makes is recorded, so
# --uninstall can take it away. The mode is given to mkdir and the owner with chown -h: a folder inside the home of the
# service account can be swapped for a link by that account, and a chmod or chown that follows it would change a file
# of root's.
mkd() {
	MKD_MADE=0
	_real=$(rp "$1")
	if [ -d "$_real" ]; then return 0; fi
	MKD_MADE=1
	track dir "$1"
	X mkdir -p -m "$2" "$_real"
	if [ -n "${3:-}" ]; then X chown -h "$3" "$_real"; fi
}

# purge_if_made marks the folder mkd has just made as one --uninstall --purge may remove with what is in it. A folder
# that was there before the installer came is never marked: what it holds is not ours.
purge_if_made() {
	if [ "$MKD_MADE" = 1 ]; then track purge "$1"; fi
}

# not_link stops on a path that is a symbolic link. The folders in the home of the service account are the account's to
# change, and root works in them (it makes them, and removes what the installer made); a link put there would aim that
# work at a file of root's.
not_link() {
	[ ! -L "$(rp "$1")" ] || fail "$1 is a symbolic link; remove it and run again" "$1 — символическая ссылка; удалите её и запустите снова"
}

# put writes stdin to a file, private from the first byte: put PATH MODE [OWNER].
put() {
	_real=$(rp "$1")
	_mode=$2
	_owner=${3:-}
	if [ "$DRY" = 1 ]; then
		cat >/dev/null
		plan "write $1 (mode $_mode${_owner:+, owner $_owner})"
		return 0
	fi
	_tmp="$_real.new.$$"
	# shellcheck disable=SC2016 # the script is for sh -c to expand
	run_priv sh -c 'umask 077 && : > "$1" && chmod "$2" "$1"' sh "$_tmp" "$_mode"
	if ! run_priv tee "$_tmp" >/dev/null; then
		run_priv rm -f "$_tmp"
		return 1
	fi
	if [ -n "$_owner" ]; then run_priv chown -h "$_owner" "$_tmp"; fi
	run_priv mv -f "$_tmp" "$_real"
}

# track notes something the installer made, for --uninstall: kind and up to two fields, tab separated.
track() {
	_line=$1
	shift
	for _f in "$@"; do _line="$_line$TAB$_f"; done
	case "$NL$MLINES" in *"$NL$_line$NL"*) return 0 ;; esac
	MLINES="$MLINES$_line$NL"
}

save_manifest() {
	{
		printf 'profile\t%s\n' "$PROFILE"
		printf 'hub\t%s\n' "$HUB_URL"
		printf 'os\t%s-%s\n' "$OS" "$ARCH"
		printf 'version\t%s\n' "${VERSION:-unknown}"
		printf 'format\t2\n'
		printf '%s' "$MLINES"
	} | put "$MANIFEST" "$MANIFEST_MODE"
}

load_manifest() {
	MLINES=
	_m=$(rp "$MANIFEST")
	[ -f "$_m" ] || return 0
	# A manifest of an earlier installer marked its home and work folders for --purge whether it had made them or not;
	# such marks are not taken over, so nothing that was not made by the installer is removed on their word.
	_fmt=$(awk -F "$TAB" '$1=="format"{print $2}' "$_m")
	while IFS=$TAB read -r _kind _a _b; do
		case $_kind in
		profile | hub | os | version | format | '') ;;
		purge) if [ "$_fmt" = 2 ]; then track "$_kind" "$_a"; fi ;;
		*) track "$_kind" "$_a" ${_b:+"$_b"} ;;
		esac
	done <"$_m"
}

# fetch downloads a URL to a file.
fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --connect-timeout 15 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed to download" "для загрузки нужен curl или wget"
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 -r "$1" | awk '{print $1}'
	else
		die "sha256sum, shasum or openssl is needed to check the download" "для проверки загрузки нужен sha256sum, shasum или openssl"
	fi
}

# json_field FILE KEY prints the first string value of KEY in a one-level JSON file such as machine.json.
json_field() {
	run_priv sed -n 's/.*"'"$2"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$1" | head -n 1
}

# safe_path says an absolute path from a manifest is deep enough and free of dots to be removed: never a top folder.
safe_path() {
	case $1 in
	/*) ;;
	*) return 1 ;;
	esac
	case $1/ in */../* | */./* | *//*) return 1 ;; esac
	_slashes=$(printf '%s' "$1" | tr -cd '/')
	[ "${#_slashes}" -ge 2 ]
}

# --- the system ---------------------------------------------------------------------------------------------------

check_root() {
	IS_ROOT=0
	[ "$(id -u)" = 0 ] && IS_ROOT=1
	if [ "$IS_ROOT" = 0 ]; then
		if ! command -v sudo >/dev/null 2>&1 || ! sudo -n true >/dev/null 2>&1; then
			fail "profile $PROFILE needs root once: run as root, or as a user with passwordless sudo (curl ... | sudo sh -s -- ...)" \
				"профилю $PROFILE один раз нужен root: запустите от root или от пользователя с sudo без пароля (curl ... | sudo sh -s -- ...)"
		fi
	fi
}

# ensure_account refuses root for the profile that gives the agents an account's access.
check_not_root() {
	IS_ROOT=0
	if [ "$(id -u)" = 0 ]; then
		fail "profile full runs as the account whose access the agents get: run it as that account, not root" \
			"профиль full работает от аккаунта, чей доступ получат агенты: запустите от него, не от root"
	fi
}

check_systemd() {
	if [ ! -d "$(rp /run/systemd/system)" ] || ! command -v systemctl >/dev/null 2>&1; then
		fail "systemd is needed for profile $PROFILE; without it use --profile container" \
			"для профиля $PROFILE нужен systemd; без него используйте --profile container"
	fi
}

install_packages() {
	if command -v apt-get >/dev/null 2>&1; then
		X apt-get update
		X env DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
	elif command -v dnf >/dev/null 2>&1; then
		X dnf install -y "$@"
	elif command -v yum >/dev/null 2>&1; then
		X yum install -y "$@"
	elif command -v zypper >/dev/null 2>&1; then
		X zypper -n install "$@"
	elif command -v pacman >/dev/null 2>&1; then
		X pacman -S --noconfirm --needed "$@"
	else
		fail "install $* and run the command again" "установите $* и запустите команду снова"
	fi
}

ensure_tools() {
	_missing=
	for _t in tmux git; do
		command -v "$_t" >/dev/null 2>&1 || _missing="$_missing $_t"
	done
	command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || _missing="$_missing curl"
	[ -n "$_missing" ] || return 0
	_missing=${_missing# }
	if [ "$PROFILE" = dir ]; then
		say "Installing $_missing" "Ставим $_missing"
		# shellcheck disable=SC2086 # the names are words
		install_packages $_missing
	elif [ "$OS" = darwin ]; then
		fail "install $_missing first (brew install $_missing) and run the command again" \
			"сначала установите $_missing (brew install $_missing) и запустите команду снова"
	else
		fail "install $_missing first (for example: sudo apt-get install $_missing) and run the command again" \
			"сначала установите $_missing (например: sudo apt-get install $_missing) и запустите команду снова"
	fi
}

# --- voice input (optional) ---------------------------------------------------------------------------------------
# The runtime brings its own speech recognition (a whisper.cpp server and a small model, fetched on the first
# dictation); what it needs from the system is ffmpeg, which turns the browser's recording into samples. Without it the
# microphone is simply not offered. Never fails the installation: dir installs the package when it can, the other
# profiles say how.
voice_tools() {
	command -v ffmpeg >/dev/null 2>&1 && return 0
	if [ "$PROFILE" = dir ]; then
		say "Installing ffmpeg (voice input)" "Ставим ffmpeg (голосовой ввод)"
		(install_packages ffmpeg) || warn "ffmpeg was not installed: voice input stays off until you install it" \
			"ffmpeg не установился: голосовой ввод выключен, пока его не поставите"
	elif [ "$OS" = darwin ]; then
		say "Voice input: brew install ffmpeg whisper-cpp, then restart the runtime" "Голосовой ввод: brew install ffmpeg whisper-cpp, затем перезапустите runtime"
	else
		say "Voice input needs ffmpeg: sudo apt-get install ffmpeg (or your system's package), then restart the runtime" \
			"Для голосового ввода нужен ffmpeg: sudo apt-get install ffmpeg (или пакет вашей системы), затем перезапустите runtime"
	fi
}

# nearest_existing prints the closest folder above a path that exists.
nearest_existing() {
	_d=$1
	while [ ! -d "$_d" ] && [ "$_d" != / ]; do _d=$(dirname "$_d"); done
	printf '%s' "$_d"
}

check_resources() {
	_free=$(df -Pk "$(nearest_existing "$(rp "$1")")" 2>/dev/null | awk 'NR==2{print $4}')
	case $_free in
	'' | *[!0-9]*) ;;
	*)
		if [ "$_free" -lt 512000 ]; then
			fail "less than 500 MB free where $1 is" "меньше 500 МБ свободно там, где $1"
		elif [ "$_free" -lt 2097152 ]; then
			warn "less than 2 GB free where $1: Claude Code and Codex will not fit comfortably" \
				"меньше 2 ГБ свободно там, где $1: Claude Code и Codex поместятся впритык"
		fi
		;;
	esac
	_ram=
	if [ -r /proc/meminfo ]; then
		_ram=$(awk '/^MemTotal:/{print $2}' /proc/meminfo)
	elif command -v sysctl >/dev/null 2>&1; then
		_ram=$(($(sysctl -n hw.memsize 2>/dev/null || echo 0) / 1024))
	fi
	case $_ram in
	'' | *[!0-9]*) ;;
	*)
		if [ "$_ram" -gt 0 ] && [ "$_ram" -lt 1048576 ]; then
			warn "less than 1 GB of memory: the agents will be slow" "меньше 1 ГБ памяти: агенты будут медленными"
		fi
		;;
	esac
}

# listening prints the local addresses of the listening TCP sockets.
listening() {
	if command -v ss >/dev/null 2>&1; then
		ss -ltn 2>/dev/null | awk 'NR>1{print $4}'
	elif command -v lsof >/dev/null 2>&1; then
		lsof -nP -iTCP -sTCP:LISTEN 2>/dev/null | awk 'NR>1{print $9}'
	elif command -v netstat >/dev/null 2>&1; then
		netstat -an 2>/dev/null | awk '/LISTEN/{print $4}'
	fi
}

# pick_ports sets RUNTIME_PORT and AGENT_PORT: the ports of an earlier install, else 18022 and the next free one.
pick_ports() {
	RUNTIME_PORT=
	AGENT_PORT=
	_rf=$(rp "$CONF_DIR/runtime.env")
	_af=$(rp "$CONF_DIR/agent.env")
	if [ -f "$_rf" ] && [ -f "$_af" ]; then
		RUNTIME_PORT=$(sed -n 's/^RUNTIME_GATE_LISTEN=127\.0\.0\.1:\([0-9][0-9]*\)$/\1/p' "$_rf" | head -n 1)
		AGENT_PORT=$(sed -n 's/^AGENT_PORT=\([0-9][0-9]*\)$/\1/p' "$_af" | head -n 1)
	fi
	if [ -n "$RUNTIME_PORT" ] && [ -n "$AGENT_PORT" ]; then return 0; fi
	_busy=$(listening)
	_p=18022
	while [ "$_p" -lt 18122 ]; do
		if ! printf '%s\n' "$_busy" | grep -Eq "[:.]$_p\$"; then
			if [ -z "$RUNTIME_PORT" ]; then RUNTIME_PORT=$_p; else
				AGENT_PORT=$_p
				return 0
			fi
		fi
		_p=$((_p + 1))
	done
	fail "no free port between 18022 and 18121" "нет свободного порта между 18022 и 18121"
	RUNTIME_PORT=18022
	AGENT_PORT=18023
}

# --- binaries -----------------------------------------------------------------------------------------------------

download_binaries() {
	step "Downloading harnessly from $HUB_URL" "Скачиваем harnessly с $HUB_URL"
	if [ "$DRY" = 1 ]; then
		plan "download $HUB_URL/dl/manifest.json, agent-$OS-$ARCH and runtime-$OS-$ARCH, and check their SHA-256"
		VERSION=dry-run
		return 0
	fi
	TMP=$(mktemp -d "${TMPDIR:-/tmp}/harnessly-install.XXXXXX")
	fetch "$HUB_URL/dl/manifest.json" "$TMP/manifest.json" ||
		die "could not download $HUB_URL/dl/manifest.json: is the address right and the hub up?" \
			"не удалось скачать $HUB_URL/dl/manifest.json: адрес верный, хаб работает?"
	_compact=$(tr -d ' \n\r\t' <"$TMP/manifest.json")
	VERSION=$(printf '%s' "$_compact" | sed -n 's/.*"version":"\([A-Za-z0-9._+-]*\)".*/\1/p' | head -n 1)
	for _part in agent runtime; do
		_file="$_part-$OS-$ARCH"
		_want=$(printf '%s' "$_compact" | sed -n 's/.*"'"$_file"'":{[^}]*"sha256":"\([0-9a-fA-F]\{64\}\)".*/\1/p' | head -n 1 | tr '[:upper:]' '[:lower:]')
		[ -n "$_want" ] || die "$_file is not in the hub's manifest: this system is not served by the hub" \
			"$_file нет в манифесте хаба: этот хаб не поставляет эту систему"
		fetch "$HUB_URL/dl/$_file" "$TMP/$_file" || die "could not download $_file" "не удалось скачать $_file"
		_got=$(sha256_of "$TMP/$_file" | tr '[:upper:]' '[:lower:]')
		[ "$_got" = "$_want" ] || die "$_file does not match the hub's checksum; nothing was installed" \
			"$_file не совпадает с контрольной суммой хаба; ничего не установлено"
	done
}

# install_binary puts a downloaded binary in place with a rename, so a running one is replaced without a crash.
install_binary() {
	_dest="$BIN_DIR/harnessly-$1"
	X cp "$TMP/$1-$OS-$ARCH" "$(rp "$_dest").new"
	X chmod 0755 "$(rp "$_dest").new"
	X mv -f "$(rp "$_dest").new" "$(rp "$_dest")"
	track file "$_dest"
}

install_binaries() {
	step "Installing the programs into $BIN_DIR" "Ставим программы в $BIN_DIR"
	if [ "$DRY" = 1 ]; then
		TMP=/nonexistent
	fi
	mkd "$BIN_DIR" 0755
	install_binary agent
	install_binary runtime
}

# --- configuration ------------------------------------------------------------------------------------------------

policy_text() {
	cat <<'EOF'
# The local policy of this machine (docs/hub/machine.md). The hub cannot change this file; the agent reads it at start,
# on SIGHUP and when it changes. A refused operation answers "denied by this machine's settings".
terminal = true          # terminals and the chat page's shell
agent_login = true       # signing agents in
keys = true              # API keys and secrets delivered from the hub
env = true               # environment variables delivered from the hub
install = true           # installing Claude Code and Codex
runtime_session = true   # the hub opening this machine's page for a person
deny_routes = []         # route prefixes of the runtime to refuse, e.g. ["/api/terminal", "/api/fs/write"]
EOF
}

# runtime_env and agent_env print the KEY=VALUE lines of the two services, for systemd files and launchd plists.
runtime_env() {
	printf 'RUNTIME_GATE_LISTEN=127.0.0.1:%s\n' "$RUNTIME_PORT"
	printf 'RUNTIME_GATE_UPSTREAM=\n'
	printf 'RUNTIME_APP_BASE_PATH=%s\n' "$BASE_PATH"
	printf 'RUNTIME_HUB_URL=%s\n' "$HUB_URL"
	printf 'RUNTIME_PREVIEW_LISTEN=off\n'
	printf 'RUNTIME_WORKSPACE_DIR=%s\n' "$WORK_DIR"
	printf 'AGENT_DATA_DIR=%s\n' "$DATA_DIR"
	printf 'AGENT_POLICY_FILE=%s\n' "$POLICY"
	if [ -n "${TMUX_DIR:-}" ]; then printf 'TMUX_TMPDIR=%s\n' "$TMUX_DIR"; fi
}

agent_env() {
	printf 'AGENT_PORT=%s\n' "$AGENT_PORT"
	printf 'RUNTIME_PORT=%s\n' "$RUNTIME_PORT"
	printf 'AGENT_DATA_DIR=%s\n' "$DATA_DIR"
	printf 'AGENT_POLICY_FILE=%s\n' "$POLICY"
	printf 'AGENT_WORK_DIR=%s\n' "$WORK_DIR"
	if [ -n "${TMUX_DIR:-}" ]; then printf 'TMUX_TMPDIR=%s\n' "$TMUX_DIR"; fi
}

write_policy() {
	if [ -f "$(rp "$POLICY")" ]; then
		say "Keeping the policy in $POLICY" "Политика в $POLICY остаётся как есть"
		return 0
	fi
	policy_text | put "$POLICY" "$POLICY_MODE"
	track file "$POLICY"
}

write_env_files() {
	{
		printf '# Written by the harnessly installer, which rewrites it when run again.\n'
		runtime_env
	} | put "$CONF_DIR/runtime.env" 0644
	{
		printf '# Written by the harnessly installer, which rewrites it when run again. "harnessly-agent status" reads the folders here.\n'
		agent_env
	} | put "$CONF_DIR/agent.env" 0644
	track file "$CONF_DIR/runtime.env"
	track file "$CONF_DIR/agent.env"
}

# as_service runs a command as the account the services run as.
as_service() {
	if [ "$PROFILE" = dir ]; then
		if command -v runuser >/dev/null 2>&1; then
			run_priv runuser -u "$SVC_USER" -- "$@"
		elif command -v sudo >/dev/null 2>&1; then
			run_priv sudo -n -u "$SVC_USER" "$@"
		else
			die "runuser or sudo is needed" "нужен runuser или sudo"
		fi
	else
		"$@"
	fi
}

# register exchanges the code for a machine id, as the account the services run as. The code goes by a private file,
# never on a command line.
register_machine() {
	step "Registering this machine with the hub" "Регистрируем машину в хабе"
	MACHINE_FILE="$DATA_DIR/machine.json"
	# A machine already registered with this hub is kept (the hub has it once, and the code is not even looked at); a new
	# registration is made when there is none, or on --re-register.
	_force=--reuse
	if [ "$REREGISTER" = 1 ]; then _force=--force; fi
	if [ "$DRY" = 1 ]; then
		plan "run: harnessly-agent register --hub $HUB_URL --code-file <the code, in a private file> --profile $PROFILE --via command${NAME:+ --name \"$NAME\"} $_force"
		BASE_PATH=/m/MACHINE_ID
		return 0
	fi
	CODE_FILE="$(rp "$DATA_DIR")/.register-code.$$"
	# The file is made by the account that owns the folder, not by root in it: root creating, chowning or writing a file
	# in a folder the account can change would follow a link the account put there. noclobber makes it a new file (a
	# name that is already taken is refused), the umask keeps it private from the first byte.
	# shellcheck disable=SC2016 # the script is for sh -c to expand
	printf '%s\n' "$TOKEN" | as_service sh -c 'umask 077 && set -C && cat > "$1"' sh "$CODE_FILE" ||
		die "could not write the registration code into $DATA_DIR" "не удалось записать код регистрации в $DATA_DIR"
	# shellcheck disable=SC2086 # $_force is a flag
	as_service env HOME="$SVC_HOME" AGENT_DATA_DIR="$(rp "$DATA_DIR")" AGENT_MACHINE_FILE="$(rp "$MACHINE_FILE")" AGENT_POLICY_FILE="$(rp "$POLICY")" \
		"$(rp "$BIN_DIR")/harnessly-agent" register --hub "$HUB_URL" --code-file "$CODE_FILE" --profile "$PROFILE" --via command \
		${NAME:+--name "$NAME"} $_force || die "the hub did not accept the registration (see above); get a new command in the hub" \
		"хаб не принял регистрацию (см. выше); возьмите в хабе новую команду"
	as_service rm -f "$CODE_FILE"
	CODE_FILE=
	BASE_PATH=$(json_field "$(rp "$MACHINE_FILE")" runtimeBasePath)
	case $BASE_PATH in
	/*) ;;
	*) die "machine.json has no runtimeBasePath" "в machine.json нет runtimeBasePath" ;;
	esac
	case $BASE_PATH in *[!A-Za-z0-9/_.-]*) die "the hub gave an odd base path" "хаб прислал странный базовый путь" ;; esac
	track data "$MACHINE_FILE"
	track data "$DATA_DIR/agent-env"
	track data "$DATA_DIR/agent-link.json"
	track data "$DATA_DIR/agent-audit.log"
}

# agent_status runs `harnessly-agent status` with the folders of this install.
agent_status() {
	run_priv env HOME="$SVC_HOME" AGENT_DATA_DIR="$(rp "$DATA_DIR")" AGENT_MACHINE_FILE="$(rp "$DATA_DIR/machine.json")" \
		AGENT_POLICY_FILE="$(rp "$POLICY")" "$(rp "$BIN_DIR")/harnessly-agent" status
}

wait_linked() {
	step "Waiting for the machine to link to the hub (up to $WAIT s)" "Ждём, пока машина свяжется с хабом (до $WAIT с)"
	if [ "$DRY" = 1 ]; then
		plan "wait until 'harnessly-agent status' says linked"
		return 0
	fi
	_waited=0
	_state=
	while :; do
		if _state=$(agent_status 2>&1); then
			printf '%s\n' "$_state"
			return 0
		fi
		[ "$_waited" -lt "$WAIT" ] || break
		sleep 2
		_waited=$((_waited + 2))
	done
	printf '%s\n' "$_state" >&2
	warn "the machine did not link to the hub in $WAIT s. It is installed; $LOG_HINT" \
		"машина не связалась с хабом за $WAIT с. Она установлена; $LOG_HINT"
	return 1
}

done_message() {
	if [ "$DRY" = 1 ]; then
		say "Dry run finished: nothing was changed." "Пробный запуск закончен: ничего не изменено."
		return 0
	fi
	_sh="sh"
	if [ "$PROFILE" = dir ]; then _sh="sudo sh"; fi
	say "Done: the machine will appear in harnessly." "Готово: машина появится в harnessly."
	say "Control it with: harnessly-agent status | off | on. To remove it: curl -fsSL $HUB_URL/install | $_sh -s -- --uninstall --profile $PROFILE" \
		"Управление: harnessly-agent status | off | on. Удалить: curl -fsSL $HUB_URL/install | $_sh -s -- --uninstall --profile $PROFILE"
}

# --- design tools (optional) --------------------------------------------------------------------------------------
# The design library's checks, pictures and PDF exports drive a headless Chromium; video exports use ffmpeg. Profile dir
# installs them (best effort: a failure is a warning, the machine works without them); the other profiles only say
# what is missing. HARNESSLY_DESIGN_TOOLS=0 skips both. The board engine itself is never installed from here: the
# owner fetches it with their own account (harnessly design engine fetch).

has_chrome() {
	for _c in chromium chromium-browser google-chrome google-chrome-stable; do
		command -v "$_c" >/dev/null 2>&1 && return 0
	done
	[ -d "/Applications/Google Chrome.app" ] || [ -d "/Applications/Chromium.app" ]
}

design_tools() {
	[ "${HARNESSLY_DESIGN_TOOLS:-1}" != 0 ] || return 0
	_want=
	has_chrome || _want="$_want chromium"
	command -v ffmpeg >/dev/null 2>&1 || _want="$_want ffmpeg"
	[ -n "$_want" ] || return 0
	_want=${_want# }
	if [ "$PROFILE" != dir ]; then
		say "For designs (checks, pictures, video exports) install: $_want" \
			"Для макетов (проверки, картинки, экспорт видео) поставьте: $_want"
		return 0
	fi
	say "Installing for designs: $_want (HARNESSLY_DESIGN_TOOLS=0 skips)" "Ставим для макетов: $_want (HARNESSLY_DESIGN_TOOLS=0 — пропустить)"
	if command -v apt-get >/dev/null 2>&1; then
		# shellcheck disable=SC2086 # the names are words
		{ X apt-get update && X env DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends $_want; } ||
			warn "could not install $_want: designs work without it" "не удалось поставить $_want: макеты работают и без этого"
	elif command -v dnf >/dev/null 2>&1; then
		# shellcheck disable=SC2086
		X dnf install -y $_want ||
			warn "could not install $_want: designs work without it" "не удалось поставить $_want: макеты работают и без этого"
	elif command -v pacman >/dev/null 2>&1; then
		# shellcheck disable=SC2086
		X pacman -S --noconfirm --needed $_want ||
			warn "could not install $_want: designs work without it" "не удалось поставить $_want: макеты работают и без этого"
	else
		warn "install $_want for designs: checks, pictures and video exports need them" \
			"для макетов поставьте $_want: без них нет проверок, картинок и экспорта видео"
	fi
}

# --- end of design tools ------------------------------------------------------------------------------------------

# --- profile dir --------------------------------------------------------------------------------------------------

layout_dir() {
	SVC_USER=harnessly
	HOME_DIR=/var/lib/harnessly
	SVC_HOME=$HOME_DIR
	DATA_DIR=$HOME_DIR/.harnessly
	TMUX_DIR=$HOME_DIR/tmux
	WORK_PARENT=/srv/harnessly
	WORK_DIR=$WORK_PARENT/work
	BIN_DIR=/usr/local/bin
	CONF_DIR=/etc/harnessly
	UNIT_DIR=/etc/systemd/system
	MANIFEST=$CONF_DIR/install-manifest
	MANIFEST_MODE=0644
	POLICY=$CONF_DIR/policy.toml
	POLICY_MODE=0644
	PRIV=1
	LOG_HINT=$(pick "see: journalctl -u harnessly-agent -u harnessly-runtime -n 50" "смотрите: journalctl -u harnessly-agent -u harnessly-runtime -n 50")
}

ensure_service_user() {
	if _entry=$(getent passwd "$SVC_USER" 2>/dev/null); then
		_home=$(printf '%s' "$_entry" | cut -d: -f6)
		[ "$_home" = "$HOME_DIR" ] || die "the user $SVC_USER exists with the home $_home, not $HOME_DIR; remove or rename it" \
			"пользователь $SVC_USER уже есть с домом $_home, а не $HOME_DIR; удалите или переименуйте его"
		return 0
	fi
	X useradd --system --user-group --create-home --home-dir "$HOME_DIR" --shell /bin/bash "$SVC_USER"
	# The user and its home are the installer's own from here on; a user that was there before is not.
	track user "$SVC_USER"
	track purge "$HOME_DIR"
}

dir_unit_runtime() {
	cat <<EOF
[Unit]
Description=harnessly runtime: terminals and coding agents of this machine
Documentation=https://github.com/ly-solutions/harnessly/blob/main/docs/hub/machine.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=$SVC_USER
Group=$SVC_USER
EnvironmentFile=$CONF_DIR/runtime.env
WorkingDirectory=$WORK_DIR
ExecStart=$BIN_DIR/harnessly-runtime
# Only the runtime stops with the service: the tmux server and the agents in it keep working through a restart or an
# upgrade, and the next runtime picks them up. For the same reason it keeps the machine's /tmp.
KillMode=process
Restart=on-failure
RestartSec=2
TimeoutStopSec=15
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=$HOME_DIR $WORK_PARENT /tmp /var/tmp
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
EOF
}

dir_unit_agent() {
	cat <<EOF
[Unit]
Description=harnessly agent: the link between this machine and its hub
Documentation=https://github.com/ly-solutions/harnessly/blob/main/docs/hub/machine.md
After=network-online.target harnessly-runtime.service
Wants=network-online.target harnessly-runtime.service

[Service]
Type=simple
User=$SVC_USER
Group=$SVC_USER
EnvironmentFile=$CONF_DIR/agent.env
WorkingDirectory=$HOME_DIR
ExecStart=$BIN_DIR/harnessly-agent
Restart=always
RestartSec=3
TimeoutStopSec=15
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ReadWritePaths=$HOME_DIR $WORK_PARENT
ProtectHome=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectControlGroups=true
RestrictSUIDSGID=true
LockPersonality=true
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
EOF
}

install_dir() {
	layout_dir
	step "Checking the system" "Проверяем систему"
	check_root
	check_systemd
	ensure_tools
	design_tools
	voice_tools
	check_resources "$HOME_DIR"
	download_binaries
	load_manifest
	step "Setting up the user $SVC_USER and its folders" "Готовим пользователя $SVC_USER и его папки"
	ensure_service_user
	mkd "$CONF_DIR" 0755
	mkd "$WORK_PARENT" 0755
	purge_if_made "$WORK_PARENT"
	mkd "$WORK_DIR" 0750 "$SVC_USER:$SVC_USER"
	purge_if_made "$WORK_DIR"
	not_link "$DATA_DIR"
	not_link "$TMUX_DIR"
	mkd "$DATA_DIR" 0700 "$SVC_USER:$SVC_USER"
	purge_if_made "$DATA_DIR"
	mkd "$TMUX_DIR" 0700 "$SVC_USER:$SVC_USER"
	purge_if_made "$TMUX_DIR"
	install_binaries
	write_policy
	save_manifest
	register_machine
	pick_ports
	step "Writing the services" "Пишем службы"
	write_env_files
	mkd "$UNIT_DIR" 0755
	dir_unit_runtime | put "$UNIT_DIR/harnessly-runtime.service" 0644
	dir_unit_agent | put "$UNIT_DIR/harnessly-agent.service" 0644
	track unit harnessly-runtime.service "$UNIT_DIR/harnessly-runtime.service"
	track unit harnessly-agent.service "$UNIT_DIR/harnessly-agent.service"
	save_manifest
	step "Starting the services" "Запускаем службы"
	X systemctl daemon-reload
	X systemctl enable harnessly-runtime.service harnessly-agent.service
	X systemctl restart harnessly-runtime.service
	X systemctl restart harnessly-agent.service
	wait_linked
	done_message
}

# --- profile full -------------------------------------------------------------------------------------------------

layout_full() {
	_home=${HOME:-}
	[ -n "$_home" ] || die "HOME is not set" "не задана переменная HOME"
	case $_home in
	/*) ;;
	*) die "HOME is not an absolute path" "HOME не является абсолютным путём" ;;
	esac
	case $_home in *[!A-Za-z0-9/_.@+-]*) die "profile full does not support a home path with spaces or odd characters: $_home" \
		"профиль full не поддерживает домашний путь с пробелами или странными символами: $_home" ;; esac
	SVC_USER=$(id -un)
	SVC_HOME=$_home
	DATA_DIR=$_home/.harnessly
	TMUX_DIR=
	WORK_DIR=$_home/work
	BIN_DIR=$_home/.local/bin
	CONF_DIR=$_home/.config/harnessly
	MANIFEST=$DATA_DIR/install-manifest
	MANIFEST_MODE=0600
	POLICY=$CONF_DIR/policy.toml
	POLICY_MODE=0600
	PRIV=0
	if [ "$OS" = darwin ]; then
		UNIT_DIR=$_home/Library/LaunchAgents
		LOG_DIR=$_home/Library/Logs/harnessly
		LOG_HINT=$(pick "see: tail -n 50 $LOG_DIR/agent.log $LOG_DIR/runtime.log" "смотрите: tail -n 50 $LOG_DIR/agent.log $LOG_DIR/runtime.log")
	else
		UNIT_DIR=$_home/.config/systemd/user
		LOG_HINT=$(pick "see: journalctl --user -u harnessly-agent -u harnessly-runtime -n 50" "смотрите: journalctl --user -u harnessly-agent -u harnessly-runtime -n 50")
	fi
}

# The user units are the files of deploy/systemd: nothing in them depends on this install.
full_unit_runtime() {
	cat <<'EOF'
[Unit]
Description=harnessly runtime: terminals and coding agents of %u
Documentation=https://github.com/ly-solutions/harnessly/blob/main/docs/hub/machine.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=%h/.config/harnessly/runtime.env
WorkingDirectory=%h
ExecStart=%h/.local/bin/harnessly-runtime
# Only the runtime stops with the service: the tmux server and the agents in it keep working through a restart or an
# upgrade, and the next runtime picks them up.
KillMode=process
Restart=on-failure
RestartSec=2
TimeoutStopSec=15
NoNewPrivileges=true

[Install]
WantedBy=default.target
EOF
}

full_unit_agent() {
	cat <<'EOF'
[Unit]
Description=harnessly agent: the link between the machine of %u and its hub
Documentation=https://github.com/ly-solutions/harnessly/blob/main/docs/hub/machine.md
After=network-online.target harnessly-runtime.service
Wants=network-online.target harnessly-runtime.service

[Service]
Type=simple
EnvironmentFile=%h/.config/harnessly/agent.env
WorkingDirectory=%h
ExecStart=%h/.local/bin/harnessly-agent
Restart=always
RestartSec=3
TimeoutStopSec=15
NoNewPrivileges=true

[Install]
WantedBy=default.target
EOF
}

xml_escape() { sed -e 's/&/\&amp;/g' -e 's/</\&lt;/g' -e 's/>/\&gt;/g'; }

# plist prints a launchd job: plist LABEL PROGRAM LOGNAME, the environment on stdin as KEY=VALUE lines.
plist() {
	printf '<?xml version="1.0" encoding="UTF-8"?>\n'
	printf '<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">\n'
	printf '<plist version="1.0">\n<dict>\n'
	printf '\t<key>Label</key>\n\t<string>%s</string>\n' "$1"
	printf '\t<key>ProgramArguments</key>\n\t<array>\n\t\t<string>%s</string>\n\t</array>\n' "$(printf '%s' "$2" | xml_escape)"
	printf '\t<key>WorkingDirectory</key>\n\t<string>%s</string>\n' "$(printf '%s' "$SVC_HOME" | xml_escape)"
	printf '\t<key>EnvironmentVariables</key>\n\t<dict>\n'
	while IFS='=' read -r _k _v; do
		[ -n "$_k" ] || continue
		printf '\t\t<key>%s</key>\n\t\t<string>%s</string>\n' "$_k" "$(printf '%s' "$_v" | xml_escape)"
	done
	printf '\t</dict>\n'
	printf '\t<key>RunAtLoad</key>\n\t<true/>\n\t<key>KeepAlive</key>\n\t<true/>\n'
	printf '\t<key>ThrottleInterval</key>\n\t<integer>3</integer>\n'
	printf '\t<key>StandardOutPath</key>\n\t<string>%s/%s.log</string>\n' "$(printf '%s' "$LOG_DIR" | xml_escape)" "$3"
	printf '\t<key>StandardErrorPath</key>\n\t<string>%s/%s.log</string>\n' "$(printf '%s' "$LOG_DIR" | xml_escape)" "$3"
	printf '</dict>\n</plist>\n'
}

full_start_linux() {
	if [ "$DRY" = 0 ] && ! systemctl --user show-environment >/dev/null 2>&1; then
		die "systemd --user is not reachable from this shell (no login session?). Log in normally, or try: loginctl enable-linger $SVC_USER" \
			"systemd --user недоступен из этой оболочки (нет сеанса входа?). Войдите обычным образом или выполните: loginctl enable-linger $SVC_USER"
	fi
	if ! loginctl show-user "$SVC_USER" -p Linger 2>/dev/null | grep -q '=yes'; then
		if [ "$DRY" = 1 ]; then
			plan "run: loginctl enable-linger $SVC_USER"
		elif loginctl enable-linger "$SVC_USER" 2>/dev/null; then
			track linger "$SVC_USER"
		else
			warn "could not keep the services running after you log out; ask an administrator to run: loginctl enable-linger $SVC_USER" \
				"не удалось оставить службы работать после выхода; попросите администратора выполнить: loginctl enable-linger $SVC_USER"
		fi
	fi
	X systemctl --user daemon-reload
	X systemctl --user enable harnessly-runtime.service harnessly-agent.service
	X systemctl --user restart harnessly-runtime.service
	X systemctl --user restart harnessly-agent.service
}

full_start_darwin() {
	_uid=$(id -u)
	for _job in runtime agent; do
		_label="io.harnessly.$_job"
		_file="$UNIT_DIR/$_label.plist"
		if [ "$DRY" = 1 ]; then
			plan "run: launchctl bootstrap gui/$_uid $_file"
			continue
		fi
		launchctl bootout "gui/$_uid/$_label" >/dev/null 2>&1 || true
		launchctl bootstrap "gui/$_uid" "$(rp "$_file")" 2>/dev/null || launchctl load -w "$(rp "$_file")"
	done
}

install_full() {
	layout_full
	step "Checking the system" "Проверяем систему"
	check_not_root
	[ "$OS" = darwin ] || check_systemd
	ensure_tools
	design_tools
	voice_tools
	check_resources "$SVC_HOME"
	download_binaries
	load_manifest
	step "Setting up the folders of $SVC_USER" "Готовим папки $SVC_USER"
	mkd "$DATA_DIR" 0700
	purge_if_made "$DATA_DIR"
	mkd "$CONF_DIR" 0700
	mkd "$WORK_DIR" 0755
	purge_if_made "$WORK_DIR"
	install_binaries
	write_policy
	save_manifest
	register_machine
	pick_ports
	step "Writing the services" "Пишем службы"
	write_env_files
	mkd "$UNIT_DIR" 0755
	if [ "$OS" = darwin ]; then
		mkd "$LOG_DIR" 0755
		runtime_env | plist io.harnessly.runtime "$BIN_DIR/harnessly-runtime" runtime | put "$UNIT_DIR/io.harnessly.runtime.plist" 0644
		agent_env | plist io.harnessly.agent "$BIN_DIR/harnessly-agent" agent | put "$UNIT_DIR/io.harnessly.agent.plist" 0644
		track unit io.harnessly.runtime "$UNIT_DIR/io.harnessly.runtime.plist"
		track unit io.harnessly.agent "$UNIT_DIR/io.harnessly.agent.plist"
	else
		full_unit_runtime | put "$UNIT_DIR/harnessly-runtime.service" 0644
		full_unit_agent | put "$UNIT_DIR/harnessly-agent.service" 0644
		track unit harnessly-runtime.service "$UNIT_DIR/harnessly-runtime.service"
		track unit harnessly-agent.service "$UNIT_DIR/harnessly-agent.service"
	fi
	save_manifest
	step "Starting the services" "Запускаем службы"
	if [ "$OS" = darwin ]; then full_start_darwin; else full_start_linux; fi
	save_manifest
	wait_linked
	done_message
}

# --- profile container --------------------------------------------------------------------------------------------

# engine sets ENGINE to the container command this account can use, with sudo in front when it needs it.
find_engine() {
	ENGINE=
	for _e in docker podman; do
		command -v "$_e" >/dev/null 2>&1 || continue
		if "$_e" info >/dev/null 2>&1; then
			ENGINE=$_e
			return 0
		fi
		if command -v sudo >/dev/null 2>&1 && sudo -n "$_e" info >/dev/null 2>&1; then
			ENGINE="sudo -n $_e"
			return 0
		fi
	done
	return 1
}

# eng runs the container command; ENGINE is "docker", "podman" or "sudo -n docker".
eng() {
	# shellcheck disable=SC2086 # the words of ENGINE
	$ENGINE "$@"
}

# engx is eng as a change: a dry run only shows it.
engx() {
	if [ "$DRY" = 1 ]; then
		plan "run: $ENGINE $*"
		return 0
	fi
	eng "$@"
}

layout_container() {
	_home=${HOME:-}
	[ -n "$_home" ] || _home=/root
	DATA_DIR=$_home/.harnessly
	MANIFEST=$DATA_DIR/install-manifest-container
	MANIFEST_MODE=0600
	PRIV=0
	IS_ROOT=0
	LOG_HINT=$(pick "see: $ENGINE logs --tail 50 $CONTAINER" "смотрите: $ENGINE logs --tail 50 $CONTAINER")
}

# hostname_of makes a host name a container accepts out of a display name.
hostname_of() {
	_h=$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | tr -c 'a-z0-9\n-' '-' | sed 's/^-*//; s/-*$//' | cut -c1-60)
	[ -n "$_h" ] || _h=machine
	printf '%s' "$_h"
}

install_container() {
	step "Checking the system" "Проверяем систему"
	if ! find_engine; then
		if [ "$DRY" = 1 ]; then ENGINE=docker; fi
		fail "Docker or Podman is needed, and this account must be able to use it (the docker group, or run as root)" \
			"нужен Docker или Podman, и этот аккаунт должен уметь им пользоваться (группа docker или root)"
	fi
	layout_container
	[ -n "$IMAGE" ] || fail "this hub has no machine image configured: the container profile is off (or give --image)" \
		"в этом хабе не настроен образ машины: профиль container выключен (или укажите --image)"
	_host=$(hostname_of "${NAME:-$(hostname 2>/dev/null || echo machine)}")
	load_manifest
	step "Pulling the image $IMAGE" "Скачиваем образ $IMAGE"
	engx pull "$IMAGE"
	step "Registering this machine with the hub" "Регистрируем машину в хабе"
	if [ "$DRY" = 1 ]; then
		plan "run: $ENGINE run --rm <the volume $VOLUME, the code in a private file> $IMAGE harnessly-agent register --hub $HUB_URL --profile container --via container"
	else
		_force=--reuse
		if [ "$REREGISTER" = 1 ]; then _force=--force; fi
		TMP=$(mktemp -d "${TMPDIR:-/tmp}/harnessly-install.XXXXXX")
		printf '%s\n' "$TOKEN" >"$TMP/code"
		chmod 0644 "$TMP/code" # the folder is private; the container's user must be able to read the file
		# A volume that is there already is not the installer's to remove: only one it makes is recorded for --purge.
		if ! eng volume inspect "$VOLUME" >/dev/null 2>&1; then track volume "$VOLUME"; fi
		eng volume create "$VOLUME" >/dev/null
		# shellcheck disable=SC2086 # _force is a flag
		eng run --rm --hostname "$_host" -v "$VOLUME:/home/agent" -v "$TMP/code:/run/harnessly-register-code:ro,z" \
			--entrypoint harnessly-agent "$IMAGE" register --hub "$HUB_URL" --code-file /run/harnessly-register-code \
			--profile container --via container $_force ${NAME:+--name "$NAME"} ||
			die "the hub did not accept the registration (see above); get a new command in the hub" \
				"хаб не принял регистрацию (см. выше); возьмите в хабе новую команду"
		rm -rf "$TMP"
		TMP=
	fi
	track container "$CONTAINER"
	step "Starting the machine" "Запускаем машину"
	mkd "$DATA_DIR" 0700
	save_manifest
	if [ "$DRY" = 1 ]; then
		plan "run: $ENGINE rm -f $CONTAINER (an older one)"
	else
		eng rm -f "$CONTAINER" >/dev/null 2>&1 || true
	fi
	engx run -d --name "$CONTAINER" --hostname "$_host" --restart unless-stopped -v "$VOLUME:/home/agent" "$IMAGE"
	wait_linked_container
	done_message
}

wait_linked_container() {
	step "Waiting for the machine to link to the hub (up to $WAIT s)" "Ждём, пока машина свяжется с хабом (до $WAIT с)"
	if [ "$DRY" = 1 ]; then
		plan "wait until 'harnessly-agent status' in the container says linked"
		return 0
	fi
	_waited=0
	_state=
	while :; do
		if _state=$(eng exec "$CONTAINER" harnessly-agent status 2>&1); then
			printf '%s\n' "$_state"
			return 0
		fi
		[ "$_waited" -lt "$WAIT" ] || break
		sleep 2
		_waited=$((_waited + 2))
	done
	printf '%s\n' "$_state" >&2
	warn "the machine did not link to the hub in $WAIT s. It is installed; $LOG_HINT" \
		"машина не связалась с хабом за $WAIT с. Она установлена; $LOG_HINT"
	return 1
}

# --- uninstall ----------------------------------------------------------------------------------------------------

# manifest_lines prints the fields of a kind in the manifest, tab separated; MANIFEST_FILE is the manifest.
manifest_lines() {
	awk -F "$TAB" -v k="$1" '$1==k{print $2 "\t" $3}' "$MANIFEST_FILE"
}

remove_path() {
	if safe_path "$1"; then
		X rm -rf "$(rp "$1")"
	else
		warn "not removing $1: it is not a path the installer would make" "не удаляем $1: установщик такой путь не создаёт"
	fi
}

# remove_svc removes a path inside the service account's own folders as that account: what it can swap for a link it can
# only use to remove what it could remove anyway, never a file of root's.
remove_svc() {
	if ! safe_path "$1"; then
		warn "not removing $1: it is not a path the installer would make" "не удаляем $1: установщик такой путь не создаёт"
	elif [ "$DRY" = 1 ]; then
		plan "run as $SVC_USER: rm -rf $1"
	else
		as_service rm -rf -- "$(rp "$1")"
	fi
}

# kill_service_processes kills what the account runs until nothing does; it stops the script when that cannot be shown.
kill_service_processes() {
	if ! command -v pkill >/dev/null 2>&1 || ! command -v pgrep >/dev/null 2>&1; then
		fail "pkill and pgrep are needed to stop the processes of $SVC_USER before removing its files" \
			"нужны pkill и pgrep, чтобы остановить процессы $SVC_USER перед удалением его файлов"
	fi
	_tries=0
	while [ "$_tries" -lt 10 ]; do
		run_priv pkill -KILL -u "$SVC_USER" >/dev/null 2>&1 || true
		# pgrep ends 1 when nothing matches and 2 or more when it could not look: only 1 is "none".
		_rc=0
		run_priv pgrep -u "$SVC_USER" >/dev/null 2>&1 || _rc=$?
		if [ "$_rc" = 1 ]; then return 0; fi
		sleep 1
		_tries=$((_tries + 1))
	done
	fail "processes of $SVC_USER are still running; stop them and run again" "процессы $SVC_USER всё ещё работают; остановите их и запустите снова"
}

# recheck_service_account is run right before root works in the account's folders: whatever started since is ended.
recheck_service_account() {
	[ "$PROFILE" = dir ] || return 0
	id "$SVC_USER" >/dev/null 2>&1 || return 0
	[ "$DRY" = 1 ] && return 0
	kill_service_processes
}

# stop_service_account ends every process of the service account and returns when none is left; it stops the script
# when that cannot be shown. Root looks at the account's folders and then works in them; a process of the account that
# runs in between could swap a folder for a link after the look. So nothing may start one: the units are already
# disabled, and here the account is locked, its cron jobs and its user manager (which starts what the account enabled
# for itself) go, and then what still runs is killed until nothing does.
stop_service_account() {
	id "$SVC_USER" >/dev/null 2>&1 || return 0
	if [ "$DRY" = 1 ]; then
		plan "run: lock $SVC_USER, remove its cron jobs, stop its user manager, pkill -KILL -u $SVC_USER (until none is left)"
		return 0
	fi
	if command -v usermod >/dev/null 2>&1; then
		run_priv usermod -L "$SVC_USER" >/dev/null 2>&1 || true
	elif command -v passwd >/dev/null 2>&1; then
		run_priv passwd -l "$SVC_USER" >/dev/null 2>&1 || true
	fi
	# The account cannot sign in or be started as: expired, no shell, and no at jobs.
	if command -v usermod >/dev/null 2>&1; then run_priv usermod -e 1 -s /usr/sbin/nologin "$SVC_USER" >/dev/null 2>&1 || true; fi
	if command -v chage >/dev/null 2>&1; then run_priv chage -E 0 "$SVC_USER" >/dev/null 2>&1 || true; fi
	if command -v atq >/dev/null 2>&1 && [ -d "$(rp /etc)" ]; then
		# shellcheck disable=SC2016 # the script is for sh -c to expand
		run_priv sh -c 'grep -qx "$1" "$2" 2>/dev/null || printf "%s\n" "$1" >> "$2"' sh "$SVC_USER" "$(rp /etc/at.deny)" >/dev/null 2>&1 || true
	fi
	if command -v crontab >/dev/null 2>&1; then run_priv crontab -r -u "$SVC_USER" >/dev/null 2>&1 || true; fi
	if command -v loginctl >/dev/null 2>&1; then
		run_priv loginctl disable-linger "$SVC_USER" >/dev/null 2>&1 || true
		run_priv loginctl terminate-user "$SVC_USER" >/dev/null 2>&1 || true
	fi
	if command -v systemctl >/dev/null 2>&1; then
		_uid=$(id -u "$SVC_USER" 2>/dev/null || true)
		if [ -n "$_uid" ]; then run_priv systemctl stop "user@$_uid.service" >/dev/null 2>&1 || true; fi
	fi
	kill_service_processes
}

# agent_run runs the installed agent's command with the folders of this install: as the account the services run as,
# or inside the container.
agent_run() {
	if [ "$PROFILE" = container ]; then
		eng exec "$CONTAINER" harnessly-agent "$@"
	else
		as_service env HOME="$SVC_HOME" AGENT_DATA_DIR="$(rp "$DATA_DIR")" AGENT_MACHINE_FILE="$(rp "$DATA_DIR/machine.json")" \
			AGENT_POLICY_FILE="$(rp "$POLICY")" "$(rp "$BIN_DIR")/harnessly-agent" "$@"
	fi
}

# agent_cleanup asks the hub to forget this machine, and takes away the git key and the ssh config block the agent
# made. Both are best effort: a hub that cannot be reached must not keep the machine from being cleaned, so it says
# what is left to do by hand and goes on.
agent_cleanup() {
	step "Removing this machine from the hub" "Убираем машину из списка хаба"
	if [ "$DRY" = 1 ]; then
		plan "run: harnessly-agent unlink (the hub forgets this machine; signed with the machine's own key)"
		if [ "$PROFILE" != container ]; then plan "run: harnessly-agent gitkey-remove (the git key and the ssh config block the agent made)"; fi
		return 0
	fi
	if [ "$PROFILE" != container ] && [ ! -x "$(rp "$BIN_DIR")/harnessly-agent" ]; then
		UNLINKED=0
		warn "harnessly-agent is not installed here, so the hub was not told: remove the machine in the hub's list" \
			"harnessly-agent здесь не установлен, поэтому хаб не уведомлён: удалите машину в списке хаба"
		return 0
	fi
	if _out=$(agent_run unlink 2>&1); then
		say "The hub has forgotten this machine." "Хаб удалил эту машину из списка."
	else
		UNLINKED=0
		warn "could not remove the machine from the hub (${_out:-no answer}). The hub may be down or unreachable from here; the files are removed anyway. Remove the machine in the hub's list, where it shows as offline." \
			"не удалось убрать машину из хаба (${_out:-нет ответа}). Возможно, хаб выключен или недоступен отсюда; файлы всё равно удаляются. Удалите машину в списке хаба, там она будет «не на связи»."
	fi
	if [ "$PROFILE" != container ]; then
		agent_run gitkey-remove >/dev/null 2>&1 || warn "could not remove the git key of the agent from ~/.ssh: see ${SVC_HOME}/.ssh" \
			"не удалось убрать git-ключ агента из ~/.ssh: смотрите ${SVC_HOME}/.ssh"
	fi
}

uninstall_one() {
	MANIFEST_FILE=$1
	_prof=$(awk -F "$TAB" '$1=="profile"{print $2}' "$MANIFEST_FILE")
	case $_prof in dir | full | container) ;; *) die "$MANIFEST_FILE is not an installer manifest" "$MANIFEST_FILE не похож на манифест установщика" ;; esac
	if [ -n "$WANT_PROFILE" ] && [ "$WANT_PROFILE" != "$_prof" ]; then return 0; fi
	PROFILE=$_prof
	step "Removing the $_prof install" "Удаляем установку $_prof"
	case $_prof in
	dir)
		layout_dir
		check_root
		agent_cleanup
		for _u in $(manifest_lines unit | cut -f1); do
			X systemctl disable --now "$_u" || true
			X systemctl kill --kill-who=all "$_u" || true
		done
		# Only then look: nothing of the account runs that could change what is looked at.
		stop_service_account
		not_link "$DATA_DIR"
		not_link "$TMUX_DIR"
		;;
	full)
		layout_full
		check_not_root
		agent_cleanup
		;;
	container)
		if ! find_engine; then
			if [ "$DRY" = 1 ]; then ENGINE=docker; fi
			fail "Docker or Podman is needed to remove the container" "для удаления контейнера нужен Docker или Podman"
		fi
		layout_container
		agent_cleanup
		;;
	esac
	if [ "$_prof" = full ] && [ "$OS" = linux ]; then
		for _u in $(manifest_lines unit | cut -f1); do
			X systemctl --user disable --now "$_u" || true
		done
	fi
	if [ "$_prof" = full ] && [ "$OS" = darwin ]; then
		for _f in $(manifest_lines unit | cut -f2); do
			X launchctl bootout "gui/$(id -u)/$(basename "$_f" .plist)" || true
		done
	fi
	for _c in $(manifest_lines container | cut -f1); do
		engx rm -f "$_c" || true
	done
	recheck_service_account
	manifest_lines unit | while IFS=$TAB read -r _u _f; do remove_path "$_f"; done
	manifest_lines file | while IFS=$TAB read -r _f _x; do remove_path "$_f"; done
	recheck_service_account
	if [ "$_prof" = dir ]; then
		manifest_lines data | while IFS=$TAB read -r _f _x; do remove_svc "$_f"; done
	else
		manifest_lines data | while IFS=$TAB read -r _f _x; do remove_path "$_f"; done
	fi
	if [ "$_prof" = dir ]; then X systemctl daemon-reload || true; fi
	if [ "$_prof" = full ] && [ "$OS" = linux ]; then
		X systemctl --user daemon-reload || true
		for _u in $(manifest_lines linger | cut -f1); do X loginctl disable-linger "$_u" || true; done
	fi
	recheck_service_account
	if [ "$PURGE" = 1 ]; then
		for _u in $(manifest_lines user | cut -f1); do
			X pkill -KILL -u "$_u" || true
			X userdel "$_u" || true
		done
		# Only what the installer made is marked for this: a folder that was there before is left as it is. A manifest of
		# an earlier installer has no such marks it can be trusted with.
		if [ "$(awk -F "$TAB" '$1=="format"{print $2}' "$MANIFEST_FILE")" = 2 ]; then
			manifest_lines purge | while IFS=$TAB read -r _f _x; do remove_path "$_f"; done
		else
			warn "this install was made by an earlier installer, which did not record which folders it made: the home and work folders are kept; remove them by hand if they are only ours" \
				"эта установка сделана прежним установщиком, который не записывал, какие папки создал сам: домашняя и рабочая папки оставлены; удалите их вручную, если там только наше"
		fi
		for _v in $(manifest_lines volume | cut -f1); do engx volume rm "$_v" || true; done
	fi
	recheck_service_account
	_dirs=$(manifest_lines dir | awk '{a[NR]=$0} END{for(i=NR;i>0;i--)print a[i]}')
	X rm -f "$MANIFEST_FILE"
	printf '%s\n' "$_dirs" | while IFS=$TAB read -r _f _x; do
		if [ -n "$_f" ] && safe_path "$_f"; then X rmdir "$(rp "$_f")" 2>/dev/null || true; fi
	done
	if [ "$PURGE" = 0 ]; then
		say "Kept: the work folder and the home of the user (run again with --purge to remove what the installer made)." \
			"Оставлено: рабочая папка и домашняя папка пользователя (для удаления созданного установщиком запустите с --purge)."
	fi
}

do_uninstall() {
	_found=0
	WANT_PROFILE=$PROFILE
	for _m in /etc/harnessly/install-manifest "${HOME:-/nonexistent}/.harnessly/install-manifest" "${HOME:-/nonexistent}/.harnessly/install-manifest-container"; do
		[ -f "$(rp "$_m")" ] || continue
		_found=1
		uninstall_one "$(rp "$_m")"
	done
	if [ "$_found" = 0 ]; then
		say "Nothing to remove: no install manifest on this machine." "Удалять нечего: на этой машине нет манифеста установки."
		return 0
	fi
	if [ "$UNLINKED" = 1 ]; then
		say "Removed, and the hub has forgotten the machine." "Удалено; хаб тоже удалил машину из списка."
	else
		say "Removed. The machine stays in the hub's list until you remove it there." \
			"Удалено. Машина остаётся в списке хаба, пока вы не удалите её там."
	fi
}

# --- main ---------------------------------------------------------------------------------------------------------

on_exit() {
	_rc=$?
	# As the account that made it; a subshell, so that a missing runuser cannot end the cleanup half way.
	if [ -n "${CODE_FILE:-}" ]; then (as_service rm -f "$CODE_FILE") 2>/dev/null || true; fi
	if [ -n "${TMP:-}" ] && [ "$TMP" != /nonexistent ]; then rm -rf "$TMP"; fi
	if [ "$_rc" != 0 ] && [ -n "${STEP:-}" ]; then
		warn "stopped during: $STEP" "остановились на шаге: $STEP"
	fi
}

main() {
	# The script may be piped in: nothing below may read what is left of it.
	exec </dev/null
	set -eu
	TAB=$(printf '\t')
	NL='
'
	ROOT=${HARNESSLY_INSTALL_ROOT:-}
	TMP=
	CODE_FILE=
	STEP=
	MLINES=
	PRIV=0
	IS_ROOT=0
	DRY=0
	PURGE=0
	REREGISTER=0
	MKD_MADE=0
	UNLINKED=1
	UNINSTALL=0
	PROFILE=
	VERSION=
	CONTAINER=harnessly-machine
	VOLUME=harnessly-machine-home
	WANT_PROFILE=
	lang_init
	parse_args "$@"
	trap on_exit EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	detect_platform
	validate_args
	if [ "$DRY" = 1 ]; then say "Dry run: nothing will be changed." "Пробный запуск: ничего не будет изменено."; fi
	if [ "$UNINSTALL" = 1 ]; then
		do_uninstall
		return 0
	fi
	say "Connecting this machine to $HUB_URL (profile $PROFILE)" "Подключаем эту машину к $HUB_URL (профиль $PROFILE)"
	case $PROFILE in
	dir) install_dir ;;
	full) install_full ;;
	container) install_container ;;
	esac
}

main "$@"
