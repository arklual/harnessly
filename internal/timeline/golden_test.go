package timeline

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/agents"
	"harnessly/internal/agents/claude"
	"harnessly/internal/agents/codex"
	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
	"harnessly/internal/proc"
	"harnessly/internal/transcript"
)

// The golden test plays testdata/scenario.json against this server and compares what every socket received, and what
// the layout store and the keeper were told, with what the previous runtime's own timeline did on the same scenario
// (testdata/golden.json, written by testdata/gen/run.mjs). A wait step sleeps as the generator did, then holds until
// what its "until" names has arrived, so a slow machine only makes the run longer.

type world struct {
	mu      sync.Mutex
	panes   map[string]*jsvalue.Object
	procs   map[int]*jsvalue.Object
	busy    map[string]bool
	recover map[string]*jsvalue.Object
	effects []any
}

func (w *world) record(call ...any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.effects = append(w.effects, call)
}

// pane reads a pane's fields under the lock: the scenario assigns to the same object while the server reads it.
func (w *world) pane(s string, read func(*jsvalue.Object)) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.panes[s]
	if p != nil {
		read(p)
	}
	return p != nil
}

func (w *world) proc(pid int) *jsvalue.Object {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.procs[pid]
}

type fakeTmux struct{ w *world }

func (t fakeTmux) str(s, k string) (v string) {
	t.w.pane(s, func(p *jsvalue.Object) { v = p.String(k) })
	return v
}
func (t fakeTmux) PanePID(s string) (pid int) {
	t.w.pane(s, func(p *jsvalue.Object) { n, _ := p.Number("pid"); pid = int(n) })
	return pid
}
func (t fakeTmux) PaneTitle(s string) string  { return t.str(s, "title") }
func (t fakeTmux) SessionCwd(s string) string { return t.str(s, "cwd") }
func (t fakeTmux) Capture(s string) (string, bool) {
	var screen string
	var ok bool
	t.w.pane(s, func(p *jsvalue.Object) { screen, ok = p.String("screen"), p.Has("screen") })
	return screen, ok
}
func (t fakeTmux) CheckTerminalProcess(s string) (bool, string) {
	cmd := t.str(s, "command")
	if cmd == "" {
		return false, "unknown"
	}
	switch cmd {
	case "bash", "zsh", "fish", "sh", "dash":
		return true, cmd
	}
	return false, cmd
}
func (t fakeTmux) SendKeys(s, line string) error { t.w.record("sendKeys", s, line); return nil }

var sessionNameRE = regexp.MustCompile(`^hs-([^-]+)-([^-]+)-([^-]+)$`)

func (t fakeTmux) ParseSessionName(s string) (string, string, string, bool) {
	m := sessionNameRE.FindStringSubmatch(s)
	if m == nil {
		return "", "", "", false
	}
	return m[1], m[2], m[3], true
}

type fakeKeeper struct{ w *world }

func (k fakeKeeper) NotifyLastUserMessage(s, m string)  { k.w.record("notifyLastUserMessage", s, m) }
func (k fakeKeeper) MarkAgentLaunch(tab string, _ bool) { k.w.record("markAgentLaunch", tab) }
func (k fakeKeeper) MarkAgentGone(tab string, _ bool)   { k.w.record("markAgentGone", tab) }
func (k fakeKeeper) IsSessionBusy(s string) bool {
	k.w.mu.Lock()
	defer k.w.mu.Unlock()
	return k.w.busy[s]
}

type fakeLayout struct{ w *world }

func (l fakeLayout) AgentState(s, p string, id, path, summary any) {
	l.w.record("agentState", s, p, id, path, summary)
}
func (l fakeLayout) AgentSessionID(s, p string, id any) { l.w.record("agentSessionId", s, p, id) }
func (l fakeLayout) AgentSummary(s, p string, sum any)  { l.w.record("agentSummary", s, p, sum) }
func (l fakeLayout) LastUserMessage(s, m string)        { l.w.record("lastUserMessage", s, m) }
func (l fakeLayout) Recover(s string) (string, string, bool) {
	l.w.record("recover", s)
	l.w.mu.Lock()
	defer l.w.mu.Unlock()
	if f := l.w.recover[s]; f != nil {
		return f.String("sessionId"), f.String("jsonlPath"), true
	}
	return "", "", false
}

