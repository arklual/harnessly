package llmimport

import (
	"context"
	"testing"
)

// the evaluation of the harness moves into the hub once: the catalog, the schedules and the finished batches
// with their runs and checks; a batch still live in llm is left, and so is one of an owner or a machine the hub lacks;
// what the hub has is kept; a dry run writes nothing.

type fakeEval struct{ data EvalData }

func (f fakeEval) Eval(context.Context) (EvalData, error) { return f.data, nil }

func TestEvalMovesOnce(t *testing.T) {
	ctx := context.Background()
	db := openHub(t)
	q := db.S().Q()
	for _, s := range []string{
		`INSERT INTO users (id, email, created_at) VALUES ('u-ann', 'ann@example.com', 1)`,
		`INSERT INTO machines (id, name, hostname, os, arch, unix_user, profile, via, key_pub, enc_pub, agent_version, created_at, info)
			VALUES ('m-ann', 'ann-vm', 'h', '', '', '', '', '', '', '', '', 1, '')`,
		`INSERT INTO history_conversations (machine_id, conv_id, owner_id, rev, seq, history_done, updated_at) VALUES ('m-ann', 'c1', 'u-ann', 1, 1, 0, 1)`,
		`INSERT INTO eval_arms (key, title, spec, fingerprint, updated_at) VALUES ('base', 'kept', '{}', 'f0', 1)`,
	} {
		if _, err := q.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	yes, no := true, false
	cost, diff, n10 := 0.5, `{"a.go":"+x"}`, 10
	slot := int64(1000)
	sched := "sched-1"
	d := EvalData{
		Arms:   []EvalArm{{Key: "base", Title: "llm title", Spec: `{"x":1}`, Fingerprint: "f1"}, {Key: "alt", Title: "Alt", Spec: `{}`, Fingerprint: "f2"}},
		Tasks:  []EvalTask{{Key: "t1", Repo: "r", Base: "b", Prompt: "p", Checks: `[]`, Hidden: `{}`, Forbidden: `["x"]`, Limits: `{}`, Extra: `{}`, HeldOut: true}},
		Suites: []EvalSuite{{Key: "smoke-8", Title: "Smoke", TaskKeys: `["t1"]`, K: 1, Arms: `["base","alt"]`}},
		Schedules: []EvalSchedule{
			{ID: "sched-1", Suite: "smoke-8", Runtime: "dev:ann@example.com", Owner: "ann@example.com", Days: `[1,3]`, AtMSK: "10:00", Every: 1, Enabled: true},
			{ID: "sched-2", Suite: "smoke-8", Runtime: "gone:ann@example.com", Owner: "ann@example.com", Days: `[1]`, AtMSK: "10:00", Every: 1},
		},
		Batches: []EvalBatch{
			{ID: "b-done", Schedule: &sched, Suite: "smoke-8", Runtime: "dev:ann@example.com", Owner: "ann@example.com", Reason: "schedule", State: "done", Slot: &slot, Created: 5},
			{ID: "b-live", Suite: "smoke-8", Runtime: "dev:ann@example.com", Owner: "ann@example.com", Reason: "manual", State: "running", Created: 6},
			{ID: "b-lost", Suite: "smoke-8", Runtime: "gone:zed@example.com", Owner: "zed@example.com", Reason: "manual", State: "done", Created: 7},
		},
		Runs: []EvalRun{
			{ID: "r1", Batch: "b-done", Task: "t1", Arm: "base", Fingerprint: "f1", RunNo: 1, State: "done", Passed: &yes, Cost: &cost, WallS: &n10, Diff: &diff, Created: 5},
			{ID: "r2", Batch: "b-done", Task: "t1", Arm: "alt", Fingerprint: "f2", RunNo: 1, State: "failed", Passed: &no, Created: 5},
			{ID: "r3", Batch: "b-live", Task: "t1", Arm: "base", Fingerprint: "f1", RunNo: 1, State: "queued", Created: 6},
		},
		Checks: []EvalCheck{{Run: "r1", Name: "unit", Passed: true, ExitCode: &n10}, {Run: "r3", Name: "unit", Passed: false}},
	}
	o := opts()
	o.History = fakeHistory{chats: []ChatRef{{ID: "c1", Owner: "ann@example.com", Machine: "dev"}}}
	o.Eval = fakeEval{d}

	dry := o
	dry.DryRun = true
	rep, err := Run(ctx, db, Data{}, dry)
	if err != nil {
		t.Fatal(err)
	}
	if e := rep.Eval; e.Arms.Added != 1 || e.Arms.Kept != 1 || e.Runs.Added != 2 || e.Checks.Added != 1 {
		t.Fatalf("dry report: %+v", e)
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_runs`) + scalar(t, db, `SELECT count(*) FROM eval_tasks`); n != 0 {
		t.Fatalf("a dry run wrote: %d", n)
	}

	rep, err = Run(ctx, db, Data{}, o)
	if err != nil {
		t.Fatal(err)
	}
	e := rep.Eval
	if e.Schedules.Added != 1 || e.Batches.Added != 1 || e.Runs.Added != 2 || e.Checks.Added != 1 || e.Tasks.Added != 1 || e.Suites.Added != 1 {
		t.Fatalf("report: %+v", e)
	}
	if e.Left["batch still live in llm"] != 1 || e.Left["batch of an owner whose email the hub does not know"] != 1 ||
		e.Left["schedule of a machine with no hub machine"] != 1 || e.Left["run of a batch that was not taken"] != 1 || e.Left["check of a run that was not taken"] != 1 {
		t.Fatalf("left: %+v", e.Left)
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_arms WHERE key = 'base' AND title = 'kept'`); n != 1 {
		t.Fatal("the hub's own arm was overwritten")
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_schedules WHERE id = 'sched-1' AND machine_id = 'm-ann' AND owner_user_id = 'u-ann' AND enabled`); n != 1 {
		t.Fatal("schedule sched-1")
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_runs WHERE id = 'r1' AND machine_id = 'm-ann' AND passed AND cost_usd = 0.5 AND wall_s = 10`); n != 1 {
		t.Fatal("run r1")
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_checks WHERE run_id = 'r1' AND machine_id = 'm-ann' AND exit_code = 10`); n != 1 {
		t.Fatal("check of r1")
	}
	if n := scalar(t, db, `SELECT count(*) FROM eval_runs WHERE id = 'r3'`); n != 0 {
		t.Fatal("a run of a live batch moved")
	}

	again, err := Run(ctx, db, Data{}, o)
	if err != nil {
		t.Fatal(err)
	}
	if a := again.Eval; a.Arms.Added+a.Tasks.Added+a.Suites.Added+a.Schedules.Added+a.Batches.Added+a.Runs.Added+a.Checks.Added != 0 {
		t.Fatalf("a second run changed something: %+v", a)
	}
}
