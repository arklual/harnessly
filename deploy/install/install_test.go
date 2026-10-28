package install

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// The installer is run for real in a folder of its own: HARNESSLY_INSTALL_ROOT puts every path it writes under a temp
// folder, and the commands that would change the host (useradd, systemctl, docker…) are stubs that write down how they
// were called. The PATH holds the stubs and links to the plain tools only, so nothing here can reach the host's own.

const machineID = "abcdefghijklmnopqrst"

// plainTools are the real commands the installer may use.
var plainTools = []string{"awk", "basename", "cat", "chmod", "cp", "curl", "cut", "date", "df", "dirname", "env", "grep", "head",
	"hostname", "ls", "mkdir", "mktemp", "mv", "rm", "rmdir", "sed", "sh", "sleep", "sort", "tail", "tee", "tr", "wc", "sha256sum", "shasum"}

const logLine = `echo "$(basename "$0") $*" >> "$FAKE_CALLS"`

// stubs maps a command to its script; most only write down the call.
var stubs = map[string]string{
	"useradd": logLine, "userdel": logLine, "chown": logLine, "pkill": logLine, "pgrep": `exit 1`, "usermod": logLine, "crontab": logLine, "launchctl": logLine,
	"apt-get": logLine, "dnf": logLine,
	"systemctl": logLine,
	"loginctl": `if [ "$1" = show-user ]; then echo "Linger=${FAKE_LINGER:-no}"; exit 0; fi
` + logLine,
	"tmux": `exit 0`,
	"git":  `exit 0`,
	"getent": `if [ -n "$FAKE_USER_HOME" ]; then echo "harnessly:x:999:999::$FAKE_USER_HOME:/bin/bash"; exit 0; fi
exit 2`,
	"id": `if [ "$1" = -u ]; then echo "${FAKE_UID:-0}"; exit 0; fi
if [ "$1" = harnessly ] && [ -n "$FAKE_SVC_USER" ]; then exit 0; fi
exec "$REAL_ID" "$@"`,
	"uname": `if [ -n "$FAKE_UNAME_S" ]; then
	case "$1" in -s) echo "$FAKE_UNAME_S"; exit 0 ;; -m) echo "$FAKE_UNAME_M"; exit 0 ;; esac
fi
exec "$REAL_UNAME" "$@"`,
	"ss": `echo "State Recv-Q Send-Q Local Address:Port Peer Address:Port"
for a in $FAKE_LISTEN; do echo "LISTEN 0 4096 $a 0.0.0.0:*"; done`,
	"runuser": logLine + `
while [ "$1" != -- ]; do shift; done
shift
exec "$@"`,
	"sudo": `[ "$*" = "-n true" ] || ` + logLine + `
[ -z "$FAKE_NO_SUDO" ] || exit 1
[ "$1" = -n ] && shift
if [ "$1" = -u ]; then shift 2; fi
[ "$1" = true ] && exit 0
exec "$@"`,
	"docker": `case "$1" in
info) [ -z "$FAKE_NO_ENGINE" ] || exit 1; exit 0 ;;
volume) if [ "$2" = inspect ]; then [ -n "$FAKE_VOLUME_EXISTS" ] && exit 0; exit 1; fi ;;
exec) if [ "$4" = unlink ]; then echo "agent unlink (container)" >> "$FAKE_CALLS"; [ -z "$FAKE_UNLINK_FAIL" ] || { echo "could not reach the hub" >&2; exit 1; }; exit 0; fi
	[ ! -f "$FAKE_DIR/unlinked" ] || exit 1; echo "linked to https://hub.example.com as abcdefghijklmnopqrst"; exit 0 ;;
esac
` + logLine + `
for a in "$@"; do
	case "$a" in *:/run/harnessly-register-code:ro,z) cat "${a%%:*}" >> "$FAKE_CODE_LOG" ;; esac
done
exit 0`,
}

// fakeAgent answers register by writing machine.json, as the real one does, and status by the state it is told.
const fakeAgent = `#!/bin/sh
case "$1" in
register)
	echo "agent $*" >> "$FAKE_CALLS"
	echo "env HOME=$HOME DATA=$AGENT_DATA_DIR POLICY=$AGENT_POLICY_FILE" >> "$FAKE_CALLS"
	while [ $# -gt 0 ]; do
		[ "$1" = --code-file ] && cat "$2" >> "$FAKE_CODE_LOG"
		shift
	done
	mkdir -p "$(dirname "$AGENT_MACHINE_FILE")"
	printf '{\n  "machineId": "abcdefghijklmnopqrst",\n  "hubUrl": "https://hub.example.com",\n  "runtimeBasePath": "/m/abcdefghijklmnopqrst"\n}\n' > "$AGENT_MACHINE_FILE"
	;;
unlink)
	echo "agent $*" >> "$FAKE_CALLS"
	[ -z "$FAKE_UNLINK_FAIL" ] || { echo "harnessly-agent unlink: could not reach the hub" >&2; exit 1; }
	;;
gitkey-remove)
	echo "agent $* HOME=$HOME" >> "$FAKE_CALLS"
	;;
status)
	[ ! -f "$FAKE_DIR/unlinked" ] || { echo "registered, but the agent is not running"; exit 1; }
	echo "linked to https://hub.example.com as abcdefghijklmnopqrst"
	;;
esac
`

type env struct {
	t     *testing.T
	dir   string // everything of the test
	root  string // HARNESSLY_INSTALL_ROOT
	home  string
	stubs string
	calls string
	hub   *httptest.Server
	bad   bool // the hub's checksum for the agent is wrong
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("a shell script")
	}
	e := &env{t: t, dir: t.TempDir()}
	e.root = filepath.Join(e.dir, "root")
	e.home = filepath.Join(e.dir, "home")
	e.stubs = filepath.Join(e.dir, "bin")
	e.calls = filepath.Join(e.dir, "calls.log")
	for _, d := range []string{e.root, e.home, e.stubs, filepath.Join(e.root, "run", "systemd", "system")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tool := range plainTools {
		if p, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(p, filepath.Join(e.stubs, tool)); err != nil {
				t.Fatal(err)
			}
		} else if tool == "curl" {
			t.Skip("no curl")
		}
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(e.stubs, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.hub = httptest.NewServer(http.HandlerFunc(e.serve))
	t.Cleanup(e.hub.Close)
	return e
}

// files the hub serves: a fake agent and a fake runtime for every system.
func (e *env) serve(w http.ResponseWriter, r *http.Request) {
	files := map[string][]byte{}
	for _, sys := range []string{"linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"} {
		files["agent-"+sys] = []byte(fakeAgent)
		files["runtime-"+sys] = []byte("#!/bin/sh\nexit 0\n")
	}
	switch name := strings.TrimPrefix(r.URL.Path, "/dl/"); {
	case name == "manifest.json":
		m := struct {
			Version string                    `json:"version"`
			Files   map[string]map[string]any `json:"files"`
		}{"9.9.9", map[string]map[string]any{}}
		for n, b := range files {
			m.Files[n] = map[string]any{"sha256": sum(b), "size": len(b)}
			if e.bad && strings.HasPrefix(n, "agent-") {
				m.Files[n]["sha256"] = strings.Repeat("0", 64)
			}
		}
		_ = json.NewEncoder(w).Encode(m)
	case files[name] != nil:
		_, _ = w.Write(files[name])
	default:
		http.NotFound(w, r)
	}
}

type result struct {
	out  string
	err  string
	code int
}

// run runs the installer as `sh install.sh args…` with extra environment.
func (e *env) run(extra []string, args ...string) result {
	e.t.Helper()
	return e.runScript(Script, extra, args...)
}

func (e *env) runScript(script []byte, extra []string, args ...string) result {
	e.t.Helper()
	path := filepath.Join(e.dir, "install.sh")
	if err := os.WriteFile(path, script, 0o644); err != nil {
		e.t.Fatal(err)
	}
	realID, _ := exec.LookPath("id")
	realUname, _ := exec.LookPath("uname")
	cmd := exec.Command("/bin/sh", append([]string{path}, args...)...)
	cmd.Env = append([]string{
		"PATH=" + e.stubs, "HOME=" + e.home, "TMPDIR=" + e.dir, "LANG=C", "HARNESSLY_INSTALL_ROOT=" + e.root,
		"HARNESSLY_HUB_URL=" + e.hub.URL, "FAKE_CALLS=" + e.calls, "FAKE_CODE_LOG=" + filepath.Join(e.dir, "code.log"),
		"FAKE_DIR=" + e.dir, "REAL_ID=" + realID, "REAL_UNAME=" + realUname,
	}, extra...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	res := result{}
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			e.t.Fatal(err)
		}
		res.code = ee.ExitCode()
	}
	res.out, res.err = out.String(), errb.String()
	return res
}

