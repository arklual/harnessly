package design

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// CLIPages is the agents' page commands of a project with meta/: GET ?project= lists the pages; POST
// {action:'create', project, title, file?, route?, variantOf?, w?, h?, expand?},
// {action:'new', title, page?, session?} makes a project of the person's with its first page, {action:'rename', project, page,
// title?, file?} and {action:'remove', project, page} change them. A page is named by its id, file or title.
func (a *API) CLIPages(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	refuse := func(err error) Answer {
		if pe, ok := err.(*PageError); ok {
			return JSON(r, pe.Status, errorBody(pe.Msg), h)
		}
		if ke, ok := err.(*KitPinError); ok {
			return JSON(r, 422, errorBody(ke.Msg), h)
		}
		a.warn("design library request failed", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not change the pages", "Не удалось изменить страницы")), h)
	}
	if r.Method == http.MethodGet {
		project, _ := Query(r, "project")
		if no := a.gate(r, project, RoleView); no != nil {
			return *no
		}
		list, err := a.L.ListPages(project)
		if err != nil {
			return refuse(err)
		}
		return JSON(r, 200, list, h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(405, h)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return JSON(r, 415, errorBody(locale.Pick("application/json is required", "Нужен application/json")), h)
	}
	body := Fields(raw)
	if Field(body, "action") == "new" {
		return a.cliNewProject(r, body, h)
	}
	project, _ := strField(body, "project")
	if no := a.gate(r, project, RoleEdit); no != nil {
		return *no
	}
	page, _ := strField(body, "page")
	optional := func(k string) *string {
		if s, ok := strField(body, k); ok {
			return &s
		}
		return nil
	}
	size := func(k string) *int { return metaSize(Field(body, k)) }
	answer := func(p Page, err error) Answer {
		if err != nil {
			return refuse(err)
		}
		return JSON(r, 200, struct {
			OK      bool   `json:"ok"`
			Project string `json:"project"`
			Page    Page   `json:"page"`
			Brief   string `json:"brief,omitempty"`
		}{true, project, p, a.L.KitBrief(project)}, h)
	}
	cli := a.L.cfg.Texts.CLI
	switch Field(body, "action") {
	case "build":
		return JSON(r, 200, struct {
			OK      bool        `json:"ok"`
			Project string      `json:"project"`
			Pages   []PageBuild `json:"pages"`
		}{true, project, a.L.BuildPages(project, page)}, h)
	case "check":
		problems := a.L.PageSourceProblems(project)
		if problems == nil {
			problems = []PageSourceProblem{}
		}
		return JSON(r, 200, struct {
			OK       bool                `json:"ok"`
			Project  string              `json:"project"`
			Problems []PageSourceProblem `json:"problems"`
		}{len(problems) == 0, project, problems}, h)
	case "create":
		title, _ := strField(body, "title")
		file, _ := strField(body, "file")
		variant, _ := strField(body, "variantOf")
		layout, _ := strField(body, "layout")
		expand, _ := Field(body, "expand").(bool)
		made, err := a.L.CreatePage(project, PageSpec{Title: title, File: file, Route: optional("route"), VariantOf: variant, Layout: layout,
			W: size("w"), H: size("h"), Expand: expand, Widths: intList(Field(body, "widths"))})
		if err == nil {
			// The board an agent made opens in its chat's pane, on that board, not on the project's first page.
			if session, ok := strField(body, "session"); ok && sessionRe.MatchString(session) && a.OpenPane != nil {
				a.OpenPane(session, project, made.File)
			}
		}
		return answer(made, err)
	case "kit":
		ref, _ := strField(body, "kit")
		pin, err := a.L.UseProjectKit(project, ref)
		if err != nil {
			return refuse(err)
		}
		out := map[string]any{"ok": true, "project": project, "brief": a.L.KitBrief(project)}
		if pin != nil {
			out["kit"] = pin.ID + "@" + pin.Version
		}
		return JSON(r, 200, out, h)
	case "connect":
		// Installing a package runs npm on this machine: only the project's owner may ask for it.
		if no := a.gate(r, project, RoleOwner); no != nil {
			return *no
		}
		source, _ := strField(body, "source")
		registry, _ := strField(body, "registry")
		pin, err := a.L.ConnectKitPackage(project, source, registry)
		if err != nil {
			return refuse(err)
		}
		return JSON(r, 200, map[string]any{"ok": true, "project": project, "kit": pin.ID + "@" + pin.Version}, h)
	case "kits":
		ids := []string{}
		for _, k := range a.L.installedKits() {
			ids = append(ids, k.ID)
		}
		pin, _ := a.L.KitPinOf(project)
		return JSON(r, 200, map[string]any{"ok": true, "installed": ids, "pinned": pin.ID}, h)
	case "brief":
		session, _ := strField(body, "session")
		// The hook asks with pinned: only a project that has a kit is worth stopping an edit for.
		if only, _ := Field(body, "pinned").(bool); only {
			if _, ok := a.L.KitPinOf(project); !ok {
				return JSON(r, 200, map[string]any{"ok": true, "brief": ""}, h)
			}
		}
		return JSON(r, 200, map[string]any{"ok": true, "brief": a.L.BriefOnce(session, project)}, h)
	case "rename":
		return answer(a.L.RenamePage(project, page, optional("title"), optional("file")))
	case "remove":
		return answer(a.L.RemovePage(project, page))
	}
	return JSON(r, 400, errorBody(locale.Pick("Unknown action: create, rename, remove, build or check ("+cli+" design page help)", "Неизвестное действие: create, rename, remove, build или check ("+cli+" design page help)")), h)
}

// NewProject is the answer of {action:'new'}: the project made, its folder, its first page and the rules a design
// dialog works by, so an agent of any chat can go on drawing on the board at once.
type NewProject struct {
	OK      bool   `json:"ok"`
	Project string `json:"project"`
	Title   string `json:"title"`
	Dir     string `json:"dir"`
	Board   string `json:"board"`
	Rules   string `json:"rules"`
	Page    Page   `json:"page"`
	Opened  bool   `json:"opened"`
	// Kit is the installed UI kit the project is pinned to (id@version/theme), when one was asked for.
	Kit string `json:"kit,omitempty"`
	// Brief is what the agent is told about the project's kit (or the one question when it has none).
	Brief string `json:"brief,omitempty"`
}

// cliNewProject makes a project of the person's from the library's template with a first page, from any chat: a
// mockup or a prototype the person asks for in a chat outside the library is drawn on a board, not in a file of the
// chat's folder. From a chat, the board opens in its pane.
func (a *API) cliNewProject(r *http.Request, body *jsvalue.Object, h http.Header) Answer {
	title, _ := strField(body, "title")
	// A kit installed on the machine (id[@version][/theme]) is checked before anything is made.
	var pin *KitPin
	if kit, _ := strField(body, "kit"); strings.TrimSpace(kit) != "" {
		p, msg := a.L.kitPinFor(strings.TrimSpace(kit))
		if p == nil {
			return JSON(r, 404, errorBody(msg), h)
		}
		pin = p
	}
	project, err := a.L.CreateProject(title)
	if err != nil {
		var ce *CreateError
		if errors.As(err, &ce) {
			return JSON(r, 422, errorBody(ce.Msg), h)
		}
		a.warn("design library request failed", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not create the project", "Не удалось создать проект")), h)
	}
	a.L.AnnounceNewBy(project.ID, a.PersonOf(r))
	if pin != nil {
		if err := a.L.SetKitPin(project.ID, pin); err != nil {
			a.warn("design: a new project's kit was not pinned", err)
		}
	}
	pageTitle, _ := strField(body, "page")
	if strings.TrimSpace(pageTitle) == "" {
		pageTitle = project.Title
	}
	page, err := a.L.CreatePage(project.ID, PageSpec{Title: pageTitle})
	if err != nil {
		if pe, ok := err.(*PageError); ok {
			return JSON(r, pe.Status, errorBody(pe.Msg), h)
		}
		a.warn("design library request failed", err)
		return JSON(r, 500, errorBody(locale.Pick("Could not create the page", "Не удалось создать страницу")), h)
	}
	_, rules, _, _ := a.L.PrepareDialog(project.ID)
	opened := false
	if session, ok := strField(body, "session"); ok && sessionRe.MatchString(session) && a.OpenPane != nil {
		opened = a.OpenPane(session, project.ID, page.File)
	}
	out := NewProject{OK: true, Project: project.ID, Title: project.Title, Dir: a.L.ArtifactDir(project.ID),
		Board: filepath.Join(a.L.ProjectDir(project.ID), page.File), Rules: rules, Page: page, Opened: opened}
	if pin != nil {
		out.Kit = pin.ID + "@" + pin.Version
		if len(pin.Mounts) > 0 {
			out.Kit += "/" + pin.Mounts[0]
		}
	}
	out.Brief = a.L.KitBrief(project.ID)
	return JSON(r, 200, out, h)
}

// kitPinFor is the pin of an installed kit named id[@version][/theme]: the newest version when none is named, all the
// kit gives when no theme is named. nil and why when it is not installed.
func (l *Library) kitPinFor(ref string) (*KitPin, string) {
	ref, theme, _ := strings.Cut(ref, "/")
	id, version, _ := strings.Cut(ref, "@")
	none := locale.Pick("No such UI kit on this machine: see `kit_list` or `harnessly design kit list`",
		"Такого UI-кита на машине нет: смотрите `kit_list` или `harnessly design kit list`")
	if !designkit.ValidID(id) || (version != "" && !designkit.ValidVersion(version)) {
		return nil, none
	}
	version = newestVersion(l.KitsDir(), id, version)
	if version == "" || designkit.Resolve(l.KitsDir(), id, version, nil) == "" {
		return nil, none
	}
	if theme == "" {
		// Unnamed, the project mounts what the kit gives (KitMountDirs).
		return &KitPin{ID: id, Version: version}, ""
	}
	for _, t := range designkit.Themes(l.KitsDir(), id, version) {
		if t.ID == theme {
			return &KitPin{ID: id, Version: version, Mounts: []string{theme}}, ""
		}
	}
	return nil, locale.Pick("The kit has no theme "+theme, "У кита нет темы "+theme)
}
