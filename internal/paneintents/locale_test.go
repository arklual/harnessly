package paneintents

import (
	"os"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

func TestMain(m *testing.M) {
	// The goldens are the previous runtime's answers, which were in Russian.
	os.Setenv("RUNTIME_LOCALE", "ru")
	os.Exit(m.Run())
}

func TestEnglishDescribe(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "en")
	now := time.UnixMilli(1_760_000_000_000)
	seen := intent("b", now.UnixMilli())
	seen.Set("appliedAt", now.UnixMilli())
	seen.Set("appliedOn", "phone")
	lines := Describe([]*jsvalue.Object{intent("a", now.UnixMilli()), seen})
	// 2025-10-09T08:53:20Z is 11:53 in Moscow.
	if lines[0] != `11:53 open "files" — waiting, it will open when the person comes to the conversation` {
		t.Errorf("waiting: %q", lines[0])
	}
	if lines[1] != `11:53 open "files" — opened 11:53 on the phone` {
		t.Errorf("applied: %q", lines[1])
	}
	if got := describeCommand(cmd("kind", "show")); got != "show the pane" {
		t.Errorf("show: %q", got)
	}
}
