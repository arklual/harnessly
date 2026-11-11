package main

import (
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The readers share one read unless the variable is exactly 0: that is the way back to a cursor each without a
// release.
func TestTranscriptIndexSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": true, "1": true, "on": true, "0": false} {
		if got := transcriptIndexOn(func(string) string { return value }); got != want {
			t.Errorf("%s=%q: on=%v", transcriptIndexEnv, value, got)
		}
	}
}

// The health probe has the shared read's numbers while it is on and nothing of them when it is off.
func TestTranscriptNumbersFollowTheSwitch(t *testing.T) {
	was := transcripts.Load()
	t.Cleanup(func() { transcripts.Store(was) })
	transcripts.Store(nil)
	if got := transcriptNumbers(); got != nil {
		t.Fatalf("numbers of a shared read that is off: %v", got)
	}
	if transcriptLines("test") != nil {
		t.Fatal("a place in a shared read that is off")
	}
	transcripts.Store(makeTranscriptIndex())
	o, ok := transcriptNumbers().(*jsvalue.Object)
	if !ok {
		t.Fatalf("numbers: %v", transcriptNumbers())
	}
	if raw := string(jsvalue.Stringify(o, "")); raw != `{"files":0,"readers":0,"looks":0,"opens":0,"bytes":0,"lost":0,"resets":0}` {
		t.Fatal(raw)
	}
	if transcriptLines("test") == nil {
		t.Fatal("no place in the shared read")
	}
}
