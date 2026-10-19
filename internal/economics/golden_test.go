package economics

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/history"
	"harnessly/internal/jsvalue"
	"harnessly/internal/nodejson"
	"harnessly/internal/pricing"
)

// testdata/golden.json: what the previous runtime's dispatcher and handler answered (testdata/gen, under node) on the
// same layouts and transcripts, and the cursor files it wrote after each request. Each request must be answered alike,
// and a cursor it wrote must be the one written here.

type goldenStep struct {
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	Query          [][2]string       `json:"query"`
	Headers        map[string]string `json:"headers"`
	Status         int               `json:"status"`
	AnswerHeaders  map[string]string `json:"answerHeaders"`
	Answer         string            `json:"answer"`
	CursorsWritten map[string]string `json:"cursorsWritten"`

	Append, Replace, Truncate, Remove string
	Content                           string
}

var devInoField = regexp.MustCompile(`"dev":\d+(?:\.\d+)?,"ino":\d+(?:\.\d+)?`)

// tabsOf walks the layouts as the previous runtime's tab lookup did: workspaces in order, panes depth first.
func tabsOf(data string) func(string) (*jsvalue.Object, bool) {
	return func(id string) (*jsvalue.Object, bool) {
		raw, _ := os.ReadFile(filepath.Join(data, "workspaces.json"))
		v, _ := jsvalue.Parse(raw)
		ws, _ := v.(*jsvalue.Object)
		var walk func(n *jsvalue.Object) *jsvalue.Object
		walk = func(n *jsvalue.Object) *jsvalue.Object {
			if n == nil {
				return nil
			}
			if n.String("type") == "pane" {
				for _, t := range n.Array("tabs") {
					if o, ok := t.(*jsvalue.Object); ok && o.String("id") == id {
						return o
					}
				}
				return nil
			}
			for _, c := range n.Array("children") {
				if o, ok := c.(*jsvalue.Object); ok {
					if t := walk(o); t != nil {
						return t
					}
				}
			}
			return nil
		}
		for _, w := range ws.Array("workspaces") {
			layout, _ := os.ReadFile(filepath.Join(data, "workspaces", w.(*jsvalue.Object).String("id"), "layout.json"))
			lv, _ := jsvalue.Parse(layout)
			if l, ok := lv.(*jsvalue.Object); ok {
				if t := walk(l.Object("root")); t != nil {
					return t, true
				}
			}
		}
		return nil, false
	}
}

func TestRouteAnswersAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Now    int64                            `json:"now"`
		Token  string                           `json:"token"`
		Limits json.RawMessage                  `json:"limits"`
		Files  []struct{ Path, Content string } `json:"files"`
		Data   map[string]string                `json:"data"`
		Cursor struct{ Path, Content string }   `json:"cursor"`
		Steps  []goldenStep                     `json:"steps"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	data := filepath.Join(root, "data")
	unscrub := func(s string) string { return strings.ReplaceAll(s, "$ROOT", root) }
	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	write := func(p, content string) {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range golden.Files {
		write(filepath.Join(root, f.Path), f.Content)
	}
	for name, content := range golden.Data {
		write(filepath.Join(data, name), unscrub(content))
	}
	store := &Store{Dir: filepath.Join(data, "economics")}
	cursorPath := unscrub(golden.Cursor.Path)
	fi, _ := os.Stat(cursorPath)
	dev, ino := devIno(fi)
	write(store.File(cursorPath), strings.Replace(unscrub(golden.Cursor.Content), `"dev":"$DEV","ino":"$INO"`,
		`"dev":`+strconv.FormatFloat(dev, 'f', -1, 64)+`,"ino":`+strconv.FormatFloat(ino, 'f', -1, 64), 1))
	lv, _ := jsvalue.Parse(golden.Limits)
	limits := lv.(*jsvalue.Object)
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
	now := time.UnixMilli(golden.Now)
	rt := &Route{Store: store, Stats: &history.Stats{Dir: filepath.Join(data, "session-stats")}, SessionOK: session,
		Authorized: func(r *http.Request) bool { return token(r) || session(r) }, FindTab: tabsOf(data),
		Limits: func(p string) any { v, _ := limits.Get(p); return v }, Now: func() time.Time { return now }}
	srv := serve(t, rt.Answer)
	lastEtag := ""
	for i, g := range golden.Steps {
		if g.Method == "" {
			file := filepath.Join(root, g.Append+g.Replace+g.Truncate+g.Remove)
			switch {
			case g.Append != "":
				f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
				_, _ = f.WriteString(g.Content)
				f.Close()
			case g.Replace != "":
				write(file+".new", g.Content)
				_ = os.Rename(file+".new", file)
			case g.Truncate != "":
				write(file, g.Content)
			case g.Remove != "":
				_ = os.Remove(file)
			}
			continue
		}
		var q []string
		for _, kv := range g.Query {
			q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
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
		where := g.Method + " " + g.Path + " #" + strconv.Itoa(i)
		if res.StatusCode != g.Status || scrub(string(body)) != g.Answer {
			t.Fatalf("%s %v\n got %d %s\nwant %d %s", where, g.Query, res.StatusCode, scrub(string(body)), g.Status, g.Answer)
		}
		checkHeaders(t, where, res, body, g.AnswerHeaders, scrub)
		store.Flush()
		for path, want := range g.CursorsWritten {
			got, err := os.ReadFile(store.File(unscrub(path)))
			if err != nil {
				t.Fatalf("%s: cursor of %s: %v", where, path, err)
			}
			if have := devInoField.ReplaceAllLiteralString(scrub(string(got)), `"dev":"$DEV","ino":"$INO"`); have != want {
				t.Fatalf("%s: cursor of %s\n got %s\nwant %s", where, path, have, want)
			}
		}
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

// TS_GOLDEN points at transcripts the generator made (GEN_RANDOM), fed in random pieces: the cursor each leaves and
// the economics summed from them must be the previous runtime's.
func TestGeneratedTranscripts(t *testing.T) {
	file := os.Getenv("TS_GOLDEN")
	if file == "" {
		t.Skip("TS_GOLDEN not set")
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Now   float64 `json:"now"`
		Cases []struct {
			Provider       string          `json:"provider"`
			SessionID      json.RawMessage `json:"sessionId"`
			StatuslineCost json.RawMessage `json:"statuslineCost"`
			Chunks         []string        `json:"chunks"`
			SubChunks      [][]string      `json:"subChunks"`
			State          string          `json:"state"`
			SubStates      []string        `json:"subStates"`
			Summary        string          `json:"summary"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	failed := 0
	fail := func(i int, what, got, want string) {
		failed++
		if failed <= 5 {
			t.Errorf("case %d %s\n got %s\nwant %s", i, what, got, want)
		}
	}
	for i, c := range set.Cases {
		feed := func(path, provider string, chunks []string) *State {
			st := NewState(path, provider)
			for _, ch := range chunks {
				st.consume(ch)
			}
			return st
		}
		main := feed("/t/"+c.Provider+".jsonl", c.Provider, c.Chunks)
		if got := string(nodejson.Marshal(main)); got != c.State {
			fail(i, "cursor", got, c.State)
			continue
		}
		var subs []*State
		for j, chunks := range c.SubChunks {
			st := feed("/t/claude.jsonl", "claude", chunks)
			if got := string(nodejson.Marshal(st)); got != c.SubStates[j] {
				fail(i, "subagent cursor", got, c.SubStates[j])
			}
			subs = append(subs, st)
		}
		var sid any = jsvalue.Undefined
		if string(c.SessionID) != `"$undefined"` {
			sid, _ = jsvalue.Parse(c.SessionID)
		}
		cost, _ := jsvalue.Parse(c.StatuslineCost)
		got := string(jsvalue.Stringify(Summarize(Source{Provider: c.Provider, SessionID: sid}, main, subs, cost, set.Now), ""))
		if got != c.Summary {
			fail(i, "summary", got, c.Summary)
		}
	}
	t.Logf("%d cases, %d differ", len(set.Cases), failed)
}

// The price tables answer every model id of the golden file as the previous runtime's did, to the last bit.
func TestPriceTables(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID     string     `json:"id"`
		Counts [5]float64 `json:"counts"`
		Cost   *float64   `json:"cost"`
	}
	var golden struct {
		Pricing struct{ Claude, OpenAI []row } `json:"pricing"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	check := func(r row, cost float64, ok bool) {
		if ok != (r.Cost != nil) || ok && cost != *r.Cost {
			t.Errorf("%q %v: got %v %v, want %v", r.ID, r.Counts, cost, ok, r.Cost)
		}
	}
	for _, r := range golden.Pricing.Claude {
		c := r.Counts
		cost, ok := pricing.ClaudeCost(r.ID, c[0], c[1], c[2], c[3], c[4])
		check(r, cost, ok)
	}
	for _, r := range golden.Pricing.OpenAI {
		c := r.Counts
		cost, ok := pricing.OpenAICost(r.ID, c[0]+c[1], c[1], c[2])
		check(r, cost, ok)
	}
	if len(golden.Pricing.Claude) < 60 || len(golden.Pricing.OpenAI) < 60 {
		t.Fatal("too few price cases")
	}
}
