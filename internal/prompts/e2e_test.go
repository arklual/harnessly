package prompts

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/tmux"
)

// The end-to-end tests run a fake agent TUI in a pane of a private tmux server (hs-test-*): the test binary itself,
// started again with fakeTUIEnv set. It draws a Claude-like prompt box, takes typed and pasted text, logs what was
// submitted, and in busy mode queues messages the way Claude Code does (hint, placeholder, transcript records) and lets
// ctrl+x ctrl+s deliver them.

const (
	fakeTUIEnv        = "PROMPTS_FAKE_TUI"
	fakeTUILog        = "PROMPTS_FAKE_LOG"
	fakeTUITranscript = "PROMPTS_FAKE_TRANSCRIPT"
	fakeTUIBusy       = "PROMPTS_FAKE_BUSY"
	fakeTUIQueued     = "PROMPTS_FAKE_QUEUED"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeTUIEnv) != "" {
		runFakeTUI()
		return
	}
	os.Exit(m.Run())
}

type fakeTUI struct {
	logFile, transcript string
	busy                bool
	history, queue      []string
	draft               []byte
}

func (f *fakeTUI) logLine(s string) {
	file, err := os.OpenFile(f.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintln(file, s)
}

func (f *fakeTUI) record(v any) {
	if f.transcript == "" {
		return
	}
	raw, _ := json.Marshal(v)
	file, err := os.OpenFile(f.transcript, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	file.Write(append(raw, '\n'))
}

func isoNow() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func (f *fakeTUI) draw() {
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")
	lines := []string{"● ready"}
	for _, h := range f.history {
		lines = append(lines, "> "+strings.ReplaceAll(h, "\n", " / "))
	}
	if f.busy {
		for _, q := range f.queue {
			lines = append(lines, "  > "+q)
		}
		if len(f.queue) > 0 {
			lines = append(lines, "  ctrl+x ctrl+s to send now")
		}
		lines = append(lines, "", "✽ Working… (3s · esc to interrupt)")
	}
	lines = append(lines, "", strings.Repeat("─", 60))
	switch {
	case len(f.draft) > 0:
		lines = append(lines, "❯ "+strings.ReplaceAll(string(f.draft), "\n", " "))
	case f.busy && len(f.queue) > 0:
		lines = append(lines, "❯ \x1b[2mPress up to edit queued messages\x1b[0m")
	default:
		lines = append(lines, "❯ \x1b[2mTry \"fix the tests\"\x1b[0m")
	}
	lines = append(lines, strings.Repeat("─", 60), "  ? for shortcuts")
	b.WriteString(strings.Join(lines, "\r\n"))
	os.Stdout.WriteString(b.String())
}

func (f *fakeTUI) submit() {
	text := string(f.draft)
	f.draft = nil
	if strings.TrimSpace(text) == "" {
		return
	}
	if f.busy {
		f.queue = append(f.queue, text)
		f.record(map[string]any{"type": "queue-operation", "operation": "enqueue", "content": text, "timestamp": isoNow()})
		f.logLine("queued:" + strconv.Quote(text))
		return
	}
	f.history = append(f.history, text)
	f.logLine("submit:" + strconv.Quote(text))
}

// sendNow is Claude's ctrl+x ctrl+s: the running call goes to the background and every queued message is delivered.
func (f *fakeTUI) sendNow() {
	f.logLine("chord")
	if !f.busy || len(f.queue) == 0 {
		return
	}
	f.record(map[string]any{"type": "user", "timestamp": isoNow(), "message": map[string]any{"role": "user", "content": []any{
		map[string]any{"type": "tool_result", "tool_use_id": "toolu_1", "content": "Command was moved to the background; it was not interrupted"}}}})
	for _, q := range f.queue {
		f.record(map[string]any{"type": "queue-operation", "operation": "remove", "content": q, "timestamp": isoNow()})
	}
	for _, q := range f.queue {
		f.record(map[string]any{"type": "user", "timestamp": isoNow(), "message": map[string]any{"role": "user", "content": q}})
		f.history = append(f.history, q)
		f.logLine("delivered:" + strconv.Quote(q))
	}
	f.queue = nil
}

func runFakeTUI() {
	f := &fakeTUI{logFile: os.Getenv(fakeTUILog), transcript: os.Getenv(fakeTUITranscript), busy: os.Getenv(fakeTUIBusy) != ""}
	if f.busy {
		f.record(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "tool_use", "id": "toolu_1", "name": "Bash", "input": map[string]any{"command": "sleep 600"}}}}})
		if q := os.Getenv(fakeTUIQueued); q != "" {
			f.draft = []byte(q)
			f.submit()
		}
	}
	f.draw()
	f.logLine("ready")
	in := bufio.NewReader(os.Stdin)
	pasting, chord := false, false
	for {
		c, err := in.ReadByte()
		if err != nil {
			return
		}
		switch {
		case c == 0x1b:
			// A bracketed paste starts with ESC [200~ and ends with ESC [201~; other escapes are ignored.
			seq := []byte{}
			for len(seq) < 5 {
				n, err := in.ReadByte()
				if err != nil {
					return
				}
				seq = append(seq, n)
				if n == '~' || (len(seq) == 1 && n != '[') {
					break
				}
			}
			switch string(seq) {
			case "[200~":
				pasting = true
			case "[201~":
				pasting = false
			}
		case pasting:
			if c == '\r' {
				c = '\n'
			}
			f.draft = append(f.draft, c)
		case c == 0x18:
			chord = true
			continue
		case c == 0x13 && chord:
			f.sendNow()
		case c == '\r' || c == '\n':
			f.submit()
		case c >= 0x20 || c >= 0x80:
			f.draft = append(f.draft, c)
		}
		chord = false
		f.draw()
	}
}

