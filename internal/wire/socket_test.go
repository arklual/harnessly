package wire_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/convo"
	"github.com/ly-solutions/harnessly/internal/wire"
	"github.com/ly-solutions/harnessly/internal/wire/wirefake"
)

// rig is a v2 server on the fake, as a standalone runtime with RUNTIME_V2_FAKE=1 serves it.
type rig struct {
	t    *testing.T
	srv  *wire.Server
	hub  *wire.Hub
	fake *wirefake.Service
	http *httptest.Server
}

func newRig(t *testing.T, step time.Duration) *rig {
	t.Helper()
	features := []wire.Feature{wire.FeatureQueue, wire.FeatureSteer, wire.FeatureQuestions}
	machine, err := wirefake.NewMachine("local", "9.9.9", features)
	if err != nil {
		t.Fatal(err)
	}
	hub := wire.NewHub(machine)
	srv := &wire.Server{Hub: hub, Machine: machine, Ops: wire.NewOpLog(""), Version: "9.9.9", Features: features,
		Limits: wire.DefaultLimits, Uploads: &wire.Uploads{Dir: t.TempDir(), Limits: wire.DefaultLimits},
		NotFound:      func(err error) bool { return err == convo.ErrNotFound },
		UnknownCursor: func(err error) bool { return err == convo.ErrUnknownCursor }}
	fake, err := wirefake.New(wirefake.Options{Hub: hub, Machine: "local", Uploads: srv.Uploads, Limits: srv.Limits, Step: step})
	if err != nil {
		t.Fatal(err)
	}
	srv.Service = fake
	srv.Terminals = fake
	srv.Designly = fake.Designly()
	srv.Sessions = func(context.Context) ([]wire.AgentSession, error) { return fake.Sessions(), nil }
	control := fake.Control(machine)
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, wirefake.ControlPrefix) {
			control.ServeHTTP(w, r)
			return
		}
		srv.ServeHTTP(w, r)
	}))
	t.Cleanup(func() {
		hub.Close()
		h.Close()
	})
	return &rig{t: t, srv: srv, hub: hub, fake: fake, http: h}
}

// do sends a request and reads its JSON answer into out; it returns the status.
func (g *rig) do(method, path string, body any, out any) int {
	g.t.Helper()
	var r io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		r = bytes.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, g.http.URL+path, r)
	req.Header.Set(wire.DeviceHeader, "dev-test")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			g.t.Fatalf("%s %s: %d %s: %v", method, path, res.StatusCode, raw, err)
		}
	}
	return res.StatusCode
}

func (g *rig) cmd(op wire.Op, body any) wire.CmdResult {
	g.t.Helper()
	var res wire.CmdResult
	if code := g.do("POST", "/api/v2/cmd/"+string(op), body, &res); code != 200 {
		g.t.Fatalf("%s: status %d", op, code)
	}
	if err := res.Validate(); err != nil {
		g.t.Fatalf("%s: %v", op, err)
	}
	return res
}

func (g *rig) control(action string, body any) {
	g.t.Helper()
	raw, _ := json.Marshal(body)
	res, err := http.Post(g.http.URL+wirefake.ControlPrefix+action, "application/json", bytes.NewReader(raw))
	if err != nil {
		g.t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		msg, _ := io.ReadAll(res.Body)
		g.t.Fatalf("control %s: %d %s", action, res.StatusCode, msg)
	}
}

// sock is one page socket; every frame it reads goes through the strict decoder.
type sock struct {
	t  *testing.T
	ws *websocket.Conn
}

func (g *rig) dial() *sock { return g.dialWith("") }

// dialWith opens a page socket whose address names caps (a comma list, "" for none).
func (g *rig) dialWith(caps string) *sock {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(g.http.URL, "http")+"/api/v2/socket?machine=local&caps="+caps, nil)
	if err != nil {
		g.t.Fatal(err)
	}
	ws.SetReadLimit(4 << 20)
	s := &sock{t: g.t, ws: ws}
	g.t.Cleanup(func() { _ = ws.CloseNow() })
	return s
}

func (s *sock) send(v any) {
	s.t.Helper()
	raw, _ := json.Marshal(v)
	if err := s.ws.Write(context.Background(), websocket.MessageText, raw); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sock) read() wire.ServerFrame {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := s.ws.Read(ctx)
	if err != nil {
		s.t.Fatalf("read: %v", err)
	}
	f, err := wire.DecodeServerFrame(raw)
	if err != nil {
		s.t.Fatalf("a frame out of contract: %v\n%s", err, raw)
	}
	return f
}

func (s *sock) hello() wire.WelcomeFrame {
	s.t.Helper()
	s.send(wire.HelloFrame{Proto: 2, Device: "dev-test", App: "test"})
	w, ok := s.read().(wire.WelcomeFrame)
	if !ok {
		s.t.Fatal("no welcome")
	}
	return w
}

func (s *sock) snapshot(ch wire.Channel) wire.SnapshotFrame {
	s.t.Helper()
	f := s.read()
	snap, ok := f.(wire.SnapshotFrame)
	if !ok || snap.Ch != ch {
		s.t.Fatalf("want a snapshot of %s, got %#v", ch, f)
	}
	return snap
}

// until reads the events of ch until one passes ok, checking that seq grows by one; it returns them all.
func (s *sock) until(ch wire.Channel, last int64, ok func(wire.Event) bool) []wire.EvFrame {
	s.t.Helper()
	var out []wire.EvFrame
	for {
		ev, isEv := s.read().(wire.EvFrame)
		if !isEv || ev.Ch != ch {
			continue
		}
		if ev.Seq != last+1 {
			s.t.Fatalf("seq %d after %d: a gap", ev.Seq, last)
		}
		last = ev.Seq
		out = append(out, ev)
		if testing.Verbose() {
			raw, _ := json.Marshal(ev.E)
			s.t.Logf("%s %d %s", ev.Ch, ev.Seq, raw)
		}
		if ok(ev.E) {
			return out
		}
	}
}

func isType(t wire.EventType) func(wire.Event) bool {
	return func(e wire.Event) bool { return e.EventType() == t }
}

func TestSocketWelcomesAndServesSnapshots(t *testing.T) {
	g := newRig(t, time.Hour)
	s := g.dial()
	w := s.hello()
	if w.Epoch != g.hub.Epoch() || w.Machine.ID != "local" || w.Limits != wire.DefaultLimits || len(w.Features) != 3 {
		t.Fatalf("welcome %+v", w)
	}
	s.send(wire.SubFrame{Ch: wire.ChannelConvs})
	if snap := s.snapshot(wire.ChannelConvs); len(snap.Convs.Conversations) != 10 || snap.Seq != 0 || snap.Mode != wire.SnapshotReplace {
		t.Fatalf("convs %d seq %d", len(snap.Convs.Conversations), snap.Seq)
	}
	s.send(wire.SubFrame{Ch: wire.ConvChannel("tab-k3Jx9QmZ")})
	if snap := s.snapshot(wire.ConvChannel("tab-k3Jx9QmZ")); len(snap.Conv.Turns) != 3 || snap.Conv.Now == nil {
		t.Fatalf("conv %+v", snap.Conv)
	}
	yes := true
	s.send(wire.SubFrame{Ch: wire.ChannelMachine, Opts: &wire.SubOpts{Load: &yes}})
	if snap := s.snapshot(wire.ChannelMachine); snap.Machine.Machine.Load == nil {
		t.Fatal("a machine snapshot asked with load carries it")
	}
	s.send(wire.SubFrame{Ch: wire.ConvChannel("tab-nothere")})
	if e, ok := s.read().(wire.ErrorFrame); !ok || e.Code != wire.FrameErrNotFound || e.Ch != wire.ConvChannel("tab-nothere") {
		t.Fatalf("an unknown conversation: %#v", e)
	}
	s.send(map[string]any{"f": "sub", "ch": "conv:nope"})
	if e, ok := s.read().(wire.ErrorFrame); !ok || e.Code != wire.FrameErrBadChannel {
		t.Fatalf("a bad channel: %#v", e)
	}
	s.send(map[string]any{"f": "dance"})
	if e, ok := s.read().(wire.ErrorFrame); !ok || e.Code != wire.FrameErrProto {
		t.Fatalf("an unknown frame: %#v", e)
	}
	s.send(wire.PingFrame{T: 42})
	for {
		f := s.read()
		if p, ok := f.(wire.PongFrame); ok {
			if p.T != 42 {
				t.Fatalf("pong %d", p.T)
			}
			break
		}
		if ev, ok := f.(wire.EvFrame); !ok || ev.E.EventType() != wire.EvMachineLoad {
			t.Fatalf("before the pong: %#v", f)
		}
	}
}

func TestSocketWantsHelloFirstAndClosesWhenIdle(t *testing.T) {
	g := newRig(t, time.Hour)
	s := g.dial()
	s.send(wire.PingFrame{T: 1})
	if e, ok := s.read().(wire.ErrorFrame); !ok || e.Code != wire.FrameErrProto {
		t.Fatalf("a socket that does not say hello: %#v", e)
	}
	restore := wire.SetSocketIdle(300 * time.Millisecond)
	defer restore()
	s = g.dial()
	s.hello()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	if _, _, err := s.ws.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("a silent socket stays open: %v", err)
	}
	if took := time.Since(start); took < 250*time.Millisecond {
		t.Fatalf("closed after %v", took)
	}
}

