package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	"harnessly/internal/files"
	"harnessly/internal/gate"
	"harnessly/internal/machines"
	"harnessly/internal/nodejson"
	"harnessly/internal/preflight"
	"harnessly/internal/settings"
	"harnessly/internal/tmux"
)

// testdata/state-golden.json: what the previous runtime's dispatcher and handlers (testdata/state, under node)
// answered to the state slice's requests and to the methods and queries the other slices now answer, and what the
// data folder held after each.

type stateStep struct {
	File          *string           `json:"file"`
	Content       *string           `json:"content"`
	PathDir       *string           `json:"pathDir"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Raw           *string           `json:"raw"`
	Headers       map[string]string `json:"headers"`
	Body          string            `json:"body"`
	At            int64             `json:"at"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
	Files         []map[string]any  `json:"files"`
}

type stateGolden struct {
	Token    string `json:"token"`
	GitSetup string `json:"gitSetup"`
	Fixtures []struct {
		Path string `json:"path"`
		Text string `json:"text"`
		Mode int    `json:"mode"`
	} `json:"fixtures"`
	Steps []stateStep `json:"steps"`
}

// dataSnapshot lists the data folder as the generator did.
func dataSnapshot(data, root string) []map[string]any {
	var out []map[string]any
	var walk func(d string)
	walk = func(d string) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		sort.Strings(names)
		for _, n := range names {
			p := filepath.Join(d, n)
			rel, _ := filepath.Rel(data, p)
			if rel == "cli-token" {
				continue
			}
			st, _ := os.Lstat(p)
			if st.IsDir() {
				out = append(out, map[string]any{"path": rel, "dir": true})
				walk(p)
				continue
			}
			raw, _ := os.ReadFile(p)
			out = append(out, map[string]any{"path": rel, "mode": strconv.FormatInt(int64(st.Mode().Perm()), 8),
				"text": strings.ReplaceAll(string(raw), root, "$ROOT")})
		}
	}
	walk(data)
	return out
}

var signedCookie = regexp.MustCompile(`session-token=[^;]+;`)

