//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	agentsessions "github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// v2 on the real binary and the real core, with a stand-in for Claude's terminal in a tmux pane (testdata/fake-claude-tui.sh)
// and no RUNTIME_V2_FAKE: hello and machines out of the live runtime, a conversation created with its first message
// (launch, typing, turn, transcript), a second message whose turn asks permission and the decision pressed in the menu,
// the socket's heartbeat answered while a turn runs, and a restart of the runtime that keeps the conversation and its pane.
func TestV2LiveRuntimeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the runtime")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go")
	}
	tmuxBin, err := exec.LookPath("tmux")
	if err != nil {
		t.Skip("no tmux")
	}
	// A tmux socket path is short: the default temp dir of a test is not.
	root, err := os.MkdirTemp("", "hsv2")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(root, "runtime")
	if out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(root, "h")
	fakes := filepath.Join(home, "bin")
	project := filepath.Join(home, "project")
	tmuxDir := filepath.Join(root, "t")
	for _, d := range []string{fakes, project, tmuxDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// The stand-in drops the keys of its first seconds, as a starting TUI does: the first message of every
	// conversation has to survive that.
	if err := os.WriteFile(filepath.Join(home, "fake-claude-boot-ms"), []byte("6000"), 0o600); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("testdata", "fake-claude-tui.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fakes, "claude"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	socket := "hs-test-live-" + strconv.Itoa(os.Getpid())
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	// The design library has a template of its own with one design system in it.
	dsDir := filepath.Join(project, "design-artifacts", ".template", "project", "_ds", "test-system-6a42ea8b-299e-4b9c-8c3f-1793b85c4b79")
	if err := os.MkdirAll(dsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	for file, body := range map[string]string{
		filepath.Join(dsDir, "README.md"):                                        "# Test System\n\nTokens and components for the tests.\n",
		filepath.Join(project, "design-artifacts", ".template", "artifact.json"): `{"v": 1, "title": "template", "boards": []}`,
	} {
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Dictation: the real ffmpeg and a recognition service of a few lines on a socket, if the machine has ffmpeg.
	ffmpeg, _ := exec.LookPath("ffmpeg")
	asr := filepath.Join(root, "asr.sock")
	if ffmpeg != "" {
		serveFakeASR(t, asr)
	}
	env := []string{
		"RUNTIME_VOICE_SOCKET=" + asr, "RUNTIME_VOICE_FFMPEG_BIN=" + ffmpeg,
		"HOME=" + home, "USER=agent", "LOGNAME=agent", "SHELL=/bin/bash", "LANG=C.UTF-8", "TMUX_TMPDIR=" + tmuxDir,
		"PATH=" + fakes + ":/usr/local/bin:/usr/bin:/bin",
		"RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "RUNTIME_GATE_UPSTREAM=", "AGENT_DATA_DIR=" + filepath.Join(home, ".hl"),
		"AGENT_VERSION=9.9.9", "RUNTIME_TMUX_SOCKET=" + socket, "RUNTIME_SESSION_PREFIX=hs-", "INIT_PASSWORD=pw-test",
		"RUNTIME_WORKSPACE_DIR=" + project, "RUNTIME_FIRST_WORKSPACE_NAME=First", "RUNTIME_GATE_COUNT_SECONDS=3600",
		"RUNTIME_CLAUDE_LAUNCHER=" + filepath.Join(fakes, "claude"),
	}
	type watch struct {
		mu       sync.Mutex
		epoch    string
		convs    map[string]wire.Conversation
		prompts  map[string]wire.MessageStateKind
		states   map[string][]wire.AgentStateKind
		perm     *wire.Permission
		bg       map[string]wire.BackgroundTask
		projects map[string]wire.Project
		cwd      string
		pongs    chan int64
		closed   bool
		ws       *websocket.Conn
		writeMu  sync.Mutex
		frameErr string
		log      []string
	}
	var lastW *watch
	logFile, _ := os.Create(filepath.Join(root, "runtime.log"))
	defer logFile.Close()
	var runtime *exec.Cmd
	start := func() {
		c := exec.Command(bin)
		c.Env = env
		c.Stdout, c.Stderr = logFile, logFile
		c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := c.Start(); err != nil {
			t.Fatal(err)
		}
		runtime = c
	}
	stop := func() {
		_ = runtime.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = runtime.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = runtime.Process.Kill()
			<-done
		}
	}
	start()
	t.Cleanup(func() {
		stop()
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(root, "runtime.log"))
			t.Logf("runtime log:\n%s", tail(string(raw), 6000))
			c := exec.Command(tmuxBin, "-L", socket, "list-sessions", "-F", "#{session_name}")
			c.Env = env
			names, _ := c.Output()
			for _, name := range strings.Fields(string(names)) {
				c := exec.Command(tmuxBin, "-L", socket, "capture-pane", "-p", "-t", name)
				c.Env = env
				out, _ := c.CombinedOutput()
				t.Logf("pane %s:\n%s", name, out)
			}
			if lastW != nil {
				lastW.mu.Lock()
				t.Logf("conversations %+v\nprompts %v\nstates %v\nevents:\n%s", lastW.convs, lastW.prompts, lastW.states, tail(strings.Join(lastW.log, "\n"), 5000))
				lastW.mu.Unlock()
			}
		}
		// The panes' shells write into the home as they exit: the server goes, and its panes, before the dir does.
		c := exec.Command(tmuxBin, "-L", socket, "kill-server")
		c.Env = env
		_ = c.Run()
		time.Sleep(300 * time.Millisecond)
		_ = os.RemoveAll(root)
	})

	client := &http.Client{Timeout: 20 * time.Second}
	var cookie string
	call := func(method, path string, body any, out any) int {
		t.Helper()
		var r io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			r = bytes.NewReader(raw)
		}
		req, _ := http.NewRequest(method, base+path, r)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(wire.DeviceHeader, "dev-live")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		res, err := client.Do(req)
		if err != nil {
			return 0
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if out != nil {
			_ = json.Unmarshal(raw, out)
		}
		for _, c := range res.Header.Values("Set-Cookie") {
			if strings.HasPrefix(c, "session-token=") {
				cookie, _, _ = strings.Cut(c, ";")
			}
		}
		return res.StatusCode
	}
	waitFor := func(what string, d time.Duration, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(d); !ok(); time.Sleep(50 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	sessions := func() []string {
		c := exec.Command(tmuxBin, "-L", socket, "list-sessions", "-F", "#{session_name}")
		c.Env = env
		out, _ := c.Output()
		return strings.Fields(string(out))
	}
	do := func(op wire.Op, body any) wire.CmdResult {
		t.Helper()
		var res wire.CmdResult
		if code := call("POST", "/api/v2/cmd/"+string(op), body, &res); code != 200 {
			t.Fatalf("%s: %d", op, code)
		}
		return res
	}
	signIn := func() {
		t.Helper()
		waitFor("the runtime", 30*time.Second, func() bool { return call("GET", "/api/v2/hello", nil, nil) == http.StatusUnauthorized })
		if code := call("POST", "/api/auth/login", map[string]string{"password": "pw-test"}, nil); code != 200 || cookie == "" {
			t.Fatalf("sign in: %d", code)
		}
	}
	signIn()

	// --- hello and machines out of the live runtime --------------------------------------------------------------
	var hello wire.Hello
	if code := call("GET", "/api/v2/hello", nil, &hello); code != 200 || hello.Proto != 2 || hello.Version != "9.9.9" || len(hello.Features) == 0 {
		t.Fatalf("hello: %d %+v", code, hello)
	}
	var machines []wire.Machine
	if code := call("GET", "/api/v2/machines", nil, &machines); code != 200 || len(machines) != 1 || machines[0].ID != "local" {
		t.Fatalf("machines: %d %+v", code, machines)
	}

	// The person and their settings, kept in <data>/v2/me.json.
	var me wire.Me
	if code := call("GET", "/api/v2/me", nil, &me); code != 200 || me.Rev != 1 || me.Settings.UIVersion != "next" {
		t.Fatalf("me: %d %+v", code, me)
	}
	var patched struct{ Me wire.Me }
	if code := call("PATCH", "/api/v2/me/settings", map[string]any{"rev": 1, "patch": map[string]any{"density": "compact"}}, &patched); code != 200 ||
		patched.Me.Rev != 2 || patched.Me.Settings.Density != "compact" {
		t.Fatalf("settings: %d %+v", code, patched)
	}

	// --- one socket, read by one goroutine ------------------------------------------------------------------------
	dial := func() *watch {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(port)+"/api/v2/socket",
			&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
		if err != nil {
			t.Fatal(err)
		}
		ws.SetReadLimit(4 << 20)
		w := &watch{ws: ws, bg: map[string]wire.BackgroundTask{}, projects: map[string]wire.Project{}, convs: map[string]wire.Conversation{}, prompts: map[string]wire.MessageStateKind{},
			states: map[string][]wire.AgentStateKind{}, pongs: make(chan int64, 256)}
		t.Cleanup(func() { _ = ws.CloseNow() })
		lastW = w
		return w
	}
	send := func(w *watch, v any) {
		raw, _ := json.Marshal(v)
		w.writeMu.Lock()
		defer w.writeMu.Unlock()
		_ = w.ws.Write(context.Background(), websocket.MessageText, raw)
	}
	apply := func(w *watch, f wire.ServerFrame) {
		w.mu.Lock()
		defer w.mu.Unlock()
		switch f := f.(type) {
		case wire.WelcomeFrame:
			w.epoch = f.Epoch
		case wire.PongFrame:
			select {
			case w.pongs <- f.T:
			default:
			}
		case wire.SnapshotFrame:
			if f.Convs != nil {
				for _, c := range f.Convs.Conversations {
					w.convs[c.ID] = c
				}
				for _, p := range f.Convs.Projects {
					w.projects[p.ID] = p
				}
				w.cwd = f.Convs.DefaultCwd
			}
			if f.Conv != nil {
				for _, m := range f.Conv.Messages {
					w.prompts[m.ClientID] = m.State.K
				}
				for _, t := range f.Conv.Background {
					w.bg[t.ID] = t
				}
				if len(f.Conv.Permissions) > 0 {
					p := f.Conv.Permissions[len(f.Conv.Permissions)-1]
					w.perm = &p
				}
			}
		case wire.EvFrame:
			if b, err := json.Marshal(f); err == nil {
				w.log = append(w.log, time.Now().Format("05.000")+" "+string(b))
			}
			switch e := f.E.(type) {
			case wire.ConversationUpsertEv:
				w.convs[e.Conv.ID] = e.Conv
			case wire.ProjectUpsertEv:
				w.projects[e.Project.ID] = e.Project
			case wire.ConversationStateEv:
				c := w.convs[e.ID]
				c.State = e.State
				w.convs[e.ID] = c
				if s := w.states[e.ID]; len(s) == 0 || s[len(s)-1] != e.State.K {
					w.states[e.ID] = append(s, e.State.K)
				}
			case wire.PromptStateEv:
				w.prompts[e.ClientID] = e.State.K
			case wire.PermissionRequestedEv:
				p := e.Permission
				w.perm = &p
			case wire.BackgroundUpsertEv:
				w.bg[e.Task.ID] = e.Task
			case wire.BackgroundRemovedEv:
				delete(w.bg, e.ID)
			}
		}
	}
	pump := func(w *watch) {
		go func() {
			for {
				_, raw, err := w.ws.Read(context.Background())
				if err != nil {
					w.mu.Lock()
					w.closed = true
					w.mu.Unlock()
					return
				}
				f, err := wire.DecodeServerFrame(raw)
				if err != nil {
					w.mu.Lock()
					w.frameErr = err.Error() + ": " + string(raw)
					w.mu.Unlock()
					continue
				}
				apply(w, f)
			}
		}()
	}
	get := func(w *watch, read func() bool) func() bool {
		return func() bool {
			w.mu.Lock()
			defer w.mu.Unlock()
			if w.frameErr != "" {
				t.Fatalf("a frame out of contract: %s", w.frameErr)
			}
			return read()
		}
	}
	w := dial()
	pump(w)
	send(w, wire.HelloFrame{Proto: 2, Device: "dev-live", App: "e2e"})
	waitFor("the welcome", 5*time.Second, get(w, func() bool { return w.epoch != "" }))
	epoch := w.epoch
	send(w, wire.SubFrame{Ch: wire.ChannelConvs})

	// --- a conversation with its first message --------------------------------------------------------------------
	res := do(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "c1", Provider: "claude", Cwd: project,
		Model: &wire.Model{ID: "sonnet"}, First: &wire.FirstMessage{ClientID: "m1", Text: "Проверь сборку", AttachmentIDs: []string{}}})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("create: %+v", res)
	}
	id := *res.ID
	send(w, wire.SubFrame{Ch: wire.ConvChannel(id)})
	state := func(k wire.AgentStateKind) func() bool {
		return get(w, func() bool { return w.convs[id].State.K == k })
	}
	waitFor("the first turn to end", 40*time.Second, get(w, func() bool {
		return w.prompts["m1"] == wire.MsgDone && w.convs[id].State.K == wire.StateReady
	}))
	w.mu.Lock()
	seen := append([]wire.AgentStateKind(nil), w.states[id]...)
	w.mu.Unlock()
	t.Logf("states of the first turn: %v", seen)
	var turns wire.TurnsPage
	if code := call("GET", "/api/v2/conversations/"+id+"/turns", nil, &turns); code != 200 || len(turns.Turns) != 1 || turns.Turns[0].Status != wire.TurnDone {
		t.Fatalf("turns: %d %+v", code, turns)
	}
	if turns.Turns[0].AnswerEntryID == nil {
		t.Fatalf("the finished turn has no answer entry: %+v", turns.Turns[0])
	}
	for _, e := range turns.Entries {
		if e.TurnID == nil || *e.TurnID != turns.Turns[0].ID {
			t.Fatalf("an entry of the new conversation is outside its turn: %+v", e)
		}
	}
	var answer string
	for _, e := range turns.Entries {
		if e.T == wire.EntrySay {
			answer = e.MD
		}
	}
	if answer != "done: Проверь сборку" {
		t.Fatalf("the answer %q, entries %+v", answer, turns.Entries)
	}

	// --- a message sent to an agent that is still starting is typed when its terminal takes keys -------------------
	// (the terminal here takes none for the first seconds and drops what is typed: its Enter is lost, its text shows later)
	res = do(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "c2", Provider: "claude", Cwd: project, Model: &wire.Model{ID: "sonnet"}})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("create: %+v", res)
	}
	typedConv := *res.ID
	send(w, wire.SubFrame{Ch: wire.ConvChannel(typedConv)})
	if res = do(wire.OpSend, wire.SendCmd{OpID: "t1", ClientID: "t1", ConversationID: typedConv, Text: "набрано руками", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	waitFor("the typed message to be done", 60*time.Second, get(w, func() bool { return w.prompts["t1"] == wire.MsgDone }))

	// --- the heartbeat is answered while a turn runs --------------------------------------------------------------
	res = do(wire.OpSend, wire.SendCmd{OpID: "m2", ClientID: "m2", ConversationID: id, Text: "ещё раз", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	if res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	var worst time.Duration
	pings := 0
	for deadline := time.Now().Add(20 * time.Second); time.Now().Before(deadline); {
		sent := time.Now()
		send(w, wire.PingFrame{T: sent.UnixMilli()})
		select {
		case <-w.pongs:
			if d := time.Since(sent); d > worst {
				worst = d
			}
			pings++
		case <-time.After(2 * time.Second):
			t.Fatal("no pong in 2 s")
		}
		if get(w, func() bool { return w.prompts["m2"] == wire.MsgDone })() {
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if pings < 5 || worst > 500*time.Millisecond {
		t.Fatalf("%d pings, the slowest answered in %v", pings, worst)
	}
	t.Logf("%d pings during a turn, the slowest answered in %v", pings, worst)
	waitFor("the second turn to end", 20*time.Second, state(wire.StateReady))

	// --- the model: listed by the machine, the new conversation's own, switched in the running agent -----------------------
	waitFor("the machine to list the models", 30*time.Second, func() bool {
		var ms []wire.Machine
		if call("GET", "/api/v2/machines", nil, &ms) != 200 || len(ms) != 1 {
			return false
		}
		for _, a := range ms[0].Agents {
			if a.Provider == wire.ProviderClaude {
				return a.CanSwitchModel && len(a.Models) > 0 && a.Models[0].ID != "" && len(a.Models[0].Efforts) == 5 && a.Models[0].Ultracode
			}
		}
		return false
	})
	if !slices.Contains(hello.Features, wire.FeatureModel) {
		t.Fatalf("hello does not announce the model: %v", hello.Features)
	}
	calls, _ := os.ReadFile(filepath.Join(home, "claude-calls.log"))
	if !strings.Contains(string(calls), "--model 'sonnet'") && !strings.Contains(string(calls), "--model sonnet") {
		t.Fatalf("the agent was not started on the model asked for:\n%s", calls)
	}
	w.mu.Lock()
	created := w.convs[id].Model
	w.mu.Unlock()
	if created == nil || created.ID != "sonnet" {
		t.Fatalf("the conversation's model %+v", created)
	}
	modelIs := func(model string, effort *wire.Effort) func() bool {
		return get(w, func() bool {
			m := w.convs[id].Model
			return m != nil && m.ID == model && (effort == nil && m.Effort == nil || effort != nil && m.Effort != nil && *m.Effort == *effort)
		})
	}
	high := wire.EffortHigh
	// «Switch model?» is answered for the person: opus asks, the page said yes by choosing it.
	if res := do(wire.OpModel, wire.ModelCmd{OpID: "mo1", ID: id, Model: "opus", Effort: &high}); res.R != wire.ResultAccepted {
		t.Fatalf("model: %+v", res)
	}
	waitFor("the conversation to carry the new model", 10*time.Second, modelIs("opus", &high))
	modelsLog, _ := os.ReadFile(filepath.Join(home, "models.log"))
	if string(modelsLog) != "model opus\neffort high\n" {
		t.Fatalf("what the agent was told: %q", modelsLog)
	}
	// A level only Claude's newer models take goes through the same way.
	xhigh := wire.EffortXHigh
	if res := do(wire.OpModel, wire.ModelCmd{OpID: "mo2x", ID: id, Model: "opus", Effort: &xhigh}); res.R != wire.ResultAccepted {
		t.Fatalf("model, xhigh: %+v", res)
	}
	waitFor("the conversation to carry xhigh", 10*time.Second, modelIs("opus", &xhigh))
	// A model that is already set is not typed again; one asked for while the agent works waits for the turn's end.
	if res := do(wire.OpModel, wire.ModelCmd{OpID: "mo2", ID: id, Model: "opus", Effort: &xhigh}); res.R != wire.ResultAccepted {
		t.Fatalf("the same model: %+v", res)
	}
	if res := do(wire.OpSend, wire.SendCmd{OpID: "m2b", ClientID: "m2b", ConversationID: id, Text: "ещё", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	waitFor("the agent to work", 20*time.Second, state(wire.StateWorking))
	if res := do(wire.OpModel, wire.ModelCmd{OpID: "mo3", ID: id, Model: "sonnet"}); res.R != wire.ResultAccepted {
		t.Fatalf("model under a working agent: %+v", res)
	}
	waitFor("the switch to wait for the turn", 10*time.Second, get(w, func() bool { return w.convs[id].PendingModel != nil }))
	waitFor("the turn to end", 30*time.Second, get(w, func() bool { return w.prompts["m2b"] == wire.MsgDone && w.convs[id].State.K == wire.StateReady }))
	if res := do(wire.OpModel, wire.ModelCmd{OpID: "mo4", ID: id, Model: "sonnet"}); res.R != wire.ResultAccepted {
		t.Fatalf("model after the turn: %+v", res)
	}
	waitFor("the conversation to carry sonnet", 10*time.Second, modelIs("sonnet", nil))
	modelsLog, _ = os.ReadFile(filepath.Join(home, "models.log"))
	if string(modelsLog) != "model opus\neffort high\neffort xhigh\nmodel sonnet\n" {
		t.Fatalf("what the agent was told: %q", modelsLog)
	}

	// --- a background shell, listed, and ended by the page ----------------------------------------------------------------
	if !slices.Contains(hello.Features, wire.FeatureBackground) {
		t.Fatalf("hello does not announce the background: %v", hello.Features)
	}
	if res := do(wire.OpSend, wire.SendCmd{OpID: "m-bg", ClientID: "m-bg", ConversationID: id, Text: "bg: запусти сервер", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	var task wire.BackgroundTask
	waitFor("the background shell to be listed", 40*time.Second, get(w, func() bool {
		for _, x := range w.bg {
			task = x
		}
		return len(w.bg) == 1
	}))
	if task.Kind != wire.BgCommand || task.Title != "dev server" || task.Status != wire.BgStatusRunning || !task.Stoppable || !task.DevServer ||
		task.OutputPath == nil || task.ConversationID != id || task.StartedAt == 0 {
		t.Fatalf("the background task %+v", task)
	}
	if len(writersOf(*task.OutputPath)) != 1 {
		t.Fatalf("no process writes %s", *task.OutputPath)
	}
	if res := do(wire.OpBgStop, wire.BgStopCmd{OpID: "bs0", ConversationID: id, TaskID: "nope"}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
		t.Fatalf("a task the agent does not run: %+v", res)
	}
	if res := do(wire.OpBgStop, wire.BgStopCmd{OpID: "bs1", ConversationID: id, TaskID: task.ID}); res.R != wire.ResultAccepted || res.Ch != wire.ConvChannel(id) {
		t.Fatalf("bg.stop: %+v", res)
	}
	if n := len(writersOf(*task.OutputPath)); n != 0 {
		t.Fatalf("%d processes still write the output of a stopped task", n)
	}
	// The agent writes the task's end in its transcript, and the task leaves the list.
	waitFor("the stopped task to leave the list", 20*time.Second, get(w, func() bool { return len(w.bg) == 0 }))
	if res := do(wire.OpBgStop, wire.BgStopCmd{OpID: "bs2", ConversationID: id, TaskID: task.ID}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
		t.Fatalf("a task that left: %+v", res)
	}
	waitFor("the turn after the background shell", 30*time.Second, get(w, func() bool {
		return w.prompts["m-bg"] == wire.MsgDone && w.convs[id].State.K == wire.StateReady
	}))

	// --- a permission menu, and the decision pressed in it ----------------------------------------------------------
	w.mu.Lock()
	w.perm = nil
	w.mu.Unlock()
	if res := do(wire.OpSend, wire.SendCmd{OpID: "m3", ClientID: "m3", ConversationID: id, Text: "perm: убери build", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	waitFor("the permission", 30*time.Second, state(wire.StateWaitingPermission))
	waitFor("the permission's options", 10*time.Second, get(w, func() bool { return w.perm != nil && len(w.perm.Options) >= 3 }))
	w.mu.Lock()
	perm := *w.perm
	w.mu.Unlock()
	if perm.Command == nil || !strings.Contains(*perm.Command, "rm -rf build") {
		t.Fatalf("the permission %+v", perm)
	}
	if res := do(wire.OpDecide, wire.DecideCmd{OpID: "d1", ConversationID: id, PermissionID: perm.ID, OptionID: perm.Options[0].ID}); res.R != wire.ResultAccepted {
		t.Fatalf("decide: %+v", res)
	}
	waitFor("the turn after the decision", 30*time.Second, get(w, func() bool {
		return w.prompts["m3"] == wire.MsgDone && w.convs[id].State.K == wire.StateReady
	}))

	// --- a restart of the runtime: the conversation, its pane and its queue stay ----------------------------------------
	stop()
	waitFor("the socket to close", 10*time.Second, get(w, func() bool { return w.closed }))
	start()
	cookie = ""
	signIn()
	w = dial()
	pump(w)
	send(w, wire.HelloFrame{Proto: 2, Device: "dev-live", App: "e2e"})
	waitFor("the welcome", 10*time.Second, get(w, func() bool { return w.epoch != "" }))
	if w.epoch == epoch {
		t.Fatal("a restart keeps its epoch")
	}
	send(w, wire.SubFrame{Ch: wire.ChannelConvs})
	send(w, wire.SubFrame{Ch: wire.ConvChannel(id)})
	waitFor("the conversation after a restart", 20*time.Second, get(w, func() bool {
		c, ok := w.convs[id]
		return ok && (c.State.K == wire.StateReady || c.State.K == wire.StateIdle)
	}))
	if res := do(wire.OpSend, wire.SendCmd{OpID: "m4", ClientID: "m4", ConversationID: id, Text: "после рестарта", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued {
		t.Fatalf("send: %+v", res)
	}
	waitFor("a message after the restart", 30*time.Second, get(w, func() bool { return w.prompts["m4"] == wire.MsgDone }))
	calls, _ = os.ReadFile(filepath.Join(home, "claude-calls.log"))
	// One agent for each of the two conversations; the restart starts none.
	if n := strings.Count(string(calls), "--settings"); n != 2 {
		t.Fatalf("the agents were started %d times, twice is right:\n%s", n, calls)
	}
	if !strings.Contains(string(calls), "-- ") || !strings.Contains(string(calls), "Проверь сборку") {
		t.Fatalf("the first message did not go with the launch:\n%s", calls)
	}

	// --- deleting the conversation ends its pane and its tab ------------------------------------------------------------
	var saved struct {
		Session string `json:"session"`
	}
	raw, err := os.ReadFile(filepath.Join(home, ".hl", "v2", "convs", id+".json"))
	if err != nil || json.Unmarshal(raw, &saved) != nil || saved.Session == "" {
		t.Fatalf("the saved conversation: %v %s", err, raw)
	}
	session := saved.Session
	sessionOf := func() string {
		if slices.Contains(sessions(), session) {
			return session
		}
		return ""
	}
	if sessionOf() == "" {
		t.Fatalf("no tmux session %q among %v", session, sessions())
	}
	ws, _, found := strings.Cut(strings.TrimPrefix(session, "hs-"), "-pane-")
	tab := session[strings.LastIndex(session, "-tab-")+len("-"):]
	if session == "" || !found || !strings.HasPrefix(tab, "tab-") {
		t.Fatalf("no tmux session of %s (%q)", id, session)
	}
	var layout json.RawMessage
	if code := call("GET", "/api/layout?workspace="+ws, nil, &layout); code != 200 || !strings.Contains(string(layout), tab) {
		t.Fatalf("the layout before the delete: %d %s", code, layout)
	}
	if res := do(wire.OpConversationDelete, wire.ConversationDeleteCmd{OpID: "del1", ID: id}); res.R != wire.ResultAccepted {
		t.Fatalf("delete: %+v", res)
	}
	waitFor("the tmux session to end", 20*time.Second, func() bool { return sessionOf() == "" })
	waitFor("the tab to leave the layout", 10*time.Second, func() bool {
		layout = nil
		return call("GET", "/api/layout?workspace="+ws, nil, &layout) == 200 && !strings.Contains(string(layout), tab)
	})

	// --- a folder made a project, seen at once and by a socket that comes later ----------------------------------
	w.mu.Lock()
	defaultCwd := w.cwd
	w.mu.Unlock()
	if defaultCwd != project {
		t.Fatalf("the default folder of the snapshot: %q, want %q", defaultCwd, project)
	}
	folder := filepath.Join(home, "second")
	if err := os.MkdirAll(filepath.Join(folder, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	made := do(wire.OpProjectCreate, wire.ProjectCreateCmd{OpID: "prj1", Path: folder, Name: "Второй"})
	if made.R != wire.ResultAccepted || made.ID == nil || made.Seq == 0 {
		t.Fatalf("project.create: %+v", made)
	}
	waitFor("the project to show", 5*time.Second, get(w, func() bool { return w.projects[*made.ID].Path == folder }))
	if again := do(wire.OpProjectCreate, wire.ProjectCreateCmd{OpID: "prj2", Path: folder}); again.ID == nil || *again.ID != *made.ID {
		t.Fatalf("the same folder again: %+v", again)
	}
	if res := do(wire.OpProjectCreate, wire.ProjectCreateCmd{OpID: "prj3", Path: filepath.Join(home, "absent")}); res.R != wire.ResultRefused || res.Code != wire.RefuseNotFound {
		t.Fatalf("a folder that is not there: %+v", res)
	}
	w2 := dial()
	pump(w2)
	send(w2, wire.HelloFrame{Proto: 2, Device: "dev-live-2", App: "e2e"})
	waitFor("the second welcome", 5*time.Second, get(w2, func() bool { return w2.epoch != "" }))
	send(w2, wire.SubFrame{Ch: wire.ChannelConvs})
	waitFor("the project in a new snapshot", 5*time.Second, get(w2, func() bool {
		p := w2.projects[*made.ID]
		return p.Path == folder && p.Name == "Второй" && p.Git && p.MachineID != ""
	}))

	// --- the folders offered for a project: those below the default folder -----------------------------------------
	if err := os.MkdirAll(filepath.Join(project, "offered", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "offered", ".git", "HEAD"), []byte("ref: refs/heads/trunk\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var dirs wire.Dirs
	if code := call("GET", "/api/v2/dirs", nil, &dirs); code != 200 {
		t.Fatalf("dirs: %d", code)
	}
	var offered *wire.Dir
	for i := range dirs {
		if dirs[i].Name == "offered" {
			offered = &dirs[i]
		}
	}
	if offered == nil || !offered.Git || offered.Branch == nil || *offered.Branch != "trunk" || offered.Path != filepath.Join(project, "offered") {
		t.Fatalf("dirs: %+v", dirs)
	}

	// --- a session the agent kept from a terminal: listed, then continued as a conversation ---------------------------
	oldDir := filepath.Join(home, ".claude", "projects", agentsessions.ProjectName(project))
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	history := `{"type":"user","cwd":"` + project + `","timestamp":"2026-10-20T10:00:00.000Z","uuid":"u-old-1","sessionId":"2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01","message":{"role":"user","content":"вопрос из терминала"}}` + "\n" +
		`{"type":"assistant","cwd":"` + project + `","timestamp":"2026-10-20T10:00:05.000Z","uuid":"a-old-1","sessionId":"2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01","message":{"role":"assistant","content":[{"type":"text","text":"ответ из терминала"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(oldDir, "2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01.jsonl"), []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	var offeredSessions wire.AgentSessions
	if code := call("GET", "/api/v2/sessions", nil, &offeredSessions); code != 200 {
		t.Fatalf("sessions: %d", code)
	}
	var terminal *wire.AgentSession
	for i := range offeredSessions {
		if offeredSessions[i].SessionID == "2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01" {
			terminal = &offeredSessions[i]
		}
	}
	if terminal == nil || terminal.Provider != wire.ProviderClaude || terminal.Title != "вопрос из терминала" || terminal.Cwd != project || terminal.Turns != 1 {
		t.Fatalf("sessions: %+v", offeredSessions)
	}
	for _, other := range offeredSessions {
		if other.SessionID == id {
			t.Fatalf("a session of a conversation is offered: %+v", other)
		}
	}
	resumed := do(wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "rs1", Provider: wire.ProviderClaude, SessionID: "2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01", Cwd: project})
	if resumed.R != wire.ResultAccepted || resumed.ID == nil {
		t.Fatalf("resume: %+v", resumed)
	}
	rid := *resumed.ID
	send(w, wire.SubFrame{Ch: wire.ConvChannel(rid)})
	waitFor("the resumed conversation to be ready", 30*time.Second, get(w, func() bool {
		c, ok := w.convs[rid]
		return ok && (c.State.K == wire.StateReady || c.State.K == wire.StateIdle)
	}))
	w.mu.Lock()
	resumedConv := w.convs[rid]
	w.mu.Unlock()
	if resumedConv.Title != "вопрос из терминала" || resumedConv.Cwd != project || resumedConv.Provider != wire.ProviderClaude {
		t.Fatalf("the resumed conversation: %+v", resumedConv)
	}
	calls, _ = os.ReadFile(filepath.Join(home, "claude-calls.log"))
	if !strings.Contains(string(calls), "--resume 2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01") {
		t.Fatalf("the agent was not started on the session:\n%s", calls)
	}
	var past wire.TurnsPage
	if code := call("GET", "/api/v2/conversations/"+rid+"/turns", nil, &past); code != 200 || len(past.Turns) != 1 {
		t.Fatalf("the history of the resumed conversation: %d %+v", code, past)
	}
	var turnEntries wire.EntriesPage
	if code := call("GET", "/api/v2/conversations/"+rid+"/turns/"+url.PathEscape(past.Turns[0].ID)+"/entries", nil, &turnEntries); code != 200 {
		t.Fatalf("the entries of the resumed turn: %d", code)
	}
	texts := ""
	for _, e := range append(past.Entries, turnEntries.Entries...) {
		texts += e.Text + e.MD + "\n"
	}
	if !strings.Contains(texts, "вопрос из терминала") || !strings.Contains(texts, "ответ из терминала") {
		t.Fatalf("the history lacks the terminal's turn: %+v %+v", past.Entries, turnEntries.Entries)
	}
	if again := do(wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "rs2", Provider: wire.ProviderClaude, SessionID: "2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01", Cwd: project}); again.R != wire.ResultAccepted || again.ID == nil || *again.ID != rid {
		t.Fatalf("the same session again: %+v", again)
	}
	if code := call("GET", "/api/v2/sessions", nil, &offeredSessions); code != 200 {
		t.Fatalf("sessions after: %d", code)
	}
	for _, other := range offeredSessions {
		if other.SessionID == "2f0c9d3e-5a71-4c68-b1d4-8e6a7f3b9c01" {
			t.Fatalf("a continued session is still offered: %+v", other)
		}
	}
	if res := do(wire.OpSend, wire.SendCmd{OpID: "m5", ClientID: "m5", ConversationID: rid, Text: "продолжаем", AttachmentIDs: []string{},
		Mode: wire.ModeQueue, Kind: wire.KindPrompt}); res.R != wire.ResultQueued && res.R != wire.ResultAccepted {
		t.Fatalf("send to the resumed conversation: %+v", res)
	}
	waitFor("a message in the resumed conversation", 30*time.Second, get(w, func() bool { return w.prompts["m5"] == wire.MsgDone }))

	// --- terminals: the agent's pane and shells of the conversation's own ------------------------------------------
	var announced wire.Hello
	if code := call("GET", "/api/v2/hello", nil, &announced); code != 200 || !slices.Contains(announced.Features, wire.FeatureTerminal) {
		t.Fatalf("hello: %d %+v", code, announced)
	}
	termPage := "/api/v2/conversations/" + rid + "/terminals"
	var shell wire.Terminal
	if code := call("POST", termPage, nil, &shell); code != 200 || shell.Validate() != nil {
		t.Fatalf("terminal create: %d %+v", code, shell)
	}
	shellSession := "hs-v2t-" + rid + "-" + shell.ID
	if !slices.Contains(sessions(), shellSession) {
		t.Fatalf("no tmux session %q among %v", shellSession, sessions())
	}
	var shells wire.Terminals
	if code := call("GET", termPage, nil, &shells); code != 200 || len(shells) != 1 || shells[0].ID != shell.ID {
		t.Fatalf("terminal list: %d %+v", code, shells)
	}
	openTerminal := func(query string) *websocket.Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(port)+"/api/v2/terminal?"+query,
			&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
		if err != nil {
			t.Fatalf("terminal %s: %v", query, err)
		}
		ws.SetReadLimit(4 << 20)
		t.Cleanup(func() { _ = ws.CloseNow() })
		return ws
	}
	screenHas := func(ws *websocket.Conn, want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		got := ""
		for !strings.Contains(got, want) {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				t.Fatalf("waiting for %q in %q: %v", want, tail(got, 800), err)
			}
			if typ == websocket.MessageBinary && len(data) > 0 && data[0] == 0x01 {
				got += string(data[1:])
			}
		}
	}
	shellWS := openTerminal("conversation=" + rid + "&terminal=" + shell.ID + "&cols=100&rows=30&clientId=page-1")
	resize := make([]byte, 5)
	resize[0], resize[2], resize[4] = 0x02, 120, 40 // cols 120, rows 40, big endian
	if err := shellWS.Write(context.Background(), websocket.MessageBinary, resize); err != nil {
		t.Fatal(err)
	}
	if err := shellWS.Write(context.Background(), websocket.MessageBinary, append([]byte{0x00}, "echo own-$((20+22))-terminal\r"...)); err != nil {
		t.Fatal(err)
	}
	screenHas(shellWS, "own-42-terminal")
	// The shell is in the conversation's folder.
	if err := shellWS.Write(context.Background(), websocket.MessageBinary, append([]byte{0x00}, "pwd\r"...)); err != nil {
		t.Fatal(err)
	}
	screenHas(shellWS, project)
	// The agent's own pane: its screen comes first.
	agentWS := openTerminal("conversation=" + rid + "&clientId=page-1")
	screenHas(agentWS, "claude")
	// What is not there is refused before a socket is made.
	for query, code := range map[string]int{
		"conversation=" + rid + "&terminal=nosuchterm1": 404,
		"conversation=tab-nothing":                      404,
		"conversation=" + rid + "&terminal=BAD":         400,
	} {
		req, _ := http.NewRequest("GET", base+"/api/v2/terminal?"+query, nil)
		req.Header.Set("Cookie", cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != code {
			t.Errorf("terminal %s: %d, want %d", query, res.StatusCode, code)
		}
	}
	// Closing ends the shell and its session; deleting the conversation takes the others with it.
	var second wire.Terminal
	if code := call("POST", termPage, nil, &second); code != 200 {
		t.Fatalf("second terminal: %d", code)
	}
	if code := call("DELETE", termPage+"/"+shell.ID, nil, nil); code != 204 {
		t.Fatalf("terminal close: %d", code)
	}
	waitFor("the closed terminal's session to end", 10*time.Second, func() bool { return !slices.Contains(sessions(), shellSession) })
	secondSession := "hs-v2t-" + rid + "-" + second.ID
	if !slices.Contains(sessions(), secondSession) {
		t.Fatalf("the other terminal went too: %v", sessions())
	}
	if res := do(wire.OpConversationDelete, wire.ConversationDeleteCmd{OpID: "del-resumed", ID: rid}); res.R != wire.ResultAccepted {
		t.Fatalf("delete: %+v", res)
	}
	waitFor("the deleted conversation's terminals to end", 20*time.Second, func() bool { return !slices.Contains(sessions(), secondSession) })

	// --- designly: a project of the library and a conversation opened in its folder ---------------------------------
	if !slices.Contains(announced.Features, wire.FeatureDesignly) {
		t.Fatalf("designly is not announced: %v", announced.Features)
	}
	var designSystems wire.DesignSystems
	if code := call("GET", "/api/v2/designly/systems", nil, &designSystems); code != 200 || designSystems.Validate() != nil {
		t.Fatalf("design systems: %d %+v", code, designSystems)
	}
	if len(designSystems) == 0 || designSystems[0].ID != "test-system" || designSystems[0].Label != "Test System" || designSystems[0].Desc != "Tokens and components for the tests." {
		t.Fatalf("the template's design system is not offered: %+v", designSystems)
	}
	system := designSystems[0].ID
	model := "sonnet"
	madeProject := wire.DesignlyCreated{}
	create := wire.DesignlyCreate{Title: "Лендинг для кофейни", Brief: "Главная и меню, тёплые цвета", Kind: wire.DesignSlides, Provider: wire.ProviderClaude,
		Model: &model, DesignSystem: &system}
	if code := call("POST", "/api/v2/designly/create", create, &madeProject); code != 200 || madeProject.Validate() != nil {
		t.Fatalf("designly create: %d %+v", code, madeProject)
	}
	var library wire.DesignLibrary
	waitFor("the library to list the project with its conversation", 20*time.Second, func() bool {
		library = wire.DesignLibrary{}
		if call("GET", "/api/v2/designly/library", nil, &library) != 200 {
			return false
		}
		for _, a := range library.Artifacts {
			if a.ID == madeProject.ID {
				return slices.Contains(a.Convs, madeProject.Conv)
			}
		}
		return false
	})
	for _, a := range library.Artifacts {
		if a.ID == madeProject.ID && (a.Kind != wire.DesignSlides || a.Pinned || a.Title != create.Title) {
			t.Fatalf("the new project: %+v", a)
		}
	}
	var pinnedProject wire.DesignlyPin
	if code := call("POST", "/api/v2/designly/pin", wire.DesignlyPin{ID: madeProject.ID, Pinned: true}, &pinnedProject); code != 200 || !pinnedProject.Pinned {
		t.Fatalf("pin: %d %+v", code, pinnedProject)
	}
	var taken wire.DesignlyRefusal
	if code := call("POST", "/api/v2/designly/create", wire.DesignlyCreate{Title: "", Brief: "x", Kind: wire.DesignSite, Provider: wire.ProviderClaude}, &taken); code != http.StatusUnprocessableEntity || taken.Message == "" {
		t.Fatalf("an empty title: %d %+v", code, taken)
	}
	// The agent started in the project's folder with the library's instructions as its system prompt, and the first
	// message reached it.
	waitFor("the agent of the project to start with the instructions", 20*time.Second, func() bool {
		calls, _ := os.ReadFile(filepath.Join(home, "claude-calls.log"))
		return strings.Contains(string(calls), "--append-system-prompt-file")
	})
	if raw, err := os.ReadFile(filepath.Join(home, ".hl", "design", "library", madeProject.ID+".md")); err != nil || !strings.Contains(string(raw), create.Title) {
		t.Fatalf("the project's instructions: %v %q", err, raw)
	}
	waitFor("the project's first message to be taken", 30*time.Second, func() bool {
		var turns wire.TurnsPage
		return call("GET", "/api/v2/conversations/"+madeProject.Conv+"/turns", nil, &turns) == 200 && len(turns.Turns) > 0
	})

	// --- voice: announced where the machine can recognize; a recording comes back as text ----------------------------
	if ffmpeg == "" {
		if slices.Contains(announced.Features, wire.FeatureVoice) {
			t.Fatalf("voice is announced without ffmpeg: %v", announced.Features)
		}
		return
	}
	var withVoice wire.Hello
	if code := call("GET", "/api/v2/hello", nil, &withVoice); code != 200 || !slices.Contains(withVoice.Features, wire.FeatureVoice) {
		t.Fatalf("voice is not announced: %d %+v", code, withVoice)
	}
	tone, err := os.ReadFile(filepath.Join("..", "..", "internal", "voice", "testdata", "tone.webm"))
	if err != nil {
		t.Fatal(err)
	}
	speak := func(query string, body []byte) (int, wire.Transcript, wire.CmdResult) {
		req, _ := http.NewRequest("POST", base+"/api/v2/voice"+query, bytes.NewReader(body))
		req.Header.Set("Content-Type", "audio/webm")
		req.Header.Set("Cookie", cookie)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var heard wire.Transcript
		var refused wire.CmdResult
		_ = json.Unmarshal(raw, &heard)
		_ = json.Unmarshal(raw, &refused)
		return res.StatusCode, heard, refused
	}
	if code, heard, _ := speak("?lang=en", tone); code != 200 || heard.Text != "heard en" {
		t.Fatalf("voice: %d %+v", code, heard)
	}
	if code, _, refused := speak("", nil); code != 400 || refused.Code != wire.RefuseBadRequest {
		t.Fatalf("an empty recording: %d %+v", code, refused)
	}
}

// serveFakeASR is the recognition service of a few lines: it hears a recording, whatever it holds, and says what the
// language of the dictation was.
func serveFakeASR(t *testing.T, path string) {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReaderSize(c, 1<<20)
				lang := ""
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					var msg struct{ Cmd, Lang string }
					_ = json.Unmarshal([]byte(line), &msg)
					switch msg.Cmd {
					case "start":
						lang = msg.Lang
						_, _ = c.Write([]byte(`{"type":"ready"}` + "\n"))
					case "stop":
						_, _ = c.Write([]byte(`{"type":"final","text":"heard ` + lang + `"}` + "\n"))
						return
					}
				}
			}()
		}
	}()
}
