package paneintents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func cmd(pairs ...any) *jsvalue.Object { return jsvalue.NewObject(pairs...) }

// Ported from the previous runtime's file intent store tests.
func TestFirstApplicationOnly(t *testing.T) {
	now := time.UnixMilli(1000)
	s := New(filepath.Join(t.TempDir(), "a.json"), func() time.Time { return now }, false)
	intent := s.Add("tab-1", cmd("kind", "open", "tab", "task", "target", "TCK-1"))
	if len(s.List("tab-2")) != 0 {
		t.Fatal("other tab")
	}
	now = time.UnixMilli(2000)
	first := s.MarkApplied("tab-1", []string{intent.String("id")}, "desktop")
	if at, _ := first[0].Number("appliedAt"); at != 2000 || first[0].String("appliedOn") != "desktop" {
		t.Fatal(jsvalue.Stringify(first[0], ""))
	}
	now = time.UnixMilli(3000)
	again := s.MarkApplied("tab-1", []string{intent.String("id"), "unknown"}, "phone")
	if at, _ := again[0].Number("appliedAt"); at != 2000 || again[0].String("appliedOn") != "desktop" {
		t.Fatal(jsvalue.Stringify(again[0], ""))
	}
	if string(jsvalue.Stringify(intent, "")) != `{"id":"`+intent.String("id")+`","command":{"kind":"open","tab":"task","target":"TCK-1"},"createdAt":1000,"appliedAt":null,"appliedOn":null}` {
		t.Fatal(string(jsvalue.Stringify(intent, "")))
	}
}

func TestForgetsADayOld(t *testing.T) {
	now := time.UnixMilli(0)
	s := New(filepath.Join(t.TempDir(), "b.json"), func() time.Time { return now }, false)
	s.Add("tab-1", cmd("kind", "show"))
	now = now.Add(TTL + time.Millisecond)
	if len(s.List("tab-1")) != 0 {
		t.Fatal("kept")
	}
}

func TestSurvivesARestart(t *testing.T) {
	file := filepath.Join(t.TempDir(), "c.json")
	s := New(file, nil, true)
	intent := s.Add("tab-1", cmd("kind", "open", "tab", "browser", "target", "localhost:4400"))
	deadline := time.Now().Add(3 * time.Second)
	for {
		raw, _ := os.ReadFile(file)
		if strings.Contains(string(raw), intent.String("id")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("not saved")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st, _ := os.Stat(file); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	got := New(file, nil, true).List("tab-1")
	if len(got) != 1 || !jsvalue.Equal(got[0], intent) {
		t.Fatal(got)
	}
}

func TestWaitForApplied(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "d.json"), nil, false)
	intent := s.Add("tab-1", cmd("kind", "show"))
	done := make(chan bool)
	go func() { done <- s.WaitForApplied("tab-1", intent.String("id"), time.Second) }()
	time.Sleep(20 * time.Millisecond)
	s.MarkApplied("tab-1", []string{intent.String("id")}, "desktop")
	if !<-done {
		t.Fatal("not told")
	}
	other := s.Add("tab-1", cmd("kind", "show"))
	if s.WaitForApplied("tab-1", other.String("id"), 20*time.Millisecond) {
		t.Fatal("told without an application")
	}
}

func intent(id string, at int64) *jsvalue.Object {
	return jsvalue.NewObject("id", id, "command", cmd("kind", "open", "tab", "files"), "createdAt", at, "appliedAt", nil, "appliedOn", nil)
}

// Ported from the previous runtime's pane intent tests (the runtime's part).
func TestPruneRecogniseDescribe(t *testing.T) {
	now := time.UnixMilli(1_760_000_000_000)
	var many []*jsvalue.Object
	for i := 0; i < PerTab+5; i++ {
		many = append(many, intent(string(rune('a'+i)), now.UnixMilli()-int64(i)))
	}
	if len(Prune(many, now)) != PerTab {
		t.Fatal("per tab")
	}
	if len(Prune([]*jsvalue.Object{intent("old", now.Add(-TTL).UnixMilli()-1)}, now)) != 0 {
		t.Fatal("ttl")
	}
	if !IsIntent(intent("a", 1)) {
		t.Fatal("intent")
	}
	bad := intent("a", 1)
	bad.Set("command", cmd("kind", "exec"))
	tv := intent("a", 1)
	tv.Set("appliedOn", "tv")
	if IsIntent(bad) || IsIntent(tv) || IsIntent("x") {
		t.Fatal("not intents")
	}
	seen := intent("b", now.UnixMilli())
	seen.Set("appliedAt", now.UnixMilli())
	seen.Set("appliedOn", "phone")
	lines := Describe([]*jsvalue.Object{intent("a", now.UnixMilli()), seen})
	if !strings.Contains(lines[0], "ждёт") || !strings.Contains(lines[1], "открыто ") || !strings.HasSuffix(lines[1], " на телефоне") {
		t.Fatal(lines)
	}
	// 2025-10-09T08:53:20Z is 11:53 in Moscow.
	if !strings.HasPrefix(lines[0], "11:53 открыть «files»") {
		t.Fatal(lines[0])
	}
}
