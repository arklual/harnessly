package apps

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A standalone app by link. An app with the `app` target shows its view full screen; the owner can hand that view to
// someone with no account and no machine of their own, by a link with an end (Guests.Create). What the guest opens is
// the view and nothing around it: no chat, no agent (guest AI is off), no tools; its storage is the link's own area,
// read-only unless the owner made the link writable; the view runs in the same sandbox as everywhere (opaque origin,
// the CSP of its network). The token is the link: only its hash is kept, a link ends at its time or when the owner
// revokes it, and uninstalling the app ends it too.
//
//	GET  /apps/guest/<token>/                      the page: the view in a frame, and the guest host script
//	GET  /apps/guest/<token>/<app id>/<file>       the view's files (the view server's layout)
//	POST /apps/guest/<token>/__guest/storage       the link's storage

//go:embed sdk/guest-host.js
var guestHostJS []byte

//go:embed sdk/guest-host.css
var guestHostCSS []byte

// Limits of a link's life.
const (
	DefaultGuestTTL = 7 * 24 * time.Hour
	MaxGuestTTL     = 30 * 24 * time.Hour
	maxGuestLinks   = 500
	maxGuestBody    = 256 << 10
)

// GuestPrefix is where the guest routes are.
const GuestPrefix = "/apps/guest/"

var (
	// ErrGuestTarget: the app or the view is not one that can be shared by link.
	ErrGuestTarget = errors.New("apps: only a view for the app target can be shared by link")
	// ErrGuestGone: no such link, or it ended.
	ErrGuestGone = errors.New("apps: the link is gone")
	// ErrGuestLimit: too many links.
	ErrGuestLimit = errors.New("apps: too many links; revoke some")
)

// GuestLink is one link, as it is kept (the token is not: only its hash).
type GuestLink struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
	App  string `json:"app"`
	View string `json:"view"`
	// Write lets the guest's view write to the link's storage; a link reads only by default.
	Write bool `json:"write,omitempty"`
	// Check marks a link `harnessly app check` made for the machine's own headless browser: any local view, a
	// couple of minutes, never listed, and the only kind whose page reports to the app's log.
	Check     bool  `json:"check,omitempty"`
	CreatedAt int64 `json:"createdAt"`
	ExpiresAt int64 `json:"expiresAt"`
	Revoked   bool  `json:"revoked,omitempty"`
}

// Live: the link has not ended at now.
func (l GuestLink) Live(now time.Time) bool { return !l.Revoked && now.UnixMilli() < l.ExpiresAt }

// Guests keeps the links of a machine.
type Guests struct {
	reg     *Registry
	storage *Storage
	// Now is the clock; nil is the real one.
	Now func() time.Time
	// OnCreate and OnRevoke are told, without blocking, of a link made and of a link ended (revoked, or its app
	// removed). The hash goes with a made link, never the token.
	OnCreate func(GuestLink)
	OnRevoke func(id string)

	mu sync.Mutex
}

// NewGuests is the links of a registry; the storage is where the links' areas are.
func NewGuests(reg *Registry, storage *Storage) *Guests { return &Guests{reg: reg, storage: storage} }

func (g *Guests) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func (g *Guests) file() string { return filepath.Join(g.reg.Root, "guest-links.json") }

func (g *Guests) read() ([]GuestLink, error) {
	raw, err := os.ReadFile(g.file())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var body struct {
		Links []GuestLink `json:"links"`
	}
	return body.Links, json.Unmarshal(raw, &body)
}

