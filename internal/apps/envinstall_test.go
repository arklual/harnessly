package apps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentInstallsAreTheOrganizationsNotThePersons(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.kit", "1.0.0")
	opt := EnvInstallOptions{Link: "own", ItemID: "item-kit", Locked: true, HostVersion: TemplateVersion, Now: teamNow}

	got, changed, err := r.InstallForEnv(dir, opt)
	if err != nil || !changed || got.ID != "acme.kit" {
		t.Fatalf("install: %v %v %+v", err, changed, got)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "env", "acme.kit", "1.0.0", ManifestPath)); err != nil {
		t.Fatalf("the copy: %v", err)
	}
	// The same files again: already in place.
	if _, changed, err := r.InstallForEnv(dir, opt); err != nil || changed {
		t.Fatalf("again: %v %v", err, changed)
	}
	if !r.EnvHasItem("own", "item-kit") || r.EnvHasItem("own", "other") || r.EnvHasItem("observe", "item-kit") {
		t.Fatal("EnvHasItem")
	}

	list, err := r.List()
	if err != nil || len(list) != 1 || !list[0].Env || !list[0].Locked || list[0].Source != "env" {
		t.Fatalf("list: %v %+v", err, list)
	}
	if err := r.Remove("acme.kit"); !errors.Is(err, ErrEnvApp) {
		t.Fatalf("remove: %v", err)
	}
	if err := r.MuteTeamApp("acme.kit", true); !errors.Is(err, ErrEnvLocked) {
		t.Fatalf("mute a pinned app: %v", err)
	}

	// Not pinned any more: the person may turn it off, and it leaves the chats' list but stays in ListAll.
	opt.Locked = false
	if _, changed, err := r.InstallForEnv(dir, opt); err != nil || !changed {
		t.Fatalf("unlock: %v %v", err, changed)
	}
	if err := r.MuteTeamApp("acme.kit", true); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.List(); len(list) != 0 {
		t.Fatalf("a muted app is on: %+v", list)
	}
	if all, _ := r.ListAll(); len(all) != 1 || !all[0].Muted {
		t.Fatalf("ListAll: %+v", all)
	}

	// The person's own install of the same app wins over the environment's.
	if _, _, err := r.Install(dir, InstallOptions{HostVersion: TemplateVersion}); err != nil {
		t.Fatal(err)
	}
	if list, _ := r.List(); len(list) != 1 || list[0].Env {
		t.Fatalf("own install: %+v", list)
	}
	if err := r.Remove("acme.kit"); err != nil {
		t.Fatalf("own remove: %v", err)
	}

	// A new version replaces the copy.
	v2 := versioned(t, "acme.kit", "1.1.0")
	if got, changed, err := r.InstallForEnv(v2, opt); err != nil || !changed || got.Version != "1.1.0" {
		t.Fatalf("update: %v %+v", err, got)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "env", "acme.kit", "1.0.0")); !os.IsNotExist(err) {
		t.Fatalf("the old copy stayed: %v", err)
	}

	// Another link's prune does not touch it; another link's item with the same app is refused.
	if gone, err := r.PruneEnv("observe", nil); err != nil || len(gone) != 0 {
		t.Fatalf("another link pruned: %v %v", err, gone)
	}
	if _, _, err := r.InstallForEnv(v2, EnvInstallOptions{Link: "observe", ItemID: "item-kit", HostVersion: TemplateVersion, Now: teamNow}); err == nil {
		t.Fatal("another link took the app over")
	}
	// The item left the list: pruned, the copy goes.
	gone, err := r.PruneEnv("own", []string{"another"})
	if err != nil || len(gone) != 1 || gone[0] != "acme.kit" {
		t.Fatalf("prune: %v %v", err, gone)
	}
	if all, _ := r.ListAll(); len(all) != 0 {
		t.Fatalf("after prune: %+v", all)
	}
	if _, err := os.Stat(filepath.Join(r.Root, "env", "acme.kit")); !os.IsNotExist(err) {
		t.Fatalf("the copy stayed: %v", err)
	}
}

func TestEnvironmentRefusesATrustedApp(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.risky", "1.0.0")
	pkg, _ := LoadPackage(dir)
	pkg.Manifest.Trust = TrustTrusted
	writeManifest(t, dir, pkg.Manifest)
	if p, ps := LoadPackage(dir); p == nil || len(ps.Errors()) > 0 {
		t.Skipf("the manifest of a trusted app needs more than the trust field: %v", ps)
	}
	if _, _, err := r.InstallForEnv(dir, EnvInstallOptions{ItemID: "x", HostVersion: TemplateVersion, Now: teamNow}); !errors.Is(err, ErrTrustedEnv) {
		t.Fatalf("trusted: %v", err)
	}
}

func TestEnvironmentAppTheOrganizationTurnedOffIsNotListedAndStaysInstalled(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.kit", "1.0.0")
	if _, _, err := r.InstallForEnv(dir, EnvInstallOptions{Link: "own", ItemID: "item-kit", HostVersion: TemplateVersion, Now: teamNow}); err != nil {
		t.Fatal(err)
	}
	off := false
	r.SetOrgOff(func(link, itemID string) bool { return off && link == "own" && itemID == "item-kit" })
	if _, err := r.Get("acme.kit"); err != nil {
		t.Fatalf("on: %v", err)
	}
	off = true
	all, err := r.ListAll()
	if err != nil || len(all) != 0 {
		t.Fatalf("off is not listed at all: %v %+v", err, all)
	}
	if !r.EnvHasItem("own", "item-kit") {
		t.Fatal("off keeps the install")
	}
	off = false
	if _, err := r.Get("acme.kit"); err != nil {
		t.Fatalf("on again: %v", err)
	}
}
