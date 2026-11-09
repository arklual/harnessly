// Package diskfs serves the Disk tab's file routes (/api/fs/*): the roots a deployment names, a listing, the favorite
// folders, a download, a new folder and resumable uploads. Everything is relative to a root; a path never leaves its
// root, neither by "..", nor by an absolute path, nor by a symbolic link.
package diskfs

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Root is one folder of the machine the Disk tab may open.
type Root struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

// Config is what a deployment sets: the roots, and where the server keeps its own state.
type Config struct {
	Roots []Root
	// StateDir keeps the favorites and the unfinished uploads (the latter under disk-uploads/).
	StateDir string
	// ChunkSize is how many bytes one PUT of an upload may carry; 0 is 8 MiB.
	ChunkSize int64
}

const (
	defaultChunk = 8 << 20
	pageSize     = 500
)

// ParseRoots reads "Label=/abs/path;Other=/abs/other". The id of a root is its label as a slug.
func ParseRoots(spec string) ([]Root, error) {
	var roots []Root
	seen := map[string]bool{}
	for _, item := range strings.Split(spec, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		label, path, ok := strings.Cut(item, "=")
		label, path = strings.TrimSpace(label), strings.TrimSpace(path)
		if !ok || label == "" || !filepath.IsAbs(path) {
			return nil, fmt.Errorf("disk root %q: want Label=/absolute/path", item)
		}
		id := slug(label)
		if id == "" || seen[id] {
			return nil, fmt.Errorf("disk root %q: the label gives an empty or repeated id", item)
		}
		seen[id] = true
		roots = append(roots, Root{ID: id, Label: label, Path: filepath.Clean(path)})
	}
	return roots, nil
}

var notSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(notSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// Server answers the routes. Make it with New.
type Server struct {
	cfg Config
	mu  sync.Mutex // favorites and upload sessions
}

// New is a server for the roots; nil when there are none (the feature is off).
func New(cfg Config) *Server {
	if len(cfg.Roots) == 0 {
		return nil
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = defaultChunk
	}
	return &Server{cfg: cfg}
}

type refusal struct {
	status int
	code   string
	msg    string
	extra  map[string]any
}

func (e *refusal) Error() string { return e.code }

func refuse(status int, code, msg string) *refusal {
	return &refusal{status: status, code: code, msg: msg}
}

var (
	errForbidden = refuse(403, "forbidden", "")
	errNotFound  = refuse(404, "not-found", "")
)

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func fail(w http.ResponseWriter, err error) {
	var e *refusal
	if !errors.As(err, &e) {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			e = errNotFound
		case errors.Is(err, fs.ErrPermission):
			e = errForbidden
		case errors.Is(err, syscall.ENOSPC):
			e = refuse(507, "no-space", "")
		default:
			e = refuse(500, "failed", "")
		}
	}
	body := map[string]any{"error": e.code}
	if e.msg != "" {
		body["message"] = e.msg
	}
	for k, v := range e.extra {
		body[k] = v
	}
	reply(w, e.status, body)
}

func (s *Server) root(id string) (Root, error) {
	for _, r := range s.cfg.Roots {
		if r.ID == id {
			return r, nil
		}
	}
	return Root{}, errNotFound
}

// inside is whether real is the base or below it.
func inside(base, real string) bool {
	return real == base || strings.HasPrefix(real, strings.TrimRight(base, string(filepath.Separator))+string(filepath.Separator))
}

// segments splits a relative path, refusing anything that could leave the root.
func segments(rel string) ([]string, error) {
	if strings.ContainsRune(rel, 0) || strings.HasPrefix(rel, "/") || strings.Contains(rel, "\\") {
		return nil, errForbidden
	}
	var out []string
	for _, p := range strings.Split(rel, "/") {
		switch p {
		case "", ".":
		case "..":
			return nil, errForbidden
		default:
			out = append(out, p)
		}
	}
	return out, nil
}

