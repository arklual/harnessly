package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// examplePkg is a scaffolded package example.app 0.1.0 asking for the rights given.
func examplePkg(t *testing.T, rights ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app")
	if err := apps.Scaffold(dir, "example.app", "Example", "chat"); err != nil {
		t.Fatal(err)
	}
	setRights(t, dir, rights...)
	return dir
}

func setRights(t *testing.T, dir string, rights ...string) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(dir, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if rights == nil {
		rights = []string{}
	}
	m["version"], m["permissions"] = "0.1.0", rights
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// HRN-913: an app that waits for consent says what it waits for, everywhere the CLI looks; nothing but the page grants.
func TestWaitingAppSaysWhatItWaitsFor(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := examplePkg(t, "storage", "notify")
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`","dev":true}`, cli); code != http.StatusOK || !strings.Contains(body, `"waitsConsent":true`) {
		t.Fatalf("dev install: %d %s", code, body)
	}
	// app enable of a waiting app says what it waits for, not «not installed».
	code, body := call("POST", "/api/cli/apps/enable", `{"session":"hs-agent","id":"example.app","on":true}`, cli)
	if code != http.StatusConflict || !strings.Contains(body, `"error":"consent"`) || !strings.Contains(body, `"access":["storage","notify"]`) {
		t.Fatalf("enable: %d %s", code, body)
	}
	// explain sees the dev install that waits.
	code, body = call("GET", "/api/cli/apps/explain?id=example.app", "", cli)
	if code != http.StatusOK || strings.Contains(body, "example.app is not installed") || !strings.Contains(body, "dev install") || !strings.Contains(body, "waits for the person's consent to storage, notify") {
		t.Fatalf("explain: %d %s", code, body)
	}
	// The page's list names what waits in the state's detail.
	if _, list := call("GET", platformAPI+"/installed-apps", "", page); !strings.Contains(list, `"detail":"storage, notify"`) {
		t.Fatalf("the list: %s", list)
	}
}

// HRN-913: a dev install whose folder gained a right lists it on the card the page draws, so the click there is taken.
func TestInstalledAppsShowsTheDevFolderAccess(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := examplePkg(t)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`","dev":true}`, cli); code != http.StatusOK || strings.Contains(body, `"waitsConsent":true`) {
		t.Fatalf("dev install of nothing: %d %s", code, body)
	}
	time.Sleep(10 * time.Millisecond)
	setRights(t, pkg, "storage")
	_, list := call("GET", platformAPI+"/installed-apps", "", page)
	if !strings.Contains(list, `"consent":true`) || !strings.Contains(list, `"key":"perm:storage"`) {
		t.Fatalf("the card lacks what waits: %s", list)
	}
	if code, body := call("POST", platformAPI+"/apps/consent", `{"id":"example.app","version":"0.1.0","access":["perm:storage"]}`, page); code != http.StatusOK {
		t.Fatalf("the click on what the card shows: %d %s", code, body)
	}
}

func TestViewLogRoutes(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := examplePkg(t)
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	body := `{"id":"example.app","view":"main","lines":[{"level":"error","text":"TypeError: boom"},{"level":"bridge","text":"storage.get: denied"},{"level":"nope","text":"x"}]}`
	if code, got := call("POST", platformAPI+"/apps/view-log", body, page); code != http.StatusOK || !strings.Contains(got, `"kept":2`) {
		t.Fatalf("report: %d %s", code, got)
	}
	if code, _ := call("POST", platformAPI+"/apps/view-log", body, cli); code != http.StatusUnauthorized {
		t.Fatalf("the report is the page's: %d", code)
	}
	if code, _ := call("POST", platformAPI+"/apps/view-log", `{"id":"example.none","lines":[{"level":"error","text":"x"}]}`, page); code != http.StatusNotFound {
		t.Fatalf("unknown app: %d", code)
	}
	code, got := call("GET", "/api/cli/apps/logs?id=example.app", "", cli)
	if code != http.StatusOK || !strings.Contains(got, "TypeError: boom") || !strings.Contains(got, `"view":"main"`) || strings.Contains(got, `"nope"`) {
		t.Fatalf("logs: %d %s", code, got)
	}
	if code, got := call("GET", "/api/cli/apps/explain?id=example.app", "", cli); code != http.StatusOK || !strings.Contains(got, "harnessly app logs example.app") {
		t.Fatalf("explain names the errors: %d %s", code, got)
	}
	if code, _ := call("GET", "/api/cli/apps/logs?id=example.app", "", page); code != http.StatusForbidden {
		t.Fatalf("logs are the CLI's: %d", code)
	}
}
