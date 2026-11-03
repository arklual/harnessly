package orgs_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/machines"
	"github.com/ly-solutions/harnessly/internal/hub/provider"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ---- fakes ----

type fakeGate struct{ on atomic.Bool }

func (g *fakeGate) On(context.Context, string, string) bool { return g.on.Load() }

type fakePub struct {
	mu     sync.Mutex
	events []string
}

func (p *fakePub) Publish(_ string, kind string, _ any) {
	p.mu.Lock()
	p.events = append(p.events, kind)
	p.mu.Unlock()
}

func (p *fakePub) count(kind string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, k := range p.events {
		if k == kind {
			n++
		}
	}
	return n
}

type resCase struct {
	veraCase
	mem  *provider.Memory
	gate *fakeGate
	pub  *fakePub
}

// newRes is a hub with the resources side wired: the feature on, an in-memory provider that finishes at the first poll
// (none when withProvider is false).
func newRes(t *testing.T, withProvider bool) resCase {
	t.Helper()
	rc := resCase{mem: provider.NewMemory(), gate: &fakeGate{}, pub: &fakePub{}}
	rc.gate.on.Store(true)
	rc.mem.Auto = true
	rc.mem.Catalog = []provider.Plan{
		{ID: "s", Name: "S", CPU: 2, RAMGB: 4, DiskGB: 50, Current: true, Price: &provider.Price{Amount: 100000, Currency: "RUB", Period: "month"}},
		{ID: "m", Name: "M", CPU: 4, RAMGB: 8, DiskGB: 80, Price: &provider.Price{Amount: 200000, Currency: "RUB", Period: "month"}},
		{ID: "n", Name: "N", CPU: 4, RAMGB: 8, DiskGB: 40},
	}
	rc.mem.Console = "https://console.example.org/servers/1"
	rc.mem.PriceFn = func(r provider.Resources) provider.Price {
		return provider.Price{Amount: int64(r.CPU*40000 + r.RAMGB*10000 + r.DiskGB*100), Currency: "RUB", Period: "month"}
	}
	rc.veraCase = newVera(t, hubtest.WithOptions(func(o *api.Options) {
		if withProvider {
			o.Provider = rc.mem
		}
		o.Features = rc.gate
		o.Publisher = rc.pub
	}))
	rc.s.Hub.Machines.SetPollEvery(15 * time.Millisecond)
	return rc
}

