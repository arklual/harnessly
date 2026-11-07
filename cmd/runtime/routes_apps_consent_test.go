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

// agreeAll is the person's click on every install screen that waits: the tests that install through the CLI and then
// use the app's rights need it, as a person would.
func agreeAll(t *testing.T, reg *apps.Registry) {
	t.Helper()
	list, err := reg.ListAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range list {
		version, shown := a.Version, a.Wants
		if a.Pending != nil {
			version, shown = a.Pending.Version, a.Pending.Wants
		} else if !a.NeedsConsent() {
			continue
		}
		if _, err := reg.Consent(a.ID, version, shown, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
}

func agreeRig(t *testing.T) { t.Helper(); agreeAll(t, apps.Open(decRigDir)) }

func TestConsentRoutes(t *testing.T) {
	call, _ := appRig(t)
	cli := map[string]string{"x-cli-token": "tok"}
	page := map[string]string{"X-Test-Session": "yes"}
	pkg := pkgDir(t, "storage", "notify")
	code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkg+`"}`, cli)
	if code != http.StatusOK || !strings.Contains(body, `"waitsConsent":true`) {
		t.Fatalf("a CLI install that reaches anything waits for the person: %d %s", code, body)
	}
	_, list := call("GET", platformAPI+"/installed-apps", "", page)
	if !strings.Contains(list, `"consent":true`) || !strings.Contains(list, `"reason":"consent"`) || !strings.Contains(list, "Показывать уведомления") {
		t.Fatalf("the page lists it waiting, with its access in words: %s", list)
	}
	// Only the page's session agrees: the CLI token (the person's shell and every agent's) does not.
	if code, _ := call("POST", platformAPI+"/apps/consent", `{"id":"acme.demo","version":"0.1.0","access":["perm:storage","perm:notify"]}`, cli); code != http.StatusUnauthorized {
		t.Fatalf("consent with the CLI token: %d", code)
	}
	// The click grants what the screen showed: a package that reaches more than that is refused, not granted.
	if code, body := call("POST", platformAPI+"/apps/consent", `{"id":"acme.demo","version":"0.1.0","access":["perm:notify"]}`, page); code != http.StatusConflict || !strings.Contains(body, `"changed"`) {
		t.Fatalf("consent to less than the app reaches: %d %s", code, body)
	}
	if code, _ := call("POST", platformAPI+"/apps/consent", `{"id":"acme.demo","version":"0.1.0"}`, page); code != http.StatusBadRequest {
		t.Fatalf("consent without the shown access: %d", code)
	}
	if code, body := call("POST", platformAPI+"/apps/consent", `{"id":"acme.demo","version":"0.1.0","access":["perm:storage","perm:notify"]}`, page); code != http.StatusOK {
		t.Fatalf("consent: %d %s", code, body)
	}
	if code, body := call("POST", platformAPI+"/apps/switch", `{"id":"acme.demo","on":false}`, page); code != http.StatusOK {
		t.Fatalf("switch off: %d %s", code, body)
	}
	if _, err := apps.Open(decRigDir).Get("acme.demo"); err == nil {
		t.Fatal("an app switched off is not reached")
	}
	if code, _ := call("POST", platformAPI+"/apps/switch", `{"id":"acme.demo","on":true}`, cli); code != http.StatusUnauthorized {
		t.Fatalf("the switch with the CLI token: %d", code)
	}
	code, body = call("GET", platformAPI+"/apps/journal?id=acme.demo", "", page)
	for _, what := range []string{`"what":"consent.waits"`, `"what":"consent"`, `"what":"off"`} {
		if code != http.StatusOK || !strings.Contains(body, what) {
			t.Fatalf("journal lacks %s: %d %s", what, code, body)
		}
	}
}

// pkgDir is the chat template of acme.demo at 0.1.0 asking for the rights given.
func pkgDir(t *testing.T, rights ...string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "demo")
	if err := apps.Scaffold(dir, "acme.demo", "Демо", "chat"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, apps.ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["version"], m["permissions"] = "0.1.0", rights
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, apps.ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}
