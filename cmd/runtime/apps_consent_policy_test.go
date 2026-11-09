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
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// writeHeldList writes environment.json of a link folder as the agent does, with the organization's rule for apps.
func writeHeldList(t *testing.T, dir, consent string, breakHash bool) {
	t.Helper()
	items := []hubproto.EnvItem{{ItemID: "skill1", Kind: "skill", Name: "Ревью", Layer: "org", Source: json.RawMessage(`{"body":"x"}`)}}
	hash := hubproto.EnvHash(items)
	if breakHash {
		hash = strings.Repeat("0", 64)
	}
	raw, _ := json.Marshal(map[string]any{"rev": 1, "hash": hash, "appsConsent": consent, "items": items})
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "environment.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAppsConsentPolicyReadsTheLinks(t *testing.T) {
	data := t.TempDir()
	p := newAppsConsentPolicy(hubLinkDirs(data, ""))
	if p.On() {
		t.Fatal("a machine without a hub asks")
	}
	writeHeldList(t, data, "", false)
	if p.On() {
		t.Fatal("a list without the rule asks")
	}
	// The rule on one link is enough.
	writeHeldList(t, filepath.Join(data, "hub"), hubproto.AppsConsentAuto, false)
	if !p.On() {
		t.Fatal("the rule auto on one link turns it on")
	}
	time.Sleep(10 * time.Millisecond)
	writeHeldList(t, filepath.Join(data, "hub"), hubproto.AppsConsentAuto, true)
	if p.On() {
		t.Fatal("a broken list is not trusted")
	}
	if err := os.Remove(filepath.Join(data, "hub", "environment.json")); err != nil {
		t.Fatal(err)
	}
	if p.On() {
		t.Fatal("the link that left takes the rule along")
	}
}

// With the rule on, a CLI install gets its rights at once and says so; what waited is granted when the rule comes on.
func TestAppsConsentPolicyOverTheCLI(t *testing.T) {
	call, _ := appRig(t)
	reg := apps.Open(decRigDir)
	link := filepath.Join(t.TempDir(), "link")
	reg.SetConsentPolicy(newAppsConsentPolicy([]string{link}).On)
	t.Cleanup(func() { reg.SetConsentPolicy(nil) })
	cli := map[string]string{"x-cli-token": "tok"}

	code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`"}`, cli)
	if code != http.StatusOK || !strings.Contains(body, `"waitsConsent":true`) {
		t.Fatalf("without the rule the install waits: %d %s", code, body)
	}
	writeHeldList(t, link, hubproto.AppsConsentAuto, false)
	grantByPolicy(reg)
	all, _ := reg.ListAll()
	for _, a := range all {
		if a.ID == "acme.demo" && (a.WaitsConsent() || a.Grant == nil || !a.Grant.Policy) {
			t.Fatalf("the rule turned on grants what waited: %+v", a.Grant)
		}
	}
	lines, _ := reg.ReadJournal("acme.demo", 10)
	if len(lines) == 0 || lines[0].What != apps.JournalConsentPolicy {
		t.Fatalf("the journal says the rule gave the rights: %+v", lines)
	}

	if err := reg.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}
	code, body = call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`"}`, cli)
	if code != http.StatusOK || !strings.Contains(body, `"waitsConsent":false`) || !strings.Contains(body, `"consentPolicy":true`) {
		t.Fatalf("with the rule the install gets its rights at once: %d %s", code, body)
	}
}
