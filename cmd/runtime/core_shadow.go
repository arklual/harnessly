package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"

	"harnessly/internal/agent"
	"harnessly/internal/agents"
	"harnessly/internal/agents/claude"
	"harnessly/internal/agents/codex"
	"harnessly/internal/gate"
	"harnessly/internal/sockets"
	"harnessly/internal/status"
)

// The core's shadow (slice "core-shadow", while "core" is off): a Go keeper next to the old runtime's, reading the same
// layouts and tmux and fed copies of the same hooks, writing nothing and typing nothing. It compares its tab states with
// the old runtime's status stream and logs what stays different, so the core is switched on only after they agree.

// shadowGrace: a difference younger than this is the two keepers' timing, not a disagreement. Each keeper polls every
// 30-60 s on its own phase, so what a poll notices (an agent gone) can reach one of them a whole interval later.
var shadowGrace = 90 * time.Second

const (
	shadowCheckEvery  = 5 * time.Second
	shadowReportEvery = 10 * time.Minute
)

// readonlyLayouts reads the layouts and writes nothing: the old runtime still owns them.
type readonlyLayouts struct{ coreLayouts }

func (readonlyLayouts) UpdateTabCliStatus(string, string, status.Opt[float64], string) error {
	return nil
}
func (readonlyLayouts) UpdateTabAgentState(string, string, status.Opt[string], status.Opt[string], status.Opt[string], status.Opt[string]) error {
	return nil
}
func (readonlyLayouts) RecoverSession(string) (string, bool) { return "", false }

// shadowTmux never pauses or resizes the terminal a person is looking at, and never types.
type shadowTmux struct{ coreTmux }

func (t shadowTmux) CaptureAtWidth(session string, _, _ int) (string, bool) {
	return t.Capture(session)
}
func (shadowTmux) SendRawKeys(string, string) error { return nil }

type nullHub struct{}

func (nullHub) Broadcast(any, *sockets.Client) {}
func (nullHub) Shutdown()                      {}

type shadowTab struct {
	CliState       string   `json:"cliState"`
	WorkspaceID    string   `json:"workspaceId"`
	TabName        string   `json:"tabName"`
	PanelType      *string  `json:"panelType"`
	AgentSessionID *string  `json:"agentSessionId"`
	HooksMissing   bool     `json:"hooksMissing"`
	DismissedAt    *float64 `json:"dismissedAt"`
	LastEvent      *struct {
		Name string `json:"name"`
		Seq  int    `json:"seq"`
	} `json:"lastEvent"`
}

type shadowDiff struct {
	since    time.Time
	reported bool
}

type coreShadow struct {
	c      *core
	m      *status.Manager
	cancel context.CancelFunc

	mu       sync.Mutex
	old      map[string]shadowTab // the old runtime's view
	synced   bool
	diffs    map[string]*shadowDiff
	matched  int
	differed int
}

// signSession is a page session for the old runtime's own socket: HS256 with the pair's secret and no audience.
func signSession(secret []byte, sub string, ttl time.Duration) string {
	enc := base64.RawURLEncoding
	now := time.Now().Unix()
	payload, _ := json.Marshal(map[string]any{"sub": sub, "iat": now, "exp": now + int64(ttl/time.Second)})
	signing := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) + "." + enc.EncodeToString(payload)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	return signing + "." + enc.EncodeToString(mac.Sum(nil))
}

func (c *core) startShadow(secret []byte, cookie, upstream string) *coreShadow {
	ctx, cancel := context.WithCancel(context.Background())
	m := status.New(status.Deps{
		Layouts: readonlyLayouts{coreLayouts{store: c.store, claude: c.claude, history: c.history}},
		Tmux:    shadowTmux{coreTmux{Tmux: c.cfg.Tmux, term: c.cfg.Term}},
		Procs:   coreProcs{},
		Providers: map[string]status.Provider{
			agents.Claude: claudeProvider{det: readonlyClaude(c.cfg.Home), snaps: claude.NewSnapshotReader(), hooks: filepath.Join(c.cfg.DataDir, "hooks.json")},
			agents.Codex:  codexProvider{det: codex.NewDetector(c.cfg.Home), snaps: codex.NewSnapshotReader()},
		},
		BackgroundTasks:  c.bg.Read,
		ForgetTranscript: c.bg.Forget,
		Log: func(level, msg string, kv ...any) {
			if level == "warn" {
				agent.Log("info", "core shadow: "+msg)
			}
		},
	}, nullHub{})
	s := &coreShadow{c: c, m: m, cancel: cancel, old: map[string]shadowTab{}, diffs: map[string]*shadowDiff{}}
	m.Start(ctx)
	go s.follow(ctx, secret, cookie, upstream)
	go s.compareLoop(ctx)
	agent.Log("info", "core shadow started")
	return s
}

