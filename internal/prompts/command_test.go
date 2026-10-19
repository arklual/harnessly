package prompts

import (
	"strings"
	"testing"
)

func TestParseRunLine(t *testing.T) {
	if l, bad := ParseRunLine("/compact keep the plan"); bad != "" || l != (RunLine{"compact", "keep the plan"}) {
		t.Fatalf("got %+v %q", l, bad)
	}
	if l, bad := ParseRunLine("/tracker:task"); bad != "" || l != (RunLine{"tracker:task", ""}) {
		t.Fatalf("got %+v %q", l, bad)
	}
	for _, line := range []string{"/compact\nrm -rf /", "/model \x1b[A", "/x y", "compact", "/", "/ spaced", "/" + strings.Repeat("a", 2000)} {
		if _, bad := ParseRunLine(line); bad == "" {
			t.Errorf("%.30q accepted", line)
		}
	}
}

func TestClassifyCommand(t *testing.T) {
	if it, ok := ClassifyCommand("claude", "logout"); !ok || it.Execution != "blocked" {
		t.Error("logout")
	}
	if it, ok := ClassifyCommand("claude", "cost"); !ok || it.Name != "usage" {
		t.Error("cost alias")
	}
	if it, ok := ClassifyCommand("codex", "model"); !ok || it.Execution != "menu" {
		t.Error("codex model")
	}
	if _, ok := ClassifyCommand("claude", "tracker:task"); ok {
		t.Error("a plugin command is a built-in")
	}
}

func TestLaunchChangeOf(t *testing.T) {
	if a, ok := LaunchChangeOf("claude", RunLine{"model", "fable"}); !ok || *a.Model != "fable" || a.Effort != nil {
		t.Error("model")
	}
	if a, ok := LaunchChangeOf("claude", RunLine{"effort", "xhigh"}); !ok || *a.Effort != "xhigh" {
		t.Error("effort")
	}
	for _, c := range []struct {
		provider string
		line     RunLine
	}{{"claude", RunLine{"effort", "ultra"}}, {"claude", RunLine{"model", "x'; rm"}}, {"codex", RunLine{"model", "gpt-5.4"}}} {
		if _, ok := LaunchChangeOf(c.provider, c.line); ok {
			t.Errorf("%+v changes the launch", c)
		}
	}
}

// fakeCommandPane plays screens in order (the last repeats) and records what was typed and pressed.
type fakeCommandPane struct {
	command string
	screens []string
	index   int
	state   string
	exists  bool
	typed   []string
	keys    []string
}

func (p *fakeCommandPane) HasSession(string) bool       { return p.exists }
func (p *fakeCommandPane) CurrentCommand(string) string { return p.command }
func (p *fakeCommandPane) IsShell(c string) bool        { return c == "bash" || c == "zsh" }
func (p *fakeCommandPane) CaptureStyled(string) (string, bool) {
	s := p.screens[min(p.index, len(p.screens)-1)]
	p.index++
	return s, true
}
func (p *fakeCommandPane) SendLine(_, line string) error { p.typed = append(p.typed, line); return nil }
func (p *fakeCommandPane) SendKey(_, key string) error   { p.keys = append(p.keys, key); return nil }
func (p *fakeCommandPane) CliState(string) (string, bool) {
	return p.state, p.state != ""
}

func commandPane(screens ...string) *fakeCommandPane {
	return &fakeCommandPane{command: "claude", screens: screens, state: StateIdle, exists: true}
}

func runOn(p *fakeCommandPane, provider, line string) RunResult {
	return RunCommandLine(RunInput{Session: "hs-ws-a-pane-b-tab-c", Provider: provider, Line: line}, p, newFakeClock())
}

func errorOf(r RunResult) string {
	if e, ok := r.Body.(*RunError); ok {
		return e.Error
	}
	return ""
}

