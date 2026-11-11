package filestore

import (
	"testing"
)

func pollOnlyMeta() *pgMeta {
	return &pgMeta{lastRev: map[string]int64{}, heard: map[string]int64{}, seenHeard: map[string]int64{}}
}

func inHeardRange(t *testing.T, m *pgMeta) bool {
	t.Helper()
	got := m.pollWait()
	return got >= pgPollHeardEvery && got <= pgPollHeardEvery+pgPollHeardEvery/2
}

func TestPollIsRareWhileNotificationsArrive(t *testing.T) {
	m := pollOnlyMeta()
	if got := m.pollWait(); got != pgPollEvery {
		t.Fatalf("not listening: %v, want %v", got, pgPollEvery)
	}
	m.setListening(true)
	if !inHeardRange(t, m) {
		t.Fatalf("listening: %v", m.pollWait())
	}
	// The first look at a room is not a missed notification.
	if !m.noteRev("a", 3) || !inHeardRange(t, m) {
		t.Fatal("the first look must report the revision and keep the long wait")
	}
	// A change that was notified keeps the long wait.
	m.heard["a"]++
	if !m.noteRev("a", 4) || !inHeardRange(t, m) {
		t.Fatal("a notified change must keep the long wait")
	}
	if m.noteRev("a", 4) {
		t.Fatal("an unchanged revision has not moved")
	}
	// A change nobody notified: the connection loses notifications, the counters are read often.
	if !m.noteRev("a", 5) {
		t.Fatal("a moved revision must be reported")
	}
	if got := m.pollWait(); got != pgPollEvery {
		t.Fatalf("after a missed notification: %v, want %v", got, pgPollEvery)
	}
	// A connection made again is trusted again.
	m.setListening(false)
	if got := m.pollWait(); got != pgPollEvery {
		t.Fatalf("connection lost: %v, want %v", got, pgPollEvery)
	}
	m.setListening(true)
	if !inHeardRange(t, m) {
		t.Fatalf("listening again: %v", m.pollWait())
	}
}
