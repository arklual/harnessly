package shots

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/transcriptindex"
	"github.com/ly-solutions/harnessly/internal/transcriptindex/tally"
)

func grow(t *testing.T, path, s string, flag int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|flag, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(s)
	f.Close()
}

// The reader lists the same pictures of the same transcripts whether it reads the files itself or takes their lines
// from the index: the agent's transcript and a subagent's, line by line, with a line cut in two writes, after a
// rewrite, and with the file gone and back.
func TestTheIndexedReaderListsWhatTheReaderLists(t *testing.T) {
	main := []string{
		use("t1", "Read", `{"file_path":"/work/shots/home.png"}`, "2026-10-25T10:00:00.000Z", "/work"),
		result("t1", `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}`),
		use("t2", "Read", `{"file_path":"/work/README.md"}`, "2026-10-25T10:00:01.000Z", "/work"),
		result("t2", `{"type":"text","text":"x"}`),
		use("t3", "Read", `{"file_path":"/work/shots/missing.jpg"}`, "2026-10-25T10:00:02.000Z", "/work"),
		strings.Replace(result("t3", `{"type":"text","text":"nope"}`), `"tool_result"`, `"tool_result","is_error":true`, 1),
		use("t4", "mcp__playwright__browser_take_screenshot", `{"fullPage":true}`, "2026-10-25T10:00:03.000Z", "/work"),
		result("t4", `{"type":"text","text":"### Result\n- [Screenshot of full page](.playwright-mcp/desk.png)\n"}`),
		`not json but it says "tool_use" and .png`,
		strings.Replace(use("t7", "Read", `{"file_path":"/work/odd.png"}`, "x", "/work"), `"timestamp":"x"`, `"timestamp":12`, 1),
		result("t7", `{"type":"image"}`),
		strings.Replace(use("t8", "Read", `{"file_path":"/work/kept.png"}`, "2026-10-25T10:00:07.000Z", "/work"), `"type":"assistant"`, `"type":"assistant","aiTitle":7`, 1),
		result("t8", `{"type":"image"}`),
		use("t5", "Read", `{"file_path":"/work/shots/home.png"}`, "2026-10-25T10:00:09.000Z", "/work"),
		result("t5", `{"type":"image"}`),
	}
	sub := []string{
		use("s1", "Read", `{"file_path":"/work/shots/mobile.webp"}`, "2026-10-25T10:00:05.000Z", "/work"),
		result("s1", `{"type":"image"}`),
		use("s2", "Read", `{"file_path":"/work/shots/tablet.gif"}`, "2026-10-25T10:00:06.000Z", "/work"),
		result("s2", `{"type":"image"}`),
	}
	dir := t.TempDir()
	ix := transcriptindex.New(transcriptindex.Options{Fresh: -1})
	plain, indexed := &Reader{}, &Reader{Index: ix}
	plainFile, indexedFile := filepath.Join(dir, "plain", "s1.jsonl"), filepath.Join(dir, "indexed", "s1.jsonl")
	subOf := func(tr string) string {
		return filepath.Join(strings.TrimSuffix(tr, ".jsonl"), "subagents", "agent-a1.jsonl")
	}
	var last []Shot
	same := func(step string) {
		t.Helper()
		last = indexed.Read(indexedFile)
		if p := plain.Read(plainFile); !reflect.DeepEqual(p, last) {
			t.Fatalf("%s: the reader lists %+v, the indexed one %+v", step, p, last)
		}
	}
	both := func(of func(string) string, s string, flag int) {
		grow(t, of(plainFile), s, flag)
		grow(t, of(indexedFile), s, flag)
	}
	itself := func(tr string) string { return tr }
	same("no file yet")
	for i, l := range main {
		l += "\n"
		if i%2 == 1 {
			both(itself, l[:len(l)/2], os.O_APPEND)
			same("half of line " + l)
			both(itself, l[len(l)/2:], os.O_APPEND)
		} else {
			both(itself, l, os.O_APPEND)
		}
		same("line " + l)
		if i == 3 {
			for _, tr := range []string{plainFile, indexedFile} {
				grow(t, strings.TrimSuffix(subOf(tr), ".jsonl")+".meta.json", `{"agentType":"Explore","description":"Check the phone"}`, os.O_TRUNC)
			}
			both(subOf, sub[0]+"\n"+sub[1]+"\n", os.O_APPEND)
			same("a subagent appeared")
		}
	}
	both(subOf, sub[2]+"\n", os.O_APPEND)
	same("the subagent's call")
	both(subOf, sub[3]+"\n", os.O_APPEND)
	same("the subagent's result")
	if len(last) != 5 || last[0].Path != "/work/shots/home.png" || last[1].Path != "/work/kept.png" || last[2].Agent != "Check the phone" {
		t.Fatalf("the transcripts' pictures: %+v", last)
	}
	before := tally.Now()
	if got := indexed.Read(indexedFile); !reflect.DeepEqual(got, last) {
		t.Fatalf("at rest: %+v", got)
	}
	if cost := tally.Now().Sub(before); cost.Opens != 0 || cost.Bytes != 0 || cost.Decodes != 0 {
		t.Fatalf("a look at transcripts at rest cost %+v", cost)
	}
	both(itself, main[0]+"\n", os.O_TRUNC)
	same("rewritten")
	both(itself, main[1]+"\n", os.O_APPEND)
	same("grown after the rewrite")
	os.Remove(plainFile)
	os.Remove(indexedFile)
	same("gone")
	both(itself, strings.Join(main, "\n")+"\n", os.O_APPEND)
	same("back")
	indexed.Forget(indexedFile)
	if n := ix.Counters(); n.Files != 0 || n.Subs != 0 || indexed.Cursors() != 0 {
		t.Fatalf("forgotten transcripts are still followed: %+v, %d cursors", n, indexed.Cursors())
	}
}
