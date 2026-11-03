package agent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const jobA = "0123456789abcdef0123456789abcdef"
const jobB = "fedcba9876543210fedcba9876543210"

type restartRig struct {
	r        *restarter
	chats    atomic.Int64
	unknown  atomic.Bool
	restarts atomic.Int64
	failWith atomic.Value // error
	back     atomic.Bool
	mu       sync.Mutex
	events   []hubproto.RestartEvent
}

func newRestartRig() *restartRig {
	rig := &restartRig{}
	rig.back.Store(true)
	rig.r = &restarter{
		chats: func() *int {
			if rig.unknown.Load() {
				return nil
			}
			n := int(rig.chats.Load())
			return &n
		},
		restart: func() error {
			rig.restarts.Add(1)
			if e, _ := rig.failWith.Load().(error); e != nil {
				return e
			}
			return nil
		},
		back: func() bool { return rig.back.Load() },
		emit: func(ev hubproto.RestartEvent) {
			rig.mu.Lock()
			rig.events = append(rig.events, ev)
			rig.mu.Unlock()
		},
		poll: 5 * time.Millisecond, settle: time.Millisecond, backMax: time.Second,
	}
	return rig
}

func (g *restartRig) states() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, e := range g.events {
		out = append(out, e.State)
	}
	return strings.Join(out, ",")
}

func (g *restartRig) waitState(t *testing.T, want string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if strings.HasSuffix(g.states(), want) {
			return
		}
	}
	t.Fatalf("states %q never ended with %q", g.states(), want)
}

func TestRestartNow(t *testing.T) {
	g := newRestartRig()
	g.chats.Store(4) // chats do not hold a restart "now"
	ev, err := g.r.Start(context.Background(), jobA, hubproto.RestartNow)
	if err != nil || ev.State != hubproto.RestartRestarting {
		t.Fatalf("%+v %v", ev, err)
	}
	g.waitState(t, "done")
	g.r.Wait()
	if g.restarts.Load() != 1 || g.states() != "restarting,done" || g.r.Draining() {
		t.Fatalf("%d %q", g.restarts.Load(), g.states())
	}
	// The next job is free to start.
	if _, err := g.r.Start(context.Background(), jobB, hubproto.RestartNow); err != nil {
		t.Fatal(err)
	}
	g.waitState(t, "done")
	g.r.Wait()
}

func TestRestartDrainWaitsForChatsAndRefusesNewOnes(t *testing.T) {
	g := newRestartRig()
	g.chats.Store(2)
	ev, err := g.r.Start(context.Background(), jobA, hubproto.RestartDrain)
	if err != nil || ev.State != hubproto.RestartWaiting || ev.ChatsNow == nil || *ev.ChatsNow != 2 {
		t.Fatalf("%+v %v", ev, err)
	}
	if !g.r.Draining() {
		t.Fatal("not draining")
	}
	time.Sleep(40 * time.Millisecond)
	if g.restarts.Load() != 0 {
		t.Fatal("restarted with chats at work")
	}
	g.chats.Store(1)
	time.Sleep(30 * time.Millisecond)
	if g.restarts.Load() != 0 {
		t.Fatal("restarted with a chat at work")
	}
	g.chats.Store(0)
	g.waitState(t, "done")
	g.r.Wait()
	if g.restarts.Load() != 1 || g.r.Draining() {
		t.Fatalf("restarts %d draining %v", g.restarts.Load(), g.r.Draining())
	}
	if s := g.states(); !strings.HasPrefix(s, "waiting") || !strings.HasSuffix(s, "restarting,done") {
		t.Fatalf("%q", s)
	}
}

func TestRestartAfterChatsDoesNotDrain(t *testing.T) {
	g := newRestartRig()
	g.chats.Store(1)
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartAfterChats); err != nil {
		t.Fatal(err)
	}
	if g.r.Draining() {
		t.Fatal("after_chats refuses new chats")
	}
	g.chats.Store(0)
	g.waitState(t, "done")
	g.r.Wait()
}

func TestRestartUnknownChatsWaitsInsteadOfGuessing(t *testing.T) {
	g := newRestartRig()
	g.unknown.Store(true)
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartAfterChats); err != nil {
		t.Fatal(err)
	}
	time.Sleep(40 * time.Millisecond)
	if g.restarts.Load() != 0 {
		t.Fatal("restarted without knowing the chats")
	}
	if _, err := g.r.Cancel(jobA); err != nil {
		t.Fatal(err)
	}
	g.r.Wait()
}

