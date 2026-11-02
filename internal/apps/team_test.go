package apps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var teamNow = time.UnixMilli(1_800_000_000_000)

// publishedToTeam publishes a package on stable and puts it in the catalog of r.
func publishedToTeam(t *testing.T, r *Registry, id, version string) CatalogEntry {
	t.Helper()
	pv, _, err := r.Publish(versioned(t, id, version), ChannelStable, PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.PublishToTeam(id, pv.Version, ChannelStable, "ann@team.test", FirstVersion, teamNow)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestArchiveRoundTripAndRefusals(t *testing.T) {
	dir := versioned(t, "acme.demo", "1.0.0")
	raw, sum, err := Pack(dir)
	if err != nil || ArchiveSum(raw) != sum {
		t.Fatalf("pack: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "out")
	if err := Unpack(raw, dst); err != nil {
		t.Fatal(err)
	}
	if pkg, ps := LoadPackage(dst); pkg == nil || len(ps.Errors()) > 0 || pkg.Manifest.ID != "acme.demo" {
		t.Fatalf("unpacked: %v", ps)
	}
	if err := Unpack([]byte("not gzip"), filepath.Join(t.TempDir(), "x")); !errors.Is(err, ErrArchive) {
		t.Fatalf("garbage: %v", err)
	}
	for _, name := range []string{"../escape", "/abs", "a/../../b"} {
		bad := tarOf(t, name, 'f')
		if err := Unpack(bad, filepath.Join(t.TempDir(), "x")); !errors.Is(err, ErrArchive) {
			t.Errorf("%s: want refused, got %v", name, err)
		}
	}
	if err := Unpack(tarOf(t, "link", 'l'), filepath.Join(t.TempDir(), "x")); !errors.Is(err, ErrArchive) {
		t.Errorf("a link entry: want refused, got %v", err)
	}
}

func TestOwnerInstallsForTheTeamAndMembersGetIt(t *testing.T) {
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	e := publishedToTeam(t, owner, "acme.demo", "1.0.0")
	if e.Version != "1.0.0" || e.Channel != "stable" {
		t.Fatalf("entry: %+v", e)
	}
	// In the catalog and not installed: nobody has the app yet.
	if list, _ := owner.List(); len(list) != 0 {
		t.Fatalf("catalog only: %+v", list)
	}
	ti, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, By: "ann@team.test", Now: teamNow.Add(time.Minute)})
	if err != nil || ti.Version != "1.0.0" {
		t.Fatalf("install for team: %v %+v", err, ti)
	}
	if list, _ := owner.List(); len(list) != 1 || !list[0].Team {
		t.Fatalf("the owner has it: %+v", list)
	}

	// The platform hands the owner's snapshot to a member's machine; the member did nothing.
	snap, err := owner.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	snap.Role = RoleMember
	member := newRegistry(t)
	problems, err := member.ApplyTeam(snap, FirstVersion, teamNow.Add(2*time.Minute))
	if err != nil || len(problems) > 0 {
		t.Fatalf("apply: %v %v", err, problems)
	}
	list, _ := member.List()
	if len(list) != 1 || list[0].ID != "acme.demo" || !list[0].Team || list[0].Version != "1.0.0" {
		t.Fatalf("the member's apps: %+v", list)
	}
	if pkg, err := member.Package("acme.demo"); err != nil || pkg.Manifest.Version != "1.0.0" {
		t.Fatalf("the member can load the package: %v", err)
	}
	if _, err := os.Stat(filepath.Join(list[0].Dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("the plugin manifest the agent loads skills by: %v", err)
	}

	// A member does not install for the team, nor remove its app; they turn it off for themselves.
	if _, err := member.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, Now: teamNow}); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("member install: %v", err)
	}
	if err := member.UninstallForTeam("acme.demo", teamNow); !errors.Is(err, ErrNotOwner) {
		t.Fatalf("member uninstall: %v", err)
	}
	if err := member.Remove("acme.demo"); !errors.Is(err, ErrTeamApp) {
		t.Fatalf("member remove: %v", err)
	}
	if err := member.MuteTeamApp("acme.demo", true); err != nil {
		t.Fatal(err)
	}
	if list, _ := member.List(); len(list) != 0 {
		t.Fatalf("muted: %+v", list)
	}
	if all, _ := member.ListAll(); len(all) != 1 || !all[0].Muted {
		t.Fatalf("muted, still listed for the page: %+v", all)
	}
	// A newer snapshot keeps the member's choice.
	snap.At += 10
	if _, err := member.ApplyTeam(snap, FirstVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if list, _ := member.List(); len(list) != 0 {
		t.Fatalf("still muted after a snapshot: %+v", list)
	}
	if err := member.MuteTeamApp("acme.demo", false); err != nil {
		t.Fatal(err)
	}
	if list, _ := member.List(); len(list) != 1 {
		t.Fatalf("on again: %+v", list)
	}

	// The member's own install of the same app wins over the team's.
	own := versioned(t, "acme.demo", "0.9.0")
	if _, _, err := member.Install(own, InstallOptions{HostVersion: FirstVersion}); err != nil {
		t.Fatal(err)
	}
	if got, _ := member.Get("acme.demo"); got.Team || got.Version != "0.9.0" {
		t.Fatalf("own wins: %+v", got)
	}
	// An older snapshot is not taken.
	old := snap
	old.At = 1
	if _, err := member.ApplyTeam(old, FirstVersion, teamNow); !errors.Is(err, ErrStaleTeam) {
		t.Fatalf("stale: %v", err)
	}
	// The owner takes it off the team's machines.
	if err := owner.UninstallForTeam("acme.demo", teamNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if list, _ := owner.List(); len(list) != 0 {
		t.Fatalf("uninstalled: %+v", list)
	}
}

func TestTrustedIsNeverInstalledForTheTeam(t *testing.T) {
	owner := newRegistry(t)
	_ = owner.SetTeam("sp1", RoleOwner, teamNow)
	dir := versioned(t, "acme.risky", "1.0.0")
	pkg, _ := LoadPackage(dir)
	pkg.Manifest.Trust = TrustTrusted
	writeManifest(t, dir, pkg.Manifest)
	if _, ps := LoadPackage(dir); len(ps.Errors()) > 0 {
		t.Skipf("the manifest of a trusted app needs more than the trust field: %v", ps)
	}
	pv, _, err := owner.Publish(dir, ChannelStable, PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := owner.PublishToTeam("acme.risky", pv.Version, ChannelStable, "x", FirstVersion, teamNow); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.InstallForTeam("acme.risky", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, Now: teamNow}); !errors.Is(err, ErrTrustedTeam) {
		t.Fatalf("install: %v", err)
	}
	// A snapshot that carries one is applied without it.
	snap, _ := owner.Snapshot()
	snap.Installs = []SnapshotInstall{{ID: "acme.risky", Channel: "stable", Version: snap.Catalog[0].Version, Sum: snap.Catalog[0].Sum, Archive: snap.Catalog[0].Archive, At: 1}}
	snap.Role = RoleMember
	member := newRegistry(t)
	problems, err := member.ApplyTeam(snap, FirstVersion, teamNow)
	if err != nil || len(problems) == 0 {
		t.Fatalf("want a problem and no install: %v %v", err, problems)
	}
	if list, _ := member.List(); len(list) != 0 {
		t.Fatalf("a trusted app reached a member: %+v", list)
	}
}

func TestNoTeamNoCatalog(t *testing.T) {
	r := newRegistry(t)
	pv, _, err := r.Publish(versioned(t, "acme.demo", "1.0.0"), ChannelStable, PublishOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.PublishToTeam("acme.demo", pv.Version, ChannelStable, "x", FirstVersion, teamNow); !errors.Is(err, ErrNoTeam) {
		t.Fatalf("publish: %v", err)
	}
	if _, err := r.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{Now: teamNow}); !errors.Is(err, ErrNoTeam) {
		t.Fatalf("install: %v", err)
	}
}

func TestTeamInstallFollowsCatalogAndRollsBack(t *testing.T) {
	owner := newRegistry(t)
	_ = owner.SetTeam("sp1", RoleOwner, teamNow)
	publishedToTeam(t, owner, "acme.demo", "1.0.0")
	if _, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, Now: teamNow}); err != nil {
		t.Fatal(err)
	}
	// A minor in the catalog reaches the install; a major does not.
	publishedToTeam(t, owner, "acme.demo", "1.1.0")
	if got, _ := owner.Get("acme.demo"); got.Version != "1.1.0" {
		t.Fatalf("minor: %s", got.Version)
	}
	publishedToTeam(t, owner, "acme.demo", "2.0.0")
	if got, _ := owner.Get("acme.demo"); got.Version != "1.1.0" {
		t.Fatalf("major waits: %s", got.Version)
	}
	// Rolling the catalog back makes the entry 1.1.0 again: the install is already there.
	e, err := owner.RollbackCatalog("acme.demo", ChannelStable, FirstVersion, teamNow)
	if err != nil || e.Version != "1.1.0" {
		t.Fatalf("rollback: %v %+v", err, e)
	}
	if _, err := owner.RollbackCatalog("acme.demo", ChannelStable, FirstVersion, teamNow); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("only one step is kept: %v", err)
	}
	info, _ := owner.Team()
	if len(info.Catalog) != 1 || !info.Catalog[0].Installed || info.Catalog[0].At != "1.1.0" {
		t.Fatalf("team view: %+v", info)
	}
}

