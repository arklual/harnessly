package agentbrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestChromeProjectProfilesAccessAndSocketSwitch(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	if err != nil {
		skipWithoutChrome(t, err)
	}
	exe, err := os.Executable()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "xvfb" {
				if _, err := exec.LookPath("Xvfb"); err != nil {
					t.Fatal(err)
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, `<meta name="viewport" content="width=device-width"><body>Account fixture</body>`)
			}))
			defer server.Close()
			data := t.TempDir()
			store := OpenProfiles(data, "")
			records := map[string]ConversationIdentity{
				"a":    {Project: "project-a", ProjectName: "First account"},
				"b":    {Project: "project-b", ProjectName: "Second account"},
				"c":    {Project: "project-c", ProjectName: "Third account"},
				"fork": {Project: "project-a", ProjectName: "First account"},
			}
			var recMu sync.Mutex
			access := NewProfileAccess(store, func(conv string) (ConversationIdentity, bool) {
				recMu.Lock()
				defer recMu.Unlock()
				r, ok := records[conv]
				return r, ok
			})
			cfg := Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode, ProfileOf: access.ProfileOf, LookupProfile: store.Lookup}
			manager := New(cfg)
			access.Manager = manager
			t.Cleanup(func() {
				manager.Close()
				entries, _ := os.ReadDir(filepath.Join(data, "agent-browser", "profiles"))
				for _, entry := range entries {
					cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", entry.Name()))
				}
			})
			first, err := manager.InstanceFor(t.Context(), "a")
			must(t, err)
			second, err := manager.InstanceFor(t.Context(), "b")
			must(t, err)
			third, err := manager.InstanceFor(t.Context(), "c")
			must(t, err)
			tabA, err := first.AgentNewTab(t.Context(), "a", server.URL)
			must(t, err)
			tabB, err := second.AgentNewTab(t.Context(), "b", server.URL)
			must(t, err)
			tabC, err := third.AgentNewTab(t.Context(), "c", server.URL)
			must(t, err)
			evaluate(t, first, tabA.ID, `document.cookie='account=first; Max-Age=3600';true`)
			evaluate(t, second, tabB.ID, `document.cookie='account=second; Max-Age=3600';true`)
			if got := string(evaluate(t, first, tabA.ID, "document.cookie")); got != `"account=first"` {
				t.Fatal(got)
			}
			if got := string(evaluate(t, second, tabB.ID, "document.cookie")); got != `"account=second"` {
				t.Fatal(got)
			}
			if got := string(evaluate(t, third, tabC.ID, "document.cookie")); got != `""` {
				t.Fatal(got)
			}
			must(t, access.SetProject(t.Context(), "project-a", ProjectAccess{
				Default: first.Profile().ID, Granted: []string{second.Profile().ID},
			}))
			if len(first.Tabs("a")) != 1 {
				t.Fatal("adding an extra grant closed the current profile's tabs")
			}
			if err := access.SetProject(t.Context(), "project-a", ProjectAccess{Default: "missing"}); err == nil {
				t.Fatal("accepted missing project default")
			}
			if len(first.Tabs("a")) != 1 {
				t.Fatal("invalid project edit closed tabs")
			}
			if pidAt(t, first.dir, "chrome") == pidAt(t, second.dir, "chrome") {
				t.Fatal("shared process")
			}
			fork, err := manager.InstanceFor(t.Context(), "fork")
			must(t, err)
			if fork != first || len(fork.Tabs("fork")) != 0 {
				t.Fatal("fork inherited tabs")
			}
			socket := openContractSocket(t, manager, "b")
			socket.tabs(t, func(m TabsMessage) bool { return m.Current == tabB.ID })
			access.Wait = 0
			answer, err := access.Request(t.Context(), "b", "Need first account")
			must(t, err)
			if answer.Status != "waiting" {
				t.Fatal(answer)
			}
			asks := access.Asks.Pending()
			if len(asks) != 1 {
				t.Fatal(asks)
			}
			must(t, access.Answer(t.Context(), asks[0].ID, first.Profile().ID, "chat", "", ""))
			select {
			case err := <-socket.errors:
				if websocket.CloseStatus(err) != websocket.StatusServiceRestart {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("socket did not invalidate its old profile")
			}
			if len(second.Tabs("b")) != 0 {
				t.Fatal("old profile still owns chat tabs")
			}
			next := openContractSocket(t, manager, "b")
			next.until(t, func(raw []byte) bool {
				var status Status
				_ = json.Unmarshal(raw, &status)
				return status.T == "status" && status.State == "up" && status.Profile.ID == first.Profile().ID
			})
			selected, err := manager.InstanceFor(t.Context(), "b")
			must(t, err)
			tabB, err = selected.AgentNewTab(t.Context(), "b", server.URL)
			must(t, err)
			if got := string(evaluate(t, selected, tabB.ID, "document.cookie")); got != `"account=first"` {
				t.Fatal(got)
			}
			recMu.Lock()
			records["b"] = records["c"]
			recMu.Unlock()
			must(t, access.ConversationChanged("b", "project-c", false))
			if len(first.Tabs("b")) != 0 {
				t.Fatal("project move retained tabs")
			}
			moved, err := manager.InstanceFor(t.Context(), "b")
			must(t, err)
			if moved != third {
				t.Fatal("move did not select new project")
			}
			must(t, access.ConversationChanged("c", "", true))
			if len(third.Tabs("c")) != 0 {
				t.Fatal("deleted chat retained tabs")
			}
			// The runtime disconnects while all three hosts remain alive. New managers reuse them.
			_ = next.ws.CloseNow()
			manager.Close()
			manager = New(cfg)
			access.Manager = manager
			restored, err := manager.InstanceFor(t.Context(), "a")
			must(t, err)
			if len(restored.Tabs("a")) != 1 || restored.Tabs("a")[0].ID != tabA.ID {
				t.Fatal("restart lost existing Chrome target")
			}
			otherPID := pidAt(t, second.dir, "chrome")
			must(t, access.Delete(t.Context(), restored.Profile().ID))
			if _, err := os.Stat(restored.dir); !os.IsNotExist(err) {
				t.Fatal("profile directory survived delete", err)
			}
			if pidAt(t, second.dir, "chrome") != otherPID {
				t.Fatal("delete changed another profile")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			stillThere, err := manager.Instance(ctx, second.Profile().ID)
			must(t, err)
			if state, _ := stillThere.State(); state != "up" {
				t.Fatal(state)
			}
			t.Log("isolated project accounts; third account empty; chat-only grant; socket 1012 and reconnect; move/delete cleanup; fork ownership; restart; independent profile deletion")
		})
	}
}

