#!/bin/sh
# harnessly installer for one machine: the latest release into ~/.local/bin, started as a service of this account,
# listening on 127.0.0.1 only.
#
#   curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- --port 8030
#
# It checks the archive against the release's checksums.txt (and its sigstore signature when cosign is installed),
# installs tmux and git with the system's package manager when they are missing, and runs the runtime as a systemd
# user service on Linux, a launchd agent on macOS, or a background process where neither is available. Nothing is
# opened to the network. Run it again to update; --uninstall removes what it installed and keeps your data.
# To connect machines to a team hub instead, use the installer the hub serves (docs/hub/machine.md).
#
# Nothing runs until main on the last line, so a download that stops half way does nothing.

set -eu

REPO=ly-solutions/harnessly
LABEL=io.github.ly-solutions.harnessly

usage() {
	cat <<'EOF'
harnessly installer

  curl -fsSL https://raw.githubusercontent.com/ly-solutions/harnessly/main/install.sh | sh -s -- [options]

  --version X.Y.Z   install this release (default: the latest)
  --port N          the page's port on 127.0.0.1 (default 8022)
  --prefix DIR      where bin/ goes (default ~/.local)
  --no-service      install the programs only, do not start anything
  --no-deps         do not install tmux and git, only check for them
  --no-browser      skip optional browser libraries, Xvfb and fonts
  --archive FILE    install a locally built release archive (trusted input)
  --uninstall       stop the service and remove the programs; your data in ~/.harnessly stays
  -h, --help        this text
EOF
}

say() { printf '%s\n' "$*"; }
step() { printf '\033[1m==> %s\033[0m\n' "$*"; }
warn() { printf 'harnessly-install: %s\n' "$*" >&2; }
die() {
	warn "$*"
	exit 1
}
have() { command -v "$1" >/dev/null 2>&1; }

parse_args() {
	VERSION=
	PORT=8022
	PREFIX=${HOME}/.local
	SERVICE=1
	DEPS=1
	UNINSTALL=0
	BROWSER=1
	LOCAL_ARCHIVE=
	while [ $# -gt 0 ]; do
		case $1 in
		--*=*)
			_k=${1%%=*}
			_v=${1#*=}
			shift
			set -- "$_k" "$_v" "$@"
			continue
			;;
		--version | --port | --prefix | --archive)
			[ $# -ge 2 ] || die "$1 needs a value"
			case $1 in
			--version) VERSION=${2#v} ;;
			--port) PORT=$2 ;;
			--prefix) PREFIX=$2 ;;
			--archive) LOCAL_ARCHIVE=$2 ;;
			esac
			shift
			;;
		--no-service) SERVICE=0 ;;
		--no-deps) DEPS=0 ;;
		--no-browser) BROWSER=0 ;;
		--uninstall) UNINSTALL=1 ;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			usage >&2
			die "unknown argument: $1"
			;;
		esac
		shift
	done
	case $PORT in '' | *[!0-9]*) die "--port must be a number, got: $PORT" ;; esac
	BIN=$PREFIX/bin
	URL=http://127.0.0.1:$PORT
}

detect_platform() {
	case $(uname -s) in
	Linux) OS=linux ;;
	Darwin) OS=darwin ;;
	*) die "harnessly runs on Linux and macOS; this is $(uname -s). On Windows, use WSL 2." ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) die "there is no release for $(uname -m); build from source: https://github.com/$REPO#from-source" ;;
	esac
}

# as_root runs a command as root: directly when we are root, through sudo otherwise.
as_root() {
	if [ "$(id -u)" = 0 ]; then
		"$@"
	elif have sudo; then
		sudo "$@"
	else
		return 1
	fi
}

install_packages() {
	if [ "$OS" = darwin ]; then
		have brew || return 1
		brew install "$@"
	elif have apt-get; then
		as_root env DEBIAN_FRONTEND=noninteractive apt-get update -qq &&
			as_root env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@" >/dev/null
	elif have dnf; then
		as_root dnf install -y -q "$@" >/dev/null
	elif have yum; then
		as_root yum install -y -q "$@" >/dev/null
	elif have apk; then
		as_root apk add -q "$@"
	elif have pacman; then
		as_root pacman -S --noconfirm --needed "$@"
	elif have zypper; then
		as_root zypper -q install -y "$@"
	else
		return 1
	fi
}