// A message to an idle agent: queued, typed, accepted, its turn, done — every state on the socket, seqs without
// gaps, the command's seq among them.
func TestSendPlaysTheWholeWay(t *testing.T) {
	g := newRig(t, 20*time.Millisecond)
	s := g.dial()
	s.hello()
	ch := wire.ConvChannel("tab-s5Qidl")
	s.send(wire.SubFrame{Ch: ch})
	snap := s.snapshot(ch)
	res := g.cmd(wire.OpSend, wire.SendCmd{OpID: "m1", ClientID: "m1", ConversationID: "tab-s5Qidl", Text: "Привет",
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	if res.R != wire.ResultQueued || res.Ch != ch || res.Seq < 1 {
		t.Fatalf("send %+v", res)
	}
	evs := s.until(ch, snap.Seq, func(e wire.Event) bool {
		p, ok := e.(wire.PromptStateEv)
		return ok && p.State.K == wire.MsgDone
	})
	var states []string
	seenSeq := false
	for _, ev := range evs {
		seenSeq = seenSeq || ev.Seq == res.Seq
		if p, ok := ev.E.(wire.PromptStateEv); ok {
			states = append(states, string(p.State.K))
		}
	}
	if got := strings.Join(states, " "); got != "typing in-input accepted in-turn done" {
		t.Fatalf("states %q", got)
	}
	if !seenSeq {
		t.Fatal("the command's seq is not among the events")
	}
	conv, _ := g.fake.Conversation(context.Background(), "tab-s5Qidl")
	last := conv.Turns[len(conv.Turns)-1]
	if last.Status != wire.TurnDone || last.AnswerEntryID == nil {
		t.Fatalf("turn %+v", last)
	}
}

// The network goes away mid-stream: the page comes back on a new socket with its epoch and seq, and gets exactly
// the events it missed, no gap, and then a snapshot "after" them that adds no turns or entries: it has caught up.
func TestResumeAfterTheConnectionDrops(t *testing.T) {
	g := newRig(t, 30*time.Millisecond)
	s := g.dial()
	s.hello()
	ch := wire.ConvChannel("tab-s4Qrea")
	s.send(wire.SubFrame{Ch: ch})
	snap := s.snapshot(ch)
	g.cmd(wire.OpSend, wire.SendCmd{OpID: "r1", ClientID: "r1", ConversationID: "tab-s4Qrea", Text: "Раз",
		AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	got := s.until(ch, snap.Seq, isType(wire.EvTurnStarted))
	last := got[len(got)-1].Seq
	epoch := g.hub.Epoch()
	_ = s.ws.CloseNow() // TCP gone, no close frame
	time.Sleep(100 * time.Millisecond)

	s = g.dial()
	if w := s.hello(); w.Epoch != epoch {
		t.Fatal("the same runtime keeps its epoch")
	}
	s.send(wire.SubFrame{Ch: ch, Epoch: epoch, AfterSeq: &last})
	var rest []wire.EvFrame
	for {
		f := s.read()
		if ev, ok := f.(wire.EvFrame); ok && ev.Ch == ch {
			if ev.Seq != last+1 {
				t.Fatalf("seq %d after %d: a gap", ev.Seq, last)
			}
			last = ev.Seq
			rest = append(rest, ev)
			continue
		}
		snap, ok := f.(wire.SnapshotFrame)
		if !ok || snap.Ch != ch {
			continue
		}
		if len(rest) == 0 {
			t.Fatal("nothing replayed before the snapshot")
		}
		if snap.Mode != wire.SnapshotAfter || snap.Seq != last || len(snap.Conv.Turns) != 0 || len(snap.Conv.Entries) != 0 {
			t.Fatalf("after the replay: %s seq %d (last %d), %d turns, %d entries", snap.Mode, snap.Seq, last,
				len(snap.Conv.Turns), len(snap.Conv.Entries))
		}
		break
	}
}

// A page that comes back with nothing missed gets no events, and a snapshot "after" its cursor all the same: without
// it, it could not tell a replay of nothing from one still on its way, and would show its cached feed as complete.
func TestResumeWithNothingMissedSaysCaughtUp(t *testing.T) {
	g := newRig(t, time.Hour)
	s := g.dial()
	s.hello()
	ch := wire.ConvChannel("tab-s4Qrea")
	s.send(wire.SubFrame{Ch: ch})
	snap := s.snapshot(ch)
	epoch := g.hub.Epoch()
	s.send(wire.UnsubFrame{Ch: ch})
	seq := snap.Seq
	s.send(wire.SubFrame{Ch: ch, Epoch: epoch, AfterSeq: &seq})
	again := s.snapshot(ch)
	if again.Mode != wire.SnapshotAfter || again.Seq != seq || len(again.Conv.Turns) != 0 || len(again.Conv.Entries) != 0 {
		t.Fatalf("resume: %s seq %d (cursor %d), %d turns, %d entries", again.Mode, again.Seq, seq, len(again.Conv.Turns),
			len(again.Conv.Entries))
	}
	if again.Conv.Queue.Rev != snap.Conv.Queue.Rev {
		t.Fatalf("queue rev %d, the first snapshot had %d", again.Conv.Queue.Rev, snap.Conv.Queue.Rev)
	}
}

// After a restart (another epoch) the page asks for what follows its last entry: a snapshot "after"; with a cursor
// the transcript does not have, a whole one.
func TestResumeAfterARestart(t *testing.T) {
	g := newRig(t, time.Hour)
	ch := wire.ConvChannel("tab-k3Jx9QmZ")
	old := g.hub.Epoch()
	g.control("restart", nil)
	s := g.dial()
	if w := s.hello(); w.Epoch == old {
		t.Fatal("a restart is a new epoch")
	}
	seq := int64(412)
	s.send(wire.SubFrame{Ch: ch, Epoch: old, AfterSeq: &seq, AfterEntryID: "c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f12#0"})
	snap := s.snapshot(ch)
	if snap.Mode != wire.SnapshotAfter || len(snap.Conv.Entries) == 0 || snap.Conv.Entries[0].ID != "c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f13#1" {
		t.Fatalf("after: %s %+v", snap.Mode, snap.Conv.Entries)
	}
	s.send(wire.SubFrame{Ch: ch, Epoch: old, AfterSeq: &seq, AfterEntryID: "c:99999999-0000-4000-8000-000000000000#0"})
	if snap := s.snapshot(ch); snap.Mode != wire.SnapshotReplace || len(snap.Conv.Turns) != 3 {
		t.Fatalf("unknown cursor: %s", snap.Mode)
	}
}

func TestTooManySubscriptions(t *testing.T) {
	g := newRig(t, time.Hour)
	s := g.dial()
	s.hello()
	var ids []string
	for i := 0; i < wire.MaxSubs; i++ {
		res := g.cmd(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: fmt.Sprint("c", i), Cwd: "/tmp", Provider: wire.ProviderClaude})
		ids = append(ids, *res.ID)
	}
	for _, id := range ids {
		s.send(wire.SubFrame{Ch: wire.ConvChannel(id)})
		s.snapshot(wire.ConvChannel(id))
	}
	s.send(wire.SubFrame{Ch: wire.ChannelConvs})
	if e, ok := s.read().(wire.ErrorFrame); !ok || e.Code != wire.FrameErrTooManySubs {
		t.Fatalf("subscription %d: %#v", wire.MaxSubs+1, e)
	}
}

// --- commands ----------------------------------------------------------------------------------------------------

func TestCommandsAreIdempotentByOpID(t *testing.T) {
	g := newRig(t, time.Hour)
	rename := wire.ConversationRenameCmd{OpID: "op-1", ID: "tab-s4Qrea", Title: "Новое имя"}
	first := g.cmd(wire.OpConversationRename, rename)
	again := g.cmd(wire.OpConversationRename, rename)
	if first.R != wire.ResultAccepted || !same(again, first) {
		t.Fatalf("first %+v, again %+v", first, again)
	}
	rename.Title = "Другое"
	if res := g.cmd(wire.OpConversationRename, rename); res.R != wire.ResultRefused || res.Code != wire.RefuseConflict {
		t.Fatalf("another body: %+v", res)
	}
	// While the first runs, a repeat waits for its answer: one effect.
	g.control("delay", map[string]any{"op": "conversation.pin", "ms": 300})
	pin := wire.ConversationPinCmd{OpID: "op-2", ID: "tab-s4Qrea", Pinned: true}
	var wg sync.WaitGroup
	results := make([]wire.CmdResult, 3)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = g.cmd(wire.OpConversationPin, pin)
		}()
	}
	wg.Wait()
	for _, r := range results[1:] {
		if !same(r, results[0]) || r.R != wire.ResultAccepted {
			t.Fatalf("results %+v", results)
		}
	}
	if *results[0].Rev != *first.Rev+1 {
		t.Fatalf("the pin ran %d times", *results[0].Rev-*first.Rev)
	}
}

func TestCommandsEndWithinTheBudget(t *testing.T) {
	g := newRig(t, time.Hour)
	g.srv.Limits.CmdBudgetMs = 300
	g.control("delay", map[string]any{"op": "conversation.rename", "ms": 2000})
	rename := wire.ConversationRenameCmd{OpID: "slow-1", ID: "tab-s4Qrea", Title: "Медленно"}
	start := time.Now()
	res := g.cmd(wire.OpConversationRename, rename)
	took := time.Since(start)
	if res.R != wire.ResultRefused || res.Code != wire.RefuseTimeout {
		t.Fatalf("a slow command: %+v", res)
	}
	if took < 250*time.Millisecond || took > time.Second {
		t.Fatalf("answered after %v, the budget is 300 ms", took)
	}
	time.Sleep(50 * time.Millisecond)
	if res := g.cmd(wire.OpConversationRename, rename); res.R != wire.ResultAccepted {
		t.Fatalf("the retry of a timed-out command: %+v", res)
	}
}

func TestCommandsRefuseWhatIsWrong(t *testing.T) {
	g := newRig(t, time.Hour)
	for name, tc := range map[string]struct {
		op   wire.Op
		body any
		want wire.RefuseCode
	}{
		"unknown op":      {"dance", map[string]string{"opId": "x"}, wire.RefuseBadRequest},
		"unknown field":   {wire.OpSeen, map[string]any{"opId": "x", "conversationId": "all", "upTo": 1, "x": 1}, wire.RefuseBadRequest},
		"send ≠ clientId": {wire.OpSend, wire.SendCmd{OpID: "a", ClientID: "b", ConversationID: "tab-s4Qrea", Text: "x", AttachmentIDs: []string{}, Mode: "queue", Kind: "prompt"}, wire.RefuseBadRequest},
		"no conversation": {wire.OpConversationRename, wire.ConversationRenameCmd{OpID: "n1", ID: "tab-none", Title: "x"}, wire.RefuseNotFound},
		"gone":            {wire.OpSend, wire.SendCmd{OpID: "g1", ClientID: "g1", ConversationID: "tab-s8Qgon", Text: "x", AttachmentIDs: []string{}, Mode: "queue", Kind: "prompt"}, wire.RefuseAgentGone},
		"attachment":      {wire.OpSend, wire.SendCmd{OpID: "a1", ClientID: "a1", ConversationID: "tab-s4Qrea", Text: "x", AttachmentIDs: []string{"up_NOPE"}, Mode: "queue", Kind: "prompt"}, wire.RefuseAttachmentMissing},
		"not working":     {wire.OpInterrupt, wire.InterruptCmd{OpID: "i1", ConversationID: "tab-s4Qrea"}, wire.RefuseNotWorking},
		"no codex":        {wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "cx", Cwd: "/tmp", Provider: wire.ProviderCodex}, wire.RefuseUnsupported},
		"both resumes":    {wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "r1", ID: "tab-k3Jx9QmZ", SessionID: "00000005-0000-4000-8000-000000000000"}, wire.RefuseBadRequest},
		"session path":    {wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "r2", Provider: wire.ProviderClaude, SessionID: "../x", Cwd: "/tmp"}, wire.RefuseBadRequest},
		"session cwd":     {wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "r3", Provider: wire.ProviderClaude, SessionID: "00000005-0000-4000-8000-000000000000", Cwd: "tmp"}, wire.RefuseBadRequest},
		"no session":      {wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "r4", Provider: wire.ProviderClaude, SessionID: "00000005-0000-4000-8000-000000000000", Cwd: "/tmp"}, wire.RefuseNotFound},
		"option unknown":  {wire.OpDecide, wire.DecideCmd{OpID: "d1", ConversationID: "tab-s3Qwai", PermissionID: "p:tab-k3Jx9QmZ:41", OptionID: "9"}, wire.RefuseOptionUnknown},
		"working, delete": {wire.OpConversationDelete, wire.ConversationDeleteCmd{OpID: "del", ID: "tab-k3Jx9QmZ"}, wire.RefuseAgentWorking},
	} {
		if res := g.cmd(tc.op, tc.body); res.R != wire.ResultRefused || res.Code != tc.want {
			t.Errorf("%s: %+v, want %s", name, res, tc.want)
		}
	}
	// No device header: bad-request too.
	req, _ := http.NewRequest("POST", g.http.URL+"/api/v2/cmd/seen", strings.NewReader(`{"opId":"s","conversationId":"all","upTo":1}`))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(raw), "bad-request") {
		t.Fatalf("no device: %d %s", res.StatusCode, raw)
	}
}

