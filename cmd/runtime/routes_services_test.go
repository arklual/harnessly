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

// The services slice through the gate: the API's own refusal, each handler's session check, the organizer written
// where the deployment names it, and routes whose service is not named left to the old runtime.
func TestServiceRoutesThroughTheGate(t *testing.T) {
	dir, home := t.TempDir(), t.TempDir()
	for k, v := range map[string]string{
		"RUNTIME_GITLAB_HOSTS": "gitlab.example.com,gitlab.com", "RUNTIME_TRACKER_URL": "https://tracker.example.com",
		"RUNTIME_TRACKER_HOSTS": "tracker.example.com", "RUNTIME_DOCS_HOST": "docs.example.com", "RUNTIME_TASK_KEY_RE": `^(ACME|OPS)-[0-9]{1,6}$`,
		"RUNTIME_ORGANIZER_FILE": "organizer.json", "RUNTIME_SECRET_CONFIG_DIRS": ".config/acme", "YOUTRACK_TOKEN": "", "GITLAB_TOKEN": "", "GITLAB_SELF_TOKEN": "",
	} {
		t.Setenv(k, v)
	}
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":["services"]}`), 0o600)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) }))
	defer old.Close()
	f, err := gate.New(gate.Config{Listen: "127.0.0.1:0", Upstream: strings.TrimPrefix(old.URL, "http://"), DataDir: dir, CountEvery: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	person := func(r *http.Request) bool { return r.Header.Get("Cookie") == "session-token=ok" }
	token := func(r *http.Request) bool { return r.Header.Get("x-cli-token") == "ok" }
	d := deps{Home: home, DataDir: dir, Old: &gate.OldRuntime{}, SessionOK: person,
		APIRefusal: func(r *http.Request) (int, any, http.Header, bool) {
			if person(r) || token(r) {
				return 0, nil, nil, false
			}
			return 401, map[string]string{"error": "Unauthorized"}, http.Header{"Content-Type": {"application/json"}}, true
		}}
	f.Add(f.AsRoutes(serviceRoutes(d)...)...)
	f.Add(f.AsBodyRoutes(serviceBodyRoutes(d)...)...)
	srv := httptest.NewServer(f)
	defer srv.Close()

	call := func(method, path, body string, headers ...string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		return res.StatusCode, res.Header, string(raw)
	}
	asPerson := []string{"Cookie", "session-token=ok"}
	for _, c := range []struct {
		method, path, body string
		headers            []string
		status             int
		answer             string
	}{
		{"GET", "/api/platform/organizer", "", nil, 401, `{"error":"Unauthorized"}`},
		{"GET", "/api/platform/organizer", "", []string{"x-cli-token", "ok"}, 403, `{"error":"Forbidden"}`},
		{"GET", "/agents/api/platform/organizer/", "", asPerson, 200, `{"organizer":{"version":1,"revision":0,"updatedAt":"1970-01-01T00:00:00.000Z","projects":[],"assignments":{},"pins":[]}}`},
		{"PATCH", "/api/platform/organizer", `{"ops":[{"op":"pin","ref":"agent:tab-1"}]}`, append([]string{"Content-Type", "application/json"}, asPerson...), 200, ""},
		{"PATCH", "/api/platform/organizer", `{"ops":`, append([]string{"Content-Type", "application/json"}, asPerson...), 400, "Invalid JSON"},
		{"GET", "/api/platform/task?key=NOPE-1", "", asPerson, 400, `{"error":"Unsupported key"}`},
		{"GET", "/api/platform/task?key=OPS-1", "", asPerson, 503, `{"error":"no-token"}`},
		{"POST", "/api/platform/link-meta/projects", "", asPerson, 200, `{"keys":[]}`},
		{"GET", "/api/platform/link-meta?url=https%3A%2F%2Fexample.com%2F", "", asPerson, 400, `{"error":"Unsupported url"}`},
		{"GET", "/api/platform/gitlab-view?url=x", "", asPerson, 400, `{"error":"Нужна ссылка на merge request или коммит GitLab"}`},
		{"POST", "/api/platform/gitlab", `{"paths":[],"mrs":[]}`, append([]string{"Content-Type", "application/json"}, asPerson...), 200, `{"pushes":[],"mrs":[]}`},
		{"GET", "/api/platform/gitlab", "", asPerson, 405, ""},
		{"GET", "/api/platform/favicon?host=127.0.0.1", "", asPerson, 400, ""},
		{"GET", "/api/platform/favicon?host=example.org", "", nil, 401, `{"error":"Unauthorized"}`},
		{"GET", "/api/platform/link-meta/avatar?login=bad!", "", asPerson, 400, ""},
	} {
		status, h, body := call(c.method, c.path, c.body, c.headers...)
		if status != c.status || c.answer != "" && body != c.answer {
			t.Errorf("%s %s: %d %q, want %d %q", c.method, c.path, status, body, c.status, c.answer)
		}
		if strings.HasPrefix(body, "{") && status != 401 && h.Get("ETag") == "" {
			t.Errorf("%s %s: a JSON answer carries its ETag", c.method, c.path)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "organizer.json"))
	if err != nil || !strings.Contains(string(raw), `"pins": [`+"\n"+`    "agent:tab-1"`) {
		t.Errorf("organizer file: %v %s", err, raw)
	}

	// The page's start data: an empty machine's goes to the old runtime, which creates the first workspace.
	if status, _, _ := call("GET", "/api/platform/bootstrap", "", asPerson...); status != 299 {
		t.Errorf("bootstrap of an empty machine: %d", status)
	}
	_ = os.WriteFile(filepath.Join(dir, "workspaces.json"), []byte(`{"workspaces":[{"id":"ws-a","name":"A","directories":["/w"]}]}`), 0o600)
	if status, _, body := call("GET", "/api/platform/bootstrap", "", asPerson...); status != 200 ||
		!strings.HasPrefix(body, `{"owner":"Workspace","preflightOk":`) || !strings.Contains(body, `"initialWorkspace":{"workspaces":[{"id":"ws-a"`) {
		t.Errorf("bootstrap: %d %s", status, body)
	}

	// The slice off: the old runtime answers.
	_ = os.WriteFile(filepath.Join(dir, "state-mode.json"), []byte(`{"goSlices":[]}`), 0o600)
	time.Sleep(1100 * time.Millisecond)
	if status, _, _ := call("GET", "/api/platform/task?key=NOPE-1", "", asPerson...); status != 299 {
		t.Errorf("with the slice off: %d", status)
	}

	// A service the deployment does not name keeps its routes with the old runtime.
	t.Setenv("RUNTIME_TRACKER_URL", "")
	t.Setenv("RUNTIME_ORGANIZER_FILE", "")
	for _, r := range serviceRoutes(deps{Home: home, DataDir: dir, Old: &gate.OldRuntime{}, SessionOK: person, APIRefusal: d.APIRefusal}) {
		req := httptest.NewRequest("GET", "/api/platform/task", nil)
		if r.Match(req) {
			t.Error("the task route is served without a tracker")
		}
		if r.Match(httptest.NewRequest("GET", "/api/platform/organizer", nil)) {
			t.Error("the organizer is served without its file")
		}
	}
}
