package usagereport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/nodejson"
)

// testdata/golden.json: what the previous runtime's dispatcher and handler answered (testdata/gen, under node) on the
// same transcripts, and the facts file it wrote after each request. Each request must be answered alike, and a facts
// file it wrote must be the one written here.

type goldenFile struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Mtime   float64 `json:"mtime"`
}

type goldenStep struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	ETag          bool              `json:"etag"`
	Answer        string            `json:"answer"`
	CacheWritten  string            `json:"cacheWritten"`

	Append, Remove, Add string
	Content             string
	Mtime               float64
	Clock               float64
}

type golden struct {
	Now   float64      `json:"now"`
	Files []goldenFile `json:"files"`
	Steps []goldenStep `json:"steps"`
}

const (
	testRoot  = "work"
	testToken = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

var testPrefixes = []string{"ABC", "DEF", "GHI"}

// filler puts this test's words where the fixtures have placeholders.
func filler(root string) func(string) string {
	r := strings.NewReplacer("$ROOT", root, "{ROOT}", testRoot, "{P1}", testPrefixes[0], "{P2}", testPrefixes[1], "{P3}", testPrefixes[2],
		"{p1}", strings.ToLower(testPrefixes[0]), "{p2}", strings.ToLower(testPrefixes[1]), "{p3}", strings.ToLower(testPrefixes[2]))
	return r.Replace
}

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func cookieToken(r *http.Request) string {
	c, err := r.Cookie("session-token")
	if err != nil {
		return ""
	}
	return c.Value
}

func testClaims(r *http.Request) map[string]any {
	switch t := cookieToken(r); {
	case t == "good":
		return map[string]any{}
	case strings.HasPrefix(t, "sub:"):
		return map[string]any{"sub": t[4:]}
	}
	return nil
}

func newTestRoute(root string, clock *fakeClock) (*Route, *Collector) {
	home := filepath.Join(root, "home")
	c := NewCollector(Config{
		ClaudeRoot: filepath.Join(home, ".claude", "projects"),
		CodexRoot:  filepath.Join(home, ".codex", "sessions"),
		CacheFile:  filepath.Join(root, "data", "stats", "usage-v2.json"),
		Projects:   &Projects{Root: testRoot, TaskPrefixes: testPrefixes},
		Clock:      Clock{Offset: 3 * 3600},
		Timezone:   "Europe/Moscow",
		Machine:    "box",
		Workers:    2,
		Now:        clock.now,
	})
	rt := &Route{
		Collector:  c,
		Authorized: func(r *http.Request) bool { return testClaims(r) != nil || r.Header.Get("x-cli-token") == testToken },
		SessionOK:  func(r *http.Request) bool { return testClaims(r) != nil },
		Claims:     testClaims,
		Owner:      "agent@box",
		Machine:    "box",
	}
	return rt, c
}

func writeFixture(t *testing.T, root string, fill func(string) string, f goldenFile) {
	t.Helper()
	p := filepath.Join(root, f.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(fill(f.Content)), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Unix(int64(f.Mtime), 0)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func layOut(t *testing.T, g golden) (string, func(string) string) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	fill := filler(root)
	for _, f := range g.Files {
		writeFixture(t, root, fill, f)
	}
	if err := os.MkdirAll(filepath.Join(root, "ws", testRoot, "app", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "ws", "lib-wt"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "ws", "lib-wt", ".git"), []byte("gitdir: "+filepath.Join(root, "ws", testRoot, "lib", ".git", "worktrees", "lib-wt")+"\n"), 0o644)
	proj := "home/.claude/projects/-w-app"
	if err := os.Symlink(filepath.Join(root, proj, "5d3c1e0a-2222-4a2b-9c3d-000000000002.jsonl"), filepath.Join(root, proj, "link.jsonl")); err != nil {
		t.Fatal(err)
	}
	return root, fill
}

func loadGolden(t *testing.T) golden {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	return g
}

// sameCache compares two facts files as data, the sizes left out (the fixtures' paths differ in length between the
// two runs); every size written here must be the file's.
func sameCache(t *testing.T, want, got string) {
	t.Helper()
	var a, b map[string]any
	if err := json.Unmarshal([]byte(want), &a); err != nil {
		t.Fatalf("golden facts file: %v", err)
	}
	if err := json.Unmarshal([]byte(got), &b); err != nil {
		t.Fatalf("facts file written: %v", err)
	}
	for side, m := range []map[string]any{a, b} {
		for name, e := range m["files"].(map[string]any) {
			entry := e.(map[string]any)
			if side == 1 {
				st, err := os.Stat(name)
				if err != nil || float64(st.Size()) != entry["size"] {
					t.Errorf("%s: size %v is not the file's", name, entry["size"])
				}
			}
			delete(entry, "size")
		}
	}
	if !reflect.DeepEqual(a, b) {
		wa, _ := json.Marshal(a)
		wb, _ := json.Marshal(b)
		t.Errorf("facts file differs:\nwant %s\n got %s", wa, wb)
	}
}

func TestGolden(t *testing.T) {
	g := loadGolden(t)
	root, fill := layOut(t, g)
	clock := &fakeClock{t: time.UnixMilli(int64(g.Now))}
	rt, c := newTestRoute(root, clock)
	cache := ""
	lastETag := ""
	for i, s := range g.Steps {
		switch {
		case s.Clock != 0:
			clock.add(time.Duration(s.Clock) * time.Second)
			continue
		case s.Append != "":
			p := filepath.Join(root, s.Append)
			f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = f.WriteString(fill(s.Content))
			_ = f.Close()
			mt := time.Unix(int64(s.Mtime), 0)
			_ = os.Chtimes(p, mt, mt)
			continue
		case s.Remove != "":
			_ = os.Remove(filepath.Join(root, s.Remove))
			continue
		case s.Add != "":
			writeFixture(t, root, fill, goldenFile{Path: s.Add, Content: s.Content, Mtime: s.Mtime})
			continue
		}
		q := url.Values{}
		for _, kv := range s.Query {
			q.Add(kv[0], kv[1])
		}
		r := httptest.NewRequest(s.Method, s.Path+"?"+q.Encode(), nil)
		for k, v := range s.Headers {
			if k == "if-none-match" {
				v = lastETag
			}
			r.Header.Set(k, v)
		}
		status, v, h := rt.Answer(r)
		var body []byte
		ct := ""
		switch x := v.(type) {
		case gate.Raw:
			body, ct = x.Body, x.ContentType
		case nil:
		default:
			body, ct = nodejson.Marshal(x), "application/json; charset=utf-8"
		}
		if status != s.Status {
			t.Fatalf("step %d: status %d, want %d (%s)", i, status, s.Status, body)
		}
		if want := fill(s.Answer); string(body) != want {
			t.Fatalf("step %d: answer\n got %s\nwant %s", i, body, want)
		}
		if s.Status != http.StatusNotModified && ct != s.AnswerHeaders["content-type"] {
			t.Errorf("step %d: content type %q, want %q", i, ct, s.AnswerHeaders["content-type"])
		}
		if got := h.Get("Cache-Control"); got != s.AnswerHeaders["cache-control"] {
			t.Errorf("step %d: cache-control %q, want %q", i, got, s.AnswerHeaders["cache-control"])
		}
		if tag := h.Get("ETag"); (tag != "") != s.ETag {
			t.Errorf("step %d: etag %q, want one: %v", i, tag, s.ETag)
		} else if tag != "" {
			if status == http.StatusOK && tag != nodejson.ETag(body) {
				t.Errorf("step %d: etag %s is not the body's", i, tag)
			}
			lastETag = tag
		}
		raw, _ := os.ReadFile(c.cfg.CacheFile)
		written := ""
		if string(raw) != cache {
			written = string(raw)
		}
		cache = string(raw)
		switch {
		case s.CacheWritten == "" && written != "":
			t.Errorf("step %d: the facts file was written, the previous runtime did not", i)
		case s.CacheWritten != "" && written == "":
			t.Errorf("step %d: the facts file was not written", i)
		case s.CacheWritten != "":
			sameCache(t, fill(s.CacheWritten), written)
		}
	}
}
