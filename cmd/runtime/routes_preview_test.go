package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// devtools.json is the previous runtime's DevTools routes, MCP server and slot endpoints driven by one script
// (internal/preview/testdata/gen/gen-devtools.mjs): the store changes are applied here the same way and every call
// must answer the same status, headers and bytes.

type previewGolden struct {
	Secret    string                       `json:"secret"`
	Token     string                       `json:"token"`
	Start     int64                        `json:"start"`
	Steps     []json.RawMessage            `json:"steps"`
	Panes     map[string]struct{ PID int } `json:"panes"`
	Capture   map[string]string            `json:"capture"`
	Listeners map[string]struct {
		Port    int     `json:"port"`
		PID     *int    `json:"pid"`
		Command *string `json:"command"`
	} `json:"listeners"`
}

type previewStep struct {
	Kind    string            `json:"kind"`
	Op      string            `json:"op"`
	At      int64             `json:"at"`
	Slot    string            `json:"slot"`
	Target  json.RawMessage   `json:"target"`
	Entry   json.RawMessage   `json:"entry"`
	ID      int64             `json:"id"`
	Patch   map[string]any    `json:"patch"`
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	Out     struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	} `json:"out"`
}

func previewTargetOf(raw json.RawMessage) devtools.Target {
	var m struct {
		Kind   string `json:"kind"`
		Port   int    `json:"port"`
		Origin string `json:"origin"`
	}
	_ = json.Unmarshal(raw, &m)
	return devtools.Target{Kind: m.Kind, Port: m.Port, Origin: m.Origin}
}

func previewBodyOf(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var rep struct {
		Repeat string `json:"repeat"`
		N      int    `json:"n"`
	}
	_ = json.Unmarshal(raw, &rep)
	return strings.Repeat(rep.Repeat, rep.N), true
}

func previewNum(v any) *int64 {
	f, ok := v.(float64)
	if !ok {
		return nil
	}
	n := int64(f)
	return &n
}

func previewStr(v any) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}

