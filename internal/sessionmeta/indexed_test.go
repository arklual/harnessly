package sessionmeta

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

func writeTo(t *testing.T, file, s string, flag int) {
	t.Helper()
	fh, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|flag, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fh.WriteString(s)
	fh.Close()
}

// The reader says the same of the same transcript whether it reads the file itself or takes its lines from the
// index: the recorded transcripts line by line, with a line cut in two writes, after a rewrite, and with the file
// gone and back.
func TestTheIndexedReaderSaysWhatTheReaderSays(t *testing.T) {
	recorded, err := filepath.Glob(filepath.Join("testdata", "*.jsonl"))
	if err != nil || len(recorded) == 0 {
		t.Fatal("no recorded transcripts", err)
	}
	ts := "2026-01-02T03:04:05Z"
	extra := []string{
		`{"type":"api-request","querySource":"repl_main_thread","params":{"output_config":{"effort":"high"}}}` + "\n",
		`{"type":"queue-operation","operation":"enqueue","timestamp":"` + ts + `","content":"Goal set: ship the index"}` + "\n",
		`{"type":"system","timestamp":"` + ts + `","content":"<local-command-stdout>Set effort level to max · Ultracode on</local-command-stdout>"}` + "\n",
		`{"type":"ai-title","aiTitle":"Index the transcripts"}` + "\n",
		`{"type":"custom-title","customTitle":"Mine","timestamp":17}` + "\n",
		`{"type":"system","subtype":"away_summary","timestamp":"` + ts + `","content":"You were away. (disable recaps in /config)"}` + "\n",
		`{"type":"attachment","timestamp":"` + ts + `","attachment":{"type":"goal_status","condition":"ship the index","met":true}}` + "\n",
		`{"type":"custom-title","customTitle":"Kept","cwd":5}` + "\n",
		`"Goal set: not a line of the transcript"` + "\n",
	}
	for _, name := range recorded {
		t.Run(filepath.Base(name), func(t *testing.T) {
			raw, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			lines := append(strings.SplitAfter(strings.TrimRight(string(raw), "\n")+"\n", "\n"), extra...)
			dir := t.TempDir()
			plainFile, indexedFile := filepath.Join(dir, "plain.jsonl"), filepath.Join(dir, "indexed.jsonl")
			ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
			plain, indexed := &Reader{}, &Reader{Index: ix}
			var last Info
			same := func(step string) {
				t.Helper()
				last = indexed.Read(indexedFile)
				if p := plain.Read(plainFile); !reflect.DeepEqual(p, last) {
					t.Fatalf("%s: the reader says %+v, the indexed one %+v", step, p, last)
				}
			}
			both := func(s string, flag int) {
				writeTo(t, plainFile, s, flag)
				writeTo(t, indexedFile, s, flag)
			}
			same("no file yet")
			for i, l := range lines {
				if i%2 == 1 && len(l) > 10 {
					both(l[:len(l)/2], os.O_APPEND)
					same("half of line " + l)
					both(l[len(l)/2:], os.O_APPEND)
				} else {
					both(l, os.O_APPEND)
				}
				same("line " + l)
			}
			if last.Title != "Kept" || last.Goal == nil || last.Goal.Status != GoalMet || last.Recap == nil || last.Effort != "max" || last.Ultracode != On {
				t.Fatalf("the transcript's last word: %+v", last)
			}
			before := tally.Now()
			same("at rest")
			if cost := tally.Now().Sub(before); cost.Opens != 0 || cost.Bytes != 0 {
				t.Fatalf("a look at a transcript at rest cost %+v", cost)
			}
			both(lines[0], os.O_TRUNC)
			same("rewritten")
			both(extra[1], os.O_APPEND)
			same("grown after the rewrite")
			os.Remove(plainFile)
			os.Remove(indexedFile)
			same("gone")
			both(strings.Join(lines, ""), os.O_APPEND)
			same("back")
			indexed.Forget(indexedFile)
			if n := ix.Counters(); n.Files != 0 || n.Subs != 0 {
				t.Fatalf("a forgotten transcript is still followed: %+v", n)
			}
		})
	}
}