// readonlyClaude finds Claude without removing stale pid files: the old runtime keeps that folder tidy.
func readonlyClaude(home string) *claude.Detector {
	d := claude.NewDetector(home)
	d.RemoveStale = false
	return d
}

func (s *coreShadow) stop() {
	s.cancel()
	agent.Log("info", "core shadow stopped")
}

// follow reads the old runtime's status socket and keeps its view; a dropped socket is dialled again.
func (s *coreShadow) follow(ctx context.Context, secret []byte, cookie, upstream string) {
	for ctx.Err() == nil {
		hdr := http.Header{"Cookie": {cookie + "=" + signSession(secret, "core-shadow", time.Hour)}}
		ws, _, err := websocket.Dial(ctx, "ws://"+upstream+"/api/status", &websocket.DialOptions{HTTPHeader: hdr})
		if err == nil {
			ws.SetReadLimit(64 << 20)
			for {
				_, data, err := ws.Read(ctx)
				if err != nil {
					break
				}
				s.take(data)
			}
			ws.CloseNow()
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
	}
}

func (s *coreShadow) take(data []byte) {
	var msg struct {
		Type  string                     `json:"type"`
		TabID string                     `json:"tabId"`
		Tabs  map[string]json.RawMessage `json:"tabs"`
	}
	if json.Unmarshal(data, &msg) != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch msg.Type {
	case "status:sync":
		s.synced = true
		s.old = map[string]shadowTab{}
		for id, raw := range msg.Tabs {
			var t shadowTab
			if json.Unmarshal(raw, &t) == nil {
				s.old[id] = t
			}
		}
	case "status:update":
		var t shadowTab
		if json.Unmarshal(data, &t) != nil {
			return
		}
		if bytes.Contains(data, []byte(`"cliState":null`)) {
			delete(s.old, msg.TabID)
			return
		}
		prev, had := s.old[msg.TabID]
		s.old[msg.TabID] = t
		if had {
			go s.mirrorPerson(msg.TabID, prev, t)
		}
	}
}

// mirrorPerson applies what the person did in the old runtime's page (dismissed an answer, answered a question): those
// messages travel inside the proxied socket, out of the shadow's sight.
func (s *coreShadow) mirrorPerson(tabID string, prev, next shadowTab) {
	if prev.CliState == status.ReadyForReview && next.CliState == status.Idle && next.DismissedAt != nil {
		s.m.Dismiss(tabID, nil)
	}
	if prev.CliState == status.NeedsInput && next.CliState == status.Busy && next.LastEvent != nil && prev.LastEvent != nil && next.LastEvent.Seq == prev.LastEvent.Seq {
		if info, ok := s.m.Tab(tabID); ok && info.CliState == status.NeedsInput {
			s.m.AckCurrent(tabID)
		}
	}
}

func (s *coreShadow) compareLoop(ctx context.Context) {
	t := time.NewTicker(shadowCheckEvery)
	defer t.Stop()
	report := time.NewTicker(shadowReportEvery)
	defer report.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.compare()
		case <-report.C:
			s.mu.Lock()
			agent.Log("info", "core shadow compared", agent.F{K: "matched", V: s.matched}, agent.F{K: "differ", V: s.differed}, agent.F{K: "tabs", V: len(s.old)})
			s.matched, s.differed = 0, 0
			s.mu.Unlock()
		}
	}
}

func (s *coreShadow) compare() {
	var goView map[string]shadowTab
	raw, _ := json.Marshal(s.m.Tabs())
	_ = json.Unmarshal(raw, &goView)
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.synced {
		return // no stream yet
	}
	now := time.Now()
	ids := map[string]bool{}
	for id := range s.old {
		ids[id] = true
	}
	for id := range goView {
		ids[id] = true
	}
	seen := map[string]bool{}
	sorted := make([]string, 0, len(ids))
	for id := range ids {
		sorted = append(sorted, id)
	}
	sort.Strings(sorted)
	for _, id := range sorted {
		o, okOld := s.old[id]
		g, okGo := goView[id]
		for _, d := range shadowFields(o, g, okOld, okGo) {
			key := id + " " + d[0]
			seen[key] = true
			diff := s.diffs[key]
			if diff == nil {
				s.diffs[key] = &shadowDiff{since: now}
				continue
			}
			if !diff.reported && now.Sub(diff.since) >= shadowGrace {
				diff.reported = true
				s.differed++
				agent.Log("warn", "core shadow differs", agent.F{K: "tab", V: id}, agent.F{K: "field", V: d[0]},
					agent.F{K: "old", V: d[1]}, agent.F{K: "go", V: d[2]}, agent.F{K: "forMs", V: now.Sub(diff.since).Milliseconds()})
			}
		}
	}
	for key := range s.diffs {
		if !seen[key] {
			delete(s.diffs, key)
		}
	}
	s.matched += len(sorted) - len(s.diffs)
}

