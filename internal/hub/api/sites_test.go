package api_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/ticket"
)

func sum256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// publish runs the upload protocol as a signed-in person.
func publish(t *testing.T, p *hubtest.Person, space, site string, files map[string]string) map[string]any {
	t.Helper()
	m := map[string]any{}
	by := map[string]string{}
	for path, c := range files {
		m[path] = map[string]any{"hash": sum256(c), "size": len(c)}
		by[sum256(c)] = c
	}
	base := "/api/sites/" + space + "/" + site
	r := p.Do("POST", base+"/deploys", map[string]any{"files": m, "source": "test"})
	if r.Code != 201 {
		t.Fatalf("begin: %s", r)
	}
	var b struct {
		ID      string
		Missing []string
	}
	r.JSON(&b)
	for _, h := range b.Missing {
		if r := p.Do("PUT", base+"/deploys/"+b.ID+"/blobs/"+h, by[h], "Content-Type", "application/octet-stream", "X-Harnessly-Client", "web"); r.Code != 204 {
			t.Fatalf("blob: %s", r)
		}
	}
	r = p.Do("POST", base+"/deploys/"+b.ID+"/commit", map[string]any{})
	if r.Code != 201 {
		t.Fatalf("commit: %s", r)
	}
	var out map[string]any
	r.JSON(&out)
	return out
}

