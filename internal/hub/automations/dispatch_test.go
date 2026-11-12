package automations

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// fakeLinks is the machines of a test: which are connected and what a start answers.
type fakeLinks struct {
	online  map[string]bool
	started []string
	params  []any
	fail    error
}

func (f *fakeLinks) Get(id string) *link.Link {
	if f.online[id] {
		return &link.Link{MachineID: id}
	}
	return nil
}

func (f *fakeLinks) Do(_ context.Context, machineID, _ string, params any, _ time.Duration) (json.RawMessage, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	f.started = append(f.started, machineID)
	f.params = append(f.params, params)
	return json.RawMessage(`{"conversationId":"c-1"}`), nil
}

type movingClock struct{ at time.Time }

func dispatcher(t *testing.T) (*Service, *fakeLinks, *movingClock) {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	links := &fakeLinks{online: map[string]bool{}}
	clock := &movingClock{at: noon()}
	s := New(Deps{Config: &config.Config{Automations: true, AutomationsSecret: "s3cret", AutomationsTokenEnv: "HARNESS_AUTOMATION_RESULT_TOKEN"}, DB: db, Links: links, Now: func() time.Time { return clock.at }})
	return s, links, clock
}

func queued(t *testing.T, s *Service, name, machine, fingerprint string) string {
	t.Helper()
	r, ok, err := s.addRule(context.Background(), Rule{OwnerID: "u1", Name: name, MachineID: machine, Trigger: Trigger{Source: "alert"}, PromptTemplate: "fix it", Model: "sonnet", Enabled: true})
	if err != nil || !ok {
		t.Fatalf("addRule: %v %v", ok, err)
	}
	out, err := s.queueRun(context.Background(), r, fingerprint, json.RawMessage(`{}`))
	if err != nil || out.RunID == "" {
		t.Fatalf("queueRun: %+v %v", out, err)
	}
	return out.RunID
}

func statusOf(t *testing.T, s *Service, id string) Run {
	t.Helper()
	r, err := s.runByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// A run whose machine is not connected stays in the queue, is not failed, and starts once the machine is back.
func TestRunWaitsInTheQueueWhileItsMachineIsOff(t *testing.T) {
	s, links, _ := dispatcher(t)
	ctx := context.Background()
	id := queued(t, s, "a", "m-off", "f1")
	for range 3 {
		if s.Dispatch(ctx) {
			t.Fatal("a run was started on a machine that is not connected")
		}
	}
	if r := statusOf(t, s, id); r.Status != StatusQueued || r.Error != "" {
		t.Fatalf("the run of a machine that is off: %+v", r)
	}
	links.online["m-off"] = true
	if !s.Dispatch(ctx) {
		t.Fatal("the run did not start once its machine was back")
	}
	if r := statusOf(t, s, id); r.Status != StatusRunning || r.ConversationID != "c-1" || r.MachineID != "m-off" {
		t.Fatalf("the started run: %+v", r)
	}
}

// A run that waits for its machine does not hold up the run of a machine that is connected.
func TestWaitingRunDoesNotHoldUpAnotherMachines(t *testing.T) {
	s, links, clock := dispatcher(t)
	ctx := context.Background()
	waiting := queued(t, s, "off", "m-off", "f1")
	clock.at = clock.at.Add(time.Second)
	ready := queued(t, s, "on", "m-on", "f2")
	links.online["m-on"] = true
	if !s.Dispatch(ctx) {
		t.Fatal("nothing was started")
	}
	if statusOf(t, s, ready).Status != StatusRunning || statusOf(t, s, waiting).Status != StatusQueued {
		t.Errorf("ready %s, waiting %s", statusOf(t, s, ready).Status, statusOf(t, s, waiting).Status)
	}
}

// A run taken by a hub that stopped before the conversation was started goes back to the queue when its lease runs
// out, and is given up after maxAttempts takes. A run whose conversation was started is never taken back.
func TestAbandonedStartReturnsToTheQueue(t *testing.T) {
	s, links, clock := dispatcher(t)
	ctx := context.Background()
	id := queued(t, s, "a", "m1", "f1")
	links.online["m1"] = true
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// taken and never finished: the hub stopped between the take and the start
		if run, _, _, _, err := s.claim(ctx, s.machineFor); err != nil || run.ID != id {
			t.Fatalf("take %d: %v", attempt, err)
		}
		s.reap(ctx)
		if st := statusOf(t, s, id).Status; st != StatusRunning {
			t.Fatalf("take %d: reaped before the lease ran out: %s", attempt, st)
		}
		clock.at = clock.at.Add(startLease + time.Second)
		s.reap(ctx)
		want := StatusQueued
		if attempt == maxAttempts {
			want = StatusFailed
		}
		if r := statusOf(t, s, id); r.Status != want {
			t.Fatalf("after take %d: %s, want %s", attempt, r.Status, want)
		}
	}

	started := queued(t, s, "b", "m1", "f2")
	if !s.Dispatch(ctx) {
		t.Fatal("the second run did not start")
	}
	clock.at = clock.at.Add(10 * startLease)
	s.reap(ctx)
	if r := statusOf(t, s, started); r.Status != StatusRunning {
		t.Errorf("a run whose conversation runs was taken back: %s", r.Status)
	}
}

