package sites

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

const adminToken = "admin-secret-token-for-tests"

type env struct {
	t     *testing.T
	svc   *Service
	api   http.Handler
	edge  http.Handler
	auth  *GatewayAuthority
	now   *time.Time
	blobs *FolderBlobs
	db    *store.DB
}

func newEnv(t *testing.T, mod func(*Config)) *env {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+t.TempDir()+"/sites.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	blobs, err := NewFolderBlobs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	clock := func() time.Time { return now }
	key := []byte("0123456789abcdef0123456789abcdef")
	auth := &GatewayAuthority{AdminToken: adminToken, LoginURL: "https://gw.test/enter", HMACTickets: NewHMACTickets(key, clock)}
	cfg := Config{Domain: "sites.test", PublicURL: "https://hub.test", Secure: true, Key: key, AbuseURL: "https://hub.test/abuse"}
	if mod != nil {
		mod(&cfg)
	}
	svc, err := New(Deps{DB: db, Blobs: blobs, Auth: auth, Config: cfg, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	svc.Mount(mux)
	return &env{t: t, svc: svc, api: mux, edge: svc.Edge(), auth: auth, now: &now, blobs: blobs, db: db}
}

type resp struct {
	*httptest.ResponseRecorder
}

func (r resp) json(v any) {
	if err := json.Unmarshal(r.Body.Bytes(), v); err != nil {
		panic(err.Error() + ": " + r.Body.String())
	}
}

func (e *env) call(method, target string, body any, hdr map[string]string) resp {
	e.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rd)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	w := httptest.NewRecorder()
	e.api.ServeHTTP(w, req)
	return resp{w}
}

func (e *env) get(host, target string, hdr map[string]string) resp {
	e.t.Helper()
	req := httptest.NewRequest("GET", "https://"+host+target, nil)
	req.Header.Set("Accept", "text/html")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.edge.ServeHTTP(w, req)
	return resp{w}
}

func sha(b string) string {
	s := sha256.Sum256([]byte(b))
	return hex.EncodeToString(s[:])
}

// deploy runs the whole protocol for files and returns the commit answer.
func (e *env) deploy(space, site string, files map[string]string, hdr map[string]string) map[string]any {
	e.t.Helper()
	m := map[string]File{}
	byHash := map[string]string{}
	for p, c := range files {
		m[p] = File{Hash: sha(c), Size: int64(len(c))}
		byHash[sha(c)] = c
	}
	base := "/api/sites/" + space + "/" + site
	r := e.call("POST", base+"/deploys", map[string]any{"files": m}, hdr)
	if r.Code != 201 {
		e.t.Fatalf("begin: %d %s", r.Code, r.Body)
	}
	var b struct {
		ID      string   `json:"id"`
		Missing []string `json:"missing"`
	}
	r.json(&b)
	for _, h := range b.Missing {
		up := e.call("PUT", base+"/deploys/"+b.ID+"/blobs/"+h, byHash[h], hdr)
		if up.Code != 204 {
			e.t.Fatalf("blob: %d %s", up.Code, up.Body)
		}
	}
	r = e.call("POST", base+"/deploys/"+b.ID+"/commit", map[string]any{}, hdr)
	if r.Code != 201 {
		e.t.Fatalf("commit: %d %s", r.Code, r.Body)
	}
	var out map[string]any
	r.json(&out)
	out["missingAtBegin"] = len(b.Missing)
	return out
}

func (e *env) newSite(space, name, access string) {
	e.t.Helper()
	r := e.call("POST", "/api/sites/"+space, map[string]any{"name": name, "access": access}, nil)
	if r.Code != 201 {
		e.t.Fatalf("create: %d %s", r.Code, r.Body)
	}
}

func TestDeployAndServe(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	out := e.deploy("acme", "shop", map[string]string{
		"index.html":             "<h1>v1</h1>",
		"assets/app-3f9a1c2b.js": "console.log(1)",
		"about/index.html":       "about",
		"team.html":              "team",
		"404.html":               "custom 404",
	}, nil)
	if out["url"] != "https://shop--acme.sites.test/" || out["version"].(float64) != 1 {
		t.Fatalf("answer %v", out)
	}

	r := e.get("shop--acme.sites.test", "/", nil)
	if r.Code != 200 || r.Body.String() != "<h1>v1</h1>" {
		t.Fatalf("index: %d %q", r.Code, r.Body)
	}
	h := r.Header()
	if h.Get("X-Robots-Tag") != "noindex, nofollow" || h.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers %v", h)
	}
	if h.Get("ETag") != `"`+sha("<h1>v1</h1>")+`"` || !strings.HasPrefix(h.Get("Content-Type"), "text/html") {
		t.Errorf("etag/type %v", h)
	}
	if h.Get("Cache-Control") != "public, max-age=0, must-revalidate" {
		t.Errorf("html cache %q", h.Get("Cache-Control"))
	}
	if h.Get("Content-Security-Policy") != "" {
		t.Errorf("host mode must not sandbox: %q", h.Get("Content-Security-Policy"))
	}

	r = e.get("shop--acme.sites.test", "/assets/app-3f9a1c2b.js", nil)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || !strings.Contains(r.Header().Get("Content-Type"), "javascript") {
		t.Errorf("asset: %d %v", r.Code, r.Header())
	}
	// Conditional request by hash.
	r = e.get("shop--acme.sites.test", "/", map[string]string{"If-None-Match": `"` + sha("<h1>v1</h1>") + `"`})
	if r.Code != 304 {
		t.Errorf("etag revalidation: %d", r.Code)
	}
	if r = e.get("shop--acme.sites.test", "/about", nil); r.Code != 301 || r.Header().Get("Location") != "/about/" {
		t.Errorf("folder without slash: %d %v", r.Code, r.Header())
	}
	if r = e.get("shop--acme.sites.test", "/about/", nil); r.Code != 200 || r.Body.String() != "about" {
		t.Errorf("folder index: %d %q", r.Code, r.Body)
	}
	if r = e.get("shop--acme.sites.test", "/team", nil); r.Code != 200 || r.Body.String() != "team" {
		t.Errorf("clean url: %d %q", r.Code, r.Body)
	}
	if r = e.get("shop--acme.sites.test", "/nope", nil); r.Code != 404 || r.Body.String() != "custom 404" {
		t.Errorf("custom 404: %d %q", r.Code, r.Body)
	}
	if r = e.get("shop--acme.sites.test", "/../../etc/passwd", nil); r.Code != 404 {
		t.Errorf("traversal: %d", r.Code)
	}

	// Pinned version.
	r = e.get("shop--v1--acme.sites.test", "/", nil)
	if r.Code != 200 || r.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("pinned: %d %v", r.Code, r.Header())
	}
	if r = e.get("shop--v9--acme.sites.test", "/", nil); r.Code != 404 {
		t.Errorf("missing version: %d", r.Code)
	}
	if r = e.get("shop--pr-1--acme.sites.test", "/", nil); r.Code != 404 {
		t.Errorf("preview is reserved: %d", r.Code)
	}
}

