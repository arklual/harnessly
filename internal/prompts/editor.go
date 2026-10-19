package prompts

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"harnessly/internal/machine"
	"harnessly/internal/tmux"
)

// Claude Code turns a long or multi-line bracketed paste into "[Pasted text #n]" and hands it to the model as pasted
// content, text the harness does not treat as the person's own words. Text Claude loads from its external editor
// (Ctrl+G) is the person's prompt. So Claude is launched with VISUAL pointing at a script that appends a hand-off file
// the runtime left for the pane to Claude's buffer; without one it opens the person's own editor.

// EditorConfig is where the hand-off lives and how a Claude started with the script is recognised.
type EditorConfig struct {
	// Dir holds the hand-off files, one per pane: <socket>-<pane number>.txt, or <pane number>.txt on UnkeyedSocket.
	Dir string
	// Script is the editor script's path.
	Script string
	// Markers are VISUAL=<script> entries of a Claude's environment that mean it runs with the script (the current
	// script and any older path of it).
	Markers []string
	// UnkeyedSocket is the tmux server whose panes are keyed by pane number alone (pane numbers repeat across servers).
	UnkeyedSocket string
	// HumanEditorVar is the variable the launch prefix keeps the person's own editor in.
	HumanEditorVar string
}

const (
	handoffWait      = 5000 * time.Millisecond
	handoffPoll      = 50 * time.Millisecond
	shownWait        = 400 * time.Millisecond
	shownPoll        = 30 * time.Millisecond
	shownTailChars   = 12
	secondPasteEnter = 600 * time.Millisecond
)

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// EditorScript is the shell script Claude runs for Ctrl+G: it appends the pane's hand-off file to the buffer file ($1)
// once, taking it with mv so a second run cannot take it again; otherwise it runs the person's editor (vi when none).
func EditorScript(cfg EditorConfig) string {
	v := cfg.HumanEditorVar
	return "#!/bin/sh\n" +
		"# Prompt editor: Claude Code runs it for Ctrl+G. A hand-off file the runtime left for this pane is appended to the\n" +
		"# prompt, so the text arrives as typed and not as a paste; without one the person's editor opens. Pane ids repeat\n" +
		"# across tmux servers, so a pane is keyed by its server too, except on " + cfg.UnkeyedSocket + ".\n" +
		"dir=" + shellQuote(cfg.Dir) + "\n" +
		"key=${TMUX_PANE#%}\n" +
		"socket=${TMUX%%,*}\n" +
		"socket=${socket##*/}\n" +
		`[ -n "$key" ] && [ -n "$socket" ] && [ "$socket" != ` + shellQuote(cfg.UnkeyedSocket) + ` ] && key="$socket-$key"` + "\n" +
		`if [ -n "$key" ] && mv "$dir/$key.txt" "$dir/$key.$$.taking" 2>/dev/null; then` + "\n" +
		`  cat "$dir/$key.$$.taking" >> "$1"` + "\n" +
		`  rm -f "$dir/$key.$$.taking"` + "\n" +
		"  exit 0\n" +
		"fi\n" +
		"editor=${" + v + ":-vi}\n" +
		`[ "$editor" = "$0" ] && editor=vi` + "\n" +
		`exec sh -c "$editor \"\$@\"" sh "$@"` + "\n"
}

// WriteEditorScript writes the script (0700, as a temporary file renamed over it) and makes its folder and the hand-off
// folder (0700).
func WriteEditorScript(cfg EditorConfig) error {
	if err := os.MkdirAll(filepath.Dir(cfg.Script), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Dir, 0o700); err != nil {
		return err
	}
	tmp := cfg.Script + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, []byte(EditorScript(cfg)), 0o700); err != nil {
		return err
	}
	return os.Rename(tmp, cfg.Script)
}

// EditorEnvPrefix goes in front of a Claude command line. The pane's shell reads the person's editor before VISUAL is
// replaced: assignments in front of a command are expanded left to right.
func EditorEnvPrefix(cfg EditorConfig) string {
	return cfg.HumanEditorVar + `="${VISUAL:-$EDITOR}" VISUAL=` + shellQuote(cfg.Script) + ` EDITOR=` + shellQuote(cfg.Script)
}