// A start that may have reached the machine (it did not answer in time) is not tried again: a second start would do
// the work twice.
func TestStartWithoutAnAnswerIsNotTriedAgain(t *testing.T) {
	s, links, _ := dispatcher(t)
	ctx := context.Background()
	id := queued(t, s, "a", "m1", "f1")
	links.online["m1"] = true
	links.fail = errors.New("no answer in time")
	s.Dispatch(ctx)
	if r := statusOf(t, s, id); r.Status != StatusFailed || r.Error != "no answer in time" {
		t.Fatalf("after a start without an answer: %+v", r)
	}
}

// A start the machine was never asked for (it went away) puts the run back for another try, with the reason kept.
func TestFailedStartIsTriedAgain(t *testing.T) {
	s, links, _ := dispatcher(t)
	ctx := context.Background()
	id := queued(t, s, "a", "m1", "f1")
	links.online["m1"] = true
	links.fail = httpx.ErrMachineOffline
	for attempt := 1; attempt < maxAttempts; attempt++ {
		if s.Dispatch(ctx) {
			t.Fatal("a failed start was reported as started")
		}
		if r := statusOf(t, s, id); r.Status != StatusQueued || r.Error != httpx.ErrMachineOffline.Error() {
			t.Fatalf("after a failed start %d: %+v", attempt, r)
		}
	}
	s.Dispatch(ctx)
	if r := statusOf(t, s, id); r.Status != StatusFailed {
		t.Fatalf("after %d failed starts: %s", maxAttempts, r.Status)
	}
}

// A run nobody could start for a day is given up, so the queue does not grow without end.
func TestRunWithoutAMachineForADayIsGivenUp(t *testing.T) {
	s, _, clock := dispatcher(t)
	ctx := context.Background()
	id := queued(t, s, "a", "m-gone", "f1")
	clock.at = clock.at.Add(queuedFor + time.Minute)
	s.Dispatch(ctx)
	if r := statusOf(t, s, id); r.Status != StatusFailed || r.Error == "" {
		t.Fatalf("a run that waited a day: %+v", r)
	}
}

// The report token reaches the conversation under the name the runtime lets a started conversation set (its prefix), and
// the prompt names the same variable.
func TestStartNamesTheTokenVariableFromTheConfig(t *testing.T) {
	s, links, _ := dispatcher(t)
	s.cfg.AutomationsTokenEnv = "TEST_AUTOMATION_RESULT_TOKEN"
	links.online["m1"] = true
	queued(t, s, "rule", "m1", "fp-env")
	if !s.Dispatch(context.Background()) {
		t.Fatal("run not dispatched")
	}
	p, ok := links.params[0].(map[string]any)
	if !ok {
		t.Fatalf("params %T", links.params[0])
	}
	env, _ := p["env"].(map[string]string)
	if env["TEST_AUTOMATION_RESULT_TOKEN"] == "" || len(env) != 1 {
		t.Fatalf("env %v", env)
	}
	if prompt, _ := p["prompt"].(string); !strings.Contains(prompt, "$TEST_AUTOMATION_RESULT_TOKEN") {
		t.Fatalf("prompt does not name the variable: %s", prompt)
	}
}
