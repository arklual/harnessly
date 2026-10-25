package design

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The role of the runtime's person in a project, and where the runtime asks for it. The gateway's snapshot (see
// accessfile.go) says who may do what; a runtime without a snapshot, or with one that does not enforce, lets everybody
// do everything, as before. Enforcement is at every entry that takes a project id from a request: the JSON routes
// (API.gate), the library server (refuseBoard), the v2 designly service and the agent hook (guard.go).

// Role is what a person may do in a project; "" is nothing, the project is not theirs to see.
type Role string

const (
	RoleNone    Role = ""
	RoleView    Role = "view"
	RoleComment Role = "comment"
	RoleEdit    Role = "edit"
	RoleManage  Role = "manage"
	RoleOwner   Role = "owner"
)

var roleRank = map[Role]int{RoleView: 1, RoleComment: 2, RoleEdit: 3, RoleManage: 4, RoleOwner: 5}

// AtLeast: the role is one of min or stronger; no role is never enough.
func (r Role) AtLeast(min Role) bool { return roleRank[r] > 0 && roleRank[r] >= roleRank[min] }

func maxRole(a, b Role) Role {
	if roleRank[b] > roleRank[a] {
		return b
	}
	return a
}

func teamRole(level string) Role {
	switch Role(level) {
	case RoleView, RoleComment, RoleEdit:
		return Role(level)
	}
	return RoleNone
}

// RoleOf is who's role in a project at now:
//  1. no snapshot, or one that does not enforce: edit (as before this existed; an old gateway never sends one);
//  2. the person's own grant in the snapshot while its until is ahead of now, the stronger of it and the team level
//     (a grant that ran out is not looked at, the next snapshot is not waited for);
//  3. the project has no row in the team table and its .owner.json names this runtime: owner (so a project just
//     made, which the gateway has not registered yet, never hides from its maker);
//  4. the team level of the snapshot;
//  5. a project the snapshot has no row for and whose owner file is not v2 "private": edit (what existed before);
//  6. nothing.
func (l *Library) RoleOf(id string, who Identity, now time.Time) Role {
	snap, ok := l.Access()
	if !ok || !snap.Enforce {
		return RoleEdit
	}
	level, hasTeam := snap.Team[id]
	g, hasGrant := snap.Projects[id]
	if hasGrant && (g.Until == nil || g.Until.After(now)) {
		return maxRole(Role(g.Role), teamRole(level))
	}
	owner, haveOwner := l.ReadOwner(id)
	// The owner file speaks only before the gateway has a row for the project (a fresh create, an unclaimed one):
	// afterwards the owner is whoever the snapshot grants, so a former owner does not stay one for good.
	if haveOwner && !hasTeam && owner.Mine(who) {
		return RoleOwner
	}
	if r := teamRole(level); r != RoleNone {
		return r
	}
	if hasGrant || hasTeam {
		return RoleNone
	}
	if haveOwner && owner.Private {
		return RoleNone
	}
	return RoleEdit
}

// RoleNow is RoleOf for the runtime's own person at the runtime's clock. An identity that cannot be told matches no
// owner file, so the project falls to the grant and team rules.
func (l *Library) RoleNow(id string) Role {
	who, _ := l.Identity()
	return l.RoleOf(id, who, l.clock())
}

// Can: the person may do what min stands for in the project.
func (l *Library) Can(id string, min Role) bool { return l.RoleNow(id).AtLeast(min) }

// Enforcing: a snapshot that restricts is held.
func (l *Library) Enforcing() bool {
	s, ok := l.Access()
	return ok && s.Enforce
}

// VisibleTo keeps the projects the person has a role in; the owner always sees what is theirs.
func (l *Library) VisibleTo(arts []*Artifact) []*Artifact {
	if !l.Enforcing() {
		return arts
	}
	who, _ := l.Identity()
	now := l.clock()
	out := make([]*Artifact, 0, len(arts))
	for _, a := range arts {
		if l.RoleOf(a.ID, who, now) != RoleNone {
			out = append(out, a)
		}
	}
	return out
}

// NoAccess is what a refusal says about the project's owner: only a name and, when the snapshot knows it, the address.
type NoAccess struct {
	Owner     string
	OwnerName string
}

// NoAccessOf: who to ask. The snapshot has the owner of the projects the person holds or held a grant in; for others
// the owner file's name is all there is.
func (l *Library) NoAccessOf(id string) NoAccess {
	var out NoAccess
	if s, ok := l.Access(); ok {
		if g, ok := s.Projects[id]; ok {
			out = NoAccess{Owner: g.Owner, OwnerName: g.OwnerName}
		}
	}
	if out.OwnerName == "" {
		if o, ok := l.ReadOwner(id); ok {
			out.OwnerName = o.Name
		}
	}
	return out
}

// NoAccessText is the words for a person or an agent that may not do something in a project. A person who sees the
// project but lacks the level hears its title and whom to ask; a person with no role gets the plain words, which name
// neither the title nor the owner (that would tell the project is there).
func (l *Library) NoAccessText(id string, role Role) string {
	if role == RoleNone {
		return locale.Pick("No access to the project", "Нет доступа к проекту")
	}
	title := id
	if a := l.ReadArtifact(id); a != nil && a.Title != "" {
		title = a.Title
	}
	text := locale.Pick("No access to the project «"+title+"»", "Нет доступа к проекту «"+title+"»")
	if name := l.NoAccessOf(id).OwnerName; name != "" {
		text += locale.Pick(": ask the owner — "+name, ": попросите владельца — "+name)
	}
	return text
}

// gate answers when the person may not do what min stands for in the project; nil to go on. With no role at all the
// answer is the one for a project that does not exist (404, nothing of owner or title); with a role that is too low
// it is 403 with whom to ask. A value that is not a project id is left to the route's own refusal.
func (a *API) gate(r *http.Request, project string, min Role) *Answer {
	if !IsLibraryID(project) {
		return nil
	}
	role := a.L.RoleNow(project)
	if role.AtLeast(min) {
		return nil
	}
	if role == RoleNone {
		ans := JSON(r, http.StatusNotFound, errorBody(locale.Pick("The design is gone", "Макета больше нет")), noStore())
		return &ans
	}
	who := a.L.NoAccessOf(project)
	ans := JSON(r, http.StatusForbidden, struct {
		Error     string `json:"error"`
		Code      string `json:"code"`
		Owner     string `json:"owner"`
		OwnerName string `json:"ownerName"`
		Role      string `json:"role,omitempty"`
	}{locale.Pick("No access to the project", "Нет доступа к проекту"), "no_access", who.Owner, who.OwnerName, string(role)}, noStore())
	return &ans
}

// gateAny is gate for a project taken from a body or a query as it came.
func (a *API) gateAny(r *http.Request, project any, min Role) *Answer {
	if s, ok := project.(string); ok {
		return a.gate(r, s, min)
	}
	return nil
}

// refuseBoard answers a library server request for a project the person may not open with the not-found page and says
// true. The page names nothing of the project.
func (l *Library) refuseBoard(w http.ResponseWriter, ref string, head bool) bool {
	if !IsLibraryID(ref) || l.Can(ref, RoleView) {
		return false
	}
	// The same answer as for an id that does not exist: the server does not say which ids are there.
	sendHTML(w, http.StatusNotFound, l.notFoundPage(), head)
	return true
}

// AnnounceNew is what a runtime does about a project it just made: the owner file (personal) and the news for the
// gateway, which then registers the person as the owner.
func (l *Library) AnnounceNew(id string) {
	if err := l.SetOwner(id); err != nil && l.cfg.Log != nil {
		l.cfg.Log("warn", "design: the owner of a new project was not recorded", err)
	}
	l.emit(agent.DesignCreated, id)
}

func (l *Library) emit(kind, project string) {
	if l.emitFn != nil {
		l.emitFn(kind, project)
		return
	}
	l.EmitDesignEvent(kind, project)
}

// ClaimOnce: this runtime's v1 owner file stands over a project the snapshot has no row for; the gateway is told so,
// once for the life of the runtime, and registers the person as the owner of it. Without a snapshot nothing is sent
// (that gateway does not know the event).
func (l *Library) ClaimOnce(id string, owner Owner, who Identity) {
	if owner.V >= 2 || !owner.Mine(who) {
		return
	}
	snap, ok := l.Access()
	if !ok {
		return
	}
	if _, has := snap.Projects[id]; has {
		return
	}
	if _, has := snap.Team[id]; has {
		return
	}
	l.mu.Lock()
	if l.access.claimed == nil {
		l.access.claimed = map[string]bool{}
	}
	done := l.access.claimed[id]
	l.access.claimed[id] = true
	l.mu.Unlock()
	if !done {
		l.emit(agent.DesignClaim, id)
	}
}

// ReannounceOwn tells the gateway again of the projects this runtime made (a v2 owner file that is mine) and that the
// snapshot has no row for: the first news can have been dropped (no agent, no link, a gateway that did not know the
// event yet), and without a row the person could never share the project. The gateway registers idempotently. Once
// per project for the life of the runtime; a new snapshot is the proof that the link is up.
func (l *Library) ReannounceOwn() {
	snap, ok := l.Access()
	if !ok {
		return
	}
	who, _ := l.Identity()
	for _, a := range l.ListArtifacts() {
		id := a.ID
		if _, has := snap.Projects[id]; has {
			continue
		}
		if _, has := snap.Team[id]; has {
			continue
		}
		owner, have := l.ReadOwner(id)
		if !have || owner.V < 2 || !owner.Mine(who) {
			continue
		}
		l.mu.Lock()
		if l.access.announced == nil {
			l.access.announced = map[string]bool{}
		}
		done := l.access.announced[id]
		l.access.announced[id] = true
		l.mu.Unlock()
		if !done {
			l.emit(agent.DesignCreated, id)
		}
	}
}

// historyTarget is the project a panel history request names: the query for a read, the body for a restore.
func historyTarget(r *http.Request, body *jsvalue.Object) any {
	if r.Method == http.MethodGet || body == nil {
		return QueryString(r, "artifact")
	}
	return Field(body, "artifact")
}

// visibleClaims drops the claim lines of projects the person has no role in.
func (l *Library) visibleClaims(lines []ClaimLine) []ClaimLine {
	if !l.Enforcing() {
		return lines
	}
	out := make([]ClaimLine, 0, len(lines))
	for _, c := range lines {
		if l.Can(c.Project, RoleView) {
			out = append(out, c)
		}
	}
	return out
}

// denyImage answers a picture request for a project the person may not see (an empty 404, as for a missing one) and
// says true.
func (a *API) denyImage(w http.ResponseWriter, project string) bool {
	if !IsLibraryID(project) || a.L.Can(project, RoleView) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	return true
}

// LibraryRootFile names, in the data folder, the library the runtime serves: the hook runs in an agent's pane, which
// knows the data folder and nothing else, and needs the root to see a search that starts at or above it.
const LibraryRootFile = "design-library-root"

// WriteRootHint keeps the library root where the hook finds it.
func (l *Library) WriteRootHint() {
	if l.cfg.DataDir == "" {
		return
	}
	_ = os.MkdirAll(l.cfg.DataDir, 0o700)
	if _, err := writeIfChanged(filepath.Join(l.cfg.DataDir, LibraryRootFile), []byte(l.Root()+"\n")); err != nil && l.cfg.Log != nil {
		l.cfg.Log("warn", "design library root not kept", err)
	}
}

func (l *Library) libraryRootHint() string {
	raw, err := os.ReadFile(filepath.Join(l.cfg.DataDir, LibraryRootFile))
	if err != nil || len(raw) > 4096 {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
