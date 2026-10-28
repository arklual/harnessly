package proxy_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// seen is what the runtime saw of a request.
type seen struct {
	Method, URI, Host, Cookie, Authorization, XFH, XFP, XFF, Body string
}

type fixture struct {
	s       *hubtest.Stack
	owner   *hubtest.Person
	spaceID string
	a       *hubtest.Agent
	id      string
	// streamEnded is closed-over by the /stream handler of the runtime.
	streamEnded chan struct{}
}

// runtime is the machine's page: it tells what it received, sets cookies it should not be able to set, streams, and
// echoes WebSocket messages.
func (f *fixture) runtime() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/m/{id}/headers", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Add("Set-Cookie", "hl_runtime=stolen; Path=/")
		w.Header().Add("Set-Cookie", "planted=1; Path=/")
		w.Header().Set("X-Runtime", "yes")
		_ = json.NewEncoder(w).Encode(seen{
			Method: r.Method, URI: r.URL.RequestURI(), Host: r.Host, Cookie: r.Header.Get("Cookie"), Authorization: r.Header.Get("Authorization"),
			XFH: r.Header.Get("X-Forwarded-Host"), XFP: r.Header.Get("X-Forwarded-Proto"), XFF: r.Header.Get("X-Forwarded-For"), Body: string(body),
		})
	})
	mux.HandleFunc("/m/{id}/framed", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		_, _ = io.WriteString(w, "framed")
	})
	mux.HandleFunc("/m/{id}/status/{code}", func(w http.ResponseWriter, r *http.Request) {
		code, _ := strconv.Atoi(r.PathValue("code"))
		w.WriteHeader(code)
		_, _ = io.WriteString(w, "status "+r.PathValue("code"))
	})
	mux.HandleFunc("/m/{id}/redirect", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/m/"+r.PathValue("id")+"/headers", http.StatusFound)
	})
	mux.HandleFunc("/m/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			select {
			case f.streamEnded <- struct{}{}:
			default:
			}
		}()
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		for {
			if _, err := io.WriteString(w, "tick\n"); err != nil {
				return
			}
			fl.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	})
	mux.HandleFunc("/m/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		c.SetReadLimit(1 << 20)
		_ = c.Write(r.Context(), websocket.MessageText, []byte("cookie="+r.Header.Get("Cookie")))
		for {
			typ, msg, err := c.Read(r.Context())
			if err != nil {
				return
			}
			if c.Write(r.Context(), typ, append([]byte("echo:"), msg...)) != nil {
				return
			}
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<h1>runtime "+r.URL.Path+"</h1>")
	})
	return mux
}

func setup(t *testing.T, opts ...hubtest.Option) *fixture {
	t.Helper()
	f := &fixture{streamEnded: make(chan struct{}, 4)}
	f.s = hubtest.New(t, opts...)
	f.owner = f.s.SignIn("owner@example.com")
	f.spaceID, _ = f.owner.NewSpace("Команда")
	f.a = f.owner.Machine(f.spaceID, f.runtime())
	f.id = f.a.MachineID
	return f
}

func (f *fixture) see(t *testing.T, p *hubtest.Person, path string, headers ...string) (hubtest.Resp, seen) {
	t.Helper()
	r := p.Do("GET", path, nil, headers...)
	var out seen
	if r.Code == 200 {
		r.JSON(&out)
	}
	return r, out
}

func (f *fixture) sessionCalls() int { return len(f.a.Calls("runtime.session")) }

// ---- path mode ----

func TestPathModeCarriesTheRuntimeAndReplacesTheCookies(t *testing.T) {
	// Nobody is a trusted proxy here, so what the browser claims about its address is not believed.
	f := setup(t, hubtest.Env("HUB_TRUSTED_PROXIES", "10.0.0.9/32"))
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)

	for name, p := range map[string]*hubtest.Person{"owner": f.owner, "member": member} {
		r, got := f.see(t, p, "/m/"+f.id+"/headers?x=1&y=two", "Authorization", "Bearer hub-or-browser-token", "X-Forwarded-For", "6.6.6.6")
		if r.Code != 200 {
			t.Fatalf("%s: %s", name, r)
		}
		if got.Method != "GET" || got.URI != "/m/"+f.id+"/headers?x=1&y=two" {
			t.Errorf("%s: the runtime saw %s %s", name, got.Method, got.URI)
		}
		if !strings.HasPrefix(got.Cookie, "hl_runtime=runtime-token-") || strings.Contains(got.Cookie, "hl_session") || strings.Contains(got.Cookie, ";") {
			t.Errorf("%s: the runtime saw cookie %q: it must be the runtime's session and nothing of the hub's", name, got.Cookie)
		}
		if got.Authorization != "" {
			t.Errorf("%s: Authorization %q reached the runtime", name, got.Authorization)
		}
		if got.Host != strings.TrimPrefix(f.s.URL, "http://") || got.XFH != got.Host || got.XFP != "http" {
			t.Errorf("%s: host %q X-Forwarded-Host %q Proto %q", name, got.Host, got.XFH, got.XFP)
		}
		if got.XFF != "127.0.0.1" {
			t.Errorf("%s: X-Forwarded-For = %q, want the connecting address and not the forged one", name, got.XFF)
		}
		// The runtime tried to set cookies on the hub's origin: none arrives.
		if c := r.Header.Values("Set-Cookie"); len(c) != 0 {
			t.Errorf("%s: Set-Cookie %v reached the browser", name, c)
		}
		if r.Header.Get("X-Runtime") != "yes" {
			t.Errorf("%s: other headers are lost: %v", name, r.Header)
		}
	}
}

