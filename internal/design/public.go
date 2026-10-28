package design

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
	"github.com/ly-solutions/harnessly/internal/jscompat"
)

// The public read of a design for a guest link: the platform's gateway signs a 60-second token for the link's project
// and fetches the board list and the project's files with it, so a guest sees the design without a session. The token's
// key is derived from the pair's secret under its own info, so it is never a session or a share-read token.

const (
	publicInfo   = "design-public/v1"
	publicMaxAge = 120
	// publicSkew is how far in the future an iat may be: the runtime's clock may lag the gateway's.
	publicSkew = 30
	// PublicFileLimit is the largest file the public route sends.
	PublicFileLimit = 32 << 20
	// PublicAppDir and PublicGuestDir are first segments of a public path that are not project files: the guest page's
	// bundle, and the script the boards load.
	PublicAppDir    = "_app"
	PublicGuestDir  = "__guest"
	PublicVendorDir = "__vendor"
	// PublicRevDir answers the revision a new pinned link should hold (see PinRevision).
	PublicRevDir = "__rev"
	// PublicRevHeader and PublicPagesHeader say on an answer which version and whether a page list it served.
	PublicRevHeader   = "X-Design-Rev"
	PublicPagesHeader = "X-Design-Pages"
)

// PublicConfig names how the public token is checked: the pair's secret, the HKDF salt (the share salt), the audience
// the gateway signs the token with and the header it carries the token in.
type PublicConfig struct {
	Secret   []byte
	Salt     string
	Audience string
	Header   string
	// Now is the clock (tests freeze it); nil is the real one.
	Now func() time.Time
}

func (c PublicConfig) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func b64url(s string) ([]byte, bool) {
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
	return b, err == nil
}

// publicScope is what a link lets its guest see, as the gateway signed it into the token: a version of the design
// (rev, a history revision; empty is the design as the owner has it now) and the pages (board paths) the guest may
// open. limited tells "these pages only" from "all of them", so a list that came out empty hides everything instead
// of opening everything.
type publicScope struct {
	// pin is the gateway's own claim, for the one call that takes a new snapshot; a guest's token never has it.
	pin     bool
	rev     string
	pages   map[string]bool
	limited bool
}

// allows: the guest may open the board.
func (s publicScope) allows(b Board) bool { return !s.limited || s.pages[b.Path] }

// scopeOf reads the optional claims; false when they are not what a link could have been given.
func scopeOf(payload map[string]any) (publicScope, bool) {
	var s publicScope
	if v, has := payload["rev"]; has {
		rev, ok := v.(string)
		if !ok || !fullSHA.MatchString(rev) {
			return s, false
		}
		s.rev = rev
	}
	if v, has := payload["pin"]; has {
		pin, ok := v.(bool)
		if !ok {
			return s, false
		}
		s.pin = pin
	}
	if v, has := payload["pages"]; has && v != nil {
		list, ok := v.([]any)
		if !ok {
			return s, false
		}
		s.limited, s.pages = true, map[string]bool{}
		for _, item := range list {
			page, ok := item.(string)
			if !ok {
				return s, false
			}
			s.pages[page] = true
		}
	}
	// A pin token is for taking the version of a link, never for reading one.
	if s.pin && (s.rev != "" || s.limited) {
		return s, false
	}
	return s, true
}

// boardFile is the file of a board's path: a path may end in a query (`lib/page.html?Name`) naming the board inside it.
func boardFile(p string) string {
	file, _, _ := strings.Cut(p, "?")
	return path.Clean(file)
}

// hides: the file is a board that is not among the guest's pages. A file that no board owns (a style, an image, the
// engine) is not a page and stays open, the pages need it; a file shared by several boards is open when any of them is.
func (s publicScope) hides(boards []Board, file string) bool {
	if !s.limited {
		return false
	}
	file = path.Clean(file)
	isBoard := false
	for _, b := range boards {
		if boardFile(b.Path) != file {
			continue
		}
		if s.allows(b) {
			return false
		}
		isBoard = true
	}
	return isBoard
}

