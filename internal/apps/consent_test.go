package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// pkgWith is a scaffolded package of id at version asking for the rights given.
func pkgWith(t *testing.T, id, version string, rights ...string) string {
	t.Helper()
	dir := scaffold(t, id, "chat")
	file := filepath.Join(dir, ManifestPath)
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = version
	if rights == nil {
		rights = []string{}
	}
	m["permissions"] = rights
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(file, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func listed(t *testing.T, r *Registry, id string) (Installed, bool) {
	t.Helper()
	list, err := r.List()
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(list, func(a Installed) bool { return a.ID == id })
	if i < 0 {
		return Installed{}, false
	}
	return list[i], true
}

func TestConsentFirstInstallWaitsForTheClick(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	got, _, err := r.Install(pkgWith(t, "acme.files", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	if !got.NeedsConsent() {
		t.Fatal("an install from the CLI that reaches files must wait for the person")
	}
	if _, ok := listed(t, r, "acme.files"); ok {
		t.Fatal("an app waiting for consent must not be on")
	}
	if st := StateOf(got, "", nil, nil, nil); st.Reason != ReasonConsent {
		t.Fatalf("state %+v, want the consent reason", st)
	}
	if _, err := r.Consent("acme.files", "0.9.0", got.Wants, time.Now()); !errors.Is(err, ErrNoConsentWaiting) {
		t.Fatalf("a click on another version than the one waiting: %v", err)
	}
	if _, err := r.Consent("acme.files", "1.0.0", got.Wants, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.files"); !ok {
		t.Fatal("after the click the app is on")
	}
}

func TestConsentNothingToAgreeToInstallsAtOnce(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	if _, _, err := r.Install(pkgWith(t, "acme.plain", "1.0.0"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.plain"); !ok {
		t.Fatal("an app that reaches nothing needs no consent")
	}
}

func TestConsentUpdateThatGrowsWaitsBesideTheRunningVersion(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	if _, _, err := r.Install(pkgWith(t, "acme.grow", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentGiven}); err != nil {
		t.Fatal(err)
	}
	// The same rights: goes on.
	if _, _, err := r.Install(pkgWith(t, "acme.grow", "1.1.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if a, _ := listed(t, r, "acme.grow"); a.Version != "1.1.0" || a.Pending != nil {
		t.Fatalf("an update that reaches no more goes on: %+v", a)
	}
	// More: waits, 1.1.0 keeps running.
	if _, _, err := r.Install(pkgWith(t, "acme.grow", "1.2.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	a, ok := listed(t, r, "acme.grow")
	if !ok || a.Version != "1.1.0" || a.Pending == nil || a.Pending.Version != "1.2.0" {
		t.Fatalf("want 1.1.0 running with 1.2.0 waiting, got %+v", a)
	}
	if _, err := os.Stat(a.Dir); err != nil {
		t.Fatalf("the running copy must stay: %v", err)
	}
	// The same version that reaches more is refused: a new right comes with a new version.
	if _, _, err := r.Install(pkgWith(t, "acme.grow", "1.1.0", "files.read:src/**", "notify"), InstallOptions{Consent: ConsentAsk}); !errors.Is(err, ErrSameVersionGrew) {
		t.Fatalf("want ErrSameVersionGrew, got %v", err)
	}
	// «Отмена»: the update is dropped, 1.1.0 stays.
	if removed, err := r.Decline("acme.grow"); err != nil || removed {
		t.Fatalf("decline: %v %v", removed, err)
	}
	if a, _ := listed(t, r, "acme.grow"); a.Pending != nil || a.Version != "1.1.0" {
		t.Fatalf("after decline: %+v", a)
	}
	// Again, and «Обновить».
	if _, _, err := r.Install(pkgWith(t, "acme.grow", "1.2.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	waiting, _ := listed(t, r, "acme.grow")
	next, err := r.Consent("acme.grow", "1.2.0", waiting.Pending.Wants, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if next.Previous != "1.1.0" || next.NeedsConsent() {
		t.Fatalf("after consent: %+v", next)
	}
	if a, _ := listed(t, r, "acme.grow"); a.Version != "1.2.0" {
		t.Fatalf("after consent the new version runs: %+v", a)
	}
}

func TestConsentInstallsFromBeforeKeepTheirRights(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	// The host's own install (and every install made before consent was asked) has no Grant.
	if _, _, err := r.Install(pkgWith(t, "acme.old", "1.0.0", "files.read:**", "notify"), InstallOptions{}); err != nil {
		t.Fatal(err)
	}
	a, ok := listed(t, r, "acme.old")
	if !ok || a.Grant != nil {
		t.Fatalf("a host install is on with no grant: %+v", a)
	}
	// An install written before Wants was kept reads what its package reaches.
	a.Wants = nil
	list, _ := r.read()
	list[0] = a
	if err := r.write(list); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Install(pkgWith(t, "acme.old", "1.0.1", "files.read:**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if a, ok := listed(t, r, "acme.old"); !ok || a.Version != "1.0.1" {
		t.Fatalf("a narrowing update of an old install goes on: %+v", a)
	}
}

func TestSwitchOffTakesTheAppOut(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	if _, _, err := r.Install(pkgWith(t, "acme.sw", "1.0.0", "notify"), InstallOptions{Consent: ConsentGiven}); err != nil {
		t.Fatal(err)
	}
	if err := r.SetOff("acme.sw", true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("acme.sw"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("an app turned off is not reached: %v", err)
	}
	all, _ := r.ListAll()
	if len(all) != 1 || StateOf(all[0], "", nil, nil, nil).Reason != ReasonOff {
		t.Fatalf("the page still lists it, off: %+v", all)
	}
	// An update keeps the switch.
	if _, _, err := r.Install(pkgWith(t, "acme.sw", "1.0.1", "notify"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if all, _ := r.ListAll(); !all[0].Off {
		t.Fatal("an update must not turn the app back on")
	}
	if err := r.SetOff("acme.sw", false); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("acme.sw"); err != nil {
		t.Fatal(err)
	}
}

func TestJournalNewestFirstAndBounded(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	for i := range 3 {
		if err := r.Journal("acme.j", JournalEntry{At: int64(i + 1), What: "secret.call", Class: ClassHigh, OK: true}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := r.ReadJournal("acme.j", 2)
	if err != nil || len(got) != 2 || got[0].At != 3 {
		t.Fatalf("journal %+v %v", got, err)
	}
	if none, err := r.ReadJournal("acme.none", 10); err != nil || none == nil || len(none) != 0 {
		t.Fatalf("an app with no journal has an empty one: %v %v", none, err)
	}
}

func TestAccessOrderAndWords(t *testing.T) {
	m := &Manifest{Permissions: []string{"notify", "files.read:src/**", "secrets:GITHUB_TOKEN", "chat.prompt"},
		Network: &Network{Connect: []string{"api.example.com"}}}
	pkg := &Package{MCP: map[string]MCPServer{"local": {Command: "node"}}, PluginParts: []string{"hooks/"}}
	got := AccessOf(m, pkg)
	if got[0].Class != ClassCritical || got[0].Key != "part:hooks/" {
		t.Fatalf("hooks first: %+v", got[0])
	}
	for i := 1; i < len(got); i++ {
		if slices.Index(Classes, got[i-1].Class) > slices.Index(Classes, got[i].Class) {
			t.Fatalf("not from the most dangerous: %+v", got)
		}
	}
	for _, a := range got {
		if a.Label.Ru == "" || a.Label.En == "" {
			t.Errorf("%s has no words", a.Key)
		}
		if Loud(a.Class) && (a.Threat.Ru == "" || a.Threat.En == "") {
			t.Errorf("%s is %s and says no threat", a.Key, a.Class)
		}
	}
	if i := slices.IndexFunc(got, func(a Access) bool { return a.Key == "perm:files.read:src/**" }); i < 0 || got[i].Label.Ru != "Читать файлы src/**" {
		t.Fatalf("files in words: %+v", got)
	}
}

func TestEverySurfaceRightHasAClassAndWords(t *testing.T) {
	for _, p := range surfacePermissions {
		if !slices.Contains(Classes, p.Class) {
			t.Errorf("%s: class %q", p.Name, p.Class)
		}
		if p.Label.Ru == "" || p.Label.En == "" {
			t.Errorf("%s: no words", p.Name)
		}
		if Loud(p.Class) && (p.Threat.Ru == "" || p.Threat.En == "") {
			t.Errorf("%s: a %s right says no threat", p.Name, p.Class)
		}
	}
}

func TestConsentGrantsOnlyWhatTheScreenShowed(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	shown, _, err := r.Install(pkgWith(t, "acme.swap", "1.0.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk})
	if err != nil {
		t.Fatal(err)
	}
	// The same version is installed again, reaching more, after the screen was drawn.
	if _, _, err := r.Install(pkgWith(t, "acme.swap", "1.0.0", "files.read:src/**", "files.write:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Consent("acme.swap", "1.0.0", shown.Wants, time.Now()); !errors.Is(err, ErrConsentChanged) {
		t.Fatalf("a click on the old screen grants the new package: %v", err)
	}
	if _, ok := listed(t, r, "acme.swap"); ok {
		t.Fatal("refused, the app stays off")
	}

	// An update the same: the pending one swapped after the screen showed it.
	if _, _, err := r.Install(pkgWith(t, "acme.up", "1.0.0"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Install(pkgWith(t, "acme.up", "1.1.0", "files.read:src/**"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	a, _ := r.Get("acme.up")
	if a.Pending == nil {
		t.Fatal("the update waits")
	}
	drawn := a.Pending.Wants
	if _, _, err := r.Install(pkgWith(t, "acme.up", "1.1.0", "files.read:src/**", "chat.prompt"), InstallOptions{Consent: ConsentAsk}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Consent("acme.up", "1.1.0", drawn, time.Now()); !errors.Is(err, ErrConsentChanged) {
		t.Fatalf("a click on the old screen grants the swapped update: %v", err)
	}
	if got, _ := r.Get("acme.up"); got.Version != "1.0.0" {
		t.Fatalf("refused, the old version keeps running: %+v", got)
	}
}

func TestDevFolderThatGrowsWaitsAgain(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	dir := pkgWith(t, "acme.dev", "0.1.0", "files.read:src/**")
	got, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Consent("acme.dev", "0.1.0", got.Wants, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.dev"); !ok {
		t.Fatal("agreed, the dev app is on")
	}
	// The author's folder adds a right without a new install: the app waits for the person again.
	raw, _ := os.ReadFile(filepath.Join(dir, ManifestPath))
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	m["permissions"] = []string{"files.read:src/**", "files.write:src/**"}
	raw, _ = json.Marshal(m)
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.dev"); ok {
		t.Fatal("a dev folder that reaches more stays on without the person's word")
	}
	// Reinstalled from the same folder with the same version, it waits too (no ErrSameVersionGrew for a draft).
	if _, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true}); err != nil {
		t.Fatal(err)
	}
	all, _ := r.read()
	a := all[slices.IndexFunc(all, func(a Installed) bool { return a.ID == "acme.dev" })]
	if !a.NeedsConsent() {
		t.Fatalf("reinstalled, the grown draft waits: wants %v grant %+v dev %v", a.Wants, a.Grant, a.Dev)
	}
	if _, err := r.Consent("acme.dev", "0.1.0", got.Wants, time.Now()); !errors.Is(err, ErrConsentChanged) {
		t.Fatalf("the old screen grants the grown draft: %v", err)
	}
	if _, err := r.Consent("acme.dev", "0.1.0", AccessKeys(AccessOf(mustManifest(t, dir), nil)), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.dev"); !ok {
		t.Fatal("agreed again, the dev app is on")
	}
}

func mustManifest(t *testing.T, dir string) *Manifest {
	t.Helper()
	pkg, _ := LoadPackage(dir)
	if pkg == nil || pkg.Manifest == nil {
		t.Fatal("no manifest in " + dir)
	}
	return pkg.Manifest
}

func TestDevFolderReadFailsClosed(t *testing.T) {
	r := &Registry{Root: t.TempDir()}
	dir := pkgWith(t, "acme.gone", "0.1.0", "files.read:src/**")
	got, _, err := r.Install(dir, InstallOptions{Consent: ConsentAsk, Dev: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Consent("acme.gone", "0.1.0", got.Wants, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Same size, same modification time, other rights: the stamp still sees the edit (the inode's change time).
	file := filepath.Join(dir, ManifestPath)
	info, _ := os.Stat(file)
	raw, _ := os.ReadFile(file)
	swapped := strings.Replace(string(raw), "files.read:src/**", "files.write:src/*", 1)
	if len(swapped) != len(raw) {
		t.Fatal("the swap must keep the size")
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(file, []byte(swapped), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(file, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed(t, r, "acme.gone"); ok {
		t.Fatal("an edit that keeps the size and the time slips past the stamp")
	}
	// A folder that cannot be read is not known to reach nothing: the app waits.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	all, _ := r.read()
	if a := all[slices.IndexFunc(all, func(a Installed) bool { return a.ID == "acme.gone" })]; !a.NeedsConsent() {
		t.Fatal("an unreadable dev folder must wait, not run with what it had")
	}
}
