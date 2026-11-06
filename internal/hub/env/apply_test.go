package env_test

import (
	"context"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/env"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestApplyOnTheMachines(t *testing.T) {
	defer env.SetApplyDelay(10 * time.Millisecond)()
	a := newAcme(t)
	status := "ok"
	box := setupAgent(t, a, a.anna, a.tenant, withSetup, &status)
	apply := func(when string) (env.ApplyResponse, int) {
		r := a.anna.Do("POST", a.url("/env/apply"), map[string]any{"layer": "tenant", "scopeId": a.tenant, "when": when})
		var out env.ApplyResponse
		r.JSON(&out)
		return out, r.Code
	}

	// «Сейчас»: the online machine runs an update at once.
	if out, code := apply(env.ApplyNow); code != 200 || out.Machines != 1 || out.Started != 1 || out.Waiting != 0 {
		t.Fatalf("now: %d %+v", code, out)
	}
	if calls := box.Calls(hubproto.OpEnvRun); len(calls) != 1 || calls[0].Str("mode") != hubproto.RunUpdate {
		t.Fatalf("calls = %+v", calls)
	}

	// «При следующем запуске»: nothing now, the run comes when the machine connects again, once.
	if out, code := apply(env.ApplyNext); code != 200 || out.Started != 0 || out.Waiting != 1 {
		t.Fatalf("next: %d %+v", code, out)
	}
	if n := len(box.Calls(hubproto.OpEnvRun)); n != 1 {
		t.Fatalf("next ran at once: %d", n)
	}
	box.Close()
	box.Connect()
	a.s.Eventually("the pending run", func() bool { return len(box.Calls(hubproto.OpEnvRun)) == 2 })
	// The mark goes once the machine answered; a run whose answer was lost is asked again at the next connection.
	// The machine's answer reaches the hub a moment after its handler ran: closing the link before that loses it.
	time.Sleep(300 * time.Millisecond)
	if n, err := a.s.DB.S().EnvApplyPendingCount(context.Background(), a.org); err != nil || n != 0 {
		t.Fatalf("the mark after the run: %d %v", n, err)
	}
	box.Close()
	box.Connect()
	time.Sleep(100 * time.Millisecond)
	if n := len(box.Calls(hubproto.OpEnvRun)); n != 2 {
		t.Fatalf("the mark was not taken off: %d runs", n)
	}

	// Bad input and no rights.
	if _, code := apply("later"); code != 400 {
		t.Errorf("bad when: %d", code)
	}
	if r := a.boris.Do("POST", a.url("/env/apply"), map[string]any{"layer": "tenant", "scopeId": a.tenant, "when": "now"}); r.Code != 403 {
		t.Errorf("a team lead applies the tenant: %s", r)
	}
}
