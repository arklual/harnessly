package automations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// DispatchEvery is how often the dispatcher looks for a queued run.
const DispatchEvery = 15 * time.Second

const (
	tokenEnv = "AUTOMATION_RESULT_TOKEN"
	startOp  = "conversations.start"
	startFor = 60 * time.Second
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
	port := s.cfg.AutomationsPortEnv
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

// machineFor is the machine that runs a rule: the one it names, else the first connected machine its owner registered.
func (s *Service) machineFor(ctx context.Context, rule Rule) (string, error) {
	if rule.MachineID != "" {
		if s.links.Get(rule.MachineID) == nil {
			return "", fmt.Errorf("machine %s is not connected", rule.MachineID)
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
	return "", errors.New("no connected machine of the owner")
}

// Dispatch is one pass: it claims one queued run and starts its conversation. It returns whether a run was started.
func (s *Service) Dispatch(ctx context.Context) bool {
	// A run waits for a machine in `queued`; look first, claim only when there is somewhere to run it.
	run, rule, token, err := s.claim(ctx)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) && ctx.Err() == nil {
			s.log.Error("automation run not claimed", "error", err)
		}
		return false
	}
	fctx := context.WithoutCancel(ctx)
	machine, err := s.machineFor(fctx, rule)
	if err != nil {
		s.fail(fctx, run.ID, err.Error())
		s.log.Warn("automation not dispatched", "rule", rule.ID, "run", run.ID, "error", err)
		return false
	}
	raw, err := s.links.Do(ctx, machine, startOp, map[string]any{
		"workspaceId": nil, "prompt": s.buildPrompt(rule, run), "model": rule.Model, "cwd": nil,
		"runId": run.ID, "resultToken": token, "env": map[string]string{tokenEnv: token},
	}, startFor)
	if err != nil {
		s.fail(fctx, run.ID, err.Error())
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
