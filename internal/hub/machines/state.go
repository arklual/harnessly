package machines

import (
	"context"
	"encoding/json"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	helloTimeout     = 8 * time.Second
	preflightTimeout = 20 * time.Second
	callTimeout      = 15 * time.Second
	// statusTimeout is what the machine may take to look at its agents: each is probed for up to 25 s, in parallel.
	statusTimeout = 35 * time.Second
	// maxInfoPiece bounds one stored report. A report is a few kilobytes; a machine that sends more is not a
	// well-behaved one, and the database should not hold what it makes up.
	maxInfoPiece = 256 << 10
)

// subscribed are the events the hub asks the agent for; the closed cluster's events (transcripts, state, …) stay off
// the wire (docs/hub/architecture.md, "Hub ↔ runtime, everything else").
var subscribed = []string{hubproto.EventJob, hubproto.EventLogin, hubproto.EventAgents, hubproto.EventPreflight, hubproto.EventPolicy}

// Hooks are what the hub does when an agent connects, reports and goes away. Give them to link.Dialer.
func (s *Service) Hooks() link.Hooks {
	return link.Hooks{Connected: s.connected, Event: s.onEvent, Disconnected: s.disconnected, Unlink: s.unlinked}
}

func clamp(v string, n int) string {
	r := []rune(v)
	if len(r) > n {
		return string(r[:n])
	}
	return v
}

func oneOf(v string, allowed ...string) bool {
	for _, a := range allowed {
		if v == a {
			return true
		}
	}
	return false
}

// connected is the routine of a fresh link (docs/hub/architecture.md, "Registration and Ed25519", step 5): hello, then
// the events the hub wants, then the first look at the machine.
func (s *Service) connected(l *link.Link, m store.Machine) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	st := s.db.S()

	var hello hubproto.Hello
	hctx, hcancel := context.WithTimeout(ctx, helloTimeout)
	err := l.CallInto(hctx, hubproto.OpHello, nil, &hello)
	hcancel()
	if err != nil {
		s.log.Warn("agent did not say hello, dropping the link", "machine", m.ID, "error", err)
		l.Close(websocket.StatusPolicyViolation, "no hello")
		return
	}
	if hello.MachineID != "" && hello.MachineID != m.ID {
		s.log.Warn("agent says it is another machine, dropping the link", "machine", m.ID)
		l.Close(websocket.StatusPolicyViolation, "wrong machine")
		return
	}
	m.Hostname, m.UnixUser, m.AgentVersion = clamp(hello.Hostname, 255), clamp(hello.User, 64), clamp(hello.AgentVersion, 64)
	if oneOf(hello.OS, "linux", "darwin") {
		m.OS = hello.OS
	}
	if oneOf(hello.Arch, "amd64", "arm64") {
		m.Arch = hello.Arch
	}
	if err := st.UpdateMachineHello(ctx, m); err != nil {
		s.log.Warn("hello not saved", "machine", m.ID, "error", err)
	}
	_ = st.TouchMachine(ctx, m.ID, s.now().UTC())
	s.setPolicy(ctx, m, hello.Policy)
	s.Publish(ctx, m)

	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpLinkSubscribe, map[string]any{"events": subscribed}, callTimeout); err != nil {
		s.log.Warn("events not subscribed", "machine", m.ID, "error", err)
	}
	if _, err := s.RunPreflight(ctx, m); err != nil {
		s.log.Info("first preflight failed", "machine", m.ID, "error", err)
	}
	if _, err := s.RefreshAgents(ctx, m); err != nil {
		s.log.Info("first agents status failed", "machine", m.ID, "error", err)
	}
	var proxy hubproto.ProxySummary
	if err := s.reg.DoInto(ctx, m.ID, hubproto.OpProxyGet, nil, &proxy, callTimeout); err == nil {
		s.SetProxy(ctx, m, proxy)
	}
}

