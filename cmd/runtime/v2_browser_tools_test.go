package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestAgentBrowserCLIConnection(t *testing.T) {
	socket := fmt.Sprintf("hs-test-browser-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	callers := &callerCache{panes: tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"}),
		convOf: func(session string) string {
			switch session {
			case "hs-browser-a":
				return "chat-a"
			case "hs-browser-b":
				return "chat-b"
			}
			return ""
		}}
	results := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Session string `json:"session"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		err := browserCaller(callers, r, callers.convOf(body.Session))
		result := "allowed"
		if err != nil {
			result = err.Error()
			w.WriteHeader(http.StatusForbidden)
		}
		results <- result
	}))
	defer server.Close()
	// Both requests are real sockets held by a child of chat A's pane.
	command := fmt.Sprintf(`curl -s -H 'Content-Type: application/json' -d '{"session":"hs-browser-b"}' %s; curl -s -H 'Content-Type: application/json' -d '{"session":"hs-browser-a"}' %s; sleep 30`, server.URL, server.URL)
	if out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", "hs-browser-a", command).CombinedOutput(); err != nil {
		t.Fatalf("tmux: %v %s", err, out)
	}
	for _, want := range []string{"does not match", "allowed"} {
		select {
		case got := <-results:
			if !strings.Contains(got, want) {
				t.Fatalf("want %s got %s", want, got)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("pane request did not arrive")
		}
	}
	res, err := http.Post(server.URL, "application/json", strings.NewReader(`{"session":"hs-browser-a"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if got := <-results; !strings.Contains(got, "Cannot identify") || res.StatusCode != http.StatusForbidden {
		t.Fatalf("outside pane: %s (%d)", got, res.StatusCode)
	}
}
