package design

import "testing"

// The themes a kit link is looked up in are read once per state of the kits folder, and read again when a version is
// installed: every open of a board asks for its link, and reading every canvas took up to a second.
func TestUsageThemesAreKeptUntilTheKitsChange(t *testing.T) {
	l := kitToolsLibrary(t)
	first := l.usageThemes("")
	if len(first) == 0 {
		t.Fatal("the fixture kit has themes")
	}
	again := l.usageThemes("")
	if &again[0] != &first[0] {
		t.Error("an unchanged kits folder is not read again")
	}
	if got := l.usageThemes("acme-ui"); len(got) != len(first) || len(l.usageThemes("other")) != 0 {
		t.Errorf("one kit: %d of %d", len(got), len(first))
	}
	installKitFixture(t, l.KitsDir(), "acme-ui", "1.1.0")
	after := l.usageThemes("")
	if len(after) == 0 || after[0].latest != "1.1.0" {
		t.Fatalf("a new version is read: %+v", after)
	}
}
