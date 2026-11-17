package journal

import (
	"sync"
	"testing"
)

// A sink sees what was written: every line when All, else warnings and errors only.
func TestSinks(t *testing.T) {
	j := Open(t.TempDir(), Options{})
	defer j.Close()
	var mu sync.Mutex
	var all, bad []string
	j.AddSink(Sink{All: true, Entry: func(e Entry) { mu.Lock(); all = append(all, e.Event); mu.Unlock() }})
	j.AddSink(Sink{Entry: func(e Entry) { mu.Lock(); bad = append(bad, e.Event); mu.Unlock() }})
	j.Add(Entry{Level: Info, Source: SourceRuntime, Event: "a"})
	j.Add(Entry{Level: Error, Source: SourceWeb, Event: "b"})
	j.Flush()
	mu.Lock()
	defer mu.Unlock()
	if len(all) != 2 || len(bad) != 1 || bad[0] != "b" {
		t.Fatalf("all %v, warnings and errors %v", all, bad)
	}
}