func TestForwardedAddressFromATrustedProxy(t *testing.T) {
	f := setup(t) // loopback is trusted by default: the hub sits behind a reverse proxy on the same host
	_, got := f.see(t, f.owner, "/m/"+f.id+"/headers", "X-Forwarded-For", "203.0.113.9", "X-Forwarded-Proto", "https")
	if got.XFF != "203.0.113.9" {
		t.Errorf("X-Forwarded-For = %q", got.XFF)
	}
}

func TestEachPersonGetsTheirOwnRuntimeSessionOnce(t *testing.T) {
	f := setup(t)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	base := f.sessionCalls()

	_, a1 := f.see(t, f.owner, "/m/"+f.id+"/headers")
	_, a2 := f.see(t, f.owner, "/m/"+f.id+"/headers")
	_, b1 := f.see(t, member, "/m/"+f.id+"/headers")
	if a1.Cookie != a2.Cookie || a1.Cookie == b1.Cookie {
		t.Errorf("cookies: %q %q %q", a1.Cookie, a2.Cookie, b1.Cookie)
	}
	if n := f.sessionCalls() - base; n != 2 {
		t.Errorf("%d runtime.session calls for two people, want 2", n)
	}
	c := f.a.Calls("runtime.session")[base]
	var subject string
	_ = json.Unmarshal(c.Params["subject"], &subject)
	if subject != "owner@example.com" && subject != "member@example.com" {
		t.Errorf("subject = %q: the runtime is told who the person is", subject)
	}
}

func TestRuntimeSessionIsAskedAgainWhenTheRuntimeRefusesIt(t *testing.T) {
	f := setup(t)
	f.see(t, f.owner, "/m/"+f.id+"/headers")
	base := f.sessionCalls()
	if r, _ := f.see(t, f.owner, "/m/"+f.id+"/status/401"); r.Code != 401 {
		t.Fatalf("status: %s", r)
	}
	f.see(t, f.owner, "/m/"+f.id+"/headers")
	if n := f.sessionCalls() - base; n != 1 {
		t.Errorf("%d new runtime.session calls after a 401, want 1", n)
	}
}

func TestRuntimeAnswersPassThrough(t *testing.T) {
	f := setup(t)
	for _, code := range []int{200, 201, 204, 404, 418, 500, 503} {
		r := f.owner.Do("GET", "/m/"+f.id+"/status/"+strconv.Itoa(code), nil)
		if r.Code != code {
			t.Errorf("status %d came back as %d", code, r.Code)
		}
	}
	r := f.owner.Do("GET", "/m/"+f.id+"/redirect", nil)
	if r.Code != 302 || r.Header.Get("Location") != "/m/"+f.id+"/headers" {
		t.Errorf("redirect: %s %v", r, r.Header)
	}
	body := strings.Repeat("0123456789", 100_000)
	r = f.owner.Do("POST", "/m/"+f.id+"/headers", body, "Content-Type", "text/plain")
	var got seen
	r.JSON(&got)
	if r.Code != 200 || got.Method != "POST" || got.Body != body {
		t.Errorf("a 1 MB body: %d, got %d bytes back", r.Code, len(got.Body))
	}
	r = f.owner.Do("HEAD", "/m/"+f.id+"/headers", nil)
	if r.Code != 200 || len(r.Body) != 0 {
		t.Errorf("HEAD: %s", r)
	}
	// No trailing slash is the machine's too.
	if r := f.owner.Do("GET", "/m/"+f.id, nil); r.Code != 200 || !strings.Contains(string(r.Body), "runtime /m/"+f.id) {
		t.Errorf("no trailing slash: %s", r)
	}
}

