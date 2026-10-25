package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// An entry is named by the bytes of its own line, so a whole read, a tail, a page back and an incremental read of the
// same file all give an entry the same id.

// idTypes is the id and the type of every entry, the ones inside an agent group too; ids must be valid and unique.
func idTypes(t *testing.T, label string, entries []*jsvalue.Object) map[string]string {
	t.Helper()
	out := map[string]string{}
	var walk func(es []*jsvalue.Object)
	walk = func(es []*jsvalue.Object) {
		for _, e := range es {
			id := e.String("id")
			if !wire.ValidEntryID(id) {
				t.Errorf("%s: %q is not an entry id (%s)", label, id, typeOf(e))
			}
			if prev, dup := out[id]; dup {
				t.Errorf("%s: id %s is twice: %s and %s", label, id, prev, typeOf(e))
			}
			out[id] = typeOf(e)
			if typeOf(e) == "agent-group" {
				var kids []*jsvalue.Object
				for _, k := range e.Array("entries") {
					kids = append(kids, k.(*jsvalue.Object))
				}
				walk(kids)
			}
		}
	}
	walk(entries)
	return out
}

func topIDs(entries []*jsvalue.Object) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.String("id")
	}
	return ids
}

// sameIDs: every entry of got is in want under its id and type.
func sameIDs(t *testing.T, label string, got, want map[string]string) {
	t.Helper()
	missed := 0
	for id, typ := range got {
		if wantTyp, ok := want[id]; !ok || wantTyp != typ {
			if missed++; missed <= 5 {
				t.Errorf("%s: %s (%s) is not in the whole read (%q)", label, id, typ, wantTyp)
			}
		}
	}
}

// cuts are growing sizes of a file: every stride bytes, so most land inside a line, and the whole.
func cuts(size, stride int64) []int64 {
	var out []int64
	for c := stride; c < size; c += stride {
		out = append(out, c)
	}
	return append(out, size)
}

// subsequenceLoss is how many of want are not in got, which must keep want's order; -1 when it does not.
func subsequenceLoss(got, want []string) int {
	i := 0
	for _, g := range got {
		for i < len(want) && want[i] != g {
			i++
		}
		if i == len(want) {
			return -1
		}
		i++
	}
	return len(want) - len(got)
}

func claudeFixtureFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pat := range []string{"recorded-*.jsonl", "synthetic-tools.jsonl", "synthetic-user.jsonl", "synthetic-plan-*.jsonl"} {
		m, err := filepath.Glob(filepath.Join("testdata", "claude", pat))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	if len(files) < 20 {
		t.Fatalf("only %d fixtures", len(files))
	}
	return files
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func claudeIncremental(t *testing.T, path string, data []byte, stride int64) []*jsvalue.Object {
	t.Helper()
	var all []*jsvalue.Object
	var offset int64
	pending := ""
	for _, cut := range cuts(int64(len(data)), stride) {
		writeFile(t, path, data[:cut])
		r := ParseIncremental(path, offset, pending)
		all = append(all, r.NewEntries...)
		offset, pending = r.NewOffset, r.PendingBuffer
	}
	return all
}

func TestClaudeIDsOfFixturesAreStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.jsonl")
	for _, f := range claudeFixtureFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		writeFile(t, path, data)
		full := ParseFile(path)
		fullIDs := idTypes(t, name+" whole", full)
		if len(full) == 0 {
			t.Errorf("%s: nothing parsed", name)
		}
		if viaContent := ParseContent(string(data)); fmt.Sprint(topIDs(viaContent)) != fmt.Sprint(topIDs(full)) {
			t.Errorf("%s: ParseContent names its entries otherwise than ParseFile", name)
		}
		for _, stride := range []int64{7, 61, 333, 5001} {
			if stride == 7 && len(data) > 8000 {
				continue
			}
			label := fmt.Sprintf("%s incremental by %d", name, stride)
			have := idTypes(t, label, claudeIncremental(t, path, data, stride))
			// Every entry the whole read keeps is in the incremental one under its id. The incremental read may hold
			// more: a command output not yet folded into its command, a queued prompt not yet matched to its delivery.
			// A group of a sidechain cut by a read into two is the one entry that may not be.
			for id, typ := range fullIDs {
				if got, ok := have[id]; !ok || got != typ && typ != "plan" && typ != "agent-group" {
					t.Errorf("%s: %s (%s) is missing", label, id, typ)
				}
			}
		}
		writeFile(t, path, data)
		tail := ReadTail(path, 3)
		sameIDs(t, name+" tail", idTypes(t, name+" tail", tail.Entries), fullIDs)
		if n := len(full); n > 3 {
			if got, want := topIDs(tail.Entries), topIDs(full[n-3:]); fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("%s: tail %v, whole read ends %v", name, got, want)
			}
		}
	}
}

