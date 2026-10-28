package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/agents/codex"
	"github.com/ly-solutions/harnessly/internal/coreapi/layoutapi"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/launch"
	"github.com/ly-solutions/harnessly/internal/sessions"
	"github.com/ly-solutions/harnessly/internal/sleep"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// goldenTmux is tmux as recorded sessions, in the shape testdata/launch-routes/stub-tmux.mjs records them.
type goldenTmux struct {
	mu       sync.Mutex
	names    *tmux.Tmux
	home     string
	sessions map[string]bool
	cwd      map[string]string
	panes    map[string][][2]string
	commands map[string]string
	created  map[string]int64
	calls    [][]string
}

func (g *goldenTmux) rec(c ...string) {
	g.mu.Lock()
	g.calls = append(g.calls, c)
	g.mu.Unlock()
}

func (g *goldenTmux) CreateSession(name string, cols, rows int, cwd string) error {
	g.rec("create", name, strconv.Itoa(cols), strconv.Itoa(rows), cwd)
	g.mu.Lock()
	g.sessions[name] = true
	g.mu.Unlock()
	return nil
}
func (g *goldenTmux) KillSession(name string) { g.rec("kill", name) }
func (g *goldenTmux) HasSession(name string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sessions[name]
}
func (g *goldenTmux) SendKeys(name, line string) error { g.rec("send", name, line); return nil }
func (g *goldenTmux) SendKeysSeparated(name, line string) error {
	g.rec("sendSeparated", name, line)
	return nil
}
func (g *goldenTmux) SendBracketedPaste(name, text string) error {
	g.rec("paste", name, text)
	return nil
}
func (g *goldenTmux) SendKeySequence(name string, keys ...string) error {
	g.rec(append([]string{"keys", name}, keys...)...)
	return nil
}
func (g *goldenTmux) ListSessions() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for s := range g.sessions {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
func (g *goldenTmux) Names() tmux.Names                 { return g.names.Names() }
func (g *goldenTmux) SessionName(w, p, t string) string { return g.names.SessionName(w, p, t) }
func (g *goldenTmux) ParseSessionName(n string) (string, string, string, bool) {
	return g.names.ParseSessionName(n)
}
func (g *goldenTmux) PaneShellName(id string, legacy bool) (string, bool) {
	return g.names.PaneShellName(id, legacy)
}
func (g *goldenTmux) ResolveExistingDir(p string) string {
	for c := p; c != ""; {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
		parent := filepath.Dir(c)
		if parent == c {
			break
		}
		c = parent
	}
	return g.home
}
func (g *goldenTmux) SessionCwd(name string) string { return g.cwd[name] }
func (g *goldenTmux) PaneCurrentCommand(name string) string {
	if c, ok := g.commands[name]; ok {
		return c
	}
	return "bash"
}
func (g *goldenTmux) PanePID(string) int                         { return 0 }
func (g *goldenTmux) Capture(string) (string, bool)              { return "line\n", true }
func (g *goldenTmux) CheckTerminalProcess(string) (bool, string) { return true, "bash" }
func (g *goldenTmux) KillServer()                                {}
func (g *goldenTmux) ApplyConfig(string)                         {}
func (g *goldenTmux) CwdAndLastCommand(name string) (string, *string) {
	return g.cwd[name], nil
}
func (g *goldenTmux) SessionsOfPane(pane, socket string) []string {
	var out []string
	for _, p := range g.panes[pane] {
		if socket == "" || p[0] == socket {
			out = append(out, p[1])
		}
	}
	return out
}

// goldenKeeper is the status keeper of testdata/launch-routes/stub-status.mjs: registrations and launches recorded.
type goldenKeeper struct {
	t       *goldenTmux
	sources []status.AgentSource
}

func entryJSON(fields map[string]any) string {
	raw, _ := json.Marshal(fields)
	return string(raw)
}

func optJSON(o status.Opt[string], fields map[string]any, k string) {
	if !o.Set {
		return
	}
	if o.Has() {
		fields[k] = o.Value()
	} else {
		fields[k] = nil
	}
}

func (k *goldenKeeper) RegisterTab(tabID string, e status.Entry) {
	f := map[string]any{"cliState": e.CliState, "workspaceId": e.WorkspaceID, "tabName": e.TabName, "tmuxSession": e.TmuxSession}
	optJSON(e.PanelType, f, "panelType")
	optJSON(e.AgentProviderID, f, "agentProviderId")
	optJSON(e.AgentSessionID, f, "agentSessionId")
	k.t.rec("register", tabID, entryJSON(f))
}
func (k *goldenKeeper) MarkAgentLaunch(tabID string, reset bool) {
	r := ""
	if reset {
		r = "reset"
	}
	k.t.rec("launch", tabID, r)
}
func (k *goldenKeeper) AgentSources() []status.AgentSource { return k.sources }

// layoutStatus is the same keeper as the layout routes see it.
type layoutStatus struct{ k *goldenKeeper }

func (s layoutStatus) RegisterTab(tabID string, e layoutapi.TabEntry) {
	f := map[string]any{"cliState": e.CliState, "workspaceId": e.WorkspaceID, "tabName": e.TabName, "tmuxSession": e.TmuxSession,
		"panelType": e.PanelType, "agentSessionId": e.AgentSessionID}
	if e.AgentProviderID != "" {
		f["agentProviderId"] = e.AgentProviderID
	}
	s.k.t.rec("register", tabID, entryJSON(f))
}
func (s layoutStatus) RemoveTab(string)                  {}
func (s layoutStatus) MarkAgentLaunch(id string, r bool) { s.k.MarkAgentLaunch(id, r) }
func (s layoutStatus) HooksMissing(string) bool          { return false }
func (s layoutStatus) AgentTab(string) string            { return "" }
func (s layoutStatus) Rescan()                           {}

type goldenSync struct{}

func (goldenSync) Broadcast(*jsvalue.Object) {}
func (goldenSync) ClientCount() int          { return 0 }

// goldenAgents are the core's agents with availability from the golden's list of missing CLIs.
type goldenAgents struct {
	*coreAgents
	missing *[]string
}

func (g goldenAgents) Availability(panelType string) *layoutapi.Unavailable {
	for _, m := range *g.missing {
		if m == panelType {
			p := agents.ProviderOfPanel(panelType)
			var suggested any
			if p == agents.Claude {
				suggested = "claude"
			}
			return &agents.Unavailable{Code: "agent-not-installed", ProviderID: p, ProviderDisplayName: agents.DisplayName(p), PanelType: panelType, SuggestedCommand: suggested}
		}
	}
	return nil
}

// normalCalls makes a registration comparable: its fields as a map (the key order differs between the call sites).
func normalCalls(calls [][]string) []string {
	out := make([]string, len(calls))
	for i, c := range calls {
		if c[0] == "register" {
			var m map[string]any
			_ = json.Unmarshal([]byte(c[2]), &m)
			delete(m, "lastEvent")
			delete(m, "eventSeq")
			raw, _ := json.Marshal(m)
			c = []string{c[0], c[1], string(raw)}
		}
		raw, _ := json.Marshal(c)
		out[i] = string(raw)
	}
	return out
}

// The launch routes, the core's agent tab start and the startup resume answer as the previous runtime's own handlers
// did on the same files (testdata/launch-routes-golden.json from testdata/launch-routes/gen.mjs): statuses, the headers
// a caller acts on, bodies, what was typed into tmux and told the keeper, and the files left.
func TestLaunchRoutesMatchThePreviousRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/launch-routes-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Token    string
		Port     int
		Fixtures map[string]string
		Outside  map[string]string
		Requests []struct {
			Setup         map[string][]string
			Method, URL   string
			Headers       map[string]string
			Body          *string
			Status        int
			AnswerHeaders map[string]string
			Answer        string
			Scrubbed      bool
			Calls         [][]string
		}
		Resume struct {
			Resumed int
			Calls   [][]string
		}
		Files map[string]string
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	data := filepath.Join(home, ".acme-ai")
	projects := filepath.Join(root, "projects")
	for _, d := range []string{filepath.Join(projects, "proj-a"), filepath.Join(projects, "proj-b"), filepath.Join(projects, "proj-c"),
		filepath.Join(root, "outside"), filepath.Join(root, "dist"), filepath.Join(data, "bin")} {
		_ = os.MkdirAll(d, 0o755)
	}
	flat := regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(root, "-")
	unscrub := strings.NewReplacer("$DATA", data, "$ROOT", root, "$FLATROOT", flat).Replace
	uuidName := regexp.MustCompile(`initial-[0-9a-f-]{36}\.txt`)
	scrub := func(s string) string {
		return uuidName.ReplaceAllString(strings.NewReplacer(data, "$DATA", root, "$ROOT", flat, "$FLATROOT").Replace(s), "initial-UUID.txt")
	}
	write := func(file, content string, mode os.FileMode) {
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	for name, content := range golden.Fixtures {
		write(filepath.Join(data, name), unscrub(content), 0o600)
	}
	for name, content := range golden.Outside {
		write(unscrub(name), unscrub(content), 0o700)
	}
	write(filepath.Join(root, "dist", "eval-runner.js"), "", 0o600)
	write(filepath.Join(data, "bin", "prompt-editor"), "#!/bin/sh\n", 0o700)
	write(filepath.Join(data, "codex-launcher.js"), "// launcher\n", 0o700)

	a, b := filepath.Join(projects, "proj-a"), filepath.Join(projects, "proj-b")
	tm := &goldenTmux{names: tmux.New(tmux.Names{Socket: "unused", Prefix: "tk-", LegacyPrefix: "pt-"}), home: home,
		sessions: map[string]bool{}, cwd: map[string]string{"tk-ws-a-pane-a-tab-2": a, "tk-ws-a-pane-a-tab-3": b},
		panes:    map[string][][2]string{"%3": {{"main", "tk-ws-a-pane-a-tab-2"}}, "%4": {{"main", "tk-ws-a-pane-a-tab-1"}, {"old", "tk-ws-a-pane-a-tab-3"}}},
		commands: map[string]string{"tk-ws-a-pane-a-tab-5": "claude", "tk-ws-a-pane-a-tab-6": "vim"},
		created:  map[string]int64{"tk-ws-a-pane-a-tab-1": 1790000000}}
	for _, n := range []string{"1", "2", "3", "5", "6"} {
		tm.sessions["tk-ws-a-pane-a-tab-"+n] = true
	}
	sid, sid2 := "425055ec-bbc8-4e3c-87d0-36fa74c1cccb", "019a0c11-2222-7333-8444-555555555555"
	keeper := &goldenKeeper{t: tm, sources: []status.AgentSource{
		{TabID: "tab-2", TmuxSession: "tk-ws-a-pane-a-tab-2", CliState: "idle", AgentSessionID: sid, Provider: "claude", PanelType: "claude-code"},
		{TabID: "tab-3", TmuxSession: "tk-ws-a-pane-a-tab-3", CliState: "busy", AgentSessionID: sid2, Provider: "codex", PanelType: "codex-cli"},
	}}
	fixed := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	store := state.New(state.Config{DataDir: data, Tmux: tm, Home: home, MetaKey: "meta", NewID: sequentialIDs(), Now: func() time.Time { return fixed }})
	core := newCoreAgents(coreAgentsConfig{DataDir: data, Home: home, ConfigFile: filepath.Join(data, "config.json"),
		ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex"), DefaultsKey: "launchDefaults",
		EditorScript: filepath.Join(data, "bin", "prompt-editor"), HumanEditorVar: "ACME_HUMAN_EDITOR", Port: golden.Port,
		MCP: codex.MCP{Server: "acme-preview", TokenEnv: "ACME_PREVIEW_TOKEN"}, MCPPath: "/api/cli/preview/mcp", TokenHeaders: []string{"x-cli-token", "x-old-token"}})
	var missing []string
	ag := goldenAgents{coreAgents: core, missing: &missing}
	starter := launch.New(launch.Config{Store: store, Lines: launchLines{core}, Availability: ag.Availability,
		Keeper: func() launch.Keeper { return keeper }, WorkspaceRoot: projects, Home: home, DataDir: data, DataDirLabel: "~/.acme-ai",
		EnvPrefix: "ACME_", PromptDir: filepath.Join(data, "launch-prompts"), Now: func() float64 { return float64(fixed.UnixMilli()) }})
	core.starter, core.launchOn = starter, func() bool { return true }
	submitted := func(session, text string) error { tm.rec("submit", session, text); return nil }
	inbox := &launch.Inbox{Starter: starter, Tmux: tm, Submit: submitted}
	cookie := func(r *http.Request) string {
		c, err := r.Cookie("session-token")
		if err != nil {
			return ""
		}
		return c.Value
	}
	d := &layoutapi.Deps{Store: store, Tmux: tm, Status: layoutStatus{keeper}, Agents: ag, Sync: goldenSync{},
		Session:  func(r *http.Request) bool { return cookie(r) == "good" || cookie(r) == "agent" },
		CLIToken: func(r *http.Request) bool { return r.Header.Get("X-Cli-Token") == golden.Token }, Home: home}
	coreRoute, _ := layoutapi.Routes(d)
	launchRoute, _ := layoutapi.LaunchRoutes(d, &layoutapi.Launch{Starter: starter, Inbox: inbox, Headlines: &launch.Headlines{},
		MachineAgent: func(r *http.Request) bool {
			return strings.HasPrefix(r.RemoteAddr, "127.0.0.1:") && cookie(r) == "agent"
		},
		SessionCreated: func(s string) int64 { return tm.created[s] }, CodexArgs: core.CodexArgs,
		EvalRuns: filepath.Join(data, "eval", "runs"), EvalRunner: filepath.Join(root, "dist", "eval-runner.js"), Node: "node",
		SideChatRuntime: filepath.Join(home, ".config", "acme", "runtime.json"),
		Sources:         func() []status.AgentSource { return keeper.sources },
		ProjectDir:      func(cwd string) string { return filepath.Join(home, ".claude", "projects", sessions.ProjectName(cwd)) }})

	keep := []string{"content-type", "etag", "allow", "cache-control", "content-length", "x-accel-buffering"}
	lastEtag := ""
	for i, g := range golden.Requests {
		if g.Setup != nil {
			missing = g.Setup["missing"]
			continue
		}
		var body io.Reader
		if g.Body != nil {
			body = strings.NewReader(unscrub(*g.Body))
		}
		req := httptest.NewRequest(g.Method, unscrub(g.URL), body)
		req.RemoteAddr = "127.0.0.1:40000"
		for k, v := range g.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			req.Header.Set(k, v)
		}
		tm.mu.Lock()
		before := len(tm.calls)
		tm.mu.Unlock()
		rec := httptest.NewRecorder()
		switch {
		case launchRoute.Match(req):
			launchRoute.Handler.ServeHTTP(rec, req)
		case coreRoute.Match(req):
			coreRoute.Handler.ServeHTTP(rec, req)
		default:
			rec.WriteHeader(404)
		}
		if strings.Contains(g.URL, "/workspace") || strings.Contains(g.URL, "/tabs?") {
			time.Sleep(800 * time.Millisecond)
		}
		res := rec.Result()
		got, _ := io.ReadAll(res.Body)
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		name := strconv.Itoa(i) + " " + g.Method + " " + g.URL
		if res.StatusCode != g.Status || previousAnswer(scrub(string(got))) != g.Answer {
			t.Errorf("%s:\n got %d %s\nwant %d %s", name, res.StatusCode, scrub(string(got)), g.Status, g.Answer)
		}
		for _, k := range keep {
			// A body that names a path or a product name differs in length and tag.
			if (k == "etag" || k == "content-length") && (g.Scrubbed || res.StatusCode == 304) {
				continue
			}
			gv := scrub(res.Header.Get(k))
			if k == "content-length" && gv == "" && len(got) == 0 && res.StatusCode != 304 && res.StatusCode != 204 {
				gv = "0" // net/http sends it for an empty answer; the recorder does not
			}
			if wv := g.AnswerHeaders[k]; gv != wv {
				t.Errorf("%s: header %s %q, want %q", name, k, gv, wv)
			}
		}
		tm.mu.Lock()
		calls := append([][]string(nil), tm.calls[before:]...)
		tm.mu.Unlock()
		for _, c := range calls {
			for j := range c {
				c[j] = previousLine(scrub(c[j]))
			}
		}
		if gc, wc := normalCalls(calls), normalCalls(g.Calls); strings.Join(gc, "\n") != strings.Join(wc, "\n") {
			t.Errorf("%s: calls\n got %s\nwant %s", name, strings.Join(gc, "\n     "), strings.Join(wc, "\n     "))
		}
	}

	rc := newResumeConfig(home, tm, func(string) (bool, bool) { return true, false }, sleep.ReadConfig(func(string) string { return "" }, sleep.EnvNames{}),
		func() time.Time { return fixed })
	rc.ChildPIDs, rc.Cmdline = nil, nil
	tm.mu.Lock()
	before := len(tm.calls)
	tm.mu.Unlock()
	targets := starter.ResumeTargets(rc)
	starter.ExecuteResume(rc, targets)
	tm.mu.Lock()
	calls := append([][]string(nil), tm.calls[before:]...)
	tm.mu.Unlock()
	for _, c := range calls {
		for j := range c {
			c[j] = previousLine(scrub(c[j]))
		}
	}
	gc, wc := normalCalls(calls), normalCalls(golden.Resume.Calls)
	sort.Strings(gc)
	sort.Strings(wc)
	if len(targets) != golden.Resume.Resumed || strings.Join(gc, "\n") != strings.Join(wc, "\n") {
		t.Errorf("resume: %d targets\n got %s\nwant %d\n     %s", len(targets), strings.Join(gc, "\n     "), golden.Resume.Resumed, strings.Join(wc, "\n     "))
	}

	// The workspace prompt files are the old runtime's to write; the scripts are compared elsewhere.
	skip := regexp.MustCompile(`^(workspaces/[^/]+/(claude|codex)-prompt\.md|bin/.*|codex-launcher\.js)$`)
	left := map[string]string{}
	_ = filepath.WalkDir(data, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		rel := scrub(strings.TrimPrefix(p, data+"/"))
		if skip.MatchString(rel) {
			return nil
		}
		content, _ := os.ReadFile(p)
		left[rel] = scrub(string(content))
		if strings.HasSuffix(rel, "/stop") {
			left[rel] = ""
		}
		return nil
	})
	for name, want := range golden.Files {
		if skip.MatchString(name) {
			continue
		}
		if got, ok := left[name]; !ok || got != want {
			t.Errorf("file %s:\n got %s\nwant %s", name, got, want)
		}
		delete(left, name)
	}
	for name := range left {
		t.Errorf("file %s is left here only", name)
	}
}