// Whatever a machine says about framing, its pages are shown only inside its own origin.
func TestMachinePagesCannotBeFramedByOtherOrigins(t *testing.T) {
	f := setup(t)
	r := f.owner.Do("GET", "/m/"+f.id+"/headers", nil)
	if csp := r.Header.Values("Content-Security-Policy"); len(csp) != 1 || csp[0] != "frame-ancestors 'self'" {
		t.Errorf("CSP = %q", csp)
	}
	if r.Header.Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("X-Frame-Options = %q", r.Header.Get("X-Frame-Options"))
	}
	// The machine's own policy stays, with ours beside it; its stricter header is not loosened.
	r = f.owner.Do("GET", "/m/"+f.id+"/framed", nil)
	if csp := strings.Join(r.Header.Values("Content-Security-Policy"), " | "); csp != "default-src 'self' | frame-ancestors 'self'" {
		t.Errorf("CSP = %q", csp)
	}
	if r.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("X-Frame-Options = %q", r.Header.Get("X-Frame-Options"))
	}
	// The hub's own pages in front of a machine are not framed at all.
	r = f.owner.Do("GET", "/m/nosuchmachine0000000/", nil, "Accept", "text/html")
	if csp := r.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || r.Header.Get("X-Frame-Options") != "DENY" {
		t.Errorf("hub page: %d CSP %q XFO %q", r.Code, csp, r.Header.Get("X-Frame-Options"))
	}
}

func TestTicketsAreTheHubsBusiness(t *testing.T) {
	f := setup(t)
	_, got := f.see(t, f.owner, "/m/"+f.id+"/headers?hl_ticket=abc&keep=1")
	if got.URI != "/m/"+f.id+"/headers?keep=1" {
		t.Errorf("the runtime saw %q", got.URI)
	}
}

func TestPathModeNeedsASessionAndMembership(t *testing.T) {
	f := setup(t)
	html := []string{"Accept", "text/html"}
	anon := f.s.Browser()

	r := anon.Do("GET", "/m/"+f.id+"/", nil, html...)
	if r.Code != 302 || r.Header.Get("Location") != "/signin?next="+url.QueryEscape("/m/"+f.id+"/") {
		t.Errorf("anonymous navigation: %s %v", r, r.Header)
	}
	if r := anon.Do("POST", "/m/"+f.id+"/api/x", map[string]any{}); r.Code != 401 || r.Err() != "unauthenticated" {
		t.Errorf("anonymous post: %s", r)
	}

	stranger := f.s.SignIn("stranger@example.com")
	r = stranger.Do("GET", "/m/"+f.id+"/", nil, html...)
	if r.Code != 403 || !strings.Contains(string(r.Body), "Нет доступа") || !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		t.Errorf("stranger navigation: %s", r)
	}
	if r.Header.Get("Content-Security-Policy") == "" {
		t.Error("the error page has no CSP")
	}
	if r := stranger.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 403 || r.Err() != "forbidden" {
		t.Errorf("stranger fetch: %s", r)
	}
	if n := f.sessionCalls(); n != 0 {
		t.Errorf("%d runtime sessions were asked for people who may not enter", n)
	}
	if got := len(f.a.Calls("tunnel.open")); got != 0 {
		t.Errorf("%d tunnels were opened for people who may not enter", got)
	}
}

// Without HUB_RUNTIME_DOMAIN a machine's page runs on the hub's own origin, as whoever opens it. Sign-up is open by
// default, so a stranger can make a space and a machine and invite the administrator to it: only the machines an
// administrator connected may run there, and nobody else's, however the person who opens it came to be a member.
func TestPathModeOpensOnlyMachinesAnAdministratorConnected(t *testing.T) {
	f := setup(t) // the owner is the first account: the administrator
	stranger := f.s.SignIn("stranger@example.com")
	theirSpace, _ := stranger.NewSpace("Чужая")
	theirs := stranger.Machine(theirSpace, f.runtime())
	stranger.Join(theirSpace, f.owner) // the administrator is invited to the stranger's space
	html := []string{"Accept", "text/html"}

	for name, p := range map[string]*hubtest.Person{"the administrator invited in": f.owner, "its creator": stranger} {
		r := p.Do("GET", "/m/"+theirs.MachineID+"/headers", nil)
		if r.Code != 403 || r.Err() != "machine_untrusted" {
			t.Errorf("%s: fetch: %s", name, r)
		}
		r = p.Do("GET", "/m/"+theirs.MachineID+"/", nil, html...)
		if r.Code != 403 || !strings.Contains(string(r.Body), "HUB_RUNTIME_DOMAIN") || !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s: navigation: %s", name, r)
		}
		r = p.Do("GET", "/api/machines/"+theirs.MachineID+"/open", nil)
		if r.Code != 403 || r.Err() != "machine_untrusted" {
			t.Errorf("%s: open: %s", name, r)
		}
	}
	if n := len(theirs.Calls("runtime.session")) + len(theirs.Calls("tunnel.open")); n != 0 {
		t.Errorf("%d sessions or tunnels were made for a machine that may not run here", n)
	}
	// someone who is not in its space is told nothing about it
	other := f.s.SignIn("other@example.com")
	if r := other.Do("GET", "/m/"+theirs.MachineID+"/headers", nil); r.Code != 403 || r.Err() != "forbidden" {
		t.Errorf("a person outside the space: %s", r)
	}
	// the machine an administrator connected opens as before, for the people of its space
	if r, got := f.see(t, f.owner, "/m/"+f.id+"/headers"); r.Code != 200 || got.URI != "/m/"+f.id+"/headers" {
		t.Errorf("the administrator's machine: %s", r)
	}
	if r := f.owner.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 200 {
		t.Errorf("open of the administrator's machine: %s", r)
	}
}

