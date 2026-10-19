package machine

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCleanCommandLine(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/vim notes.md":                     "vim notes.md",
		"/usr/bin/node /opt/app/dist/server.js --x": "server --x",
		"python3 /home/u/tool.py a":                 "tool.py a",
		"node":                                      "node",
		"/usr/local/bin/bun run.mjs":                "run",
		"":                                          "",
	} {
		if got := cleanCommandLine(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestPeers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("200.json", `{"name":"b","pid":200,"tmux":"/tmp/x,1,0:pt-ws-a-pane-b-tab-c9_x:0.0","updatedAt":5}`)
	write("100.json", `{"name":"a","pid":100,"sessionId":"s1","status":"busy"}`)
	write("300.json", `{"pid":300}`)
	write("notes.txt", `{"name":"n","pid":1}`)
	write("400.json", `broken`)
	peers := Peers(home)
	if len(peers) != 2 || peers[0].Name != "a" || peers[1].Name != "b" {
		t.Fatalf("%+v", peers)
	}
	if peers[0].SessionID == nil || *peers[0].SessionID != "s1" || peers[0].TabID != nil || peers[0].UpdatedAt != nil {
		t.Fatalf("first %+v", peers[0])
	}
	if peers[1].TabID == nil || *peers[1].TabID != "tab-c9_x" || *peers[1].UpdatedAt != 5 {
		t.Fatalf("second %+v", peers[1])
	}
	if got := Peers(t.TempDir()); got == nil || len(got) != 0 {
		t.Fatal("no sessions folder should list []")
	}
}

func TestNewestChild(t *testing.T) {
	sh := exec.Command("sh", "-c", "sleep 30 & sleep 0.2; sleep 31 & wait")
	if err := sh.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sh.Process.Kill(); _ = sh.Wait() }()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if child := newestChild(sh.Process.Pid); child > 0 {
			args, _ := processArgs(child)
			if args == "sleep 31" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("the newest child of %s was not found", strconv.Itoa(sh.Process.Pid))
}