// Procs reads the process table; the machine's by default.
type Procs struct {
	Children func(pid int) []int
	Args     func(pid int) (string, bool)
	Environ  func(pid int) ([]string, bool)
}

func machineProcs() Procs {
	return Procs{Children: machine.ChildPIDs, Args: machine.ProcessArgs, Environ: readEnviron}
}

func readEnviron(pid int) ([]string, bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, false
	}
	return strings.Split(string(raw), "\x00"), true
}

// editorClaude remembers, per session, the Claude found running with the script and its pane: while that process lives
// the next message skips the tmux lookups and only re-reads its environment.
type editorClaude struct {
	pid  int
	pane string
}

type editor struct {
	cfg   EditorConfig
	panes Panes
	procs Procs
	clock Clock
	enter *EnterClock
	goos  string
	warn  func(msg string, kv ...any)

	mu    sync.Mutex
	known map[string]*editorClaude
}

func (e *editor) marked(env []string) bool {
	for _, m := range e.cfg.Markers {
		if slices.Contains(env, m) {
			return true
		}
	}
	return false
}

// usesEditor says whether the Claude in this session was started with the script (Linux only: it reads /proc). The
// remembered Claude is re-checked by its environment; otherwise the pane's children and grandchildren are searched.
func (e *editor) usesEditor(session string) bool {
	if e.goos != "linux" || len(e.cfg.Markers) == 0 {
		return false
	}
	e.mu.Lock()
	known := e.known[session]
	e.mu.Unlock()
	if known != nil {
		if env, ok := e.procs.Environ(known.pid); ok && e.marked(env) {
			return true
		}
		e.forget(session)
	}
	panePID := e.panes.PanePID(session)
	if panePID <= 0 {
		return false
	}
	children := e.procs.Children(panePID)
	candidates := append([]int{}, children...)
	for _, c := range children {
		candidates = append(candidates, e.procs.Children(c)...)
	}
	for _, pid := range candidates {
		if args, ok := e.procs.Args(pid); !ok || !strings.Contains(args, "claude") {
			continue
		}
		if env, ok := e.procs.Environ(pid); ok && e.marked(env) {
			e.mu.Lock()
			e.known[session] = &editorClaude{pid: pid}
			e.mu.Unlock()
			return true
		}
	}
	return false
}

func (e *editor) forget(session string) {
	e.mu.Lock()
	delete(e.known, session)
	e.mu.Unlock()
}

// pane is the session's pane id, remembered with its Claude; copy mode is left at the same time.
func (e *editor) pane(session string) string {
	e.panes.ExitCopyMode(session)
	e.mu.Lock()
	known := e.known[session]
	e.mu.Unlock()
	if known != nil && known.pane != "" {
		return known.pane
	}
	id := e.panes.PaneID(session)
	if id != "" && known != nil {
		e.mu.Lock()
		known.pane = id
		e.mu.Unlock()
	}
	return id
}

// HandoffFile is the hand-off file of a pane (%12) on a tmux server.
func HandoffFile(cfg EditorConfig, socket, paneID string) string {
	name := strings.TrimPrefix(paneID, "%") + ".txt"
	if socket != cfg.UnkeyedSocket {
		name = socket + "-" + name
	}
	return filepath.Join(cfg.Dir, name)
}

// WaitGone is true as soon as file is gone — on its folder's change event, with a poll for a missed one — and false
// after timeout.
func WaitGone(file string, timeout time.Duration) bool {
	gone := func() bool { _, err := os.Stat(file); return os.IsNotExist(err) }
	if gone() {
		return true
	}
	var events chan fsnotify.Event
	if w, err := fsnotify.NewWatcher(); err == nil {
		defer w.Close()
		if w.Add(filepath.Dir(file)) == nil {
			events = w.Events
		}
	}
	poll := time.NewTicker(handoffPoll)
	defer poll.Stop()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case <-events:
		case <-poll.C:
		case <-deadline.C:
			return gone()
		}
		if gone() {
			return true
		}
	}
}

