package design

import (
	"bytes"
	"compress/gzip"
	"container/list"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// A small read-only web server on the machine's loopback that serves the library. The right panel opens it like any
// dev server (localhost:<port>): the preview proxy puts the element picker into its pages, so a board is picked,
// commented on and sent to an agent the way a page of a running app is.

const (
	scriptPath   = "/__lib/client.js"
	versionPath  = "/__lib/version"
	boardsPath   = "/__lib/boards"
	logPath      = "/__lib/log"
	eventsPath   = "/__lib/events"
	changesPath  = "/__lib/changes"
	statePath    = "/__lib/state"
	kitsPath     = "/__lib/kits"
	heartbeat    = 20 * time.Second
	versionParam = "tkv"
	immutable    = "public, max-age=31536000, immutable"
	gzipMinBytes = 1024
	gzipCacheMax = 96 * 1024 * 1024
)

var (
	compressible = map[string]bool{".js": true, ".mjs": true, ".jsx": true, ".css": true, ".json": true, ".svg": true, ".txt": true}
	fontExt      = map[string]bool{".woff": true, ".woff2": true, ".ttf": true, ".otf": true}
	imageExt     = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true, ".avif": true, ".ico": true, ".svg": true}
)

type server struct {
	mu   sync.Mutex
	port int
	ln   net.Listener
	srv  *http.Server
	gz   gzipCache
}

// EnsureServer starts the library server on first use and returns its port; every caller gets the same one. The
// preferred port taken, a free one is used.
func (l *Library) EnsureServer() (int, error) {
	l.mu.Lock()
	if l.srv == nil {
		l.srv = &server{}
	}
	s := l.srv
	l.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln != nil {
		return s.port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(l.cfg.Port))
	if err != nil {
		if !errors.Is(err, syscall.EADDRINUSE) {
			return 0, err
		}
		if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {
			return 0, err
		}
	}
	s.ln, s.port = ln, ln.Addr().(*net.TCPAddr).Port
	s.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { l.serveLibrary(w, r, s.port) }),
		ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	_ = l.EnsureGuide()
	go l.WatchModes()
	l.bgStarts.Add(1)
	go func() {
		defer l.bgStarts.Done()
		l.StartBackground()
	}()
	return s.port, nil
}

