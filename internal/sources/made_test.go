package sources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/wire"
)

func madeLinks() *Links { return &Links{DocsHost: "docs.example.com"} }

// madePair is a tool call and its result as Claude writes them; the call at second n, its result a half second later.
func madePair(t *testing.T, n int, name string, input map[string]any, result string, isError bool) string {
	t.Helper()
	in, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(result)
	id := fmt.Sprintf("toolu_%d", n)
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-10-12T10:00:%02d.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}}`, n, id, name, in) + "\n" +
		fmt.Sprintf(`{"type":"user","timestamp":"2026-10-12T10:00:%02d.500Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s,"is_error":%v}]}}`, n, id, out, isError) + "\n"
}

func madeRoute(t *testing.T, body string) (*Route, string) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Route{Links: madeLinks(), Home: home}, path
}

func appendTo(t *testing.T, path, more string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(more); err != nil {
		t.Fatal(err)
	}
}

func madeLine(m MadeLink) string {
	return fmt.Sprintf("%s %s %q %s %s", m.Kind, m.Op, m.Title, m.Ref, time.UnixMilli(m.At).UTC().Format("05.000"))
}

func madeLines(res Result) string {
	var out []string
	for _, m := range res.Made {
		out = append(out, madeLine(m))
	}
	return strings.Join(out, "\n")
}

// What the conversation made or touched is listed: a published page, a page it opened or read back, a board a script
// made (its key kept), a design it wrote into; a failed call, a link only said in words and a link the person pasted
// are not.
func TestMadeIsWhatTheChatTouchedNotWhatItQuoted(t *testing.T) {
	body := `{"type":"user","timestamp":"2026-10-12T09:59:00.000Z","message":{"role":"user","content":"see https://docs.example.com/a/pasted-by-person"}}` + "\n" +
		madePair(t, 1, "Artifact", map[string]any{"file_path": "/tmp/s/report.html", "title": "Разбор выката"}, "Published https://claude.ai/code/artifact/abc-123 (private).", false) +
		madePair(t, 2, "Bash", map[string]any{"command": `acme-artifact publish plan.md --title "План работ"`}, "ok\nhttps://docs.example.com/a/plan\n", false) +
		madePair(t, 3, "Artifact", map[string]any{"file_path": "/tmp/s/plan.html", "url": "https://docs.example.com/a/plan"}, "Updated https://docs.example.com/a/plan", false) +
		madePair(t, 4, "Bash", map[string]any{"command": "harnessly open https://docs.example.com/a/spec-made"}, "opened", false) +
		madePair(t, 5, "Bash", map[string]any{"command": "curl -s -X POST https://docs.example.com/b/api/boards"}, `{"url":"https://docs.example.com/b/board-1?k=KEY1"}`, false) +
		madePair(t, 6, "Write", map[string]any{"file_path": "/w/design-artifacts/landing/project/index.html", "content": "x"}, "ok", false) +
		madePair(t, 7, "Bash", map[string]any{"command": "curl https://docs.example.com/a/failed-read"}, "https://docs.example.com/a/failed-read", true) +
		madePair(t, 8, "Read", map[string]any{"file_path": "/w/x.md"}, "see https://docs.example.com/a/read-back", false) +
		`{"type":"assistant","timestamp":"2026-10-12T10:00:09.000Z","message":{"role":"assistant","content":[{"type":"text","text":"cited https://docs.example.com/a/cited-only"}]}}` + "\n"
	rt, path := madeRoute(t, body)
	res, found, failed := rt.Read(path, "")
	if !found || failed {
		t.Fatalf("found=%v failed=%v", found, failed)
	}
	want := strings.Join([]string{
		`doc changed "" https://docs.example.com/a/read-back 08.500`,
		`design changed "" landing 06.000`,
		`board changed "" https://docs.example.com/b/board-1?k=KEY1 05.500`,
		`doc changed "" https://docs.example.com/a/spec-made 04.500`,
		`doc created "План работ" https://docs.example.com/a/plan 03.500`,
		`doc created "Разбор выката" https://claude.ai/code/artifact/abc-123 01.500`,
	}, "\n")
	// The page published at second 2 and updated at 3 is one item, created, at the later time.
	if got := madeLines(res); got != want {
		t.Fatalf("made\n got:\n%s\nwant:\n%s", got, want)
	}
}

