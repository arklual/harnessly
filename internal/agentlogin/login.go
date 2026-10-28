// Package agentlogin signs the agent CLIs in on the machine, for the hub's «Войти» buttons.
//
// Three ways, one login per machine at a time:
//
//   - Claude by link: `claude auth login` runs in a pane of the agent's own tmux server, wide enough that its sign-in
//     address is one line. The address goes to the person, who signs in on Anthropic's page and pastes the code that page
//     shows; the code is typed into the pane, and the login is proved by `claude auth status`.
//   - Codex by link: a device-code login. `codex app-server` starts it and says when the person has confirmed; where it
//     cannot be used, the screen of `codex login --device-auth` gives the same address and code.
//   - By console: a terminal tab of the runtime runs the CLI's own login, the person works in it, and the status is polled.
//
// A login ends in done, failed, expired or cancelled, and says so through Config.Emit each time its state changes.
package agentlogin

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Shell makes the commands of the account's login shell; preflight.Checker is one.
type Shell interface {
	LoginCmd(ctx context.Context, script string, extraEnv ...string) (*exec.Cmd, error)
}

// Timing is how long the login waits for each thing. Zero values are the contract's numbers.
type Timing struct {
	URLWait     time.Duration // the address on the screen: 10 s
	AppServer   time.Duration // Codex's app-server to start the login: 12 s
	VerifyWait  time.Duration // Claude to take the code: 60 s
	ScreenEvery time.Duration // how often the screen is read: 250 ms
	StatusEvery time.Duration // how often the status is asked while waiting: 3 s
	ClaudeTTL   time.Duration // 10 min
	CodexTTL    time.Duration // 15 min
	ConsoleTTL  time.Duration // 15 min
}

func (t Timing) withDefaults() Timing {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&t.URLWait, 10*time.Second)
	def(&t.AppServer, 12*time.Second)
	def(&t.VerifyWait, 60*time.Second)
	def(&t.ScreenEvery, 250*time.Millisecond)
	def(&t.StatusEvery, 3*time.Second)
	def(&t.ClaudeTTL, 10*time.Minute)
	def(&t.CodexTTL, 15*time.Minute)
	def(&t.ConsoleTTL, 15*time.Minute)
	return t
}

// Config is what a Manager works with.
type Config struct {
	Shell Shell
	// Status is how the provider stands now; a login is done when it says logged_in.
	Status func(ctx context.Context, provider string) hubproto.AgentStatus
	// Console opens a terminal tab of the runtime that runs command, and gives the path that shows it under the
	// runtime's base path. nil makes the console method fail.
	Console func(ctx context.Context, provider, command string) (string, error)
	// Emit hands a login's new state to the hub.
	Emit func(hubproto.Login)
	// Finished is told when a login has ended, so what the agent reports about its CLIs can be refreshed.
	Finished func(provider string)

	// EnvDir holds the agent's env files, which the login's commands source (a proxy is how the provider is reached).
	EnvDir string
	// Dir is where the launcher files of a pane are made.
	Dir string
	// Tmux is the tmux program ("" — tmux on the PATH); Socket the name of the agent's own server.
	Tmux   string
	Socket string
	// Version is shown to Codex's app-server as the client's version.
	Version string
	Timing  Timing
	Now     func() time.Time
}

var errCancelled = errors.New("login cancelled")

// Manager runs the logins of one machine.
type Manager struct {
	cfg Config
	ctx context.Context
	wg  sync.WaitGroup

	emitMu sync.Mutex

	mu     sync.Mutex
	active *entry
	byID   map[string]*entry
	order  []string
}

// kept is how many finished logins can still be asked about.
const kept = 16

// New is a Manager whose logins end with ctx.
func New(ctx context.Context, cfg Config) *Manager {
	cfg.Timing = cfg.Timing.withDefaults()
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Tmux == "" {
		cfg.Tmux = "tmux"
	}
	if cfg.Socket == "" {
		cfg.Socket = "harnessly-agent-login"
	}
	if cfg.Version == "" {
		cfg.Version = "1"
	}
	return &Manager{cfg: cfg, ctx: ctx, byID: map[string]*entry{}}
}

