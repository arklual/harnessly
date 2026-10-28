package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/chatguard"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

func TestHubRoutesOnlyForAHubsRuntime(t *testing.T) {
	t.Setenv("RUNTIME_HUB_URL", "")
	if hubMode() || hubRoutes(deps{}) != nil {
		t.Fatal("a runtime no hub carries has hub routes")
	}
	t.Setenv("RUNTIME_HUB_URL", "https://hub.example.com")
	if !hubMode() || len(hubRoutes(deps{})) != 1 {
		t.Fatal("a hub's runtime has none")
	}
}

// consoleRig is a console over a real tmux server of its own; a request is a page session unless it says otherwise.
func consoleRig(t *testing.T) (*hubConsole, *tmux.Tmux) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	home := t.TempDir()
	socket := "hs-test-console-" + strconv.Itoa(os.Getpid())
	shell := "/bin/sh"
	if _, err := os.Stat("/bin/bash"); err == nil {
		shell = "/bin/bash"
	}
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, ConfigFile: "/dev/null", Home: home,
		Env: []string{"HOME=" + home, "SHELL=" + shell, "PS1=$ "}})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	c := &hubConsole{tm: tm, cwd: home, base: "/m/abcdefghijklmnopqrst", ready: 300 * time.Millisecond, now: time.Now,
		agent:   func(r *http.Request) bool { return r.Header.Get("X-Test-Agent") == "yes" },
		session: func(r *http.Request) bool { return r.Header.Get("X-Test-Session") == "yes" }}
	return c, tm
}

func (c *hubConsole) do(method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	if !c.matches(req) {
		rec.WriteHeader(http.StatusTeapot)
		return rec
	}
	c.serve(rec, req)
	return rec
}

var (
	asAgent  = map[string]string{"X-Test-Agent": "yes"}
	asPerson = map[string]string{"X-Test-Session": "yes"}
)

