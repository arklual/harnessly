package convo

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Ultracode is a mode beside the effort (Claude Code 2.1.310): «/effort ultracode on|off», typed after the model and
// the effort, at the same safe moment.

var ultraAnswers = map[string]string{
	"/model opus":           "Set model to Opus 5.5",
	"/model sonnet":         "Set model to Sonnet 5.5",
	"/model haiku":          "Set model to Haiku 4.5",
	"/effort medium":        "Set effort level to medium (this session only): Balanced approach",
	"/effort high":          "Set effort level to high (this session only): Comprehensive implementation",
	"/effort ultracode on":  "Ultracode on (this session only): dynamic workflows on every task. Effort stays medium.",
	"/effort ultracode off": "Ultracode off. Effort stays medium.",
}

func ultraModels(wire.Provider) []wire.AgentModel {
	levels := []wire.Effort{wire.EffortLow, wire.EffortMedium, wire.EffortHigh, wire.EffortXHigh, wire.EffortMax}
	return []wire.AgentModel{
		{ID: "opus", Label: "Opus", Efforts: levels, Ultracode: true},
		{ID: "sonnet", Label: "Sonnet", Efforts: levels, Ultracode: true},
		{ID: "haiku", Label: "Haiku", Efforts: []wire.Effort{}},
	}
}

func on() *bool  { v := true; return &v }
func off() *bool { v := false; return &v }

func TestUltracodeIsTurnedOnAndOffAfterTheModelAndTheEffort(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = ultraModels
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(ultraAnswers)
	medium := wire.EffortMedium

	// All three at once: the model, the effort, then the mode.
	res := r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "opus", Effort: &medium, Ultracode: on()})
	if res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model opus", "line:/effort medium", "line:/effort ultracode on"}) {
		t.Fatalf("%v", got)
	}
	if m := r.modelOf(); m.ID != "opus" || *m.Effort != medium || !m.Ultracode {
		t.Fatalf("%+v", m)
	}

	// Another model and level, the mode not mentioned: it stays on, nothing is typed for it.
	r.pane.calls = nil
	high := wire.EffortHigh
	r.do(wire.ModelCmd{OpID: "u2", ID: r.id, Model: "sonnet", Effort: &high})
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model sonnet", "line:/effort high"}) {
		t.Fatalf("%v", got)
	}
	if m := r.modelOf(); m.ID != "sonnet" || *m.Effort != high || !m.Ultracode {
		t.Fatalf("%+v", m)
	}

	// The mode alone, off.
	r.pane.calls = nil
	r.do(wire.ModelCmd{OpID: "u3", ID: r.id, Model: "sonnet", Effort: &high, Ultracode: off()})
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/effort ultracode off"}) {
		t.Fatalf("%v", got)
	}
	if m := r.modelOf(); m.Ultracode || *m.Effort != high {
		t.Fatalf("%+v", m)
	}
	log := r.record().ModelLog
	if n := len(log); n != 3 || !log[0].To.Ultracode || log[2].To.Ultracode || !log[2].From.Ultracode {
		t.Fatalf("log %+v", log)
	}
}

func TestUltracodeOnAModelWithoutItIsRefusedBeforeAnyKey(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = ultraModels
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(ultraAnswers)
	if res := r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "haiku", Ultracode: on()}); res.R != wire.ResultRefused || res.Code != wire.RefuseOptionUnknown {
		t.Fatalf("%+v", res)
	}
	// A chat on ultracode asked for Haiku without a word on the mode keeps the mode: refused all the same.
	r.do(wire.ModelCmd{OpID: "u2", ID: r.id, Model: "opus", Ultracode: on()})
	r.pane.calls = nil
	if res := r.do(wire.ModelCmd{OpID: "u3", ID: r.id, Model: "haiku"}); res.R != wire.ResultRefused || res.Code != wire.RefuseOptionUnknown {
		t.Fatalf("%+v", res)
	}
	// Haiku with the mode off goes: the model, then the mode off.
	if res := r.do(wire.ModelCmd{OpID: "u4", ID: r.id, Model: "haiku", Ultracode: off()}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/model haiku", "line:/effort ultracode off"}) {
		t.Fatalf("%v", got)
	}
}

// The agent refuses the mode (a setup without dynamic workflows): the model that took stays on the record, the mode
// does not, and the answer says why.
func TestUltracodeTheAgentRefusesLeavesWhatTook(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	answers := map[string]string{"/model opus": "Set model to Opus 5.5",
		"/effort ultracode on": "Ultracode needs dynamic workflows enabled (see /config). Valid options are: low, medium"}
	r.agentPlays(answers)
	res := r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "opus", Ultracode: on()})
	if res.R != wire.ResultRefused || res.Code != wire.RefuseOptionUnknown {
		t.Fatalf("%+v", res)
	}
	if m := r.modelOf(); m == nil || m.ID != "opus" || m.Ultracode {
		t.Fatalf("%+v", m)
	}
}