func TestSnapshotRefusesWhatItShould(t *testing.T) {
	good := TeamSnapshot{Space: "s", Role: RoleMember, At: 5}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, s := range map[string]TeamSnapshot{
		"no time":        {Space: "s", Role: RoleMember},
		"role alone":     {Role: RoleMember, At: 1},
		"unknown role":   {Space: "s", Role: "admin", At: 1},
		"bad entry":      {Space: "s", Role: RoleMember, At: 1, Catalog: []SnapshotEntry{{CatalogEntry: CatalogEntry{ID: "../x", Version: "1.0.0", Channel: "stable"}}}},
		"bad channel":    {Space: "s", Role: RoleMember, At: 1, Catalog: []SnapshotEntry{{CatalogEntry: CatalogEntry{ID: "a.b", Version: "1.0.0", Channel: "nightly"}}}},
		"bad install id": {Space: "s", Role: RoleMember, At: 1, Installs: []SnapshotInstall{{ID: "x", Channel: "stable", Version: "1.0.0", Sum: "s"}}}, "install without a pin": {Space: "s", Role: RoleMember, At: 1, Installs: []SnapshotInstall{{ID: "a.b", Channel: "stable"}}},
	} {
		if err := s.Validate(); err == nil {
			t.Errorf("%s: want refused", name)
		}
	}
	// A tampered archive is not taken for the entry's sum.
	owner := newRegistry(t)
	_ = owner.SetTeam("sp1", RoleOwner, teamNow)
	publishedToTeam(t, owner, "acme.demo", "1.0.0")
	snap, _ := owner.Snapshot()
	snap.Installs = []SnapshotInstall{{ID: "acme.demo", Channel: "stable", Version: snap.Catalog[0].Version, Sum: snap.Catalog[0].Sum, Archive: bytes.Repeat([]byte{1}, 64), At: 1}}
	snap.Catalog[0].Archive = bytes.Repeat([]byte{1}, 64)
	member := newRegistry(t)
	problems, _ := member.ApplyTeam(snap, FirstVersion, teamNow)
	if len(problems) == 0 {
		t.Fatal("want the tampered archive reported")
	}
	if list, _ := member.List(); len(list) != 0 {
		t.Fatalf("installed from a tampered archive: %+v", list)
	}
}