// agent connects a machine of the tenant that says it can sample load and drain.
func (rc resCase) agent(t *testing.T, features ...string) *hubtest.Agent {
	t.Helper()
	a := rc.s.NewAgent(nil)
	a.Hello.Features = append(slices.Clone(a.Hello.Features), features...)
	a.Hello.Hardware = &hubproto.Hardware{Cores: 4, RAMBytes: 8 << 30, DiskBytes: 80 << 30, Model: "Example CPU", OS: "linux", Arch: "amd64"}
	if r := a.Register(rc.anna.NewCode(rc.tenant, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	return a
}

func (rc resCase) manage(t *testing.T, id string) {
	t.Helper()
	if r := rc.anna.Do("PUT", "/api/machines/"+id+"/managed", map[string]any{"capability": "resources", "managed": true}); r.Code != http.StatusOK {
		t.Fatalf("managed: %s", r)
	}
}

func (rc resCase) waitOp(t *testing.T, id, opID, status string) map[string]any {
	t.Helper()
	var last map[string]any
	rc.s.Eventually("operation "+opID+" to be "+status, func() bool {
		r := rc.anna.Do("GET", "/api/machines/"+id+"/ops", nil)
		var out struct{ Ops []map[string]any }
		r.JSON(&out)
		for _, o := range out.Ops {
			if o["id"] == opID {
				last = o
				return o["status"] == status
			}
		}
		return false
	})
	return last
}

func (rc resCase) resources(t *testing.T, id string) map[string]any {
	t.Helper()
	r := rc.anna.Do("GET", "/api/machines/"+id+"/resources", nil)
	if r.Code != http.StatusOK {
		t.Fatalf("resources: %s", r)
	}
	var out map[string]any
	r.JSON(&out)
	return out
}

// ---- the gate ----

func TestResourcesAreDarkWithoutTheFeature(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t, hubproto.FeatureMetrics, hubproto.FeatureDrain)
	id := a.MachineID
	rc.gate.on.Store(false)

	for _, c := range []struct{ method, path string }{
		{"GET", "/resources"}, {"POST", "/resources/quote"}, {"POST", "/resize"}, {"PUT", "/managed"}, {"GET", "/ops"},
		{"POST", "/ops/x/cancel"}, {"GET", "/metrics"},
	} {
		r := rc.anna.Do(c.method, "/api/machines/"+id+c.path, map[string]any{})
		if r.Code != http.StatusNotFound || r.Err() != "not_enabled" {
			t.Errorf("%s %s: %s", c.method, c.path, r)
		}
	}
	// Nobody else is let in either way: a person without rights does not learn that the feature is off.
	if r := rc.gleb.Do("GET", "/api/machines/"+id+"/resources", nil); r.Code != http.StatusForbidden {
		t.Errorf("a stranger: %s", r)
	}

	// The list carries none of the new parts, and the restart is the old one.
	m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
	if m == nil || m.Cloud != nil || m.Hardware != nil || m.Managed != nil || m.Load != nil || len(m.ActiveOps) != 0 {
		t.Errorf("list row with the feature off: %+v", m)
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "vm", "when": "now"}); r.Code != http.StatusNotImplemented || r.Err() != "not_implemented" {
		t.Errorf("vm restart with the feature off: %s", r)
	}
	a.Handle(hubproto.OpRuntimeRestart, func(hubtest.Call) (any, error) { return map[string]any{}, nil })
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "after_chats"}); r.Code != http.StatusAccepted {
		t.Errorf("old restart: %s", r)
	}
	if ops, err := rc.s.DB.S().OpsByMachine(context.Background(), id, false, 10); err != nil || len(ops) != 0 {
		t.Errorf("an operation was stored with the feature off: %v %v", ops, err)
	}
	if got := rc.mem.Calls(); len(got) != 0 {
		t.Errorf("provider calls = %v", got)
	}
}

func TestResourcesWithoutAProvider(t *testing.T) {
	rc := newRes(t, false)
	a := rc.agent(t)
	id := a.MachineID

	res := rc.resources(t, id)
	prov, _ := res["provider"].(map[string]any)
	if prov["name"] != "none" || prov["supported"] != false || res["managed"] != false || res["consoleUrl"] != nil || res["current"] != nil {
		t.Errorf("resources = %v", res)
	}
	if plans, _ := res["plans"].([]any); plans == nil || len(plans) != 0 {
		t.Errorf("plans = %v, want []", res["plans"])
	}
	hw, _ := res["hardware"].(map[string]any)
	if hw["cores"] != float64(4) || hw["model"] != "Example CPU" {
		t.Errorf("hardware from hello = %v", res["hardware"])
	}
	for _, path := range []string{"/resources/quote", "/resize"} {
		r := rc.anna.Do("POST", "/api/machines/"+id+path, map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80})
		if r.Code != http.StatusUnprocessableEntity || r.Err() != "not_supported" {
			t.Errorf("%s: %s", path, r)
		}
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "machine", "when": "now"}); r.Code != http.StatusUnprocessableEntity || r.Err() != "not_supported" {
		t.Errorf("machine restart: %s", r)
	}
	// The runner restart needs no provider.
	a.Handle(hubproto.OpRuntimeRestart, func(hubtest.Call) (any, error) { return hubproto.RestartEvent{State: hubproto.RestartRestarting}, nil })
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusAccepted {
		t.Errorf("runner restart: %s", r)
	}
}