func TestPreviewDevtoolsGolden(t *testing.T) {
	file := "../../internal/preview/testdata/devtools.json"
	if f := os.Getenv("PREVIEW_DEVTOOLS"); f != "" {
		file = f // a larger generated script, not committed
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var g previewGolden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	dir, home := t.TempDir(), t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "cli-token"), []byte(g.Token), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "secret"), []byte(g.Secret), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["preview"]}`), 0o600)
	for k, v := range map[string]string{"AGENT_SECRET_FILE": filepath.Join(dir, "secret"), "RUNTIME_CLI_NAME": "acme", "RUNTIME_MCP_SERVER": "acme-preview",
		"RUNTIME_PLATFORM_HOST": "app.example.org", "RUNTIME_RESERVED_PORTS": "", "RUNTIME_GATE_LISTEN": ""} {
		t.Setenv(k, v)
	}
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	defer old.Close()
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(g.Secret)
	cli := &gate.CLIToken{DataDir: dir, Headers: []string{"x-acme-token"}}
	sessionOK := func(r *http.Request) bool {
		c, err := r.Cookie("session-token")
		// The page session in the goldens was signed when they were taken: it is checked at the goldens' clock, not
		// today's, or the test fails once that session expires.
		return err == nil && agent.VerifySession(secret, c.Value, time.UnixMilli(g.Start)) == nil
	}
	d := deps{Home: home, DataDir: dir, CLI: cli, SessionOK: sessionOK, Tmux: tmux.New(tmux.Names{Socket: "hs-test-preview", Prefix: "hs-"}),
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || cli.OK(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	s := previewOf(d)
	clock := time.UnixMilli(g.Start)
	s.store.Now = func() time.Time { return clock }
	var ss strings.Builder
	for _, l := range g.Listeners {
		if l.PID != nil {
			ss.WriteString("LISTEN 0 511 127.0.0.1:" + previewItoa(l.Port) + " 0.0.0.0:* users:((\"" + *l.Command + "\",pid=" + previewItoa(*l.PID) + ",fd=1))\n")
		} else {
			ss.WriteString("LISTEN 0 1 0.0.0.0:" + previewItoa(l.Port) + " 0.0.0.0:*\n")
		}
	}
	s.scanner.SS = func() (string, error) { return ss.String(), nil }
	s.logs.Panes = func() (map[string]tmux.PaneInfo, bool) {
		out := map[string]tmux.PaneInfo{}
		for k, v := range g.Panes {
			out[k] = tmux.PaneInfo{PID: v.PID}
		}
		return out, true
	}
	s.logs.Capture = func(session string, lines int) (string, bool) { text, ok := g.Capture[session]; return text, ok }
	f.Add(f.AsRoutes(previewRoutes(d)...)...)
	f.Add(f.AsBodyRoutes(previewBodyRoutes(d)...)...)
	mux := http.NewServeMux()
	mux.HandleFunc("/__slot/", func(w http.ResponseWriter, r *http.Request) {
		s.proxy.AnswerDevtools(w, r, strings.TrimPrefix(r.URL.Path, "/__slot"), r.Header.Get("x-golden-slot"), previewTargetOf(json.RawMessage(r.Header.Get("x-golden-target"))))
	})
	mux.Handle("/", f)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	failures := 0
	for i, rawStep := range g.Steps {
		var st previewStep
		if err := json.Unmarshal(rawStep, &st); err != nil {
			t.Fatal(err)
		}
		if st.Kind == "op" {
			switch st.Op {
			case "clock":
				clock = time.UnixMilli(st.At)
			case "touch":
				s.store.Touch(st.Slot, previewTargetOf(st.Target))
			case "request":
				var e devtools.NetworkEntry
				_ = json.Unmarshal(st.Entry, &e)
				if id := s.store.RecordRequest(st.Slot, e); id != st.ID {
					t.Fatalf("step %d: request id %d, want %d", i, id, st.ID)
				}
			case "finish":
				s.store.Finish(st.Slot, st.ID, func(e *devtools.NetworkEntry) {
					for k, v := range st.Patch {
						switch k {
						case "status":
							e.Status = previewNum(v)
						case "size":
							e.Size = previewNum(v)
						case "durationMs":
							e.DurationMs = previewNum(v)
						case "mime":
							e.Mime = previewStr(v)
						case "error":
							e.Error = previewStr(v)
						case "blocked":
							e.Blocked = v == true
						case "type":
							e.Type = v.(string)
						}
					}
				})
			case "console":
				var e devtools.ConsoleEntry
				_ = json.Unmarshal(st.Entry, &e)
				s.store.RecordConsole(st.Slot, e)
			}
			continue
		}
		body, hasBody := previewBodyOf(st.Body)
		var rd io.Reader
		if hasBody {
			rd = strings.NewReader(body)
		}
		req, _ := http.NewRequest(st.Method, srv.URL+st.URL, rd)
		for k, v := range st.Headers {
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode == 299 && st.Out.Status == 404 && st.Out.Body == "" {
			continue // a path no route of the previous runtime has: the gate hands it on, and that runtime answers its own 404
		}
		bad := res.StatusCode != st.Out.Status || string(got) != st.Out.Body
		for _, k := range []string{"content-type", "cache-control", "allow", "etag", "x-content-type-options", "x-robots-tag"} {
			if res.Header.Get(k) != st.Out.Headers[k] {
				bad = true
				t.Errorf("step %d %s %s: header %s = %q, want %q", i, st.Method, st.URL, k, res.Header.Get(k), st.Out.Headers[k])
			}
		}
		if bad {
			failures++
			t.Errorf("step %d %s %s:\n got %d %.600s\nwant %d %.600s", i, st.Method, st.URL, res.StatusCode, got, st.Out.Status, st.Out.Body)
			if failures > 8 {
				t.FailNow()
			}
		}
	}
}

func previewItoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// The proxy through the gate: an HMR WebSocket of a local dev server passes both ways once the slice is on, private and
// loopback addresses of url targets are refused, and with the slice off the request is the previous runtime's.
func TestPreviewProxyThroughTheGate(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	secret := []byte("pair-secret-for-the-proxy-test-0123")
	_ = os.WriteFile(filepath.Join(dir, "secret"), secret, 0o600)
	t.Setenv("AGENT_SECRET_FILE", filepath.Join(dir, "secret"))
	t.Setenv("RUNTIME_PREVIEW_CLAIM_HEADER", "x-acme-preview")
	t.Setenv("RUNTIME_PREVIEW_CLAIM_AUDIENCE", "acme-preview-proxy")
	dev := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hmr" {
			c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				return
			}
			defer c.CloseNow()
			for {
				typ, msg, err := c.Read(context.Background())
				if err != nil {
					return
				}
				_ = c.Write(context.Background(), typ, append([]byte("hmr:"), msg...))
			}
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<!doctype html><script src="http://localhost:`+strconv.Itoa(portOf(strings.TrimPrefix(r.Host, "")))+`/x.js"></script>`)
	}))
	defer dev.Close()
	devPort := portOf(strings.TrimPrefix(dev.URL, "http://"))
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	defer old.Close()
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	d := deps{Home: home, DataDir: dir, CLI: &gate.CLIToken{DataDir: dir}, SessionOK: func(*http.Request) bool { return false },
		APIRefusal: func(*http.Request) (int, any, http.Header, bool) { return 401, nil, nil, true }}
	s := previewOf(d)
	f.Add(f.AsBodyRoutes(previewBodyRoutes(d)...)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	slot := "https://ai-alice-1.example.org"
	claim := func(target string) string {
		head := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
		body := `{"sub":"alice","target":` + target + `,"slotOrigin":"` + slot + `","portSlots":{` + map[bool]string{true: `"` + strconv.Itoa(devPort) + `":"` + slot + `"`, false: ""}[strings.Contains(target, "port")] +
			`},"aud":"acme-preview-proxy","exp":` + strconv.FormatInt(time.Now().Unix()+60, 10) + `}`
		signing := head + "." + base64.RawURLEncoding.EncodeToString([]byte(body))
		mac := hmac.New(sha256.New, secret)
		mac.Write([]byte(signing))
		return signing + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	}
	portClaim := claim(`{"kind":"port","port":` + strconv.Itoa(devPort) + `}`)
	path := "/agents" + platformAPI + "/preview/proxy"
	get := func(target, claimValue string) (int, string) {
		req, _ := http.NewRequest("GET", srv.URL+path+target, nil)
		req.Header.Set("x-acme-preview", claimValue)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if status, _ := get("/", portClaim); status != 299 {
		t.Fatalf("slice off: %d, want the previous runtime's answer", status)
	}
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["preview"]}`), 0o600)
	time.Sleep(1100 * time.Millisecond)
	if status, body := get("/", portClaim); status != 200 || !strings.Contains(body, slot+"/x.js") {
		t.Fatalf("page: %d %s", status, body)
	}
	for _, target := range []string{`{"kind":"url","origin":"http://127.0.0.1:` + strconv.Itoa(devPort) + `"}`, `{"kind":"url","origin":"http://10.0.0.1"}`,
		`{"kind":"url","origin":"http://[::1]:` + strconv.Itoa(devPort) + `"}`, `{"kind":"url","origin":"http://169.254.169.254"}`, `{"kind":"url","origin":"http://localhost:` + strconv.Itoa(devPort) + `"}`} {
		if status, _ := get("/", claim(target)); status != 403 {
			t.Errorf("url target %s: %d, want 403", target, status)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+path+"/hmr", &websocket.DialOptions{HTTPHeader: http.Header{"x-acme-preview": {portClaim}}})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	for _, m := range []string{"one", "two"} {
		if err := ws.Write(ctx, websocket.MessageText, []byte(m)); err != nil {
			t.Fatal(err)
		}
		_, got, err := ws.Read(ctx)
		if err != nil || string(got) != "hmr:"+m {
			t.Fatalf("hmr echo %q %v", got, err)
		}
	}
	log := s.store.Read(slot, 0).Network
	if len(log) == 0 || log[len(log)-1].Type != "websocket" || log[len(log)-1].Status == nil || *log[len(log)-1].Status != 101 {
		t.Errorf("the socket is not logged as a 101 websocket: %+v", log)
	}
}
