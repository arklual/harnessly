package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type harness struct {
	t      *testing.T
	rig    *agentstest.Rig
	mgr    *Manager
	mu     sync.Mutex
	events []hubproto.Job
	done   []string

	report  hubproto.PreflightReport
	check   hubproto.ProviderCheck
	checked int
	gate    chan struct{} // when set, the provider check waits for it
}

func okReport() hubproto.PreflightReport {
	return hubproto.PreflightReport{Checks: []hubproto.Check{
		{ID: "disk", Level: "ok"}, {ID: "memory", Level: "ok"}, {ID: "tmux", Level: "ok"}, {ID: "git", Level: "ok"}, {ID: "node", Level: "ok"},
	}}
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	rig := agentstest.New(t)
	rig.Link("bash")
	rig.Link("ls")
	h := &harness{t: t, rig: rig, report: okReport()}
	h.check = hubproto.ProviderCheck{Verdict: hubproto.VerdictOK, Host: "api.example.com", Hosts: []hubproto.HostCheck{}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.mgr = NewManager(ctx, Deps{
		Shell: rig.Checker, EnvDir: rig.EnvDir, Home: rig.Home, JobsDir: filepath.Join(rig.Home, ".harnessly", "install"),
		Preflight: func(context.Context) hubproto.PreflightReport { return h.report },
		Check: func(_ context.Context, provider string) hubproto.ProviderCheck {
			h.mu.Lock()
			h.checked++
			gate := h.gate
			h.mu.Unlock()
			if gate != nil {
				<-gate
			}
			c := h.check
			c.Provider = provider
			return c
		},
		Emit: func(j hubproto.Job) { h.mu.Lock(); h.events = append(h.events, j); h.mu.Unlock() },
		Done: func(p string, ok bool) {
			h.mu.Lock()
			h.done = append(h.done, p+":"+map[bool]string{true: "ok", false: "failed"}[ok])
			h.mu.Unlock()
		},
	})
	return h
}

func (h *harness) run(provider string, from bool) hubproto.Job {
	h.t.Helper()
	if err := h.mgr.Start("job1", provider, from); err != nil {
		h.t.Fatal(err)
	}
	h.mgr.Wait()
	j, ok := h.mgr.Get("job1")
	if !ok {
		h.t.Fatal("no job")
	}
	return j
}

func states(j hubproto.Job) string {
	var out []string
	for _, s := range j.Steps {
		out = append(out, s.ID+"="+s.State)
	}
	return strings.Join(out, " ")
}

// fakeCurl writes the "installer" it is asked to download: a script that puts a working claude into ~/.local/bin.
const fakeCurl = `
out=""
while [ $# -gt 0 ]; do
  if [ "$1" = "-o" ]; then out="$2"; shift; fi
  shift
done
cat > "$out" <<'INSTALLER'
mkdir -p "$HOME/.local/bin"
printf '#!/bin/sh\necho "2.1.310 (Claude Code)"\n' > "$HOME/.local/bin/claude"
chmod +x "$HOME/.local/bin/claude"
echo "installed"
INSTALLER
echo "$HTTPS_PROXY" > "$HOME/curl-proxy"
`

func TestClaudeInstall(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", fakeCurl)
	h.rig.WriteEnv("all.env", "HTTPS_PROXY='http://proxy.example.com:3128'\n")
	j := h.run("claude", false)
	if j.State != hubproto.JobDone || j.Error != nil || j.Check != nil {
		t.Fatalf("%+v", j)
	}
	if want := "system=ok provider=ok download=ok install=ok verify=ok"; states(j) != want {
		t.Fatalf("%s", states(j))
	}
	if j.Kind != "agent-install" || j.Provider == nil || *j.Provider != "claude" || j.ID != "job1" {
		t.Fatalf("%+v", j)
	}
	if d := j.Steps[4].Detail; d == nil || *d != "2.1.310" {
		t.Fatalf("verify detail %v", d)
	}
	if got, _ := os.ReadFile(filepath.Join(h.rig.Home, "curl-proxy")); strings.TrimSpace(string(got)) != "http://proxy.example.com:3128" {
		t.Errorf("the download did not go through the agent's proxy: %q", got)
	}
	// ~/.local/bin was off the PATH, so the profile got the line, once.
	profile, _ := os.ReadFile(filepath.Join(h.rig.Home, ".profile"))
	if strings.Count(string(profile), profileMarker) != 1 || !strings.Contains(string(profile), `$HOME/.local/bin`) {
		t.Errorf("profile %q", profile)
	}
	if _, err := os.Stat(filepath.Join(h.rig.Home, ".harnessly", "install", "job1")); !os.IsNotExist(err) {
		t.Errorf("the job's folder is left: %v", err)
	}
	if len(h.done) != 1 || h.done[0] != "claude:ok" {
		t.Errorf("done %v", h.done)
	}
	// Events: one per change, each a snapshot, steps only ever move forward.
	if len(h.events) < 10 {
		t.Fatalf("%d events", len(h.events))
	}
	rank := map[string]int{"wait": 0, "run": 1, "ok": 2, "fail": 2}
	prev := map[string]int{}
	for i, e := range h.events {
		for _, s := range e.Steps {
			if rank[s.State] < prev[s.ID] {
				t.Fatalf("event %d: step %s went back to %s", i, s.ID, s.State)
			}
			prev[s.ID] = rank[s.State]
		}
	}
	if last := h.events[len(h.events)-1]; last.State != "done" {
		t.Errorf("last event %+v", last)
	}
	if first := h.events[0]; first.State != "running" {
		t.Errorf("first event %+v", first)
	}
}

func TestClaudeInstallTwiceKeepsOneProfileLine(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", fakeCurl)
	h.run("claude", false)
	if err := h.mgr.Start("job2", "claude", false); err != nil {
		t.Fatal(err)
	}
	h.mgr.Wait()
	profile, _ := os.ReadFile(filepath.Join(h.rig.Home, ".profile"))
	if strings.Count(string(profile), profileMarker) != 1 {
		t.Errorf("profile %q", profile)
	}
}

const fakeNPM = `
case "$1" in
  pack) echo "openai-codex-0.152.0.tgz"; : > openai-codex-0.152.0.tgz ;;
  install)
    prefix=""
    while [ $# -gt 0 ]; do if [ "$1" = "--prefix" ]; then prefix="$2"; fi; shift; done
    mkdir -p "$prefix/bin"
    printf '#!/bin/sh\necho "codex-cli 0.152.0"\n' > "$prefix/bin/codex"
    chmod +x "$prefix/bin/codex" ;;
esac
`

func TestCodexInstall(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("npm", fakeNPM)
	j := h.run("codex", false)
	if j.State != "done" || states(j) != "system=ok provider=ok download=ok install=ok verify=ok" {
		t.Fatalf("%s: %+v", states(j), j)
	}
	if d := j.Steps[4].Detail; d == nil || *d != "0.152.0" {
		t.Fatalf("verify detail %v", d)
	}
}

func TestSystemStepFails(t *testing.T) {
	h := newHarness(t)
	h.report.Checks[0] = hubproto.Check{ID: "disk", Level: "fail", Message: "Мало места на диске: 1,2 ГБ свободно, нужно хотя бы 3 ГБ"}
	j := h.run("claude", false)
	if j.State != "failed" || states(j) != "system=fail provider=wait download=wait install=wait verify=wait" {
		t.Fatalf("%s", states(j))
	}
	if j.Error == nil || j.Error.Code != "system" || !strings.Contains(j.Error.Message, "Мало места") {
		t.Fatalf("%+v", j.Error)
	}
	if h.checked != 0 {
		t.Error("the provider was asked after the system failed")
	}
	if len(h.done) != 1 || h.done[0] != "claude:failed" {
		t.Errorf("done %v", h.done)
	}
}

func TestCodexNeedsNode(t *testing.T) {
	h := newHarness(t)
	h.report.Checks[4] = hubproto.Check{ID: "node", Level: "warn", Message: "Node.js не найден. Codex не поставить без Node.js 20+"}
	j := h.run("codex", false)
	if j.State != "failed" || j.Error == nil || j.Error.Code != "system" || !strings.Contains(j.Error.Message, "Node.js 20+") {
		t.Fatalf("%+v", j)
	}
	// Claude does not need Node.
	h2 := newHarness(t)
	h2.report.Checks[4] = hubproto.Check{ID: "node", Level: "warn", Message: "Node.js не найден"}
	h2.rig.Script("curl", fakeCurl)
	if j := h2.run("claude", false); j.State != "done" {
		t.Fatalf("%+v", j)
	}
}

func TestProviderStepFails(t *testing.T) {
	for verdict, text := range map[string]string{"network": "не отвечают", "rejected": "отказали"} {
		h := newHarness(t)
		detail := "timeout"
		h.check = hubproto.ProviderCheck{Verdict: verdict, Host: "api.anthropic.com", Detail: &detail, Hosts: []hubproto.HostCheck{{Host: "api.anthropic.com"}}}
		j := h.run("claude", false)
		if j.State != "failed" || states(j) != "system=ok provider=fail download=wait install=wait verify=wait" {
			t.Fatalf("%s: %s", verdict, states(j))
		}
		if j.Error == nil || j.Error.Code != "provider" || !strings.Contains(j.Error.Message, "Anthropic") || !strings.Contains(j.Error.Message, text) {
			t.Fatalf("%s: %+v", verdict, j.Error)
		}
		if j.Check == nil || j.Check.Verdict != verdict || j.Check.Provider != "claude" {
			t.Fatalf("%s: the check is not on the job: %+v", verdict, j.Check)
		}
		if d := j.Steps[1].Detail; d == nil || !strings.Contains(*d, "api.anthropic.com") || !strings.Contains(*d, "timeout") {
			t.Fatalf("detail %v", d)
		}
	}
}

func TestStartFromProvider(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", fakeCurl)
	h.report.Checks[0] = hubproto.Check{ID: "disk", Level: "fail", Message: "never looked at"}
	j := h.run("claude", true)
	if j.State != "done" || states(j) != "system=ok provider=ok download=ok install=ok verify=ok" {
		t.Fatalf("%s %+v", states(j), j.Error)
	}
	if h.checked != 1 {
		t.Errorf("provider checked %d times", h.checked)
	}
}

func TestDownloadAndInstallFailures(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", `echo "curl: (22) The requested URL returned error: 403"; exit 22`)
	j := h.run("claude", false)
	if j.State != "failed" || states(j) != "system=ok provider=ok download=fail install=wait verify=wait" {
		t.Fatalf("%s", states(j))
	}
	if j.Error.Code != "download" || j.Steps[2].Detail == nil || !strings.Contains(*j.Steps[2].Detail, "403") {
		t.Fatalf("%+v %v", j.Error, j.Steps[2].Detail)
	}

	h = newHarness(t)
	h.rig.Script("npm", `case "$1" in pack) : > x.tgz;; install) echo "npm ERR! EACCES: permission denied"; exit 243;; esac`)
	j = h.run("codex", false)
	if j.State != "failed" || states(j) != "system=ok provider=ok download=ok install=fail verify=wait" {
		t.Fatalf("%s", states(j))
	}
	if j.Error.Code != "install" || j.Steps[3].Detail == nil || !strings.Contains(*j.Steps[3].Detail, "EACCES") {
		t.Fatalf("%+v %v", j.Error, j.Steps[3].Detail)
	}

	// No curl and no wget at all.
	h = newHarness(t)
	j = h.run("claude", false)
	if j.State != "failed" || j.Error.Code != "download" || j.Steps[2].Detail == nil || !strings.Contains(*j.Steps[2].Detail, "curl") {
		t.Fatalf("%+v", j)
	}
}

func TestVerifyFailures(t *testing.T) {
	// The installer did nothing.
	h := newHarness(t)
	h.rig.Script("curl", `out=""; while [ $# -gt 0 ]; do if [ "$1" = "-o" ]; then out="$2"; shift; fi; shift; done; echo "true" > "$out"`)
	j := h.run("claude", false)
	if j.State != "failed" || j.Error.Code != "verify" || j.Steps[4].Detail == nil || !strings.Contains(*j.Steps[4].Detail, "не найден") {
		t.Fatalf("%+v %v", j.Error, j.Steps[4].Detail)
	}
	// Installed in ~/.local/bin, and the login shell never reads the profile (a shell that ignores it).
	h = newHarness(t)
	h.rig.Script("curl", fakeCurl)
	if err := os.WriteFile(h.rig.Checker.Shell, []byte("#!/bin/sh\nPATH=\""+h.rig.Bin+":"+h.rig.Tools+"\"; export PATH\nshift\neval \"$1\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	j = h.run("claude", false)
	if j.State != "failed" || j.Error.Code != "verify" || j.Steps[4].Detail == nil || !strings.Contains(*j.Steps[4].Detail, "PATH") {
		t.Fatalf("%+v %v", j.Error, j.Steps[4].Detail)
	}
}

func TestOneInstallPerProvider(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", fakeCurl)
	h.rig.Script("npm", fakeNPM)
	h.gate = make(chan struct{})
	if err := h.mgr.Start("a", "claude", false); err != nil {
		t.Fatal(err)
	}
	if err := h.mgr.Start("b", "claude", false); err != ErrBusy {
		t.Fatalf("a second install of the same agent: %v", err)
	}
	if err := h.mgr.Start("c", "codex", false); err != nil {
		t.Fatalf("another agent is fine: %v", err)
	}
	if err := h.mgr.Start("a", "codex", false); err == nil {
		t.Fatal("a reused job id")
	}
	close(h.gate)
	h.mgr.Wait()
	if err := h.mgr.Start("d", "claude", false); err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
	h.mgr.Wait()
	if err := h.mgr.Start("e", "gemini", false); err == nil {
		t.Fatal("gemini has no installer")
	}
	if _, ok := h.mgr.Get("nope"); ok {
		t.Fatal("a job out of nothing")
	}
}

func TestTail(t *testing.T) {
	if got := tail("\n  \nfirst\n\x1b[31mred\x1b[0m\nlast\n"); got != "first / [31mred[0m / last" && got != "first / red / last" {
		// ESC is dropped, the rest of the sequence stays: readable enough.
		if !strings.HasPrefix(got, "first / ") || !strings.HasSuffix(got, "/ last") {
			t.Errorf("%q", got)
		}
	}
	long := tail(strings.Repeat("я", 1000))
	if r := []rune(long); len(r) != outputTail+1 || r[0] != '…' {
		t.Errorf("%d", len(r))
	}
	if got := tail("1\n2\n3\n4\n5\n6"); got != "3 / 4 / 5 / 6" {
		t.Errorf("%q", got)
	}
}

func TestEmitDoesNotShareState(t *testing.T) {
	h := newHarness(t)
	h.rig.Script("curl", fakeCurl)
	h.run("claude", false)
	h.events[0].Steps[0].State = "tampered"
	j, _ := h.mgr.Get("job1")
	if j.Steps[0].State != "ok" {
		t.Fatalf("an event shares memory with the job: %+v", j.Steps[0])
	}
	_ = time.Second
}
