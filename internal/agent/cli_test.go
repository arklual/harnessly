package agent

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// cliRig is a home and a data folder for the commands to work in, set through the environment they read.
func cliRig(t *testing.T) (home, data string) {
	t.Helper()
	home = t.TempDir()
	data = filepath.Join(home, "data")
	t.Setenv("HOME", home)
	t.Setenv("AGENT_DATA_DIR", data)
	t.Setenv("AGENT_MACHINE_FILE", "")
	t.Setenv("AGENT_POLICY_FILE", filepath.Join(home, "policy.toml"))
	return home, data
}

func runCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code, handled := RunCLI(args, &out, &errb)
	if !handled {
		t.Fatalf("%v was not handled", args)
	}
	return code, out.String(), errb.String()
}

func TestRunCLIWithoutACommandRunsTheAgent(t *testing.T) {
	if _, handled := RunCLI(nil, os.Stdout, os.Stderr); handled {
		t.Error("no arguments are the agent itself")
	}
	code, _, errText := runCLI(t, "frobnicate")
	if code != 2 || !strings.Contains(errText, "unknown command") || !strings.Contains(errText, "register") {
		t.Errorf("%d %q", code, errText)
	}
	if code, out, _ := runCLI(t, "version"); code != 0 || strings.TrimSpace(out) != BuildVersion {
		t.Errorf("%d %q", code, out)
	}
}

