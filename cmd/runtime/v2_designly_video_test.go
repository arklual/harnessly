package main

import (
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

func TestVideoLineTellsTheSettingsAndTheCommands(t *testing.T) {
	t.Setenv("RUNTIME_LOCALE", "")
	secs := 30
	line := videoLine(&wire.DesignlyVideo{Formats: []wire.DesignVideoFormat{"16:9", "9:16"}, Seconds: &secs, Subs: true}, "harnessly", "clip")
	for _, want := range []string{"16:9 and 9:16", "about 30 seconds", "with subtitles", "harnessly design video guide", "guide director", "guide motion", "design video check clip", "--sheet"} {
		if !strings.Contains(line, want) {
			t.Errorf("line lacks %q: %s", want, line)
		}
	}
	// No settings from the page: the defaults.
	def := videoLine(nil, "harnessly", "clip")
	if !strings.Contains(def, "16:9") || !strings.Contains(def, "any length") {
		t.Errorf("defaults: %s", def)
	}
	if k := videoKindLine(); !strings.Contains(k, "one scene") || !strings.Contains(k, "setTimeout") || strings.Contains(k, "data-tk-at") {
		t.Errorf("kind line: %s", k)
	}
}
