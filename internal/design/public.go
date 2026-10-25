package design

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/files"
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

// verify checks the token for one project: 0 when it is good, 401 for no, forged, expired or overlong token, 403 for a
// valid token of another project.
func (c PublicConfig) verify(token, project string) int {
	if token == "" || len(c.Secret) == 0 {
		return 401
	}
	key, err := hkdf.Key(sha256.New, c.Secret, []byte(c.Salt), publicInfo, 32)
	if err != nil {
		return 401
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return 401
	}
	rawHeader, ok := b64url(parts[0])
	if !ok {
		return 401
	}
	var header map[string]any
	if json.Unmarshal(rawHeader, &header) != nil || header["alg"] != "HS256" {
		return 401
	}
	if _, crit := header["crit"]; crit {
		return 401
	}
	sig, ok := b64url(parts[2])
	if !ok {
		return 401
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(mac.Sum(nil), sig) {
		return 401
	}
	rawPayload, ok := b64url(parts[1])
	if !ok {
		return 401
	}
	var payload map[string]any
	if json.Unmarshal(rawPayload, &payload) != nil || payload == nil {
		return 401
	}
	epoch := math.Floor(float64(c.now().UnixMilli()) / 1000)
	iat, ok1 := payload["iat"].(float64)
	exp, ok2 := payload["exp"].(float64)
	if !ok1 || !ok2 || exp <= epoch || epoch-iat > publicMaxAge || epoch-iat < -publicSkew || exp-iat > publicMaxAge {
		return 401
	}
	if aud, ok := payload["aud"].(string); !ok || aud != c.Audience {
		return 401
	}
	if prj, ok := payload["prj"].(string); !ok || prj != project {
		return 403
	}
	return 0
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
	if code := a.PublicToken.verify(strings.Join(r.Header.Values(a.PublicToken.Header), ", "), project); code != 0 {
		publicText(w, code, http.StatusText(code))
		return
	}
	if !IsLibraryID(project) {
		publicText(w, 404, "Not found")
		return
	}
	art := a.L.ReadArtifact(project)
	if art == nil {
		publicText(w, 404, "Not found")
		return
	}
	head := r.Method == http.MethodHead
	if len(segments) == 0 {
		idx := publicIndex{Title: art.Title, Boards: make([]publicBoard, 0, len(art.Boards))}
		for _, b := range art.Boards {
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
	typ := ContentType(extname(segments[len(segments)-1]))
	if typ == "" {
		publicText(w, 404, "Not found")
		return
	}
	f, st, ok := a.openPublic(project, segments)
	if !ok {
		publicText(w, 404, "Not found")
		return
	}
	defer f.Close()
	if st.Size() > PublicFileLimit {
		publicText(w, 404, "Not found")
		return
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(200)
	if !head {
		_, _ = io.Copy(w, io.LimitReader(f, PublicFileLimit))
	}
}

// openPublic opens a file of the project folder without leaving it, symlinks included; the project's own support.js
// is the engine of its release when it is on one, as the board server serves it.
func (a *API) openPublic(project string, segments []string) (*os.File, os.FileInfo, bool) {
	if len(segments) == 1 && segments[0] == EngineFile {
		if engine := a.L.EngineFor(project); engine != "" {
			return openRegular(engine)
		}
	}
	root, err := os.OpenRoot(a.L.ProjectDirOf(project))
	if err != nil {
		return nil, nil, false
	}
	defer root.Close()
	f, err := root.Open(filepath.Join(segments...))
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
