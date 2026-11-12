// Package eval is the hub's evaluation of the harness (moved from the old gateway so that it can be switched
// off): on a schedule it queues the golden tasks of a suite for a machine, hands them one at a time to the machine's
// runner, reads the verdicts back, closes the batches and warns the owner when the smoke suites get worse. The runs are
// paid from the machine owner's own subscription, so nothing starts for a person whose rollout switch is off, nor when the
// subscription's windows are filling up, nor when the week's runs already took their share. The tables are those of
// migration 0062 (docs/hub/eval.md).
package eval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// SwitchScheduler is the rollout switch (docs/hub/rollout-switches.md) that lets the scheduler queue and start runs on the
// machines of a person. Off for everyone until a row says otherwise.
const SwitchScheduler = "hub-eval"

const (
	leaseName = "eval-scheduler"
	tickEvery = 20 * time.Second
	firstWait = 30 * time.Second

	// A smoke run never starts when the owner's weekly window is used this much, or the five-hour one.
	weekGuard     = 70.0
	fiveHourGuard = 60.0
	// windowFresh is how old a reading of a window may be: an older one says nothing about the window now, and the run
	// waits for a newer one.
	windowFresh = 30 * time.Minute

	startGrace = 10 * time.Minute
	overrun    = 20 * time.Minute

	// A slot is taken when the scheduler comes to it within this long; a later one is let go.
	slotWindow = 6 * time.Hour
	// A queued run that did not start this long after its batch's slot is dropped; a big batch gets three times as long.
	queueTTL      = 24 * time.Hour
	bigBatchRuns  = 40
	maxStartTries = 30
	retryEvery    = 2 * time.Minute
	// A run the machine put off for the subscription (not for the task) goes back to the queue this many times.
	maxAttempts = 12
	deferFor    = 10 * time.Minute

	// weekCapPct is the share of the weekly window all the runs of the last seven days may take, a window being
	// usdPerWeekPct dollars per percent.
	weekCapPct    = 8.0
	usdPerWeekPct = 29.0
)

// smokeSuites run first and are the ones the regression check reads.
var smokeSuites = []string{"smoke-8", "aa-8"}

func isSmoke(suite string) bool { return suite == smokeSuites[0] || suite == smokeSuites[1] }

// Deps are what the scheduler is built from.
type Deps struct {
	DB   *store.DB
	Gate Gate
	// Link is the way to the machines' runners; nil means the scheduler does nothing.
	Link Link
	Log  *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Scheduler runs the evaluation. Of the replicas of a hub one does a pass at a time (store.TakeLease).
type Scheduler struct {
	db     *store.DB
	gate   Gate
	link   Link
	log    *slog.Logger
	now    func() time.Time
	holder string
}

// New builds the scheduler.
func New(d Deps) *Scheduler {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	s := &Scheduler{db: d.DB, gate: d.Gate, link: d.Link, log: d.Log, now: d.Now, holder: hex.EncodeToString(b)}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s
}

// Run does a pass every tickEvery until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	if s.link == nil || s.gate == nil {
		return
	}
	wait := firstWait
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = tickEvery
		if err := s.Pass(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("eval: pass failed", "error", err)
		}
	}
}

// Pass does one step: queues the due slots, then for each machine with open work reads the runner's state and hands the
// next run over, then closes what is finished. A pass that finds another replica holding the job does nothing.
func (s *Scheduler) Pass(ctx context.Context) error {
	ok, err := s.db.S().TakeLease(ctx, leaseName, s.holder, s.now(), 3*tickEvery)
	if err != nil || !ok {
		return err
	}
	if err := s.schedule(ctx); err != nil {
		s.log.Warn("eval: schedules not read", "error", err)
	}
	machines, err := s.openMachines(ctx)
	if err != nil {
		return err
	}
	for _, m := range machines {
		if err := s.poll(ctx, m.machine); err != nil && ctx.Err() == nil {
			s.log.Warn("eval: poll failed", "machine", m.machine, "error", err)
		}
		if err := s.dispatch(ctx, m); err != nil && ctx.Err() == nil {
			s.log.Warn("eval: dispatch failed", "machine", m.machine, "error", err)
		}
	}
	return s.closeBatches(ctx)
}

