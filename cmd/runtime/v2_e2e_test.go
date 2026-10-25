//go:build linux

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/wire/wirefake"
)

// v2 on the real binary, standalone, with RUNTIME_V2_FAKE=1, as the page's end-to-end tests run it: sign in with
// INIT_PASSWORD, hello, one socket with its channels, a message to an idle agent, the TCP connection cut in the middle
// of its turn and the page back on its cursor, a repeated opId, a command over its budget, a restart and the entry
// cursor, the sizes of the snapshots.
func TestV2FakeRuntimeEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and runs the runtime")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "runtime")
	if out, err := exec.Command(goBin, "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(root, "home")
	_ = os.MkdirAll(home, 0o700)
	port := freePort(t)
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	logFile, _ := os.Create(filepath.Join(root, "runtime.log"))
	defer logFile.Close()
	cmd := exec.Command(bin)
	cmd.Env = []string{"HOME=" + home, "USER=agent", "PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "TMUX_TMPDIR=" + root,
		"RUNTIME_GATE_LISTEN=127.0.0.1:" + strconv.Itoa(port), "AGENT_DATA_DIR=" + filepath.Join(home, ".harnessly"),
		"AGENT_VERSION=9.9.9", "RUNTIME_TMUX_SOCKET=hs-test-v2-" + strconv.Itoa(os.Getpid()), "INIT_PASSWORD=pw-test",
		"RUNTIME_V2_FAKE=1", "RUNTIME_V2_FAKE_STEP_MS=60"}
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
		if t.Failed() {
			raw, _ := os.ReadFile(filepath.Join(root, "runtime.log"))
			t.Logf("runtime log:\n%s", tail(string(raw), 4000))
		}
		_ = exec.Command("tmux", "-L", "hs-test-v2-"+strconv.Itoa(os.Getpid()), "kill-server").Run()
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
		req.Header.Set(wire.DeviceHeader, "dev-e2e")
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
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(30 * time.Second); !ok(); time.Sleep(100 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	waitFor("the runtime", func() bool { return call("GET", "/api/v2/hello", nil, nil) == http.StatusUnauthorized })
	if code := call("POST", "/api/auth/login", map[string]string{"password": "pw-test"}, nil); code != 200 || cookie == "" {
		t.Fatalf("sign in: %d", code)
	}
	var hello wire.Hello
	if code := call("GET", "/agents/api/v2/hello?machine=local", nil, &hello); code != 200 || hello.Proto != 2 || hello.Version != "9.9.9" {
		t.Fatalf("hello: %d %+v", code, hello)
	}
	if !slices.Contains(hello.Features, wire.FeatureVoice) {
		t.Fatalf("the fake does not announce voice: %v", hello.Features)
	}
	voiceReq, _ := http.NewRequest("POST", base+"/api/v2/voice", strings.NewReader("a recording"))
	voiceReq.Header.Set("Cookie", cookie)
	voiceRes, err := client.Do(voiceReq)
	if err != nil {
		t.Fatal(err)
	}
	var said wire.Transcript
	_ = json.NewDecoder(voiceRes.Body).Decode(&said)
	voiceRes.Body.Close()
	if voiceRes.StatusCode != 200 || said.Text != v2FakeTranscript {
		t.Fatalf("voice: %d %+v", voiceRes.StatusCode, said)
	}
	// A side question streams the Legacy frames; a question the fake is told is still running is a 409.
	side := func(body string) (int, string) {
		req, _ := http.NewRequest("POST", base+"/api/v2/conversations/tab-k3Jx9QmZ/side-chat", strings.NewReader(body))
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, out := side(`{"question":"Что это?"}`); code != 200 || !strings.Contains(out, `"type":"delta"`) || !strings.Contains(out, `"type":"done"`) {
		t.Fatalf("side chat: %d %q", code, out)
	}
	if code, out := side(`{"question":"busy"}`); code != 409 || !strings.Contains(out, `"code":"busy"`) {
		t.Fatalf("side chat busy: %d %q", code, out)
	}
	var listing wire.DirListing
	if code := call("GET", "/api/v2/conversations/tab-k3Jx9QmZ/files?path=src", nil, &listing); code != 200 || len(listing.Entries) == 0 || listing.Validate() != nil {
		t.Fatalf("files: %d %+v", code, listing)
	}
	var plan wire.Tasks
	if code := call("GET", "/api/v2/conversations/tab-k3Jx9QmZ/tasks", nil, &plan); code != 200 || len(plan) == 0 || plan.Validate() != nil {
		t.Fatalf("tasks: %d %+v", code, plan)
	}
	var opened wire.FileContent
	if code := call("GET", "/api/v2/conversations/tab-k3Jx9QmZ/file?path=src/main.go", nil, &opened); code != 200 || opened.Content == "" {
		t.Fatalf("file: %d %+v", code, opened)
	}
	var me wire.Me
	if code := call("GET", "/api/v2/me", nil, &me); code != 200 || me.Rev != 1 || me.Settings.UIVersion != "next" {
		t.Fatalf("me: %d %+v", code, me)
	}
	// A session past half its age is signed again on a v2 request, as on a Legacy one.
	secret, err := agent.Config{ConfigFile: filepath.Join(home, ".harnessly", "config.json")}.LoadSecret()
	if err != nil {
		t.Fatal(err)
	}
	aging := "session-token=" + agent.SignHS256(secret, "user", "", time.Hour)
	cookie = aging
	if code := call("GET", "/api/v2/hello", nil, nil); code != 200 || cookie == aging {
		t.Fatalf("an aging session was not refreshed: %d", code)
	}
	do := func(op wire.Op, body any) wire.CmdResult {
		t.Helper()
		var res wire.CmdResult
		if code := call("POST", "/api/v2/cmd/"+string(op)+"?machine=local", body, &res); code != 200 {
			t.Fatalf("%s: %d", op, code)
		}
		return res
	}

	// --- the socket ----------------------------------------------------------------------------------------------
	type sock struct{ ws *websocket.Conn }
	sizes := map[wire.Channel]int{}
	dial := func() *sock {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(port)+"/agents/api/v2/socket?machine=local",
			&websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {cookie}}})
		if err != nil {
			t.Fatal(err)
		}
		ws.SetReadLimit(4 << 20)
		t.Cleanup(func() { _ = ws.CloseNow() })
		return &sock{ws}
	}
	send := func(s *sock, v any) {
		raw, _ := json.Marshal(v)
		_ = s.ws.Write(context.Background(), websocket.MessageText, raw)
	}
	read := func(s *sock) (wire.ServerFrame, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, raw, err := s.ws.Read(ctx)
		if err != nil {
			return nil, err
		}
		f, err := wire.DecodeServerFrame(raw)
		if err != nil {
			t.Fatalf("a frame out of contract: %v\n%s", err, raw)
		}
		if snap, ok := f.(wire.SnapshotFrame); ok {
			sizes[snap.Ch] = len(raw)
		}
		return f, nil
	}
	must := func(s *sock) wire.ServerFrame {
		t.Helper()
		f, err := read(s)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	hi := func(s *sock) wire.WelcomeFrame {
		t.Helper()
		send(s, wire.HelloFrame{Proto: 2, Device: "dev-e2e", App: "e2e"})
		w, ok := must(s).(wire.WelcomeFrame)
		if !ok {
			t.Fatal("no welcome")
		}
		return w
	}
	if _, _, err := websocket.Dial(context.Background(), "ws://127.0.0.1:"+strconv.Itoa(port)+"/api/v2/socket", nil); err == nil {
		t.Fatal("a socket without a session")
	}
	s := dial()
	w := hi(s)
	if w.Machine.ID != "local" || w.Limits.CmdBudgetMs != 10_000 {
		t.Fatalf("welcome %+v", w)
	}
	conv := wire.ConvChannel("tab-s5Qidl")
	send(s, wire.SubFrame{Ch: wire.ChannelConvs})
	send(s, wire.SubFrame{Ch: wire.ConvChannel("tab-k3Jx9QmZ")})
	send(s, wire.SubFrame{Ch: conv})
	last := map[wire.Channel]int64{}
	for len(last) < 3 {
		if snap, ok := must(s).(wire.SnapshotFrame); ok {
			last[snap.Ch] = snap.Seq
		}
	}

	// --- a message, and the connection cut in the middle of its turn ---------------------------------------------
	res := do(wire.OpSend, wire.SendCmd{OpID: "e1", ClientID: "e1", ConversationID: "tab-s5Qidl", Text: "Проверь сборку",
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	if res.R != wire.ResultQueued || res.Ch != conv {
		t.Fatalf("send %+v", res)
	}
	follow := func(s *sock, stop func(wire.Event) bool) error {
		for {
			f, err := read(s)
			if err != nil {
				return err
			}
			ev, ok := f.(wire.EvFrame)
			if !ok {
				continue
			}
			if ev.Seq != last[ev.Ch]+1 {
				t.Fatalf("%s: seq %d after %d", ev.Ch, ev.Seq, last[ev.Ch])
			}
			last[ev.Ch] = ev.Seq
			if ev.Ch == conv && stop(ev.E) {
				return nil
			}
		}
	}
	_ = follow(s, func(e wire.Event) bool { return e.EventType() == wire.EvTurnStarted })
	if code := call("POST", wirefake.ControlPrefix+"drop", nil, nil); code != 200 {
		t.Fatalf("drop: %d", code)
	}
	if err := follow(s, func(wire.Event) bool { return false }); err == nil {
		t.Fatal("the socket outlived its connection")
	}
	s = dial()
	if again := hi(s); again.Epoch != w.Epoch {
		t.Fatal("the epoch changed without a restart")
	}
	after := last[conv]
	send(s, wire.SubFrame{Ch: conv, Epoch: w.Epoch, AfterSeq: &after})
	if err := follow(s, func(e wire.Event) bool {
		p, ok := e.(wire.PromptStateEv)
		return ok && p.ClientID == "e1" && p.State.K == wire.MsgDone
	}); err != nil {
		t.Fatalf("after the resume: %v", err)
	}

	// --- the same opId, and a command past its budget ------------------------------------------------------------
	rename := wire.ConversationRenameCmd{OpID: "ren-1", ID: "tab-s5Qidl", Title: "Сборка"}
	first, second := do(wire.OpConversationRename, rename), do(wire.OpConversationRename, rename)
	if first.R != wire.ResultAccepted || second.Seq != first.Seq || *second.Rev != *first.Rev {
		t.Fatalf("a repeated opId: %+v then %+v", first, second)
	}
	if code := call("POST", wirefake.ControlPrefix+"delay", map[string]any{"op": "conversation.pin", "ms": 11_000}, nil); code != 200 {
		t.Fatalf("delay: %d", code)
	}
	pin := wire.ConversationPinCmd{OpID: "pin-1", ID: "tab-s5Qidl", Pinned: true}
	start := time.Now()
	if res := do(wire.OpConversationPin, pin); res.Code != wire.RefuseTimeout {
		t.Fatalf("over budget: %+v", res)
	}
	if took := time.Since(start); took < 9*time.Second || took > 11*time.Second {
		t.Fatalf("the timeout came after %v, the budget is 10 s", took)
	}
	if res := do(wire.OpConversationPin, pin); res.R != wire.ResultAccepted {
		t.Fatalf("the retry: %+v", res)
	}

	// --- a restart: another epoch, the page resumes on its last entry ---------------------------------------------
	var entries wire.EntriesPage
	_ = call("GET", "/api/v2/conversations/tab-k3Jx9QmZ/turns/"+strings.ReplaceAll("c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10#0", "#", "%23")+"/entries", nil, &entries)
	if len(entries.Entries) < 3 {
		t.Fatalf("entries page: %+v", entries)
	}
	cursor := entries.Entries[2].ID
	if code := call("POST", wirefake.ControlPrefix+"restart", nil, nil); code != 200 {
		t.Fatalf("restart: %d", code)
	}
	waitFor("the close of a restart", func() bool { _, err := read(s); return err != nil })
	s = dial()
	if w2 := hi(s); w2.Epoch == w.Epoch {
		t.Fatal("a restart keeps its epoch")
	}
	seq := last[wire.ConvChannel("tab-k3Jx9QmZ")]
	send(s, wire.SubFrame{Ch: wire.ConvChannel("tab-k3Jx9QmZ"), Epoch: w.Epoch, AfterSeq: &seq, AfterEntryID: cursor})
	if snap, ok := must(s).(wire.SnapshotFrame); !ok || snap.Mode != wire.SnapshotAfter || snap.Conv.Entries[0].ID != entries.Entries[3].ID {
		t.Fatalf("after a restart: %#v", snap)
	}

	// --- budgets ---------------------------------------------------------------------------------------------------
	t.Logf("the last snapshot of each channel: convs %d B, conv:tab-k3Jx9QmZ %d B, conv:tab-s5Qidl %d B", sizes[wire.ChannelConvs],
		sizes[wire.ConvChannel("tab-k3Jx9QmZ")], sizes[conv])
	if sizes[wire.ChannelConvs] > wire.ConvsBudget || sizes[wire.ConvChannel("tab-k3Jx9QmZ")] > wire.ConvBudget {
		t.Fatalf("over budget: %v", sizes)
	}

	// --- a project made on the fake machine ------------------------------------------------------------------------
	made := do(wire.OpProjectCreate, wire.ProjectCreateCmd{OpID: "prj1", Path: "/srv/new-app"})
	if made.R != wire.ResultAccepted || made.ID == nil || made.Ch != wire.ChannelConvs || made.Seq == 0 {
		t.Fatalf("project.create: %+v", made)
	}
	if res := do(wire.OpProjectCreate, wire.ProjectCreateCmd{OpID: "prj2", Path: "/srv/new-app/"}); res.ID == nil || *res.ID != *made.ID {
		t.Fatalf("the same folder again: %+v", res)
	}
}