func TestCLIRegisterStatusOffOn(t *testing.T) {
	home, data := cliRig(t)
	hub := newFakeRegistry(t)

	// Nothing registered yet.
	if code, out, _ := runCLI(t, "status"); code != 1 || strings.TrimSpace(out) != "not registered" {
		t.Errorf("%d %q", code, out)
	}

	// The code may come from a file; the command says what it did and writes the file privately.
	codeFile := filepath.Join(t.TempDir(), "code")
	if err := os.WriteFile(codeFile, []byte("hx_fromfile\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errText := runCLI(t, "register", "--hub", hub.srv.URL, "--code-file", codeFile, "--profile", "full", "--via", "local", "--name", "Ada box")
	if code != 0 || !strings.Contains(out, "registered as abcdefghijklmnopqrst") || errText != "" {
		t.Fatalf("%d %q %q", code, out, errText)
	}
	if got := hub.got[0]; got.Code != "hx_fromfile" || got.Profile != "full" || got.Via != "local" || got.Hostname != "Ada box" {
		t.Errorf("%+v", got)
	}
	st, err := os.Stat(filepath.Join(data, "machine.json"))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, st)
	}

	// Again without --force: refused, with the reason, and the first registration stays.
	if code, _, errText := runCLI(t, "register", "--hub", hub.srv.URL, "--token", "hx_other", "--profile", "full"); code != 1 || !strings.Contains(errText, "already registered") {
		t.Errorf("%d %q", code, errText)
	}
	if len(hub.got) != 1 {
		t.Errorf("the hub was asked again: %d", len(hub.got))
	}

	// --reuse keeps the registration without asking the hub, so a code that was used does not matter.
	if code, out, errText := runCLI(t, "register", "--hub", hub.srv.URL, "--token", "hx_used", "--profile", "full", "--reuse"); code != 0 || !strings.Contains(out, "already registered as abcdefghijklmnopqrst") || errText != "" {
		t.Errorf("%d %q %q", code, out, errText)
	}
	if len(hub.got) != 1 {
		t.Errorf("--reuse asked the hub: %d", len(hub.got))
	}

	// Registered, no agent running.
	if code, out, _ := runCLI(t, "status"); code != 1 || !strings.Contains(out, "agent is not running") {
		t.Errorf("%d %q", code, out)
	}

	// A running agent that is linked.
	write := func(s LinkState) {
		t.Helper()
		if err := WriteLinkState(LinkStatePath(data), s); err != nil {
			t.Fatal(err)
		}
	}
	write(LinkState{MachineID: "abcdefghijklmnopqrst", Linked: true, UpdatedAt: time.Now().UnixMilli()})
	if code, out, _ := runCLI(t, "status"); code != 0 || strings.TrimSpace(out) != "linked to "+hub.srv.URL+" as abcdefghijklmnopqrst" {
		t.Errorf("%d %q", code, out)
	}

	// Off: the switch file is written next to the named policy file, status says so, and a second `off` changes nothing.
	if code, out, _ := runCLI(t, "off"); code != 0 || !strings.Contains(out, "off:") {
		t.Errorf("%d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, "disabled")); err != nil {
		t.Errorf("no switch file next to the policy: %v", err)
	}
	if code, out, _ := runCLI(t, "status"); code != 1 || !strings.HasPrefix(out, "off (") {
		t.Errorf("%d %q", code, out)
	}
	if code, out, _ := runCLI(t, "off"); code != 0 || !strings.Contains(out, "already off") {
		t.Errorf("%d %q", code, out)
	}
	if code, out, _ := runCLI(t, "policy"); code != 0 || !strings.Contains(out, "kill switch is on") || !strings.Contains(out, `"terminal": true`) {
		t.Errorf("%d %q", code, out)
	}

	// On again.
	if code, out, _ := runCLI(t, "on"); code != 0 || !strings.Contains(out, "on:") {
		t.Errorf("%d %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(home, "disabled")); !os.IsNotExist(err) {
		t.Errorf("the switch file is still there: %v", err)
	}
	if code, out, _ := runCLI(t, "on"); code != 0 || !strings.Contains(out, "already on") {
		t.Errorf("%d %q", code, out)
	}

	// Not linked, with the reason the agent gave; a stale file is an agent that is gone.
	write(LinkState{MachineID: "abcdefghijklmnopqrst", Note: "revoked by the hub", UpdatedAt: time.Now().UnixMilli()})
	if code, out, _ := runCLI(t, "status"); code != 1 || !strings.Contains(out, "revoked by the hub") {
		t.Errorf("%d %q", code, out)
	}
	write(LinkState{MachineID: "abcdefghijklmnopqrst", Linked: true, UpdatedAt: time.Now().Add(-10 * time.Minute).UnixMilli()})
	if code, out, _ := runCLI(t, "status"); code != 1 || !strings.Contains(out, "not running") {
		t.Errorf("a stale file: %d %q", code, out)
	}
	// The file of another registration is not this machine's.
	write(LinkState{MachineID: "zzzzzzzzzzzzzzzzzzzz", Linked: true, UpdatedAt: time.Now().UnixMilli()})
	if code, out, _ := runCLI(t, "status"); code != 1 || !strings.Contains(out, "not running") {
		t.Errorf("another machine's file: %d %q", code, out)
	}
}

func TestCLIRegisterRefusals(t *testing.T) {
	cliRig(t)
	hub := newFakeRegistry(t)
	for _, tc := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"register", "--hub", hub.srv.URL, "--profile", "dir"}, 1, "code is empty"},
		{[]string{"register", "--hub", hub.srv.URL, "--token", "hx_a", "--code-file", "/nope"}, 2, "not both"},
		{[]string{"register", "--hub", hub.srv.URL, "--code-file", "/nonexistent/code", "--profile", "dir"}, 1, "code file"},
		{[]string{"register", "--hub", "ftp://x", "--token", "hx_a", "--profile", "dir"}, 1, "--hub"},
		{[]string{"register", "--hub", hub.srv.URL, "--token", "hx_a", "--profile", "root"}, 1, "--profile"},
		{[]string{"register", "--hub", hub.srv.URL, "--token", "hx_a", "--profile", "dir", "--via", "carrier-pigeon"}, 1, "--via"},
		{[]string{"register", "--nope"}, 2, "flag provided but not defined"},
		{[]string{"register", "stray"}, 2, "unexpected argument"},
	} {
		code, _, errText := runCLI(t, tc.args...)
		if code != tc.code || !strings.Contains(errText, tc.want) {
			t.Errorf("%v: %d %q", tc.args, code, errText)
		}
	}
	if len(hub.got) != 0 {
		t.Errorf("the hub was called by a refused command: %d", len(hub.got))
	}
	// A hub that says no: its message is what the person reads, and nothing is written.
	hub.reply = func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(410)
		_, _ = w.Write([]byte(`{"error":"expired","message":"Код истёк"}`))
	}
	code, _, errText := runCLI(t, "register", "--hub", hub.srv.URL, "--token", "hx_old", "--profile", "dir")
	if code != 1 || !strings.Contains(errText, "Код истёк") {
		t.Errorf("%d %q", code, errText)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("AGENT_DATA_DIR"), "machine.json")); !os.IsNotExist(err) {
		t.Errorf("a refused registration left a file: %v", err)
	}
}

