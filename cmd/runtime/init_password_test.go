package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/settings"
)

func TestGenerateInitPassword(t *testing.T) {
	shape := regexp.MustCompile(`^([2-9A-HJKMNP-TV-Z]{4}-){4}[2-9A-HJKMNP-TV-Z]{4}$`)
	seen := map[string]bool{}
	for range 200 {
		pw := generateInitPassword()
		if !shape.MatchString(pw) {
			t.Fatalf("password %q is not five groups of four characters from the alphabet", pw)
		}
		if seen[pw] {
			t.Fatalf("password %q drawn twice", pw)
		}
		seen[pw] = true
	}
}

func TestListensOnLoopback(t *testing.T) {
	for listen, want := range map[string]bool{
		"127.0.0.1:8022": true, "127.0.0.1:0": true, "localhost:8022": true, "[::1]:8022": true, "127.1.2.3:80": true,
		"0.0.0.0:8022": false, ":8022": false, "[::]:8022": false, "192.168.1.5:8022": false, "example.org:8022": false,
		"": false, "8022": false, "unix:/run/harnessly.sock": false,
	} {
		if got := listensOnLoopback(listen); got != want {
			t.Errorf("listensOnLoopback(%q) = %v, want %v", listen, got, want)
		}
	}
}

func TestWantsGeneratedInitPassword(t *testing.T) {
	cases := []struct {
		name     string
		listen   string
		force    string
		legacy   string
		upstream string
		want     bool
	}{
		{name: "container listener", listen: "0.0.0.0:8022", want: true},
		{name: "all interfaces without a host", listen: ":8022", want: true},
		{name: "loopback keeps the dev flow", listen: "127.0.0.1:8022", want: false},
		{name: "loopback forced on", listen: "127.0.0.1:8022", force: "1", want: true},
		{name: "public forced off", listen: "0.0.0.0:8022", force: "0", want: false},
		{name: "legacy AUTH_PASSWORD", listen: "0.0.0.0:8022", legacy: "old", want: false},
		{name: "behind a gate", listen: "0.0.0.0:8022", upstream: "127.0.0.1:3000", want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("RUNTIME_GATE_LISTEN", c.listen)
			t.Setenv("RUNTIME_GENERATE_INIT_PASSWORD", c.force)
			t.Setenv("AUTH_PASSWORD", c.legacy)
			if got := wantsGeneratedInitPassword(c.upstream); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// startAuth builds the sign-in routes of a runtime in a fresh data folder and returns a caller for them and what was printed.
func startAuth(t *testing.T, listen, initEnv string, config string) (call func(method, path, body string) (int, string), printed func() string, store *settings.Store) {
	t.Helper()
	data := t.TempDir()
	t.Setenv("RUNTIME_GATE_LISTEN", listen)
	t.Setenv("INIT_PASSWORD", initEnv)
	t.Setenv("AUTH_PASSWORD", "")
	t.Setenv("RUNTIME_GENERATE_INIT_PASSWORD", "")
	var out bytes.Buffer
	old := initPasswordOut
	initPasswordOut = &out
	t.Cleanup(func() { initPasswordOut = old })
	cfgFile := filepath.Join(data, "config.json")
	store = &settings.Store{DataDir: data, ConfigFile: cfgFile}
	if config != "" {
		if err := os.WriteFile(cfgFile, []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	d := deps{DataDir: data, ConfigFile: cfgFile, Cookie: "session-token", SignSession: func() string { return "a.b.c" },
		SessionOK: func(r *http.Request) bool {
			c, err := r.Cookie("session-token")
			return err == nil && c.Value == "a.b.c"
		}}
	routes := authRoutes(d, store, func(h settings.Handler) func(*http.Request) (int, any, http.Header) { return settings.Guarded(nil, h) })
	call = func(method, path, body string) (int, string) {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		for _, route := range routes {
			if route.Match(r) {
				status, v, _ := route.Compute(r)
				switch x := v.(type) {
				case *jsvalue.Object:
					return status, string(jsvalue.Stringify(x, ""))
				case json.RawMessage:
					return status, string(x)
				case gate.Raw:
					return status, string(x.Body)
				}
				return status, ""
			}
		}
		t.Fatalf("no route for %s", path)
		return 0, ""
	}
	return call, out.String, store
}

var initLine = regexp.MustCompile(`(?m)^\s*Initial password: (\S+)$`)

func TestFirstStartGeneratesInitialPassword(t *testing.T) {
	call, printed, _ := startAuth(t, "0.0.0.0:8022", "", "")
	m := initLine.FindStringSubmatch(printed())
	if m == nil {
		t.Fatalf("no \"Initial password:\" line in the output:\n%s", printed())
	}
	pw := m[1]
	if n := strings.Count(printed(), pw); n != 1 {
		t.Errorf("the password is printed %d times, want once", n)
	}
	if status, body := call("GET", "/api/auth/setup", ""); status != 200 || !strings.Contains(body, `"requiresAuth":true`) {
		t.Errorf("setup before sign-in: %d %s, want 200 and requiresAuth", status, body)
	}
	if status, _ := call("POST", "/api/auth/setup", `{"authPassword":"mine-mine"}`); status != 401 {
		t.Errorf("setup without signing in: %d, want 401: the first visitor must not be able to claim the machine", status)
	}
	if status, _ := call("POST", "/api/auth/login", `{"password":"not-the-one"}`); status != 401 {
		t.Errorf("login with a wrong password: %d, want 401", status)
	}
	if status, _ := call("POST", "/api/auth/login", `{"password":"`+pw+`"}`); status != 200 {
		t.Errorf("login with the printed password: %d, want 200", status)
	}
}

func TestInitPasswordFromEnvIsKeptAndNotPrinted(t *testing.T) {
	call, printed, _ := startAuth(t, "0.0.0.0:8022", "chosen-by-operator", "")
	if p := printed(); p != "" {
		t.Errorf("INIT_PASSWORD is set but the runtime printed:\n%s", p)
	}
	if status, _ := call("POST", "/api/auth/login", `{"password":"chosen-by-operator"}`); status != 200 {
		t.Errorf("login with INIT_PASSWORD: %d, want 200", status)
	}
}

func TestLoopbackKeepsTheDevFlow(t *testing.T) {
	call, printed, _ := startAuth(t, "127.0.0.1:8022", "", "")
	if p := printed(); p != "" {
		t.Errorf("a loopback runtime printed:\n%s", p)
	}
	if status, body := call("GET", "/api/auth/setup", ""); status != 200 || !strings.Contains(body, `"requiresAuth":false`) || !strings.Contains(body, `"needsSetup":true`) {
		t.Errorf("setup on loopback: %d %s, want open setup as before", status, body)
	}
}

func TestNoNewPasswordOnceOneIsSet(t *testing.T) {
	stored := settings.HashPassword("already-chosen")
	call, printed, _ := startAuth(t, "0.0.0.0:8022", "", `{"authPassword":"`+stored+`","authSecret":"s3cret"}`)
	if p := printed(); p != "" {
		t.Errorf("a runtime with a password printed:\n%s", p)
	}
	if status, _ := call("POST", "/api/auth/login", `{"password":"already-chosen"}`); status != 200 {
		t.Errorf("login with the stored password: %d, want 200", status)
	}
}

func TestStrongInitPasswordIsRetriedPerStart(t *testing.T) {
	_, first, _ := startAuth(t, "0.0.0.0:8022", "", "")
	_, second, _ := startAuth(t, "0.0.0.0:8022", "", "")
	a, b := initLine.FindStringSubmatch(first()), initLine.FindStringSubmatch(second())
	if a == nil || b == nil || a[1] == b[1] {
		t.Errorf("two starts should print two different passwords, got %v and %v", a, b)
	}
}