func (w *world) procs4() proc.Procs {
	ints := func(v any) []int {
		var out []int
		list, _ := v.([]any)
		for _, x := range list {
			if f, ok := x.(float64); ok {
				out = append(out, int(f))
			}
		}
		return out
	}
	return proc.Procs{
		Children: func(pid int) []int {
			if p := w.proc(pid); p != nil {
				v, _ := p.Get("children")
				return ints(v)
			}
			return nil
		},
		Args: func(pid int) (string, bool) {
			if p := w.proc(pid); p != nil && p.Has("args") {
				return p.String("args"), true
			}
			return "", false
		},
		Cwd: func(pid int) string {
			if p := w.proc(pid); p != nil {
				return p.String("cwd")
			}
			return ""
		},
	}
}

var fixedNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func newTestServer(t *testing.T, w *world, home string) *Server {
	procs := w.procs4()
	claudeDet := &claude.Detector{Dir: filepath.Join(home, ".claude"), Procs: procs, Match: agents.MatchArgv, RemoveStale: true,
		Installed: func() bool { return true }}
	codexDet := &codex.Detector{Dir: filepath.Join(home, ".codex"), Procs: procs, Match: agents.MatchArgv,
		Installed: func() bool { return true }, Now: func() time.Time { return fixedNow }}
	projects := claudeDet.ProjectsDir()
	providers := map[string]*Provider{
		agents.ClaudePanel: {ID: agents.Claude, DisplayName: "Claude Code", Detect: claudeDet.Detect, Running: claudeDet.IsRunning,
			ParsePaneTitle: claude.ParsePaneTitle,
			SessionIDFromPath: func(p string) string {
				if p == "" {
					return ""
				}
				return strings.TrimSuffix(filepath.Base(p), ".jsonl")
			},
			ValidSessionID: agents.IsUUID,
			FindSession:    func(id string) string { return claude.FindTranscript(projects, id) },
			SessionsDir:    filepath.Join(claudeDet.Dir, "sessions")},
		agents.CodexPanel: {ID: agents.Codex, DisplayName: "Codex",
			Detect:  func(c []int) (agents.SessionInfo, error) { return codexDet.Detect(c, false), nil },
			Running: codexDet.IsRunning, ParsePaneTitle: codex.ParsePaneTitle, SessionIDFromPath: codex.SessionIDFromPath,
			ValidSessionID: agents.IsUUID,
			FindSession: func(id string) string {
				if m, ok := codex.FindSessionByID(codexDet.SessionsRoot(), id, fixedNow, codex.ScanDays); ok {
					return m.JSONLPath
				}
				return ""
			}},
	}
	timing := DefaultTiming
	timing.PIDPoll, timing.InstallCheck, timing.Heartbeat = time.Hour, time.Hour, time.Hour
	s := New(Deps{
		Tmux: fakeTmux{w}, Procs: procs, Alive: func(pid int) bool { return w.proc(pid) != nil }, Providers: providers,
		Keeper: func() Keeper { return fakeKeeper{w} }, Layout: fakeLayout{w},
		ResumeLine: func(provider, id, session string) (string, error) {
			ws, _, _, _ := fakeTmux{w}.ParseSessionName(session)
			bin := "claude --resume "
			if provider == agents.Codex {
				bin = "codex resume "
			}
			line := bin + id
			if ws != "" {
				line += " --workspace " + ws
			}
			return line, nil
		},
		ClaudeProjects: projects, CodexSessions: codexDet.SessionsRoot(), StatsDir: filepath.Join(home, ".data", "session-stats"),
		Timing: timing,
	})
	s.inlineEffects = true
	return s
}

type socketLog struct {
	mu   sync.Mutex
	msgs []any
	// done is closed when the socket's reader has stopped.
	done chan struct{}
}

func (l *socketLog) add(v any) {
	l.mu.Lock()
	l.msgs = append(l.msgs, v)
	l.mu.Unlock()
}

func TestGoldenScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("plays a 30 s scenario")
	}
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	want := string(raw)
	got := playScenario(t)
	if got != want {
		if out := os.Getenv("TIMELINE_GOLDEN_GOT"); out != "" {
			_ = os.WriteFile(out, []byte(got), 0o644)
		}
		t.Fatalf("the Go timeline answered differently from the previous runtime:\n%s", firstDiff(want, got))
	}
}

func firstDiff(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var x, y string
		if i < len(al) {
			x = al[i]
		}
		if i < len(bl) {
			y = bl[i]
		}
		if x != y {
			from := max(0, i-3)
			ctx := strings.Join(al[from:min(len(al), i+1)], "\n")
			return "line " + strconv.Itoa(i+1) + "\n--- context (golden) ---\n" + ctx + "\n--- go ---\n" + y
		}
	}
	return ""
}

func playScenario(t *testing.T) string {
	n := 0
	oldID := transcript.NewID
	transcript.NewID = func() string { n++; return "id" + leftPad(strconv.FormatInt(int64(n), 36), 4) }
	oldNow := transcript.Now
	transcript.Now = func() float64 { return float64(fixedNow.UnixMilli()) }
	t.Cleanup(func() { transcript.NewID, transcript.Now = oldID, oldNow })

	root := t.TempDir()
	home := filepath.Join(root, "home")
	stage := filepath.Join(root, "stage")
	must(t, os.MkdirAll(home, 0o755))
	must(t, os.MkdirAll(stage, 0o755))
	vars := map[string]string{"home": home, "claude": filepath.Join(home, ".claude"), "codex": filepath.Join(home, ".codex"),
		"data": filepath.Join(home, ".data")}
	w := &world{panes: map[string]*jsvalue.Object{}, procs: map[int]*jsvalue.Object{}, busy: map[string]bool{}, recover: map[string]*jsvalue.Object{}}
	s := newTestServer(t, w, home)
	srv := httptest.NewServer(http.HandlerFunc(s.ServeHTTP))
	defer srv.Close()
	defer s.Shutdown()

	raw, err := os.ReadFile("testdata/scenario.json")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsvalue.Parse(expandVars(raw, vars))
	if err != nil {
		t.Fatal(err)
	}
	steps := doc.(*jsvalue.Object).Array("steps")
	conns := map[string]*websocket.Conn{}
	logs := map[string]*socketLog{}
	seen := map[string]int{}
	for i, v := range steps {
		st := v.(*jsvalue.Object)
		path := st.String("path")
		switch op := st.String("op"); op {
		case "mkdir":
			must(t, os.MkdirAll(path, 0o755))
		case "copy":
			b, err := os.ReadFile(filepath.Join("testdata/fixtures", st.String("from")))
			must(t, err)
			place(t, stage, path, b)
		case "append":
			b, err := os.ReadFile(filepath.Join("testdata/fixtures", st.String("from")))
			must(t, err)
			appendFile(t, path, b)
		case "appendText":
			appendFile(t, path, []byte(st.String("text")))
		case "write":
			d, _ := st.Get("data")
			place(t, stage, path, jsvalue.Stringify(d, ""))
		case "rm":
			_ = os.Remove(path)
		case "pane":
			w.mu.Lock()
			p := w.panes[st.String("session")]
			if p == nil {
				p = jsvalue.NewObject()
				w.panes[st.String("session")] = p
			}
			p.Assign(st.Object("pane"))
			w.mu.Unlock()
		case "proc":
			pid, _ := st.Number("pid")
			w.mu.Lock()
			w.procs[int(pid)] = st.Object("proc")
			w.mu.Unlock()
		case "noproc":
			pid, _ := st.Number("pid")
			w.mu.Lock()
			delete(w.procs, int(pid))
			w.mu.Unlock()
		case "busy":
			on, _ := st.Get("on")
			w.mu.Lock()
			w.busy[st.String("session")] = on == true
			w.mu.Unlock()
		case "recover":
			w.mu.Lock()
			w.recover[st.String("session")] = st.Object("found")
			w.mu.Unlock()
		case "open":
			name := st.String("socket")
			l := &socketLog{done: make(chan struct{})}
			logs[name] = l
			c, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/timeline?"+st.String("query"),
				&websocket.DialOptions{CompressionMode: websocket.CompressionContextTakeover})
			must(t, err)
			c.SetReadLimit(100 << 20)
			conns[name] = c
			go func() {
				defer close(l.done)
				for {
					_, data, err := c.Read(context.Background())
					if err != nil {
						var ce websocket.CloseError
						if errors.As(err, &ce) {
							l.add(jsvalue.NewObject("close", float64(ce.Code), "reason", ce.Reason))
						}
						return
					}
					v, err := jsvalue.Parse(data)
					if err != nil {
						t.Errorf("socket %s: %v", name, err)
						continue
					}
					l.add(v)
				}
			}()
		case "send":
			var data []byte
			if r, has := st.Get("raw"); has {
				data = []byte(r.(string))
			} else {
				d, _ := st.Get("data")
				data = jsvalue.Stringify(d, "")
			}
			must(t, conns[st.String("socket")].Write(context.Background(), websocket.MessageText, data))
		case "close":
			name := st.String("socket")
			closeClient(t, s, conns[name], logs[name])
			delete(conns, name)
		case "stats":
			s.BroadcastSessionStats(st.Object("data"))
		case "codexHook":
			info := st.Object("info")
			pid, _ := info.Number("pid")
			s.CodexSessionInfo(st.String("session"), agents.SessionInfo{Status: agents.SessionStatus(info.String("status")),
				SessionID: info.String("sessionId"), JSONLPath: info.String("jsonlPath"), PID: int(pid), Cwd: info.String("cwd")})
		case "wait":
			ms, _ := st.Number("ms")
			time.Sleep(time.Duration(ms) * time.Millisecond)
			if until := st.Object("until"); until != nil {
				awaitEvents(t, i, until, logs, w, seen)
			}
		default:
			t.Fatalf("unknown step %s", op)
		}
	}
	for name, c := range conns {
		closeClient(t, s, c, logs[name])
	}
	for name, l := range logs {
		select {
		case <-l.done:
		case <-time.After(eventWait):
			t.Fatalf("socket %s: the reader never stopped", name)
		}
	}
	s.Shutdown()
	w.mu.Lock()
	effects := append([]any(nil), w.effects...)
	w.mu.Unlock()
	return normalize(logs, effects, home)
}

