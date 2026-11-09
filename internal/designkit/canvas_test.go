package designkit

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureKit writes an invented installed kit: one theme «sun» with a component group, an icon group and a template of
// two sections, the way a kit's build leaves it.
func fixtureKit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, "acme-cards", "1.2.0")
	write := func(rel string, data []byte) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("kit.json", []byte(`{"title":"Acme Cards","themes":[{"id":"sun","folder":"sun","namespace":"Acme","title":"Sun"}]}`))
	write("sun/design-system.json", []byte(`{
  "title": "Acme Sun",
  "namespace": "Acme",
  "sections": {"components": {"title": "Parts"}},
  "groups": [{"id": "basics", "title": "Basics", "components": [
    {"id": "Badge", "name": "Badge", "description": "A small label", "props": [{"name": "text", "type": "string", "required": true}],
     "variants": [{"id": "quiet", "name": "Quiet", "props": {"tone": "quiet"}}]}
  ]}],
  "assetGroups": {"icons": {"title": "Icons", "kind": "icon", "component": "Icon", "prop": "name", "items": [
    {"name": "star", "file": "icons/star.png"}, {"name": "moon", "file": "icons/moon.png"}
  ]}},
  "templates": [{"id": "deck", "title": "Deck", "kind": "slides", "aspect": [16, 9], "component": "Slide", "cover": "cover",
    "sections": [
      {"id": "covers", "title": "Covers", "layouts": [
        {"id": "cover", "name": "Cover", "purpose": "The first slide", "props": {"layout": "c1"},
         "board": "_harnessly/layouts/cover.dc.html", "thumb": "_harnessly/thumbs/cover.jpg",
         "slots": [{"key": "title", "kind": "text", "sample": "Hello"}]}
      ]},
      {"id": "content", "title": "Content", "layouts": [
        {"id": "list", "name": "List of points", "props": {"layout": "l2"}, "slots": []}
      ]}
    ]}]
}`))
	write("sun/icons/star.png", pngOf(t, 64, 64, color.NRGBA{200, 120, 0, 255}))
	write("sun/icons/moon.png", pngOf(t, 32, 48, color.NRGBA{0, 0, 200, 128}))
	write("sun/_harnessly/thumbs/cover.jpg", jpegOf(t, 480, 270))
	write("sun/_harnessly/layouts/cover.dc.html", []byte(`<div data-layout="cover"></div>`))
	return dir
}

func pngOf(t *testing.T, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestReadCanvasLaysOutTheKitByItsFields(t *testing.T) {
	dir := fixtureKit(t)
	c, err := ReadCanvas(dir, "acme-cards", "1.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Theme != "sun" || c.Base != "sun" || c.Namespace != "Acme" || c.Title != "Acme Cards" {
		t.Fatalf("canvas head = %q %q %q %q", c.Theme, c.Base, c.Namespace, c.Title)
	}
	if len(c.Groups) != 1 || len(c.Groups[0].Components) != 1 || c.Groups[0].Components[0].NodeID != "component:Badge" {
		t.Fatalf("groups = %+v", c.Groups)
	}
	if len(c.Assets) != 1 || len(c.Assets[0].Items) != 2 || c.Assets[0].Items[0].NodeID != "asset:icons/star" {
		t.Fatalf("assets = %+v", c.Assets)
	}
	if len(c.Templates) != 1 || len(c.Templates[0].Sections) != 2 {
		t.Fatalf("templates = %+v", c.Templates)
	}
	cover := c.Templates[0].Sections[0].Layouts[0]
	if cover.NodeID != "layout:deck/cover" || cover.Address != "acme-cards/sun/cover" {
		t.Fatalf("cover = %+v", cover)
	}
	if list := c.Templates[0].Sections[1].Layouts[0]; list.Address != "" {
		t.Fatalf("a layout without a board has no address: %+v", list)
	}
}

func TestReadCanvasRefusesAThemeItDoesNotHaveAndAKitWithoutFields(t *testing.T) {
	dir := fixtureKit(t)
	if _, err := ReadCanvas(dir, "acme-cards", "1.2.0", "moon"); !errors.Is(err, ErrNoCanvas) {
		t.Fatalf("unknown theme: %v", err)
	}
	plain := filepath.Join(dir, "plain", "0.1.0")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, ManifestFile), []byte(`{"title":"Plain"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCanvas(dir, "plain", "0.1.0", ""); !errors.Is(err, ErrNoCanvas) {
		t.Fatalf("no canvas fields: %v", err)
	}
}

func TestSearchFindsByNameKindAndLayoutWords(t *testing.T) {
	c, err := ReadCanvas(fixtureKit(t), "acme-cards", "1.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"star": "asset:icons/star", "badge": "component:Badge", "points": "layout:deck/list"}
	for q, want := range cases {
		got := Search(c.Index, q, 5)
		if len(got) == 0 || got[0].Node != want {
			t.Errorf("Search(%q) = %+v, want first %s", q, got, want)
		}
	}
	if got := Search(c.Index, "nothing-like-this", 5); len(got) != 0 {
		t.Errorf("a miss found %+v", got)
	}
}

func TestPrepareMakesAtlasesOnceBesideTheVersion(t *testing.T) {
	dir := fixtureKit(t)
	c, err := ReadCanvas(dir, "acme-cards", "1.2.0", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := Prepare(dir, c); err != nil {
		t.Fatal(err)
	}
	atlas := c.Assets[0].Atlas
	if atlas == nil || atlas.Count != 2 || atlas.File == "" {
		t.Fatalf("icon atlas = %+v", atlas)
	}
	if c.Assets[0].Items[1].Cell != 1 {
		t.Fatalf("moon cell = %d", c.Assets[0].Items[1].Cell)
	}
	p := PreparedFile(dir, "acme-cards", "1.2.0", "sun", atlas.File)
	if p == "" {
		t.Fatalf("no prepared file for %s", atlas.File)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	if PreparedFile(dir, "acme-cards", "1.2.0", "sun", "../kit.json") != "" {
		t.Fatal("a prepared file outside the folder was served")
	}
	// The version's own folder stays as installed.
	entries, _ := os.ReadDir(filepath.Join(dir, "acme-cards", "1.2.0"))
	for _, e := range entries {
		if strings.Contains(e.Name(), "prep") {
			t.Fatalf("prep inside the version: %s", e.Name())
		}
	}
	again, _ := ReadCanvas(dir, "acme-cards", "1.2.0", "")
	if err := Prepare(dir, again); err != nil || again.Assets[0].Atlas == nil || again.Assets[0].Atlas.File != atlas.File {
		t.Fatalf("second prepare: %v %+v", err, again.Assets[0].Atlas)
	}
}
