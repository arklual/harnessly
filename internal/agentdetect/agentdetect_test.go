package agentdetect

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"harnessly/internal/sessions"
)

type panes map[string]int

func (p panes) HasSession(n string) bool { _, ok := p[n]; return ok }
func (p panes) PanePID(n string) int     { return p[n] }

type proc struct {
	args     string
	cwd      string
	children []int
}

func fake(home string, pp panes, procs map[int]proc) *Detector {
	return &Detector{
		Home:  home,
		Panes: pp,
		Now:   func() time.Time { return time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local) },
		Procs: Procs{
			Children: func(pid int) []int { return procs[pid].children },
			Args: func(pid int) (string, bool) {
				p, ok := procs[pid]
				return p.args, ok
			},
			Cwd: func(pid int) string { return procs[pid].cwd },
		},
	}
}

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const id1 = "11111111-2222-3333-4444-555555555555"
const id2 = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func TestNotRunning(t *testing.T) {
	d := fake(t.TempDir(), panes{"s": 10, "zero": 0}, map[int]proc{10: {args: "-bash", children: []int{11}}, 11: {args: "vim x"}})
	for _, s := range []string{"missing", "zero", "s"} {
		a, err := d.Check(s)
		if err != nil || a.Running || a.Provider != nil {
			t.Fatalf("%s: %+v %v", s, a, err)
		}
		j := a.JSON()
		if len(j) != 3 || j["sessionId"] != nil || j["running"] != false {
			t.Fatalf("%s: %v", s, j)
		}
	}
}

func TestClaudeFromPidFile(t *testing.T) {
	home := t.TempDir()
	cwd := "/work/app"
	put(t, filepath.Join(home, ".claude", "sessions", "21.json"), `{"pid":21,"sessionId":"`+id1+`","cwd":"`+cwd+`"}`)
	d := fake(home, panes{"s": 10}, map[int]proc{
		10: {args: "-bash", children: []int{20}},
		20: {args: "/opt/launcher/claude-wrapper", children: []int{21}},
		21: {args: "claude --resume " + id2},
	})
	a, err := d.Check("s")
	if err != nil || !a.Running || a.Provider != &Claude || a.SessionID != id1 || a.Resumable {
		t.Fatalf("no transcript yet: %+v %v", a, err)
	}
	// The resumed conversation's transcript stands in when the record's own is not written yet.
	proj := filepath.Join(home, ".claude", "projects", sessions.ProjectName(cwd))
	put(t, filepath.Join(proj, id2+".jsonl"), "{}\n")
	if a, _ = d.Check("s"); a.SessionID != id2 || !a.Resumable {
		t.Fatalf("resumed: %+v", a)
	}
	put(t, filepath.Join(proj, id1+".jsonl"), "{}\n")
	if a, _ = d.Check("s"); a.SessionID != id1 || !a.Resumable {
		t.Fatalf("own transcript: %+v", a)
	}
	j := a.JSON()
	raw, _ := json.Marshal(j)
	var back map[string]any
	_ = json.Unmarshal(raw, &back)
	if back["providerId"] != "claude" || back["providerDisplayName"] != "Claude Code" || back["providerPanelType"] != "claude-code" || back["resumable"] != true {
		t.Fatalf("%s", raw)
	}
}

func TestClaudeStaleRecordAndArgs(t *testing.T) {
	home := t.TempDir()
	put(t, filepath.Join(home, ".claude", "sessions", "20.json"), `{"pid":20,"sessionId":"old","cwd":"/x"}`)
	d := fake(home, panes{"s": 10}, map[int]proc{
		10: {args: "bash", children: []int{20}},
		20: {args: "node /usr/lib/claude-tools/run.js", children: []int{}}, // not Claude's own command line
	})
	// The record's process has "claude" in its command line, so it counts; without a transcript it is not resumable.
	a, err := d.Check("s")
	if err != nil || a.SessionID != "old" || a.Resumable {
		t.Fatalf("%+v %v", a, err)
	}
	// A record naming another process is not this pane's.
	put(t, filepath.Join(home, ".claude", "sessions", "20.json"), `{"pid":99,"sessionId":"x","cwd":"/x"}`)
	d.Procs.Args = func(pid int) (string, bool) {
		if pid == 20 {
			return "claude --resume " + id2, true
		}
		return "bash", true
	}
	d.Procs.Cwd = func(int) string { return "/y" }
	if a, _ = d.Check("s"); a.SessionID != id2 || a.Resumable {
		t.Fatalf("from args: %+v", a)
	}
	d.Procs.Cwd = func(int) string { return "" }
	if a, _ = d.Check("s"); a.SessionID != nil || !a.Running {
		t.Fatalf("args without a folder: %+v", a)
	}
	// A record without a working folder made the previous runtime fail the request.
	put(t, filepath.Join(home, ".claude", "sessions", "20.json"), `{"pid":20,"sessionId":"x"}`)
	if _, err := d.Check("s"); err != ErrBadRecord {
		t.Fatalf("want ErrBadRecord, got %v", err)
	}
	// Without ~/.claude nothing is known of the conversation.
	if a, _ := fake(t.TempDir(), panes{"s": 10}, map[int]proc{10: {children: []int{20}}, 20: {args: "claude"}}).Check("s"); !a.Running || a.SessionID != nil {
		t.Fatalf("no claude dir: %+v", a)
	}
}

func TestCodex(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".codex", "sessions")
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.Local)
	put(t, filepath.Join(sessions.CodexDayDir(root, now.AddDate(0, 0, -2)), "rollout-x-"+id1+".jsonl"),
		`{"type":"session_meta","payload":{"id":"`+id1+`","cwd":"/w"}}`+"\n")
	procs := map[int]proc{
		10: {args: "bash", children: []int{20}},
		20: {args: "node /usr/bin/codex resume '" + id1 + "'", children: []int{21}},
		21: {args: "/usr/lib/codex/codex-linux resume " + id1},
	}
	d := fake(home, panes{"s": 10}, procs)
	a, err := d.Check("s")
	if err != nil || a.Provider != &Codex || a.SessionID != id1 || !a.Resumable {
		t.Fatalf("%+v %v", a, err)
	}
	procs[20] = proc{args: "codex --resume-session-id=" + id2, children: []int{}}
	if a, _ = d.Check("s"); a.SessionID != id2 || a.Resumable {
		t.Fatalf("unknown transcript: %+v", a)
	}
	procs[20] = proc{args: "codex", children: []int{}}
	if a, _ = d.Check("s"); !a.Running || a.SessionID != nil {
		t.Fatalf("fresh codex: %+v", a)
	}
	// Claude is asked first.
	procs[20] = proc{args: "claude", children: []int{21}}
	if a, _ = d.Check("s"); a.Provider != &Claude {
		t.Fatalf("claude first: %+v", a)
	}
}
