package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// layoutKit installs a made-up kit "acme-ds" 1.0.0 with the theme "plain" and one layout "closing".
func layoutKit(t *testing.T, g *deckRig) {
	t.Helper()
	kits := t.TempDir()
	g.l.cfg.KitsDir = kits
	theme := filepath.Join(kits, "acme-ds", "1.0.0", "themes", "plain")
	lay := filepath.Join(theme, "_harnessly", "layouts")
	for p, body := range map[string]string{
		filepath.Join(lay, "layouts.json"):              `[{"id":"closing","name":"Closing","mode":"light","purpose":"Ends","file":"closing.dc.html"}]`,
		filepath.Join(lay, "closing.dc.html"):           `<x-dc><main data-layout="closing">ds/plain/components/bundle.js</main></x-dc>`,
		filepath.Join(theme, "components", "bundle.js"): "window.Plain={}",
		filepath.Join(theme, "_harnessly", "gaps.json"): "[]",
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAPageCanStartFromALayoutOfAnInstalledKit(t *testing.T) {
	g := newDeckRig(t)
	layoutKit(t, g)
	p, err := g.l.CreatePage("admin", PageSpec{Title: "The end", Layout: "acme-ds/plain/closing"})
	if err != nil {
		t.Fatal(err)
	}
	body := readFileT(t, filepath.Join(g.l.ProjectDir("admin"), p.File))
	if !strings.Contains(body, `data-layout="closing"`) {
		t.Fatalf("the page is not the layout: %s", body)
	}
	// The theme is not copied: the project is pinned to the kit's version and ds/plain/ is answered from it.
	if _, err := os.Stat(filepath.Join(g.l.ProjectDir("admin"), "ds")); err == nil {
		t.Fatal("the theme was copied into the project")
	}
	pin, ok := g.l.KitPinOf("admin")
	if !ok || pin.ID != "acme-ds" || pin.Version != "1.0.0" || len(pin.Mounts) != 1 || pin.Mounts[0] != "plain" {
		t.Fatalf("pin: %+v %v", pin, ok)
	}
	file := g.l.ResolveProjectFile("admin", []string{"ds", "plain", "components", "bundle.js"})
	if file == "" || readFileT(t, file) != "window.Plain={}" {
		t.Fatalf("the theme is not served from the kit: %q", file)
	}
	for _, bad := range []string{"acme-ds/plain/nope", "nope/plain/closing", "acme-ds/plain", "acme-ds@9.9.9/plain/closing"} {
		if _, err := g.l.CreatePage("admin", PageSpec{Title: "x", Layout: bad}); err == nil {
			t.Errorf("%s: want a refusal", bad)
		}
	}
	if _, err := g.l.CreatePage("admin", PageSpec{Title: "y", Layout: "acme-ds/plain/closing", File: "y.dc.html"}); err == nil {
		t.Error("a layout with a file of its own: want a refusal")
	}
}

func TestALayoutNeverWritesThroughALinkedDsFolder(t *testing.T) {
	g := newDeckRig(t)
	layoutKit(t, g)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(g.l.ProjectDir("admin"), "ds")); err != nil {
		t.Fatal(err)
	}
	if _, err := g.l.CreatePage("admin", PageSpec{Title: "Linked", Layout: "acme-ds/plain/closing"}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatalf("something was written outside the project: %v", entries)
	}
}