func (e *env) calledWith() string {
	raw, _ := os.ReadFile(e.calls)
	return string(raw)
}

func (e *env) path(p string) string { return filepath.Join(e.root, p) }

func (e *env) read(p string) string {
	e.t.Helper()
	raw, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(raw)
}

func (e *env) mode(p string) os.FileMode {
	e.t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		e.t.Fatal(err)
	}
	return st.Mode().Perm()
}

func (e *env) exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func mustContain(t *testing.T, what, text string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(text, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, text)
		}
	}
}

func mustNotContain(t *testing.T, what, text string, nots ...string) {
	t.Helper()
	for _, n := range nots {
		if strings.Contains(text, n) {
			t.Errorf("%s has %q:\n%s", what, n, text)
		}
	}
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestInstallDirProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run(nil, "--token", "hx_secretcode42", "--profile", "dir", "--name", "Ada box", "--wait", "4")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "output", r.out, "Done: the machine will appear in harnessly.", "linked to https://hub.example.com as "+machineID, "| sudo sh -s -- --uninstall --profile dir")
	// One message, in one language.
	if n := strings.Count(r.out, "machine will appear in harnessly") + strings.Count(r.out, "машина появится в harnessly"); n != 1 {
		t.Errorf("the done message printed %d times:\n%s", n, r.out)
	}

	// The programs, owned by root and executable; the policy and the services.
	for _, b := range []string{"harnessly-agent", "harnessly-runtime"} {
		if m := e.mode(e.path("/usr/local/bin/" + b)); m != 0o755 {
			t.Errorf("%s is %v", b, m)
		}
	}
	if m := e.mode(e.path("/etc/harnessly/policy.toml")); m != 0o644 {
		t.Errorf("policy %v", m)
	}
	pol, err := agent.ParsePolicy(e.read(e.path("/etc/harnessly/policy.toml")), "file")
	if err != nil || !pol.Terminal || !pol.AgentLogin || !pol.Keys || !pol.Env || !pol.Install || !pol.RuntimeSession || len(pol.DenyRoutes) != 0 {
		t.Errorf("the default policy: %+v %v", pol, err)
	}

	rt := e.read(e.path("/etc/harnessly/runtime.env"))
	mustContain(t, "runtime.env", rt, "RUNTIME_GATE_LISTEN=127.0.0.1:18022\n", "RUNTIME_GATE_UPSTREAM=\n", "RUNTIME_APP_BASE_PATH=/m/"+machineID+"\n",
		"RUNTIME_HUB_URL="+e.hub.URL+"\n", "RUNTIME_PREVIEW_LISTEN=off\n", "RUNTIME_WORKSPACE_DIR=/srv/harnessly/work\n",
		"AGENT_DATA_DIR=/var/lib/harnessly/.harnessly\n", "AGENT_POLICY_FILE=/etc/harnessly/policy.toml\n", "TMUX_TMPDIR=/var/lib/harnessly/tmux\n")
	ag := e.read(e.path("/etc/harnessly/agent.env"))
	mustContain(t, "agent.env", ag, "AGENT_PORT=18023\n", "RUNTIME_PORT=18022\n", "AGENT_DATA_DIR=/var/lib/harnessly/.harnessly\n",
		"AGENT_POLICY_FILE=/etc/harnessly/policy.toml\n", "AGENT_WORK_DIR=/srv/harnessly/work\n")
	unitRt := e.read(e.path("/etc/systemd/system/harnessly-runtime.service"))
	unitAg := e.read(e.path("/etc/systemd/system/harnessly-agent.service"))
	mustContain(t, "runtime unit", unitRt, "User=harnessly\n", "EnvironmentFile=/etc/harnessly/runtime.env\n", "ExecStart=/usr/local/bin/harnessly-runtime\n",
		"KillMode=process\n", "NoNewPrivileges=true\n", "ProtectSystem=strict\n", "ReadWritePaths=/var/lib/harnessly /srv/harnessly /tmp /var/tmp\n")
	mustNotContain(t, "runtime unit", unitRt, "PrivateTmp")
	mustContain(t, "agent unit", unitAg, "User=harnessly\n", "EnvironmentFile=/etc/harnessly/agent.env\n", "ExecStart=/usr/local/bin/harnessly-agent\n",
		"PrivateTmp=true\n", "NoNewPrivileges=true\n", "Wants=network-online.target harnessly-runtime.service\n")

	// What was asked of the system, in order.
	calls := e.calledWith()
	mustContain(t, "calls", calls, "useradd --system --user-group --create-home --home-dir /var/lib/harnessly --shell /bin/bash harnessly\n",
		"chown -h harnessly:harnessly "+e.path("/srv/harnessly/work")+"\n", "chown -h harnessly:harnessly "+e.path("/var/lib/harnessly/.harnessly")+"\n")
	// Root never makes, chowns or writes anything inside the service account's folders but the folders themselves: the
	// code file is made by the account (a link put in its place would otherwise be followed by root).
	mustContain(t, "calls", calls, "runuser -u harnessly -- sh -c umask 077 && set -C && cat > \"$1\" sh "+e.path("/var/lib/harnessly/.harnessly")+"/.register-code.",
		"runuser -u harnessly -- rm -f "+e.path("/var/lib/harnessly/.harnessly")+"/.register-code.")
	mustNotContain(t, "calls", calls, "chown harnessly "+e.path("/var/lib/harnessly/.harnessly")+"/.register-code.", "chown harnessly:harnessly "+e.path("/var/lib/harnessly"), "chown "+e.path("/var/lib/harnessly"))
	order := []string{"systemctl daemon-reload\n", "systemctl enable harnessly-runtime.service harnessly-agent.service\n",
		"systemctl restart harnessly-runtime.service\n", "systemctl restart harnessly-agent.service\n"}
	at := 0
	for _, o := range order {
		i := strings.Index(calls[at:], o)
		if i < 0 {
			t.Fatalf("%q missing or out of order in:\n%s", o, calls)
		}
		at += i + len(o)
	}

	// The registration ran as the service user, with the code in a private file that is gone, never on a command line.
	mustContain(t, "calls", calls, "runuser -u harnessly -- env HOME=/var/lib/harnessly ", "--hub "+e.hub.URL+" ", "--profile dir --via command --name Ada box")
	mustNotContain(t, "calls", calls, "hx_secretcode42")
	mustNotContain(t, "output", r.out+r.err, "hx_secretcode42")
	if got := strings.TrimSpace(e.read(filepath.Join(e.dir, "code.log"))); got != "hx_secretcode42" {
		t.Errorf("the code the agent read: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(e.path("/var/lib/harnessly/.harnessly"), ".register-code.*")); len(left) != 0 {
		t.Errorf("the code file stayed: %v", left)
	}
	// The registration keeps a machine that is registered with this hub already, and only asks the hub when there is none.
	mustContain(t, "calls", calls, "--profile dir --via command --name Ada box --reuse")
	mustNotContain(t, "calls", calls, "--force")

	// The manifest says what to take away.
	man := e.read(e.path("/etc/harnessly/install-manifest"))
	mustContain(t, "manifest", man, "profile\tdir\n", "hub\t"+e.hub.URL+"\n", "version\t9.9.9\n", "file\t/usr/local/bin/harnessly-agent\n", "file\t/usr/local/bin/harnessly-runtime\n",
		"file\t/etc/harnessly/policy.toml\n", "file\t/etc/harnessly/runtime.env\n", "unit\tharnessly-agent.service\t/etc/systemd/system/harnessly-agent.service\n",
		"format\t2\n", "user\tharnessly\n", "purge\t/var/lib/harnessly\n", "purge\t/srv/harnessly\n", "data\t/var/lib/harnessly/.harnessly/machine.json\n", "dir\t/etc/harnessly\n")
	mustNotContain(t, "manifest", man, "hx_secretcode42")
}

