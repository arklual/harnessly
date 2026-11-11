package usagereport

import (
	"os"
	"strings"
	"testing"
	"time"
)

// With the database on, the facts survive a start that finds no JSON file, and the transcripts are not read again.
func TestFactsDatabaseCarriesTheFactsWithoutTheJSONFile(t *testing.T) {
	g := loadGolden(t)
	root, fill := layOut(t, g)
	var want string
	for _, s := range g.Steps {
		if s.CacheWritten != "" {
			want = fill(s.Answer)
			break
		}
	}
	clock := &fakeClock{t: time.UnixMilli(int64(g.Now))}
	rt, c := newTestRoute(root, clock)
	c.cfg.SQLiteFacts = true
	if _, got := answerBody(t, rt, "/api/platform/stats", "sub:dev@example.com"); got != want {
		t.Fatalf("first answer\n got %s\nwant %s", got, want)
	}
	if _, err := os.Stat(sqlitePath(c.cfg.CacheFile)); err != nil {
		t.Fatalf("no database: %v", err)
	}
	if _, err := os.Stat(c.cfg.CacheFile); err != nil {
		t.Fatalf("the JSON file is still written at the first pass: %v", err)
	}
	var n int
	if err := c.sql.db.QueryRow(`SELECT count(*) FROM facts`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("rows %d err %v", n, err)
	}
	c.sql.db.Close()

	// A second start: no JSON file, transcripts blanked at their size and time. Only the database can answer.
	_ = os.Remove(c.cfg.CacheFile)
	for _, p := range c.order {
		st, _ := os.Stat(p)
		_ = os.WriteFile(p, []byte(strings.Repeat(" ", int(st.Size()))), 0o644)
		_ = os.Chtimes(p, st.ModTime(), st.ModTime())
	}
	rt2, c2 := newTestRoute(root, clock)
	c2.cfg.CacheFile = c.cfg.CacheFile
	c2.cfg.SQLiteFacts = true
	if _, got := answerBody(t, rt2, "/api/platform/stats", "sub:dev@example.com"); got != want {
		t.Fatalf("answer from the database\n got %s\nwant %s", got, want)
	}
}

// A pass writes only the rows that changed, and the JSON file only every jsonSnapshotEvery.
func TestFactsDatabaseWritesChangedRowsOnly(t *testing.T) {
	db, err := openFactsDB(t.TempDir() + "/usage-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	defer db.db.Close()
	mk := func(size float64) *cachedFile { return &cachedFile{size: size, mtimeMs: 1, facts: &Facts{}} }
	files := map[string]*cachedFile{"a": mk(1), "b": mk(2)}
	if err := db.save([]string{"a", "b"}, files); err != nil {
		t.Fatal(err)
	}
	var before int
	_ = db.db.QueryRow(`SELECT total_changes()`).Scan(&before)
	files["b"] = mk(3)
	if err := db.save([]string{"a", "b"}, files); err != nil {
		t.Fatal(err)
	}
	var after int
	_ = db.db.QueryRow(`SELECT total_changes()`).Scan(&after)
	if after-before != 1 {
		t.Fatalf("a pass over one changed transcript changed %d rows", after-before)
	}
	delete(files, "a")
	if err := db.save([]string{"b"}, files); err != nil {
		t.Fatal(err)
	}
	order, got := db.load()
	if len(order) != 1 || order[0] != "b" || got["b"].size != 3 {
		t.Fatalf("loaded %v %v", order, got)
	}
}
