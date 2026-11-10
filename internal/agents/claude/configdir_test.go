package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/sessions"
)

// HRN-674: on a Coder workspace the person's shell sets CLAUDE_CONFIG_DIR and ~/.claude does not exist. The pane's
// Claude keeps its pid file and its transcript under that folder: the detector must find both there, or the chat
// waits for its first answer forever.
func TestDetectFollowsClaudeConfigDirOfTheProcess(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	cfg := filepath.Join(root, "cfg", "claude")
	cwd := "/home/coder/workspace/app"
	const pid, id = 4242, "5d2f3c1a-0b6e-4c8e-9a51-2f6d8e1b7c90"
	write := func(p, s string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(cfg, "sessions", strconv.Itoa(pid)+".json"), `{"pid":`+strconv.Itoa(pid)+`,"sessionId":"`+id+`","cwd":"`+cwd+`"}`)
	transcript := filepath.Join(cfg, "projects", sessions.ProjectName(cwd), id+".jsonl")
	write(transcript, "{}\n")

	f := fakeProcs{
		10:  {args: "bash", children: []int{pid}, env: []string{"CLAUDE_CONFIG_DIR=" + cfg}},
		pid: {args: "/usr/local/bin/claude", env: []string{"HOME=" + home, "CLAUDE_CONFIG_DIR=" + cfg}},
	}
	d := &Detector{Dir: filepath.Join(home, ".claude"), Procs: f.procs(), Match: agents.MatchArgv}
	info, err := d.Detect([]int{pid})
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != agents.Running || info.SessionID != id || info.JSONLPath != transcript || info.Dir != cfg {
		t.Fatalf("got %+v, want the session under %s", info, cfg)
	}
	// The folder is remembered: a lookup by id finds the transcript after the Claude is gone.
	gone := &Detector{Dir: filepath.Join(home, ".claude"), Procs: fakeProcs{}.procs(), Match: agents.MatchArgv}
	if p := gone.FindTranscript(id); p != transcript {
		t.Fatalf("FindTranscript = %q, want %q", p, transcript)
	}
	if p := gone.TranscriptIn(cwd, id); p != transcript {
		t.Fatalf("TranscriptIn = %q, want %q", p, transcript)
	}

	// Without the variable the folder under the home is still the one.
	plain := fakeProcs{pid: {args: "claude", env: []string{"HOME=" + home}}}
	if dir := (&Detector{Dir: filepath.Join(home, ".claude"), Procs: plain.procs(), Match: agents.MatchArgv}).DirOf(pid); dir != filepath.Join(home, ".claude") {
		t.Fatalf("DirOf without the variable = %q", dir)
	}
}