// eventWait bounds the wait for what a step causes; a loaded machine is slow, a missing event fails the comparison.
const eventWait = 30 * time.Second

// place writes a file whole, as the previous runtime's harness did: its watchers ran only after the synchronous write,
// so they never saw a file created and not yet written. Here they run alongside, hence a rename from outside the
// watched folders.
func place(t *testing.T, stage, path string, b []byte) {
	f, err := os.CreateTemp(stage, "file")
	must(t, err)
	_, err = f.Write(b)
	must(t, err)
	must(t, f.Close())
	must(t, os.Rename(f.Name(), path))
}

func eventName(m any) string {
	if o, ok := m.(*jsvalue.Object); ok {
		if o.Has("close") {
			return "close"
		}
		return o.String("type")
	}
	return ""
}

// awaitEvents holds the scenario until what a step waits for has happened: messages per socket and calls to the layout
// store and the keeper ("effects"), each list matched past what the earlier steps already took.
func awaitEvents(t *testing.T, step int, until *jsvalue.Object, logs map[string]*socketLog, w *world, seen map[string]int) {
	t.Helper()
	deadline := time.Now().Add(eventWait)
	for _, key := range until.Keys() {
		v, _ := until.Get(key)
		var want []string
		for _, x := range v.([]any) {
			want = append(want, x.(string))
		}
		items := func() []any {
			if key == "effects" {
				w.mu.Lock()
				defer w.mu.Unlock()
				return append([]any(nil), w.effects...)
			}
			l := logs[key]
			if l == nil {
				return nil
			}
			l.mu.Lock()
			defer l.mu.Unlock()
			return append([]any(nil), l.msgs...)
		}
		name := eventName
		if key == "effects" {
			name = func(e any) string { return jscompat.String(e.([]any)[0]) }
		}
		for {
			if end, ok := matchFrom(items(), seen[key], want, name); ok {
				seen[key] = end
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("step %d: %s never got %v", step, key, want)
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
}

// matchFrom finds every wanted name among the items from start on, each item used once; end is past the last one used.
func matchFrom(items []any, start int, want []string, name func(any) string) (end int, ok bool) {
	used := map[int]bool{}
	end = start
	for _, n := range want {
		found := false
		for i := start; i < len(items); i++ {
			if !used[i] && name(items[i]) == n {
				used[i], found = true, true
				end = max(end, i+1)
				break
			}
		}
		if !found {
			return 0, false
		}
	}
	return end, true
}

// closeClient is the page closing its socket: the old runtime's client saw its own close as 1005. It returns once the
// socket's reader has stopped and the server has let the socket go.
func closeClient(t *testing.T, s *Server, c *websocket.Conn, l *socketLog) {
	closed := func() bool {
		l.mu.Lock()
		defer l.mu.Unlock()
		n := len(l.msgs)
		return n > 0 && eventName(l.msgs[n-1]) == "close"
	}
	byServer := closed()
	before := s.Count()
	_ = c.Close(websocket.StatusNoStatusRcvd, "")
	_ = c.CloseNow()
	deadline := time.After(eventWait)
	select {
	case <-l.done:
	case <-deadline:
		t.Fatal("a closed socket's reader never stopped")
	}
	for !byServer && s.Count() >= before {
		select {
		case <-deadline:
			t.Fatal("the server never let a closed socket go")
		case <-time.After(2 * time.Millisecond):
		}
	}
	if !closed() {
		l.add(jsvalue.NewObject("close", 1005.0, "reason", ""))
	}
}

func leftPad(s string, n int) string {
	for len(s) < n {
		s = "0" + s
	}
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func appendFile(t *testing.T, path string, b []byte) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	must(t, err)
	_, err = f.Write(b)
	must(t, err)
	must(t, f.Close())
}

var varRE = regexp.MustCompile(`\{(home|claude|codex|data)\}`)

func expandVars(raw []byte, vars map[string]string) []byte {
	return varRE.ReplaceAllFunc(raw, func(m []byte) []byte {
		v := vars[string(m[1:len(m)-1])]
		q := jsvalue.Stringify(v, "")
		return q[1 : len(q)-1]
	})
}

var idRE = regexp.MustCompile(`^id[0-9a-z]{4,}$`)

func normalize(logs map[string]*socketLog, effects []any, home string) string {
	ids := map[string]string{}
	var walk func(v any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case string:
			if idRE.MatchString(x) {
				if _, ok := ids[x]; !ok {
					ids[x] = "entry-" + strconv.Itoa(len(ids)+1)
				}
				return ids[x]
			}
			return strings.ReplaceAll(x, home, "{home}")
		case []any:
			out := make([]any, len(x))
			for i, e := range x {
				out[i] = walk(e)
			}
			return out
		case *jsvalue.Object:
			out := jsvalue.NewObject()
			for _, k := range x.Keys() {
				e, _ := x.Get(k)
				out.Set(k, walk(e))
			}
			return out
		}
		return v
	}
	names := make([]string, 0, len(logs))
	for k := range logs {
		names = append(names, k)
	}
	sort.Strings(names)
	sockets := jsvalue.NewObject()
	for _, name := range names {
		var kept []any
		for _, m := range logs[name].msgs {
			if o, ok := m.(*jsvalue.Object); ok && o.String("type") == "timeline:heartbeat" {
				continue
			}
			kept = append(kept, walk(m))
		}
		if kept == nil {
			kept = []any{}
		}
		sockets.Set(name, kept)
	}
	tabOf := func(call any) string {
		parts := strings.Split(jscompat.String(call.([]any)[1]), "-")
		return parts[len(parts)-1]
	}
	effects = append([]any(nil), effects...)
	sort.SliceStable(effects, func(i, j int) bool { return tabOf(effects[i]) < tabOf(effects[j]) })
	eff := make([]any, len(effects))
	for i, e := range effects {
		call := e.([]any)
		out := make([]any, len(call))
		for j, x := range call {
			out[j] = walk(x)
		}
		eff[i] = out
	}
	var b bytes.Buffer
	b.Write(jsvalue.Stringify(jsvalue.NewObject("sockets", sockets, "effects", eff), " "))
	b.WriteByte('\n')
	return b.String()
}
