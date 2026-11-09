package design

import (
	"errors"
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The routes of kit versions and project versions (platform API, slice "design"):
//
//	GET  design/kits/state?kit=[&changes=0]        the kit's versions, draft (with its changed files) and projects
//	POST design/kits/draft {kit, action}           action "start" makes the draft, "discard" puts the kit back
//	POST design/kits/publish {kit, part, notes}    the draft becomes the next version (part: patch | minor | major)
//	POST design/kits/settings {kit, readOnly}      the kit's maker makes it for viewing only for everyone, or not
//	POST design/kits/adopt {kit, dryRun}           projects with a copy of the kit's themes move onto its version
//	GET  design/project/kit?artifact=              the project's kit, a newer version, the kit's draft
//	POST design/project/kit {artifact, action, version?}
//	                                               "edit" (Править в ките), "use" (Обновить / back to a version),
//	                                               "stay" (Остаться on the current one)
//	GET  design/project/versions?artifact=         the project's points and named versions
//	POST design/project/versions {artifact, name}  Сохранить версию
//	POST design/project/versions/rename {artifact, revision, name}
//	GET  design/project/versions/diff?artifact=&revision=
//	POST design/project/versions/restore {artifact, revision}

func (a *API) kitFail(r *http.Request, err error, what string) Answer {
	var ke *KitError
	if errors.As(err, &ke) {
		return JSON(r, ke.Status, errorBody(ke.Msg), noStore())
	}
	var pe *KitPinError
	if errors.As(err, &pe) {
		return JSON(r, http.StatusConflict, errorBody(pe.Msg), noStore())
	}
	if IsHistoryRefusal(err) {
		return JSON(r, http.StatusConflict, errorBody(err.Error()), noStore())
	}
	a.warn("design versions: "+what, err)
	return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not "+what, "Не удалось: "+what)), noStore())
}

// personLine is who a person's action is saved under in the history.
func (a *API) personLine() string {
	if id, err := a.L.Identity(); err == nil && id.Person != "" {
		return id.Person
	}
	return locale.Pick("person", "человек")
}

// KitVersionsState is GET design/kits/state?kit=.
func (a *API) KitVersionsState(r *http.Request) Answer {
	if _, no := a.Guard(r, http.MethodGet); no != nil {
		return *no
	}
	kit := QueryString(r, "kit")
	// changes=0: without the draft's changed files (a project on the kit asks only whether it may edit it).
	st, err := a.L.KitState(kit, QueryString(r, "changes") != "0")
	if err != nil {
		return a.kitFail(r, err, locale.Pick("read the kit", "прочитать кит"))
	}
	person := a.PersonOf(r)
	st.Settings = a.L.KitSettingsFor(kit, person)
	st.MayEdit = !st.Settings.ReadOnly && a.mayDraftKit(r, kit)
	// Only the projects the person may open are named, and the draft only to whom may open it.
	visible := st.Projects[:0]
	for _, p := range st.Projects {
		if a.L.RoleFor(p.Project, person).AtLeast(RoleView) {
			visible = append(visible, p)
		}
	}
	st.Projects = visible
	if st.Draft != nil && !a.L.RoleFor(st.Draft.Project, person).AtLeast(RoleView) {
		st.Draft = nil
	}
	return JSON(r, http.StatusOK, st, noStore())
}

// KitDraftAction is POST design/kits/draft {kit, action: start | discard}.
func (a *API) KitDraftAction(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	kit, _ := strField(body, "kit")
	action, _ := strField(body, "action")
	if a.L.KitReadOnly(kit) {
		return a.kitFail(r, errKitReadOnly(), "")
	}
	if d, ok := a.L.KitDraft(kit); ok {
		if gated := a.gate(r, d.Project, RoleEdit); gated != nil {
			return *gated
		}
	}
	switch action {
	case "start":
		if !a.mayDraftKit(r, kit) {
			return JSON(r, http.StatusForbidden, errorBody(locale.Pick("No access to the kit", "Нет доступа к киту")), noStore())
		}
		d, err := a.L.StartKitDraft(kit)
		if err != nil {
			return a.kitFail(r, err, locale.Pick("start the kit's draft", "начать черновик кита"))
		}
		return JSON(r, http.StatusOK, d, noStore())
	case "discard":
		if err := a.L.DiscardKitDraft(kit); err != nil {
			return a.kitFail(r, err, locale.Pick("discard the draft", "отменить черновик"))
		}
		return Empty(http.StatusNoContent, noStore())
	}
	return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("action is start or discard", "action: start или discard")), noStore())
}

// KitSettingsRoute is POST design/kits/settings {kit, readOnly}: the kit's maker makes it for viewing only, or not.
func (a *API) KitSettingsRoute(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	kit, _ := strField(body, "kit")
	on, _ := Field(body, "readOnly").(bool)
	person := a.PersonOf(r)
	if err := a.L.SetKitReadOnly(kit, person, on); err != nil {
		return a.kitFail(r, err, locale.Pick("change the kit's settings", "изменить настройки кита"))
	}
	return JSON(r, http.StatusOK, a.L.KitSettingsFor(kit, person), noStore())
}

// mayDraftKit is whether the person may start the kit's draft (and so publish its versions): the library's owner, or
// whoever may edit a project on the kit. Without either the answer is no.
func (a *API) mayDraftKit(r *http.Request, kit string) bool {
	person := a.PersonOf(r)
	if person == "" || strings.EqualFold(person, a.L.ownerEmail()) {
		return true
	}
	for project := range a.L.ProjectsOnKit(kit) {
		if a.L.RoleFor(project, person).AtLeast(RoleEdit) {
			return true
		}
	}
	return false
}