func waitScreen(t *testing.T, c *hubConsole, id, want string) string {
	t.Helper()
	var screen string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		rec := c.do("GET", "/agents/api/console/"+id+"/screen", "", asPerson)
		var out struct {
			Alive  bool   `json:"alive"`
			Screen string `json:"screen"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("%v: %s", err, rec.Body.String())
		}
		screen = out.Screen
		if strings.Contains(screen, want) {
			return screen
		}
	}
	t.Fatalf("the screen never showed %q:\n%s", want, screen)
	return ""
}

func TestHubConsoleRunsACommandAndTakesKeys(t *testing.T) {
	c, tm := consoleRig(t)
	const api = "/agents/api/agent/terminal-tab"

	// Only the machine agent may ask for one; the command must be a plain line.
	for _, tc := range []struct {
		hdr  map[string]string
		body string
		want int
	}{
		{nil, `{"command":"echo hi"}`, 401},
		{asPerson, `{"command":"echo hi"}`, 401},
		{asAgent, `{"command":""}`, 400},
		{asAgent, `{"command":"echo a\nrm -rf ~"}`, 400},
		{asAgent, `{"command":"` + strings.Repeat("x", commandMax+1) + `"}`, 400},
		{asAgent, `not json`, 400},
	} {
		if rec := c.do("POST", api, tc.body, tc.hdr); rec.Code != tc.want {
			t.Errorf("%v %q: %d %s", tc.hdr, tc.body, rec.Code, rec.Body.String())
		}
	}
	if rec := c.do("GET", api, "", asAgent); rec.Code != 405 {
		t.Errorf("GET: %d", rec.Code)
	}
	if got := tm.ListSessions(); len(got) != 0 {
		t.Fatalf("a refused request made a session: %v", got)
	}

	rec := c.do("POST", api, `{"command":"echo console-says-$((20+22)); read -r word; echo got-$word"}`, asAgent)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var tab struct{ TabID, URL string }
	if err := json.Unmarshal(rec.Body.Bytes(), &struct {
		TabID *string `json:"tabId"`
		URL   *string `json:"url"`
	}{&tab.TabID, &tab.URL}); err != nil || !validConsoleID(tab.TabID) || tab.URL != "/console/"+tab.TabID {
		t.Fatalf("%v %+v %s", err, tab, rec.Body.String())
	}
	waitScreen(t, c, tab.TabID, "console-says-42")

	// A person's session types into it; a cross-site page and an unknown key are refused.
	keys := "/agents/api/console/" + tab.TabID + "/keys"
	if rec := c.do("POST", keys, `{"text":"x","keys":["Enter"]}`, nil); rec.Code != 401 {
		t.Errorf("no session: %d", rec.Code)
	}
	cross := map[string]string{"X-Test-Session": "yes", "Sec-Fetch-Site": "cross-site"}
	if rec := c.do("POST", keys, `{"text":"x","keys":["Enter"]}`, cross); rec.Code != 403 {
		t.Errorf("cross-site: %d", rec.Code)
	}
	for _, body := range []string{`{"keys":["F13"]}`, `{"keys":["C-z"]}`, `{"keys":["Enter","Enter","Enter","Enter","Enter","Enter","Enter","Enter","Enter"]}`, `nope`} {
		if rec := c.do("POST", keys, body, asPerson); rec.Code != 400 {
			t.Errorf("%s: %d", body, rec.Code)
		}
	}
	if rec := c.do("POST", keys, `{"text":"typed-here","keys":["Enter"]}`, asPerson); rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitScreen(t, c, tab.TabID, "got-typed-here")

	// The page is for a signed-in person only, and for ids of consoles.
	page := "/m/abcdefghijklmnopqrst/console/" + tab.TabID
	if rec := c.do("GET", page, "", nil); rec.Code != 401 {
		t.Errorf("page without a session: %d", rec.Code)
	}
	rec = c.do("GET", page, "", asPerson)
	body := rec.Body.String()
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") || rec.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(rec.Header().Get("Content-Security-Policy"), "default-src 'none'") ||
		!strings.Contains(body, "/api/console/") || strings.Contains(body, tab.TabID) {
		t.Errorf("%d %v\n%.300s", rec.Code, rec.Header(), body)
	}
	for _, p := range []string{"/m/abcdefghijklmnopqrst/console/nope", "/m/abcdefghijklmnopqrst/console/../etc", "/agents/api/console/nope/screen"} {
		if rec := c.do("GET", p, "", asPerson); rec.Code != 404 && rec.Code != http.StatusTeapot {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	if rec := c.do("DELETE", page, "", asPerson); rec.Code != 405 {
		t.Errorf("DELETE page: %d", rec.Code)
	}

	// A console that is gone says so instead of failing.
	tm.KillSession(c.sessionName(tab.TabID))
	rec = c.do("GET", "/agents/api/console/"+tab.TabID+"/screen", "", asPerson)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"alive":false`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := c.do("POST", keys, `{"text":"x"}`, asPerson); rec.Code != 410 {
		t.Errorf("keys to a gone console: %d", rec.Code)
	}
}

func TestHubConsoleSweepsOldOnes(t *testing.T) {
	c, tm := consoleRig(t)
	rec := c.do("POST", "/agents/api/agent/terminal-tab", `{"command":"sleep 60"}`, asAgent)
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if n := len(tm.ListSessions()); n != 1 {
		t.Fatalf("%d sessions", n)
	}
	// Three hours on, the next console makes the first go; one that is not a console stays.
	if err := tm.CreateSession("hs-other", 80, 24, ""); err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	if rec := c.do("POST", "/agents/api/agent/terminal-tab", `{"command":"sleep 60"}`, asAgent); rec.Code != 200 {
		t.Fatalf("%d", rec.Code)
	}
	var consoles, others int
	for _, name := range tm.ListSessions() {
		if strings.HasPrefix(name, "hs-console-") {
			consoles++
		} else {
			others++
		}
	}
	if consoles != 1 || others != 1 {
		t.Errorf("%d consoles and %d others: %v", consoles, others, tm.ListSessions())
	}
}

// The first-run password setup would replace the pair's secret under the machine agent: a hub's runtime has none.
func TestHubSetupIsClosed(t *testing.T) {
	c := &hubConsole{base: "/m/abcdefghijklmnopqrst"}
	for _, p := range []string{"/agents/api/auth/setup", "/api/auth/setup"} {
		rec := c.do("GET", p, "", nil)
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != 200 || got["needsSetup"] != false || got["requiresAuth"] != false {
			t.Errorf("GET %s: %d %s", p, rec.Code, rec.Body.String())
		}
		rec = c.do("POST", p, `{"authPassword":"hunter2","networkAccess":"all"}`, nil)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "Setup already completed") {
			t.Errorf("POST %s: %d %s", p, rec.Code, rec.Body.String())
		}
	}
	if rec := c.do("PUT", "/api/auth/setup", "{}", nil); rec.Code != 405 {
		t.Errorf("PUT: %d", rec.Code)
	}
	// Other auth routes are the runtime's own.
	if rec := c.do("POST", "/api/auth/login", "{}", nil); rec.Code != http.StatusTeapot {
		t.Errorf("login is the runtime's: %d", rec.Code)
	}
}

