package proxy

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/safego"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What each machine last said of itself, kept in the hub (machine_cards): its runtime and agents with their
// models, and what is left of its agents' subscriptions. The fleet's list carries them, so the start page draws the
// model pill and the limits chip at once, whatever the machines do now; a live machine's own word only refines them.
//
//   - The agents: the hub asks a machine that is on for GET /api/v2/machines (the runtime's own record of itself, with
//     the page's caps) when a person lists the fleet and it last asked longer than cardFresh ago. A machine that is on
//     and does not answer within cardWait hangs (hangs): it is listed so, and the page starts its chats elsewhere.
//   - The subscriptions: the platform's …/machines, which the page asks of a machine every minute, is kept as it
//     passes through the workspace (keepUsage).

const (
	cardFresh = time.Minute
	cardWait  = 2 * time.Second
	maxCard   = 512 << 10
)

// cardCaps: the caps of the page the hub serves, so the agents come with their profiles.
var cardCaps = strings.Join([]string{string(wire.CapDelegation), string(wire.CapWorkflows), string(wire.CapProfiles), string(wire.CapMsgStatus)}, ",")

// platformMachines is the platform's list of machines with their subscriptions' limits (web: loadMachineLimits).
var platformMachines = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+/machines$`)

// cardAsk is when the hub last asked a machine for its card, and whether it answered.
type cardAsk struct {
	at      time.Time
	pending bool
	missed  bool
}

// machineCard is a machine's row of machine_cards; a part it never gave is nil.
type machineCard struct {
	runtime json.RawMessage
	agents  json.RawMessage
	usage   json.RawMessage
}

// features of the runtime the machine last described; nil when it never did.
func (c machineCard) features() []wire.Feature {
	if c.runtime == nil {
		return nil
	}
	var rt wire.RuntimeInfo
	if json.Unmarshal(c.runtime, &rt) != nil {
		return nil
	}
	return rt.Features
}

// cards reads the kept cards of the machines; a machine without one is not in the map.
func (s *Service) cards(ctx context.Context, ids []string) (map[string]machineCard, error) {
	out := map[string]machineCard{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.Query(ctx, `SELECT machine_id, runtime, agents, usage FROM machine_cards WHERE machine_id IN (?`+
		strings.Repeat(", ?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var runtime, agents, usage sql.NullString
		if err := rows.Scan(&id, &runtime, &agents, &usage); err != nil {
			return nil, err
		}
		out[id] = machineCard{runtime: rawOf(runtime), agents: rawOf(agents), usage: rawOf(usage)}
	}
	return out, rows.Err()
}

func rawOf(v sql.NullString) json.RawMessage {
	if !v.Valid {
		return nil
	}
	return json.RawMessage(v.String)
}

// cardHangs: the machine is on and did not give its card when last asked.
func (s *Service) cardHangs(id string) bool {
	s.mu.Lock()
	a, ok := s.asks[id]
	s.mu.Unlock()
	return ok && a.missed && s.reg.Get(id) != nil
}

// refreshCards asks each machine of the list that is on for its card, unless it was asked a moment ago; the list does
// not wait for them.
func (s *Service) refreshCards(host string, u store.User, list []FleetMachine) {
	for _, f := range list {
		if !f.Online {
			continue
		}
		s.mu.Lock()
		a, ok := s.asks[f.ID]
		due := !ok || (!a.pending && !s.now().Before(a.at.Add(cardFresh)))
		if due {
			if len(s.asks) >= maxAuthz {
				s.asks = map[string]cardAsk{}
			}
			s.asks[f.ID] = cardAsk{at: s.now(), pending: true, missed: a.missed}
		}
		s.mu.Unlock()
		if due {
			safego.Go("proxy askCard", func() { s.askCard(host, u, f.ID) })
		}
	}
}

// askCard reads the machine's own record (GET /api/v2/machines on it) and keeps its runtime and agents.
func (s *Service) askCard(host string, u store.User, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), cardWait)
	defer cancel()
	missed := false
	defer func() {
		s.mu.Lock()
		s.asks[id] = cardAsk{at: s.now(), missed: missed}
		s.mu.Unlock()
	}()
	m, err := s.admit(ctx, u.ID, id)
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v2/machines?caps="+cardCaps, nil)
	if err != nil {
		return
	}
	req.Host = host
	req.Header.Set("Accept", "application/json")
	rec := &pageRecorder{header: http.Header{}, status: http.StatusOK}
	s.forward(rec, req, u, m)
	if ctx.Err() != nil {
		// No answer in time from a machine whose agent keeps the link: its runtime hangs.
		missed = s.reg.Get(id) != nil
		return
	}
	if rec.status != http.StatusOK || rec.body.Len() > maxCard {
		return
	}
	var self []wire.Machine
	if err := json.Unmarshal(rec.body.Bytes(), &self); err != nil || len(self) != 1 {
		s.log.Warn("machine cards: the machine's record is unreadable", "machine", id, "err", err)
		return
	}
	agents := self[0].Agents
	if agents == nil {
		agents = []wire.MachineAgent{}
	}
	ab, err := json.Marshal(agents)
	if err != nil {
		return
	}
	var rb []byte
	if self[0].Runtime != nil {
		if rb, err = json.Marshal(self[0].Runtime); err != nil {
			return
		}
	}
	var runtime any
	if rb != nil {
		runtime = string(rb)
	}
	if _, err := s.db.Exec(context.WithoutCancel(ctx), `INSERT INTO machine_cards (machine_id, runtime, agents, agents_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET runtime = excluded.runtime, agents = excluded.agents, agents_at = excluded.agents_at`,
		id, runtime, string(ab), store.Ms(s.now())); err != nil {
		s.log.Warn("machine cards: not kept", "machine", id, "err", err)
	}
}

// usageTee passes the platform's …/machines to the page and keeps a copy of what it said.
type usageTee struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
	over   bool
}

func (t *usageTee) WriteHeader(code int) {
	if t.status == 0 {
		t.status = code
	}
	t.ResponseWriter.WriteHeader(code)
}

func (t *usageTee) Write(b []byte) (int, error) {
	if t.status == 0 {
		t.status = http.StatusOK
	}
	if !t.over && t.body.Len()+len(b) <= maxCard {
		t.body.Write(b)
	} else {
		t.over = true
	}
	return t.ResponseWriter.Write(b)
}

func (t *usageTee) Flush() {
	if f, ok := t.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (t *usageTee) Unwrap() http.ResponseWriter { return t.ResponseWriter }

// keepUsage keeps the subscriptions' limits the machine gave in the platform's …/machines: its own entry of the list
// (the only one, for a runtime that answers for itself).
func (s *Service) keepUsage(ctx context.Context, id string, t *usageTee) {
	if t.status != http.StatusOK || t.over {
		return
	}
	var doc struct {
		Machines []struct {
			ID     string          `json:"id"`
			Limits json.RawMessage `json:"limits"`
		} `json:"machines"`
	}
	if json.Unmarshal(t.body.Bytes(), &doc) != nil {
		return
	}
	var limits json.RawMessage
	for _, m := range doc.Machines {
		if m.ID == id {
			limits = m.Limits
		}
	}
	if limits == nil && len(doc.Machines) == 1 {
		limits = doc.Machines[0].Limits
	}
	var shape map[string]json.RawMessage
	if limits == nil || json.Unmarshal(limits, &shape) != nil {
		return
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, limits); err != nil {
		return
	}
	if _, err := s.db.Exec(context.WithoutCancel(ctx), `INSERT INTO machine_cards (machine_id, usage, usage_at) VALUES (?, ?, ?)
		ON CONFLICT (machine_id) DO UPDATE SET usage = excluded.usage, usage_at = excluded.usage_at`,
		id, compact.String(), store.Ms(s.now())); err != nil && !errors.Is(err, context.Canceled) {
		s.log.Warn("machine cards: usage not kept", "machine", id, "err", err)
	}
}