// ShownTail is the end of text as it shows on screen with wrapping and indents gone: what says the box has it.
func ShownTail(text string) string {
	return lastUnits(noSpace(tmux.StripControlChars(text)), shownTailChars)
}

// ScreenShows says whether the bottom 40 lines of a screen hold tail, whitespace ignored.
func ScreenShows(screen, tail string) bool {
	if tail == "" {
		return false
	}
	lines := strings.Split(screen, "\n")
	return strings.Contains(noSpace(strings.Join(lines[max(0, len(lines)-40):], "")), tail)
}

func (e *editor) waitShown(session, text string) bool {
	tail := ShownTail(text)
	deadline := e.clock.Now().Add(shownWait)
	for e.clock.Now().Before(deadline) {
		if s, ok := e.panes.Capture(session); ok && ScreenShows(s, tail) {
			return true
		}
		e.clock.Sleep(shownPoll)
	}
	return false
}

// loadThroughEditor puts text into Claude's prompt box through Ctrl+G: the hand-off file is written (0600, control
// characters stripped), Ctrl+G pressed, and the file must be taken within 5 s; then up to 400 ms for Claude to show the
// text. True once taken, shown or not. Not taken in time, the file is withdrawn (a later Ctrl+G of the person must not
// get it) and the remembered Claude forgotten.
func (e *editor) loadThroughEditor(session, text string, steps *Steps) bool {
	pane := e.pane(session)
	if pane == "" || os.MkdirAll(e.cfg.Dir, 0o700) != nil {
		return false
	}
	file := HandoffFile(e.cfg, e.panes.SocketOf(session), pane)
	tmp := file + "." + strconv.Itoa(os.Getpid()) + ".tmp"
	if os.WriteFile(tmp, []byte(tmux.StripControlChars(text)), 0o600) != nil || os.Rename(tmp, file) != nil {
		_ = os.Remove(tmp)
		return false
	}
	steps.Mark("handoff")
	if e.panes.SendRawKeys(session, "C-g") != nil {
		_ = os.Remove(file)
		return false
	}
	steps.Mark("ctrl_g")
	if WaitGone(file, handoffWait) {
		steps.Mark("taken")
		if e.waitShown(session, text) {
			steps.Mark("shown")
		} else {
			steps.Mark("settled")
		}
		return true
	}
	_ = os.Remove(file)
	e.forget(session)
	e.warn("Claude did not open the prompt editor in time, falling back to a paste", "session", session)
	return false
}

// typePromptText puts text into the prompt box without submitting it: through the editor where Claude has it, else as a
// bracketed paste. It answers "editor" or "paste".
func (e *editor) typePromptText(session, text string, steps *Steps) (string, error) {
	if NeedsEditor(text) && e.usesEditor(session) {
		steps.Mark("editor_check")
		if e.loadThroughEditor(session, text, steps) {
			return "editor", nil
		}
	}
	if err := e.panes.SendBracketedPasteOnly(session, text); err != nil {
		return "", err
	}
	steps.Mark("pasted")
	return "paste", nil
}

// submitPromptText types text and submits it. Text from the editor is in the box once Claude shows it, so Enter follows
// at once; a paste waits 250 ms and gets a second Enter 600 ms later (Claude Code may ask to confirm a long one).
func (e *editor) submitPromptText(session, text string, steps *Steps) (string, error) {
	via, err := e.typePromptText(session, text, steps)
	if err != nil {
		return "", err
	}
	if via == "paste" {
		e.clock.Sleep(pasteSettle)
	}
	if err := e.panes.SendRawKeys(session, "Enter"); err != nil {
		return "", err
	}
	e.enter.Note(session)
	steps.Mark("enter")
	if via == "paste" {
		e.clock.Sleep(secondPasteEnter)
		if err := e.panes.SendRawKeys(session, "Enter"); err != nil {
			return "", err
		}
	}
	return via, nil
}

func newEditor(cfg EditorConfig, panes Panes, procs Procs, clock Clock, enter *EnterClock, warn func(string, ...any)) *editor {
	return &editor{cfg: cfg, panes: panes, procs: procs, clock: clock, enter: enter, goos: runtime.GOOS, warn: warn, known: map[string]*editorClaude{}}
}
