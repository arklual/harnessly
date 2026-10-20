package sources

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/messagecounts"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// testdata/golden.json: what the previous runtime's dispatcher, conversation-sources and message-counts handlers
// answered (testdata/gen, under node) while the same transcripts grew, shrank and were replaced between requests.

type goldenStep struct {
	Op      string `json:"op"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Corpus  string `json:"corpus"`
	Huge    int    `json:"huge"`
	Tail    string `json:"tail"`
	Repeat  *struct {
		Line  string `json:"line"`
		Count int    `json:"count"`
		Tail  string `json:"tail"`
	} `json:"repeat"`

	Method        string            `json:"method"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
}

func testLinks() *Links {
	return &Links{TrackerURL: "https://tracker.example.io", TrackerHosts: []string{"tracker.example.io", "youtrack.example.io"},
		IssueKeys: []string{"ABC", "EXP", "OPS"}, GitLabHosts: []string{"gitlab.com", "gitlab.example.io"},
		DocsHost: "a.example.io", ReaderHost: "ai.example.io", ReaderPath: "/agents/docs/"}
}

func (s goldenStep) content(t *testing.T) string {
	switch {
	case s.Repeat != nil:
		return strings.Repeat(s.Repeat.Line+"\n", s.Repeat.Count) + s.Repeat.Tail
	case s.Huge > 0:
		return `{"type":"assistant","message":{"content":[{"type":"text","text":"` + strings.Repeat("h", s.Huge) +
			` https://x.example.org/huge"}]}}` + "\n" + s.Tail
	case s.Corpus != "":
		raw, err := os.ReadFile(filepath.Join("..", "transcript", "testdata", s.Corpus))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	return s.Content
}

func (s goldenStep) apply(t *testing.T, root string) {
	p := filepath.Join(root, s.Path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	var err error
	switch s.Op {
	case "write":
		err = os.WriteFile(p, []byte(s.content(t)), 0o644)
	case "append":
		var f *os.File
		if f, err = os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			_, err = f.WriteString(s.content(t))
			f.Close()
		}
	case "replace":
		if err = os.WriteFile(p+".new", []byte(s.content(t)), 0o644); err == nil {
			err = os.Rename(p+".new", p)
		}
	case "remove":
		err = os.RemoveAll(p)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// testRoutes are both routes behind a gate, as the runtime serves them.
func testRoutes(t *testing.T, home, token string) *httptest.Server {
	authorized := func(r *http.Request) bool {
		if sessionOK(r) {
			return true
		}
		for _, h := range []string{"X-Cli-Token", "X-Old-Cli-Token"} {
			if vs, ok := r.Header[h]; ok {
				return vs[0] == token
			}
		}
		return false
	}
	src := &Route{Links: testLinks(), Home: home, Authorized: authorized, SessionOK: sessionOK}
	counts := &messagecounts.Route{Home: home, Authorized: authorized}
	g, err := gate.New(gate.Config{DataDir: t.TempDir(), Upstream: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(g.AsRoutes(
		gate.JSONRoute{Match: func(r *http.Request) bool { return strings.Contains(r.URL.Path, "/sources") }, Compute: src.Answer},
		gate.JSONRoute{Match: func(r *http.Request) bool { return strings.Contains(r.URL.Path, "/message-counts") }, Compute: counts.Answer},
	)...)
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv
}

func sessionOK(r *http.Request) bool {
	c, err := r.Cookie("session-token")
	return err == nil && c.Value == "good"
}

func TestAnswersAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Token string       `json:"token"`
		Steps []goldenStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	srv := testRoutes(t, filepath.Join(root, "home"), golden.Token)
	lastEtag := ""
	for i, c := range golden.Steps {
		if c.Op != "" {
			c.apply(t, root)
			continue
		}
		var q []string
		for _, kv := range c.Query {
			q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(strings.ReplaceAll(kv[1], "$ROOT", root)))
		}
		u := srv.URL + c.Path
		if len(q) > 0 {
			u += "?" + strings.Join(q, "&")
		}
		req, _ := http.NewRequest(c.Method, u, nil)
		for k, v := range c.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			req.Header.Set(k, v)
		}
		where := fmt.Sprintf("#%d %s %s %v", i, c.Method, c.Path, c.Query)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		got := strings.ReplaceAll(string(body), root, "$ROOT")
		if res.StatusCode != c.Status || got != c.Answer {
			t.Fatalf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, around(got, c.Answer), c.Status, around(c.Answer, got))
		}
		for k, want := range c.AnswerHeaders {
			have := res.Header.Get(k)
			switch {
			case k == "content-length" || k == "content-type" && res.StatusCode == http.StatusNotModified:
			case k == "etag" && got != string(body):
				if have != nodejson.ETag(body) {
					t.Errorf("%s: etag %s", where, have)
				}
			case have != want:
				t.Errorf("%s: %s = %q, want %q", where, k, have, want)
			}
		}
		for _, k := range []string{"ETag", "Allow", "Cache-Control"} {
			if _, ok := c.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("%s: unexpected %s: %s", where, k, res.Header.Get(k))
			}
		}
	}
}