// entry is one login.
type entry struct {
	m       *Manager
	ctx     context.Context
	cancel  context.CancelCauseFunc
	started time.Time
	code    chan string

	mu       sync.Mutex
	login    hubproto.Login
	finished bool
	codeSent bool
}

// Start begins a login. The Login it returns is in state starting; the rest is told through Emit.
func (m *Manager) Start(loginID, provider, method string) (hubproto.Login, error) {
	switch provider {
	case hubproto.ProviderClaude, hubproto.ProviderCodex:
	default:
		return hubproto.Login{}, hubproto.Errf(hubproto.CodeUnsupported, "no login for %q", provider)
	}
	if method != hubproto.MethodLink && method != hubproto.MethodConsole {
		return hubproto.Login{}, hubproto.Errf(hubproto.CodeInvalid, "unknown login method %q", method)
	}
	if loginID == "" || len(loginID) > 64 {
		return hubproto.Login{}, hubproto.Errf(hubproto.CodeInvalid, "bad login id")
	}
	m.mu.Lock()
	if m.byID[loginID] != nil {
		m.mu.Unlock()
		return hubproto.Login{}, hubproto.Errf(hubproto.CodeInvalid, "login %s exists", loginID)
	}
	if m.active != nil {
		m.mu.Unlock()
		return hubproto.Login{}, hubproto.Errf(hubproto.CodeBusy, "a login for %s is already running", m.active.login.Provider)
	}
	ctx, cancel := context.WithCancelCause(m.ctx)
	e := &entry{m: m, ctx: ctx, cancel: cancel, started: m.cfg.Now(), code: make(chan string, 1),
		login: hubproto.Login{ID: loginID, Provider: provider, Method: method, State: hubproto.LoginStarting}}
	m.active = e
	m.byID[loginID] = e
	m.order = append(m.order, loginID)
	for len(m.order) > kept {
		if old := m.byID[m.order[0]]; old != nil && old != m.active {
			delete(m.byID, m.order[0])
		}
		m.order = m.order[1:]
	}
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer cancel(nil)
		switch {
		case method == hubproto.MethodConsole:
			m.console(e)
		case provider == hubproto.ProviderClaude:
			m.claudeLink(e)
		default:
			m.codexLink(e)
		}
		// A routine that returned without saying how the login ended did not know either.
		e.finish(hubproto.LoginFailed, "Вход прерван", nil)
	}()
	return e.snapshot(), nil
}

// Code hands the Claude login the code the person got on Anthropic's page.
func (m *Manager) Code(loginID, code string) error {
	e := m.entry(loginID)
	if e == nil {
		return hubproto.Errf(hubproto.CodeNotFound, "no login %s", loginID)
	}
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 512 || strings.ContainsFunc(code, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return hubproto.Errf(hubproto.CodeInvalid, "that is not a login code")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.finished {
		return hubproto.Errf(hubproto.CodeInvalid, "the login is over")
	}
	if !e.login.NeedsCode || e.codeSent {
		return hubproto.Errf(hubproto.CodeInvalid, "the login is not asking for a code")
	}
	e.codeSent = true
	e.code <- code
	return nil
}

// Cancel ends a login. One that has ended already stays as it ended.
func (m *Manager) Cancel(loginID string) error {
	e := m.entry(loginID)
	if e == nil {
		return hubproto.Errf(hubproto.CodeNotFound, "no login %s", loginID)
	}
	e.cancel(errCancelled)
	return nil
}

// Get is a login as it stands.
func (m *Manager) Get(loginID string) (hubproto.Login, bool) {
	e := m.entry(loginID)
	if e == nil {
		return hubproto.Login{}, false
	}
	return e.snapshot(), true
}

// Busy says a login is running, and for which provider.
func (m *Manager) Busy() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return "", false
	}
	return m.active.login.Provider, true
}

// Wait returns when no login is running.
func (m *Manager) Wait() { m.wg.Wait() }

func (m *Manager) entry(id string) *entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.byID[id]
}

func (e *entry) snapshot() hubproto.Login {
	e.mu.Lock()
	defer e.mu.Unlock()
	return copyLogin(e.login)
}

