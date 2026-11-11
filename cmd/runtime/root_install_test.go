package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func installerFunctions(t *testing.T, body string, stubs map[string]string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	for name, script := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../../install.sh")
	if err != nil {
		t.Fatal(err)
	}
	policyDir := filepath.Join(root, "apparmor.d")
	if err = os.MkdirAll(filepath.Join(policyDir, "abi"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(policyDir, "abi", "5.0"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.ReplaceAll(string(raw), "/etc/apparmor.d", policyDir))
	raw = []byte(strings.ReplaceAll(string(raw), "/proc/sys/kernel/apparmor_restrict_unprivileged_userns",
		filepath.Join(root, "restricted")))
	source := strings.TrimSuffix(strings.TrimSpace(string(raw)), `main "$@"`)
	script := filepath.Join(root, "install.sh")
	if err = os.WriteFile(script, []byte(source+"\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"), "PATH="+bin+":/usr/bin:/bin", "CALLS="+filepath.Join(root, "calls"), "AGENT_DATA_DIR=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	return string(out), string(calls)
}

func TestRootInstallerBrowserDependencies(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		passwordless, terminal, password bool
	}{
		{"passwordless", true, false, false}, {"no_privilege_no_terminal", false, false, false},
		{"terminal_password", false, true, true}, {"terminal_denied", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sudo := `echo "sudo $*" >> "$CALLS"; if [ "$1" = -n ]; then shift; `
			if tc.passwordless {
				sudo += `exec "$@"; `
			} else {
				sudo += `exit 1; `
			}
			sudo += "fi; "
			if tc.password {
				sudo += `exec "$@"`
			} else {
				sudo += "exit 1"
			}
			terminal := "false"
			if tc.terminal {
				terminal = "true"
			}
			body := `parse_args --no-service
OS=linux
browser_system_chrome() { return 1; }
browser_libraries_present() { return 1; }
browser_has_tty() { ` + terminal + `; }
ensure_browser
echo completed`
			out, calls := installerFunctions(t, body, map[string]string{
				"id": "echo 1000", "sudo": sudo, "apt-get": `echo "apt-get $*" >> "$CALLS"`,
			})
			if !strings.Contains(out, "completed") || !strings.Contains(calls, "sudo -n") {
				t.Fatalf("%s\n%s", out, calls)
			}
			if tc.passwordless || tc.password {
				for _, want := range []string{"apt-get install", "xvfb", "fonts-liberation", "fonts-noto-color-emoji", "libnss3"} {
					if !strings.Contains(calls, want) {
						t.Errorf("missing %s: %s", want, calls)
					}
				}
			} else if !strings.Contains(out, "Run outside harnessly") || !strings.Contains(out, "sudo sh -c") {
				t.Fatalf("no repair command: %s", out)
			}
			if tc.terminal && !strings.Contains(out, "password is needed once") {
				t.Fatal(out)
			}
			if !tc.terminal && strings.Contains(calls, "sudo sh") {
				t.Fatalf("interactive sudo without terminal: %s", calls)
			}
		})
	}
}

func TestRootInstallerDependenciesAllowPassword(t *testing.T) {
	_, calls := installerFunctions(t, `parse_args
OS=linux
have() { [ "$1" != tmux ]; }
ensure_deps`, map[string]string{
		"id": "echo 1000", "sudo": `echo "sudo $*" >> "$CALLS"; [ "$1" != -n ] || exit 1; exec "$@"`,
		"apt-get": `echo "apt-get $*" >> "$CALLS"`,
	})
	if !strings.Contains(calls, "install -y -qq tmux") || strings.Contains(calls, "sudo -n") {
		t.Fatal(calls)
	}
}

func TestRootInstallerBrowserAlreadyInstalled(t *testing.T) {
	for _, native := range []string{"true", "false"} {
		out, calls := installerFunctions(t, `parse_args
OS=linux
browser_system_chrome() { `+native+`; }
browser_libraries_present() { return 0; }
ensure_browser
echo completed`, map[string]string{
			"Xvfb": "exit 0", "sudo": `echo unexpected >> "$CALLS"; exit 1`,
			"apt-get": `echo unexpected >> "$CALLS"; exit 1`,
		})
		if calls != "" || !strings.Contains(out, "completed") {
			t.Fatalf("%s\n%s", out, calls)
		}
	}
	_, calls := installerFunctions(t, "parse_args --no-browser\nOS=linux\nensure_browser", map[string]string{
		"sudo": `echo unexpected >> "$CALLS"; exit 1`, "apt-get": `echo unexpected >> "$CALLS"; exit 1`,
	})
	if calls != "" {
		t.Fatal(calls)
	}
}

func TestRootInstallerAppArmor(t *testing.T) {
	for _, restricted := range []string{"0", "1"} {
		for _, native := range []string{"true", "false"} {
			out, calls := installerFunctions(t, `parse_args
OS=linux
ARCH=amd64
printf `+restricted+` > "$HOME/restricted"
mkdir -p "$(dirname "$ENV_FILE")"
printf 'AGENT_DATA_DIR="%s/custom data"\n' "$HOME" > "$ENV_FILE"
browser_system_chrome() { `+native+`; }
browser_libraries_present() { return 0; }
ensure_browser
ensure_browser
if [ -f "$HOME/apparmor.d/harnessly-browser" ]; then cat "$HOME/apparmor.d/harnessly-browser"; fi`, map[string]string{
				"Xvfb": "exit 0", "id": "echo 1000", "sudo": `echo "sudo $*" >> "$CALLS"; shift; exec "$@"`,
				"apparmor_parser": `echo "parser $*" >> "$CALLS"`,
				"apt-get":         `echo unexpected >> "$CALLS"; exit 1`,
			})
			if restricted == "1" && native == "false" {
				for _, want := range []string{"custom data/agent-browser/chrome/*/chrome-linux64/chrome", "abi <abi/5.0>", "userns,"} {
					if !strings.Contains(out, want) {
						t.Fatalf("missing %s: %s", want, out)
					}
				}
				if strings.Count(calls, "\nparser -r") != 1 || strings.Contains(calls, "unexpected") {
					t.Fatal(calls)
				}
			} else if calls != "" {
				t.Fatal(calls)
			}
		}
	}
}

func TestRootInstallerRemoveAppArmor(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		sudo := `echo "sudo $*" >> "$CALLS"; [ "$1" = -n ] || exit 90; shift; `
		if allowed {
			sudo += `exec "$@"`
		} else {
			sudo += "exit 1"
		}
		out, calls := installerFunctions(t, `parse_args
OS=linux
printf profile > "$HOME/apparmor.d/harnessly-browser"
remove_browser_profile
if [ -f "$HOME/apparmor.d/harnessly-browser" ]; then echo retained; else echo removed; fi`, map[string]string{
			"id": "echo 1000", "sudo": sudo, "apparmor_parser": `echo "parser $*" >> "$CALLS"`,
		})
		want := "retained"
		if allowed {
			want = "removed"
		}
		if !strings.Contains(out, want) || !strings.Contains(calls, "sudo -n") {
			t.Fatalf("%s\n%s", out, calls)
		}
		if !allowed && !strings.Contains(out, "outside harnessly") {
			t.Fatal(out)
		}
	}
}