func TestInstallAgainKeepsThePolicyAndTheManifest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_one", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	manBefore := e.read(e.path("/etc/harnessly/install-manifest"))
	edited := "terminal = false\n"
	if err := os.WriteFile(e.path("/etc/harnessly/policy.toml"), []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	// The user exists now; a port in use by our own services is taken again.
	if r := e.run([]string{"FAKE_USER_HOME=/var/lib/harnessly", "FAKE_LISTEN=127.0.0.1:18022 127.0.0.1:18023"}, "--token", "hx_two", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if got := e.read(e.path("/etc/harnessly/policy.toml")); got != edited {
		t.Errorf("the policy was rewritten: %q", got)
	}
	if got := strings.Count(e.calledWith(), "useradd "); got != 1 {
		t.Errorf("useradd called %d times", got)
	}
	if !strings.Contains(e.calledWith(), "--profile dir --via command --reuse") || strings.Contains(e.calledWith(), "--force") {
		t.Errorf("the second registration did not keep the machine:\n%s", e.calledWith())
	}
	// Only an explicit flag registers the machine anew.
	_ = os.Truncate(e.calls, 0)
	if r := e.run([]string{"FAKE_USER_HOME=/var/lib/harnessly", "FAKE_LISTEN=127.0.0.1:18022 127.0.0.1:18023"}, "--token", "hx_three", "--profile", "dir", "--wait", "2", "--re-register"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if !strings.Contains(e.calledWith(), "--profile dir --via command --force") {
		t.Errorf("--re-register did not force:\n%s", e.calledWith())
	}
	if !strings.Contains(e.read(e.path("/etc/harnessly/runtime.env")), "RUNTIME_GATE_LISTEN=127.0.0.1:18022\n") {
		t.Errorf("the ports moved:\n%s", e.read(e.path("/etc/harnessly/runtime.env")))
	}
	man := e.read(e.path("/etc/harnessly/install-manifest"))
	if man != manBefore {
		t.Errorf("the manifest changed:\n--- before\n%s--- after\n%s", manBefore, man)
	}
}

// --update takes neither a code nor a profile: the profile is the installed one, and the machine is kept.
func TestUpdateNeedsNoCodeAndKeepsTheMachine(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--update", "--wait", "2"); r.code == 0 || !strings.Contains(r.err, "no harnessly install") {
		t.Fatalf("an update of nothing went on: %d\n%s", r.code, r.err)
	}
	if r := e.run(nil, "--token", "hx_one", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	_ = os.Truncate(e.calls, 0)
	r := e.run([]string{"FAKE_USER_HOME=/var/lib/harnessly", "FAKE_LISTEN=127.0.0.1:18022 127.0.0.1:18023"}, "--update", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if !strings.Contains(e.calledWith(), "--profile dir --via command --reuse") {
		t.Errorf("the update did not keep the machine:\n%s", e.calledWith())
	}
}

// The service account owns its home, so it can put a link where the installer expects a folder of its own. Root works in
// that home (it makes the folders, it removes what the installer made), so a link there is refused before anything is
// written through it.
func TestInstallDirRefusesALinkedDataFolder(t *testing.T) {
	t.Parallel()
	for _, name := range []string{".harnessly", "tmux"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			victim := filepath.Join(e.dir, "victim")
			if err := os.MkdirAll(victim, 0o755); err != nil {
				t.Fatal(err)
			}
			home := e.path("/var/lib/harnessly")
			if err := os.MkdirAll(home, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(victim, filepath.Join(home, name)); err != nil {
				t.Fatal(err)
			}
			r := e.run([]string{"FAKE_USER_HOME=/var/lib/harnessly"}, "--token", "hx_secretcode42", "--profile", "dir", "--wait", "2")
			if r.code == 0 {
				t.Fatalf("installed through a link:\n%s\n%s", r.out, r.err)
			}
			mustContain(t, "stderr", r.err, "is a symbolic link")
			if left, _ := os.ReadDir(victim); len(left) != 0 {
				t.Errorf("something was written through the link: %v", left)
			}
			mustNotContain(t, "calls", e.calledWith(), "hx_secretcode42", "register")
		})
	}
}

// Uninstall removes paths inside the same home as root, so it refuses a linked data folder too.
func TestUninstallDirRefusesALinkedDataFolder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	data := e.path("/var/lib/harnessly/.harnessly")
	moved := filepath.Join(e.dir, "elsewhere")
	if err := os.Rename(data, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, data); err != nil {
		t.Fatal(err)
	}
	r := e.run([]string{"FAKE_UID=0"}, "--uninstall")
	if r.code == 0 {
		t.Fatalf("uninstalled through a link:\n%s\n%s", r.out, r.err)
	}
	mustContain(t, "stderr", r.err, "is a symbolic link")
	if !e.exists(filepath.Join(moved, "machine.json")) {
		t.Error("a file behind the link was removed")
	}
}

// The code file's name may already be taken, by a link: the account's own shell refuses to write through it.
func TestRegisterCodeFileIsNeverWrittenThroughALink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link, dangling := filepath.Join(dir, "link"), filepath.Join(dir, "dangling")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "nowhere"), dangling); err != nil {
		t.Fatal(err)
	}
	script := `umask 077 && set -C && cat > "$1"`
	for _, f := range []string{link, dangling} {
		cmd := exec.Command("sh", "-c", script, "sh", f)
		cmd.Stdin = strings.NewReader("hx_code\n")
		if err := cmd.Run(); err == nil {
			t.Errorf("wrote through %s", filepath.Base(f))
		}
	}
	if got, _ := os.ReadFile(target); string(got) != "keep\n" {
		t.Errorf("the file behind the link: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "nowhere")); err == nil {
		t.Error("the dangling link's target was created")
	}
	fresh := filepath.Join(dir, "fresh")
	cmd := exec.Command("sh", "-c", script, "sh", fresh)
	cmd.Stdin = strings.NewReader("hx_code\n")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(fresh); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("a new code file: %v %v", st, err)
	}
}

func TestInstallPicksFreePorts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"FAKE_LISTEN=127.0.0.1:18022 [::]:18024"}, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "runtime.env", e.read(e.path("/etc/harnessly/runtime.env")), "RUNTIME_GATE_LISTEN=127.0.0.1:18023\n")
	mustContain(t, "agent.env", e.read(e.path("/etc/harnessly/agent.env")), "AGENT_PORT=18025\n", "RUNTIME_PORT=18023\n")
}

func TestInstallStopsOnAWrongChecksumBeforeChangingAnything(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.bad = true
	r := e.run(nil, "--token", "hx_a", "--profile", "dir")
	if r.code == 0 || !strings.Contains(r.err, "does not match the hub's checksum") {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	if e.exists(e.path("/usr/local/bin/harnessly-agent")) || strings.Contains(e.calledWith(), "useradd") {
		t.Errorf("something was installed:\n%s", e.calledWith())
	}
	// A manifest that is not served, or does not carry the system, says so.
	r = e.run([]string{"FAKE_UNAME_S=Linux", "FAKE_UNAME_M=aarch64"}, "--token", "hx_a", "--profile", "dir", "--hub", e.hub.URL+"/nowhere")
	if r.code == 0 || !strings.Contains(r.err, "could not download") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestInstallParsesAnIndentedManifest(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dl/manifest.json" {
			var buf bytes.Buffer
			m := map[string]any{"version": "1.0.0", "files": map[string]any{
				"agent-linux-amd64":     map[string]any{"size": 1, "sha256": strings.ToUpper(sum([]byte(fakeAgent)))},
				"agent-linux-amd64.sig": map[string]any{"sha256": strings.Repeat("a", 64)},
				"runtime-linux-amd64":   map[string]any{"sha256": sum([]byte("#!/bin/sh\nexit 0\n")), "size": 2},
			}}
			enc := json.NewEncoder(&buf)
			enc.SetIndent("", "    ")
			_ = enc.Encode(m)
			_, _ = w.Write(buf.Bytes())
			return
		}
		e.serve(w, r)
	}))
	defer srv.Close()
	if runtime.GOARCH != "amd64" {
		t.Skip("the manifest names linux-amd64")
	}
	r := e.run([]string{"FAKE_UNAME_S=Linux", "FAKE_UNAME_M=x86_64"}, "--token", "hx_a", "--profile", "dir", "--hub", srv.URL, "--wait", "2")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "manifest", e.read(e.path("/etc/harnessly/install-manifest")), "version\t1.0.0\n")
}