// genClaude is a transcript of n turns: unique uuids, lines with several entries, lines without a uuid (queued
// prompts) and text in two bytes per letter, so that a byte offset is not a character count.
func genClaude(n, pad int) []byte {
	var b strings.Builder
	filler := strings.Repeat("щ", pad)
	uuid := func(kind, i int) string { return fmt.Sprintf("%08x-0000-4000-8000-%012x", i, kind) }
	for i := 0; i < n; i++ {
		ts := fmt.Sprintf("2026-03-01T10:%02d:%02d.000Z", (i/60)%60, i%60)
		fmt.Fprintf(&b, `{"type":"user","uuid":%q,"timestamp":%q,"cwd":"/w","message":{"role":"user","content":"вопрос %d %s"}}`+"\n",
			uuid(1, i), ts, i, filler)
		if i%7 == 3 {
			fmt.Fprintf(&b, `{"type":"queue-operation","operation":"enqueue","timestamp":%q,"content":"в очереди %d"}`+"\n", ts, i)
		}
		fmt.Fprintf(&b, `{"type":"assistant","uuid":%q,"timestamp":%q,"cwd":"/w","message":{"role":"assistant","content":[`+
			`{"type":"thinking","thinking":"думаю %d"},{"type":"text","text":"ответ %d %s"},`+
			`{"type":"tool_use","id":"toolu_%d","name":"Bash","input":{"command":"ls %d"}}]}}`+"\n", uuid(2, i), ts, i, i, filler, i, i)
		fmt.Fprintf(&b, `{"type":"user","uuid":%q,"timestamp":%q,"cwd":"/w","message":{"role":"user","content":[`+
			`{"type":"tool_result","tool_use_id":"toolu_%d","content":"a\nb"}]}}`+"\n", uuid(3, i), ts, i)
	}
	return []byte(b.String())
}