func TestRootInstallerUnitKeepsBrowserHosts(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args
OS=linux
start_systemd
cat "$UNIT_DIR/harnessly.service"`, map[string]string{"systemctl": "exit 0", "loginctl": "echo yes"})
	for _, want := range []string{"KillMode=process", "browser-host processes survive", "NoNewPrivileges=true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("unit missing %s", want)
		}
	}
}

func TestRootInstallerUninstallValidatesHostPID(t *testing.T) {
	_, calls := installerFunctions(t, `parse_args
mkdir -p "$HOME/profiles/one" "$HOME/profiles/stale"
printf 12345 > "$HOME/profiles/one/host.pid"
printf 99999 > "$HOME/profiles/stale/host.pid"
kill() { echo "kill $*" >> "$CALLS"; }
stop_browser_hosts`, map[string]string{
		"id": "echo 1000",
		"ps": `echo "12345 runtime browser-host --profile $HOME/profiles/one --socket /tmp/one --chrome chrome"
echo "12346 runtime browser-host --profile $HOME/profiles/stale --socket /tmp/two --chrome chrome"
echo "12347 unrelated --profile $HOME/profiles/one"`,
	})
	if strings.TrimSpace(calls) != "kill -TERM 12345" {
		t.Fatalf("unsafe signals: %s", calls)
	}
}

func TestRootInstallerLocalArchive(t *testing.T) {
	out, _ := installerFunctions(t, `mkdir -p "$HOME/release"
for program in runtime agent harnessly; do
 printf '#!/bin/sh\nexit 0\n' > "$HOME/release/$program"
done
tar -czf "$HOME/release.tar.gz" -C "$HOME/release" runtime agent harnessly
parse_args --archive "$HOME/release.tar.gz" --prefix "$HOME/installed" --no-service --no-browser
resolve_version
download_and_verify
install_programs
for program in harnessly-runtime harnessly-agent harnessly; do test -x "$HOME/installed/bin/$program"; done
echo local-archive-installed`, nil)
	if !strings.Contains(out, "local-archive-installed") {
		t.Fatal(out)
	}
}

func TestRootInstallerAppArmorEscapesDataDirectory(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args
OS=linux
AGENT_DATA_DIR="$HOME/custom [data]*"
ARCH=arm64
browser_profile
start_systemd
cat "$UNIT_DIR/harnessly.service"`, map[string]string{"systemctl": "exit 0", "loginctl": "echo yes"})
	if !strings.Contains(out, `custom \[data\]\*/agent-browser/chrome/*/chrome-linux-arm64/chrome`) {
		t.Fatal(out)
	}
	if !strings.Contains(out, `Environment="AGENT_DATA_DIR=`) || !strings.Contains(out, `custom [data]*"`) {
		t.Fatal(out)
	}
}