// A person named in HUB_ADMIN_EMAILS is an administrator whenever they signed in, not only the first one.
func TestPathModeTrustsMachinesOfEveryAdministrator(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_ADMIN_EMAILS", "owner@example.com, boss@example.com"))
	boss := f.s.SignIn("boss@example.com")
	space, _ := boss.NewSpace("Boss")
	m := boss.Machine(space, f.runtime())
	if r := boss.Do("GET", "/m/"+m.MachineID+"/headers", nil); r.Code != 200 {
		t.Errorf("a machine of a second administrator: %s", r)
	}
	// a stranger is not one, whatever address they claim
	member := f.s.SignIn("member@example.com")
	space, _ = member.NewSpace("Member")
	m = member.Machine(space, f.runtime())
	if r := member.Do("GET", "/m/"+m.MachineID+"/headers", nil); r.Code != 403 || r.Err() != "machine_untrusted" {
		t.Errorf("a machine of a member: %s", r)
	}
}

// With a runtime domain every machine has an origin of its own, so whose machine it is does not matter.
func TestSubdomainModeOpensMachinesOfAnyMember(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	member := f.s.SignIn("member@example.com")
	space, _ := member.NewSpace("Member")
	m := member.Machine(space, f.runtime())
	if host, _ := f.open(t, member, m.MachineID); host != f.host(m.MachineID) {
		t.Errorf("host = %q", host)
	}
}

func TestUnknownAndRevokedMachines(t *testing.T) {
	f := setup(t)
	html := []string{"Accept", "text/html"}
	for _, path := range []string{"/m/aaaaaaaaaaaaaaaaaaaa/", "/m/short/", "/m/", "/m/" + strings.ToUpper(f.id) + "/"} {
		r := f.owner.Do("GET", path, nil, html...)
		if r.Code != 404 || !strings.Contains(string(r.Body), "Не найдено") {
			t.Errorf("%s: %s", path, r)
		}
	}
	f.owner.Do("DELETE", "/api/machines/"+f.id, nil)
	r := f.owner.Do("GET", "/m/"+f.id+"/", nil, html...)
	if r.Code != 404 {
		t.Errorf("revoked: %s", r)
	}
}

func TestOfflineMachine(t *testing.T) {
	f := setup(t)
	f.a.Close()
	f.s.Eventually("the link to go", func() bool {
		var got struct{ Machine struct{ Status string } }
		f.owner.Do("GET", "/api/machines/"+f.id, nil).JSON(&got)
		return got.Machine.Status == "offline"
	})
	r := f.owner.Do("GET", "/m/"+f.id+"/", nil, "Accept", "text/html")
	if r.Code != 502 || !strings.Contains(string(r.Body), "Машина не на связи") {
		t.Errorf("page: %s", r)
	}
	r = f.owner.Do("GET", "/m/"+f.id+"/api/x", nil)
	if r.Code != 502 || r.Err() != "machine_offline" {
		t.Errorf("api: %s", r)
	}
}

func TestPolicyThatForbidsRuntimeSessions(t *testing.T) {
	f := setup(t)
	f.a.Handle("runtime.session", func(hubtest.Call) (any, error) {
		return nil, hubtest.Fail(hubproto.CodePolicyDenied, "runtime_session is off")
	})
	r := f.owner.Do("GET", "/m/"+f.id+"/", nil, "Accept", "text/html")
	if r.Code != 403 || !strings.Contains(string(r.Body), "Запрещено настройками машины") {
		t.Errorf("page: %s", r)
	}
	r = f.owner.Do("GET", "/m/"+f.id+"/x", nil)
	if r.Code != 403 || r.Err() != "machine_policy" {
		t.Errorf("api: %s", r)
	}
}

