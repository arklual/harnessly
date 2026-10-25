package subagents

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
	"github.com/ly-solutions/harnessly/internal/transcript"
)

// testdata/golden.json: what the previous runtime's dispatcher and handler answered (testdata/gen, under node) on the
// same files. Each request must be answered alike: status, the headers a caller acts on, body.

type goldenFile struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Mtime   float64 `json:"mtime"`
}

type goldenRequest struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
}

func layOut(t *testing.T, root string, files []goldenFile) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
		sec := int64(f.Mtime)
		mt := time.Unix(sec, int64((f.Mtime-float64(sec))*1e9))
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRouteAnswersAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Now      int64           `json:"now"`
		Token    string          `json:"token"`
		Files    []goldenFile    `json:"files"`
		Requests []goldenRequest `json:"requests"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	update := updatingGolden()
	doc, err := jsvalue.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	requests := doc.(*jsvalue.Object).Array("requests")
	root, _ := filepath.EvalSymlinks(t.TempDir())
	layOut(t, root, golden.Files)
	home := filepath.Join(root, "home")
	now := time.UnixMilli(golden.Now)
	token := func(r *http.Request) bool {
		for _, h := range []string{"X-Cli-Token", "X-Old-Cli-Token"} {
			if vs, ok := r.Header[h]; ok {
				return vs[0] == golden.Token
			}
		}
		return false
	}
	session := func(r *http.Request) bool {
		c, err := r.Cookie("session-token")
		return err == nil && c.Value == "good"
	}
	rt := &Route{Lister: &Lister{Now: func() time.Time { return now }}, Authorized: func(r *http.Request) bool { return token(r) || session(r) },
		SessionOK: session, Paths: Paths{ClaudeProjects: filepath.Join(home, ".claude", "projects"), CodexSessions: filepath.Join(home, ".codex", "sessions")}}
	prevNow := transcript.Now
	transcript.Now = func() float64 { return float64(golden.Now) }
	t.Cleanup(func() { transcript.Now = prevNow })
	srv := serve(t, func(r *http.Request) (int, any, http.Header) {
		if parts, _, _ := files.APIParts(r); len(parts) == 4 && parts[2] == "subagents" {
			return rt.AnswerTranscript(r, parts[3])
		}
		return rt.Answer(r)
	})
	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	lastEtag := ""
	for i, g := range golden.Requests {
		rt.Lister = &Lister{Now: func() time.Time { return now }} // the generator cleared the old handler's cache before each
		var q []string
		for _, kv := range g.Query {
			q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(strings.ReplaceAll(kv[1], "$ROOT", root)))
		}
		u := srv.URL + g.Path
		if len(q) > 0 {
			u += "?" + strings.Join(q, "&")
		}
		req, _ := http.NewRequest(g.Method, u, nil)
		for k, v := range g.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			req.Header.Set(k, v)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		where := g.Method + " " + g.Path + " #" + jscompat.Number(float64(i))
		if update {
			updateRequest(requests[i].(*jsvalue.Object), g.Answer, scrub(string(body)), body, res.Header.Get("ETag"))
			continue
		}
		if res.StatusCode != g.Status || scrub(string(body)) != g.Answer {
			t.Fatalf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, scrub(string(body)), g.Status, g.Answer)
		}
		checkHeaders(t, where, res, body, g.AnswerHeaders, scrub)
	}
	if update && !t.Failed() {
		writeGolden(t, "testdata/golden.json", doc)
	}
}

// checkHeaders compares the headers a caller acts on. A body holding this machine's paths is tagged by its own bytes;
// a 304 carries no content type here (net/http drops it), the length is the body's.
func checkHeaders(t *testing.T, where string, res *http.Response, body []byte, want map[string]string, scrub func(string) string) {
	t.Helper()
	for k, w := range want {
		have := res.Header.Get(k)
		switch {
		case k == "content-length" || k == "content-type" && res.StatusCode == http.StatusNotModified:
		case k == "etag" && scrub(string(body)) != string(body):
			if have != nodejson.ETag(body) {
				t.Errorf("%s: etag %s", where, have)
			}
		case scrub(have) != w:
			t.Errorf("%s: %s = %q, want %q", where, k, have, w)
		}
	}
	for _, k := range []string{"ETag", "Allow", "Cache-Control", "Content-Type"} {
		if _, ok := want[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
			t.Errorf("%s: unexpected %s: %s", where, k, res.Header.Get(k))
		}
	}
}

// serve answers like the runtime gate does for a JSON route.
func serve(t *testing.T, compute func(*http.Request) (int, any, http.Header)) *httptest.Server {
	g, err := gate.New(gate.Config{DataDir: t.TempDir(), Upstream: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(g.AsRoutes(gate.JSONRoute{Match: func(*http.Request) bool { return true }, Compute: compute})...)
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv
}

// TS_GOLDEN points at summaries the generator made of generated transcripts (GEN_RANDOM): every one must match.
func TestSummariesOfGeneratedTranscripts(t *testing.T) {
	file := os.Getenv("TS_GOLDEN")
	if file == "" {
		t.Skip("TS_GOLDEN not set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Cases []struct {
			Raw     string          `json:"raw"`
			MetaRaw *string         `json:"metaRaw"`
			MtimeMs float64         `json:"mtimeMs"`
			Now     float64         `json:"now"`
			Result  json.RawMessage `json:"result"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	failed := 0
	for i, c := range set.Cases {
		var meta any = agents.NewObj()
		if c.MetaRaw != nil {
			if v, err := agents.Decode([]byte(*c.MetaRaw)); err == nil {
				meta = v
			}
		}
		got := "null"
		if s, ok := Summarize(c.Raw, meta, "id"+jscompat.Number(float64(i)), c.MtimeMs, c.Now); ok {
			got = string(nodejson.Marshal(s))
		}
		if got != string(c.Result) {
			failed++
			if failed <= 5 {
				t.Errorf("case %d\n raw %q meta %v\n got %s\nwant %s", i, c.Raw, c.MetaRaw, got, c.Result)
			}
		}
	}
	t.Logf("%d cases, %d differ", len(set.Cases), failed)
}

// A summary made while the agent counted as running is not served once it went quiet: it shows as stalled then. The
// previous runtime kept serving "running" until the transcript changed (a fixed quirk; the shadow differs here).
func TestRunningSummaryIsNotKeptPastTheWindow(t *testing.T) {
	dir := t.TempDir()
	session := filepath.Join(dir, "s.jsonl")
	sub := DirFor(session)
	_ = os.MkdirAll(sub, 0o755)
	file := filepath.Join(sub, "agent-x1.jsonl")
	_ = os.WriteFile(file, []byte(`{"type":"user","message":{"content":"go"}}`+"\n"), 0o644)
	start := time.Unix(1_800_000_000, 0)
	_ = os.Chtimes(file, start, start)
	now := start.Add(10 * time.Second)
	l := &Lister{Now: func() time.Time { return now }}
	if got := l.List(session); len(got) != 1 || got[0].Status != "running" {
		t.Fatalf("%+v", got)
	}
	now = start.Add(time.Minute)
	if got := l.List(session); got[0].Status != "stalled" {
		t.Fatalf("still %s a minute later", got[0].Status)
	}
	_ = os.WriteFile(file, []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}}`+"\n"), 0o644)
	_ = os.Chtimes(file, start, start)
	if got := l.List(session); got[0].Status != "done" || got[0].Result != "ok" {
		t.Fatalf("%+v", got[0])
	}
}
