package eval

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/efficiency"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// the hub's scheduler of the evaluation. Slots are counted in Moscow time from a fixed Monday; nothing is queued
// or started for a person whose switch is off; a run waits while the subscription's windows are filling up or the
// readings are old; a finished batch is closed and drawn on the efficiency charts.

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, msk)
	if err != nil {
		panic(err)
	}
	return t
}

func TestSlots(t *testing.T) {
	days := []int{1}
	if s, ok := LatestSlot(days, "11:30", 4, at("2026-11-02 12:00")); !ok || !s.Equal(at("2026-11-02 11:30")) {
		t.Fatalf("every 4th Monday: %v %v", s, ok)
	}
	if _, ok := LatestSlot(days, "11:30", 4, at("2026-11-09 12:00")); ok {
		t.Fatal("a Monday of a skipped week has no slot")
	}
	if s, ok := NextSlot(days, "11:30", 4, at("2026-11-09 12:00")); !ok || !s.Equal(at("2026-11-30 11:30")) {
		t.Fatalf("next: %v %v", s, ok)
	}
	// a slot later today is not the latest one yet; Sunday is day 7
	if s, ok := LatestSlot([]int{1, 7}, "10:00", 1, at("2026-11-09 09:00")); !ok || !s.Equal(at("2026-11-08 10:00")) {
		t.Fatalf("latest before the slot: %v %v", s, ok)
	}
	if _, ok := LatestSlot(days, "25:00", 1, at("2026-11-09 12:00")); ok {
		t.Fatal("a bad time gives no slot")
	}
}

type fakeGate map[string]bool

func (g fakeGate) On(_ context.Context, feature, person, _ string) bool { return g[feature+"|"+person] }

type fakeLink struct {
	connected bool
	started   []RunRequest
	states    map[string]RunState
}

func (l *fakeLink) Connected(string) bool { return l.connected }
func (l *fakeLink) Start(_ context.Context, _ string, r RunRequest) error {
	l.started = append(l.started, r)
	return nil
}
func (l *fakeLink) Poll(_ context.Context, _ string, ids []string) (map[string]RunState, error) {
	out := map[string]RunState{}
	for _, id := range ids {
		if s, ok := l.states[id]; ok {
			out[id] = s
		}
	}
	return out, nil
}
func (l *fakeLink) Stop(context.Context, string, string) error { return nil }

type env struct {
	db   *store.DB
	s    *Scheduler
	link *fakeLink
	now  time.Time
	gate fakeGate
}