ensure_deps() {
	have curl || die "curl is needed"
	have tar || die "tar is needed"
	missing=
	for c in tmux git; do have "$c" || missing="$missing $c"; done
	[ -n "$missing" ] || return 0
	if [ "$DEPS" = 1 ]; then
		step "Installing$missing"
		# shellcheck disable=SC2086 # the list is words on purpose
		install_packages $missing || die "could not install$missing; install it and run this again"
	else
		die "harnessly needs$missing; install it and run this again"
	fi
}

# Keep browser privilege escalation optional, including when curl pipes the installer into sh.
browser_has_tty() { ( : </dev/tty ) 2>/dev/null; }
browser_noninteractive_root() {
	if [ "$(id -u)" = 0 ]; then "$@"; else have sudo && sudo -n "$@"; fi
}

browser_system_chrome() {
	for browser in google-chrome google-chrome-stable chromium chromium-browser; do
		browser_path=$(command -v "$browser" 2>/dev/null) || continue
		case "$browser_path" in /snap/*) continue ;; esac
		# Ubuntu's transitional package installs a shell wrapper, not a native browser.
		if head -c 4096 "$browser_path" | grep -Eq '/snap/bin/chromium|exec snap run chromium'; then continue; fi
		return 0
	done
	return 1
}

browser_libraries_present() {
	if have ldconfig; then
		library_cache=$(ldconfig -p 2>/dev/null) || return 1
	else
		library_cache=$(/sbin/ldconfig -p 2>/dev/null) || return 1
	fi
	for library in libnss3 libnspr4 libatk-1.0 libatk-bridge-2.0 libatspi libcups libdrm libxkbcommon \
		libX11 libXcomposite libXdamage libXext libXfixes libXrandr libgbm libasound libpango-1.0 libcairo \
		libdbus-1 libxcb libexpat libglib-2.0 libgio-2.0 libgobject-2.0 libudev; do
		printf '%s\n' "$library_cache" | grep -F "$library.so" >/dev/null || return 1
	done
}

browser_data_dir() {
	# Match the runtime default and the environment file used by its service.
	(
		cd "$HOME"
		[ ! -f "$ENV_FILE" ] || . "$ENV_FILE"
		printf '%s\n' "${AGENT_DATA_DIR:-$HOME/.harnessly}"
	)
}

browser_restricted() { [ "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null)" = 1 ]; }
browser_quote() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\"'\\\"'/g")"; }

browser_profile() {
	browser_abi=
	for abi in 5.0 4.0; do
		if [ -f "/etc/apparmor.d/abi/$abi" ]; then browser_abi=$abi; break; fi
	done
	[ -n "$browser_abi" ] || { warn "AppArmor ABI 4.0 or 5.0 is required; update AppArmor outside harnessly."; return 1; }
	data=$(browser_data_dir)
	case "$data" in /*) ;; *) data="$HOME/$data" ;; esac
	if have realpath; then data=$(realpath -m -- "$data") || return 1; fi
	# Quote literal path metacharacters before adding the intentional version wildcard.
	data=$(printf '%s' "$data" | sed 's/[][\\"*?{}]/\\&/g')
	browser_platform=linux64
	[ "${ARCH:-amd64}" != arm64 ] || browser_platform=linux-arm64
	cat <<EOF
# Managed by harnessly's installer; the version wildcard survives Chrome updates.
abi <abi/$browser_abi>,
include <tunables/global>
profile harnessly-browser "$data/agent-browser/chrome/*/chrome-$browser_platform/chrome" flags=(unconfined) {
  userns,
}
EOF
}