// resolve is the real path of rel under the root, which must exist; a link that leads out of the root is refused.
func (s *Server) resolve(rootID, rel string) (Root, string, error) {
	root, err := s.root(rootID)
	if err != nil {
		return root, "", err
	}
	parts, err := segments(rel)
	if err != nil {
		return root, "", err
	}
	base, err := filepath.EvalSymlinks(root.Path)
	if err != nil {
		return root, "", err
	}
	real, err := filepath.EvalSymlinks(filepath.Join(append([]string{base}, parts...)...))
	if err != nil {
		return root, "", err
	}
	if !inside(base, real) {
		return root, "", errForbidden
	}
	return root, real, nil
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= 255 &&
		!strings.ContainsAny(name, "/\\\x00")
}

// Serve answers one request; sub is the path after /api/fs.
func (s *Server) Serve(w http.ResponseWriter, r *http.Request, sub []string) {
	switch {
	case len(sub) == 1 && sub[0] == "roots" && r.Method == http.MethodGet:
		reply(w, 200, map[string]any{"roots": s.cfg.Roots})
	case len(sub) == 1 && sub[0] == "list" && r.Method == http.MethodGet:
		s.list(w, r)
	case len(sub) == 1 && sub[0] == "download" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.download(w, r)
	case len(sub) == 1 && sub[0] == "mkdir" && r.Method == http.MethodPost:
		s.mkdir(w, r)
	case len(sub) == 1 && sub[0] == "favorites":
		s.favorites(w, r)
	case len(sub) >= 1 && sub[0] == "uploads":
		s.uploads(w, r, sub[1:])
	default:
		reply(w, 404, map[string]any{"error": "not-found"})
	}
}

func readJSON(r *http.Request, v any) error {
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v); err != nil {
		return refuse(400, "bad-request", "")
	}
	return nil
}

type entry struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	_, dir, err := s.resolve(q.Get("root"), q.Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	items, err := os.ReadDir(dir)
	if err != nil {
		fail(w, err)
		return
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })
	from := 0
	if c := q.Get("cursor"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 0 {
			fail(w, refuse(400, "bad-request", ""))
			return
		}
		from = n
	}
	if from > len(items) {
		from = len(items)
	}
	end := from + pageSize
	var next any
	if end < len(items) {
		next = strconv.Itoa(end)
	} else {
		end = len(items)
	}
	out := make([]entry, 0, end-from)
	for _, it := range items[from:end] {
		info, err := it.Info() // Lstat: a link is shown as a link and never followed
		if err != nil {
			continue
		}
		e := entry{Name: it.Name(), Kind: "file", Size: info.Size(), Mtime: info.ModTime().UnixMilli()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			e.Kind, e.Size = "link", 0
		case info.IsDir():
			e.Kind, e.Size = "dir", 0
		}
		out = append(out, e)
	}
	reply(w, 200, map[string]any{"entries": out, "nextCursor": next, "truncated": false})
}

func (s *Server) download(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	_, real, err := s.resolve(q.Get("root"), q.Get("path"))
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(real)
	if err != nil {
		fail(w, err)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		fail(w, errNotFound)
		return
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(real)}))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", info.ModTime(), f)
}

func (s *Server) mkdir(w http.ResponseWriter, r *http.Request) {
	var in struct{ Root, Path, Name string }
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if !validName(in.Name) {
		fail(w, refuse(400, "bad-name", ""))
		return
	}
	_, dir, err := s.resolve(in.Root, in.Path)
	if err != nil {
		fail(w, err)
		return
	}
	if err := os.Mkdir(filepath.Join(dir, in.Name), 0o755); err != nil {
		if errors.Is(err, fs.ErrExist) {
			fail(w, refuse(409, "exists", ""))
			return
		}
		fail(w, err)
		return
	}
	reply(w, 201, map[string]any{"ok": true})
}

// ---- favorites

type favorite struct {
	Root string `json:"root"`
	Path string `json:"path"`
}

type favoriteView struct {
	favorite
	Name    string `json:"name"`
	Missing bool   `json:"missing"`
}

func (s *Server) favoritesFile() string { return filepath.Join(s.cfg.StateDir, "disk-favorites.json") }

func (s *Server) loadFavorites() []favorite {
	data, err := os.ReadFile(s.favoritesFile())
	var list []favorite
	if err == nil {
		_ = json.Unmarshal(data, &list)
	}
	return list
}

