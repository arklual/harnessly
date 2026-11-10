package apps

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// HRN-898. An app installed for a space is the same package on every machine of it, and the store keeps it
// (team/archives): its view is static, so the hub serves it to the space's members with every machine off, as it serves
// a guest link. The page is the guest's host page with the member's storage: the user area (the person's own) and the
// space's, both in the store. What runs on a machine (the chat, an agent, the app's tools and its MCP backend) is not
// there, and the page says so when the view asks for it.
//
//	GET  /apps/space/<space id>/<app id>/                     the page: the view in a sandboxed frame and the host script
//	GET  /apps/space/<space id>/<app id>/<app id>/<path>      a file of the app's views, under the app's CSP
//	POST /apps/space/<space id>/<app id>/__guest/storage      hl.storage.user and hl.storage.space

// MemberPrefix is where the hub serves the apps of a space to its members.
const MemberPrefix = "/apps/space/"

// Member is who opens an app at the hub: the space, and the person's name in the store (the name of their user area,
// storehub.PersonName). The hub checks the session and the role before it hands a request over.
type Member struct {
	Space  string
	Person string
}

// Members serves the apps of the spaces' teams to their members.
type Members struct {
	// Lookup finds an app installed for a space, unpacked where the files can be read; ErrNotInstalled when it is not
	// (or its package is not in the store).
	Lookup func(ctx context.Context, spaceID, appID string) (Installed, error)
	// Storage is the storage of a space as a person sees it: its areas in the store, the user area the person's.
	Storage func(ctx context.Context, m Member) (*Storage, error)
}

// MemberPath is the page of an app of a space at the hub.
func MemberPath(spaceID, appID string) string { return MemberPrefix + spaceID + "/" + appID + "/" }

func memberNonce(m Member, appID string) string {
	return hashToken("member/" + m.Space + "/" + m.Person + "/" + appID)[:32]
}

func memberGone(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("This app is not installed for the space."))
}

// ServeMember answers a member's request under MemberPrefix; the space in the path must be the member's.
func (ms *Members) ServeMember(w http.ResponseWriter, r *http.Request, m Member) {
	rest, _ := strings.CutPrefix(r.URL.Path, MemberPrefix)
	spaceID, rest, _ := strings.Cut(rest, "/")
	appID, rest, hasSlash := strings.Cut(rest, "/")
	if spaceID == "" || spaceID != m.Space || m.Person == "" || !idRe.MatchString(appID) {
		memberGone(w)
		return
	}
	app, err := ms.Lookup(r.Context(), spaceID, appID)
	if err != nil || app.Manifest == nil || app.ID != appID {
		if err != nil && !errors.Is(err, ErrNotInstalled) {
			http.Error(w, "failed", http.StatusServiceUnavailable)
			return
		}
		memberGone(w)
		return
	}
	v, ok := app.Manifest.ShareableView("")
	if !ok {
		memberGone(w)
		return
	}
	if !hasSlash {
		// Relative (http.Redirect would make it absolute): the hub may sit under a base path.
		w.Header().Set("Location", appID+"/")
		w.WriteHeader(http.StatusFound)
		return
	}
	nonce := memberNonce(m, appID)
	switch {
	case rest == "":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		lang := guestLang(r)
		hostPage(w, r, lang, app, v, map[string]any{
			"nonce": nonce, "app": app.ID, "version": app.Version, "view": v.ID, "write": true, "locale": lang, "member": true,
		})
	case rest == "__guest/host.js":
		serveBytes(w, r, "text/javascript; charset=utf-8", guestHostJS)
	case rest == "__guest/host.css":
		serveBytes(w, r, "text/css; charset=utf-8", guestHostCSS)
	case strings.HasPrefix(rest, "__hl/"):
		(&Guests{}).sdkFile(w, r, strings.TrimPrefix(rest, "__hl/"))
	case rest == "__guest/storage":
		ms.storageCall(w, r, m, app)
	default:
		serveViewFile(w, r, nil, nonce, rest, app)
	}
}

// storageCall is a member's hl.storage call: the user and space areas; install, chat and doc belong to a machine, an
// open chat or a document, none of which is here.
func (ms *Members) storageCall(w http.ResponseWriter, r *http.Request, m Member, app Installed) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Only the page's own script: the view's frame is an opaque origin, and another site must not write a member's area
	// with their session.
	if site := r.Header.Get("Sec-Fetch-Site"); site != "same-origin" {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "not from this page")
		return
	}
	if !app.Manifest.Allows("storage") {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "the app does not ask for the storage right")
		return
	}
	b, ok := readStorageCall(w, r)
	if !ok {
		return
	}
	scope := Scope(b.Scope)
	if scope != ScopeUser && scope != ScopeSpace {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "with every machine off only the user and space areas are here")
		return
	}
	// The bridge's own rule: a write of the space's area needs storage.space.write.
	if scope == ScopeSpace && (b.Op == "set" || b.Op == "delete") && !app.Manifest.Allows("storage.space.write") {
		guestRefusal(w, http.StatusForbidden, CodePermissionDenied, "a write of the space's area needs the permission storage.space.write")
		return
	}
	st, err := ms.Storage(r.Context(), m)
	if err != nil {
		guestRefusal(w, http.StatusServiceUnavailable, CodeHostUnavailable, "the store is not reachable")
		return
	}
	runStorageCall(w, st, app.ID, scope, "", b, true, "", "the app's storage is full")
}
