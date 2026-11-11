package design

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepDelayFollowsWhatTheRuntimeHolds(t *testing.T) {
	if got := sweepDelay(SweepResult{Own: 1}, nil); got != sweepEvery {
		t.Errorf("a held claim: %v, want %v", got, sweepEvery)
	}
	if got := sweepDelay(SweepResult{}, errors.New("no identity")); got != sweepEvery {
		t.Errorf("a failed pass: %v, want %v", got, sweepEvery)
	}
	for i := 0; i < 50; i++ {
		if got := sweepDelay(SweepResult{}, nil); got < sweepIdleEvery || got > sweepIdleEvery+sweepIdleEvery/2 {
			t.Fatalf("nothing held: %v, want %v to %v", got, sweepIdleEvery, sweepIdleEvery+sweepIdleEvery/2)
		}
		if got := sweepDelay(SweepResult{Offline: true, Own: 0}, nil); got < sweepOfflineEvery || got > sweepOfflineEvery+sweepOfflineEvery/2 {
			t.Fatalf("no answer: %v, want %v to %v", got, sweepOfflineEvery, sweepOfflineEvery+sweepOfflineEvery/2)
		}
	}
}

func TestHistoryPollIsRareWhileTheWatcherRuns(t *testing.T) {
	if got := historyPollDelay(false, historyPoll); got != historyPoll {
		t.Errorf("unwatched: %v, want %v", got, historyPoll)
	}
	for i := 0; i < 50; i++ {
		if got := historyPollDelay(true, historyPoll); got < historyPollWatched || got > historyPollWatched+historyPollWatched/2 {
			t.Fatalf("watched: %v, want %v to %v", got, historyPollWatched, historyPollWatched+historyPollWatched/2)
		}
	}
	if got := historyPollDelay(true, time.Hour); got != time.Hour {
		t.Errorf("a poll longer than the watched one is kept: %v", got)
	}
}

func TestATakenClaimWakesTheKeeperAndCountsAsOwn(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "files", "A.dc.html"), "<html></html>")
	l := testLibrary(t, root)
	l.bg.mu.Lock()
	l.bg.wake = make(chan struct{}, 1)
	l.bg.mu.Unlock()
	now := nowMs()
	owner := ClaimOwner{User: "ann", Runtime: "rt", TabID: "tab-1", ChatTitle: "Agent"}
	if res, err := l.ClaimBoard("files", "A", owner, now); err != nil || res.Held != nil {
		t.Fatalf("claim: %+v %v", res, err)
	}
	select {
	case <-l.bg.wake:
	default:
		t.Fatal("a taken claim must wake the keeper")
	}
	chat := func(string) (ChatView, bool) { return ChatView{State: "busy", Title: "Agent"}, true }
	if res := l.SweepClaims(now+1, "rt", chat, map[string]int64{}); res.Own != 1 {
		t.Errorf("a working chat's claim: own %d, want 1", res.Own)
	}
	if res := l.SweepClaims(now+1, "other", chat, map[string]int64{}); res.Own != 0 {
		t.Errorf("another runtime's claim: own %d, want 0", res.Own)
	}
	idle := func(string) (ChatView, bool) { return ChatView{State: "idle", Title: "Agent"}, true }
	if res := l.SweepClaims(now+claimGraceMs+1, "rt", idle, map[string]int64{}); res.Own != 0 || len(res.Released) != 1 {
		t.Errorf("a released claim: own %d released %d, want 0 and 1", res.Own, len(res.Released))
	}
}
