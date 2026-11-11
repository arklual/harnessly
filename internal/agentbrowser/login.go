package agentbrowser

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agentbrowser/host"
	"github.com/ly-solutions/harnessly/internal/secrets"
	"github.com/ly-solutions/harnessly/internal/vault"
)

// LoginAsk is page-only metadata. Agent results contain no page contents or titles.
type LoginAsk struct {
	ID      string `json:"id"`
	Conv    string `json:"conv"`
	Title   string `json:"title"`
	Why     string `json:"why"`
	URL     string `json:"url"`
	Tab     string `json:"tab"`
	At      int64  `json:"at"`
	Project string `json:"-"`
}

// LoginAnswer deliberately exposes only the outcome and the current URL.
type LoginAnswer struct {
	Status string `json:"status"`
	URL    string `json:"url"`
}

type loginRequest struct {
	mu       sync.Mutex
	ask      LoginAsk
	instance *Instance
	answer   LoginAnswer
	done     chan struct{}
	settled  time.Time
}

func (x *loginRequest) settle(status, currentURL string) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.answer.Status != "waiting" {
		return
	}
	x.answer = LoginAnswer{Status: status, URL: currentURL}
	x.settled = time.Now()
	close(x.done)
}

// LoginRequests coalesces retries across tool calls and panel connections.
type LoginRequests struct {
	mu      sync.Mutex
	entries map[string]*loginRequest
	limiter *vault.Limiter
}

// NewLoginRequests creates the page request queue independently of browser connections.
func NewLoginRequests() *LoginRequests {
	return &LoginRequests{entries: map[string]*loginRequest{}, limiter: vault.NewLimiter(vault.Rule{Limit: 6, Window: 10 * time.Minute})}
}

func (q *LoginRequests) open(conv string, identity ConversationIdentity, target, why string) (*loginRequest, bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	var recent *loginRequest
	waiting := 0
	for id, x := range q.entries {
		x.mu.Lock()
		pending := x.answer.Status == "waiting"
		if !pending && time.Since(x.settled) >= 2*time.Minute {
			delete(q.entries, id)
			x.mu.Unlock()
			continue
		}
		match := x.ask.Conv == conv && x.ask.Project == identity.Project
		exact := match && x.ask.URL == target && x.ask.Why == why
		if pending {
			waiting++
		}
		if match && pending {
			x.mu.Unlock()
			return x, false, nil
		}
		if exact && (recent == nil || x.ask.At > recent.ask.At) {
			recent = x
		}
		x.mu.Unlock()
	}
	if recent != nil {
		return recent, false, nil
	}
	if waiting >= 16 {
		return nil, false, errors.New("Too many browser login requests")
	}
	if ok, _ := q.limiter.Take(conv, time.Now().UnixMilli()); !ok {
		return nil, false, errors.New("Too many browser login requests")
	}
	x := &loginRequest{ask: LoginAsk{ID: randomProfileID(), Conv: conv, Project: identity.Project, Title: secrets.CleanWhy(identity.Title), Why: why, URL: target, At: time.Now().UnixMilli()}, answer: LoginAnswer{Status: "waiting", URL: "about:blank"}, done: make(chan struct{})}
	q.entries[x.ask.ID] = x
	return x, true, nil
}

// loginHold is how long a waiting login keeps its browser from the idle stop: the person may sign in and return
// control minutes later, and a stopped browser would lose the tab the request is bound to.
const loginHold = 30 * time.Minute

func (x *loginRequest) heldAt(now time.Time) bool {
	if x == nil {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.answer.Status == "waiting" && now.Sub(time.UnixMilli(x.ask.At)) < loginHold
}

// dropLogins settles the logins of a browser that went down: its tabs come back under new IDs in a new instance,
// so a request left waiting could never be answered and would stay on every chat's screen.
func (i *Instance) dropLogins() {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, p := range i.pages {
		if p.login != nil {
			p.login.settle("cancelled", i.redactLocked(p.URL))
			p.login = nil
		}
	}
}

func (q *LoginRequests) find(id string) *loginRequest {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.entries[id]
}

// Pending supplies global cards even when the requesting chat has no panel connection.
func (q *LoginRequests) Pending() []LoginAsk {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := []LoginAsk{}
	for _, x := range q.entries {
		x.mu.Lock()
		if x.answer.Status == "waiting" && x.ask.Tab != "" {
			out = append(out, x.ask)
		}
		x.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].At < out[j].At || out[i].At == out[j].At && out[i].ID < out[j].ID })
	return out
}

func (x *loginRequest) wait(ctx context.Context, duration time.Duration) LoginAnswer {
	timer := time.NewTimer(max(0, min(duration, 55*time.Second)))
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	case <-x.done:
	}
	x.mu.Lock()
	answer, i, id := x.answer, x.instance, x.ask.Tab
	x.mu.Unlock()
	if i != nil {
		i.mu.Lock()
		if p := i.pages[id]; answer.Status == "waiting" && p != nil {
			answer.URL = p.URL
		}
		answer.URL = i.redactLocked(answer.URL)
		i.mu.Unlock()
	}
	return answer
}

