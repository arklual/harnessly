package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/proc"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/tmux"
	"github.com/ly-solutions/harnessly/internal/typist"
)

// What a runtime carried by a hub (RUNTIME_HUB_URL) answers besides its own page:
//
//   - The first-run password setup is closed. The hub signs people in and hands the page a session signed with the
//     pair's secret; a password chosen here would also replace that secret in the config, and the machine agent, which
//     signs those sessions, would keep the old one.
//   - The machine agent can ask for a console: a terminal that runs one command (an agent's login), shown on a page of
//     its own (`<base>/console/<id>`) that works from the screen's text, since the chat page has no tab for a shell that
//     belongs to no chat.

//   - The machine agent says whether the machine's local policy keeps the terminal off for the hub (POST
//     /api/agent/policy), and while it does the chat refuses to type into a pane that holds no running agent and to
//     type a bash-mode line ("!…"): the route filter in the agent cannot see what a pane holds. Until the agent has said,
//     the terminal counts as off.

// hubMode says a hub carries this runtime.
func hubMode() bool { return strings.TrimSpace(os.Getenv("RUNTIME_HUB_URL")) != "" }

// hubAnalytics: the personal statistics come from the hub (RUNTIME_HUB_ANALYTICS=on) and the runtime keeps no usage
// report of its own. It needs a hub to ask, so it is off on a runtime no hub carries. The machine agent keeps
// writing the facts the hub pulls.
func hubAnalytics() bool {
	return hubMode() && strings.EqualFold(strings.TrimSpace(os.Getenv("RUNTIME_HUB_ANALYTICS")), "on")
}

// chatGuard is the chat's view of the machine's terminal policy, shared by the prompt service, the conversations and
// the route the machine agent tells it by. A runtime a hub carries starts with the terminal off, until told otherwise.
var chatGuard = chatguard.New(hubMode())

const (
	consolePrefix  = "console-"
	consoleCols    = 300 // wide enough that a login URL stays on one line
	consoleRows    = 40
	consoleTTL     = 2 * time.Hour
	consoleBodyMax = 16 << 10
	consoleTextMax = 4096
	commandMax     = 4096
	// shellReady is the time a new shell gets before a command is typed into it.
	shellReady = 400 * time.Millisecond
)

// consoleKeys are the keys the console page may press, as tmux names them.
var consoleKeys = map[string]bool{
	"Enter": true, "Escape": true, "Tab": true, "Up": true, "Down": true, "Left": true, "Right": true, "BSpace": true,
	"C-c": true, "C-d": true, "Space": true,
}

type hubConsole struct {
	tm *tmux.Tmux
	// cwd is where a console's shell starts ("": the home folder).
	cwd string
	// base is the page's base path ("" for the root).
	base    string
	agent   func(*http.Request) bool
	session func(*http.Request) bool
	// guard is told by the machine agent whether the terminal is off.
	guard *chatguard.Guard
	// ready is how long a new shell is given before the command is typed.
	ready time.Duration
	now   func() time.Time
	// busy is how many chats have an agent at work; exit ends the process (a test replaces it).
	busy func() int
	exit func(code int)
	// claims are the page session's claims (its person is "sub"); ports are the ports a console's processes listen on.
	claims func(*http.Request) map[string]any
	ports  func(name string) []int
	holds  consoleHolds
}

// hubRoutes are the routes of a runtime carried by a hub; none for any other.
func hubRoutes(d deps) []gate.Route {
	if !hubMode() {
		return nil
	}
	c := &hubConsole{tm: d.Tmux, cwd: os.Getenv("RUNTIME_WORKSPACE_DIR"), base: appBase(), agent: d.MachineAgent, session: d.SessionOK,
		guard: chatGuard, ready: shellReady, now: time.Now, busy: d.Busy, exit: os.Exit, claims: d.Claims}
	c.ports = func(name string) []int { return proc.TreeListeningPorts(c.tm.PanePID(name)) }
	return []gate.Route{{Match: c.matches, Handler: http.HandlerFunc(c.serve)}}
}

// apiPath is the request's path as the API routes know it: the alias form of the app base (`/agents/api/…`) is taken
// back to `/api/…`. "" for a path that is not the API's.
func apiPath(p string) string {
	if rest, ok := strings.CutPrefix(p, "/agents"); ok && strings.HasPrefix(rest, "/api/") {
		return rest
	}
	if strings.HasPrefix(p, "/api/") {
		return p
	}
	return ""
}

// consoleID is the id in `<id>/screen` style tails, checked to be the hex a console is given.
func validConsoleID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (c *hubConsole) pagePrefix() string { return c.base + "/console/" }