func TestRestartCancel(t *testing.T) {
	g := newRestartRig()
	g.chats.Store(1)
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartDrain); err != nil {
		t.Fatal(err)
	}
	if _, err := g.r.Cancel(jobB); err == nil || hubproto.ParseError(err.Error()).Code != hubproto.CodeNotFound {
		t.Fatalf("cancel of another job: %v", err)
	}
	ev, err := g.r.Cancel(jobA)
	if err != nil || ev.State != hubproto.RestartCancelled {
		t.Fatalf("%+v %v", ev, err)
	}
	g.r.Wait()
	g.chats.Store(0)
	time.Sleep(30 * time.Millisecond)
	if g.restarts.Load() != 0 || g.r.Draining() {
		t.Fatalf("a cancelled job restarted (%d) or still drains", g.restarts.Load())
	}
	if !strings.HasSuffix(g.states(), "cancelled") {
		t.Fatalf("%q", g.states())
	}
	// A new job may start after the cancel.
	if _, err := g.r.Start(context.Background(), jobB, hubproto.RestartNow); err != nil {
		t.Fatal(err)
	}
	g.waitState(t, "done")
	g.r.Wait()
}

func TestRestartOneJobAtATime(t *testing.T) {
	g := newRestartRig()
	g.chats.Store(1)
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartAfterChats); err != nil {
		t.Fatal(err)
	}
	if _, err := g.r.Start(context.Background(), jobB, hubproto.RestartNow); err == nil || hubproto.ParseError(err.Error()).Code != hubproto.CodeBusy {
		t.Fatalf("a second job: %v", err)
	}
	// The same job again (the hub retried) is the same job.
	if ev, err := g.r.Start(context.Background(), jobA, hubproto.RestartAfterChats); err != nil || ev.JobID != jobA {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, err := g.r.Start(context.Background(), jobB, "later"); err == nil {
		t.Fatal("an unknown when was accepted")
	}
	_, _ = g.r.Cancel(jobA)
	g.r.Wait()
}

func TestRestartFailures(t *testing.T) {
	g := newRestartRig()
	g.failWith.Store(errors.New("the runtime refused"))
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartNow); err != nil {
		t.Fatal(err)
	}
	g.waitState(t, "failed")
	g.r.Wait()
	g.mu.Lock()
	last := g.events[len(g.events)-1]
	g.mu.Unlock()
	if last.Error != "the runtime refused" || g.r.Draining() {
		t.Fatalf("%+v", last)
	}

	g = newRestartRig()
	g.back.Store(false)
	g.r.backMax = 40 * time.Millisecond
	if _, err := g.r.Start(context.Background(), jobA, hubproto.RestartDrain); err != nil {
		t.Fatal(err)
	}
	g.waitState(t, "failed")
	g.r.Wait()
	if g.r.Draining() {
		t.Fatal("still drains after a failed restart")
	}
}

func TestStartsChatAndDrainFilter(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/api/v2/cmd/conversation.create", true},
		{"POST", "/m/abc/api/v2/cmd/conversation.create", true},
		{"POST", "/m/abc/agents/api/v2/cmd/conversation.fork", true},
		{"POST", "/api/v2/cmd/conversation.resume", true},
		{"POST", "/api/v2/cmd/conversation.handoff", true},
		{"POST", "/api/platform/conversations", true},
		{"GET", "/api/platform/conversations", false},
		{"POST", "/api/v2/cmd/prompt.send", false},
		{"POST", "/api/platform/prompt-queue", false},
		{"POST", "/api/v2/cmd/conversation.archive", false},
	}
	f := newRouteFilter(1, "/m/abc", DefaultPolicy, nil)
	for _, c := range cases {
		if got := startsChat(c.method, f.pathForms(c.path)); got != c.want {
			t.Errorf("%s %s: %v, want %v", c.method, c.path, got, c.want)
		}
	}

	var draining atomic.Bool
	f.draining = draining.Load
	f.roots = []string{t.TempDir()}
	req := func(method, path string) *http.Request {
		return httptest.NewRequest(method, path, strings.NewReader("{}"))
	}
	if reason, _ := f.decide(DefaultPolicy(), req("POST", "/m/abc/api/v2/cmd/conversation.create")); reason != "" {
		t.Fatalf("refused while not draining: %s", reason)
	}
	draining.Store(true)
	if reason, _ := f.decide(DefaultPolicy(), req("POST", "/m/abc/api/v2/cmd/conversation.create")); reason != drainReason {
		t.Fatalf("not refused while draining: %q", reason)
	}
	if reason, _ := f.decide(DefaultPolicy(), req("POST", "/m/abc/api/v2/cmd/prompt.send")); reason != "" {
		t.Fatalf("a message to a running chat was refused: %s", reason)
	}
	if reason, _ := f.decide(DefaultPolicy(), req("GET", "/m/abc/api/v2/conversations")); reason != "" {
		t.Fatalf("a read was refused: %s", reason)
	}
	// The answer is a 503 a page can show, not a policy denial.
	rec := httptest.NewRecorder()
	f.ServeHTTP(rec, req("POST", "/m/abc/api/v2/cmd/conversation.create"))
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "machine_draining") || f.denials.Load() != 0 {
		t.Fatalf("%d %s denials %d", rec.Code, rec.Body, f.denials.Load())
	}
}
