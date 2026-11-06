package vaultkv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/secrets"
)

// fakeServer is enough of a KV engine (v1 or v2) and the login methods for the client: one mount "secret", tokens
// that the test may revoke.
type fakeServer struct {
	t         *testing.T
	version   int
	hideMount bool // the policy does not allow sys/internal/ui/mounts
	namespace string

	mu     sync.Mutex
	data   map[string]map[string]any
	tokens map[string]bool
	logins int
	seen   []string
}

func newFake(t *testing.T, version int) (*fakeServer, *httptest.Server) {
	f := &fakeServer{t: t, version: version, data: map[string]map[string]any{}, tokens: map[string]bool{"root-tok": true}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeServer) answer(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := strings.TrimPrefix(r.URL.Path, "/v1/")
	list := r.URL.Query().Get("list") == "true"
	f.seen = append(f.seen, r.Method+" "+p)
	if f.namespace != "" && r.Header.Get("X-Vault-Namespace") != f.namespace {
		f.answer(w, 404, map[string]any{"errors": []string{"no handler for route"}})
		return
	}
	if strings.HasPrefix(p, "auth/") && strings.HasSuffix(p, "/login") {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["secret_id"] == "sid-ok" || body["jwt"] == "jwt-ok" {
			f.logins++
			tok := "tok-" + string(rune('a'+f.logins))
			f.tokens[tok] = true
			f.answer(w, 200, map[string]any{"auth": map[string]any{"client_token": tok, "lease_duration": 900}})
			return
		}
		f.answer(w, 400, map[string]any{"errors": []string{"invalid credentials"}})
		return
	}
	if !f.tokens[r.Header.Get("X-Vault-Token")] {
		f.answer(w, 403, map[string]any{"errors": []string{"permission denied"}})
		return
	}
	if p == "sys/internal/ui/mounts/secret" {
		if f.hideMount {
			f.answer(w, 403, map[string]any{"errors": []string{"permission denied"}})
			return
		}
		opts := map[string]string{}
		if f.version == 2 {
			opts["version"] = "2"
		}
		f.answer(w, 200, map[string]any{"data": map[string]any{"type": "kv", "options": opts}})
		return
	}
	var key string
	switch f.version {
	case 2:
		switch {
		case strings.HasPrefix(p, "secret/data/"):
			key = strings.TrimPrefix(p, "secret/data/")
		case strings.HasPrefix(p, "secret/metadata/"):
			key = strings.TrimPrefix(p, "secret/metadata/")
		default:
			f.answer(w, 404, nil)
			return
		}
	default:
		key = strings.TrimPrefix(p, "secret/")
	}
	switch {
	case list && r.Method == http.MethodGet:
		prefix := strings.TrimSuffix(key, "/")
		if prefix != "" {
			prefix += "/"
		}
		keys := map[string]bool{}
		for k := range f.data {
			if rest, ok := strings.CutPrefix(k, prefix); ok {
				if i := strings.Index(rest, "/"); i >= 0 {
					keys[rest[:i+1]] = true
				} else {
					keys[rest] = true
				}
			}
		}
		if len(keys) == 0 {
			f.answer(w, 404, map[string]any{"errors": []string{}})
			return
		}
		out := []string{}
		for k := range keys {
			out = append(out, k)
		}
		f.answer(w, 200, map[string]any{"data": map[string]any{"keys": out}})
	case r.Method == http.MethodGet:
		d, ok := f.data[key]
		if !ok {
			f.answer(w, 404, map[string]any{"errors": []string{}})
			return
		}
		if f.version == 2 {
			f.answer(w, 200, map[string]any{"data": map[string]any{"data": d, "metadata": map[string]any{"version": 1}}})
			return
		}
		f.answer(w, 200, map[string]any{"data": d})
	case r.Method == http.MethodPut || r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f.version == 2 {
			if !strings.HasPrefix(p, "secret/data/") {
				f.answer(w, 405, nil)
				return
			}
			body, _ = body["data"].(map[string]any)
		}
		f.data[key] = body
		if f.version == 2 {
			f.answer(w, 200, map[string]any{"data": map[string]any{"version": 1}})
			return
		}
		f.answer(w, 204, nil)
	case r.Method == http.MethodDelete:
		delete(f.data, key)
		f.answer(w, 204, nil)
	default:
		f.answer(w, 405, nil)
	}
}

func writeFile(t *testing.T, name, v string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(v+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadWriteListDeleteBothVersions(t *testing.T) {
	for _, version := range []int{1, 2} {
		t.Run(map[int]string{1: "v1", 2: "v2"}[version], func(t *testing.T) {
			_, srv := newFake(t, version)
			c, err := New(Config{Addr: srv.URL, Auth: TokenFile{Path: writeFile(t, "tok", "root-tok")}})
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			if _, err := c.Read(ctx, "harnessly/u/gh"); secrets.ReasonOf(err) != secrets.ReasonNotFound {
				t.Fatalf("read of nothing: %v", err)
			}
			keys, err := c.List(ctx, "harnessly/u")
			if err != nil || len(keys) != 0 {
				t.Fatalf("empty list: %v %v", keys, err)
			}
			if err := c.Write(ctx, "harnessly/u/gh", map[string]string{"value": "s3cr3t-value"}); err != nil {
				t.Fatal(err)
			}
			got, err := c.Read(ctx, "harnessly/u/gh")
			if err != nil || got["value"] != "s3cr3t-value" {
				t.Fatalf("read: %v %v", got, err)
			}
			keys, err = c.List(ctx, "harnessly/u")
			if err != nil || len(keys) != 1 || keys[0] != "gh" {
				t.Fatalf("list: %v %v", keys, err)
			}
			if err := c.Delete(ctx, "harnessly/u/gh"); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Read(ctx, "harnessly/u/gh"); secrets.ReasonOf(err) != secrets.ReasonNotFound {
				t.Fatalf("read after delete: %v", err)
			}
		})
	}
}

func TestVersionFallsBackWhenTheMountIsHidden(t *testing.T) {
	f, srv := newFake(t, 1)
	f.hideMount = true
	c, err := New(Config{Addr: srv.URL, KVVersion: 1, Auth: TokenFile{Path: writeFile(t, "tok", "root-tok")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), "a/b", map[string]string{"value": "x1234"}); err != nil {
		t.Fatal(err)
	}
	if f.seen[len(f.seen)-1] != "PUT secret/a/b" {
		t.Fatalf("v1 path expected, saw %v", f.seen)
	}
}

func TestAppRoleLogsInAgainAfterRevoke(t *testing.T) {
	f, srv := newFake(t, 2)
	c, err := New(Config{Addr: srv.URL, Auth: AppRole{RoleID: "r", SecretIDFile: writeFile(t, "sid", "sid-ok")}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := c.Write(ctx, "p/x", map[string]string{"value": "abcd"}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.tokens = map[string]bool{}
	f.mu.Unlock()
	if _, err := c.Read(ctx, "p/x"); err != nil {
		t.Fatalf("read after the token was revoked: %v", err)
	}
	if f.logins != 2 {
		t.Fatalf("logins = %d, want 2", f.logins)
	}
}

func TestTokenExpiryLogsInAgain(t *testing.T) {
	f, srv := newFake(t, 2)
	now := time.Unix(1_800_000_000, 0)
	c, err := New(Config{Addr: srv.URL, Now: func() time.Time { return now },
		Auth: JWT{Kind: "kubernetes", Role: "harnessly", TokenFile: writeFile(t, "sa", "jwt-ok")}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	now = now.Add(14 * time.Minute) // past 90% of 900 s
	if _, err := c.List(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if f.logins != 2 {
		t.Fatalf("logins = %d, want 2", f.logins)
	}
	if !strings.Contains(strings.Join(f.seen, ","), "POST auth/kubernetes/login") {
		t.Fatalf("kubernetes mount not used: %v", f.seen)
	}
}

func TestBadLoginIsDeniedAndNamesNoValue(t *testing.T) {
	_, srv := newFake(t, 2)
	c, err := New(Config{Addr: srv.URL, Auth: JWT{Kind: "jwt", Role: "r", TokenFile: writeFile(t, "jwt", "jwt-wrong-value")}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Read(context.Background(), "a")
	if r := secrets.ReasonOf(err); r != secrets.ReasonFailed && r != secrets.ReasonDenied {
		t.Fatalf("reason %q (%v)", r, err)
	}
	if strings.Contains(err.Error(), "jwt-wrong-value") {
		t.Fatalf("the error carries the token: %v", err)
	}
}

func TestDeniedPathIsDenied(t *testing.T) {
	_, srv := newFake(t, 2)
	c, err := New(Config{Addr: srv.URL, Auth: TokenFile{Path: writeFile(t, "tok", "not-a-token")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), "a"); secrets.ReasonOf(err) != secrets.ReasonDenied {
		t.Fatalf("want denied, got %v", err)
	}
}

func TestUnreachableIsUnavailable(t *testing.T) {
	_, srv := newFake(t, 2)
	addr := srv.URL
	srv.Close()
	c, err := New(Config{Addr: addr, KVVersion: 2, Auth: TokenFile{Path: writeFile(t, "tok", "root-tok")}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Read(context.Background(), "a"); secrets.ReasonOf(err) != secrets.ReasonUnavailable {
		t.Fatalf("want unavailable, got %v", err)
	}
}

func TestNamespaceHeader(t *testing.T) {
	f, srv := newFake(t, 2)
	f.namespace = "team-a"
	c, err := New(Config{Addr: srv.URL, Namespace: "team-a", Auth: TokenFile{Path: writeFile(t, "tok", "root-tok")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(context.Background(), "a", map[string]string{"value": "abcd"}); err != nil {
		t.Fatal(err)
	}
}

func TestPathsAreChecked(t *testing.T) {
	for _, p := range []string{"", "a/../b", "/a", "a//b", "a/b/", "a b", "a?list=true"} {
		if ValidPath(p) {
			t.Errorf("%q passed", p)
		}
	}
	for _, p := range []string{"a", "harnessly/users/alice/gh-token", "a.b_c-d"} {
		if !ValidPath(p) {
			t.Errorf("%q refused", p)
		}
	}
	if _, err := New(Config{Addr: "ftp://x", Auth: TokenFile{}}); err == nil {
		t.Error("ftp accepted")
	}
}