func setup(t *testing.T, week, five float64) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{db: db, now: at("2026-11-09 10:05"), link: &fakeLink{connected: true}, gate: fakeGate{SwitchScheduler + "|ann@example.com": true}}
	q := db.S().Q()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, created_at) VALUES ('u-ann', 'Ann@example.com', 1)`,
		`INSERT INTO eval_arms (key, title, spec, fingerprint, updated_at) VALUES ('base', 'Base', '{"model":"x"}', 'fp-base-0001', 1)`,
		`INSERT INTO eval_tasks (key, repo, base, prompt, checks, hidden, forbidden, limits, extra, updated_at)
			VALUES ('t1', 'repo', 'main', 'do it', '[]', '{}', '[]', '{"wallS":600}', '{"flavor":"x"}', 1)`,
		`INSERT INTO eval_suites (key, title, task_keys, k, arms, updated_at) VALUES ('smoke-8', 'Smoke', '["t1"]', 1, '["base"]', 1)`,
		`INSERT INTO eval_schedules (id, suite_key, machine_id, owner_user_id, days, at_msk, every_weeks, enabled, updated_at)
			VALUES ('sch', 'smoke-8', 'm-ann', 'u-ann', '[1]', '10:00', 1, TRUE, 1)`,
	} {
		if _, err := q.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for win, used := range map[string]float64{"seven_day": week, "five_hour": five} {
		if err := db.S().PutProviderWindow(ctx, store.ProviderWindow{MachineID: "m-ann", Provider: "claude", Window: win, UsedPct: used, SampledAt: store.Ms(e.now)}); err != nil {
			t.Fatal(err)
		}
	}
	e.s = New(Deps{DB: db, Gate: e.gate, Link: e.link, Now: func() time.Time { return e.now }})
	return e
}

func (e *env) one(t *testing.T, query string, dest ...any) {
	t.Helper()
	if err := e.db.QueryRow(context.Background(), query).Scan(dest...); err != nil {
		t.Fatal(err)
	}
}

func TestPassRunsABatchToTheEnd(t *testing.T) {
	e := setup(t, 10, 10)
	ctx := context.Background()
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	if len(e.link.started) != 1 {
		t.Fatalf("one run handed over, got %d", len(e.link.started))
	}
	r := e.link.started[0]
	if len(r.RunID) != 36 || !strings.Contains(string(r.Task), `"prompt":"do it"`) || !strings.Contains(string(r.Task), `"flavor":"x"`) || !strings.Contains(string(r.Arm), `"model"`) {
		t.Fatalf("request: %+v %s", r, r.Task)
	}
	var state, bstate string
	e.one(t, `SELECT state FROM eval_runs`, &state)
	e.one(t, `SELECT state FROM eval_batches`, &bstate)
	if state != "claimed" || bstate != "running" {
		t.Fatalf("after the hand-over: run %s batch %s", state, bstate)
	}
	// the same slot is not queued twice
	e.now = e.now.Add(time.Minute)
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	e.one(t, `SELECT COUNT(*) FROM eval_batches`, &n)
	if n != 1 || len(e.link.started) != 1 {
		t.Fatalf("batches %d, started %d", n, len(e.link.started))
	}
	// the runner reports a pass: the run and the batch close, a marker is drawn
	yes, usd, wall, exit := true, 0.4, 120, 0
	e.link.states = map[string]RunState{r.RunID: {RunID: r.RunID, State: "done", Passed: &yes, WallS: &wall, Fingerprint: "fp-new-0002", FinishedAt: e.now.UTC().Format(time.RFC3339),
		Usage: &Usage{USD: &usd, ModelCalls: 3}, Checks: []Check{{Name: "hidden", Passed: true, ExitCode: &exit, Ms: 900}}}}
	e.now = e.now.Add(time.Minute)
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var passed bool
	var fp string
	var cost float64
	e.one(t, `SELECT state, passed, fingerprint, cost_usd FROM eval_runs`, &state, &passed, &fp, &cost)
	e.one(t, `SELECT state FROM eval_batches`, &bstate)
	if state != "done" || !passed || fp != "fp-new-0002" || cost != 0.4 || bstate != "done" {
		t.Fatalf("run %s %v %s %v batch %s", state, passed, fp, cost, bstate)
	}
	e.one(t, `SELECT COUNT(*) FROM eval_checks WHERE machine_id = 'm-ann' AND name = 'hidden' AND passed`, &n)
	if n != 1 {
		t.Fatal("the check is stored")
	}
	facts, err := e.db.S().EffFactsBetween(ctx, efficiency.SourceMarker, 0, store.Ms(e.now.Add(time.Hour)))
	if err != nil || len(facts) != 1 || !strings.Contains(facts[0].Data, "smoke-8: 1 из 1") {
		t.Fatalf("marker: %v %+v", err, facts)
	}
}

func TestSwitchOffQueuesNothing(t *testing.T) {
	e := setup(t, 10, 10)
	delete(e.gate, SwitchScheduler+"|ann@example.com")
	if err := e.s.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	var n int
	e.one(t, `SELECT COUNT(*) FROM eval_batches`, &n)
	if n != 0 || len(e.link.started) != 0 {
		t.Fatalf("batches %d, started %d", n, len(e.link.started))
	}
}

func TestSwitchOffAfterQueueingStopsStarts(t *testing.T) {
	e := setup(t, 10, 10)
	e.link.connected = false
	if err := e.s.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	delete(e.gate, SwitchScheduler+"|ann@example.com")
	e.link.connected = true
	e.now = e.now.Add(time.Minute)
	if err := e.s.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(e.link.started) != 0 {
		t.Fatal("a person who turned the switch off gets no new run")
	}
}

func TestWindowsAndLinkMakeARunWait(t *testing.T) {
	cases := []struct {
		name       string
		week, five float64
		connected  bool
		stale      bool
		note       string
	}{
		{"week", 75, 10, true, false, "недельное окно подписки занято на 75%"},
		{"five hours", 10, 65, true, false, "пятичасовое окно подписки занято на 65%"},
		{"no link", 10, 10, false, false, "машина не на связи"},
		{"old reading", 10, 10, true, true, "нет свежих данных"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := setup(t, c.week, c.five)
			e.link.connected = c.connected
			if c.stale {
				e.now = e.now.Add(time.Hour)
			}
			if err := e.s.Pass(context.Background()); err != nil {
				t.Fatal(err)
			}
			var note, state string
			e.one(t, `SELECT COALESCE(note, ''), state FROM eval_batches`, &note, &state)
			if len(e.link.started) != 0 || state != "queued" || !strings.Contains(note, c.note) {
				t.Fatalf("started %d, batch %s, note %q", len(e.link.started), state, note)
			}
		})
	}
}

func TestWeekCapSkipsTheBatch(t *testing.T) {
	e := setup(t, 10, 10)
	_, err := e.db.S().Q().Exec(context.Background(), `INSERT INTO eval_runs (id, batch_id, machine_id, task_key, arm_key, fingerprint, run_no, state, cost_usd, finished_at, created_at)
		VALUES ('old', 'b0', 'm-ann', 't1', 'base', 'f', 1, 'done', 300, ?, 1)`, store.Ms(e.now.Add(-24*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.s.Pass(context.Background()); err != nil {
		t.Fatal(err)
	}
	var state, note string
	e.one(t, `SELECT state, COALESCE(note, '') FROM eval_batches`, &state, &note)
	if state != "skipped" || !strings.Contains(note, "прогоны недели") || len(e.link.started) != 0 {
		t.Fatalf("batch %s %q, started %d", state, note, len(e.link.started))
	}
}

func TestDeferredSkipGoesBackToTheQueue(t *testing.T) {
	e := setup(t, 10, 10)
	ctx := context.Background()
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	id := e.link.started[0].RunID
	e.link.states = map[string]RunState{id: {RunID: id, State: "skipped", Error: "лимит подписки: окно занято"}}
	e.now = e.now.Add(time.Minute)
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	var attempts int
	var retry int64
	e.one(t, `SELECT state, attempts, retry_after FROM eval_runs`, &state, &attempts, &retry)
	if state != "queued" || attempts != 1 || retry != store.Ms(e.now.Add(deferFor)) {
		t.Fatalf("%s %d %d", state, attempts, retry)
	}
	if len(e.link.started) != 1 {
		t.Fatal("not handed over again before the retry time")
	}
}

func TestSilentRunnerFailsAClaimedRun(t *testing.T) {
	e := setup(t, 10, 10)
	ctx := context.Background()
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	e.now = e.now.Add(11 * time.Minute)
	if err := e.s.Pass(ctx); err != nil {
		t.Fatal(err)
	}
	var state, errText, bstate string
	e.one(t, `SELECT state, error FROM eval_runs`, &state, &errText)
	e.one(t, `SELECT state FROM eval_batches`, &bstate)
	if state != "failed" || !strings.Contains(errText, "10 минут") {
		t.Fatalf("%s %q", state, errText)
	}
	// the only run failed: nothing is done, the batch is partial and, being a smoke batch, has a hole in it
	if bstate != "partial" {
		t.Fatalf("batch %s", bstate)
	}
	var alerts int
	e.one(t, `SELECT COUNT(*) FROM eff_regression_alerts WHERE addressee = 'ann@example.com' AND metric LIKE 'smoke: результат есть у 0 из 1%'`, &alerts)
	if alerts != 1 {
		t.Fatalf("coverage alerts %d", alerts)
	}
}
