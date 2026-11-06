package agentbrowser

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRegistryCreationOrderAndSelection(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	for _, tab := range []Tab{
		{ID: "z", URL: "about:blank", Conversation: "chat", Order: 1},
		{ID: "x", URL: "about:blank", Conversation: "other", Order: 2},
		{ID: "m", URL: "about:blank", Conversation: "chat", Order: 3},
		{ID: "a", URL: "about:blank", Conversation: "chat", Order: 4},
		{ID: "dev", URL: "devtools://devtools", Conversation: "chat", Order: 5},
		{ID: "ext", URL: "chrome-extension://fixture", Conversation: "chat", Order: 6},
		{ID: "side", URL: "chrome://side-panel.top-chrome/", Conversation: "chat", Order: 7},
	} {
		i.pages[tab.ID] = newPage(tab)
	}
	ids := func(tabs []Tab) []string {
		out := []string{}
		for _, tab := range tabs {
			out = append(out, tab.ID)
		}
		return out
	}
	if got := ids(i.Tabs("")); !reflect.DeepEqual(got, []string{"z", "x", "m", "a"}) {
		t.Fatal(got)
	}
	if got := i.Current("chat"); got != "a" {
		t.Fatal(got)
	}
	must(t, i.SetAgentTab("chat", "m"))
	i.mu.Lock()
	i.removeLocked("m")
	i.mu.Unlock()
	if got := i.Current("chat"); got != "a" {
		t.Fatal(got)
	}
	i.mu.Lock()
	i.removeLocked("a")
	i.mu.Unlock()
	if got := i.Current("chat"); got != "z" {
		t.Fatal(got)
	}
	i.save()
	restored := newInstance(m, DefaultProfile)
	if got := ids(restored.Tabs("")); !reflect.DeepEqual(got, []string{"z", "x"}) {
		t.Fatal(got)
	}
	if got := restored.Current("chat"); got != "z" {
		t.Fatal(got)
	}
	t.Log("creation order, same-chat neighbor, fallback, hidden service URLs and persistence verified")
}

func TestStartupReasonAndBackoff(t *testing.T) {
	log := "[FATAL] Socket path too long: /private/profile/SingletonSocket?token=fixture\ncleanup\n"
	reason := startupReason(log, "CDP connection closed")
	if !strings.Contains(reason, "Socket path too long") || strings.Contains(reason,
		"/private") || strings.Contains(reason, "token") {
		t.Fatal(reason)
	}
	delay := time.Second
	got := []time.Duration{}
	for n := 0; n < 8; n++ {
		delay = retryDelay(delay, false)
		got = append(got, delay)
		delay *= 2
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second,
		8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second,
		30 * time.Second}
	if !reflect.DeepEqual(got, want) || retryDelay(30*time.Second, true) != time.Second {
		t.Fatal(got)
	}
	t.Logf("retry delays=%v; stable connection resets to 1s; diagnostic=%q", got, reason)
}

func TestTargetInfoEventUpdatesWithoutChrome(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	i.pages["page"] = newPage(Tab{ID: "page", URL: "about:blank", Order: 1})
	i.event("Target.targetInfoChanged", "", []byte(`{"targetInfo":{
 "targetId":"page","type":"page","title":"updated","url":"https://example.org/"
}}`))
	tab := i.Tabs("")[0]
	if tab.Title != "updated" || tab.URL != "https://example.org/" || i.writes != 0 {
		t.Fatalf("event update: %+v writes=%d", tab, i.writes)
	}
	t.Log("targetInfoChanged updated metadata without a CDP connection or synchronous disk write")
}

func TestDiagnosticRedactsUnknownFailure(t *testing.T) {
	reason := startupReason("[FATAL:engine.cc:7] renderer launch failed at /private/profile/socket "+
		"token=fixture https://example.org/private",
		"closed")
	if !strings.Contains(reason, "renderer launch failed") || strings.Contains(reason,
		"/private") || strings.Contains(reason, "fixture") || strings.Contains(reason,
		"example.org") {
		t.Fatal(reason)
	}
	t.Log(reason)
}