func (s *Server) saveFavorites(list []favorite) error {
	if err := os.MkdirAll(s.cfg.StateDir, 0o700); err != nil {
		return err
	}
	data, _ := json.Marshal(list)
	tmp := s.favoritesFile() + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.favoritesFile())
}

func (s *Server) favoriteViews(list []favorite) []favoriteView {
	out := make([]favoriteView, 0, len(list))
	for _, f := range list {
		root, err := s.root(f.Root)
		if err != nil {
			continue // a root the deployment no longer names
		}
		name := root.Label
		if f.Path != "" {
			name = f.Path[strings.LastIndex(f.Path, "/")+1:]
		}
		missing := true
		if _, real, err := s.resolve(f.Root, f.Path); err == nil {
			if info, err := os.Stat(real); err == nil && info.IsDir() {
				missing = false
			}
		}
		out = append(out, favoriteView{favorite: f, Name: name, Missing: missing})
	}
	return out
}

func (s *Server) favorites(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.loadFavorites()
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost, http.MethodDelete:
		var in favorite
		if r.Method == http.MethodPost {
			if err := readJSON(r, &in); err != nil {
				fail(w, err)
				return
			}
		} else {
			in = favorite{Root: r.URL.Query().Get("root"), Path: r.URL.Query().Get("path")}
		}
		parts, err := segments(in.Path)
		if err != nil {
			fail(w, err)
			return
		}
		in.Path = strings.Join(parts, "/")
		if _, err := s.root(in.Root); err != nil {
			fail(w, err)
			return
		}
		kept := make([]favorite, 0, len(list)+1)
		for _, f := range list {
			if f != in {
				kept = append(kept, f)
			}
		}
		if r.Method == http.MethodPost {
			_, real, err := s.resolve(in.Root, in.Path)
			if err != nil {
				fail(w, err)
				return
			}
			if info, err := os.Stat(real); err != nil || !info.IsDir() {
				fail(w, errNotFound)
				return
			}
			kept = append(kept, in)
		}
		list = kept
		if err := s.saveFavorites(list); err != nil {
			fail(w, err)
			return
		}
	default:
		reply(w, 405, map[string]any{"error": "method-not-allowed"})
		return
	}
	reply(w, 200, map[string]any{"favorites": s.favoriteViews(list)})
}

// ---- uploads

var uploadID = regexp.MustCompile(`^[0-9a-f]{32}$`)

type session struct {
	Root    string `json:"root"`
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Policy  string `json:"policy"`
	Created int64  `json:"created"`
}

func (s *Server) uploadsDir() string        { return filepath.Join(s.cfg.StateDir, "disk-uploads") }
func (s *Server) metaFile(id string) string { return filepath.Join(s.uploadsDir(), id+".json") }
func (s *Server) partFile(id string) string { return filepath.Join(s.uploadsDir(), id+".part") }

func (s *Server) loadSession(id string) (*session, error) {
	data, err := os.ReadFile(s.metaFile(id))
	if err != nil {
		return nil, errNotFound
	}
	var ses session
	if json.Unmarshal(data, &ses) != nil {
		return nil, errNotFound
	}
	return &ses, nil
}

func partSize(path string) int64 {
	if info, err := os.Stat(path); err == nil {
		return info.Size()
	}
	return 0
}

// freeName is "a (1).txt" for "a.txt" when that is taken.
func freeName(dir, name string) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, candidate)); errors.Is(err, fs.ErrNotExist) {
			return candidate
		}
	}
}

func (s *Server) uploads(w http.ResponseWriter, r *http.Request, rest []string) {
	switch {
	case len(rest) == 0 && r.Method == http.MethodPost:
		s.createUpload(w, r)
		return
	case len(rest) >= 1 && uploadID.MatchString(rest[0]):
	default:
		reply(w, 404, map[string]any{"error": "not-found"})
		return
	}
	id := rest[0]
	s.mu.Lock()
	defer s.mu.Unlock()
	ses, err := s.loadSession(id)
	if err != nil {
		if r.Method == http.MethodHead {
			w.WriteHeader(404)
			return
		}
		fail(w, err)
		return
	}
	part := s.partFile(id)
	switch {
	case len(rest) == 1 && r.Method == http.MethodHead:
		w.Header().Set("Upload-Offset", strconv.FormatInt(partSize(part), 10))
		w.Header().Set("Upload-Length", strconv.FormatInt(ses.Size, 10))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(200)
	case len(rest) == 1 && r.Method == http.MethodPut:
		s.appendChunk(w, r, ses, part)
	case len(rest) == 1 && r.Method == http.MethodDelete:
		_ = os.Remove(part)
		_ = os.Remove(s.metaFile(id))
		w.WriteHeader(204)
	case len(rest) == 2 && rest[1] == "complete" && r.Method == http.MethodPost:
		s.finish(w, id, ses, part)
	default:
		reply(w, 404, map[string]any{"error": "not-found"})
	}
}

