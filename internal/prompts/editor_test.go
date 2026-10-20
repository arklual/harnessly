package prompts

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/machine"
)

func TestShownTailAndScreenShows(t *testing.T) {
	text := "Сделай, чтобы сообщение доходило сразу,\nа не через три секунды"
	tail := ShownTail(text)
	box := strings.Join([]string{"● Готово.", strings.Repeat("─", 40), "❯ Сделай, чтобы сообщение доходило сразу,", "  а не через три сек", "  унды", strings.Repeat("─", 40)}, "\n")
	if !ScreenShows(box, tail) {
		t.Errorf("the wrapped text is not found (tail %q)", tail)
	}
	if ScreenShows("❯ \n"+strings.Repeat("─", 40), tail) || ScreenShows(box, "") {
		t.Error("found in an empty box, or an empty tail")
	}
}

func TestWaitGone(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "7.txt")
	_ = os.WriteFile(file, []byte("текст"), 0o600)
	start := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.Rename(file, filepath.Join(dir, "7.1.taking"))
	}()
	if !WaitGone(file, 2*time.Second) {
		t.Fatal("the take was not seen")
	}
	if time.Since(start) > time.Second {
		t.Errorf("seen after %v", time.Since(start))
	}
	_ = os.WriteFile(file, []byte("текст"), 0o600)
	if WaitGone(file, 120*time.Millisecond) {
		t.Fatal("a file that stays reads as gone")
	}
}

type scriptRig struct {
	dir, script, handoff string
	cfg                  EditorConfig
}

