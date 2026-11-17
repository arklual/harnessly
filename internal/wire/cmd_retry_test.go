package wire

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A retry with the same opId that comes while the first attempt is still running joins it; when that attempt ends in
// timeout (its budget, not the retry's, ran out) the retry is not handed that timeout: it makes its own attempt
// within its own budget.
func TestARetryThatJoinedATimedOutAttemptMakesItsOwn(t *testing.T) {
	o := NewOpLog("")
	cmd := ConversationRenameCmd{OpID: "o1", ID: "tab-abc", Title: "x"}
	var runs atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	run := func(ctx context.Context) CmdResult {
		if runs.Add(1) == 1 {
			close(started)
			<-release // the first attempt outlives its caller's budget
			return Refused(RefuseTimeout)
		}
		return Accepted(ChannelConvs, 1)
	}
	first := make(chan CmdResult, 1)
	go func() { first <- o.Run(context.Background(), cmd, 10*time.Millisecond, run) }()
	<-started
	if res := <-first; res.R != ResultRefused || res.Code != RefuseTimeout {
		t.Fatalf("first caller: %+v, want refused{timeout}", res)
	}
	retry := make(chan CmdResult, 1)
	go func() { retry <- o.Run(context.Background(), cmd, 30*time.Second, run) }()
	// Let the retry join the attempt still running (if it is late, it makes its own attempt anyway and the test holds:
	// the pause is what makes it test the join, not what makes it pass); that attempt then ends in timeout.
	time.Sleep(50 * time.Millisecond)
	close(release)
	if res := <-retry; res.R == ResultRefused {
		t.Fatalf("retry: %+v, want its own attempt's answer", res)
	}
	if n := runs.Load(); n != 2 {
		t.Fatalf("%d attempts, want 2", n)
	}
}
