package automations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "time/tzdata" // a schedule's time zone must load on an image without the zone files

	"github.com/ly-solutions/harnessly/internal/hub/eventbus"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The triggers that are not an outside event: a schedule fires at the minutes of a cron expression, a wait
// fires when an event that starts it is not followed by an event that stops it within a time. Both queue a run as an
// event does, so the daily cap, the quiet hours and the dispatcher are the same.

// SwitchTriggers is the rollout switch of the schedule and wait triggers: off for a person, their rule of these kinds
// cannot be saved and does not fire.
const SwitchTriggers = "automation-triggers"

// Reserved sources: an outside event may not carry them, so that it cannot pass for a schedule or a wait.
const (
	SourceSchedule = "schedule"
	SourceWait     = "wait"
	// SourceBus is the rule the catalog of triggers keeps for a trigger in the mode «agent» (AgentForEvent): no event
	// from outside matches it, the bus queues its runs.
	SourceBus = "bus"
)

// Gate says whether a rollout switch is on for a person (by e-mail) on a machine.
type Gate interface {
	On(ctx context.Context, feature, person, machine string) bool
}

// Schedule fires a rule at the minutes of a five-field cron expression read in a time zone (Europe/Moscow by default).
type Schedule struct {
	Cron string `json:"cron"`
	TZ   string `json:"tz,omitempty"`
}

// Match is one kind of outside event: its source and, optionally, fields it must carry with exactly these values.
type Match struct {
	Source string         `json:"source"`
	Filter map[string]any `json:"filter,omitempty"`
}

func (m *Match) matches(source string, event map[string]any) bool {
	return m != nil && m.Source == source && matchesFilter(m.Filter, event)
}

// Wait fires a rule when an event of Start is not followed by an event of Stop with the same key within After. Key names
// a field of the event (empty: one timer for the whole rule). A second Start while the timer runs does not move it: the
// wait counts from the first unanswered event. When an event is both a Stop and a Start (a watchdog: "this sender went
// quiet"), it re-arms the timer.
type Wait struct {
	Start Match  `json:"start"`
	Stop  *Match `json:"stop,omitempty"`
	Key   string `json:"key,omitempty"`
	After string `json:"after"`
}

const (
	defScheduleTZ = "Europe/Moscow"
	// catchUp is how far back a missed slot is still fired: a hub that was restarting at nine sends the nine o'clock
	// digest at five past, not at noon.
	catchUp     = 15 * time.Minute
	minWait     = time.Minute
	maxWait     = 7 * 24 * time.Hour
	firePerPass = 100
)

func (sc *Schedule) location() (*time.Location, error) {
	tz := sc.TZ
	if tz == "" {
		tz = defScheduleTZ
	}
	return time.LoadLocation(tz)
}

func (w *Wait) after() (time.Duration, error) {
	return time.ParseDuration(strings.TrimSpace(w.After))
}

// checkTrigger fills the reserved source of a schedule or a wait and checks the trigger.
func checkTrigger(t *Trigger) error {
	t.Source = strings.TrimSpace(t.Source)
	switch {
	case t.Schedule != nil && t.Wait != nil:
		return invalid("У правила одно из двух: расписание (trigger.schedule) или ожидание (trigger.wait)")
	case t.Schedule != nil:
		if t.Source != "" && t.Source != SourceSchedule {
			return invalid(`У правила с расписанием источник "schedule"`)
		}
		t.Source, t.Filter = SourceSchedule, nil
		if _, err := parseCron(t.Schedule.Cron); err != nil {
			return invalid("Расписание: " + err.Error())
		}
		if _, err := t.Schedule.location(); err != nil {
			return invalid("Часовой пояс расписания: " + t.Schedule.TZ)
		}
	case t.Wait != nil:
		if t.Source != "" && t.Source != SourceWait {
			return invalid(`У правила с ожиданием источник "wait"`)
		}
		t.Source, t.Filter = SourceWait, nil
		w := t.Wait
		w.Start.Source = strings.TrimSpace(w.Start.Source)
		if w.Stop != nil {
			w.Stop.Source = strings.TrimSpace(w.Stop.Source)
		}
		switch {
		case w.Start.Source == "" || reserved(w.Start.Source):
			return invalid("Ожидание: укажите источник события, с которого оно начинается (trigger.wait.start.source)")
		case w.Stop != nil && (w.Stop.Source == "" || reserved(w.Stop.Source)):
			return invalid("Ожидание: у события, которое его снимает, нужен источник (trigger.wait.stop.source)")
		case len(w.Key) > 100:
			return invalid("Ожидание: имя поля ключа длиннее 100 символов")
		}
		d, err := w.after()
		if err != nil || d < minWait || d > maxWait {
			return invalid("Ожидание: after — длительность от 1m до 168h, например 20m")
		}
	case t.Source == "":
		return invalid("Укажите источник события (trigger.source)")
	case reserved(t.Source):
		return invalid(`Источники "schedule" и "wait" задаются через trigger.schedule и trigger.wait`)
	}
	return nil
}

