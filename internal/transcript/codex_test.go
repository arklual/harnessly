package transcript

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCodexImageURL(t *testing.T) {
	up := NewCodexImageURL("/home/agent/.runtime/uploads/", "")
	cases := map[string]string{
		"/home/agent/.runtime/uploads/w/a b.png":     "/api/uploads/w/a%20b.png",
		"/home/agent/.runtime/uploads/w/x.JPG":       "/api/uploads/w/x.JPG",
		"/home/agent/.runtime/uploads/w/../w2/x.gif": "/api/uploads/w2/x.gif",
		"/home/agent/.runtime/uploads/w/x.txt":       "",
		"/home/agent/.runtime/uploads":               "",
		"/home/agent/.runtime/uploads-other/x.png":   "",
		"/home/agent/.runtime/uploads/x\x00.png":     "",
	}
	for in, want := range cases {
		if got := up(in); got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
	if got := NewCodexImageURL("/u", "/api")("/u/x.png"); got != "/api/uploads/x.png" {
		t.Errorf("a prefix the path already has is not repeated: %q", got)
	}
}

func TestCodexStatsCache(t *testing.T) {
	prev := Now
	t.Cleanup(func() { Now = prev; clearCodexStatsCache() })
	now := 1_000_000.0
	Now = func() float64 { return now }
	clearCodexStatsCache()

	path := filepath.Join(t.TempDir(), "s.jsonl")
	meta := `{"type":"session_meta","payload":{"id":"one","timestamp":"2026-01-01T00:00:00Z"}}` + "\n"
	if err := os.WriteFile(path, []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	mtime := time.Unix(1_700_000_000, 0)
	_ = os.Chtimes(path, mtime, mtime)
	if s := CodexTimelineStats(path); s == nil || s.String("sessionId") != "one" {
		t.Fatalf("stats: %v", s)
	}

	// Within the window and with the same mtime the earlier read stands, as in the previous runtime.
	_ = os.WriteFile(path, []byte(`{"type":"session_meta","payload":{"id":"two","timestamp":"2026-01-01T00:00:00Z"}}`+"\n"), 0o644)
	_ = os.Chtimes(path, mtime, mtime)
	now += 1000
	if s := CodexTimelineStats(path); s.String("sessionId") != "one" {
		t.Fatalf("cached read expected, got %v", s.String("sessionId"))
	}
	if r, _ := CodexTimelineStats(path).Number("receivedAt"); r != now {
		t.Fatalf("receivedAt is the time of the call: %v", r)
	}
	now += codexStatsTTL
	if s := CodexTimelineStats(path); s.String("sessionId") != "two" {
		t.Fatalf("expired cache not re-read: %v", s.String("sessionId"))
	}
}

func TestCodexParserDispose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.jsonl")
	line := `{"timestamp":"2026-01-01T00:00:00Z","type":"event_msg","payload":{"type":"exec_command_begin","call_id":"c","command":"x"}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewCodexParser(path, CodexOptions{})
	if r := p.ParseIncremental(); len(r.NewEntries) != 0 || r.NewOffset != int64(len(line)) || p.Offset() != r.NewOffset {
		t.Fatalf("incremental: %+v", r)
	}
	if got := len(p.FlushStale(1)); got != 2 {
		t.Fatalf("a call in flight ends as a notice and its entry, got %d", got)
	}
	if got := len(p.FlushStale(2)); got != 0 {
		t.Fatalf("flushed once, got %d", got)
	}
	_ = os.WriteFile(path, []byte(line+line), 0o644)
	p.ParseIncremental()
	p.Dispose()
	if got := len(p.FlushStale(3)); got != 0 {
		t.Fatalf("dispose forgets calls in flight, got %d", got)
	}
	if entries, off := p.ParseAll(); len(entries) != 0 || off != int64(2*len(line)) {
		t.Fatalf("parse all: %d %d", len(entries), off)
	}
}
