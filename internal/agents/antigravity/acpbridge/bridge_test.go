package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// The test binary is also a fake `agy acp`: an ACP agent that asks to run a command before it answers, and that
// waits for a cancel when asked to.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_AGY_ACP") == "1" {
		fakeAgent()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeAgent() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<24)
	var mu sync.Mutex
	write := func(v any) {
		raw, _ := json.Marshal(v)
		mu.Lock()
		os.Stdout.Write(append(raw, '\n'))
		mu.Unlock()
	}
	options := func(model, effort, mode string) []map[string]any {
		return []map[string]any{
			{"id": "model", "category": "model", "currentValue": model, "options": []map[string]string{{"value": "gemini-x", "name": "Gemini X"}, {"value": "claude-y", "name": "Claude Y"}, {"value": "oss-medium", "name": "OSS"}}},
			{"id": "thought_level", "category": "thought_level", "currentValue": effort, "options": []map[string]string{{"value": "low"}, {"value": "high"}}},
			{"id": "mode", "category": "mode", "currentValue": mode, "options": []map[string]string{{"value": "default"}, {"value": "accept-edits"}, {"value": "plan"}}},
		}
	}
	model, effort, mode := "gemini-x", "high", "default"
	answers := map[string]chan json.RawMessage{}
	cancel := make(chan struct{}, 1)
	running := 0
	for in.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(in.Bytes(), &m)
		if m.Method == "" {
			mu.Lock()
			ch := answers[string(m.ID)]
			mu.Unlock()
			if ch != nil {
				ch <- m.Result
			}
			continue
		}
		reply := func(result any) { write(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result}) }
		switch m.Method {
		case "initialize":
			reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"sessionCapabilities": map[string]any{"resume": map[string]any{}}}})
		case "session/new", "session/resume":
			reply(map[string]any{"sessionId": "11111111-2222-4333-8444-555555555555", "configOptions": options(model, effort, mode)})
		case "session/set_config_option":
			var p struct{ ConfigID, Value string }
			_ = json.Unmarshal(m.Params, &p)
			switch p.ConfigID {
			case "model":
				model = p.Value
			case "thought_level":
				effort = p.Value
			case "mode":
				mode = p.Value
			}
			reply(map[string]any{"configOptions": options(model, effort, mode)})
		case "session/cancel":
			select {
			case cancel <- struct{}{}:
			default:
			}
		case "session/prompt":
			var p struct {
				Prompt []struct{ Type, Text string } `json:"prompt"`
			}
			_ = json.Unmarshal(m.Params, &p)
			text := p.Prompt[0].Text
			id := m.ID
			running++
			first := running == 1
			go func() {
				update := func(u map[string]any) {
					write(map[string]any{"jsonrpc": "2.0", "method": "session/update", "params": map[string]any{"update": u}})
				}
				done := func(stop string) {
					write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"stopReason": stop}})
				}
				switch {
				case strings.Contains(text, "wait"):
					<-cancel
					done("cancelled")
				case strings.Contains(text, "run"):
					rid := json.RawMessage(`"perm-1"`)
					ch := make(chan json.RawMessage, 1)
					mu.Lock()
					answers[string(rid)] = ch
					mu.Unlock()
					write(map[string]any{"jsonrpc": "2.0", "id": rid, "method": "session/request_permission", "params": map[string]any{
						"toolCall": map[string]any{"toolCallId": "call-1", "name": "run_command", "kind": "execute", "title": "echo hi", "rawInput": map[string]string{"CommandLine": "echo hi"}},
						"options":  []map[string]string{{"optionId": "allow", "kind": "allow_once"}, {"optionId": "deny", "kind": "reject_once"}}}})
					res := <-ch
					update(map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "call-1", "status": "completed", "content": []any{}})
					update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "answer: " + string(res) + "\n"}})
					done("end_turn")
				default:
					if first {
						time.Sleep(200 * time.Millisecond)
					}
					update(map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]string{"type": "text", "text": "ok " + model + "\n"}})
					done("end_turn")
				}
			}()
		}
	}
}

type sink struct {
	mu     sync.Mutex
	states []State
	ended  []string
	asks   []Permission
	answer string
	lost   int
	turns  chan string
}

func (s *sink) State(_ string, st State) { s.mu.Lock(); s.states = append(s.states, st); s.mu.Unlock() }
func (s *sink) TurnStarted(string)       {}
func (s *sink) TurnEnded(_, stop, _ string) {
	s.mu.Lock()
	s.ended = append(s.ended, stop)
	s.mu.Unlock()
	s.turns <- stop
}
func (s *sink) Approval(_ context.Context, _ string, p Permission) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asks = append(s.asks, p)
	return s.answer, true
}
func (s *sink) Lost(string) { s.mu.Lock(); s.lost++; s.mu.Unlock() }