func TestInstallWithSudoAndNoRoot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"FAKE_UID=1000"}, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	calls := e.calledWith()
	mustContain(t, "calls", calls, "sudo -n useradd ", "sudo -n systemctl restart harnessly-agent.service\n", "sudo -n runuser -u harnessly -- env ")
	// Without it the installer says what it needs and changes nothing.
	e2 := newEnv(t)
	r = e2.run([]string{"FAKE_UID=1000", "FAKE_NO_SUDO=1"}, "--token", "hx_a", "--profile", "dir")
	if r.code == 0 || !strings.Contains(r.err, "needs root once") || e2.exists(e2.path("/usr/local/bin/harnessly-agent")) {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestInstallFullProfileOnLinux(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	// A home of its own: the paths are the account's, no install root.
	script := accountScript(e)
	// An account without passwordless sudo: nothing goes through root.
	r := e.runScript([]byte(script), []string{"FAKE_UID=1000", "FAKE_NO_SUDO=1"}, "--token", "hx_full", "--profile", "full", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	h := e.home
	for _, b := range []string{"harnessly-agent", "harnessly-runtime"} {
		if m := e.mode(filepath.Join(h, ".local/bin", b)); m != 0o755 {
			t.Errorf("%s is %v", b, m)
		}
	}
	if m := e.mode(filepath.Join(h, ".config/harnessly/policy.toml")); m != 0o600 {
		t.Errorf("policy %v", m)
	}
	if m := e.mode(filepath.Join(h, ".harnessly/install-manifest")); m != 0o600 {
		t.Errorf("manifest %v", m)
	}
	mustContain(t, "runtime.env", e.read(filepath.Join(h, ".config/harnessly/runtime.env")), "RUNTIME_WORKSPACE_DIR="+h+"/work\n", "AGENT_DATA_DIR="+h+"/.harnessly\n", "RUNTIME_APP_BASE_PATH=/m/"+machineID+"\n")
	mustNotContain(t, "runtime.env", e.read(filepath.Join(h, ".config/harnessly/runtime.env")), "TMUX_TMPDIR")

	// The units are the ones in deploy/systemd, comments aside.
	for _, name := range []string{"harnessly-runtime.service", "harnessly-agent.service"} {
		got := e.read(filepath.Join(h, ".config/systemd/user", name))
		want := e.read(filepath.Join("..", "systemd", name))
		if stripComments(got) != stripComments(want) {
			t.Errorf("%s differs from deploy/systemd/%s:\n--- installed\n%s--- file\n%s", name, name, got, want)
		}
	}
	calls := e.calledWith()
	mustContain(t, "calls", calls, "systemctl --user daemon-reload\n", "systemctl --user enable harnessly-runtime.service harnessly-agent.service\n",
		"systemctl --user restart harnessly-agent.service\n", "loginctl enable-linger ")
	mustNotContain(t, "calls", calls, "runuser", "sudo", "useradd", "hx_full")
	mustContain(t, "manifest", e.read(filepath.Join(h, ".harnessly/install-manifest")), "profile\tfull\n", "linger\t", "unit\tharnessly-runtime.service\t"+h+"/.config/systemd/user/harnessly-runtime.service\n")

	// Root is refused: this profile is the account's own.
	e2 := newEnv(t)
	r = e2.run([]string{"FAKE_UID=0"}, "--token", "hx_full", "--profile", "full")
	if r.code == 0 || !strings.Contains(r.err, "not root") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func stripComments(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func TestInstallFullProfileOnMac(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	script := accountScript(e)
	r := e.runScript([]byte(script), []string{"FAKE_UID=501", "FAKE_UNAME_S=Darwin", "FAKE_UNAME_M=arm64"}, "--token", "hx_mac", "--profile", "full", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	for job, bin := range map[string]string{"runtime": "harnessly-runtime", "agent": "harnessly-agent"} {
		raw := e.read(filepath.Join(e.home, "Library/LaunchAgents", "io.harnessly."+job+".plist"))
		// It is well-formed, and holds the program, the environment and the log.
		dec := xml.NewDecoder(strings.NewReader(raw))
		for {
			if _, err := dec.Token(); err != nil {
				if err.Error() != "EOF" {
					t.Fatalf("%s is not XML: %v\n%s", job, err, raw)
				}
				break
			}
		}
		mustContain(t, job+" plist", raw, "<string>io.harnessly."+job+"</string>", "<string>"+e.home+"/.local/bin/"+bin+"</string>", "<key>KeepAlive</key>",
			e.home+"/Library/Logs/harnessly/"+job+".log")
	}
	mustContain(t, "runtime plist", e.read(filepath.Join(e.home, "Library/LaunchAgents/io.harnessly.runtime.plist")),
		"<key>RUNTIME_APP_BASE_PATH</key>\n\t\t<string>/m/"+machineID+"</string>", "<key>RUNTIME_GATE_UPSTREAM</key>\n\t\t<string></string>")
	mustContain(t, "agent plist", e.read(filepath.Join(e.home, "Library/LaunchAgents/io.harnessly.agent.plist")), "<key>AGENT_PORT</key>\n\t\t<string>18023</string>")
	mustContain(t, "calls", e.calledWith(), "launchctl bootstrap gui/501 ")
	mustNotContain(t, "calls", e.calledWith(), "systemctl")
	// A mac gets the profile full only.
	r = e.runScript([]byte(script), []string{"FAKE_UID=501", "FAKE_UNAME_S=Darwin", "FAKE_UNAME_M=arm64"}, "--token", "hx_mac", "--profile", "dir")
	if r.code == 0 || !strings.Contains(r.err, "macOS machines use --profile full") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestInstallContainerProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"FAKE_UID=1000", "HARNESSLY_IMAGE=registry.example.com/harnessly/machine:1.2"}, "--token", "hx_ctr", "--profile", "container", "--name", "Ada's Box", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	calls := e.calledWith()
	mustContain(t, "calls", calls, "docker pull registry.example.com/harnessly/machine:1.2\n", "docker volume create harnessly-machine-home\n",
		"--entrypoint harnessly-agent registry.example.com/harnessly/machine:1.2 register --hub "+e.hub.URL+" --code-file /run/harnessly-register-code --profile container --via container --reuse --name Ada's Box\n",
		"docker rm -f harnessly-machine\n",
		"docker run -d --name harnessly-machine --hostname ada-s-box --restart unless-stopped -v harnessly-machine-home:/home/agent registry.example.com/harnessly/machine:1.2\n")
	mustNotContain(t, "calls", calls, "hx_ctr")
	if got := strings.TrimSpace(e.read(filepath.Join(e.dir, "code.log"))); got != "hx_ctr" {
		t.Errorf("the code the container read: %q", got)
	}
	mustContain(t, "output", r.out, "linked to ", "Done: the machine will appear in harnessly.")
	man := e.read(filepath.Join(e.root, e.home, ".harnessly/install-manifest-container"))
	mustContain(t, "manifest", man, "profile\tcontainer\n", "volume\tharnessly-machine-home\n", "container\tharnessly-machine\n")

	// No image, no engine: it says so.
	e2 := newEnv(t)
	r = e2.run([]string{"FAKE_UID=1000"}, "--token", "hx_ctr", "--profile", "container")
	if r.code == 0 || !strings.Contains(r.err, "no machine image") {
		t.Errorf("no image: exit %d\n%s", r.code, r.err)
	}
	r = e2.run([]string{"FAKE_UID=1000", "FAKE_NO_ENGINE=1", "FAKE_NO_SUDO=1"}, "--token", "hx_ctr", "--profile", "container", "--image", "x/y:1")
	if r.code == 0 || !strings.Contains(r.err, "Docker or Podman is needed") {
		t.Errorf("no engine: exit %d\n%s", r.code, r.err)
	}
	// The image may come from the flag.
	e3 := newEnv(t)
	if r = e3.run([]string{"FAKE_UID=1000"}, "--token", "hx_ctr", "--profile", "container", "--image", "x/y:1", "--wait", "2"); r.code != 0 {
		t.Errorf("--image: exit %d\n%s", r.code, r.err)
	}
}

func TestInstallDoesNotWaitForeverForTheLink(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := os.WriteFile(filepath.Join(e.dir, "unlinked"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code == 0 {
		t.Fatalf("exit 0\n%s", r.out)
	}
	mustContain(t, "stderr", r.err, "did not link to the hub in 2 s", "journalctl -u harnessly-agent", "stopped during: Waiting")
	mustNotContain(t, "output", r.out, "Done: the machine")
	if !e.exists(e.path("/etc/systemd/system/harnessly-agent.service")) {
		t.Error("the install was not left in place")
	}
}

func TestDryRunChangesNothing(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		profile string
		extra   []string
	}{
		{"dir", []string{"FAKE_UID=1000", "FAKE_NO_SUDO=1"}},
		{"full", []string{"FAKE_UID=1000"}},
		{"container", []string{"FAKE_UID=1000", "HARNESSLY_IMAGE=x/y:1"}},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			script := strings.ReplaceAll(string(Script), "ROOT=${HARNESSLY_INSTALL_ROOT:-}", "ROOT="+e.root)
			r := e.runScript([]byte(script), tc.extra, "--token", "hx_dry", "--profile", tc.profile, "--name", "Ada", "--dry-run")
			if r.code != 0 {
				t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
			}
			mustContain(t, "output", r.out, "Dry run: nothing will be changed.", "would ", "Dry run finished")
			mustNotContain(t, "output", r.out+r.err, "hx_dry", "Done:")
			if got := e.calledWith(); got != "" {
				t.Errorf("a dry run called the system:\n%s", got)
			}
			var made []string
			_ = filepath.Walk(e.root, func(p string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() {
					made = append(made, p)
				}
				return nil
			})
			if len(made) != 0 {
				t.Errorf("a dry run made files: %v", made)
			}
			if left, _ := filepath.Glob(filepath.Join(e.dir, "harnessly-install.*")); len(left) != 0 {
				t.Errorf("a temp folder stayed: %v", left)
			}
			if e.exists(filepath.Join(e.home, ".harnessly")) || e.exists(filepath.Join(e.home, ".config")) {
				t.Error("a dry run made folders in the home")
			}
		})
	}
}

func TestDryRunPlansTheDirProfile(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"FAKE_UID=0"}, "--token", "hx_dry", "--profile", "dir", "--dry-run")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "plan", r.out, "would run: useradd --system --user-group --create-home --home-dir /var/lib/harnessly --shell /bin/bash harnessly",
		"would write /etc/systemd/system/harnessly-agent.service (mode 0644)", "would write /etc/harnessly/policy.toml (mode 0644)",
		"would run: systemctl restart harnessly-agent.service", "would download "+e.hub.URL+"/dl/manifest.json, agent-linux-")
	if e.exists(e.path("/etc/harnessly")) {
		t.Error("the plan made a folder")
	}
}

