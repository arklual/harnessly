package apps

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func guestRig(t *testing.T) (*Guests, *Registry, *time.Time) {
	t.Helper()
	reg, _ := installedPaneApp(t, nil)
	now := time.UnixMilli(1_800_000_000_000)
	g := NewGuests(reg, &Storage{Root: filepath.Join(t.TempDir(), "storage")})
	g.Now = func() time.Time { return now }
	return g, reg, &now
}

func guestDo(g *Guests, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	return w
}

func TestGuestOpensTheViewAndNothingElse(t *testing.T) {
	g, _, _ := guestRig(t)
	token, link, err := g.Create("acme.demo", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	base := GuestPrefix + token + "/"

	page := guestDo(g, http.MethodGet, base, "")
	if page.Code != 200 || !strings.Contains(page.Body.String(), `sandbox="allow-scripts allow-forms allow-popups"`) {
		t.Fatalf("page: %d %s", page.Code, page.Body.String())
	}
	body := page.Body.String()
	if strings.Contains(body, token) {
		t.Fatal("the token is in the page: a view could read it")
	}
	if !strings.Contains(body, `src="acme.demo/ui/index.html"`) || !strings.Contains(body, `&#34;nonce&#34;:`) {
		t.Fatalf("page: %s", body)
	}
	if csp := page.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Fatalf("shell CSP: %s", csp)
	}
	// The slashless address goes to the slashed one: the page's relative links need it.
	if w := guestDo(g, http.MethodGet, GuestPrefix+token, ""); w.Code != http.StatusFound {
		t.Fatalf("redirect: %d", w.Code)
	}

	// The view's page carries the nonce the host expects, under the app's CSP.
	view := guestDo(g, http.MethodGet, base+"acme.demo/ui/index.html", "")
	if view.Code != 200 || !strings.Contains(view.Body.String(), `name="hl-nonce" content="`+guestNonce(token)+`"`) {
		t.Fatalf("view: %d %s", view.Code, view.Body.String())
	}
	if csp := view.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox allow-scripts") || !strings.Contains(csp, "connect-src 'none'") {
		t.Fatalf("view CSP: %s", csp)
	}
	// Only a view's files: not the manifest, not the agent's instructions, not another app.
	for _, p := range []string{"acme.demo/harnessly/app.json", "acme.demo/instructions.md", "acme.demo/secret.txt", "other.app/ui/index.html", "acme.demo/../x"} {
		if w := guestDo(g, http.MethodGet, base+p, ""); w.Code != http.StatusNotFound {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	// The SDK and the shim the view loads by their absolute paths are public.
	for _, p := range []string{SDKPath, ShimPath} {
		if w := guestDo(g, http.MethodGet, p, ""); w.Code != 200 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if !GuestMatch(httptest.NewRequest("GET", base, nil)) || !GuestMatch(httptest.NewRequest("GET", SDKPath, nil)) ||
		GuestMatch(httptest.NewRequest("GET", "/api/cli/apps", nil)) {
		t.Fatal("GuestMatch")
	}
	if link.ID == "" || !strings.HasPrefix(link.Hash, link.ID) {
		t.Fatalf("link: %+v", link)
	}
}

func TestGuestLinkEndsAtItsTimeAndWhenRevoked(t *testing.T) {
	g, reg, now := guestRig(t)
	token, link, err := g.Create("acme.demo", "main", 24*time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	base := GuestPrefix + token + "/"
	if w := guestDo(g, http.MethodGet, base, ""); w.Code != 200 {
		t.Fatalf("live: %d", w.Code)
	}
	if w := guestDo(g, http.MethodGet, GuestPrefix+"nope/", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown token: %d", w.Code)
	}
	*now = now.Add(25 * time.Hour)
	if w := guestDo(g, http.MethodGet, base, ""); w.Code != http.StatusNotFound {
		t.Fatalf("expired: %d", w.Code)
	}

	*now = now.Add(-25 * time.Hour)
	if err := g.Revoke(link.ID); err != nil {
		t.Fatal(err)
	}
	if w := guestDo(g, http.MethodGet, base, ""); w.Code != http.StatusNotFound {
		t.Fatalf("revoked: %d", w.Code)
	}

	// Uninstalling the app ends its links; a very long ttl is cut to the longest.
	token2, link2, _ := g.Create("acme.demo", "", 365*24*time.Hour, false)
	if got := time.UnixMilli(link2.ExpiresAt).Sub(*now); got != MaxGuestTTL {
		t.Fatalf("ttl %v", got)
	}
	if err := reg.Remove("acme.demo"); err != nil {
		t.Fatal(err)
	}
	if w := guestDo(g, http.MethodGet, GuestPrefix+token2+"/", ""); w.Code != http.StatusNotFound {
		t.Fatalf("uninstalled: %d", w.Code)
	}
	if _, _, err := g.Resolve(token2); !errors.Is(err, ErrGuestGone) {
		t.Fatalf("resolve: %v", err)
	}
}

func TestOnlyAnAppViewIsShared(t *testing.T) {
	reg := newRegistry(t)
	dir := scaffold(t, "acme.chatty", "chat")
	if _, _, err := reg.Install(dir, InstallOptions{HostVersion: TemplateVersion}); err != nil {
		t.Fatal(err)
	}
	g := NewGuests(reg, &Storage{Root: t.TempDir()})
	if _, _, err := g.Create("acme.chatty", "", 0, false); !errors.Is(err, ErrGuestTarget) {
		t.Fatalf("a chat-only app: %v", err)
	}
	if _, _, err := g.Create("acme.none", "", 0, false); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("not installed: %v", err)
	}
	g2, _, _ := guestRig(t)
	if _, _, err := g2.Create("acme.demo", "ghost", 0, false); !errors.Is(err, ErrGuestTarget) {
		t.Fatalf("no such view: %v", err)
	}
}

func TestGuestStorageIsTheLinksOwnAndReadsOnly(t *testing.T) {
	g, _, _ := guestRig(t)
	ro, _, _ := g.Create("acme.demo", "", 0, false)
	rw, _, _ := g.Create("acme.demo", "", 0, true)
	call := func(token, body string) *httptest.ResponseRecorder {
		return guestDo(g, http.MethodPost, GuestPrefix+token+"/__guest/storage", body)
	}
	if w := call(ro, `{"op":"set","key":"a","value":1}`); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "permission_denied") {
		t.Fatalf("read-only link wrote: %d %s", w.Code, w.Body.String())
	}
	if w := call(rw, `{"op":"set","key":"a","value":{"n":1}}`); w.Code != 200 {
		t.Fatalf("write: %d %s", w.Code, w.Body.String())
	}
	if w := call(rw, `{"op":"get","key":"a"}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"n":1`) {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	if w := call(rw, `{"op":"keys"}`); !strings.Contains(w.Body.String(), `"a"`) {
		t.Fatalf("keys: %s", w.Body.String())
	}
	// Another link has its own area.
	if w := call(ro, `{"op":"get","key":"a"}`); w.Code != 200 || strings.Contains(w.Body.String(), `"n":1`) {
		t.Fatalf("another link read it: %s", w.Body.String())
	}
	// A request from another site is refused.
	req := httptest.NewRequest(http.MethodPost, GuestPrefix+rw+"/__guest/storage", strings.NewReader(`{"op":"get","key":"a"}`))
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("cross-site: %d", w.Code)
	}
	if w := call(rw, `{"op":"drop"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("unknown op: %d", w.Code)
	}
}

func TestGuestHooksTellOfLinksAndNeverTheToken(t *testing.T) {
	g, _, _ := guestRig(t)
	var made []GuestLink
	var ended []string
	g.OnCreate = func(l GuestLink) { made = append(made, l) }
	g.OnRevoke = func(id string) { ended = append(ended, id) }
	t1, l1, err := g.Create("acme.demo", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	_, l2, _ := g.Create("acme.demo", "", 0, false)
	if len(made) != 2 || made[0].Hash != hashToken(t1) || strings.Contains(made[0].Hash, t1) || made[0].ExpiresAt != l1.ExpiresAt {
		t.Fatalf("created: %+v", made)
	}
	if err := g.Revoke(l1.ID); err != nil || len(ended) != 1 || ended[0] != l1.ID {
		t.Fatalf("revoke: %v %v", err, ended)
	}
	// The app goes: the links still live end with it, the one already ended is not told twice.
	n, err := g.RevokeApp("acme.demo")
	if err != nil || n != 1 || len(ended) != 2 || ended[1] != l2.ID {
		t.Fatalf("app removed: %d %v %v", n, err, ended)
	}
	if n, _ := g.RevokeApp("acme.demo"); n != 0 || len(ended) != 2 {
		t.Fatalf("again: %d %v", n, ended)
	}
	if live, _ := g.List(""); len(live) != 0 {
		t.Fatalf("live: %+v", live)
	}
}

func TestGuestPageReachesTheSDKByRelativePaths(t *testing.T) {
	g, _, _ := guestRig(t)
	token, _, err := g.Create("acme.demo", "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	base := GuestPrefix + token + "/"
	view := guestDo(g, http.MethodGet, base+"acme.demo/ui/index.html", "")
	body := view.Body.String()
	// ui/index.html is two folders under the link: the SDK is three levels up.
	if !strings.Contains(body, `<script src="../../__hl/bridge-shim.js">`) || strings.Contains(body, `"`+ShimPath+`"`) {
		t.Fatalf("shim: %s", body)
	}
	for _, p := range []string{"__hl/app-sdk.js", "__hl/app-sdk.mjs", "__hl/bridge-shim.js"} {
		if w := guestDo(g, http.MethodGet, base+p, ""); w.Code != 200 || w.Body.Len() == 0 {
			t.Errorf("%s: %d", p, w.Code)
		}
	}
	if w := guestDo(g, http.MethodGet, base+"__hl/other.js", ""); w.Code != 404 {
		t.Errorf("unknown sdk file: %d", w.Code)
	}
	// A link that ended serves no SDK either.
	if w := guestDo(g, http.MethodGet, GuestPrefix+"nope/__hl/app-sdk.js", ""); w.Code != 404 {
		t.Errorf("unknown token: %d", w.Code)
	}
	// The old absolute paths still serve.
	if w := guestDo(g, http.MethodGet, ShimPath, ""); w.Code != 200 {
		t.Errorf("absolute: %d", w.Code)
	}
	got := relativeSDK([]byte(`<script src="/__hl/app-sdk.js"></script><link href='/__hl/x'>`), "ui/index.html", false)
	if string(got) != `<script src="../../__hl/app-sdk.js"></script><link href='../../__hl/x'>` {
		t.Errorf("html: %s", got)
	}
	js := relativeSDK([]byte(`import {a} from "/__hl/app-sdk.mjs"; fetch("/__hl/nope")`), "main.js", true)
	if string(js) != `import {a} from "../__hl/app-sdk.mjs"; fetch("/__hl/nope")` {
		t.Errorf("js: %s", js)
	}
}
