// Package state keeps the runtime's workspace list (`workspaces.json`) and each workspace's layout
// (`workspaces/<id>/layout.json`): reads with their normalisation, atomic writes in the bytes the previous runtime
// wrote, the commit of every change to the cluster when the cluster owns the pair's state, the changes the cluster
// makes, and the tree and tab operations behind the layout and workspace routes.
//
// Documents are jsvalue objects, so fields this package does not model survive every write in their place.
package state

import (
	"crypto/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/machine"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

// Tmux is what the stores ask of tmux. FromTmux adapts the runtime's own.
type Tmux interface {
	CreateSession(name string, cols, rows int, cwd string) error
	KillSession(name string)
	HasSession(name string) bool
	SendKeys(name, line string) error
	ListSessions() []string
	Names() tmux.Names
	SessionName(wsID, paneID, tabID string) string
	ParseSessionName(name string) (wsID, paneID, tabID string, ok bool)
	ResolveExistingDir(preferred string) string
	// SessionCwd is the folder of the session's shell, else tmux's idea of the pane, "" when neither is known.
	SessionCwd(name string) string
	PaneCurrentCommand(name string) string
}

type tmuxAdapter struct{ *tmux.Tmux }

func (t tmuxAdapter) SessionCwd(name string) string { return machine.SessionDir(t.Tmux, name) }

// FromTmux is the runtime's tmux layer as the stores use it.
func FromTmux(t *tmux.Tmux) Tmux { return tmuxAdapter{t} }

// Broadcaster tells the pages that a layout or the workspace list changed (sync events `layout` and `workspace`).
type Broadcaster interface {
	Layout(wsID string)
	Workspaces()
}

// Reconciler keeps the tab statuses in step with the layouts: after a write, the tabs a workspace still has; after a
// layout is removed, none.
type Reconciler interface {
	ReconcileWorkspaceTabs(wsID string, tabIDs []string)
	RemoveWorkspaceTabs(wsID string)
}

// Prompts writes the agent CLIs' per-workspace prompt files after a workspace is created, renamed or changes folders.
type Prompts interface {
	WriteWorkspacePrompts(ws *jsvalue.Object)
}

// Config is where the stores live and what they tell.
type Config struct {
	DataDir    string
	Tmux       Tmux
	Broadcast  Broadcaster
	Reconciler Reconciler
	Prompts    Prompts
	// AgentTokenHeader carries the token of agent.json on the machine agent's loopback state routes.
	AgentTokenHeader string
	// MetaKey is the tab field holding the platform's own metadata (launch choice, mode).
	MetaKey string
	// Home is the account's home: a new workspace's folder when none is given, the folder of the migrated layout.
	Home string
	// Now and NewID are the clock and the six-character id source; tests make them predictable.
	Now   func() time.Time
	NewID func() string
	// HTTP calls the machine agent; its own timeout is the commit timeout.
	HTTP *http.Client
}

// Store holds the locks and caches of both stores. Use one per data directory.
type Store struct {
	cfg Config

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex // one per workspace layout
	wsLock  sync.Mutex             // workspaces.json

	cacheMu       sync.Mutex
	layoutCache   map[string]string
	workspacesKey *string

	modeMu sync.Mutex
	mode   modeCache
}

const commitTimeout = 6 * time.Second

// New opens the stores of cfg.DataDir.
func New(cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.NewID == nil {
		cfg.NewID = func() string { return nanoid(6) }
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: commitTimeout}
	}
	return &Store{cfg: cfg, locks: map[string]*sync.Mutex{}, layoutCache: map[string]string{}}
}

// Error is a refusal with the status and message the route answers.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return e.Message }

func notFound(msg string) error   { return &Error{Status: http.StatusNotFound, Message: msg} }
func badRequest(msg string) error { return &Error{Status: http.StatusBadRequest, Message: msg} }

var workspaceIDRe = regexp.MustCompile(`^[\w-]+$`)

// ValidWorkspaceID says whether id can name a workspace folder; anything else could leave the data directory.
func ValidWorkspaceID(id string) bool { return workspaceIDRe.MatchString(id) }

func (s *Store) layoutLock(wsID string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	m := s.locks[wsID]
	if m == nil {
		m = &sync.Mutex{}
		s.locks[wsID] = m
	}
	return m
}

func (s *Store) now() string {
	return s.cfg.Now().UTC().Format("2006-01-02T15:04:05.000Z")
}

func (s *Store) broadcastLayout(wsID string) {
	if s.cfg.Broadcast != nil {
		s.cfg.Broadcast.Layout(wsID)
	}
}

func (s *Store) broadcastWorkspaces() {
	if s.cfg.Broadcast != nil {
		s.cfg.Broadcast.Workspaces()
	}
}

func (s *Store) writePrompts(ws *jsvalue.Object) {
	if s.cfg.Prompts != nil {
		s.cfg.Prompts.WriteWorkspacePrompts(ws)
	}
}

// nanoid is n random characters of the URL-safe alphabet.
func nanoid(n int) string {
	const alphabet = "useandom-26T198340PX75pxJACKVERYMINDBUSHWOLF_GQZbfghjklqvwyzrict"
	b := make([]byte, n)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[b[i]&63]
	}
	return string(b)
}
