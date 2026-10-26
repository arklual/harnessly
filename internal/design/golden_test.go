package design

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/prompts"
	"github.com/ly-solutions/harnessly/internal/push"
)

type goldenRequest struct {
	Server        string
	Method        string
	URL           string
	Headers       map[string]string
	Body          *string
	At            *int64
	Status        int
	AnswerHeaders map[string]string
	Answer        string
	SetSubs       []push.Subscription
	Statuses      map[string]int
}

type goldenFile struct {
	Now        int64
	Token      string
	Files      map[string]string
	Dates      map[string]float64
	ClaimFiles map[string]string
	Sources    []struct {
		TabID       string `json:"tabId"`
		TmuxSession string `json:"tmuxSession"`
		JSONLPath   *string
	}
	Transcript string
	Requests   []goldenRequest
	Tree       map[string]string
	DataFiles  map[string]string
	Typed      []typedLine
	Pushed     []pushedMessage
	Removed    []string
}

type typedLine struct {
	Session string `json:"session"`
	Text    string `json:"text"`
	Via     string `json:"via"`
}

type pushedMessage struct {
	Endpoint string          `json:"endpoint"`
	Message  json.RawMessage `json:"message"`
}

var neutralTexts = Texts{CLI: "acme-cli", Host: "ai.acme.ru", AppName: "Acme AI", Tag: "acme-design", HistoryEmail: "design@acme.local",
	SharedDir: "/srv/acme-design", KitGlobal: "AcmeDS", PagesGlobal: "AcmePages", ArgsGlobal: "__ACME_ARGS"}

type fakeTerminal struct {
	tabs   map[string][2]string
	fronts map[string][2]any
	typed  []typedLine
}

func (f *fakeTerminal) AgentTab(id string) (string, string, bool) {
	t, ok := f.tabs[id]
	return t[0], t[1], ok
}

func (f *fakeTerminal) Front(s string) (bool, string) {
	if v, ok := f.fronts[s]; ok {
		return v[0].(bool), v[1].(string)
	}
	return true, "zsh"
}

func (f *fakeTerminal) Deliver(session, text string, viaEditor bool) error {
	via := "type"
	if viaEditor {
		via = "editor"
	}
	f.typed = append(f.typed, typedLine{session, text, via})
	return nil
}

func (f *fakeTerminal) NeedsEditor(text string) bool { return prompts.NeedsEditor(text) }

func etagOf(b []byte) string {
	sum := sha1.Sum(b)
	return `"` + base64.RawURLEncoding.EncodeToString(sum[:])[:27] + `"`
}

// writeAnswer is the gate's writing of a JSON route's answer.
func writeGateAnswer(w http.ResponseWriter, a Answer) {
	for k, vs := range a.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if a.Body == nil {
		w.WriteHeader(a.Status)
		return
	}
	var raw []byte
	switch b := a.Body.(type) {
	case gate.Raw:
		w.Header().Set("Content-Type", b.ContentType)
		raw = b.Body
	default:
		raw = gate.Encode(b)
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
		}
	}
	w.WriteHeader(a.Status)
	_, _ = w.Write(raw)
}

