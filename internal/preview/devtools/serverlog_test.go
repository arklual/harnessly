package devtools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/tmux"
)

// A dev server an agent started in the background writes to a file: the log is that file's tail, found from the
// listener's process up its tree; one in a terminal pane is that pane's text.
func TestServerLogSources(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	dir := t.TempDir()
	logFile := filepath.Join(dir, "dev.log")
	_ = os.WriteFile(logFile, []byte("starting\nERROR in ./src/a.ts\n    at f (a.ts:1:2)\nready in 30ms\n"), 0o600)
	out, _ := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND, 0)
	cmd := exec.Command("sleep", "30")
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	store := NewStore()
	store.Touch("https://ai-alice-1.example.org", Target{Kind: "port", Port: 5173})
	store.Touch("https://ai-alice-2.example.org", Target{Kind: "port", Port: 4200})
	store.Touch("https://ai-alice-3.example.org", Target{Kind: "url", Origin: "https://site.example.org"})
	pid := cmd.Process.Pid
	logs := &ServerLogs{Store: store, Tmux: tmux.New(tmux.Names{Socket: "hs-test-serverlog", Prefix: "hs-"}),
		Listener: func(port int) (*Listener, error) {
			if port == 5173 {
				return &Listener{Port: port, PID: pid, HasPID: true, Command: "sleep"}, nil
			}
			return &Listener{Port: port}, nil
		},
		Panes: func() (map[string]tmux.PaneInfo, bool) { return map[string]tmux.PaneInfo{}, true },
	}
	time.Sleep(50 * time.Millisecond)
	got, err := logs.Get("https://ai-alice-1.example.org", false, 0)
	if err != nil || got.Source == nil || got.Source.Kind != "file" || got.Source.Path != logFile || len(got.Lines) != 2 {
		t.Fatalf("file source: %+v %v", got, err)
	}
	if got.Lines[0].Text != "ERROR in ./src/a.ts\n    at f (a.ts:1:2)" || got.Lines[1].Text != "ready in 30ms" {
		t.Errorf("problem lines: %+v", got.Lines)
	}
	if all, _ := logs.Get("https://ai-alice-1.example.org", true, 2); len(all.Lines) != 2 || all.Lines[1].Text != "ready in 30ms" {
		t.Errorf("all lines: %+v", all.Lines)
	}
	if other, _ := logs.Get("https://ai-alice-2.example.org", false, 0); other.Reason == nil || *other.Reason != "Порт 4200 занят процессом другого пользователя" {
		t.Errorf("another user's port: %+v", other)
	}
	if site, _ := logs.Get("https://ai-alice-3.example.org", false, 0); site.Reason == nil || *site.Reason != "Внешний сайт: терминала нет" {
		t.Errorf("external site: %+v", site)
	}
	logs.Panes = func() (map[string]tmux.PaneInfo, bool) {
		return map[string]tmux.PaneInfo{"hs-ws-1-pane-2-tab-3": {PID: pid}}, true
	}
	logs.Capture = func(session string, lines int) (string, bool) {
		return "warning: x\n", session == "hs-ws-1-pane-2-tab-3" && lines == 5000
	}
	logs.cache = nil
	pane, _ := logs.Get("https://ai-alice-1.example.org", false, 0)
	if pane.Source == nil || pane.Source.Kind != "tmux" || pane.Source.Pane != "pane-2" || len(pane.Lines) != 1 || pane.Lines[0].Level != "warn" {
		t.Errorf("pane source: %+v", pane)
	}
}
