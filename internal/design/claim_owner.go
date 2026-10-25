package design

import (
	"net/http"
	"os"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// ClaimOwnerless makes this runtime's person the owner of a project nobody owns: it exists, has no owner file and the
// snapshot has no row for it. The owner file written is v1 (the gateway then registers the owner on design.claim).
func (l *Library) ClaimOwnerless(id string) error {
	if !IsLibraryID(id) || l.ReadArtifact(id) == nil {
		return &OwnerClaimError{Msg: locale.Pick("There is no such project", "Такого проекта нет"), Status: http.StatusNotFound}
	}
	if s, ok := l.Access(); ok {
		_, inProjects := s.Projects[id]
		_, inTeam := s.Team[id]
		if inProjects || inTeam {
			return &OwnerClaimError{Msg: locale.Pick("The project already has an owner", "У проекта уже есть владелец"), Status: http.StatusConflict}
		}
	}
	who, err := l.Identity()
	if err != nil {
		return err
	}
	file := l.ownerPath(id)
	body := append(jsvalue.Stringify(jsvalue.NewObject("v", 1, "user", who.Runtime, "name", who.Person), "  "), '\n')
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return &OwnerClaimError{Msg: locale.Pick("The project already has an owner", "У проекта уже есть владелец"), Status: http.StatusConflict}
		}
		return err
	}
	_, err = f.Write(body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(file)
		return err
	}
	l.emit(agent.DesignClaim, id)
	return nil
}

// OwnerClaimError is a refusal to take ownership, with the status to answer.
type OwnerClaimError struct {
	Msg    string
	Status int
}

func (e *OwnerClaimError) Error() string { return e.Msg }

// ClaimProject is POST {project}: take ownership of an ownerless project.
func (a *API) ClaimProject(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	project, _ := strField(body, "project")
	if !IsLibraryID(project) {
		return JSON(r, 400, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), noStore())
	}
	if err := a.L.ClaimOwnerless(project); err != nil {
		if ce, ok := err.(*OwnerClaimError); ok {
			return JSON(r, ce.Status, errorBody(ce.Msg), noStore())
		}
		a.warn("design owner not taken", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not take the project", "Не удалось взять проект")), noStore())
	}
	return JSON(r, 200, map[string]bool{"ok": true}, noStore())
}
