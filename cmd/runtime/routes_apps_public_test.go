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

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// The public catalog over its routes: an author publishes from the shell, anyone browses and installs from the page, the
// platform reviews, and the trusted tier never gets in.
func TestPublicCatalogRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := deps{DataDir: dir, CLI: &gate.CLIToken{DataDir: dir, Headers: []string{"x-cli-token"}},
		SessionOK:    func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" },
		MachineAgent: func(r *http.Request) bool { return r.Header.Get("X-Test-Platform") == "yes" }}
	reg := apps.Open(dir)
	routes := appPublicRoutes(d, reg, "0.17.0")
	call := func(method, path, body string, h map[string]string) (int, string) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		for k, v := range h {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		for _, rt := range routes {
			if rt.Match(req) {
				rt.Handler.ServeHTTP(w, req)
				return w.Code, w.Body.String()
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	platform := map[string]string{"X-Test-Platform": "yes"}

	// Packages: one plain, one that asks for the whole network, one trusted. Published to stable first, as an author does.
	publish := func(id string, edit func(m map[string]any)) string {
		pkg := filepath.Join(t.TempDir(), id)
		if err := apps.Scaffold(pkg, id, "App "+id, "pane-chat"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "LICENSE"), []byte("MIT"), 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(pkg, apps.ManifestPath)
		raw, _ := os.ReadFile(path)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		edit(m)
		raw, _ = json.Marshal(m)
		_ = os.WriteFile(path, raw, 0o644)
		code, body := call("POST", "/api/cli/apps/public/publish", `{"id":"`+id+`","version":"0.1.0"}`, cli)
		if code != http.StatusNotFound && code != http.StatusConflict {
			t.Fatalf("publish public before stable: %d %s", code, body)
		}
		if _, ps, err := reg.Publish(pkg, apps.ChannelStable, apps.PublishOptions{}); err != nil {
			t.Fatalf("%s: %v %v", id, err, ps)
		}
		return pkg
	}
	publish("acme.plain", func(map[string]any) {})
	publish("acme.wide", func(m map[string]any) {
		m["network"] = map[string]any{"connect": []string{"*"}, "reason": "the person's own server"}
	})
	publish("acme.trusted", func(m map[string]any) { m["trust"] = "trusted" })

	if code, _ := call("POST", "/api/cli/apps/public/publish", `{"id":"acme.plain","version":"0.1.0"}`, nil); code != http.StatusForbidden {
		t.Fatalf("no token: %d", code)
	}
	if code, body := call("POST", "/api/cli/apps/public/publish", `{"id":"acme.plain","version":"0.1.0"}`, cli); code != http.StatusOK || !strings.Contains(body, `"review":"approved"`) {
		t.Fatalf("publish: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/apps/public/publish", `{"id":"acme.wide","version":"0.1.0"}`, cli); code != http.StatusOK || !strings.Contains(body, `"review":"pending"`) {
		t.Fatalf("publish wide: %d %s", code, body)
	}
	if code, body := call("POST", "/api/cli/apps/public/publish", `{"id":"acme.trusted","version":"0.1.0"}`, cli); code != http.StatusUnprocessableEntity || !strings.Contains(body, `"trusted"`) {
		t.Fatalf("publish trusted: %d %s", code, body)
	}

	// Anyone with the page browses; the one that waits for a look and the trusted one are not there.
	var listed struct {
		Apps []struct {
			ID, Sum     string
			Permissions []string
		} `json:"apps"`
	}
	code, body := call("GET", platformAPI+"/apps/public?q=plain", "", page)
	if code != http.StatusOK || json.Unmarshal([]byte(body), &listed) != nil || len(listed.Apps) != 1 || listed.Apps[0].ID != "acme.plain" {
		t.Fatalf("browse: %d %s", code, body)
	}
	if code, body := call("GET", platformAPI+"/apps/public", "", page); code != http.StatusOK || strings.Contains(body, "acme.wide") || strings.Contains(body, "acme.trusted") {
		t.Fatalf("browse all: %d %s", code, body)
	}
	if code, _ := call("GET", platformAPI+"/apps/public", "", nil); code != http.StatusUnauthorized {
		t.Fatalf("browse with no session: %d", code)
	}

	// Install: the sum that was shown, or nothing.
	if code, body := call("POST", platformAPI+"/apps/public/install", `{"id":"acme.plain","sum":"feed"}`, page); code != http.StatusConflict || !strings.Contains(body, "changed") {
		t.Fatalf("install another sum: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/public/install", `{"id":"acme.wide","sum":"x"}`, page); code != http.StatusNotFound {
		t.Fatalf("install what waits for a look: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/public/install", `{"id":"acme.plain","sum":"`+listed.Apps[0].Sum+`"}`, page); code != http.StatusOK || !strings.Contains(body, `"id":"acme.plain"`) {
		t.Fatalf("install: %d %s", code, body)
	}
	if got, _ := reg.Get("acme.plain"); got.Team || got.Dev {
		t.Fatalf("the person's own install: %+v", got)
	}

	// The platform: only its relay reviews.
	if code, _ := call("POST", platformAPI+"/apps/public/review", `{"id":"acme.wide","version":"0.1.0","approve":true}`, page); code != http.StatusForbidden {
		t.Fatalf("a page reviewing: %d", code)
	}
	if code, body := call("POST", platformAPI+"/apps/public/pending", `{}`, platform); code != http.StatusOK || !strings.Contains(body, "acme.wide") {
		t.Fatalf("pending: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/public/review", `{"id":"acme.wide","version":"0.1.0","approve":true}`, platform); code != http.StatusOK {
		t.Fatalf("review: %d %s", code, body)
	}
	if code, body := call("GET", platformAPI+"/apps/public", "", page); code != http.StatusOK || !strings.Contains(body, "acme.wide") {
		t.Fatalf("browse after the look: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/public/snapshot", `{}`, platform); code != http.StatusOK || !strings.Contains(body, "archive") {
		t.Fatalf("snapshot: %d", code)
	}
}