func TestStateGolden(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	golden := filepath.Join("testdata", "state-golden.json")
	if f := os.Getenv("STATE_GOLDEN_FILE"); f != "" {
		golden = f // generated cases, run once
	}
	raw, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	var g stateGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home, data, work := filepath.Join(root, "home"), filepath.Join(root, "data"), filepath.Join(root, "work")
	for _, d := range []string{home, data, work} {
		_ = os.MkdirAll(d, 0o755)
	}
	for _, f := range g.Fixtures {
		p := filepath.Join(root, f.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		mode := os.FileMode(0o644)
		if f.Mode != 0 {
			mode = os.FileMode(f.Mode)
		}
		_ = os.WriteFile(p, []byte(f.Text), mode)
	}
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(g.Token), 0o600)
	t.Setenv("HOME", home)
	t.Setenv("PATH", filepath.Join(root, "nobin")+":/usr/bin:/bin")
	t.Setenv("RUNTIME_REACT_SCAN_FILE", filepath.Join(root, "node_modules", "react-scan", "dist", "auto.global.js"))
	for _, k := range []string{"HOST", "RUNTIME_ACCESS_HOST", "INIT_PASSWORD", "AUTH_PASSWORD", "DATABASE_URL", "PGHOST", "RUNTIME_OWNER_EMAIL"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	setup := exec.Command("sh", "-c", g.GitSetup)
	setup.Env = append(os.Environ(), "ROOT="+root, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := setup.CombinedOutput(); err != nil {
		t.Fatalf("git setup: %v %s", err, out)
	}
	t.Setenv("RUNTIME_LAYOUT_KEY", "acmeLayout")
	t.Setenv("RUNTIME_LAUNCH_DEFAULTS_KEY", "acmeLaunchDefaults")

	socket := "hs-test-state-" + strconv.Itoa(os.Getpid())
	tm := tmux.NewConfig(tmux.Config{Names: tmux.Names{Socket: socket, Prefix: "hs-"}, Home: home})
	t.Cleanup(func() {
		tm.KillServer()
		_ = os.Remove(filepath.Join(os.TempDir(), "tmux-"+strconv.Itoa(os.Getuid()), socket))
	})
	for name, dir := range map[string]string{"tab-1": filepath.Join(work, "repo"), "tab-plain": work, "tab-broken": filepath.Join(work, "broken")} {
		if out, err := exec.Command("tmux", "-L", socket, "-f", "/dev/null", "new-session", "-d", "-s", name, "-c", dir, "sleep 3600").CombinedOutput(); err != nil {
			t.Fatalf("tmux: %v %s", err, out)
		}
	}

	headers := []string{"x-cli-token", "x-old-cli-token"}
	cookie := func(r *http.Request, values ...string) bool {
		c, err := r.Cookie("session-token")
		return err == nil && (c.Value == values[0] || len(values) > 1 && c.Value == values[1])
	}
	sessionOK := func(r *http.Request) bool { return cookie(r, "good", "agent") }
	tokenOK := cliTokenFirst(data, headers)
	old := &gate.OldRuntime{Upstream: "127.0.0.1:18099", DataDir: data, TokenHeaders: headers}
	d := deps{Home: home, DataDir: data, ConfigFile: filepath.Join(data, "config.json"), WorkspaceRoot: work, Tmux: tm,
		TokenHeaders: headers, Old: old, SessionOK: sessionOK,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || tokenOK(r) {
				return 0, nil, nil, false
			}
			return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		MachineAgent: func(r *http.Request) bool { return cookie(r, "agent") },
		Cookie:       "session-token", SignSession: func() string { return "a.b.c" }, Tools: preflight.New(home), Upstream: "127.0.0.1:18099"}

	var mu sync.Mutex
	clock := time.Now()
	draws, ids := uint32(0), 0
	store := &settings.Store{DataDir: data, ConfigFile: d.ConfigFile,
		Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return clock },
		RandomBytes: func(n int) []byte {
			mu.Lock()
			defer mu.Unlock()
			draws++
			b := make([]byte, n)
			binary.BigEndian.PutUint32(b[n-4:], draws)
			return b
		},
		NewID: func() string {
			mu.Lock()
			defer mu.Unlock()
			ids++
			id := "id" + fmt.Sprintf("%04s", strconv.FormatInt(int64(ids), 36))
			return id[len(id)-min(len(id), 21):]
		}}
	prevUploads := newUploads
	newUploads = func(d deps) *files.Uploads {
		return &files.Uploads{Dir: uploadsDir(d), Now: func() time.Time { mu.Lock(); defer mu.Unlock(); return clock },
			Random: func() string { return fmt.Sprintf("%x", store.RandomBytes(4)) }}
	}
	t.Cleanup(func() { newUploads = prevUploads })
	agents := newCoreAgents(coreAgentsConfig{DataDir: data, Home: home, ConfigFile: d.ConfigFile,
		ClaudeDir: filepath.Join(home, ".claude"), CodexDir: filepath.Join(home, ".codex"), DefaultsKey: "acmeLaunchDefaults"})

	gateDir := filepath.Join(root, "gate") // state-mode.json is not part of the data folder the generator saw
	_ = os.MkdirAll(gateDir, 0o755)
	_ = os.WriteFile(filepath.Join(gateDir, "state-mode.json"), []byte(`{"goSlices":["state","fsread","fswrite","voice"]}`), 0o600)
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: "127.0.0.1:1", DataDir: gateDir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	f.Add(f.AsRoutes(stateRoutes(d, store, agents.ValidateLaunch)...)...)
	f.Add(unshadowed(f, automationRoutes(d, store))...)
	for _, group := range []func(deps) []gate.JSONRoute{fileRoutes, gitRoutes, changesRoutes, fswriteRoutes, sessionRoutes, otherMethodRoutes} {
		f.Add(f.AsRoutes(group(d)...)...)
	}
	f.Add(f.AsBodyRoutes(fileBodyRoutes(d)...)...)
	stopper := &machines.Stopper{Listed: func() ([]machines.LoadProcess, error) { return nil, nil }, UID: os.Getuid(),
		RuntimePIDs: func() []int { return []int{os.Getpid()} }}
	f.Add(f.AsRoutes(stopRoute(d, stopper))...)
	srv := httptest.NewServer(f)
	defer srv.Close()

	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	unscrub := func(s string) string { return strings.ReplaceAll(s, "$ROOT", root) }
	for i, c := range g.Steps {
		switch {
		case c.File != nil:
			p := filepath.Join(data, *c.File)
			if c.Content == nil {
				_ = os.Remove(p)
			} else {
				_ = os.MkdirAll(filepath.Dir(p), 0o755)
				_ = os.WriteFile(p, []byte(*c.Content), 0o644)
			}
			continue
		case c.PathDir != nil:
			t.Setenv("PATH", filepath.Join(root, *c.PathDir)+":/usr/bin:/bin")
			continue
		}
		mu.Lock()
		clock = time.UnixMilli(c.At)
		mu.Unlock()
		u := srv.URL + c.Path
		switch {
		case c.Raw != nil:
			u += "?" + *c.Raw
		case len(c.Query) > 0:
			var q []string
			for _, kv := range c.Query {
				q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(unscrub(kv[1])))
			}
			u += "?" + strings.ReplaceAll(strings.Join(q, "&"), "+", "%20")
		}
		req, _ := http.NewRequest(c.Method, u, strings.NewReader(unscrub(c.Body)))
		for k, v := range c.Headers {
			req.Header[http.CanonicalHeaderKey(k)] = []string{v}
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("#%d: %v", i, err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		where := fmt.Sprintf("#%d %s %s %v", i, c.Method, c.Path, c.Query)
		answer := scrub(string(got))
		if res.StatusCode != c.Status || answer != c.Answer && !(strings.Contains(c.Path, "daily-report") && sameJSONText(answer, c.Answer)) {
			t.Errorf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, answer, c.Status, c.Answer)
			continue
		}
		for k, w := range c.AnswerHeaders {
			have := scrub(res.Header.Get(k))
			switch k {
			case "content-length":
				continue
			case "set-cookie": // the generator's sessions are a stand-in word, these are signed tokens
				have = signedCookie.ReplaceAllString(have, "session-token=signed;")
			case "etag":
				if strings.Contains(c.Path, "daily-report") {
					continue
				}
				// The old tag hashed the old words and paths; a neutralized answer is checked against its own bytes.
				if strings.Contains(c.Answer, "acme") || strings.Contains(c.Answer, "$ROOT") {
					w = nodejson.ETag(got)
				}
			}
			if have != w {
				t.Errorf("%s: %s = %q, want %q", where, k, have, w)
			}
		}
		for _, k := range []string{"ETag", "Allow", "Cache-Control", "Set-Cookie"} {
			if _, ok := c.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("%s: unexpected %s: %s", where, k, res.Header.Get(k))
			}
		}
		if c.Files != nil {
			want, _ := json.Marshal(c.Files)
			have, _ := json.Marshal(dataSnapshot(data, root))
			if !sameJSONText(string(have), string(want)) {
				t.Errorf("%s: data folder\n got %s\nwant %s", where, have, want)
			}
		}
	}
}

func sameJSONText(a, b string) bool {
	var x, y any
	if json.Unmarshal([]byte(a), &x) != nil || json.Unmarshal([]byte(b), &y) != nil {
		return false
	}
	p, _ := json.Marshal(x)
	q, _ := json.Marshal(y)
	return string(p) == string(q)
}
