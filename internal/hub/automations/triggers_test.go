package automations

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

func TestParseCron(t *testing.T) {
	msk, _ := time.LoadLocation("Europe/Moscow")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, msk)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, c := range []struct {
		expr string
		at   string
		want bool
	}{
		{"0 9 * * 1-5", "2026-11-13 09:00", true},  // Friday
		{"0 9 * * 1-5", "2026-11-14 09:00", false}, // Saturday
		{"0 9 * * 1-5", "2026-11-13 09:01", false},
		{"*/15 * * * *", "2026-11-13 10:45", true},
		{"*/15 * * * *", "2026-11-13 10:46", false},
		{"0 0 * * 7", "2026-11-15 00:00", true}, // 7 is Sunday
		{"0 0 * * 0", "2026-11-15 00:00", true},
		{"0 12 1 * 5", "2026-11-13 12:00", true}, // day of month or day of week
		{"0 12 1 * 5", "2026-11-01 12:00", true},
		{"0 12 1 * 5", "2026-11-02 12:00", false},
		{"30 8-18/2 * * *", "2026-11-13 10:30", true},
		{"30 8-18/2 * * *", "2026-11-13 11:30", false},
		{"0,30 9 * * *", "2026-11-13 09:30", true},
		{"@daily", "2026-11-13 00:00", true},
		{"@hourly", "2026-11-13 07:00", true},
	} {
		spec, err := parseCron(c.expr)
		if err != nil {
			t.Fatalf("%s: %v", c.expr, err)
		}
		if got := spec.matches(at(c.at)); got != c.want {
			t.Errorf("%s at %s: %v, want %v", c.expr, c.at, got, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "*/0 * * * *", "a * * * *", "5-1 * * * *"} {
		if _, err := parseCron(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

func TestLastSlotCatchesUpOnlyALittle(t *testing.T) {
	spec, _ := parseCron("0 9 * * *")
	msk, _ := time.LoadLocation("Europe/Moscow")
	nine := time.Date(2026, 11, 13, 9, 0, 0, 0, msk)
	if s, ok := spec.lastSlot(nine.Add(5*time.Minute), time.Time{}, catchUp, msk); !ok || !s.Equal(nine) {
		t.Fatalf("five past nine: %v %v", s, ok)
	}
	if _, ok := spec.lastSlot(nine.Add(3*time.Hour), time.Time{}, catchUp, msk); ok {
		t.Fatal("a slot three hours back was caught up")
	}
	if _, ok := spec.lastSlot(nine.Add(5*time.Minute), nine.Add(time.Minute), catchUp, msk); ok {
		t.Fatal("a slot before the rule was saved fired")
	}
}

type clock struct{ at time.Time }

func clocked(t *testing.T, gate Gate) (*Service, *clock) {
	t.Helper()
	db := storetest.Open(t)
	c := &clock{at: noon()}
	s := New(Deps{Config: &config.Config{Automations: true, AutomationsSecret: "s3cret"}, DB: db, Gate: gate, Now: func() time.Time { return c.at }})
	return s, c
}

func addTriggerRule(t *testing.T, s *Service, name string, trig Trigger, limits Limits) Rule {
	t.Helper()
	r := Rule{OwnerID: "u1", Name: name, PromptTemplate: "look", Enabled: true}
	if err := s.apply(context.Background(), &r, ruleBody{Trigger: &trig, Limits: &limits}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	r, ok, err := s.addRule(context.Background(), r)
	if err != nil || !ok {
		t.Fatalf("addRule: %v %v", ok, err)
	}
	return r
}

func runsOf(t *testing.T, s *Service, ruleID string) []Run {
	t.Helper()
	runs, err := s.runs(context.Background(), "u1", ruleID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return runs
}

func TestScheduleQueuesEachSlotOnce(t *testing.T) {
	s, c := clocked(t, nil)
	c.at = time.Date(2026, 11, 13, 5, 58, 0, 0, time.UTC) // 08:58 Moscow
	many := 100
	r := addTriggerRule(t, s, "digest", Trigger{Schedule: &Schedule{Cron: "0 9 * * *"}}, Limits{MaxRunsPerDay: &many})
	if r.Trigger.Source != SourceSchedule {
		t.Fatalf("source: %q", r.Trigger.Source)
	}
	ctx := context.Background()
	s.Fire(ctx)
	if n := len(runsOf(t, s, r.ID)); n != 0 {
		t.Fatalf("before the slot: %d runs", n)
	}
	for _, at := range []time.Duration{2 * time.Minute, 2*time.Minute + 15*time.Second, 10 * time.Minute} {
		c.at = time.Date(2026, 11, 13, 5, 58, 0, 0, time.UTC).Add(at)
		s.Fire(ctx)
	}
	runs := runsOf(t, s, r.ID)
	if len(runs) != 1 || runs[0].Fingerprint != "schedule:2026-11-13T06:00:00Z" || !strings.Contains(string(runs[0].EventPayload), `"slot":"2026-11-13T09:00:00+03:00"`) {
		t.Fatalf("runs: %+v", runs)
	}
	// The next day the hub was down at nine and came back at noon: that slot is not caught up.
	c.at = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	s.Fire(ctx)
	if n := len(runsOf(t, s, r.ID)); n != 1 {
		t.Fatalf("caught up three hours back: %d runs", n)
	}
}

func TestTriggerValidation(t *testing.T) {
	s, _ := clocked(t, nil)
	for name, trig := range map[string]Trigger{
		"bad cron":       {Schedule: &Schedule{Cron: "every morning"}},
		"bad zone":       {Schedule: &Schedule{Cron: "0 9 * * *", TZ: "Mars/Olympus"}},
		"both":           {Schedule: &Schedule{Cron: "0 9 * * *"}, Wait: &Wait{Start: Match{Source: "a"}, After: "20m"}},
		"short wait":     {Wait: &Wait{Start: Match{Source: "a"}, After: "10s"}},
		"no start":       {Wait: &Wait{After: "20m"}},
		"reserved start": {Wait: &Wait{Start: Match{Source: "wait"}, After: "20m"}},
		"reserved plain": {Source: "schedule"},
		"other source":   {Source: "alert", Schedule: &Schedule{Cron: "0 9 * * *"}},
	} {
		r := Rule{OwnerID: "u1", Name: "x", PromptTemplate: "p"}
		if err := s.apply(context.Background(), &r, ruleBody{Trigger: &trig}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type offGate struct{}

func (offGate) On(context.Context, string, string, string) bool { return false }

func TestTriggersStayBehindTheSwitch(t *testing.T) {
	s, _ := clocked(t, offGate{})
	r := Rule{OwnerID: "u1", Name: "x", PromptTemplate: "p"}
	err := s.apply(context.Background(), &r, ruleBody{Trigger: &Trigger{Schedule: &Schedule{Cron: "* * * * *"}}})
	if err == nil || !strings.Contains(err.Error(), "triggers_off") && !strings.Contains(err.Error(), "не всем") {
		t.Fatalf("switch off, schedule saved: %v", err)
	}
	plain := Rule{OwnerID: "u1", Name: "y", PromptTemplate: "p"}
	if err := s.apply(context.Background(), &plain, ruleBody{Trigger: &Trigger{Source: "alert"}}); err != nil {
		t.Fatalf("plain rule held by the switch: %v", err)
	}
	// A schedule saved before the switch went off does not fire.
	sched := Rule{OwnerID: "u1", Name: "z", PromptTemplate: "p", Trigger: Trigger{Source: SourceSchedule, Schedule: &Schedule{Cron: "* * * * *"}}, Enabled: true}
	sched, _, _ = s.addRule(context.Background(), sched)
	s.now = func() time.Time { return noon().Add(time.Minute) }
	s.Fire(context.Background())
	if n := len(runsOf(t, s, sched.ID)); n != 0 {
		t.Fatalf("fired behind the switch: %d", n)
	}
}

func hookEvent(t *testing.T, s *Service, source, fp, event string) map[string]any {
	t.Helper()
	w := post(s, "s3cret", `{"source":"`+source+`","fingerprint":"`+fp+`","event":`+event+`}`)
	if w.Code != http.StatusAccepted {
		t.Fatalf("hook %s: %d %s", source, w.Code, w.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func timers(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(context.Background(), `SELECT COUNT(*) FROM automation_timers`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestWaitFiresOnlyWithoutAnAnswer(t *testing.T) {
	s, c := clocked(t, nil)
	many := 100
	r := addTriggerRule(t, s, "reply", Trigger{Wait: &Wait{
		Start: Match{Source: "telegram", Filter: map[string]any{"direction": "in"}},
		Stop:  &Match{Source: "telegram", Filter: map[string]any{"direction": "out"}},
		Key:   "chat", After: "20m",
	}}, Limits{MaxRunsPerDay: &many})
	ctx := context.Background()
	t0 := c.at

	// Chat 1: a message, a second one (does not move the timer), no answer: fires once, 20 minutes after the first.
	out := hookEvent(t, s, "telegram", "m1", `{"chat":"1","direction":"in","text":"привет"}`)
	if !strings.Contains(mustJSON(out["waits"]), `"armed":true`) {
		t.Fatalf("not armed: %v", out)
	}
	c.at = t0.Add(5 * time.Minute)
	hookEvent(t, s, "telegram", "m2", `{"chat":"1","direction":"in","text":"ты тут?"}`)
	// Chat 2: a message and an answer within the time: no run.
	hookEvent(t, s, "telegram", "m3", `{"chat":"2","direction":"in"}`)
	c.at = t0.Add(10 * time.Minute)
	if out := hookEvent(t, s, "telegram", "m4", `{"chat":"2","direction":"out"}`); !strings.Contains(mustJSON(out["waits"]), `"cleared":true`) {
		t.Fatalf("not cleared: %v", out)
	}
	// An event without the key field touches nothing.
	hookEvent(t, s, "telegram", "m5", `{"direction":"in"}`)
	if n := timers(t, s); n != 1 {
		t.Fatalf("timers: %d", n)
	}

	c.at = t0.Add(19 * time.Minute)
	s.Fire(ctx)
	if n := len(runsOf(t, s, r.ID)); n != 0 {
		t.Fatalf("fired early: %d", n)
	}
	c.at = t0.Add(20 * time.Minute)
	s.Fire(ctx)
	s.Fire(ctx)
	runs := runsOf(t, s, r.ID)
	if len(runs) != 1 || !strings.Contains(string(runs[0].EventPayload), `"text":"привет"`) || !strings.Contains(string(runs[0].EventPayload), `"key":"1"`) {
		t.Fatalf("runs: %+v", runs)
	}
	if n := timers(t, s); n != 0 {
		t.Fatalf("timer left after firing: %d", n)
	}
	// A new message after the run arms a new wait, which fires as a run of its own.
	hookEvent(t, s, "telegram", "m6", `{"chat":"1","direction":"in"}`)
	c.at = t0.Add(41 * time.Minute)
	s.Fire(ctx)
	if n := len(runsOf(t, s, r.ID)); n != 2 {
		t.Fatalf("second wait: %d runs", n)
	}
}

func TestWatchdogIsReArmedByEachEvent(t *testing.T) {
	s, c := clocked(t, nil)
	hb := Match{Source: "heartbeat", Filter: map[string]any{"sender": "entry-keeper"}}
	r := addTriggerRule(t, s, "keeper silent", Trigger{Wait: &Wait{Start: hb, Stop: &hb, After: "30m"}}, Limits{})
	t0 := c.at
	for i := 0; i < 4; i++ {
		c.at = t0.Add(time.Duration(i) * 20 * time.Minute)
		hookEvent(t, s, "heartbeat", "h"+string(rune('0'+i)), `{"sender":"entry-keeper"}`)
		s.Fire(context.Background())
	}
	if n := len(runsOf(t, s, r.ID)); n != 0 {
		t.Fatalf("fired while the sender was alive: %d", n)
	}
	c.at = t0.Add(60*time.Minute + 30*time.Minute)
	s.Fire(context.Background())
	if n := len(runsOf(t, s, r.ID)); n != 1 {
		t.Fatalf("silent sender not caught: %d", n)
	}
}

func TestReservedSourcesAndDeletedRules(t *testing.T) {
	s, c := clocked(t, nil)
	if w := post(s, "s3cret", `{"source":"schedule","fingerprint":"x","event":{}}`); w.Code != http.StatusBadRequest {
		t.Fatalf("outside event as a schedule: %d", w.Code)
	}
	r := addTriggerRule(t, s, "w", Trigger{Wait: &Wait{Start: Match{Source: "a"}, After: "1m"}}, Limits{})
	hookEvent(t, s, "a", "1", `{}`)
	if ok, err := s.deleteRule(context.Background(), r.ID, "u1"); !ok || err != nil {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if n := timers(t, s); n != 0 {
		t.Fatalf("timer outlived its rule: %d", n)
	}
	// A timer of a switched-off rule is dropped without a run.
	r2 := addTriggerRule(t, s, "w2", Trigger{Wait: &Wait{Start: Match{Source: "a"}, After: "1m"}}, Limits{})
	hookEvent(t, s, "a", "2", `{}`)
	r2.Enabled = false
	if err := s.saveRule(context.Background(), r2); err != nil {
		t.Fatal(err)
	}
	c.at = c.at.Add(2 * time.Minute)
	s.Fire(context.Background())
	if n := timers(t, s); n != 0 || len(runsOf(t, s, r2.ID)) != 0 {
		t.Fatalf("switched-off rule: timers %d runs %d", n, len(runsOf(t, s, r2.ID)))
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