func (g *Guests) write(list []GuestLink) error {
	raw, err := json.MarshalIndent(map[string]any{"links": list}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(g.file(), append(raw, '\n'))
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// ShareableView is the view an app shares by link: the one named, or the first whose targets include app.
func (m *Manifest) ShareableView(view string) (View, bool) {
	for _, v := range m.Views {
		if (view == "" || v.ID == view) && contains(v.Targets, TargetApp) && v.Entry != "" && !strings.HasPrefix(v.Entry, "https://") {
			return v, true
		}
	}
	return View{}, false
}

// CheckTTL is how long a link of a check lives.
const CheckTTL = 2 * time.Minute

// ErrCheckRemote: the view lives at an address of its own, which a check on the machine does not open.
var ErrCheckRemote = errors.New("apps: the view is at an address of its own; a check opens only a view the package carries")

// checkView is the view a check opens: the one named, or the first the package carries.
func (m *Manifest) checkView(view string) (View, bool) {
	for _, v := range m.Views {
		if (view == "" || v.ID == view) && v.Entry != "" && !strings.HasPrefix(v.Entry, "https://") {
			return v, true
		}
	}
	return View{}, false
}

// viewOf is the view a link shows.
func (l GuestLink) viewOf(m *Manifest) (View, bool) {
	if l.Check {
		return m.checkView(l.View)
	}
	return m.ShareableView(l.View)
}

// CreateCheck makes a link of a check to an installed app's view (any local one); the token comes back once.
func (g *Guests) CreateCheck(appID, view string) (token string, link GuestLink, err error) {
	app, err := g.reg.Find(appID)
	if err != nil {
		return "", GuestLink{}, err
	}
	if app.Manifest == nil {
		return "", GuestLink{}, ErrGuestTarget
	}
	v, ok := app.Manifest.checkView(view)
	if !ok {
		return "", GuestLink{}, ErrCheckRemote
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", GuestLink{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	now := g.now()
	h := hashToken(token)
	link = GuestLink{ID: h[:16], Hash: h, App: appID, View: v.ID, Check: true, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(CheckTTL).UnixMilli()}
	g.mu.Lock()
	defer g.mu.Unlock()
	list, err := g.read()
	if err != nil {
		return "", GuestLink{}, err
	}
	live := list[:0:0]
	for _, l := range list {
		if l.Live(now) {
			live = append(live, l)
		}
	}
	return token, link, g.write(append(live, link))
}

// Create makes a link to an installed app's view; the token comes back once. A ttl of 0 is DefaultGuestTTL.
func (g *Guests) Create(appID, view string, ttl time.Duration, write bool) (token string, link GuestLink, err error) {
	app, err := g.reg.Get(appID)
	if err != nil {
		return "", GuestLink{}, err
	}
	if app.Manifest == nil || !app.Manifest.HasTarget(TargetApp) {
		return "", GuestLink{}, ErrGuestTarget
	}
	v, ok := app.Manifest.ShareableView(view)
	if !ok {
		return "", GuestLink{}, ErrGuestTarget
	}
	if ttl <= 0 {
		ttl = DefaultGuestTTL
	}
	if ttl > MaxGuestTTL {
		ttl = MaxGuestTTL
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", GuestLink{}, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	now := g.now()
	h := hashToken(token)
	link = GuestLink{ID: h[:16], Hash: h, App: appID, View: v.ID, Write: write, CreatedAt: now.UnixMilli(), ExpiresAt: now.Add(ttl).UnixMilli()}

	g.mu.Lock()
	defer g.mu.Unlock()
	list, err := g.read()
	if err != nil {
		return "", GuestLink{}, err
	}
	// Links that ended are dropped here, with their areas; the count is of the live ones.
	live := list[:0:0]
	for _, l := range list {
		if l.Live(now) {
			live = append(live, l)
		}
	}
	if len(live) >= maxGuestLinks {
		return "", GuestLink{}, ErrGuestLimit
	}
	if err := g.write(append(live, link)); err != nil {
		return "", GuestLink{}, err
	}
	if g.OnCreate != nil {
		g.OnCreate(link)
	}
	return token, link, nil
}

// List are the links that have not ended.
func (g *Guests) List(appID string) ([]GuestLink, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	list, err := g.read()
	if err != nil {
		return nil, err
	}
	now := g.now()
	out := []GuestLink{}
	for _, l := range list {
		if l.Live(now) && !l.Check && (appID == "" || l.App == appID) {
			out = append(out, l)
		}
	}
	return out, nil
}

// Revoke ends a link by its id.
func (g *Guests) Revoke(id string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	list, err := g.read()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID == id {
			list[i].Revoked = true
			if err := g.write(list); err != nil {
				return err
			}
			if g.OnRevoke != nil {
				g.OnRevoke(id)
			}
			return nil
		}
	}
	return ErrGuestGone
}

// RevokeApp ends every live link of an app (it was removed); the number ended comes back.
func (g *Guests) RevokeApp(appID string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	list, err := g.read()
	if err != nil {
		return 0, err
	}
	now := g.now()
	var ended []string
	for i := range list {
		if list[i].App == appID && list[i].Live(now) {
			list[i].Revoked = true
			ended = append(ended, list[i].ID)
		}
	}
	if len(ended) == 0 {
		return 0, nil
	}
	if err := g.write(list); err != nil {
		return 0, err
	}
	if g.OnRevoke != nil {
		for _, id := range ended {
			g.OnRevoke(id)
		}
	}
	return len(ended), nil
}

// Resolve is the live link of a token, and the app it shows. A link whose app is gone, or no longer shows an app view,
// is gone.
func (g *Guests) Resolve(token string) (GuestLink, Installed, error) {
	if token == "" || len(token) > 128 {
		return GuestLink{}, Installed{}, ErrGuestGone
	}
	h := hashToken(token)
	g.mu.Lock()
	list, err := g.read()
	g.mu.Unlock()
	if err != nil {
		return GuestLink{}, Installed{}, err
	}
	now := g.now()
	for _, l := range list {
		if l.Hash != h {
			continue
		}
		if !l.Live(now) {
			return GuestLink{}, Installed{}, ErrGuestGone
		}
		find := g.reg.Get
		if l.Check {
			find = g.reg.Find
		}
		app, err := find(l.App)
		if err != nil || app.Manifest == nil || (!l.Check && !app.Manifest.HasTarget(TargetApp)) {
			return GuestLink{}, Installed{}, ErrGuestGone
		}
		if _, ok := l.viewOf(app.Manifest); !ok {
			return GuestLink{}, Installed{}, ErrGuestGone
		}
		return l, app, nil
	}
	return GuestLink{}, Installed{}, ErrGuestGone
}

// guestNonce is the nonce of the link's view pages: the frame presents it in the handshake. It follows the token, which
// only this page and its files have seen.
func guestNonce(token string) string { return hashToken("nonce/" + token)[:32] }

func guestOwner(l GuestLink) string { return "guest-" + l.ID }

// GuestMatch: the request is one of the guest routes or the public SDK files the views load by absolute path.
func GuestMatch(r *http.Request) bool {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	return strings.HasPrefix(p, GuestPrefix) || p == SDKPath || p == SDKModulePath || p == ShimPath || p == UICSSPath
}

func guestGone(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = io.WriteString(w, "This link has ended.")
}

// ServeHTTP answers the guest routes. No session: the token in the path is the whole authority, and it names one view.
func (g *Guests) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	switch p {
	case SDKPath:
		serveBytes(w, r, "text/javascript; charset=utf-8", classicSDK())
		return
	case SDKModulePath:
		serveBytes(w, r, "text/javascript; charset=utf-8", sdkJS)
		return
	case ShimPath:
		serveBytes(w, r, "text/javascript; charset=utf-8", shimJS)
		return
	case UICSSPath:
		serveBytes(w, r, "text/css; charset=utf-8", hlUICSS)
		return
	}
	token, rest, hasSlash := strings.Cut(strings.TrimPrefix(p, GuestPrefix), "/")
	link, app, err := g.Resolve(token)
	if err != nil {
		if errors.Is(err, ErrGuestGone) {
			guestGone(w)
			return
		}
		http.Error(w, "failed", http.StatusInternalServerError)
		return
	}
	if !hasSlash {
		http.Redirect(w, r, GuestPrefix+token+"/", http.StatusFound)
		return
	}
	switch {
	case rest == "":
		g.page(w, r, token, link, app)
	case rest == "__guest/host.js":
		serveBytes(w, r, "text/javascript; charset=utf-8", guestHostJS)
	case rest == "__guest/host.css":
		serveBytes(w, r, "text/css; charset=utf-8", guestHostCSS)
	case strings.HasPrefix(rest, "__hl/"):
		g.sdkFile(w, r, strings.TrimPrefix(rest, "__hl/"))
	case rest == "__guest/storage":
		g.storageCall(w, r, link, app)
	case rest == "__guest/report":
		g.report(w, r, link)
	default:
		g.serveFile(w, r, token, rest, app)
	}
}

func guestLang(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.Header.Get("Accept-Language"))), "ru") {
		return "ru"
	}
	return "en"
}

