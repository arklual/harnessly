package webhook

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// A GitLab connection with a target and a token puts its address at the project by itself, keeps it there (puts it
// back once it is gone, does nothing while it is there), and takes it off when the connection goes.
func TestSelfRegistration(t *testing.T) {
	var mu sync.Mutex
	hooks := map[string]map[string]any{}
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.Method == http.MethodPost && r.URL.EscapedPath() == "/api/v4/projects/g%2Fp/hooks":
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			id := len(hooks) + 7
			hooks[strings.TrimPrefix(r.URL.Path, "")+"/"+itoa(id)] = b
			_, _ = w.Write([]byte(`{"id":` + itoa(id) + `}`))
		case r.Method == http.MethodGet || r.Method == http.MethodDelete:
			if _, ok := hooks[r.URL.Path]; !ok {
				w.WriteHeader(404)
				return
			}
			if r.Method == http.MethodDelete {
				delete(hooks, r.URL.Path)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	old := Base
	Base = func() string { return "https://hub.example.com/api/platform/automations/hook/m/m1" }
	defer func() { Base = old }()

	memo := map[string]json.RawMessage{}
	in := connectors.Instance{ID: "c1", Connector: ID, HTTP: srv.Client(),
		Config: map[string]string{"preset": PresetGitLab, fieldTarget: "g/p", fieldService: srv.URL},
		Secret: func(f string) (string, error) {
			return map[string]string{"secret": "s3cret", fieldToken: "tok"}[f], nil
		},
		Load: func(v any) error {
			b, ok := memo["m"]
			if !ok {
				return nil
			}
			return json.Unmarshal(b, v)
		},
		Store: func(v any) error { b, err := json.Marshal(v); memo["m"] = b; return err },
	}
	ctx := context.Background()
	h := hookConn{}
	if _, _, err := h.Poll(ctx, in, ""); err != nil {
		t.Fatal(err)
	}
	if len(hooks) != 1 {
		t.Fatalf("hooks %v", hooks)
	}
	for _, b := range hooks {
		if b["url"] != "https://hub.example.com/api/platform/automations/hook/m/m1/c1" || b["token"] != "s3cret" || b["pipeline_events"] != true {
			t.Fatalf("registered %v", b)
		}
	}
	// There: nothing new.
	if _, _, err := h.Poll(ctx, in, ""); err != nil || len(hooks) != 1 {
		t.Fatalf("again: %v %v", err, hooks)
	}
	// Removed at the service: put back.
	clear(hooks)
	if _, _, err := h.Poll(ctx, in, ""); err != nil || len(hooks) != 1 {
		t.Fatalf("put back: %v %v", err, hooks)
	}
	h.Forget(ctx, in)
	if len(hooks) != 0 {
		t.Fatalf("not taken off: %v", hooks)
	}
	// A wrong token says so, without the token.
	in.Secret = func(f string) (string, error) {
		return map[string]string{"secret": "s3cret", fieldToken: "bad"}[f], nil
	}
	memo = map[string]json.RawMessage{}
	_, _, err := h.Poll(ctx, in, "")
	if err == nil || strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "right to add webhooks") {
		t.Fatalf("refused: %v", err)
	}
	// Without a token: nothing happens, as before.
	in.Secret = func(f string) (string, error) { return map[string]string{"secret": "s3cret"}[f], nil }
	n := len(calls)
	if _, _, err := h.Poll(ctx, in, ""); err != nil || len(calls) != n {
		t.Fatalf("no token: %v %d", err, len(calls)-n)
	}
}

func TestAPIBase(t *testing.T) {
	for _, c := range []struct{ preset, svc, want string }{
		{PresetGitLab, "", "https://gitlab.com/api/v4"},
		{PresetGitLab, "https://gitlab.example.com/", "https://gitlab.example.com/api/v4"},
		{PresetGitHub, "", "https://api.github.com"},
		{PresetGitHub, "https://github.example.com", "https://github.example.com/api/v3"},
	} {
		in := connectors.Instance{Config: map[string]string{"preset": c.preset, fieldService: c.svc}}
		if got := apiBase(in); got != c.want {
			t.Errorf("%s %q: %s", c.preset, c.svc, got)
		}
	}
	gh := connectors.Instance{Config: map[string]string{"preset": PresetGitHub}}
	if got := hooksURL(gh, "https://api.github.com", "org:acme"); got != "https://api.github.com/orgs/acme/hooks" {
		t.Error(got)
	}
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }
