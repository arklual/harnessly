package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// publicPackage is a stable-ready package with a license; edit changes its manifest first.
func publicPackage(t *testing.T, id, version string, edit func(m map[string]any)) string {
	t.Helper()
	dir := versioned(t, id, version)
	if err := os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("MIT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		path := filepath.Join(dir, ManifestPath)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		raw, _ = json.Marshal(m)
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func publishStable(t *testing.T, r *Registry, dir string) PublishedVersion {
	t.Helper()
	pv, ps, err := r.Publish(dir, ChannelStable, PublishOptions{})
	if err != nil {
		t.Fatalf("publish: %v %v", err, ps)
	}
	return pv
}

func TestPublicCatalogFromPublishToInstall(t *testing.T) {
	author := newRegistry(t)
	pv := publishStable(t, author, publicPackage(t, "acme.demo", "1.0.0", nil))
	e, err := author.PublishPublic("acme.demo", pv.Version, "ann@acme.test", teamNow)
	if err != nil || e.Review != ReviewApproved || e.Channel != "stable" || e.Sum == "" {
		t.Fatalf("publish public: %v %+v", err, e)
	}
	// Anyone browses: by word of the name, the description or the id; none matches nothing.
	for q, want := range map[string]int{"": 1, "demo": 1, "DEMO": 1, "acme": 1, "nothing-like-it": 0} {
		got, err := author.PublicCatalog(q)
		if err != nil || len(got) != want {
			t.Errorf("browse %q: %d entries, want %d (%v)", q, len(got), want, err)
		}
	}

	// A second machine, with no team, gets the platform's reviewed list and installs for the person.
	snap, err := author.PublicSnapshot()
	if err != nil || len(snap.Entries) != 1 || len(snap.Entries[0].Archive) == 0 {
		t.Fatalf("snapshot: %v %+v", err, snap)
	}
	reader := newRegistry(t)
	if problems, err := reader.ApplyPublic(snap, TemplateVersion, teamNow); err != nil || len(problems) != 0 {
		t.Fatalf("apply: %v %v", err, problems)
	}
	listed, _ := reader.PublicCatalog("")
	if len(listed) != 1 || listed[0].Version != "1.0.0" || len(listed[0].Permissions) == 0 {
		t.Fatalf("the reader browses: %+v", listed)
	}
	if _, _, err := reader.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion}); !errors.Is(err, ErrPublicChanged) {
		t.Fatalf("an install that names no sum: %v", err)
	}
	if _, _, err := reader.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: "0000"}); !errors.Is(err, ErrPublicChanged) {
		t.Fatalf("an install of another archive: %v", err)
	}
	got, _, err := reader.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: listed[0].Sum})
	if err != nil || got.ID != "acme.demo" || got.Version != "1.0.0" || got.Team || got.Dev {
		t.Fatalf("install: %v %+v", err, got)
	}
	if list, _ := reader.List(); len(list) != 1 {
		t.Fatalf("installed: %+v", list)
	}
	if err := reader.Remove("acme.demo"); err != nil {
		t.Fatalf("the person's own install is removable: %v", err)
	}
	if _, _, err := reader.InstallFromPublic("acme.none", PublicInstallOptions{HostVersion: TemplateVersion, Sum: "x"}); !errors.Is(err, ErrNotPublic) {
		t.Fatalf("an app that is not there: %v", err)
	}
	// A host that cannot run it says so.
	old := newRegistry(t)
	_, _ = old.ApplyPublic(snap, TemplateVersion, teamNow)
	if _, _, err := old.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: "0.1.0", Sum: listed[0].Sum}); !errors.Is(err, ErrHostVersion) {
		t.Fatalf("a host too old: %v", err)
	}
}

func TestPublicCatalogRefusals(t *testing.T) {
	r := newRegistry(t)

	// Only what stable points at.
	dir := publicPackage(t, "acme.beta", "1.0.0", nil)
	b, _, err := r.Publish(dir, ChannelBeta, PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PublishPublic("acme.beta", b.Version, "ann", teamNow); !errors.Is(err, ErrNotOnStable) {
		t.Errorf("a beta: %v", err)
	}
	if _, err := r.PublishPublic("acme.beta", "9.9.9", "ann", teamNow); !errors.Is(err, ErrNoVersion) {
		t.Errorf("a version never published: %v", err)
	}

	// The trusted tier is in no catalog.
	trusted := publicPackage(t, "acme.trusted", "1.0.0", func(m map[string]any) { m["trust"] = "trusted" })
	tv := publishStable(t, r, trusted)
	if _, err := r.PublishPublic("acme.trusted", tv.Version, "ann", teamNow); !errors.Is(err, ErrTrustedPublic) {
		t.Errorf("a trusted app: %v", err)
	}

	// The checks: a license, no binaries, no warnings.
	noLicense := versioned(t, "acme.nolicense", "1.0.0")
	nv := publishStable(t, r, noLicense)
	if _, err := r.PublishPublic("acme.nolicense", nv.Version, "ann", teamNow); !errors.Is(err, ErrPublishChecks) {
		t.Errorf("no license: %v", err)
	}
	binary := publicPackage(t, "acme.binary", "1.0.0", nil)
	if err := os.WriteFile(filepath.Join(binary, "tool"), append([]byte{0x7f, 'E', 'L', 'F'}, make([]byte, 64)...), 0o644); err != nil {
		t.Fatal(err)
	}
	bv := publishStable(t, r, binary)
	if _, err := r.PublishPublic("acme.binary", bv.Version, "ann", teamNow); !errors.Is(err, ErrPublishChecks) {
		t.Errorf("a native binary: %v", err)
	}
	script := publicPackage(t, "acme.script", "1.0.0", nil)
	if err := os.WriteFile(filepath.Join(script, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	sv := publishStable(t, r, script)
	if _, err := r.PublishPublic("acme.script", sv.Version, "ann", teamNow); err != nil {
		t.Errorf("a script is text, not a binary: %v", err)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 1 || got[0].ID != "acme.script" {
		t.Errorf("only what passed is listed: %+v", got)
	}

	// A snapshot cannot put a trusted app in a machine's catalog, whatever its entry says.
	snap := PublicSnapshot{At: teamNow.UnixMilli()}
	raw, sum, err := Pack(r.VersionDir("acme.trusted", tv.Version))
	if err != nil {
		t.Fatal(err)
	}
	snap.Entries = []PublicSnapshotEntry{{PublicEntry: PublicEntry{CatalogEntry: CatalogEntry{ID: "acme.trusted", Channel: "stable", Version: tv.Version, Name: "x",
		Trust: TrustSandboxed, Sum: sum, Targets: []string{}, Permissions: []string{}}, Review: ReviewApproved}, Archive: raw}}
	other := newRegistry(t)
	problems, err := other.ApplyPublic(snap, TemplateVersion, teamNow)
	if err != nil || len(problems) != 1 {
		t.Fatalf("apply a trusted app: %v %v", err, problems)
	}
	if got, _ := other.PublicCatalog(""); len(got) != 0 {
		t.Errorf("a trusted app got in by a snapshot: %+v", got)
	}
	// An archive that is not the one the entry names is left out too.
	snap.Entries[0].Sum = "feed"
	if problems, _ := other.ApplyPublic(snap, TemplateVersion, teamNow.Add(time.Second)); len(problems) != 1 {
		t.Errorf("a wrong sum: %v", problems)
	}
	// An old snapshot is not taken over a newer one.
	if _, err := other.ApplyPublic(PublicSnapshot{At: 1, Entries: []PublicSnapshotEntry{}}, TemplateVersion, teamNow); !errors.Is(err, ErrStaleTeam) {
		t.Errorf("a stale snapshot: %v", err)
	}
}

func TestPublicNetworkEverywhereWaitsForReview(t *testing.T) {
	r := newRegistry(t)
	dir := publicPackage(t, "acme.wide", "1.0.0", func(m map[string]any) {
		m["network"] = map[string]any{"connect": []string{"*"}, "reason": "the app calls the person's own server"}
	})
	pv := publishStable(t, r, dir)
	e, err := r.PublishPublic("acme.wide", pv.Version, "ann", teamNow)
	if err != nil || e.Review != ReviewPending || len(e.Why) != 1 {
		t.Fatalf("publish: %v %+v", err, e)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 0 {
		t.Fatalf("listed before a look: %+v", got)
	}
	if _, _, err := r.InstallFromPublic("acme.wide", PublicInstallOptions{HostVersion: TemplateVersion, Sum: e.Sum}); !errors.Is(err, ErrNotPublic) {
		t.Fatalf("installed before a look: %v", err)
	}
	if pending, _ := r.PendingPublic(); len(pending) != 1 {
		t.Fatalf("pending: %+v", pending)
	}
	if err := r.ReviewPublic("acme.wide", "9.9.9", true, teamNow); !errors.Is(err, ErrNotPublic) {
		t.Errorf("a review of a version that is not there: %v", err)
	}
	if err := r.ReviewPublic("acme.wide", pv.Version, true, teamNow); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 1 {
		t.Fatalf("listed after a look: %+v", got)
	}
	if err := r.ReviewPublic("acme.wide", pv.Version, true, teamNow); !errors.Is(err, ErrNotPublic) {
		t.Errorf("a review twice: %v", err)
	}

	// A refusal takes the entry out.
	pv2 := publishStable(t, r, publicPackage(t, "acme.wider", "1.0.0", func(m map[string]any) {
		m["network"] = map[string]any{"frames": []string{"*"}, "connect": []string{"*"}, "reason": "x"}
	}))
	if _, err := r.PublishPublic("acme.wider", pv2.Version, "ann", teamNow); err != nil {
		t.Fatal(err)
	}
	if err := r.ReviewPublic("acme.wider", pv2.Version, false, teamNow); err != nil {
		t.Fatal(err)
	}
	if pending, _ := r.PendingPublic(); len(pending) != 0 {
		t.Errorf("a refused entry stays: %+v", pending)
	}
}

func publicSnapshotOf(t *testing.T) PublicSnapshot {
	t.Helper()
	author := newRegistry(t)
	pv := publishStable(t, author, publicPackage(t, "acme.demo", "1.0.0", nil))
	if _, err := author.PublishPublic("acme.demo", pv.Version, "ann@acme.test", teamNow); err != nil {
		t.Fatal(err)
	}
	snap, err := author.PublicSnapshot()
	if err != nil || len(snap.Entries) != 1 {
		t.Fatalf("snapshot: %v %+v", err, snap)
	}
	return snap
}

// A snapshot is not trusted for what the person is shown: name, tier, targets and rights come from the archive, also for an
// entry that this host is too old to run.
func TestApplyPublicTakesMetadataFromTheArchive(t *testing.T) {
	for _, host := range []string{TemplateVersion, "0.1.0"} {
		snap := publicSnapshotOf(t)
		real := snap.Entries[0].Permissions
		e := &snap.Entries[0]
		e.Name, e.Description, e.Permissions, e.Targets = "Harmless", "nothing to see", []string{}, []string{"chat"}
		r := newRegistry(t)
		if problems, err := r.ApplyPublic(snap, host, teamNow); err != nil || len(problems) != 0 {
			t.Fatalf("host %s: %v %v", host, err, problems)
		}
		got, _ := r.PublicCatalog("")
		if len(got) != 1 || got[0].Name == "Harmless" || !sameSet(got[0].Permissions, real) || len(got[0].Permissions) == 0 {
			t.Fatalf("host %s: the entry shows what the snapshot said: %+v", host, got)
		}
	}
	// An archive that cannot be read leaves the entry out.
	snap := publicSnapshotOf(t)
	snap.Entries[0].Archive = []byte("not an archive")
	r := newRegistry(t)
	problems, err := r.ApplyPublic(snap, TemplateVersion, teamNow)
	if got, _ := r.PublicCatalog(""); err != nil && len(got) != 0 || err == nil && (len(problems) == 0 || len(got) != 0) {
		t.Fatalf("a broken archive is listed: %v %v %+v", err, problems, got)
	}
}

// What is installed is what was shown: an entry whose rights are not the archive's is refused, and the shared folder of the
// catalog is not what the install is made from, so swapping it between the check and the install changes nothing.
func TestInstallFromPublicRefusesOtherRights(t *testing.T) {
	snap := publicSnapshotOf(t)
	r := newRegistry(t)
	if _, err := r.ApplyPublic(snap, TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	listed, _ := r.PublicCatalog("")
	shared := r.publicPackageDir("acme.demo", "1.0.0")
	path := filepath.Join(shared, ManifestPath)
	orig, _ := os.ReadFile(path)
	var m map[string]any
	_ = json.Unmarshal(orig, &m)
	m["description"] = "swapped after the check"
	swapped, _ := json.Marshal(m)
	if err := os.WriteFile(path, swapped, 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, err := r.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: listed[0].Sum})
	if err == nil && got.Source != shared {
		t.Fatalf("the install says it came from %s, not the catalog's folder %s", got.Source, shared)
	}
	if err != nil || got.Manifest.Description == "swapped after the check" {
		t.Fatalf("the install was made from the shared folder: %v %+v", err, got.Manifest)
	}
	if err := r.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}

	// The entry says other rights than the archive holds.
	p, err := r.readPublic()
	if err != nil || len(p.Entries) != 1 {
		t.Fatalf("read: %v", err)
	}
	p.Entries[0].Permissions = []string{"storage"}
	if err := r.writePublic(p); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.InstallFromPublic("acme.demo", PublicInstallOptions{HostVersion: TemplateVersion, Sum: listed[0].Sum}); !errors.Is(err, ErrPublicChanged) {
		t.Fatalf("other rights than shown: %v", err)
	}
	if list, _ := r.List(); len(list) != 0 {
		t.Fatalf("installed anyway: %+v", list)
	}
}

// A folder left from another archive of the same id and version is not reused: it is unpacked afresh.
func TestUnpackArchiveReusesOnlyItsOwnFolder(t *testing.T) {
	pkgA := publicPackage(t, "acme.demo", "1.0.0", nil)
	pkgB := publicPackage(t, "acme.demo", "1.0.0", func(m map[string]any) { m["description"] = "the other one" })
	rawA, _, errA := Pack(pkgA)
	rawB, _, errB := Pack(pkgB)
	if errA != nil || errB != nil {
		t.Fatalf("pack: %v %v", errA, errB)
	}
	dst := filepath.Join(t.TempDir(), "unpacked")
	for name, tc := range map[string]struct {
		raw []byte
		sum string
	}{"first": {rawA, ArchiveSum(rawA)}, "other archive over it": {rawB, ArchiveSum(rawB)}, "first again": {rawA, ArchiveSum(rawA)}} {
		pkg, err := unpackArchive(tc.raw, "acme.demo", "1.0.0", tc.sum, TemplateVersion, dst)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := "the other one"
		if tc.sum == ArchiveSum(rawA) {
			want = pkgDescription(t, pkgA)
		}
		if pkg.Manifest.Description != want {
			t.Fatalf("%s: the folder holds %q, want %q", name, pkg.Manifest.Description, want)
		}
	}
	// A folder with no marker (left by an older run) is replaced, and the marker names the archive that made it.
	_ = os.Remove(dst + ".sum")
	if err := os.WriteFile(filepath.Join(dst, "planted.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := unpackArchive(rawA, "acme.demo", "1.0.0", ArchiveSum(rawA), TemplateVersion, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "planted.txt")); err == nil {
		t.Fatal("a folder of unknown origin was reused")
	}
	if b, _ := os.ReadFile(dst + ".sum"); string(b) != ArchiveSum(rawA)+"\n" {
		t.Fatalf("marker: %q", b)
	}
}

func pkgDescription(t *testing.T, dir string) string {
	t.Helper()
	pkg, _ := LoadPackage(dir)
	if pkg == nil {
		t.Fatal("no package")
	}
	return pkg.Manifest.Description
}

// The platform's list carries the author's own entry that waits for a look: it stays pending on the machine, is not
// listed, cannot be installed and is not offered as approved.
func TestApplyPublicKeepsAnAuthorsPendingEntryPending(t *testing.T) {
	author := newRegistry(t)
	dir := publicPackage(t, "acme.wide", "1.0.0", func(m map[string]any) {
		m["network"] = map[string]any{"connect": []string{"*"}, "reason": "the app calls the person's own server"}
	})
	pv := publishStable(t, author, dir)
	e, err := author.PublishPublic("acme.wide", pv.Version, "ann", teamNow)
	if err != nil || e.Review != ReviewPending {
		t.Fatalf("publish: %v %+v", err, e)
	}
	snap, err := author.PublicSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	// The hub hands the list back, the pending entry in it; with its bytes and without them.
	for name, entries := range map[string][]PublicSnapshotEntry{"with bytes": snap.Entries, "without": {{PublicEntry: snap.Entries[0].PublicEntry}}} {
		list := PublicSnapshot{At: snap.At + 1, Entries: entries}
		if problems, err := author.ApplyPublic(list, TemplateVersion, teamNow); err != nil || len(problems) != 0 {
			t.Fatalf("%s: %v %v", name, err, problems)
		}
		if got, _ := author.PublicCatalog(""); len(got) != 0 {
			t.Errorf("%s: listed as approved: %+v", name, got)
		}
		pending, _ := author.PendingPublic()
		if len(pending) != 1 || pending[0].Review != ReviewPending || len(pending[0].Why) != 1 {
			t.Errorf("%s: pending after the list: %+v", name, pending)
		}
		if _, _, err := author.InstallFromPublic("acme.wide", PublicInstallOptions{HostVersion: TemplateVersion, Sum: e.Sum}); !errors.Is(err, ErrNotPublic) {
			t.Errorf("%s: installed while pending: %v", name, err)
		}
		snap.At++
	}
}

func TestPublishPublicTellsTheHook(t *testing.T) {
	r := newRegistry(t)
	calls := 0
	r.OnPublicChange(func() { calls++ })
	pv := publishStable(t, r, publicPackage(t, "acme.demo", "1.0.0", nil))
	if _, err := r.PublishPublic("acme.demo", pv.Version, "ann", teamNow); err != nil || calls != 1 {
		t.Fatalf("publish: %v %d", err, calls)
	}
	if _, err := r.PublishPublic("acme.demo", "9.9.9", "ann", teamNow); err == nil || calls != 1 {
		t.Fatalf("a refused publish tells no one: %v %d", err, calls)
	}
	snap, _ := r.PublicSnapshot()
	snap.At++
	if _, err := r.ApplyPublic(snap, TemplateVersion, teamNow); err != nil || calls != 1 {
		t.Fatalf("a list from the platform tells no one: %v %d", err, calls)
	}
}

// An entry this machine published after the platform's list was made stays, though the list lacks it; a later list that
// still lacks it drops it.
func TestApplyPublicKeepsAFreshOwnEntryUntilALaterListLacksIt(t *testing.T) {
	r := newRegistry(t)
	pv := publishStable(t, r, publicPackage(t, "acme.demo", "1.0.0", nil))
	listAt := teamNow.UnixMilli()
	publishedAt := teamNow.Add(time.Minute)
	e, err := r.PublishPublic("acme.demo", pv.Version, "ann", publishedAt)
	if err != nil {
		t.Fatal(err)
	}
	// A list made before the publish, not holding it (the hub had not read the snapshot): not stale, and the entry stays.
	if problems, err := r.ApplyPublic(PublicSnapshot{At: listAt, Entries: []PublicSnapshotEntry{}}, TemplateVersion, publishedAt); err != nil || len(problems) != 0 {
		t.Fatalf("early list: %v %v", err, problems)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 1 || got[0].Sum != e.Sum {
		t.Fatalf("the fresh entry went with an earlier list: %+v", got)
	}
	// The snapshot this machine gives the platform still has it.
	if snap, _ := r.PublicSnapshot(); len(snap.Entries) != 1 || snap.At < publishedAt.UnixMilli() {
		t.Fatalf("snapshot: %+v", snap)
	}
	// An older list than the one taken is stale.
	if _, err := r.ApplyPublic(PublicSnapshot{At: listAt - 1, Entries: []PublicSnapshotEntry{}}, TemplateVersion, publishedAt); !errors.Is(err, ErrStaleTeam) {
		t.Fatalf("stale: %v", err)
	}
	// A list made after the publish that still lacks it: the platform dropped it, so does the machine.
	if _, err := r.ApplyPublic(PublicSnapshot{At: publishedAt.UnixMilli() + 1, Entries: []PublicSnapshotEntry{}}, TemplateVersion, publishedAt); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 0 {
		t.Fatalf("a later list lacks it, still listed: %+v", got)
	}
	if snap, _ := r.PublicSnapshot(); len(snap.Entries) != 0 {
		t.Fatalf("still in the snapshot: %+v", snap.Entries)
	}
}

// An entry the platform has listed is the platform's from then on: a later list without it drops it, however fresh.
func TestApplyPublicDropsAnEntryOnceTheListHeldIt(t *testing.T) {
	r := newRegistry(t)
	pv := publishStable(t, r, publicPackage(t, "acme.demo", "1.0.0", nil))
	if _, err := r.PublishPublic("acme.demo", pv.Version, "ann", teamNow); err != nil {
		t.Fatal(err)
	}
	snap, _ := r.PublicSnapshot()
	snap.At = teamNow.UnixMilli() - 1000
	if _, err := r.ApplyPublic(snap, TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, err := r.ApplyPublic(PublicSnapshot{At: teamNow.UnixMilli() - 500, Entries: []PublicSnapshotEntry{}}, TemplateVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.PublicCatalog(""); len(got) != 0 {
		t.Fatalf("the platform dropped it: %+v", got)
	}
}