// tarOf is a one-entry gzip tar whose entry is named name; kind 'f' a file, 'l' a symlink.
func tarOf(t *testing.T, name string, kind byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: name, Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}
	if kind == 'l' {
		hdr = &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if kind == 'f' {
		_, _ = tw.Write([]byte("x"))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

// A version a member puts in the catalog does not reach the team's machines: they get the package the owner installed.
func TestSnapshotInstallsThePackageTheOwnerApproved(t *testing.T) {
	owner := newRegistry(t)
	if err := owner.SetTeam("sp1", RoleOwner, teamNow); err != nil {
		t.Fatal(err)
	}
	e1 := publishedToTeam(t, owner, "acme.demo", "1.0.0")
	if _, err := owner.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, By: "ann", Now: teamNow.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	// Someone without owner rights publishes a newer version to the same catalog entry.
	if err := owner.SetTeam("sp1", RoleMember, teamNow.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e2 := publishedToTeam(t, owner, "acme.demo", "1.1.0")
	snap, err := owner.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Installs) != 1 || snap.Installs[0].Version != "1.0.0" || snap.Installs[0].Sum != e1.Sum {
		t.Fatalf("the snapshot pins the owner's install: %+v", snap.Installs)
	}
	if snap.Catalog[0].Version != "1.1.0" || snap.Catalog[0].Sum != e2.Sum {
		t.Fatalf("the catalog moved on: %+v", snap.Catalog[0].CatalogEntry)
	}
	member := newRegistry(t)
	if problems, err := member.ApplyTeam(snap, FirstVersion, teamNow.Add(3*time.Minute)); err != nil || len(problems) > 0 {
		t.Fatalf("apply: %v %v", err, problems)
	}
	if got, _ := member.Get("acme.demo"); got.Version != "1.0.0" {
		t.Fatalf("the member runs the approved version, not the head: %+v", got)
	}
	// A snapshot whose pinned bytes are not the pinned sum installs nothing.
	bad := snap
	bad.At += 10
	bad.Installs = []SnapshotInstall{snap.Installs[0]}
	bad.Installs[0].Sum = e2.Sum
	fresh := newRegistry(t)
	problems, err := fresh.ApplyTeam(bad, FirstVersion, teamNow)
	if err != nil || len(problems) == 0 {
		t.Fatalf("a wrong sum is a problem: %v %v", err, problems)
	}
	if list, _ := fresh.List(); len(list) != 0 {
		t.Fatalf("nothing installed: %+v", list)
	}
}

func TestTeamChangeHookAndAMembersPublishKeepsTheTeamsTime(t *testing.T) {
	r := newRegistry(t)
	calls := 0
	r.OnTeamChange(func() { calls++ })
	if err := r.SetTeam("sp1", RoleMember, teamNow); err != nil {
		t.Fatal(err)
	}
	at := teamNow.UnixMilli()
	publishedToTeam(t, r, "acme.demo", "1.0.0")
	if calls != 1 {
		t.Fatalf("publish tells once: %d", calls)
	}
	// The member's publish did not advance the team's time: the platform's next snapshot is not stale.
	snap, _ := r.Snapshot()
	if snap.At != at {
		t.Fatalf("the member's publish moved the team's time: %d != %d", snap.At, at)
	}
	// The platform's snapshot, made at the same moment the team was set, is taken.
	if _, err := r.ApplyTeam(TeamSnapshot{Space: "sp1", Role: RoleMember, At: at}, FirstVersion, teamNow); err != nil || calls != 1 {
		t.Fatalf("a snapshot applied from the platform tells no one: %v %d", err, calls)
	}
	if err := r.SetTeam("sp1", RoleOwner, teamNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	publishedToTeam(t, r, "acme.demo", "1.1.0")                                                                                                               // 2
	publishedToTeam(t, r, "acme.demo", "1.2.0")                                                                                                               // 3
	if _, err := r.InstallForTeam("acme.demo", ChannelStable, TeamInstallOptions{HostVersion: FirstVersion, Now: teamNow.Add(2 * time.Minute)}); err != nil { // 4
		t.Fatal(err)
	}
	if _, err := r.RollbackCatalog("acme.demo", ChannelStable, FirstVersion, teamNow.Add(3*time.Minute)); err != nil { // 5
		t.Fatal(err)
	}
	if err := r.UninstallForTeam("acme.demo", teamNow.Add(4*time.Minute)); err != nil { // 6
		t.Fatal(err)
	}
	if calls != 6 {
		t.Fatalf("install, rollback and uninstall tell too: %d", calls)
	}
	if err := r.MuteTeamApp("x", true); err == nil {
		t.Fatal("mute of an app the team has not")
	}
	if calls != 6 {
		t.Fatalf("a refused call tells no one: %d", calls)
	}
}