func TestBrokenRuntimeSessionIsRefused(t *testing.T) {
	f := setup(t)
	cases := map[string]hubproto.SessionGrant{
		"empty token":        {CookieName: "hl_runtime", Token: ""},
		"semicolon in token": {CookieName: "hl_runtime", Token: "a; admin=1"},
		"newline in name":    {CookieName: "x\r\nSet-Cookie: y", Token: "t"},
		"no name":            {Token: "t"},
	}
	for name, g := range cases {
		t.Run(name, func(t *testing.T) {
			f.a.Handle("runtime.session", func(hubtest.Call) (any, error) { return g, nil })
			// A fresh person each time: grants are cached per person.
			p := f.s.SignIn(strings.ReplaceAll(name, " ", "") + "@example.com")
			f.owner.Join(f.spaceID, p)
			if r := p.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 502 || r.Err() != "machine_error" {
				t.Errorf("%s", r)
			}
		})
	}
}

func TestRuntimePageSignOutIsTheHubs(t *testing.T) {
	f := setup(t)
	before := len(f.a.Calls("tunnel.open"))
	r := f.owner.Do("POST", "/m/"+f.id+"/api/auth/logout", map[string]any{})
	if r.Code != 204 {
		t.Fatalf("logout: %s", r)
	}
	if n := len(f.a.Calls("tunnel.open")); n != before {
		t.Error("the logout reached the runtime")
	}
	// The hub session stays: the page's «Выйти» goes to the hub's own sign-out.
	if r := f.owner.Do("GET", "/api/me", nil); r.Code != 200 {
		t.Errorf("me: %s", r)
	}
	if r := f.owner.Do("POST", "/m/"+f.id+"/api/auth/logout", map[string]any{}, "Sec-Fetch-Site", "cross-site"); r.Code != 403 || r.Err() != "cross_site" {
		t.Errorf("cross-site logout: %s", r)
	}
}

func TestWebSocketThroughTheTunnel(t *testing.T) {
	f := setup(t)
	u, _ := url.Parse(f.s.URL)
	var cookie []string
	for _, c := range f.owner.Client.Jar.Cookies(u) {
		cookie = append(cookie, c.Name+"="+c.Value)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://"+u.Host+"/m/"+f.id+"/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": {strings.Join(cookie, "; ")}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 20)
	_, hello, err := c.Read(ctx)
	if err != nil || !strings.HasPrefix(string(hello), "cookie=hl_runtime=runtime-token-") || strings.Contains(string(hello), "hl_session") {
		t.Fatalf("hello = %q %v", hello, err)
	}
	for _, msg := range []string{"one", strings.Repeat("big", 50_000)} {
		if err := c.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		_, got, err := c.Read(ctx)
		if err != nil || string(got) != "echo:"+msg {
			t.Fatalf("echo of %d bytes: %d bytes back, %v", len(msg), len(got), err)
		}
	}

	// Without the session the upgrade is refused.
	_, res, err := websocket.Dial(ctx, "ws://"+u.Host+"/m/"+f.id+"/ws", nil)
	if err == nil || res == nil || (res.StatusCode != 302 && res.StatusCode != 401) {
		t.Errorf("anonymous upgrade: %v %v", res, err)
	}
}

func TestRemovingAMemberEndsWhatTheyHaveOpen(t *testing.T) {
	f := setup(t)
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)

	req, _ := http.NewRequest("GET", f.s.URL+"/m/"+f.id+"/stream", nil)
	res, err := member.Client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	br := bufio.NewReader(res.Body)
	if line, err := br.ReadString('\n'); err != nil || line != "tick\n" {
		t.Fatalf("first line %q %v", line, err)
	}
	ended := make(chan error, 1)
	go func() {
		for {
			if _, err := br.ReadString('\n'); err != nil {
				ended <- err
				return
			}
		}
	}()

	if r := f.owner.Do("DELETE", "/api/spaces/"+f.spaceID+"/members/"+member.ID, nil); r.Code != 204 {
		t.Fatal(r)
	}
	select {
	case err := <-ended:
		t.Logf("the removed member's stream ended: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the removed member's stream is still running")
	}
	select {
	case <-f.streamEnded:
	case <-time.After(5 * time.Second):
		t.Error("the runtime was not told the request ended")
	}
	// And they do not get back in on the strength of a belief the hub cached.
	if r := member.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 403 {
		t.Errorf("after removal: %s", r)
	}
}

func TestRevokingAMachineEndsItsSessions(t *testing.T) {
	f := setup(t)
	f.see(t, f.owner, "/m/"+f.id+"/headers")
	f.owner.Do("DELETE", "/api/machines/"+f.id, nil)
	if r := f.owner.Do("GET", "/m/"+f.id+"/headers", nil); r.Code != 404 {
		t.Errorf("after revoke: %s", r)
	}
}

