package agentbrowser

import (
	"testing"
	"time"
)

// The person signs in and returns control minutes later. The idle stop must not take the browser away meanwhile,
// and a browser that does go down must not leave its request waiting on every chat's screen.
func TestPendingLoginHoldsBrowserAndSettlesWhenItGoesDown(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()})
	defer m.Close()
	i := newInstance(m, DefaultProfile)
	defer i.cancel()
	q := NewLoginRequests()
	x, _, err := q.open("chat", ConversationIdentity{Project: "project", Title: "Chat"}, "https://example.org/login", "Sign in")
	must(t, err)
	p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "https://example.org/login", Control: "person"})
	p.login = x
	x.instance, x.ask.Tab = i, p.ID
	i.pages[p.ID] = p
	now := time.Now()
	i.lastActivity = now.Add(-time.Hour)
	if idle := i.idleFor(now); idle != 0 {
		t.Fatalf("a waiting login left the browser idle for %s", idle)
	}
	i.lastActivity = now.Add(-2 * time.Hour)
	if i.idleFor(now.Add(loginHold+time.Minute)) == 0 {
		t.Fatal("an abandoned login keeps the browser forever")
	}
	if len(q.Pending()) != 1 {
		t.Fatal("request is not shown while it waits")
	}
	i.dropLogins()
	if got := x.wait(t.Context(), 0); got.Status != "cancelled" {
		t.Fatal(got)
	}
	if left := q.Pending(); len(left) != 0 {
		t.Fatalf("request of a stopped browser is still shown: %v", left)
	}
}

func TestLoginAnswerForStoppedBrowserSettles(t *testing.T) {
	data := t.TempDir()
	a := NewProfileAccess(OpenProfiles(data, ""), func(string) (ConversationIdentity, bool) { return ConversationIdentity{}, true })
	m := New(Config{DataDir: data})
	defer m.Close()
	a.Manager = m
	i := newInstance(m, DefaultProfile)
	x := &loginRequest{ask: LoginAsk{ID: "ask", Conv: "chat", Tab: "gone", At: time.Now().UnixMilli()}, instance: i, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
	a.Logins.entries["ask"] = x
	i.cancel()
	must(t, a.AnswerLogin(t.Context(), "ask", "cancel"))
	if left := a.Logins.Pending(); len(left) != 0 {
		t.Fatalf("cancelled request of a stopped browser is still shown: %v", left)
	}
}