ensure_browser() {
	[ "$BROWSER" = 1 ] && [ "$OS" = linux ] || return 0
	browser_native=0
	if browser_system_chrome; then browser_native=1; fi
	browser_script=
	if ! have Xvfb || { [ "$browser_native" = 0 ] && ! browser_libraries_present; }; then
		if have apt-get; then
			suffix=
			distro=$(sed -n 's/^ID=//p' /etc/os-release | tr -d '"')
			release=$(sed -n 's/^VERSION_ID=//p' /etc/os-release | tr -d '"')
			case "$distro:$release" in ubuntu:2[4-9].* | debian:1[3-9]) suffix=t64 ;; esac
			packages="xvfb fonts-liberation fonts-noto-color-emoji libnss3 libnspr4 libatk1.0-0$suffix"
			packages="$packages libatk-bridge2.0-0$suffix libatspi2.0-0$suffix libcups2$suffix libdrm2 libxkbcommon0"
			packages="$packages libxcomposite1 libxdamage1 libxfixes3 libxrandr2 libgbm1 libasound2$suffix"
			packages="$packages libpango-1.0-0 libcairo2 libudev1"
			browser_script="apt-get update -qq && env DEBIAN_FRONTEND=noninteractive apt-get install -y $packages"
		elif have dnf; then
			browser_script="dnf install -y chromium xorg-x11-server-Xvfb liberation-fonts google-noto-color-emoji-fonts"
		elif have apk; then
			browser_script="apk add chromium xvfb font-liberation font-noto-emoji"
		elif have pacman; then
			browser_script="pacman -S --noconfirm --needed chromium xorg-server-xvfb ttf-liberation noto-fonts-emoji"
		else
			warn "Install Chromium, Xvfb and fonts with your package manager; without Xvfb the browser uses headless mode."
		fi
	fi
	if [ "$browser_native" = 0 ] && browser_restricted; then
		if profile=$(browser_profile); then
			if [ "$(cat /etc/apparmor.d/harnessly-browser 2>/dev/null)" != "$profile" ]; then
				[ -z "$browser_script" ] || browser_script="$browser_script && "
				# Publish the file only after loading succeeds, so a failed attempt can be retried.
				browser_script="$browser_script""profile_file=\$(mktemp /etc/apparmor.d/.harnessly-browser.XXXXXX) &&
trap 'rm -f \"\$profile_file\"' EXIT &&
printf '%s\\n' $(browser_quote "$profile") > \"\$profile_file\" &&
chmod 644 \"\$profile_file\" && apparmor_parser -r \"\$profile_file\" &&
mv \"\$profile_file\" /etc/apparmor.d/harnessly-browser"
			fi
		fi
	fi
	[ -n "$browser_script" ] || return 0
	step "Preparing browser libraries, Xvfb, fonts and sandbox access where needed"
	if ! browser_noninteractive_root sh -c "$browser_script"; then
		if browser_has_tty; then
			say "    These dependencies let Chrome render pages safely. Administrator password is needed once."
			if as_root sh -c "$browser_script"; then return 0; fi
		fi
		warn "Browser setup is incomplete. Run outside harnessly (normal SSH or local terminal):"
		warn "Processes in harnessly use NoNewPrivileges and cannot use sudo."
		warn "sudo sh -c $(browser_quote "$browser_script")"
	fi
	return 0
}

remove_browser_profile() {
	[ "$OS" = linux ] && [ -f /etc/apparmor.d/harnessly-browser ] || return 0
	remove_command='apparmor_parser -R /etc/apparmor.d/harnessly-browser && rm -f /etc/apparmor.d/harnessly-browser'
	if ! browser_noninteractive_root sh -c "$remove_command"; then
		warn "Remove the browser profile outside harnessly (normal SSH or local terminal):"
		warn "sudo sh -c $(browser_quote "$remove_command")"
	fi
}

# Verify both ownership and the command before trusting a stale host.pid.
stop_browser_hosts() {
	# Enumerate this user's hosts so custom data roots also stop, without trusting unrelated PID files.
	ps -u "$(id -u)" -o pid= -o args= | while read -r host_pid command; do
		case "$command" in *" browser-host --profile "*" --socket "*) ;; *) continue ;; esac
		profile=${command#* browser-host --profile }
		profile=${profile% --socket *}
		pid_file=$profile/host.pid
		[ -f "$pid_file" ] || continue
		recorded=$(cat "$pid_file")
		[ "$recorded" = "$host_pid" ] || continue
		case $host_pid in '' | *[!0-9]*) continue ;; esac
		[ "$host_pid" -gt 1 ] || continue
		kill -TERM "$host_pid" 2>/dev/null || true
	done
}