// ---- resources, quote, resize ----

func TestResourcesQuoteAndResize(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t)
	id := a.MachineID

	res := rc.resources(t, id)
	cur, _ := res["current"].(map[string]any)
	price, _ := res["price"].(map[string]any)
	if cur["cpu"] != float64(2) || cur["ramGB"] != float64(4) || cur["diskGB"] != float64(50) || price["amount"] != float64(100000) || price["currency"] != "RUB" {
		t.Fatalf("current = %v price = %v", res["current"], res["price"])
	}
	if res["consoleUrl"] != "https://console.example.org/servers/1" || res["managed"] != false {
		t.Errorf("resources = %v", res)
	}
	plans, _ := res["plans"].([]any)
	if len(plans) != 3 || plans[2].(map[string]any)["price"] != nil || plans[0].(map[string]any)["current"] != true {
		t.Errorf("plans = %v (a plan without a price says null)", plans)
	}

	// Quote: the price is the provider's, the delta is against what the machine costs now.
	r := rc.anna.Do("POST", "/api/machines/"+id+"/resources/quote", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80})
	var q struct {
		Price, Current, Delta map[string]any
	}
	r.JSON(&q)
	if r.Code != http.StatusOK || q.Price["amount"] != float64(4*40000+8*10000+80*100) || q.Current["amount"] != float64(100000) ||
		q.Delta["amount"] != float64(4*40000+8*10000+80*100-100000) {
		t.Errorf("quote: %s", r)
	}
	for _, body := range []map[string]any{{"cpu": 0, "ramGB": 8, "diskGB": 80}, {"cpu": 4, "ramGB": 0, "diskGB": 80}, {}} {
		if r := rc.anna.Do("POST", "/api/machines/"+id+"/resources/quote", body); r.Code != http.StatusBadRequest {
			t.Errorf("quote %v: %s", body, r)
		}
	}
	if r := rc.vera.Do("POST", "/api/machines/"+id+"/resources/quote", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80}); r.Code != http.StatusForbidden {
		t.Errorf("a member quotes: %s", r)
	}
	// A machine of no team is read by the tenant's managers; a plain member of the tenant is not one of them.
	if r := rc.vera.Do("GET", "/api/machines/"+id+"/resources", nil); r.Code != http.StatusForbidden {
		t.Errorf("a member reads: %s", r)
	}

	// Not managed: the hub does not touch the server.
	want := map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", want); r.Code != http.StatusConflict || r.Err() != "not_managed" {
		t.Errorf("resize of an unmanaged machine: %s", r)
	}
	if r := rc.vera.Do("PUT", "/api/machines/"+id+"/managed", map[string]any{"capability": "resources", "managed": true}); r.Code != http.StatusForbidden {
		t.Errorf("a member manages: %s", r)
	}
	if r := rc.anna.Do("PUT", "/api/machines/"+id+"/managed", map[string]any{"capability": "nope", "managed": true}); r.Code != http.StatusBadRequest {
		t.Errorf("unknown capability: %s", r)
	}
	rc.manage(t, id)
	if m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box"); m == nil || m.Managed == nil || !m.Managed.Resources || m.Managed.Budget {
		t.Errorf("managed in the list: %+v", m)
	}

	// The refusals before anything reaches the provider.
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 40}); r.Code != http.StatusUnprocessableEntity || r.Err() != "disk_shrink" {
		t.Errorf("disk shrink: %s", r)
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 2, "ramGB": 4, "diskGB": 50}); r.Code != http.StatusConflict || r.Err() != "no_change" {
		t.Errorf("no change: %s", r)
	}
	if r := rc.vera.Do("POST", "/api/machines/"+id+"/resize", want); r.Code != http.StatusForbidden {
		t.Errorf("a member resizes: %s", r)
	}
	if got := rc.mem.Calls(); len(got) != 0 {
		t.Fatalf("provider calls before a real resize = %v", got)
	}

	// The resize: accepted at once, done when the provider says so, and the stored size follows.
	r = rc.anna.Do("POST", "/api/machines/"+id+"/resize", want)
	var out struct{ Op map[string]any }
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.Op["kind"] != "resize" || out.Op["machineId"] != id {
		t.Fatalf("resize: %s", r)
	}
	opID, _ := out.Op["id"].(string)
	if len(opID) != 32 {
		t.Errorf("op id = %q", opID)
	}
	if got := rc.mem.Calls(); len(got) != 1 || got[0] != "resize:"+id+":4/8/80" {
		t.Errorf("provider calls = %v", got)
	}
	done := rc.waitOp(t, id, opID, "done")
	if done["finishedAt"] == nil || done["error"] != nil {
		t.Errorf("finished op = %v", done)
	}
	rc.s.Eventually("the stored size", func() bool {
		c, _ := rc.resources(t, id)["current"].(map[string]any)
		return c["cpu"] == float64(4) && c["diskGB"] == float64(80)
	})
	if p, _ := rc.resources(t, id)["price"].(map[string]any); p["amount"] != float64(200000) {
		t.Errorf("price after the resize = %v", p)
	}
	if rc.pub.count(machines.EventMachineOp) < 3 {
		t.Errorf("machine.op events = %v", rc.pub.events)
	}
	if ops := rc.anna.Do("GET", "/api/machines/"+id+"/ops?active=1", nil); !strings.Contains(string(ops.Body), `"ops":[]`) {
		t.Errorf("active ops after the end: %s", ops)
	}
	if r := rc.anna.Do("GET", "/api/machines/"+id+"/ops?active=x", nil); r.Code != http.StatusBadRequest {
		t.Errorf("active=x: %s", r)
	}
}