// isMarkup: a file a browser would run as a page, by its extension in any case.
func isMarkup(file string) bool {
	switch strings.ToLower(filepath.Ext(file)) {
	case ".html", ".htm", ".xhtml", ".xht", ".shtml":
		return true
	}
	return false
}

// isChosenBoard: the opened file is the very file of one of the chosen boards. It is the opened file that is compared
// (os.SameFile), not its spelling, so a case, a "./" or a link cannot name another page as a chosen one.
func (a *API) isChosenBoard(lib *Library, project string, boards []Board, scope publicScope, opened os.FileInfo) bool {
	dir := lib.ProjectDirOf(project)
	for _, b := range boards {
		if !scope.allows(b) {
			continue
		}
		st, err := os.Stat(filepath.Join(dir, filepath.FromSlash(boardFile(b.Path))))
		if err == nil && os.SameFile(opened, st) {
			return true
		}
	}
	return false
}

// check verifies the token for one project and returns the scope it gives the guest: 0 when it is good, 401 for no,
// forged, expired or overlong token, 403 for a valid token of another project.
func (c PublicConfig) check(token, project string) (int, publicScope) {
	none := publicScope{}
	if token == "" || len(c.Secret) == 0 {
		return 401, none
	}
	key, err := hkdf.Key(sha256.New, c.Secret, []byte(c.Salt), publicInfo, 32)
	if err != nil {
		return 401, none
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 401, none
	}
	rawHeader, ok := b64url(parts[0])
	if !ok {
		return 401, none
	}
	var header map[string]any
	if json.Unmarshal(rawHeader, &header) != nil || header["alg"] != "HS256" {
		return 401, none
	}
	if _, crit := header["crit"]; crit {
		return 401, none
	}
	sig, ok := b64url(parts[2])
	if !ok {
		return 401, none
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return 401, none
	}
	rawPayload, ok := b64url(parts[1])
	if !ok {
		return 401, none
	}
	var payload map[string]any
	if json.Unmarshal(rawPayload, &payload) != nil || payload == nil {
		return 401, none
	}
	epoch := math.Floor(float64(c.now().UnixMilli()) / 1000)
	iat, ok1 := payload["iat"].(float64)
	exp, ok2 := payload["exp"].(float64)
	if !ok1 || !ok2 || exp <= epoch || epoch-iat > publicMaxAge || epoch-iat < -publicSkew || exp-iat > publicMaxAge {
		return 401, none
	}
	if aud, ok := payload["aud"].(string); !ok || aud != c.Audience {
		return 401, none
	}
	if prj, ok := payload["prj"].(string); !ok || prj != project {
		return 403, none
	}
	scope, ok := scopeOf(payload)
	if !ok {
		return 401, none
	}
	return 0, scope
}

type publicBoard struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	W     *int   `json:"w"`
	H     *int   `json:"h"`
}

type publicIndex struct {
	Title  string        `json:"title"`
	Kind   *string       `json:"kind"`
	Deck   *publicDeck   `json:"deck"`
	Boards []publicBoard `json:"boards"`
}

func publicText(w http.ResponseWriter, status int, msg string) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg))
}

// publicSegment: a segment of a guest's path; one starting with a dot (hidden files, "..") or holding a separator is
// never served.
func publicSegment(s string) bool {
	return s != "" && !strings.HasPrefix(s, ".") && !strings.ContainsAny(s, "/\\\x00")
}

// PublicMatch: the request is for design-public of the platform's API.
func (a *API) PublicMatch(r *http.Request) bool {
	parts, _, ok := files.APIParts(r)
	want := append(strings.Split(strings.Trim(a.PlatformAPI, "/"), "/"), "design-public")
	if !ok || len(parts) <= len(want) {
		return false
	}
	for i, p := range want {
		if parts[i] != p {
			return false
		}
	}
	return true
}