func (c *hubConsole) matches(r *http.Request) bool {
	if p := apiPath(r.URL.Path); p != "" {
		return p == "/api/auth/setup" || p == "/api/agent/terminal-tab" || p == "/api/agent/console-state" || p == "/api/agent/policy" || p == "/api/agent/load" || p == "/api/agent/restart" || strings.HasPrefix(p, "/api/console/")
	}
	return strings.HasPrefix(r.URL.Path, c.pagePrefix())
}

func (c *hubConsole) serve(w http.ResponseWriter, r *http.Request) {
	p := apiPath(r.URL.Path)
	switch {
	case p == "/api/auth/setup":
		c.setup(w, r)
	case p == "/api/agent/terminal-tab":
		c.open(w, r)
	case p == "/api/agent/console-state":
		c.consoleState(w, r)
	case p == "/api/agent/policy":
		c.policy(w, r)
	case p == "/api/agent/load":
		c.load(w, r)
	case p == "/api/agent/restart":
		c.restart(w, r)
	case strings.HasPrefix(p, "/api/console/"):
		c.api(w, r, strings.TrimPrefix(p, "/api/console/"))
	default:
		c.page(w, r, strings.TrimPrefix(r.URL.Path, c.pagePrefix()))
	}
}

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func errReply(w http.ResponseWriter, status int, msg string) {
	jsonReply(w, status, map[string]string{"error": msg})
}

// setup answers the first-run setup of a runtime a hub carries: there is none to do.
func (c *hubConsole) setup(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonReply(w, http.StatusOK, map[string]any{"needsSetup": false, "requiresAuth": false, "hostEnvLocked": false})
	case http.MethodPost:
		errReply(w, http.StatusBadRequest, "Setup already completed.")
	default:
		w.Header().Set("Allow", "GET, POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

// policy is the machine agent telling the runtime what its local policy says of the terminal: {"terminal": true|false}.
// It says it when it starts, when the policy changes and every few seconds after, so a runtime that restarted learns
// it again.
func (c *hubConsole) policy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c.agent == nil || !c.agent(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var body struct {
		Terminal *bool `json:"terminal"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, consoleBodyMax)).Decode(&body); err != nil || body.Terminal == nil {
		errReply(w, http.StatusBadRequest, `A JSON body {"terminal": true|false} is required.`)
		return
	}
	off := !*body.Terminal
	if c.guard.Off() != off {
		agent.Log("info", "hub: the terminal policy changed", agent.F{K: "terminal", V: *body.Terminal})
	}
	c.guard.Set(off)
	jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
}

// load is the machine agent asking how many chats have an agent at work: {"chatsNow": n}. The hub shows it, the drain of a
// restart waits for it to reach 0.
func (c *hubConsole) load(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c.agent == nil || !c.agent(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	n := 0
	if c.busy != nil {
		n = c.busy()
	}
	jsonReply(w, http.StatusOK, map[string]int{"chatsNow": n})
}

// restart is the machine agent asking the runtime to exit so that its service starts it again (the hub's "restart the
// runner"). The answer goes out first. tmux and the agents in it are not the runtime's children to stop.
func (c *hubConsole) restart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c.agent == nil || !c.agent(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	agent.Log("info", "hub: the runtime restarts on the hub's request")
	exit := c.exit
	if exit == nil {
		exit = os.Exit
	}
	go func() {
		time.Sleep(time.Second)
		exit(exitRestart)
	}()
}

func (c *hubConsole) sessionName(id string) string { return c.tm.Names().Prefix + consolePrefix + id }

// open makes a console running a command, for the machine agent.
func (c *hubConsole) open(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if c.agent == nil || !c.agent(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	var body struct {
		Command string `json:"command"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, consoleBodyMax)).Decode(&body); err != nil {
		errReply(w, http.StatusBadRequest, "A JSON body with a command is required.")
		return
	}
	command := strings.TrimSpace(body.Command)
	if command == "" || len(command) > commandMax || strings.ContainsFunc(command, func(r rune) bool { return unicode.IsControl(r) }) {
		errReply(w, http.StatusBadRequest, "The command is empty, too long or has control characters.")
		return
	}
	c.sweep()
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		errReply(w, http.StatusInternalServerError, "No random id.")
		return
	}
	id := hex.EncodeToString(raw)
	name := c.sessionName(id)
	if err := c.tm.CreateSession(name, consoleCols, consoleRows, c.tm.ResolveExistingDir(c.cwd)); err != nil {
		agent.Log("warn", "hub: console not made", agent.F{K: "err", V: err.Error()})
		errReply(w, http.StatusInternalServerError, "The terminal could not be started.")
		return
	}
	time.Sleep(c.ready)
	if err := typist.Write(r.Context(), c.tm, name, func(held context.Context) error { return c.tm.With(held).SendLiteralThenEnter(name, command) }); err != nil {
		c.tm.KillSession(name)
		errReply(w, http.StatusInternalServerError, "The command could not be typed.")
		return
	}
	jsonReply(w, http.StatusOK, map[string]string{"tabId": id, "url": "/console/" + id})
}