// KitPublish is POST design/kits/publish {kit, part, notes}.
func (a *API) KitPublish(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	kit, _ := strField(body, "kit")
	part, _ := strField(body, "part")
	notes, _ := strField(body, "notes")
	if a.L.KitReadOnly(kit) {
		return a.kitFail(r, errKitReadOnly(), "")
	}
	d, ok := a.L.KitDraft(kit)
	if !ok {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The kit has no draft", "У кита нет черновика")), noStore())
	}
	if gated := a.gate(r, d.Project, RoleEdit); gated != nil {
		return *gated
	}
	pub, err := a.L.PublishKitDraft(kit, part, notes)
	if err != nil {
		return a.kitFail(r, err, locale.Pick("publish the version", "опубликовать версию"))
	}
	return JSON(r, http.StatusOK, pub, noStore())
}

// ProjectKitRoute is GET / POST design/project/kit.
func (a *API) ProjectKitRoute(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleView, http.MethodGet, http.MethodPost)
	if no != nil {
		return *no
	}
	if r.Method == http.MethodGet {
		return JSON(r, http.StatusOK, a.L.ProjectKit(id), noStore())
	}
	if gated := a.gate(r, id, RoleEdit); gated != nil {
		return *gated
	}
	action, _ := strField(body, "action")
	version, _ := strField(body, "version")
	if version != "" && !designkit.ValidVersion(version) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Invalid version", "Неверная версия")), noStore())
	}
	var err error
	switch action {
	case "edit":
		// A draft that is there is joined only by who may edit it, as «Редактировать» on the kit's screen.
		if pin, ok := a.L.KitPinOf(id); ok {
			if d, ok := a.L.KitDraft(pin.ID); ok {
				if gated := a.gate(r, d.Project, RoleEdit); gated != nil {
					return *gated
				}
			}
		}
		_, err = a.L.EditKitFrom(id)
	case "use":
		_, err = a.L.UseKitVersion(id, version, false)
	case "stay":
		_, err = a.L.UseKitVersion(id, version, true)
	default:
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("action is edit, use or stay", "action: edit, use или stay")), noStore())
	}
	if err != nil {
		return a.kitFail(r, err, locale.Pick("change the project's kit", "сменить кит проекта"))
	}
	return JSON(r, http.StatusOK, a.L.ProjectKit(id), noStore())
}

// ProjectVersionsRoute is GET / POST design/project/versions.
func (a *API) ProjectVersionsRoute(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleView, http.MethodGet, http.MethodPost)
	if no != nil {
		return *no
	}
	a.startBackground()
	if r.Method == http.MethodGet {
		h, err := a.L.ProjectHistory(id, 100)
		if err != nil {
			return a.kitFail(r, err, locale.Pick("read the project's history", "прочитать историю проекта"))
		}
		return JSON(r, http.StatusOK, h, noStore())
	}
	if gated := a.gate(r, id, RoleEdit); gated != nil {
		return *gated
	}
	name, _ := strField(body, "name")
	v, err := a.L.SaveProjectVersion(id, name, a.personLine())
	if err != nil {
		return a.kitFail(r, err, locale.Pick("save the version", "сохранить версию"))
	}
	return JSON(r, http.StatusOK, v, noStore())
}

// ProjectVersionRename is POST design/project/versions/rename {artifact, revision, name}; an empty name unnames it.
func (a *API) ProjectVersionRename(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	rev, _ := strField(body, "revision")
	name, _ := strField(body, "name")
	if err := a.L.RenameProjectVersion(id, rev, name); err != nil {
		return a.kitFail(r, err, locale.Pick("rename the version", "переименовать версию"))
	}
	return Empty(http.StatusNoContent, noStore())
}

// ProjectVersionDiff is GET design/project/versions/diff?artifact=&revision=.
func (a *API) ProjectVersionDiff(r *http.Request) Answer {
	_, id, no := a.deckGuarded(r, RoleView, http.MethodGet)
	if no != nil {
		return *no
	}
	d, err := a.L.DiffProject(id, QueryString(r, "revision"))
	if err != nil {
		return a.kitFail(r, err, locale.Pick("compare the versions", "сравнить версии"))
	}
	return JSON(r, http.StatusOK, d, noStore())
}

// ProjectVersionRestore is POST design/project/versions/restore {artifact, revision}.
func (a *API) ProjectVersionRestore(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	rev, _ := strField(body, "revision")
	snap, err := a.L.RestoreProjectVersion(id, rev, a.personLine())
	if err != nil {
		return a.kitFail(r, err, locale.Pick("restore the version", "вернуть версию"))
	}
	return JSON(r, http.StatusOK, struct {
		Revision string `json:"revision"`
	}{snap}, noStore())
}

// KitAdopt is POST design/kits/adopt {kit, dryRun}: the projects that carry a copy of the kit's themes move onto the
// kit's versions (AdoptKitCopies), only those the person may edit; dryRun reports and changes nothing.
func (a *API) KitAdopt(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	kit, _ := strField(body, "kit")
	dry, _ := Field(body, "dryRun").(bool)
	person := a.PersonOf(r)
	may := func(project string) bool { return a.L.RoleFor(project, person).AtLeast(RoleEdit) }
	rep, err := a.L.AdoptKitCopies(kit, dry, may)
	if err != nil {
		return a.kitFail(r, err, locale.Pick("move the projects onto the kit", "перевести проекты на кит"))
	}
	return JSON(r, http.StatusOK, rep, noStore())
}
