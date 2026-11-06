package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser"
	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
)

func TestAgentBrowserLoginPageGuards(t *testing.T) {
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(t.TempDir(), ""), func(string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{}, true
	})
	a.Manager = agentbrowser.New(agentbrowser.Config{DataDir: t.TempDir()})
	defer a.Manager.Close()
	d := deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}
	route := agentBrowserLoginRoutes(d, a)[0]
	for _, tt := range []struct {
		name, method, origin, cookie, body string
		status                             int
	}{
		{"no page session", "GET", "", "", "", 401},
		{"agent token cannot answer", "POST", "", "", `{"id":"test","action":"done"}`, 401},
		{"foreign origin read", "GET", "https://other.example.org", "session=ok", "", 403},
		{"sibling origin write", "POST", "https://sub.example.org", "session=ok", `{"id":"test","action":"done"}`, 403},
		{"same origin pending", "GET", "http://example.org", "session=ok", "", 200},
		{"unsupported method", "DELETE", "http://example.org", "session=ok", "", 405},
		{"invalid body", "POST", "http://example.org", "session=ok", "{", 400},
		{"agent cannot create login through page", "POST", "http://example.org", "session=ok", `{"action":"request","url":"https://example.org","why":"Login"}`, 400},
		{"missing pending request", "POST", "http://example.org", "session=ok", `{"id":"missing","action":"done"}`, 400},
		{"invalid decision", "POST", "http://example.org", "session=ok", `{"id":"missing","action":"approve"}`, 400},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(tt.method, "http://example.org/api/agent-browser/logins", strings.NewReader(tt.body))
			r.Header.Set("Cookie", tt.cookie)
			r.Header.Set("Origin", tt.origin)
			r.Header.Set("X-Agent-Token", "test-agent-token")
			if !route.Match(r) {
				t.Fatal("login route did not match")
			}
			w := httptest.NewRecorder()
			route.Handler.ServeHTTP(w, r)
			if w.Code != tt.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if tt.status == 200 && strings.TrimSpace(w.Body.String()) != `{"asks":[]}` {
				t.Fatal(w.Body.String())
			}
		})
	}
	a.Manager = agentbrowser.New(agentbrowser.Config{Off: true})
	defer a.Manager.Close()
	r := httptest.NewRequest("GET", "http://example.org/api/agent-browser/logins", nil)
	r.Header.Set("Cookie", "session=ok")
	w := httptest.NewRecorder()
	route.Handler.ServeHTTP(w, r)
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
}

func TestAgentBrowserLoginPageDecisionsChrome(t *testing.T) {
	if testing.Short() {
		t.Skip("drives Chrome")
	}
	chrome, err := agentbrowser.FindChrome()
	if err != nil {
		skipWithoutChrome(t, err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data := t.TempDir()
	a := agentbrowser.NewProfileAccess(agentbrowser.OpenProfiles(data, ""), func(conv string) (agentbrowser.ConversationIdentity, bool) {
		return agentbrowser.ConversationIdentity{Title: "Manual login"}, conv == "chat"
	})
	a.Wait = time.Millisecond
	m := agentbrowser.New(agentbrowser.Config{DataDir: data, Executable: executable, Chrome: chrome, Mode: "headless", LookupProfile: a.Store.Lookup, ProfileOf: a.ProfileOf})
	a.Manager = m
	t.Cleanup(func() {
		m.Close()
		profile := filepath.Join(data, "agent-browser", "profiles", agentbrowser.DefaultProfile)
		// Wait for Chrome's final profile writes before TempDir removes its files.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		socket := host.SocketPath(filepath.Join(data, "agent-browser", "run"), agentbrowser.DefaultProfile)
		if err := host.Stop(ctx, profile, socket); err != nil {
			t.Errorf("stop private browser host: %v", err)
		}

	})
	d := deps{SessionOK: func(r *http.Request) bool { return r.Header.Get("Cookie") == "session=ok" }}
	handler := agentBrowserLoginRoutes(d, a)[0].Handler
	call := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.org/api/agent-browser/logins", strings.NewReader(body))
		r.Header.Set("Cookie", "session=ok")
		r.Header.Set("Origin", "http://example.org")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, action := range []string{"done", "cancel"} {
		if _, err := a.RequestLogin(t.Context(), "chat", "about:blank", action); err != nil {
			t.Fatal(err)
		}
		w := call("GET", "")
		var pending struct {
			Asks []agentbrowser.LoginAsk `json:"asks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &pending); err != nil || len(pending.Asks) != 1 {
			t.Fatalf("%s %v", w.Body.String(), err)
		}
		ask := pending.Asks[0]
		w = call("POST", `{"id":"`+ask.ID+`","action":"`+action+`"}`)
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		answer, err := a.RequestLogin(t.Context(), "chat", "about:blank", action)
		if err != nil {
			t.Fatal(err)
		}
		want := "done"
		if action == "cancel" {
			want = "cancelled"
		}
		if answer.Status != want || answer.URL != "about:blank" {
			t.Fatal(answer)
		}
		if w = call("GET", ""); strings.TrimSpace(w.Body.String()) != `{"asks":[]}` {
			t.Fatal(w.Body.String())
		}
		if w = call("POST", `{"id":"`+ask.ID+`","action":"done"}`); w.Code != 400 {
			t.Fatal("settled answer accepted")
		}
	}
}