func copyLogin(l hubproto.Login) hubproto.Login {
	cp := func(s *string) *string {
		if s == nil {
			return nil
		}
		v := *s
		return &v
	}
	l.URL, l.UserCode, l.ConsoleURL, l.Error = cp(l.URL), cp(l.UserCode), cp(l.ConsoleURL), cp(l.Error)
	if l.ExpiresAt != nil {
		v := *l.ExpiresAt
		l.ExpiresAt = &v
	}
	if l.Status != nil {
		s := *l.Status
		l.Status = &s
	}
	return l
}

// update changes the login and tells the hub, unless it has ended.
func (e *entry) update(change func(*hubproto.Login)) {
	e.mu.Lock()
	if e.finished {
		e.mu.Unlock()
		return
	}
	change(&e.login)
	l := copyLogin(e.login)
	e.mu.Unlock()
	e.m.emit(l)
}

func (m *Manager) emit(l hubproto.Login) {
	if m.cfg.Emit == nil {
		return
	}
	m.emitMu.Lock()
	defer m.emitMu.Unlock()
	m.cfg.Emit(l)
}

// finish ends the login in a final state; the first call wins. text is the reason, shown to the person.
func (e *entry) finish(state, text string, status *hubproto.AgentStatus) {
	e.mu.Lock()
	if e.finished {
		e.mu.Unlock()
		return
	}
	e.finished = true
	l := &e.login
	l.State = state
	l.URL, l.UserCode, l.ConsoleURL, l.NeedsCode, l.ExpiresAt = nil, nil, nil, false, nil
	l.Error = nil
	if text != "" && state != hubproto.LoginDone {
		l.Error = &text
	}
	l.Status = status
	out := copyLogin(*l)
	provider := l.Provider
	e.mu.Unlock()

	e.m.emit(out)
	e.m.mu.Lock()
	if e.m.active == e {
		e.m.active = nil
	}
	e.m.mu.Unlock()
	if e.m.cfg.Finished != nil {
		e.m.cfg.Finished(provider)
	}
}

// cancelled finishes the login as cancelled when that is why its context ended.
func (e *entry) cancelled() bool {
	if e.ctx.Err() == nil {
		return false
	}
	e.finish(hubproto.LoginCancelled, "", nil)
	return true
}

// sleep waits d; false when the login was cancelled meanwhile.
func (e *entry) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-e.ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (e *entry) expiry(ttl time.Duration) (int64, time.Duration) {
	at := e.started.Add(ttl)
	return at.UnixMilli(), time.Until(at)
}

func (m *Manager) binary(provider string) string {
	if provider == hubproto.ProviderClaude {
		return "claude"
	}
	return "codex"
}

// script is what the login shell runs: the env files sourced (a proxy), the CLI found as the tabs find it, then run.
// No browser is opened on the machine and no display is reachable, since the person is somewhere else.
func (m *Manager) script(provider, run string) string {
	bin := m.binary(provider)
	return agents.EnvSourcePrefix(m.cfg.EnvDir, provider) + agents.FindBinary(bin) +
		`if [ -z "$bin" ]; then echo "` + bin + ` is not installed"; exit 127; fi
export BROWSER=
unset DISPLAY WAYLAND_DISPLAY
` + run
}

// verified is the status when the provider says the login worked, else nil.
func (m *Manager) verified(e *entry) *hubproto.AgentStatus {
	ctx, cancel := context.WithTimeout(e.ctx, 30*time.Second)
	defer cancel()
	st := m.cfg.Status(ctx, e.login.Provider)
	if st.Login == hubproto.LoggedIn {
		return &st
	}
	return nil
}

// consoleHint is what the person is told when the screen gave no address.
const consoleHint = "Не нашли ссылку входа, войдите в консоли"

// startPane runs the provider's login command in a pane.
func (m *Manager) startPane(e *entry, run string) (*pane, error) {
	provider := e.login.Provider
	cmd, err := m.cfg.Shell.LoginCmd(context.Background(), m.script(provider, run))
	if err != nil {
		return nil, err
	}
	return startPane(e.ctx, m.cfg.Tmux, m.cfg.Socket, "hl-login-"+provider, m.cfg.Dir, cmd)
}

