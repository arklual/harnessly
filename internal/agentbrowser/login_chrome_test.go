package agentbrowser

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestChromeManualLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	must(t, err)
	exe, err := os.Executable()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			data := t.TempDir()
			var mu sync.Mutex
			project := "first"
			a := NewProfileAccess(OpenProfiles(data, ""), func(conv string) (ConversationIdentity, bool) {
				mu.Lock()
				defer mu.Unlock()
				return ConversationIdentity{Project: project, ProjectName: "Project", Title: "Login task"}, conv == "chat"
			})
			a.Wait = time.Millisecond
			m := New(Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode, LookupProfile: a.Store.Lookup, ProfileOf: a.ProfileOf})
			a.Manager = m
			t.Cleanup(func() {
				m.Close()
				entries, _ := os.ReadDir(filepath.Join(data, "agent-browser", "profiles"))
				for _, entry := range entries {
					cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", entry.Name()))
				}
			})
			i, err := m.InstanceFor(t.Context(), "chat")
			must(t, err)
			requests := make(chan string, 10)
			page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					tabs := i.Tabs("chat")
					if len(tabs) != 1 || tabs[0].Control != "person" {
						requests <- "navigation began without person control"
					} else {
						requests <- "protected"
					}
					http.Redirect(w, r, "/account", http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				_, _ = w.Write([]byte(`<title>Private account title</title><input type="password" value="private"><body>Account</body>`))
			}))
			defer page.Close()
			got, err := a.RequestLogin(t.Context(), "chat", page.URL+"/login", "Please sign in")
			must(t, err)
			if got.Status != "waiting" {
				t.Fatal(got)
			}
			select {
			case msg := <-requests:
				if msg != "protected" {
					t.Fatal(msg)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no navigation")
			}
			asks := a.Logins.Pending()
			if len(asks) != 1 || asks[0].Why != "Please sign in" {
				t.Fatal(asks)
			}
			id := asks[0].Tab
			eventually(t, 5*time.Second, func() bool { return i.loginURL(id) == page.URL+"/account" })
			retry, err := a.RequestLogin(t.Context(), "chat", page.URL+"/login", "Please sign in")
			must(t, err)
			if retry.Status != "waiting" || len(i.Tabs("chat")) != 1 {
				t.Fatal(retry)
			}
			for _, method := range []string{"Accessibility.getFullAXTree", "Page.captureScreenshot", "Runtime.evaluate", "Page.navigate", "Input.insertText", "Page.handleJavaScriptDialog"} {
				_, err := i.AgentCall(t.Context(), "chat", id, method, nil)
				if !errors.Is(err, ErrPersonControl) {
					t.Fatalf("%s: %v", method, err)
				}
			}
			_, _, err = i.AgentEvents(t.Context(), "chat", id)
			if !errors.Is(err, ErrPersonControl) {
				t.Fatal(err)
			}
			if err = i.AgentCloseTab(t.Context(), "chat", id); !errors.Is(err, ErrPersonControl) {
				t.Fatal(err)
			}
			// A popup during an identity-provider flow must also retain person control.
			evaluate(t, i, id, `window.open('about:blank');true`)
			eventually(t, 5*time.Second, func() bool { return len(i.Tabs("chat")) == 2 })
			for _, tab := range i.Tabs("chat") {
				if tab.Control != "person" {
					t.Fatalf("popup exposed: %+v", tab)
				}
			}
			must(t, a.AnswerLogin(t.Context(), asks[0].ID, "done"))
			done, err := a.RequestLogin(t.Context(), "chat", page.URL+"/login", "Please sign in")
			must(t, err)
			raw, _ := json.Marshal(done)
			if done.Status != "done" || done.URL != page.URL+"/account" || len(raw) > len(page.URL)+60 {
				t.Fatal(string(raw))
			}
			must(t, i.AgentCheck("chat", id, "Runtime.evaluate"))
			// Closing a login cancels; a retry remembers that result.
			_, err = a.RequestLogin(t.Context(), "chat", page.URL+"/account", "Another sign in")
			must(t, err)
			ask := a.Logins.Pending()[0]
			must(t, i.CloseTab(t.Context(), ask.Tab))
			cancelled, err := a.RequestLogin(t.Context(), "chat", page.URL+"/account", "Another sign in")
			must(t, err)
			if cancelled.Status != "cancelled" {
				t.Fatal(cancelled)
			}
			// Returning control through the ordinary panel command completes the request.
			_, err = a.RequestLogin(t.Context(), "chat", page.URL+"/account", "Return control")
			must(t, err)
			ask = a.Logins.Pending()[0]
			must(t, i.SetControl(ask.Tab, "agent"))
			if got := a.Logins.find(ask.ID).wait(t.Context(), 0); got.Status != "done" {
				t.Fatal(got)
			}
			_, err = a.RequestLogin(t.Context(), "chat", page.URL+"/account", "Move project")
			must(t, err)
			ask = a.Logins.Pending()[0]
			must(t, a.ConversationChanged("chat", "second", false))
			mu.Lock()
			project = "second"
			mu.Unlock()
			if got := a.Logins.find(ask.ID).wait(t.Context(), 0); got.Status != "cancelled" {
				t.Fatal(got)
			}
			if len(a.Logins.Pending()) != 0 {
				t.Fatal("move retained login card")
			}
		})
	}
}
