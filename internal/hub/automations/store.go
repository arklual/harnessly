package automations

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// Trigger says which outside events a rule takes: the source that sent it and, optionally, fields the event must carry
// with exactly these values.
type Trigger struct {
	Source string         `json:"source"`
	Filter map[string]any `json:"filter,omitempty"`
}

// Limits keep a rule from running away. Quiet hours are in Moscow time (a fixed UTC+3) as in the platform it came from.
type Limits struct {
	MaxRunsPerDay      *int `json:"maxRunsPerDay,omitempty"`
	QuietHoursStartMsk *int `json:"quietHoursStartMsk,omitempty"`
	QuietHoursEndMsk   *int `json:"quietHoursEndMsk,omitempty"`
}

const (
	defMaxRunsPerDay = 3
	defQuietStart    = 0
	defQuietEnd      = 8
)

func (l Limits) maxRuns() int {
	if l.MaxRunsPerDay != nil {
		return *l.MaxRunsPerDay
	}
	return defMaxRunsPerDay
}

// Rule is one automation.
type Rule struct {
	ID             string  `json:"id"`
	OwnerID        string  `json:"ownerId"`
	Name           string  `json:"name"`
	Trigger        Trigger `json:"trigger"`
	PromptTemplate string  `json:"promptTemplate"`
	Model          string  `json:"model"`
	Limits         Limits  `json:"limits"`
	// MachineID is the machine that runs it; empty: any connected machine the owner registered.
	MachineID string `json:"machineId,omitempty"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// Run states.
const (
	StatusQueued           = "queued"
	StatusRunning          = "running"
	StatusAwaitingApproval = "awaiting_approval"
	StatusApproved         = "approved"
	StatusRejected         = "rejected"
	StatusFailed           = "failed"
)

// Run is one event a rule took and what came of it.
type Run struct {
	ID             string          `json:"id"`
	RuleID         string          `json:"ruleId"`
	Fingerprint    string          `json:"fingerprint"`
	Status         string          `json:"status"`
	EventPayload   json.RawMessage `json:"eventPayload"`
	ConversationID string          `json:"conversationId,omitempty"`
	MachineID      string          `json:"machineId,omitempty"`
	BugIssueID     string          `json:"bugIssueId,omitempty"`
	MRURL          string          `json:"mrUrl,omitempty"`
	Summary        string          `json:"summary,omitempty"`
	Error          string          `json:"error,omitempty"`
	CreatedAt      string          `json:"createdAt"`
	DispatchedAt   string          `json:"dispatchedAt,omitempty"`
	FinishedAt     string          `json:"finishedAt,omitempty"`
}

func iso(ms int64) string { return time.UnixMilli(ms).UTC().Format(time.RFC3339) }

func isoNull(ms *int64) string {
	if ms == nil {
		return ""
	}
	return iso(*ms)
}

type rowScanner interface{ Scan(...any) error }

const ruleCols = `id, owner_id, name, trigger, prompt_template, model, limits, machine_id, enabled, created_at, updated_at`

func scanRule(sc rowScanner) (Rule, error) {
	var r Rule
	var trig, lim string
	var machine *string
	var enabled int
	var created, updated int64
	if err := sc.Scan(&r.ID, &r.OwnerID, &r.Name, &trig, &r.PromptTemplate, &r.Model, &lim, &machine, &enabled, &created, &updated); err != nil {
		return Rule{}, err
	}
	if err := json.Unmarshal([]byte(trig), &r.Trigger); err != nil {
		return Rule{}, err
	}
	if err := json.Unmarshal([]byte(lim), &r.Limits); err != nil {
		return Rule{}, err
	}
	if machine != nil {
		r.MachineID = *machine
	}
	r.Enabled, r.CreatedAt, r.UpdatedAt = enabled == 1, iso(created), iso(updated)
	return r, nil
}

const runCols = `id, rule_id, fingerprint, status, event_payload, conversation_id, machine_id, bug_issue_id, mr_url, summary, error, created_at, dispatched_at, finished_at`

func scanRun(sc rowScanner) (Run, error) {
	var r Run
	var payload string
	var conv, machine, bug, mr, sum, errText *string
	var created int64
	var disp, fin *int64
	if err := sc.Scan(&r.ID, &r.RuleID, &r.Fingerprint, &r.Status, &payload, &conv, &machine, &bug, &mr, &sum, &errText, &created, &disp, &fin); err != nil {
		return Run{}, err
	}
	r.EventPayload = json.RawMessage(payload)
	for _, p := range []struct {
		to   *string
		from *string
	}{{&r.ConversationID, conv}, {&r.MachineID, machine}, {&r.BugIssueID, bug}, {&r.MRURL, mr}, {&r.Summary, sum}, {&r.Error, errText}} {
		if p.from != nil {
			*p.to = *p.from
		}
	}
	r.CreatedAt, r.DispatchedAt, r.FinishedAt = iso(created), isoNull(disp), isoNull(fin)
	return r, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// AddRule keeps a new rule; false when the owner has a rule of that name.
func (s *Service) addRule(ctx context.Context, r Rule) (Rule, bool, error) {
	now := s.now()
	r.ID, r.CreatedAt, r.UpdatedAt = store.NewID(), now.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339)
	trig, _ := json.Marshal(r.Trigger)
	lim, _ := json.Marshal(r.Limits)
	res, err := s.db.Exec(ctx, `INSERT INTO automation_rules (`+ruleCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (owner_id, name) DO NOTHING`,
		r.ID, r.OwnerID, r.Name, string(trig), r.PromptTemplate, r.Model, string(lim), nullable(r.MachineID), b2i(r.Enabled), store.Ms(now), store.Ms(now))
	if err != nil {
		return Rule{}, false, err
	}
	n, _ := res.RowsAffected()
	return r, n > 0, nil
}