func TestBareDomainAndUnknownSite(t *testing.T) {
	e := newEnv(t, nil)
	for _, host := range []string{"sites.test", "nosuch--acme.sites.test", "garbage.sites.test"} {
		r := e.get(host, "/", nil)
		if r.Code != 404 || !strings.Contains(r.Body.String(), "Пожаловаться на сайт") || !strings.Contains(r.Body.String(), "https://hub.test/abuse") {
			t.Errorf("%s: %d %q", host, r.Code, r.Body)
		}
	}
	e.newSite("acme", "empty", "link")
	if r := e.get("empty--acme.sites.test", "/", nil); r.Code != 404 || !strings.Contains(r.Body.String(), "пока пуст") {
		t.Errorf("empty site: %d %q", r.Code, r.Body)
	}
}

func TestSPAFallback(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "webapp", "link")
	e.deploy("acme", "webapp", map[string]string{"index.html": "shell", "main.js": "js"}, nil)
	if r := e.get("webapp--acme.sites.test", "/dashboard/x", nil); r.Code != 404 {
		t.Fatalf("no fallback by default: %d", r.Code)
	}
	if r := e.call("PATCH", "/api/sites/acme/webapp", map[string]any{"spa": true}, nil); r.Code != 200 {
		t.Fatalf("patch: %d %s", r.Code, r.Body)
	}
	*e.now = e.now.Add(3 * time.Second) // the site cache lives 2 s
	if r := e.get("webapp--acme.sites.test", "/dashboard/x", nil); r.Code != 200 || r.Body.String() != "shell" {
		t.Fatalf("fallback: %d %q", r.Code, r.Body)
	}
	if r := e.get("webapp--acme.sites.test", "/missing.js", nil); r.Code != 404 {
		t.Fatalf("a missing file with an extension is a 404 even with the fallback: %d", r.Code)
	}
}