// watch reads the pane's screen until found says it has what it looks for. It returns that screen, or why not: the
// time ran out, the program ended (its last screen and exit code are returned), or the login was cancelled.
type watchEnd int

const (
	watchFound watchEnd = iota
	watchTimeout
	watchExited
	watchCancelled
)

func (m *Manager) watch(e *entry, p *pane, wait time.Duration, found func(screen string) bool) (string, int, watchEnd) {
	deadline := time.Now().Add(wait)
	for {
		if e.ctx.Err() != nil {
			return "", 0, watchCancelled
		}
		screen, _ := p.screen(e.ctx)
		if found(screen) {
			return screen, 0, watchFound
		}
		if dead, code, _ := p.exited(e.ctx); dead {
			screen, _ = p.screen(e.ctx)
			return screen, code, watchExited
		}
		if time.Now().After(deadline) {
			return screen, 0, watchTimeout
		}
		if !e.sleep(m.cfg.Timing.ScreenEvery) {
			return "", 0, watchCancelled
		}
	}
}

// conclude ends a login whose program has ended: signed in, or not and why.
func (m *Manager) conclude(e *entry, p *pane, secret string) {
	if st := m.verified(e); st != nil {
		e.finish(hubproto.LoginDone, "", st)
		return
	}
	if e.cancelled() {
		return
	}
	var tail string
	if p != nil {
		screen, _ := p.screen(e.ctx)
		tail = scrub(LastLines(screen, 3), secret)
	}
	if tail == "" {
		e.finish(hubproto.LoginFailed, "Вход не удался", nil)
		return
	}
	e.finish(hubproto.LoginFailed, "Вход не удался: "+tail, nil)
}

// scrub makes a screen's text fit for the person: addresses (they carry the login's state) and the secret typed into
// the pane are left out, and it is short.
func scrub(text, secret string) string {
	text = anyURL.ReplaceAllString(text, "…")
	if secret != "" {
		text = strings.ReplaceAll(text, secret, "…")
	}
	if r := []rune(text); len(r) > 300 {
		text = string(r[:300]) + "…"
	}
	return text
}

// claudeLink is the Claude login by link.
func (m *Manager) claudeLink(e *entry) {
	t := m.cfg.Timing
	p, err := m.startPane(e, `exec "$bin" auth login --claudeai`)
	if err != nil {
		if !e.cancelled() {
			e.finish(hubproto.LoginFailed, "Не удалось запустить вход: "+err.Error(), nil)
		}
		return
	}
	defer p.close()

	screen, code, end := m.watch(e, p, t.URLWait, func(s string) bool { return ClaudeLoginURL(s) != "" })
	switch end {
	case watchCancelled:
		e.cancelled()
		return
	case watchExited:
		if code == 127 || strings.Contains(screen, " is not installed") {
			e.finish(hubproto.LoginFailed, "Claude Code не установлен на машине", nil)
			return
		}
		// It ended without showing an address: it may have found a login in place.
		m.conclude(e, p, "")
		return
	case watchTimeout:
		e.finish(hubproto.LoginFailed, consoleHint, nil)
		return
	}
	url := ClaudeLoginURL(screen)
	expires, left := e.expiry(t.ClaudeTTL)
	e.update(func(l *hubproto.Login) {
		l.State, l.URL, l.NeedsCode, l.ExpiresAt = hubproto.LoginURL, &url, true, &expires
	})

	ttl := time.NewTimer(left)
	defer ttl.Stop()
	tick := time.NewTicker(t.ScreenEvery)
	defer tick.Stop()
	var typed string
	for typed == "" {
		select {
		case typed = <-e.code:
		case <-ttl.C:
			e.finish(hubproto.LoginExpired, "Время на вход вышло, начните заново", nil)
			return
		case <-e.ctx.Done():
			e.cancelled()
			return
		case <-tick.C:
			// The program ended on its own: the login was completed another way.
			if dead, _, _ := p.exited(e.ctx); dead {
				m.conclude(e, p, "")
				return
			}
		}
	}
	if err := p.typeLine(e.ctx, typed); err != nil {
		if !e.cancelled() {
			e.finish(hubproto.LoginFailed, "Не удалось передать код Claude", nil)
		}
		return
	}
	e.update(func(l *hubproto.Login) { l.State, l.NeedsCode = hubproto.LoginVerifying, false })

	deadline := time.Now().Add(t.VerifyWait)
	nextStatus := time.Now().Add(t.StatusEvery)
	for {
		if e.ctx.Err() != nil {
			e.cancelled()
			return
		}
		if dead, _, _ := p.exited(e.ctx); dead {
			m.conclude(e, p, typed)
			return
		}
		if time.Now().After(nextStatus) {
			if st := m.verified(e); st != nil {
				e.finish(hubproto.LoginDone, "", st)
				return
			}
			nextStatus = time.Now().Add(t.StatusEvery)
		}
		if time.Now().After(deadline) {
			screen, _ := p.screen(e.ctx)
			text := "Claude не подтвердил вход"
			if tail := scrub(LastLines(screen, 3), typed); tail != "" {
				text += ": " + tail
			}
			e.finish(hubproto.LoginFailed, text, nil)
			return
		}
		if !e.sleep(t.ScreenEvery) {
			e.cancelled()
			return
		}
	}
}

