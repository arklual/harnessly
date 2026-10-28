package api_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

func TestHealth(t *testing.T) {
	s := hubtest.New(t)
	b := s.Browser()
	if r := b.Do("GET", "/healthz", nil); r.Code != 200 || !strings.Contains(string(r.Body), `"ok":true`) || !strings.Contains(string(r.Body), `"version"`) {
		t.Errorf("healthz: %s", r)
	}
	if r := b.Do("GET", "/readyz", nil); r.Code != 200 || !strings.Contains(string(r.Body), `"ok":true`) {
		t.Errorf("readyz: %s", r)
	}
	s.DB.Close()
	if r := b.Do("GET", "/readyz", nil); r.Code != 503 || !strings.Contains(string(r.Body), `"ok":false`) {
		t.Errorf("readyz with the database gone: %s", r)
	}
	if r := b.Do("GET", "/healthz", nil); r.Code != 200 {
		t.Errorf("healthz must not depend on the database: %s", r)
	}
}

type configView struct {
	Version     string
	PublicURL   string
	Edition     string
	Signup      string
	RuntimeMode string
	TermsURL    *string
	Auth        struct {
		Email struct{ Mode string }
		// Passkey stays false here: the test hub is reached by an IP address, which has none.
		Passkey bool
		GitHub  bool
		Yandex  bool
		OIDC    []struct{ ID, Name string }
	}
	Connect struct {
		Local    struct{ Available bool }
		Command  bool
		SSH      struct{ Available bool }
		Teleport bool
	}
	Profiles struct{ Dir, Container, Full bool }
}

func config(t *testing.T, s *hubtest.Stack) configView {
	t.Helper()
	r := s.Browser().Do("GET", "/api/config", nil)
	if r.Code != 200 {
		t.Fatal(r)
	}
	var c configView
	r.JSON(&c)
	return c
}

func TestConfigDefaults(t *testing.T) {
	s := hubtest.New(t)
	c := config(t, s)
	if c.PublicURL != s.URL || c.Edition != "self-hosted" || c.Signup != "open" || c.RuntimeMode != "path" || c.TermsURL != nil || c.Version == "" {
		t.Errorf("config = %+v", c)
	}
	if c.Auth.Email.Mode != "smtp" { // the test mailer says it sends
		t.Errorf("email = %+v", c.Auth.Email)
	}
	if c.Auth.Passkey || c.Auth.GitHub || c.Auth.Yandex || len(c.Auth.OIDC) != 0 {
		t.Errorf("auth = %+v: unconfigured providers must be absent", c.Auth)
	}
	if !c.Connect.Command || !c.Connect.SSH.Available || c.Connect.Local.Available || c.Connect.Teleport {
		t.Errorf("connect = %+v", c.Connect)
	}
	if !c.Profiles.Dir || !c.Profiles.Full || c.Profiles.Container {
		t.Errorf("profiles = %+v", c.Profiles)
	}
	// Open to anyone, with no cookie: the sign-in page needs it.
	r := s.Browser().Do("GET", "/api/config", nil)
	if r.Header.Get("Set-Cookie") != "" {
		t.Errorf("config set a cookie: %v", r.Header)
	}
}

