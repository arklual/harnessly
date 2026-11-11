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

// A dev install where signatures are required goes through, says so in words and writes it in the app's journal.
func TestAppsDevInstallWarnsWhereSignaturesAreRequired(t *testing.T) {
	call, _ := appRig(t)
	reg := apps.Open(decRigDir)
	cli := map[string]string{"x-cli-token": "tok"}
	code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`","dev":true}`, cli)
	if code != http.StatusOK || !strings.Contains(body, `"warning":""`) {
		t.Fatalf("no rule, no warning: %d %s", code, body)
	}
	if err := reg.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}
	apps.SetNetwork(apps.DeployNetwork{RequireSigned: true})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	code, body = call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`","dev":true}`, cli)
	if code != http.StatusOK || !strings.Contains(body, "подпись не проверяет") {
		t.Fatalf("the dev install warns: %d %s", code, body)
	}
	lines, _ := reg.ReadJournal("acme.demo", 10)
	var found bool
	for _, l := range lines {
		found = found || l.What == "install.dev-unsigned"
	}
	if !found {
		t.Fatalf("the journal remembers the exception: %+v", lines)
	}
}

// Where signatures are required a folder install over the CLI is refused in words (the fail-open of HRN-966); only the
// explicit dev install passes.
func TestAppsFolderInstallRefusedWhereSignaturesAreRequired(t *testing.T) {
	call, _ := appRig(t)
	reg := apps.Open(decRigDir)
	cli := map[string]string{"x-cli-token": "tok"}
	apps.SetNetwork(apps.DeployNetwork{RequireSigned: true})
	t.Cleanup(func() { apps.SetNetwork(apps.DeployNetwork{}) })
	code, body := call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`"}`, cli)
	if code != http.StatusForbidden || !strings.Contains(body, `"error":"signature"`) || !strings.Contains(body, "подпис") {
		t.Fatalf("a folder install is refused: %d %s", code, body)
	}
	if list, _ := reg.List(); len(list) != 0 {
		t.Fatalf("nothing installed: %+v", list)
	}
	code, body = call("POST", "/api/cli/apps/install", `{"dir":"`+pkgDir(t, "storage", "notify")+`","dev":true}`, cli)
	if code != http.StatusOK {
		t.Fatalf("the dev install is the exception: %d %s", code, body)
	}
}

func TestAppsPolicyReadsTheOrganizationsOffApps(t *testing.T) {
	data := t.TempDir()
	hub := filepath.Join(data, "hub")
	p := newAppsConsentPolicy(hubLinkDirs(data, ""))
	write := func(items []hubproto.EnvItem) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"rev": 1, "hash": hubproto.EnvHash(items), "items": items})
		if err := os.MkdirAll(hub, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(hub, "environment.json"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if p.AppOff(hub, "boards") {
		t.Fatal("no list, nothing off")
	}
	write([]hubproto.EnvItem{
		{ItemID: "boards", Kind: hubproto.EnvKindApp, Name: "Доски", Layer: "org", Source: json.RawMessage(`{"from":"repo","off":true,"path":"a"}`)},
		{ItemID: "docs", Kind: hubproto.EnvKindApp, Name: "Документы", Layer: "org", Source: json.RawMessage(`{"from":"repo","path":"b"}`)},
		{ItemID: "skill1", Kind: "skill", Name: "x", Layer: "org", Source: json.RawMessage(`{"off":true}`)},
	})
	if !p.AppOff(hub, "boards") || p.AppOff(hub, "docs") || p.AppOff(hub, "skill1") || p.AppOff(data, "boards") {
		t.Fatal("only the app item that is off, of its own link")
	}
	time.Sleep(10 * time.Millisecond)
	write([]hubproto.EnvItem{{ItemID: "boards", Kind: hubproto.EnvKindApp, Name: "Доски", Layer: "org", Source: json.RawMessage(`{"from":"repo","path":"a"}`)}})
	if p.AppOff(hub, "boards") {
		t.Fatal("turned on again")
	}
}