func TestOneOperationPerMachine(t *testing.T) {
	rc := newRes(t, true)
	rc.mem.Auto = false
	a := rc.agent(t)
	id := a.MachineID
	rc.manage(t, id)

	r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80})
	var out struct{ Op map[string]any }
	r.JSON(&out)
	if r.Code != http.StatusAccepted {
		t.Fatalf("resize: %s", r)
	}
	opID := out.Op["id"].(string)
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 8, "ramGB": 16, "diskGB": 100}); r.Code != http.StatusConflict || r.Err() != "op_busy" {
		t.Errorf("second resize: %s", r)
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "machine", "when": "now"}); r.Code != http.StatusConflict || r.Err() != "op_busy" {
		t.Errorf("restart during a resize: %s", r)
	}
	// What the provider has begun cannot be taken back.
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/ops/"+opID+"/cancel", map[string]any{}); r.Code != http.StatusConflict || r.Err() != "not_cancellable" {
		t.Errorf("cancel of a resize: %s", r)
	}
	if res := rc.resources(t, id); len(res["activeOps"].([]any)) != 1 {
		t.Errorf("activeOps = %v", res["activeOps"])
	}
	if m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box"); m == nil || len(m.ActiveOps) != 1 || m.ActiveOps[0].ID != opID {
		t.Errorf("activeOps in the list: %+v", m)
	}
	rc.mem.Complete("op-1", "quota exceeded")
	failed := rc.waitOp(t, id, opID, "failed")
	if failed["error"] != "quota exceeded" {
		t.Errorf("failed op = %v", failed)
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/ops/"+opID+"/cancel", map[string]any{}); r.Code != http.StatusConflict || r.Err() != "op_finished" {
		t.Errorf("cancel of a finished op: %s", r)
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/ops/nope/cancel", map[string]any{}); r.Code != http.StatusNotFound {
		t.Errorf("cancel of an unknown op: %s", r)
	}
	// The provider refuses the call itself: the request fails and the operation ends failed.
	rc.mem.FailNext(&provider.Error{Status: 402, Code: "no_funds", Message: "top up the account"})
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/resize", map[string]any{"cpu": 4, "ramGB": 8, "diskGB": 80}); r.Code < 400 {
		t.Errorf("refused call: %s", r)
	}
	if ops := rc.anna.Do("GET", "/api/machines/"+id+"/ops?active=1", nil); !strings.Contains(string(ops.Body), `"ops":[]`) {
		t.Errorf("a refused call left an operation open: %s", ops)
	}
}

