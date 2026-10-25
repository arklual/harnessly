package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClaudeRecentAcrossFolders(t *testing.T) {
	l := NewClaudeLister(t.TempDir())
	user := func(cwd, text string) string {
		return `{"type":"user","cwd":"` + cwd + `","timestamp":"2026-10-20T10:00:00Z","message":{"content":"` + text + `"}}` + "\n"
	}
	old := filepath.Join(l.ProjectDir("/work/app"), "00000001-0000-4000-8000-000000000000.jsonl")
	write(t, old, user("/work/app", "first ask")+`{"type":"assistant","message":"x"}`+"\n")
	named := filepath.Join(l.ProjectDir("/work/lib"), "00000002-0000-4000-8000-000000000000.jsonl")
	write(t, named, user("/work/lib", "build it")+`{"type":"ai-title","aiTitle":"Build the lib","sessionId":"00000002-0000-4000-8000-000000000000"}`+"\n"+
		`{"type":"ai-title","aiTitle":"Build the library","sessionId":"00000002-0000-4000-8000-000000000000"}`+"\n")
	write(t, filepath.Join(l.ProjectDir("/work/lib"), "00000003-0000-4000-8000-000000000000.jsonl"), `{"type":"summary","cwd":"/work/lib"}`+"\n")
	write(t, filepath.Join(l.ProjectDir("/work/lib"), "00000004-0000-4000-8000-000000000000.jsonl"), user("/work/lib", "side question"))
	write(t, filepath.Join(l.ProjectDir("/work/lib"), "notes.txt"), "x")
	base := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for i, p := range []string{old, named} {
		ts := base.Add(time.Duration(i) * time.Hour)
		_ = os.Chtimes(p, ts, ts)
	}

	got := l.Recent(func(id string) bool { return id == "00000004-0000-4000-8000-000000000000" }, 50)
	if len(got) != 2 || got[0].SessionID != "00000002-0000-4000-8000-000000000000" || got[1].SessionID != "00000001-0000-4000-8000-000000000000" {
		t.Fatalf("%+v", got)
	}
	if got[0].Title != "Build the library" || got[0].Cwd != "/work/lib" || got[0].Turns != 1 || !got[0].UpdatedAt.Equal(base.Add(time.Hour)) {
		t.Fatalf("named: %+v", got[0])
	}
	if got[1].Title != "first ask" || got[1].Cwd != "/work/app" || got[1].Path != old {
		t.Fatalf("old: %+v", got[1])
	}
	if one := l.Recent(func(id string) bool { return id == "00000004-0000-4000-8000-000000000000" }, 1); len(one) != 1 || one[0].SessionID != "00000002-0000-4000-8000-000000000000" {
		t.Fatalf("limit: %+v", one)
	}

	found, ok := l.Find("00000001-0000-4000-8000-000000000000")
	if !ok || found.Cwd != "/work/app" || found.Title != "first ask" {
		t.Fatalf("find: %+v %v", found, ok)
	}
	for _, id := range []string{"00000009-0000-4000-8000-000000000000", "", "..", "../old", "a/b", "old"} {
		if _, ok := l.Find(id); ok {
			t.Fatalf("found %q", id)
		}
	}
}

func TestCodexRecentAcrossFolders(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	l := NewCodexLister(t.TempDir())
	l.Now = func() time.Time { return now }
	today := CodexDayDir(l.Root, now)
	older := CodexDayDir(l.Root, now.AddDate(0, 0, -3))
	meta := func(id, cwd string) string {
		return `{"type":"session_meta","timestamp":"2026-10-17T10:00:00Z","payload":{"id":"` + id + `","cwd":"` + cwd + `"}}` + "\n"
	}
	ask := func(text string) string {
		return `{"type":"event_msg","payload":{"type":"user_message","message":"` + text + `"}}` + "\n"
	}
	write(t, filepath.Join(today, "rollout-2026-10-17T10-00-00-00000005-0000-4000-8000-000000000000.jsonl"), meta("00000005-0000-4000-8000-000000000000", "/w/a")+ask("fix the bug"))
	write(t, filepath.Join(older, "rollout-2026-10-14T10-00-00-00000006-0000-4000-8000-000000000000.jsonl"), meta("00000006-0000-4000-8000-000000000000", "/w/b")+ask("older"))
	write(t, filepath.Join(today, "rollout-2026-10-17T11-00-00-00000007-0000-4000-8000-000000000000.jsonl"), meta("00000007-0000-4000-8000-000000000000", "/w/c"))
	ta := time.Date(2026, 10, 17, 11, 0, 0, 0, time.UTC)
	_ = os.Chtimes(filepath.Join(today, "rollout-2026-10-17T10-00-00-00000005-0000-4000-8000-000000000000.jsonl"), ta, ta)
	tb := ta.Add(-72 * time.Hour)
	_ = os.Chtimes(filepath.Join(older, "rollout-2026-10-14T10-00-00-00000006-0000-4000-8000-000000000000.jsonl"), tb, tb)

	got := l.Recent(func(id string) bool { return false }, 50)
	if len(got) != 2 || got[0].SessionID != "00000005-0000-4000-8000-000000000000" || got[1].SessionID != "00000006-0000-4000-8000-000000000000" {
		t.Fatalf("%+v", got)
	}
	if got[0].Title != "fix the bug" || got[0].Cwd != "/w/a" || got[0].Turns != 1 {
		t.Fatalf("a: %+v", got[0])
	}
	if skipped := l.Recent(func(id string) bool { return id == "00000005-0000-4000-8000-000000000000" }, 50); len(skipped) != 1 || skipped[0].SessionID != "00000006-0000-4000-8000-000000000000" {
		t.Fatalf("skip: %+v", skipped)
	}
	f, ok := l.Find("00000006-0000-4000-8000-000000000000")
	if !ok || f.Cwd != "/w/b" || f.Title != "older" {
		t.Fatalf("find: %+v %v", f, ok)
	}
	if _, ok := l.Find("00000008-0000-4000-8000-000000000000"); ok {
		t.Fatal("found a missing one")
	}
}
