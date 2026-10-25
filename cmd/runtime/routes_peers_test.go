package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/peers"
)

func peerRig(t *testing.T) (func(method, path, body string, h map[string]string) (int, map[string]any), []byte) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := []byte(strings.Repeat("k", 32))
	d := deps{DataDir: dir, Secret: secret, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	routes := peerRoutes(d, nil)
	return func(method, path, body string, h map[string]string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				var out map[string]any
				_ = json.Unmarshal(w.Body.Bytes(), &out)
				return w.Code, out
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, nil
	}, secret
}

// Each caller gets in only by its own door: the page by its session, an agent by the cli-token, another machine by a
// token signed with this runtime's secret.
func TestPeerRoutesAuth(t *testing.T) {
	call, secret := peerRig(t)
	session := map[string]string{"X-Test-Session": "yes"}
	cli := map[string]string{"x-cli-token": "tok"}
	if code, _ := call("GET", "/agents/api/platform/peers/settings", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("settings without a session: %d", code)
	}
	if code, _ := call("GET", "/agents/api/platform/peers/settings", "", cli); code != http.StatusUnauthorized {
		t.Fatalf("the cli-token is not a person: %d", code)
	}
	if code, _ := call("GET", "/api/cli/peers", "", session); code != http.StatusForbidden {
		t.Fatalf("a page session is not an agent: %d", code)
	}
	if code, out := call("POST", "/agents/api/peers/inbox", `{"conversationId":"tab-a","text":"x"}`, nil); code != http.StatusForbidden || out["reason"] != "unverified" {
		t.Fatalf("inbox without a token: %d %v", code, out)
	}
	token := peers.SignInbox(secret, peers.Sender{Person: "ivan@example.com", Machine: "dev-department"})
	if code, out := call("POST", "/agents/api/peers/inbox", `{"conversationId":"tab-a","text":"x"}`, map[string]string{"Authorization": "Bearer " + token}); code != http.StatusOK || out["reason"] != "unknown-chat" {
		t.Fatalf("a verified message to a missing chat: %d %v", code, out)
	}

	code, out := call("GET", "/agents/api/platform/peers/settings", "", session)
	settings, _ := out["settings"].(map[string]any)
	if code != http.StatusOK || settings["accept"] != true || settings["scope"] != "team" || out["relay"] != false {
		t.Fatalf("settings: %d %v", code, out)
	}
	audit, _ := out["audit"].([]any)
	if len(audit) != 1 {
		t.Fatalf("the refused inbox message is in the audit: %v", out["audit"])
	}
	put := `{"accept":true,"scope":"list","delivery":"turn","rules":[{"kind":"person","id":"ivan@example.com","name":"Иван","allow":true}],"rev":0}`
	if code, out := call("PUT", "/agents/api/platform/peers/settings", put, session); code != http.StatusOK || out["settings"].(map[string]any)["rev"] != float64(1) {
		t.Fatalf("put: %d %v", code, out)
	}
	if code, _ := call("PUT", "/agents/api/platform/peers/settings", put, session); code != http.StatusConflict {
		t.Fatalf("a stale put: %d", code)
	}
	if code, out := call("POST", "/api/cli/peers/send", `{"to":"dev-department/tab-x","text":"hi"}`, cli); code != http.StatusOK || out["reason"] != "no-relay" {
		t.Fatalf("send without a relay: %d %v", code, out)
	}
}

func TestPeerSelf(t *testing.T) {
	env := map[string]string{"RUNTIME_FLEET_ID": "runner-2", "RUNTIME_OWNER_NAME": "Пётр", "RUNTIME_OWNER_EMAIL": "s@t.ru"}
	s := peerSelf(func(k string) string { return env[k] })
	if s.Machine != "runner-2" || s.PersonName != "Пётр" || s.Person != "s@t.ru" {
		t.Fatalf("self: %+v", s)
	}
	s = peerSelf(func(string) string { return "" })
	if s.Machine == "" || s.Machine[0] < 'a' || s.Machine[0] > 'z' {
		t.Fatalf("a host name makes a fleet id: %+v", s)
	}
}