// e2eRig is a private tmux server with the fake TUI in session hs-e2e.
type e2eRig struct {
	t                    *testing.T
	tm                   *tmux.Tmux
	socket, log, transcr string
}

func newE2E(t *testing.T, busy bool, queued string) *e2eRig {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r := &e2eRig{t: t, socket: fmt.Sprintf("hs-test-%d", rand.IntN(1_000_000_000)), log: filepath.Join(dir, "tui.log"), transcr: filepath.Join(dir, "session.jsonl")}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "-L", r.socket, "kill-server").Run()
		tmpdir := os.Getenv("TMUX_TMPDIR")
		if tmpdir == "" {
			tmpdir = "/tmp"
		}
		_ = os.Remove(filepath.Join(tmpdir, "tmux-"+strconv.Itoa(os.Getuid()), r.socket))
	})
	env := []string{fakeTUIEnv + "=1", fakeTUILog + "=" + r.log, fakeTUITranscript + "=" + r.transcr}
	if busy {
		env = append(env, fakeTUIBusy+"=1")
	}
	if queued != "" {
		env = append(env, fakeTUIQueued+"="+queued)
	}
	cmd := "stty raw -echo; exec env"
	for _, e := range env {
		cmd += " " + shellQuote(e)
	}
	cmd += " " + shellQuote(self) + " -test.run=^$"
	out, err := exec.Command("tmux", "-f", "/dev/null", "-L", r.socket, "new-session", "-d", "-s", "hs-e2e", "-x", "120", "-y", "40", cmd).CombinedOutput()
	if err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	r.tm = tmux.New(tmux.Names{Socket: r.socket, Prefix: "hs-"})
	r.waitLog("ready", 10*time.Second)
	return r
}

func (r *e2eRig) logLines() []string {
	raw, _ := os.ReadFile(r.log)
	return strings.Split(strings.TrimSpace(string(raw)), "\n")
}

