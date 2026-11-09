package convo

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// the fast mode and the permission mode of a running Claude change like ultracode, at the same safe moment
// and after it: «/fast on|off», then shift+tab round the modes until the footer says the one asked for.

func fastModels(wire.Provider) []wire.AgentModel {
	levels := []wire.Effort{wire.EffortLow, wire.EffortMedium, wire.EffortHigh}
	return []wire.AgentModel{
		{ID: "opus", Label: "Opus", Efforts: levels, Fast: true},
		{ID: "sonnet", Label: "Sonnet", Efforts: levels},
	}
}

const idleFooter = "⏵⏵ accept edits on\x1b[38;5;246m (shift+tab to cycle)"

func TestFastAndPermissionModesAreTypedIntoARunningClaude(t *testing.T) {
	r := newRig(t)
	terms := &fakeTerms{owned: true}
	r.deps = func(d *Deps) { d.Terminals = terms }
	r.rt = r.runtime()
	r.rt.d.Models = fastModels
	r.open()
	r.settle(wire.StateReady)
	idle := tui(t, "claude-idle-placeholder.ansi.txt")
	if !strings.Contains(idle, idleFooter) {
		t.Fatal("fixture has no mode footer")
	}
	// Claude started without bypass: its shift+tab goes round accept edits, plan and the footer that names no mode.
	footers := []string{idleFooter, "⏸ plan mode on\x1b[38;5;246m (shift+tab to cycle)", "? for shortcuts"}
	at := 0
	answers := map[string]string{"/model opus": "Set model to Opus 5.5", "/fast on": "Fast mode ON", "/fast off": "Fast mode OFF"}
	r.pane.onSend = func(call string) {
		if call == "keys:BTab" {
			at = (at + 1) % len(footers)
			r.pane.screen = strings.Replace(idle, idleFooter, footers[at], 1)
			return
		}
		if text, ok := strings.CutPrefix(call, "line:"); ok {
			if line, ok := answers[text]; ok {
				r.pane.screen = strings.Replace(idle, idleFooter, footers[at], 1) + "\n  ⎿  " + line + "\n"
			}
		}
	}
	// A chat that never said its mode is restarted in the one asked for, after what is typed: shift+tab would only guess.
	plan := wire.PermissionPlan
	res := r.do(wire.ModelCmd{OpID: "f1", ID: r.id, Model: "opus", Fast: on(), Permission: &plan})
	if res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model opus", "line:/fast on"}) {
		t.Fatalf("%v", got)
	}
	if len(terms.stopped) != 1 || len(r.launcher.resumed) != 1 {
		t.Fatalf("stopped %v, resumed %v", terms.stopped, r.launcher.resumed)
	}
	if m := r.modelOf(); m.ID != "opus" || !m.Fast || m.Permission != wire.PermissionPlan {
		t.Fatalf("%+v", m)
	}
	// Back in plan mode, the permission alone to ask goes round by shift+tab; fast stays, nothing typed for it.
	at = 1
	r.pane.screen = strings.Replace(idle, idleFooter, footers[at], 1)
	r.rt.LaunchReady(r.id)
	r.pane.calls = nil
	ask := wire.PermissionAsk
	if res := r.do(wire.ModelCmd{OpID: "f2", ID: r.id, Model: "opus", Permission: &ask}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"keys:BTab"}) {
		t.Fatalf("%v", got)
	}
	if m := r.modelOf(); !m.Fast || m.Permission != wire.PermissionAsk {
		t.Fatalf("%+v", m)
	}
	if len(terms.stopped) != 1 {
		t.Fatalf("stopped %v", terms.stopped)
	}
	log := r.record().ModelLog
	if n := len(log); n < 2 || log[n-1].To.Permission != wire.PermissionAsk || log[n-1].From.Permission != wire.PermissionPlan {
		t.Fatalf("%+v", log)
	}
	// Bypass is not on that cycle: the agent is restarted in it.
	r.pane.calls = nil
	bypass := wire.PermissionBypass
	if res := r.do(wire.ModelCmd{OpID: "f3", ID: r.id, Model: "opus", Permission: &bypass}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if len(r.pane.calls) != 0 || len(terms.stopped) != 2 || len(r.launcher.resumed) != 2 {
		t.Fatalf("calls %v, stopped %v, resumed %v", r.pane.calls, terms.stopped, r.launcher.resumed)
	}
	if m := r.modelOf(); m.Permission != wire.PermissionBypass {
		t.Fatalf("%+v", m)
	}
	// Fast on a model without it is refused before anything is typed; an unknown mode is a bad request.
	at = 2
	r.pane.screen = strings.Replace(idle, idleFooter, footers[at], 1)
	r.rt.LaunchReady(r.id)
	r.pane.calls = nil
	if res := r.do(wire.ModelCmd{OpID: "f4", ID: r.id, Model: "sonnet", Fast: on()}); res.R != wire.ResultRefused {
		t.Fatalf("%+v", res)
	}
	auto := wire.PermissionMode("auto")
	if res := r.do(wire.ModelCmd{OpID: "f5", ID: r.id, Model: "opus", Permission: &auto}); res.R != wire.ResultRefused {
		t.Fatalf("%+v", res)
	}
	if len(r.pane.calls) != 0 {
		t.Fatalf("%v", r.pane.calls)
	}
}

// Without terminals of the runtime's own the agent cannot be restarted: the change is refused, nothing is stopped.
func TestAPermissionModeNeedingARestartIsRefusedWithoutTerminals(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = fastModels
	r.open()
	r.settle(wire.StateReady)
	bypass := wire.PermissionBypass
	res := r.do(wire.ModelCmd{OpID: "b1", ID: r.id, Model: "opus", Permission: &bypass})
	if res.R != wire.ResultRefused || res.Code != wire.RefuseUnsupported {
		t.Fatalf("%+v", res)
	}
	if len(r.launcher.resumed) != 0 {
		t.Fatalf("%v", r.launcher.resumed)
	}
}
