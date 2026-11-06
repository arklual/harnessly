package agentbrowser

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/provision"
)

func TestLoginRequestDoesNotHoldAccessLockDuringStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := t.TempDir()
		a := NewProfileAccess(OpenProfiles(data, ""), func(string) (ConversationIdentity, bool) { return ConversationIdentity{}, true })
		m := New(Config{DataDir: data})
		defer m.Close()
		a.Manager = m
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		m.instances[DefaultProfile] = i
		defer delete(m.instances, DefaultProfile)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := a.RequestLogin(ctx, "chat", "https://example.org", "Sign in"); done <- err }()
		synctest.Wait()
		unlocked := a.mu.TryLock()
		if unlocked {
			a.mu.Unlock()
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("startup cancellation: %v", err)
		}
		if !unlocked {
			t.Fatal("browser startup holds the access lock and blocks other login answers")
		}
	})
}

func TestLoginAnswerDoesNotHoldAccessLockDuringHandoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := t.TempDir()
		a := NewProfileAccess(OpenProfiles(data, ""), func(string) (ConversationIdentity, bool) { return ConversationIdentity{}, true })
		m := New(Config{DataDir: data})
		defer m.Close()
		a.Manager = m
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "person"})
		i.pages[p.ID] = p
		x := &loginRequest{ask: LoginAsk{ID: "ask", Conv: "chat", Tab: p.ID}, instance: i, answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
		p.login = x
		a.Logins.entries["ask"] = x
		connectHandoffFixture(t, i, p, "")
		unlock := i.LockConversation("chat")
		done := make(chan error, 1)
		go func() { done <- a.AnswerLogin(t.Context(), "ask", "done") }()
		synctest.Wait()
		unlocked := a.mu.TryLock()
		if unlocked {
			a.mu.Unlock()
		}
		unlock()
		must(t, <-done)
		if !unlocked {
			t.Fatal("handoff holds the access lock and blocks other login answers")
		}
	})
}

func TestLoginRequestRechecksProjectAfterStartup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := t.TempDir()
		project := "first"
		a := NewProfileAccess(OpenProfiles(data, ""), func(string) (ConversationIdentity, bool) { return ConversationIdentity{Project: project}, true })
		// A fixture stands in for Chrome: startup must not look for, download or probe a real one.
		m := New(Config{DataDir: data, Chrome: "true"})
		m.probe = func(context.Context, string, string) (provision.Inspection, error) {
			return provision.Inspection{Version: "Chrome fixture", Sandbox: "enabled"}, nil
		}
		defer m.Close()
		a.Manager = m
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		m.instances[DefaultProfile] = i
		defer delete(m.instances, DefaultProfile)
		done := make(chan error, 1)
		go func() { _, err := a.RequestLogin(t.Context(), "chat", "https://example.org", "Sign in"); done <- err }()
		synctest.Wait()
		// Finish startup only after moving the chat. No target may be created for the old request.
		project = "second"
		connectHandoffFixture(t, i, newPage(Tab{ID: "fixture"}), "Target.createTarget")
		i.mu.Lock()
		i.state = "up"
		i.mu.Unlock()
		err := <-done
		if err == nil || err.Error() != "The requesting chat has moved or been removed" {
			t.Fatalf("startup did not reject its stale login request: %v", err)
		}
		if len(i.Tabs("chat")) != 0 {
			t.Fatal("stale login opened a tab after the chat moved")
		}
	})
}