// TestGeneratedTranscripts replays generated transcripts read in random stretches against what the previous runtime
// answered after each, from a file the generator writes (GEN_RANDOM): SOURCES_RANDOM=<that file>.
func TestGeneratedTranscripts(t *testing.T) {
	path := os.Getenv("SOURCES_RANDOM")
	if path == "" {
		t.Skip("SOURCES_RANDOM not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Cases []struct {
			Path  string `json:"path"`
			Steps []struct {
				Append  *string            `json:"append"`
				Cwd     string             `json:"cwd"`
				Sources [2]json.RawMessage `json:"sources"`
				Counts  [2]json.RawMessage `json:"counts"`
			} `json:"steps"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	src := &Route{Links: testLinks(), Home: home}
	counts := &messagecounts.Route{Home: home}
	failed, compared := 0, 0
	for i, c := range golden.Cases {
		p := filepath.Join(root, c.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p+".new", nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(p+".new", p); err != nil {
			t.Fatal(err)
		}
		for j, s := range c.Steps {
			if s.Append != nil {
				f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = f.WriteString(*s.Append)
				f.Close()
				continue
			}
			compared++
			var wantStatus int
			var wantBody string
			_ = json.Unmarshal(s.Sources[0], &wantStatus)
			_ = json.Unmarshal(s.Sources[1], &wantBody)
			res, found, threw := src.Read(p, s.Cwd)
			gotStatus, gotBody := 200, ""
			switch {
			case threw:
				gotStatus, gotBody = 500, "Internal Server Error"
			case !found:
				gotStatus, gotBody = 404, `{"error":"Transcript not found"}`
			default:
				gotBody = string(nodejson.Marshal(res.Value()))
			}
			gotBody = strings.ReplaceAll(gotBody, root, "$ROOT")
			if gotStatus != wantStatus || gotBody != wantBody {
				failed++
				if failed <= 3 {
					t.Errorf("case %d step %d sources:\n got %d %s\nwant %d %s", i, j, gotStatus, around(gotBody, wantBody), wantStatus, around(wantBody, gotBody))
				}
			}
			_ = json.Unmarshal(s.Counts[1], &wantBody)
			if got := string(nodejson.Marshal(counts.Count(p).Value())); got != wantBody {
				failed++
				if failed <= 3 {
					t.Errorf("case %d step %d counts:\n got %s\nwant %s", i, j, around(got, wantBody), around(wantBody, got))
				}
			}
		}
	}
	t.Logf("%d cases, %d reads compared", len(golden.Cases), compared)
	if failed > 0 {
		t.Fatalf("%d answers differ", failed)
	}
}

// around is a stretch of a around where it first differs from b.
func around(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	from, to := max(0, i-300), min(len(a), i+300)
	return fmt.Sprintf("@%d …%s…", i, a[from:to])
}
