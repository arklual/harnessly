// Package design is the design library of a machine: projects of a design tool kept as plain folders, listed, served
// to the right panel's browser by a small loopback server, checked in a headless browser, claimed board by board by
// the agents that edit them and saved to a private history. The folder can be shared by several runtimes (several
// Unix users) and is written by every agent, so everything read from it is untrusted.
package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	ProjectDirname = "project"
	ArtifactFile   = "artifact.json"
)

// Texts are the words of the deployment the library speaks to people and agents with.
type Texts struct {
	// CLI is the command agents run (`<cli> design check …`).
	CLI string
	// Host is the platform's address people open on a phone.
	Host string
	// AppName names the platform in a dialog's instructions.
	AppName string
	// Tag names what this runtime writes into shared places: the history's author and marker, the rules file's marker,
	// a check's temporary profile.
	Tag string
	// HistoryEmail is the history's author address.
	HistoryEmail string
	// SharedDir is where the team's shared library lives on the platform's machines, named in the rules file.
	SharedDir string
	// KitGlobal, PagesGlobal and ArgsGlobal are the window globals of a project's UI kit, its page renderer and the
	// renderer's arguments.
	KitGlobal, PagesGlobal, ArgsGlobal string
	// People maps the local part of the owner's address to the name a claim shows.
	People map[string]string
}

// Config is what the deployment names.
type Config struct {
	DataDir string
	// ConfigFile is the runtime's config (the owner's address).
	ConfigFile string
	// LibraryDir is an explicit library folder; WorkspaceDir (or the workspace root of WorkspaceConfig) holds
	// design-artifacts otherwise; the data dir is the last resort.
	LibraryDir, WorkspaceDir, AltWorkspaceDir, WorkspaceConfig string
	// HistoryDir holds the private history repositories (the data dir when empty).
	HistoryDir string
	// Port is the library server's preferred loopback port.
	Port int
	// Chrome is the headless browser a check drives; the usual places are looked at when empty.
	Chrome string
	// PlatformAPI prefixes the platform's routes (a check's screenshot address).
	PlatformAPI string
	// ClaudeProjects and CodexSessions are the folders whose transcripts may be read.
	ClaudeProjects, CodexSessions string
	// OwnerEmail is the owner when the runtime's config names none.
	OwnerEmail string
	Texts      Texts
	// Log hears what goes wrong in the background.
	Log func(level, msg string, err error)
}

// Library is one runtime's view of its library: its caches, its server and its background work.
type Library struct {
	cfg Config

	mu       sync.Mutex
	locks    map[string]*sync.Mutex
	listing  listingCache
	srv      *server
	bg       background
	bgStarts sync.WaitGroup
	live     liveState
	tabs     func() ([]Chat, bool)
	identity identityCache
	hist     historyState
	modes    modesState
	sources  sourcesState
	texts    sync.Map
	osUser   func() string
}

func New(cfg Config) *Library {
	if cfg.Port == 0 {
		cfg.Port = 18470
	}
	if cfg.Texts.Tag == "" {
		cfg.Texts.Tag = "design"
	}
	l := &Library{cfg: cfg, locks: map[string]*sync.Mutex{}}
	l.live.init()
	return l
}

func (l *Library) Config() Config { return l.cfg }

// lockOf is a mutex per key: the edits of one project queue instead of racing.
func (l *Library) lockOf(key string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.locks[key]
	if m == nil {
		m = &sync.Mutex{}
		l.locks[key] = m
	}
	return m
}

func (l *Library) configuredWorkspaceRoot() string {
	if l.cfg.WorkspaceConfig == "" {
		return ""
	}
	b, err := os.ReadFile(l.cfg.WorkspaceConfig)
	if err != nil {
		return ""
	}
	var raw map[string]any
	if json.Unmarshal(b, &raw) != nil {
		return ""
	}
	root, _ := raw["workspace_root"].(string)
	if strings.HasPrefix(root, "/") {
		return root
	}
	return ""
}

// Root is the one folder of all design projects.
func (l *Library) Root() string {
	if l.cfg.LibraryDir != "" {
		abs, err := filepath.Abs(l.cfg.LibraryDir)
		if err != nil {
			return filepath.Clean(l.cfg.LibraryDir)
		}
		return abs
	}
	ws := l.cfg.WorkspaceDir
	if ws == "" {
		ws = l.cfg.AltWorkspaceDir
	}
	if ws == "" {
		ws = l.configuredWorkspaceRoot()
	}
	if ws != "" {
		return filepath.Join(ws, "design-artifacts")
	}
	return filepath.Join(l.cfg.DataDir, "design-artifacts")
}

func (l *Library) ArtifactDir(id string) string { return filepath.Join(l.Root(), id) }
func (l *Library) ProjectDir(id string) string {
	return filepath.Join(l.ArtifactDir(id), ProjectDirname)
}

// ProjectDirOf is the folder an artifact's pages are served from; "" when the ref is not one.
func (l *Library) ProjectDirOf(ref string) string {
	if !IsLibraryID(ref) {
		return ""
	}
	return l.ProjectDir(ref)
}

var contentTypes = map[string]string{
	".html": "text/html; charset=utf-8", ".htm": "text/html; charset=utf-8",
	".js": "text/javascript; charset=utf-8", ".mjs": "text/javascript; charset=utf-8", ".jsx": "text/javascript; charset=utf-8",
	".css": "text/css; charset=utf-8", ".json": "application/json; charset=utf-8",
	".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif",
	".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon",
	".woff": "font/woff", ".woff2": "font/woff2", ".ttf": "font/ttf", ".otf": "font/otf",
	".txt": "text/plain; charset=utf-8",
}

// ContentType of a served extension; "" for one a design does not need.
func ContentType(ext string) string { return contentTypes[strings.ToLower(ext)] }

// extname is node's path.extname: from the last dot of the base name, none for a leading dot alone.
func extname(p string) string {
	base := p[strings.LastIndex(p, "/")+1:]
	i := strings.LastIndex(base, ".")
	if i <= 0 {
		return ""
	}
	return base[i:]
}

// isInside: child is root or lies under it.
func isInside(child, root string) bool {
	if child == root {
		return true
	}
	if !strings.HasSuffix(root, string(filepath.Separator)) {
		root += string(filepath.Separator)
	}
	return strings.HasPrefix(child, root)
}

// ResolveProjectFile is a requested file as a real path confined to the artifact's project folder, or "" when the
// ref is not one, the path escapes (.., a link) or the extension is not something a design needs.
func (l *Library) ResolveProjectFile(ref string, segments []string) string {
	dir := l.ProjectDirOf(ref)
	if dir == "" || len(segments) == 0 {
		return ""
	}
	for _, s := range segments {
		if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "\\\x00") {
			return ""
		}
	}
	if ContentType(extname(segments[len(segments)-1])) == "" {
		return ""
	}
	requested := filepath.Join(append([]string{dir}, segments...)...)
	if rel, err := filepath.Rel(dir, requested); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	real, err := filepath.EvalSymlinks(requested)
	if err != nil {
		return ""
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ""
	}
	if real != realDir && !strings.HasPrefix(real, realDir+string(filepath.Separator)) {
		return ""
	}
	st, err := os.Stat(real)
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return real
}

func (l *Library) warn(msg string, err error) {
	if l.cfg.Log != nil {
		l.cfg.Log("warn", msg, err)
	}
}