func reserved(source string) bool {
	return source == SourceSchedule || source == SourceWait || source == SourceBus
}

// newKind says whether the trigger is one of the kinds behind SwitchTriggers.
func (t Trigger) newKind() bool { return t.Schedule != nil || t.Wait != nil }

// triggersOn says whether the switch of the new triggers is on for the person; without a gate (tests) it is on.
func (s *Service) triggersOn(ctx context.Context, ownerID string) bool {
	if s.gate == nil {
		return true
	}
	var email string
	if err := s.db.QueryRow(ctx, `SELECT email FROM users WHERE id = ?`, ownerID).Scan(&email); err != nil || email == "" {
		return false
	}
	return s.gate.On(ctx, SwitchTriggers, strings.ToLower(email), "")
}

// Fire is one pass of the triggers that come from time: the schedules whose slot has come and the waits that ran out.
// The leader runs it before each dispatch.
func (s *Service) Fire(ctx context.Context) {
	rules, err := s.rules(ctx, "", true)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("automation triggers not read", "error", err)
		}
		return
	}
	on := map[string]bool{}
	allowed := func(r Rule) bool {
		v, ok := on[r.OwnerID]
		if !ok {
			v = s.triggersOn(ctx, r.OwnerID)
			on[r.OwnerID] = v
		}
		return v
	}
	byID := map[string]Rule{}
	for _, r := range rules {
		byID[r.ID] = r
		if r.Trigger.Schedule != nil && allowed(r) {
			s.fireSchedule(ctx, r)
		}
	}
	s.fireTimers(ctx, byID, allowed)
	s.sweepRoutines(ctx)
}

func (s *Service) fireSchedule(ctx context.Context, r Rule) {
	spec, err := parseCron(r.Trigger.Schedule.Cron)
	if err != nil {
		return
	}
	loc, err := r.Trigger.Schedule.location()
	if err != nil {
		return
	}
	// A slot before the rule was last saved is not the rule's: a rule switched on at 9:03 does not fire for 9:00.
	since, _ := time.Parse(time.RFC3339, r.UpdatedAt)
	slot, ok := spec.lastSlot(s.now(), since, catchUp, loc)
	if !ok {
		return
	}
	event, _ := json.Marshal(map[string]any{"schedule": map[string]any{
		"cron": r.Trigger.Schedule.Cron, "tz": loc.String(), "slot": slot.Format(time.RFC3339),
	}})
	s.journal(ctx, eventbus.Event{Owner: r.OwnerID, Type: "schedule.fired", Source: "harnessly", Ext: "schedule:" + r.ID + ":" + slot.UTC().Format(time.RFC3339),
		At: slot, Facts: map[string]string{"rule": r.ID}, Via: []string{eventbus.ViaSchedule}})
	out, err := s.queueRun(ctx, r, "schedule:"+slot.UTC().Format(time.RFC3339), event)
	if err != nil {
		s.log.Warn("automation schedule not queued", "rule", r.ID, "error", err)
		return
	}
	if out.Queued {
		s.log.Info("automation schedule fired", "rule", r.ID, "run", out.RunID, "slot", slot)
	}
}

// waitKey is the timer key an event gives a wait: the value of its key field, or "*" when the rule has one timer.
// false when the event has no such field.
func waitKey(w *Wait, event map[string]any) (string, bool) {
	if w.Key == "" {
		return "*", true
	}
	v, ok := event[w.Key]
	if !ok || v == nil {
		return "", false
	}
	if str, ok := v.(string); ok {
		return str, str != ""
	}
	b, _ := json.Marshal(v)
	return string(b), true
}

