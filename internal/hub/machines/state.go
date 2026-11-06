package machines

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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
	var herr error
	if l.Fence() != 0 {
		// A stale owner writes nothing: the write is guarded by the fencing token (docs/hub/replicas.md).
		herr = st.UpdateMachineHelloFenced(ctx, m, l.Owner(), l.Fence())
		if errors.Is(herr, store.ErrFenced) {
			s.log.Info("link no longer held, hello dropped", "machine", m.ID)
			l.Close(websocket.StatusCode(hubproto.CloseReplaced), "taken over by another replica")
			return
		}
		_ = st.TouchMachineFenced(ctx, m.ID, s.now().UTC(), l.Owner(), l.Fence())
	} else {
		herr = st.UpdateMachineHello(ctx, m)
		_ = st.TouchMachine(ctx, m.ID, s.now().UTC())
	}
	if herr != nil {
		s.log.Warn("hello not saved", "machine", m.ID, "error", herr)
	}
	s.setPolicy(ctx, m, hello.Policy)
	s.Publish(ctx, m)

	events := slices.Clone(subscribed)
	if s.usage != nil && slices.Contains(hello.Features, hubproto.FeatureUsage) {
		events = append(events, hubproto.EventUsageAvailable)
	}
	if s.gate != nil {
		// The load and the progress of a restart are asked for only by a hub that has the resources side wired.
		s.res.mu.Lock()
		s.res.features[m.ID] = slices.Clone(hello.Features)
		s.res.mu.Unlock()
		if slices.Contains(hello.Features, hubproto.FeatureMetrics) {
			events = append(events, hubproto.EventMetricsSample)
		}
		if slices.Contains(hello.Features, hubproto.FeatureDrain) {
			events = append(events, hubproto.EventRestart)
		}
		s.saveHardware(ctx, m, hello.Hardware)
	}
	if s.env != nil && slices.Contains(hello.Features, hubproto.FeatureEnvironment) {
		events = append(slices.Clone(events), hubproto.EventEnvState)
		if slices.Contains(hello.Features, hubproto.FeatureEnvSetup) {
			events = append(events, hubproto.EventEnvRun)
		}
	}
	if s.design != nil && slices.Contains(hello.Features, hubproto.FeatureDesignAccess) {
		events = append(slices.Clone(events), hubproto.EventDesign)
	}
	if _, err := s.reg.Do(ctx, m.ID, hubproto.OpLinkSubscribe, map[string]any{"events": events}, callTimeout); err != nil {
		s.log.Warn("events not subscribed", "machine", m.ID, "error", err)
	}
	if s.usage != nil {
		s.usage.Connected(m.ID, hello.Features)
	}
	if s.budget != nil {
		s.budget.Connected(m.ID, hello)
	}
	if s.env != nil {
		s.env.Connected(m.ID, hello)
	}
	if s.design != nil {
		s.design.Connected(m, hello.Features)
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

func (s *Service) disconnected(l *link.Link, m store.Machine, current bool) {
	if !current {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if l.Fence() != 0 {
		_ = s.db.S().TouchMachineFenced(ctx, m.ID, s.now().UTC(), l.Owner(), l.Fence())
	} else {
		_ = s.db.S().TouchMachine(ctx, m.ID, s.now().UTC())
	}
	s.res.mu.Lock()
	delete(s.res.features, m.ID)
	delete(s.res.latest, m.ID)
	s.res.mu.Unlock()
	s.Publish(ctx, m)
}

func (s *Service) onEvent(l *link.Link, m store.Machine, ev link.Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if l.Fence() != 0 && !s.db.S().LinkHeld(ctx, m.ID, l.Owner(), l.Fence(), s.now().UTC()) {
		return // a stale owner records nothing
	}
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
	case hubproto.EventEnvState:
		var e struct{ State hubproto.EnvState }
		if s.env != nil && json.Unmarshal(ev.Raw, &e) == nil {
			s.env.Reported(m.ID, e.State)
		}
	case hubproto.EventEnvRun:
		var e struct{ Run hubproto.EnvRun }
		if s.env != nil && json.Unmarshal(ev.Raw, &e) == nil {
			s.env.Run(m.ID, e.Run)
		}
	case hubproto.EventDesign:
		var e struct{ Design hubproto.DesignEvent }
		if s.design != nil && json.Unmarshal(ev.Raw, &e) == nil {
			go s.design.Event(m, e.Design)
		}
	case "doc.publish", "doc.acl":
		if s.docs != nil {
			s.docs.Event(l, m, ev)
		}
	case hubproto.EventUsageAvailable:
		if s.usage != nil {
			s.usage.Trigger(m.ID)
		}
	case hubproto.EventMetricsSample:
		var e struct{ Sample hubproto.MetricsSample }
		if s.gate != nil && json.Unmarshal(ev.Raw, &e) == nil {
			s.onSample(ctx, m, e.Sample)
		}
	case hubproto.EventRestart:
		var e struct{ Restart hubproto.RestartEvent }
		if s.gate != nil && json.Unmarshal(ev.Raw, &e) == nil {
			s.onRestart(ctx, m, e.Restart)
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

// saveHardware remembers what the machine said it is made of, for an organization that has the resources feature on.
func (s *Service) saveHardware(ctx context.Context, m store.Machine, hw *hubproto.Hardware) {
	if hw == nil || !s.on(ctx, m.SpaceID) {
		return
	}
	h := store.MachineHW{MachineID: m.ID, Cores: max(hw.Cores, 0), RAMTotal: max(hw.RAMBytes, 0), DiskTotal: max(hw.DiskBytes, 0),
		OS: clamp(hw.OS, 32), Arch: clamp(hw.Arch, 32), Model: clamp(hw.Model, 200), Laptop: hw.Laptop, UpdatedAt: s.now().UTC()}
	if err := s.db.S().PutHW(ctx, h); err != nil {
		s.log.Warn("hardware not saved", "machine", m.ID, "error", err)
	}
}
