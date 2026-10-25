package wirefake

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// ControlPrefix is where a test steers the fake: POST <prefix><action> with a JSON body.
//
//	reset                                    the fixtures again, a new epoch, every socket closed
//	restart                                  a new epoch with the state kept, as a restart of the runtime
//	drop                                     every socket cut without a close frame, as a lost network
//	delay        {op, ms}                    the next command op waits ms before it runs (ms > cmdBudgetMs: timeout)
//	turn.finish  {conversationId}            the live turn ends with an answer
//	turn.fail    {conversationId}            the live turn fails on a rate limit
//	ask          {conversationId, kind}      the agent asks: question, permission or plan
//	agent        {provider, status}          an agent CLI becomes ready, missing or login-required
//	apps         {app, state}                an app of the Apps screen is read (ready), has no token (off) or does not answer (failed)
const ControlPrefix = "/api/v2/fake/"

// Control serves ControlPrefix for a fake whose machine is m.
func (s *Service) Control(m *Machine) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, action, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/agents"), ControlPrefix)
		var body struct {
			Op             wire.Op                 `json:"op"`
			Ms             int64                   `json:"ms"`
			ConversationID string                  `json:"conversationId"`
			Kind           wire.GateKind           `json:"kind"`
			Provider       wire.Provider           `json:"provider"`
			Status         wire.AgentInstallStatus `json:"status"`
			App            wire.AppID              `json:"app"`
			State          wire.AppState           `json:"state"`
		}
		if r.ContentLength != 0 {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if err := s.control(m, action, body.Op, body.Ms, body.ConversationID, body.Kind, body.Provider, body.Status, body.App, body.State); err != "" {
			http.Error(w, err, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
}

func (s *Service) control(m *Machine, action string, op wire.Op, ms int64, id string, kind wire.GateKind, p wire.Provider, st wire.AgentInstallStatus, app wire.AppID, state wire.AppState) string {
	switch action {
	case "reset":
		s.mu.Lock()
		s.gen++
		s.delays = map[wire.Op]time.Duration{}
		err := s.load()
		s.mu.Unlock()
		if err != nil {
			return err.Error()
		}
		m.tool.reset()
		m.apps.reset()
		m.settings.reset()
		s.opt.Hub.Reset()
	case "restart":
		s.opt.Hub.Reset()
	case "drop":
		s.opt.Hub.Drop()
	case "agent":
		if !slices.Contains(wire.Providers, p) || !slices.Contains(wire.AgentInstallStatuses, st) {
			return "provider or status unknown"
		}
		m.SetAgent(p, st)
	case "apps":
		if !slices.Contains(wire.AppIDs, app) || !slices.Contains(wire.AppStates, state) {
			return "app or state unknown"
		}
		m.apps.set(app, state)
	case "delay":
		if !slices.Contains(wire.Ops, op) || ms < 0 {
			return "op unknown"
		}
		s.mu.Lock()
		s.delays[op] = time.Duration(ms) * time.Millisecond
		s.mu.Unlock()
	case "turn.finish", "turn.fail", "ask":
		s.mu.Lock()
		defer s.mu.Unlock()
		c, err := s.get(id)
		if err != nil {
			return "conversation not found"
		}
		switch {
		case action == "ask" && slices.Contains(wire.GateKinds, kind):
			s.ask(c, kind)
		case action == "ask":
			return "kind unknown"
		case c.live() == nil:
			return "no live turn"
		case action == "turn.finish":
			s.closeGates(c, wire.ClosedAgentMovedOn)
			s.answer(c, c.live().ID)
		default:
			detail := "429 rate_limit_error"
			s.finish(c, wire.TurnFailed, &wire.TurnFailure{Code: wire.TurnFailedRateLimit, Detail: &detail})
		}
	default:
		return "action unknown"
	}
	return ""
}
