package design

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fontKit installs a made-up kit "acme-ds" 1.0.0 that offers the font "Acme Sans".
func fontKit(t *testing.T, g *deckRig) {
	t.Helper()
	kits := t.TempDir()
	g.l.cfg.KitsDir = kits
	root := filepath.Join(kits, "acme-ds", "1.0.0")
	if err := os.MkdirAll(filepath.Join(root, "fonts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"fonts/AcmeSans-Regular.woff2", "fonts/AcmeSans-Bold.woff2"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("font"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec := `{"id":"acme-ds","version":"1.0.0","fonts":[{"id":"acme-sans","family":"Acme Sans","license":{"kind":"restricted"},` +
		`"faces":[{"weight":400,"file":"fonts/AcmeSans-Regular.woff2"},{"weight":700,"file":"fonts/AcmeSans-Bold.woff2"},{"weight":500,"file":"fonts/missing.woff2"},{"weight":400,"file":"../escape.woff2"}]}]}`
	if err := os.WriteFile(filepath.Join(root, "kit.json"), []byte(rec), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestADeckTakesAFontOfAnInstalledKitAndLoadsItFromTheKit(t *testing.T) {
	g := newDeckRig(t)
	fontKit(t, g)
	fonts := g.l.KitFonts()
	if len(fonts) != 1 || fonts[0].ID != "kit:acme-ds:1.0.0:acme-sans" || !fonts[0].Restricted || len(fonts[0].Faces) != 2 {
		t.Fatalf("fonts: %+v", fonts)
	}
	put := func(font string) int {
		code, _ := g.call(t, g.a.DeckSettings, "PUT", "/x", `{"artifact":"admin","settings":{"font":"`+font+`"}}`)
		return code
	}
	if code := put("kit:acme-ds:1.0.0:acme-sans"); code != 200 {
		t.Fatalf("a kit font: %d", code)
	}
	css := readFileT(t, filepath.Join(g.root, "admin", ProjectDirname, "deck-theme.css"))
	for _, want := range []string{`@font-face{font-family:"Acme Sans";font-weight:400;`, `url("/kit/acme-ds/1.0.0/fonts/AcmeSans-Bold.woff2")`, `--deck-font:"Acme Sans", system-ui, sans-serif;`} {
		if !strings.Contains(css, want) {
			t.Errorf("css has no %q: %s", want, css)
		}
	}
	if strings.Contains(css, "escape") || strings.Contains(css, "missing") {
		t.Errorf("a face outside the kit or a missing file is in the css: %s", css)
	}
	for _, bad := range []string{"kit:acme-ds:1.0.0:other", "kit:acme-ds:2.0.0:acme-sans", "kit:nope:1.0.0:acme-sans"} {
		if code := put(bad); code != 400 {
			t.Errorf("%s: %d, want 400", bad, code)
		}
	}
	if code := put("serif"); code != 200 {
		t.Fatalf("a built-in font after a kit's: %d", code)
	}
}
