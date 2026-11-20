package feed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcript"
	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestAntigravityIndexedSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	s := &AntigravitySource{Root: filepath.Dir(path), Path: path, Index: ix}
	defer s.Close()
	s.Tail(100)
	line := `{"source":"MODEL","type":"PLANNER_RESPONSE","content":"hello","status":"DONE"}` + "\n"
	appendFile(t, path, line, os.O_APPEND)
	inc := s.Since()
	if len(inc.NewEntries) != 1 || inc.NewEntries[0].String("markdown") != "hello" {
		t.Fatal(inc)
	}
	if again := s.Since(); len(again.NewEntries) != 0 {
		t.Fatal("indexed replay", again)
	}
}

func TestAntigravitySourcePartialAndTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	user := `{"source":"USER_EXPLICIT","type":"USER_INPUT","content":"<USER_REQUEST>hi</USER_REQUEST>"}` + "\n"
	reply := `{"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"hello"}` + "\n"
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(user + reply[:20])
	s := &AntigravitySource{Root: filepath.Dir(path), Path: path}
	if c := s.Tail(100); len(c.Entries) != 1 {
		t.Fatal(c)
	}
	if inc := s.Since(); len(inc.NewEntries) != 0 {
		t.Fatal(inc)
	}
	write(user + reply)
	if inc := s.Since(); len(inc.NewEntries) != 1 {
		t.Fatal(inc)
	}
	if inc := s.Since(); len(inc.NewEntries) != 0 {
		t.Fatal("replay", inc)
	}
	write(user)
	if inc := s.Since(); len(inc.NewEntries) != 1 || inc.NewOffset != int64(len(user)) {
		t.Fatal("truncate", inc)
	}
}

func TestAntigravityDoneDoesNotEndTurnOrSettleTool(t *testing.T) {
	content := `{"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","content":"<USER_REQUEST>draw</USER_REQUEST>"}` + "\n" +
		`{"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"generate_image","args":{"Prompt":"mug"}}]}` + "\n" +
		`{"source":"MODEL","type":"GENERIC","status":"DONE","content":"Generated image","media":[{"mime_type":"image/jpeg","uri":"file:///tmp/mug.jpg"}]}` + "\n" +
		`{"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"Done"}` + "\n"
	b := newBuilder("test", wire.Provider("antigravity"))
	entries := transcript.ParseAntigravityContent(content)
	for _, e := range entries {
		step := b.add(e, 0)
		for _, sig := range step.signals {
			if sig.K == SignalTurnEnded {
				t.Fatal("native step ended whole turn")
			}
		}
	}
	if b.open == nil {
		t.Fatal("turn became idle")
	}
	if len(b.entries) != 4 {
		t.Fatalf("entries: %d", len(b.entries))
	}
	if b.entries[1].OK != nil || b.entries[2].OK != nil {
		t.Fatal("synthetic successful tool completion")
	}
	if img := b.entries[2].Images; img == nil || len(img.Done) != 1 || img.Done[0] != "/tmp/mug.jpg" {
		t.Fatal(img)
	}
	// Replaying native bytes cannot create duplicate entries.
	for _, e := range entries {
		b.add(e, 0)
	}
	if len(b.entries) != 4 {
		t.Fatal("replay duplicated entries")
	}
}
