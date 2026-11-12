package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/hub/efficiency"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

type batch struct{ id, suite, owner, state string }

// closeBatches skips the queued runs of batches past their term, closes the batches whose runs all ended, puts a marker on
// the efficiency charts for each and checks the closed smoke batches.
func (s *Scheduler) closeBatches(ctx context.Context) error {
	if err := s.expire(ctx); err != nil {
		return err
	}
	rows, err := s.db.Query(ctx, `SELECT b.id, b.suite_key, b.owner_user_id FROM eval_batches b
		WHERE b.state IN ('queued', 'running') AND EXISTS (SELECT 1 FROM eval_runs r WHERE r.batch_id = b.id)
		AND NOT EXISTS (SELECT 1 FROM eval_runs r WHERE r.batch_id = b.id AND r.state IN ('queued', 'claimed', 'preparing', 'running', 'grading'))`)
	if err != nil {
		return err
	}
	var ended []batch
	for rows.Next() {
		var b batch
		if err := rows.Scan(&b.id, &b.suite, &b.owner); err != nil {
			rows.Close()
			return err
		}
		ended = append(ended, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	now := store.Ms(s.now())
	for _, b := range ended {
		var notSkipped, notDone int
		if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE state <> 'skipped'), COUNT(*) FILTER (WHERE state <> 'done') FROM eval_runs WHERE batch_id = ?`, b.id).Scan(&notSkipped, &notDone); err != nil {
			return err
		}
		b.state = "done"
		switch {
		case notSkipped == 0:
			b.state = "skipped"
		case notDone > 0:
			b.state = "partial"
		}
		if _, err := s.db.Exec(ctx, `UPDATE eval_batches SET state = ?, finished_at = ? WHERE id = ? AND state IN ('queued', 'running')`, b.state, now, b.id); err != nil {
			return err
		}
		if b.state != "skipped" {
			if err := s.marker(ctx, b); err != nil {
				return err
			}
		}
		if isSmoke(b.suite) {
			if err := s.checkCoverage(ctx, b); err != nil {
				return err
			}
			if err := s.checkRegression(ctx, b); err != nil {
				return err
			}
		}
	}
	return nil
}

// expire skips the queued runs of batches past expires_at, with the batch's waiting note as the reason, so a slot the
// subscription never allowed does not hang as queued for days.
func (s *Scheduler) expire(ctx context.Context) error {
	now := store.Ms(s.now())
	rows, err := s.db.Query(ctx, `SELECT DISTINCT b.id, COALESCE(b.note, '') FROM eval_batches b JOIN eval_runs r ON r.batch_id = b.id
		WHERE b.state IN ('queued', 'running') AND b.expires_at < ? AND r.state = 'queued'`, now)
	if err != nil {
		return err
	}
	type lost struct{ id, note string }
	var batches []lost
	for rows.Next() {
		var l lost
		if err := rows.Scan(&l.id, &l.note); err != nil {
			rows.Close()
			return err
		}
		batches = append(batches, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, l := range batches {
		why := l.note
		if why == "" {
			why = "машина не взяла прогон"
		}
		if _, err := s.db.Exec(ctx, `UPDATE eval_runs SET state = 'skipped', error = ?, finished_at = ? WHERE batch_id = ? AND state = 'queued'`,
			"не дождались: "+why+" (срок ожидания вышел)", now, l.id); err != nil {
			return err
		}
		s.log.Warn("eval: queued runs expired", "batch", l.id, "note", l.note)
	}
	return nil
}

// marker draws the batch on the efficiency charts: «suite: passed of done». A batch is put once.
func (s *Scheduler) marker(ctx context.Context, b batch) error {
	var passed, done int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE passed), COUNT(*) FILTER (WHERE state = 'done') FROM eval_runs WHERE batch_id = ?`, b.id).Scan(&passed, &done); err != nil {
		return err
	}
	data, _ := json.Marshal(map[string]string{"kind": "eval", "title": fmt.Sprintf("%s: %d из %d", b.suite, passed, done), "detail": "Фоновый прогон golden-задач"})
	now := s.now()
	return s.db.S().UpsertEffFacts(ctx, []store.EffFact{{Source: efficiency.SourceMarker, Key: "eval:" + b.id, At: now.UnixMilli(), Data: string(data)}}, now.UnixMilli())
}

func (s *Scheduler) emailOf(ctx context.Context, userID string) string {
	var email string
	_ = s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = ?`, userID).Scan(&email)
	return strings.ToLower(email)
}

// checkCoverage: a smoke batch that closed with results for fewer than half of its runs is a hole in the series, not a
// verdict on the harness; the alert names why (the commonest run errors) so it is not mistaken for «all green».
func (s *Scheduler) checkCoverage(ctx context.Context, b batch) error {
	var total, done int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE state = 'done') FROM eval_runs WHERE batch_id = ?`, b.id).Scan(&total, &done); err != nil {
		return err
	}
	if total == 0 || done*2 >= total {
		return nil
	}
	rows, err := s.db.Query(ctx, `SELECT COALESCE(error, ''), COUNT(*) FROM eval_runs WHERE batch_id = ? AND state <> 'done' GROUP BY error ORDER BY COUNT(*) DESC, error LIMIT 3`, b.id)
	if err != nil {
		return err
	}
	defer rows.Close()
	diff := []string{}
	for rows.Next() {
		var e string
		var n int
		if err := rows.Scan(&e, &n); err != nil {
			return err
		}
		if e == "" {
			e = "без причины"
		}
		diff = append(diff, fmt.Sprintf("%d × %s", n, e))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return s.alert(ctx, store.EffRegressionAlert{ID: "cov-" + b.id, BatchID: b.id, Metric: fmt.Sprintf("smoke: результат есть у %d из %d прогонов", done, total),
		Baseline: float64(total), Observed: float64(done)}, diff, b.owner)
}

type score struct {
	id          string
	passed      int
	total       int
	usd         *float64
	fingerprint string
	finishedAt  int64
}

// smokeScores are the newest closed smoke batches, newest first.
func (s *Scheduler) smokeScores(ctx context.Context, limit int) ([]score, error) {
	rows, err := s.db.Query(ctx, `SELECT b.id, COUNT(*) FILTER (WHERE r.passed), COUNT(*) FILTER (WHERE r.state = 'done'),
			AVG(r.cost_usd) FILTER (WHERE r.state = 'done'), COALESCE(MAX(r.fingerprint), ''), COALESCE(b.finished_at, 0)
		FROM eval_batches b JOIN eval_runs r ON r.batch_id = b.id
		WHERE b.suite_key IN ('smoke-8', 'aa-8') AND b.state IN ('done', 'partial')
		GROUP BY b.id, b.finished_at ORDER BY b.finished_at DESC, b.id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []score
	for rows.Next() {
		var c score
		if err := rows.Scan(&c.id, &c.passed, &c.total, &c.usd, &c.fingerprint, &c.finishedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func short(fp string) string {
	if len(fp) > 8 {
		return fp[:8]
	}
	return fp
}

// checkRegression: a smoke batch below the band of the previous seven, or a fifth dearer per task than their median, opens
// an alert for the schedule's owner with what changed in between (the harness's fingerprint, the markers).
func (s *Scheduler) checkRegression(ctx context.Context, b batch) error {
	scores, err := s.smokeScores(ctx, 8)
	if err != nil {
		return err
	}
	var cur *score
	var prev []score
	for i := range scores {
		switch {
		case scores[i].id == b.id:
			cur = &scores[i]
		case scores[i].total > 0 && len(prev) < 7:
			prev = append(prev, scores[i])
		}
	}
	if cur == nil || cur.total == 0 || len(prev) < 3 {
		return nil
	}
	rates := make([]float64, len(prev))
	for i, p := range prev {
		rates[i] = float64(p.passed) / float64(p.total)
	}
	sort.Float64s(rates)
	low := rates[0]
	rate := float64(cur.passed) / float64(cur.total)
	var usds []float64
	for _, p := range prev {
		if p.usd != nil {
			usds = append(usds, *p.usd)
		}
	}
	sort.Float64s(usds)
	var median *float64
	if len(usds) > 0 {
		median = &usds[len(usds)/2]
	}
	dearer := median != nil && cur.usd != nil && *cur.usd > *median*1.2
	if rate >= low && !dearer {
		return nil
	}
	last := prev[0]
	diff := []string{}
	if last.fingerprint != cur.fingerprint {
		diff = append(diff, fmt.Sprintf("Отпечаток харнеса: %s → %s", short(last.fingerprint), short(cur.fingerprint)))
	}
	if marks, err := s.db.S().EffFactsBetween(ctx, efficiency.SourceMarker, last.finishedAt+1, store.Ms(s.now())+1); err == nil {
		for _, f := range marks {
			var d struct {
				Title string `json:"title"`
			}
			if json.Unmarshal([]byte(f.Data), &d) == nil && d.Title != "" {
				diff = append(diff, d.Title)
			}
		}
	}
	a := store.EffRegressionAlert{ID: "reg-" + b.id, BatchID: b.id, FingerprintFrom: last.fingerprint, FingerprintTo: cur.fingerprint,
		Band: int(math.Round(rates[len(rates)-1] - low))}
	if rate < low {
		a.Metric = fmt.Sprintf("smoke: %d из %d при нижней границе %d за %d запусков", cur.passed, cur.total, int(math.Round(low*float64(cur.total))), len(prev))
		a.Baseline, a.Observed = low, rate
	} else {
		a.Metric = fmt.Sprintf("$ на задачу %.2f против медианы %.2f (+%d%%)", *cur.usd, *median, int(math.Round((*cur.usd / *median - 1)*100)))
		a.Baseline, a.Observed = *median, *cur.usd
	}
	return s.alert(ctx, a, diff, b.owner)
}

// alert stores one alert addressed to the owner's e-mail; one batch gives one alert of a kind, whatever the passes do.
func (s *Scheduler) alert(ctx context.Context, a store.EffRegressionAlert, diff []string, ownerID string) error {
	d, _ := json.Marshal(diff)
	a.Diff, a.Addressee, a.State, a.CreatedAt = string(d), s.emailOf(ctx, ownerID), "open", store.Ms(s.now())
	if err := s.db.S().InsertEffRegressionAlerts(ctx, []store.EffRegressionAlert{a}); err != nil {
		return err
	}
	s.log.Warn("eval: alert", "batch", a.BatchID, "metric", a.Metric)
	return nil
}
