package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/apps"
)

// HRN-852: an app reaches the agent's browser of its chat only with browser.read (to look) or browser.drive (to act),
// only through the tools of its right, never a password's, a file's or a script's.
func TestAppBrowserRoute(t *testing.T) {
	call, conv := appRig(t)
	conv.Apps = []string{"acme.demo"}
	conv.Cwd = t.TempDir()
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(pkg, "acme.demo", "Демо", "pane-chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(pkg, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"browser.read"}
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(pkg, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, map[string]string{"x-cli-token": "tok"}); code != http.StatusOK {
		t.Fatalf("install: %d %s", code, body)
	}
	agreeRig(t)
	_, body := call("POST", platformAPI+"/apps/session", `{"app":"acme.demo","view":"main","target":"pane","conversation":"tab-1"}`, page)
	var sess struct{ Session string }
	_ = json.Unmarshal([]byte(body), &sess)
	browse := func(rest string) (int, string) {
		return call("POST", platformAPI+"/apps/browser", `{"session":"`+sess.Session+`",`+rest+`}`, page)
	}
	for _, c := range []struct{ name, rest, want string }{
		{"drive without the right", `"right":"browser.drive","tool":"click","args":{"ref":"e1"}`, "browser.drive"},
		{"a script", `"right":"browser.read","tool":"evaluate","args":{"expression":"document.cookie"}`, "no such tool"},
		{"a secret", `"right":"browser.read","tool":"fill_secret"`, "no such tool"},
		{"another right", `"right":"files.read","tool":"tabs"`, ""},
		{"args not an object", `"right":"browser.read","tool":"tabs","args":[1]`, "object"},
	} {
		if code, body := browse(c.rest); code < 400 || !strings.Contains(body, c.want) {
			t.Errorf("%s: %d %s", c.name, code, body)
		}
	}
	// The rig has no agent browser: the call is the host's to refuse, after the checks; and it is journaled.
	if code, body := browse(`"right":"browser.read","tool":"snapshot","args":{"tab":"t1"}`); code < 400 || !strings.Contains(body, "agent browser") {
		t.Errorf("no browser: %d %s", code, body)
	}
	conv.Apps = nil
	if code, body := browse(`"right":"browser.read","tool":"tabs"`); code != http.StatusForbidden || !strings.Contains(body, "not-on") {
		t.Errorf("the app off in the chat: %d %s", code, body)
	}
}
