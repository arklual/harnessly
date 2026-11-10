package apps

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"sort"
	"sync"
	"time"
)

// The tools a view of an app offers from its page (tools.page), and what it publishes for the agent to read
// (context.publish). A view registers them with hl.tools.register or the standard navigator.modelContext.registerTool;
// the page that shows the view reports them here while the view is up, and the agent of the chat lists them with the
// host tool page_tools and calls one with page_call. A call waits here until the page takes it, hands it to the view's
// window and brings the answer back. Memory only: a tool is as alive as the view that offers it.

const (
	// PageToolsTTL is how long a report holds without being renewed: the page renews it every few seconds while the view
	// is up, so a closed view's tools are gone soon after.
	PageToolsTTL = 15 * time.Second
	// PageCallTTL is how long a call waits for the view's answer.
	PageCallTTL           = 60 * time.Second
	MaxPageTools          = 32
	MaxPageToolSchema     = 16 << 10
	MaxPageToolArgs       = 64 << 10
	MaxPageToolResult     = 256 << 10
	MaxPageContext        = 64 << 10
	MaxPageCallsPerChat   = 8
	maxPageToolDescLength = 1000
)

var (
	// ErrPageToolArgs: a report or a call past the limits, or a tool of no valid name.
	ErrPageToolArgs = errors.New("apps: bad page tool")
	// ErrPageToolGone: no view of the app offers that tool now.
	ErrPageToolGone = errors.New("apps: the view that offers this tool is not open")
	// ErrPageCallBusy: too many calls of the chat wait for the page.
	ErrPageCallBusy = errors.New("apps: too many page calls waiting in this chat")
	// ErrPageCallGone: no such waiting call.
	ErrPageCallGone = errors.New("apps: no such page call")
	// ErrPageCallTimeout: the page did not answer in time (the view was closed, or the tool hung).
	ErrPageCallTimeout = errors.New("apps: the view did not answer")
)

var pageToolNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// PageTool is a tool a view offers, as the view described it.
type PageTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	// Confirm: the page asks the person before it runs the tool; a refusal comes back as the call's error.
	Confirm bool `json:"confirm,omitempty"`
}

// PageReport is what the page reports for one app of a chat.
type PageReport struct {
	App     string          `json:"app"`
	Tools   []PageTool      `json:"tools"`
	Context json.RawMessage `json:"context,omitempty"`
	// Hooks: a view of the app hears the agent's tool calls before they run (hooks.agent).
	Hooks bool `json:"hooks,omitempty"`
}