// goldenLibrary lays the fixture out under root as the generator did.
func goldenLibrary(t *testing.T, g goldenFile, root string) (lib, data, home, transcript string) {
	home, data = filepath.Join(root, "home"), filepath.Join(root, "data")
	lib = filepath.Join(root, "ws", "design-artifacts")
	for _, d := range []string{home, data, lib} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(data, "cli-token"), []byte(g.Token), 0o600)
	_ = os.WriteFile(filepath.Join(data, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"Alpha"}]}`), 0o600)
	for name, content := range g.Files {
		file := filepath.Join(lib, name)
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		b := []byte(content)
		if strings.HasSuffix(name, ".png") {
			var out []byte
			for _, r := range content {
				out = append(out, byte(r))
			}
			b = out
		}
		if err := os.WriteFile(file, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.Symlink(filepath.Join(lib, "files-only"), filepath.Join(lib, "linked"))
	// Deepest first, so a folder's date is not moved by the files dated after it.
	names := make([]string, 0, len(g.Dates))
	for n := range g.Dates {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool { return strings.Count(names[i], "/") > strings.Count(names[j], "/") })
	for _, n := range names {
		at := time.Unix(int64(g.Dates[n]), 0)
		_ = os.Chtimes(filepath.Join(lib, n), at, at)
	}
	claims := filepath.Join(lib, ".claims", "main-site")
	_ = os.MkdirAll(claims, 0o755)
	for n, c := range g.ClaimFiles {
		_ = os.WriteFile(filepath.Join(claims, n), []byte(c), 0o644)
	}
	transcript = filepath.Join(home, ".claude", "projects", "-ws", "sess.jsonl")
	_ = os.MkdirAll(filepath.Dir(transcript), 0o755)
	_ = os.WriteFile(transcript, []byte(strings.ReplaceAll(g.Transcript, "$ROOT", root)), 0o644)
	return
}

type goldenRig struct {
	g        goldenFile
	root     string
	lib      string
	l        *Library
	a        *API
	term     *fakeTerminal
	now      *int64
	subs     *[]push.Subscription
	statuses *map[string]int
	pushed   *[]pushedMessage
	removed  *[]string
	routes   map[string]func(*http.Request) Answer
}

// newGoldenRig lays out the fixture and wires an API to it the way golden.json was answered.
func newGoldenRig(t *testing.T) *goldenRig {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g goldenFile
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Clients json.RawMessage
	}
	_ = json.Unmarshal(raw, &doc)
	clientsV, _ := jsvalue.Parse(doc.Clients)
	clients := clientsV.(*jsvalue.Object)

	root, _ := filepath.EvalSymlinks(t.TempDir())
	lib, data, home, transcript := goldenLibrary(t, g, root)
	now := g.Now
	prevNow := nowMs
	nowMs = func() int64 { return now }
	t.Cleanup(func() { nowMs = prevNow })

	l := New(Config{DataDir: data, WorkspaceDir: filepath.Join(root, "ws"), Port: 0, PlatformAPI: "/api/platform", OwnerEmail: "ann@example.com",
		ClaudeProjects: filepath.Join(home, ".claude", "projects"), Texts: neutralTexts})
	l.cfg.Port = freePort(t)
	l.osUser = func() string { return "agent" }
	t.Cleanup(l.StopServer)
	t.Cleanup(l.StopWatching)
	t.Cleanup(l.StopBackground)
	sessions := map[string]string{}
	transcripts := map[string]string{}
	for _, s := range g.Sources {
		sessions[s.TabID] = s.TmuxSession
		if s.JSONLPath != nil {
			transcripts[s.TabID] = transcript
		}
	}
	l.SetKeeper(func() ([]Chat, bool) {
		var out []Chat
		for _, id := range clients.Keys() {
			c := clients.Object(id)
			last := 0.0
			if le := c.Object("lastEvent"); le != nil {
				last, _ = le.Number("at")
			}
			out = append(out, Chat{TabID: id, PanelType: c.String("panelType"), State: c.String("cliState"), AgentSummary: c.String("agentSummary"),
				TabName: c.String("tabName"), WorkspaceID: c.String("workspaceId"), Session: sessions[id], Transcript: transcripts[id], LastEventAt: last})
		}
		return out, true
	})
	term := &fakeTerminal{
		tabs:   map[string][2]string{"tab-2": {"hs-ws-a-tab-2", "claude-code"}, "tab-3": {"hs-ws-a-tab-3", "codex-cli"}, "tab-1": {"hs-ws-a-tab-1", "terminal"}},
		fronts: map[string][2]any{"hs-ws-a-tab-2": {false, "claude"}, "hs-ws-a-tab-3": {false, "codex"}},
	}
	var subs []push.Subscription
	statuses := map[string]int{}
	var pushed []pushedMessage
	var removed []string
	tokenOK := func(r *http.Request) bool { return r.Header.Get("X-Cli-Token") == g.Token }
	sessionOK := func(r *http.Request) bool { return strings.Contains(r.Header.Get("Cookie"), "session-token=good") }
	a := &API{L: l, Session: sessionOK, CLIToken: tokenOK, PlatformAPI: "/api/platform", Terminal: term, AppBase: "/agents",
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || tokenOK(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		Push: func(req PushRequest) []int {
			msg := PushMessage(req, "acme:")
			var out []int
			for _, s := range PhoneTargets(subs, req.DeviceID) {
				st := statuses[s.Endpoint]
				if st == 0 {
					st = 201
				}
				b, _ := json.Marshal(msg)
				pushed = append(pushed, pushedMessage{Endpoint: s.Endpoint, Message: b})
				if st == 404 || st == 410 {
					removed = append(removed, s.Endpoint)
				}
				out = append(out, st)
			}
			return out
		},
	}
	routes := map[string]func(*http.Request) Answer{
		"/api/platform/design/library": a.Library, "/api/platform/design/library/boards": a.Boards, "/api/platform/design/library/locate": a.Locate,
		"/api/platform/design/library/send": a.Send, "/api/platform/design/library/message": a.Message, "/api/cli/design": a.CLIDesign,
		"/api/cli/design/claims": a.CLIClaims,
	}
	routes["/api/cli/design/history"] = a.CLIHistory
	return &goldenRig{g: g, root: root, lib: lib, l: l, a: a, term: term, now: &now, subs: &subs, statuses: &statuses, pushed: &pushed, removed: &removed, routes: routes}
}

func TestGoldenRoutes(t *testing.T) {
	rig := newGoldenRig(t)
	g, root, lib, l, term, routes := rig.g, rig.root, rig.lib, rig.l, rig.term, rig.routes
	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	portRe := regexp.MustCompile(`"port":\d+`)
	var lastEtag, supportVersion string
	for i, req := range g.Requests {
		if req.SetSubs != nil || req.Statuses != nil {
			*rig.subs, *rig.statuses = req.SetSubs, req.Statuses
			continue
		}
		if req.At != nil {
			*rig.now = *req.At
		}
		url := strings.Replace(req.URL, "VERSION", supportVersion, 1)
		var body io.Reader
		if req.Body != nil {
			body = strings.NewReader(*req.Body)
		}
		// Go's server refuses a malformed escape in the request line itself (400) before any handler sees it.
		if strings.Contains(url, "%A4%A") {
			continue
		}
		r := httptest.NewRequest(req.Method, url, body)
		for k, v := range req.Headers {
			if k == "host" {
				r.Host = v
				continue
			}
			if v == "ETAG" {
				v = lastEtag
			}
			r.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		if req.Server == "lib" {
			l.serveLibrary(w, r, 18470)
		} else {
			path := url
			if i := strings.IndexByte(path, '?'); i >= 0 {
				path = path[:i]
			}
			writeGateAnswer(w, routes[path](r))
		}
		res := w.Result()
		got, _ := io.ReadAll(res.Body)
		if res.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(bytes.NewReader(got))
			if err != nil {
				t.Fatalf("#%d %s %s: gzip: %v", i, req.Method, url, err)
			}
			got, _ = io.ReadAll(zr)
		}
		if et := res.Header.Get("ETag"); et != "" {
			lastEtag = et
		}
		if strings.HasSuffix(url, "support.js") && res.Header.Get("ETag") != "" {
			et := res.Header.Get("ETag")
			supportVersion = et[3 : len(et)-1]
		}
		answer := scrub(jscompat.DecodeUTF8(got))
		if req.Server == "api" {
			answer = portRe.ReplaceAllString(answer, `"port":0`)
		}
		if res.StatusCode != req.Status || answer != req.Answer {
			t.Errorf("#%d %s %s: got %d %q\nwant %d %q", i, req.Method, url, res.StatusCode, clipText(answer), req.Status, clipText(req.Answer))
			continue
		}
		for k, want := range req.AnswerHeaders {
			gotH := scrub(res.Header.Get(k))
			if k == "etag" && req.Server == "api" {
				gotH = etagOf([]byte(answer))
			}
			if k == "content-length" && req.Server == "api" {
				continue
			}
			if gotH != want {
				t.Errorf("#%d %s %s: header %s %q, want %q", i, req.Method, url, k, gotH, want)
			}
		}
		for _, k := range []string{"Etag", "Allow", "Cache-Control", "Content-Encoding"} {
			if res.Header.Get(k) != "" {
				if _, ok := req.AnswerHeaders[strings.ToLower(k)]; !ok {
					t.Errorf("#%d %s %s: extra header %s: %s", i, req.Method, url, k, res.Header.Get(k))
				}
			}
		}
	}
	tree := map[string]string{}
	_ = filepath.Walk(lib, func(p string, info os.FileInfo, err error) error {
		if err != nil || p == lib {
			return nil
		}
		rel, _ := filepath.Rel(lib, p)
		if info.Mode()&os.ModeSymlink != 0 {
			tree[rel] = "link"
			return nil
		}
		if info.IsDir() || strings.Contains(rel, "node_modules") {
			return nil
		}
		b, _ := os.ReadFile(p)
		tree[rel] = scrub(string(b))
		return nil
	})
	for name, want := range g.Tree {
		if got, ok := tree[name]; !ok {
			t.Errorf("file %s missing", name)
		} else if got != want && !strings.HasSuffix(name, ".png") {
			t.Errorf("file %s:\n%s\nwant\n%s", name, clipText(got), clipText(want))
		}
	}
	for name := range tree {
		if _, ok := g.Tree[name]; !ok {
			t.Errorf("extra file %s", name)
		}
	}
	for i := range term.typed {
		term.typed[i].Text = scrub(term.typed[i].Text)
	}
	if b1, b2 := mustJSON(term.typed), mustJSON(g.Typed); b1 != b2 {
		t.Errorf("typed %s\nwant %s", b1, b2)
	}
	if b1, b2 := mustJSON(*rig.pushed), mustJSON(g.Pushed); b1 != b2 {
		t.Errorf("pushed %s\nwant %s", b1, b2)
	}
	if b1, b2 := mustJSON(*rig.removed), mustJSON(g.Removed); b1 != b2 {
		t.Errorf("removed %s\nwant %s", b1, b2)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	var x any
	_ = json.Unmarshal(b, &x)
	b, _ = json.Marshal(x)
	return string(b)
}

func clipText(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}

func freePort(t *testing.T) int {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	var port int
	_, _ = strings.NewReader(""), 0
	parts := strings.Split(srv.Listener.Addr().String(), ":")
	for _, c := range parts[len(parts)-1] {
		port = port*10 + int(c-'0')
	}
	return port
}