// ---- restart ----

func restartEvent(a *hubtest.Agent, jobID, state string, chats *int) {
	a.Emit(map[string]any{"event": hubproto.EventRestart, "restart": hubproto.RestartEvent{JobID: jobID, State: state, ChatsNow: chats}})
}

func intp(n int) *int { return &n }

func TestRestartRunner(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t, hubproto.FeatureDrain)
	id := a.MachineID
	a.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) {
		return hubproto.RestartEvent{JobID: c.Str("jobId"), State: hubproto.RestartRestarting}, nil
	})

	r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"})
	var out struct {
		JobID string
		Op    map[string]any
	}
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.JobID == "" || out.JobID != out.Op["id"] || out.Op["kind"] != "restart_runner" || out.Op["status"] != "running" {
		t.Fatalf("restart: %s", r)
	}
	if c := a.Calls(hubproto.OpRuntimeRestart); len(c) != 1 || c[0].Str("jobId") != out.JobID || c[0].Str("when") != "now" {
		t.Errorf("agent calls = %+v", c)
	}
	restartEvent(a, out.JobID, hubproto.RestartDone, nil)
	rc.waitOp(t, id, out.JobID, "done")

	// An agent's job event for an operation the hub does not know changes nothing.
	restartEvent(a, "ffffffffffffffffffffffffffffffff", hubproto.RestartFailed, nil)

	// Waiting for the chats: the number goes with the operation, and the operation can be taken back.
	a.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) {
		return hubproto.RestartEvent{JobID: c.Str("jobId"), State: hubproto.RestartWaiting, ChatsNow: intp(3)}, nil
	})
	a.Handle(hubproto.OpRuntimeRestartCancel, func(hubtest.Call) (any, error) { return map[string]any{}, nil })
	r = rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "drain"})
	out.Op = nil
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.Op["kind"] != "drain" || out.Op["status"] != "pending" || out.Op["chatsNow"] != float64(3) {
		t.Fatalf("drain: %s", r)
	}
	restartEvent(a, out.JobID, hubproto.RestartWaiting, intp(1))
	rc.s.Eventually("the number of chats", func() bool {
		var l struct{ Ops []map[string]any }
		rc.anna.Do("GET", "/api/machines/"+id+"/ops?active=1", nil).JSON(&l)
		return len(l.Ops) == 1 && l.Ops[0]["chatsNow"] == float64(1)
	})
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusConflict || r.Err() != "op_busy" {
		t.Errorf("restart during a drain: %s", r)
	}
	if r := rc.vera.Do("POST", "/api/machines/"+id+"/ops/"+out.JobID+"/cancel", map[string]any{}); r.Code != http.StatusForbidden {
		t.Errorf("a member cancels: %s", r)
	}
	r = rc.anna.Do("POST", "/api/machines/"+id+"/ops/"+out.JobID+"/cancel", map[string]any{})
	var c struct{ Op map[string]any }
	r.JSON(&c)
	if r.Code != http.StatusOK || c.Op["status"] != "cancelled" {
		t.Fatalf("cancel: %s", r)
	}
	if cc := a.Calls(hubproto.OpRuntimeRestartCancel); len(cc) != 1 || cc[0].Str("jobId") != out.JobID {
		t.Errorf("cancel calls = %+v", cc)
	}
	// The old spelling still works.
	r = rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "after_chats"})
	if r.Code != http.StatusAccepted {
		t.Errorf("after_chats: %s", r)
	}
}

