package wire

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// The answers outlive a restart: the same opId gets the same answer from the next process, another body a conflict.
func TestOpLogKeepsAnswersOnDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v2", "ops.json")
	log := NewOpLog(path)
	cmd := ConversationPinCmd{OpID: "op-1", ID: "tab-a", Pinned: true}
	runs := 0
	run := func(context.Context) CmdResult { runs++; return Accepted(ChannelConvs, 7) }
	if res := log.Run(context.Background(), cmd, time.Second, run); res.R != ResultAccepted || res.Seq != 7 {
		t.Fatalf("first %+v", res)
	}
	deadline := time.Now().Add(2 * time.Second)
	var next *OpLog
	for {
		next = NewOpLog(path)
		next.mu.Lock()
		n := len(next.m)
		next.mu.Unlock()
		if n == 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if res := next.Run(context.Background(), cmd, time.Second, run); res.Seq != 7 || runs != 1 {
		t.Fatalf("after a restart: %+v, ran %d times", res, runs)
	}
	cmd.Pinned = false
	if res := next.Run(context.Background(), cmd, time.Second, run); res.Code != RefuseConflict {
		t.Fatalf("another body: %+v", res)
	}
	// Past OpsKeep an answer is forgotten.
	next.now = func() time.Time { return time.Now().Add(OpsKeep + time.Hour) }
	next.mu.Lock()
	next.prune()
	n := len(next.m)
	next.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d answers past their day", n)
	}
}