// A question answered on one device shows as answered, with that device, on every socket.
func TestAnswerReachesEverySocket(t *testing.T) {
	g := newRig(t, 20*time.Millisecond)
	ch := wire.ConvChannel("tab-s2Qwai")
	a, b := g.dial(), g.dial()
	var seqs [2]int64
	for i, s := range []*sock{a, b} {
		s.hello()
		s.send(wire.SubFrame{Ch: ch})
		seqs[i] = s.snapshot(ch).Seq
	}
	res := g.cmd(wire.OpAnswer, wire.AnswerCmd{OpID: "ans", ConversationID: "tab-s2Qwai", QuestionID: "q:toolu_01AskQ",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{0}}}})
	if res.R != wire.ResultAccepted {
		t.Fatalf("answer %+v", res)
	}
	for i, s := range []*sock{a, b} {
		evs := s.until(ch, seqs[i], func(e wire.Event) bool {
			u, ok := e.(wire.GateUpdatedEv)
			return ok && u.State.K == wire.GateStateAnswered
		})
		if u := evs[len(evs)-1].E.(wire.GateUpdatedEv); u.State.ByDevice != "dev-test" {
			t.Fatalf("answered by %q", u.State.ByDevice)
		}
	}
	if again := g.cmd(wire.OpAnswer, wire.AnswerCmd{OpID: "ans-2", ConversationID: "tab-s2Qwai", QuestionID: "q:toolu_01AskQ",
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{1}}}}); again.Code != wire.RefuseGateAnswered {
		t.Fatalf("a second answer: %+v", again)
	}
}

// --- pages -------------------------------------------------------------------------------------------------------

func TestPagesUpTheFeed(t *testing.T) {
	g := newRig(t, time.Hour)
	base := "/api/v2/conversations/tab-k3Jx9QmZ"
	var page wire.TurnsPage
	if code := g.do("GET", base+"/turns", nil, &page); code != 200 || len(page.Turns) != wire.TurnsPageDefault || page.BeforeTurnID == nil {
		t.Fatalf("first page: %d %d turns", code, len(page.Turns))
	}
	answered := 0
	for _, turn := range page.Turns {
		if turn.AnswerEntryID != nil {
			answered++
		}
	}
	if len(page.Entries) != answered {
		t.Fatalf("every answered turn comes with its answer: %d entries, %d answered", len(page.Entries), answered)
	}
	var older wire.TurnsPage
	if code := g.do("GET", base+"/turns?limit=100&before="+url.QueryEscape(*page.BeforeTurnID), nil, &older); code != 200 ||
		len(older.Turns) != 43-30 || older.BeforeTurnID != nil {
		t.Fatalf("older page: %d %d turns, before %v", code, len(older.Turns), older.BeforeTurnID)
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=x", "before=nope"} {
		var r wire.CmdResult
		if code := g.do("GET", base+"/turns?"+q, nil, &r); code != 400 || r.Code != wire.RefuseBadRequest {
			t.Errorf("%s: %d %+v", q, code, r)
		}
	}
	var r wire.CmdResult
	if code := g.do("GET", base+"/turns?before="+url.QueryEscape("c:99999999-0000-4000-8000-000000000000#0"), nil, &r); code != 404 {
		t.Fatalf("a turn it does not have: %d", code)
	}
	live := url.PathEscape("c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f10#0")
	var entries wire.EntriesPage
	if code := g.do("GET", base+"/turns/"+live+"/entries", nil, &entries); code != 200 || len(entries.Entries) < 5 {
		t.Fatalf("entries: %d %d", code, len(entries.Entries))
	}
	var out wire.OutputPage
	if code := g.do("GET", base+"/entries/"+url.PathEscape("c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f12#0")+"/output", nil, &out); code != 200 ||
		len(out.Output.Text) < 30000 || out.Output.Truncated != nil {
		t.Fatalf("output: %d %d B", code, len(out.Output.Text))
	}
	if code := g.do("GET", base+"/entries/"+url.PathEscape("c:3f1d2a9e-7c41-4b8e-9a51-0d6c2b7e8f11#0")+"/output", nil, &r); code != 404 {
		t.Fatalf("the output of a thought: %d", code)
	}
	if code := g.do("GET", "/api/v2/conversations/nope/turns", nil, &r); code != 400 {
		t.Fatalf("a bad id: %d", code)
	}
	var fleet []wire.Machine
	if code := g.do("GET", "/api/v2/machines?machine=local", nil, &fleet); code != 200 || len(fleet) != 1 || fleet[0].ID != "local" ||
		fleet[0].Reach != wire.ReachOnline || fleet[0].Runtime == nil {
		t.Fatalf("machines: %d %+v", code, fleet)
	}
	var hello wire.Hello
	if code := g.do("GET", "/agents/api/v2/hello", nil, &hello); code != 200 || hello.Proto != 2 || hello.Version != "9.9.9" {
		t.Fatalf("hello under /agents: %d %+v", code, hello)
	}
}

