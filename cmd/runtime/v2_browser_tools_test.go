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

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	browsertools "github.com/ly-solutions/harnessly/internal/agentbrowser/tools"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestAgentBrowserCLIConnection(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
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

func TestAgentBrowserProfilesCLIConnectionIsolation(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	socket := fmt.Sprintf("hs-test-profile-%d", rand.IntN(1_000_000_000))
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	callers := &callerCache{panes: tmux.New(tmux.Names{Socket: socket, Prefix: "hs-"}), convOf: func(session string) string {
		if session == "hs-browser-b" {
			return "b"
		}
		return ""
	}}
	store := agentbrowser.OpenProfiles(t.TempDir(), "")
	access := agentbrowser.NewProfileAccess(store, func(id string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Project: id, ProjectName: "Private " + id}, id == "a" || id == "b"
	})
	manager := agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir(), ProfileOf: access.ProfileOf, LookupProfile: store.Lookup})
	defer manager.Close()
	access.Manager = manager
	_, private, err := access.List("a")
	if err != nil {
		t.Fatal(err)
	}
	service := &browsertools.Service{Manager: manager, Access: access}
	defer service.Close()
	results := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := callers.of(r)
		if err := browserCaller(callers, r, id); err != nil {
			t.Error(err)
			w.WriteHeader(403)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		set := browsertools.Tools{S: service, Caller: id}
		name, args := "chrome_use_profile", jsvalue.NewObject("name", body.Name)
		if body.Name == "" {
			name, args = "chrome_profiles", jsvalue.NewObject()
		}
		raw := string(jsvalue.Stringify(set.Call(r.Context(), name, args), ""))
		results <- raw
		_, _ = io.WriteString(w, raw)
	}))
	defer server.Close()
	command := fmt.Sprintf(`curl -s -d '{"name":"Private a"}' %s; curl -s -d '{"name":"missing"}' %s; curl -s -d '{}' %s; sleep 30`, server.URL, server.URL, server.URL)
	if out, err := exec.Command("tmux", "-f", "/dev/null", "-L", socket, "new-session", "-d", "-s", "hs-browser-b", command).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	got := []string{}
	for range 3 {
		select {
		case raw := <-results:
			got = append(got, raw)
		case <-time.After(10 * time.Second):
			t.Fatal("CLI pane timed out")
		}
	}
	if got[0] != got[1] || !strings.Contains(got[0], "unavailable to this chat") {
		t.Fatal(got)
	}
	for _, raw := range got {
		if strings.Contains(raw, private.Name) || strings.Contains(raw, private.ID) {
			t.Fatal("other project's profile leaked", raw)
		}
	}
	if !strings.Contains(got[2], "Private b") {
		t.Fatal(got[2])
	}
}