// ---- subdomain mode ----

const runtimeDomain = "m.example.test"

// origin is a browser that has no cookie jar of its own: the test carries cookies by hand, so that the hub's origin and
// a machine's cannot leak into each other through the jar (both are 127.0.0.1 here).
type origin struct {
	f      *fixture
	raw    *hubtest.Person
	cookie string
}

func (f *fixture) machineOrigin() *origin {
	return &origin{f: f, raw: &hubtest.Person{S: f.s, Client: &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}}
}

func (f *fixture) host(id string) string {
	u, _ := url.Parse(f.s.URL)
	return id + "." + runtimeDomain + ":" + u.Port()
}

func (o *origin) do(host, method, path string, headers ...string) hubtest.Resp {
	o.f.s.T.Helper()
	if o.cookie != "" {
		headers = append(headers, "Cookie", o.cookie)
	}
	return o.raw.DoAt(o.f.s.URL, host, method, path, nil, headers...)
}

// open asks the hub for a machine's address and returns its path and host.
func (f *fixture) open(t *testing.T, p *hubtest.Person, id string) (host, requestURI string) {
	t.Helper()
	r := p.Do("GET", "/api/machines/"+id+"/open", nil)
	var out struct{ URL string }
	r.JSON(&out)
	u, err := url.Parse(out.URL)
	if err != nil || r.Code != 200 {
		t.Fatalf("open: %s", r)
	}
	return u.Host, u.RequestURI()
}

func (o *origin) enter(host, requestURI string) hubtest.Resp {
	o.f.s.T.Helper()
	r := o.do(host, "GET", requestURI)
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == "hl_run" {
			o.cookie = "hl_run=" + c.Value
		}
	}
	return r
}

func TestSubdomainModeTicketCookieAndRuntime(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	host, uri := f.open(t, f.owner, f.id)
	if host != f.host(f.id) {
		t.Fatalf("host = %q, want %q", host, f.host(f.id))
	}
	o := f.machineOrigin()

	// The ticket buys a cookie of this origin only, and a page that refreshes into the address without it. It is not a
	// redirect: the cookie is Strict, and a browser would not send it with the request that follows a redirect of a
	// navigation that began on the hub's page.
	r := o.enter(host, uri)
	if r.Code != 200 || r.Header.Get("Location") != "" || !strings.HasPrefix(r.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("redeem: %s %v", r, r.Header)
	}
	if want := `<meta http-equiv="refresh" content="0;url=/m/` + f.id + `/`; !strings.Contains(string(r.Body), want) || strings.Contains(string(r.Body), "hl_ticket") {
		t.Errorf("landing page does not refresh into the clean address: %s", r.Body)
	}
	if r.Header.Get("Referrer-Policy") != "no-referrer" || r.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("headers = %v", r.Header)
	}
	var run *http.Cookie
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		if c.Name == "hl_run" {
			run = c
		}
	}
	if run == nil || !run.HttpOnly || run.Domain != "" || run.Path != "/" || run.SameSite != http.SameSiteStrictMode || run.MaxAge != 12*3600 {
		t.Fatalf("cookie = %+v", run)
	}

	// With the cookie the runtime is served, with the runtime's cookie in place of it.
	r = o.do(host, "GET", "/m/"+f.id+"/headers?q=1")
	var got seen
	r.JSON(&got)
	if r.Code != 200 || !strings.HasPrefix(got.Cookie, "hl_runtime=runtime-token-") || strings.Contains(got.Cookie, "hl_run=") || got.Host != host {
		t.Errorf("runtime: %s\n%+v", r, got)
	}
	if got.XFH != host {
		t.Errorf("X-Forwarded-Host = %q", got.XFH)
	}

	// The ticket is single use: the second try buys nothing and goes round again.
	o2 := f.machineOrigin()
	r = o2.enter(host, uri)
	if r.Code != 302 || len(r.Header.Values("Set-Cookie")) != 0 || !strings.HasPrefix(r.Header.Get("Location"), f.s.URL+"/api/machines/"+f.id+"/open?redirect=1&path=") {
		t.Errorf("replayed ticket: %s %v", r, r.Header)
	}
}

