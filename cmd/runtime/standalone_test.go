package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/runlock"
	"github.com/ly-solutions/harnessly/internal/status"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// standaloneGolden is what the previous runtime wrote at its start on an empty data folder (testdata/standalone/gen.mjs).
type standaloneGolden struct {
	Lock  map[string]json.RawMessage `json:"lock"`
	Files struct {
		ConfigCreated string `json:"configCreated"`
		All           map[string]struct {
			Content string `json:"content"`
			Mode    string `json:"mode"`
		} `json:"all"`
	} `json:"files"`
	HookResult struct {
		CodexHookInstallFailed bool `json:"codexHookInstallFailed"`
	} `json:"hookResult"`
	LockFileName string `json:"lockFileName"`
}

type lockCase struct {
	Exited  *int    `json:"exited"`
	File    *string `json:"file"`
	Content *string `json:"content"`
}

func loadStandaloneGolden(t *testing.T) standaloneGolden {
	t.Helper()
	raw, err := os.ReadFile("testdata/standalone-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g standaloneGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// standaloneEnv is the deployment's environment in the neutral words the golden was made with.
func standaloneEnv(t *testing.T, home, data string) {
	t.Helper()
	wd, _ := os.Getwd()
	for k, v := range map[string]string{
		"HOME": home, "AGENT_SECRET_FILE": "", "AGENT_CONFIG": "",
		// The files compared here are the previous runtime's own; the agent skills are the Go runtime's addition.
		"RUNTIME_AGENT_SKILLS":     "off",
		"RUNTIME_SCRIPT_DATA_DIRS": ".acme-ai,.oldmux", "RUNTIME_CLI_TOKEN_HEADERS": "x-acme-token,x-omux-token",
		"RUNTIME_MCP_SERVER": "acme-preview", "RUNTIME_MCP_ALLOW": "mcp__acme-preview,mcp__oldmux-preview", "RUNTIME_MCP_TOKEN_ENV": "ACME_PREVIEW_TOKEN",
		"RUNTIME_MCP_PATH": "", "RUNTIME_PROMPT_EDITOR": filepath.Join(data, "bin", "acme-prompt-editor"), "RUNTIME_PROMPT_EDITOR_NAME": "",
		"RUNTIME_PROMPT_EDITOR_TITLE": "Acme prompt editor", "RUNTIME_PROMPT_HUMAN_EDITOR_VAR": "ACME_HUMAN_EDITOR",
		"RUNTIME_PROMPT_HANDOFF_DIR": filepath.Join(data, "prompt-handoff"), "RUNTIME_PROMPT_HANDOFF_UNKEYED_SOCKET": "violet",
		"RUNTIME_LOCK_FILE": "omux.lock", "RUNTIME_HEALTH_APP": "oldmux", "RUNTIME_RELEASE_FILE": "",
		"RUNTIME_WORKSPACE_PROMPTS_FILE": filepath.Join(wd, "testdata", "standalone", "workspace-prompts.json"),
		"RUNTIME_CODEX_LAUNCHER_FILE":    filepath.Join(wd, "testdata", "standalone", "codex-launcher.js"),
	} {
		t.Setenv(k, v)
	}
}

var (
	hex64     = regexp.MustCompile(`[0-9a-f]{64}`)
	isoStamp  = regexp.MustCompile(`"updatedAt": "[^"]+"`)
	lockStamp = regexp.MustCompile(`"pid": (?:\d+|\$PID),\n  "port": (\d+),\n  "startedAt": \d+`)
)

// normalized leaves out what differs by run: the random secret and token, the stamp, the process.
func normalized(s, data, home string) string {
	if data != "" {
		s = strings.ReplaceAll(strings.ReplaceAll(s, data, "$DATA"), home, "$HOME")
	}
	s = hex64.ReplaceAllString(s, "<hex>")
	s = isoStamp.ReplaceAllString(s, `"updatedAt": "<at>"`)
	return lockStamp.ReplaceAllString(s, `"pid": <pid>,`+"\n"+`  "port": $1,`+"\n"+`  "startedAt": <at>`)
}

// A standalone start writes what the previous runtime wrote at its start, byte for byte: the lock, the config with the
// pair's secret, the cli-token, the port file, the agents' scripts, settings and MCP files, the prompt editor and a
// workspace's prompts from the release's templates.
func TestStandaloneStartWritesThePreviousRuntimesFiles(t *testing.T) {
	g := loadStandaloneGolden(t)
	root := t.TempDir()
	home, data := filepath.Join(root, "home"), filepath.Join(root, "data")
	_ = os.MkdirAll(home, 0o700)
	standaloneEnv(t, home, data)
	cfg := gate.Config{Listen: "127.0.0.1:8022", DataDir: data}
	s, err := standaloneStart(cfg, configFileOf(cfg), 8022)
	if err != nil {
		t.Fatal(err)
	}
	var created string
	if raw, err := os.ReadFile(filepath.Join(data, "config.json")); err == nil {
		created = string(raw)
	}
	s.files(8022, nil)
	if err := workspacePrompts().Write(data, "ws-golden"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(created, "authSecret") || normalized(g.Files.ConfigCreated, data, home) != `{`+"\n"+`  "updatedAt": "<at>"`+"\n}" {
		t.Fatalf("config: %s", created)
	}
	got := map[string]string{}
	_ = filepath.Walk(data, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(data, p)
			raw, _ := os.ReadFile(p)
			got[rel] = string(raw)
		}
		return nil
	})
	for name, f := range g.Files.All {
		want := normalized(strings.ReplaceAll(strings.ReplaceAll(f.Content, "$DATA", data), "$HOME", home), data, home)
		if have, ok := got[name]; !ok {
			t.Errorf("%s not written", name)
		} else if normalized(have, data, home) != want {
			t.Errorf("%s differs:\n--- go\n%s\n--- ts\n%s", name, normalized(have, data, home), want)
		}
		delete(got, name)
	}
	var fresh lockCase
	_ = json.Unmarshal(g.Lock["fresh"], &fresh)
	if have := normalized(got[g.LockFileName], data, home); have != normalized(*fresh.Content, data, home) || *fresh.File != g.LockFileName {
		t.Errorf("lock:\n%s\n--- ts\n%s", have, *fresh.Content)
	}
	delete(got, g.LockFileName)
	launcher, _ := os.ReadFile(os.Getenv("RUNTIME_CODEX_LAUNCHER_FILE"))
	if got["codex-launcher.js"] != string(launcher) {
		t.Error("the Codex launcher is the release's")
	}
	delete(got, "codex-launcher.js")
	for name := range got {
		t.Errorf("written beyond the previous runtime's start: %s", name)
	}

	// An existing token is kept; the port file goes and the lock is released at stop.
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte("kept-token\n"), 0o600)
	s.stop()
	if _, err := os.Stat(filepath.Join(data, "port")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("port file left at stop")
	}
	if _, err := os.Stat(filepath.Join(data, g.LockFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lock left at stop")
	}
	s, err = standaloneStart(cfg, configFileOf(cfg), 8023)
	if err != nil || s.token != "kept-token" {
		t.Fatalf("%v %q", err, s.token)
	}
	s.stop()
}

// The lock left behind, as the previous runtime treated it: garbage, a holder that is gone, a live process that is not a
// runtime are taken over; a runtime answering on its port keeps the lock.
func TestStandaloneLockAsThePreviousRuntime(t *testing.T) {
	g := loadStandaloneGolden(t)
	file := filepath.Join(t.TempDir(), g.LockFileName)
	serve := func(app string) (int, func()) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/health" {
				_, _ = w.Write([]byte(`{"app":"` + app + `"}`))
			}
		}))
		port, _ := strconv.Atoi(srv.URL[strings.LastIndex(srv.URL, ":")+1:])
		return port, srv.Close
	}
	cases := []struct {
		name   string
		before func() (string, func())
	}{
		{"garbage", func() (string, func()) { return "not json", func() {} }},
		{"dead holder", func() (string, func()) { return `{"pid":4206649,"port":1,"startedAt":1}`, func() {} }},
		{"live runtime", func() (string, func()) {
			port, stop := serve("oldmux")
			return `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":` + strconv.Itoa(port) + `,"startedAt":1}`, stop
		}},
		{"live other process", func() (string, func()) {
			port, stop := serve("other")
			return `{"pid":` + strconv.Itoa(os.Getppid()) + `,"port":` + strconv.Itoa(port) + `,"startedAt":1}`, stop
		}},
	}
	for _, c := range cases {
		var want lockCase
		if err := json.Unmarshal(g.Lock[c.name], &want); err != nil {
			t.Fatal(c.name, err)
		}
		before, done := c.before()
		_ = os.WriteFile(file, []byte(before), 0o600)
		l, err := runlock.Acquire(runlock.Config{File: file, App: "oldmux"}, 8022)
		done()
		var held *runlock.HeldError
		if (want.Exited != nil) != errors.As(err, &held) {
			t.Fatalf("%s: %v, previous runtime exited %v", c.name, err, want.Exited)
		}
		raw, _ := os.ReadFile(file)
		have := string(raw)
		if have == before {
			have = "(kept)"
		}
		if normalized(have, "", "") != normalized(*want.Content, "", "") {
			t.Errorf("%s:\n%s\n--- ts\n%s", c.name, have, *want.Content)
		}
		if l != nil {
			l.Release()
		}
	}
}