func TestDedupAndRollback(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "one", "big.js": "same"}, nil)
	out := e.deploy("acme", "shop", map[string]string{"index.html": "two", "big.js": "same"}, nil)
	if out["missingAtBegin"].(int) != 1 {
		t.Fatalf("only the changed file is uploaded again, got %v", out["missingAtBegin"])
	}
	*e.now = e.now.Add(3 * time.Second)
	if r := e.get("shop--acme.sites.test", "/", nil); r.Body.String() != "two" {
		t.Fatalf("v2 current: %q", r.Body)
	}
	r := e.call("POST", "/api/sites/acme/shop/current", map[string]any{"version": 1}, nil)
	if r.Code != 200 {
		t.Fatalf("rollback: %d %s", r.Code, r.Body)
	}
	// Rolled back in this process at once, without waiting for any cache.
	if r := e.get("shop--acme.sites.test", "/", nil); r.Body.String() != "one" {
		t.Fatalf("v1 current: %q", r.Body)
	}
	if r := e.get("shop--v2--acme.sites.test", "/", nil); r.Body.String() != "two" {
		t.Fatalf("v2 still reachable pinned: %q", r.Body)
	}
	if r := e.call("POST", "/api/sites/acme/shop/current", map[string]any{"version": 7}, nil); r.Code != 404 {
		t.Fatalf("unknown version: %d", r.Code)
	}
	var site SiteView
	e.call("GET", "/api/sites/acme/shop", nil, nil).json(&site)
	if site.CurrentVersion != 1 || len(site.Versions) != 2 || site.Versions[0].Version != 2 || !site.Versions[1].Current {
		t.Fatalf("site view %+v", site)
	}
}

func TestCommitWithoutMakingCurrent(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "live"}, nil)
	m := map[string]File{"index.html": {Hash: sha("draft"), Size: 5}}
	var b struct {
		ID string `json:"id"`
	}
	e.call("POST", "/api/sites/acme/shop/deploys", map[string]any{"files": m}, nil).json(&b)
	e.call("PUT", "/api/sites/acme/shop/deploys/"+b.ID+"/blobs/"+sha("draft"), "draft", nil)
	r := e.call("POST", "/api/sites/acme/shop/deploys/"+b.ID+"/commit", map[string]any{"current": false}, nil)
	if r.Code != 201 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	*e.now = e.now.Add(3 * time.Second)
	if r := e.get("shop--acme.sites.test", "/", nil); r.Body.String() != "live" {
		t.Fatalf("current unchanged: %q", r.Body)
	}
	if r := e.get("shop--v2--acme.sites.test", "/", nil); r.Body.String() != "draft" {
		t.Fatalf("draft pinned: %q", r.Body)
	}
}

func TestUploadChecks(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	base := "/api/sites/acme/shop"

	begin := func(files map[string]File, extra map[string]any) resp {
		body := map[string]any{"files": files}
		for k, v := range extra {
			body[k] = v
		}
		return e.call("POST", base+"/deploys", body, nil)
	}
	if r := begin(map[string]File{"../x": {Hash: sha("a"), Size: 1}}, nil); r.Code != 400 {
		t.Errorf("path escape: %d", r.Code)
	}
	if r := begin(map[string]File{}, nil); r.Code != 400 {
		t.Errorf("empty: %d", r.Code)
	}
	if r := begin(map[string]File{"a": {Hash: sha("a"), Size: DefaultLimits.MaxFileBytes + 1}}, nil); r.Code != 400 {
		t.Errorf("big file: %d", r.Code)
	}

	// A file named like a secret is refused with the list, and an explicit allow lets it through.
	r := begin(map[string]File{"index.html": {Hash: sha("a"), Size: 1}, ".env": {Hash: sha("b"), Size: 1}}, nil)
	if r.Code != 422 || !strings.Contains(r.Body.String(), `"path":".env"`) || !strings.Contains(r.Body.String(), "secrets_found") {
		t.Errorf("secret name: %d %s", r.Code, r.Body)
	}
	if r = begin(map[string]File{".env": {Hash: sha("b"), Size: 1}}, map[string]any{"allowSecrets": true}); r.Code != 201 {
		t.Errorf("allowed: %d %s", r.Code, r.Body)
	}

	// A secret inside a file is found when the file comes in, and the answer names the path, not the secret.
	leak := "key = '" + "AKIA" + "IOSFODNN7EXAMPLE'"
	var b struct {
		ID string `json:"id"`
	}
	begin(map[string]File{"config.js": {Hash: sha(leak), Size: int64(len(leak))}}, nil).json(&b)
	r = e.call("PUT", base+"/deploys/"+b.ID+"/blobs/"+sha(leak), leak, nil)
	if r.Code != 422 || !strings.Contains(r.Body.String(), "config.js") || strings.Contains(r.Body.String(), "IOSFODNN7") {
		t.Errorf("secret content: %d %s", r.Code, r.Body)
	}

	// Content that is not the announced hash.
	begin(map[string]File{"a.txt": {Hash: sha("right"), Size: 5}}, nil).json(&b)
	if r = e.call("PUT", base+"/deploys/"+b.ID+"/blobs/"+sha("right"), "wrong", nil); r.Code != 400 {
		t.Errorf("hash mismatch: %d %s", r.Code, r.Body)
	}
	// A hash that is not in the manifest.
	if r = e.call("PUT", base+"/deploys/"+b.ID+"/blobs/"+sha("other"), "other", nil); r.Code != 400 {
		t.Errorf("foreign hash: %d", r.Code)
	}
	// Commit before the blobs are in.
	if r = e.call("POST", base+"/deploys/"+b.ID+"/commit", map[string]any{}, nil); r.Code != 409 || !strings.Contains(r.Body.String(), "blobs_missing") {
		t.Errorf("early commit: %d %s", r.Code, r.Body)
	}
	if r = e.call("POST", base+"/deploys/nosuch/commit", map[string]any{}, nil); r.Code != 404 {
		t.Errorf("unknown deploy: %d", r.Code)
	}
	// An expired upload.
	*e.now = e.now.Add(3 * time.Hour)
	if r = e.call("POST", base+"/deploys/"+b.ID+"/commit", map[string]any{}, nil); r.Code != 410 {
		t.Errorf("expired: %d", r.Code)
	}
}

