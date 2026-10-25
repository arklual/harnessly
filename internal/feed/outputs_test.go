package feed

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// toolPair is a call and its result as Claude writes them.
func toolPair(t *testing.T, n int, name string, input map[string]any, result string) string {
	t.Helper()
	in, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(result)
	id := fmt.Sprintf("toolu_%d", n)
	return fmt.Sprintf(`{"type":"assistant","timestamp":"2026-10-12T10:00:%02d.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":%q,"name":%q,"input":%s}]}}`, n, id, name, in) + "\n" +
		fmt.Sprintf(`{"type":"user","timestamp":"2026-10-12T10:00:%02d.500Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":%q,"content":%s}]}}`, n, id, out) + "\n"
}

// A turn's outputs are the things it made: the page it published, the merge request it opened, the design and the
// document it wrote, the stand it showed; a plain file is not one, and what is named twice is one card.
func TestATurnListsWhatItMade(t *testing.T) {
	t.Setenv("RUNTIME_DOCS_HOST", "docs.example.com")
	t.Setenv("RUNTIME_STAND_DOMAIN", ".preview.example.com")
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"сделай"}}` + "\n" +
		toolPair(t, 1, "Artifact", map[string]any{"file_path": "/tmp/s/report.html", "title": "Разбор выката"},
			"Published https://claude.ai/code/artifact/abc-123 (private).") +
		toolPair(t, 2, "Bash", map[string]any{"command": `acme-artifact publish plan.md --title "План работ"`},
			"ok\nhttps://docs.example.com/a/plan-raboт\n") +
		toolPair(t, 3, "Bash", map[string]any{"command": `glab mr create --title "Outputs cards" --yes`},
			"Creating merge request\nhttps://gitlab.example.com/team/harnessly/-/merge_requests/80\n") +
		toolPair(t, 4, "Write", map[string]any{"file_path": "/w/design-artifacts/landing-v2/project/index.dc.html", "content": "x"}, "ok") +
		toolPair(t, 5, "Edit", map[string]any{"file_path": "/w/design-artifacts/landing-v2/project/about.dc.html", "old_string": "a", "new_string": "b"}, "ok") +
		toolPair(t, 6, "Write", map[string]any{"file_path": "/w/repo/docs/outputs.md", "content": "# x"}, "ok") +
		toolPair(t, 7, "Write", map[string]any{"file_path": "/w/repo/web/app/turn.tsx", "content": "x"}, "ok") +
		toolPair(t, 8, "Bash", map[string]any{"command": "harnessly open http://localhost:5173/dev/chat"}, "opened") +
		toolPair(t, 9, "Bash", map[string]any{"command": "curl -s https://abc-1.preview.example.com/"}, "<html>") +
		toolPair(t, 10, "Bash", map[string]any{"command": "glab mr view 80"}, "https://gitlab.example.com/team/harnessly/-/merge_requests/80")
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	if len(f.b.turns) != 1 {
		t.Fatalf("turns: %d\n%s", len(f.b.turns), shape(f))
	}
	var got []string
	for _, o := range f.b.turns[0].rec.Outputs {
		got = append(got, fmt.Sprintf("%s %s %q %s", o.Kind, o.Op, o.Title, o.Ref))
	}
	want := []string{
		`artifact created "Разбор выката" https://claude.ai/code/artifact/abc-123`,
		`artifact created "План работ" https://docs.example.com/a/plan-raboт`,
		`mr created "Outputs cards" https://gitlab.example.com/team/harnessly/-/merge_requests/80`,
		`design changed "landing-v2" landing-v2`,
		`doc created "outputs" /w/repo/docs/outputs.md`,
		`stand created "localhost:5173" http://localhost:5173/dev/chat`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("outputs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if n := len(f.b.turns[0].rec.Files); n != 4 {
		t.Fatalf("files: %d", n)
	}
}

// An Artifact call that updates a page changes it; a failed call made nothing.
// The team's catalogue and its stands are the deployment's hosts: a page or a stand of another host is not one, and
// with none set only claude.ai artifacts are pages.
func TestCatalogueAndStandsAreTheDeploymentsHosts(t *testing.T) {
	t.Setenv("RUNTIME_DOCS_HOST", "docs.example.com")
	t.Setenv("RUNTIME_STAND_DOMAIN", ".preview.example.com")
	for link, want := range map[string]bool{
		"https://docs.example.com/a/plan":     true,
		"https://DOCS.example.com/b/x?k=1":    true,
		"https://docs.example.com.evil.io/a/": false,
		"https://other.example.com/a/plan":    false,
		"https://claude.ai/code/artifact/abc": true,
	} {
		if artifactLink(link) != want {
			t.Errorf("artifact %s: want %v", link, want)
		}
	}
	for link, want := range map[string]bool{
		"https://abc-1.preview.example.com/":         true,
		"https://abc-1.preview.example.com:8443/x":   true,
		"https://preview.example.com/":               false,
		"https://a.b.preview.example.com/":           false,
		"https://abc-1.preview.example.com.evil.io/": false,
	} {
		if standLink(link) != want {
			t.Errorf("stand %s: want %v", link, want)
		}
	}
	t.Setenv("RUNTIME_DOCS_HOST", "")
	t.Setenv("RUNTIME_STAND_DOMAIN", "")
	if artifactLink("https://docs.example.com/a/plan") || standLink("https://abc-1.preview.example.com/") {
		t.Fatal("no hosts set, yet a page or a stand")
	}
	if !artifactLink("https://claude.ai/artifact/abc") {
		t.Fatal("a claude.ai artifact needs no host")
	}
}

func TestAnUpdatedPageIsChangedAndAFailedCallMadeNothing(t *testing.T) {
	failed := `{"type":"assistant","timestamp":"2026-10-12T10:00:03.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_9","name":"Artifact","input":{"file_path":"/tmp/b.html"}}]}}` + "\n" +
		`{"type":"user","timestamp":"2026-10-12T10:00:04.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_9","is_error":true,"content":"refused https://claude.ai/artifact/zzz"}]}}` + "\n"
	text := `{"type":"user","timestamp":"2026-10-12T10:00:00.000Z","message":{"role":"user","content":"обнови"}}` + "\n" +
		toolPair(t, 1, "Artifact", map[string]any{"file_path": "/tmp/s/board.html", "url": "https://claude.ai/artifact/old-1"}, "Updated.") +
		failed
	f := New("c1", wire.ProviderClaude, &ClaudeSource{Path: write(t, text)})
	f.Load()
	validate(t, f)
	outs := f.b.turns[0].rec.Outputs
	if len(outs) != 1 || outs[0].Op != wire.OutputChanged || outs[0].Ref != "https://claude.ai/artifact/old-1" || outs[0].Title != "board" {
		t.Fatalf("outputs: %+v", outs)
	}
}