func TestSitesInTheHubSubdomains(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	member := s.SignIn("member@example.com")
	stranger := s.SignIn("stranger@example.com")
	id, slug := owner.NewSpace("Acme")
	owner.Join(id, member)

	var cfg struct {
		Sites struct {
			Enabled        bool
			Scheme, Domain string
		}
	}
	s.Browser().Do("GET", "/api/config", nil).JSON(&cfg)
	if !cfg.Sites.Enabled || cfg.Sites.Scheme != "subdomain" || cfg.Sites.Domain != "sites.example.test" {
		t.Fatalf("config: %+v", cfg)
	}

	// Members create and deploy; a stranger and an anonymous visitor do not.
	if r := stranger.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop"}); r.Code != 403 {
		t.Fatalf("stranger creates: %s", r)
	}
	if r := s.Browser().Do("GET", "/api/sites/"+slug, nil); r.Code != 401 {
		t.Fatalf("anonymous lists: %s", r)
	}
	r := member.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop"})
	if r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	out := publish(t, member, slug, "shop", map[string]string{"index.html": "<h1>shop</h1>"})
	wantURL := "http://shop--" + slug + ".sites.example.test:" + strings.Split(s.URL, ":")[2] + "/"
	if out["url"] != wantURL {
		t.Fatalf("url %v, want %v", out["url"], wantURL)
	}
	host := "shop--" + slug + ".sites.example.test:" + strings.Split(s.URL, ":")[2] // a browser sends the port

	// The site is on its own origin and answers nothing of the hub.
	anon := s.Browser()
	r = anon.DoAt(s.URL, host, "GET", "/", nil)
	if r.Code != 302 || !strings.HasPrefix(r.Header.Get("Location"), s.URL+"/api/site-entry?host=") {
		t.Fatalf("anonymous on a team site: %s %v", r, r.Header)
	}
	if r := anon.DoAt(s.URL, host, "GET", "/api/config", nil); r.Code == 200 && strings.Contains(string(r.Body), "edition") {
		t.Fatalf("the hub's API answered on a site's origin: %s", r)
	}

	// The entry needs a hub session: anonymous goes to sign in; a stranger is refused; a member gets a ticket.
	entry := strings.TrimPrefix(anon.DoAt(s.URL, host, "GET", "/", nil).Header.Get("Location"), s.URL)
	if r := anon.Do("GET", entry, nil); r.Code != 302 || !strings.HasPrefix(r.Header.Get("Location"), "/signin?next=") {
		t.Fatalf("entry, anonymous: %s %v", r, r.Header)
	}
	if r := stranger.Do("GET", entry, nil); r.Code != 403 {
		t.Fatalf("entry, stranger: %s", r)
	}
	r = member.Do("GET", entry, nil)
	if r.Code != 302 {
		t.Fatalf("entry, member: %s", r)
	}
	back, _ := url.Parse(r.Header.Get("Location"))
	if back.Query().Get("hl_site_ticket") == "" || back.Host != host {
		t.Fatalf("back to the site: %s", back)
	}
	// An address that is not a site is not a place to send a ticket to.
	if r := member.Do("GET", "/api/site-entry?host=evil.example.net&return="+url.QueryEscape("http://evil.example.net/"), nil); r.Code != 400 {
		t.Fatalf("open redirect: %s", r)
	}
	if r := member.Do("GET", "/api/site-entry?host="+url.QueryEscape(host)+"&return="+url.QueryEscape("http://evil.example.net/"), nil); r.Code != 400 {
		t.Fatalf("open redirect to another host: %s", r)
	}

	// The ticket opens the site once, and the cookie keeps it open; the member's hub cookie never reaches the page.
	r = member.DoAt(s.URL, host, "GET", back.RequestURI(), nil)
	if r.Code != 302 || r.Header.Get("Location") != "/" {
		t.Fatalf("ticket: %s %v", r, r.Header)
	}
	r = member.DoAt(s.URL, host, "GET", "/", nil)
	if r.Code != 200 || string(r.Body) != "<h1>shop</h1>" || r.Header.Get("X-Robots-Tag") != "noindex, nofollow" || r.Header.Get("X-Frame-Options") != "" {
		t.Fatalf("with the cookie: %s %v", r, r.Header)
	}
	if r := s.Browser().DoAt(s.URL, host, "GET", back.RequestURI(), nil); r.Code != 302 || !strings.Contains(r.Header.Get("Location"), "/api/site-entry") {
		t.Fatalf("a spent ticket: %s %v", r, r.Header)
	}

	// Leaving the space ends access at once, cookie or not.
	if r := owner.Do("DELETE", "/api/spaces/"+id+"/members/"+member.ID, nil); r.Code != 204 {
		t.Fatalf("remove member: %s", r)
	}
	if r := member.DoAt(s.URL, host, "GET", "/", nil); r.Code != 403 {
		t.Fatalf("after leaving: %s", r)
	}

	// «Все, у кого есть ссылка».
	if r := owner.Do("PATCH", "/api/sites/"+slug+"/shop", map[string]any{"access": "link"}); r.Code != 200 {
		t.Fatalf("patch: %s", r)
	}
	s.Clock.Advance(3_000_000_000)
	if r := s.Browser().DoAt(s.URL, host, "GET", "/", nil); r.Code != 200 {
		t.Fatalf("open by link: %s", r)
	}
}

func TestSitesPathFallback(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	if r := owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop", "access": "link"}); r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	out := publish(t, owner, slug, "shop", map[string]string{"index.html": "<script>1</script>"})
	if out["url"] != s.URL+"/sites/shop--"+slug+"/" {
		t.Fatalf("url %v", out["url"])
	}
	r := s.Browser().Do("GET", "/sites/shop--"+slug+"/", nil)
	if r.Code != 200 || string(r.Body) != "<script>1</script>" {
		t.Fatalf("%s", r)
	}
	csp := r.Header.Get("Content-Security-Policy")
	if csp != "sandbox allow-scripts allow-forms allow-popups allow-modals" || r.Header.Get("X-Frame-Options") != "" {
		t.Fatalf("headers %v", r.Header)
	}
	// Team-only on the hub's own origin: the hub session decides.
	if r := owner.Do("PATCH", "/api/sites/"+slug+"/shop", map[string]any{"access": "team"}); r.Code != 200 {
		t.Fatalf("patch: %s", r)
	}
	s.Clock.Advance(3_000_000_000)
	if r := s.Browser().Do("GET", "/sites/shop--"+slug+"/", nil, "Accept", "text/html"); r.Code != 302 || !strings.HasPrefix(r.Header.Get("Location"), "/signin?next=") {
		t.Fatalf("anonymous: %s %v", r, r.Header)
	}
	if r := owner.Do("GET", "/sites/shop--"+slug+"/", nil); r.Code != 200 {
		t.Fatalf("owner: %s", r)
	}
}