func TestSiteNamesAndLimits(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Limits = Limits{MaxDeployBytes: 1 << 20, MaxFileBytes: 1 << 20, MaxFiles: 10, MaxSitesPerSpace: 2, KeepVersions: 2}
	})
	for _, n := range []string{"www", "A", "a--b", "x_y", ""} {
		if r := e.call("POST", "/api/sites/acme", map[string]any{"name": n}, nil); r.Code != 400 {
			t.Errorf("%q: %d", n, r.Code)
		}
	}
	if r := e.call("POST", "/api/sites/a--b", map[string]any{"name": "ok"}, nil); r.Code != 400 {
		t.Errorf("space with --: %d", r.Code)
	}
	e.newSite("acme", "one", "")
	if r := e.call("POST", "/api/sites/acme", map[string]any{"name": "one"}, nil); r.Code != 409 {
		t.Errorf("taken: %d", r.Code)
	}
	e.newSite("acme", "two", "")
	if r := e.call("POST", "/api/sites/acme", map[string]any{"name": "three"}, nil); r.Code != 409 || !strings.Contains(r.Body.String(), "too_many_sites") {
		t.Errorf("limit: %d %s", r.Code, r.Body)
	}
	var s SiteView
	e.call("GET", "/api/sites/acme/one", nil, nil).json(&s)
	if s.Access != AccessTeam {
		t.Errorf("the default access is the team: %q", s.Access)
	}
	// Only KeepVersions stay; the current one is never dropped.
	for i := 0; i < 4; i++ {
		e.deploy("acme", "one", map[string]string{"index.html": strings.Repeat("v", i+1)}, nil)
	}
	e.call("GET", "/api/sites/acme/one", nil, nil).json(&s)
	if len(s.Versions) != 2 || s.Versions[0].Version != 4 || s.Versions[1].Version != 3 {
		t.Errorf("kept versions: %+v", s.Versions)
	}
}