func TestRunCommandLine(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")

	p := commandPane(idle)
	if r := runOn(p, "claude", "/logout"); r.Status != 422 || len(p.typed) != 0 {
		t.Errorf("blocked: %+v", r)
	}

	p = commandPane("agent@host:~$ ")
	p.command = "zsh"
	if r := runOn(p, "claude", "/compact"); r.Status != 409 || errorOf(r) != RunNotRunning || len(p.typed) != 0 {
		t.Errorf("shell: %+v", r)
	}

	if r := runOn(commandPane(tui(t, "claude-switch-model-confirm.txt")), "claude", "/context"); errorOf(r) != RunMenuOpen {
		t.Errorf("menu: %+v", r)
	}
	busy := commandPane(idle)
	busy.state = StateBusy
	if r := runOn(busy, "claude", "/context"); errorOf(r) != RunBusy {
		t.Errorf("busy: %+v", r)
	}
	if r := runOn(commandPane(tui(t, "claude-input-draft.ansi.txt")), "claude", "/context"); errorOf(r) != RunInputNotEmpty {
		t.Errorf("draft: %+v", r)
	}

	// Prompt-like commands go through while busy: the CLI queues them.
	p = commandPane(idle)
	p.state = StateBusy
	r := runOn(p, "claude", "/tracker:task найди задачу")
	if b, ok := r.Body.(*RunBody); r.Status != 200 || !ok || b.Outcome != "sent" || b.Expect != "reply" || len(p.typed) != 1 || p.typed[0] != "/tracker:task найди задачу" {
		t.Errorf("queueable: %+v %v", r, p.typed)
	}

	// «Switch model?» is answered for /model <alias>. The idle capture already shows an earlier "Set model to", so the
	// change counts as applied only once the confirmation was answered and the wait ran out (as in the previous runtime).
	applied := strings.Replace(tui(t, "claude-effort-applied.txt"), "Set effort level to medium", "Set model to Sonnet 5", 1)
	p = commandPane(idle, idle, tui(t, "claude-switch-model-confirm.txt"), applied)
	r = runOn(p, "claude", "/model sonnet")
	if b, ok := r.Body.(*RunBody); !ok || b.Outcome != "confirmed" || *b.Applied.Model != "sonnet" || !*b.Asked || len(p.keys) != 1 || p.keys[0] != "Enter" {
		t.Errorf("switch model: %+v %v", r.Body, p.keys)
	}

	p = commandPane(idle, tui(t, "claude-effort-applied.txt"))
	r = runOn(p, "claude", "/effort medium")
	if b, ok := r.Body.(*RunBody); !ok || b.Outcome != "confirmed" || *b.Applied.Effort != "medium" || *b.Asked || len(p.keys) != 0 {
		t.Errorf("effort: %+v", r.Body)
	}

	p = commandPane(tui(t, "codex-idle-placeholder.ansi.txt"), tui(t, "codex-model-step1.txt"))
	p.command = "node"
	r = runOn(p, "codex", "/model")
	if b, ok := r.Body.(*RunBody); r.Status != 200 || !ok || b.Outcome != "menu" || *b.Menu.Title != "Select Model and Effort" {
		t.Errorf("codex menu: %+v", r.Body)
	}

	r = runOn(commandPane(idle, tui(t, "claude-status.txt")), "claude", "/status")
	if b, ok := r.Body.(*RunBody); !ok || b.Outcome != "panel" || !strings.Contains(*b.Screen, "Esc to cancel") {
		t.Errorf("panel: %+v", r.Body)
	}

	p = commandPane(idle)
	r = runOn(p, "claude", "/context")
	if b, ok := r.Body.(*RunBody); r.Status != 200 || !ok || b.Outcome != "sent" || b.Expect != "transcript" || b.Command != "/context" || len(p.typed) != 1 {
		t.Errorf("sent: %+v", r.Body)
	}

	if r := RunCommandLine(RunInput{Session: "hs-x", Provider: "claude", Line: "/model", ExpectModel: true}, commandPane(idle), newFakeClock()); r.Status != 400 {
		t.Errorf("switch-model mode: %+v", r)
	}
	gone := commandPane(idle)
	gone.exists = false
	if r := runOn(gone, "claude", "/context"); r.Status != 404 {
		t.Errorf("gone: %+v", r)
	}
	if r := RunCommandLine(RunInput{Session: "a:b.c", Provider: "claude", Line: "/context"}, commandPane(idle), newFakeClock()); r.Status != 400 {
		t.Errorf("bad session: %+v", r)
	}
}

func TestRunCommandFollowUps(t *testing.T) {
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	r := newRig(t, idle, Tab{})
	r.events.stuckChecks = make(chan string, 4)
	r.panes.onSend = func(call string) {
		if call == "line:/effort medium" {
			r.panes.setScreen(tui(t, "claude-effort-applied.txt"))
		}
	}
	res := r.svc.RunCommand(RunInput{Session: testSession, Provider: "claude", Line: "/effort medium"})
	if res.Status != 200 {
		t.Fatalf("run: %+v", res)
	}
	if got := <-r.events.stuckChecks; got != "tab-1" {
		t.Errorf("stuck check for %s", got)
	}
	if ev := r.events.events(); len(ev) != 1 || ev[0] != "launch:"+testSession+":effort=medium" {
		t.Errorf("events %v", ev)
	}

	r.panes.setScreen(idle)
	r.svc.RunCommand(RunInput{Session: testSession, Provider: "claude", Line: "/reload-plugins"})
	<-r.events.stuckChecks
	if ev := r.events.events(); ev[len(ev)-1] != "reloaded:claude" {
		t.Errorf("events %v", ev)
	}
}