func newScriptRig(t *testing.T) *scriptRig {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	r := &scriptRig{dir: dir, script: filepath.Join(dir, "bin", "prompt-editor"), handoff: filepath.Join(dir, "handoff")}
	r.cfg = EditorConfig{Dir: r.handoff, Script: r.script, UnkeyedSocket: "legacy", HumanEditorVar: "HS_HUMAN_EDITOR",
		Markers: []string{"VISUAL=" + r.script}}
	if err := WriteEditorScript(r.cfg); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *scriptRig) run(t *testing.T, buffer string, env ...string) {
	t.Helper()
	cmd := exec.Command(r.script, buffer)
	cmd.Env = append(os.Environ(), append([]string{"TMUX=", "TMUX_PANE=", "HS_HUMAN_EDITOR="}, env...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script: %v %s", err, out)
	}
}

func (r *scriptRig) left(t *testing.T) []string {
	entries, _ := os.ReadDir(r.handoff)
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func read(t *testing.T, file string) string {
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEditorScriptAppendsOnce(t *testing.T) {
	r := newScriptRig(t)
	if st, _ := os.Stat(r.script); st.Mode().Perm() != 0o700 {
		t.Errorf("script mode %v", st.Mode())
	}
	buffer := filepath.Join(r.dir, "prompt.md")
	_ = os.WriteFile(buffer, []byte("[Image #1]"), 0o600)
	_ = os.WriteFile(filepath.Join(r.handoff, "7.txt"), []byte(" первая строка\nвторая"), 0o600)
	r.run(t, buffer, "TMUX_PANE=%7")
	if got := read(t, buffer); got != "[Image #1] первая строка\nвторая" {
		t.Fatalf("buffer %q", got)
	}
	if left := r.left(t); len(left) != 0 {
		t.Errorf("left %v", left)
	}
}

func TestEditorScriptKeysPanesBySocket(t *testing.T) {
	r := newScriptRig(t)
	buffer := filepath.Join(r.dir, "prompt.md")
	_ = os.WriteFile(buffer, nil, 0o600)
	_ = os.WriteFile(filepath.Join(r.handoff, "7.txt"), []byte("вкладка на старом"), 0o600)
	_ = os.WriteFile(filepath.Join(r.handoff, "current-7.txt"), []byte("вкладка на новом"), 0o600)
	r.run(t, buffer, "TMUX=/tmp/tmux-1000/current,123,0", "TMUX_PANE=%7")
	if got := read(t, buffer); got != "вкладка на новом" {
		t.Fatalf("current server: %q", got)
	}
	r.run(t, buffer, "TMUX=/tmp/tmux-1000/legacy,99,0", "TMUX_PANE=%7")
	if got := read(t, buffer); got != "вкладка на новомвкладка на старом" {
		t.Fatalf("legacy server: %q", got)
	}
	if left := r.left(t); len(left) != 0 {
		t.Errorf("left %v", left)
	}
	if HandoffFile(r.cfg, "legacy", "%7") != filepath.Join(r.handoff, "7.txt") || HandoffFile(r.cfg, "current", "%7") != filepath.Join(r.handoff, "current-7.txt") {
		t.Error("hand-off names differ from the script's")
	}
}

func TestEditorScriptOpensTheHumanEditor(t *testing.T) {
	r := newScriptRig(t)
	buffer := filepath.Join(r.dir, "prompt.md")
	_ = os.WriteFile(buffer, nil, 0o600)
	_ = os.WriteFile(filepath.Join(r.handoff, "8.txt"), []byte("чужая вкладка"), 0o600)
	human := filepath.Join(r.dir, "human-editor")
	_ = os.WriteFile(human, []byte("#!/bin/sh\necho \"редактор человека\" > \"$1\"\n"), 0o700)
	r.run(t, buffer, "TMUX_PANE=%7", "HS_HUMAN_EDITOR="+human)
	if got := read(t, buffer); got != "редактор человека\n" {
		t.Fatalf("buffer %q", got)
	}
	if left := r.left(t); !reflect.DeepEqual(left, []string{"8.txt"}) {
		t.Errorf("left %v", left)
	}
}

func TestEditorEnvPrefixKeepsTheHumanEditor(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	r := newScriptRig(t)
	cmd := exec.Command("bash", "-c", EditorEnvPrefix(r.cfg)+" env")
	cmd.Env = append(os.Environ(), "VISUAL=code --wait")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"HS_HUMAN_EDITOR=code --wait", "VISUAL=" + r.script, "EDITOR=" + r.script} {
		if !strings.Contains(string(out), want+"\n") {
			t.Errorf("missing %q", want)
		}
	}
}

// A shell standing for the pane, with a process named claude under it started with or without the script as VISUAL.
func paneWithClaude(t *testing.T, visual string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("bash", "-c", `bash -c "exec -a claude sleep 30" & wait`)
	cmd.Env = append(os.Environ(), "VISUAL="+visual)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopPane(cmd) })
	// Wait for the exec: until then the child is a copy of the shell, mid-fork.
	execed := func() bool {
		for _, pid := range machine.ChildPIDs(cmd.Process.Pid) {
			if args, ok := machine.ProcessArgs(pid); ok && strings.HasPrefix(args, "claude") {
				return true
			}
		}
		return false
	}
	for i := 0; i < 250 && !execed(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	return cmd
}

func stopPane(cmd *exec.Cmd) {
	for _, pid := range machine.ChildPIDs(cmd.Process.Pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// countingPanes counts pane pid lookups.
type countingPanes struct {
	*fakePanes
	calls int
}

func (p *countingPanes) PanePID(string) int { p.calls++; return p.panePID }

func editorOn(t *testing.T, script string) (*editor, *countingPanes) {
	t.Helper()
	panes := &countingPanes{fakePanes: newFakePanes(testSession, "")}
	cfg := EditorConfig{Dir: t.TempDir(), Script: script, Markers: []string{"VISUAL=" + script, "VISUAL=/old/place/prompt-editor"}}
	return newEditor(cfg, panes, machineProcs(), newFakeClock(), NewEnterClock(newFakeClock()), func(string, ...any) {}), panes
}

func TestEditorClaudeLookup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	script := filepath.Join(t.TempDir(), "prompt-editor")

	t.Run("looked up once, then checked by its own environment", func(t *testing.T) {
		e, panes := editorOn(t, script)
		panes.panePID = paneWithClaude(t, script).Process.Pid
		for i := 0; i < 3; i++ {
			if !e.usesEditor("hs-a") {
				t.Fatal("not found")
			}
		}
		if panes.calls != 1 {
			t.Errorf("%d pane lookups", panes.calls)
		}
	})

	t.Run("looked up again once that Claude is gone", func(t *testing.T) {
		e, panes := editorOn(t, script)
		first := paneWithClaude(t, script)
		panes.panePID = first.Process.Pid
		if !e.usesEditor("hs-b") {
			t.Fatal("not found")
		}
		stopPane(first)
		time.Sleep(100 * time.Millisecond)
		panes.panePID = paneWithClaude(t, "").Process.Pid
		if e.usesEditor("hs-b") || panes.calls != 2 {
			t.Errorf("still used, %d lookups", panes.calls)
		}
	})

	t.Run("not remembered without the script", func(t *testing.T) {
		e, panes := editorOn(t, script)
		panes.panePID = paneWithClaude(t, "").Process.Pid
		if e.usesEditor("hs-c") || e.usesEditor("hs-c") || panes.calls != 2 {
			t.Errorf("%d lookups", panes.calls)
		}
	})

	t.Run("an older path of the script counts", func(t *testing.T) {
		e, panes := editorOn(t, script)
		panes.panePID = paneWithClaude(t, "/old/place/prompt-editor").Process.Pid
		if !e.usesEditor("hs-d") {
			t.Error("the older marker is not recognised")
		}
	})
}

// The hand-off itself: the editor takes the file (a stand-in for Claude's Ctrl+G), the text shows, Enter follows.
func TestLoadThroughEditor(t *testing.T) {
	dir := t.TempDir()
	panes := newFakePanes(testSession, "❯ ")
	panes.socket = "current"
	cfg := EditorConfig{Dir: dir, UnkeyedSocket: "legacy"}
	e := newEditor(cfg, panes, Procs{}, newFakeClock(), NewEnterClock(newFakeClock()), func(string, ...any) {})
	text := "первая строка\nвторая строка\x1b[201~"
	panes.onSend = func(call string) {
		if call != "keys:C-g" {
			return
		}
		file := filepath.Join(dir, "current-7.txt")
		got, _ := os.ReadFile(file)
		if string(got) != "первая строка\nвторая строка␛[201~" {
			t.Errorf("hand-off %q", got)
		}
		_ = os.Rename(file, file+".taking")
		panes.setScreen("❯ первая строка\n  вторая строка␛[201~")
	}
	steps := NewSteps(newFakeClock())
	if via, err := e.submitPromptText(testSession, text, steps); err != nil || via != "paste" {
		// Without a Claude using the script the text is pasted.
		t.Fatalf("via %s %v", via, err)
	}
	if !e.loadThroughEditor(testSession, text, steps) {
		t.Fatal("not taken")
	}
	names := steps.Names()
	if !slices.Contains(names, "handoff") || !slices.Contains(names, "ctrl_g") || !slices.Contains(names, "taken") || !slices.Contains(names, "shown") {
		t.Errorf("steps %v", names)
	}

	// Not taken in 5 s: the file is withdrawn, so a later Ctrl+G of the person does not get it.
	if testing.Short() {
		return
	}
	panes.onSend = nil
	if e.loadThroughEditor(testSession, "x\ny", nil) {
		t.Fatal("taken by nobody")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".taking") {
		t.Errorf("left %v", entries)
	}
}
