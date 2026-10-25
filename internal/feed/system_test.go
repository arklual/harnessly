package feed

import (
	"encoding/json"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestHarnessLinesAreSystemNotes(t *testing.T) {
	cases := []struct{ name, text, want string }{
		{"goal check-in", "<task-notification>\n<summary>Goal check-in: background work still running</summary>\n</task-notification>",
			"Goal check-in: background work still running"},
		{"check-in with a reminder", "<task-notification>\n<summary>Goal check-in: background work still running</summary>\n</task-notification>\n" +
			"<system-reminder>\nThe goal is not met yet.\n</system-reminder>",
			"Goal check-in: background work still running\nThe goal is not met yet."},
		{"wrapped in a reminder", "<system-reminder><task-notification><summary>Build done</summary></task-notification></system-reminder>",
			"Build done"},
		{"unknown fields", "<task-notification>\n<task-id>b9</task-id>\n<output-file>/tmp/b9.out</output-file>\n<summary>Monitor fired</summary>\n<foo>bar</foo>\n</task-notification>",
			"Monitor fired\ntask-id: b9\noutput-file: /tmp/b9.out\nfoo: bar"},
		{"no summary", "<task-notification>\n<status>killed</status>\n</task-notification>", "status: killed"},
		{"reminder alone", "<system-reminder>\nToday is Sunday.\nKeep going.\n</system-reminder>", "Today is Sunday.\nKeep going."},
		{"system notification", "[SYSTEM NOTIFICATION - NOT USER INPUT]\nThe background shell exited with code 0.\nOutput saved.",
			"The background shell exited with code 0.\nOutput saved."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := systemNote(c.text)
			if !ok || got != c.want {
				t.Fatalf("got %v %q, want %q", ok, got, c.want)
			}
		})
	}
}

func TestThePersonsWordsAreNotSystemNotes(t *testing.T) {
	for _, text := range []string{
		"Посмотри, почему <task-notification> не парсится",
		"Ответь на <system-reminder>x</system-reminder> выше",
		"Вот лог: [SYSTEM NOTIFICATION - NOT USER INPUT] пришёл дважды",
		"",
	} {
		if note, ok := systemNote(text); ok {
			t.Fatalf("%q: %q", text, note)
		}
	}
}

// The goal check-in opens a turn as a quiet notice, not as the person's message, and the outbox never sees it.
func TestAGoalCheckInOpensATurnAsASystemNote(t *testing.T) {
	content, _ := json.Marshal("<task-notification>\n<summary>Goal check-in: background work still running</summary>\n</task-notification>")
	text := conversation(1) +
		`{"type":"system","subtype":"turn_duration","durationMs":1000,"timestamp":"2026-10-12T10:00:01.000Z"}` + "\n" +
		`{"type":"user","uuid":"00000000-0000-4000-8000-0000000000bb","timestamp":"2026-10-12T10:01:00.000Z","message":{"role":"user","content":` + string(content) + `}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-10-12T10:01:01.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Still waiting on CI"}]}}` + "\n"
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	u := f.Load()
	validate(t, f)
	if len(f.b.turns) != 2 {
		t.Fatalf("turns:\n%s", shape(f))
	}
	woke := f.b.turns[1].rec
	opener := f.b.entries[f.b.byID[woke.ID]]
	if opener.T != wire.EntryNotice || opener.Code != systemNoteCode || opener.Detail == nil ||
		*opener.Detail != "Goal check-in: background work still running" {
		t.Fatalf("opener: %+v", opener)
	}
	if woke.StepCount != 1 {
		t.Fatalf("the opener counted as a step: %+v", woke)
	}
	for _, e := range f.b.entries {
		if e.T == wire.EntryUser && e.ID == opener.ID {
			t.Fatal("the check-in is a user entry")
		}
	}
	for _, s := range u.Signals {
		if s.K == SignalUser && s.EntryID == opener.ID {
			t.Fatal("the check-in went to the outbox")
		}
	}
}
