package statusline

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/history"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/limits"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// testdata/golden.json: what the previous runtime's dispatcher and handler answered (testdata/gen, under node) to the
// same posts on the same files, the files each post changed and what it broadcast.

type goldenStep struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Headers       map[string]string `json:"headers"`
	Body          json.RawMessage   `json:"body"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
	Broadcasts    []string          `json:"broadcasts"`
	Changed       map[string]string `json:"changed"`

	Write, Remove, Content string
}

type fixture struct {
	t          *testing.T
	root, data string
	token      string
	sink       *Sink
	srv        *httptest.Server

	mu         sync.Mutex
	broadcasts []string
}

// ultracodeOf looks sessions up in the layouts as the previous runtime did: the tab's agent session, its remembered
// launch effort.
func ultracodeOf(data string) func(any) bool {
	return func(id any) bool {
		raw, _ := os.ReadFile(filepath.Join(data, "workspaces", "ws-a", "layout.json"))
		v, _ := jsvalue.Parse(raw)
		layout, _ := v.(*jsvalue.Object)
		for _, t := range layout.Object("root").Array("tabs") {
			tab := t.(*jsvalue.Object)
			sid, _ := tab.Object("agentState").Get("sessionId")
			effort, _ := tab.Object("meta").Object("launch").Get("effort")
			if s, ok := sid.(string); ok && s != "" && sid == id && effort == "ultracode" {
				return true
			}
		}
		return false
	}
}

func newFixture(t *testing.T, token string, data map[string]string, now int64) *fixture {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	f := &fixture{t: t, root: root, data: filepath.Join(root, "data"), token: token}
	for name, content := range data {
		f.write(filepath.Join(f.data, name), content)
	}
	_ = os.MkdirAll(filepath.Join(root, "home"), 0o755)
	first := func(r *http.Request) bool {
		for _, h := range []string{"X-Cli-Token", "X-Old-Cli-Token"} {
			if vs, ok := r.Header[h]; ok {
				return vs[0] == token
			}
		}
		return false
	}
	session := func(r *http.Request) bool {
		c, err := r.Cookie("session-token")
		return err == nil && c.Value == "good"
	}
	at := time.UnixMilli(now)
	f.sink = &Sink{Authorized: func(r *http.Request) bool { return first(r) || session(r) }, TokenOK: first,
		Home: filepath.Join(root, "home"), ClaudeDir: filepath.Join(root, "home", ".claude"),
		Limits: &limits.Cache{File: filepath.Join(f.data, "rate-limits.json")}, Stats: &history.Stats{Dir: filepath.Join(f.data, "session-stats")},
		Ultracode: ultracodeOf(f.data), Script: filepath.Join(f.data, "statusline.sh"), Now: func() time.Time { return at },
		Broadcast: func(s *jsvalue.Object) {
			f.mu.Lock()
			f.broadcasts = append(f.broadcasts, string(jsvalue.Stringify(s, "")))
			f.mu.Unlock()
		}}
	f.srv = httptest.NewServer(f.sink)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) write(p, content string) {
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) scrub(s string) string { return strings.ReplaceAll(s, f.root, "$ROOT") }

// files are the stats files and the limits, as the generator lists them.
func (f *fixture) files() map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(filepath.Join(f.data, "session-stats"))
	for _, e := range entries {
		raw, _ := os.ReadFile(filepath.Join(f.data, "session-stats", e.Name()))
		out["session-stats/"+e.Name()] = string(raw)
	}
	raw, _ := os.ReadFile(filepath.Join(f.data, "rate-limits.json"))
	out["rate-limits.json"] = string(raw)
	return out
}

type answer struct {
	status     int
	header     http.Header
	body       []byte
	broadcasts []string
}

func (f *fixture) post(method, path string, headers map[string]string, body string, hasBody bool) answer {
	f.mu.Lock()
	f.broadcasts = nil
	f.mu.Unlock()
	var rd io.Reader
	if hasBody {
		rd = strings.NewReader(strings.ReplaceAll(body, "$ROOT", f.root))
	}
	req, _ := http.NewRequest(method, f.srv.URL+path, rd)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	return answer{res.StatusCode, res.Header, b, append([]string(nil), f.broadcasts...)}
}

// sameFile compares a stats file byte for byte and rate-limits.json as data: its writer here keeps no key order.
func sameFile(name, got, want string) bool {
	if name != "rate-limits.json" {
		return got == want
	}
	var a, b any
	return json.Unmarshal([]byte(got), &a) == nil && json.Unmarshal([]byte(want), &b) == nil && reflect.DeepEqual(a, b)
}

func TestSinkAnswersAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Now   int64             `json:"now"`
		Token string            `json:"token"`
		Data  map[string]string `json:"data"`
		Steps []goldenStep      `json:"steps"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, golden.Token, golden.Data, golden.Now)
	seen := f.files()
	for i, g := range golden.Steps {
		if g.Method == "" {
			if g.Write != "" {
				f.write(filepath.Join(f.root, g.Write), g.Content)
			} else {
				_ = os.Remove(filepath.Join(f.root, g.Remove))
			}
			continue
		}
		body, hasBody := "", len(g.Body) > 0 && string(g.Body) != "null"
		if hasBody && json.Unmarshal(g.Body, &body) != nil {
			var rep struct{ Repeat int }
			_ = json.Unmarshal(g.Body, &rep)
			body = strings.Repeat("a", rep.Repeat)
		}
		a := f.post(g.Method, g.Path, g.Headers, body, hasBody)
		where := g.Method + " " + g.Path + " #" + strconv.Itoa(i)
		if a.status != g.Status || f.scrub(string(a.body)) != g.Answer {
			t.Fatalf("%s %s\n got %d %q\nwant %d %q", where, body, a.status, f.scrub(string(a.body)), g.Status, g.Answer)
		}
		checkHeaders(t, where, a, g.AnswerHeaders, f.scrub)
		if len(a.broadcasts) != len(g.Broadcasts) {
			t.Fatalf("%s: broadcasts %v, want %v", where, a.broadcasts, g.Broadcasts)
		}
		for j := range a.broadcasts {
			if f.scrub(a.broadcasts[j]) != g.Broadcasts[j] {
				t.Fatalf("%s: broadcast\n got %s\nwant %s", where, a.broadcasts[j], g.Broadcasts[j])
			}
		}
		now := f.files()
		for name, content := range now {
			want, changed := g.Changed[name]
			if changed != (seen[name] != content) || changed && !sameFile(name, f.scrub(content), want) {
				t.Fatalf("%s: %s\n got %s\nwant %s (changed %v)", where, name, content, want, changed)
			}
		}
		seen = now
	}
}

