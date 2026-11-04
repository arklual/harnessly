package agentbrowser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
)

func TestConversationGateBlocksHandoffUntilToolCompletes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := New(Config{DataDir: t.TempDir()})
		defer m.Close()
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		x := &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
		p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "person"})
		p.login = x
		i.pages[p.ID] = p
		unlock := sync.OnceFunc(i.LockConversation("chat"))
		defer unlock()
		inspected := make(chan struct{})
		finished := make(chan error, 1)
		go func() {
			finished <- i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
				close(inspected)
				return "document", true, nil
			})
		}()
		// The handoff must be blocked on the conversation gate. synctest waits
		// for actual blocking rather than relying on a scheduling delay.
		synctest.Wait()
		select {
		case <-inspected:
			t.Fatal("handoff inspection bypassed the active tool conversation gate")
		case err := <-finished:
			t.Fatalf("handoff completed while the tool still held its conversation gate: %v", err)
		case <-x.done:
			t.Fatal("login settled while the tool still held its conversation gate")
		default:
		}
		other := newPage(Tab{ID: "other-tab", Conversation: "other-chat", URL: "about:blank", Control: "person"})
		i.pages[other.ID] = other
		must(t, i.setControl(other.ID, "agent", func(context.Context, string) (string, bool, error) {
			return "other-document", false, nil
		}))
		if other.Control != "agent" {
			t.Fatal("an independent conversation was blocked by another chat's tool")
		}
		unlock()
		must(t, <-finished)
		select {
		case <-inspected:
		default:
			t.Fatal("handoff did not inspect after the tool released its gate")
		}
		if tab := i.Tabs("chat")[0]; tab.Control != "agent" || !tab.Sealed || !tab.SecretUntracked {
			t.Fatal("handoff did not install its untracked seal after the tool completed")
		}
		select {
		case <-x.done:
		default:
			t.Fatal("login did not settle after the guarded handoff")
		}
	})
}

func TestConversationGateToolSeesCompletedHandoffSeal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := New(Config{DataDir: t.TempDir()})
		defer m.Close()
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		x := &loginRequest{answer: LoginAnswer{Status: "waiting"}, done: make(chan struct{})}
		p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "person"})
		p.login = x
		i.pages[p.ID] = p
		inspecting := make(chan struct{})
		allowInspection := make(chan struct{})
		release := sync.OnceFunc(func() { close(allowInspection) })
		defer release()
		finished := make(chan error, 1)
		go func() {
			finished <- i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
				close(inspecting)
				<-allowInspection
				return "new-person-document", true, nil
			})
		}()
		<-inspecting
		toolStarted := make(chan struct{})
		observed := make(chan Tab, 1)
		go func() {
			close(toolStarted)
			unlock := i.LockConversation("chat")
			defer unlock()
			observed <- i.Tabs("chat")[0]
		}()
		<-toolStarted
		synctest.Wait()
		select {
		case <-observed:
			t.Fatal("tool passed its read barrier while handoff inspection was in progress")
		case <-x.done:
			t.Fatal("login settled before handoff inspection completed")
		default:
		}
		release()
		tab := <-observed
		must(t, <-finished)
		if tab.Control != "agent" || !tab.Sealed || !tab.SecretTouched || !tab.SecretUntracked || tab.SecretDocument != "new-person-document" {
			t.Fatal("tool read stale metadata instead of the completed handoff seal")
		}
	})
}

func TestConversationGateRechecksWaitingPage(t *testing.T) {
	for _, change := range []string{"owner", "page"} {
		t.Run(change, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				m := New(Config{DataDir: t.TempDir()})
				defer m.Close()
				i := newInstance(m, DefaultProfile)
				defer i.cancel()
				p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "person"})
				i.pages[p.ID] = p
				unlock := sync.OnceFunc(i.LockConversation("chat"))
				defer unlock()
				inspected := false
				finished := make(chan error, 1)
				go func() {
					finished <- i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
						inspected = true
						return "document", false, nil
					})
				}()
				synctest.Wait()
				i.mu.Lock()
				if change == "owner" {
					p.Conversation = "other-chat"
				} else {
					i.pages[p.ID] = newPage(p.Tab)
				}
				i.mu.Unlock()
				unlock()
				if err := <-finished; err == nil || inspected {
					t.Fatal("handoff trusted a page or owner captured before waiting for the gate")
				}
			})
		})
	}
}

func TestConversationGatesBelongToTheirInstance(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first, second := &Instance{}, &Instance{}
		unlock := first.LockConversation("chat")
		defer unlock()
		independent := second.LockConversation("chat")
		independent()
	})
}

func TestConversationGateReturnCancelsToolBeforeInspecting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := New(Config{DataDir: t.TempDir()})
		defer m.Close()
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "person"})
		i.pages[p.ID] = p
		ctx, release, err := i.LockToolConversation(t.Context(), "chat")
		must(t, err)
		unlock := sync.OnceFunc(release)
		defer unlock()
		inspected := false
		done := make(chan error, 1)
		go func() {
			done <- i.setControl(p.ID, "agent", func(context.Context, string) (string, bool, error) {
				inspected = true
				return "document", true, nil
			})
		}()
		synctest.Wait()
		if !errors.Is(context.Cause(ctx), ErrControlInterrupted) {
			t.Fatal("return did not cancel the active tool")
		}
		if inspected {
			t.Fatal("handoff inspected while the cancelled tool still held the gate")
		}
		unlock()
		must(t, <-done)
		if !inspected || !i.Tabs("chat")[0].Sealed {
			t.Fatal("handoff did not seal after cancellation completed")
		}
	})
}

func TestConversationGateTakeoverDoesNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := New(Config{DataDir: t.TempDir()})
		defer m.Close()
		i := newInstance(m, DefaultProfile)
		defer i.cancel()
		p := newPage(Tab{ID: "tab", Conversation: "chat", URL: "about:blank", Control: "agent"})
		i.pages[p.ID] = p
		_, unlock, err := i.LockToolConversation(t.Context(), "chat")
		must(t, err)
		defer unlock()
		must(t, i.SetControl(p.ID, "person"))
		if !errors.Is(i.AgentCheck("chat", p.ID, "Runtime.evaluate"), ErrPersonControl) {
			t.Fatal("takeover did not stop agent commands")
		}
	})
}