func TestDeployTokens(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.newSite("acme", "other", "link")
	var tk TokenView
	r := e.call("POST", "/api/sites/acme/shop/tokens", map[string]any{"name": "gitlab-ci"}, nil)
	if r.Code != 201 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	r.json(&tk)
	if !strings.HasPrefix(tk.Token, "hsite_") {
		t.Fatalf("token %q", tk.Token)
	}
	var list struct{ Tokens []TokenView }
	e.call("GET", "/api/sites/acme/shop/tokens", nil, nil).json(&list)
	if len(list.Tokens) != 1 || list.Tokens[0].Token != "" {
		t.Fatalf("the secret is shown once: %+v", list)
	}

	as := map[string]string{"Authorization": "Bearer " + tk.Token}
	e.deploy("acme", "shop", map[string]string{"index.html": "by token"}, as)
	if r := e.get("shop--acme.sites.test", "/", nil); r.Body.String() != "by token" {
		t.Fatalf("served: %q", r.Body)
	}
	var s SiteView
	e.call("GET", "/api/sites/acme/shop", nil, as).json(&s)
	if s.UpdatedBy != "токен «gitlab-ci»" {
		t.Errorf("updatedBy %q", s.UpdatedBy)
	}

	// The token is for one site.
	if r := e.call("POST", "/api/sites/acme/other/deploys", map[string]any{"files": map[string]File{"a": {Hash: sha("a"), Size: 1}}}, as); r.Code != 401 {
		t.Errorf("another site: %d", r.Code)
	}
	// It does not manage: no tokens, no settings, no delete, no creation.
	for _, c := range [][2]string{{"POST", "/api/sites/acme/shop/tokens"}, {"PATCH", "/api/sites/acme/shop"}, {"DELETE", "/api/sites/acme/shop"}, {"POST", "/api/sites/acme"}} {
		if r := e.call(c[0], c[1], map[string]any{"name": "x"}, as); r.Code != 403 {
			t.Errorf("%v: %d", c, r.Code)
		}
	}
	// The CLI asks which site a token is for.
	var info map[string]string
	e.call("GET", "/api/sites-token", nil, as).json(&info)
	if info["space"] != "acme" || info["site"] != "shop" {
		t.Errorf("token info %v", info)
	}

	if r := e.call("DELETE", "/api/sites/acme/shop/tokens/"+tk.ID, nil, nil); r.Code != 204 {
		t.Fatalf("revoke: %d", r.Code)
	}
	if r := e.call("GET", "/api/sites/acme/shop", nil, as); r.Code != 401 {
		t.Errorf("revoked token: %d", r.Code)
	}
	// No credentials at all.
	if r := e.call("GET", "/api/sites/acme", nil, map[string]string{"Authorization": ""}); r.Code != 401 {
		t.Errorf("anonymous: %d", r.Code)
	}
	if r := e.call("GET", "/api/sites/acme", nil, map[string]string{"Authorization": "Bearer wrong"}); r.Code != 401 {
		t.Errorf("wrong admin token: %d", r.Code)
	}
}

func TestTeamAccessWithTickets(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "secret", "team")
	e.deploy("acme", "secret", map[string]string{"index.html": "private page"}, nil)
	host := "secret--acme.sites.test"

	// Anonymous: sent to the gateway, told where to come back to.
	r := e.get(host, "/page?x=1", nil)
	if r.Code != 302 {
		t.Fatalf("anonymous: %d %q", r.Code, r.Body)
	}
	loc, _ := url.Parse(r.Header().Get("Location"))
	if loc.Host != "gw.test" || loc.Query().Get("host") != host || loc.Query().Get("return") != "https://"+host+"/page?x=1" {
		t.Fatalf("entry: %s", loc)
	}
	// An XHR gets a 401, not a redirect to an HTML page.
	if r := e.get(host, "/", map[string]string{"Accept": "application/json"}); r.Code != 401 {
		t.Errorf("api-like request: %d", r.Code)
	}
	// No leak of the content, and nothing cacheable.
	if strings.Contains(e.get(host, "/", nil).Body.String(), "private page") {
		t.Fatal("content leaked")
	}

	// A ticket for this host becomes a host-only cookie and the ticket leaves the address.
	tk := e.auth.IssueTicket("user-1", host)
	r = e.get(host, "/?hl_site_ticket="+tk+"&keep=1", nil)
	if r.Code != 302 || r.Header().Get("Location") != "/?keep=1" {
		t.Fatalf("redeem: %d %v", r.Code, r.Header())
	}
	cs := r.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != "__Host-hl_site" || cs[0].Domain != "" {
		t.Fatalf("cookie %v", cs)
	}
	cookie := cs[0].Name + "=" + cs[0].Value
	r = e.get(host, "/", map[string]string{"Cookie": cookie})
	if r.Code != 200 || r.Body.String() != "private page" || r.Header().Get("Cache-Control") != "private, max-age=0, must-revalidate" {
		t.Fatalf("with cookie: %d %q %v", r.Code, r.Body, r.Header())
	}
	// The ticket is spent.
	if r := e.get(host, "/?hl_site_ticket="+tk, nil); r.Code != 302 || !strings.HasPrefix(r.Header().Get("Location"), "https://gw.test/") {
		t.Errorf("replay: %d %v", r.Code, r.Header().Get("Location"))
	}
	// A ticket made for another site does not open this one.
	other := e.auth.IssueTicket("user-1", "other--acme.sites.test")
	if r := e.get(host, "/?hl_site_ticket="+other, nil); r.Code != 302 || !strings.HasPrefix(r.Header().Get("Location"), "https://gw.test/") {
		t.Errorf("wrong host ticket: %d", r.Code)
	}
	// The cookie of this host is worth nothing on another team site.
	e.newSite("acme", "second", "team")
	e.deploy("acme", "second", map[string]string{"index.html": "second"}, nil)
	if r := e.get("second--acme.sites.test", "/", map[string]string{"Cookie": cookie}); r.Code != 302 {
		t.Errorf("cookie reused on another host: %d", r.Code)
	}
	// Session expires after 12 hours.
	*e.now = e.now.Add(13 * time.Hour)
	if r := e.get(host, "/", map[string]string{"Cookie": cookie}); r.Code != 302 {
		t.Errorf("expired session: %d", r.Code)
	}
	// Making the site open to anyone with the link needs no sign-in.
	e.call("PATCH", "/api/sites/acme/secret", map[string]any{"access": "link"}, nil)
	*e.now = e.now.Add(3 * time.Second)
	if r := e.get(host, "/", nil); r.Code != 200 {
		t.Errorf("link access: %d", r.Code)
	}
	if r := e.call("PATCH", "/api/sites/acme/secret", map[string]any{"access": "everyone"}, nil); r.Code != 400 {
		t.Errorf("unknown access: %d", r.Code)
	}
}

