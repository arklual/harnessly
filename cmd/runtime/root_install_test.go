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
	source := strings.TrimSuffix(strings.TrimSpace(string(raw)), `main "$@"`)
	script := filepath.Join(root, "install.sh")
	if err = os.WriteFile(script, []byte(source+"\n"+body), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", script)
	cmd.Env = append(os.Environ(), "HOME="+root, "XDG_CONFIG_HOME="+filepath.Join(root, "config"),
		"XDG_STATE_HOME="+filepath.Join(root, "state"), "PATH="+bin+":/usr/bin:/bin", "CALLS="+filepath.Join(root, "calls"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer: %v\n%s", err, out)
	}
	calls, _ := os.ReadFile(filepath.Join(root, "calls"))
	return string(out), string(calls)
}

func TestRootInstallerBrowserDependencies(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "sudo_available", false: "no_privilege"}[allowed], func(t *testing.T) {
			sudo := `echo "sudo $*" >> "$CALLS"; [ "$1" = -n ] || exit 90; shift; `
			if allowed {
				sudo += `exec "$@"`
			} else {
				sudo += `exit 1`
			}
			out, calls := installerFunctions(t, "parse_args --no-service\nOS=linux\nensure_browser\necho completed", map[string]string{
				"id": "echo 1000", "sudo": sudo, "apt-get": `echo "apt-get $*" >> "$CALLS"`,
			})
			if !strings.Contains(out, "completed") || !strings.Contains(calls, "sudo -n") {
				t.Fatalf("%s\n%s", out, calls)
			}
			if allowed {
				for _, want := range []string{"xvfb", "fonts-liberation", "fonts-noto-color-emoji", "libnss3"} {
					if !strings.Contains(calls, want) {
						t.Errorf("missing %s: %s", want, calls)
					}
				}
			} else if !strings.Contains(out, "Run: sudo apt-get install") {
				t.Fatalf("no repair command: %s", out)
			}
		})
	}
	_, calls := installerFunctions(t, "parse_args --no-browser\nOS=linux\nensure_browser", map[string]string{
		"sudo": `echo unexpected >> "$CALLS"; exit 1`, "apt-get": `echo unexpected >> "$CALLS"; exit 1`,
	})
	if calls != "" {
		t.Fatalf("--no-browser invoked packages: %s", calls)
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
