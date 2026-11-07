package design

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/locale"
)

func (a *API) folderFailure(r *http.Request, err error, what string) Answer {
	switch e := err.(type) {
	case *FolderError:
		return JSON(r, e.Status, errorBody(e.Msg), noStore())
	case *ProjectDeleteError:
		return JSON(r, e.Status, errorBody(e.Msg), noStore())
	}
	a.warn(what, err)
	return JSON(r, 500, errorBody(locale.Pick("Could not do it", "Не получилось")), noStore())
}

func ok(r *http.Request) Answer { return JSON(r, 200, map[string]bool{"ok": true}, noStore()) }

// Layout is POST {}: the person's folders, where their projects lie and the design system each was built on.
func (a *API) Layout(r *http.Request) Answer {
	if _, no := a.Guard(r, http.MethodPost); no != nil {
		return *no
	}
	out, err := a.L.LayoutFor(a.person(r))
	if err != nil {
		return a.folderFailure(r, err, "design layout not read")
	}
	return JSON(r, 200, out, noStore())
}

// FolderSave is POST {id?, name, parent?}: a folder made or renamed.
func (a *API) FolderSave(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	id, _ := strField(body, "id")
	name, _ := strField(body, "name")
	parent, hasParent := strField(body, "parent")
	f, err := a.L.SaveFolder(a.person(r), id, name, parent, hasParent)
	if err != nil {
		return a.folderFailure(r, err, "design folder not kept")
	}
	return JSON(r, 200, f, noStore())
}

// FolderDelete is POST {id, projects: "keep"|"trash"}: the folder goes. "keep" lays its projects at the top; "trash" sends
// those the person owns to the trash first (the rest are laid at the top, they are not theirs to delete).
func (a *API) FolderDelete(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	id, _ := strField(body, "id")
	mode, _ := strField(body, "projects")
	person := a.person(r)
	trashed := []string{}
	if mode == "trash" {
		ids, err := a.L.FolderProjects(person, id)
		if err != nil {
			return a.folderFailure(r, err, "design folder not read")
		}
		for _, p := range ids {
			if a.gateDelete(r, p) != nil {
				continue
			}
			if err := a.L.DeleteProject(p); err != nil {
				a.warn("design project not deleted with its folder", err)
				continue
			}
			trashed = append(trashed, p)
		}
	}
	if err := a.L.RemoveFolder(person, id); err != nil {
		return a.folderFailure(r, err, "design folder not removed")
	}
	return JSON(r, 200, map[string]any{"ok": true, "trashed": trashed}, noStore())
}

// FolderMove is POST {projects: [id], folder}: projects laid in a folder ("" is the top).
func (a *API) FolderMove(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	folder, _ := strField(body, "folder")
	var ids []string
	for _, v := range body.Array("projects") {
		if s, ok := v.(string); ok {
			ids = append(ids, s)
		}
	}
	if err := a.L.MoveProjects(a.person(r), ids, folder); err != nil {
		return a.folderFailure(r, err, "design projects not moved")
	}
	return ok(r)
}

// TrashList is POST {}: the person's projects in the trash.
func (a *API) TrashList(r *http.Request) Answer {
	if _, no := a.Guard(r, http.MethodPost); no != nil {
		return *no
	}
	return JSON(r, 200, map[string]any{"items": a.L.TrashFor(a.person(r))}, noStore())
}

// TrashRestore is POST {project}: the project is back in the library.
func (a *API) TrashRestore(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	project, _ := strField(body, "project")
	if err := a.L.RestoreProject(project, a.person(r)); err != nil {
		return a.folderFailure(r, err, "design project not restored")
	}
	return ok(r)
}

// TrashPurge is POST {project}: the project is erased for good.
func (a *API) TrashPurge(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	project, _ := strField(body, "project")
	if err := a.L.PurgeProject(project, a.person(r)); err != nil {
		return a.folderFailure(r, err, "design project not erased")
	}
	return ok(r)
}