func TestUploadsAndTheMessageThatCarriesThem(t *testing.T) {
	g := newRig(t, time.Hour)
	put := func(name, typ string, body []byte) (int, wire.AttachmentRef, wire.CmdResult) {
		req, _ := http.NewRequest("PUT", g.http.URL+"/api/v2/uploads?name="+url.QueryEscape(name), bytes.NewReader(body))
		if typ != "" {
			req.Header.Set("Content-Type", typ)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		var ref wire.AttachmentRef
		var refusal wire.CmdResult
		if res.StatusCode == 200 {
			_ = json.Unmarshal(raw, &ref)
		} else {
			_ = json.Unmarshal(raw, &refusal)
		}
		return res.StatusCode, ref, refusal
	}
	code, img, _ := put("shot.png", "image/png", []byte("\x89PNG...."))
	if code != 200 || img.Kind != wire.AttachmentImage || img.Mime != "image/png" || !strings.HasPrefix(img.ID, "up_") {
		t.Fatalf("image: %d %+v", code, img)
	}
	code, file, _ := put("../../etc/trace.zip", "application/zip; charset=binary", []byte("PK"))
	if code != 200 || file.Kind != wire.AttachmentFile || file.Name != "trace.zip" {
		t.Fatalf("file: %d %+v", code, file)
	}
	g.srv.Uploads.Limits.MaxImageBytes = 4
	if code, _, r := put("big.png", "image/png", []byte("0123456789")); code != 413 || r.Code != wire.RefuseAttachmentTooLarge {
		t.Fatalf("too large: %d %+v", code, r)
	}
	if code, _, r := put("", "image/png", []byte("x")); code != 400 || r.Code != wire.RefuseBadRequest {
		t.Fatalf("no name: %d %+v", code, r)
	}
	if code, _, r := put("x", "not a type", []byte("x")); code != 415 || r.Code != wire.RefuseAttachmentType {
		t.Fatalf("bad type: %d %+v", code, r)
	}
	get := func(id string) (int, http.Header, string) {
		res, err := http.Get(g.http.URL + "/api/v2/uploads/" + id)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(raw)
	}
	if code, h, body := get(img.ID); code != 200 || h.Get("Content-Type") != "image/png" || body != "\x89PNG...." ||
		h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("the image back: %d %v %q", code, h, body)
	}
	if code, h, _ := get(file.ID); code != 200 || h.Get("Content-Type") != "application/octet-stream" ||
		!strings.HasPrefix(h.Get("Content-Disposition"), "attachment") {
		t.Fatalf("the file back: %d %v", code, h)
	}
	if _, path, ok := g.srv.Uploads.Get(img.ID); !ok || !strings.HasSuffix(path, img.ID+".png") {
		t.Fatalf("a picture's file must carry its extension, Claude takes a bare path as text: %q", path)
	}
	if _, path, ok := g.srv.Uploads.Get(file.ID); !ok || !strings.HasSuffix(path, file.ID+".zip") {
		t.Fatalf("a file keeps its name's extension: %q", path)
	}
	for _, id := range []string{"up_NOPE", "..%2F..%2Fetc%2Fpasswd", "up_a.json"} {
		if code, _, _ := get(id); code != 404 {
			t.Fatalf("%s: %d", id, code)
		}
	}
	res := g.cmd(wire.OpSend, wire.SendCmd{OpID: "att", ClientID: "att", ConversationID: "tab-k3Jx9QmZ", Text: "Смотри",
		AttachmentIDs: []string{img.ID, file.ID}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	if res.R != wire.ResultQueued {
		t.Fatalf("send with attachments: %+v", res)
	}
	snap, _ := g.fake.Conversation(context.Background(), "tab-k3Jx9QmZ")
	for _, m := range snap.Messages {
		if m.ClientID == "att" && (len(m.Attachments) != 2 || m.State.Position != 3 || m.State.Reason != wire.QueuedBehind) {
			t.Fatalf("message %+v", m)
		}
	}
}

func same(a, b wire.CmdResult) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// The scripted agent's scenarios, every frame through the strict decoder: a question, a failure, Stop with the
// queue paused and resumed, a launch that fails.
func TestFakeScenarios(t *testing.T) {
	g := newRig(t, 10*time.Millisecond)
	s := g.dial()
	s.hello()
	ch := wire.ConvChannel("tab-s5Qidl")
	s.send(wire.SubFrame{Ch: ch})
	last := s.snapshot(ch).Seq
	send := func(id, text string) wire.CmdResult {
		return g.cmd(wire.OpSend, wire.SendCmd{OpID: id, ClientID: id, ConversationID: "tab-s5Qidl", Text: text,
			AttachmentIDs: []string{}, Mode: wire.ModeQueue, Kind: wire.KindPrompt})
	}
	upTo := func(ok func(wire.Event) bool) wire.Event {
		evs := s.until(ch, last, ok)
		last = evs[len(evs)-1].Seq
		return evs[len(evs)-1].E
	}

	send("q1", "Спроси меня "+wirefake.MarkQuestion)
	q := upTo(isType(wire.EvQuestionAsked)).(wire.QuestionAskedEv).Question
	if res := g.cmd(wire.OpAnswer, wire.AnswerCmd{OpID: "a1", ConversationID: "tab-s5Qidl", QuestionID: q.ID,
		Answers: []wire.QuestionAnswer{{I: 0, Choices: []int{0}}}}); res.R != wire.ResultAccepted {
		t.Fatalf("answer %+v", res)
	}
	if f := upTo(isType(wire.EvTurnFinished)).(wire.TurnFinishedEv); f.Turn.Status != wire.TurnDone {
		t.Fatalf("after the answer: %s", f.Turn.Status)
	}

	send("f1", "Упади "+wirefake.MarkFail)
	if f := upTo(isType(wire.EvTurnFailed)).(wire.TurnFailedEv); f.Turn.Failure.Code != wire.TurnFailedRateLimit {
		t.Fatalf("failure %+v", f.Turn.Failure)
	}

	send("w1", "Работай "+wirefake.MarkWork)
	upTo(func(e wire.Event) bool { st, ok := e.(wire.StepEv); return ok && st.Entry.T == wire.EntryTool })
	send("w2", "Следующее")
	if m := upTo(func(e wire.Event) bool { m, ok := e.(wire.MessageUpsertEv); return ok && m.Message.ClientID == "w2" }).(wire.MessageUpsertEv); m.Message.State.Reason != wire.QueuedAgentBusy {
		t.Fatalf("behind a working agent: %+v", m.Message.State)
	}
	// The queue's order: w3 goes before w2, and what is not in the line is refused.
	send("w3", "Третье")
	before := "w2"
	if res := g.cmd(wire.OpQueueMove, wire.QueueMoveCmd{OpID: "mv", ConversationID: "tab-s5Qidl", ClientID: "w3", BeforeClientID: &before}); res.R != wire.ResultAccepted {
		t.Fatalf("queue.move %+v", res)
	}
	upTo(func(e wire.Event) bool {
		q, ok := e.(wire.QueueChangedEv)
		return ok && slices.Equal(q.Queue.Items, []string{"w3", "w2"})
	})
	if res := g.cmd(wire.OpQueueMove, wire.QueueMoveCmd{OpID: "mv2", ConversationID: "tab-s5Qidl", ClientID: "w1"}); res.R != wire.ResultRefused || res.Code != wire.RefuseTooLate {
		t.Fatalf("a message that is past the line: %+v", res)
	}
	if res := g.cmd(wire.OpCancel, wire.CancelCmd{OpID: "c3", ConversationID: "tab-s5Qidl", ClientID: "w3"}); res.R != wire.ResultAccepted {
		t.Fatalf("cancel %+v", res)
	}
	if res := g.cmd(wire.OpInterrupt, wire.InterruptCmd{OpID: "stop", ConversationID: "tab-s5Qidl"}); res.R != wire.ResultAccepted {
		t.Fatalf("interrupt %+v", res)
	}
	if f := upTo(isType(wire.EvTurnFinished)).(wire.TurnFinishedEv); f.Turn.Status != wire.TurnInterrupted {
		t.Fatalf("stopped turn: %s", f.Turn.Status)
	}
	snap, _ := g.fake.Conversation(context.Background(), "tab-s5Qidl")
	if snap.Queue.Paused == nil || snap.Queue.Paused.Reason != wire.PausedInterrupted || len(snap.Queue.Items) != 1 {
		t.Fatalf("after Stop the queue waits: %+v", snap.Queue)
	}
	g.cmd(wire.OpQueueResume, wire.QueueResumeCmd{OpID: "go", ConversationID: "tab-s5Qidl"})
	upTo(func(e wire.Event) bool {
		p, ok := e.(wire.PromptStateEv)
		return ok && p.ClientID == "w2" && p.State.K == wire.MsgDone
	})

	s.send(wire.SubFrame{Ch: wire.ChannelConvs})
	cs := s.snapshot(wire.ChannelConvs).Seq
	res := g.cmd(wire.OpConversationCreate, wire.ConversationCreateCmd{OpID: "new", Cwd: "/tmp", Provider: wire.ProviderClaude,
		First: &wire.FirstMessage{ClientID: "first", Text: "Начни " + wirefake.MarkLaunchFail, AttachmentIDs: []string{}}})
	s.until(wire.ChannelConvs, cs, func(e wire.Event) bool {
		st, ok := e.(wire.ConversationStateEv)
		return ok && st.ID == *res.ID && st.State.K == wire.StateLaunchFailed
	})
}

// slowConvs holds the list of conversations until wait is closed: a busy runtime.
type slowConvs struct {
	wire.Service
	wait chan struct{}
}

func (s slowConvs) Conversations(ctx context.Context) (wire.ConvsSnapshot, error) {
	select {
	case <-s.wait:
	case <-ctx.Done():
	}
	return s.Service.Conversations(ctx)
}

// §12: the socket's heartbeat does not wait for a snapshot. A ping sent behind a subscription whose snapshot is slow is
// answered at once, and the snapshot follows.
func TestAPingIsAnsweredWhileASnapshotWaits(t *testing.T) {
	g := newRig(t, time.Hour)
	wait := make(chan struct{})
	g.srv.Service = slowConvs{Service: g.srv.Service, wait: wait}
	s := g.dial()
	s.hello()
	s.send(wire.SubFrame{Ch: wire.ChannelConvs})
	s.send(wire.PingFrame{T: 7})
	start := time.Now()
	if p, ok := s.read().(wire.PongFrame); !ok || p.T != 7 || time.Since(start) > time.Second {
		t.Fatalf("the pong behind a slow snapshot: %#v after %v", p, time.Since(start))
	}
	close(wait)
	if snap := s.snapshot(wire.ChannelConvs); len(snap.Convs.Conversations) != 10 {
		t.Fatalf("convs %d", len(snap.Convs.Conversations))
	}
}

// A subscription queued behind a slow snapshot does not outlive its socket: the socket closes right after the frames,
// and the hub holds no subscription and no demand for the machine once it has let the socket go.
func TestASubscriptionDoesNotOutliveItsSocket(t *testing.T) {
	for range 20 {
		g := newRig(t, time.Hour)
		wait := make(chan struct{})
		g.srv.Service = slowConvs{Service: g.srv.Service, wait: wait}
		s := g.dial()
		s.hello()
		s.send(wire.SubFrame{Ch: wire.ChannelConvs})
		s.send(wire.SubFrame{Ch: wire.ChannelMachine})
		s.send(wire.SubFrame{Ch: wire.ConvChannel("tab-k3Jx9QmZ")})
		_ = s.ws.CloseNow()
		deadline := time.Now().Add(5 * time.Second)
		for g.hub.Clients() != 0 {
			if time.Now().After(deadline) {
				t.Fatal("the hub still holds the socket")
			}
			time.Sleep(5 * time.Millisecond)
		}
		close(wait)
		time.Sleep(20 * time.Millisecond) // anything left in the socket's queue would run about now
		if n, d := g.hub.Subscriptions(), g.hub.MachineDemand(); n != 0 || d != 0 {
			t.Fatalf("after the socket: %d subscriptions, %d machine demands", n, d)
		}
	}
}

// The sessions page lists what the machine's agent kept outside any conversation; resuming one opens a conversation and
// takes it off the list.
func TestSessionsPageAndResume(t *testing.T) {
	g := newRig(t, time.Hour)
	var list wire.AgentSessions
	if code := g.do("GET", "/api/v2/sessions", nil, &list); code != 200 || len(list) != 2 {
		t.Fatalf("sessions: %d %+v", code, list)
	}
	if err := list.Validate(); err != nil {
		t.Fatal(err)
	}
	first := list[0]
	res := g.cmd(wire.OpConversationResume, wire.ConversationResumeCmd{OpID: "rs1", Provider: first.Provider, SessionID: first.SessionID, Cwd: first.Cwd})
	if res.R != wire.ResultAccepted || res.ID == nil {
		t.Fatalf("resume: %+v", res)
	}
	var after wire.AgentSessions
	if code := g.do("GET", "/api/v2/sessions", nil, &after); code != 200 || len(after) != 1 || after[0].SessionID == first.SessionID {
		t.Fatalf("after: %d %+v", code, after)
	}
	if code := g.do("POST", "/api/v2/sessions", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", code)
	}
}

// A conversation's own terminals: made, listed oldest first, ended; the socket speaks the terminal protocol; what is not
// there is a 404, a ninth is a 409.
func TestTerminalsPagesAndSocket(t *testing.T) {
	g := newRig(t, time.Hour)
	const conv = "tab-k3Jx9QmZ"
	page := "/api/v2/conversations/" + conv + "/terminals"
	var none wire.Terminals
	if code := g.do("GET", page, nil, &none); code != 200 || none == nil || len(none) != 0 {
		t.Fatalf("no terminals: %d %+v", code, none)
	}
	var first, second wire.Terminal
	if code := g.do("POST", page, nil, &first); code != 200 || first.Validate() != nil {
		t.Fatalf("create: %d %+v", code, first)
	}
	if code := g.do("POST", page, nil, &second); code != 200 || second.ID == first.ID {
		t.Fatalf("create again: %d %+v", code, second)
	}
	var list wire.Terminals
	if code := g.do("GET", page, nil, &list); code != 200 || len(list) != 2 || list[0].ID != first.ID || list.Validate() != nil {
		t.Fatalf("list: %d %+v", code, list)
	}

	dial := func(query string) (*websocket.Conn, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(g.http.URL, "http")+"/api/v2/terminal?"+query, nil)
		return ws, err
	}
	readUntil := func(ws *websocket.Conn, want string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		got := ""
		for !strings.Contains(got, want) {
			typ, data, err := ws.Read(ctx)
			if err != nil || typ != websocket.MessageBinary || len(data) == 0 || data[0] != 0x01 {
				t.Fatalf("waiting for %q after %q: %v %v %v", want, got, typ, data, err)
			}
			got += string(data[1:])
		}
		return got
	}
	ws, err := dial("conversation=" + conv + "&terminal=" + first.ID + "&cols=100&rows=30")
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	readUntil(ws, "$ ")
	if err := ws.Write(context.Background(), websocket.MessageBinary, append([]byte{0x00}, "ls\r"...)); err != nil {
		t.Fatal(err)
	}
	readUntil(ws, "ls")
	if agentPane, err := dial("conversation=" + conv); err != nil {
		t.Fatal(err)
	} else {
		readUntil(agentPane, "agent")
		agentPane.CloseNow()
	}
	for query, want := range map[string]int{
		"conversation=tab-nothing":                       404,
		"conversation=" + conv + "&terminal=nosuchterm1": 404,
		"conversation=nope":                              400,
		"conversation=" + conv + "&terminal=BAD":         400,
		"terminal=" + first.ID:                           400,
	} {
		if _, err := dial(query); err == nil {
			t.Errorf("%s: dialed", query)
		}
		var refusal wire.CmdResult
		if code := g.do("GET", "/api/v2/terminal?"+query, nil, &refusal); code != want || refusal.R != wire.ResultRefused {
			t.Errorf("%s: %d %+v, want %d", query, code, refusal, want)
		}
	}

	if code := g.do("DELETE", page+"/"+first.ID, nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	var refusal wire.CmdResult
	if code := g.do("DELETE", page+"/"+first.ID, nil, &refusal); code != 404 {
		t.Fatalf("delete twice: %d %+v", code, refusal)
	}
	if code := g.do("GET", "/api/v2/conversations/tab-nothing/terminals", nil, &refusal); code != 404 {
		t.Fatalf("a conversation that is not there: %d", code)
	}
	if code := g.do("GET", page+"/"+second.ID, nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET of one: %d", code)
	}
	for i := 0; i < wire.TerminalsMax; i++ {
		code := g.do("POST", page, nil, nil)
		if want := map[bool]int{true: 409, false: 200}[i >= wire.TerminalsMax-1]; code != want {
			t.Fatalf("terminal %d: %d", i+3, code)
		}
	}
}

// A recording becomes text; a runtime without dictation does not have the page, a machine that cannot recognize now is
// unsupported, a bad request is told from a failed recording.
func TestVoicePage(t *testing.T) {
	g := newRig(t, time.Hour)
	audio := []byte("a recording")
	if code := g.do("POST", "/api/v2/voice", audio, nil); code != http.StatusNotFound {
		t.Fatalf("without dictation: %d", code)
	}
	var lang string
	var heard []byte
	var fail error
	g.srv.Voice = func(_ context.Context, r io.Reader, l string) (string, error) {
		lang = l
		heard, _ = io.ReadAll(r)
		return " сказано ", fail
	}
	var got wire.Transcript
	if code := g.do("POST", "/api/v2/voice", audio, &got); code != 200 || got.Text != " сказано " || lang != "ru" || !bytes.Equal(heard, audio) {
		t.Fatalf("voice: %d %+v lang %q heard %q", code, got, lang, heard)
	}
	if code := g.do("POST", "/api/v2/voice?lang=auto", audio, &got); code != 200 || lang != "auto" {
		t.Fatalf("auto: %d lang %q", code, lang)
	}
	var refused wire.CmdResult
	for _, c := range []struct {
		name, path string
		body       []byte
		fail       error
		code       int
		want       wire.RefuseCode
	}{
		{"unknown language", "/api/v2/voice?lang=de", audio, nil, 400, wire.RefuseBadRequest},
		{"empty body", "/api/v2/voice", []byte{}, nil, 400, wire.RefuseBadRequest},
		{"too large", "/api/v2/voice", make([]byte, wire.VoiceBodyMax+1), nil, 413, wire.RefuseAttachmentTooLarge},
		{"cannot recognize", "/api/v2/voice", audio, wire.ErrVoiceUnavailable, 400, wire.RefuseUnsupported},
		{"failed", "/api/v2/voice", audio, errors.New("ffmpeg exited"), 500, wire.RefuseInternal},
	} {
		fail = c.fail
		code := g.do("POST", c.path, c.body, &refused)
		if code != c.code || refused.R != wire.ResultRefused || refused.Code != c.want {
			t.Errorf("%s: %d %+v", c.name, code, refused)
		}
	}
	if code := g.do("GET", "/api/v2/voice", nil, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET: %d", code)
	}
}

// A page of another origin cannot open the socket, a terminal or a recording, nor make or end a terminal, with the
// person's cookie or not; the page's own origin and a client that is no browser can.
func TestCrossSiteRequestsAreRefused(t *testing.T) {
	g := newRig(t, time.Hour)
	g.srv.Voice = func(context.Context, io.Reader, string) (string, error) { return "x", nil }
	const conv = "tab-k3Jx9QmZ"
	host := strings.TrimPrefix(g.http.URL, "http://")
	do := func(method, path string, h map[string]string) int {
		req, _ := http.NewRequest(method, g.http.URL+path, strings.NewReader("a recording"))
		req.Header.Set(wire.DeviceHeader, "dev-test")
		for k, v := range h {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	paths := []struct{ method, path string }{
		{"GET", "/api/v2/socket"}, {"GET", "/api/v2/terminal?conversation=" + conv}, {"POST", "/api/v2/voice"},
		{"POST", "/api/v2/designly/create"}, {"POST", "/api/v2/designly/pin"}, {"POST", "/api/v2/conversations/" + conv + "/side-chat"},
		{"POST", "/api/v2/conversations/" + conv + "/terminals"}, {"DELETE", "/api/v2/conversations/" + conv + "/terminals/abcdef"},
		{"POST", "/api/v2/machine/processes/stop"}, {"PATCH", "/api/v2/agent-settings/claude"}, {"PATCH", "/api/v2/me/settings"},
		{"PUT", "/api/v2/providers/kimi/key"}, {"DELETE", "/api/v2/providers/kimi/key"}, {"POST", "/api/v2/providers/kimi/test"},
		{"POST", "/api/v2/agents/claude/login"}, {"POST", "/api/v2/agents/claude/login/code"}, {"DELETE", "/api/v2/agents/claude/login"}, {"PUT", "/api/v2/agents/codex/key"}, {"POST", "/api/v2/agents/codex/logout"},
	}
	foreign := []map[string]string{
		{"Origin": "https://evil.example"},
		{"Origin": "http://" + host + ".evil.example"},
		{"Origin": "null"},
		{"Sec-Fetch-Site": "cross-site"},
		{"Sec-Fetch-Site": "same-site", "Origin": "http://" + host},
	}
	for _, p := range paths {
		for _, h := range foreign {
			if code := do(p.method, p.path, h); code != http.StatusForbidden {
				t.Errorf("%s %s %v: %d", p.method, p.path, h, code)
			}
		}
		for _, h := range []map[string]string{nil, {"Origin": "http://" + host}, {"Sec-Fetch-Site": "same-origin"}, {"Sec-Fetch-Site": "same-origin", "Origin": "http://proxy.example"},
			{"Origin": "https://app.example", "X-Forwarded-Host": "app.example"}} {
			if code := do(p.method, p.path, h); code == http.StatusForbidden {
				t.Errorf("%s %s %v: refused", p.method, p.path, h)
			}
		}
	}
}

// The design library: a project is made and a conversation opened in its folder, the library lists the projects with the
// conversations that work on them, a pin is kept, a bad body and a taken name are refused with a message.
func TestDesignlyPages(t *testing.T) {
	g := newRig(t, time.Hour)
	var systems wire.DesignSystems
	if code := g.do("GET", "/api/v2/designly/systems", nil, &systems); code != 200 || len(systems) == 0 || systems.Validate() != nil {
		t.Fatalf("systems: %d %+v", code, systems)
	}
	var before wire.DesignLibrary
	if code := g.do("GET", "/api/v2/designly/library", nil, &before); code != 200 || before.Validate() != nil || len(before.Artifacts) == 0 {
		t.Fatalf("library: %d %+v", code, before)
	}
	create := wire.DesignlyCreate{Title: "Сайт для пекарни", Brief: "Главная и меню", Kind: wire.DesignSite, Provider: wire.ProviderClaude}
	var made wire.DesignlyCreated
	if code := g.do("POST", "/api/v2/designly/create", create, &made); code != 200 || made.Validate() != nil || made.Title != create.Title {
		t.Fatalf("create: %d %+v", code, made)
	}
	var after wire.DesignLibrary
	g.do("GET", "/api/v2/designly/library", nil, &after)
	if len(after.Artifacts) != len(before.Artifacts)+1 {
		t.Fatalf("after: %+v", after.Artifacts)
	}
	found := false
	for _, a := range after.Artifacts {
		if a.ID == made.ID {
			found = true
			if !slices.Contains(a.Convs, made.Conv) || a.Kind != wire.DesignSite || a.Pinned {
				t.Fatalf("the new project: %+v", a)
			}
		}
	}
	if !found {
		t.Fatalf("no project %s in %+v", made.ID, after.Artifacts)
	}
	var pinned wire.DesignlyPin
	if code := g.do("POST", "/api/v2/designly/pin", wire.DesignlyPin{ID: made.ID, Pinned: true}, &pinned); code != 200 || !pinned.Pinned {
		t.Fatalf("pin: %d %+v", code, pinned)
	}
	g.do("GET", "/api/v2/designly/library", nil, &after)
	for _, a := range after.Artifacts {
		if a.ID == made.ID && !a.Pinned {
			t.Fatalf("the pin was not kept: %+v", a)
		}
	}

	var refused wire.DesignlyRefusal
	if code := g.do("POST", "/api/v2/designly/create", create, &refused); code != http.StatusConflict || refused.Code != wire.RefuseConflict || refused.Message == "" || refused.Validate() != nil {
		t.Fatalf("taken: %d %+v", code, refused)
	}
	refused = wire.DesignlyRefusal{}
	bad := map[string]any{"title": "x", "brief": "y", "kind": "poster", "provider": "claude", "model": nil, "effort": nil, "designSystem": nil}
	if code := g.do("POST", "/api/v2/designly/create", bad, &refused); code != http.StatusUnprocessableEntity || refused.Code != wire.RefuseBadRequest || refused.Validate() != nil {
		t.Fatalf("bad kind: %d %+v", code, refused)
	}
	if code := g.do("POST", "/api/v2/designly/create", []byte(`{"title":"x"}`), &refused); code != http.StatusUnprocessableEntity {
		t.Fatalf("short body: %d", code)
	}
	if code := g.do("POST", "/api/v2/designly/pin", wire.DesignlyPin{ID: "nothing-here", Pinned: true}, &refused); code != http.StatusNotFound || refused.Code != wire.RefuseNotFound {
		t.Fatalf("pin of a project that is not there: %d %+v", code, refused)
	}
	if code := g.do("GET", "/api/v2/designly/create", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET create: %d", code)
	}
	g.srv.Designly = nil
	if code := g.do("GET", "/api/v2/designly/library", nil, nil); code != http.StatusNotFound {
		t.Fatalf("without a library: %d", code)
	}
}

// The side chat is a route of its own: POST only, a 404 on a machine without it, the conversation's id checked, a stream
// the function writes, an unknown conversation refused.
func TestSideChatRoute(t *testing.T) {
	g := newRig(t, time.Hour)
	const conv = "tab-k3Jx9QmZ"
	path := "/api/v2/conversations/" + conv + "/side-chat"
	post := func(path string) (int, string) {
		req, _ := http.NewRequest("POST", g.http.URL+path, strings.NewReader(`{"question":"q"}`))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, _ := post(path); code != http.StatusNotFound {
		t.Fatalf("no side chat on this machine: %d", code)
	}
	var asked string
	g.srv.SideChat = func(w http.ResponseWriter, r *http.Request, id string) error {
		asked = id
		if id == "tab-gone" {
			return convo.ErrNotFound
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"done\",\"text\":\"ok\"}\n\n")
		return nil
	}
	if code, body := post(path); code != 200 || asked != conv || !strings.Contains(body, `"type":"done"`) {
		t.Fatalf("%d %q %q", code, body, asked)
	}
	if code, _ := post("/api/v2/conversations/tab-gone/side-chat"); code != http.StatusNotFound {
		t.Fatalf("unknown conversation: %d", code)
	}
	if code, _ := post("/api/v2/conversations/not%20an%20id/side-chat"); code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", code)
	}
	req, _ := http.NewRequest("GET", g.http.URL+path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d", res.StatusCode)
	}
}

// The files of a conversation: a folder listed, a text file read, every refusal with its status and a message, a path
// out of contract a 400, and a 404 where the machine has no files.
func TestFilesPages(t *testing.T) {
	g := newRig(t, time.Hour)
	const conv = "tab-k3Jx9QmZ"
	if code := g.do("GET", "/api/v2/conversations/"+conv+"/files", nil, nil); code != http.StatusNotFound {
		t.Fatalf("no files on this machine: %d", code)
	}
	g.srv.Files = g.fake.Files()
	var list wire.DirListing
	if code := g.do("GET", "/api/v2/conversations/"+conv+"/files", nil, &list); code != 200 || list.Validate() != nil || len(list.Entries) != 4 ||
		list.Entries[0].Kind != wire.FileDirectory || list.Path != "" {
		t.Fatalf("root: %d %+v", code, list)
	}
	if code := g.do("GET", "/api/v2/conversations/"+conv+"/files?path=src", nil, &list); code != 200 || list.Path != "src" || len(list.Entries) != 3 || list.Entries[0].Path != "src/components" {
		t.Fatalf("src: %d %+v", code, list)
	}
	var file wire.FileContent
	if code := g.do("GET", "/api/v2/conversations/"+conv+"/file?path=src/main.go", nil, &file); code != 200 || file.Validate() != nil || file.Path != "src/main.go" || file.Bytes != len(file.Content) {
		t.Fatalf("file: %d %+v", code, file)
	}
	for _, tc := range []struct {
		path   string
		status int
		code   wire.RefuseCode
	}{
		{"/files?path=forbidden", 403, wire.RefuseForbidden}, {"/file?path=forbidden/x", 403, wire.RefuseForbidden},
		{"/files?path=nope", 404, wire.RefuseNotFound}, {"/file?path=nope.txt", 404, wire.RefuseNotFound},
		{"/file?path=binary.bin", 415, wire.RefuseUnsupported}, {"/file", 400, wire.RefuseBadRequest}, {"/files?path=a%00b", 400, wire.RefuseBadRequest},
		{"/files?path=" + strings.Repeat("a", wire.FilesPathMax+1), 400, wire.RefuseBadRequest},
	} {
		var refused wire.FileRefusal
		if code := g.do("GET", "/api/v2/conversations/"+conv+tc.path, nil, &refused); code != tc.status || refused.Code != tc.code || refused.Validate() != nil {
			t.Errorf("%s: %d %+v", tc.path, code, refused)
		}
	}
	if code := g.do("GET", "/api/v2/conversations/tab-nothere/files", nil, nil); code != http.StatusNotFound {
		t.Errorf("unknown conversation: %d", code)
	}
	if code := g.do("POST", "/api/v2/conversations/"+conv+"/files", nil, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", code)
	}
	if code := g.do("PUT", "/api/v2/conversations/"+conv+"/files?path=src", wire.FileSave{Content: "x", Revision: "r"}, nil); code != http.StatusMethodNotAllowed {
		t.Errorf("PUT on a folder: %d", code)
	}
}

// Saving a file: the page names the revision it read, the answer is the file as it is now, a stale revision is a 409
// conflict, a body out of contract a 400, and a cross-site request is refused.
func TestFilesSave(t *testing.T) {
	g := newRig(t, time.Hour)
	const conv = "tab-k3Jx9QmZ"
	url := "/api/v2/conversations/" + conv + "/file?path=docs/plan.md"
	if code := g.do("PUT", url, wire.FileSave{Content: "x", Revision: "r"}, nil); code != http.StatusNotFound {
		t.Fatalf("no files on this machine: %d", code)
	}
	g.srv.Files = g.fake.Files()
	var first wire.FileContent
	if code := g.do("GET", url, nil, &first); code != 200 || first.Revision == "" {
		t.Fatalf("read: %d %+v", code, first)
	}
	var saved wire.FileContent
	if code := g.do("PUT", url, wire.FileSave{Content: "# Plan\n\nDone.\n", Revision: first.Revision}, &saved); code != 200 ||
		saved.Validate() != nil || saved.Content != "# Plan\n\nDone.\n" || saved.Revision == first.Revision || saved.Bytes != len(saved.Content) {
		t.Fatalf("save: %d %+v", code, saved)
	}
	var again wire.FileContent
	if code := g.do("GET", url, nil, &again); code != 200 || again != saved {
		t.Fatalf("read back: %d %+v", code, again)
	}
	for name, tc := range map[string]struct {
		path   string
		body   any
		status int
		code   wire.RefuseCode
	}{
		"stale":     {"docs/plan.md", wire.FileSave{Content: "mine", Revision: first.Revision}, 409, wire.RefuseConflict},
		"forbidden": {"forbidden/x", wire.FileSave{Content: "x", Revision: "r"}, 403, wire.RefuseForbidden},
		"gone":      {"nope.txt", wire.FileSave{Content: "x", Revision: "r"}, 404, wire.RefuseNotFound},
		"no path":   {"", wire.FileSave{Content: "x", Revision: "r"}, 400, wire.RefuseBadRequest},
		"no rev":    {"docs/plan.md", wire.FileSave{Content: "x"}, 400, wire.RefuseBadRequest},
		"unknown":   {"docs/plan.md", []byte(`{"content":"x","revision":"r","extra":1}`), 400, wire.RefuseBadRequest},
		"not json":  {"docs/plan.md", []byte(`nope`), 400, wire.RefuseBadRequest},
	} {
		var refused wire.FileRefusal
		if code := g.do("PUT", "/api/v2/conversations/"+conv+"/file?path="+tc.path, tc.body, &refused); code != tc.status || refused.Code != tc.code || refused.Validate() != nil {
			t.Errorf("%s: %d %+v", name, code, refused)
		}
	}
	req, _ := http.NewRequest("PUT", g.http.URL+url, strings.NewReader(`{"content":"evil","revision":"`+saved.Revision+`"}`))
	req.Header.Set(wire.DeviceHeader, "dev-test")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	var final wire.FileContent
	if g.do("GET", url, nil, &final); res.StatusCode == 200 || final.Content != saved.Content {
		t.Errorf("cross-site save: %d, text %q", res.StatusCode, final.Content)
	}
}

// The agent's task list of a conversation: a list (empty for an agent with none, never null), 404 for a conversation
// that is not here and where the machine keeps none.
func TestTasksPage(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/conversations/tab-k3Jx9QmZ/tasks", nil, nil); code != http.StatusNotFound {
		t.Fatalf("no tasks on this machine: %d", code)
	}
	g.srv.Tasks = g.fake.Tasks
	var list wire.Tasks
	if code := g.do("GET", "/api/v2/conversations/tab-k3Jx9QmZ/tasks", nil, &list); code != 200 || list.Validate() != nil || len(list) != 4 || list[1].Status != wire.TaskInProgress {
		t.Fatalf("tasks: %d %+v", code, list)
	}
	req, _ := http.NewRequest("GET", g.http.URL+"/api/v2/conversations/tab-s5Qidl/tasks", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 || strings.TrimSpace(string(body)) != "[]" {
		t.Fatalf("a conversation with no tasks: %d %q", res.StatusCode, body)
	}
	if code := g.do("GET", "/api/v2/conversations/tab-nothere/tasks", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown conversation: %d", code)
	}
}

// The status of messages by client id: the answer in contract, one per id; a stalled message reaches a page without the
// msgstatus cap as failed{unconfirmed}; ids that are missing, too many or twice are a bad request; no source is a 404.
func TestMessageStatusPage(t *testing.T) {
	g := newRig(t, time.Hour)
	path := "/api/v2/conversations/tab-k3Jx9QmZ/messages?ids=m1,m2"
	if code := g.do("GET", path, nil, nil); code != http.StatusNotFound {
		t.Fatalf("no source: %d", code)
	}
	g.srv.MessageStatus = func(_ context.Context, conv string, ids []string) (wire.MessageStatuses, error) {
		if conv != "tab-k3Jx9QmZ" {
			return wire.MessageStatuses{}, convo.ErrNotFound
		}
		m := wire.Message{ClientID: ids[0], ConversationID: conv, Attachments: []wire.AttachmentRef{}, Mode: wire.ModeQueue,
			Kind: wire.KindPrompt, CreatedAt: 1, Rev: 3, State: wire.MessageState{K: wire.MsgQueued, Position: 1, Reason: wire.QueuedStalled, At: 1700}}
		return wire.MessageStatuses{Messages: []wire.MessageStatus{{ClientID: ids[0], K: wire.StatusKnown, Message: &m},
			{ClientID: ids[1], K: wire.StatusUnknown}}}, nil
	}
	var got wire.MessageStatuses
	if code := g.do("GET", path+"&caps=msgstatus", nil, &got); code != 200 || got.Validate() != nil ||
		got.Messages[0].Message.State.Reason != wire.QueuedStalled || got.Messages[1].K != wire.StatusUnknown {
		t.Fatalf("with the cap: %d %+v", code, got)
	}
	got = wire.MessageStatuses{}
	if code := g.do("GET", path, nil, &got); code != 200 || got.Messages[0].Message.State.K != wire.MsgFailed ||
		got.Messages[0].Message.State.Code != wire.MsgFailedUnconfirmed || got.Messages[0].Message.State.At != 1700 {
		t.Fatalf("without the cap: %d %+v", code, got)
	}
	many := make([]string, wire.MessageStatusMax+1)
	for i := range many {
		many[i] = fmt.Sprintf("m%d", i)
	}
	for _, bad := range []string{"", "?ids=", "?ids=a,,b", "?ids=a,a", "?ids=" + strings.Join(many, ",")} {
		if code := g.do("GET", "/api/v2/conversations/tab-k3Jx9QmZ/messages"+bad, nil, nil); code != http.StatusBadRequest {
			t.Fatalf("%q: %d", bad, code)
		}
	}
	if code := g.do("GET", "/api/v2/conversations/tab-nothere/messages?ids=m1", nil, nil); code != http.StatusNotFound {
		t.Fatalf("unknown conversation: %d", code)
	}
}

// The Machine tool: the stats are in contract, a stop ends a process the person may stop and refuses a guarded one, one
// that is gone and one whose pid is another process now, each with its status and reason.
func TestMachineToolPages(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/machine/stats", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a machine without the tool answers %d", code)
	}
	machine, err := wirefake.NewMachine("local", "9.9.9", wire.Features)
	if err != nil {
		t.Fatal(err)
	}
	g.srv.MachineTool = machine.Tool()

	var stats wire.MachineStats
	if code := g.do("GET", "/api/v2/machine/stats", nil, &stats); code != 200 || stats.Validate() != nil || len(stats.Cores) == 0 || len(stats.History) == 0 || len(stats.Procs) < 4 {
		t.Fatalf("stats: %d %v %+v", code, stats.Validate(), stats)
	}
	if code := g.do("POST", "/api/v2/machine/stats", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST stats: %d", code)
	}
	if code := g.do("GET", "/api/v2/machine/processes/stop", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("GET stop: %d", code)
	}
	if code := g.do("GET", "/api/v2/machine/nothing", nil, nil); code != http.StatusNotFound {
		t.Fatalf("an unknown page: %d", code)
	}
	var stoppable, guarded wire.MachineProc
	for _, p := range stats.Procs {
		if p.Guard && guarded.PID == 0 {
			guarded = p
		}
		if !p.Guard && stoppable.PID == 0 {
			stoppable = p
		}
	}
	stop := func(p wire.MachineProc, want int, reason wire.MachineStopReason) {
		t.Helper()
		if want == 200 {
			var done wire.MachineStopped
			if code := g.do("POST", "/api/v2/machine/processes/stop", wire.MachineStop{PID: p.PID, StartTime: p.StartTime}, &done); code != 200 || done.Validate() != nil {
				t.Fatalf("stop %d: %d %+v", p.PID, code, done)
			}
			return
		}
		var ref wire.MachineRefusal
		if code := g.do("POST", "/api/v2/machine/processes/stop", wire.MachineStop{PID: p.PID, StartTime: p.StartTime}, &ref); code != want || ref.Reason != reason || ref.Validate() != nil {
			t.Fatalf("stop %d: %d %+v, want %d %s", p.PID, code, ref, want, reason)
		}
	}
	stop(guarded, http.StatusForbidden, wire.StopProtected)
	stop(wire.MachineProc{PID: stoppable.PID, StartTime: "1"}, http.StatusConflict, wire.StopChanged)
	stop(stoppable, 200, "")
	stop(stoppable, http.StatusNotFound, wire.StopGone)
	var after wire.MachineStats
	g.do("GET", "/api/v2/machine/stats", nil, &after)
	if len(after.Procs) != len(stats.Procs)-1 {
		t.Fatalf("a stopped process is no longer listed: %d of %d", len(after.Procs), len(stats.Procs))
	}
	for _, bad := range []any{[]byte(`{"pid":1,"startTime":"5"}`), []byte(`{"pid":4211}`), []byte(`{"pid":4211,"startTime":"5","x":1}`), []byte(`nope`)} {
		if code := g.do("POST", "/api/v2/machine/processes/stop", bad, nil); code != http.StatusBadRequest {
			t.Fatalf("bad body %s: %d", bad, code)
		}
	}
}

// The Apps screen: a machine without it answers 404, the feed is in contract with both apps, and an app that is off or
// does not answer says why. Nothing here changes state, so no origin is refused.
func TestAppsPage(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/apps", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a machine without the screen answers %d", code)
	}
	machine, err := wirefake.NewMachine("local", "9.9.9", wire.Features)
	if err != nil {
		t.Fatal(err)
	}
	g.srv.Apps = machine.Apps()

	var feed wire.AppsFeed
	if code := g.do("GET", "/api/v2/apps", nil, &feed); code != 200 || feed.Validate() != nil || len(feed.Apps) != 2 {
		t.Fatalf("apps: %d %v %+v", code, feed.Validate(), feed)
	}
	for _, app := range feed.Apps {
		if want := map[wire.AppID]int{wire.AppTracker: 2, wire.AppGitLab: 1}[app.ID]; app.State != wire.AppReady || len(app.Sections) != want {
			t.Fatalf("%+v", app)
		}
		for _, s := range app.Sections {
			if len(s.Rows) == 0 {
				t.Fatalf("%s/%s has no rows", app.ID, s.ID)
			}
		}
	}
	if code := g.do("POST", "/api/v2/apps", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST apps: %d", code)
	}
}