resolve_version() {
	if [ -n "$LOCAL_ARCHIVE" ]; then VERSION=${VERSION:-local}; return 0; fi
	[ -z "$VERSION" ] || return 0
	# The /releases/latest page redirects to the tag; this needs no API token and no rate limit.
	tag_url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest") ||
		die "could not reach github.com"
	VERSION=${tag_url##*/v}
	case $VERSION in '' | *[!0-9.]*) die "could not read the latest version from $tag_url" ;; esac
}

sha256_of() {
	if have sha256sum; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}

download_and_verify() {
	if [ -n "$LOCAL_ARCHIVE" ]; then
		TMP=$(mktemp -d)
		trap 'rm -rf "$TMP"' EXIT
		tar -xzf "$LOCAL_ARCHIVE" -C "$TMP" || die "could not unpack local archive"
		return 0
	fi
	ARCHIVE=harnessly_${VERSION}_${OS}_${ARCH}.tar.gz
	base=https://github.com/$REPO/releases/download/v$VERSION
	TMP=$(mktemp -d)
	trap 'rm -rf "$TMP"' EXIT
	step "Downloading harnessly $VERSION for $OS/$ARCH"
	(cd "$TMP" && curl -fsSL --retry 3 -O "$base/$ARCHIVE" -O "$base/checksums.txt" -O "$base/checksums.txt.sigstore.json") ||
		die "could not download $base/$ARCHIVE"
	if have cosign; then
		cosign verify-blob "$TMP/checksums.txt" --bundle "$TMP/checksums.txt.sigstore.json" \
			--certificate-identity "https://github.com/$REPO/.github/workflows/release.yml@refs/tags/v$VERSION" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com >/dev/null 2>&1 ||
			die "the signature of checksums.txt does not verify"
		say "    signature of checksums.txt: ok (cosign)"
	fi
	want=$(awk -v f="$ARCHIVE" '$2 == f { print $1 }' "$TMP/checksums.txt")
	[ -n "$want" ] || die "$ARCHIVE is not in checksums.txt"
	[ "$(sha256_of "$TMP/$ARCHIVE")" = "$want" ] || die "$ARCHIVE does not match its checksum"
	say "    checksum: ok"
	tar -xzf "$TMP/$ARCHIVE" -C "$TMP"
}

