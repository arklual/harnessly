package gate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"harnessly/internal/agent"
)

func TestSessionRefreshPastHalfItsAge(t *testing.T) {
	secret := []byte("pair-secret")
	g, err := New(Config{Upstream: "127.0.0.1:1", DataDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	g.Add(Route{Match: func(r *http.Request) bool { return !strings.HasPrefix(r.URL.Path, "/api/shadowed") }, Handler: ok},
		Route{Slice: "x-shadow", Match: func(r *http.Request) bool { return true }, Handler: ok})
	g.slices.at, g.slices.on = time.Now().Add(time.Hour), map[string]bool{"x-shadow": true}
	g.RefreshSessions(SessionRefresh{Secret: secret, Cookie: "session-token", MaxAge: 7 * 24 * time.Hour,
		Skip: func(r *http.Request, p string) bool {
			return strings.HasPrefix(p, "/api/cli/") || r.Header.Get("x-tok") == "t"
		}})

	old := agent.SignHS256(secret, "user", "", time.Hour)        // past half of seven days
	young := agent.SignHS256(secret, "user", "", 6*24*time.Hour) // under half its age
	call := func(path, token string, h ...string) string {
		r := httptest.NewRequest("GET", path, nil)
		if token != "" {
			r.Header.Set("Cookie", "other=1; session-token="+token+"; session-token=ignored")
		}
		for i := 0; i+1 < len(h); i += 2 {
			r.Header.Set(h[i], h[i+1])
		}
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		return w.Header().Get("Set-Cookie")
	}
	set := call("/agents/api/layout", old)
	parts := strings.Split(set, "; ")
	if len(parts) != 5 || !strings.HasPrefix(parts[0], "session-token=") || strings.Join(parts[1:], "; ") != "HttpOnly; SameSite=Lax; Path=/; Max-Age=604800" {
		t.Fatalf("%q", set)
	}
	fresh := strings.TrimPrefix(parts[0], "session-token=")
	exp, valid := agent.SessionExpiry(secret, fresh, time.Now())
	if !valid || int64(exp)-time.Now().Unix() < 604790 {
		t.Fatalf("the new token %v %v", exp, valid)
	}
	if set := call("/api/layout", old, "X-Forwarded-Proto", "https"); !strings.HasSuffix(set, "; Secure") {
		t.Fatalf("%q", set)
	}
	for name, got := range map[string]string{
		"young":        call("/api/layout", young),
		"no session":   call("/api/layout", ""),
		"forged":       call("/api/layout", agent.SignHS256([]byte("other"), "user", "", time.Hour)),
		"link token":   call("/api/layout", agent.SignHS256(secret, "user", "agent", time.Hour)),
		"open path":    call("/api/cli/tabs", old),
		"cli token":    call("/api/layout", old, "x-tok", "t"),
		"not the API":  call("/internal/terminal/size", old),
		"socket":       call("/api/status", old, "Upgrade", "websocket"),
		"shadow route": call("/api/shadowed", old),
	} {
		if got != "" {
			t.Errorf("%s: refreshed %q", name, got)
		}
	}
}