// The Settings screen's agent files: a machine without them answers 404, both CLIs read in contract, a write quotes the
// revision it was made on (a stale one is a conflict) and returns the new state, and a body that is not a patch of this
// CLI is refused. Only Claude Code has rules and an environment.
func TestAgentSettingsPages(t *testing.T) {
	g := newRig(t, time.Hour)
	if code := g.do("GET", "/api/v2/agent-settings/claude", nil, nil); code != http.StatusNotFound {
		t.Fatalf("a machine without the files answers %d", code)
	}
	machine, err := wirefake.NewMachine("local", "9.9.9", wire.Features)
	if err != nil {
		t.Fatal(err)
	}
	g.srv.AgentSettings = machine.AgentSettings()

	var claude, codex wire.AgentSettings
	if code := g.do("GET", "/api/v2/agent-settings/claude", nil, &claude); code != 200 || claude.Validate() != nil || claude.Rules == nil {
		t.Fatalf("claude: %d %v %+v", code, claude.Validate(), claude)
	}
	if code := g.do("GET", "/api/v2/agent-settings/codex", nil, &codex); code != 200 || codex.Validate() != nil || codex.Rules != nil || len(codex.Env) != 0 {
		t.Fatalf("codex: %d %v %+v", code, codex.Validate(), codex)
	}
	if code := g.do("GET", "/api/v2/agent-settings/gemini", nil, nil); code != http.StatusNotFound {
		t.Fatalf("an unknown CLI: %d", code)
	}
	if code := g.do("PUT", "/api/v2/agent-settings/claude", nil, nil); code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT: %d", code)
	}

	edit := func(provider string, rev string, patch wire.AgentSettingsPatch, out any) int {
		return g.do("PATCH", "/api/v2/agent-settings/"+provider, wire.AgentSettingsEdit{Rev: rev, Patch: patch}, out)
	}
	var saved wire.AgentSettings
	patch := wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptModel, Value: "sonnet"}, {Key: wire.OptEffort, Value: nil}}}
	if code := edit("claude", claude.Rev, patch, &saved); code != 200 || saved.Validate() != nil || saved.Rev == claude.Rev {
		t.Fatalf("save: %d %v %+v", code, saved.Validate(), saved)
	}
	values := map[wire.AgentOptionKey]any{}
	for _, o := range saved.Options {
		values[o.Key] = o.Value
	}
	if values[wire.OptModel] != "sonnet" || values[wire.OptEffort] != nil {
		t.Fatalf("the saved options: %+v", saved.Options)
	}
	var refused wire.CmdResult
	if code := edit("claude", claude.Rev, patch, &refused); code != http.StatusConflict || refused.Code != wire.RefuseConflict {
		t.Fatalf("a stale revision: %d %+v", code, refused)
	}
	rules := wire.AgentRules{Allow: []string{"Bash(go test *)"}, Ask: []string{}, Deny: []string{}}
	if code := edit("claude", saved.Rev, wire.AgentSettingsPatch{Rules: &rules, EnvSet: []wire.AgentEnvSet{{Key: "API_TOKEN", Value: "abcdef123456"}}, EnvUnset: []string{"BASH_DEFAULT_TIMEOUT_MS"}}, &saved); code != 200 || saved.Validate() != nil {
		t.Fatalf("rules and env: %d %v", code, saved.Validate())
	}
	if len(saved.Rules.Allow) != 1 || saved.Env[len(saved.Env)-1].Value != "••••3456" || !saved.Env[len(saved.Env)-1].Secret {
		t.Fatalf("a secret comes back as its tail: %+v", saved.Env)
	}
	if code := edit("claude", saved.Rev, wire.AgentSettingsPatch{Mcp: &wire.AgentMcpToggle{Name: "tracker", Enabled: false}}, &saved); code != 200 || saved.Mcp[1].Enabled {
		t.Fatalf("an MCP server off: %d %+v", code, saved.Mcp)
	}
	for name, bad := range map[string]struct {
		provider string
		body     any
	}{
		"empty patch":      {"claude", wire.AgentSettingsEdit{Rev: saved.Rev}},
		"no rev":           {"claude", []byte(`{"patch":{"options":[{"key":"model","value":"x"}]}}`)},
		"unknown option":   {"claude", wire.AgentSettingsEdit{Rev: saved.Rev, Patch: wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptFast, Value: true}}}}},
		"value out of set": {"claude", wire.AgentSettingsEdit{Rev: saved.Rev, Patch: wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptEffort, Value: "extreme"}}}}},
		"mode only read":   {"claude", wire.AgentSettingsEdit{Rev: saved.Rev, Patch: wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptPermissionMode, Value: "bypassPermissions"}}}}},
		"rules for codex":  {"codex", wire.AgentSettingsEdit{Rev: codex.Rev, Patch: wire.AgentSettingsPatch{Rules: &rules}}},
		"base url as env":  {"claude", wire.AgentSettingsEdit{Rev: saved.Rev, Patch: wire.AgentSettingsPatch{EnvSet: []wire.AgentEnvSet{{Key: "ANTHROPIC_BASE_URL", Value: "https://x.example"}}}}},
		"a script as url":  {"codex", wire.AgentSettingsEdit{Rev: codex.Rev, Patch: wire.AgentSettingsPatch{Options: []wire.AgentOption{{Key: wire.OptBaseURL, Value: "javascript:alert(1)"}}}}},
		"extra field":      {"claude", []byte(`{"rev":"x","patch":{"model":"a"}}`)},
		"not json":         {"claude", []byte(`nope`)},
	} {
		if code := g.do("PATCH", "/api/v2/agent-settings/"+bad.provider, bad.body, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, code)
		}
	}
}
