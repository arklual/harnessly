package usagereport

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMRsOfCall(t *testing.T) {
	const host = "gitlab.example.com"
	cases := []struct {
		name, command, output string
		want                  []string
	}{
		{
			name:    "curl POST answers with its JSON",
			command: `curl -s -X POST -H "PRIVATE-TOKEN: $T" "https://gitlab.example.com/api/v4/projects/team%2Fapp/merge_requests" -d source_branch=x -d target_branch=main`,
			output:  `{"id":9,"iid":12,"project_id":3,"web_url":"https://Gitlab.Example.com/team/app/-/merge_requests/12","state":"opened"}`,
			want:    []string{host + "/team/app/!12"},
		},
		{
			name:    "a script prints only the iid",
			command: `curl --request POST --data "title=t" https://gitlab.example.com/api/v4/projects/team%2Fapp/merge_requests`,
			output:  `{"iid": 31, "title": "t"}`,
			want:    []string{host + "/team/app/!31"},
		},
		{
			name:    "glab mr create",
			command: `glab mr create --fill --yes`,
			output:  "Creating merge request for feat into main in team/sub/app\n\nhttps://gitlab.example.com/team/sub/app/-/merge_requests/7\n",
			want:    []string{host + "/team/sub/app/!7"},
		},
		{
			name:    "push notice",
			command: `git push -u origin feat`,
			output:  "remote: View merge request for feat:\nremote:   https://gitlab.example.com/team/app/-/merge_requests/44\nremote:\n",
			want:    []string{host + "/team/app/!44"},
		},
		{
			name:    "reading a list of requests is not creating one",
			command: `curl -s -G "https://gitlab.example.com/api/v4/projects/team%2Fapp/merge_requests" --data-urlencode state=opened`,
			output:  `[{"iid":1,"web_url":"https://gitlab.example.com/team/app/-/merge_requests/1"}]`,
		},
		{
			name:    "a plain GET names none",
			command: `curl -s https://gitlab.example.com/api/v4/projects/team%2Fapp/merge_requests/5`,
			output:  `{"iid":5,"web_url":"https://gitlab.example.com/team/app/-/merge_requests/5"}`,
		},
		{
			name:    "a link into a request is not the request",
			command: `glab mr create`,
			output:  "see https://gitlab.example.com/team/app/-/merge_requests/9/diffs and https://gitlab.example.com/team/app/-/merge_requests/9\n",
			want:    []string{host + "/team/app/!9"},
		},
		{name: "an unrelated command", command: `go test ./...`, output: "ok"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MRsOfCall(c.command, c.output); !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestMRsOfMCP(t *testing.T) {
	out := `Merge request created: https://gitlab.example.com/a/b/-/merge_requests/3`
	if got := MRsOfMCP("save_merge_request", out); !reflect.DeepEqual(got, []string{"gitlab.example.com/a/b/!3"}) {
		t.Errorf("got %v", got)
	}
	if got := MRsOfMCP("get_merge_request", out); got != nil {
		t.Errorf("a read named %v", got)
	}
}

func TestPRKeys(t *testing.T) {
	keys, other := PRKeys([]string{"https://GitLab.example.com/a/b/-/merge_requests/3", "https://github.com/a/b/pull/4"})
	if !reflect.DeepEqual(keys, []string{"gitlab.example.com/a/b/!3"}) || !reflect.DeepEqual(other, []string{"https://github.com/a/b/pull/4"}) {
		t.Errorf("keys %v other %v", keys, other)
	}
}

const mrTranscript = `{"type":"assistant","sessionId":"s1","timestamp":"2026-10-30T10:00:00.000Z","cwd":"/work/app","requestId":"r1","message":{"id":"m1","model":"claude-sonnet-4","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"curl -s -X POST https://gitlab.example.com/api/v4/projects/team%2Fapp/merge_requests -d source_branch=x"}},{"type":"tool_use","id":"tu2","name":"Bash","input":{"command":"ls"}}]}}
{"type":"user","sessionId":"s1","timestamp":"2026-10-30T10:00:05.000Z","cwd":"/work/app","uuid":"u1","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"{\"iid\":12,\"web_url\":\"https://gitlab.example.com/team/app/-/merge_requests/12\"}"},{"type":"tool_result","tool_use_id":"tu2","content":"x"}]}}
{"type":"pr-link","sessionId":"s1","prUrl":"https://gitlab.example.com/team/app/-/merge_requests/12"}
`

func TestParseClaudeMRKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s1.jsonl")
	if err := os.WriteFile(path, []byte(mrTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := (&Projects{}).ParseClaude(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(f.MRKeys, []string{"gitlab.example.com/team/app/!12"}) {
		t.Errorf("MRKeys %v", f.MRKeys)
	}
	if len(f.PRs) != 1 {
		t.Errorf("PRs %v", f.PRs)
	}
}

// A facts file written before merge requests were looked for gets them a few transcripts at a time, and keeps them in
// the file beside it; the facts file itself is not rewritten for it.
func TestCollectorBackfillsMRKeys(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects", "p")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < backfillPerPass+5; i++ {
		body := strings.ReplaceAll(mrTranscript, "s1", "s"+string(rune('a'+i)))
		if err := os.WriteFile(filepath.Join(root, "t"+string(rune('a'+i))+".jsonl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cache := filepath.Join(dir, "stats", "usage-v2.json")
	now := time.Now()
	cfg := Config{ClaudeRoot: filepath.Join(dir, "projects"), CodexRoot: filepath.Join(dir, "none"), CacheFile: cache, Projects: &Projects{}, Workers: 2, Now: func() time.Time { return now }}
	c := NewCollector(cfg)
	c.Refresh()
	for _, e := range c.Entries() {
		if len(e.Facts.MRKeys) != 1 {
			t.Fatalf("a fresh parse has the keys: %v", e.Facts.MRKeys)
		}
	}
	// Forget the sidecar: the facts file stays, as a file of the previous version would be.
	if err := os.Remove(cache + MRSuffix); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(cache)
	c2 := NewCollector(cfg)
	c2.Refresh()
	missing := 0
	for _, e := range c2.Entries() {
		if e.Facts.MRKeys == nil {
			missing++
		}
	}
	if missing != 5 {
		t.Fatalf("one pass reads %d transcripts again, %d wait; want 5 waiting", backfillPerPass, missing)
	}
	c2.Refresh()
	for _, e := range c2.Entries() {
		if len(e.Facts.MRKeys) != 1 {
			t.Fatalf("after the second pass: %v", e.Facts.MRKeys)
		}
	}
	if after, _ := os.Stat(cache); !after.ModTime().Equal(before.ModTime()) {
		t.Error("the facts file was rewritten for a backfill")
	}
	// The stored reader sees the keys.
	s := &Stored{Path: cache}
	s.Refresh()
	for _, e := range s.Entries() {
		if len(e.Facts.MRKeys) != 1 {
			t.Fatalf("stored: %v", e.Facts.MRKeys)
		}
	}
}

const toolTranscript = `{"type":"assistant","sessionId":"s1","timestamp":"2026-10-30T10:00:00.000Z","cwd":"/work/app","requestId":"r1","message":{"id":"m1","model":"claude-sonnet-4","usage":{"input_tokens":1,"output_tokens":1},"content":[{"type":"tool_use","id":"tu1","name":"Skill","input":{"skill":"release"}},{"type":"tool_use","id":"tu2","name":"mcp__tracker__get_issue","input":{}},{"type":"tool_use","id":"tu3","name":"mcp__tracker__search","input":{}}]}}
{"type":"user","sessionId":"s1","timestamp":"2026-10-30T10:00:05.000Z","cwd":"/work/app","uuid":"u1","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"ok"},{"type":"tool_result","tool_use_id":"tu2","content":"ok"},{"type":"tool_result","tool_use_id":"tu3","content":"boom","is_error":true}]}}
`

// The tool calls are kept beside the facts file, summed by day, kind and name, so that the agent that reads the
// runtime's facts file (Stored) sees them; a side file written before them makes the transcripts be read again.
func TestToolCallsSurviveTheFactsFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects", "p")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "s1.jsonl"), []byte(toolTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "stats", "usage-v2.json")
	now := time.Now()
	cfg := Config{ClaudeRoot: filepath.Join(dir, "projects"), CodexRoot: filepath.Join(dir, "none"), CacheFile: cache, Projects: &Projects{}, Workers: 1, Now: func() time.Time { return now }}
	ts := float64(time.Date(2026, 10, 30, 10, 0, 5, 0, time.UTC).Unix())
	want := []ToolCall{
		{Ts: ts, Kind: ToolMCP, Name: "tracker", Error: true, N: 2, Errs: 1},
		{Ts: ts, Kind: ToolSkill, Name: "release", N: 1},
	}
	count := func(calls []ToolCall) (n, errs int) {
		for _, c := range calls {
			a, b := c.Count()
			n, errs = n+a, errs+b
		}
		return n, errs
	}
	c := NewCollector(cfg)
	c.Refresh()
	if got := c.Entries()[0].Facts.ToolCalls; !reflect.DeepEqual(got, want) {
		t.Fatalf("collector: %+v", got)
	}
	s := &Stored{Path: cache}
	s.Refresh()
	got := s.Entries()[0].Facts.ToolCalls
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored: %+v", got)
	}
	if n, errs := count(got); n != 3 || errs != 1 {
		t.Fatalf("stored counts %d calls, %d failed", n, errs)
	}
	// A side file of the previous version: keys but no tools.
	if err := os.WriteFile(cache+MRSuffix, []byte(`{"version":1,"files":{"`+filepath.Join(root, "s1.jsonl")+`":{"size":`+
		strconv.Itoa(len(toolTranscript))+`,"mtimeMs":`+strconv.FormatFloat(c.Entries()[0].MtimeMs, 'f', -1, 64)+`,"keys":[]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := &Stored{Path: cache}
	s2.Refresh()
	if s2.Entries()[0].Facts.ToolCalls != nil {
		t.Fatal("an entry without tools reads as known")
	}
	c2 := NewCollector(cfg)
	c2.Refresh()
	if got := c2.Entries()[0].Facts.ToolCalls; !reflect.DeepEqual(got, want) {
		t.Fatalf("after the backfill: %+v", got)
	}
	s3 := &Stored{Path: cache}
	s3.Refresh()
	if got := s3.Entries()[0].Facts.ToolCalls; !reflect.DeepEqual(got, want) {
		t.Fatalf("stored after the backfill: %+v", got)
	}
}

func TestCountListedSkills(t *testing.T) {
	got := CountListedSkills("- a:one: does a\n  more\n- b\n- a:one: again\n\n- c: x")
	if got != 3 {
		t.Fatalf("got %d, want 3 (a:one, b, c)", got)
	}
	if CountListedSkills("no list here") != 0 {
		t.Fatal("text without list lines has no skills")
	}
}

// The count of skills the initial listing offered is read from the transcript, kept in the side file and read back by a
// machine that reads the runtime's facts file; a listing that is not the initial one, or a subagent's, is not counted.
func TestSkillsListedSurviveTheFactsFile(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects", "p")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	listing := `{"type":"attachment","timestamp":"2026-10-30T10:00:00.000Z","attachment":{"type":"skill_listing","isInitial":true,"content":"- a: one\n- b:c\n- a: again"}}` + "\n" +
		`{"type":"attachment","timestamp":"2026-10-30T10:00:01.000Z","attachment":{"type":"skill_listing","isInitial":false,"content":"- x\n- y\n- z"}}` + "\n"
	if err := os.WriteFile(filepath.Join(root, "s1.jsonl"), []byte(listing+toolTranscript), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "stats", "usage-v2.json")
	now := time.Now()
	cfg := Config{ClaudeRoot: filepath.Join(dir, "projects"), CodexRoot: filepath.Join(dir, "none"), CacheFile: cache, Projects: &Projects{}, Workers: 1, Now: func() time.Time { return now }}
	c := NewCollector(cfg)
	c.Refresh()
	if got := c.Entries()[0].Facts.SkillsListed; got != 2 {
		t.Fatalf("collector: %d, want 2", got)
	}
	s := &Stored{Path: cache}
	s.Refresh()
	if got := s.Entries()[0].Facts.SkillsListed; got != 2 {
		t.Fatalf("stored: %d, want 2", got)
	}
}
