package envsetup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeApps struct {
	installed map[string]string // item → folder
	locked    map[string]bool
	pruned    [][]string
}

func (a *fakeApps) Install(dir, itemID string, locked bool) (string, bool, error) {
	if _, err := os.Stat(filepath.Join(dir, "harnessly", "app.json")); err != nil {
		return "", false, err
	}
	changed := a.installed[itemID] != dir || a.locked[itemID] != locked
	a.installed[itemID], a.locked[itemID] = dir, locked
	return "acme.kit", changed, nil
}
func (a *fakeApps) Has(itemID string) bool { _, ok := a.installed[itemID]; return ok }
func (a *fakeApps) Prune(keep []string) ([]string, error) {
	a.pruned = append(a.pruned, keep)
	return nil, nil
}

func TestAnAppIsInstalledFromTheFolderOfARepositoryAndPrunedAfterAFullRun(t *testing.T) {
	f := newFixture(t)
	apps := &fakeApps{installed: map[string]string{}, locked: map[string]bool{}}
	f.r.Apps = apps
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && c.Args[0] == "clone" {
			dir := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(dir, "apps", "kit", "harnessly"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, "apps", "kit", "harnessly", "app.json"), []byte("{}"), 0o644)
		}
		return reply{}
	}
	app := hubproto.EnvItem{ItemID: "a1", Kind: hubproto.EnvKindApp, Name: "Кит", Layer: "org", Pinned: true,
		Source: json.RawMessage(`{"from":"repo","repoUrl":"https://gitlab.com/acme/kit.git","branch":"main","path":"apps/kit"}`)}
	noPath := hubproto.EnvItem{ItemID: "a2", Kind: hubproto.EnvKindApp, Name: "Без папки", Layer: "org",
		Source: json.RawMessage(`{"from":"repo","repoUrl":"https://gitlab.com/acme/kit.git"}`)}
	f.setList(app, noPath)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "a1"); s.Status != hubproto.EnvOK || s.Message != "поставлено: acme.kit" {
		t.Fatalf("a1: %+v", s)
	}
	if !apps.locked["a1"] || filepath.Base(apps.installed["a1"]) != "kit" {
		t.Fatalf("installed: %+v %+v", apps.installed, apps.locked)
	}
	if s := f.step(run, "a2"); s.Status != hubproto.EnvError {
		t.Fatalf("a2: %+v", s)
	}
	if len(apps.pruned) != 1 || len(apps.pruned[0]) != 2 {
		t.Fatalf("pruned: %v", apps.pruned)
	}

	// A look changes nothing and does not prune.
	f.request("look-run-01", hubproto.RunCheck)
	f.r.Tick(context.Background())
	if s := f.step(f.runFile("look-run-01"), "a1"); s.Status != hubproto.EnvSkipped {
		t.Fatalf("look: %+v", s)
	}
	if len(apps.pruned) != 1 {
		t.Fatalf("a look pruned: %v", apps.pruned)
	}
}

func TestAMachineWithoutAppsSaysSo(t *testing.T) {
	f := newFixture(t)
	f.setList(hubproto.EnvItem{ItemID: "a1", Kind: hubproto.EnvKindApp, Name: "Кит", Layer: "org",
		Source: json.RawMessage(`{"from":"repo","repoUrl":"https://gitlab.com/acme/kit.git","path":"apps/kit"}`)})
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "a1"); s.Status != hubproto.EnvError || s.Message != "эта машина не ставит приложения" {
		t.Fatalf("%+v", s)
	}
}
