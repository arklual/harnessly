package consent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		tool, cmd string
		want      Class
	}{
		{"Read", "", Read},
		{"Grep", "", Read},
		{"Edit", "", Local},
		{"Write", "", Local},
		{"Bash", "go test ./internal/voice/...", Local},
		{"Bash", "git status && git log --oneline -3", Local},
		{"Bash", "git push origin HEAD:main", External},
		{"Bash", "git -C repo push", External},
		{"Bash", "glab mr merge 12", External},
		{"Bash", "gh pr create --fill", External},
		{"Bash", "kubectl -n app rollout restart deploy/web", External},
		{"Bash", "kubectl get pods", Local},
		{"Bash", "curl -s https://example.com/api", Local},
		{"Bash", "curl -s -X POST https://example.com/api -d '{}'", External},
		{"Bash", "npm publish", External},
		{"Bash", "rm -rf build", External},
		{"Bash", "tg-send 'привет'", External},
		{"mcp__tracker__search_issues", "", Read},
		{"mcp__tracker__get_issue", "", Read},
		{"mcp__tracker__create_issue", "", External},
		{"mcp__tracker__add_issue_comment", "", External},
		{"mcp__mail__send_email", "", External},
		{"mcp__shop__refund_order", "", External},
		{"mcp__docs__delete_page", "", External},
		{"mcp__lake__query", "", Read},
		{"mcp__preview__browser_click", "", Read},
		{"mcp__preview__send_to_conversation", "", Local},
		{"mcp__preview__delegate_task", "", Local},
		{"mcp__preview__generate_images", "", Local},
		{"Bash", "curl -s -dx=1 https://example.com", External},
		{"Bash", "curl --json '{}' https://example.com", External},
		{"Bash", "wget --post-data=x https://example.com", External},
		{"Bash", "gh api repos/o/r/issues -f title=x", External},
		{"Bash", "gh api repos/o/r/issues", Local},
		{"Bash", "scp a.txt host:/tmp", External},
		{"Bash", "rsync -a dist/ host:/srv", External},
		{"Bash", "rsync -a dist/ out/", Local},
		{"Bash", "aws s3 cp a s3://b", External},
		{"Bash", "echo Z2l0IHB1c2g= | base64 -d | sh", External},
		{"mcp__other__get_and_delete_record", "", External},
		{"mcp__other__send_conversation_reply", "", External},
		{"mcp__other__frobnicate", "", External},
		{"mcp__preview__preview_set_throttle", "", Local},
		{"SomethingNew", "", External},
	}
	for _, c := range cases {
		got := Classify(c.tool, c.cmd)
		if got.Class != c.want {
			t.Errorf("%s %q: %s, want %s", c.tool, c.cmd, got.Class, c.want)
		}
		if got.Class == External && got.What == "" {
			t.Errorf("%s %q: an outside call says what it does", c.tool, c.cmd)
		}
	}
}

func TestTheMarkAndTheAnswer(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if Live(dir, "s1", now) {
		t.Fatal("no mark, no call")
	}
	if err := Mark(dir, "s1", true); err != nil {
		t.Fatal(err)
	}
	if !Live(dir, "s1", now) || Live(dir, "s1", now.Add(time.Hour)) {
		t.Fatal("a fresh mark is a call, a stale one is not")
	}
	if Mark(dir, "../x", true) == nil || Live(dir, "../x", now) {
		t.Fatal("a session name never leaves the folder")
	}
	_ = Mark(dir, "s1", false)
	if Live(dir, "s1", now) {
		t.Fatal("taken away")
	}

	push := []byte(`{"tool_name":"Bash","tool_input":{"command":"git push origin HEAD:main"}}`)
	var got struct {
		H struct {
			D string `json:"permissionDecision"`
			R string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(Answer(push, false), &got); err != nil || got.H.D != "ask" || !strings.Contains(got.H.R, "git push") {
		t.Fatalf("claude: %+v %v", got, err)
	}
	codex := []byte(`{"tool_name":"shell","tool_input":{"command":["bash","-lc","git push"]}}`)
	if err := json.Unmarshal(Answer(codex, true), &got); err != nil || got.H.D != "deny" || !strings.Contains(got.H.R, "Делаем?") {
		t.Fatalf("codex: %+v %v", got, err)
	}
	if Answer([]byte(`{"tool_name":"Read","tool_input":{}}`), false) != nil {
		t.Fatal("reading goes at once")
	}
	if Answer([]byte(`nope`), false) == nil || Answer(nil, false) == nil {
		t.Fatal("a call the hook can not read is asked about while the voice is on")
	}
}
