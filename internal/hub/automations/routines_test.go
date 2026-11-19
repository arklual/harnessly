package automations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/store/storetest"
)

type rang struct{ key, title, icon string }

func routineSvc(t *testing.T) (*Service, *clock, *[]rang) {
	t.Helper()
	db := storetest.Open(t)
	c := &clock{at: time.Date(2026, 11, 18, 6, 0, 0, 0, time.UTC)} // 09:00 Moscow, Wednesday
	bells := &[]rang{}
	s := New(Deps{Config: &config.Config{Automations: true, LaunchToken: strings.Repeat("k", 40), PublicURL: "https://hub.example.com"}, DB: db,
		Now: func() time.Time { return c.at },
		Bell: func(_ context.Context, _, _, icon, key, title, _, _ string) bool {
			*bells = append(*bells, rang{key, title, icon})
			return true
		}})
	if _, err := db.Exec(context.Background(), `INSERT INTO users (id, email, name, created_at) VALUES ('u1', 'me@example.com', 'Me', 0)`); err != nil {
		t.Fatalf("user: %v", err)
	}
	return s, c, bells
}

func addRoutine(t *testing.T, s *Service, name string, rt Routine) Rule {
	t.Helper()
	trig := Trigger{Schedule: &Schedule{Cron: "0 9 * * 1-5"}}
	r := Rule{OwnerID: "u1", Name: name, PromptTemplate: "Собери дайджест дня", Enabled: true}
	if err := s.apply(context.Background(), &r, ruleBody{Trigger: &trig, Routine: &rt}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	r, ok, err := s.addRule(context.Background(), r)
	if err != nil || !ok {
		t.Fatalf("addRule: %v %v", ok, err)
	}
	return r
}

// started queues a run of the rule and takes it as the dispatcher would; it returns the run and its token.
func started(t *testing.T, s *Service, r Rule, fp string) (Run, string) {
	t.Helper()
	ctx := context.Background()
	out, err := s.queueRun(ctx, r, fp, json.RawMessage(`{}`))
	if err != nil || !out.Queued {
		t.Fatalf("queue: %v %+v", err, out)
	}
	run, _, token, _, err := s.claim(ctx, anyMachine)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.markDispatched(ctx, run.ID, "c1", "m1"); err != nil {
		t.Fatal(err)
	}
	return run, token
}

func report(t *testing.T, s *Service, run Run, token string, rep Report) Run {
	t.Helper()
	rule, err := s.ruleOfRun(context.Background(), run.ID)
	if err != nil {
		t.Fatal(err)
	}
	rep.RunID, rep.ResultToken = run.ID, token
	got, err := s.recordRoutine(context.Background(), rule, rep)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	return got
}

func TestRoutineDefaultsLeaveNoQuietHours(t *testing.T) {
	s, _, _ := routineSvc(t)
	r := addRoutine(t, s, "digest", Routine{})
	if quietNow(r, time.Date(2026, 11, 18, 4, 0, 0, 0, time.UTC)) {
		t.Fatal("a routine at 07:00 Moscow is held by the quiet hours")
	}
	if r.Limits.maxRuns() < 24 || r.Routine.Delivery.When != DeliverChanged || r.Routine.Autonomy != "read" {
		t.Fatalf("defaults: %+v %+v", r.Limits, r.Routine)
	}
	if s.routineModel(r) != "sonnet" {
		t.Fatalf("model: %s", s.routineModel(r))
	}
}

func TestRoutineValidation(t *testing.T) {
	s, _, _ := routineSvc(t)
	for name, rt := range map[string]Routine{
		"bad when":       {Delivery: Delivery{When: "sometimes"}},
		"bad channel":    {Delivery: Delivery{Channels: []string{"pigeon"}}},
		"file no path":   {Delivery: Delivery{Channels: []string{"file"}}},
		"bad check":      {Checks: []Check{{Kind: "vibes"}}},
		"bad regex":      {Checks: []Check{{Kind: "regex", Value: "("}}},
		"empty contains": {Checks: []Check{{Kind: "contains"}}},
		"bad autonomy":   {Autonomy: "god"},
		"bad until":      {Until: "tomorrow"},
	} {
		r := Rule{OwnerID: "u1", Name: "x", PromptTemplate: "p"}
		if err := s.apply(context.Background(), &r, ruleBody{Trigger: &Trigger{Schedule: &Schedule{Cron: "0 9 * * *"}}, Routine: &rt}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestRoutineChecksDecideTheOutcome(t *testing.T) {
	s, _, bells := routineSvc(t)
	r := addRoutine(t, s, "digest", Routine{
		Delivery: Delivery{Channels: []string{"notice", "file"}, File: "digest/{date}.md"},
		Checks:   []Check{{Kind: "contains", Value: "Риски"}, {Kind: "file"}, {Kind: "fresh"}},
	})
	run, token := started(t, s, r, "schedule:1")
	good := report(t, s, run, token, Report{Outcome: "ok", Summary: "Дайджест за 2026-11-18", Body: "## Итог\n## Риски\nнет",
		Artifacts: []Artifact{{Kind: "file", Title: "digest/2026-11-18.md", Meta: "версия 2"}, {Kind: "x", Title: ""}}})
	if good.Status != StatusDone || good.Outcome != OutcomeOK {
		t.Fatalf("good run: %+v", good)
	}
	var res RoutineResult
	_ = json.Unmarshal(good.Result, &res)
	if len(res.Checks) != 3 || len(res.Artifacts) != 1 || res.Model != "sonnet" {
		t.Fatalf("result: %+v", res)
	}
	if len(*bells) != 1 || (*bells)[0].key != "routine:"+run.ID {
		t.Fatalf("bells: %+v", *bells)
	}
	run2, token2 := started(t, s, r, "schedule:2")
	bad := report(t, s, run2, token2, Report{Outcome: "ok", Summary: "Дайджест", Body: "без нужного раздела"})
	if bad.Outcome != OutcomeFailed || !strings.Contains(bad.Error, "Проверка не прошла") {
		t.Fatalf("checks did not fail the run: %+v", bad)
	}
	// A wrong token is refused and the run stays running.
	run3, _ := started(t, s, r, "schedule:3")
	rule, _ := s.ruleOfRun(context.Background(), run3.ID)
	if _, err := s.recordRoutine(context.Background(), rule, Report{RunID: run3.ID, ResultToken: "nope", Outcome: "ok"}); err == nil {
		t.Fatal("wrong token taken")
	}
}

func TestQuietRunIsNotHeardAndKeepsTheMemory(t *testing.T) {
	s, _, bells := routineSvc(t)
	r := addRoutine(t, s, "sales", Routine{Delivery: Delivery{Channels: []string{"notice"}}, Checks: []Check{{Kind: "contains", Value: "Выручка"}}})
	run, token := started(t, s, r, "schedule:1")
	mem := "Прошлая неделя: 4,82 млн"
	got := report(t, s, run, token, Report{Outcome: "quiet", Summary: "Нового нет", State: &mem})
	if got.Outcome != OutcomeQuiet {
		t.Fatalf("quiet run failed its checks: %+v", got)
	}
	if len(*bells) != 0 {
		t.Fatalf("a quiet run rang: %+v", *bells)
	}
	r, _ = s.ruleByID(context.Background(), r.ID)
	if r.State != mem {
		t.Fatalf("memory: %q", r.State)
	}
	run2, _ := s.runByID(context.Background(), run.ID)
	_ = run2
	p := s.buildRoutinePrompt(r, Run{ID: "x", EventPayload: json.RawMessage(`{}`), Fingerprint: "manual:1"})
	for _, want := range []string{mem, "вручную", "x-result-token", "Собери дайджест дня", "outcome"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt has no %q", want)
		}
	}
}

func TestThreeFailuresPauseOnceAndResumeClears(t *testing.T) {
	s, _, bells := routineSvc(t)
	r := addRoutine(t, s, "limits", Routine{Delivery: Delivery{Channels: []string{"notice"}, When: DeliverNever}})
	for i := 0; i < 3; i++ {
		run, token := started(t, s, r, "schedule:"+string(rune('a'+i)))
		report(t, s, run, token, Report{Outcome: "failed", Summary: "Не вышло", Error: "нет Codex"})
	}
	r, _ = s.ruleByID(context.Background(), r.ID)
	if r.Enabled || r.FailStreak != 3 || !strings.Contains(r.PausedReason, "3 сбоев") {
		t.Fatalf("not paused: %+v", r)
	}
	if len(*bells) != 1 || !strings.HasPrefix((*bells)[0].key, "routine-paused:") {
		t.Fatalf("pause notice: %+v", *bells)
	}
	on := true
	if err := s.apply(context.Background(), &r, ruleBody{Enabled: &on}); err != nil {
		t.Fatal(err)
	}
	if r.FailStreak != 0 || r.PausedReason != "" {
		t.Fatalf("resume kept the streak: %+v", r)
	}
}

func TestSweepEndsSilentRunsAndCountsThem(t *testing.T) {
	s, c, _ := routineSvc(t)
	r := addRoutine(t, s, "silent", Routine{})
	run, _ := started(t, s, r, "schedule:1")
	c.at = c.at.Add(3 * time.Hour)
	s.sweepRoutines(context.Background())
	got, _ := s.runByID(context.Background(), run.ID)
	if got.Status != StatusFailed || got.Outcome != OutcomeFailed || !strings.Contains(got.Error, "не прислал итог") {
		t.Fatalf("silent run: %+v", got)
	}
	r, _ = s.ruleByID(context.Background(), r.ID)
	if r.FailStreak != 1 {
		t.Fatalf("streak: %d", r.FailStreak)
	}
}

func TestOnceRoutineTurnsOffAfterItsRun(t *testing.T) {
	s, _, _ := routineSvc(t)
	r := addRoutine(t, s, "flag", Routine{Once: true})
	run, token := started(t, s, r, "schedule:1")
	report(t, s, run, token, Report{Outcome: "ok", Summary: "Флаг убран"})
	r, _ = s.ruleByID(context.Background(), r.ID)
	if r.Enabled {
		t.Fatal("a one-time routine stayed on")
	}
}

func TestNextSlots(t *testing.T) {
	s, _, _ := routineSvc(t)
	r := addRoutine(t, s, "weekly", Routine{Until: "2026-11-30"})
	r.Trigger.Schedule.Cron = "0 10 * * 1"
	got := nextSlots(r, s.now(), 3)
	if len(got) != 2 || got[0] != "2026-11-23T07:00:00Z" || got[1] != "2026-11-30T07:00:00Z" {
		t.Fatalf("next: %v", got)
	}
}

func TestFeedForTheDay(t *testing.T) {
	s, _, _ := routineSvc(t)
	r := addRoutine(t, s, "digest", Routine{Words: "по будням в 9:00"})
	run, token := started(t, s, r, "schedule:1")
	report(t, s, run, token, Report{Outcome: "ok", Summary: "Три дела ждут", Artifacts: []Artifact{{Kind: "doc", Title: "Дайджест", URL: "https://docs.example.com/a/x"}}})
	// A trigger's run is in the day too (what the hub did for the person and what waits for their ok).
	watch, ok, err := s.addRule(context.Background(), Rule{OwnerID: "u1", Name: "alerter quiet", Trigger: Trigger{Source: SourceWait,
		Wait: &Wait{Start: Match{Source: "heartbeat"}, After: "15m"}}, PromptTemplate: "x", Model: "sonnet", Enabled: true})
	if err != nil || !ok {
		t.Fatalf("addRule: %v %v", ok, err)
	}
	if q, err := s.queueRun(context.Background(), watch, "wait:1", json.RawMessage(`{}`)); err != nil || !q.Queued {
		t.Fatalf("queue: %+v %v", q, err)
	}
	mux := http.NewServeMux()
	s.Mount(mux)
	ask := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/platform/automations/routines/feed?person=me@example.com", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	if w := ask("wrong"); w.Code != http.StatusForbidden {
		t.Fatalf("wrong token: %d", w.Code)
	}
	w := ask(strings.Repeat("k", 40))
	if w.Code != http.StatusOK {
		t.Fatalf("feed: %d %s", w.Code, w.Body)
	}
	var out struct {
		Routines []FeedRoutine `json:"routines"`
		Runs     []FeedRun     `json:"runs"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Routines) != 1 || out.Routines[0].Words != "по будням в 9:00" || out.Routines[0].Next == "" {
		t.Fatalf("routines: %+v", out.Routines)
	}
	kinds := map[string]FeedRun{}
	for _, r := range out.Runs {
		kinds[r.Kind] = r
	}
	rt, tr := kinds[FeedRoutineRun], kinds[FeedTriggerRun]
	if len(out.Runs) != 2 || rt.Name != "digest" || rt.Summary != "Три дела ждут" || len(rt.Artifacts) != 1 || !strings.Contains(rt.URL, "/routines/") {
		t.Fatalf("runs: %+v", out.Runs)
	}
	if tr.Name != "alerter quiet" || tr.Source != SourceWait || tr.Status != StatusQueued || tr.URL != "" {
		t.Fatalf("trigger run: %+v", tr)
	}
}