// page is the shell of a link: the view in a frame and the host script.
func (g *Guests) page(w http.ResponseWriter, r *http.Request, token string, l GuestLink, app Installed) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	v, _ := l.viewOf(app.Manifest)
	lang := guestLang(r)
	cfg, _ := json.Marshal(map[string]any{
		"nonce": guestNonce(token), "app": app.ID, "version": app.Version, "view": v.ID, "write": l.Write, "locale": lang, "check": l.Check,
	})
	title := html.EscapeString(app.Manifest.Name)
	body := `<!doctype html><html lang="` + lang + `"><head><meta charset="utf-8">` +
		`<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">` +
		`<meta name="robots" content="noindex"><meta name="referrer" content="no-referrer">` +
		`<meta name="color-scheme" content="light dark">` +
		`<meta name="hl-guest" content="` + html.EscapeString(string(cfg)) + `">` +
		`<title>` + title + `</title><link rel="stylesheet" href="__guest/host.css"></head><body>` +
		`<iframe id="view" title="` + title + `" sandbox="allow-scripts allow-forms allow-popups" referrerpolicy="no-referrer" src="` +
		html.EscapeString(app.ID+"/"+path.Clean(v.Entry)) + `"></iframe>` +
		`<script src="__guest/host.js"></script></body></html>`
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; frame-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Robots-Tag", "noindex")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}

