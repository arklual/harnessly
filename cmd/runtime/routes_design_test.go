package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/gate"
)

// The design slice through the gate: its routes under both path forms while the slice is on, the API's refusal before
// the handler's, and everything left to the old runtime once the slice is off.
func TestDesignRoutesThroughTheGate(t *testing.T) {
	dir, home, ws := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("RUNTIME_WORKSPACE_DIR", ws)
	t.Setenv("RUNTIME_DESIGN_LIBRARY_DIR", "")
	lib := filepath.Join(ws, "design-artifacts", "demo", "project")
	_ = os.MkdirAll(lib, 0o755)
	_ = os.WriteFile(filepath.Join(lib, "Main.dc.html"), []byte("<p>Привет, доска</p>\n"), 0o644)
	mode := filepath.Join(dir, "state-mode.json")
	_ = os.WriteFile(mode, []byte(`{"goSlices":["design"]}`), 0o600)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	defer old.Close()
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	person := func(r *http.Request) bool { return r.Header.Get("Cookie") == "session-token=ok" }
	token := func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	d := deps{Home: home, DataDir: dir, Old: &gate.OldRuntime{}, SessionOK: person, TokenHeaders: []string{"x-cli-token"},
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if person(r) || token(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	f.Add(f.AsRoutes(designRoutes(d)...)...)
	srv := httptest.NewServer(f)
	defer srv.Close()
	call := func(path, body string, headers ...string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(raw)
	}
	locate := `{"path":"/a/demo/p/Main.dc.html","text":"Привет"}`
	if code, body := call(platformAPI+"/design/library/locate", locate); code != 401 || body != `{"error":"Unauthorized"}` {
		t.Fatalf("no session: %d %s", code, body)
	}
	if code, body := call(platformAPI+"/design/library/locate", locate, "x-cli-token", "ok", "Content-Type", "application/json"); code != 403 {
		t.Fatalf("token only: %d %s", code, body)
	}
	code, body := call("/agents"+platformAPI+"/design/library/locate", locate, "Cookie", "session-token=ok", "Content-Type", "application/json")
	if code != 200 || !strings.Contains(body, `"kind":"template"`) || !strings.Contains(body, `"line":1`) {
		t.Fatalf("locate: %d %s", code, body)
	}
	if _, err := os.Stat(filepath.Join(ws, "design-artifacts", "CLAUDE.md")); err != nil {
		t.Fatalf("rules file not written: %v", err)
	}
	_ = os.WriteFile(mode, []byte(`{"goSlices":[]}`), 0o600)
	time.Sleep(1100 * time.Millisecond)
	if code, _ := call(platformAPI+"/design/library/locate", locate, "Cookie", "session-token=ok"); code != 299 {
		t.Fatalf("slice off: %d, want the old runtime's", code)
	}
}