func TestClaudeIDsOfBigFileAreStable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.jsonl")
	for _, size := range []struct{ turns, pad int }{{600, 150}, {2200, 150}} {
		data := genClaude(size.turns, size.pad)
		name := fmt.Sprintf("%d turns (%d bytes)", size.turns, len(data))
		writeFile(t, path, data)
		whole := ParseContent(string(data))
		wholeIDs := idTypes(t, name+" whole", whole)
		if !strings.Contains(strings.Join(topIDs(whole), " "), "x:") || !strings.Contains(strings.Join(topIDs(whole), " "), "#2") {
			t.Fatalf("%s: the file should have lines named by offset and lines with three entries", name)
		}

		// ParseFile reads only the last 512 000 bytes of a file over 1 MiB.
		file := ParseFile(path)
		sameIDs(t, name+" file", idTypes(t, name+" file", file), wholeIDs)
		if n := len(file); fmt.Sprint(topIDs(file)) != fmt.Sprint(topIDs(whole[len(whole)-n:])) {
			t.Errorf("%s: ParseFile does not end as the whole read", name)
		}

		// A page back from the tail, then another, until the start: together the whole read.
		var pages [][]string
		c := ReadTail(path, 100)
		for {
			sameIDs(t, name+" page", idTypes(t, name+" page", c.Entries), wholeIDs)
			pages = append(pages, topIDs(c.Entries))
			if !c.HasMore {
				break
			}
			c = ReadBefore(path, c.StartByteOffset, 100)
			if len(c.Entries) == 0 {
				t.Fatalf("%s: a page back is empty before %d", name, c.StartByteOffset)
			}
		}
		var stitched []string
		for i := len(pages) - 1; i >= 0; i-- {
			stitched = append(stitched, pages[i]...)
		}
		// A page ends at the start of a line, so a line cut by the page limit gives its first entries to nobody: the
		// pages are the whole read less a few entries, in order.
		if lost := subsequenceLoss(stitched, topIDs(whole)); lost < 0 || lost > len(pages)*2 {
			t.Errorf("%s: the pages back (%d entries) are not the whole read (%d) in order, less %d", name, len(stitched), len(whole), lost)
		}

		inc := claudeIncremental(t, path, data, 40_009)
		sameIDs(t, name+" incremental", idTypes(t, name+" incremental", inc), wholeIDs)
		if fmt.Sprint(topIDs(inc)) != fmt.Sprint(topIDs(whole)) {
			t.Errorf("%s: the incremental read does not give the whole read's ids", name)
		}
	}
}

// A line cut inside a letter of two bytes is decoded in two broken halves; its entries are named by the line all the
// same, and the lines after it by their own offsets.
func TestClaudeIDsSurviveACutInsideALetter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.jsonl")
	line := func(i int) string {
		return fmt.Sprintf(`{"type":"queue-operation","operation":"enqueue","timestamp":"2026-03-01T10:00:00.000Z","content":"щщщ %d"}`+"\n", i)
	}
	data := []byte(line(1) + line(2) + line(3))
	whole := idTypes(t, "whole", ParseContent(string(data)))
	if len(whole) != 3 {
		t.Fatalf("whole read: %v", whole)
	}
	cut := int64(strings.Index(string(data), "щ") + 1) // between the two bytes of the first letter
	writeFile(t, path, data[:cut])
	first := ParseIncremental(path, 0, "")
	writeFile(t, path, data)
	second := ParseIncremental(path, first.NewOffset, first.PendingBuffer)
	got := idTypes(t, "cut", second.NewEntries)
	if len(got) != 3 {
		t.Fatalf("after the cut: %v", got)
	}
	for id := range got {
		if _, ok := whole[id]; !ok {
			t.Errorf("%s is not among %v", id, whole)
		}
	}
}

func codexOpts() CodexOptions { return CodexOptions{} }

func codexIncremental(t *testing.T, path string, data []byte, stride int64) []*jsvalue.Object {
	t.Helper()
	p := NewCodexParser(path, codexOpts())
	var all []*jsvalue.Object
	for _, cut := range cuts(int64(len(data)), stride) {
		writeFile(t, path, data[:cut])
		all = append(all, p.ParseIncremental().NewEntries...)
	}
	return all
}

