package design

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// Who may open which project. The platform's gateway owns the grants; it hands this runtime a snapshot of them (the
// agent op design.access) that is kept in <data dir>/design-access.json, so a restart or a gateway that is down does
// not open personal projects to everybody. A runtime that never got a snapshot behaves as before: everything is
// visible.

const AccessFile = "design-access.json"

// Grant is the person's own role in one project, already folded from the gateway's tables.
type Grant struct {
	Role      string     `json:"role"`
	Owner     string     `json:"owner"`
	OwnerName string     `json:"ownerName"`
	Until     *time.Time `json:"until"`
}

// AccessSnapshot is the wire body of design.access without its op: the person's roles by project and the team level of
// every project the gateway knows.
type AccessSnapshot struct {
	Enforce  bool              `json:"enforce"`
	At       time.Time         `json:"at"`
	Projects map[string]Grant  `json:"projects"`
	Team     map[string]string `json:"team"`
}

var (
	accessRoles = map[string]bool{"owner": true, "manage": true, "edit": true, "comment": true, "view": true}
	teamLevels  = map[string]bool{"none": true, "view": true, "comment": true, "edit": true}
)

const (
	accessMaxEntries   = 10000
	accessMaxOwner     = 254
	accessMaxOwnerName = 64
)

// ErrStaleAccess: the snapshot is older than the one held; an out-of-order relay must not roll the roles back.
var ErrStaleAccess = errors.New("stale access snapshot")

func cleanLine(v string, max int) bool {
	if utf8.RuneCountInString(v) > max {
		return false
	}
	for _, r := range v {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// accessMaxAhead: a posted snapshot dated further ahead than this of the runtime clock is kept with its time clamped
// to it. A wrong far-future "at" would otherwise make every honest snapshot after it look stale; refusing it would
// leave the old roles in force, and a kept file is never dropped for its time (that would open everything on a
// restart with the clock still behind).
const accessMaxAhead = 5 * time.Minute

func (l *Library) clock() time.Time {
	if l.now != nil {
		return l.now()
	}
	return time.Now()
}

func (l *Library) clampAhead(s AccessSnapshot) AccessSnapshot {
	if limit := l.clock().Add(accessMaxAhead); s.At.After(limit) {
		l.logAccess("design access snapshot dated ahead of the clock, time clamped", errors.New(s.At.Format(time.RFC3339)))
		s.At = limit
	}
	return s
}

// Validate refuses a snapshot the runtime must not act on: no time (an empty or garbled body decodes to the zero
// snapshot, which would switch enforcement off), unknown roles or levels, foreign ids, oversized text or tables.
func (s AccessSnapshot) Validate() error {
	if s.At.IsZero() {
		return errors.New("snapshot has no time")
	}
	if len(s.Projects) > accessMaxEntries || len(s.Team) > accessMaxEntries {
		return errors.New("too many projects")
	}
	for id, g := range s.Projects {
		if !IsLibraryID(id) || !accessRoles[g.Role] || !cleanLine(g.Owner, accessMaxOwner) || !cleanLine(g.OwnerName, accessMaxOwnerName) {
			return errors.New("bad grant for " + id)
		}
	}
	for id, level := range s.Team {
		if !IsLibraryID(id) || !teamLevels[level] {
			return errors.New("bad team level for " + id)
		}
	}
	return nil
}

type accessState struct {
	snap    AccessSnapshot
	have    bool
	claimed map[string]bool // projects the gateway was told this runtime owns (design.claim)
	// projects the gateway was told again of after its snapshot had no row for them (design.created)
	announced map[string]bool
}

func (l *Library) accessPath() string {
	if l.cfg.DataDir == "" {
		return ""
	}
	return filepath.Join(l.cfg.DataDir, AccessFile)
}

// loadAccess reads the kept snapshot at start; a malformed or unreadable file means no snapshot, as before.
func (l *Library) loadAccess() {
	file := l.accessPath()
	if file == "" {
		return
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		if !os.IsNotExist(err) {
			l.logAccess("design access file not read", err)
		}
		return
	}
	var s AccessSnapshot
	if err := json.Unmarshal(raw, &s); err != nil {
		l.logAccess("design access file ignored", err)
		return
	}
	if err := s.Validate(); err != nil {
		l.logAccess("design access file ignored", err)
		return
	}
	l.access.snap, l.access.have = l.clampAhead(s), true
}

func (l *Library) logAccess(msg string, err error) {
	agent.Log("warn", msg, agent.F{K: "module", V: "design-library"}, agent.F{K: "error", V: err.Error()})
}

// SetAccess takes a snapshot as the current one and keeps it on disk (a temp file, synced, renamed over the old one).
// The snapshot is in force in memory even when the disk refuses; the error says the restart would lose it.
func (l *Library) SetAccess(s AccessSnapshot) error {
	l.accessWrite.Lock()
	defer l.accessWrite.Unlock()
	return l.setAccess(s)
}

// SetAccessOrdered is SetAccess that refuses (ErrStaleAccess) a snapshot made before the one held.
func (l *Library) SetAccessOrdered(s AccessSnapshot) error {
	l.accessWrite.Lock()
	defer l.accessWrite.Unlock()
	if cur, ok := l.Access(); ok && s.At.Before(cur.At) {
		return ErrStaleAccess
	}
	return l.setAccess(s)
}

// setAccess runs under accessWrite, so what is on disk is always what is in memory.
func (l *Library) setAccess(s AccessSnapshot) error {
	if s.Projects == nil {
		s.Projects = map[string]Grant{}
	}
	if s.Team == nil {
		s.Team = map[string]string{}
	}
	l.mu.Lock()
	l.access.snap, l.access.have = s, true
	l.mu.Unlock()
	file := l.accessPath()
	if file == "" {
		return nil
	}
	l.WriteRootHint()
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(file), "."+AccessFile+"."+randomHex(6)+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(temp, file)
	}
	if err != nil {
		_ = os.Remove(temp)
	}
	return err
}

// Access is the current snapshot; false while the runtime has none.
func (l *Library) Access() (AccessSnapshot, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.access.snap, l.access.have
}

// EmitDesignEvent tells the cluster, through the machine agent, that a project was made here (kind "design.created")
// or that this runtime's owner file stands over a project the cluster has no row for ("design.claim"). It never waits
// and never fails the caller: with no agent running the event is simply not sent.
func (l *Library) EmitDesignEvent(kind, project string) {
	agent.EmitDesignEvent(l.cfg.DataDir, l.cfg.AgentTokenHeader, kind, project)
}

const accessBodyMax = 4 << 20

// Access route: the gateway's snapshot, relayed by the machine agent. Nobody else may post it.
func (a *API) PostAccess(r *http.Request) Answer {
	h := noStore()
	if a.MachineAgent == nil || !a.MachineAgent(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, accessBodyMax+1))
	var s AccessSnapshot
	if err != nil || len(raw) > accessBodyMax || json.Unmarshal(raw, &s) != nil || s.Validate() != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("A bad access snapshot", "Некорректный снимок доступа")), h)
	}
	if err := a.L.SetAccessOrdered(a.L.clampAhead(s)); err != nil {
		if errors.Is(err, ErrStaleAccess) {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The access snapshot is older than the one held", "Снимок доступа старее текущего")), h)
		}
		if a.Log != nil {
			a.Log("warn", "design access not kept", err)
		}
		return JSON(r, http.StatusInternalServerError, errorBody("Internal Server Error"), h)
	}
	go a.L.ReannounceOwn()
	return JSON(r, http.StatusOK, map[string]bool{"ok": true}, h)
}