func TestRootInstallerLibraryDetection(t *testing.T) {
	for _, missing := range []bool{false, true} {
		cache := `for lib in libnss3 libnspr4 libatk-1.0 libatk-bridge-2.0 libatspi libcups libdrm libxkbcommon \
 libX11 libXcomposite libXdamage libXext libXfixes libXrandr libgbm libasound libpango-1.0 libcairo \
 libdbus-1 libxcb libexpat libglib-2.0 libgio-2.0 libgobject-2.0 libudev; do echo "$lib.so.1"; done`
		if missing {
			cache = "echo libnss3.so"
		}
		out, _ := installerFunctions(t, `if browser_libraries_present; then echo complete; else echo missing; fi`, map[string]string{"ldconfig": cache})
		want := "complete"
		if missing {
			want = "missing"
		}
		if !strings.Contains(out, want) {
			t.Fatal(out)
		}
	}
}

func TestRootInstallerPreservesCustomDataDirectory(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args
AGENT_DATA_DIR="$HOME/custom data"
write_env_file
unset AGENT_DATA_DIR
write_env_file
browser_data_dir`, nil)
	if !strings.HasSuffix(strings.TrimSpace(out), "/custom data") {
		t.Fatal(out)
	}
}

func TestRootInstallerAppArmorLoadFailureCanRetry(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args
OS=linux
printf 1 > "$HOME/restricted"
browser_system_chrome() { return 1; }
browser_libraries_present() { return 0; }
browser_has_tty() { return 1; }
ensure_browser
test ! -f "$HOME/apparmor.d/harnessly-browser"
touch "$HOME/allow-profile"
ensure_browser
test -f "$HOME/apparmor.d/harnessly-browser"
echo recovered`, map[string]string{
		"id": "echo 1000", "Xvfb": "exit 0", "sudo": `shift; exec "$@"`,
		"apparmor_parser": `test -f "$HOME/allow-profile"`,
	})
	if !strings.Contains(out, "recovered") {
		t.Fatal(out)
	}
}