func TestInstallRefusesBadArguments(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		args  []string
		extra []string
		want  string
	}{
		{"no token", []string{"--profile", "dir"}, nil, "--token is required"},
		{"no profile", []string{"--token", "hx_a"}, nil, "--profile dir, container or full is required"},
		{"bad profile", []string{"--token", "hx_a", "--profile", "root"}, nil, "--profile must be dir, container or full"},
		{"odd token", []string{"--token", "hx_a;rm -rf ~", "--profile", "dir"}, nil, "the code has odd characters"},
		{"long token", []string{"--token", strings.Repeat("a", 513), "--profile", "dir"}, nil, "the code is too long"},
		{"unknown flag", []string{"--token", "hx_a", "--profile", "dir", "--frobnicate"}, nil, "unknown argument: --frobnicate"},
		{"missing value", []string{"--token", "hx_a", "--profile"}, nil, "--profile needs a value"},
		{"odd name", []string{"--token", "hx_a", "--profile", "dir", "--name", "a\tb"}, nil, "--name has control characters"},
		{"long name", []string{"--token", "hx_a", "--profile", "dir", "--name", strings.Repeat("n", 101)}, nil, "--name is longer"},
		{"bad wait", []string{"--token", "hx_a", "--profile", "dir", "--wait", "soon"}, nil, "--wait is a number"},
		{"bad hub", []string{"--token", "hx_a", "--profile", "dir", "--hub", "ftp://x"}, nil, "--hub must be an http"},
		{"odd hub", []string{"--token", "hx_a", "--profile", "dir", "--hub", "https://x/$(id)"}, nil, "odd characters"},
		{"windows", []string{"--token", "hx_a", "--profile", "dir"}, []string{"FAKE_UNAME_S=MINGW64_NT-10.0", "FAKE_UNAME_M=x86_64"}, "Windows is not supported"},
		{"freebsd", []string{"--token", "hx_a", "--profile", "dir"}, []string{"FAKE_UNAME_S=FreeBSD", "FAKE_UNAME_M=amd64"}, "unsupported system"},
		{"riscv", []string{"--token", "hx_a", "--profile", "dir"}, []string{"FAKE_UNAME_S=Linux", "FAKE_UNAME_M=riscv64"}, "unsupported processor"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			r := e.run(append([]string{"FAKE_UID=0"}, tc.extra...), tc.args...)
			if r.code != 1 || !strings.Contains(r.err, tc.want) {
				t.Errorf("exit %d\n%s", r.code, r.err)
			}
			if got := e.calledWith(); got != "" {
				t.Errorf("the system was called:\n%s", got)
			}
		})
	}
	// Without a hub address.
	e := newEnv(t)
	script := strings.ReplaceAll(string(Script), "HUB_URL=${HARNESSLY_HUB_URL:-}", "HUB_URL=")
	if r := e.runScript([]byte(script), nil, "--token", "hx_a", "--profile", "dir"); r.code != 1 || !strings.Contains(r.err, "the hub's address is unknown") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestInstallSpeaksRussianWhenTheLocaleDoes(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"LANG=ru_RU.UTF-8"}, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "output", r.out, "==> Проверяем систему", "==> Запускаем службы", "Готово: машина появится в harnessly")
	mustNotContain(t, "output", r.out, "Done: the machine", "==> Checking")
	r = e.run([]string{"LANG=ru_RU.UTF-8", "FAKE_UID=0"}, "--token", "hx_a", "--profile", "dir", "--dry-run")
	mustContain(t, "output", r.out, "Пробный запуск: ничего не будет изменено.", "Пробный запуск закончен")
	r = e.run([]string{"LC_ALL=ru_RU.UTF-8", "LANG=C"}, "--profile", "dir")
	mustContain(t, "error", r.err, "нужен --token")
	r = e.run([]string{"HARNESSLY_LANG=ru"}, "--help")
	if r.code != 0 || !strings.Contains(r.out, "--profile P") {
		t.Errorf("help: exit %d\n%s", r.code, r.out)
	}
}