func (s *Service) disconnected(_ *link.Link, m store.Machine, current bool) {
	if !current {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = s.db.S().TouchMachine(ctx, m.ID, s.now().UTC())
	s.Publish(ctx, m)
}

func (s *Service) onEvent(_ *link.Link, m store.Machine, ev link.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	switch ev.Name {
	case hubproto.EventJob:
		var e struct{ Job hubproto.Job }
		if json.Unmarshal(ev.Raw, &e) != nil || e.Job.ID == "" {
			return
		}
		if raw, ok := normalized(e.Job); ok {
			s.PublishJob(m, raw)
		}
	case hubproto.EventLogin:
		var e struct{ Login hubproto.Login }
		if json.Unmarshal(ev.Raw, &e) != nil || e.Login.ID == "" {
			return
		}
		clean, refused := CleanLogin(e.Login, m.ID)
		if len(refused) > 0 {
			s.log.Warn("login fields refused", "machine", m.ID, "provider", clean.Provider, "fields", refused)
		}
		if raw, ok := normalized(clean); ok {
			s.PublishLogin(m, raw)
		}
		if e.Login.Status != nil {
			// A login that finished carries the new status of the agent.
			s.IngestAgents(ctx, m, []hubproto.AgentStatus{*e.Login.Status})
		}
	case hubproto.EventAgents:
		var e struct{ Agents []hubproto.AgentStatus }
		if json.Unmarshal(ev.Raw, &e) == nil {
			s.IngestAgents(ctx, m, e.Agents)
		}
	case hubproto.EventPreflight:
		var e struct{ Report hubproto.PreflightReport }
		if json.Unmarshal(ev.Raw, &e) == nil {
			s.setPreflight(ctx, m, e.Report)
		}
	case hubproto.EventPolicy:
		var e struct{ Policy hubproto.Policy }
		if json.Unmarshal(ev.Raw, &e) == nil {
			s.setPolicy(ctx, m, e.Policy)
		}
	}
}

// normalized is what the API sends for something an agent reported: the contract's fields only, times as RFC 3339.
func normalized(v any) (json.RawMessage, bool) {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > maxInfoPiece {
		return nil, false
	}
	return link.Times(raw), true
}

// update changes what is remembered of a machine and tells the space.
func (s *Service) update(ctx context.Context, m store.Machine, change func(*info)) {
	s.infoMu.Lock()
	cur, err := s.db.S().MachineByID(ctx, m.ID)
	if err != nil {
		s.infoMu.Unlock()
		return
	}
	var in info
	_ = json.Unmarshal([]byte(cur.Info), &in)
	change(&in)
	raw, err := json.Marshal(in)
	if err == nil {
		err = s.db.S().UpdateMachineInfo(ctx, m.ID, string(raw))
	}
	s.infoMu.Unlock()
	if err != nil {
		s.log.Warn("machine info not saved", "machine", m.ID, "error", err)
		return
	}
	s.Publish(ctx, m)
}

func (s *Service) setPolicy(ctx context.Context, m store.Machine, p hubproto.Policy) {
	if raw, ok := normalized(p); ok {
		s.update(ctx, m, func(in *info) { in.Policy = raw })
	}
}

func (s *Service) setPreflight(ctx context.Context, m store.Machine, r hubproto.PreflightReport) json.RawMessage {
	raw, ok := normalized(r)
	if !ok {
		return nil
	}
	s.update(ctx, m, func(in *info) { in.Preflight = raw })
	return raw
}

// SetProxy remembers the machine's proxy settings (never with credentials: the agent does not send them).
func (s *Service) SetProxy(ctx context.Context, m store.Machine, p hubproto.ProxySummary) {
	if raw, ok := normalized(p); ok {
		s.update(ctx, m, func(in *info) { in.Proxy = raw })
	}
}

// IngestAgents merges reported agent statuses into what is remembered, and records the summary of each in the
// database. Only claude and codex are taken: gemini is a card the hub draws itself.
func (s *Service) IngestAgents(ctx context.Context, m store.Machine, reported []hubproto.AgentStatus) {
	st := s.db.S()
	now := s.now().UTC()
	fresh := map[string]json.RawMessage{}
	for _, a := range reported {
		if a.Provider != hubproto.ProviderClaude && a.Provider != hubproto.ProviderCodex {
			continue
		}
		a.Soon = false
		raw, ok := normalized(a)
		if !ok {
			continue
		}
		fresh[a.Provider] = raw
		conn := store.AgentConnection{MachineID: m.ID, Provider: a.Provider, Installed: a.Installed, LoginState: a.Login}
		if a.Version != nil {
			conn.Version = clamp(*a.Version, 64)
		}
		if a.AuthMode != nil {
			conn.AuthMode = clamp(*a.AuthMode, 32)
		}
		if a.Access != nil {
			conn.ProviderVerdict = clamp(a.Access.Verdict, 32)
			if a.Access.At > 0 {
				t := time.UnixMilli(a.Access.At).UTC()
				conn.CheckedAt = &t
			}
		} else {
			conn.CheckedAt = &now
		}
		if err := st.UpsertAgentConnection(ctx, conn); err != nil {
			s.log.Warn("agent summary not saved", "machine", m.ID, "provider", a.Provider, "error", err)
		}
	}
	if len(fresh) == 0 {
		return
	}
	s.update(ctx, m, func(in *info) {
		merged := map[string]json.RawMessage{}
		var old []json.RawMessage
		_ = json.Unmarshal(in.Agents, &old)
		for _, raw := range old {
			var p struct{ Provider string }
			if json.Unmarshal(raw, &p) == nil {
				merged[p.Provider] = raw
			}
		}
		for p, raw := range fresh {
			merged[p] = raw
		}
		list := make([]json.RawMessage, 0, len(merged))
		for _, p := range []string{hubproto.ProviderClaude, hubproto.ProviderCodex} {
			if raw, ok := merged[p]; ok {
				list = append(list, raw)
			}
		}
		in.Agents, _ = json.Marshal(list)
	})
}

// RefreshAgents asks the agent for the status of its agents and remembers the answer. It returns the agents as the
// API sends them (claude, codex, gemini).
func (s *Service) RefreshAgents(ctx context.Context, m store.Machine) ([]json.RawMessage, error) {
	var res struct{ Agents []hubproto.AgentStatus }
	if err := s.reg.DoInto(ctx, m.ID, hubproto.OpAgentsStatus, nil, &res, statusTimeout); err != nil {
		return nil, err
	}
	s.IngestAgents(ctx, m, res.Agents)
	fresh, err := s.db.S().MachineByID(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	var in info
	_ = json.Unmarshal([]byte(fresh.Info), &in)
	return agentsView(in.Agents), nil
}

// Agents is what is remembered of the machine's agents, without asking it.
func (s *Service) Agents(m store.Machine) []json.RawMessage {
	var in info
	_ = json.Unmarshal([]byte(m.Info), &in)
	return agentsView(in.Agents)
}

// RunPreflight asks the machine to look at its disk, memory and tools, remembers the report and returns it as the API
// sends it.
func (s *Service) RunPreflight(ctx context.Context, m store.Machine) (json.RawMessage, error) {
	var report hubproto.PreflightReport
	if err := s.reg.DoInto(ctx, m.ID, hubproto.OpPreflightRun, nil, &report, preflightTimeout); err != nil {
		return nil, err
	}
	return s.setPreflight(ctx, m, report), nil
}
