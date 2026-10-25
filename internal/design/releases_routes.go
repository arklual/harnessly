package design

import (
	"net/http"

	"github.com/ly-solutions/harnessly/internal/locale"
)

type releasesResponse struct {
	// Configured: the machine has a mirror of releases; everything else is empty without one.
	Configured bool      `json:"configured"`
	Releases   []Release `json:"releases"`
	// Newest is the newest stable release, NewProjects the one a new project starts on, Chosen the library's own
	// choice of it (null: new projects follow the newest stable release).
	Newest      *string         `json:"newest"`
	NewProjects *string         `json:"newProjects"`
	Chosen      *string         `json:"chosen"`
	Project     *ProjectRelease `json:"project,omitempty"`
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (a *API) releasesAnswer(r *http.Request, project string) Answer {
	h := noStore()
	list := a.L.Releases()
	out := releasesResponse{Configured: list != nil, Releases: []Release{}}
	if list != nil {
		out.Releases = list
		out.Newest = strOrNil(NewestStable(list))
		out.NewProjects = strOrNil(a.L.NewProjectsRelease())
		out.Chosen = strOrNil(a.L.ChosenNewProjectsRelease())
	}
	if project != "" {
		if no := a.gate(r, project, RoleView); no != nil {
			return *no
		}
		if !IsLibraryID(project) || a.L.ReadArtifact(project) == nil {
			return JSON(r, 404, errorBody(locale.Pick("There is no such project", "Такого проекта нет")), h)
		}
		out.Project = a.L.ProjectReleaseOf(project, list)
	}
	return JSON(r, 200, out, h)
}

// Releases is the board format's releases on this machine. GET ?artifact= lists them with the project's own; POST
// {artifact, release} moves a project to a stable release, POST {newProjects: release|null} sets the release new
// projects start on (null: the newest stable one). Nothing moves a project but an explicit call.
func (a *API) Releases(r *http.Request) Answer {
	if r.Method == http.MethodGet {
		if _, no := a.Guard(r, http.MethodGet); no != nil {
			return *no
		}
		return a.releasesAnswer(r, QueryString(r, "artifact"))
	}
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	bad := JSON(r, 400, errorBody(locale.Pick("Invalid request", "Неверный запрос")), h)
	refused := func(err error) Answer {
		if IsReleaseError(err) {
			return JSON(r, 422, errorBody(err.Error()), h)
		}
		return JSON(r, 500, errorBody(locale.Pick("Could not change the release", "Не удалось сменить выпуск")), h)
	}
	if Has(body, "newProjects") {
		v := Field(body, "newProjects")
		release, isString := v.(string)
		if v != nil && (!isString || release == "") {
			return bad
		}
		if err := a.L.SetNewProjectsRelease(release); err != nil {
			return refused(err)
		}
		return a.releasesAnswer(r, "")
	}
	artifact, ok1 := strField(body, "artifact")
	release, ok2 := strField(body, "release")
	if !ok1 || !ok2 {
		return bad
	}
	if no := a.gate(r, artifact, RoleEdit); no != nil {
		return *no
	}
	if _, err := a.L.SetProjectRelease(artifact, release); err != nil {
		return refused(err)
	}
	return a.releasesAnswer(r, artifact)
}