// A corrected tab state is reported to the deployment's endpoint, as the old runtime reported it.
func TestMismatchReport(t *testing.T) {
	got := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got <- r.URL.Path + " " + string(raw)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	ms := 1499.6
	x := status.Mismatch{Kind: "stuck-settled", Shown: "busy", Provider: "claude", DurationMs: &ms}
	alone := &core{cfg: coreConfig{Standalone: true, Telemetry: telemetry.New(telemetry.Config{URL: srv.URL + "/batch/", Key: "k",
		Common: jsvalue.NewObject("app", "ai"), DistinctID: func() string { return "ann@example.org" }, Delay: 10 * time.Millisecond})}}
	alone.reportMismatch(x)
	body := <-got
	if !strings.HasPrefix(body, `/batch/ {"api_key":"k","batch":[{"event":"platform_status_mismatch","timestamp":"`) ||
		!strings.HasSuffix(body, `"properties":{"app":"ai","kind":"stuck-settled","shown":"busy","actual":null,"provider":"claude","duration_ms":1500,"unhooked":false,"distinct_id":"ann@example.org","$process_person_profile":false}}]}`) {
		t.Fatal(body)
	}
}

// Alone, a request no route takes: an old route is unavailable, anything else not found, a socket path refused (and
// unauthorized without a session or the cli-token).
func TestStandaloneFallback(t *testing.T) {
	h := standaloneFallback(func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" })
	get := func(p string) (int, string, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec.Code, rec.Body.String(), rec.Header().Get("Content-Type")
	}
	if code, body, ct := get("/api/stats/overview"); code != 503 || body != `{"error":"Service unavailable"}` || ct != "application/json" {
		t.Fatalf("%d %s %s", code, body, ct)
	}
	if code, body, _ := get("/agents/api/nothing"); code != 404 || body != `{"error":"Not found"}` {
		t.Fatalf("%d %s", code, body)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	for p, want := range map[string]string{"/api/timeline": "HTTP/1.1 503 Service Unavailable", "/agents/api/elsewhere?token": "HTTP/1.1 404 Not Found",
		"/agents/api/elsewhere": "HTTP/1.1 401 Unauthorized", "/api/install": "HTTP/1.1 404 Not Found"} {
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest("GET", srv.URL+p, nil)
		req.Header.Set("Connection", "Upgrade")
		req.Header.Set("Upgrade", "websocket")
		if req.URL.RawQuery == "token" {
			req.Header.Set("x-cli-token", "ok")
		}
		_ = req.Write(conn)
		line, _ := bufio.NewReader(conn).ReadString('\n')
		conn.Close()
		if strings.TrimSpace(line) != want {
			t.Errorf("%s: %q", p, line)
		}
	}
}

// In front of the old runtime, a socket upgrade no route took does not reach it unless a page session or the cli-token
// stands behind it, and the first-run install shell never does (security review).
func TestUpstreamGuardRefusesBeforeProxying(t *testing.T) {
	var reached atomic.Int32
	guard := upstreamGuard(func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" },
		func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/preview/") })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if guard.Match(r) {
			guard.Handler.ServeHTTP(w, r)
			return
		}
		reached.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	line := func(method, path string, upgrade bool, header map[string]string) string {
		conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		req, _ := http.NewRequest(method, srv.URL+path, nil)
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		for k, v := range header {
			req.Header.Set(k, v)
		}
		_ = req.Write(conn)
		l, _ := bufio.NewReader(conn).ReadString('\n')
		return strings.TrimSpace(l)
	}
	ok := map[string]string{"x-cli-token": "ok"}
	for _, c := range []struct {
		path   string
		header map[string]string
		want   string
		proxy  bool
	}{
		{"/api/install?command=claude", nil, "HTTP/1.1 404 Not Found", false},
		{"/agents/api/install", nil, "HTTP/1.1 404 Not Found", false},
		{"/api/install", ok, "HTTP/1.1 404 Not Found", false},
		{"/agents/api/install", ok, "HTTP/1.1 404 Not Found", false},
		{"/api/elsewhere", nil, "HTTP/1.1 401 Unauthorized", false},
		{"/agents/api/elsewhere", nil, "HTTP/1.1 401 Unauthorized", false},
		{"/api/elsewhere", ok, "HTTP/1.1 200 OK", true},
		{"/preview/hmr", nil, "HTTP/1.1 200 OK", true},
	} {
		before := reached.Load()
		if got := line("GET", c.path, true, c.header); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.path, c.header, got, c.want)
		}
		if proxied := reached.Load() != before; proxied != c.proxy {
			t.Errorf("%s %v: proxied %v, want %v", c.path, c.header, proxied, c.proxy)
		}
	}
	// A plain request is the old runtime's own business.
	before := reached.Load()
	if got := line("GET", "/api/install", false, nil); got != "HTTP/1.1 200 OK" || reached.Load() != before+1 {
		t.Errorf("plain request: %q", got)
	}
}