// RUNTIME_* given to the installer go into the env file, so every later start of the service reads them, and
// the launchd job, which reads no env file, carries them.
func TestRootInstallerKeepsGivenRuntimeEnv(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args
OS=linux
export RUNTIME_PREVIEW_DOMAIN='first.example.com'
export RUNTIME_GATE_LISTEN=0.0.0.0:1
write_env_file
export RUNTIME_PREVIEW_DOMAIN='preview.example.com'
export RUNTIME_PREVIEW_ID='box-1'
write_env_file
unset RUNTIME_PREVIEW_DOMAIN RUNTIME_PREVIEW_ID RUNTIME_GATE_LISTEN
write_env_file
cat "$ENV_FILE"
echo ---
env_file_runtime
echo ---
start_launchd
cat "$PLIST"`, map[string]string{"launchctl": "exit 0"})
	file, rest, _ := strings.Cut(out, "---\n")
	lines, plist, _ := strings.Cut(rest, "---\n")
	if strings.Count(file, "RUNTIME_PREVIEW_DOMAIN=") != 1 || !strings.Contains(file, `RUNTIME_PREVIEW_DOMAIN="preview.example.com"`) ||
		!strings.Contains(file, `RUNTIME_PREVIEW_ID="box-1"`) || strings.Contains(file, "RUNTIME_GATE_LISTEN") {
		t.Fatalf("env file:\n%s", file)
	}
	if !strings.Contains(lines, "RUNTIME_PREVIEW_DOMAIN=preview.example.com\n") || !strings.Contains(lines, "RUNTIME_PREVIEW_ID=box-1\n") {
		t.Fatalf("env_file_runtime:\n%s", lines)
	}
	if !strings.Contains(plist, "<key>RUNTIME_PREVIEW_DOMAIN</key><string>preview.example.com</string>") {
		t.Fatalf("plist:\n%s", plist)
	}
}

// stableCurl answers the stable pointer's address with the given body (nothing: 404) and the latest release with 0.16.200.
func stableCurl(pointer string) string {
	stable := "exit 22"
	if pointer != "" {
		stable = "printf '%s' '" + pointer + "'; exit 0"
	}
	return `echo "curl $*" >> "$CALLS"
case "$*" in
*stable.json*) ` + stable + ` ;;
*releases/latest*) printf 'https://github.com/ly-solutions/harnessly/releases/tag/v0.16.200'; exit 0 ;;
*latest.txt*) printf 'v0.16.200\n'; exit 0 ;;
esac
exit 22`
}

const installedStub = `
mkdir -p "$BIN"
printf '#!/bin/sh\necho "harnessly %s"\n' "$INSTALLED" > "$BIN/harnessly"
chmod 755 "$BIN/harnessly"
`

func TestRootInstallerChannels(t *testing.T) {
	const pointer = `{"schema": 1, "channel": "stable", "version": "0.16.180", "released_at": "2026-10-20T10:00:00Z"}`
	for _, tc := range []struct {
		name, args, installed, pointer string
		wantVersion, wantChannel       string
		wantOut, wantCall, noCall      string
	}{
		{name: "a new installation takes the stable release", pointer: pointer,
			wantVersion: "0.16.180", wantChannel: "stable", wantCall: "releases/download/channels/stable.json", noCall: "releases/latest"},
		{name: "the latest channel takes every release", args: "--channel latest", pointer: pointer,
			wantVersion: "0.16.200", wantChannel: "latest", wantCall: "releases/latest", noCall: "stable.json"},
		{name: "an older installation moves up to stable", installed: "0.16.100", pointer: pointer,
			wantVersion: "0.16.180", wantChannel: "stable"},
		{name: "an installation ahead of stable stays", installed: "0.16.200", pointer: pointer,
			wantVersion: "0.16.200", wantChannel: "stable", wantOut: "staying on 0.16.200 until a newer one is stable", noCall: "releases/latest"},
		{name: "no pointer: what is installed stays, with the reason", installed: "0.16.100",
			wantVersion: "0.16.100", wantChannel: "stable", wantOut: "no stable release is named yet", noCall: "releases/latest"},
		{name: "no pointer and nothing installed: the latest release, said aloud",
			wantVersion: "0.16.200", wantChannel: "stable", wantOut: "installing the latest release instead", wantCall: "releases/latest"},
		{name: "a mirror keeps its pointer beside latest.txt", args: "--mirror https://mirror.example.org/h", pointer: pointer,
			wantVersion: "0.16.180", wantChannel: "stable", wantCall: "https://mirror.example.org/h/stable.json", noCall: "latest.txt"},
		{name: "a version asked for wins", args: "--version 0.16.150", pointer: pointer,
			wantVersion: "0.16.150", wantChannel: "stable", noCall: "curl"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := "parse_args " + tc.args + "\nOS=linux\nARCH=amd64\n"
			if tc.installed != "" {
				body += "INSTALLED=" + tc.installed + installedStub
			}
			body += "resolve_version\necho \"version=$VERSION channel=$CHANNEL\""
			out, calls := installerFunctions(t, body, map[string]string{"curl": stableCurl(tc.pointer)})
			if want := "version=" + tc.wantVersion + " channel=" + tc.wantChannel; !strings.Contains(out, want) {
				t.Fatalf("want %s:\n%s", want, out)
			}
			if tc.wantOut != "" && !strings.Contains(out, tc.wantOut) {
				t.Fatalf("no %q in:\n%s", tc.wantOut, out)
			}
			if tc.wantCall != "" && !strings.Contains(calls, tc.wantCall) {
				t.Fatalf("no request for %s:\n%s", tc.wantCall, calls)
			}
			if tc.noCall != "" && strings.Contains(calls, tc.noCall) {
				t.Fatalf("a request for %s:\n%s", tc.noCall, calls)
			}
		})
	}
}

// The channel is kept in the service's env file: the runtime's notice reads it there, and so does the next run.
func TestRootInstallerKeepsTheChannel(t *testing.T) {
	out, _ := installerFunctions(t, `parse_args --channel latest
OS=linux
ARCH=amd64
resolve_version
write_env_file
grep -c '^RUNTIME_UPDATE_CHANNEL="latest"$' "$ENV_FILE"
parse_args
resolve_version
echo "again=$CHANNEL"
parse_args --channel stable
resolve_version
write_env_file
grep '^RUNTIME_UPDATE_CHANNEL=' "$ENV_FILE"`, map[string]string{"curl": stableCurl(`{"schema":1,"channel":"stable","version":"0.16.180"}`)})
	for _, want := range []string{"1\n", "again=latest", `RUNTIME_UPDATE_CHANNEL="stable"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, `RUNTIME_UPDATE_CHANNEL="latest"`) {
		t.Fatalf("the earlier channel is still in the env file:\n%s", out)
	}
}

func TestRootInstallerRefusesAnUnknownChannel(t *testing.T) {
	out, _ := installerFunctions(t, `(parse_args --channel nightly) || echo refused`, nil)
	if !strings.Contains(out, "--channel is stable or latest, got: nightly") || !strings.Contains(out, "refused") {
		t.Fatal(out)
	}
}
