package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/usagereport"
)

// The runtime's facts file is read while the runtime keeps it; without it, the agent reads the transcripts itself.
func TestUsageSourcePrefersTheRuntimesFacts(t *testing.T) {
	dir := t.TempDir()
	stats := filepath.Join(dir, "stats")
	if err := os.MkdirAll(stats, 0o700); err != nil {
		t.Fatal(err)
	}
	owns := 0
	now := time.Now()
	u := &usageSource{
		statsDir: stats,
		stored:   map[string]*usagereport.Stored{},
		own: func() *usagereport.Collector {
			owns++
			return usagereport.NewCollector(usagereport.Config{ClaudeRoot: filepath.Join(dir, "none"), CodexRoot: filepath.Join(dir, "none")})
		},
		now: func() time.Time { return now },
	}
	u.Refresh()
	if owns != 1 || u.cur == nil {
		t.Fatalf("no runtime file: own reading expected, got %d", owns)
	}
	facts := filepath.Join(stats, "acme-usage-v2.json")
	if err := os.WriteFile(facts, []byte(`{"version":2,"files":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The agent's own file never counts as a runtime's.
	if err := os.WriteFile(filepath.Join(stats, ownFacts+usagereport.AliveSuffix), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	u.Refresh()
	if u.col == nil {
		t.Fatal("a facts file nobody keeps alive was used")
	}
	if err := os.WriteFile(facts+usagereport.AliveSuffix, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	u.Refresh()
	if u.col != nil {
		t.Fatal("own reading kept while the runtime's facts are alive")
	}
	if s, ok := u.cur.(*usagereport.Stored); !ok || s.Path != facts {
		t.Fatalf("source = %#v", u.cur)
	}
	// The runtime stops: back to the agent's own reading.
	now = now.Add(storedMaxAge + time.Minute)
	u.Refresh()
	if u.col == nil || owns != 2 {
		t.Fatalf("stale runtime facts still used (owns %d)", owns)
	}
}