// RequestLogin opens a person-controlled tab before navigating and waits without holding access locks.
func (a *ProfileAccess) RequestLogin(ctx context.Context, conv, target, why string) (LoginAnswer, error) {
	if err := AllowedURL(target); err != nil {
		return LoginAnswer{}, err
	}
	why = secrets.CleanWhy(why)
	if why == "" {
		return LoginAnswer{}, errors.New("A reason is required")
	}
	a.mu.Lock()
	_, identity, err := a.resolve(conv)
	var x *loginRequest
	var fresh bool
	if err == nil && a.Manager == nil {
		err = errors.New("Browser login is unavailable")
	}
	if err == nil {
		x, fresh, err = a.Logins.open(conv, identity, target, why)
	}
	a.mu.Unlock()
	if err == nil && fresh {
		var i *Instance
		i, err = a.Manager.InstanceFor(ctx, conv)
		if err == nil && !a.loginIdentityCurrent(conv, identity.Project) {
			err = errors.New("The requesting chat has moved or been removed")
		}
		if err == nil {
			var tab Tab
			tab, err = i.newTab(ctx, conv, "agent", target, x)
			if err == nil && !a.loginIdentityCurrent(conv, identity.Project) {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_ = i.CloseTab(cleanup, tab.ID)
				cancel()
				err = errors.New("The requesting chat has moved or been removed")
			}
		}
		if err != nil {
			x.settle("cancelled", "about:blank")
		}
	}
	if err != nil {
		return LoginAnswer{}, err
	}
	return x.wait(ctx, a.Wait), nil
}

// AnswerLogin is called only by authenticated page routes. Return-to-agent also completes through SetControl.
func (a *ProfileAccess) AnswerLogin(ctx context.Context, id, action string) error {
	if action != "done" && action != "cancel" {
		return errors.New("Invalid login action")
	}
	x := a.Logins.find(id)
	if x == nil {
		return errors.New("Login request is no longer pending")
	}
	x.mu.Lock()
	ask, i, pending := x.ask, x.instance, x.answer.Status == "waiting"
	x.mu.Unlock()
	if !pending || i == nil {
		return errors.New("Login request is no longer pending")
	}
	a.mu.Lock()
	identity, err := a.identity(ask.Conv)
	valid := err == nil && identity.Project == ask.Project && a.ProfileOf(ask.Conv) == i.profile.ID
	a.mu.Unlock()
	if !valid {
		x.settle("cancelled", i.loginURL(ask.Tab))
		return errors.New("The requesting chat has moved or been removed")
	}
	if i.ctx.Err() != nil {
		x.settle("cancelled", "about:blank")
		if action == "cancel" {
			return nil
		}
		return errors.New("Login request is no longer pending")
	}
	if action == "cancel" {
		return i.CloseTab(ctx, ask.Tab)
	}
	return i.SetControl(ask.Tab, "agent")
}

func (i *Instance) loginURL(id string) string {
	i.mu.Lock()
	defer i.mu.Unlock()
	if p := i.pages[id]; p != nil {
		return i.redactLocked(p.URL)
	}
	return "about:blank"
}

// AllowedURL is shared by chrome_open and login requests: http(s) or about:blank. Where an address may lead
// (localhost only to the machine's dev servers) is the egress's to decide; see host.Refusal.
func AllowedURL(raw string) error {
	if host.AllowedNavigation(raw) {
		return nil
	}
	return errors.New("URL refused: use http://, https:// or about:blank (localhost: this machine's dev servers)")
}

func (i *Instance) pendingLogin(id string) *LoginAsk {
	i.mu.Lock()
	defer i.mu.Unlock()
	p := i.pages[id]
	if p == nil || p.login == nil {
		return nil
	}
	p.login.mu.Lock()
	defer p.login.mu.Unlock()
	if p.login.answer.Status != "waiting" {
		return nil
	}
	ask := p.login.ask
	return &ask
}

// AgentTabs omits person-controlled page contents while preserving tab IDs and current URLs.
func (i *Instance) AgentTabs(conv string) []Tab {
	tabs := i.Tabs(conv)
	for n := range tabs {
		if tabs[n].Control == "person" {
			tabs[n].Title = ""
			tabs[n].Dialog = nil
			tabs[n].SecretDocument = ""
		}
	}
	return tabs
}

// Startup and navigation run outside the access lock. Recheck the request's
// identity on both sides so a concurrent move cannot leave a stale login tab.
func (a *ProfileAccess) loginIdentityCurrent(conv, project string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	identity, err := a.identity(conv)
	return err == nil && identity.Project == project
}