// A later read takes only what was appended; a replaced shorter file starts over.
func TestMadeGrowsWithTheTranscript(t *testing.T) {
	rt, path := madeRoute(t, madePair(t, 1, "Bash", map[string]any{"command": "harnessly open https://docs.example.com/a/one"}, "opened", false))
	res, _, _ := rt.Read(path, "")
	if len(res.Made) != 1 {
		t.Fatalf("first: %s", madeLines(res))
	}
	appendTo(t, path, madePair(t, 2, "Bash", map[string]any{"command": "harnessly open https://docs.example.com/a/two"}, "opened", false))
	res, _, _ = rt.Read(path, "")
	if len(res.Made) != 2 || !strings.Contains(res.Made[0].Ref, "/a/two") {
		t.Fatalf("grown: %s", madeLines(res))
	}
	if err := os.WriteFile(path, []byte(madePair(t, 3, "Bash", map[string]any{"command": "harnessly open https://docs.example.com/a/three"}, "o", false)), 0o644); err != nil {
		t.Fatal(err)
	}
	res, _, _ = rt.Read(path, "")
	if len(res.Made) != 1 || !strings.Contains(res.Made[0].Ref, "/a/three") {
		t.Fatalf("replaced: %s", madeLines(res))
	}
}

// A board's key is kept when a later link to the same board has none.
func TestMadeKeepsTheBoardKey(t *testing.T) {
	body := madePair(t, 1, "Bash", map[string]any{"command": "make-board"}, "https://docs.example.com/b/board-1?k=KEY1", false) +
		madePair(t, 2, "Bash", map[string]any{"command": "cat notes"}, "https://docs.example.com/b/board-1", false)
	rt, path := madeRoute(t, body)
	res, _, _ := rt.Read(path, "")
	if len(res.Made) != 1 || res.Made[0].Kind != wire.MadeBoard || res.Made[0].Ref != "https://docs.example.com/b/board-1?k=KEY1" {
		t.Fatalf("board: %s", madeLines(res))
	}
}

// A growth over the budget is read behind: the answer is not complete until the reader has caught up.
func TestMadeReadBudgetReadsBehind(t *testing.T) {
	var body strings.Builder
	for i := 1; i <= 30; i++ {
		body.WriteString(madePair(t, i, "Bash", map[string]any{"command": fmt.Sprintf("harnessly open https://docs.example.com/a/p%d", i)}, "opened", false))
	}
	rt, path := madeRoute(t, body.String())
	res, found, failed, complete := rt.ReadBudget(path, "", 1)
	if !found || failed || complete {
		t.Fatalf("first: found=%v failed=%v complete=%v", found, failed, complete)
	}
	_ = res
	deadline := time.Now().Add(20 * time.Second)
	for !complete {
		if time.Now().After(deadline) {
			t.Fatal("never complete")
		}
		time.Sleep(10 * time.Millisecond)
		res, _, failed, complete = rt.ReadBudget(path, "", 1)
		if failed {
			t.Fatal("failed")
		}
	}
	if len(res.Made) != 30 {
		t.Fatalf("made: %d", len(res.Made))
	}
	if _, _, _, ok := rt.ReadBudget(filepath.Join(t.TempDir(), "nope.jsonl"), "", 1); ok {
		t.Fatal("a path that is not a transcript is complete")
	}
}

// A file handed over, a dev server opened and the commit a push put on its server are things the chat made.
func TestMadeKeepsFilesStandsAndPushedCommits(t *testing.T) {
	file := delivery.Link(delivery.Info{Path: "/w/out/report.pdf", Name: "report.pdf", Mime: "application/pdf", Size: 12, SHA256: "ab"})
	body := madePair(t, 1, "mcp__harnessly__deliver_file", map[string]any{"path": "/w/out/report.pdf"}, "Delivered "+file, false) +
		madePair(t, 2, "Bash", map[string]any{"command": "harnessly open 4400"}, "opened http://localhost:4400/", false) +
		madePair(t, 3, "Bash", map[string]any{"command": "git push origin HEAD:main"}, "To https://gitlab.example.com/acme/web.git\n   fc896f915..730b8d5d8  HEAD -> main\n", false) +
		madePair(t, 4, "Bash", map[string]any{"command": "git push origin HEAD:main"}, "To https://gitlab.example.com/acme/web.git\n ! [rejected]        HEAD -> main (fetch first)\n", true)
	rt, path := madeRoute(t, body)
	res, found, failed := rt.Read(path, "")
	if !found || failed {
		t.Fatalf("found=%v failed=%v", found, failed)
	}
	got := madeLines(res)
	for _, want := range []string{
		`commit created "web 730b8d5d8" https://gitlab.example.com/acme/web/-/commit/730b8d5d8 03.500`,
		`file created "report.pdf" /w/out/report.pdf 01.500`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("made has no %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "rejected") || strings.Count(got, "commit ") != 1 {
		t.Errorf("a refused push made a commit:\n%s", got)
	}
}