// codexLink is the Codex device-code login.
func (m *Manager) codexLink(e *entry) {
	if m.codexAppServer(e) {
		return
	}
	m.codexScreen(e)
}

// codexAppServer runs the login through `codex app-server`. It returns false, with the login untouched, when the server
// cannot be used before it has given a code, so the screen can be tried.
func (m *Manager) codexAppServer(e *entry) bool {
	t := m.cfg.Timing
	cmd, err := m.cfg.Shell.LoginCmd(context.Background(), m.script(hubproto.ProviderCodex, `exec "$bin" app-server`))
	if err != nil {
		return false
	}
	srv, err := startAppServer(cmd)
	if err != nil {
		return false
	}
	defer srv.stop()

	ictx, cancel := context.WithTimeout(e.ctx, t.AppServer)
	defer cancel()
	if _, err := srv.call(ictx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "harnessly-agent", "version": m.cfg.Version}}, nil); err != nil {
		return e.cancelled()
	}
	if err := srv.notify("initialized"); err != nil {
		return false
	}
	raw, err := srv.call(ictx, "account/login/start", map[string]any{"type": "chatgptDeviceCode"}, nil)
	if err != nil {
		return e.cancelled()
	}
	var dev deviceLogin
	if err := json.Unmarshal(raw, &dev); err != nil || dev.Type != "chatgptDeviceCode" || dev.UserCode == "" || dev.VerificationURL == "" {
		return false
	}
	cancel()

	expires, left := e.expiry(t.CodexTTL)
	e.update(func(l *hubproto.Login) {
		l.State, l.URL, l.UserCode, l.ExpiresAt = hubproto.LoginURL, &dev.VerificationURL, &dev.UserCode, &expires
	})
	ttl := time.NewTimer(left)
	defer ttl.Stop()
	tick := time.NewTicker(t.StatusEvery)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			// Tell the server the person gave up, give it a moment to take that in, then let the process go.
			if id, err := srv.request("account/login/cancel", map[string]any{"loginId": dev.LoginID}); err == nil {
				wctx, wcancel := context.WithTimeout(context.Background(), time.Second)
				_, _ = srv.await(wctx, id, nil)
				wcancel()
			}
			e.cancelled()
			return true
		case <-ttl.C:
			e.finish(hubproto.LoginExpired, "Время на вход вышло, начните заново", nil)
			return true
		case msg, ok := <-srv.msgs:
			if !ok {
				// The server ended: it may have finished the login first.
				m.conclude(e, nil, "")
				return true
			}
			if msg.Method != "account/login/completed" {
				continue
			}
			var done loginCompleted
			_ = json.Unmarshal(msg.Params, &done)
			if done.LoginID != nil && *done.LoginID != dev.LoginID {
				continue
			}
			if !done.Success {
				text := "Вход не удался"
				if done.Error != nil && *done.Error != "" {
					text += ": " + scrub(*done.Error, "")
				}
				e.finish(hubproto.LoginFailed, text, nil)
				return true
			}
			m.conclude(e, nil, "")
			return true
		case <-tick.C:
			if st := m.verified(e); st != nil {
				e.finish(hubproto.LoginDone, "", st)
				return true
			}
		}
	}
}