func TestChromeProfileDeletionDuringAnAgentCall(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	if err != nil {
		skipWithoutChrome(t, err)
	}
	exe, err := os.Executable()
	must(t, err)
	data := t.TempDir()
	store := OpenProfiles(data, "")
	access := NewProfileAccess(store, func(string) (ConversationIdentity, bool) {
		return ConversationIdentity{Project: "project", ProjectName: "Account"}, true
	})
	var block atomic.Bool
	entered, release := make(chan struct{}, 1), make(chan struct{})
	manager := New(Config{DataDir: data, Chrome: chrome, Executable: exe, Mode: "headless", LookupProfile: store.Lookup,
		ProfileOf: func(conv string) string {
			if block.Load() {
				entered <- struct{}{}
				<-release
			}
			return access.ProfileOf(conv)
		}})
	access.Manager = manager
	t.Cleanup(func() {
		manager.Close()
		profiles, _ := store.Snapshot()
		for _, p := range profiles {
			cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", p.ID))
		}
	})
	instance, err := manager.InstanceFor(t.Context(), "chat")
	must(t, err)
	tab, err := instance.AgentNewTab(t.Context(), "chat", "about:blank")
	must(t, err)
	block.Store(true)
	called := make(chan error, 1)
	go func() {
		_, err := instance.AgentCall(t.Context(), "chat", tab.ID, "Runtime.evaluate", map[string]any{"expression": "1"})
		called <- err
	}()
	<-entered
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	removed := make(chan error, 1)
	go func() { removed <- access.Delete(ctx, instance.Profile().ID) }()
	eventually(t, time.Second, func() bool { instance.mu.Lock(); defer instance.mu.Unlock(); return instance.closing })
	block.Store(false)
	close(release)
	select {
	case err := <-removed:
		must(t, err)
	case <-ctx.Done():
		t.Fatal("profile deletion deadlocked with an agent profile check")
	}
	select {
	case err := <-called:
		if err == nil {
			t.Fatal("agent call succeeded after profile shutdown")
		}
	case <-ctx.Done():
		t.Fatal("agent call remained blocked after deletion")
	}
	if _, ok := store.Lookup(instance.Profile().ID); ok {
		t.Fatal("deleted profile remains authorized")
	}
}