// Asked during a turn, the mode waits with the model and goes in before the next queued message.
func TestUltracodeAskedDuringATurnGoesInWithTheModelBeforeTheNextMessage(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = ultraModels
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentPlays(ultraAnswers)
	medium := wire.EffortMedium
	if res := r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "opus", Effort: &medium, Ultracode: on()}); res.R != wire.ResultAccepted {
		t.Fatalf("%+v", res)
	}
	if p := r.record().PendingModel; p == nil || !p.Ultracode || p.ID != "opus" {
		t.Fatalf("pending %+v", p)
	}
	r.send("m1", "next please", wire.ModeQueue)
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	want := []string{"line:/model opus", "line:/effort medium", "line:/effort ultracode on", "line:next please"}
	if got := r.pane.calls; !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %v", got)
	}
	if rec := r.record(); rec.PendingModel != nil || !rec.Model.Ultracode {
		t.Fatalf("%+v", rec)
	}
}

// A waiting change the agent kept its model for is not lost in silence: the log says it did not take, and why.
func TestAWaitingChangeTheAgentKeptIsLoggedAsRefused(t *testing.T) {
	r := newRig(t)
	r.user("first")
	r.busy()
	r.open()
	r.settle(wire.StateWorking)
	r.agentPlays(map[string]string{"/model sonnet": "Kept model as Opus 5.5"})
	r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "sonnet"})
	r.say("done")
	r.turnEnd()
	r.idle()
	r.hook("stop")
	rec := r.record()
	if rec.PendingModel != nil || rec.Model != nil && rec.Model.ID == "sonnet" {
		t.Fatalf("%+v", rec)
	}
	log := rec.ModelLog
	if len(log) != 1 || log[0].Refused == nil || *log[0].Refused != wire.RefuseConflict || log[0].To.ID != "sonnet" {
		t.Fatalf("log %+v", log)
	}
}

// Claude Code keeps ultracode for one process: a chat on it that is resumed has it typed again, before its message.
func TestAResumedChatOnUltracodeHasItTypedAgain(t *testing.T) {
	r := newRig(t)
	r.rt.d.Models = ultraModels
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(ultraAnswers)
	medium := wire.EffortMedium
	r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "opus", Effort: &medium, Ultracode: on()})
	r.machine.probes["s1"] = Probe{Session: true, Shell: true, Resumable: true}
	r.tick(time.Second)
	if st := r.state(); st.K != wire.StateStopped {
		t.Fatalf("%+v", st)
	}
	// A new process: its screen has none of the answers of the one before.
	r.pane.calls, r.pane.screen = nil, tui(t, "claude-idle-placeholder.ansi.txt")
	r.send("m1", "hello", wire.ModeQueue)
	rec := r.record()
	if rec.Model.Ultracode || rec.PendingModel == nil || !rec.PendingModel.Ultracode {
		t.Fatalf("after the resume: model %+v pending %+v", rec.Model, rec.PendingModel)
	}
	r.machine.probes["s1"] = Probe{Session: true, Foreground: true}
	r.rt.LaunchReady(r.id)
	if got := r.pane.calls; !reflect.DeepEqual(got, []string{"line:/effort ultracode on", "line:hello"}) {
		t.Fatalf("%v", got)
	}
	if m := r.record().Model; !m.Ultracode || m.ID != "opus" || *m.Effort != medium {
		t.Fatalf("%+v", m)
	}
}

// A record saved when ultracode was a level of the effort comes back as the mode: xhigh with ultracode on.
func TestARecordOfBeforeWithEffortUltracodeComesBackAsTheMode(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentPlays(map[string]string{"/model opus": "Set model to Opus 5.5", "/effort xhigh": "Set effort level to xhigh",
		"/effort ultracode on": "Ultracode on (this session only): x. Effort stays xhigh."})
	xhigh := wire.EffortXHigh
	r.do(wire.ModelCmd{OpID: "u1", ID: r.id, Model: "opus", Effort: &xhigh, Ultracode: on()})
	r.rt.Stop()
	path := filepath.Join(r.dir, "v2", "convs", r.id+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	now := `"effort":"xhigh","ultracode":true`
	if !strings.Contains(string(raw), now) {
		t.Fatalf("saved %s", raw)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), now, `"effort":"ultracode"`)), 0o600); err != nil {
		t.Fatal(err)
	}
	r.restart()
	m := r.record().Model
	if m == nil || m.ID != "opus" || m.Effort == nil || *m.Effort != wire.EffortXHigh || !m.Ultracode {
		t.Fatalf("%+v", m)
	}
	// An ask of a page of before, effort "ultracode", is read the same: nothing to type, the chat runs it already.
	cmd, err := wire.DecodeCommand(wire.OpModel, []byte(`{"opId":"u2","id":"`+r.id+`","model":"opus","effort":"ultracode"}`))
	if err != nil {
		t.Fatal(err)
	}
	r.pane.calls = nil
	if res := r.do(cmd); res.R != wire.ResultAccepted || len(r.pane.calls) != 0 {
		t.Fatalf("%+v %v", res, r.pane.calls)
	}
}
