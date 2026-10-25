package sources

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func transcriptLines(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// What the agent and the person wrote is a mention; what a tool printed is not, nor is a link to another server.
func TestMentionsAreTheTextsNotTheToolOutput(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "projects", "p")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s.jsonl")
	body := transcriptLines(
		`{"type":"user","timestamp":"2026-10-24T10:00:00Z","message":{"role":"user","content":"look at https://gitlab.example.org/team/app/-/merge_requests/7 please"}}`,
		`{"type":"assistant","timestamp":"2026-10-24T10:00:05Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{"command":"grep -r merge_requests ."}}]}}`,
		`{"type":"user","timestamp":"2026-10-24T10:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":"fixtures: https://gitlab.example.org/team/admin/-/merge_requests/502 and https://evil.example/a/b/-/merge_requests/1"}]}}`,
		`{"type":"assistant","timestamp":"2026-10-24T10:00:10Z","message":{"role":"assistant","content":[{"type":"text","text":"The change is [here](https://gitlab.example.org/group/sub/proj/-/merge_requests/80/diffs), not https://evil.example/a/b/-/merge_requests/2 or http://gitlab.example.org/x/y/-/merge_requests/3."}]}}`,
		`{"type":"assistant","timestamp":"2026-10-24T10:00:11Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":"https://gitlab.example.org/secret/th/-/merge_requests/9"}]}}`,
	)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := &Route{Links: &Links{GitLabHosts: []string{"gitlab.example.org"}}, Home: home}
	res, found, failed := rt.Read(path, "")
	if !found || failed {
		t.Fatalf("found=%v failed=%v", found, failed)
	}
	var got []string
	for _, m := range res.MRs {
		got = append(got, m.URL+" "+m.Project+" "+m.IID)
	}
	want := []string{
		"https://gitlab.example.org/team/app/-/merge_requests/7 team/app 7",
		"https://gitlab.example.org/group/sub/proj/-/merge_requests/80 group/sub/proj 80",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("mentions\n got %q\nwant %q", got, want)
	}
}
