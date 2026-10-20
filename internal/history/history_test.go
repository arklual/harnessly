package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// testdata/ts-session-history.json: what the previous runtime's store wrote, run under node on the same files.
func TestStoreWritesWhatTheOldRuntimeWrote(t *testing.T) {
	raw, err := os.ReadFile("testdata/ts-session-history.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Input        string          `json:"input"`
		Entry        Entry           `json:"entry"`
		AfterAdd     string          `json:"afterAdd"`
		Dismissed    json.RawMessage `json:"dismissed"`
		AfterDismiss string          `json:"afterDismiss"`
		Bak          *string         `json:"bak"`
		Clipped      json.RawMessage `json:"clipped"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	for i, c := range cases {
		dir := t.TempDir()
		file := filepath.Join(dir, "session-history.json")
		if c.Input != "" {
			_ = os.WriteFile(file, []byte(c.Input), 0o600)
		}
		s := &Store{File: file}
		if err := s.Add(c.Entry); err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); string(got) != c.AfterAdd {
			t.Errorf("#%d after add:\n%s\nwant\n%s", i, got, c.AfterAdd)
		}
		dismissed, err := s.Dismiss("t2", 77)
		if err != nil {
			t.Fatal(err)
		}
		var got any = nil
		if dismissed != nil {
			got = dismissed.Clip()
		}
		if !sameJSON(nodejson.Marshal(got), c.Dismissed) {
			t.Errorf("#%d dismissed %s want %s", i, nodejson.Marshal(got), c.Dismissed)
		}
		if after, _ := os.ReadFile(file); string(after) != c.AfterDismiss {
			t.Errorf("#%d after dismiss:\n%s\nwant\n%s", i, after, c.AfterDismiss)
		}
		bak, err := os.ReadFile(file + ".bak")
		if (c.Bak == nil) != (err != nil) || c.Bak != nil && string(bak) != *c.Bak {
			t.Errorf("#%d backup %q %v want %v", i, bak, err, c.Bak)
		}
		if !sameJSON(nodejson.Marshal(c.Entry.Clip()), c.Clipped) {
			t.Errorf("#%d clip %s want %s", i, nodejson.Marshal(c.Entry.Clip()), c.Clipped)
		}
	}
}

func sameJSON(a, b []byte) bool {
	var x, y any
	_ = json.Unmarshal(a, &x)
	_ = json.Unmarshal(b, &y)
	return reflect.DeepEqual(x, y)
}

func TestStoreKeepsTheNewest200AndSkipsUnchangedWrites(t *testing.T) {
	file := filepath.Join(t.TempDir(), "session-history.json")
	s := &Store{File: file}
	for i := 0; i < 205; i++ {
		_ = s.Add(Entry{ID: strings.Repeat("i", i%3+1), TabID: "t", ToolUsage: json.RawMessage("{}"), TouchedFiles: json.RawMessage("[]")})
	}
	if n := len(s.All()); n != MaxEntries {
		t.Fatalf("%d entries", n)
	}
	st, _ := os.Stat(file)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	before := st.ModTime()
	_ = os.Chtimes(file, before.Add(-3600e9), before.Add(-3600e9))
	if _, err := s.Dismiss("absent", 1); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(file); !st.ModTime().Equal(before.Add(-3600e9)) {
		t.Fatal("an unchanged list was written again")
	}
}

func TestStatsMergeAndCleanup(t *testing.T) {
	dir := t.TempDir()
	s := &Stats{Dir: filepath.Join(dir, "session-stats")}
	id := "0b8f5c2e-1d2a-4e6f-9a7b-3c4d5e6f7a8b"
	if got, _ := s.Update("../escape", jsvalue.NewObject("x", 1.0)); got != nil {
		t.Fatal("a non-UUID id was written")
	}
	_, _ = s.Update(id, jsvalue.NewObject("sessionId", id, "inputTokens", 5.0, "model", "Opus", "transcriptPath", "/t.jsonl"))
	got, _ := s.Update(id, jsvalue.NewObject("model", nil, "cost", 1.5), "transcriptPath")
	if want := `{"sessionId":"` + id + `","inputTokens":5,"model":null,"cost":1.5}`; string(jsvalue.Stringify(got, "")) != want {
		t.Fatalf("%s", jsvalue.Stringify(got, ""))
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "session-stats", id+".json")); string(raw) != string(jsvalue.Stringify(got, "")) {
		t.Fatalf("stored %s", raw)
	}
	projects := filepath.Join(dir, "projects")
	_ = os.MkdirAll(filepath.Join(projects, "-srv-app"), 0o755)
	keep := "1b8f5c2e-1d2a-4e6f-9a7b-3c4d5e6f7a8b"
	_ = os.WriteFile(filepath.Join(projects, "-srv-app", keep+".jsonl"), nil, 0o600)
	_, _ = s.Update(keep, jsvalue.NewObject())
	if n := s.CleanupOrphans(projects); n != 1 || s.Read(id) != nil || s.Read(keep) == nil {
		t.Fatalf("removed %d", n)
	}
	if SessionIDOfTranscript("/x/"+keep+".jsonl") != keep || SessionIDOfTranscript("/x/agent-1.jsonl") != "" {
		t.Fatal("session id of a transcript")
	}
}
