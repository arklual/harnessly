package apps

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// The organization's rule «Не спрашивать разрешений».

func policyRegistry(t *testing.T) (*Registry, *atomic.Bool) {
	t.Helper()
	r := &Registry{Root: t.TempDir()}
	on := &atomic.Bool{}
	r.SetConsentPolicy(on.Load)
	return r, on
}

func policyLines(t *testing.T, r *Registry, id string) []JournalEntry {
	t.Helper()
	lines, err := r.ReadJournal(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(lines, func(e JournalEntry) bool { return e.What != JournalConsentPolicy })
}

func TestPolicyOnGrantsAnInstallAtOnce(t *testing.T) {
	r, on := policyRegistry(t)
	on.Store(true)
	got, _, err := r.Install(pkgWith(t, "example.files", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	if got.WaitsConsent() || got.Grant == nil || !got.Grant.Policy || !slices.Equal(got.Grant.Keys, got.Wants) {
		t.Fatalf("with the rule on the install gets its rights at once: %+v", got.Grant)
	}
	if _, ok := listed(t, r, "example.files"); !ok {
		t.Fatal("the app is on")
	}
	if lines := policyLines(t, r, "example.files"); len(lines) != 1 || lines[0].Detail != "1.0.0" {
		t.Fatalf("the journal says the rule gave the rights, with the version: %+v", lines)
	}
	// An update that reaches more is not staged: it takes the place of the running version, and the journal says so.
	got, _, err = r.Install(pkgWith(t, "example.files", "1.1.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.1.0" || got.Pending != nil || got.WaitsConsent() {
		t.Fatalf("an update under the rule runs at once: %+v", got)
	}
	if lines := policyLines(t, r, "example.files"); len(lines) != 2 || lines[0].Detail != "1.1.0" {
		t.Fatalf("the update is in the journal: %+v", lines)
	}
}

func TestPolicyOffAsksAsBefore(t *testing.T) {
	r, _ := policyRegistry(t)
	got, _, err := r.Install(pkgWith(t, "example.files", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NeedsConsent() {
		t.Fatal("with the rule off the install waits for the person")
	}
	if granted, err := r.GrantWaiting(time.Now()); err != nil || len(granted) != 0 {
		t.Fatalf("with the rule off nothing is granted: %v %v", granted, err)
	}
	// A registry nobody gave a rule (a machine without a hub) asks too.
	plain := &Registry{Root: t.TempDir()}
	got, _, err = plain.Install(pkgWith(t, "example.files", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil || !got.NeedsConsent() {
		t.Fatalf("without a hub the install waits: %v %v", got.NeedsConsent(), err)
	}
}

func TestPolicyTurnedOnGrantsWhatWaits(t *testing.T) {
	r, on := policyRegistry(t)
	// A first install and an update that grew both wait while the rule is off.
	if _, _, err := r.Install(pkgWith(t, "example.first", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Install(pkgWith(t, "example.grown", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentGiven}); err != nil {
		t.Fatal(err)
	}
	staged, _, err := r.Install(pkgWith(t, "example.grown", "2.0.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil || staged.Pending == nil {
		t.Fatalf("the grown update waits beside the running version: %+v %v", staged, err)
	}
	on.Store(true)
	granted, err := r.GrantWaiting(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, a := range granted {
		ids = append(ids, a.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"example.first", "example.grown"}) {
		t.Fatalf("granted %v", ids)
	}
	if a, ok := listed(t, r, "example.first"); !ok || !a.Grant.Policy {
		t.Fatal("the first install is on, by the rule")
	}
	if a, ok := listed(t, r, "example.grown"); !ok || a.Version != "2.0.0" || a.Pending != nil || a.Previous != "1.0.0" {
		t.Fatalf("the pending update took the place of the running version: %+v", a)
	}
	for _, id := range ids {
		if len(policyLines(t, r, id)) != 1 {
			t.Fatalf("%s: the grant is in the journal", id)
		}
	}
	if again, _ := r.GrantWaiting(time.Now()); len(again) != 0 {
		t.Fatalf("nothing waits any more: %v", again)
	}
}

func TestPolicyTurnedOffTakesNothingBack(t *testing.T) {
	r, on := policyRegistry(t)
	on.Store(true)
	if _, _, err := r.Install(pkgWith(t, "example.files", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	on.Store(false)
	if a, ok := listed(t, r, "example.files"); !ok || a.NeedsConsent() {
		t.Fatal("rights given by the rule stay when it is turned off")
	}
	// A new ask waits for the person again.
	got, _, err := r.Install(pkgWith(t, "example.files", "1.1.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	if got.Pending == nil || got.Version != "1.0.0" {
		t.Fatalf("the update that grew waits beside the running version: %+v", got)
	}
	if other, _, err := r.Install(pkgWith(t, "example.other", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	} else if !other.NeedsConsent() {
		t.Fatal("a new install waits")
	}
}

func TestPolicyGrantsADevFolderThatGrew(t *testing.T) {
	r, on := policyRegistry(t)
	on.Store(true)
	dir := pkgWith(t, "example.dev", "0.1.0", "files.read:src/**")
	if _, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"files.read:src/**", "files.write:src/**"}
	raw, _ = json.Marshal(m)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "example.dev"); ok {
		t.Fatal("the grown folder is off until the rule's pass")
	}
	if granted, err := r.GrantWaiting(time.Now()); err != nil || len(granted) != 1 {
		t.Fatalf("the rule's pass grants the grown folder: %v %v", granted, err)
	}
	if a, ok := listed(t, r, "example.dev"); !ok || !slices.Contains(a.Grant.Keys, "perm:files.write:src/**") {
		t.Fatalf("the dev app is on with the new right: %+v", a.Grant)
	}
}