func strOf(p *string) string {
	if p == nil {
		return "null"
	}
	return *p
}

// shadowFields are the differences that matter between two views of a tab: [field, old, go].
func shadowFields(o, g shadowTab, okOld, okGo bool) [][3]string {
	if !okOld || !okGo {
		return [][3]string{{"present", boolStr(okOld), boolStr(okGo)}}
	}
	var out [][3]string
	add := func(f, a, b string) {
		if a != b {
			out = append(out, [3]string{f, a, b})
		}
	}
	add("cliState", o.CliState, g.CliState)
	add("workspaceId", o.WorkspaceID, g.WorkspaceID)
	add("tabName", o.TabName, g.TabName)
	add("panelType", strOf(o.PanelType), strOf(g.PanelType))
	add("agentSessionId", strOf(o.AgentSessionID), strOf(g.AgentSessionID))
	add("hooksMissing", boolStr(o.HooksMissing), boolStr(g.HooksMissing))
	return out
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// shadowTees copy what reaches the old runtime and moves tab states to the shadow too: hooks and launch marks.
func (c *core) shadowTees(f *gate.Gate, shadow func() *coreShadow) []gate.Route {
	tee := func(path string, feed func(s *coreShadow, r *http.Request, body []byte)) gate.Route {
		return gate.Route{Slice: "core-shadow", Match: func(r *http.Request) bool {
			return r.Method == http.MethodPost && pathIs(r, path) && !f.SliceOn("core") // the core takes them itself
		},
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				r.Body = io.NopCloser(bytes.NewReader(body))
				q := r.URL.Query()
				f.Proxy(w, r)
				if s := shadow(); s != nil {
					cp := r.Clone(context.Background())
					cp.URL.RawQuery = q.Encode()
					go feed(s, cp, body)
				}
			})}
	}
	return []gate.Route{
		tee("/api/status/hook", func(s *coreShadow, r *http.Request, body []byte) {
			var b map[string]any
			_ = json.Unmarshal(body, &b)
			if r.URL.Query().Get("provider") == "codex" {
				session := r.URL.Query().Get("tmuxSession")
				t := codex.ProcessHook(b)
				_, cliState, ok := s.m.ApplyHookMeta(agents.Codex, session, hookMetaOf(t.Meta))
				if ok && t.Event != nil && codex.ShouldEmit(b, cliState) {
					s.m.HandleProviderEvent(agents.Codex, session, *t.Event, nil)
				}
				return
			}
			event, _ := b["event"].(string)
			session, _ := b["session"].(string)
			if event == "" || event == "poll" || session == "" {
				s.m.Poll()
				return
			}
			notif, _ := b["notificationType"].(string)
			ev, ok := claude.TranslateHook(event, notif)
			if !ok {
				return
			}
			var at *float64
			if f, ok := b["at"].(float64); ok {
				at = &f
			}
			s.m.HandleProviderEvent(agents.Claude, session, ev, at)
		}),
		tee("/api/status/agent-launch", func(s *coreShadow, _ *http.Request, body []byte) {
			var b struct {
				TabID string `json:"tabId"`
				Reset bool   `json:"resetAgentSession"`
			}
			if json.Unmarshal(body, &b) == nil && b.TabID != "" {
				s.m.MarkAgentLaunch(b.TabID, b.Reset)
			}
		}),
	}
}

// hookMetaOf is a Codex hook's conversation details as the keeper takes them.
func hookMetaOf(m codex.Meta) status.HookMeta {
	opt := func(p *string) *status.Opt[string] {
		if p == nil {
			return nil
		}
		o := status.Some(*p)
		return &o
	}
	out := status.HookMeta{SessionID: opt(m.SessionID), JSONLPath: opt(m.JSONLPath), LastUserMessage: opt(m.LastUserMessage),
		AgentSummary: opt(m.AgentSummary), ClearMessages: m.ClearMessages}
	if m.SetPermission {
		raw := json.RawMessage("null")
		if m.Permission != nil {
			raw, _ = json.Marshal(m.Permission)
		}
		out.PermissionRequest = &raw
	}
	return out
}