func TestDrainNeedsAnAgentThatCanDoIt(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t) // no drain feature
	id := a.MachineID
	rc.manage(t, id)
	a.Handle(hubproto.OpRuntimeRestart, func(hubtest.Call) (any, error) { return hubproto.RestartEvent{State: hubproto.RestartRestarting}, nil })
	for _, what := range []string{"runner", "machine"} {
		r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": what, "when": "drain"})
		if r.Code != http.StatusUnprocessableEntity || r.Err() != "drain_unsupported" {
			t.Errorf("%s drain: %s", what, r)
		}
	}
	if got := rc.mem.Calls(); len(got) != 0 {
		t.Errorf("provider calls = %v", got)
	}
	for _, body := range []map[string]any{{"what": "disk", "when": "now"}, {"what": "runner", "when": "tomorrow"}, {}} {
		if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", body); r.Code != http.StatusBadRequest {
			t.Errorf("%v: %s", body, r)
		}
	}
	for _, p := range []*hubtest.Person{rc.vera, rc.boris} {
		if r := p.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "runner", "when": "now"}); r.Code != http.StatusForbidden {
			t.Errorf("restart by a person without rights: %s", r)
		}
	}
}

func TestRestartMachine(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t, hubproto.FeatureDrain)
	id := a.MachineID
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "machine", "when": "now"}); r.Code != http.StatusConflict || r.Err() != "not_managed" {
		t.Errorf("unmanaged: %s", r)
	}
	rc.manage(t, id)

	// At once: the provider reboots the server.
	r := rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "vm", "when": "now"})
	var out struct {
		JobID string
		Op    map[string]any
	}
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.Op["kind"] != "restart_machine" {
		t.Fatalf("restart machine: %s", r)
	}
	if got := rc.mem.Calls(); len(got) != 1 || got[0] != "reboot:"+id {
		t.Errorf("provider calls = %v", got)
	}
	rc.waitOp(t, id, out.JobID, "done")

	// After the drain: the server is rebooted once the agent says the chats are over, not before.
	a.Handle(hubproto.OpRuntimeRestart, func(c hubtest.Call) (any, error) {
		return hubproto.RestartEvent{JobID: c.Str("jobId"), State: hubproto.RestartWaiting, ChatsNow: intp(2)}, nil
	})
	r = rc.anna.Do("POST", "/api/machines/"+id+"/restart", map[string]any{"what": "machine", "when": "drain"})
	out.Op = nil
	r.JSON(&out)
	if r.Code != http.StatusAccepted || out.Op["status"] != "pending" || out.Op["chatsNow"] != float64(2) {
		t.Fatalf("machine drain: %s", r)
	}
	time.Sleep(60 * time.Millisecond)
	if got := rc.mem.Calls(); len(got) != 1 {
		t.Fatalf("rebooted before the chats were done: %v", got)
	}
	restartEvent(a, out.JobID, hubproto.RestartDone, nil)
	rc.s.Eventually("the reboot", func() bool { return len(rc.mem.Calls()) == 2 })
	rc.waitOp(t, id, out.JobID, "done")
	// A second "done" for the same job reboots nothing more.
	restartEvent(a, out.JobID, hubproto.RestartDone, nil)
	time.Sleep(60 * time.Millisecond)
	if got := rc.mem.Calls(); len(got) != 2 {
		t.Errorf("provider calls = %v", got)
	}
}