// The hub puts HARNESSLY_HUB_URL on the lines after the first one.
func TestTheHubCanSetItsAddressInTheScript(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	first, rest, _ := strings.Cut(string(Script), "\n")
	served := first + "\nHARNESSLY_HUB_URL='" + e.hub.URL + "'\n" + rest
	cmd := []string{"FAKE_UID=0"}
	script := []byte(served)
	r := e.runScript(script, append(cmd, "HARNESSLY_HUB_URL="), "--token", "hx_a", "--profile", "dir", "--dry-run")
	if r.code != 0 || !strings.Contains(r.out, "Connecting this machine to "+e.hub.URL) {
		t.Errorf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
}

// A download that stops half way must do nothing: the call of main() is the last line, and nothing else runs.
func TestAHalfDownloadedScriptDoesNothing(t *testing.T) {
	t.Parallel()
	text := string(Script)
	last := lines(text)
	if last[len(last)-1] != `main "$@"` {
		t.Fatalf("the last line is %q", last[len(last)-1])
	}
	cut := strings.LastIndex(text, `main "$@"`)
	for _, part := range []string{text[:cut], text[:cut/2], text[:cut-40]} {
		e := newEnv(t)
		r := e.runScript([]byte(part), []string{"FAKE_UID=0"}, "--token", "hx_a", "--profile", "dir")
		if strings.Contains(r.out, "==>") || e.calledWith() != "" {
			t.Errorf("a cut script ran:\n%s\n%s", r.out, e.calledWith())
		}
	}
	// Only function definitions, comments and the call: no command runs at the top level.
	inFunc := false
	for i, l := range strings.Split(text, "\n") {
		switch {
		case inFunc:
			if l == "}" {
				inFunc = false
			}
		case l == "" || strings.HasPrefix(l, "#"):
		case strings.HasSuffix(l, "() {"):
			inFunc = true
		case strings.Contains(l, "() { ") && strings.HasSuffix(l, "}"):
		case l == `main "$@"`:
		default:
			t.Errorf("line %d runs at the top level: %q", i+1, l)
		}
	}
}

func TestUninstallDir(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	// The data the agent keeps stays until it is purged.
	data := e.path("/var/lib/harnessly/.harnessly")
	for _, f := range []string{"agent-link.json", "agent-audit.log"} {
		_ = os.WriteFile(filepath.Join(data, f), []byte("{}"), 0o600)
	}
	_ = os.MkdirAll(filepath.Join(data, "agent-env"), 0o700)
	_ = os.WriteFile(filepath.Join(data, "agent-env", "all.env"), []byte("GH_TOKEN='x'\n"), 0o600)
	_ = os.WriteFile(e.path("/srv/harnessly/work/mine.txt"), []byte("my code"), 0o644)
	if err := os.Truncate(e.calls, 0); err != nil {
		t.Fatal(err)
	}

	r := e.run([]string{"FAKE_UID=0", "FAKE_SVC_USER=1"}, "--uninstall")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	// Every process of the account is ended before root looks at its folders, and what is inside them goes as the account.
	if c := e.calledWith(); !strings.Contains(c, "usermod -L harnessly\n") || !strings.Contains(c, "usermod -e 1 -s /usr/sbin/nologin harnessly\n") || !strings.Contains(c, "crontab -r -u harnessly\n") {
		t.Errorf("the account is not locked and its cron jobs not removed:\n%s", c)
	}
	if c := e.calledWith(); !strings.Contains(c, "pkill -KILL -u harnessly\n") || strings.Index(c, "pkill -KILL") > strings.Index(c, "systemctl daemon-reload") {
		t.Errorf("the account's processes are not ended first:\n%s", c)
	}
	mustContain(t, "output", r.out, "Removed, and the hub has forgotten the machine", "Kept: the work folder")
	calls := e.calledWith()
	// The hub is told first, by the installed agent as the service account, and the git key goes the same way.
	mustContain(t, "calls", calls, "agent unlink\n", "agent gitkey-remove HOME=/var/lib/harnessly\n")
	if strings.Index(calls, "agent unlink") > strings.Index(calls, "systemctl disable --now") {
		t.Errorf("the hub was told after the services went:\n%s", calls)
	}
	mustContain(t, "calls", calls, "systemctl disable --now harnessly-runtime.service\n", "systemctl disable --now harnessly-agent.service\n",
		"systemctl kill --kill-who=all harnessly-agent.service\n", "systemctl daemon-reload\n")
	mustNotContain(t, "calls", calls, "userdel")
	for _, gone := range []string{"/usr/local/bin/harnessly-agent", "/usr/local/bin/harnessly-runtime", "/etc/harnessly/policy.toml", "/etc/harnessly/runtime.env",
		"/etc/harnessly/agent.env", "/etc/harnessly/install-manifest", "/etc/systemd/system/harnessly-agent.service", "/etc/systemd/system/harnessly-runtime.service",
		"/var/lib/harnessly/.harnessly/machine.json", "/var/lib/harnessly/.harnessly/agent-env", "/var/lib/harnessly/.harnessly/agent-link.json", "/etc/harnessly"} {
		if e.exists(e.path(gone)) {
			t.Errorf("%s is still there", gone)
		}
	}
	if !e.exists(e.path("/srv/harnessly/work/mine.txt")) {
		t.Error("the person's work was removed")
	}

	// Again: nothing to do. And with --purge on a fresh install, the user, the home and the work folder go.
	if r = e.run([]string{"FAKE_UID=0"}, "--uninstall"); r.code != 0 || !strings.Contains(r.out, "Nothing to remove") {
		t.Errorf("exit %d\n%s", r.code, r.out)
	}
	e2 := newEnv(t)
	if r = e2.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if r = e2.run([]string{"FAKE_UID=0"}, "--uninstall", "--purge"); r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "calls", e2.calledWith(), "pkill -KILL -u harnessly\n", "userdel harnessly\n")
	for _, gone := range []string{"/srv/harnessly", "/var/lib/harnessly"} {
		if e2.exists(e2.path(gone)) {
			t.Errorf("%s is still there after --purge", gone)
		}
	}
	// The work folder was there before the second install, so --purge leaves it, and what is in it.
	if r = e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if r = e.run([]string{"FAKE_UID=0"}, "--uninstall", "--purge"); r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	if !e.exists(e.path("/srv/harnessly/work/mine.txt")) {
		t.Error("--purge removed a folder that was there before this install")
	}
}

