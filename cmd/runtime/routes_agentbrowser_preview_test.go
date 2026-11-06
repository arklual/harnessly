package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
)

func TestAgentBrowserProjectPreviewGuardsValidationAndNoWrites(t *testing.T) {
	data := t.TempDir()
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(data, ""), func(id string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Project: "project", ProjectName: "Project"}, id == "chat"
	})
	_, first, err := a.List("chat")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.Store.Create("Other", "#123456")
	if err != nil {
		t.Fatal(err)
	}
	a.Manager = agentbrowser.New(agentbrowser.Config{DataDir: data, Chrome: "unavailable", LookupProfile: a.Store.Lookup})
	defer a.Manager.Close()
	dir := filepath.Join(data, "agent-browser", "profiles", first.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tabsPath := filepath.Join(dir, "tabs.json")
	tabs := `{"tabs":[{"id":"one","conversation":"chat","url":"https://example.org"},{"id":"two","conversation":"chat","url":"https://example.org"},{"id":"other","conversation":"other","url":"https://example.org"}]}`
	if err := os.WriteFile(tabsPath, []byte(tabs), 0600); err != nil {
		t.Fatal(err)
	}
	metadataPath := filepath.Join(data, "agent-browser", "profiles.json")
	before, err := os.ReadFile(metadataPath)
	if err != nil {
		t.Fatal(err)
	}
	handler := agentBrowserProfileRoutes(deps{DataDir: data, SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}, a, func(id string) bool { return id == "project" })[0].Handler
	for _, tt := range []struct {
		name, origin, cookie, project, defaultID string
		granted                                  []string
		status, count                            int
	}{
		{name: "no session", project: "project", defaultID: second.ID, status: 401},
		{name: "foreign origin", origin: "https://other.example.org", cookie: "session=ok", project: "project", defaultID: second.ID, status: 403},
		{name: "missing project", cookie: "session=ok", project: "missing", defaultID: second.ID, status: 400},
		{name: "empty project", cookie: "session=ok", defaultID: second.ID, status: 400},
		{name: "missing default", cookie: "session=ok", project: "project", defaultID: "missing", status: 400},
		{name: "missing grant", cookie: "session=ok", project: "project", defaultID: first.ID, granted: []string{"missing"}, status: 400},
		{name: "unchanged", cookie: "session=ok", project: "project", defaultID: first.ID, status: 200},
		{name: "extra grant", cookie: "session=ok", project: "project", defaultID: first.ID, granted: []string{second.ID}, status: 200},
		{name: "change default", cookie: "session=ok", project: "project", defaultID: second.ID, status: 200, count: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := json.Marshal(browserProfileAction{Action: "project-preview", Project: tt.project, Default: tt.defaultID, Granted: tt.granted})
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://example.org/api/agent-browser/profiles", strings.NewReader(string(raw)))
			r.Header.Set("Cookie", tt.cookie)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("X-Agent-Token", "agent-token")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if tt.status == 200 {
				var response map[string]int
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				count, ok := response["affectedChats"]
				if len(response) != 1 || !ok || count != tt.count {
					t.Fatal(w.Body.String())
				}
			}
			saved, err := os.ReadFile(metadataPath)
			if err != nil || string(saved) != string(before) {
				t.Fatal("preview changed metadata", err)
			}
			saved, err = os.ReadFile(tabsPath)
			if err != nil || string(saved) != tabs {
				t.Fatal("preview changed tabs", err)
			}
			if _, err := os.Stat(filepath.Join(dir, "chrome")); !os.IsNotExist(err) {
				t.Fatal("preview created Chrome data", err)
			}
		})
	}
}
