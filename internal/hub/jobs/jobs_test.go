package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func b(id string, caps []string, slots, running int) Candidate {
	return Candidate{ID: id, Name: id, Role: "builder", Online: true, Offers: true, Caps: caps, Slots: slots, Running: running}
}

func TestPlaceHeavy(t *testing.T) {
	need := Need{Kind: "heavy", Caps: []string{"blender"}}
	agents := Candidate{ID: "a", Name: "a", Role: "agents", Online: true, Offers: true, Caps: []string{"blender"}, Slots: 4}
	t.Run("least loaded fitting builder", func(t *testing.T) {
		p := Place(need, []Candidate{agents, b("b1", []string{"blender"}, 2, 1), b("b2", []string{"blender", "gpu"}, 2, 0), b("b3", []string{"ffmpeg"}, 8, 0)})
		if p.Machine == nil || p.Machine.ID != "b2" || !strings.Contains(p.Reason, "b2") {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("chats' machines never take a heavy job", func(t *testing.T) {
		p := Place(need, []Candidate{agents})
		if !p.Local || p.Machine != nil || !strings.Contains(p.Reason, "no builders") {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("busy builders: wait", func(t *testing.T) {
		p := Place(need, []Candidate{b("b1", []string{"blender"}, 1, 1)})
		if !p.Wait || p.Local || p.Machine != nil {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("offline or not offering: local with the reason", func(t *testing.T) {
		off := b("b1", []string{"blender"}, 1, 0)
		off.Online = false
		noOpt := b("b2", []string{"blender"}, 1, 0)
		noOpt.Offers = false
		p := Place(need, []Candidate{off, noOpt, b("b3", []string{"ffmpeg"}, 1, 0)})
		if !p.Local || !strings.Contains(p.Reason, "offline") || !strings.Contains(p.Reason, "AGENT_HUB_JOBS") || !strings.Contains(p.Reason, "no blender") {
			t.Fatalf("%+v", p)
		}
	})
	t.Run("explicit builder passes the same filters", func(t *testing.T) {
		ok := Place(Need{Kind: "heavy", Caps: []string{"blender"}, Machine: "b1"}, []Candidate{b("b1", []string{"blender"}, 2, 0), b("b2", []string{"blender"}, 2, 0)})
		if ok.Machine == nil || ok.Machine.ID != "b1" || !strings.Contains(ok.Reason, "chosen") {
			t.Fatalf("%+v", ok)
		}
		busy := Place(Need{Kind: "heavy", Machine: "b1"}, []Candidate{b("b1", nil, 1, 1), b("b2", nil, 2, 0)})
		if !busy.Wait {
			t.Fatalf("the chosen builder is busy, the job waits for it: %+v", busy)
		}
		wrong := Place(Need{Kind: "heavy", Caps: []string{"gpu"}, Machine: "b1"}, []Candidate{b("b1", []string{"blender"}, 2, 0), b("b2", []string{"gpu"}, 2, 0)})
		if !wrong.Local || !strings.Contains(wrong.Reason, "no gpu") {
			t.Fatalf("an explicit machine that does not fit is not replaced silently: %+v", wrong)
		}
		notBuilder := Place(Need{Kind: "heavy", Machine: "a"}, []Candidate{agents, b("b1", nil, 2, 0)})
		if !notBuilder.Local || !strings.Contains(notBuilder.Reason, "not a builder") {
			t.Fatalf("%+v", notBuilder)
		}
	})
}

type fakeLink struct {
	mu     sync.Mutex
	online map[string]bool
	calls  []string
	state  map[string]hubproto.JobState
	start  error
}

func (f *fakeLink) Online(id string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.online[id] }
func (f *fakeLink) Do(_ context.Context, id, op string, params any, _ time.Duration) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op+" "+id)
	switch op {
	case hubproto.OpJobStart:
		return json.RawMessage(`{"ok":true}`), f.start
	case hubproto.OpJobPoll:
		out := map[string]hubproto.JobState{}
		for k, v := range f.state {
			out[k] = v
		}
		raw, _ := json.Marshal(map[string]any{"jobs": out})
		return raw, nil
	case hubproto.OpJobCaps:
		return json.RawMessage(`{"caps":["Blender","ffmpeg"],"slots":2,"tools":["blender"]}`), nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}

type rig struct {
	s     *Service
	link  *fakeLink
	space string
	user  string
	b1    store.Machine
	home  store.Machine
}

func newRig(t *testing.T) *rig {
	t.Helper()
	ctx := context.Background()
	db := storetest.Open(t)
	st := db.S()
	u := &store.User{Email: "a@example.com", Name: "A"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	sp := &store.Space{Slug: "s", Name: "S", CreatedBy: u.ID}
	if err := st.CreateSpace(ctx, sp); err != nil {
		t.Fatal(err)
	}
	mk := func(name, key string) store.Machine {
		m := &store.Machine{SpaceID: sp.ID, Name: name, Profile: "dir", Via: "command", KeyPub: key, EncPub: key, CreatedBy: u.ID}
		if err := st.CreateMachine(ctx, m); err != nil {
			t.Fatal(err)
		}
		return *m
	}
	r := &rig{space: sp.ID, user: u.ID, b1: mk("builder-1", "k1"), home: mk("home", "k2")}
	r.link = &fakeLink{online: map[string]bool{r.b1.ID: true, r.home.ID: true}, state: map[string]hubproto.JobState{}}
	r.s = New(Deps{DB: db, Link: r.link, Offers: func(string, string) bool { return true }, Dir: t.TempDir()})
	return r
}

func (r *rig) req() Request {
	var q Request
	q.Need.Caps = []string{"blender"}
	q.Argv = []string{"blender", "-b"}
	q.Outputs = []string{"out/*.png"}
	return q
}

func TestRoleAndLifecycle(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	actor := Actor{SpaceID: r.space, UserID: r.user, MachineID: r.home.ID}

	// no builders yet: local slots, with the explanation and draft limits
	j, err := r.s.Submit(ctx, actor, r.req())
	if err != nil || !j.Local || j.Status != hubproto.HeavyRunning || j.MachineID != r.home.ID || !strings.Contains(j.Reason, "no builders") {
		t.Fatalf("%+v %v", j, err)
	}
	if v := r.s.View(ctx, j); v.LocalLimits == nil || v.LocalLimits.TimeoutSec != localDraftSec || v.Choice.Mode != "auto" {
		t.Fatalf("%+v", v)
	}
	j, err = r.s.ReportLocal(ctx, j, 0, "", "ok")
	if err != nil || j.Status != hubproto.HeavyDone {
		t.Fatalf("%+v %v", j, err)
	}

	// the machine becomes a builder: labels and slots come from its agent
	b, err := r.s.SetRole(ctx, r.b1, "builder", nil, 0, r.user)
	if err != nil || b.Slots != 2 || strings.Join(b.Caps, ",") != "blender,ffmpeg" {
		t.Fatalf("%+v %v", b, err)
	}
	bs, _ := r.s.Builders(ctx, r.space)
	if len(bs) != 1 || !bs[0].Ready || bs[0].Name != "builder-1" {
		t.Fatalf("%+v", bs)
	}

	// a job with input waits for its files, then goes to the builder, and its result comes back
	q := r.req()
	q.HasInput = true
	q.Need.Machine = "builder-1"
	j, err = r.s.Submit(ctx, actor, q)
	if err != nil || j.Local || j.Status != hubproto.HeavyQueued || j.WantMachine != r.b1.ID {
		t.Fatalf("%+v %v", j, err)
	}
	r.s.Tick(ctx)
	if got, _ := r.s.Job(ctx, r.space, j.ID); got.Status != hubproto.HeavyQueued {
		t.Fatalf("started before its files arrived: %s", got.Status)
	}
	if _, err := r.s.files.PutInput(j.ID, strings.NewReader("tgz")); err != nil {
		t.Fatal(err)
	}
	r.s.Tick(ctx)
	got, _ := r.s.Job(ctx, r.space, j.ID)
	if got.Status != hubproto.HeavyRunning || got.MachineID != r.b1.ID || !strings.Contains(got.Reason, "chosen builder") {
		t.Fatalf("%+v", got)
	}
	bs, _ = r.s.Builders(ctx, r.space)
	if bs[0].Running != 1 {
		t.Fatalf("load not shown: %+v", bs)
	}
	if _, err := r.s.files.PutOut(j.ID, "out/f.png", strings.NewReader("png")); err != nil {
		t.Fatal(err)
	}
	zero := 0
	r.link.state[j.ID] = hubproto.JobState{Status: hubproto.HeavyDone, ExitCode: &zero, Log: "Blender done"}
	r.s.Tick(ctx)
	got, _ = r.s.Job(ctx, r.space, j.ID)
	if got.Status != hubproto.HeavyDone || len(got.Result) != 1 || got.Result[0].Path != "out/f.png" || got.LogTail != "Blender done" {
		t.Fatalf("%+v", got)
	}

	// the builder goes offline: a job for any builder falls back to the local slots
	r.link.online[r.b1.ID] = false
	j2, err := r.s.Submit(ctx, actor, r.req())
	if err != nil || !j2.Local || !strings.Contains(j2.Reason, "offline") {
		t.Fatalf("%+v %v", j2, err)
	}
	// from a page there is no machine of the caller's own: it fails with the reason, it is not lost
	j3, err := r.s.Submit(ctx, Actor{SpaceID: r.space, UserID: r.user}, r.req())
	if err != nil || j3.Status != hubproto.HeavyFailed || !strings.Contains(j3.Error, "no online builder") {
		t.Fatalf("%+v %v", j3, err)
	}
}

func TestQueueCancelAndLostBuilder(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	actor := Actor{SpaceID: r.space, UserID: r.user, MachineID: r.home.ID}
	if _, err := r.s.SetRole(ctx, r.b1, "builder", []string{"Blender"}, 1, r.user); err != nil {
		t.Fatal(err)
	}
	a, _ := r.s.Submit(ctx, actor, r.req())
	b2, _ := r.s.Submit(ctx, actor, r.req())
	r.s.Tick(ctx)
	ja, _ := r.s.Job(ctx, r.space, a.ID)
	jb, _ := r.s.Job(ctx, r.space, b2.ID)
	if ja.Status != hubproto.HeavyRunning || jb.Status != hubproto.HeavyQueued || !strings.Contains(jb.Reason, "busy") {
		t.Fatalf("one slot: %s / %s %q", ja.Status, jb.Status, jb.Reason)
	}
	// cancelling the queued one is at once; the running one asks its builder
	jb, _ = r.s.Cancel(ctx, jb)
	if jb.Status != hubproto.HeavyCancelled {
		t.Fatal(jb.Status)
	}
	ja, _ = r.s.Cancel(ctx, ja)
	if !strings.Contains(strings.Join(r.link.calls, "|"), hubproto.OpJobCancel) {
		t.Fatalf("builder not asked: %v", r.link.calls)
	}
	r.link.state[a.ID] = hubproto.JobState{Status: hubproto.HeavyCancelled}
	r.s.Tick(ctx)
	if ja, _ = r.s.Job(ctx, r.space, a.ID); ja.Status != hubproto.HeavyCancelled {
		t.Fatal(ja.Status)
	}
	// a job the builder lost
	c, _ := r.s.Submit(ctx, actor, r.req())
	r.s.Tick(ctx)
	for range 3 {
		delete(r.link.state, c.ID)
		r.s.Tick(ctx)
	}
	if jc, _ := r.s.Job(ctx, r.space, c.ID); jc.Status != hubproto.HeavyFailed {
		t.Fatalf("%s", jc.Status)
	}
	// a machine that was never a builder, and a bad role
	if _, err := r.s.SetRole(ctx, r.b1, "boss", nil, 1, r.user); err == nil {
		t.Fatal("bad role accepted")
	}
	if _, err := r.s.SetRole(ctx, r.b1, "agents", nil, 0, r.user); err != nil {
		t.Fatal(err)
	}
	d, _ := r.s.Submit(ctx, actor, r.req())
	if !d.Local {
		t.Fatalf("a machine that went back to agents takes no jobs: %+v", d)
	}
}

func TestStartRefusedFailsTheJob(t *testing.T) {
	ctx := context.Background()
	r := newRig(t)
	if _, err := r.s.SetRole(ctx, r.b1, "builder", []string{"blender"}, 1, r.user); err != nil {
		t.Fatal(err)
	}
	r.link.start = httpx.New(403, "machine_policy", "tool not allowed")
	j, _ := r.s.Submit(ctx, Actor{SpaceID: r.space, UserID: r.user, MachineID: r.home.ID}, r.req())
	r.s.Tick(ctx)
	if got, _ := r.s.Job(ctx, r.space, j.ID); got.Status != hubproto.HeavyFailed || !strings.Contains(got.Error, "refused") {
		t.Fatalf("%+v", got)
	}
}

func TestRequestValidation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	actor := Actor{SpaceID: r.space, UserID: r.user, MachineID: r.home.ID}
	bad := []func(q *Request){
		func(q *Request) { q.Argv = nil },
		func(q *Request) { q.Argv = []string{"/bin/sh", "-c", "x"} },
		func(q *Request) { q.Env = map[string]string{"LD_PRELOAD": "x"} },
		func(q *Request) { q.Outputs = []string{"../etc"} },
		func(q *Request) { q.Need.Caps = []string{"Bad Cap!"} },
		func(q *Request) { q.TimeoutSec = 99 * 3600 },
		func(q *Request) { q.Need.Kind = "chat" },
	}
	for i, f := range bad {
		q := r.req()
		f(&q)
		_, err := r.s.Submit(ctx, actor, q)
		var he *httpx.Error
		if !errors.As(err, &he) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
}

func TestFiles(t *testing.T) {
	f := Files{Root: t.TempDir()}
	id := "abcdefgh12"
	if _, err := f.PutOut(id, "../x", strings.NewReader("x")); err == nil {
		t.Fatal("path escape accepted")
	}
	if _, err := f.PutOut(id, "a/b.png", strings.NewReader("12345")); err != nil {
		t.Fatal(err)
	}
	if l := f.ListOut(id); len(l) != 1 || l[0].Path != "a/b.png" || l[0].Size != 5 {
		t.Fatalf("%+v", l)
	}
	if f.HasInput(id) {
		t.Fatal("input before upload")
	}
	f.Remove(id)
	if len(f.ListOut(id)) != 0 {
		t.Fatal("not removed")
	}
}
