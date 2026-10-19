package design

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"harnessly/internal/gate"
)

type routeCase struct {
	Method, URL string
	Headers     map[string]*string
	Body        *string
	Answer      struct {
		Status  int
		Headers map[string]string
		Body    string
	}
}

func TestHistoryRoutesMatchGolden(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("no unzip")
	}
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	raw, err := os.ReadFile("testdata/history/routes-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Token    string
		Requests []routeCase
		Library  []string
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	l, library := historyLibrary(t)
	_ = os.MkdirAll(filepath.Join(library, "p1", ProjectDirname), 0o777)
	_ = os.WriteFile(filepath.Join(library, "p1", ProjectDirname, "A.dc.html"), []byte("<main class=\"board\">a</main>\n"), 0o644)
	session := func(r *http.Request) bool { return strings.Contains(r.Header.Get("Cookie"), "session-token=good") }
	token := func(r *http.Request) bool { return r.Header.Get("x-cli-token") == g.Token }
	a := &API{L: l, Session: session, CLIToken: token, PlatformAPI: "/api/platform",
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if session(r) || token(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	defer a.L.StopBackground()
	handlers := map[string]func(*http.Request) Answer{
		"/api/platform/design/library/history": a.History,
		"/api/platform/design/library/import":  a.Import,
		"/api/cli/design/history":              a.CLIHistory,
	}
	zip, _ := os.ReadFile("testdata/history/project.zip")
	for _, c := range g.Requests {
		var body []byte
		if c.Body != nil {
			body = []byte(*c.Body)
			if *c.Body == "@project.zip" {
				body = zip
			}
		}
		req := httptest.NewRequest(c.Method, c.URL, bytes.NewReader(body))
		for k, v := range c.Headers {
			if v != nil {
				req.Header.Set(k, *v)
			}
		}
		if c.Body != nil {
			req.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
		path, _, _ := strings.Cut(c.URL, "?")
		h := handlers[path]
		route := (*gate.Gate)(nil).AsRoutes(gate.JSONRoute{Match: func(*http.Request) bool { return true },
			Compute: func(r *http.Request) (int, any, http.Header) { return h(r).Split() }})[0]
		rec := httptest.NewRecorder()
		route.Handler.ServeHTTP(rec, req)
		got := rec.Result()
		gotBody := rec.Body.String()
		if got.StatusCode != c.Answer.Status || gotBody != c.Answer.Body {
			t.Errorf("%s %s: %d %s, want %d %s", c.Method, c.URL, got.StatusCode, gotBody, c.Answer.Status, c.Answer.Body)
		}
		for _, k := range []string{"content-type", "cache-control", "allow", "etag"} {
			if want := c.Answer.Headers[k]; got.Header.Get(k) != want {
				t.Errorf("%s %s: %s %q, want %q", c.Method, c.URL, k, got.Header.Get(k), want)
			}
		}
	}
	entries, _ := os.ReadDir(library)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(g.Library, ",") {
		t.Errorf("library after: %v, want %v", names, g.Library)
	}
}

func TestCLIHistoryNeedsKeeper(t *testing.T) {
	for body, want := range map[string]bool{`{"action":"restore","session":"tk-1"}`: true, `{"action":"restore"}`: false, `{"session":null}`: true, `[1]`: false, `{`: false} {
		r := httptest.NewRequest(http.MethodPost, "/api/cli/design/history", strings.NewReader(body))
		if got := CLIHistoryNeedsKeeper(r); got != want {
			t.Errorf("%s: %v", body, got)
		}
		if again, _ := io.ReadAll(r.Body); string(again) != body {
			t.Errorf("body after the look: %q", again)
		}
	}
	if CLIHistoryNeedsKeeper(httptest.NewRequest(http.MethodGet, "/api/cli/design/history", strings.NewReader(`{"session":"x"}`))) {
		t.Error("a GET needs no keeper")
	}
}