func (s *Service) ruleOf(ctx context.Context, id, owner string) (Rule, error) {
	return scanRule(s.db.QueryRow(ctx, `SELECT `+ruleCols+` FROM automation_rules WHERE id = ? AND owner_id = ?`, id, owner))
}

func (s *Service) ruleByID(ctx context.Context, id string) (Rule, error) {
	return scanRule(s.db.QueryRow(ctx, `SELECT `+ruleCols+` FROM automation_rules WHERE id = ?`, id))
}

func (s *Service) rules(ctx context.Context, owner string, enabledOnly bool) ([]Rule, error) {
	q, args := `SELECT `+ruleCols+` FROM automation_rules`, []any{}
	var where []string
	if owner != "" {
		where = append(where, `owner_id = ?`)
		args = append(args, owner)
	}
	if enabledOnly {
		where = append(where, `enabled = 1`)
	}
	for i, w := range where {
		if i == 0 {
			q += ` WHERE ` + w
		} else {
			q += ` AND ` + w
		}
	}
	rows, err := s.db.Query(ctx, q+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Service) saveRule(ctx context.Context, r Rule) error {
	trig, _ := json.Marshal(r.Trigger)
	lim, _ := json.Marshal(r.Limits)
	_, err := s.db.Exec(ctx, `UPDATE automation_rules SET name = ?, trigger = ?, prompt_template = ?, model = ?, limits = ?, machine_id = ?, enabled = ?, updated_at = ?
		WHERE id = ? AND owner_id = ?`,
		r.Name, string(trig), r.PromptTemplate, r.Model, string(lim), nullable(r.MachineID), b2i(r.Enabled), store.Ms(s.now()), r.ID, r.OwnerID)
	return err
}

func (s *Service) deleteRule(ctx context.Context, id, owner string) (bool, error) {
	var gone bool
	err := s.db.Tx(ctx, func(t store.Store) error {
		res, err := t.Q().Exec(ctx, `DELETE FROM automation_rules WHERE id = ? AND owner_id = ?`, id, owner)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if gone = n > 0; gone {
			_, err = t.Q().Exec(ctx, `DELETE FROM automation_runs WHERE rule_id = ?`, id)
		}
		return err
	})
	return gone, err
}

// QueueOutcome is what queueing an event came to.
type QueueOutcome struct {
	Queued bool   `json:"queued"`
	RunID  string `json:"runId,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// queueRun adds a queued run for a matched event unless the rule saw this fingerprint already (duplicate) or ran its
// daily cap (rate_limited); the check and the insert are one transaction.
func (s *Service) queueRun(ctx context.Context, rule Rule, fingerprint string, event json.RawMessage) (QueueOutcome, error) {
	var out QueueOutcome
	err := s.db.Tx(ctx, func(t store.Store) error {
		now := s.now()
		var n int
		if err := t.Q().QueryRow(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id = ? AND created_at > ? AND status <> 'rejected'`,
			rule.ID, store.Ms(now.Add(-24*time.Hour))).Scan(&n); err != nil {
			return err
		}
		if n >= rule.Limits.maxRuns() {
			out = QueueOutcome{Reason: "rate_limited"}
			return nil
		}
		id := store.NewID()
		res, err := t.Q().Exec(ctx, `INSERT INTO automation_runs (id, rule_id, fingerprint, status, event_payload, created_at) VALUES (?, ?, ?, 'queued', ?, ?)
			ON CONFLICT (rule_id, fingerprint) DO NOTHING`, id, rule.ID, fingerprint, string(event), store.Ms(now))
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			out = QueueOutcome{Reason: "duplicate"}
			return nil
		}
		out = QueueOutcome{Queued: true, RunID: id}
		return nil
	})
	return out, err
}