func TestDisablingAMachinePowersTheServerOff(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t)
	id := a.MachineID

	// Not managed: only the machine is disabled.
	r := rc.anna.Do("POST", "/api/machines/"+id+"/disable", map[string]any{})
	var out map[string]any
	r.JSON(&out)
	if r.Code != http.StatusOK || out["op"] != nil || len(rc.mem.Calls()) != 0 {
		t.Fatalf("disable of an unmanaged machine: %s %v", r, rc.mem.Calls())
	}
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/enable", map[string]any{}); r.Code != http.StatusOK {
		t.Fatalf("enable: %s", r)
	}

	rc.manage(t, id)
	r = rc.anna.Do("POST", "/api/machines/"+id+"/disable", map[string]any{})
	var d struct{ Op map[string]any }
	r.JSON(&d)
	if r.Code != http.StatusOK || d.Op["kind"] != "power_off" {
		t.Fatalf("disable: %s", r)
	}
	rc.waitOp(t, id, d.Op["id"].(string), "done")
	// Twice is the same: no second operation.
	if r := rc.anna.Do("POST", "/api/machines/"+id+"/disable", map[string]any{}); r.Code != http.StatusOK || strings.Contains(string(r.Body), `"power_off"`) {
		t.Errorf("disable twice: %s", r)
	}
	r = rc.anna.Do("POST", "/api/machines/"+id+"/enable", map[string]any{})
	d.Op = nil
	r.JSON(&d)
	if r.Code != http.StatusOK || d.Op["kind"] != "power_on" {
		t.Fatalf("enable: %s", r)
	}
	if got := rc.mem.Calls(); !slices.Equal(got, []string{"power-off:" + id, "power-on:" + id}) {
		t.Errorf("provider calls = %v", got)
	}
}

// ---- load ----

func TestLoadSamples(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t, hubproto.FeatureMetrics)
	id := a.MachineID

	var sub struct{ Events []string }
	for _, c := range a.Calls(hubproto.OpLinkSubscribe) {
		_ = json.Unmarshal(c.Params["events"], &sub.Events)
	}
	if !slices.Contains(sub.Events, hubproto.EventMetricsSample) {
		t.Errorf("subscribed events = %v, want %s among them", sub.Events, hubproto.EventMetricsSample)
	}

	cpu, ram, disk := 41.26, 62.0, 55.5
	a.Emit(map[string]any{"event": hubproto.EventMetricsSample, "sample": hubproto.MetricsSample{CPUPct: &cpu, RAMPct: &ram, DiskPct: &disk, ChatsNow: intp(2), At: rc.s.Clock.Now().UnixMilli()}})
	rc.s.Eventually("the load in the list", func() bool {
		m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
		return m != nil && m.Load != nil
	})
	m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
	if m.Load.CPUPct == nil || *m.Load.CPUPct != 41.3 || *m.Load.RAMPct != 62 || *m.Load.DiskPct != 55.5 {
		t.Errorf("load = %+v", m.Load)
	}
	if m.Hardware == nil || m.Hardware.Cores == nil || *m.Hardware.Cores != 4 {
		t.Errorf("hardware = %+v", m.Hardware)
	}

	r := rc.anna.Do("GET", "/api/machines/"+id+"/metrics?range=1h", nil)
	var out struct {
		Range       string
		StepSeconds int
		Points      []map[string]any
	}
	r.JSON(&out)
	if r.Code != http.StatusOK || out.Range != "1h" || out.StepSeconds <= 0 || len(out.Points) != 1 {
		t.Fatalf("metrics: %s", r)
	}
	p := out.Points[0]
	if p["cpuAvg"] != 41.3 || p["ramMax"] != float64(62) || p["chatsMax"] != float64(2) || p["samples"] != float64(1) {
		t.Errorf("point = %v", p)
	}
	if r := rc.anna.Do("GET", "/api/machines/"+id+"/metrics?range=1y", nil); r.Code != http.StatusBadRequest {
		t.Errorf("range=1y: %s", r)
	}
	if r := rc.anna.Do("GET", "/api/machines/"+id+"/metrics", nil); r.Code != http.StatusOK || !strings.Contains(string(r.Body), `"range":"24h"`) {
		t.Errorf("default range: %s", r)
	}

	// A machine that is gone shows no load.
	a.Close()
	rc.s.Eventually("the machine to go", func() bool {
		m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box")
		return m != nil && m.Status != "online"
	})
	if m := machineByName(rc.machinesList(t, rc.anna, ""), "dev-box"); m.Load != nil {
		t.Errorf("load of an offline machine = %+v", m.Load)
	}
}