func TestSitesZipFromTheBrowser(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "landing", "access": "link"})
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("index.html")
	if _, err := w.Write([]byte("zipped")); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	// A form cannot make this request: it declares neither JSON nor the client header.
	if r := owner.Do("POST", "/api/sites/"+slug+"/landing/upload", buf.Bytes(), "Content-Type", "application/zip"); r.Code != 403 {
		t.Fatalf("without the client header: %s", r)
	}
	r := owner.Do("POST", "/api/sites/"+slug+"/landing/upload", buf.Bytes(), "Content-Type", "application/zip", "X-Harnessly-Client", "web")
	if r.Code != 201 {
		t.Fatalf("zip: %s", r)
	}
	if r := s.Browser().Do("GET", "/sites/landing--"+slug+"/", nil); string(r.Body) != "zipped" {
		t.Fatalf("%s", r)
	}
}

func TestSitesDeployTokenOnTheHubAPI(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop", "access": "link"})
	var tk struct{ Token string }
	owner.Do("POST", "/api/sites/"+slug+"/shop/tokens", map[string]any{"name": "ci"}).JSON(&tk)
	ci := s.Browser() // no cookies: only the token
	auth := []string{"Authorization", "Bearer " + tk.Token}
	// The CLI sends plain JSON from a process that is not a browser; no cross-site headers, no cookies.
	r := ci.Do("GET", "/api/sites-token", nil, auth...)
	if r.Code != 200 || !strings.Contains(string(r.Body), `"site":"shop"`) {
		t.Fatalf("token info: %s", r)
	}
	r = ci.Do("POST", "/api/sites/"+slug+"/shop/deploys", map[string]any{"files": map[string]any{"index.html": map[string]any{"hash": sum256("x"), "size": 1}}}, auth...)
	if r.Code != 201 {
		t.Fatalf("begin with a token: %s", r)
	}
	var b struct{ ID string }
	r.JSON(&b)
	// Raw bytes with a token need neither the JSON type nor the client header.
	if r := ci.Do("PUT", "/api/sites/"+slug+"/shop/deploys/"+b.ID+"/blobs/"+sum256("x"), "x", append([]string{"Content-Type", "application/octet-stream"}, auth...)...); r.Code != 204 {
		t.Fatalf("blob with a token: %s", r)
	}
}

func TestSiteCheckForOnDemandCertificates(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop"})
	ask := func(domain string) int {
		return s.Browser().Do("GET", "/api/site-check?domain="+url.QueryEscape(domain), nil).Code
	}
	// httptest connects from loopback, which is who may ask.
	if c := ask("shop--" + slug + ".sites.example.test"); c != 200 {
		t.Errorf("a site that exists: %d", c)
	}
	for _, d := range []string{"sites.example.test", "nosuch--" + slug + ".sites.example.test"} {
		want := 404
		if d == "sites.example.test" {
			want = 200
		}
		if c := ask(d); c != want {
			t.Errorf("%s: %d, want %d", d, c, want)
		}
	}
	for _, d := range []string{"evil.example.net", "shop--" + slug + ".other.example.test", ""} {
		if c := ask(d); c != 404 {
			t.Errorf("%q: %d", d, c)
		}
	}
}

