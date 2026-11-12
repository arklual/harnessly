package store

import (
	"context"
	"database/sql"
	"errors"
)

// EffFact is a fact a collector took from an outside source. Times are Unix milliseconds.
type EffFact struct {
	Source string
	Key    string
	At     int64
	Data   string
}

// EffSourceState is how the last attempt of one collector ended.
type EffSourceState struct {
	Source      string
	CollectedAt int64
	OK          bool
	Error       string
	OKAt        int64
	Facts       int
}

// UpsertEffFacts stores facts of one source, replacing a fact with the same key.
func (s Store) UpsertEffFacts(ctx context.Context, facts []EffFact, now int64) error {
	for _, f := range facts {
		if _, err := s.q.Exec(ctx, `INSERT INTO eff_source_facts (source, fact_key, at, data, collected_at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (source, fact_key) DO UPDATE SET at = excluded.at, data = excluded.data, collected_at = excluded.collected_at`,
			f.Source, f.Key, f.At, f.Data, now); err != nil {
			return err
		}
	}
	return nil
}

// EffFactsBetween lists the facts of a source with at in [from, to), oldest first.
func (s Store) EffFactsBetween(ctx context.Context, source string, from, to int64) ([]EffFact, error) {
	rows, err := s.q.Query(ctx, `SELECT source, fact_key, at, data FROM eff_source_facts WHERE source = ? AND at >= ? AND at < ? ORDER BY at, fact_key`, source, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffFact
	for rows.Next() {
		var f EffFact
		if err := rows.Scan(&f.Source, &f.Key, &f.At, &f.Data); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// NewestEffFactAt is the newest fact time of a source, 0 when there is none.
func (s Store) NewestEffFactAt(ctx context.Context, source string) (int64, error) {
	var v *int64
	if err := s.q.QueryRow(ctx, `SELECT MAX(at) FROM eff_source_facts WHERE source = ?`, source).Scan(&v); err != nil {
		return 0, err
	}
	if v == nil {
		return 0, nil
	}
	return *v, nil
}

// RecordEffSource notes how a collector's attempt ended; a success moves ok_at, a failure keeps the last one.
func (s Store) RecordEffSource(ctx context.Context, st EffSourceState) error {
	var okAt any
	if st.OK {
		okAt = st.CollectedAt
	}
	_, err := s.q.Exec(ctx, `INSERT INTO eff_source_state (source, collected_at, ok, error, ok_at, facts) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (source) DO UPDATE SET collected_at = excluded.collected_at, ok = excluded.ok, error = excluded.error,
		ok_at = COALESCE(excluded.ok_at, eff_source_state.ok_at), facts = excluded.facts`,
		st.Source, st.CollectedAt, st.OK, nullStr(st.Error), okAt, st.Facts)
	return err
}

// EffSourceStates lists every collector's state.
func (s Store) EffSourceStates(ctx context.Context) ([]EffSourceState, error) {
	rows, err := s.q.Query(ctx, `SELECT source, collected_at, ok, error, ok_at, facts FROM eff_source_state ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffSourceState
	for rows.Next() {
		var st EffSourceState
		var e sql.NullString
		var okAt sql.NullInt64
		if err := rows.Scan(&st.Source, &st.CollectedAt, &st.OK, &e, &okAt, &st.Facts); err != nil {
			return nil, err
		}
		st.Error, st.OKAt = e.String, okAt.Int64
		out = append(out, st)
	}
	return out, rows.Err()
}

// PutEffPulse stores a person's answer for a week (its Monday as YYYY-MM-DD), replacing an earlier one.
func (s Store) PutEffPulse(ctx context.Context, userID, week string, speed, quality, friction int, now int64) error {
	if speed < 1 || speed > 5 || quality < 1 || quality > 5 || friction < 1 || friction > 5 {
		return errors.New("pulse answers are 1..5")
	}
	_, err := s.q.Exec(ctx, `INSERT INTO eff_pulse (user_id, week, speed, quality, friction, answered_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, week) DO UPDATE SET speed = excluded.speed, quality = excluded.quality, friction = excluded.friction, answered_at = excluded.answered_at`,
		userID, week, speed, quality, friction, now)
	return err
}

// EffPulseAnswer is one stored week of a person's pulse.
type EffPulseAnswer struct {
	Week                     string
	Speed, Quality, Friction int
}

// EffPulsesOf lists the person's own answers, newest week first, at most limit.
func (s Store) EffPulsesOf(ctx context.Context, userID string, limit int) ([]EffPulseAnswer, error) {
	rows, err := s.q.Query(ctx, `SELECT week, speed, quality, friction FROM eff_pulse WHERE user_id = ? ORDER BY week DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffPulseAnswer
	for rows.Next() {
		var a EffPulseAnswer
		if err := rows.Scan(&a.Week, &a.Speed, &a.Quality, &a.Friction); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// EffRegressionAlert is one regression alert of the evaluation: a metric that left its band after a change of the harness.
// Times are Unix milliseconds; Diff is a JSON array of lines.
type EffRegressionAlert struct {
	ID, BatchID, Metric                string
	Baseline, Observed                 float64
	Band                               int
	FingerprintFrom, FingerprintTo     string
	Diff, Addressee, State, ResolvedBy string
	SeenAt                             *int64
	CreatedAt                          int64
}

// InsertEffRegressionAlerts stores alerts, leaving one with the same id as it is: the state a person gave it stays.
func (s Store) InsertEffRegressionAlerts(ctx context.Context, alerts []EffRegressionAlert) error {
	for _, a := range alerts {
		if _, err := s.q.Exec(ctx, `INSERT INTO eff_regression_alerts (id, batch_id, metric, baseline, observed, band, fingerprint_from,
			fingerprint_to, diff, addressee, seen_at, state, resolved_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (id) DO NOTHING`,
			a.ID, a.BatchID, a.Metric, a.Baseline, a.Observed, a.Band, a.FingerprintFrom, a.FingerprintTo, a.Diff, a.Addressee,
			a.SeenAt, a.State, a.ResolvedBy, a.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

// EffRegressionAlertsFor lists the alerts addressed to an e-mail, newest first; openOnly leaves out the resolved ones.
func (s Store) EffRegressionAlertsFor(ctx context.Context, addressee string, openOnly bool) ([]EffRegressionAlert, error) {
	q := `SELECT id, batch_id, metric, baseline, observed, band, fingerprint_from, fingerprint_to, diff, addressee, seen_at, state,
		resolved_by, created_at FROM eff_regression_alerts WHERE addressee = ?`
	if openOnly {
		q += ` AND state = 'open'`
	}
	rows, err := s.q.Query(ctx, q+` ORDER BY created_at DESC, id`, addressee)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EffRegressionAlert
	for rows.Next() {
		var a EffRegressionAlert
		if err := rows.Scan(&a.ID, &a.BatchID, &a.Metric, &a.Baseline, &a.Observed, &a.Band, &a.FingerprintFrom, &a.FingerprintTo,
			&a.Diff, &a.Addressee, &a.SeenAt, &a.State, &a.ResolvedBy, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceEffFacts stores facts of one source and drops the facts of the source that this pass did not bring: a source
// that reports a whole current state (the issues in progress, the documents of a window) leaves nothing stale.
func (s Store) ReplaceEffFacts(ctx context.Context, source string, facts []EffFact, now int64) error {
	if err := s.UpsertEffFacts(ctx, facts, now); err != nil {
		return err
	}
	_, err := s.q.Exec(ctx, `DELETE FROM eff_source_facts WHERE source = ? AND collected_at <> ?`, source, now)
	return err
}

// PruneEffFacts drops the facts of a source older than a time.
func (s Store) PruneEffFacts(ctx context.Context, source string, before int64) error {
	_, err := s.q.Exec(ctx, `DELETE FROM eff_source_facts WHERE source = ? AND at < ?`, source, before)
	return err
}
