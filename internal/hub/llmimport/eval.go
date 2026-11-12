package llmimport

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The evaluation step: the catalog of golden tasks, the schedules, and the batches and runs that are over, with
// their checks, from llm's eval_* tables into the hub's (docs/hub/eval.md). It runs inside the chat step, which learns
// which hub machine an llm machine is for a person. A row is taken once, by its id: a row the hub has is left as it is, so
// the step can be run again and changes nothing the second time. A batch that is still queued or running in llm is left
// (llm is still running it; it expires by itself), and so are its runs.

// EvalArm is a row of llm's eval_arms; the JSON columns come as text.
type EvalArm struct {
	Key, Title, Spec, Fingerprint string
	Updated                       int64
}

// EvalTask is a row of llm's eval_tasks.
type EvalTask struct {
	Key, Repo, Base          string
	Source                   *string
	Prompt, Checks, Hidden   string
	Forbidden, Limits, Extra string
	HeldOut                  bool
	Updated                  int64
}

// EvalSuite is a row of llm's eval_suites.
type EvalSuite struct {
	Key, Title, TaskKeys, Arms string
	K                          int
	Updated                    int64
}

// EvalSchedule is a row of llm's eval_schedules. Runtime is «<machine>:<email>».
type EvalSchedule struct {
	ID, Suite, Runtime, Owner, Days, AtMSK string
	Every, RunsPerWeek                     int
	WeekPct                                float64
	Enabled                                bool
	LastSlot                               *int64
	Updated                                int64
}

// EvalBatch is a row of llm's eval_batches.
type EvalBatch struct {
	ID, Suite, Runtime, Owner, Reason, State string
	Schedule, Note                           *string
	Slot, Finished, Expires                  *int64
	Created                                  int64
}

// EvalRun is a row of llm's eval_runs.
type EvalRun struct {
	ID, Batch, Task, Arm, Fingerprint, State string
	RunNo, Attempts                          int
	Passed                                   *bool
	Cost                                     *float64
	WallS, ModelCalls, FirstCtx, PeakCtx     *int
	Subagents                                *int
	TokensIn, TokensOut                      *int64
	Diff, Error                              *string
	RetryAfter, Claimed, Started, Finished   *int64
	Created                                  int64
}

// EvalCheck is a row of llm's eval_checks.
type EvalCheck struct {
	Run, Name          string
	Passed             bool
	ExitCode, Duration *int
}

// EvalData is everything the evaluation step reads.
type EvalData struct {
	Arms      []EvalArm
	Tasks     []EvalTask
	Suites    []EvalSuite
	Schedules []EvalSchedule
	Batches   []EvalBatch
	Runs      []EvalRun
	Checks    []EvalCheck
}

// Eval is what the evaluation step reads. *Source is one.
type Eval interface {
	Eval(ctx context.Context) (EvalData, error)
}

// EvalReport counts what the evaluation step did.
type EvalReport struct {
	Arms, Tasks, Suites, Schedules, Batches, Runs, Checks struct{ Read, Added, Kept int }
	// Left says why rows were not taken, by reason.
	Left map[string]int
}

func (e EvalReport) summary(b *strings.Builder) {
	line := func(name string, c struct{ Read, Added, Kept int }) {
		fmt.Fprintf(b, "eval %-10s %d read, %d added, %d kept (the hub had them)\n", name+":", c.Read, c.Added, c.Kept)
	}
	line("arms", e.Arms)
	line("tasks", e.Tasks)
	line("suites", e.Suites)
	line("schedules", e.Schedules)
	line("batches", e.Batches)
	line("runs", e.Runs)
	line("checks", e.Checks)
	for _, k := range sortedKeys(e.Left) {
		fmt.Fprintf(b, "  eval left: %s: %d\n", k, e.Left[k])
	}
}

