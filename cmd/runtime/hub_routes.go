package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/tmux"
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
}

// hubRoutes are the routes of a runtime carried by a hub; none for any other.
func hubRoutes(d deps) []gate.Route {
	if !hubMode() {
		return nil
	}
	c := &hubConsole{tm: d.Tmux, cwd: os.Getenv("RUNTIME_WORKSPACE_DIR"), base: appBase(), agent: d.MachineAgent, session: d.SessionOK,
		guard: chatGuard, ready: shellReady, now: time.Now}
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
		return p == "/api/auth/setup" || p == "/api/agent/terminal-tab" || p == "/api/agent/policy" || strings.HasPrefix(p, "/api/console/")
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
	case p == "/api/agent/policy":
		c.policy(w, r)
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
	if err := c.tm.SendLiteralThenEnter(name, command); err != nil {
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
}

// api is `/api/console/<id>/screen` (GET) and `/api/console/<id>/keys` (POST).
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
	switch {
	case what == "screen" && r.Method == http.MethodGet:
		if !c.tm.HasSession(name) {
			jsonReply(w, http.StatusOK, map[string]any{"alive": false, "screen": ""})
			return
		}
		out, err := c.tm.Command(name, "capture-pane", "-p", "-J", "-t", name)
		if err != nil {
			jsonReply(w, http.StatusOK, map[string]any{"alive": false, "screen": ""})
			return
		}
		jsonReply(w, http.StatusOK, map[string]any{"alive": true, "screen": strings.TrimRight(tmux.StripControlChars(out), " \n\t")})
	case what == "keys" && r.Method == http.MethodPost:
		c.keys(w, r, name)
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
	if body.Text != "" {
		if err := c.tm.SendLiteral(name, body.Text); err != nil {
			errReply(w, http.StatusInternalServerError, "The text could not be typed.")
			return
		}
	}
	for _, k := range body.Keys {
		if err := c.tm.SendRawKeys(name, k); err != nil {
			errReply(w, http.StatusInternalServerError, "The key could not be pressed.")
			return
		}
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

// consoleTexts are the page's words, in the runtime's language.
func consoleTexts() map[string]string {
	return map[string]string{
		"title":   locale.Pick("Machine terminal", "Терминал машины"),
		"hint":    locale.Pick("Sign in to the agent here. A link in the output opens with a click; type what it asks for below.", "Войдите в агента здесь. Ссылка в выводе открывается кликом, то, что просят ввести, вводите ниже."),
		"input":   locale.Pick("Type here and press Enter", "Введите здесь и нажмите Enter"),
		"send":    locale.Pick("Send", "Отправить"),
		"ended":   locale.Pick("The terminal has ended.", "Сеанс завершён."),
		"offline": locale.Pick("No connection to the machine…", "Нет связи с машиной…"),
	}
}

// consolePage is the console's page. It holds no data of the machine: it asks for the screen and shows it as text, with
// the links made clickable from the text node by node, never as markup.
func consolePage() string {
	t := consoleTexts()
	raw, _ := json.Marshal(t)
	texts := strings.ReplaceAll(string(raw), "</", `<\/`)
	return `<!doctype html>
<html lang="` + locale.Pick("en", "ru") + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>` + html.EscapeString(t["title"]) + `</title>
<style>
:root{color-scheme:light dark;--bg:#fff;--fg:#1c1c1e;--mute:#6b6b70;--line:#d9d9de;--panel:#f5f5f7;--accent:#2563eb}
@media (prefers-color-scheme:dark){:root{--bg:#121214;--fg:#ececf0;--mute:#9a9aa3;--line:#2e2e34;--panel:#1b1b1f;--accent:#7aa2ff}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.45 system-ui,-apple-system,"Segoe UI",sans-serif;display:grid;grid-template-rows:auto 1fr auto;height:100dvh}
header{padding:12px 16px;border-bottom:1px solid var(--line)}
h1{margin:0 0 2px;font-size:15px;font-weight:600}
header p{margin:0;color:var(--mute);font-size:13px}
#screen{margin:0;padding:12px 16px;overflow:auto;font:13px/1.4 ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;white-space:pre-wrap;overflow-wrap:anywhere;background:var(--panel)}
#screen a{color:var(--accent)}
footer{border-top:1px solid var(--line);padding:10px 16px;display:grid;gap:8px}
form{display:flex;gap:8px}
input{flex:1;min-width:0;font:inherit;padding:8px 10px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg)}
button{font:inherit;padding:8px 12px;border:1px solid var(--line);border-radius:8px;background:var(--bg);color:var(--fg);cursor:pointer}
button.primary{background:var(--accent);border-color:var(--accent);color:#fff}
.keys{display:flex;flex-wrap:wrap;gap:6px}
.keys button{padding:5px 10px;font-size:13px}
#state{color:var(--mute);font-size:13px;min-height:1.2em}
</style>
</head>
<body>
<header><h1>` + html.EscapeString(t["title"]) + `</h1><p>` + html.EscapeString(t["hint"]) + `</p></header>
<pre id="screen" aria-live="polite"></pre>
<footer>
<form id="form" autocomplete="off"><input id="text" type="text" spellcheck="false" autocapitalize="off" autocorrect="off" placeholder="` + html.EscapeString(t["input"]) + `" aria-label="` + html.EscapeString(t["input"]) + `"><button class="primary" type="submit">` + html.EscapeString(t["send"]) + `</button></form>
<div class="keys" id="keys"></div>
<div id="state"></div>
</footer>
<script>
(function () {
  var T = ` + texts + `;
  var m = location.pathname.match(/^(.*)\/console\/([0-9a-f]{16})\/?$/);
  if (!m) return;
  var api = m[1] + '/api/console/' + m[2];
  var screen = document.getElementById('screen'), state = document.getElementById('state');
  var text = document.getElementById('text'), last = null, ended = false;
  function show(t) {
    if (t === last) return;
    last = t;
    var stick = screen.scrollHeight - screen.scrollTop - screen.clientHeight < 40;
    screen.textContent = '';
    var re = /https?:\/\/[^\s]+/g, pos = 0, hit;
    while ((hit = re.exec(t))) {
      screen.append(t.slice(pos, hit.index));
      var a = document.createElement('a');
      a.href = hit[0]; a.textContent = hit[0]; a.target = '_blank'; a.rel = 'noopener noreferrer';
      screen.append(a);
      pos = hit.index + hit[0].length;
    }
    screen.append(t.slice(pos));
    if (stick) screen.scrollTop = screen.scrollHeight;
  }
  function poll() {
    if (ended || document.hidden) return;
    fetch(api + '/screen', { credentials: 'same-origin', cache: 'no-store' }).then(function (r) {
      if (!r.ok) throw new Error(r.status);
      return r.json();
    }).then(function (d) {
      show(d.screen || '');
      ended = !d.alive;
      state.textContent = ended ? T.ended : '';
    }).catch(function () { state.textContent = T.offline; });
  }
  function send(body) {
    if (ended) return;
    fetch(api + '/keys', { method: 'POST', credentials: 'same-origin', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
      .then(function () { setTimeout(poll, 150); }).catch(function () { state.textContent = T.offline; });
  }
  document.getElementById('form').addEventListener('submit', function (e) {
    e.preventDefault();
    send({ text: text.value, keys: ['Enter'] });
    text.value = '';
  });
  [['Enter', 'Enter'], ['Esc', 'Escape'], ['Tab', 'Tab'], ['↑', 'Up'], ['↓', 'Down'], ['←', 'Left'], ['→', 'Right'], ['Ctrl+C', 'C-c']].forEach(function (k) {
    var b = document.createElement('button');
    b.type = 'button'; b.textContent = k[0];
    b.addEventListener('click', function () { send({ keys: [k[1]] }); text.focus(); });
    document.getElementById('keys').append(b);
  });
  document.addEventListener('visibilitychange', poll);
  setInterval(poll, 800);
  poll();
  text.focus();
})();
</script>
</body>
</html>
`
}
