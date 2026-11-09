package agentbrowser

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
)

func TestLifecycleRejectsUnknownAndInvalidProfiles(t *testing.T) {
	data := t.TempDir()
	m := New(Config{DataDir: data, LookupProfile: func(string) (Profile, bool) { return Profile{}, false }})
	defer m.Close()
	for _, id := range []string{"../outside", "", "unknown"} {
		if err := m.StopProfile(t.Context(), id); err == nil {
			t.Fatalf("stop %q accepted", id)
		}
		if err := m.ClearProfile(t.Context(), id); err == nil {
			t.Fatalf("clear %q accepted", id)
		}
	}
	entries, err := os.ReadDir(data)
	must(t, err)
	if len(entries) != 0 {
		t.Fatal("invalid profile accessed disk")
	}
}

func TestLifecycleAttachedConfiguration(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), CDP: "http://legacy.invalid", LookupProfile: func(id string) (Profile, bool) {
		switch id {
		case "managed":
			return Profile{ID: id, Kind: "managed"}, true
		case "attached":
			return Profile{ID: id, Kind: "attached", CDP: "http://profile.invalid"}, true
		}
		return Profile{}, false
	}, ProfileOf: func(string) string { return "attached" }})
	defer m.Close()
	for _, id := range []string{"managed", "attached"} {
		p, err := m.lookupProfile(id)
		must(t, err)
		if id == "managed" && p.CDP != "" {
			t.Fatal("managed profile used global CDP")
		}
		if id == "attached" && p.CDP != "http://profile.invalid" {
			t.Fatal("attached profile ignored own CDP")
		}
	}
	if s := m.ReadyFor(t.Context(), "chat"); s.State != "up" {
		t.Fatal(s)
	}
	if s := m.RetryFor(t.Context(), "chat"); s.State != "up" {
		t.Fatal(s)
	}
	if m.preparation != nil {
		t.Fatal("attached readiness started provisioning")
	}
	if err := m.ClearProfile(t.Context(), "attached"); err == nil {
		t.Fatal("cleared external browser")
	}
	must(t, m.StopProfile(t.Context(), "attached"))
}

func TestLifecycleIdleAccounting(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	now := time.Now()
	i.lastActivity = now.Add(-time.Hour)
	if i.idleFor(now) < time.Hour {
		t.Fatal("idle duration")
	}
	must(t, i.beginOperation())
	if i.idleFor(now.Add(time.Hour)) != 0 {
		t.Fatal("active call became idle")
	}
	i.endOperation()
	if i.idleFor(time.Now()) > time.Second {
		t.Fatal("completion did not refresh activity")
	}
	i.pages["page"] = newPage(Tab{ID: "page"})
	i.pages["page"].viewers[&Viewer{}] = true
	i.lastActivity = now.Add(-time.Hour)
	if i.idleFor(now) != 0 {
		t.Fatal("viewer became idle")
	}
	for _, tt := range []struct {
		value string
		want  time.Duration
	}{{"", DefaultIdle}, {"0", 0}, {"0s", 0}, {"off", 0}, {"30m", 30 * time.Minute}, {"-1s", 0}, {"invalid", DefaultIdle}} {
		if got := browserIdle(tt.value); got != tt.want {
			t.Fatalf("idle %q: %v", tt.value, got)
		}
	}
}

func TestLifecycleSandboxPreferenceOverridesEnvironment(t *testing.T) {
	for _, value := range []bool{false, true} {
		extra := []string{"--no-sandbox=true", "--disable-setuid-sandbox", "--fixture"}
		want := []string{"--fixture"}
		if value {
			want = append(want, "--no-sandbox")
		}
		if got := provision.SandboxArgs(extra, &value); !reflect.DeepEqual(got, want) {
			t.Fatal(got)
		}
		if got := provision.SandboxArgs(extra, nil); !reflect.DeepEqual(got, extra) {
			t.Fatal("legacy args changed")
		}
	}
}

func TestLifecycleCloseDormantRegistryWithoutChrome(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), Chrome: "unavailable"})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	i.pages["a"] = newPage(Tab{ID: "a", Conversation: "chat", URL: "https://example.org", Order: 1})
	i.pages["b"] = newPage(Tab{ID: "b", Conversation: "other", URL: "https://example.com", Order: 2})
	i.changedLocked()
	i.save()
	if !m.ConversationHasTabs("chat") {
		t.Fatal("missing dormant tabs")
	}
	must(t, m.CloseConversation(context.Background(), "chat"))
	if m.ConversationHasTabs("chat") || !m.ConversationHasTabs("other") {
		t.Fatal("registry cleanup failed")
	}
	if m.preparation != nil || m.ProfileRunning(DefaultProfile) {
		t.Fatal("cleanup started Chrome")
	}
	if _, err := os.Stat(filepath.Join(i.dir, "chrome")); !os.IsNotExist(err) {
		t.Fatal("cleanup created Chrome data")
	}
}
