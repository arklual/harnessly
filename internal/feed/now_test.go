package feed

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestNowIsWhatTheOpenTurnDoes(t *testing.T) {
	lines := strings.SplitAfter(conversation(1), "\n")
	path := write(t, lines[0]+lines[1])
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: path})
	f.Load()
	n := f.Now("Running", 5)
	if n == nil || n.Action == nil || n.Action.Tool == "" || n.Word == nil || *n.Word != "Running" {
		t.Fatalf("now during a tool call: %+v", n)
	}
	if err := n.Validate(); err != nil {
		t.Fatal(err)
	}
	fh, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if _, err := fh.WriteString(lines[2]); err != nil {
		t.Fatal(err)
	}
	fh.Close()
	f.Poll()
	if n := f.Now("", 6); n == nil || n.Action != nil {
		t.Fatalf("the tool finished, still its action: %+v", n)
	}
	f.EndTurn(f.OpenTurn(), wire.TurnDone, nil, 7)
	if n := f.Now("", 8); n != nil {
		t.Fatalf("now outside a turn: %+v", n)
	}
}

// At most two a second, the latest one wins, the same one again goes nowhere.
func TestNowThrottle(t *testing.T) {
	var th NowThrottle
	t0 := time.UnixMilli(1_000_000)
	now := func(word string) *wire.Now { return &wire.Now{TurnID: "x:0#0", Word: sptr(word)} }
	if ev := th.Offer(now("a"), t0); ev == nil {
		t.Fatal("the first now waits")
	}
	if ev := th.Offer(now("a"), t0.Add(10*time.Millisecond)); ev != nil {
		t.Fatal("the same now went again")
	}
	if ev := th.Offer(now("b"), t0.Add(100*time.Millisecond)); ev != nil {
		t.Fatal("a second now inside the window")
	}
	if ev := th.Offer(now("c"), t0.Add(200*time.Millisecond)); ev != nil {
		t.Fatal("a third now inside the window")
	}
	if w := th.Wake(); !w.Equal(t0.Add(NowEvery)) {
		t.Fatalf("wake %v", w)
	}
	if ev := th.Due(t0.Add(400 * time.Millisecond)); ev != nil {
		t.Fatal("due before the window ends")
	}
	ev := th.Due(t0.Add(NowEvery))
	if ev == nil || *ev.Word != "c" {
		t.Fatalf("due: %+v", ev)
	}
	if !th.Wake().IsZero() {
		t.Fatal("nothing waits, still a wake")
	}
	sent := 1
	for i := range 100 {
		at := t0.Add(NowEvery + time.Duration(i)*20*time.Millisecond)
		if th.Offer(now(string(rune('d'+i%20))), at) != nil {
			sent++
		}
		if th.Due(at) != nil {
			sent++
		}
	}
	// 2 s of offers every 20 ms: at most 2 Hz, plus the first.
	if sent > 6 {
		t.Fatalf("%d nows in 2.5 s", sent)
	}
}
