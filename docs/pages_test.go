package docs

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The Russian page wins for ru; a page with no translation, and any other locale, is the English one.
func TestPagesLocale(t *testing.T) {
	guide := fstest.MapFS{
		"build-apps/README.md":          {Data: []byte("# Guide\n\n## Start\n```\n## not a heading\n```\n")},
		"build-apps/01-first-plugin.md": {Data: []byte("# First plugin\n")},
		"apps.md":                       {Data: []byte("# Reference\n")},
		"ru/build-apps/README.md":       {Data: []byte("# Руководство\n\n## Начало\n")},
	}
	ru, err := Pages(guide, LocaleRU)
	if err != nil || len(ru) != 3 || ru[0].Name != "overview" || ru[0].Title != "Руководство" || ru[0].Headings[0] != "Начало" {
		t.Fatalf("ru: %v %+v", err, ru)
	}
	if ru[1].Title != "First plugin" || ru[2].Name != "reference" {
		t.Errorf("fallback: %+v", ru)
	}
	en, _ := Pages(guide, LocaleEN)
	if en[0].Title != "Guide" || len(en[0].Headings) != 1 {
		t.Errorf("en: %+v", en[0])
	}
	if raw, ok, _ := Read(guide, LocaleRU, "overview"); !ok || !strings.HasPrefix(string(raw), "# Руководство") {
		t.Errorf("read ru: %q", raw)
	}
	if _, ok, _ := Read(guide, LocaleRU, "../apps"); ok {
		t.Error("traversal")
	}
	if _, err := Pages(Guide, LocaleRU); err != nil {
		t.Errorf("embedded: %v", err)
	}
}
