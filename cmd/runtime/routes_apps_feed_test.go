package main

import (
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// the day feed of a start-page view is read through the runtime with the app's secret as a bearer token: the
// page shows no sign-in, a missing or refused secret answers 409, and only the day, an item and an item's action go on.
func TestAppFeedRoute(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	remote := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" "+string(body))
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer feed-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"calendar":[],"path":"`+r.URL.Path+`"}`)
	}))
	defer remote.Close()
	roots := x509.NewCertPool()
	roots.AddCert(remote.Certificate())
	apps.SetNetwork(apps.DeployNetwork{Internal: []string{"127.0.0.1"}, Roots: roots})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })

	call, _ := appRig(t)
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "day")
	if err := apps.Scaffold(pkg, "acme.day", "День", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["targets"] = append(m["targets"].([]any), "home")
	m["permissions"] = []string{"secrets:token"}
	m["secrets"] = []map[string]any{{"name": "token", "description": "Токен ленты", "optional": true}}
	m["network"] = map[string]any{"connect": []string{"127.0.0.1"}}
	views := m["views"].([]any)
	entry := views[0].(map[string]any)["entry"]
	m["views"] = append(views, map[string]any{"id": "home", "entry": entry, "targets": []string{"home"}, "height": "stage",
		"feed": remote.URL + "/api/day", "feedSecret": "token"})
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, map[string]string{"x-cli-token": "tok"}); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	feed := platformAPI + feedProxyPath("acme.day", "home")

	if code, _ := call("GET", feed, "", nil); code != http.StatusUnauthorized {
		t.Fatalf("without the person's session: %d", code)
	}
	if code, body := call("GET", feed, "", page); code != http.StatusConflict || !strings.Contains(body, "secret-missing") {
		t.Fatalf("no secret: %d %s", code, body)
	}
	store := safeFor(rigDeps).store
	if err := store.Local.PutApp("acme.day", "token", "feed-token"); err != nil {
		t.Fatal(err)
	}
	if code, body := call("GET", feed, "", page); code != http.StatusOK || !strings.Contains(body, `"path":"/api/day"`) {
		t.Fatalf("the day: %d %s", code, body)
	}
	if code, body := call("GET", feed+"/items/gl:42", "", page); code != http.StatusOK || !strings.Contains(body, `"/api/day/items/gl:42"`) {
		t.Fatalf("an item: %d %s", code, body)
	}
	if code, body := call("POST", feed+"/items/gl:42/ask", `{"say":"посмотри"}`, page); code != http.StatusOK {
		t.Fatalf("an action: %d %s", code, body)
	}
	for _, p := range []string{"/settings", "/items/gl:42/take/more", "/../sources"} {
		if code, _ := call("GET", feed+p, "", page); code != http.StatusNotFound {
			t.Fatalf("%s goes on: %d", p, code)
		}
	}
	if code, _ := call("GET", feed+"/items/gl:42/take", "", page); code != http.StatusNotFound {
		t.Fatalf("an action by GET goes on: %d", code)
	}
	if code, _ := call("GET", platformAPI+feedProxyPath("acme.day", "main"), "", page); code != http.StatusNotFound {
		t.Fatalf("a view without a feed: %d", code)
	}
	if err := store.Local.PutApp("acme.day", "token", "stale"); err != nil {
		t.Fatal(err)
	}
	if code, body := call("GET", feed, "", page); code != http.StatusConflict || !strings.Contains(body, "secret-refused") {
		t.Fatalf("a refused secret: %d %s", code, body)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"GET /api/day ", "GET /api/day/items/gl:42 ", `POST /api/day/items/gl:42/ask {"say":"посмотри"}`, "GET /api/day "}
	if strings.Join(seen, "\n") != strings.Join(want, "\n") {
		t.Fatalf("the feed saw:\n%s", strings.Join(seen, "\n"))
	}
}
