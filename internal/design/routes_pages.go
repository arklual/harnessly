package design

import (
	"net/http"
	"strings"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// CLIPages is the agents' page commands of a project with meta/: GET ?project= lists the pages; POST
// {action:'create', project, title, file?, route?, variantOf?, w?, h?, expand?}, {action:'rename', project, page,
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
		}{true, project, p}, h)
	}
	cli := a.L.cfg.Texts.CLI
	switch Field(body, "action") {
	case "create":
		title, _ := strField(body, "title")
		file, _ := strField(body, "file")
		variant, _ := strField(body, "variantOf")
		expand, _ := Field(body, "expand").(bool)
		return answer(a.L.CreatePage(project, PageSpec{Title: title, File: file, Route: optional("route"), VariantOf: variant,
			W: size("w"), H: size("h"), Expand: expand, Widths: intList(Field(body, "widths"))}))
	case "rename":
		return answer(a.L.RenamePage(project, page, optional("title"), optional("file")))
	case "remove":
		return answer(a.L.RemovePage(project, page))
	}
	return JSON(r, 400, errorBody(locale.Pick("Unknown action: create, rename or remove ("+cli+" design page help)", "Неизвестное действие: create, rename или remove ("+cli+" design page help)")), h)
}