// sweep ends the consoles that outlived their login.
func (c *hubConsole) sweep() {
	prefix := c.tm.Names().Prefix + consolePrefix
	for _, name := range c.tm.ListSessions() {
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		born := c.tm.PaneDetail(name).SessionCreated
		if born > 0 && c.now().Sub(time.Unix(born, 0)) > consoleTTL {
			c.tm.KillSession(name)
		}
	}
	c.holds.drop(func(id string) bool { return c.tm.HasSession(c.sessionName(id)) }, c.now())
}

// api is `/api/console/<id>/screen` (GET), `/api/console/<id>/keys` (POST) and `/api/console/<id>/callback` (POST).
func (c *hubConsole) api(w http.ResponseWriter, r *http.Request, tail string) {
	if !c.session(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	id, what, _ := strings.Cut(tail, "/")
	if !validConsoleID(id) {
		errReply(w, http.StatusNotFound, "Not found")
		return
	}
	name := c.sessionName(id)
	if !c.claim(id, r) {
		errReply(w, http.StatusForbidden, "This terminal belongs to another person.")
		return
	}
	switch {
	case what == "screen" && r.Method == http.MethodGet:
		if !c.tm.HasSession(name) {
			jsonReply(w, http.StatusOK, map[string]any{"alive": false, "screen": "", "attempt": c.attempt(id)})
			return
		}
		out, err := c.tm.Command(name, "capture-pane", "-p", "-J", "-t", name)
		if err != nil {
			jsonReply(w, http.StatusOK, map[string]any{"alive": false, "screen": "", "attempt": c.attempt(id)})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"alive": true, "screen": strings.TrimRight(tmux.StripControlChars(out), " \n\t"),
			"callback": c.callbackState(id, name), "attempt": c.attempt(id)})
	case what == "keys" && r.Method == http.MethodPost:
		c.keys(w, r, name)
	case what == "callback" && r.Method == http.MethodPost:
		c.callback(w, r, id, name)
	case what == "cancel" && r.Method == http.MethodPost:
		c.cancel(w, r, id, name)
	default:
		errReply(w, http.StatusNotFound, "Not found")
	}
}

// keys types text and presses named keys in a console: {"text": "…", "keys": ["Enter"]}.
func (c *hubConsole) keys(w http.ResponseWriter, r *http.Request, name string) {
	if sameorigin.CrossSite(r) {
		errReply(w, http.StatusForbidden, "Cross-site request refused.")
		return
	}
	var body struct {
		Text string   `json:"text"`
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, consoleBodyMax)).Decode(&body); err != nil || len(body.Text) > consoleTextMax || len(body.Keys) > 8 {
		errReply(w, http.StatusBadRequest, "Bad keys.")
		return
	}
	for _, k := range body.Keys {
		if !consoleKeys[k] {
			errReply(w, http.StatusBadRequest, "Unknown key.")
			return
		}
	}
	if !c.tm.HasSession(name) {
		errReply(w, http.StatusGone, "The terminal has ended.")
		return
	}
	// The text and its keys go in as one writer of the pane: nothing else types between them.
	textFailed := false
	err := typist.Write(r.Context(), c.tm, name, func(held context.Context) error {
		keys := c.tm.With(held)
		if body.Text != "" {
			if err := keys.SendLiteral(name, body.Text); err != nil {
				textFailed = true
				return err
			}
		}
		for _, k := range body.Keys {
			if err := keys.SendRawKeys(name, k); err != nil {
				return err
			}
		}
		return nil
	})
	var busy *typist.BusyError
	switch {
	case errors.As(err, &busy) || typist.IsSlow(err):
		errReply(w, http.StatusServiceUnavailable, "The terminal is busy; try again in a moment.")
		return
	case err != nil && textFailed:
		errReply(w, http.StatusInternalServerError, "The text could not be typed.")
		return
	case err != nil:
		errReply(w, http.StatusInternalServerError, "The key could not be pressed.")
		return
	}
	jsonReply(w, http.StatusOK, map[string]bool{"ok": true})
}

// page serves the console's page: it polls the screen and sends what the person types.
func (c *hubConsole) page(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		errReply(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !c.session(r) {
		errReply(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	if !validConsoleID(strings.TrimSuffix(id, "/")) {
		errReply(w, http.StatusNotFound, "Not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'")
	_, _ = io.WriteString(w, consolePage())
}