func TestConfigFollowsTheSettings(t *testing.T) {
	oidc := []hubtest.Option{
		hubtest.Env("HUB_OIDC_PROVIDERS", "sso"),
		hubtest.Env("HUB_OIDC_SSO_ISSUER", "http://127.0.0.1:1/unreachable"), hubtest.Env("HUB_OIDC_SSO_CLIENT_ID", "c"),
		hubtest.Env("HUB_OIDC_SSO_CLIENT_SECRET", "s"), hubtest.Env("HUB_OIDC_SSO_NAME", "Корпоративный вход"),
	}
	s := hubtest.New(t, append(oidc,
		hubtest.Env("HUB_GITHUB_CLIENT_ID", "gh"), hubtest.Env("HUB_GITHUB_CLIENT_SECRET", "ghs"),
		hubtest.Env("HUB_YANDEX_CLIENT_ID", "ya"), hubtest.Env("HUB_YANDEX_CLIENT_SECRET", "yas"),
		hubtest.Env("HUB_SIGNUP", "invite"), hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"),
		hubtest.Env("HUB_TERMS_URL", "https://example.com/terms"), hubtest.Env("HUB_CONTAINER_IMAGE", "img:1"), hubtest.Env("HUB_SSH", "off"),
	)...)
	c := config(t, s)
	if c.Signup != "invite" || c.RuntimeMode != "subdomain" || c.TermsURL == nil || *c.TermsURL != "https://example.com/terms" {
		t.Errorf("config = %+v", c)
	}
	if !c.Auth.GitHub || !c.Auth.Yandex {
		t.Errorf("auth = %+v", c.Auth)
	}
	if len(c.Auth.OIDC) != 1 || c.Auth.OIDC[0].ID != "sso" || c.Auth.OIDC[0].Name != "Корпоративный вход" {
		t.Errorf("oidc = %+v", c.Auth.OIDC)
	}
	if !c.Profiles.Container || c.Connect.SSH.Available {
		t.Errorf("connect = %+v profiles = %+v", c.Connect, c.Profiles)
	}

	// The cloud runs no generic OIDC (docs/hub/architecture.md, OSS and cloud).
	cloud := hubtest.New(t, append(oidc, hubtest.Env("HUB_EDITION", "cloud"), hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"))...)
	if c := config(t, cloud); c.Edition != "cloud" || len(c.Auth.OIDC) != 0 {
		t.Errorf("cloud config = %+v", c)
	}
}

func TestSSHOffAnswersWhatTheContractSays(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_SSH", "off"))
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Команда")
	r := owner.Do("POST", "/api/spaces/"+id+"/machines/ssh", map[string]any{"host": "10.0.0.1", "user": "dev", "profile": "dir"})
	if r.Code != 422 || r.Err() != "provider_off" {
		t.Errorf("ssh off: %s", r)
	}
}

func TestUnknownPathsAnswerJSON(t *testing.T) {
	s := hubtest.New(t)
	b := s.Browser()
	for _, path := range []string{"/api/nope", "/api/spaces/x/nope", "/agent/nope", "/dl/nope.bin"} {
		r := b.Do("GET", path, nil)
		if r.Code != 404 || r.Err() != "not_found" {
			t.Errorf("%s: %s", path, r)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	s := hubtest.New(t)
	r := s.Browser().Do("GET", "/api/config", nil)
	for k, want := range map[string]string{"X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY", "Referrer-Policy": "same-origin"} {
		if got := r.Header.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	if r.Header.Get("Strict-Transport-Security") != "" {
		t.Error("HSTS over plain http")
	}
	// The public address is https: that is what the hub sits behind in a real setup.
	https := hubtest.New(t, hubtest.Env("HUB_PUBLIC_URL", "https://hub.example.com"))
	if got := https.Browser().Do("GET", "/healthz", nil).Header.Get("Strict-Transport-Security"); got != "max-age=31536000" {
		t.Errorf("HSTS = %q", got)
	}
}

func TestAccessLogHasNoQueryAndNoBody(t *testing.T) {
	s := hubtest.New(t)
	b := s.Browser()
	b.Do("GET", "/api/config?code=QUERYSECRET&state=QUERYSTATE", nil)
	b.Do("POST", "/api/auth/email/start", map[string]string{"email": "bodysecret@example.com", "intent": "signup"})
	b.Do("GET", "/api/nope?hl_ticket=TICKETSECRET", nil)
	logs := s.Logs.String()
	for _, n := range []string{"QUERYSECRET", "QUERYSTATE", "TICKETSECRET"} {
		if strings.Contains(logs, n) {
			t.Errorf("%q is in the log", n)
		}
	}
	line := lineWith(logs, "route=/api/ ")
	if !strings.Contains(line, "level=INFO") || !strings.Contains(line, "status=404") {
		t.Errorf("the failed request is not in the log at info level: %q", line)
	}
	// A healthy poll is for the debug level only.
	b.Do("GET", "/healthz", nil)
	if line := lineWith(s.Logs.String(), "route=/healthz"); !strings.Contains(line, "level=DEBUG") {
		t.Errorf("healthz line = %q", line)
	}
}

// An invitation token is the last segment of the address it travels in, on the page a person opens and on the API the
// page calls: a log of raw paths would hold every live invitation.
func TestAccessLogHasNoTokensOfPaths(t *testing.T) {
	s := hubtest.New(t)
	b := s.Browser()
	const token = "INVITETOKENSECRETabcdefghij0123456789"
	b.Do("GET", "/api/invites/"+token, nil)
	b.Do("POST", "/api/invites/"+token+"/accept", map[string]any{})
	b.Do("GET", "/invite/"+token, nil)
	b.Do("GET", "/api/invites/"+token+"/no/such/route", nil)
	b.Do("GET", "/dl/"+token, nil)
	logs := s.Logs.String()
	if strings.Contains(logs, token) {
		t.Errorf("a token is in the log:\n%s", logs)
	}
	if line := lineWith(logs, "route=/api/invites/{token} "); !strings.Contains(line, "method=GET") {
		t.Errorf("the invitation lookup is not in the log by its route: %q", line)
	}
	if lineWith(logs, "route=/api/invites/{token}/accept ") == "" {
		t.Error("the acceptance is not in the log by its route")
	}
}

func lineWith(logs, sub string) string {
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// A script registers a machine without a cookie and without the headers a browser sends; the guard against forged
// cross-site requests is for the API a cookie opens.
func TestMachineEndpointsAreNotBehindTheCookieGuard(t *testing.T) {
	s := hubtest.New(t)
	b := s.Browser()
	r := b.Do("POST", "/agent/register", `{"code":"hx_nope"}`, "Content-Type", "text/plain", "Origin", "https://evil.example")
	if r.Err() == "cross_site" {
		t.Errorf("/agent/register went through the cookie guard: %s", r)
	}
	owner := s.SignIn("owner@example.com")
	if r := owner.Do("POST", "/api/spaces", map[string]string{"name": "x"}, "Origin", "https://evil.example"); r.Code != 403 || r.Err() != "cross_site" {
		t.Errorf("cross-site API call: %s", r)
	}
	if r := owner.Do("POST", "/api/spaces", `{"name":"x"}`, "Content-Type", "text/plain"); r.Code != 403 || r.Err() != "cross_site" {
		t.Errorf("a form-shaped API call: %s", r)
	}
}

func TestInstallerAndBinariesAreServed(t *testing.T) {
	s := hubtest.New(t)
	s.WriteDist("install.sh", "#!/bin/sh\necho installing\n")
	s.WriteDist("agent-linux-amd64", "AGENT")
	s.WriteDist("runtime-linux-amd64", "RUNTIME")
	s.WriteDist("notes.txt", "private")
	b := s.Browser()

	r := b.Do("GET", "/install", nil)
	if r.Code != 200 || !strings.HasPrefix(string(r.Body), "#!/bin/sh\nHARNESSLY_HUB_URL=") || !strings.Contains(string(r.Body), s.URL) || !strings.Contains(string(r.Body), "echo installing") {
		t.Errorf("install: %s", r)
	}
	r = b.Do("GET", "/dl/manifest.json", nil)
	var m struct {
		Files map[string]struct {
			SHA256 string
			Size   int
		}
	}
	r.JSON(&m)
	if r.Code != 200 || m.Files["agent-linux-amd64"].Size != 5 || len(m.Files["agent-linux-amd64"].SHA256) != 64 {
		t.Errorf("manifest: %s", r)
	}
	if _, ok := m.Files["notes.txt"]; ok {
		t.Errorf("a file that is no binary is in the manifest: %s", r)
	}
	if r := b.Do("GET", "/dl/agent-linux-amd64", nil); r.Code != 200 || string(r.Body) != "AGENT" {
		t.Errorf("binary: %s", r)
	}
	if r := b.Do("GET", "/dl/notes.txt", nil); r.Code != 404 {
		t.Errorf("a file that is no binary: %s", r)
	}
}

func TestInstallerNamesTheMachineImage(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_CONTAINER_IMAGE", "registry.example.com/machine:1"))
	s.WriteDist("install.sh", "#!/bin/sh\necho installing\n")
	r := s.Browser().Do("GET", "/install", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), "\nHARNESSLY_IMAGE='registry.example.com/machine:1'\n") {
		t.Errorf("install: %s", r)
	}
	plain := hubtest.New(t)
	plain.WriteDist("install.sh", "#!/bin/sh\necho installing\n")
	if r := plain.Browser().Do("GET", "/install", nil); strings.Contains(string(r.Body), "HARNESSLY_IMAGE") {
		t.Errorf("a hub without an image names one: %s", r)
	}
}

func TestShutdownEndsOpenStreams(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Команда")
	stream := owner.Events(id)
	if stream.Status != 200 {
		t.Fatalf("stream status %d", stream.Status)
	}
	s.Eventually("the stream to be counted", func() bool { return s.Hub.Events.Count(id) == 1 })
	s.Hub.Shutdown()
	s.Eventually("the stream to end", func() bool { return s.Hub.Events.Count(id) == 0 })
	// A hub that is stopping does not take new streams.
	if r := owner.Events(id); r.Status != 503 {
		t.Errorf("stream after shutdown: %d", r.Status)
	}
}

func TestShutdownClosesTheAgentLinks(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	id, _ := owner.NewSpace("Команда")
	a := owner.Machine(id, nil)
	s.Eventually("the link", func() bool { return s.Hub.Registry.Get(a.MachineID) != nil })
	s.Hub.Shutdown()
	if code := a.WaitClosed(); code != 1001 {
		t.Errorf("close code %d, want 1001 (going away)", code)
	}
}

func TestRunWritesTheLocalCodeAndStops(t *testing.T) {
	file := filepath.Join(t.TempDir(), "code")
	s := hubtest.New(t, hubtest.Env("HUB_LOCAL_CODE_FILE", file))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { s.Hub.Run(ctx); close(done) }()
	s.Eventually("the local code file", func() bool {
		b, err := os.ReadFile(file)
		return err == nil && strings.TrimSpace(string(b)) != ""
	})
	// The file holds a code the registration endpoint takes.
	b, _ := os.ReadFile(file)
	a := s.NewAgent(nil)
	if r := a.Register(strings.TrimSpace(string(b)), "dir", "local"); r.Code != 201 {
		t.Errorf("registering with the local code: %s", r)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func TestThePageIsServedWithItsPolicy(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":         {Data: []byte(`<!doctype html><script>document.documentElement.dataset.theme="dark"</script><script type="module" src="/assets/app-1a2b.js"></script><div id=app></div>`)},
		"assets/app-1a2b.js": {Data: []byte(`console.log("app")`)},
		"favicon.svg":        {Data: []byte(`<svg/>`)},
	}
	s := hubtest.New(t, hubtest.WithOptions(func(o *api.Options) { o.UI = ui }))
	b := s.Browser()

	for _, path := range []string{"/", "/s/team/setup/agents", "/invite/abc", "/sign-in"} {
		r := b.Do("GET", path, nil)
		if r.Code != 200 || !strings.Contains(string(r.Body), `<div id=app>`) {
			t.Errorf("%s: %s", path, r)
			continue
		}
		if r.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s: Cache-Control = %q: the page names the current assets and must not be cached", path, r.Header.Get("Cache-Control"))
		}
		csp := r.Header.Get("Content-Security-Policy")
		// One inline script is allowed by its hash; the module has a src and needs none.
		if !strings.Contains(csp, "script-src 'self' 'sha256-") || strings.Count(csp, "'sha256-") != 1 || !strings.Contains(csp, "frame-ancestors 'none'") || strings.Contains(csp, "script-src 'self' 'unsafe-inline'") {
			t.Errorf("%s: CSP = %q", path, csp)
		}
	}
	if r := b.Do("GET", "/assets/app-1a2b.js", nil); r.Code != 200 || string(r.Body) != `console.log("app")` || !strings.Contains(r.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("asset: %s %v", r, r.Header)
	}
	if r := b.Do("GET", "/favicon.svg", nil); r.Code != 200 || r.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("favicon: %s %v", r, r.Header)
	}
	// A path that looks like a file and is not there is not a route of the app.
	if r := b.Do("GET", "/assets/gone-9f.js", nil); r.Code != 404 {
		t.Errorf("missing asset: %s", r)
	}
	if r := b.Do("POST", "/s/team", "x"); r.Code != 405 {
		t.Errorf("POST to the page: %s", r)
	}
	// The API is never the page.
	if r := b.Do("GET", "/api/nothing-here", nil); r.Code != 404 || r.Err() != "not_found" {
		t.Errorf("api: %s", r)
	}
	// Dot segments do not climb out of the build.
	if r := b.Do("GET", "/assets/../../etc/passwd", nil); r.Code == 200 && strings.Contains(string(r.Body), "root:") {
		t.Errorf("path escape: %s", r)
	}
}

func TestWebDirWinsAndIsReread(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) {
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`<!doctype html><p>first</p>`)
	embedded := fstest.MapFS{"index.html": {Data: []byte(`<!doctype html><p>embedded</p>`)}}
	s := hubtest.New(t, hubtest.Env("HUB_WEB_DIR", dir), hubtest.WithOptions(func(o *api.Options) { o.UI = embedded }))
	b := s.Browser()
	if r := b.Do("GET", "/", nil); !strings.Contains(string(r.Body), "first") {
		t.Errorf("web dir: %s", r)
	}
	write(`<!doctype html><p>second</p>`)
	if r := b.Do("GET", "/", nil); !strings.Contains(string(r.Body), "second") {
		t.Errorf("web dir is not re-read: %s", r)
	}
}

func TestWithoutAPageTheRootSaysHow(t *testing.T) {
	s := hubtest.New(t)
	r := s.Browser().Do("GET", "/", nil)
	if r.Code != 200 || !strings.Contains(string(r.Body), "HUB_WEB_DIR") || !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		t.Errorf("root: %s", r)
	}
}

// The first ten minutes of a new hub: sign up, make a space, connect a machine, open it, sign out.
func TestFirstTenMinutes(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	var me struct {
		User struct {
			IsAdmin bool
			Email   string
		}
		Spaces []any
	}
	owner.Do("GET", "/api/me", nil).JSON(&me)
	if !me.User.IsAdmin || me.User.Email != "owner@example.com" || len(me.Spaces) != 0 {
		t.Fatalf("me = %+v", me)
	}
	spaceID, slug := owner.NewSpace("Моя команда")
	if slug == "" {
		t.Fatal("no slug")
	}
	a := owner.Machine(spaceID, nil)

	var open struct{ URL string }
	owner.Do("GET", "/api/machines/"+a.MachineID+"/open", nil).JSON(&open)
	if open.URL != "/m/"+a.MachineID+"/" {
		t.Fatalf("open = %+v", open)
	}
	page := owner.Do("GET", open.URL, nil)
	if page.Code != 200 || !strings.Contains(string(page.Body), "runtime GET /m/"+a.MachineID+"/") {
		t.Errorf("the runtime page: %s", page)
	}
	if r := owner.Do("POST", "/api/auth/logout", map[string]any{}); r.Code != 204 {
		t.Errorf("logout: %s", r)
	}
	if r := owner.Do("GET", open.URL, nil); r.Code == 200 {
		t.Errorf("the runtime after sign-out: %s", r)
	}
	if strings.Contains(s.Logs.String(), "level=ERROR") {
		t.Errorf("errors in the log:\n%s", s.Logs.String())
	}
}
