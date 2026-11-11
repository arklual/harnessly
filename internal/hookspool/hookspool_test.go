package hookspool

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func line(id, event string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"event":%q,"session":"s-1"}`, id, event))
}

func TestEventsWrittenWhileNobodyReadAreAppliedInOrderOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	for i, ev := range []string{"prompt-submit", "notification", "stop"} {
		if err := Append(path, line(fmt.Sprint("e", i), ev)); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	q := &Queue{Path: path, Apply: func(e map[string]any) { got = append(got, e["event"].(string)) }}
	if n := q.Drain(); n != 3 || fmt.Sprint(got) != "[prompt-submit notification stop]" {
		t.Fatalf("applied %d: %v", n, got)
	}
	if st, err := os.Stat(path); err != nil || st.Size() != 0 {
		t.Fatalf("the spool is not emptied: %v %v", st, err)
	}
	if n := q.Drain(); n != 0 || len(got) != 3 {
		t.Fatalf("a second read applied %d", n)
	}
}

func TestADeliveredEventIsNotAppliedAgainFromTheSpool(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	var got []string
	q := &Queue{Path: path, Apply: func(e map[string]any) { got = append(got, e["id"].(string)) }}
	// The hook wrote the event and delivered it; the missed one before it goes first.
	_ = Append(path, line("missed", "prompt-submit"))
	_ = Append(path, line("now", "stop"))
	if !q.Deliver("now", map[string]any{"id": "now", "event": "stop"}) {
		t.Fatal("not delivered")
	}
	if fmt.Sprint(got) != "[missed now]" {
		t.Fatalf("%v", got)
	}
	// The same event again, by the request and by the file: nothing more.
	_ = Append(path, line("now", "stop"))
	q.Deliver("now", map[string]any{"id": "now", "event": "stop"})
	q.Drain()
	if fmt.Sprint(got) != "[missed now]" {
		t.Fatalf("applied twice: %v", got)
	}
	// An event the hook could not write is applied from the request alone.
	q.Deliver("unwritten", map[string]any{"id": "unwritten"})
	if fmt.Sprint(got) != "[missed now unwritten]" {
		t.Fatalf("%v", got)
	}
}

func TestEventsWaitInTheSpoolUntilTheyCanBeApplied(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	ready := false
	var got []string
	q := &Queue{Path: path, Ready: func() bool { return ready }, Apply: func(e map[string]any) { got = append(got, e["id"].(string)) }}
	_ = Append(path, line("a", "stop"))
	if q.Deliver("a", map[string]any{"id": "a"}) || q.Drain() != 0 || len(got) != 0 {
		t.Fatalf("applied before it was ready: %v", got)
	}
	ready = true
	if q.Drain() != 1 || fmt.Sprint(got) != "[a]" {
		t.Fatalf("%v", got)
	}
}

func TestABrokenLineDoesNotStopTheRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), File)
	_ = os.WriteFile(path, []byte("{\"id\":\"a\"}\n{\"id\":\"cut\n{\"id\":\"b\"}\n"), 0o600)
	var got []string
	q := &Queue{Path: path, Apply: func(e map[string]any) { got = append(got, e["id"].(string)) }}
	if q.Drain() != 2 || fmt.Sprint(got) != "[a b]" {
		t.Fatalf("%v", got)
	}
}