// PageCall is a call the page takes to the view.
type PageCall struct {
	ID        string          `json:"id"`
	App       string          `json:"app"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	// Hook: not a tool of the view but the agent's tool call, for the view's hook to stop or let through.
	Hook bool `json:"hook,omitempty"`
}

// PageAnswer is the view's answer: a result, or an error in words.
type PageAnswer struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

type pageEntry struct {
	PageReport
	at time.Time
}

type openPageCall struct {
	PageCall
	conv   string
	taken  bool
	result chan PageAnswer
}

// PageTools is the page tools of the machine, by chat and app.
type PageTools struct {
	mu    sync.Mutex
	chats map[string]map[string]pageEntry
	calls map[string]*openPageCall
	// answered is when the last call of an app's view in a chat was answered, by chat and app.
	answered map[string]time.Time
	// Now and CallTTL are for tests.
	Now     func() time.Time
	CallTTL time.Duration
}

// AgentAfter is how long after a view answers the agent's call of its tool the view's loud calls still count as the
// agent's: the page asks the person about them, and the runtime runs them only with the person's yes.
const AgentAfter = 30 * time.Second

func NewPageTools() *PageTools {
	return &PageTools{chats: map[string]map[string]pageEntry{}, calls: map[string]*openPageCall{}, answered: map[string]time.Time{}}
}

func (p *PageTools) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func checkReport(r PageReport) error {
	if !idRe.MatchString(r.App) || len(r.Tools) > MaxPageTools || len(r.Context) > MaxPageContext {
		return ErrPageToolArgs
	}
	if len(r.Context) > 0 && !json.Valid(r.Context) {
		return ErrPageToolArgs
	}
	seen := map[string]bool{}
	for _, t := range r.Tools {
		if !pageToolNameRe.MatchString(t.Name) || seen[t.Name] || len(t.Description) > maxPageToolDescLength || len(t.InputSchema) > MaxPageToolSchema {
			return ErrPageToolArgs
		}
		if len(t.InputSchema) > 0 && !json.Valid(t.InputSchema) {
			return ErrPageToolArgs
		}
		seen[t.Name] = true
	}
	return nil
}

// Report replaces what a chat's page offers for the apps it names; the caller has checked the apps are on in the chat
// and hold their rights (Allowed). An app the page shows no more is dropped when its report runs out (PageToolsTTL).
func (p *PageTools) Report(conv string, reports []PageReport) error {
	for _, r := range reports {
		if err := checkReport(r); err != nil {
			return err
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	apps := p.chats[conv]
	if apps == nil {
		apps = map[string]pageEntry{}
		p.chats[conv] = apps
	}
	for _, r := range reports {
		apps[r.App] = pageEntry{PageReport: r, at: p.now()}
	}
	return nil
}

// Offered is what the views of a chat offer now, by app.
func (p *PageTools) Offered(conv string) []PageReport {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []PageReport
	for app, e := range p.chats[conv] {
		if p.now().Sub(e.at) > PageToolsTTL {
			delete(p.chats[conv], app)
			continue
		}
		out = append(out, e.PageReport)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].App < out[j].App })
	return out
}

// Hooked says whether a view of the app in the chat hears the agent's tool calls now.
func (p *PageTools) Hooked(conv, app string) bool {
	for _, r := range p.Offered(conv) {
		if r.App == app && r.Hooks {
			return true
		}
	}
	return false
}

func (p *PageTools) offers(conv, app, tool string) bool {
	for _, r := range p.Offered(conv) {
		if r.App != app {
			continue
		}
		for _, t := range r.Tools {
			if t.Name == tool {
				return true
			}
		}
	}
	return false
}

// Call puts a call to a view's tool and waits for its answer, the context's end or PageCallTTL.
func (p *PageTools) Call(ctx context.Context, conv, app, tool string, args json.RawMessage) (PageAnswer, error) {
	if !p.offers(conv, app, tool) {
		return PageAnswer{}, ErrPageToolGone
	}
	ttl := p.CallTTL
	if ttl == 0 {
		ttl = PageCallTTL
	}
	return p.put(ctx, PageCall{App: app, Tool: tool, Arguments: args}, conv, ttl)
}

// Hook hands the agent's tool call to the view of the app that hears them and waits for its word, at most ttl. The
// view acts in the agent's turn while it decides, so its loud calls count as the agent's, as a tool call's do.
func (p *PageTools) Hook(ctx context.Context, conv, app string, call json.RawMessage, ttl time.Duration) (PageAnswer, error) {
	if !p.Hooked(conv, app) {
		return PageAnswer{}, ErrPageToolGone
	}
	return p.put(ctx, PageCall{App: app, Arguments: call, Hook: true}, conv, ttl)
}

// put puts a call for the chat's page and waits for its answer, the context's end or ttl.
func (p *PageTools) put(ctx context.Context, pc PageCall, conv string, ttl time.Duration) (PageAnswer, error) {
	app, args := pc.App, pc.Arguments
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	if len(args) > MaxPageToolArgs || !json.Valid(args) {
		return PageAnswer{}, ErrPageToolArgs
	}
	raw := make([]byte, 12)
	_, _ = rand.Read(raw)
	pc.ID, pc.Arguments = hex.EncodeToString(raw), args
	c := &openPageCall{PageCall: pc, conv: conv, result: make(chan PageAnswer, 1)}
	p.mu.Lock()
	n := 0
	for _, o := range p.calls {
		if o.conv == conv {
			n++
		}
	}
	if n >= MaxPageCallsPerChat {
		p.mu.Unlock()
		return PageAnswer{}, ErrPageCallBusy
	}
	p.calls[c.ID] = c
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.calls, c.ID)
		// However the call ends (an answer, a timeout, the agent gone), the view may still act on it a while.
		p.answered[conv+"\x00"+app] = p.now()
		p.mu.Unlock()
	}()
	timer := time.NewTimer(ttl)
	defer timer.Stop()
	select {
	case a := <-c.result:
		return a, nil
	case <-ctx.Done():
		return PageAnswer{}, ctx.Err()
	case <-timer.C:
		return PageAnswer{}, ErrPageCallTimeout
	}
}

// Driving says whether the agent of a chat is using an app's view: a call of the view's tool waits, or was answered
// less than AgentAfter ago.
func (p *PageTools) Driving(conv, app string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.calls {
		if c.conv == conv && c.App == app {
			return true
		}
	}
	at, ok := p.answered[conv+"\x00"+app]
	if ok && p.now().Sub(at) >= AgentAfter {
		delete(p.answered, conv+"\x00"+app)
		return false
	}
	return ok
}

// Take hands the page the calls of a chat nobody took yet; each goes out once.
func (p *PageTools) Take(conv string) []PageCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []PageCall{}
	for _, c := range p.calls {
		if c.conv == conv && !c.taken {
			c.taken = true
			out = append(out, c.PageCall)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Answer brings a view's answer to the call that waits for it; conv must be the call's chat.
func (p *PageTools) Answer(conv, id string, a PageAnswer) error {
	if len(a.Result) > MaxPageToolResult || (len(a.Result) > 0 && !json.Valid(a.Result)) || len(a.Error) > 2000 {
		return ErrPageToolArgs
	}
	p.mu.Lock()
	c, ok := p.calls[id]
	p.mu.Unlock()
	if !ok || c.conv != conv {
		return ErrPageCallGone
	}
	select {
	case c.result <- a:
	default:
	}
	return nil
}

// PageApp is the manifest of an app whose page may offer tools and context in a chat: a builtin app's (Sinopia runs in
// the host's page wherever a document is open, and is on in no chat's list), or an installed app on in the chat. nil
// for any other.
func PageApp(id string, enabled []string, reg *Registry) *Manifest {
	if m := builtinOf(id); m != nil {
		return m
	}
	if reg == nil || !slices.Contains(enabled, id) {
		return nil
	}
	in, err := reg.Get(id)
	if err != nil {
		return nil
	}
	return in.Manifest
}

// PageReportAllowed keeps of a report what the app may offer: tools need tools.page, the context context.publish.
func PageReportAllowed(m *Manifest, r PageReport) PageReport {
	if !m.Allows("tools.page") {
		r.Tools = nil
	}
	if !m.Allows("context.publish") {
		r.Context = nil
	}
	if !m.Allows("hooks.agent") {
		r.Hooks = false
	}
	return r
}
