package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectName(t *testing.T) {
	long := "/root/" + strings.Repeat("проект-", 40) + "😀/x"
	for in, want := range map[string]string{
		"/root/project":                "-root-project",
		"/tmp/a b/😀":                   "-tmp-a-b---",
		long:                           "-root" + strings.Repeat("-", 195) + "-fcah9y",
		"/" + strings.Repeat("a", 250): "-" + strings.Repeat("a", 199) + "-feo44x",
	} {
		if got := ProjectName(in); got != want {
			t.Errorf("%q:\n got %q\nwant %q", in, got, want)
		}
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func appendTo(t *testing.T, path, body string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(body); err != nil {
		t.Fatal(err)
	}
}

func TestClaudeList(t *testing.T) {
	home := t.TempDir()
	l := NewClaudeLister(home)
	dir := l.ProjectDir("/work/app")
	a := filepath.Join(dir, "a.jsonl")
	write(t, a, `{"type":"summary","timestamp":"2025-10-16T10:00:00.5Z"}`+"\n"+
		`{"type":"user","isMeta":true,"message":"skip me"}`+"\n"+
		`{"type":"user","message":{"content":[{"type":"image"},{"type":"text","text":"hello there"}]}}`+"\n"+
		`{"type":"assistant","message":"x"}`+"\n"+
		`{"type": "user","message":"second"}`+"\n")
	b := filepath.Join(dir, "b.jsonl")
	write(t, b, `{"type":"user","message":{"content":[null,{"type":"text","text":"lost"}]}}`+"\n"+
		`{"type":"user","isCompactSummary":true,"message":"compact"}`+"\n"+
		`{"type":"user","message":{"content":"`+strings.Repeat("я", 210)+`"}}`+"\n")
	write(t, filepath.Join(dir, "fork.jsonl"), `{"type":"user","message":"fork"}`+"\n")
	write(t, filepath.Join(dir, "notes.txt"), "x")
	if err := os.Mkdir(filepath.Join(dir, "d.jsonl"), 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	_ = os.Chtimes(b, old, old)

	list := l.List("/work/app", func(id string) bool { return id == "fork" })
	if len(list) != 2 || list[0].SessionID != "a" || list[1].SessionID != "b" {
		t.Fatalf("%+v", list)
	}
	if list[0].StartedAt != "2025-10-16T10:00:00.500Z" || list[0].FirstMessage != "hello there" || list[0].TurnCount != 2 {
		t.Fatalf("a: %+v", list[0])
	}
	if list[1].LastActivityAt != "2025-01-01T00:00:00.000Z" || list[1].TurnCount != 2 {
		t.Fatalf("b: %+v", list[1])
	}
	if want := strings.Repeat("я", 200) + "…"; list[1].FirstMessage != want {
		t.Fatalf("b message %q", list[1].FirstMessage)
	}
	if list[1].StartedAt == "" || !strings.HasSuffix(list[1].StartedAt, "Z") {
		t.Fatalf("b started at its birth time: %q", list[1].StartedAt)
	}

	// Appended lines are read from where the last read stopped; an unfinished line counts, and is read again whole.
	appendTo(t, a, `{"type":"user","message":"third"}`+"\n"+`{"type":"user","mess`)
	if got := l.List("/work/app", nil); got[0].SessionID != "a" || got[0].TurnCount != 4 || len(got) != 3 { // the unfinished line names a user
		t.Fatalf("after append: %+v", got)
	}
	if off := l.scans[a].offset; off == 0 {
		t.Fatal("offset not kept")
	}
	appendTo(t, a, `age":"fourth"}`+"\n")
	if got := l.List("/work/app", nil); got[0].TurnCount != 4 || got[0].FirstMessage != "hello there" {
		t.Fatalf("after the line ends: %+v", got[0])
	}
	// A file that shrank is read again from its start.
	write(t, a, `{"type":"user","message":"new"}`+"\n")
	if got := l.List("/work/app", nil); got[0].SessionID != "a" || got[0].TurnCount != 1 || got[0].FirstMessage != "new" {
		t.Fatalf("after rewrite: %+v", got[0])
	}
	if got := l.List("/nowhere", nil); got == nil || len(got) != 0 {
		t.Fatal("missing folder lists []")
	}
}

func TestClaudeFirstLineOnlyCarriesStart(t *testing.T) {
	home := t.TempDir()
	l := NewClaudeLister(home)
	p := filepath.Join(l.ProjectDir("/x"), "s.jsonl")
	write(t, p, `{"type":"user","timestamp":1700000000000,"message":"hi"}`+"\r\n"+`{"timestamp":"2020-01-01T00:00:00Z"}`)
	got := l.List("/x", nil)
	if len(got) != 1 || got[0].StartedAt != "2023-11-14T22:13:20.000Z" || got[0].TurnCount != 1 || got[0].FirstMessage != "hi" {
		t.Fatalf("%+v", got)
	}
}

func TestCodexList(t *testing.T) {
	home := t.TempDir()
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	l := NewCodexLister(home)
	l.Now = func() time.Time { return now }
	today := CodexDayDir(l.Root, now)
	older := CodexDayDir(l.Root, now.AddDate(0, 0, -3))
	big := strings.Repeat(`{"type":"response_item","payload":{"type":"message"}}`+"\n", 30000) // past the 1 MiB tail
	write(t, filepath.Join(today, "rollout-a.jsonl"),
		`{"type":"session_meta","timestamp":"2026-10-17T10:00:00Z","payload":{"id":"id-a","cwd":"/w","model":null}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"<environment_context>x"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"  fix the bug  "}}`+"\n"+
			`{"type":"turn_context","payload":{"model":"gpt-old"}}`+"\n"+
			big+
			`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":30,"output_tokens":5}}}}`+"\n"+
			`{"type":"turn_context","payload":{"model":"gpt-new"}}`+"\n"+
			`{"type":"event_msg","payload":{"type":"user_message","message":"late"}}`+"\n")
	write(t, filepath.Join(older, "rollout-b.jsonl"),
		`{"type":"session_meta","payload":{"id":"id-b","cwd":"/w","timestamp":"2026-10-14T09:00:00.250Z","model":"m-b"}}`+"\n")
	write(t, filepath.Join(today, "rollout-c.jsonl"), `{"type":"session_meta","payload":{"id":"id-c","cwd":"/other","timestamp":"2026-10-17T01:00:00Z"}}`+"\n")
	write(t, filepath.Join(today, "rollout-d.jsonl"), "\n"+`{"type":"session_meta","payload":{"id":"id-d","cwd":"/w","timestamp":"2026-10-17T01:00:00Z"}}`+"\n")
	write(t, filepath.Join(today, "rollout-e.jsonl"), `{"type":"session_meta","payload":{"id":"id-e","cwd":"/w"}}`+"\n")
	write(t, filepath.Join(today, "notes.txt"), "x")
	write(t, filepath.Join(CodexDayDir(l.Root, now.AddDate(0, 0, -40)), "rollout-f.jsonl"),
		`{"type":"session_meta","payload":{"id":"id-f","cwd":"/w","timestamp":"2026-09-01T00:00:00Z"}}`+"\n")
	ta := time.Date(2026, 10, 17, 11, 0, 0, 123456789, time.UTC)
	_ = os.Chtimes(filepath.Join(today, "rollout-a.jsonl"), ta, ta)
	tb := ta.Add(-time.Hour)
	_ = os.Chtimes(filepath.Join(older, "rollout-b.jsonl"), tb, tb)

	list, dirs, files := l.List("/w", 30)
	if dirs != 2 || files != 5 {
		t.Fatalf("dirs %d files %d", dirs, files)
	}
	if len(list) != 2 || list[0].SessionID != "id-a" || list[1].SessionID != "id-b" {
		t.Fatalf("%+v", list)
	}
	a := list[0]
	if a.StartedAt != 1792231200000 || a.LastActivityAt != 1792234800123.4568 || a.TurnCount != 2 || a.Model != "gpt-new" {
		t.Fatalf("a: %+v", a)
	}
	if a.FirstUserMessage == nil || *a.FirstUserMessage != "fix the bug" || a.TotalTokens == nil || *a.TotalTokens != 75 {
		t.Fatalf("a: %+v", a)
	}
	b := list[1]
	if b.StartedAt != 1791968400250 || b.Model != "m-b" || b.TotalTokens != nil || b.FirstUserMessage != nil {
		t.Fatalf("b: %+v", b)
	}
	if list, _, _ := l.List("/w", 2); len(list) != 1 {
		t.Fatalf("two days: %+v", list)
	}
	if id, ok := CodexFirstLine(filepath.Join(today, "rollout-a.jsonl")); !ok || id != "id-a" {
		t.Fatal("first line")
	}
	if _, ok := CodexFirstLine(filepath.Join(today, "rollout-d.jsonl")); ok {
		t.Fatal("an empty first line is no meta")
	}
}

func TestForkIDsRunsClean(t *testing.T) {
	if ForkIDs() == nil {
		t.Fatal("nil")
	}
}