func TestSubdomainModeWithoutACookie(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	o := f.machineOrigin()
	host := f.host(f.id)

	r := o.do(host, "GET", "/m/"+f.id+"/x?a=1")
	want := f.s.URL + "/api/machines/" + f.id + "/open?redirect=1&path=" + url.QueryEscape("/m/"+f.id+"/x?a=1")
	if r.Code != 302 || r.Header.Get("Location") != want {
		t.Errorf("navigation: %s\nLocation %q\nwant     %q", r, r.Header.Get("Location"), want)
	}
	if r := o.do(host, "POST", "/m/"+f.id+"/x"); r.Code != 401 {
		t.Errorf("post: %s", r)
	}
	if r := o.do(host, "GET", "/"); r.Code != 302 || r.Header.Get("Location") != "/m/"+f.id+"/" {
		t.Errorf("root: %s %v", r, r.Header)
	}
	// A machine's origin runs that machine and nothing of the hub.
	for _, path := range []string{"/api/me", "/api/config", "/signin", "/healthz", "/m/aaaaaaaaaaaaaaaaaaaa/"} {
		r := o.do(host, "GET", path, "Accept", "text/html")
		if r.Code != 404 {
			t.Errorf("%s on a machine origin: %s", path, r)
		}
	}
	if r := o.do(host, "POST", "/api/auth/logout"); r.Code == 204 || r.Code == 200 {
		t.Errorf("the hub's sign-out answered on a machine origin: %s", r)
	}
}

func TestSubdomainModeSendsHubPathsToTheMachinesOrigin(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	r := f.owner.Do("GET", "/m/"+f.id+"/x?a=1", nil)
	want := "/api/machines/" + f.id + "/open?redirect=1&path=" + url.QueryEscape("/m/"+f.id+"/x?a=1")
	if r.Code != 302 || r.Header.Get("Location") != want {
		t.Errorf("%s\nLocation %q\nwant     %q", r, r.Header.Get("Location"), want)
	}
}

func TestOneMachinesCookieIsNotAnothers(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	other := f.owner.Machine(f.spaceID, f.runtime())

	o := f.machineOrigin()
	host, uri := f.open(t, f.owner, f.id)
	o.enter(host, uri)
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 200 {
		t.Fatalf("own machine: %s", r)
	}
	// The same cookie on another machine's origin is a stranger's.
	r := o.do(f.host(other.MachineID), "GET", "/m/"+other.MachineID+"/headers")
	if r.Code != 302 || !strings.Contains(r.Header.Get("Location"), "/open?redirect=1") {
		t.Errorf("another machine with this cookie: %s", r)
	}
	// And a ticket for one machine does not open another.
	_, uri = f.open(t, f.owner, f.id)
	o3 := f.machineOrigin()
	r = o3.enter(f.host(other.MachineID), strings.ReplaceAll(uri, f.id, other.MachineID))
	if len(r.Header.Values("Set-Cookie")) != 0 {
		t.Errorf("a ticket of one machine bought a cookie for another: %v", r.Header)
	}
}

func TestSubdomainModeNonMembersAndRemoval(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	host, uri := f.open(t, member, f.id)
	o := f.machineOrigin()
	o.enter(host, uri)
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 200 {
		t.Fatalf("member: %s", r)
	}
	f.owner.Do("DELETE", "/api/spaces/"+f.spaceID+"/members/"+member.ID, nil)
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 403 {
		t.Errorf("a removed member's cookie still works: %s", r)
	}
	stranger := f.s.SignIn("stranger@example.com")
	if r := stranger.Do("GET", "/api/machines/"+f.id+"/open", nil); r.Code != 403 {
		t.Errorf("a stranger got a ticket: %s", r)
	}
}

func TestSubdomainLogoutEndsTheMachinesSession(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	host, uri := f.open(t, f.owner, f.id)
	o := f.machineOrigin()
	o.enter(host, uri)
	r := o.do(host, "POST", "/m/"+f.id+"/api/auth/logout")
	if r.Code != 204 {
		t.Fatalf("logout: %s", r)
	}
	cleared := false
	for _, c := range (&http.Response{Header: r.Header}).Cookies() {
		cleared = cleared || (c.Name == "hl_run" && c.MaxAge < 0)
	}
	if !cleared {
		t.Errorf("the cookie was not cleared: %v", r.Header)
	}
	if r := o.do(host, "GET", "/m/"+f.id+"/headers"); r.Code != 302 {
		t.Errorf("the old cookie still works: %s", r)
	}
	// The hub session is untouched.
	if r := f.owner.Do("GET", "/api/me", nil); r.Code != 200 {
		t.Errorf("hub session: %s", r)
	}
}