// A machine of a space proves itself with its key and gets a session to publish in that space, and only there.
func TestSitesMachinePublishingSession(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	id, slug := owner.NewSpace("Acme")
	_, other := owner.NewSpace("Beta")
	m := owner.Machine(id, nil)

	ask := func(auth string) hubtest.Resp {
		return s.Browser().Do("POST", "/agent/sites", nil, "Authorization", auth)
	}
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	dial, _ := hubproto.SignDial(m.Priv, m.MachineID, s.Clock.Now(), host)
	if r := ask(dial); r.Code != http.StatusUnauthorized {
		t.Fatalf("a dial signature opened a session: %s", r)
	}
	auth, _ := hubproto.SignSites(m.Priv, m.MachineID, s.Clock.Now(), host)
	r := ask(auth)
	if r.Code != 200 {
		t.Fatalf("session: %s", r)
	}
	var sess struct{ Space, Token, URL string }
	r.JSON(&sess)
	if sess.Space != slug || sess.Token == "" || sess.URL != s.Cfg.PublicURL {
		t.Fatalf("session %+v", sess)
	}
	// The same signature is used once.
	if r := ask(auth); r.Code != http.StatusUnauthorized {
		t.Fatalf("a replayed signature: %s", r)
	}

	bearer := []string{"Authorization", "Bearer " + sess.Token}
	web := s.Browser()
	if r := web.Do("POST", "/api/sites/"+slug, map[string]any{"name": "mockup", "access": "link"}, bearer...); r.Code != 201 {
		t.Fatalf("create with the session: %s", r)
	}
	body := "<h1>from a machine</h1>"
	base := "/api/sites/" + slug + "/mockup"
	var b struct {
		ID      string
		Missing []string
	}
	web.Do("POST", base+"/deploys", map[string]any{"files": map[string]any{"index.html": map[string]any{"hash": sum256(body), "size": len(body)}}}, bearer...).JSON(&b)
	if len(b.Missing) != 1 {
		t.Fatalf("missing %v", b.Missing)
	}
	if r := web.Do("PUT", base+"/deploys/"+b.ID+"/blobs/"+b.Missing[0], body, append(bearer, "Content-Type", "application/octet-stream")...); r.Code != 204 {
		t.Fatalf("blob: %s", r)
	}
	if r := web.Do("POST", base+"/deploys/"+b.ID+"/commit", map[string]any{}, bearer...); r.Code != 201 {
		t.Fatalf("commit: %s", r)
	}
	if r := web.Do("GET", "/api/sites/"+other, nil, bearer...); r.Code != http.StatusUnauthorized {
		t.Fatalf("a session of one space listed another's sites: %s", r)
	}
	if r := web.Do("GET", "/api/sites/"+slug, nil, "Authorization", "Bearer hsm_forged"); r.Code != http.StatusUnauthorized {
		t.Fatalf("forged session: %s", r)
	}
}

// Whoever made a site is told apart by id, not by the name shown: two people called alike are two people.
func TestSitesDeleteIsByIdNotByName(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	maker := s.SignIn("maker@example.com")
	twin := s.SignIn("twin@example.com")
	id, slug := owner.NewSpace("Acme")
	owner.Join(id, maker)
	owner.Join(id, twin)
	for _, p := range []*hubtest.Person{maker, twin} {
		if r := p.Do("PATCH", "/api/me", map[string]any{"name": "Sam"}); r.Code != 200 {
			t.Fatalf("rename: %s", r)
		}
	}
	if r := maker.Do("POST", "/api/sites/"+slug, map[string]any{"name": "shop"}); r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	if r := twin.Do("DELETE", "/api/sites/"+slug+"/shop", nil); r.Code != 403 {
		t.Fatalf("a member with the same name deleted the site: %s", r)
	}
	if r := maker.Do("DELETE", "/api/sites/"+slug+"/shop", nil); r.Code != 204 {
		t.Fatalf("the maker: %s", r)
	}
}

func machineSession(t *testing.T, s *hubtest.Stack, m *hubtest.Agent, person string) hubtest.Resp {
	t.Helper()
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	auth, _ := hubproto.SignSites(m.Priv, m.MachineID, s.Clock.Now(), host)
	var body any
	if person != "" {
		body = map[string]any{"person": person}
	}
	return s.Browser().Do("POST", "/agent/sites", body, "Authorization", auth)
}