func (s *Service) runs(ctx context.Context, owner, rule string, limit int) ([]Run, error) {
	q := `SELECT ` + prefixCols("r.", runCols) + ` FROM automation_runs r JOIN automation_rules ar ON ar.id = r.rule_id WHERE ar.owner_id = ?`
	args := []any{owner}
	if rule != "" {
		q += ` AND r.rule_id = ?`
		args = append(args, rule)
	}
	rows, err := s.db.Query(ctx, q+` ORDER BY r.created_at DESC, r.id LIMIT ?`, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func prefixCols(prefix, cols string) string {
	out := ""
	for i, c := range splitCols(cols) {
		if i > 0 {
			out += ", "
		}
		out += prefix + c
	}
	return out
}

func splitCols(cols string) []string {
	var out []string
	cur := ""
	for _, ch := range cols {
		switch ch {
		case ',':
			out = append(out, cur)
			cur = ""
		case ' ':
		default:
			cur += string(ch)
		}
	}
	return append(out, cur)
}

func (s *Service) runByID(ctx context.Context, id string) (Run, error) {
	return scanRun(s.db.QueryRow(ctx, `SELECT `+runCols+` FROM automation_runs WHERE id = ?`, id))
}

// decide moves a run that awaits approval to approved or rejected; false when it does not await it (any more).
func (s *Service) decide(ctx context.Context, id, owner, status string) (Run, bool, error) {
	res, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = ? WHERE id = ? AND status = ?
		AND rule_id IN (SELECT id FROM automation_rules WHERE owner_id = ?)`, status, id, StatusAwaitingApproval, owner)
	if err != nil {
		return Run{}, false, err
	}
	n, _ := res.RowsAffected()
	run, err := s.runByID(ctx, id)
	return run, n > 0, err
}

func tokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Service) fail(ctx context.Context, id, reason string) {
	if _, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = 'failed', error = ?, finished_at = ? WHERE id = ?`, reason, store.Ms(s.now()), id); err != nil {
		s.log.Warn("automation run not marked failed", "run", id, "error", err)
	}
}

// claim takes the oldest queued run of an enabled rule that is not in its quiet hours and marks it running, with the
// hash of a fresh result token (returned once). Two replicas cannot take one run: the update only matches while queued.
func (s *Service) claim(ctx context.Context) (Run, Rule, string, error) {
	rows, err := s.db.Query(ctx, `SELECT `+prefixCols("r.", runCols)+` FROM automation_runs r JOIN automation_rules ar ON ar.id = r.rule_id
		WHERE r.status = 'queued' AND ar.enabled = 1 ORDER BY r.created_at, r.id LIMIT 20`)
	if err != nil {
		return Run{}, Rule{}, "", err
	}
	var queued []Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return Run{}, Rule{}, "", err
		}
		queued = append(queued, r)
	}
	rows.Close()
	for _, run := range queued {
		rule, err := s.ruleByID(ctx, run.RuleID)
		if err != nil {
			continue
		}
		if quietNow(rule, s.now()) {
			continue
		}
		token := newToken()
		res, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = 'running', dispatched_at = ?, result_token_hash = ? WHERE id = ? AND status = 'queued'`,
			store.Ms(s.now()), tokenHash(token), run.ID)
		if err != nil {
			return Run{}, Rule{}, "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		run.Status = StatusRunning
		return run, rule, token, nil
	}
	return Run{}, Rule{}, "", store.ErrNotFound
}

// quietNow says whether the rule may not start a run at this moment. Moscow is a fixed UTC+3.
func quietNow(r Rule, at time.Time) bool {
	start, end := defQuietStart, defQuietEnd
	if r.Limits.QuietHoursStartMsk != nil {
		start = *r.Limits.QuietHoursStartMsk
	}
	if r.Limits.QuietHoursEndMsk != nil {
		end = *r.Limits.QuietHoursEndMsk
	}
	if start == end {
		return false
	}
	h := (at.UTC().Hour() + 3) % 24
	if start < end {
		return h >= start && h < end
	}
	return h >= start || h < end
}

func (s *Service) markDispatched(ctx context.Context, id, conversation, machine string) error {
	_, err := s.db.Exec(ctx, `UPDATE automation_runs SET conversation_id = ?, machine_id = ? WHERE id = ?`, nullable(conversation), machine, id)
	return err
}

var errBadToken = errors.New("result token does not match")

// recordResult keeps what a run's conversation reported and moves the run to awaiting_approval. The token must match the
// hash stored when the run was claimed, and the run must still be running.
func (s *Service) recordResult(ctx context.Context, runID, token, summary, bug, mr string) error {
	var hash *string
	var status string
	if err := s.db.QueryRow(ctx, `SELECT result_token_hash, status FROM automation_runs WHERE id = ?`, runID).Scan(&hash, &status); err != nil {
		return err
	}
	if hash == nil || subtle.ConstantTimeCompare([]byte(tokenHash(token)), []byte(*hash)) != 1 {
		return errBadToken
	}
	if status != StatusRunning {
		return errors.New("run is " + status + ", not running")
	}
	_, err := s.db.Exec(ctx, `UPDATE automation_runs SET status = 'awaiting_approval', summary = ?, bug_issue_id = ?, mr_url = ?, finished_at = ?
		WHERE id = ? AND status = 'running'`, summary, nullable(bug), nullable(mr), store.Ms(s.now()), runID)
	return err
}