// sequentialIDs are the golden's nanoid stub: id0001, id0002, …
func sequentialIDs() func() string {
	n := 0
	return func() string {
		n++
		s := strconv.FormatInt(int64(n), 36)
		return "id" + strings.Repeat("0", max(0, 4-len(s))) + s
	}
}

// The previous runtime typed its lines for bash; this one types the same line for any shell (`sh -c '…; exec env …'`,
// the prompt through printf). previousLine turns a line of this runtime back into the previous one's shape, so the
// recordings still compare.
var printfArg = regexp.MustCompile(`"\$\(printf %b '((?:[^']|'\\'')*)'\)"`)

func previousLine(s string) string {
	inner, ok := agents.UnwrapAnyShell(s)
	if !ok {
		return s
	}
	inner = strings.Replace(inner, "exec env ", "", 1)
	return printfArg.ReplaceAllStringFunc(inner, func(m string) string {
		return ansiCQuote(printfDecode(strings.ReplaceAll(printfArg.FindStringSubmatch(m)[1], `'\''`, "'")))
	})
}

// previousAnswer is previousLine for the command of a launch-command answer.
func previousAnswer(s string) string {
	var a map[string]any
	if !strings.HasPrefix(s, `{"command":"`) || json.Unmarshal([]byte(s), &a) != nil || len(a) != 1 {
		return s
	}
	line, _ := a["command"].(string)
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(map[string]string{"command": previousLine(line)}) != nil {
		return s
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// printfDecode reads the escapes agents.PrintfQuote writes.
func printfDecode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '0':
			n, _ := strconv.ParseUint(s[i+1:i+4], 8, 8)
			b.WriteByte(byte(n))
			i += 3
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// ansiCQuote is the previous runtime's $'…'.
func ansiCQuote(s string) string {
	var b strings.Builder
	b.WriteString("$'")
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\'':
			b.WriteString(`\'`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			b.WriteString(`\x` + strconv.FormatInt(int64(r)+0x100, 16)[1:])
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString("'")
	return b.String()
}