// A machine's session is not a member: it makes sites and deploys to its own, nothing of other people's, and it ends
// with the machine.
func TestSitesMachineSessionIsNarrow(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	s.SignIn("stranger@example.com")
	id, slug := owner.NewSpace("Acme")
	m := owner.Machine(id, nil)

	// A person who is no member is not believed: the session is the machine's, with no person, and says nothing more.
	if r := machineSession(t, s, m, "stranger@example.com"); r.Code != 200 {
		t.Fatalf("session for a non-member: %s", r)
	}
	r := machineSession(t, s, m, "owner@example.com")
	if r.Code != 200 {
		t.Fatalf("session: %s", r)
	}
	var sess struct{ Token string }
	r.JSON(&sess)
	bearer := []string{"Authorization", "Bearer " + sess.Token}
	web := s.Browser()

	// Somebody's own site, made by a person.
	if r := owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "theirs", "access": "team"}); r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/sites/" + slug + "/theirs"},
		{"PATCH", "/api/sites/" + slug + "/theirs"},
		{"DELETE", "/api/sites/" + slug + "/theirs"},
		{"POST", "/api/sites/" + slug + "/theirs/tokens"},
		{"GET", "/api/sites/" + slug + "/theirs/tokens"},
		{"POST", "/api/sites/" + slug + "/theirs/deploys"},
		{"POST", "/api/sites/" + slug + "/theirs/current"},
	} {
		var body any
		if c.method == "PATCH" {
			body = map[string]any{"access": "link"}
		} else if c.method != "GET" && c.method != "DELETE" {
			body = map[string]any{"files": map[string]any{}}
		}
		if r := web.Do(c.method, c.path, body, bearer...); r.Code != 403 {
			t.Errorf("machine %s %s on another's site: %d, want 403", c.method, c.path, r.Code)
		}
	}
	// Its own site: made with an origin, deployed to, access changed; but no tokens and no delete.
	if r := web.Do("POST", "/api/sites/"+slug, map[string]any{"name": "mine", "access": "team", "origin": "sinopia:p1"}, bearer...); r.Code != 201 {
		t.Fatalf("machine creates: %s", r)
	}
	var list struct{ Sites []struct{ Name string } }
	web.Do("GET", "/api/sites/"+slug, nil, bearer...).JSON(&list)
	if len(list.Sites) != 1 || list.Sites[0].Name != "mine" {
		t.Fatalf("a machine lists only its own sites: %+v", list)
	}
	if r := web.Do("PATCH", "/api/sites/"+slug+"/mine", map[string]any{"access": "link"}, bearer...); r.Code != 200 {
		t.Fatalf("machine changes the access of its own site: %s", r)
	}
	if r := web.Do("POST", "/api/sites/"+slug+"/mine/tokens", map[string]any{"name": "x"}, bearer...); r.Code != 403 {
		t.Fatalf("a session minted a persistent token: %s", r)
	}
	if r := web.Do("DELETE", "/api/sites/"+slug+"/mine", nil, bearer...); r.Code != 403 {
		t.Fatalf("a machine deleted a site: %s", r)
	}
	var got struct{ Origin string }
	owner.Do("GET", "/api/sites/"+slug+"/mine", nil).JSON(&got)
	if got.Origin != "sinopia:p1" {
		t.Fatalf("origin %q", got.Origin)
	}

	// A person cannot put an origin on a site, and a revoked machine's session ends at once.
	if r := owner.Do("POST", "/api/sites/"+slug, map[string]any{"name": "plain", "origin": "sinopia:p1"}); r.Code != 201 {
		t.Fatalf("create: %s", r)
	}
	got.Origin = ""
	owner.Do("GET", "/api/sites/"+slug+"/plain", nil).JSON(&got)
	if got.Origin != "" {
		t.Fatalf("a person set an origin: %q", got.Origin)
	}
	if r := owner.Do("DELETE", "/api/machines/"+m.MachineID, nil); r.Code != 204 {
		t.Fatalf("revoke: %s", r)
	}
	if r := web.Do("GET", "/api/sites/"+slug, nil, bearer...); r.Code != 401 {
		t.Fatalf("a revoked machine's session still works: %s", r)
	}
}

