package design

import (
	"errors"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// KitLayoutsList is GET (a session): {layouts: [designkit.Layout]}, the slide layouts of the installed kits.
func (a *API) KitLayoutsList(r *http.Request) Answer {
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	if status, v, hh, refused := a.APIRefusal(r); refused {
		return Answer{status, v, hh}
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	return JSON(r, http.StatusOK, struct {
		Layouts []designkit.Layout `json:"layouts"`
	}{designkit.Layouts(a.L.KitsDir(), "")}, h)
}

// DeckSlide is POST {artifact, layout} (edit): a new slide that starts as the layout of an installed kit
// (<kit>[@version]/<theme>/<layout>); the theme's folder is put into the project. Answers {page}.
func (a *API) DeckSlide(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	addr, _ := strField(body, "layout")
	if addr == "" {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("layout is expected", "Нужен layout")), noStore())
	}
	lay, _, err := designkit.FindLayout(a.L.KitsDir(), addr)
	if err != nil {
		return JSON(r, http.StatusNotFound, errorBody(err.Error()), noStore())
	}
	p, err := a.L.CreatePage(id, PageSpec{Title: lay.Name, Layout: addr})
	var pe *PageError
	if errors.As(err, &pe) {
		return JSON(r, pe.Status, errorBody(pe.Msg), noStore())
	}
	if err != nil {
		return a.deckFail(r, err, "add the slide")
	}
	return JSON(r, http.StatusOK, struct {
		Page Page `json:"page"`
	}{p}, noStore())
}