func (s *Server) createUpload(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Root, Path, Name, OnConflict string
		Size                         int64
	}
	if err := readJSON(r, &in); err != nil {
		fail(w, err)
		return
	}
	if !validName(in.Name) || in.Size < 0 {
		fail(w, refuse(400, "bad-request", ""))
		return
	}
	_, dir, err := s.resolve(in.Root, in.Path)
	if err != nil {
		fail(w, err)
		return
	}
	name := in.Name
	if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
		switch in.OnConflict {
		case "replace":
		case "keep-both":
			name = freeName(dir, name)
		default:
			fail(w, refuse(409, "exists", ""))
			return
		}
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		fail(w, err)
		return
	}
	id := hex.EncodeToString(raw[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.uploadsDir(), 0o700); err != nil {
		fail(w, err)
		return
	}
	meta, _ := json.Marshal(session{Root: in.Root, Path: in.Path, Name: name, Size: in.Size, Policy: in.OnConflict, Created: time.Now().UnixMilli()})
	f, err := os.OpenFile(s.partFile(id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		fail(w, err)
		return
	}
	_ = f.Close()
	if err := os.WriteFile(s.metaFile(id), meta, 0o600); err != nil {
		_ = os.Remove(s.partFile(id))
		fail(w, err)
		return
	}
	reply(w, 201, map[string]any{"uploadId": id, "name": name, "offset": 0, "chunkSize": s.cfg.ChunkSize})
}

func (s *Server) appendChunk(w http.ResponseWriter, r *http.Request, ses *session, part string) {
	have := partSize(part)
	want, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil || want < 0 {
		fail(w, refuse(400, "bad-request", ""))
		return
	}
	if want != have {
		e := refuse(409, "offset-mismatch", "")
		e.extra = map[string]any{"offset": have}
		fail(w, e)
		return
	}
	room := ses.Size - have
	if room > s.cfg.ChunkSize {
		room = s.cfg.ChunkSize
	}
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		fail(w, err)
		return
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, room+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > room {
		_ = os.Truncate(part, have)
		w.Header().Set("Connection", "close")
		fail(w, refuse(413, "chunk-too-large", ""))
		return
	}
	if err != nil {
		_ = os.Truncate(part, have)
		fail(w, err)
		return
	}
	reply(w, 200, map[string]any{"offset": have + n})
}

func (s *Server) finish(w http.ResponseWriter, id string, ses *session, part string) {
	have := partSize(part)
	if have != ses.Size {
		e := refuse(409, "incomplete", "")
		e.extra = map[string]any{"offset": have}
		fail(w, e)
		return
	}
	_, dir, err := s.resolve(ses.Root, ses.Path)
	if err != nil {
		fail(w, err)
		return
	}
	name := ses.Name
	if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
		switch ses.Policy {
		case "replace":
		case "keep-both":
			name = freeName(dir, name)
		default:
			fail(w, refuse(409, "exists", ""))
			return
		}
	}
	target := filepath.Join(dir, name)
	// The staging folder may be on another disk: copy beside the target, then rename, so a reader never sees half a file.
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		fail(w, err)
		return
	}
	src, err := os.Open(part)
	if err == nil {
		_, err = io.Copy(tmp, src)
		_ = src.Close()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), target)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		fail(w, err)
		return
	}
	_ = os.Remove(part)
	_ = os.Remove(s.metaFile(id))
	reply(w, 200, map[string]any{"name": name, "size": ses.Size, "offset": ses.Size})
}