// codexScreen is the Codex login read off the screen of `codex login --device-auth`.
func (m *Manager) codexScreen(e *entry) {
	t := m.cfg.Timing
	p, err := m.startPane(e, `exec "$bin" login --device-auth`)
	if err != nil {
		if !e.cancelled() {
			e.finish(hubproto.LoginFailed, "Не удалось запустить вход: "+err.Error(), nil)
		}
		return
	}
	defer p.close()

	screen, code, end := m.watch(e, p, t.URLWait, func(s string) bool { u, c := CodexDevice(s); return u != "" && c != "" })
	switch end {
	case watchCancelled:
		e.cancelled()
		return
	case watchExited:
		if code == 127 || strings.Contains(screen, " is not installed") {
			e.finish(hubproto.LoginFailed, "Codex не установлен на машине", nil)
			return
		}
		m.conclude(e, p, "")
		return
	case watchTimeout:
		e.finish(hubproto.LoginFailed, consoleHint, nil)
		return
	}
	url, userCode := CodexDevice(screen)
	expires, left := e.expiry(t.CodexTTL)
	e.update(func(l *hubproto.Login) {
		l.State, l.URL, l.UserCode, l.ExpiresAt = hubproto.LoginURL, &url, &userCode, &expires
	})

	ttl := time.NewTimer(left)
	defer ttl.Stop()
	tick := time.NewTicker(t.ScreenEvery)
	defer tick.Stop()
	nextStatus := time.Now().Add(t.StatusEvery)
	for {
		select {
		case <-e.ctx.Done():
			e.cancelled()
			return
		case <-ttl.C:
			e.finish(hubproto.LoginExpired, "Время на вход вышло, начните заново", nil)
			return
		case <-tick.C:
			if dead, _, _ := p.exited(e.ctx); dead {
				m.conclude(e, p, "")
				return
			}
			if time.Now().After(nextStatus) {
				if st := m.verified(e); st != nil {
					e.finish(hubproto.LoginDone, "", st)
					return
				}
				nextStatus = time.Now().Add(t.StatusEvery)
			}
		}
	}
}

// console is the login in a terminal tab of the runtime: the CLI's own login runs there, and the person does it by
// hand while the status is watched.
func (m *Manager) console(e *entry) {
	t := m.cfg.Timing
	provider := e.login.Provider
	if m.cfg.Console == nil {
		e.finish(hubproto.LoginFailed, "Консоль на этой машине недоступна", nil)
		return
	}
	command := "claude auth login --claudeai"
	if provider == hubproto.ProviderCodex {
		command = "codex login --device-auth"
	}
	url, err := m.cfg.Console(e.ctx, provider, agents.ForAnyShell(agents.EnvSourcePrefix(m.cfg.EnvDir, provider)+command))
	if err != nil {
		if !e.cancelled() {
			e.finish(hubproto.LoginFailed, "Не удалось открыть консоль: "+scrub(err.Error(), ""), nil)
		}
		return
	}
	expires, left := e.expiry(t.ConsoleTTL)
	e.update(func(l *hubproto.Login) {
		l.State, l.ConsoleURL, l.ExpiresAt = hubproto.LoginWaiting, &url, &expires
	})
	ttl := time.NewTimer(left)
	defer ttl.Stop()
	tick := time.NewTicker(t.StatusEvery)
	defer tick.Stop()
	for {
		select {
		case <-e.ctx.Done():
			e.cancelled()
			return
		case <-ttl.C:
			e.finish(hubproto.LoginExpired, "Время на вход вышло, начните заново", nil)
			return
		case <-tick.C:
			if st := m.verified(e); st != nil {
				e.finish(hubproto.LoginDone, "", st)
				return
			}
		}
	}
}