func TestLoadIsNotKeptWhenTheFeatureIsOff(t *testing.T) {
	rc := newRes(t, true)
	rc.gate.on.Store(false)
	a := rc.agent(t, hubproto.FeatureMetrics)
	cpu, ram, disk := 10.0, 10.0, 10.0
	a.Emit(map[string]any{"event": hubproto.EventMetricsSample, "sample": hubproto.MetricsSample{CPUPct: &cpu, RAMPct: &ram, DiskPct: &disk, At: rc.s.Clock.Now().UnixMilli()}})
	time.Sleep(100 * time.Millisecond)
	bs, err := rc.s.DB.S().MetricsRange(context.Background(), a.MachineID, 0, rc.s.Clock.Now().UnixMilli()+1)
	if err != nil || len(bs) != 0 {
		t.Errorf("buckets = %v %v", bs, err)
	}
	if hw, _ := rc.s.DB.S().HWByMachines(context.Background(), []string{a.MachineID}); len(hw) != 0 {
		t.Errorf("hardware saved with the feature off: %v", hw)
	}
}

// ---- after a restart of the hub, and upkeep ----

func TestResumeAndMaintainOperations(t *testing.T) {
	rc := newRes(t, true)
	a := rc.agent(t)
	id := a.MachineID
	rc.manage(t, id)
	ctx := context.Background()
	st := rc.s.DB.S()
	now := rc.s.Clock.Now()

	mk := func(kind, providerOp string, age time.Duration) string {
		t.Helper()
		op := store.MachineOp{ID: store.RandomHex(16), MachineID: id, Kind: kind, Status: store.OpRunning, Params: "{}",
			ProviderOpID: providerOp, CreatedAt: now.Add(-age), UpdatedAt: now.Add(-age)}
		if err := st.CreateOp(ctx, &op); err != nil {
			t.Fatal(err)
		}
		return op.ID
	}
	state := func(opID string) string {
		op, err := st.OpByID(ctx, opID)
		if err != nil {
			t.Fatal(err)
		}
		return op.Status
	}

	// A fresh operation that waited for chats is written off at start; one at the provider is asked about again.
	waiting := mk(store.OpRestartMachine, "", time.Minute)
	rc.s.Hub.Machines.ResumeOps(ctx)
	if got := state(waiting); got != store.OpFailed {
		t.Errorf("waiting restart after the hub restarted = %s", got)
	}

	// Maintain leaves a young operation alone and writes off a stale one.
	young := mk(store.OpReboot, "op-x", time.Minute)
	rc.s.Hub.Machines.Maintain(ctx)
	if got := state(young); got != store.OpRunning {
		t.Errorf("young op = %s", got)
	}
	old := mk(store.OpReboot, "op-x", 3*time.Hour)
	rc.s.Hub.Machines.Maintain(ctx)
	if got := state(old); got != store.OpFailed {
		t.Errorf("stale op = %s", got)
	}
	_, _ = st.SetOp(ctx, young, store.OpDone, "", "", now)

	// The provider does not know the operation: it is written off, not watched forever.
	unknown := mk(store.OpReboot, "no-such-op", time.Minute)
	rc.s.Hub.Machines.ResumeOps(ctx)
	rc.s.Eventually("the unknown op to fail", func() bool { return state(unknown) == store.OpFailed })

	// Old load buckets are deleted by the upkeep.
	if err := st.AddMetricSample(ctx, id, now.Add(-store.MetricsRetention-time.Hour).UnixMilli(), 1, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	rc.s.Clock.Advance(11 * time.Minute)
	rc.s.Hub.Machines.Maintain(ctx)
	if bs, err := st.MetricsRange(ctx, id, 0, now.UnixMilli()); err != nil || len(bs) != 0 {
		t.Errorf("old buckets = %v %v", bs, err)
	}
}
