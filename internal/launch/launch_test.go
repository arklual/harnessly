package launch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/status"
)

func TestResolveCwd(t *testing.T) {
	root := t.TempDir()
	inside, other := filepath.Join(root, "proj"), t.TempDir()
	_ = os.MkdirAll(inside, 0o755)
	_ = os.Symlink(other, filepath.Join(root, "link"))
	_ = os.WriteFile(filepath.Join(root, "file"), nil, 0o600)
	for _, c := range []struct{ dir, root, want string }{
		{inside, "", "На этой машине не задан корневой каталог проектов"},
		{"rel", root, "Каталог запуска должен быть абсолютным путём"},
		{inside, filepath.Join(root, "none"), "Корневой каталог проектов недоступен"},
		{filepath.Join(root, "link"), root, "Каталог запуска вне корневого каталога проектов"},
		{filepath.Join(root, "file"), root, "Каталог запуска не найден"},
	} {
		if _, err := ResolveCwd(c.dir, c.root, nil); err == nil || err.Error() != c.want {
			t.Errorf("%s in %s: %v, want %s", c.dir, c.root, err, c.want)
		}
	}
	if got, err := ResolveCwd(filepath.Join(root, "link"), root, []string{other}); err != nil || got != other {
		t.Fatalf("%q %v", got, err)
	}
}

func TestHeadlinesReadOnlyWhatWasAppended(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s.jsonl")
	_ = os.WriteFile(file, []byte(`{"type":"user","message":{"content":"one"},"timestamp":"2026-10-17T10:00:00.000Z"}`+"\n"+`{"type":"ai-title","aiTitle":"T1"}`+"\n"+`{"type":"user","message":{"content":"partial`), 0o600)
	var h Headlines
	got := h.Read(file)
	if got.Title == nil || *got.Title != "T1" || got.LastPromptAt == nil || *got.LastPromptAt != 1792231200000 {
		t.Fatalf("%+v", got)
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`"},"timestamp":"2026-10-17T11:00:00.000Z"}` + "\n" + `{"type":"ai-title","aiTitle":"T2"}` + "\n")
	_ = f.Close()
	got = h.Read(file)
	if *got.Title != "T2" || *got.LastPromptAt != 1792234800000 {
		t.Fatalf("%v %v", *got.Title, *got.LastPromptAt)
	}
	if got := h.Read(filepath.Join(t.TempDir(), "none")); got.Title != nil || got.LastPromptAt != nil {
		t.Fatalf("%+v", got)
	}
}

type inboxKeeper struct {
	mu      sync.Mutex
	sources []status.AgentSource
}

func (k *inboxKeeper) RegisterTab(string, status.Entry) {}
func (k *inboxKeeper) MarkAgentLaunch(string, bool)     {}
func (k *inboxKeeper) AgentSources() []status.AgentSource {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]status.AgentSource(nil), k.sources...)
}

type inboxTmux struct{}

func (inboxTmux) HasSession(string) bool                 { return true }
func (inboxTmux) SessionsOfPane(string, string) []string { return nil }
func (inboxTmux) SessionCwd(string) string               { return "" }

func TestInboxWaitsForTheTurnToEnd(t *testing.T) {
	k := &inboxKeeper{sources: []status.AgentSource{{TabID: "t1", TmuxSession: "s1", CliState: status.Busy}}}
	var mu sync.Mutex
	var typed []string
	b := &Inbox{Starter: New(Config{Keeper: func() Keeper { return k }}), Tmux: inboxTmux{}, Every: time.Hour,
		Submit: func(s, text string) error { mu.Lock(); typed = append(typed, s+":"+text); mu.Unlock(); return nil }}
	for _, m := range []string{"a", "b"} {
		out, err := b.Deliver(Delivery{TabID: "t1", Message: m})
		if err != nil || out.String("mode") != "queued" {
			t.Fatalf("%v %v", out, err)
		}
	}
	b.Flush()
	if len(typed) != 0 {
		t.Fatalf("typed while busy: %v", typed)
	}
	k.mu.Lock()
	k.sources[0].CliState = status.Idle
	k.mu.Unlock()
	// Idle, but a message is still waiting: the new one queues behind it.
	if out, _ := b.Deliver(Delivery{TabID: "t1", Message: "c"}); out.String("mode") != "queued" {
		t.Fatal(out)
	}
	b.Flush()
	b.Flush()
	b.Flush()
	if strings.Join(typed, ",") != "s1:a,s1:b,s1:c" {
		t.Fatalf("%v", typed)
	}
	k.mu.Lock()
	k.sources[0].CliState = status.Busy
	k.mu.Unlock()
	_, _ = b.Deliver(Delivery{TabID: "t1", Message: "lost"})
	k.mu.Lock()
	k.sources = nil
	k.mu.Unlock()
	b.Flush()
	if b.queueLen("t1") != 0 {
		t.Fatal("a message for a gone tab is dropped")
	}
}