func TestCodexIDsAreStable(t *testing.T) {
	for _, tc := range []struct {
		file    string
		strides []int64
		page    int
	}{
		{"basic.jsonl", []int64{37, 997, 5003}, 20},
		{"big.jsonl", []int64{100_003}, 400},
	} {
		src := filepath.Join("testdata", "codex", "corpus", tc.file)
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), tc.file)
		writeFile(t, path, data)
		all, _ := NewCodexParser(path, codexOpts()).ParseAll()
		allIDs := idTypes(t, tc.file+" whole", all)
		if len(all) < 20 {
			t.Fatalf("%s: %d entries", tc.file, len(all))
		}
		if viaContent := ParseCodexContent(string(data), codexOpts()); fmt.Sprint(topIDs(viaContent)) != fmt.Sprint(topIDs(all)) {
			t.Errorf("%s: ParseCodexContent names its entries otherwise than ParseAll", tc.file)
		}
		for _, stride := range tc.strides {
			label := fmt.Sprintf("%s incremental by %d", tc.file, stride)
			inc := codexIncremental(t, path, data, stride)
			idTypes(t, label, inc)
			if fmt.Sprint(topIDs(inc)) != fmt.Sprint(topIDs(all)) {
				t.Errorf("%s: the ids differ from the whole read's", label)
			}
		}

		writeFile(t, path, data)
		var pages [][]string
		c := ReadCodexTail(path, tc.page, codexOpts())
		for {
			sameIDs(t, tc.file+" page", idTypes(t, tc.file+" page", c.Entries), allIDs)
			pages = append(pages, topIDs(c.Entries))
			if !c.HasMore {
				break
			}
			c = ReadCodexBefore(path, c.StartByteOffset, tc.page, c.StartByteOffset, codexOpts())
			if len(c.Entries) == 0 {
				t.Fatalf("%s: a page back is empty before %d", tc.file, c.StartByteOffset)
			}
		}
		var stitched []string
		for i := len(pages) - 1; i >= 0; i-- {
			stitched = append(stitched, pages[i]...)
		}
		if fmt.Sprint(stitched) != fmt.Sprint(topIDs(all)) {
			t.Errorf("%s: the pages back (%d entries) are not the whole read (%d)", tc.file, len(stitched), len(all))
		}
	}
}

// A call the transcript gave no id is named by its line, not by luck.
func TestCodexSyntheticCallIDsAreStable(t *testing.T) {
	lines := []string{
		`{"timestamp":"2026-03-01T10:00:00.000Z","type":"response_item","payload":{"type":"web_search_call","status":"completed","query":"щ"}}`,
		`{"timestamp":"2026-03-01T10:00:01.000Z","type":"event_msg","payload":{"type":"request_user_input","question":"ok?"}}`,
		`{"timestamp":"2026-03-01T10:00:02.000Z","type":"event_msg","payload":{"type":"exec_approval_request","command":"ls"}}`,
	}
	content := strings.Join(lines, "\n") + "\n"
	first := ParseCodexContent(content, codexOpts())
	again := ParseCodexContent(content, codexOpts())
	if len(first) != 3 || entriesJSON(first) != entriesJSON(again) {
		t.Fatalf("two reads of the same bytes differ:\n%s\n%s", entriesJSON(first), entriesJSON(again))
	}
	idTypes(t, "synthetic", first)
	for _, e := range first {
		call := e.String("callId") + e.String("toolUseId")
		if !strings.HasPrefix(call, "call:") {
			t.Errorf("%s: call id %q", typeOf(e), call)
		}
	}
	// Read from the second line on, as a tail does, the first line's entry has the offset of its line in the file.
	tail := ParseCodexContent(strings.Join(lines[1:], "\n")+"\n", codexOpts())
	if len(tail) != 2 || tail[0].String("toolUseId") == first[1].String("toolUseId") {
		t.Errorf("a read from the middle should name by its own start: %s", entriesJSON(tail))
	}
}

// A call still open when its source goes quiet is ended by the flush; its entries are named by the line that began the
// call, past anything one line makes.
func TestCodexFlushIDsDoNotMeetALinesIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.jsonl")
	begin := `{"timestamp":"2026-03-01T10:00:00.000Z","type":"event_msg","payload":{"type":"exec_command_begin","call_id":"c1","command":"sleep 1"}}` + "\n"
	writeFile(t, path, []byte(begin+begin))
	p := NewCodexParser(path, codexOpts())
	entries, _ := p.ParseAll()
	flushed := p.FlushStale(1)
	all := append(entries, flushed...)
	if len(flushed) != 2 {
		t.Fatalf("flushed %s", entriesJSON(flushed))
	}
	ids := idTypes(t, "flush", all)
	offset := len(begin)
	for _, want := range []string{fmt.Sprintf("x:%d#1000", offset), fmt.Sprintf("x:%d#1001", offset)} {
		if _, ok := ids[want]; !ok {
			t.Errorf("no %s among %v", want, ids)
		}
	}
	if again := p.FlushStale(2); len(again) != 0 {
		t.Errorf("a second flush made %s", entriesJSON(again))
	}
}