func TestRequestSanitizing(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.StripHeaderPrefixes = []string{"x-gw-", "x-preview-"} })
	req := httptest.NewRequest("GET", "https://x--y.sites.test/", nil)
	req.Header.Set("Cookie", "session=abc; __Host-hl_site=mine; other=1")
	req.Header.Set("X-Gw-User", "admin")
	req.Header.Set("x-preview-bypass", "1")
	req.Header.Set("X-Keep", "yes")
	e.svc.sanitize(req)
	if req.Header.Get("Cookie") != "__Host-hl_site=mine" || req.Header.Get("X-Gw-User") != "" || req.Header.Get("X-Preview-Bypass") != "" || req.Header.Get("X-Keep") != "yes" {
		t.Fatalf("headers after sanitizing: %v", req.Header)
	}
	req.Header.Set("Cookie", "a=b")
	e.svc.sanitize(req)
	if _, ok := req.Header["Cookie"]; ok {
		t.Fatal("foreign cookies are dropped")
	}
}

func makeZip(t *testing.T, files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(w, body)
	}
	zw.Close()
	return buf.Bytes()
}

func TestZipUpload(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "landing", "link")
	hdr := map[string]string{"Content-Type": "application/zip"}

	// A folder zipped from outside: the site is what is inside.
	z := makeZip(t, map[string]string{"dist/index.html": "landing", "dist/css/a.css": "body{}", "__MACOSX/dist/._index.html": "x", "dist/.DS_Store": "x"})
	r := e.call("POST", "/api/sites/acme/landing/upload", z, hdr)
	if r.Code != 201 {
		t.Fatalf("%d %s", r.Code, r.Body)
	}
	if g := e.get("landing--acme.sites.test", "/", nil); g.Body.String() != "landing" {
		t.Fatalf("served: %q", g.Body)
	}
	if g := e.get("landing--acme.sites.test", "/css/a.css", nil); g.Code != 200 {
		t.Fatalf("nested: %d", g.Code)
	}
	var s SiteView
	e.call("GET", "/api/sites/acme/landing", nil, nil).json(&s)
	if s.Versions[0].FileCount != 2 || s.Versions[0].Source != "zip" {
		t.Fatalf("version %+v", s.Versions[0])
	}

	// Zip-slip.
	z = makeZip(t, map[string]string{"../evil.html": "x", "index.html": "x"})
	if r := e.call("POST", "/api/sites/acme/landing/upload", z, hdr); r.Code != 400 {
		t.Errorf("zip-slip: %d %s", r.Code, r.Body)
	}
	// Not a zip.
	if r := e.call("POST", "/api/sites/acme/landing/upload", "plain text", hdr); r.Code != 400 {
		t.Errorf("not a zip: %d", r.Code)
	}
	// Secrets.
	z = makeZip(t, map[string]string{"index.html": "x", ".env": "A=1"})
	if r := e.call("POST", "/api/sites/acme/landing/upload", z, hdr); r.Code != 422 || !strings.Contains(r.Body.String(), ".env") {
		t.Errorf("secret in zip: %d %s", r.Code, r.Body)
	}
	if r := e.call("POST", "/api/sites/acme/landing/upload?allowSecrets=1", z, hdr); r.Code != 201 {
		t.Errorf("allowed: %d %s", r.Code, r.Body)
	}
}

func TestZipBombIsStopped(t *testing.T) {
	e := newEnv(t, func(c *Config) {
		c.Limits = Limits{MaxDeployBytes: 1 << 20, MaxFileBytes: 64 << 10, MaxFiles: 10, MaxSitesPerSpace: 5, KeepVersions: 5}
	})
	e.newSite("acme", "bomb", "link")
	big := strings.Repeat("A", 200<<10) // compresses to almost nothing, expands past the file limit
	z := makeZip(t, map[string]string{"index.html": "ok", "big.bin": big})
	if r := e.call("POST", "/api/sites/acme/bomb/upload", z, map[string]string{"Content-Type": "application/zip"}); r.Code != 400 {
		t.Fatalf("bomb: %d %s", r.Code, r.Body)
	}
}