install_programs() {
	step "Installing into $BIN"
	mkdir -p "$BIN"
	# A running runtime keeps its file open; replacing it by rename leaves the running one alone until the restart.
	for pair in runtime:harnessly-runtime agent:harnessly-agent harnessly:harnessly; do
		src=${pair%%:*}
		dst=${pair#*:}
		cp "$TMP/$src" "$BIN/.$dst.new"
		chmod 755 "$BIN/.$dst.new"
		mv -f "$BIN/.$dst.new" "$BIN/$dst"
	done
	say "    harnessly-runtime, harnessly-agent, harnessly"
}

# --- the service -----------------------------------------------------------------------------------------------

UNIT_DIR=${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user
ENV_FILE=${XDG_CONFIG_HOME:-$HOME/.config}/harnessly/env
PLIST=$HOME/Library/LaunchAgents/$LABEL.plist
STATE_DIR=${XDG_STATE_HOME:-$HOME/.local/state}/harnessly
PID_FILE=$STATE_DIR/runtime.pid
LOG_FILE=$STATE_DIR/runtime.log

user_systemd_works() {
	[ "$OS" = linux ] && have systemctl && systemctl --user show-environment >/dev/null 2>&1
}

write_env_file() {
	mkdir -p "$(dirname "$ENV_FILE")"
	umask 077
	if [ ! -f "$ENV_FILE" ]; then
		cat >"$ENV_FILE" <<'EOF'
# Environment of the harnessly service; every variable is in docs/configuration.md.
# The page listens on 127.0.0.1 only. To reach it from another computer, use an SSH tunnel or put a TLS proxy in front
# and set INIT_PASSWORD first (docs/deploy.md, "The first visit").
# RUNTIME_WORKSPACE_DIR=
# INIT_PASSWORD=
EOF
	fi
	# Preserve an explicitly supplied data root across later installer runs from another shell.
	if [ -n "${AGENT_DATA_DIR:-}" ] && ! grep -Eq '^[[:space:]]*(export[[:space:]]+)?AGENT_DATA_DIR=' "$ENV_FILE"; then
		data_value=$(printf '%s' "$AGENT_DATA_DIR" | sed 's/[\\"$`]/\\&/g')
		printf '\nAGENT_DATA_DIR="%s"\n' "$data_value" >>"$ENV_FILE"
	fi
	umask 022
}

systemd_quote() { printf '%s' "$1" | sed 's/[\\"]/\\&/g; s/%/%%/g'; }

start_systemd() {
	mkdir -p "$UNIT_DIR"
	cat >"$UNIT_DIR/harnessly.service" <<EOF
# Written by harnessly's install.sh; run it again to update. Your settings go to $ENV_FILE.
[Unit]
Description=harnessly: terminals and coding agents of %u
Documentation=https://github.com/$REPO/blob/main/docs/deploy.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=RUNTIME_GATE_LISTEN=127.0.0.1:$PORT
Environment=RUNTIME_GATE_UPSTREAM=
Environment=RUNTIME_APP_BASE_PATH=/
Environment="AGENT_DATA_DIR=$(systemd_quote "$(browser_data_dir)")"
Environment=PATH=$BIN:$PATH
EnvironmentFile=-"$(systemd_quote "$ENV_FILE")"
WorkingDirectory=%h
ExecStart=$BIN/harnessly-runtime
# Only the runtime stops: tmux, agents and browser-host processes survive a restart or update.
KillMode=process
Restart=on-failure
RestartSec=2
TimeoutStopSec=15
NoNewPrivileges=true

[Install]
WantedBy=default.target
EOF
	systemctl --user daemon-reload
	systemctl --user enable harnessly >/dev/null 2>&1
	systemctl --user restart harnessly
	# Keep it running after you log out (agents work while the laptop is closed). It needs polkit's yes on some systems.
	if have loginctl && [ "$(loginctl show-user "$(id -un)" -P Linger 2>/dev/null)" != yes ]; then
		loginctl enable-linger "$(id -un)" 2>/dev/null ||
			warn "could not keep the service running after logout; run: sudo loginctl enable-linger $(id -un)"
	fi
	HOW="systemd user service   systemctl --user status harnessly · journalctl --user -u harnessly -f"
}

start_launchd() {
	mkdir -p "$(dirname "$PLIST")" "$STATE_DIR"
	cat >"$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>$LABEL</string>
  <key>ProgramArguments</key><array><string>$BIN/harnessly-runtime</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>RUNTIME_GATE_LISTEN</key><string>127.0.0.1:$PORT</string>
    <key>RUNTIME_GATE_UPSTREAM</key><string></string>
    <key>RUNTIME_APP_BASE_PATH</key><string>/</string>
    <key>PATH</key><string>$BIN:/opt/homebrew/bin:/usr/local/bin:$PATH</string>
  </dict>
  <key>WorkingDirectory</key><string>$HOME</string>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
  <key>AbandonProcessGroup</key><true/>
  <key>StandardOutPath</key><string>$LOG_FILE</string>
  <key>StandardErrorPath</key><string>$LOG_FILE</string>
</dict>
</plist>
EOF
	launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
	launchctl bootstrap "gui/$(id -u)" "$PLIST"
	HOW="launchd agent   launchctl print gui/$(id -u)/$LABEL · log: $LOG_FILE"
}

stop_background() {
	if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
		kill "$(cat "$PID_FILE")" 2>/dev/null || true
		sleep 1
	fi
	rm -f "$PID_FILE"
}

start_background() {
	mkdir -p "$STATE_DIR"
	stop_background
	# No service manager here (a container, WSL without systemd): a background process, which does not survive a reboot.
	(
		cd "$HOME"
		# shellcheck disable=SC1090
		if [ -f "$ENV_FILE" ]; then set -a && . "$ENV_FILE" && set +a; fi
		RUNTIME_GATE_LISTEN=127.0.0.1:$PORT RUNTIME_GATE_UPSTREAM='' RUNTIME_APP_BASE_PATH=/ PATH=$BIN:$PATH \
			nohup "$BIN/harnessly-runtime" >>"$LOG_FILE" 2>&1 &
		echo $! >"$PID_FILE"
	)
	HOW="background process (no systemd here; it stops on reboot)   log: $LOG_FILE · run this installer again to restart it"
}

start_service() {
	write_env_file
	step "Starting harnessly"
	if [ "$OS" = darwin ]; then
		start_launchd
	elif user_systemd_works; then
		start_systemd
	else
		start_background
	fi
}

wait_healthy() {
	i=0
	while [ $i -lt 30 ]; do
		curl -fsS -o /dev/null "$URL/api/health" 2>/dev/null && return 0
		sleep 1
		i=$((i + 1))
	done
	return 1
}

# --- the end ---------------------------------------------------------------------------------------------------

agents_hint() {
	n=
	have claude && n="$n Claude Code"
	have codex && n="$n${n:+,} Codex"
	if [ -n "$n" ]; then
		say "  Agents found:$n. Log in to them once in a terminal tab of the page if you have not yet."
	else
		say "  No agent CLI found. Install one, then log in from a terminal tab of the page:"
		say "    curl -fsSL https://claude.ai/install.sh | bash     # Claude Code"
		say "    npm install -g @openai/codex                       # Codex"
	fi
}

finish() {
	say ""
	if [ "$SERVICE" = 0 ]; then
		say "harnessly $VERSION is installed in $BIN. Start it with:"
		say "  RUNTIME_GATE_LISTEN=127.0.0.1:$PORT RUNTIME_GATE_UPSTREAM= RUNTIME_APP_BASE_PATH=/ $BIN/harnessly-runtime"
		return 0
	fi
	if ! wait_healthy; then
		warn "harnessly did not answer on $URL within 30 seconds"
		case $HOW in
		systemd*) warn "see: journalctl --user -u harnessly -n 50" ;;
		*) warn "see: $LOG_FILE" ;;
		esac
		exit 1
	fi
	printf '\033[1mharnessly %s is running: %s\033[0m\n' "$VERSION" "$URL"
	if [ -n "${SSH_CONNECTION:-}" ]; then
		host=$(printf '%s' "$SSH_CONNECTION" | awk '{ print $3 }')
		say "  This is a remote machine and the page is on its loopback only. On your computer, run"
		say "    ssh -N -L $PORT:127.0.0.1:$PORT $(id -un)@$host"
		say "  and open $URL there."
	else
		say "  Open it and choose your password."
	fi
	agents_hint
	say "  Runs as: $HOW"
	say "  Update: run this installer again. Remove: add --uninstall."
	case ":$PATH:" in *":$BIN:"*) ;; *) say "  The harnessly command is in $BIN; add it to your PATH." ;; esac
}

uninstall() {
	step "Removing harnessly"
	if [ "$OS" = darwin ]; then
		launchctl bootout "gui/$(id -u)/$LABEL" >/dev/null 2>&1 || true
		rm -f "$PLIST"
	elif user_systemd_works && [ -f "$UNIT_DIR/harnessly.service" ]; then
		systemctl --user disable --now harnessly >/dev/null 2>&1 || true
		rm -f "$UNIT_DIR/harnessly.service"
		systemctl --user daemon-reload
	fi
	stop_background
	stop_browser_hosts
	remove_browser_profile
	rm -f "$BIN/harnessly-runtime" "$BIN/harnessly-agent" "$BIN/harnessly"
	say "Removed the programs and the service. Kept: your data (~/.harnessly), settings ($ENV_FILE) and tmux sessions."
}

main() {
	parse_args "$@"
	detect_platform
	if [ "$UNINSTALL" = 1 ]; then
		uninstall
		return 0
	fi
	ensure_deps
	ensure_browser
	resolve_version
	download_and_verify
	install_programs
	[ "$SERVICE" = 0 ] || start_service
	finish
}

main "$@"