// on says whether the rollout switch is on for the person; an unknown person is off.
func (s *Scheduler) on(ctx context.Context, userID, machineID string) bool {
	var email string
	if err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = ?`, userID).Scan(&email); err != nil {
		return false
	}
	return s.gate.On(ctx, SwitchScheduler, strings.ToLower(email), machineID)
}

type schedule struct {
	id, suite, machine, owner, days, at string
	every                               int
	last                                *int64
}

// schedule turns every due slot of an enabled schedule, of a person the switch is on for, into a batch.
func (s *Scheduler) schedule(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT id, suite_key, machine_id, owner_user_id, days, at_msk, every_weeks, last_slot FROM eval_schedules WHERE enabled`)
	if err != nil {
		return err
	}
	var all []schedule
	for rows.Next() {
		var x schedule
		if err := rows.Scan(&x.id, &x.suite, &x.machine, &x.owner, &x.days, &x.at, &x.every, &x.last); err != nil {
			rows.Close()
			return err
		}
		all = append(all, x)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := s.now()
	for _, x := range all {
		var days []int
		if json.Unmarshal([]byte(x.days), &days) != nil {
			continue
		}
		slot, ok := LatestSlot(days, x.at, x.every, now)
		if !ok || now.Sub(slot) > slotWindow || (x.last != nil && *x.last >= slot.UnixMilli()) {
			continue
		}
		if !s.on(ctx, x.owner, x.machine) {
			continue
		}
		week, err := s.weekPct(ctx)
		if err != nil {
			return err
		}
		var made string
		err = s.db.Tx(ctx, func(st store.Store) error {
			if _, err := st.Q().Exec(ctx, `UPDATE eval_schedules SET last_slot = ?, updated_at = ? WHERE id = ?`, slot.UnixMilli(), store.Ms(now), x.id); err != nil {
				return err
			}
			id, err := enqueue(ctx, st, enqueueIn{suite: x.suite, machine: x.machine, owner: x.owner, reason: "schedule", schedule: x.id, slot: &slot, now: now})
			made = id
			if err != nil || id == "" {
				return err
			}
			if week >= weekCapPct {
				note := "прогоны недели уже взяли " + pct(week) + "% окна"
				if _, err := st.Q().Exec(ctx, `UPDATE eval_batches SET state = 'skipped', note = ?, finished_at = ? WHERE id = ?`, note, store.Ms(now), id); err != nil {
					return err
				}
				_, err = st.Q().Exec(ctx, `UPDATE eval_runs SET state = 'skipped', error = 'потолок недели', finished_at = ? WHERE batch_id = ?`, store.Ms(now), id)
			}
			return err
		})
		if err != nil {
			return err
		}
		if made != "" {
			s.log.Info("eval: batch queued", "schedule", x.id, "batch", made, "weekPct", pct(week))
		}
	}
	return nil
}

func pct(v float64) string {
	b, _ := json.Marshal(float64(int(v*10+0.5)) / 10)
	return string(b)
}

type openMachine struct{ machine, owner string }

// openMachines are the machines that have a batch not yet closed, with the person whose batch it is.
func (s *Scheduler) openMachines(ctx context.Context) ([]openMachine, error) {
	rows, err := s.db.Query(ctx, `SELECT machine_id, MIN(owner_user_id) FROM eval_batches WHERE state IN ('queued', 'running') GROUP BY machine_id ORDER BY machine_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []openMachine
	for rows.Next() {
		var m openMachine
		if err := rows.Scan(&m.machine, &m.owner); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// weekPct is the share of the weekly window the runs finished in the last seven days cost.
func (s *Scheduler) weekPct(ctx context.Context) (float64, error) {
	var usd *float64
	from := store.Ms(s.now().Add(-7 * 24 * time.Hour))
	if err := s.db.QueryRow(ctx, `SELECT SUM(cost_usd) FROM eval_runs WHERE finished_at > ?`, from).Scan(&usd); err != nil {
		return 0, err
	}
	if usd == nil {
		return 0, nil
	}
	return *usd / usdPerWeekPct, nil
}
