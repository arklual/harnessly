package agentbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestLoginRetryAndWait(t *testing.T) {
	q := NewLoginRequests()
	identity := ConversationIdentity{Project: "project", Title: "Chat"}
	x, fresh, err := q.open("chat", identity, "https://example.org/login", "Sign in")
	must(t, err)
	if !fresh {
		t.Fatal("first request reused")
	}
	again, fresh, err := q.open("chat", identity, "https://example.org/login", "Sign in")
	must(t, err)
	if fresh || again != x {
		t.Fatal("retry made another request")
	}
	// A second pending task in one chat must reuse the existing tab too.
	again, fresh, err = q.open("chat", identity, "https://other.example.org/login", "Other reason")
	must(t, err)
	if fresh || again != x {
		t.Fatal("parallel request bypassed existing login")
	}
	if answer := x.wait(t.Context(), time.Millisecond); answer.Status != "waiting" {
		t.Fatal(answer)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if answer := x.wait(ctx, time.Hour); answer.Status != "waiting" {
		t.Fatal(answer)
	}
	result := make(chan LoginAnswer, 2)
	go func() { result <- x.wait(t.Context(), time.Hour) }()
	go func() { result <- x.wait(t.Context(), time.Hour) }()
	x.settle("done", "https://example.org/account")
	for range 2 {
		select {
		case answer := <-result:
			raw, _ := json.Marshal(answer)
			if string(raw) != `{"status":"done","url":"https://example.org/account"}` {
				t.Fatal(string(raw))
			}
		case <-time.After(time.Second):
			t.Fatal("completion did not wake all retries")
		}
	}
	again, fresh, err = q.open("chat", identity, "https://example.org/login", "Sign in")
	must(t, err)
	if fresh || again != x {
		t.Fatal("completion was not remembered")
	}
	other, fresh, err := q.open("other", identity, "https://example.org/login", "Sign in")
	must(t, err)
	if !fresh || other == x {
		t.Fatal("another chat reused login")
	}
	x.mu.Lock()
	x.settled = time.Now().Add(-3 * time.Minute)
	x.mu.Unlock()
	again, fresh, err = q.open("chat", identity, "https://example.org/login", "Sign in")
	must(t, err)
	if !fresh || again == x {
		t.Fatal("old completion was not pruned")
	}
}

func TestLoginProtectionAndSettlement(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	x := &loginRequest{ask: LoginAsk{ID: "ask", Conv: "chat", Tab: "tab", Why: "Sign in"}, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "https://example.org/account", Control: "person"})
	p.login = x
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "")
	for _, method := range []string{"Runtime.evaluate", "Page.captureScreenshot", "Accessibility.getFullAXTree", "Page.navigate", "Input.insertText", "Page.handleJavaScriptDialog", "Target.closeTarget", ""} {
		if err := i.AgentCheck("chat", p.ID, method); !errors.Is(err, ErrPersonControl) {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if ask := i.pendingLogin(p.ID); ask == nil || ask.Why != "Sign in" {
		t.Fatal(ask)
	}
	p.Title = "Private page title"
	p.Dialog = &Dialog{Type: "prompt", Message: "Private dialog", DefaultPrompt: "Private answer"}
	private, _ := json.Marshal(i.AgentTabs("chat"))
	if strings.Contains(string(private), "Private") {
		t.Fatalf("person-controlled page contents leaked: %s", private)
	}
	if i.Tabs("chat")[0].Title != "Private page title" {
		t.Fatal("agent filtering changed the panel metadata")
	}
	must(t, i.SetControl(p.ID, "agent"))
	if got := x.wait(t.Context(), 0); got.Status != "done" || got.URL != p.URL {
		t.Fatal(got)
	}
	if i.pendingLogin(p.ID) != nil {
		t.Fatal("settled banner survived")
	}
	must(t, i.AgentCheck("chat", p.ID, "Page.handleJavaScriptDialog"))
	x = &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p.login = x
	i.mu.Lock()
	i.removeLocked(p.ID)
	i.mu.Unlock()
	if got := x.wait(t.Context(), 0); got.Status != "cancelled" || got.URL != p.URL {
		t.Fatal(got)
	}
}

func TestLoginInputPolicyBeforeBrowserStartup(t *testing.T) {
	a := NewProfileAccess(OpenProfiles(t.TempDir(), ""), func(string) (ConversationIdentity, bool) { return ConversationIdentity{}, true })
	for _, target := range []string{"", "javascript:alert(1)", "file:///tmp/test", "data:text/html,login", "chrome://version", "https://user:pass@example.org", "about:blank#login"} {
		if _, err := a.RequestLogin(t.Context(), "chat", target, "Sign in"); err == nil || !strings.Contains(err.Error(), "URL refused") {
			t.Fatalf("%q: %v", target, err)
		}
	}
	if _, err := a.RequestLogin(t.Context(), "chat", "https://example.org", "   "); err == nil || err.Error() != "A reason is required" {
		t.Fatal(err)
	}
}

func TestLoginResultURLRedaction(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	value := "private value for login test"
	current := "https://example.org/account?value=" + url.QueryEscape(value)
	x := &loginRequest{ask: LoginAsk{ID: "ask", Conv: "chat", Tab: "tab"}, instance: i, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: current, Control: "person"})
	p.login = x
	i.pages[p.ID] = p
	connectHandoffFixture(t, i, p, "")
	i.RememberSecret(value)
	check := func(answer LoginAnswer) {
		t.Helper()
		if strings.Contains(answer.URL, url.QueryEscape(value)) || !strings.Contains(answer.URL, "[redacted]") {
			t.Fatal("login result URL was not redacted")
		}
	}
	check(x.wait(t.Context(), 0))
	must(t, i.SetControl(p.ID, "agent"))
	check(x.wait(t.Context(), 0))
	x = &loginRequest{ask: LoginAsk{Tab: p.ID}, instance: i, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	p.login = x
	i.mu.Lock()
	i.removeLocked(p.ID)
	i.mu.Unlock()
	check(x.wait(t.Context(), 0))
}
