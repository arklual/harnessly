package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// DispatchEvery is how often the dispatcher looks for a queued run.
const DispatchEvery = 15 * time.Second

const (
	startOp  = "conversations.start"
	startFor = 60 * time.Second
	// startLease is how long a taken run may stay without its conversation started before it is taken to be
	// abandoned (the hub that took it stopped) and goes back to the queue: the start's own time and a margin.
	startLease = startFor + 30*time.Second
	// maxAttempts is how many times a run is taken before it is given up as failed.
	maxAttempts = 3
	// queuedFor is how long a run waits for its machine before it is given up.
	queuedFor = 24 * time.Hour
)

// Run starts the queued runs, one at a time, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	t := time.NewTicker(DispatchEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.Dispatch(ctx)
	}
}

// promptEvent drops `raw` (a sender's whole webhook body repeats what the event already carries).
func promptEvent(event json.RawMessage) string {
	var m map[string]any
	if json.Unmarshal(event, &m) != nil {
		return string(event)
	}
	delete(m, "raw")
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// buildPrompt is the task the conversation gets. The token and the agent's port reach it as environment variables, never
// as text: the prompt ends up in the transcript.
func (s *Service) buildPrompt(rule Rule, run Run) string {
	port, tokenEnv := s.cfg.AutomationsPortEnv, s.cfg.AutomationsTokenEnv
	rendered := strings.Replace(rule.PromptTemplate, "{{event}}", promptEvent(run.EventPayload), 1)
	return fmt.Sprintf(`Это автоматизация «%s», запущена без участия человека по событию из %s.
Никогда не мерджи и не выкатывай сам: решение принимает человек.

%s

Когда закончишь (успешно или нет), отправь итог одним запросом. Токен лежит в переменной окружения $%s — не печатай и не логируй его значение, подставь саму переменную:

curl -sS -X POST "http://127.0.0.1:$%s/automations/report" \
  -H 'content-type: application/json' \
  -d "{\"runId\":\"%s\",\"resultToken\":\"$%s\",\"summary\":\"<короткая сводка>\",\"bugIssueId\":\"<ключ карточки или пусто>\",\"mrUrl\":\"<ссылка на MR или пусто>\"}"
`, rule.Name, rule.Trigger.Source, rendered, tokenEnv, port, run.ID, tokenEnv)
}

// errNoMachine says a rule has no machine to run on just now.
var errNoMachine = errors.New("no machine")

// machineFor is the machine that runs a rule: the one it names, else the first connected machine its owner registered.
func (s *Service) machineFor(ctx context.Context, rule Rule) (string, error) {
	if rule.MachineID != "" {
		if s.links.Get(rule.MachineID) == nil {
			return "", fmt.Errorf("machine %s is not connected: %w", rule.MachineID, errNoMachine)
		}
		return rule.MachineID, nil
	}
	rows, err := s.db.Query(ctx, `SELECT id FROM machines WHERE created_by = ? AND revoked_at IS NULL AND disabled_at IS NULL ORDER BY created_at, id`, rule.OwnerID)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		if s.links.Get(id) != nil {
			return id, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no connected machine of the owner: %w", errNoMachine)
}

// Dispatch is one pass: it puts back the runs whose start was abandoned, then takes one queued run whose machine is
// connected and starts its conversation. It returns whether a run was started.
func (s *Service) Dispatch(ctx context.Context) bool {
	s.reap(ctx)
	// A run waits for its machine in `queued`: the machine is looked for first, and the run is taken only when there
	// is somewhere to run it.
	run, rule, token, machine, err := s.claim(ctx, s.machineFor)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			s.log.Error("automation run not claimed", "error", err)
		}
		return false
	}
	fctx := context.WithoutCancel(ctx)
	raw, err := s.links.Do(ctx, machine, startOp, map[string]any{
		"workspaceId": nil, "prompt": s.buildPrompt(rule, run), "model": rule.Model, "cwd": nil,
		"runId": run.ID, "resultToken": token, "env": map[string]string{s.cfg.AutomationsTokenEnv: token},
	}, startFor)
	if err != nil {
		if errors.Is(err, httpx.ErrMachineOffline) {
			// The machine went away between the look and the start, so it was never asked: the run waits for the
			// next pass.
			s.release(fctx, run.ID, err.Error())
		} else {
			// The machine may have started the conversation and not said so in time: a second start would do the
			// work twice, so the run is failed and not tried again.
			s.failStart(fctx, run.ID, err.Error())
		}
		s.log.Warn("automation not dispatched", "rule", rule.ID, "run", run.ID, "machine", machine, "error", err)
		return false
	}
	var started struct {
		ConversationID string `json:"conversationId"`
	}
	_ = json.Unmarshal(raw, &started)
	if err := s.markDispatched(fctx, run.ID, started.ConversationID, machine); err != nil {
		s.log.Warn("automation dispatched but not recorded", "run", run.ID, "error", err)
	}
	s.log.Info("automation dispatched", "rule", rule.ID, "run", run.ID, "machine", machine, "conversation", started.ConversationID)
	return true
}