// A person leaving the space ends the session made for them.
func TestSitesMachineSessionEndsWithThePerson(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	member := s.SignIn("member@example.com")
	id, slug := owner.NewSpace("Acme")
	owner.Join(id, member)
	m := owner.Machine(id, nil)
	s.Hub.Tickets.Issue(member.ID, m.MachineID) // the hub let the member into the machine
	r := machineSession(t, s, m, "member@example.com")
	if r.Code != 200 {
		t.Fatalf("session: %s", r)
	}
	var sess struct{ Token string }
	r.JSON(&sess)
	if r := s.Browser().Do("GET", "/api/sites/"+slug, nil, "Authorization", "Bearer "+sess.Token); r.Code != 200 {
		t.Fatalf("before: %s", r)
	}
	owner.Do("DELETE", "/api/spaces/"+id+"/members/"+member.ID, nil)
	if r := s.Browser().Do("GET", "/api/sites/"+slug, nil, "Authorization", "Bearer "+sess.Token); r.Code != 401 {
		t.Fatalf("after the person left: %s", r)
	}
}

// A hash another space uploaded proves nothing: the bytes must come from the space that publishes them.
func TestSitesBlobsAreNotSharedAcrossSpaces(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	a := s.SignIn("a@example.com")
	b := s.SignIn("b@example.com")
	_, slugA := a.NewSpace("Alpha")
	_, slugB := b.NewSpace("Beta")
	a.Do("POST", "/api/sites/"+slugA, map[string]any{"name": "secret", "access": "team"})
	publish(t, a, slugA, "secret", map[string]string{"index.html": "<h1>alpha only</h1>"})
	b.Do("POST", "/api/sites/"+slugB, map[string]any{"name": "mine", "access": "link"})

	h := sum256("<h1>alpha only</h1>")
	base := "/api/sites/" + slugB + "/mine"
	r := b.Do("POST", base+"/deploys", map[string]any{"files": map[string]any{"index.html": map[string]any{"hash": h, "size": len("<h1>alpha only</h1>")}}})
	if r.Code != 201 {
		t.Fatalf("begin: %s", r)
	}
	var begun struct {
		ID      string
		Missing []string
	}
	r.JSON(&begun)
	if len(begun.Missing) != 1 || begun.Missing[0] != h {
		t.Fatalf("the hash of another space was taken as held: %+v", begun)
	}
	if r := b.Do("POST", base+"/deploys/"+begun.ID+"/commit", map[string]any{}); r.Code != 409 {
		t.Fatalf("commit without the bytes: %s", r)
	}
	// With the bytes it works.
	publish(t, b, slugB, "mine", map[string]string{"index.html": "<h1>alpha only</h1>"})
}

// The return address of a site entry gives only its path: the origin is the hub's own, the port the sites domain's.
func TestSiteEntryBuildsTheReturnOriginItself(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	_, slug := owner.NewSpace("Acme")
	host := "shop--" + slug + ".sites.example.test"
	port := strings.Split(s.URL, ":")[2]
	entry := func(ret string) hubtest.Resp {
		return owner.Do("GET", "/api/site-entry?host="+url.QueryEscape(host)+"&return="+url.QueryEscape(ret), nil)
	}
	for _, bad := range []string{"//evil.example/x", "/\\evil.example", "https://evil.example/x", "x"} {
		if r := entry(bad); r.Code != 400 {
			t.Errorf("return %q: %d, want 400", bad, r.Code)
		}
	}
	r := entry("https://" + host + "/docs/a?x=1")
	if r.Code != 302 {
		t.Fatalf("a good return: %s", r)
	}
	to, _ := url.Parse(r.Header.Get("Location"))
	if to.Scheme != "http" || to.Host != host+":"+port || to.Path != "/docs/a" || to.Query().Get("x") != "1" || to.Query().Get("hl_site_ticket") == "" {
		t.Fatalf("location %s", to)
	}
}