func (r *e2eRig) waitLog(want string, within time.Duration) {
	r.t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, l := range r.logLines() {
			if l == want {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	r.t.Fatalf("the TUI never logged %q; log %q", want, r.logLines())
}

func (r *e2eRig) service(tab Tab) (*Service, *fakeTabs) {
	tab.ID, tab.Session, tab.Provider, tab.TranscriptPath = "tab-1", "hs-e2e", ProviderClaude, r.transcr
	tabs := newFakeTabs(tab)
	svc := New(Config{Panes: TmuxPanes{Tmux: r.tm}, Tabs: tabs, Agents: &fakeAgents{running: true}, Events: &fakeEvents{}, Broadcast: &fakeBroadcast{}})
	r.t.Cleanup(svc.Stop)
	return svc, tabs
}

func TestE2EQueueDelivery(t *testing.T) {
	r := newE2E(t, false, "")
	svc, _ := r.service(Tab{CliState: StateIdle})

	// A short line through the queue: typed with its Enter.
	res := svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "first message"}))
	if res.Status != 200 {
		t.Fatalf("add: %+v", res)
	}
	r.waitLog(`submit:"first message"`, 10*time.Second)
	if len(svc.Queue().List("tab-1")) != 0 {
		t.Fatal("the message is still queued")
	}

	// A multi-line text from the composer: pasted, submitted, and the second Enter lands on an empty box.
	res = svc.PromptText(body(map[string]any{"tabId": "tab-1", "text": "line one\nline two", "messageId": "m-1"}))
	if res.Status != 200 || asJSON(t, res)["via"] != "paste" {
		t.Fatalf("prompt-text: %+v", res)
	}
	r.waitLog(`submit:"line one\nline two"`, 10*time.Second)

	// A draft in the box blocks the queue: nothing is typed over it.
	if err := r.tm.SendLiteral("hs-e2e", "my draft"); err != nil {
		t.Fatal(err)
	}
	waitScreen(t, r.tm, "❯ my draft")
	item, _ := svc.Queue().Add("tab-1", "must wait", nil)
	if got := svc.Pump("tab-1", true, svc.deliveryDriver(Tab{Session: "hs-e2e", Provider: ProviderClaude})); got != PumpBlocked {
		t.Fatalf("pump over a draft: %s", got)
	}
	if q := svc.Queue().List("tab-1"); len(q) != 1 || q[0].ID != item.ID {
		t.Fatalf("queue %+v", q)
	}
	for _, l := range r.logLines() {
		if strings.Contains(l, "must wait") {
			t.Fatalf("typed over the draft: %v", r.logLines())
		}
	}
}

func TestE2ESendNow(t *testing.T) {
	const queued = "queued while working"
	r := newE2E(t, true, queued)
	svc, _ := r.service(Tab{CliState: StateBusy})
	waitScreen(t, r.tm, "ctrl+x ctrl+s to send now")
	res := svc.SendNowRoute(body(map[string]any{"tabId": "tab-1", "text": queued}))
	if got := asJSON(t, res); res.Status != 200 || got["outcome"] != "sent" {
		t.Fatalf("send now: %d %v; log %q", res.Status, got, r.logLines())
	}
	r.waitLog(`delivered:"queued while working"`, 5*time.Second)
}

func TestE2ESteerHandsOver(t *testing.T) {
	r := newE2E(t, true, "")
	svc, _ := r.service(Tab{CliState: StateBusy})
	res := svc.PromptQueue(body(map[string]any{"tabId": "tab-1", "action": "add", "text": "steer this in"}))
	if got := asJSON(t, res); res.Status != 200 || got["delivery"] != "steered" {
		t.Fatalf("add: %d %v; log %q", res.Status, got, r.logLines())
	}
	r.waitLog(`queued:"steer this in"`, 5*time.Second)
	r.waitLog(`delivered:"steer this in"`, 5*time.Second)
	chords := 0
	for _, l := range r.logLines() {
		if l == "chord" {
			chords++
		}
	}
	if chords != 1 {
		t.Errorf("%d chords", chords)
	}
}

func waitScreen(t *testing.T, tm *tmux.Tmux, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if s, ok := tm.Capture("hs-e2e"); ok && strings.Contains(s, want) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	s, _ := tm.Capture("hs-e2e")
	t.Fatalf("the screen never showed %q:\n%s", want, s)
}