func TestBridgeOverItsSocket(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AGY_ACP", "1")
	dir, err := os.MkdirTemp("", "agyb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	_ = os.Chmod(dir, 0o700)
	snk := &sink{answer: "allow", turns: make(chan string, 8)}
	mgr := &Manager{Dir: dir, Sink: snk}
	socket := mgr.Socket("tab-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ended := make(chan error, 1)
	go func() {
		ended <- Run(ctx, Config{Agy: exe, Cwd: t.TempDir(), Socket: socket, Model: "gemini-x", Effort: "low",
			Transcript: func(id string) string { return filepath.Join("/t", id) }})
	}()
	deadline := time.Now().Add(10 * time.Second)
	for !mgr.Has("tab-1") {
		if time.Now().After(deadline) {
			t.Fatal("the bridge never listened")
		}
		time.Sleep(20 * time.Millisecond)
	}
	wait := func(want string) {
		t.Helper()
		select {
		case got := <-snk.turns:
			if got != want {
				t.Fatalf("turn ended %q, want %q", got, want)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("no turn end (%s)", want)
		}
	}
	// The hello of a bridge at rest tells its session, and that nothing runs.
	wait("idle")
	snk.mu.Lock()
	if len(snk.states) == 0 || snk.states[0].Session != "11111111-2222-4333-8444-555555555555" || snk.states[0].Effort != "low" ||
		snk.states[0].Transcript != "/t/11111111-2222-4333-8444-555555555555" {
		t.Fatalf("state on hello: %+v", snk.states)
	}
	snk.mu.Unlock()

	// A message whose tool call asks: the runtime's card answers it, and the turn ends.
	send := func(id, text string) {
		t.Helper()
		sctx, scancel := context.WithTimeout(ctx, 5*time.Second)
		defer scancel()
		if err := mgr.Send(sctx, "tab-1", Message{ID: id, Text: text}); err != nil {
			t.Fatalf("send %s: %v", id, err)
		}
	}
	send("m1", "please run it")
	wait("end_turn")
	snk.mu.Lock()
	if len(snk.asks) != 1 || snk.asks[0].Tool != "run_command" || snk.asks[0].Kind != "execute" || snk.asks[0].Title != "echo hi" {
		t.Fatalf("asks: %+v", snk.asks)
	}
	snk.mu.Unlock()
	// The same message again starts nothing.
	send("m1", "please run it")

	// A second message during a turn joins it: one turn ends, once both are answered.
	send("m2", "hello")
	send("m3", "steer")
	wait("end_turn")

	// Model, effort and mode change as the session runs; a family's only value is found by its name.
	cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
	st, err := mgr.Configure(cctx, "tab-1", "oss", "", ModeBypass)
	ccancel()
	if err != nil || st.Model != "oss-medium" || st.Mode != ModeBypass {
		t.Fatalf("configure: %+v %v", st, err)
	}
	cctx, ccancel = context.WithTimeout(ctx, 5*time.Second)
	if _, err := mgr.Configure(cctx, "tab-1", "nope", "", ""); err == nil {
		t.Fatal("an unknown model was taken")
	}
	ccancel()
	// In bypass the bridge allows by itself: the card is not asked.
	send("m4", "run again")
	wait("end_turn")
	snk.mu.Lock()
	if len(snk.asks) != 1 {
		t.Fatalf("bypass asked the person: %+v", snk.asks)
	}
	snk.mu.Unlock()

	// Stop ends a waiting turn as cancelled.
	send("m5", "wait for it")
	time.Sleep(100 * time.Millisecond)
	xctx, xcancel := context.WithTimeout(ctx, 5*time.Second)
	if err := mgr.Cancel(xctx, "tab-1"); err != nil {
		t.Fatal(err)
	}
	xcancel()
	wait("cancelled")

	cancel()
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the bridge did not end")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal("the socket was left behind")
	}
}

func TestResolveAndBox(t *testing.T) {
	o := &configOption{Options: []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	}{{Value: "a-b"}, {Value: "c-low"}, {Value: "c-high"}}}
	if v, ok := resolve(o, "a"); !ok || v != "a-b" {
		t.Fatalf("family of one: %q %v", v, ok)
	}
	if _, ok := resolve(o, "c"); ok {
		t.Fatal("a family of two is no single value")
	}
	got := boxLines("one two three", 12)
	if got[0] != "> one two" || got[1] != "  three" {
		t.Fatalf("box: %q", got)
	}
	if boxLines("", 20)[0] != ">" {
		t.Fatal("an empty box is a lone >")
	}
}
