package sidewidget

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

type fakeReg struct{ installed map[string]apps.Installed }

func (f *fakeReg) Install(dir string, _ apps.InstallOptions) (apps.Installed, apps.Problems, error) {
	raw, err := os.ReadFile(filepath.Join(dir, apps.ManifestPath))
	if err != nil {
		return apps.Installed{}, nil, err
	}
	m, probs := apps.ParseManifest(raw)
	if errs := probs.Errors(); len(errs) > 0 {
		return apps.Installed{}, probs, apps.ErrInvalid
	}
	got := apps.Installed{ID: m.ID, Version: m.Version, Dir: dir, Dev: true}
	f.installed[m.ID] = got
	return got, nil, nil
}

func (f *fakeReg) Uninstall(id string, _ bool, _ *secrets.Store) error {
	if _, ok := f.installed[id]; !ok {
		return apps.ErrNotInstalled
	}
	delete(f.installed, id)
	return nil
}

func (f *fakeReg) List() ([]apps.Installed, error) {
	out := []apps.Installed{{ID: "acme.other"}}
	for _, a := range f.installed {
		out = append(out, a)
	}
	return out, nil
}

// a widget an agent makes is a valid sandboxed package of the side target, and create, update, list and
// remove keep its folder and the install together.
func TestAWidgetIsMadeChangedListedAndRemoved(t *testing.T) {
	reg := &fakeReg{installed: map[string]apps.Installed{}}
	clock := time.Unix(1_800_000_000, 0)
	s := &Service{Reg: reg, Root: t.TempDir(), Host: "0.16.999", Now: func() time.Time { clock = clock.Add(time.Second); return clock }}

	if _, err := s.Create(Spec{Name: "limits-forecast", Title: "Прогноз лимитов", Sources: []string{"limits"},
		Smart: "Хватит ли лимита до вечера?", Every: 15, Icon: "gauge"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.installed["local.widget-limits-forecast"]; !ok {
		t.Fatal("not installed")
	}
	raw, _ := os.ReadFile(filepath.Join(s.Root, "limits-forecast", apps.ManifestPath))
	m, probs := apps.ParseManifest(raw)
	if len(probs.Errors()) > 0 {
		t.Fatalf("the package is not valid: %v", probs)
	}
	v := m.Views[0]
	if v.Widget == nil || v.Widget.Smart == nil || v.Widget.Smart.Every != 15 || v.Targets[0] != apps.TargetSide || len(m.Permissions) != 0 {
		t.Fatalf("the view: %+v", v)
	}
	if _, err := s.Create(Spec{Name: "broken", Title: "x", Smart: "y", Every: 2}); err == nil {
		t.Fatal("made a widget asked too often")
	}
	if _, err := os.Stat(filepath.Join(s.Root, "broken")); !os.IsNotExist(err) {
		t.Fatal("a refused widget left its folder")
	}
	if _, err := s.Create(Spec{Name: "limits-forecast", Title: "x", Smart: "y"}); !errors.Is(err, ErrExists) {
		t.Fatalf("create twice: %v", err)
	}
	every := 60
	if _, err := s.Update("limits-forecast", Patch{Every: &every}); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List()
	if len(list) != 1 || list[0].Every != 60 || list[0].Title != "Прогноз лимитов" || list[0].Smart == "" {
		t.Fatalf("list: %+v", list)
	}
	if _, err := s.Update("nope", Patch{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update of nothing: %v", err)
	}
	if err := s.Remove("limits-forecast"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Root, "limits-forecast")); !os.IsNotExist(err) {
		t.Fatal("the folder stayed")
	}
	if err := s.Remove("limits-forecast"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("remove twice: %v", err)
	}
}

func TestAWidgetThatShowsNothingOrBreaksTheRulesIsRefused(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	cases := map[string]Spec{
		"no name":      {Title: "x", Smart: "y"},
		"bad name":     {Name: "Bad_Name", Title: "x", Smart: "y"},
		"no title":     {Name: "a", Smart: "y"},
		"nothing":      {Name: "a", Title: "x"},
		"often":        {Name: "a", Title: "x", Smart: "y", Every: 1},
		"bad source":   {Name: "a", Title: "x", Sources: []string{"secrets"}},
		"bad size":     {Name: "a", Title: "x", Smart: "y", Size: "xl"},
		"fast refresh": {Name: "a", Title: "x", Sources: []string{"machine"}, Refresh: 5},
	}
	for name, sp := range cases {
		if _, _, err := Package(sp, now); err == nil {
			t.Errorf("%s: made", name)
		}
	}
}

func TestTheToolsMakeAndListAWidget(t *testing.T) {
	reg := &fakeReg{installed: map[string]apps.Installed{}}
	tools := Tools{S: &Service{Reg: reg, Root: t.TempDir(), Host: "0.16.999"}}
	args := jsvalue.NewObject("name", "machine-load", "title", "Нагрузка", "sources", []any{"machine"}, "smart", "Всё ли в порядке?")
	res := jsvalue.Stringify(tools.Call(context.Background(), ToolCreate, args), "")
	if strings.Contains(string(res), `"isError":true`) {
		t.Fatalf("create: %s", res)
	}
	res = jsvalue.Stringify(tools.Call(context.Background(), ToolList, jsvalue.NewObject()), "")
	if !strings.Contains(string(res), "machine-load") {
		t.Fatalf("list: %s", res)
	}
}

// The package is installed by the real registry, as on a machine: the manifest is where an app package keeps it.
func TestTheRealRegistryInstallsAWidget(t *testing.T) {
	data := t.TempDir()
	s := &Service{Reg: apps.Open(data), Root: filepath.Join(data, "side-widgets"), Host: "0.16.999"}
	got, err := s.Create(Spec{Name: "machine-load", Title: "Нагрузка", Icon: "gauge", Sources: []string{"machine"}, Smart: "Всё ли в порядке?"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "local.widget-machine-load" {
		t.Fatalf("installed: %+v", got)
	}
	if list, _ := s.List(); len(list) != 1 || list[0].Title != "Нагрузка" {
		t.Fatalf("list: %+v", list)
	}
	if err := s.Remove("machine-load"); err != nil {
		t.Fatal(err)
	}
}

// A name from an agent never reaches a path out of the widgets' folder: not `../x`, not an absolute path, not an empty
// name, not a link out of it; and nothing outside is written or removed.
func TestANameNeverLeadsOutOfTheWidgetsFolder(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(outside, "keep.txt")
	_ = os.WriteFile(keep, []byte("x"), 0o644)
	root := filepath.Join(base, "side-widgets")
	_ = os.MkdirAll(root, 0o755)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	s := &Service{Reg: &fakeReg{installed: map[string]apps.Installed{}}, Root: root, Host: "0.16.999"}
	every := 60
	for _, name := range []string{"../outside", "..", outside, "", "escape", "a/../../outside"} {
		if _, err := s.Create(Spec{Name: name, Title: "x", Smart: "y", Every: 2}); err == nil {
			t.Errorf("%q: made", name)
		}
		if _, err := s.Update(name, Patch{Every: &every}); err == nil {
			t.Errorf("%q: changed", name)
		}
		if err := s.Remove(name); err == nil {
			t.Errorf("%q: removed", name)
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("a file outside is gone: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "escape")); err != nil {
		t.Fatalf("the link was removed: %v", err)
	}
}

func TestAWidgetScriptThatNeverConnectsIsConnectedFirst(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	_, files, err := Package(Spec{Name: "snow", Title: "Снег", HTML: "<canvas></canvas>", JS: "draw();"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if js := files["ui/main.js"]; !strings.HasPrefix(js, "window.harnessly.connect().then(") || !strings.Contains(js, "draw();") {
		t.Fatalf("main.js = %q", js)
	}
	_, files, err = Package(Spec{Name: "snow", Title: "Снег", HTML: "<canvas></canvas>", JS: "window.harnessly.connect().then(go);"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if js := files["ui/main.js"]; js != "window.harnessly.connect().then(go);" {
		t.Fatalf("a script that connects is kept as written: %q", js)
	}
}

// a widget an agent makes is for the chat it was made in, unless it asks for every chat; the person's page
// reads which from the view's widget block.
func TestAWidgetBelongsToTheChatItWasMadeIn(t *testing.T) {
	reg := &fakeReg{installed: map[string]apps.Installed{}}
	s := &Service{Reg: reg, Root: t.TempDir(), Host: "0.16.999", Now: func() time.Time { return time.Unix(1_800_000_000, 0) }}
	chatOf := func(name string) string {
		raw, err := os.ReadFile(filepath.Join(s.Root, name, apps.ManifestPath))
		if err != nil {
			t.Fatal(err)
		}
		m, probs := apps.ParseManifest(raw)
		if len(probs.Errors()) > 0 {
			t.Fatalf("%v", probs)
		}
		return m.Views[0].Widget.Chat
	}
	made := func(name, scope, chat string) {
		t.Helper()
		sp, err := Place(Spec{Name: name, Title: name, Sources: []string{"limits"}, Scope: scope}, chat)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Create(sp); err != nil {
			t.Fatal(err)
		}
	}
	made("render-progress", "", "tab-aaa111")
	made("limits-forecast", ScopeGlobal, "tab-aaa111")
	made("from-terminal", "", "")
	if got := chatOf("render-progress"); got != "tab-aaa111" {
		t.Fatalf("the default is the chat it was made in: %q", got)
	}
	if got := chatOf("limits-forecast"); got != "" {
		t.Fatalf("a global widget has no chat: %q", got)
	}
	if got := chatOf("from-terminal"); got != "" {
		t.Fatalf("a caller with no chat makes it for every chat: %q", got)
	}
	// Pinned to every chat, and back to this chat.
	g := ScopeGlobal
	p, err := PlacePatch(Patch{Scope: &g}, "tab-bbb222")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update("render-progress", p); err != nil {
		t.Fatal(err)
	}
	if got := chatOf("render-progress"); got != "" {
		t.Fatalf("global after the update: %q", got)
	}
	c := ScopeChat
	p, _ = PlacePatch(Patch{Scope: &c}, "tab-bbb222")
	if _, err := s.Update("render-progress", p); err != nil {
		t.Fatal(err)
	}
	if got := chatOf("render-progress"); got != "tab-bbb222" {
		t.Fatalf("the caller's chat after the update: %q", got)
	}
	if _, err := Place(Spec{Scope: "everyone"}, "tab-x"); err == nil {
		t.Fatal("a scope that is none is taken")
	}
	list, _ := s.List()
	for _, w := range list {
		if w.Name == "render-progress" && (w.Scope != ScopeChat || w.Chat != "tab-bbb222") {
			t.Fatalf("list: %+v", w)
		}
	}
}

// an update of the markup or the style leaves the frame's shell as it was (the page keeps the frame and hands
// the new markup to it); a new script, or a script that connects itself, starts the frame anew.
func TestAnUpdateOfTheMarkupKeepsTheShell(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	shellOf := func(sp Spec) string {
		m, files, err := Package(sp, now)
		if err != nil {
			t.Fatal(err)
		}
		if sp.JS == "" || !strings.Contains(sp.JS, "harnessly.connect") {
			if !strings.Contains(files["ui/index.html"], `data-rev="`+ContentOf(sp).Rev+`"`) {
				t.Fatalf("the markup has no revision: %s", files["ui/index.html"])
			}
		}
		return m.Views[0].Widget.Shell
	}
	a := Spec{Name: "w", Title: "w", HTML: "<b>10%</b>", CSS: "b{color:red}", JS: "draw()"}
	b := a
	b.HTML, b.CSS = "<b>60%</b>", "b{color:blue}"
	if shellOf(a) == "" || shellOf(a) != shellOf(b) {
		t.Fatalf("the shell moved with the markup: %q %q", shellOf(a), shellOf(b))
	}
	if ContentOf(a).Rev == ContentOf(b).Rev {
		t.Fatal("the revision does not follow the markup")
	}
	c := a
	c.JS = "draw2()"
	if shellOf(a) == shellOf(c) {
		t.Fatal("a new script keeps the shell")
	}
	own := a
	own.JS = "window.harnessly.connect().then(draw)"
	if shellOf(own) != "" {
		t.Fatal("a script that connects itself cannot be patched")
	}
	s := &Service{Reg: &fakeReg{installed: map[string]apps.Installed{}}, Root: t.TempDir(), Host: "0.16.999", Now: func() time.Time { return now }}
	if _, err := s.Create(a); err != nil {
		t.Fatal(err)
	}
	got, err := s.Content(ID("w"))
	if err != nil || got.HTML != "<b>10%</b>" || !strings.Contains(got.CSS, "b{color:red}") || got.Rev != ContentOf(a).Rev {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.Content("acme.other"); err == nil {
		t.Fatal("an app that is no widget has content")
	}
}
