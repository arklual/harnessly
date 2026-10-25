package convo

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// lastPromptAt is the time of the person's last real message: the agent's lines, meta lines, compaction summaries,
// hook and task notifications, tool results and reminders do not move it.
func TestLastPromptAtIsTheLastRealUserLine(t *testing.T) {
	r := newRig(t)
	r.open()
	created := r.rec().CreatedAt
	if got := r.rec().LastPromptAt; got != created {
		t.Fatalf("a chat with no messages keeps its creation time: %d, created %d", got, created)
	}

	r.tick(time.Minute)
	r.user("first")
	r.tick(time.Second)
	first := r.rec().LastPromptAt
	if first <= created {
		t.Fatalf("a user line did not move it: %d", first)
	}

	r.tick(time.Minute)
	r.say("answer")
	r.tool("t1", "ls")
	r.result("t1", "files")
	r.turnEnd()
	r.compactBoundary(10)
	r.compactSummary()
	r.line(`"type":"user","isMeta":true,"message":{"role":"user","content":%q}`, "meta text")
	r.line(`"type":"user","isMeta":true,"message":{"role":"user","content":%q}`, "<local-command-caveat>Caveat: do not respond</local-command-caveat>")
	r.line(`"type":"user","message":{"role":"user","content":%q}`, "<system-reminder>remember the rules</system-reminder>")
	r.line(`"type":"user","message":{"role":"user","content":%q}`, "<task-notification><task-id>b1</task-id><status>completed</status><summary>Done</summary></task-notification>")
	r.line(`"type":"attachment","attachment":{"type":"hook_success","hookName":"SessionStart:resume","content":"hi"}`)
	r.tick(time.Second)
	if got := r.rec().LastPromptAt; got != first {
		t.Fatalf("lines that are not the person's moved it: %d, was %d", got, first)
	}

	r.tick(time.Minute)
	r.user("second")
	r.tick(time.Second)
	if got := r.rec().LastPromptAt; got <= first {
		t.Fatalf("the next real line did not move it: %d, was %d", got, first)
	}
}

// A message sent from the UI moves it at the send, before the transcript shows the line; the agent's work after it
// does not.
func TestLastPromptAtMovesAtTheSend(t *testing.T) {
	r := newRig(t)
	r.open()
	r.settle(wire.StateReady)
	r.agentTakes()
	before := r.rec().LastPromptAt

	r.tick(time.Hour)
	sentAt := r.clock.Now().UnixMilli()
	r.send("m1", "hello", wire.ModeQueue)
	if got := r.rec().LastPromptAt; got != sentAt {
		t.Fatalf("the send did not move it to the moment of the send: %d, want %d (was %d)", got, sentAt, before)
	}

	r.tick(time.Minute)
	r.say("working")
	r.turnEnd()
	r.tick(time.Second)
	if got := r.rec().LastPromptAt; got < sentAt || got > sentAt+int64(time.Second/time.Millisecond)*2 {
		t.Fatalf("the agent's work moved it: %d, sent at %d", got, sentAt)
	}
}

// It survives a restart.
func TestLastPromptAtSurvivesARestart(t *testing.T) {
	r := newRig(t)
	r.open()
	r.tick(time.Minute)
	r.user("hello")
	r.tick(time.Second)
	want := r.rec().LastPromptAt
	r.rt.Stop()
	r.rt = r.runtime()
	r.rt.Start(t.Context())
	if got := r.rec().LastPromptAt; got != want {
		t.Fatalf("after a restart: %d, want %d", got, want)
	}
}
