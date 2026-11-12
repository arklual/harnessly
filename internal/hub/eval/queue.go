package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type enqueueIn struct {
	suite, machine, owner, reason, schedule string
	slot                                    *time.Time
	arms                                    []string
	now                                     time.Time
}

func jsonList(raw string) []string {
	var out []string
	_ = json.Unmarshal([]byte(raw), &out)
	return out
}

// enqueue creates the batch of a suite and its runs (one per run number, task and arm) and returns the batch's id; "" when
// the schedule's slot already has a batch. Call it inside a transaction.
func enqueue(ctx context.Context, st store.Store, in enqueueIn) (string, error) {
	var tasksRaw, armsRaw string
	var k int
	err := st.Q().QueryRow(ctx, `SELECT task_keys, arms, k FROM eval_suites WHERE key = ?`, in.suite).Scan(&tasksRaw, &armsRaw, &k)
	if errors.Is(err, store.ErrNotFound) {
		return "", fmt.Errorf("eval: no suite %q", in.suite)
	}
	if err != nil {
		return "", err
	}
	tasks, armKeys := jsonList(tasksRaw), jsonList(armsRaw)
	if len(in.arms) > 0 {
		armKeys = in.arms
	}
	type arm struct{ key, fingerprint string }
	var arms []arm
	for _, key := range armKeys {
		var a arm
		a.key = key
		if err := st.Q().QueryRow(ctx, `SELECT fingerprint FROM eval_arms WHERE key = ?`, key).Scan(&a.fingerprint); err == nil {
			arms = append(arms, a)
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	ttl := queueTTL
	if len(tasks)*k*len(arms) >= bigBatchRuns {
		ttl *= 3
	}
	base := in.now
	var slot, sched any
	if in.slot != nil {
		base = *in.slot
		slot = in.slot.UnixMilli()
		sched = in.schedule
	}
	id := newUUID()
	res, err := st.Q().Exec(ctx, `INSERT INTO eval_batches (id, schedule_id, suite_key, machine_id, owner_user_id, reason, slot, state, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?) ON CONFLICT (schedule_id, slot) DO NOTHING`,
		id, sched, in.suite, in.machine, in.owner, in.reason, slot, store.Ms(in.now), store.Ms(base.Add(ttl)))
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", nil
	}
	for no := 1; no <= k; no++ {
		for _, task := range tasks {
			for _, a := range arms {
				if _, err := st.Q().Exec(ctx, `INSERT INTO eval_runs (id, batch_id, machine_id, task_key, arm_key, fingerprint, run_no, state, created_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, 'queued', ?)`, newUUID(), id, in.machine, task, a.key, a.fingerprint, no, store.Ms(in.now)); err != nil {
					return "", err
				}
			}
		}
	}
	return id, nil
}

type activeRun struct {
	id, state string
	claimedAt int64
	wallS     int
}

// activeRuns are the machine's runs that are with the runner now.
func (s *Scheduler) activeRuns(ctx context.Context, machine string) ([]activeRun, error) {
	rows, err := s.db.Query(ctx, `SELECT r.id, r.state, COALESCE(r.claimed_at, 0), t.limits FROM eval_runs r LEFT JOIN eval_tasks t ON t.key = r.task_key
		WHERE r.machine_id = ? AND r.state IN ('claimed', 'preparing', 'running', 'grading')`, machine)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []activeRun
	for rows.Next() {
		var r activeRun
		var limits *string
		if err := rows.Scan(&r.id, &r.state, &r.claimedAt, &limits); err != nil {
			return nil, err
		}
		if limits != nil {
			var l struct {
				WallS int `json:"wallS"`
			}
			_ = json.Unmarshal([]byte(*limits), &l)
			r.wallS = l.WallS
		}
		if r.wallS <= 0 {
			r.wallS = 1800
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// windows reads the owner's subscription windows on the machine; ok is false when there is no fresh reading of the weekly
// or the five-hour window of the provider the runs use.
func (s *Scheduler) windows(ctx context.Context, machine string) (week, five float64, ok bool, err error) {
	ws, err := s.db.S().ProviderWindowsOfMachine(ctx, machine)
	if err != nil {
		return 0, 0, false, err
	}
	since := store.Ms(s.now().Add(-windowFresh))
	var haveWeek, haveFive bool
	for _, w := range ws {
		if w.Provider != "claude" || w.SampledAt < since {
			continue
		}
		switch w.Window {
		case "seven_day":
			week, haveWeek = w.UsedPct, true
		case "five_hour":
			five, haveFive = w.UsedPct, true
		}
	}
	return week, five, haveWeek && haveFive, nil
}

// dispatch hands the machine's next queued run to its runner, unless a run is already with it, the switch is off, the
// subscription's windows or the week's cap say wait.
func (s *Scheduler) dispatch(ctx context.Context, m openMachine) error {
	active, err := s.activeRuns(ctx, m.machine)
	if err != nil || len(active) > 0 {
		return err
	}
	if !s.on(ctx, m.owner, m.machine) {
		return nil
	}
	week, five, fresh, err := s.windows(ctx, m.machine)
	if err != nil {
		return err
	}
	switch {
	case !fresh:
		return s.noteWaiting(ctx, m.machine, "ждёт: нет свежих данных об окнах подписки")
	case week >= weekGuard:
		return s.noteWaiting(ctx, m.machine, fmt.Sprintf("ждёт: недельное окно подписки занято на %.0f%% (порог %.0f%%)", week, weekGuard))
	case five >= fiveHourGuard:
		return s.noteWaiting(ctx, m.machine, fmt.Sprintf("ждёт: пятичасовое окно подписки занято на %.0f%% (порог %.0f%%)", five, fiveHourGuard))
	}
	cap, err := s.weekPct(ctx)
	if err != nil {
		return err
	}
	if cap >= weekCapPct {
		_, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = 'skipped', error = 'потолок недели', finished_at = ? WHERE machine_id = ? AND state = 'queued'`, store.Ms(s.now()), m.machine)
		return err
	}
	// Nothing is claimed for a machine out of reach, so one that is away for an hour does not burn the queue run by run.
	if !s.link.Connected(m.machine) {
		return s.noteWaiting(ctx, m.machine, "ждёт: машина не на связи")
	}
	req, err := s.claim(ctx, m.machine)
	if err != nil || req == nil {
		return err
	}
	if err := s.link.Start(ctx, m.machine, *req); err != nil {
		if errors.Is(err, ErrNotDelivered) {
			s.log.Warn("eval: run not delivered, queued again", "machine", m.machine, "run", req.RunID, "error", err)
			return s.requeue(ctx, req.RunID, err.Error())
		}
		// A timeout may come after the runner started: the run stays claimed and the poll decides.
		s.log.Warn("eval: start unanswered, waiting for the runner", "machine", m.machine, "run", req.RunID, "error", err)
		return nil
	}
	s.log.Info("eval: run handed over", "machine", m.machine, "run", req.RunID)
	return nil
}

func (s *Scheduler) noteWaiting(ctx context.Context, machine, note string) error {
	_, err := s.db.Exec(ctx, `UPDATE eval_batches SET note = ? WHERE machine_id = ? AND state = 'queued' AND (note IS NULL OR note <> ?)`, note, machine, note)
	return err
}

// claim marks the machine's next queued run claimed and builds the runner's request: smoke suites first, then batches of a
// schedule before manual ones, oldest batch first.
func (s *Scheduler) claim(ctx context.Context, machine string) (*RunRequest, error) {
	now := store.Ms(s.now())
	var id, taskKey, armKey string
	err := s.db.QueryRow(ctx, `SELECT r.id, r.task_key, r.arm_key FROM eval_runs r JOIN eval_batches b ON b.id = r.batch_id
		WHERE r.machine_id = ? AND r.state = 'queued' AND (r.retry_after IS NULL OR r.retry_after <= ?)
		ORDER BY CASE WHEN b.suite_key IN ('smoke-8', 'aa-8') THEN 0 ELSE 1 END, CASE WHEN b.reason = 'schedule' THEN 0 ELSE 1 END,
			b.created_at, r.run_no, r.created_at LIMIT 1`, machine, now).Scan(&id, &taskKey, &armKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var task struct{ repo, base, prompt, checks, hidden, forbidden, limits, extra string }
	if err := s.db.QueryRow(ctx, `SELECT repo, base, prompt, checks, hidden, forbidden, limits, extra FROM eval_tasks WHERE key = ?`, taskKey).
		Scan(&task.repo, &task.base, &task.prompt, &task.checks, &task.hidden, &task.forbidden, &task.limits, &task.extra); err != nil {
		return nil, fmt.Errorf("eval: task %q: %w", taskKey, err)
	}
	var arm string
	if err := s.db.QueryRow(ctx, `SELECT spec FROM eval_arms WHERE key = ?`, armKey).Scan(&arm); err != nil {
		return nil, fmt.Errorf("eval: arm %q: %w", armKey, err)
	}
	// The task is what its extra fields say, with the fixed ones over them.
	doc := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(task.extra), &doc)
	str := func(v string) json.RawMessage { b, _ := json.Marshal(v); return b }
	doc["key"], doc["repo"], doc["base"], doc["prompt"] = str(taskKey), str(task.repo), str(task.base), str(task.prompt)
	doc["checks"], doc["hidden"], doc["forbidden"], doc["limits"] = json.RawMessage(task.checks), json.RawMessage(task.hidden), json.RawMessage(task.forbidden), json.RawMessage(task.limits)
	taskDoc, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("eval: task %q is not valid: %w", taskKey, err)
	}
	res, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = 'claimed', claimed_at = ? WHERE id = ? AND state = 'queued'`, now, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, nil
	}
	if _, err := s.db.Exec(ctx, `UPDATE eval_batches SET state = 'running', note = NULL WHERE id = (SELECT batch_id FROM eval_runs WHERE id = ?) AND state = 'queued'`, id); err != nil {
		return nil, err
	}
	return &RunRequest{RunID: id, Task: taskDoc, Arm: json.RawMessage(arm)}, nil
}

// requeue puts a run the machine did not take back for two minutes; after maxStartTries tries it fails.
func (s *Scheduler) requeue(ctx context.Context, runID, why string) error {
	now := s.now()
	msg := "машина не приняла прогон: " + why
	_, err := s.db.Exec(ctx, `UPDATE eval_runs SET
			state = CASE WHEN attempts + 1 >= ? THEN 'failed' ELSE 'queued' END, error = ?, attempts = attempts + 1,
			claimed_at = CASE WHEN attempts + 1 >= ? THEN claimed_at ELSE NULL END,
			finished_at = CASE WHEN attempts + 1 >= ? THEN ? ELSE NULL END,
			retry_after = CASE WHEN attempts + 1 >= ? THEN NULL ELSE ? END
		WHERE id = ? AND state = 'claimed'`,
		maxStartTries, msg, maxStartTries, maxStartTries, store.Ms(now), maxStartTries, store.Ms(now.Add(retryEvery)), runID)
	return err
}

// failRun ends a run that is not yet over.
func (s *Scheduler) failRun(ctx context.Context, runID, state, why string) error {
	_, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = ?, error = ?, finished_at = ? WHERE id = ? AND state NOT IN ('done', 'failed', 'timeout', 'stopped', 'skipped')`,
		state, why, store.Ms(s.now()), runID)
	return err
}

// poll reads the runner's state of the machine's active runs. A poll that did not get through says nothing about the runs.
func (s *Scheduler) poll(ctx context.Context, machine string) error {
	active, err := s.activeRuns(ctx, machine)
	if err != nil || len(active) == 0 {
		return err
	}
	ids := make([]string, len(active))
	for i, r := range active {
		ids[i] = r.id
	}
	states, err := s.link.Poll(ctx, machine, ids)
	if err != nil {
		return err
	}
	now := s.now()
	for _, r := range active {
		age := now.Sub(time.UnixMilli(r.claimedAt))
		st, ok := states[r.id]
		switch {
		case ok:
			if err := s.apply(ctx, machine, st); err != nil {
				return err
			}
			if !terminalStates[st.State] && age > time.Duration(r.wallS)*time.Second+overrun {
				_ = s.link.Stop(ctx, machine, r.id)
				if err := s.failRun(ctx, r.id, "timeout", "прогон не закончился вовремя"); err != nil {
					return err
				}
			}
		case r.state == "claimed" && age > startGrace:
			if err := s.failRun(ctx, r.id, "failed", "прогонщик не начал работу за 10 минут"); err != nil {
				return err
			}
		}
	}
	return nil
}

// deferred are the put-offs the runner reports as skipped that are about the subscription, not the task: they are tried again.
func deferred(msg string) bool {
	return strings.HasPrefix(msg, "вход по подписке") || strings.HasPrefix(msg, "лимит подписки")
}

func parseMs(v string) *int64 {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil
	}
	ms := t.UnixMilli()
	return &ms
}

// apply writes the runner's report of one run; a finished run also gets its checks.
func (s *Scheduler) apply(ctx context.Context, machine string, st RunState) error {
	now := s.now()
	if st.State == "skipped" && deferred(st.Error) {
		res, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = 'queued', error = ?, claimed_at = NULL, started_at = NULL, finished_at = NULL,
			attempts = attempts + 1, retry_after = ? WHERE id = ? AND machine_id = ? AND attempts < ?`,
			st.Error, store.Ms(now.Add(deferFor)), st.RunID, machine, maxAttempts)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	terminal := terminalStates[st.State]
	var usd *float64
	var calls, first, peak, subagents *int
	var in, out *int64
	if u := st.Usage; u != nil {
		usd, first, peak = u.USD, u.FirstCtx, u.PeakCtx
		calls, in, out, subagents = &u.ModelCalls, &u.TokensIn, &u.TokensOut, &u.Subagents
	}
	var diff *string
	if st.Diff != nil {
		b, _ := json.Marshal(st.Diff)
		d := string(b)
		diff = &d
	}
	var errText *string
	if st.Error != "" {
		errText = &st.Error
	}
	var finished *int64
	if terminal {
		ms := store.Ms(now)
		if f := parseMs(st.FinishedAt); f != nil {
			ms = *f
		}
		finished = &ms
	}
	var fp *string
	if st.Fingerprint != "" {
		fp = &st.Fingerprint
	}
	if _, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = ?, passed = ?, cost_usd = ?, wall_s = ?, model_calls = ?, first_ctx = ?, peak_ctx = ?, tokens_in = ?, tokens_out = ?,
			subagents = ?, diff = ?, error = ?, fingerprint = COALESCE(?, fingerprint), started_at = COALESCE(started_at, ?), finished_at = ?
		WHERE id = ? AND machine_id = ?`,
		st.State, st.Passed, usd, st.WallS, calls, first, peak, in, out, subagents, diff, errText, fp, parseMs(st.StartedAt), finished, st.RunID, machine); err != nil {
		return err
	}
	if !terminal {
		return nil
	}
	for _, c := range st.Checks {
		if _, err := s.db.Exec(ctx, `INSERT INTO eval_checks (run_id, machine_id, name, passed, exit_code, duration_ms) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (run_id, name) DO UPDATE SET passed = excluded.passed, exit_code = excluded.exit_code, duration_ms = excluded.duration_ms`,
			st.RunID, machine, c.Name, c.Passed, c.ExitCode, c.Ms); err != nil {
			return err
		}
	}
	return nil
}
