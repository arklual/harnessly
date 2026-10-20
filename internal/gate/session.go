package gate

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
)

// SessionRefresh re-signs a page's session past half its age, as the previous runtime's API did on every request it
// answered, so a person who keeps working never meets the sign-in page.
type SessionRefresh struct {
	Secret []byte
	// Cookie is the session cookie's name, MaxAge its lifetime.
	Cookie string
	MaxAge time.Duration
	// Skip: the request is let in without a session (an open path, or a valid cli-token, which is checked first).
	Skip func(r *http.Request, apiPath string) bool
	now  func() time.Time
}

// RefreshSessions turns the refresh on for every API request a Go route answers.
func (f *Gate) RefreshSessions(s SessionRefresh) {
	if s.now == nil {
		s.now = time.Now
	}
	f.refresh = &s
}

// apiPathOf is the path the API sees: without the app's mount point and trailing slashes; false for a non-API path.
func apiPathOf(r *http.Request) (string, bool) {
	p := r.URL.Path
	if rest, ok := strings.CutPrefix(p, "/agents"); ok && (rest == "" || strings.HasPrefix(rest, "/")) {
		p = rest
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	return p, p == "/api" || strings.HasPrefix(p, "/api/")
}

// cookieValue is the first cookie of that name, unquoted, as the previous runtime read it.
func cookieValue(r *http.Request, name string) string {
	for _, line := range r.Header.Values("Cookie") {
		for _, pair := range strings.Split(line, ";") {
			k, v, ok := strings.Cut(pair, "=")
			if !ok || strings.TrimSpace(k) != name {
				continue
			}
			v = strings.TrimSpace(v)
			if len(v) >= 2 && strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`) {
				v = v[1 : len(v)-1]
			}
			return v
		}
	}
	return ""
}

// apply sets a fresh session cookie on the answer when the request's session is valid and past half its age.
func (s *SessionRefresh) apply(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return
	}
	p, isAPI := apiPathOf(r)
	if !isAPI || (s.Skip != nil && s.Skip(r, p)) {
		return
	}
	token := cookieValue(r, s.Cookie)
	if token == "" {
		return
	}
	now := s.now()
	exp, ok := agent.SessionExpiry(s.Secret, token, now)
	if !ok {
		return
	}
	maxAge := int64(s.MaxAge / time.Second)
	remaining := int64(exp) - now.Unix()
	if remaining <= 0 || remaining >= maxAge/2 {
		return
	}
	cookie := s.Cookie + "=" + agent.SignHS256(s.Secret, "user", "", s.MaxAge) + "; HttpOnly; SameSite=Lax; Path=/; Max-Age=" + strconv.FormatInt(maxAge, 10)
	if r.Header.Get("X-Forwarded-Proto") == "https" {
		cookie += "; Secure"
	}
	w.Header().Set("Set-Cookie", cookie)
}