// Public answers GET design-public/<project>[/<path>] of the platform's API: the board list of the project, or one
// file of its folder, for a request carrying a good token of the project and nothing else.
func (a *API) Public(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		publicText(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	parts, _, ok := files.APIParts(r)
	prefix := strings.Split(strings.Trim(a.PlatformAPI, "/"), "/")
	if !ok || len(parts) < len(prefix)+2 {
		publicText(w, 404, "Not found")
		return
	}
	rest := parts[len(prefix)+1:]
	project, segments := rest[0], rest[1:]
	code, scope := a.PublicToken.check(strings.Join(r.Header.Values(a.PublicToken.Header), ", "), project)
	if code != 0 {
		publicText(w, code, http.StatusText(code))
		return
	}
	if !IsLibraryID(project) {
		publicText(w, 404, "Not found")
		return
	}
	// A pinned link reads the snapshot of its version; the others read the folder as it is.
	lib := a.L
	if scope.rev != "" {
		var ok bool
		if lib, ok = a.L.pinnedLibrary(project, scope.rev); !ok {
			publicText(w, 404, "Not found")
			return
		}
	}
	// The gateway refuses an answer that does not say it kept the scope: a runtime too old to know the claims would
	// serve the latest version and every page, and these headers are how it tells the two apart.
	if scope.rev != "" {
		w.Header().Set(PublicRevHeader, scope.rev)
	}
	if scope.limited {
		w.Header().Set(PublicPagesHeader, "limited")
	}
	art := lib.ReadArtifact(project)
	if art == nil {
		publicText(w, 404, "Not found")
		return
	}
	head := r.Method == http.MethodHead
	if len(segments) == 0 {
		kind := a.publicKind(lib, project)
		idx := publicIndex{Title: art.Title, Kind: kind, Deck: a.publicDeckOf(lib, project, scope, kind), Boards: make([]publicBoard, 0, len(art.Boards))}
		for _, b := range art.Boards {
			if !scope.allows(b) {
				continue
			}
			id := b.ID
			if id == "" {
				id = BoardKeyOf(b)
			}
			idx.Boards = append(idx.Boards, publicBoard{ID: id, Title: b.Title, Path: b.Path, W: b.W, H: b.H})
		}
		body, err := json.Marshal(idx)
		if err != nil {
			publicText(w, 500, "Internal error")
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		if !head {
			_, _ = w.Write(body)
		}
		return
	}
	for _, s := range segments {
		if !publicSegment(s) {
			publicText(w, 404, "Not found")
			return
		}
	}
	switch segments[0] {
	case PublicAppDir:
		a.publicApp(w, r, segments[1:], head)
		return
	case PublicGuestDir:
		if len(segments) == 2 && segments[1] == "client.js" {
			body := []byte(a.L.clientScript())
			h := w.Header()
			h.Set("Content-Type", "text/javascript; charset=utf-8")
			h.Set("Content-Length", strconv.Itoa(len(body)))
			h.Set("Cache-Control", "private, max-age=300")
			h.Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(200)
			if !head {
				_, _ = w.Write(body)
			}
			return
		}
		publicText(w, 404, "Not found")
		return
	case PublicRevDir:
		if len(segments) != 1 {
			publicText(w, 404, "Not found")
			return
		}
		// A guest's file path reaches this route too, so it takes the gateway's own claim to snapshot anything.
		if !scope.pin {
			publicText(w, 403, "Forbidden")
			return
		}
		rev, err := a.L.PinRevision()
		if err != nil {
			a.warn("could not take the version for a guest link", err)
			publicText(w, 503, "Unavailable")
			return
		}
		body, _ := json.Marshal(struct {
			Rev string `json:"rev"`
		}{rev})
		h := w.Header()
		h.Set("Content-Type", "application/json; charset=utf-8")
		h.Set("Cache-Control", "no-store")
		w.WriteHeader(200)
		if !head {
			_, _ = w.Write(body)
		}
		return
	case PublicMediaDir:
		a.publicMediaServe(w, r, lib, project, scope, segments[1:])
		return
	case PublicVendorDir:
		if len(segments) == 2 {
			a.L.serveVendor(w, r, vendorPrefix+segments[1], head)
			return
		}
		publicText(w, 404, "Not found")
		return
	}
	typ := ContentType(extname(segments[len(segments)-1]))
	if typ == "" {
		publicText(w, 404, "Not found")
		return
	}
	if scope.hides(art.Boards, strings.Join(segments, "/")) {
		publicText(w, 404, "Not found")
		return
	}
	f, st, file, ok := a.openPublic(lib, project, segments)
	if !ok {
		publicText(w, 404, "Not found")
		return
	}
	defer f.Close()
	if scope.limited && isMarkup(file) && !a.isChosenBoard(lib, project, art.Boards, scope, st) {
		publicText(w, 404, "Not found")
		return
	}
	if st.Size() > PublicFileLimit {
		publicText(w, 404, "Not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if strings.HasPrefix(typ, "text/html") {
		// A board gets the library's board script in guest mode: the bridge to the framing page (picking a block, the
		// board's settings, the canvas keys) without the owner's tools.
		src, err := readPublicFile(f, PublicFileLimit)
		if err != nil {
			publicText(w, 404, "Not found")
			return
		}
		root, err := filepath.EvalSymlinks(lib.ProjectDirOf(project))
		if err != nil {
			publicText(w, 404, "Not found")
			return
		}
		prefix := strings.Repeat("../", len(segments)-1)
		versioned := withAssetVersions(jscompat.DecodeUTF8(src), filepath.Dir(file), root, lib.engineSwap(project), nil)
		body := []byte(withGuestClient(versioned, project, prefix+PublicGuestDir+"/client.js", prefix+PublicVendorDir+"/"))
		if len(body) > PublicFileLimit {
			publicText(w, 404, "Not found")
			return
		}
		h.Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(200)
		if !head {
			_, _ = w.Write(body)
		}
		return
	}
	if !a.L.servePublicStatic(w, r, f, st, file, typ, strings.ToLower(extname(file)), publicCachePolicy(r, FileVersion(st)), head) {
		publicText(w, 404, "Not found")
	}
}

// withGuestClient puts the board script in guest mode at the head of a board; src is relative to the board, so it
// resolves under whatever address the board is served at.
func withGuestClient(source, project, src, vendor string) string {
	tag := `<script data-artifact="` + project + `" data-guest="1" src="` + src + `"></script>` + resourcesScriptAt(vendor)
	if m := headTag.FindString(source); m != "" {
		return strings.Replace(source, m, m+tag, 1)
	}
	return tag + source
}

func publicCachePolicy(r *http.Request, version string) string {
	if v, ok := search(r.URL.Query(), versionParam); ok && v == version {
		return "private, max-age=31536000, immutable"
	}
	return "private, no-cache"
}

// openPublic opens one stable handle inside the project. The Root keeps every path lookup confined while it runs, and
// the returned handle keeps serving the same inode if an attacker swaps a path after the lookup. A release engine is
// trusted runtime state outside the project, but is likewise opened once and served from that handle.
func (a *API) openPublic(lib *Library, project string, segments []string) (*os.File, os.FileInfo, string, bool) {
	if len(segments) == 1 && segments[0] == EngineFile {
		if engine := lib.EngineFor(project); engine != "" {
			f, st, ok := openRegular(engine)
			return f, st, engine, ok
		}
	}
	dir := lib.ProjectDirOf(project)
	library := lib.Root()
	if dir == "" || library == "" {
		return nil, nil, "", false
	}
	root, err := os.OpenRoot(library)
	if err != nil {
		return nil, nil, "", false
	}
	defer root.Close()
	rel := filepath.Join(append([]string{project, ProjectDirname}, segments...)...)
	f, err := root.Open(rel)
	if err != nil {
		return nil, nil, "", false
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, nil, "", false
	}
	return f, st, filepath.Join(append([]string{dir}, segments...)...), true
}

func openRegular(path string) (*os.File, os.FileInfo, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, false
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		return nil, nil, false
	}
	return f, st, true
}

var errPublicFileLimit = errors.New("public file exceeds limit")

// readPublicFile reads at most one byte over limit from an already validated handle.
func readPublicFile(f *os.File, limit int64) ([]byte, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errPublicFileLimit
	}
	return body, nil
}
