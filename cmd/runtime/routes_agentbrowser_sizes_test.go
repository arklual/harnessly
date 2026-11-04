package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
)

func TestAgentBrowserProfileSizeJSON(t *testing.T) {
	data := t.TempDir()
	store := agentbrowser.OpenProfiles(data, "http://127.0.0.1:1")
	managed, err := store.Create("Managed", "#123456")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Resolve("attached-chat", "", "", "Chat")
	if err != nil {
		t.Fatal(err)
	}
	chrome := filepath.Join(data, "agent-browser", "profiles", managed.ID, "chrome")
	if err := os.MkdirAll(chrome, 0700); err != nil {
		t.Fatal(err)
	}
	attachedChrome := filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile, "chrome")
	if err := os.MkdirAll(attachedChrome, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachedChrome, "data"), []byte("external profile stays unknown"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Create("Missing directory", "#123456"); err != nil {
		t.Fatal(err)
	}
	a := agentbrowser.NewProfileAccess(store, nil)
	a.Manager = agentbrowser.New(agentbrowser.Config{DataDir: data, LookupProfile: store.Lookup})
	defer a.Manager.Close()
	handler := agentBrowserProfileRoutes(deps{DataDir: data, SessionOK: func(*http.Request) bool { return true }}, a, func(string) bool { return true })[0].Handler
	read := func() []map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "http://example.org/api/agent-browser/profiles", nil))
		var body struct {
			Profiles []map[string]any `json:"profiles"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		return body.Profiles
	}
	for _, p := range read() {
		if _, ok := p["sizeBytes"]; ok {
			t.Fatal("first response blocked on traversal", p)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, p := range read() {
			if p["id"] == managed.ID && p["sizeBytes"] == float64(0) {
				return
			}
			if p["id"] != managed.ID {
				if _, ok := p["sizeBytes"]; ok {
					t.Fatal("missing directory has known size", p)
				}
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("empty Chrome directory did not report numeric zero size")
}
