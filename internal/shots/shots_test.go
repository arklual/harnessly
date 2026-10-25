package shots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func use(id, name, input, ts, cwd string) string {
	return `{"type":"assistant","timestamp":"` + ts + `","cwd":"` + cwd + `","message":{"role":"assistant","content":[{"type":"tool_use","id":"` + id +
		`","name":"` + name + `","input":` + input + `}]}}`
}

func result(id, text string) string {
	return `{"type":"user","message":{"role":"user","content":[{"tool_use_id":"` + id + `","type":"tool_result","content":[` + text + `]}]}}`
}

func write(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadListsImagesTheAgentLookedAt(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "s1.jsonl")
	write(t, tr,
		use("t1", "Read", `{"file_path":"/work/shots/home.png"}`, "2026-10-25T10:00:00.000Z", "/work"),
		result("t1", `{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}`),
		use("t2", "Read", `{"file_path":"/work/README.md"}`, "2026-10-25T10:00:01.000Z", "/work"),
		result("t2", `{"type":"text","text":"x"}`),
		use("t3", "Read", `{"file_path":"/work/shots/missing.jpg"}`, "2026-10-25T10:00:02.000Z", "/work"),
		strings.Replace(result("t3", `{"type":"text","text":"nope"}`), `"tool_result"`, `"tool_result","is_error":true`, 1),
		use("t4", "mcp__playwright__browser_take_screenshot", `{"fullPage":true}`, "2026-10-25T10:00:03.000Z", "/work"),
		result("t4", `{"type":"text","text":"### Result\n- [Screenshot of full page](.playwright-mcp/desk.png)\n"}`),
		use("t5", "Read", `{"file_path":"/work/shots/home.png"}`, "2026-10-25T10:00:09.000Z", "/work"),
		result("t5", `{"type":"image"}`),
	)
	sub := filepath.Join(dir, "s1", "subagents", "agent-a1.jsonl")
	write(t, sub,
		use("s1", "Read", `{"file_path":"/work/shots/mobile.webp"}`, "2026-10-25T10:00:05.000Z", "/work"),
		result("s1", `{"type":"image"}`),
	)
	if err := os.WriteFile(strings.TrimSuffix(sub, ".jsonl")+".meta.json", []byte(`{"agentType":"Explore","description":"Проверить телефон"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var r Reader
	got := r.Read(tr)
	type row struct{ path, agent string }
	var rows []row
	for _, s := range got {
		rows = append(rows, row{s.Path, s.Agent})
	}
	want := []row{
		{"/work/shots/home.png", ""}, // looked at twice: once, by the later look
		{"/work/shots/mobile.webp", "Проверить телефон"},
		{"/work/.playwright-mcp/desk.png", ""},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %+v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Fatalf("row %d: got %+v want %+v", i, rows[i], want[i])
		}
	}
	if got[0].At != time.Date(2026, 10, 25, 10, 0, 9, 0, time.UTC).UnixMilli() || got[0].ID == "" || got[0].Name() != "home.png" {
		t.Fatalf("first: %+v", got[0])
	}

	// A line appended later is read on the next call, without the rest being read again.
	f, _ := os.OpenFile(tr, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(use("t6", "Read", `{"file_path":"/work/new.gif"}`, "2026-10-25T10:01:00.000Z", "/work") + "\n" +
		result("t6", `{"type":"image"}`) + "\n")
	f.Close()
	got = r.Read(tr)
	if len(got) != 4 || got[0].Path != "/work/new.gif" {
		t.Fatalf("after append: %+v", got)
	}
}

func TestListIsCapped(t *testing.T) {
	dir := t.TempDir()
	tr := filepath.Join(dir, "s.jsonl")
	var lines []string
	for i := 0; i < Max+10; i++ {
		id := "t" + string(rune('A'+i%26)) + string(rune('a'+i/26))
		p := `{"file_path":"/work/` + id + `.png"}`
		lines = append(lines, use(id, "Read", p, time.Date(2026, 10, 25, 10, 0, i, 0, time.UTC).Format("2006-01-02T15:04:05.000Z"), "/work"), result(id, `{"type":"image"}`))
	}
	write(t, tr, lines...)
	var r Reader
	got := r.Read(tr)
	if len(got) != Max {
		t.Fatalf("got %d", len(got))
	}
	if !strings.HasSuffix(got[0].Path, "tEb.png") && got[0].At < got[len(got)-1].At {
		t.Fatalf("not newest first")
	}
}
