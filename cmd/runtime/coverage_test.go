package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
)

// allSlices are the slices a machine runs with once every one is switched on.
var allSlices = []string{"terminal", "core", "fsread", "fswrite", "machines", "launch", "timeline", "services", "voice",
	"design", "preview", "transcripts", "state"}

// concreteOldRoutes are the previous runtime's routes with each parameter filled in.
func concreteOldRoutes() []string {
	var out []string
	for _, route := range oldRoutes {
		parts := strings.Split(strings.Replace(route, "<platform>", platformAPI, 1), "/")
		for i, p := range parts {
			switch {
			case strings.HasPrefix(p, "[..."):
				parts[i] = "a/b.png"
			case strings.HasPrefix(p, "["):
				parts[i] = "p1"
			}
		}
		out = append(out, strings.Join(parts, "/"))
	}
	return out
}

// liveGate is the whole gate as main builds it, every slice on and the core running on a private tmux server, in front of
// a stand-in previous runtime that records what reaches it.
type liveGate struct {
	url, data, socket string
	secret            []byte
	cookie            string
	hits              func() []string
}

var liveGates int

func newLiveGate(t *testing.T) *liveGate {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	home, data := t.TempDir(), t.TempDir()
	liveGates++
	socket := "hs-test-cover-" + strconv.Itoa(os.Getpid()) + "-" + strconv.Itoa(liveGates)
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("RUNTIME_TMUX_SOCKET", socket)
	t.Setenv("RUNTIME_SESSION_PREFIX", "hs-")
	secretFile := filepath.Join(data, "secret")
	secret := []byte("0123456789abcdef0123456789abcdef")
	_ = os.WriteFile(secretFile, secret, 0o600)
	t.Setenv("AGENT_SECRET_FILE", secretFile)
	for k, v := range map[string]string{
		"RUNTIME_TRACKER_URL": "http://127.0.0.1:1", "RUNTIME_TRACKER_HOSTS": "tracker.example.org",
		"RUNTIME_GITLAB_HOSTS": "gitlab.example.org", "RUNTIME_DOCS_HOST": "docs.example.org", "RUNTIME_TASK_KEY_RE": "^(ACME)-[0-9]{1,6}$",
		"RUNTIME_ORGANIZER_FILE": filepath.Join(data, "organizer.json"), "RUNTIME_PUSH_SUBJECT": "mailto:ops@example.org",
		"RUNTIME_PUSH_TAG_PREFIX": "app:", "RUNTIME_APP_NAME": "Acme", "RUNTIME_OWNER_EMAIL": "owner@example.org",
		"RUNTIME_MANIFEST_FILE": filepath.Join(data, "manifest.json"), "RUNTIME_LAUNCH_DEFAULTS_KEY": "acmeLaunchDefaults",
		"RUNTIME_LAYOUT_KEY": "acmeLayout", "RUNTIME_API_GUIDE_FILE": filepath.Join(data, "api-guide.md"),
		"RUNTIME_SHARE_BASE_HEADER": "x-acme-share-base", "RUNTIME_SHARE_TOKEN_HEADER": "x-acme-share-token",
		"RUNTIME_SHARE_TOKEN_SALT": "acme", "RUNTIME_SHARE_TOKEN_AUDIENCE": "acme-share-read", "RUNTIME_SHARE_FILE_PREFIX": "acme",
		"RUNTIME_SHARE_DROP_HOST": "example.org",
	} {
		t.Setenv(k, v)
	}
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(strings.Repeat("c", 64)), 0o600) // the previous runtime made it at start
	_ = os.WriteFile(filepath.Join(data, "manifest.json"), []byte(`{"name":"Acme"}`), 0o600)
	_ = os.WriteFile(filepath.Join(data, "api-guide.md"), []byte("# API\n"), 0o600)
	_ = os.WriteFile(filepath.Join(data, "state-mode.json"), []byte(`{"owned":true,"headless":true,"goSlices":["`+strings.Join(allSlices, `","`)+`"]}`), 0o600)
	var mu sync.Mutex
	hits := map[string]bool{}
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.Method+" "+r.URL.RequestURI()] = true
		mu.Unlock()
		w.WriteHeader(299)
	}))
	t.Cleanup(old.Close)
	f, err := build(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: data, CountEvery: time.Hour,
		TokenHeaders: []string{"x-cli-token"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c := f.core.c; c != nil {
			c.stop()
		}
		// Its own server by name: tmuxNames is the latest gate's, and a test may start several.
		_ = exec.Command("tmux", "-L", socket, "kill-server").Run()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	deadline := time.Now().Add(20 * time.Second)
	for f.core.running() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the core did not start")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cookie := "session-token=" + agent.SignHS256(secret, "user", "", time.Hour)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return &liveGate{url: srv.URL, data: data, socket: socket, secret: secret, cookie: cookie, hits: func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, 0, len(hits))
		for k := range hits {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}}
}

// proxiedRequests runs every route of the previous runtime, in every method and the odd forms a caller may send,
// through the gate with all slices on, and names the requests that still went to the previous runtime.
func proxiedRequests(t *testing.T) []string {
	g := newLiveGate(t)
	cookie := g.cookie
	srv := struct{ URL string }{g.url}
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	send := func(method, uri string, upgrade bool) {
		req, _ := http.NewRequest(method, srv.URL+uri, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
			req.Header.Set("Sec-WebSocket-Version", "13")
			req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
		}
		if upgrade {
			conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			_ = req.Write(conn)
			_, _ = bufio.NewReader(conn).ReadString('\n') // the status line is enough: who answered is in hits
			return
		}
		res, err := client.Do(req)
		if err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
	}
	methods := []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	for _, p := range concreteOldRoutes() {
		for _, m := range methods {
			send(m, p, false)
			send(m, p+"?q=1&q=2&session=a&session=b", false)
			send(m, p+"?%zz=1&session=%zz", false)
			send(m, "/agents"+p, false)
			send(m, p+"/", false)
		}
	}
	for _, p := range []string{"/api/terminal", "/api/terminal?session=x", "/api/timeline", "/api/sync", "/api/status", "/api/voice",
		"/api/install", "/api/nothing"} {
		send("GET", p, true)
	}
	for _, p := range []string{"/", "/agents", "/agents/", "/agents/workspace", "/_next/static/x.js", "/agents/_next/static/x.js",
		"/fonts/a.woff2", "/favicon.ico", "/api", "/api/", "/api/nothing", "/agents/api/nothing/deeper", "/login"} {
		for _, m := range methods {
			send(m, p, false)
		}
	}
	return g.hits()
}

// stillProxied are the paths the previous runtime may still be asked for with every slice on: its own internal API,
// which only this gate calls (and only while a slice is off).
var stillProxied = regexp.MustCompile(`^(/agents)?/api/internal/(core|terminal)/`)

func TestEveryOldRouteIsAnsweredHere(t *testing.T) {
	if os.Getenv("RUNTIME_COVERAGE") == "" {
		t.Skip("set RUNTIME_COVERAGE=1: it runs every route of the previous runtime against a private tmux server")
	}
	for _, k := range proxiedRequests(t) {
		_, uri, _ := strings.Cut(k, " ")
		if !stillProxied.MatchString(uri) {
			t.Error("proxied: " + k)
		} else if testing.Verbose() {
			t.Log("proxied as expected: " + k)
		}
	}
}