// The machine says whom it publishes for; the hub believes it only for the machine's owner, or for a member the hub
// itself let into this machine lately. A stranger's address, a member's who never opened it, or an unknown one all end
// the same way: a session of the machine without a person, and no difference in the answer.
func TestSitesMachineSessionDoesNotTrustThePerson(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HARNESSLY_SITES_DOMAIN", "sites.example.test"))
	owner := s.SignIn("owner@example.com")
	member := s.SignIn("member@example.com")
	s.SignIn("stranger@example.com")
	id, slug := owner.NewSpace("Acme")
	owner.Join(id, member)
	for p, name := range map[*hubtest.Person]string{owner: "Olga Owner", member: "Mark Member"} {
		if r := p.Do("PATCH", "/api/me", map[string]any{"name": name}); r.Code != 200 {
			t.Fatalf("rename: %s", r)
		}
	}
	m := owner.Machine(id, nil)
	other := owner.Machine(id, nil)

	madeBy := func(person, site string) string {
		t.Helper()
		r := machineSession(t, s, m, person)
		if r.Code != 200 {
			t.Fatalf("session for %q: %s", person, r)
		}
		var sess struct{ Token string }
		r.JSON(&sess)
		if r := s.Browser().Do("POST", "/api/sites/"+slug, map[string]any{"name": site, "origin": "sinopia:" + site}, "Authorization", "Bearer "+sess.Token); r.Code != 201 {
			t.Fatalf("create: %s", r)
		}
		var got struct {
			CreatedBy string `json:"createdBy"`
		}
		owner.Do("GET", "/api/sites/"+slug+"/"+site, nil).JSON(&got)
		return got.CreatedBy
	}

	if by := madeBy("owner@example.com", "by-owner"); by != "Olga Owner" {
		t.Errorf("the machine's owner is believed, got %q", by)
	}
	machineName := madeBy("", "by-nobody")
	if machineName == "Olga Owner" || machineName == "Mark Member" {
		t.Fatalf("a session without a person carries a person: %q", machineName)
	}
	for i, who := range []string{"stranger@example.com", "nobody-at-all@example.com", "member@example.com"} {
		if by := madeBy(who, "spoof"+string(rune('a'+i))); by != machineName {
			t.Errorf("%s was taken for a person (%q), the session should be the machine's alone (%q)", who, by, machineName)
		}
	}
	// The answers do not tell an unknown address from a stranger.
	a := machineSession(t, s, m, "stranger@example.com")
	b := machineSession(t, s, m, "nobody-at-all@example.com")
	if a.Code != b.Code {
		t.Errorf("unknown and non-member differ: %d / %d", a.Code, b.Code)
	}

	// Tickets for a site, or for another machine, vouch for nothing here.
	s.Hub.Tickets.Issue(member.ID, "site:shop--"+slug+".sites.example.test")
	s.Hub.Tickets.Issue(member.ID, other.MachineID)
	if by := madeBy("member@example.com", "other-ticket"); by != machineName {
		t.Errorf("a ticket for a site or another machine vouched: %q", by)
	}

	// A member the hub let into the machine is believed, for a while.
	s.Hub.Tickets.Issue(member.ID, m.MachineID)
	if by := madeBy("member@example.com", "by-member"); by != "Mark Member" {
		t.Errorf("a member who opened the machine: %q", by)
	}
	s.Clock.Advance(ticket.OpenedFor + time.Minute)
	if by := madeBy("member@example.com", "stale"); by != machineName {
		t.Errorf("an old ticket still vouches: %q", by)
	}
}
