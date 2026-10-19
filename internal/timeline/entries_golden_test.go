package timeline

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"harnessly/internal/gate"
	"harnessly/internal/nodejson"
	"harnessly/internal/subagents"
	"harnessly/internal/transcript"
)

// testdata/entries.json: what the previous runtime's dispatcher and transcript-page handler answered (testdata/entries,
// under node) on the same files. Entry ids are a counter in both runs.

type entriesFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type entriesRequest struct {
	Method        string            `json:"method"`
	Path          string            `json:"path"`
	Query         [][2]string       `json:"query"`
	Headers       map[string]string `json:"headers"`
	Stays         bool              `json:"stays"`
	Status        int               `json:"status"`
	AnswerHeaders map[string]string `json:"answerHeaders"`
	Answer        string            `json:"answer"`
}

// counterIDs makes entry ids the generator's counter (id0001, id0002, ...) and fixes the clock.
func counterIDs(t *testing.T, now float64) {
	n := 0
	prevID, prevNow := transcript.NewID, transcript.Now
	transcript.NewID = func() string { n++; return "id" + fmt.Sprintf("%04s", strconv.FormatInt(int64(n), 36)) }
	transcript.Now = func() float64 { return now }
	t.Cleanup(func() { transcript.NewID, transcript.Now = prevID, prevNow })
}

func TestEntriesAnswerAsTheOldRuntime(t *testing.T) {
	raw, err := os.ReadFile("testdata/entries.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Now      float64          `json:"now"`
		Token    string           `json:"token"`
		Files    []entriesFile    `json:"files"`
		Requests []entriesRequest `json:"requests"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	for _, f := range golden.Files {
		p := filepath.Join(root, f.Path)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	counterIDs(t, golden.Now)
	home := filepath.Join(root, "home")
	authorized := func(r *http.Request) bool {
		if c, err := r.Cookie("session-token"); err == nil && c.Value == "good" {
			return true
		}
		for _, h := range []string{"X-Cli-Token", "X-Old-Cli-Token"} {
			if vs, ok := r.Header[h]; ok {
				return vs[0] == golden.Token
			}
		}
		return false
	}
	route := &EntriesRoute{Authorized: authorized,
		Paths: subagents.Paths{ClaudeProjects: filepath.Join(home, ".claude", "projects"), CodexSessions: filepath.Join(home, ".codex", "sessions")}}
	g, err := gate.New(gate.Config{DataDir: t.TempDir(), Upstream: "127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	g.Add(g.AsRoutes(gate.JSONRoute{Match: route.Takes, Compute: route.Answer})...)
	srv := httptest.NewServer(g)
	defer srv.Close()
	lastEtag := ""
	for i, c := range golden.Requests {
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
		if c.Stays {
			if route.Takes(req) {
				t.Errorf("%s: taken, the old runtime should answer it", where)
			}
			continue
		}
		if !route.Takes(req) {
			t.Errorf("%s: left to the old runtime", where)
			continue
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
		got := strings.ReplaceAll(string(body), root, "$ROOT")
		if res.StatusCode != c.Status || got != c.Answer {
			t.Fatalf("%s\n got %d %s\nwant %d %s", where, res.StatusCode, clipText(got), c.Status, clipText(c.Answer))
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

// TestEntriesOfGeneratedTranscripts compares page reads of generated transcripts with the previous runtime's, from a
// file the generator writes (GEN_RANDOM): ENTRIES_RANDOM=<that file>.
func TestEntriesOfGeneratedTranscripts(t *testing.T) {
	path := os.Getenv("ENTRIES_RANDOM")
	if path == "" {
		t.Skip("ENTRIES_RANDOM not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Now   float64 `json:"now"`
		Cases []struct {
			Codex   bool   `json:"codex"`
			Content string `json:"content"`
			Before  int64  `json:"before"`
			Limit   int    `json:"limit"`
			Until   int64  `json:"until"`
			Answer  string `json:"answer"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}
	counterIDs(t, golden.Now)
	file := filepath.Join(t.TempDir(), "case.jsonl")
	failed := 0
	for i, c := range golden.Cases {
		if err := os.WriteFile(file, []byte(c.Content), 0o644); err != nil {
			t.Fatal(err)
		}
		var page transcript.Chunk
		if c.Codex {
			page = transcript.ReadCodexBefore(file, c.Before, c.Limit, c.Until, transcript.CodexOptions{})
		} else {
			page = transcript.ReadBefore(file, c.Before, c.Limit)
		}
		got := string(nodejson.Marshal(entriesAnswer(page, c.Codex)))
		if got != c.Answer {
			failed++
			if failed <= 3 {
				t.Errorf("case %d (codex %v, before %d, limit %d, until %d):\n got %s\nwant %s", i, c.Codex, c.Before, c.Limit, c.Until, clipText(got), clipText(c.Answer))
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d of %d differ", failed, len(golden.Cases))
	}
}

func clipText(s string) string {
	if len(s) > 1500 {
		return s[:1500] + "…"
	}
	return s
}