// sdkFile serves the SDK files under the link's own path, so a guest's page reaches them by a relative path and a
// gateway in front of the machine needs no rule for the absolute /__hl/ ones.
func (g *Guests) sdkFile(w http.ResponseWriter, r *http.Request, name string) {
	switch "/__hl/" + name {
	case SDKPath:
		serveBytes(w, r, "text/javascript; charset=utf-8", classicSDK())
	case SDKModulePath:
		serveBytes(w, r, "text/javascript; charset=utf-8", sdkJS)
	case ShimPath:
		serveBytes(w, r, "text/javascript; charset=utf-8", shimJS)
	case UICSSPath:
		serveBytes(w, r, "text/css; charset=utf-8", hlUICSS)
	default:
		http.NotFound(w, r)
	}
}

// relativeSDK turns the absolute SDK paths in a page (or in the import lines of a script) into paths relative to the
// file, whose name inside the link is rel under the app's folder: <token>/<app>/<rel> reaches <token>/__hl/ by one
// ../ for the app's folder and one for each folder of rel.
func relativeSDK(body []byte, rel string, script bool) []byte {
	up := strings.Repeat("../", strings.Count(rel, "/")+1)
	for _, q := range []string{`"`, `'`} {
		old := q + "/__hl/"
		if script {
			// Only the import specifiers: a string in a script that is fetched is read against the page, not the file.
			for _, kw := range []string{"from ", "from", "import ", "import(", "import ("} {
				body = bytes.ReplaceAll(body, []byte(kw+old), []byte(kw+q+up+"__hl/"))
			}
			continue
		}
		body = bytes.ReplaceAll(body, []byte(old), []byte(q+up+"__hl/"))
	}
	return body
}

// file serves one file of the shown app's views, as the view server does for a session: only a view's own files, under
// the app's CSP, a page with the nonce and the shim.
func (g *Guests) serveFile(w http.ResponseWriter, r *http.Request, token, rest string, app Installed) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, rel, ok := strings.Cut(rest, "/")
	if !ok || id != app.ID {
		http.NotFound(w, r)
		return
	}
	miss := func(why string) {
		g.reg.LogEventOnce(app.ID, "", ViewLogServe, "GET "+clip(rel, 200)+" — 404: "+why)
		http.NotFound(w, r)
	}
	pkg := &Package{Dir: app.Dir, Manifest: app.Manifest}
	full, err := pkg.ViewFile(rel)
	if err != nil {
		miss("not among the files the views may load (" + clip(err.Error(), 120) + ")")
		return
	}
	f, err := os.Open(full)
	if err != nil {
		miss("the file is not there")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() > maxViewBytes {
		miss("a folder, or a file over the size a view may load")
		return
	}
	size := info.Size()
	var page []byte
	ct := contentType(full)
	isHTML, isJS := strings.HasPrefix(ct, "text/html"), strings.HasPrefix(ct, "text/javascript")
	if isHTML || isJS {
		raw, err := io.ReadAll(io.LimitReader(f, maxViewBytes+1))
		if err != nil || len(raw) > maxViewBytes {
			http.NotFound(w, r)
			return
		}
		page = relativeSDK(raw, rel, isJS)
		if isHTML {
			up := strings.Repeat("../", strings.Count(rel, "/")+1)
			page = injectNonceShim(page, guestNonce(token), up+"__hl/bridge-shim.js")
		}
		size = int64(len(page))
	}
	h := w.Header()
	h.Set("Content-Type", contentType(full))
	h.Set("Content-Security-Policy", app.Manifest.ViewCSP())
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		return
	}
	if page != nil {
		_, _ = w.Write(page)
		return
	}
	_, _ = io.Copy(w, f)
}

func guestJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func guestRefusal(w http.ResponseWriter, status int, code, message string) {
	guestJSON(w, status, map[string]string{"error": code, "message": message})
}

// storageCall is a view's hl.storage call: the link's area (any scope the view names is this one), when the app asks
// for the storage right at all, and a write only when the owner made the link writable.
func (g *Guests) storageCall(w http.ResponseWriter, r *http.Request, l GuestLink, app Installed) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// A request from another site must not reach a guest's area: the page's own script posts from this origin.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "not from this page")
		return
	}
	if !slices.Contains(app.Manifest.Permissions, "storage") {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "the app does not ask for the storage right")
		return
	}
	var b struct {
		Op        string          `json:"op"`
		Key       string          `json:"key"`
		Value     json.RawMessage `json:"value"`
		IfVersion *int64          `json:"ifVersion"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxGuestBody+1))
	if err != nil || len(raw) > maxGuestBody || json.Unmarshal(raw, &b) != nil {
		guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, "a bad request")
		return
	}
	owner := guestOwner(l)
	fail := func(err error) {
		var conflict *ConflictError
		switch {
		case errors.As(err, &conflict):
			guestJSON(w, http.StatusConflict, map[string]any{"error": CodeConflict, "message": "the key changed since it was read", "version": conflict.Current})
		case errors.Is(err, ErrQuota):
			guestRefusal(w, http.StatusRequestEntityTooLarge, CodeQuotaExceeded, "the link's storage is full")
		case errors.Is(err, ErrStorageArgs):
			guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, "a bad key")
		default:
			guestRefusal(w, http.StatusInternalServerError, CodeHostUnavailable, "failed")
		}
	}
	switch b.Op {
	case "get":
		v, version, ok, err := g.storage.GetVersioned(app.ID, ScopeDoc, owner, b.Key)
		if err != nil {
			fail(err)
			return
		}
		if !ok {
			guestJSON(w, http.StatusOK, map[string]any{"value": nil, "version": version})
			return
		}
		guestJSON(w, http.StatusOK, map[string]any{"value": v, "version": version})
	case "keys":
		keys, err := g.storage.Keys(app.ID, ScopeDoc, owner)
		if err != nil {
			fail(err)
			return
		}
		if keys == nil {
			keys = []string{}
		}
		guestJSON(w, http.StatusOK, map[string]any{"keys": keys})
	case "set", "delete":
		if !l.Write {
			guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "this link only reads")
			return
		}
		var err error
		var version int64
		if b.Op == "set" {
			if len(b.Value) == 0 {
				guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, "a value")
				return
			}
			if b.IfVersion != nil && *b.IfVersion < 0 {
				guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, "ifVersion is 0 or more")
				return
			}
			version, err = g.storage.SetIf(app.ID, ScopeDoc, owner, b.Key, b.Value, b.IfVersion)
		} else {
			err = g.storage.Delete(app.ID, ScopeDoc, owner, b.Key)
		}
		if err != nil {
			fail(err)
			return
		}
		if b.Op == "set" {
			guestJSON(w, http.StatusOK, map[string]any{"version": version})
			return
		}
		guestJSON(w, http.StatusOK, map[string]any{})
	default:
		guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, fmt.Sprintf("no op %q", b.Op))
	}
}

// report takes the lines the page of a check link sends (host.js): the view's console, its handshake, the wait that ran
// out. Only a link of a check may write to the app's log; the lines are bounded as every report is.
func (g *Guests) report(w http.ResponseWriter, r *http.Request, l GuestLink) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !l.Check {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "this link does not report")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "not from this page")
		return
	}
	var b struct {
		Lines []ViewLogLine `json:"lines"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxGuestBody+1))
	if err != nil || len(raw) > maxGuestBody || json.Unmarshal(raw, &b) != nil {
		guestRefusal(w, http.StatusBadRequest, CodeInvalidParams, "a bad request")
		return
	}
	for i := range b.Lines {
		b.Lines[i].View = l.View
	}
	kept, err := g.reg.AppendViewLog(l.App, b.Lines, g.now())
	if err != nil {
		guestRefusal(w, http.StatusInternalServerError, CodeHostUnavailable, "failed")
		return
	}
	guestJSON(w, http.StatusOK, map[string]int{"kept": len(kept)})
}
