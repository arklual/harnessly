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
	// values are the installation's values each item was installed with; wants are the names the package asks for.
	values map[string]map[string]string
	wants  []string
}

func (a *fakeApps) Install(dir, itemID string, locked bool, install map[string]string) (string, bool, []string, error) {
	if _, err := os.Stat(filepath.Join(dir, "harnessly", "app.json")); err != nil {
		return "", false, nil, err
	}
	changed := a.installed[itemID] != dir || a.locked[itemID] != locked
	a.installed[itemID], a.locked[itemID] = dir, locked
	if a.values != nil {
		a.values[itemID] = install
	}
	var missing []string
	for _, n := range a.wants {
		if install[n] == "" {
			missing = append(missing, n)
		}
	}
	return "acme.kit", changed, missing, nil
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

// The hub's values for the package reach the install; an app the installation has no service for is skipped as not
// configured, not failed.
func TestAnAppGetsTheInstallationsValues(t *testing.T) {
	f := newFixture(t)
	apps := &fakeApps{installed: map[string]string{}, locked: map[string]bool{}, values: map[string]map[string]string{}, wants: []string{"boardsUrl"}}
	f.r.Apps = apps
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && c.Args[0] == "clone" {
			dir := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(dir, "apps", "boards", "harnessly"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, "apps", "boards", "harnessly", "app.json"), []byte("{}"), 0o644)
		}
		return reply{}
	}
	f.setList(
		hubproto.EnvItem{ItemID: "b1", Kind: hubproto.EnvKindApp, Name: "Доски", Layer: "org",
			Source: json.RawMessage(`{"from":"repo","repoUrl":"https://gitlab.com/acme/boards.git","path":"apps/boards","install":{"boardsUrl":"https://boards.example"}}`)},
		hubproto.EnvItem{ItemID: "b2", Kind: hubproto.EnvKindApp, Name: "Доски без адреса", Layer: "org",
			Source: json.RawMessage(`{"from":"repo","repoUrl":"https://gitlab.com/acme/boards.git","path":"apps/boards"}`)},
	)
	f.r.Tick(context.Background())
	run := f.onlyRun()
	if s := f.step(run, "b1"); s.Status != hubproto.EnvOK || apps.values["b1"]["boardsUrl"] != "https://boards.example" {
		t.Fatalf("b1: %+v, values %v", s, apps.values["b1"])
	}
	if s := f.step(run, "b2"); s.Status != hubproto.EnvSkipped || s.Message != "не настроено: у хаба нет адреса сервиса (boardsUrl)" {
		t.Fatalf("b2: %+v", s)
	}
}