// WaitOutcome is what an outside event did to the timer of one wait rule.
type WaitOutcome struct {
	RuleID  string `json:"ruleId"`
	Key     string `json:"key"`
	Cleared bool   `json:"cleared,omitempty"`
	Armed   bool   `json:"armed,omitempty"`
	DueAt   string `json:"dueAt,omitempty"`
}

// onWait applies an outside event to a wait rule: a stop removes the timer of the event's key, a start arms it unless
// it already runs. ok is false when the event is neither.
func (s *Service) onWait(ctx context.Context, r Rule, source string, event map[string]any, raw json.RawMessage) (WaitOutcome, bool, error) {
	w := r.Trigger.Wait
	stop, start := w.Stop.matches(source, event), w.Start.matches(source, event)
	if !stop && !start {
		return WaitOutcome{}, false, nil
	}
	key, ok := waitKey(w, event)
	if !ok {
		return WaitOutcome{}, false, nil
	}
	if len(key) > 200 {
		sum := sha256.Sum256([]byte(key))
		key = "sha256:" + hex.EncodeToString(sum[:])
	}
	out := WaitOutcome{RuleID: r.ID, Key: key}
	if stop {
		res, err := s.db.Exec(ctx, `DELETE FROM automation_timers WHERE rule_id = ? AND timer_key = ?`, r.ID, key)
		if err != nil {
			return out, true, err
		}
		n, _ := res.RowsAffected()
		out.Cleared = n > 0
	}
	if start {
		d, err := w.after()
		if err != nil {
			return out, true, nil
		}
		now := s.now()
		res, err := s.db.Exec(ctx, `INSERT INTO automation_timers (rule_id, timer_key, started_at, due_at, event_payload) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (rule_id, timer_key) DO NOTHING`, r.ID, key, store.Ms(now), store.Ms(now.Add(d)), string(raw))
		if err != nil {
			return out, true, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			out.Armed, out.DueAt = true, now.Add(d).UTC().Format(time.RFC3339)
		}
	}
	return out, true, nil
}

type timer struct {
	ruleID, key string
	startedAt   int64
	payload     string
}

// fireTimers queues a run for each timer that came due and removes it. A timer of a rule that is gone, switched off or
// no longer a wait is removed without a run.
func (s *Service) fireTimers(ctx context.Context, rules map[string]Rule, allowed func(Rule) bool) {
	rows, err := s.db.Query(ctx, `SELECT rule_id, timer_key, started_at, event_payload FROM automation_timers WHERE due_at <= ? ORDER BY due_at LIMIT ?`,
		store.Ms(s.now()), firePerPass)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Warn("automation timers not read", "error", err)
		}
		return
	}
	var due []timer
	for rows.Next() {
		var t timer
		if err := rows.Scan(&t.ruleID, &t.key, &t.startedAt, &t.payload); err != nil {
			rows.Close()
			return
		}
		due = append(due, t)
	}
	rows.Close()
	for _, t := range due {
		r, ok := rules[t.ruleID]
		if ok && r.Trigger.Wait != nil && allowed(r) {
			event, _ := json.Marshal(map[string]any{
				"wait":  map[string]any{"key": t.key, "after": r.Trigger.Wait.After, "startedAt": iso(t.startedAt)},
				"event": json.RawMessage(t.payload),
			})
			fp := fmt.Sprintf("wait:%s:%d", t.key, t.startedAt)
			s.journal(ctx, eventbus.Event{Owner: r.OwnerID, Type: "wait.expired", Source: "harnessly", Ext: r.ID + ":" + fp,
				Facts: map[string]string{"rule": r.ID}, Via: []string{eventbus.ViaSchedule}})
			out, err := s.queueRun(ctx, r, fp, event)
			if err != nil {
				s.log.Warn("automation wait not queued", "rule", r.ID, "error", err)
				continue // the timer stays and is tried on the next pass
			}
			s.log.Info("automation wait fired", "rule", r.ID, "queued", out.Queued, "run", out.RunID, "reason", out.Reason)
		}
		// Only this arming of the timer goes: one armed again since then stays.
		if _, err := s.db.Exec(ctx, `DELETE FROM automation_timers WHERE rule_id = ? AND timer_key = ? AND started_at = ?`, t.ruleID, t.key, t.startedAt); err != nil {
			s.log.Warn("automation timer not removed", "rule", t.ruleID, "error", err)
		}
	}
}