func TestApiPath(t *testing.T) {
	for in, want := range map[string]string{
		"/api/x": "/api/x", "/agents/api/x": "/api/x", "/agents/apix": "", "/apix": "", "/m/a/api/x": "", "/": "", "/agents": "",
	} {
		if got := apiPath(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// A launch line starts with the env files a machine agent keeps, for the CLI of its provider; the bare resume line is
// what the starter puts the variables of a start and the same prefix in front of itself.
func TestLaunchLinesSourceTheEnvFiles(t *testing.T) {
	dir, home, envDir := t.TempDir(), t.TempDir(), t.TempDir()
	launcher := filepath.Join(home, "agent-bin")
	ag := newCoreAgents(coreAgentsConfig{DataDir: dir, Home: home, ConfigFile: filepath.Join(dir, "config.json"),
		ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex"), DefaultsKey: "launchDefaults",
		Launcher: launcher, EnvDir: envDir})
	const sid = "425055ec-bbc8-4e3c-87d0-36fa74c1cccb"

	// Without env files, nothing changes.
	bare, err := ag.resumeLine(agents.Claude, sid, "ws-a", nil)
	if err != nil || !strings.HasPrefix(bare, "DISABLE_AUTOUPDATER=1 ") {
		t.Fatalf("%q %v", bare, err)
	}
	if line, _ := ag.ResumeLine(agents.Claude, sid, "ws-a", nil); line != agents.ForAnyShell(bare) {
		t.Errorf("without env files: %q", line)
	}

	all, claude := agents.EnvFile(envDir, agents.EnvScopeAll), agents.EnvFile(envDir, agents.Claude)
	if err := os.WriteFile(all, []byte("GH_TOKEN='x'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	wantAll := "set -a; . '" + all + "'; set +a; "
	if line, _ := ag.ResumeLine(agents.Claude, sid, "ws-a", nil); line != agents.ForAnyShell(wantAll+bare) {
		t.Errorf("resume: %q", line)
	}
	if line, err := ag.LaunchLine(agents.Claude, "ws-a", nil); err != nil || !strings.HasPrefix(unwrapped(line), wantAll+"exec env DISABLE_AUTOUPDATER=1 ") {
		t.Errorf("launch: %q %v", line, err)
	}
	// The starter's lines stay bare, the prefix is the starter's to place.
	if line, _ := (lines{ag}).Resume(agents.Claude, sid, "ws-a", nil); line != bare {
		t.Errorf("starter's resume: %q", line)
	}
	if line, _ := (lines{ag}).Launch(agents.Claude, "ws-a", nil, nil); strings.HasPrefix(line, "set -a") {
		t.Errorf("starter's launch: %q", line)
	}
	if got := ag.envSource(agents.Claude); got != wantAll {
		t.Errorf("%q", got)
	}

	// An agent's own file comes after the shared one, so it wins.
	if err := os.WriteFile(claude, []byte("ANTHROPIC_API_KEY='k'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, want := ag.envSource(agents.Claude), "set -a; . '"+all+"'; . '"+claude+"'; set +a; "; got != want {
		t.Errorf("%q, want %q", got, want)
	}
	if got := ag.envSource(agents.Codex); got != wantAll {
		t.Errorf("codex has the shared file only: %q", got)
	}
}

// The machine agent tells the runtime whether the machine's policy keeps the terminal off; nobody else may.
func TestHubPolicyRouteTellsTheChatGuard(t *testing.T) {
	g := chatguard.New(true) // a hub's runtime starts with the terminal off
	c := &hubConsole{base: "/m/abcdefghijklmnopqrst", guard: g,
		agent: func(r *http.Request) bool { return r.Header.Get("X-Test-Agent") == "yes" }}
	for _, path := range []string{"/agents/api/agent/policy", "/api/agent/policy"} {
		for _, tc := range []struct {
			method string
			hdr    map[string]string
			body   string
			want   int
		}{
			{"POST", nil, `{"terminal":true}`, 401},
			{"POST", asPerson, `{"terminal":true}`, 401},
			{"GET", asAgent, ``, 405},
			{"POST", asAgent, `not json`, 400},
			{"POST", asAgent, `{}`, 400},
			{"POST", asAgent, `{"terminal":null}`, 400},
		} {
			if rec := c.do(tc.method, path, tc.body, tc.hdr); rec.Code != tc.want {
				t.Errorf("%s %s %q as %v: %d, want %d", tc.method, path, tc.body, tc.hdr, rec.Code, tc.want)
			}
			if !g.Off() {
				t.Fatalf("%s %s %q as %v changed the policy", tc.method, path, tc.body, tc.hdr)
			}
		}
		if rec := c.do("POST", path, `{"terminal":true}`, asAgent); rec.Code != 200 || g.Off() {
			t.Fatalf("terminal on: %d %s, off=%v", rec.Code, rec.Body.String(), g.Off())
		}
		if rec := c.do("POST", path, `{"terminal":false}`, asAgent); rec.Code != 200 || !g.Off() {
			t.Fatalf("terminal off: %d %s, off=%v", rec.Code, rec.Body.String(), g.Off())
		}
		g.Set(true)
	}
	// Not the runtime's route when nothing is told (a path of another prefix).
	if rec := c.do("POST", "/api/policy", `{"terminal":true}`, asAgent); rec.Code != http.StatusTeapot {
		t.Errorf("another path: %d", rec.Code)
	}
}

// unwrapped is the sh line of a line written for any shell.
func unwrapped(line string) string {
	inner, _ := agents.UnwrapAnyShell(line)
	return inner
}
