package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestAgentBrowserProfilePageGuardsAndRequests(t *testing.T) {
	dir := t.TempDir()
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(dir, ""), func(id string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Project: "project", ProjectName: "Project", Title: "Chat"}, id == "chat"
	})
	m := agentbrowser.New(agentbrowser.Config{DataDir: dir, LookupProfile: a.Store.Lookup, ProfileOf: a.ProfileOf})
	a.Manager = m
	defer m.Close()
	d := deps{DataDir: dir, SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}
	handler := agentBrowserProfileRoutes(d, a, func(id string) bool { return id == "project" })[0].Handler
	for _, tt := range []struct {
		name, method, origin, cookie, token string
		status                              int
	}{
		{"no session", "GET", "", "", "", 401},
		{"agent token only", "POST", "", "", "agent-token", 401},
		{"foreign origin read", "GET", "https://other.example.org", "session=ok", "", 403},
		{"foreign origin write", "POST", "https://other.example.org", "session=ok", "", 403},
		{"session", "GET", "http://example.org", "session=ok", "", 200},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "http://example.org/api/agent-browser/profiles", strings.NewReader(`{"action":"create","name":"Other"}`))
			r.Header.Set("Cookie", tt.cookie)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("X-Agent-Token", tt.token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.org/api/agent-browser/profiles"+path, strings.NewReader(body))
		r.Header.Set("Cookie", "session=ok")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if w := call("POST", "", `{"action":"create","name":"Private","color":"#123456"}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := call("GET", "?conv=chat", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "Private") {
		t.Fatal(w.Body.String())
	}
	profiles, _ := a.Store.Snapshot()
	private := ""
	for _, p := range profiles {
		if p.Name == "Private" {
			private = p.ID
		}
	}
	ask, err := a.Asks.Open("chat", "project", "Chat", "Need another account")
	if err != nil {
		t.Fatal(err)
	}
	w = call("GET", "", "")
	if !strings.Contains(w.Body.String(), ask.ID) || !strings.Contains(w.Body.String(), "Need another account") {
		t.Fatal(w.Body.String())
	}
	body, _ := json.Marshal(map[string]string{"action": "answer", "askId": ask.ID, "id": private, "scope": "chat"})
	if w = call("POST", "", string(body)); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w = call("GET", "?conv=chat", "")
	if !strings.Contains(w.Body.String(), "Private") {
		t.Fatal(w.Body.String())
	}
	if w = call("GET", "?conv=missing", ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
}

func TestAgentBrowserConversationPublisherCleanup(t *testing.T) {
	currentProject := "before"
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(t.TempDir(), ""), func(string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Project: currentProject, ProjectName: "Project"}, true
	})
	_, _, err := a.List("chat")
	if err != nil {
		t.Fatal(err)
	}
	// The publisher runs inside the actor: its callback uses event records, never an actor read.
	p := autoOpenPublisher{hub: wire.NewHub(nil), browser: a}
	project := "after"
	p.Publish(wire.ChannelConvs, wire.ConversationUpsertEv{Conv: wire.Conversation{ID: "chat", ProjectID: &project}})
	if _, ok := a.Store.ProjectOf("chat"); ok {
		t.Fatal("move retained old browser selection")
	}
	currentProject = project
	_, _, err = a.List("chat")
	if err != nil {
		t.Fatal(err)
	}
	p.Publish(wire.ChannelConvs, wire.ConversationRemovedEv{ID: "chat"})
	if _, ok := a.Store.ProjectOf("chat"); ok {
		t.Fatal("delete retained browser selection")
	}
}

func TestAgentBrowserSandboxFallbackRequiresUnavailableSandbox(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome's sandbox probe")
	}
	chrome, err := agentbrowser.FindChrome()
	if err != nil {
		t.Fatal(err)
	}
	manager := agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir(), Chrome: chrome,
		NoSandbox: func() bool { return false }})
	defer manager.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	err = browserSandboxFallback(ctx, manager)
	status := manager.Ready(ctx)
	switch status.State {
	case "up":
		if err == nil {
			t.Fatal("accepted sandbox bypass when the sandbox is usable")
		}
	case "error":
		if !strings.HasPrefix(status.Error, "Chrome sandbox is unavailable.") || err != nil {
			t.Fatalf("unexpected sandbox probe: %s, %v", status.Error, err)
		}
	default:
		t.Fatalf("probe did not finish: %+v, %v", status, err)
	}
	t.Log("sandbox bypass permission checked against the real renderer probe")
}

func TestAgentBrowserProfilesKeepProjectAccessUntilTheCatalogIsLoaded(t *testing.T) {
	dir := t.TempDir()
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(dir, ""), func(string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{}, false
	})
	m := agentbrowser.New(agentbrowser.Config{DataDir: dir, LookupProfile: a.Store.Lookup, ProfileOf: a.ProfileOf})
	a.Manager = m
	defer m.Close()
	profile, err := a.Store.Create("Work", "#123456")
	if err != nil {
		t.Fatal(err)
	}
	for _, project := range []string{"kept", "removed"} {
		if err := a.Store.SetProject(project, agentbrowser.ProjectAccess{Default: profile.ID}); err != nil {
			t.Fatal(err)
		}
	}
	known := map[string]bool{}
	d := deps{DataDir: dir, SessionOK: func(*http.Request) bool { return true }}
	handler := agentBrowserProfileRoutes(d, a, func(id string) bool { return known[id] })[0].Handler
	read := func() map[string]agentbrowser.ProjectAccess {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "http://example.org/api/agent-browser/profiles", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		_, projects := a.Store.Snapshot()
		return projects
	}
	// An open page asks while the runtime is still reading its projects: nobody lost access.
	if got := read(); len(got) != 2 {
		t.Fatalf("access lost before the catalog loaded: %v", got)
	}
	// Once the catalog answers, a project it no longer has goes.
	known["kept"] = true
	got := read()
	if _, ok := got["kept"]; !ok || len(got) != 1 {
		t.Fatalf("after the catalog loaded: %v", got)
	}
}