// A page of one machine must not be able to use a person's session on another: the origins are siblings, so the
// browser would attach the cookie. Only a navigation, a person following a link, may come from elsewhere.
func TestSubdomainModeRefusesRequestsOfOtherPages(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	host, uri := f.open(t, f.owner, f.id)
	o := f.machineOrigin()
	o.enter(host, uri)
	path := "/m/" + f.id + "/headers"
	sibling := "http://other." + runtimeDomain

	for name, tc := range map[string]struct {
		method  string
		headers []string
		want    int
	}{
		"the machine's own page":            {"GET", []string{"Sec-Fetch-Site", "same-origin", "Sec-Fetch-Mode", "cors"}, 200},
		"a request without Fetch Metadata":  {"GET", nil, 200},
		"a typed address":                   {"GET", []string{"Sec-Fetch-Site", "none", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"}, 200},
		"a link from the hub or elsewhere":  {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"}, 200},
		"an iframe of another site":         {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "iframe"}, 403},
		"an iframe of a sibling machine":    {"GET", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "iframe"}, 403},
		"a frame of a sibling machine":      {"GET", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "frame"}, 403},
		"an embed of another site":          {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "embed"}, 403},
		"an object of another site":         {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "object"}, 403},
		"a navigation that names no kind":   {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate"}, 403},
		"the machine's own iframe":          {"GET", []string{"Sec-Fetch-Site", "same-origin", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "iframe"}, 200},
		"a link from a sibling machine":     {"GET", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"}, 200},
		"a fetch of a sibling machine":      {"GET", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "cors"}, 403},
		"a fetch of another site":           {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "cors"}, 403},
		"an image of a sibling machine":     {"GET", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "no-cors", "Sec-Fetch-Dest", "image"}, 403},
		"a form of a sibling machine":       {"POST", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "navigate"}, 403},
		"a write of a sibling, by Origin":   {"POST", []string{"Origin", sibling}, 403},
		"a preflight of a sibling":          {"OPTIONS", []string{"Sec-Fetch-Site", "same-site", "Sec-Fetch-Mode", "cors"}, 403},
		"a write of the machine's own page": {"POST", []string{"Origin", "http://" + host}, 200},
	} {
		r := o.do(host, tc.method, path, tc.headers...)
		if r.Code != tc.want {
			t.Errorf("%s: %s", name, r)
		}
		if tc.want == 403 && r.Err() != "cross_site" {
			t.Errorf("%s: answered %q", name, r.Err())
		}
	}

	// A socket of a sibling's page is refused before it upgrades.
	u, _ := url.Parse(f.s.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, res, err := websocket.Dial(ctx, "ws://"+u.Host+"/m/"+f.id+"/ws", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {o.cookie}, "Origin": {sibling}, "Sec-Fetch-Site": {"same-site"}}, Host: host})
	if err == nil || res == nil || res.StatusCode != 403 {
		t.Errorf("a sibling's socket: %v %v", res, err)
	}
}

// Path mode is one origin, but a page of another site still cannot make requests with the person's session.
func TestPathModeRefusesRequestsOfOtherSites(t *testing.T) {
	f := setup(t)
	path := "/m/" + f.id + "/headers"
	for name, tc := range map[string]struct {
		method  string
		headers []string
		want    int
	}{
		"the hub's own page":     {"GET", []string{"Sec-Fetch-Site", "same-origin", "Sec-Fetch-Mode", "cors"}, 200},
		"a link from elsewhere":  {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "document"}, 200},
		"an iframe of elsewhere": {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "navigate", "Sec-Fetch-Dest", "iframe"}, 403},
		"a fetch from elsewhere": {"GET", []string{"Sec-Fetch-Site", "cross-site", "Sec-Fetch-Mode", "cors"}, 403},
		"a write by Origin":      {"POST", []string{"Origin", "https://evil.example"}, 403},
	} {
		r := f.owner.Do(tc.method, path, nil, tc.headers...)
		if r.Code != tc.want {
			t.Errorf("%s: %s", name, r)
		}
	}
}

func TestSubdomainWebSocket(t *testing.T) {
	f := setup(t, hubtest.Env("HUB_RUNTIME_DOMAIN", runtimeDomain))
	host, uri := f.open(t, f.owner, f.id)
	o := f.machineOrigin()
	o.enter(host, uri)
	u, _ := url.Parse(f.s.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h := http.Header{"Cookie": {o.cookie}}
	c, _, err := websocket.Dial(ctx, "ws://"+u.Host+"/m/"+f.id+"/ws", &websocket.DialOptions{HTTPHeader: h, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	_, hello, err := c.Read(ctx)
	if err != nil || !strings.HasPrefix(string(hello), "cookie=hl_runtime=") || strings.Contains(string(hello), "hl_run=") {
		t.Fatalf("hello = %q %v", hello, err)
	}
	_ = c.Write(ctx, websocket.MessageText, []byte("hi"))
	if _, got, err := c.Read(ctx); err != nil || string(got) != "echo:hi" {
		t.Errorf("echo = %q %v", got, err)
	}
}
