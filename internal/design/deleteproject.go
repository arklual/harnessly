package design

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// DeleteProject moves a project's folder to the library's .trash/, whole: nothing is erased, and the folder can be put
// back by hand. The library's server, the guest links and the listing stop knowing the project at once; the gateway is
// told, so the project's access row and its links go too.
func (l *Library) DeleteProject(id string) error {
	if !IsLibraryID(id) {
		return &ProjectDeleteError{Msg: locale.Pick("Unknown design", "Неизвестный макет"), Status: http.StatusBadRequest}
	}
	m := l.lockOf("project\x00" + id)
	m.Lock()
	defer m.Unlock()
	defer l.listing.invalidatePath(id)
	if l.ReadArtifact(id) == nil {
		return &ProjectDeleteError{Msg: locale.Pick("The design is gone", "Макета больше нет"), Status: http.StatusNotFound}
	}
	trash := filepath.Join(l.Root(), ".trash")
	if err := os.MkdirAll(trash, 0o777); err != nil {
		return err
	}
	if err := os.Rename(l.ArtifactDir(id), filepath.Join(trash, id+"-"+strconv.FormatInt(nowMs(), 10))); err != nil {
		return err
	}
	l.dropFromAccess(id)
	l.noteDeleted(id)
	l.emit(agent.DesignDeleted, id)
	return nil
}

// trashedProject is the name of a project's folder in .trash/: the id and the moment it went.
var trashedProject = regexp.MustCompile(`^([a-z0-9][a-z0-9-]{0,62})-[0-9]{10,}$`)

// trashedProjectIDs are the ids of the projects in .trash/ that are not in the library now (a folder replaced by an import
// with force leaves its old self there under the id it still has).
func (l *Library) trashedProjectIDs() map[string]bool {
	out := map[string]bool{}
	entries, _ := os.ReadDir(filepath.Join(l.Root(), ".trash"))
	for _, e := range entries {
		m := trashedProject.FindStringSubmatch(e.Name())
		if !e.IsDir() || m == nil || !IsLibraryID(m[1]) {
			continue
		}
		if _, err := os.Lstat(l.ArtifactDir(m[1])); err == nil {
			continue
		}
		out[m[1]] = true
	}
	return out
}

// dropFromAccess takes the project out of the snapshot held, so its row does not outlive the folder here.
func (l *Library) dropFromAccess(id string) {
	l.accessWrite.Lock()
	defer l.accessWrite.Unlock()
	snap, ok := l.Access()
	if !ok {
		return
	}
	projects := make(map[string]Grant, len(snap.Projects))
	for k, v := range snap.Projects {
		if k != id {
			projects[k] = v
		}
	}
	team := make(map[string]string, len(snap.Team))
	for k, v := range snap.Team {
		if k != id {
			team[k] = v
		}
	}
	snap.Projects, snap.Team = projects, team
	if err := l.setAccess(snap); err != nil {
		l.logAccess("design access not kept after a delete", err)
	}
}

// announceDeletedAfter is how long a delete notice waits for the snapshot to lose the project before it is sent again.
const announceDeletedAfter = 10 * time.Minute

func (l *Library) noteDeleted(id string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.access.deletedAt == nil {
		l.access.deletedAt = map[string]time.Time{}
	}
	l.access.deletedAt[id] = l.clock()
}

// ReannounceDeleted sends the delete notice again for a trashed project the gateway's snapshot still lists: the first
// notice can be lost (no agent, no link). The snapshot not naming the project any more is the gateway's confirmation.
func (l *Library) ReannounceDeleted() {
	snap, ok := l.Access()
	if !ok {
		return
	}
	now := l.clock()
	for id := range l.trashedProjectIDs() {
		_, inProjects := snap.Projects[id]
		_, inTeam := snap.Team[id]
		if !inProjects && !inTeam {
			continue
		}
		l.mu.Lock()
		last, seen := l.access.deletedAt[id]
		due := !seen || now.Sub(last) >= announceDeletedAfter
		if due {
			if l.access.deletedAt == nil {
				l.access.deletedAt = map[string]time.Time{}
			}
			l.access.deletedAt[id] = now
		}
		l.mu.Unlock()
		if due {
			l.emit(agent.DesignDeleted, id)
		}
	}
}

// ProjectDeleteError is a refusal to delete, with the status to answer.
type ProjectDeleteError struct {
	Msg    string
	Status int
}

func (e *ProjectDeleteError) Error() string { return e.Msg }

// DeleteProject is POST {project}: the project goes to the trash. Only its owner may.
func (a *API) DeleteProject(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	project, _ := strField(body, "project")
	if !IsLibraryID(project) {
		return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), noStore())
	}
	if no := a.gateDelete(r, project); no != nil {
		return *no
	}
	if err := a.L.DeleteProject(project); err != nil {
		if de, ok := err.(*ProjectDeleteError); ok {
			return JSON(r, de.Status, errorBody(de.Msg), noStore())
		}
		a.warn("design project not deleted", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not delete the project", "Не удалось удалить проект")), noStore())
	}
	return JSON(r, 200, map[string]bool{"ok": true}, noStore())
}

// gateDelete lets the project's owner delete it: by the snapshot's role, and with no snapshot that restricts, by the
// owner file naming this runtime's person (everybody "edits" then, so the role says nothing).
func (a *API) gateDelete(r *http.Request, project string) *Answer {
	if a.L.Enforcing() {
		return a.gate(r, project, RoleOwner)
	}
	who, _ := a.L.Identity()
	if o, ok := a.L.ReadOwner(project); ok && o.Mine(who) {
		return nil
	}
	ans := JSON(r, http.StatusForbidden, errorBody(locale.Pick("Only the owner can delete the project", "Удалить проект может только владелец")), noStore())
	return &ans
}