func TestLoadAgentEnvFillsOnlyWhatIsMissing(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "etc.env")
	second := filepath.Join(dir, "user.env")
	if err := os.WriteFile(first, []byte("# the service's folders\nAGENT_DATA_DIR=/var/lib/harnessly/.harnessly\nAGENT_POLICY_FILE='/etc/harnessly/policy.toml'\nAGENT_PORT=18023\nRUNTIME_PORT=1\nAGENT_MACHINE_FILE=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("AGENT_DATA_DIR=/elsewhere\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	have := map[string]string{"AGENT_POLICY_FILE": "/mine"}
	loadAgentEnv([]string{filepath.Join(dir, "missing.env"), first, second}, func(k string) (string, bool) { v, ok := have[k]; return v, ok }, func(k, v string) { got[k] = v })
	// The first file that exists is the only one read; only the folder variables are taken; set ones are kept.
	if len(got) != 1 || got["AGENT_DATA_DIR"] != "/var/lib/harnessly/.harnessly" {
		t.Errorf("%v", got)
	}
}

func TestSwitchTargetsPreferTheSystemFolderWhenItExists(t *testing.T) {
	home, data := t.TempDir(), t.TempDir()
	etc := t.TempDir()
	s := NewPolicyStore("", home, data)
	s.etc = etc
	p := cliPaths{home: home, dataDir: data, policy: s}
	// No account folder yet: the system folder, then the data folder.
	want := []string{filepath.Join(etc, "disabled"), filepath.Join(data, "disabled")}
	got := p.switchTargets()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("%v", got)
	}
	if err := os.MkdirAll(filepath.Join(home, ".config", "harnessly"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := p.switchTargets(); len(got) != 3 || got[1] != filepath.Join(home, ".config", "harnessly", "disabled") {
		t.Errorf("%v", got)
	}
	// A named policy file: its folder, then the data folder.
	s = NewPolicyStore(filepath.Join(home, "p", "policy.toml"), home, data)
	if err := os.MkdirAll(filepath.Join(home, "p"), 0o700); err != nil {
		t.Fatal(err)
	}
	p = cliPaths{home: home, dataDir: data, policy: s}
	if got := p.switchTargets(); len(got) != 2 || got[0] != filepath.Join(home, "p", "disabled") {
		t.Errorf("%v", got)
	}
}

// `policy` says when the file is one the account can change, since then a widening waits for a SIGHUP.
func TestPolicyCommandNotesAFileTheAccountCanWrite(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	writeFile(t, file, "terminal = false\n")
	for _, tc := range []struct {
		name     string
		writable func(string) bool
		note     bool
	}{{"a file the account writes", writableByAgent, true}, {"a root-owned file", rootOwned, false}} {
		s := NewPolicyStore(file, dir, filepath.Join(dir, "data"))
		s.writable = tc.writable
		var out strings.Builder
		if code := cmdPolicy(cliPaths{home: dir, dataDir: filepath.Join(dir, "data"), policy: s}, &out); code != 0 {
			t.Fatalf("%s: exit %d", tc.name, code)
		}
		if got := strings.Contains(out.String(), "waits for SIGHUP"); got != tc.note {
			t.Errorf("%s: note %v, want %v\n%s", tc.name, got, tc.note, out.String())
		}
		if !strings.Contains(out.String(), `"terminal": false`) {
			t.Errorf("%s: the policy is not printed\n%s", tc.name, out.String())
		}
	}
}