// moveEval runs the evaluation step in one transaction (a dry run rolls it back, so its counts are the real ones).
func moveEval(ctx context.Context, db *store.DB, src Eval, o Options, users, machineOf map[string]string, e *EvalReport) error {
	e.Left = map[string]int{}
	d, err := src.Eval(ctx)
	if err != nil {
		return err
	}
	return runTx(ctx, db, o.DryRun, func(st store.Store) error {
		put := func(c *struct{ Read, Added, Kept int }, query string, args ...any) error {
			c.Read++
			res, err := st.Q().Exec(ctx, query, args...)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				c.Added++
			} else {
				c.Kept++
			}
			return nil
		}
		for _, a := range d.Arms {
			if err := put(&e.Arms, `INSERT INTO eval_arms (key, title, spec, fingerprint, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				a.Key, a.Title, a.Spec, a.Fingerprint, a.Updated); err != nil {
				return fmt.Errorf("arm %s: %w", a.Key, err)
			}
		}
		for _, t := range d.Tasks {
			if err := put(&e.Tasks, `INSERT INTO eval_tasks (key, repo, base, source, prompt, checks, hidden, forbidden, limits, extra, held_out, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				t.Key, t.Repo, t.Base, t.Source, t.Prompt, t.Checks, t.Hidden, t.Forbidden, t.Limits, t.Extra, t.HeldOut, t.Updated); err != nil {
				return fmt.Errorf("task %s: %w", t.Key, err)
			}
		}
		for _, s := range d.Suites {
			if err := put(&e.Suites, `INSERT INTO eval_suites (key, title, task_keys, k, arms, updated_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				s.Key, s.Title, s.TaskKeys, s.K, s.Arms, s.Updated); err != nil {
				return fmt.Errorf("suite %s: %w", s.Key, err)
			}
		}
		// Which hub machine and person a schedule or a batch is for: llm names them «<llm machine>:<email>» and the owner.
		resolve := func(runtime, owner string) (machine, user string, why string) {
			llmMachine, _, _ := strings.Cut(runtime, ":")
			user, ok := users[strings.ToLower(owner)]
			if !ok {
				return "", "", "an owner whose email the hub does not know"
			}
			machine, ok = machineOf[llmMachine+"|"+strings.ToLower(owner)]
			if !ok {
				return "", "", "a machine with no hub machine"
			}
			return machine, user, ""
		}
		for _, s := range d.Schedules {
			machine, user, why := resolve(s.Runtime, s.Owner)
			if why != "" {
				e.Schedules.Read++
				e.Left["schedule of "+why]++
				continue
			}
			// Imported switched off: the hub's scheduler (behind its own switch) is turned on by a person, not by the move.
			if err := put(&e.Schedules, `INSERT INTO eval_schedules (id, suite_key, machine_id, owner_user_id, days, at_msk, every_weeks, runs_per_week, week_pct, enabled, last_slot, updated_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				s.ID, s.Suite, machine, user, s.Days, s.AtMSK, s.Every, s.RunsPerWeek, s.WeekPct, s.Enabled, s.LastSlot, s.Updated); err != nil {
				return fmt.Errorf("schedule %s: %w", s.ID, err)
			}
		}
		taken := map[string]string{} // batch id → its hub machine
		for _, b := range d.Batches {
			if b.State == "queued" || b.State == "running" {
				e.Batches.Read++
				e.Left["batch still live in llm"]++
				continue
			}
			machine, user, why := resolve(b.Runtime, b.Owner)
			if why != "" {
				e.Batches.Read++
				e.Left["batch of "+why]++
				continue
			}
			if err := put(&e.Batches, `INSERT INTO eval_batches (id, schedule_id, suite_key, machine_id, owner_user_id, reason, slot, state, note, created_at, finished_at, expires_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				b.ID, b.Schedule, b.Suite, machine, user, b.Reason, b.Slot, b.State, b.Note, b.Created, b.Finished, b.Expires); err != nil {
				return fmt.Errorf("batch %s: %w", b.ID, err)
			}
			taken[b.ID] = machine
		}
		ran := map[string]string{} // run id → its hub machine
		for _, r := range d.Runs {
			machine, ok := taken[r.Batch]
			if !ok {
				e.Runs.Read++
				e.Left["run of a batch that was not taken"]++
				continue
			}
			if err := put(&e.Runs, `INSERT INTO eval_runs (id, batch_id, machine_id, task_key, arm_key, fingerprint, run_no, state, passed, cost_usd, wall_s, model_calls,
				first_ctx, peak_ctx, tokens_in, tokens_out, subagents, diff, error, attempts, retry_after, claimed_at, started_at, finished_at, created_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				r.ID, r.Batch, machine, r.Task, r.Arm, r.Fingerprint, r.RunNo, r.State, r.Passed, r.Cost, r.WallS, r.ModelCalls,
				r.FirstCtx, r.PeakCtx, r.TokensIn, r.TokensOut, r.Subagents, r.Diff, r.Error, r.Attempts, r.RetryAfter, r.Claimed, r.Started, r.Finished, r.Created); err != nil {
				return fmt.Errorf("run %s: %w", r.ID, err)
			}
			ran[r.ID] = machine
		}
		for _, c := range d.Checks {
			machine, ok := ran[c.Run]
			if !ok {
				e.Checks.Read++
				e.Left["check of a run that was not taken"]++
				continue
			}
			if err := put(&e.Checks, `INSERT INTO eval_checks (run_id, machine_id, name, passed, exit_code, duration_ms) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
				c.Run, machine, c.Name, c.Passed, c.ExitCode, c.Duration); err != nil {
				return fmt.Errorf("check %s/%s: %w", c.Run, c.Name, err)
			}
		}
		return nil
	})
}

// Eval reads llm's eval_* tables in one read-only transaction.
func (s *Source) Eval(ctx context.Context) (EvalData, error) {
	var d EvalData
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return d, err
	}
	defer func() { _ = tx.Rollback() }()
	each := func(table, query string, scan func(r *sql.Rows) error) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("%s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			if err := scan(rows); err != nil {
				return fmt.Errorf("%s: %w", table, err)
			}
		}
		return rows.Err()
	}
	if err := each("eval_arms", `select key, title, spec::text, fingerprint, `+msOf("updated_at")+` from eval_arms order by key`, func(r *sql.Rows) error {
		var a EvalArm
		if err := r.Scan(&a.Key, &a.Title, &a.Spec, &a.Fingerprint, &a.Updated); err != nil {
			return err
		}
		d.Arms = append(d.Arms, a)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_tasks", `select key, repo, base, source, prompt, checks::text, hidden::text, to_json(forbidden)::text, limits::text, extra::text, held_out, `+msOf("updated_at")+` from eval_tasks order by key`, func(r *sql.Rows) error {
		var t EvalTask
		var src sql.NullString
		if err := r.Scan(&t.Key, &t.Repo, &t.Base, &src, &t.Prompt, &t.Checks, &t.Hidden, &t.Forbidden, &t.Limits, &t.Extra, &t.HeldOut, &t.Updated); err != nil {
			return err
		}
		if src.Valid {
			t.Source = &src.String
		}
		d.Tasks = append(d.Tasks, t)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_suites", `select key, title, to_json(task_keys)::text, k, to_json(arms)::text, `+msOf("updated_at")+` from eval_suites order by key`, func(r *sql.Rows) error {
		var s EvalSuite
		if err := r.Scan(&s.Key, &s.Title, &s.TaskKeys, &s.K, &s.Arms, &s.Updated); err != nil {
			return err
		}
		d.Suites = append(d.Suites, s)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_schedules", `select id, suite_key, runtime_id, lower(owner_email), to_json(days)::text, at_msk, every_weeks, runs_per_week, week_pct::float8, enabled, `+
		msOf("last_slot")+`, `+msOf("updated_at")+` from eval_schedules order by id`, func(r *sql.Rows) error {
		var s EvalSchedule
		var last sql.NullInt64
		if err := r.Scan(&s.ID, &s.Suite, &s.Runtime, &s.Owner, &s.Days, &s.AtMSK, &s.Every, &s.RunsPerWeek, &s.WeekPct, &s.Enabled, &last, &s.Updated); err != nil {
			return err
		}
		if last.Valid {
			s.LastSlot = &last.Int64
		}
		d.Schedules = append(d.Schedules, s)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_batches", `select id::text, schedule_id, suite_key, runtime_id, lower(owner_email), reason, state, note, `+msOf("slot")+`, `+msOf("created_at")+`, `+
		msOf("finished_at")+`, `+msOf("expires_at")+` from eval_batches order by created_at, id`, func(r *sql.Rows) error {
		var b EvalBatch
		var sched, note sql.NullString
		var slot, fin, exp sql.NullInt64
		if err := r.Scan(&b.ID, &sched, &b.Suite, &b.Runtime, &b.Owner, &b.Reason, &b.State, &note, &slot, &b.Created, &fin, &exp); err != nil {
			return err
		}
		if sched.Valid {
			b.Schedule = &sched.String
		}
		if note.Valid {
			b.Note = &note.String
		}
		b.Slot, b.Finished, b.Expires = nullMs(slot), nullMs(fin), nullMs(exp)
		d.Batches = append(d.Batches, b)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_runs", `select id::text, batch_id::text, task_key, arm_key, fingerprint, run_no, state, passed, cost_usd::float8, wall_s, model_calls, first_ctx, peak_ctx,
		tokens_in, tokens_out, subagents, diff::text, error, attempts, `+msOf("retry_after")+`, `+msOf("claimed_at")+`, `+msOf("started_at")+`, `+msOf("finished_at")+`, `+
		msOf("created_at")+` from eval_runs order by created_at, id`, func(r *sql.Rows) error {
		var x EvalRun
		var passed sql.NullBool
		var cost sql.NullFloat64
		var wall, calls, first, peak, subs sql.NullInt64
		var tin, tout sql.NullInt64
		var diff, errText sql.NullString
		var retry, claimed, started, finished sql.NullInt64
		if err := r.Scan(&x.ID, &x.Batch, &x.Task, &x.Arm, &x.Fingerprint, &x.RunNo, &x.State, &passed, &cost, &wall, &calls, &first, &peak,
			&tin, &tout, &subs, &diff, &errText, &x.Attempts, &retry, &claimed, &started, &finished, &x.Created); err != nil {
			return err
		}
		if passed.Valid {
			x.Passed = &passed.Bool
		}
		if cost.Valid {
			x.Cost = &cost.Float64
		}
		x.WallS, x.ModelCalls, x.FirstCtx, x.PeakCtx, x.Subagents = nullInt(wall), nullInt(calls), nullInt(first), nullInt(peak), nullInt(subs)
		x.TokensIn, x.TokensOut = nullMs(tin), nullMs(tout)
		if diff.Valid {
			x.Diff = &diff.String
		}
		if errText.Valid {
			x.Error = &errText.String
		}
		x.RetryAfter, x.Claimed, x.Started, x.Finished = nullMs(retry), nullMs(claimed), nullMs(started), nullMs(finished)
		d.Runs = append(d.Runs, x)
		return nil
	}); err != nil {
		return d, err
	}
	if err := each("eval_checks", `select run_id::text, name, passed, exit_code, duration_ms from eval_checks order by run_id, name`, func(r *sql.Rows) error {
		var c EvalCheck
		var code, dur sql.NullInt64
		if err := r.Scan(&c.Run, &c.Name, &c.Passed, &code, &dur); err != nil {
			return err
		}
		c.ExitCode, c.Duration = nullInt(code), nullInt(dur)
		d.Checks = append(d.Checks, c)
		return nil
	}); err != nil {
		return d, err
	}
	return d, tx.Commit()
}

// nullMs is a nullable BIGINT as a pointer (nil for NULL).
func nullMs(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	return &n.Int64
}

// nullInt is a nullable integer as a pointer (nil for NULL).
func nullInt(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