// A window of a big file starts at a byte inside some letter: the ids name lines by the bytes they really start at.
func TestCodexIDsOfWindowsInTwoByteText(t *testing.T) {
	var b strings.Builder
	letters := strings.Repeat("щ", 300)
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&b, `{"timestamp":"2026-03-01T10:00:00.000Z","type":"event_msg","payload":{"type":"agent_message","message":"%s %d"}}`+"\n", letters, i)
	}
	// A blank line more moves where the windows start by a byte: some of them start inside a letter, some not.
	for _, tail := range []string{"", "\n"} {
		data := []byte(b.String() + tail)
		path := filepath.Join(t.TempDir(), "case.jsonl")
		writeFile(t, path, data)
		all, _ := NewCodexParser(path, codexOpts()).ParseAll()
		allIDs := idTypes(t, "whole", all)
		if len(all) != 1000 {
			t.Fatalf("%d entries", len(all))
		}
		seen := 0
		c := ReadCodexTail(path, 40, codexOpts())
		for {
			sameIDs(t, "window", idTypes(t, "window", c.Entries), allIDs)
			seen += len(c.Entries)
			if !c.HasMore || len(c.Entries) == 0 {
				break
			}
			c = ReadCodexBefore(path, c.StartByteOffset, 40, c.StartByteOffset, codexOpts())
		}
		if seen < 800 {
			t.Errorf("only %d entries seen in the windows", seen)
		}
	}
}

// A chunk that starts inside a letter: the previous runtime's count of where its first line starts is two bytes past
// the line (a broken byte decodes to three), and the ids use the real one.
func TestChunkStartsInsideALetter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "case.jsonl")
	writeFile(t, path, []byte("щщщ\nx\n"))
	if _, counted, real, err := readChunk(path, 1, 9); err != nil || counted != 9 || real != 7 {
		t.Errorf("claude: counted %d, real %d, %v", counted, real, err)
	}
	if _, counted, real, err := readCodexChunk(path, 1, 9); err != nil || counted != 9 || real != 7 {
		t.Errorf("codex: counted %d, real %d, %v", counted, real, err)
	}
}

// Whole lines taken from the middle of a file, as a client that fell behind is given them, are named as the file's read
// names them once the text is told where it starts.
func TestContentFromTheMiddleOfAFile(t *testing.T) {
	claude := genClaude(40, 5)
	cut := 0
	for i := 0; i < 60; i++ {
		cut += strings.IndexByte(string(claude[cut:]), '\n') + 1
	}
	whole := idTypes(t, "whole", ParseContent(string(claude)))
	part := idTypes(t, "part", ParseContentAt(string(claude[cut:]), int64(cut)))
	sameIDs(t, "claude part", part, whole)
	if len(part) < 20 {
		t.Errorf("%d entries", len(part))
	}

	codex, err := os.ReadFile(filepath.Join("testdata", "codex", "corpus", "basic.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	cut = 0
	for i := 0; i < 30; i++ {
		cut += strings.IndexByte(string(codex[cut:]), '\n') + 1
	}
	all := idTypes(t, "whole", ParseCodexContent(string(codex), codexOpts()))
	rest := idTypes(t, "part", ParseCodexContentAt(string(codex[cut:]), int64(cut), codexOpts()))
	sameIDs(t, "codex part", rest, all)
	if len(rest) < 5 {
		t.Errorf("%d entries", len(rest))
	}
}
