package automations

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/eventbus"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// The automations write every event they take to the journal of the bus (docs/hub/event-bus.md) before matching it to
// the rules, so the journal shows what came even when no rule took it, and the bus's subscribers see it. The rules
// still match synchronously here (the /hook answers which runs it queued); a journal that refuses an event does not
// stop the rules.

// journal appends an event to the bus, when there is one.
func (s *Service) journal(ctx context.Context, e eventbus.Event) {
	if s.bus == nil {
		return
	}
	if e.Space != "" && e.Org == "" {
		// The organization of the space: the level an organization's admin reads the journal at.
		if sp, err := s.db.S().SpaceByID(ctx, e.Space); err == nil {
			e.Org = sp.OrgID
		}
	}
	if _, _, err := s.bus.Append(ctx, e); err != nil {
		level := s.log.Warn
		if errors.Is(err, eventbus.ErrOff) {
			level = s.log.Debug // its owner switched the kind off
		} else if errors.Is(err, eventbus.ErrInvalid) {
			level = s.log.Info // a sender's malformed event: the rules may still take it
		}
		level("event not journaled", "source", e.Source, "type", e.Type, "error", err)
	}
}

// hookBusEvent is an event of the deployment's /hook: whatever the sender put, it is a webhook of its source.
func hookBusEvent(owner, source, fingerprint string, ev map[string]any) eventbus.Event {
	e := eventbus.Event{Owner: owner, Source: source, Fingerprint: fingerprint, Via: []string{eventbus.ViaWebhook}}
	e.Type = str(ev, "type")
	if e.Type == "" {
		e.Type = "webhook.received"
	}
	e.Ext, e.URL, e.Actor, e.Thread = str(ev, "id"), str(ev, "url"), str(ev, "actor"), str(ev, "thread")
	e.At = when(ev)
	return e
}

// machineBusEvent is a connector's event of a machine: the envelope of connectors.Envelope.HubEvent, the owner the
// machine's, its text already left on the machine.
func machineBusEvent(m store.Machine, kind, fingerprint string, ev map[string]any) eventbus.Event {
	e := eventbus.Event{Owner: m.CreatedBy, Source: str(ev, "connector"), Type: str(ev, "type"), Connection: str(ev, "connection"),
		Ext: str(ev, "id"), Fingerprint: fingerprint, Actor: str(ev, "actor"), Thread: str(ev, "thread"), Ref: str(ev, "ref"),
		Machine: m.ID, Space: m.SpaceID, Via: []string{eventbus.ViaConnector}}
	if e.Source == "" {
		e.Source = kind
	}
	e.Mine, _ = ev["mine"].(bool)
	e.At = when(ev)
	if f, ok := ev["facts"].(map[string]any); ok {
		e.Facts = map[string]string{}
		for k, v := range f {
			e.Facts[k] = fmt.Sprint(v)
		}
	}
	return e
}

func str(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

func when(m map[string]any) time.Time {
	t, _ := time.Parse(time.RFC3339, str(m, "at"))
	return t
}

// agentPrompt is what the agent of a trigger in the mode «agent» is told; the event follows it.
const agentPrompt = `Пришло событие, которое ты по настройке триггера берёшь сам. Прочитай его целиком командой из поля ref
(или по ссылке url), пойми, чего от человека ждут, и доведи дело до конца по своим правилам самостоятельности: ничего не
отправляй и не сливай без «ок» человека, черновик и план — можно. Если по событию делать нечего — так и отчитайся.`

// AgentForEvent queues a run for an event whose trigger is in the mode «agent»: one rule per trigger of the
// owner, kept by the catalog (source "bus", so no outside event matches it), the run's fingerprint the event's id so a
// delivery twice queues it once.
func (s *Service) AgentForEvent(ctx context.Context, e eventbus.Event) error {
	if e.Owner == "" {
		return nil
	}
	name := "Триггер · " + e.Source + " · " + e.Type
	if e.Connection != "" {
		name += " · " + e.Connection
	}
	rule, err := scanRule(s.db.QueryRow(ctx, `SELECT `+ruleCols+` FROM automation_rules WHERE owner_id = ? AND name = ?`, e.Owner, name))
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		rule, _, err = s.addRule(ctx, Rule{OwnerID: e.Owner, Name: name, Trigger: Trigger{Source: SourceBus, Filter: map[string]any{"type": e.Type}},
			PromptTemplate: agentPrompt, Model: "sonnet", Enabled: true})
		if err != nil {
			return err
		}
	}
	if !rule.Enabled {
		return nil // the person switched the rule off by hand
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	out, err := s.queueRun(ctx, rule, "bus:"+e.ID, raw)
	if err != nil {
		return err
	}
	s.log.Info("trigger in the mode agent queued a run", "rule", rule.ID, "queued", out.Queued, "reason", out.Reason)
	return nil
}