func TestSweepRemovesOnlyUnreferencedOldBlobs(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "keep me"}, nil)
	e.deploy("acme", "shop", map[string]string{"index.html": "newer"}, nil) // v1 stays as a version

	// An upload that never got committed: a blob nothing refers to.
	orphan := "orphan content"
	if err := e.blobs.Put(ctx, sha(orphan), strings.NewReader(orphan), int64(len(orphan))); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.repo.addBlob(ctx, sha(orphan), int64(len(orphan))); err != nil {
		t.Fatal(err)
	}
	n, err := e.svc.Sweep(ctx)
	if err != nil || n != 0 {
		t.Fatalf("a young blob may be an upload in flight: %d %v", n, err)
	}
	*e.now = e.now.Add(2 * time.Hour)
	n, err = e.svc.Sweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep: %d %v", n, err)
	}
	if ok, _ := e.blobs.Has(ctx, sha(orphan)); ok {
		t.Error("the orphan is still there")
	}
	for _, c := range []string{"keep me", "newer"} {
		if ok, _ := e.blobs.Has(ctx, sha(c)); !ok {
			t.Errorf("%q was referenced and must stay", c)
		}
	}
	// Deleting the site frees its blobs at the next sweep.
	if r := e.call("DELETE", "/api/sites/acme/shop", nil, nil); r.Code != 204 {
		t.Fatalf("delete: %d", r.Code)
	}
	if r := e.get("shop--acme.sites.test", "/", nil); r.Code != 404 {
		t.Errorf("deleted site: %d", r.Code)
	}
	n, _ = e.svc.Sweep(ctx)
	if n != 2 {
		t.Errorf("blobs of the deleted site: %d", n)
	}
}

func TestPathFallback(t *testing.T) {
	e := newEnv(t, func(c *Config) { c.Domain = "" })
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "<script>1</script>", "a/b.css": "x"}, nil)
	var s SiteView
	e.call("GET", "/api/sites/acme/shop", nil, nil).json(&s)
	if s.Address != "https://hub.test/sites/shop--acme/" {
		t.Fatalf("address %q", s.Address)
	}
	h := e.svc.PathEdge()
	get := func(p string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://hub.test"+p, nil))
		return w
	}
	w := get("/sites/shop--acme/")
	if w.Code != 200 || w.Body.String() != "<script>1</script>" {
		t.Fatalf("%d %q", w.Code, w.Body)
	}
	csp := w.Header().Get("Content-Security-Policy")
	if csp != "sandbox allow-scripts allow-forms allow-popups allow-modals" || strings.Contains(csp, "allow-same-origin") {
		t.Fatalf("csp %q", csp)
	}
	if w = get("/sites/shop--acme/a/b.css"); w.Code != 200 || w.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("asset %d", w.Code)
	}
	if w = get("/sites/shop--acme"); w.Code != 301 || w.Header().Get("Location") != "/sites/shop--acme/" {
		t.Errorf("no slash: %d %v", w.Code, w.Header())
	}
	if w = get("/sites/nosuch--acme/"); w.Code != 404 {
		t.Errorf("unknown: %d", w.Code)
	}
	if w = get("/sites/garbage/"); w.Code != 404 {
		t.Errorf("garbage: %d", w.Code)
	}
	if e.svc.Subdomains() {
		t.Error("no domain, no subdomains")
	}
}

// Redirects are built from the cleaned path: a path of slashes never names another host.
func TestRedirectsStayOnTheHost(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "home", "docs/index.html": "docs"}, nil)
	for _, p := range []string{"//docs", "///docs", "/\\docs", "//evil.example/../docs"} {
		r := e.get("shop--acme.sites.test", p, nil)
		if r.Code < 300 || r.Code > 399 {
			continue // served or refused, never redirected: fine
		}
		loc := r.Header().Get("Location")
		if !strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "//") || strings.HasPrefix(loc, "/\\") {
			t.Errorf("%q redirects to %q", p, loc)
		}
	}
	if got := localURI("//evil.example/x", "a=1"); got != "/evil.example/x?a=1" {
		t.Errorf("localURI: %q", got)
	}
	if got := localURI("/\\evil.example", ""); strings.HasPrefix(got, "//") || strings.Contains(got, "\\") {
		t.Errorf("localURI backslash: %q", got)
	}
	// The viewer-entry return path of a private site never begins with two slashes either.
	e.newSite("acme", "inner", "team")
	e.deploy("acme", "inner", map[string]string{"index.html": "x"}, nil)
	req := httptest.NewRequest("GET", "https://inner--acme.sites.test//evil.example", nil)
	req.Header.Set("Accept", "text/html")
	w := httptest.NewRecorder()
	e.edge.ServeHTTP(w, req)
	if loc := w.Header().Get("Location"); strings.Contains(loc, "return=%2F%2F") || strings.Contains(loc, "return=//") {
		t.Errorf("the entry carries a return of two slashes: %q", loc)
	}
}

