package usagereport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TS_GOLDEN=<file written by testdata/gen with GEN_RANDOM>: generated transcripts, the facts the previous runtime
// read from each and its reports over groups of them; every one must be the same here.
func TestGeneratedAgainstPrevious(t *testing.T) {
	file := os.Getenv("TS_GOLDEN")
	if file == "" {
		t.Skip("TS_GOLDEN not set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Now     float64                    `json:"now"`
		Files   []goldenFile               `json:"files"`
		Facts   map[string]json.RawMessage `json:"facts"`
		Reports []struct {
			Files  []string `json:"files"`
			Report string   `json:"report"`
		} `json:"reports"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	fill := filler(root)
	p := &Projects{Root: testRoot, TaskPrefixes: testPrefixes}
	facts := map[string]*Facts{}
	bad := 0
	for _, f := range g.Files {
		path := filepath.Join(root, f.Path)
		_ = os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, []byte(fill(f.Content)), 0o644); err != nil {
			t.Fatal(err)
		}
		var got *Facts
		if strings.Contains(f.Path, "/.codex/") {
			got, err = p.ParseCodex(path)
		} else {
			got, err = p.ParseClaude(path)
		}
		want := fill(string(g.Facts[f.Path]))
		if strings.HasPrefix(want, `{"error"`) {
			if err == nil {
				t.Errorf("%s: parsed, the previous runtime failed: %s", f.Path, want)
				bad++
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v, want %s", f.Path, err, want)
			bad++
			continue
		}
		facts[f.Path] = got
		var w jw
		got.write(&w)
		var a, b any
		_ = json.Unmarshal([]byte(want), &a)
		if err := json.Unmarshal(w.Bytes(), &b); err != nil {
			t.Fatalf("%s: facts written are not JSON: %v", f.Path, err)
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: facts\n got %s\nwant %s\nfile %q", f.Path, w.Bytes(), want, fill(f.Content))
			bad++
		}
		if bad > 10 {
			t.FailNow()
		}
	}
	for i, rep := range g.Reports {
		list := make([]*Facts, 0, len(rep.Files))
		for _, f := range rep.Files {
			list = append(list, facts[f])
		}
		r, err := Build(list, "p@example.com", "box", "Europe/Moscow", Clock{Offset: 3 * 3600}, g.Now)
		want := fill(rep.Report)
		if strings.HasPrefix(want, "error: ") {
			if err == nil {
				t.Errorf("report %d: built, the previous runtime failed: %s", i, want)
			}
			continue
		}
		if err != nil {
			t.Errorf("report %d: %v", i, err)
			continue
		}
		if got := string(r.JSON()); got != want {
			t.Errorf("report %d:\n got %s\nwant %s", i, got, want)
		}
	}
	t.Logf("%d transcripts, %d reports compared", len(g.Files), len(g.Reports))
}
