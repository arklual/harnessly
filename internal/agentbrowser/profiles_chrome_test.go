package agentbrowser

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestChromeProfileResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := FindChrome()
	if err != nil {
		t.Skip(err)
	}
	exe, err := os.Executable()
	must(t, err)
	for _, mode := range []string{"headless", "xvfb"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "xvfb" {
				if _, err := exec.LookPath("Xvfb"); err != nil {
					t.Skip(err)
				}
			}
			data := t.TempDir()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "profile fixture")
			}))
			defer server.Close()
			config := Config{DataDir: data, Executable: exe, Chrome: chrome, Mode: mode,
				ProfileOf: func(conv string) string {
					if conv == "chat" {
						return "first"
					}
					return "second"
				}}
			firstManager := New(config)
			config.ProfileOf = func(string) string { return "second" }
			secondManager := New(config)
			t.Cleanup(func() {
				firstManager.Close()
				secondManager.Close()
				for _, id := range []string{"first", "second"} {
					cleanupHost(t, filepath.Join(data, "agent-browser", "profiles", id))
				}
			})
			first, err := firstManager.InstanceFor(t.Context(), "chat")
			must(t, err)
			second, err := secondManager.InstanceFor(t.Context(), "chat")
			must(t, err)
			if first.Profile().ID != "first" || second.Profile().ID != "second" {
				t.Fatal("profile resolver ignored")
			}
			a, err := first.AgentNewTab(t.Context(), "chat", server.URL)
			must(t, err)
			b, err := second.AgentNewTab(t.Context(), "chat", server.URL)
			must(t, err)
			evaluate(t, first, a.ID, `document.cookie='scope=first; Max-Age=3600';true`)
			if got := string(evaluate(t, second, b.ID, "document.cookie")); got != `""` {
				t.Fatalf("cookie leaked: %s", got)
			}
			evaluate(t, second, b.ID, `document.cookie='scope=second; Max-Age=3600';true`)
			if got := string(evaluate(t, first, a.ID, "document.cookie")); got != `"scope=first"` {
				t.Fatal(got)
			}
			if got := string(evaluate(t, second, b.ID, "document.cookie")); got != `"scope=second"` {
				t.Fatal(got)
			}
			firstPID := pidAt(t, first.dir, "chrome")
			secondPID := pidAt(t, second.dir, "chrome")
			if firstPID == secondPID {
				t.Fatal("profiles share a Chrome process")
			}
			socket := openContractSocket(t, firstManager, "chat")
			socket.tabs(t, func(msg TabsMessage) bool { return msg.Current == a.ID && len(msg.Tabs) == 1 })
			// Reconnect to an already managed host through a separate manager only after
			// releasing the old relay client; the host intentionally accepts one client.
			secondManager.Close()
			socket2 := openContractSocket(t, firstManager, "another-chat")
			socket2.tabs(t, func(msg TabsMessage) bool {
				return len(msg.Tabs) == 1 && msg.Tabs[0].ID == b.ID && msg.Tabs[0].Owner == "other"
			})
			t.Log("two resolver functions: profiles first/second, distinct Chrome PIDs, isolated cookies; " +
				"socket resolves by conversation")
		})
	}
}