func TestUninstallRefusesPathsItWouldNotMake(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := os.MkdirAll(e.path("/etc/harnessly"), 0o755); err != nil {
		t.Fatal(err)
	}
	precious := e.path("/etc/passwd")
	_ = os.WriteFile(precious, []byte("root"), 0o644)
	_ = os.MkdirAll(e.path("/home/ada/code"), 0o755)
	man := "profile\tdir\nformat\t2\nhub\thttps://h\nfile\t/etc\nfile\t/\nfile\t/etc/harnessly/../passwd\nfile\trelative/path\ndata\t/home\npurge\t/usr/local/../..\nfile\t/etc/harnessly/policy.toml\n"
	if err := os.WriteFile(e.path("/etc/harnessly/install-manifest"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(e.path("/etc/harnessly/policy.toml"), nil, 0o644)
	r := e.run([]string{"FAKE_UID=0"}, "--uninstall", "--purge")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, bad := range []string{"/etc", "/", "/etc/harnessly/../passwd", "relative/path", "/home", "/usr/local/../.."} {
		mustContain(t, "stderr", r.err, "not removing "+bad+":")
	}
	if !e.exists(precious) || !e.exists(e.path("/home/ada/code")) {
		t.Error("something outside the installer's paths was removed")
	}
	if e.exists(e.path("/etc/harnessly/policy.toml")) {
		t.Error("the path it did make was not removed")
	}
	// A file that is not a manifest stops it.
	if err := os.WriteFile(e.path("/etc/harnessly/install-manifest"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r = e.run([]string{"FAKE_UID=0"}, "--uninstall"); r.code == 0 || !strings.Contains(r.err, "is not an installer manifest") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestUninstallFullAndContainer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	script := []byte(accountScript(e))
	if r := e.runScript(script, []string{"FAKE_UID=1000"}, "--token", "hx_a", "--profile", "full", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	// The user's own folders and their data stay.
	runtimeData := filepath.Join(e.home, ".harnessly", "config.json")
	_ = os.WriteFile(runtimeData, []byte("{}"), 0o600)
	_ = os.WriteFile(filepath.Join(e.home, "work", "mine.txt"), []byte("x"), 0o644)
	_ = os.Truncate(e.calls, 0)
	if r := e.runScript(script, []string{"FAKE_UID=1000"}, "--uninstall"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e.calledWith(), "systemctl --user disable --now harnessly-agent.service\n", "loginctl disable-linger ")
	for _, gone := range []string{".local/bin/harnessly-agent", ".config/systemd/user/harnessly-agent.service", ".config/harnessly/runtime.env", ".harnessly/machine.json", ".harnessly/install-manifest"} {
		if e.exists(filepath.Join(e.home, gone)) {
			t.Errorf("%s is still there", gone)
		}
	}
	for _, kept := range []string{".harnessly/config.json", "work/mine.txt"} {
		if !e.exists(filepath.Join(e.home, kept)) {
			t.Errorf("%s was removed", kept)
		}
	}

	e2 := newEnv(t)
	script2 := []byte(accountScript(e))
	if r := e2.runScript(script2, []string{"FAKE_UID=1000", "HARNESSLY_IMAGE=x/y:1"}, "--token", "hx_a", "--profile", "container", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	_ = os.Truncate(e2.calls, 0)
	if r := e2.runScript(script2, []string{"FAKE_UID=1000"}, "--uninstall", "--profile", "container"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e2.calledWith(), "docker rm -f harnessly-machine\n")
	mustNotContain(t, "calls", e2.calledWith(), "volume rm")
	_ = e2.runScript(script2, []string{"FAKE_UID=1000", "HARNESSLY_IMAGE=x/y:1"}, "--token", "hx_a", "--profile", "container", "--wait", "2")
	_ = os.Truncate(e2.calls, 0)
	if r := e2.runScript(script2, []string{"FAKE_UID=1000"}, "--uninstall", "--purge"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e2.calledWith(), "docker volume rm harnessly-machine-home\n")
}

func TestEmbeddedScriptIsTheFile(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("install.sh")
	if err != nil || !bytes.Equal(raw, Script) {
		t.Fatalf("%v", err)
	}
	if !bytes.HasPrefix(Script, []byte("#!/bin/sh\n")) {
		t.Error("no shebang")
	}
}

// The units and plists the installer writes are what systemd accepts.
func TestDirUnitsAreAcceptedBySystemd(t *testing.T) {
	t.Parallel()
	analyze, err := exec.LookPath("systemd-analyze")
	if err != nil {
		t.Skip("no systemd-analyze")
	}
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	for _, b := range []string{"harnessly-agent", "harnessly-runtime"} {
		if !e.exists(e.path("/usr/local/bin/" + b)) {
			t.Fatal("not installed")
		}
	}
	_ = os.WriteFile(e.path("/etc/passwd"), []byte("harnessly:x:999:999::/var/lib/harnessly:/bin/bash\n"), 0o644)
	_ = os.WriteFile(e.path("/etc/group"), []byte("harnessly:x:999:\n"), 0o644)
	for _, name := range []string{"harnessly-runtime.service", "harnessly-agent.service"} {
		out, _ := exec.Command(analyze, "--root="+e.root, "verify", name).CombinedOutput()
		// This environment may not be able to load a whole unit tree; what matters is that no directive is unknown or invalid.
		for _, l := range lines(string(out)) {
			if strings.Contains(l, "Unknown key") || strings.Contains(l, "Invalid") || strings.Contains(l, "Failed to parse") || strings.Contains(l, "Unknown section") {
				t.Errorf("%s: %s", name, l)
			}
		}
	}
}

func TestScriptsPassShellcheck(t *testing.T) {
	t.Parallel()
	sc, err := exec.LookPath("shellcheck")
	if err != nil {
		t.Skip("no shellcheck")
	}
	for _, f := range []string{"install.sh", filepath.Join("..", "container", "entrypoint.sh")} {
		if out, err := exec.Command(sc, "-s", "sh", f).CombinedOutput(); err != nil {
			t.Errorf("%s:\n%s", f, out)
		}
	}
	if dash, err := exec.LookPath("dash"); err == nil {
		if out, err := exec.Command(dash, "-n", "install.sh").CombinedOutput(); err != nil {
			t.Errorf("dash -n:\n%s", out)
		}
	}
}

// Run the way the hub's command does, from a pipe: nothing the installer starts may read what is left of the script.
func TestInstallRunsFromAPipe(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	realID, _ := exec.LookPath("id")
	realUname, _ := exec.LookPath("uname")
	cmd := exec.Command("/bin/sh", "-s", "--", "--token", "hx_pipe", "--profile", "dir", "--wait", "2")
	cmd.Stdin = bytes.NewReader(Script)
	cmd.Env = []string{"PATH=" + e.stubs, "HOME=" + e.home, "TMPDIR=" + e.dir, "LANG=C", "HARNESSLY_INSTALL_ROOT=" + e.root,
		"HARNESSLY_HUB_URL=" + e.hub.URL, "FAKE_CALLS=" + e.calls, "FAKE_CODE_LOG=" + filepath.Join(e.dir, "code.log"),
		"FAKE_DIR=" + e.dir, "REAL_ID=" + realID, "REAL_UNAME=" + realUname, "FAKE_UID=1000"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	mustContain(t, "output", string(out), "Done: the machine will appear in harnessly.")
	mustContain(t, "calls", e.calledWith(), "sudo -n systemctl restart harnessly-agent.service\n")
}

func TestInstallInstallsMissingToolsOnlyWhereItCan(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if err := os.Remove(filepath.Join(e.stubs, "tmux")); err != nil {
		t.Fatal(err)
	}
	r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e.calledWith(), "apt-get update\n", "apt-get install -y tmux\n")
	// Not as root, or without a package manager, it asks the person.
	e2 := newEnv(t)
	_ = os.Remove(filepath.Join(e2.stubs, "tmux"))
	script := accountScript(e)
	r = e2.runScript([]byte(script), []string{"FAKE_UID=1000"}, "--token", "hx_a", "--profile", "full")
	if r.code == 0 || !strings.Contains(r.err, "install tmux first") || strings.Contains(e2.calledWith(), "apt-get") {
		t.Errorf("full: exit %d\n%s\n%s", r.code, r.err, e2.calledWith())
	}
	e3 := newEnv(t)
	_ = os.Remove(filepath.Join(e3.stubs, "git"))
	_ = os.Remove(filepath.Join(e3.stubs, "apt-get"))
	_ = os.Remove(filepath.Join(e3.stubs, "dnf"))
	if r = e3.run(nil, "--token", "hx_a", "--profile", "dir"); r.code == 0 || !strings.Contains(r.err, "install git and run the command again") {
		t.Errorf("no package manager: exit %d\n%s", r.code, r.err)
	}
}

func TestInstallRefusesAFullDisk(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	df := "#!/bin/sh\necho 'Filesystem 1024-blocks Used Available Capacity Mounted'\necho \"/dev/x 1000000 900000 $FAKE_FREE_KB 90% /\"\n"
	_ = os.Remove(filepath.Join(e.stubs, "df"))
	if err := os.WriteFile(filepath.Join(e.stubs, "df"), []byte(df), 0o755); err != nil {
		t.Fatal(err)
	}
	r := e.run([]string{"FAKE_FREE_KB=100000"}, "--token", "hx_a", "--profile", "dir")
	if r.code == 0 || !strings.Contains(r.err, "less than 500 MB free") || strings.Contains(e.calledWith(), "useradd") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
	// Tight but possible: a warning.
	r = e.run([]string{"FAKE_FREE_KB=1500000"}, "--token", "hx_a", "--profile", "dir", "--wait", "2")
	if r.code != 0 || !strings.Contains(r.err, "less than 2 GB free") {
		t.Errorf("exit %d\n%s", r.code, r.err)
	}
}

func TestUninstallTakesTheProfileThatWasAsked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	script := []byte(accountScript(e))
	for _, p := range []string{"full", "container"} {
		if r := e.runScript(script, []string{"FAKE_UID=1000", "HARNESSLY_IMAGE=x/y:1"}, "--token", "hx_a", "--profile", p, "--wait", "2"); r.code != 0 {
			t.Fatalf("%s: %s\n%s", p, r.out, r.err)
		}
	}
	// Both are in the home; asking for the container takes only it.
	if r := e.runScript(script, []string{"FAKE_UID=1000"}, "--uninstall", "--profile", "container"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if !e.exists(filepath.Join(e.home, ".harnessly/install-manifest")) || e.exists(filepath.Join(e.home, ".harnessly/install-manifest-container")) {
		t.Error("the wrong manifest went")
	}
	// Asking for none takes what is left.
	if r := e.runScript(script, []string{"FAKE_UID=1000"}, "--uninstall"); r.code != 0 || e.exists(filepath.Join(e.home, ".harnessly/install-manifest")) {
		t.Errorf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
}

func TestUninstallDryRunRemovesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	_ = os.Truncate(e.calls, 0)
	r := e.run([]string{"FAKE_UID=0"}, "--uninstall", "--dry-run", "--purge")
	if r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "plan", r.out, "would run: systemctl disable --now harnessly-agent.service", "would run: userdel harnessly", "would run: rm -rf "+e.path("/srv/harnessly"))
	if e.calledWith() != "" || !e.exists(e.path("/usr/local/bin/harnessly-agent")) || !e.exists(e.path("/etc/harnessly/install-manifest")) {
		t.Errorf("a dry run removed things:\n%s", e.calledWith())
	}
}

// accountScript is the installer with no install root, for the profiles whose paths are the account's own. What it asks
// of the system itself is pointed at the test's folder: systemd is looked for there, not in the /run of the machine the
// tests run on (a container has none).
func accountScript(e *env) string {
	s := strings.ReplaceAll(string(Script), "ROOT=${HARNESSLY_INSTALL_ROOT:-}", "ROOT=")
	return strings.ReplaceAll(s, `"$(rp /run/systemd/system)"`, `"`+filepath.Join(e.root, "run", "systemd", "system")+`"`)
}

// Without pgrep nothing shows that the account has no process left, so the uninstall stops before it touches a file.
func TestUninstallDirStopsWithoutPgrep(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if err := os.Remove(filepath.Join(e.stubs, "pgrep")); err != nil {
		t.Fatal(err)
	}
	r := e.run([]string{"FAKE_UID=0", "FAKE_SVC_USER=1"}, "--uninstall")
	if r.code == 0 {
		t.Fatalf("uninstalled without being able to look for processes:\n%s", r.out)
	}
	mustContain(t, "stderr", r.err, "pgrep")
	if !e.exists(e.path("/etc/harnessly/install-manifest")) {
		t.Error("files were removed")
	}
}

// A hub that cannot be reached does not keep the machine from being removed; the person is told what is left.
func TestUninstallGoesOnWhenTheHubIsUnreachable(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	r := e.run([]string{"FAKE_UID=0", "FAKE_UNLINK_FAIL=1"}, "--uninstall")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "stderr", r.err, "could not remove the machine from the hub", "could not reach the hub", "Remove the machine in the hub's list")
	mustContain(t, "output", r.out, "Removed. The machine stays in the hub's list")
	if e.exists(e.path("/usr/local/bin/harnessly-agent")) || e.exists(e.path("/etc/harnessly/install-manifest")) {
		t.Error("the files stayed because the hub did not answer")
	}
}

// --purge removes what the installer made and nothing that was there before it came.
func TestPurgeKeepsWhatWasThereBefore(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	// Folders of the same names, with data of somebody else, and a user of that name: none of it is the installer's.
	for f, body := range map[string]string{"/srv/harnessly/theirs.txt": "a", "/var/lib/harnessly/theirs.txt": "b", "/srv/harnessly/work/theirs.txt": "c"} {
		if err := os.MkdirAll(filepath.Dir(e.path(f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(e.path(f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	existing := []string{"FAKE_USER_HOME=/var/lib/harnessly", "FAKE_SVC_USER=1", "FAKE_UID=0"}
	if r := e.run(existing, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	man := e.read(e.path("/etc/harnessly/install-manifest"))
	mustNotContain(t, "manifest", man, "user\tharnessly", "purge\t/var/lib/harnessly\n", "purge\t/srv/harnessly\n", "purge\t/srv/harnessly/work\n")
	mustContain(t, "manifest", man, "purge\t/var/lib/harnessly/.harnessly\n", "purge\t/var/lib/harnessly/tmux\n")
	if r := e.run(existing, "--uninstall", "--purge"); r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustNotContain(t, "calls", e.calledWith(), "userdel")
	for _, kept := range []string{"/srv/harnessly/theirs.txt", "/var/lib/harnessly/theirs.txt", "/srv/harnessly/work/theirs.txt"} {
		if !e.exists(e.path(kept)) {
			t.Errorf("%s was removed", kept)
		}
	}
	for _, gone := range []string{"/var/lib/harnessly/.harnessly", "/var/lib/harnessly/tmux"} {
		if e.exists(e.path(gone)) {
			t.Errorf("%s, made by the installer, is still there", gone)
		}
	}

	// Only the work folder is new: that one goes, and the folder around it stays.
	e2 := newEnv(t)
	if err := os.MkdirAll(e2.path("/srv/harnessly"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(e2.path("/srv/harnessly/theirs.txt"), []byte("a"), 0o644)
	if r := e2.run(existing, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	_ = os.WriteFile(e2.path("/srv/harnessly/work/mine.txt"), []byte("a"), 0o644)
	if r := e2.run(existing, "--uninstall", "--purge"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if !e2.exists(e2.path("/srv/harnessly/theirs.txt")) || e2.exists(e2.path("/srv/harnessly/work")) {
		t.Error("the work folder of the installer stayed, or the folder around it went")
	}
}

// A manifest of an earlier installer marked its folders for --purge whether it had made them or not; those marks are
// not trusted.
func TestPurgeIgnoresTheMarksOfAnEarlierInstaller(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, d := range []string{"/etc/harnessly", "/srv/harnessly", "/var/lib/harnessly"} {
		if err := os.MkdirAll(e.path(d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(e.path("/srv/harnessly/theirs.txt"), []byte("a"), 0o644)
	man := "profile\tdir\nhub\thttps://h\nuser\tharnessly\npurge\t/var/lib/harnessly\npurge\t/srv/harnessly\n"
	if err := os.WriteFile(e.path("/etc/harnessly/install-manifest"), []byte(man), 0o644); err != nil {
		t.Fatal(err)
	}
	r := e.run([]string{"FAKE_UID=0"}, "--uninstall", "--purge")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "stderr", r.err, "earlier installer")
	if !e.exists(e.path("/srv/harnessly/theirs.txt")) || !e.exists(e.path("/var/lib/harnessly")) {
		t.Error("folders were removed on the word of an old manifest")
	}
}

func TestPurgeKeepsAVolumeThatWasThereBefore(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	run := []string{"FAKE_UID=1000", "HARNESSLY_IMAGE=x/y:1", "FAKE_VOLUME_EXISTS=1"}
	if r := e.runScript(Script, run, "--token", "hx_a", "--profile", "container", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	man := e.read(filepath.Join(e.root, e.home, ".harnessly/install-manifest-container"))
	mustNotContain(t, "manifest", man, "volume\t")
	_ = os.Truncate(e.calls, 0)
	if r := e.runScript(Script, []string{"FAKE_UID=1000"}, "--uninstall", "--purge", "--profile", "container"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e.calledWith(), "agent unlink (container)\n")
	mustNotContain(t, "calls", e.calledWith(), "volume rm")
}

// An account's install with passwordless sudo puts ffmpeg there for voice input, and nothing else through root.
func TestInstallFullProfileInstallsFFmpegWithSudo(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.runScript([]byte(accountScript(e)), []string{"FAKE_UID=1000"}, "--token", "hx_full", "--profile", "full", "--wait", "2")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	calls := e.calledWith()
	mustContain(t, "calls", calls, "sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y ffmpeg\n")
	for _, line := range strings.Split(calls, "\n") {
		if strings.HasPrefix(line, "sudo") && !strings.Contains(line, "apt-get") {
			t.Errorf("through root besides ffmpeg: %s", line)
		}
	}
}

func TestInstallDesignToolsWhereItCan(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if r := e.run(nil, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	mustContain(t, "calls", e.calledWith(), "apt-get install -y --no-install-recommends chromium ffmpeg\n")
	// Asked not to, it installs nothing for designs.
	e2 := newEnv(t)
	if r := e2.run([]string{"HARNESSLY_DESIGN_TOOLS=0"}, "--token", "hx_a", "--profile", "dir", "--wait", "2"); r.code != 0 {
		t.Fatalf("%s\n%s", r.out, r.err)
	}
	if strings.Contains(e2.calledWith(), "chromium") {
		t.Errorf("design tools installed anyway:\n%s", e2.calledWith())
	}
}