func checkHeaders(t *testing.T, where string, a answer, want map[string]string, scrub func(string) string) {
	t.Helper()
	for k, w := range want {
		have := a.header.Get(k)
		switch {
		case k == "content-length":
		case k == "etag" && scrub(string(a.body)) != string(a.body):
			if have != nodejson.ETag(a.body) {
				t.Errorf("%s: etag %s", where, have)
			}
		case scrub(have) != w:
			t.Errorf("%s: %s = %q, want %q", where, k, have, w)
		}
	}
	for _, k := range []string{"ETag", "Allow", "Cache-Control", "Content-Type"} {
		if _, ok := want[strings.ToLower(k)]; !ok && a.header.Get(k) != "" {
			t.Errorf("%s: unexpected %s: %s", where, k, a.header.Get(k))
		}
	}
}

// TS_GOLDEN points at generated status lines the generator posted (GEN_RANDOM), one after another on one set of
// files: every answer, broadcast and file change must be the previous runtime's.
func TestGeneratedStatusLines(t *testing.T) {
	file := os.Getenv("TS_GOLDEN")
	if file == "" {
		t.Skip("TS_GOLDEN not set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Now   int64             `json:"now"`
		Token string            `json:"token"`
		Data  map[string]string `json:"data"`
		Cases []struct {
			Body       string            `json:"body"`
			Status     int               `json:"status"`
			Answer     string            `json:"answer"`
			Broadcasts []string          `json:"broadcasts"`
			Changed    map[string]string `json:"changed"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, set.Token, set.Data, set.Now)
	f.write(filepath.Join(f.root, "home", ".claude", "settings.json"), `{"statusLine":{"type":"command","command":"cat"}}`)
	seen := f.files()
	failed := 0
	for i, c := range set.Cases {
		a := f.post("POST", "/api/status/statusline", map[string]string{"x-cli-token": set.Token, "content-type": "application/json"}, c.Body, true)
		ok := a.status == c.Status && string(a.body) == c.Answer && len(a.broadcasts) == len(c.Broadcasts)
		for j := 0; ok && j < len(a.broadcasts); j++ {
			ok = a.broadcasts[j] == c.Broadcasts[j]
		}
		now := f.files()
		for name, content := range now {
			want, changed := c.Changed[name]
			if changed != (seen[name] != content) || changed && !sameFile(name, content, want) {
				ok = false
			}
		}
		seen = now
		if !ok {
			failed++
			if failed <= 5 {
				t.Errorf("case %d %s\n got %d %s %v\nwant %d %s %v %v", i, c.Body, a.status, a.body, a.broadcasts, c.Status, c.Answer, c.Broadcasts, c.Changed)
			}
		}
	}
	t.Logf("%d cases, %d differ", len(set.Cases), failed)
}
