package prompts

import (
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/typist"
)

// A Legacy writer of a pane waits for the lock v2's typist holds on it, and writes nothing meanwhile; another pane is
// not held up.
func TestALegacyWriterWaitsForThePaneLockOfV2(t *testing.T) {
	r := newRig(t, "", Tab{})
	unlock := typist.Lock(testSession)
	done := make(chan bool)
	go func() { done <- r.svc.Interrupt("tab-1") }()
	select {
	case <-done:
		t.Fatal("interrupted a pane another writer holds")
	case <-time.After(150 * time.Millisecond):
	}
	if sent := r.panes.sent(); len(sent) != 0 {
		t.Fatalf("keys went in while the pane was held: %v", sent)
	}
	unlock()
	select {
	case ok := <-done:
		if !ok {
			t.Fatal("not interrupted once the pane was free")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("still waiting after the pane was free")
	}
	if sent := r.panes.sent(); len(sent) != 1 {
		t.Fatalf("%v", sent)
	}
}

func TestEveryLegacyWriterLetsTheLockGo(t *testing.T) {
	r := newRig(t, "", Tab{})
	for range 3 {
		r.svc.Interrupt("tab-1")
		r.svc.RunCommand(RunInput{Session: testSession, Provider: ProviderClaude, Line: "/help"})
		r.svc.SendInput([]byte(`{"session":"` + testSession + `","input":"Escape"}`))
		r.svc.CommandScreenKey([]byte(`{"session":"` + testSession + `","key":"Escape"}`))
		_, _ = r.svc.TypePrompt("tab-1", "hello", true, "m1", nil)
	}
	unlock, ok := typist.TryLock(testSession)
	if !ok {
		t.Fatal("a writer kept the lock")
	}
	unlock()
}
