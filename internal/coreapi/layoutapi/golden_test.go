package layoutapi

import (
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

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/state"
	"github.com/ly-solutions/harnessly/internal/vault"
)

type goldenRequest struct {
	Method, URL   string
	Headers       map[string]string
	Body          json.RawMessage
	Status        int
	AnswerHeaders map[string]string
	Answer        string
}

// sequentialIDs is testdata/gen/stub-nanoid.mjs: id0001, id0002, …
func sequentialIDs() func() string {
	n := 0
	return func() string {
		n++
		s := strconv.FormatInt(int64(n), 36)
		return "id" + strings.Repeat("0", max(0, 4-len(s))) + s
	}
}

// The requests of testdata/gen/gen.mjs, answered by the previous runtime's own dispatcher and handlers
// (testdata/golden.json), must be answered alike: status, the headers a caller acts on, body, and the files left.
func TestGoldenAgainstPreviousRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Token    string
		Fixtures map[string]string
		Requests []goldenRequest
		Files    map[string]*string
		Pasted   [][]string
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "home")
	data := filepath.Join(home, "data")
	for _, d := range []string{home, filepath.Join(root, "proj-a"), filepath.Join(root, "proj-b")} {
		_ = os.MkdirAll(d, 0o755)
	}
	unscrub := func(s string) string { return strings.ReplaceAll(s, "$ROOT", root) }
	scrub := func(s string) string { return strings.ReplaceAll(s, root, "$ROOT") }
	for name, content := range golden.Fixtures {
		file := filepath.Join(data, name)
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		if err := os.WriteFile(file, []byte(unscrub(content)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tm := newFakeTmux(home)
	tm.sessions["tk-ws-a-pane-a-tab-1"], tm.sessions["tk-ws-a-pane-a-tab-2"] = true, true
	tm.screen = "line\n"
	sy := &fakeSync{}
	store := state.New(state.Config{DataDir: data, Tmux: tm, Home: home, MetaKey: "meta", NewID: sequentialIDs(),
		Now: func() time.Time { return time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC) }})
	publish := func(e *jsvalue.Object) { sy.Broadcast(e) }
	d := &Deps{
		Store: store, Tmux: tm, Sync: sy,
		Intents: paneintents.New(filepath.Join(data, "pane-intents.json"), nil, false),
		Panel:   panel.NewBridge(publish, sy.ClientCount), Vault: vault.NewBridge(publish, sy.ClientCount),
		Session: func(r *http.Request) bool {
			c, err := r.Cookie("session-token")
			return err == nil && c.Value == "good"
		},
		CLIToken: func(r *http.Request) bool { return r.Header.Get("X-Cli-Token") == golden.Token },
		Home:     home,
	}
	main, _ := Routes(d)

	lastEtag := ""
	for i, g := range golden.Requests {
		var body io.Reader
		var bodyText string
		if len(g.Body) > 0 && string(g.Body) != "null" {
			var s string
			if json.Unmarshal(g.Body, &s) == nil {
				bodyText = s
			} else {
				var rep struct{ Repeat int }
				_ = json.Unmarshal(g.Body, &rep)
				bodyText = strings.Repeat("a", rep.Repeat)
			}
			body = strings.NewReader(bodyText)
		}
		req := httptest.NewRequest(g.Method, g.URL, body)
		for k, v := range g.Headers {
			if k == "if-none-match" && v == "ETAG" {
				v = lastEtag
			}
			req.Header.Set(k, v)
		}
		sent := req.Header.Get("If-None-Match")
		rec := httptest.NewRecorder()
		main.Handler.ServeHTTP(rec, req)
		res := rec.Result()
		got, _ := io.ReadAll(res.Body)
		if e := res.Header.Get("ETag"); e != "" {
			lastEtag = e
		}
		where := strconv.Itoa(i) + " " + g.Method + " " + g.URL
		if res.StatusCode != g.Status || scrub(string(got)) != g.Answer {
			t.Fatalf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, scrub(string(got)), g.Status, g.Answer)
		}
		for k, want := range g.AnswerHeaders {
			have := res.Header.Get(k)
			switch {
			case k == "etag" && g.Status == 304:
				if have != sent {
					t.Errorf("%s: etag %s, sent %s", where, have, sent)
				}
			case k == "etag" && strings.Contains(g.Answer, "$ROOT"):
				// The body holds this machine's paths, so the tag is checked against the body itself.
				if have != etag(got) {
					t.Errorf("%s: etag %s", where, have)
				}
			case k == "content-length" && strings.Contains(g.Answer, "$ROOT"):
				if have != strconv.Itoa(len(got)) {
					t.Errorf("%s: content-length %s", where, have)
				}
			case scrub(have) != want:
				t.Errorf("%s: %s = %q, want %q", where, k, have, want)
			}
		}
		for _, k := range []string{"ETag", "Allow", "Cache-Control"} {
			if _, ok := g.AnswerHeaders[strings.ToLower(k)]; !ok && res.Header.Get(k) != "" {
				t.Errorf("%s: unexpected %s", where, k)
			}
		}
	}
	for name, want := range golden.Files {
		raw, err := os.ReadFile(filepath.Join(data, name))
		if want == nil {
			if err == nil {
				t.Errorf("%s exists", name)
			}
			continue
		}
		if scrub(string(raw)) != *want {
			t.Errorf("%s\n got %s\nwant %s", name, scrub(string(raw)), *want)
		}
	}
	var pasted [][]string
	for _, c := range tm.took() {
		if c[0] == "paste" {
			pasted = append(pasted, c[1:])
		}
	}
	if b1, _ := json.Marshal(pasted); string(b1) != mustJSON(golden.Pasted) {
		t.Fatalf("pasted %s, want %s", b1, mustJSON(golden.Pasted))
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