// What another space uploaded proves nothing: only a hash linked to this space can be left out of the upload.
func TestBlobsBelongToTheirSpace(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("alpha", "a", "link")
	e.newSite("beta", "b", "link")
	e.deploy("alpha", "a", map[string]string{"index.html": "alpha-only"}, nil)

	files := map[string]File{"index.html": {Hash: sha("alpha-only"), Size: int64(len("alpha-only"))}}
	r := e.call("POST", "/api/sites/beta/b/deploys", map[string]any{"files": files}, nil)
	if r.Code != 201 {
		t.Fatalf("begin: %d %s", r.Code, r.Body)
	}
	var b struct {
		ID      string   `json:"id"`
		Missing []string `json:"missing"`
	}
	r.json(&b)
	if len(b.Missing) != 1 {
		t.Fatalf("another space's blob counted as held: %+v", b)
	}
	if r := e.call("POST", "/api/sites/beta/b/deploys/"+b.ID+"/commit", map[string]any{}, nil); r.Code != 409 {
		t.Fatalf("commit without the bytes: %d %s", r.Code, r.Body)
	}
	// Bytes that do not match the hash are refused, and so is a blob of another size in the manifest.
	if r := e.call("PUT", "/api/sites/beta/b/deploys/"+b.ID+"/blobs/"+sha("alpha-only"), "other bytes", nil); r.Code == 204 {
		t.Fatalf("a blob of other bytes was taken")
	}
	// The same space can leave it out.
	e.deploy("alpha", "a", map[string]string{"index.html": "alpha-only", "x.txt": "x"}, nil)
	files = map[string]File{"index.html": {Hash: sha("alpha-only"), Size: int64(len("alpha-only"))}}
	var again struct {
		Missing []string `json:"missing"`
	}
	e.call("POST", "/api/sites/alpha/a/deploys", map[string]any{"files": files}, nil).json(&again)
	if len(again.Missing) != 0 {
		t.Fatalf("own blob asked for again: %+v", again)
	}
}

// The manifest's size must be the blob's size when the version is committed.
func TestCommitChecksSizes(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "abc"}, nil)
	files := map[string]File{"index.html": {Hash: sha("abc"), Size: 999}}
	r := e.call("POST", "/api/sites/acme/shop/deploys", map[string]any{"files": files}, nil)
	if r.Code != 201 {
		// The size is only known to be wrong once the bytes are held: begin must accept and commit must decide.
		t.Fatalf("begin: %d %s", r.Code, r.Body)
	}
	var b struct {
		ID      string   `json:"id"`
		Missing []string `json:"missing"`
	}
	r.json(&b)
	if len(b.Missing) != 0 {
		t.Fatalf("the blob is held by the space, nothing should be missing: %+v", b)
	}
	c := e.call("POST", "/api/sites/acme/shop/deploys/"+b.ID+"/commit", map[string]any{}, nil)
	if c.Code < 400 || c.Code >= 500 {
		t.Fatalf("a wrong size must be refused at commit with a client error, got %d %s", c.Code, c.Body)
	}
	var site SiteView
	e.call("GET", "/api/sites/acme/shop", nil, nil).json(&site)
	if site.CurrentVersion != 1 || len(site.Versions) != 1 {
		t.Fatalf("a refused commit left a version behind: %+v", site)
	}
}

// On-demand certificates are for versions that exist, and for no preview.
func TestCertificatesOnlyForExistingVersions(t *testing.T) {
	e := newEnv(t, nil)
	e.newSite("acme", "shop", "link")
	e.deploy("acme", "shop", map[string]string{"index.html": "one"}, nil)
	ctx := context.Background()
	for host, want := range map[string]bool{
		"shop--acme.sites.test":       true,
		"shop--v1--acme.sites.test":   true,
		"shop--v9--acme.sites.test":   false,
		"shop--pr-3--acme.sites.test": false,
		"nothing--acme.sites.test":    false,
		"shop--v1--other.sites.test":  false,
		"sites.test":                  true,
		"shop--acme.elsewhere.test":   false,
	} {
		if got := e.svc.Exists(ctx, host); got != want {
			t.Errorf("Exists(%s) = %v, want %v", host, got, want)
		}
	}
}