// ServerPort is the port once the server is up, 0 before; the preview's component lookup leaves this port alone.
func (l *Library) ServerPort() int {
	l.mu.Lock()
	s := l.srv
	l.mu.Unlock()
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// StopServer closes the server and stops the background work it started, waiting out a history snapshot under way (a
// test's cleanup: nothing may write into the folders a test is about to remove).
func (l *Library) StopServer() {
	l.mu.Lock()
	s := l.srv
	l.srv = nil
	l.mu.Unlock()
	if s != nil && s.srv != nil {
		_ = s.srv.Close()
	}
	l.bgStarts.Wait()
	l.StopBackground()
	for {
		l.hist.mu.Lock()
		running := l.hist.running
		l.hist.mu.Unlock()
		if !running {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func send(w http.ResponseWriter, status int, typ string, body []byte, head bool, extra ...string) {
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	for i := 0; i+1 < len(extra); i += 2 {
		h.Set(extra[i], extra[i+1])
	}
	w.WriteHeader(status)
	if !head {
		_, _ = w.Write(body)
	}
}

func sendHTML(w http.ResponseWriter, status int, body string, head bool) {
	send(w, status, "text/html; charset=utf-8", []byte(body), head)
}

var notFoundJSON = []byte(`{"error":"not found"}`)

// HostAllowed: a DNS-rebinding page must not read the library through the loopback, only the names the proxy and a
// person use.
func HostAllowed(host string, port int) bool {
	p := strconv.Itoa(port)
	return host == "localhost:"+p || host == "127.0.0.1:"+p || host == "[::1]:"+p
}

// frameAncestors is the policy of every answer of the library server: a board is framed by the library's own pages, by
// the platform on this machine (another loopback port) and by the configured platform hosts. A board shown through a
// preview slot gets the slot's own policy instead (the proxy drops this one and names the apps that opened the slot).
func (l *Library) frameAncestors() string {
	sources := []string{"'self'", "http://localhost:*", "http://127.0.0.1:*"}
	sources = append(sources, l.platformOrigins()...)
	return "frame-ancestors " + strings.Join(sources, " ")
}

// platformOrigins are the configured platform hosts that show boards, normalized; what is not an origin is left out.
func (l *Library) platformOrigins() []string {
	out := []string{}
	for _, o := range l.cfg.KitOrigins {
		if o = NormalizeOrigin(o); o != "" && o != "null" {
			out = append(out, o)
		}
	}
	return out
}

// search is URLSearchParams.get: the first value, ok false when absent.
func search(q url.Values, k string) (string, bool) {
	vs, ok := q[k]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

func searchPtr(q url.Values, k string) *string {
	if v, ok := search(q, k); ok {
		return &v
	}
	return nil
}

// queryNumber is Number() of a query value: null and "" are 0, NaN when it is not a number.
func queryNumber(s string, present bool) float64 {
	if !present {
		return 0
	}
	t := jscompat.Trim(s)
	if t == "" {
		return 0
	}
	if len(t) > 2 && t[0] == '0' && strings.ContainsRune("xXoObB", rune(t[1])) {
		base := map[byte]int{'x': 16, 'X': 16, 'o': 8, 'O': 8, 'b': 2, 'B': 2}[t[1]]
		if n, err := strconv.ParseUint(t[2:], base, 64); err == nil {
			return float64(n)
		}
		return math.NaN()
	}
	switch t {
	case "Infinity", "+Infinity":
		return math.Inf(1)
	case "-Infinity":
		return math.Inf(-1)
	}
	if strings.ContainsAny(t, "xXpP_") || strings.HasPrefix(strings.TrimLeft(t, "+-"), "Inf") || strings.HasPrefix(strings.TrimLeft(t, "+-"), "inf") || strings.EqualFold(t, "nan") {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			return f
		}
		return math.NaN()
	}
	return f
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func (l *Library) serveLibrary(w http.ResponseWriter, r *http.Request, port int) {
	defer func() {
		if v := recover(); v != nil {
			if v == http.ErrAbortHandler {
				panic(v)
			}
			send(w, 500, "text/plain; charset=utf-8", []byte("Internal error"), false)
		}
	}()
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		send(w, 405, "text/plain; charset=utf-8", []byte("Method not allowed"), false, "Allow", "GET, HEAD")
		return
	}
	if !HostAllowed(r.Host, port) {
		send(w, 403, "text/plain; charset=utf-8", []byte("Forbidden"), false)
		return
	}
	// Only the platform may frame a board: a page of another site would read it through the board script's messages.
	w.Header().Set("Content-Security-Policy", l.frameAncestors())
	head := r.Method == http.MethodHead
	pathname := r.URL.EscapedPath()
	if pathname == "" {
		pathname = "/"
	}
	q := r.URL.Query()
	id, _ := search(q, "a")
	switch pathname {
	case versionPath, eventsPath, statePath, logPath, changesPath, boardsPath:
		need := RoleView
		if pathname == statePath || pathname == logPath {
			need = RoleEdit // they write the live state and log
		}
		if IsLibraryID(id) && !l.Can(id, need) {
			send(w, 404, "application/json", notFoundJSON, head)
			return
		}
	}
	switch pathname {
	case "/favicon.ico":
		w.WriteHeader(204)
		return
	case scriptPath:
		send(w, 200, "text/javascript; charset=utf-8", []byte(l.clientScript()), head, "Cache-Control", "public, max-age=300")
		return
	case versionPath:
		if !IsLibraryID(id) || l.ReadArtifact(id) == nil {
			send(w, 404, "application/json", notFoundJSON, head)
			return
		}
		send(w, 200, "application/json", nodejson.Marshal(struct {
			Version int64 `json:"version"`
		}{l.ArtifactVersion(id, versionTTL)}), head)
		return
	case eventsPath:
		l.serveEvents(w, r, id, head)
		return
	case statePath, logPath:
		if r.Header.Get("Sec-Fetch-Site") != "same-origin" {
			send(w, 403, "text/plain; charset=utf-8", []byte("Forbidden"), head)
			return
		}
		if !IsLibraryID(id) {
			send(w, 404, "application/json", notFoundJSON, head)
			return
		}
		if pathname == statePath {
			l.RecordState(id, searchPtr(q, "s"))
		} else {
			// A batch of timeline events (`e`) rides the same request as a log line.
			l.RecordLiveLog(id, searchPtr(q, "k"), searchPtr(q, "p"), searchPtr(q, "m"))
			l.RecordTimeline(id, searchPtr(q, "p"), searchPtr(q, "e"))
		}
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(204)
		return
	case changesPath:
		sinceRaw, present := search(q, "since")
		since := queryNumber(sinceRaw, present)
		if !IsLibraryID(id) || !finite(since) {
			send(w, 400, "application/json", []byte(`{"error":"bad request"}`), head)
			return
		}
		send(w, 200, "application/json", nodejson.Marshal(struct {
			Paths   []string `json:"paths"`
			Version int64    `json:"version"`
		}{l.changedSince(id, since), l.ArtifactVersion(id, versionTTL)}), head)
		return
	case boardsPath:
		var a *Artifact
		if IsLibraryID(id) {
			a = l.ReadArtifact(id)
		}
		if a == nil {
			send(w, 404, "application/json", notFoundJSON, head)
			return
		}
		send(w, 200, "application/json", nodejson.Marshal(struct {
			Title  string     `json:"title"`
			Boards []barBoard `json:"boards"`
		}{a.Title, l.boardsForBar(a)}), head)
		return
	case kitsPath:
		l.serveKitList(w, q, head)
		return
	case "/":
		sendHTML(w, 200, l.indexPage(l.VisibleTo(l.ListArtifacts()), l.Root()), head)
		return
	}
	if strings.HasPrefix(pathname, hashedPrefix) {
		l.serveHashed(w, r, pathname, head)
		return
	}
	if strings.HasPrefix(pathname, vendorPrefix) {
		l.serveVendor(w, r, pathname, head)
		return
	}
	raw := strings.Split(pathname, "/")[1:]
	segments := make([]string, len(raw))
	for i, s := range raw {
		d, ok := decodeURIComponent(s)
		if !ok {
			send(w, 400, "text/plain; charset=utf-8", []byte("Bad request"), head)
			return
		}
		segments[i] = d
	}
	at := func(i int) (string, bool) {
		if i < len(segments) {
			return segments[i], true
		}
		return "", false
	}
	area, _ := at(0)
	ref, _ := at(1)
	if area == "kit" {
		l.serveKit(w, r, segments[1:], port, head)
		return
	}
	if area != "a" || !IsLibraryID(ref) {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	if l.refuseBoard(w, ref, head) {
		return
	}
	kind, hasKind := at(2)
	if !hasKind || (kind == "" && len(segments) == 3) {
		if a := l.ReadArtifact(ref); a != nil {
			sendHTML(w, 200, l.artifactPage(a), head)
		} else {
			sendHTML(w, 404, l.notFoundPage(), head)
		}
		return
	}
	if kind == "p" {
		if to, ok := l.goneBoardRedirect(ref, segments[3:]); ok {
			h := w.Header()
			h.Set("Location", to)
			h.Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusFound)
			return
		}
		l.serveFile(w, r, q, ref, segments[3:], head)
		return
	}
	sendHTML(w, 404, l.notFoundPage(), head)
}

type barBoard struct {
	Title   string       `json:"title"`
	Href    string       `json:"href"`
	Wrapper *string      `json:"wrapper"`
	Tweaks  *BoardTweaks `json:"tweaks"`
}

// boardsForBar: the address of each board, and for a page shown by a wrapper board its tweaks and the wrapper's own
// address, so a link to the wrapper can lead to the page instead.
func (l *Library) boardsForBar(a *Artifact) []barBoard {
	out := make([]barBoard, 0, len(a.Boards))
	for _, b := range a.Boards {
		bb := barBoard{Title: b.Title, Href: BoardHref(a.ID, b.Path)}
		wrapper := b.Title + dcSuffix
		file := ""
		if strings.HasPrefix(b.Path, "lib/page.html?") && IsBoardFile(wrapper) {
			file = l.ResolveProjectFile(a.ID, []string{wrapper})
		}
		if file != "" {
			href := BoardHref(a.ID, wrapper)
			bb.Wrapper = &href
			if src, err := os.ReadFile(file); err == nil && len(src) > 0 {
				bb.Tweaks = ParseBoardTweaks(jscompat.DecodeUTF8(src))
			}
		}
		out = append(out, bb)
	}
	return out
}

var headTag = regexp.MustCompile(`(?i)<head(?:\s[^>]*)?>`)

func withClient(source, id string) string {
	tag := resourcesScript() + `<script data-artifact="` + id + `" src="` + scriptPath + `"></script>`
	if m := headTag.FindString(source); m != "" {
		return strings.Replace(source, m, m+tag, 1)
	}
	return tag + source
}

// FileVersion is a file's version in an address: time and size in base 36.
func FileVersion(st os.FileInfo) string {
	return strconv.FormatInt(round(mtimeMs(st)), 36) + "-" + strconv.FormatInt(st.Size(), 36)
}

var (
	assetStart   = regexp.MustCompile(`(?i)<(?:script|link|img)\b`)
	assetAttr    = regexp.MustCompile(`(?i)^\s(?:src|href)\s*=\s*`)
	projectURLRe = regexp.MustCompile(`(?i)^(?:[a-z][a-z0-9+.-]*:|/|#)`)
)

func isProjectURL(u string) bool { return !projectURLRe.MatchString(u) && !strings.Contains(u, "{{") }

type assetMatch struct {
	start, end         int
	prefix, quote, url string
}

// assetMatches finds what /(<(?:script|link|img)\b[^>]*?\s(?:src|href)\s*=\s*)(["'])([^"'<>]+)\2/gi finds.
func assetMatches(src string) []assetMatch {
	var out []assetMatch
	pos := 0
	for pos < len(src) {
		loc := assetStart.FindStringIndex(src[pos:])
		if loc == nil {
			break
		}
		start := pos + loc[0]
		found := false
		for j := pos + loc[1]; j < len(src); j++ {
			if m := assetAttr.FindStringIndex(src[j:]); m != nil {
				k := j + m[1]
				if k < len(src) && (src[k] == '"' || src[k] == '\'') {
					q := src[k]
					e := k + 1
					for e < len(src) && !strings.ContainsRune(`"'<>`, rune(src[e])) {
						e++
					}
					if e > k+1 && e < len(src) && src[e] == q {
						out = append(out, assetMatch{start: start, end: e + 1, prefix: src[start:k], quote: string(q), url: src[k+1 : e]})
						pos, found = e+1, true
						break
					}
				}
			}
			if src[j] == '>' {
				break
			}
		}
		if !found {
			pos = start + 1
		}
	}
	return out
}

// withAssetVersions puts each asset's version into its address (styles.css?tkv=…): such a file is kept for good by
// the browser, so opening a board again asks only for the page; an edited file gets a new address. served maps a
// file to the one actually answered for it (the release's engine for the project's support.js).
func withAssetVersions(source, dir, projectRoot string, served func(real string) string, hx *hashIndex) string {
	matches := assetMatches(source)
	if len(matches) == 0 {
		return source
	}
	versions := map[string]string{}
	hashedURLs := map[string]string{}
	for _, m := range matches {
		if _, seen := versions[m.url]; seen || !isProjectURL(m.url) {
			continue
		}
		versions[m.url] = ""
		base := m.url
		if i := strings.IndexAny(base, "?#"); i >= 0 {
			base = base[:i]
		}
		decoded, ok := decodeURIComponent(base)
		if !ok {
			continue
		}
		target := decoded
		if !filepath.IsAbs(target) {
			target = filepath.Join(dir, decoded)
		}
		real, err := filepath.EvalSymlinks(target)
		if err != nil || !strings.HasPrefix(real, projectRoot+string(filepath.Separator)) {
			continue
		}
		if served != nil {
			real = served(real)
		}
		if st, err := os.Stat(real); err == nil && st.Mode().IsRegular() {
			// a heavy file, hashed after the swap: a release's engine is addressed by its own bytes, not the project copy's
			if u, ok := hx.urlFor(real, st); ok {
				hashedURLs[m.url] = u
				continue
			}
			versions[m.url] = FileVersion(st)
		}
	}
	var b strings.Builder
	last := 0
	for _, m := range matches {
		v := versions[m.url]
		b.WriteString(source[last:m.start])
		if u, ok := hashedURLs[m.url]; ok {
			frag := ""
			if i := strings.Index(m.url, "#"); i >= 0 {
				frag = m.url[i:]
			}
			b.WriteString(m.prefix + m.quote + u + frag + m.quote)
		} else if v == "" {
			b.WriteString(source[m.start:m.end])
		} else {
			parts := strings.Split(m.url, "#")
			base, sep := parts[0], "?"
			if strings.Contains(base, "?") {
				sep = "&"
			}
			hash := ""
			if len(parts) > 1 {
				hash = "#" + parts[1]
			}
			b.WriteString(m.prefix + m.quote + base + sep + versionParam + "=" + v + hash + m.quote)
		}
		last = m.end
	}
	b.WriteString(source[last:])
	return b.String()
}

func cachePolicy(q url.Values, ext, version string) string {
	if v, ok := search(q, versionParam); ok && v == version {
		return immutable
	}
	if fontExt[ext] {
		return "public, max-age=604800"
	}
	if imageExt[ext] {
		return "public, max-age=3600"
	}
	return "no-cache"
}

// takesGzip: every browser takes gzip; the preview proxy says identity only because it rewrites pages, and leaves a
// compressed body alone.
func takesGzip(r *http.Request) bool {
	vs, ok := r.Header["Accept-Encoding"]
	if !ok {
		return true
	}
	h := strings.Join(vs, ", ")
	return h == "" || jscompat.Trim(h) == "identity" || gzipWord.MatchString(h)
}

var gzipWord = regexp.MustCompile(`(?i)\bgzip\b`)

// engineSwap maps the project's own support.js to the engine of its release; nil when the project is on its own.
func (l *Library) engineSwap(id string) func(string) string {
	engine := l.EngineFor(id)
	if engine == "" {
		return nil
	}
	own := filepath.Join(l.ProjectDir(id), EngineFile)
	if real, err := filepath.EvalSymlinks(l.ProjectDir(id)); err == nil {
		own = filepath.Join(real, EngineFile)
	}
	return func(real string) string {
		if real == own {
			return engine
		}
		return real
	}
}

func (l *Library) serveFile(w http.ResponseWriter, r *http.Request, q url.Values, id string, segments []string, head bool) {
	file := l.ResolveProjectFile(id, segments)
	// A project on a release gets the release's engine as its support.js, whether or not it keeps a copy of its own.
	if len(segments) == 1 && segments[0] == EngineFile {
		if engine := l.EngineFor(id); engine != "" {
			file = engine
		}
	}
	ext := strings.ToLower(extname(file))
	typ := ""
	if file != "" {
		typ = ContentType(ext)
	}
	if typ == "" {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	if strings.HasPrefix(typ, "text/html") {
		src, err := os.ReadFile(file)
		root, rerr := filepath.EvalSymlinks(l.ProjectDirOf(id))
		if err != nil || rerr != nil {
			sendHTML(w, 404, l.notFoundPage(), head)
			return
		}
		send(w, 200, typ, []byte(withClient(withAssetVersions(jscompat.DecodeUTF8(src), filepath.Dir(file), root, l.engineSwap(id), l.hashed), id)), head)
		return
	}
	st, err := os.Stat(file)
	if err != nil {
		sendHTML(w, 404, l.notFoundPage(), head)
		return
	}
	l.serveStatic(w, r, file, st, typ, ext, cachePolicy(q, ext, FileVersion(st)), head)
}

// serveStatic sends a file with its version as the ETag and the given Cache-Control, gzipped when it is text a browser
// takes compressed.
func (l *Library) serveStatic(w http.ResponseWriter, r *http.Request, file string, st os.FileInfo, typ, ext, cacheControl string, head bool) {
	version := FileVersion(st)
	etag := `W/"` + version + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Last-Modified", st.ModTime().UTC().Format(http.TimeFormat))
	h.Set("Cache-Control", cacheControl)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Add("Vary", "Accept-Encoding")
	if inm, ok := r.Header["If-None-Match"]; ok && strings.Join(inm, ", ") == etag {
		w.WriteHeader(304)
		return
	}
	if compressible[ext] && st.Size() >= gzipMinBytes && takesGzip(r) {
		l.mu.Lock()
		if l.srv == nil {
			l.srv = &server{}
		}
		s := l.srv
		l.mu.Unlock()
		packed, err := s.gz.get(file, st)
		if err == nil {
			h.Set("Content-Type", typ)
			h.Set("Content-Encoding", "gzip")
			h.Set("Content-Length", strconv.Itoa(len(packed)))
			w.WriteHeader(200)
			if !head {
				_, _ = w.Write(packed)
			}
			return
		}
	}
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.WriteHeader(200)
	if head {
		return
	}
	f, err := os.Open(file)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = io.Copy(w, f)
}

// servePublicStatic is serveStatic for an untrusted public path. It reads only from the handle opened below the
// project's os.Root, so a path or symlink swap between validation and response cannot change the bytes being served.
// The gzip cache has a separate namespace and is populated only from that stable handle.
func (l *Library) servePublicStatic(w http.ResponseWriter, r *http.Request, f *os.File, st os.FileInfo, file, typ, ext, cacheControl string, head bool) bool {
	if st.Size() < 0 || st.Size() > PublicFileLimit {
		return false
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	version := FileVersion(st)
	etag := `W/"` + version + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Last-Modified", st.ModTime().UTC().Format(http.TimeFormat))
	h.Set("Cache-Control", cacheControl)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Add("Vary", "Accept-Encoding")
	if inm, ok := r.Header["If-None-Match"]; ok && strings.Join(inm, ", ") == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	if compressible[ext] && st.Size() >= gzipMinBytes && takesGzip(r) {
		l.mu.Lock()
		if l.srv == nil {
			l.srv = &server{}
		}
		s := l.srv
		l.mu.Unlock()
		packed, err := s.gz.getWith("public-open\n"+file, st, func() ([]byte, error) {
			return readPublicFile(f, PublicFileLimit)
		})
		if err == nil {
			h.Set("Content-Type", typ)
			h.Set("Content-Encoding", "gzip")
			h.Set("Content-Length", strconv.Itoa(len(packed)))
			w.WriteHeader(http.StatusOK)
			if !head {
				_, _ = w.Write(packed)
			}
			return true
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return false
	}
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	w.WriteHeader(http.StatusOK)
	if !head {
		_, _ = io.CopyN(w, f, st.Size())
	}
	return true
}

// gzipCache keeps the compressed copy of a text asset by path, time and size, the least recently used dropped first.
type gzipCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
	bytes int
}

type gzEntry struct {
	key  string
	data []byte
}

func (c *gzipCache) get(file string, st os.FileInfo) ([]byte, error) {
	return c.getWith(file, st, func() ([]byte, error) { return os.ReadFile(file) })
}

func (c *gzipCache) getWith(file string, st os.FileInfo, load func() ([]byte, error)) ([]byte, error) {
	key := file + "\n" + strconv.FormatInt(st.ModTime().UnixNano(), 10) + "\n" + strconv.FormatInt(st.Size(), 10)
	c.mu.Lock()
	if c.items == nil {
		c.items, c.order = map[string]*list.Element{}, list.New()
	}
	if el, ok := c.items[key]; ok {
		c.order.MoveToBack(el)
		data := el.Value.(*gzEntry).data
		c.mu.Unlock()
		return data, nil
	}
	c.mu.Unlock()
	raw, err := load()
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, 6)
	_, _ = zw.Write(raw)
	_ = zw.Close()
	packed := buf.Bytes()
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.items[key]; !ok {
		c.items[key] = c.order.PushBack(&gzEntry{key: key, data: packed})
		c.bytes += len(packed)
	}
	for c.bytes > gzipCacheMax && c.order.Len() > 0 {
		front := c.order.Front()
		e := front.Value.(*gzEntry)
		c.order.Remove(front)
		delete(c.items, e.key)
		c.bytes -= len(e.data)
	}
	return packed, nil
}

// goneBoardRedirect: a board page that is no longer there (an agent renamed or removed it) leads to the project's first
// board, or to the project's page when it has none — a reload of an open board never ends on «no such design». Only
// board pages of a project that exists are led on; any other missing file stays a 404.
func (l *Library) goneBoardRedirect(ref string, segments []string) (string, bool) {
	if len(segments) == 0 || !IsBoardFile(segments[len(segments)-1]) {
		return "", false
	}
	if l.ResolveProjectFile(ref, segments) != "" {
		return "", false
	}
	a := l.ReadArtifact(ref)
	if a == nil {
		return "", false
	}
	if len(a.Boards) == 0 {
		return "/a/" + EncodeURIComponent(ref) + "/", true
	}
	return BoardHref(ref, a.Boards[0].Path), true
}
