package share

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"harnessly/internal/gate"
	"harnessly/internal/jsvalue"
	"harnessly/internal/launch"
	"harnessly/internal/nodejson"
)

// testdata/golden.json: what the previous runtime's dispatcher and handlers answered (testdata/gen, under node) to a
// run of requests on fixture tabs and transcripts with a moving clock, and the share files it left after each. The Go
// route gets the same requests at the same times and must answer and write the same bytes.

type goldenStep struct {
	Now           float64           `json:"now"`
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Body          *string           `json:"body"`
	Token         string            `json:"token"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
	Files         struct {
		Written map[string]string `json:"written"`
		Removed []string          `json:"removed"`
	} `json:"files"`

	Advance float64 `json:"advance"`
	Touch   string  `json:"touch"`
	Mtime   float64 `json:"mtime"`
}

type goldenFile struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
	Repeat  *struct {
		Line  string `json:"line"`
		Count int    `json:"count"`
	} `json:"repeat"`
	From string `json:"from"`
}

type golden struct {
	Secret    string          `json:"secret"`
	CLIToken  string          `json:"cliToken"`
	Env       []string        `json:"env"`
	ShareBase string          `json:"shareBase"`
	Files     []goldenFile    `json:"files"`
	Tabs      json.RawMessage `json:"tabs"`
	Steps     []goldenStep    `json:"steps"`
	Neutral   struct {
		Base, Token, Salt, Audience, Brand, FilePrefix, ProfileHomes string
	} `json:"neutral"`
}

// testRoute is the route over a fresh root laid out as the golden file says, with the clock and ids it needs.
func testRoute(t *testing.T, g golden) (*Route, string, *float64) {
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	workspace := filepath.Join(home, "work", "acme")
	data := filepath.Join(root, "data")
	for _, d := range []string{workspace, data} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	unscrub := func(s string) string { return strings.ReplaceAll(s, "$ROOT", root) }
	for _, f := range g.Files {
		var content []byte
		switch {
		case f.Content != nil:
			content = []byte(unscrub(*f.Content))
		case f.Repeat != nil:
			content = []byte(strings.Repeat(unscrub(f.Repeat.Line)+"\n", f.Repeat.Count))
		default:
			b, err := os.ReadFile(f.From)
			if err != nil {
				t.Fatal(err)
			}
			content = b
		}
		p := filepath.Join(root, f.Path)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(data, "cli-token"), []byte(g.CLIToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tabsValue, _ := jsvalue.Parse([]byte(unscrub(string(g.Tabs))))
	tabs, _ := tabsValue.([]any)
	var env []string
	for _, kv := range g.Env {
		env = append(env, strings.ReplaceAll(unscrub(kv), "$WORKSPACE", workspace))
	}
	now := new(float64)
	ids := 0
	subs := map[string]any{"good": nil, "person": "alice@example.com", "password": "user", "number": 7.0}
	cookie := func(r *http.Request) (string, bool) {
		c, err := r.Cookie("session-token")
		if err != nil {
			return "", false
		}
		_, ok := subs[c.Value]
		return c.Value, ok
	}
	sessionOK := func(r *http.Request) bool { _, ok := cookie(r); return ok }
	tokenOK := func(r *http.Request) bool {
		for _, h := range []string{"X-Cli-Token", "X-Old-Cli-Token"} {
			if vs, ok := r.Header[h]; ok {
				return vs[0] == g.CLIToken
			}
		}
		return false
	}
	clock := func() time.Time { return time.UnixMilli(int64(*now)) }
	rt := &Route{
		Store: &Store{DataDir: data, Now: clock, NewID: func() string {
			ids++
			return base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{byte(ids)}, 32))
		}},
		Page:  Page{Brand: g.Neutral.Brand, DropPathPrefix: "/agents"},
		Token: TokenConfig{Salt: g.Neutral.Salt, Audience: g.Neutral.Audience}, Secret: []byte(g.Secret),
		Home: home, EnvHome: home, WorkspaceDir: workspace, ProfileHomes: g.Neutral.ProfileHomes,
		Environ:         func() []string { return env },
		ShareBaseHeader: g.Neutral.Base, ShareTokenHeader: g.Neutral.Token, FilePrefix: g.Neutral.FilePrefix,
		SessionOK: sessionOK,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if sessionOK(r) || tokenOK(r) {
				return 0, nil, nil, false
			}
			return http.StatusUnauthorized, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		},
		Claims: func(r *http.Request) map[string]any {
			c, ok := cookie(r)
			if !ok || subs[c] == nil {
				return map[string]any{}
			}
			return map[string]any{"sub": subs[c]}
		},
		FindTab: func(id string) (*jsvalue.Object, bool) {
			for _, tv := range tabs {
				if o, ok := tv.(*jsvalue.Object); ok && o.String("id") == id {
					return o, true
				}
			}
			return nil, false
		},
		Headlines: &launch.Headlines{},
		Now:       clock,
	}
	return rt, root, now
}

// serveRoute answers like the runtime gate does.
func serveRoute(t *testing.T, rt *Route, platformAPI string) *httptest.Server {
	g, err := gate.New(gate.Config{DataDir: t.TempDir(), Upstream: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(g.AsRoutes(gate.JSONRoute{Match: func(*http.Request) bool { return true }, Compute: func(r *http.Request) (int, any, http.Header) {
		kind, id := Match(r, platformAPI)
		switch kind {
		case Snapshots:
			return rt.Snapshots(r)
		case Download:
			return rt.Download(r, id)
		case Preview:
			return rt.Preview(r, id)
		case Shares:
			return rt.Shares(r)
		case ShareOne:
			return rt.Share(r, id)
		case ShareRead:
			return rt.ShareRead(r, id)
		}
		return http.StatusNotFound, nil, nil
	}})...)
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv
}

func shareFiles(t *testing.T, data string, scrub func(string) string) map[string]string {
	out := map[string]string{}
	_ = filepath.WalkDir(filepath.Join(data, "shares"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, _ := os.ReadFile(p)
		rel, _ := filepath.Rel(data, p)
		out[rel] = scrub(string(b))
		return nil
	})
	return out
}

// fakeCredentials expands the placeholders the golden stores instead of provider-shaped fake credentials (so the
// committed file holds nothing a secret scanner blocks) back into exactly what the generator fed the old runtime.
var fakeCredentials = strings.NewReplacer(
	"@FAKE_ANTHROPIC_KEY@", "sk-"+"ant-api03-"+strings.Repeat("A", 30),
	"@FAKE_GITHUB_TOKEN@", "gh"+"p_"+strings.Repeat("b", 36),
)

func readGolden(t *testing.T) golden {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g golden
	if err := json.Unmarshal([]byte(fakeCredentials.Replace(string(raw))), &g); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestRoutesAnswerAsTheOldRuntime(t *testing.T) {
	g := readGolden(t)
	rt, root, now := testRoute(t, g)
	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	srv := serveRoute(t, rt, "/api/platform")
	data := rt.Store.DataDir
	lastEtag := ""
	want := map[string]string{}
	for i, s := range g.Steps {
		if s.Method == "" {
			switch {
			case s.Advance != 0:
				*now += s.Advance
			case s.Touch != "":
				mt := time.UnixMilli(int64(s.Mtime))
				if err := os.Chtimes(filepath.Join(data, s.Touch), mt, mt); err != nil {
					t.Fatal(err)
				}
			}
			continue
		}
		*now = s.Now
		var q []string
		for _, kv := range s.Query {
			q = append(q, url.QueryEscape(kv[0])+"="+url.QueryEscape(kv[1]))
		}
		u := srv.URL + s.Path
		if len(q) > 0 {
			u += "?" + strings.Join(q, "&")
		}
		var body io.Reader
		if s.Body != nil {
			body = strings.NewReader(*s.Body)
		}
		req, _ := http.NewRequest(s.Method, u, body)
		for k, v := range s.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			req.Header.Set(k, v)
		}
		if s.Token != "" {
			req.Header.Set(g.Neutral.Token, s.Token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		where := s.Method + " " + s.Path + " #" + strconv.Itoa(i)
		if res.StatusCode != s.Status || scrub(string(got)) != s.Answer {
			a, b := scrub(string(got)), s.Answer
			k := 0
			for k < len(a) && k < len(b) && a[k] == b[k] {
				k++
			}
			from := max(0, k-200)
			t.Fatalf("%s %v\n got %d …%s\nwant %d …%s", where, s.Query, res.StatusCode, clip(a[from:], 400), s.Status, clip(b[from:], 400))
		}
		if s.AnswerHeaders["etag"] == "IF-NONE-MATCH" {
			s.AnswerHeaders["etag"] = req.Header.Get("If-None-Match")
		}
		checkHeaders(t, where, res, got, s.AnswerHeaders, scrub)
		for name, content := range s.Files.Written {
			want[name] = content
		}
		for _, name := range s.Files.Removed {
			delete(want, name)
		}
		files := shareFiles(t, data, scrub)
		for name, w := range want {
			if files[name] != w {
				t.Fatalf("%s: file %s\n got %s\nwant %s", where, name, clip(files[name], 600), clip(w, 600))
			}
		}
		for name := range files {
			if _, ok := want[name]; !ok {
				t.Fatalf("%s: unexpected file %s", where, name)
			}
		}
	}
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// checkHeaders compares the headers a caller acts on. A body holding this machine's paths (or the neutral words) is
// tagged by its own bytes; a 304 carries no content type here (net/http drops it).
func checkHeaders(t *testing.T, where string, res *http.Response, body []byte, want map[string]string, scrub func(string) string) {
	t.Helper()
	for k, w := range want {
		have := res.Header.Get(k)
		switch {
		case k == "content-type" && res.StatusCode == http.StatusNotModified:
		case k == "content-length":
			if have != "" && have != strconv.Itoa(len(body)) {
				t.Errorf("%s: content-length %s for %d bytes", where, have, len(body))
			}
		case k == "etag" && (w == "RECOMPUTE" || scrub(string(body)) != string(body)):
			if have != nodejson.ETag(body) {
				t.Errorf("%s: etag %s", where, have)
			}
		case scrub(have) != w:
			t.Errorf("%s: %s = %q, want %q", where, k, have, w)
		}
	}
	for _, k := range []string{"ETag", "Allow", "Cache-Control", "Content-Type", "Content-Disposition", "Content-Security-Policy"} {
		if _, ok := want[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" && !(k == "Content-Type" && res.StatusCode == http.StatusNotModified) {
			t.Errorf("%s: unexpected %s: %s", where, k, res.Header.Get(k))
		}
	}
}
