package design

// The kit screen of Sinopia (HRN-897): an installed kit whose themes describe a canvas (designkit.ReadCanvas) is shown
// as one canvas with a tree and a search instead of a project of boards. These routes give the screen the kits, a
// theme's canvas with its prepared atlases, the kit's files through the runtime, a template as a project of boards,
// and where the kit is used.

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// KitCard is an installed kit with a canvas as the library lists it.
type KitCard struct {
	ID       string                  `json:"id"`
	Title    string                  `json:"title"`
	Version  string                  `json:"version"`
	Versions []string                `json:"versions"`
	Themes   []designkit.CanvasTheme `json:"themes"`
	// Cover is a picture of the kit: the first template's cover in its first theme (a path in the version), "" when
	// none.
	Cover      string `json:"cover"`
	CoverTheme string `json:"coverTheme"`
	Layouts    int    `json:"layouts"`
	Assets     int    `json:"assets"`
	// ThemeCards is a card per theme (a kit of several brands shows each on the shelf), in the kit's order.
	ThemeCards []KitThemeCard `json:"themeCards"`
}

// KitThemeCard is a theme of a kit on the shelf: its cover (a path in the version) and how many templates it holds.
type KitThemeCard struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Cover   string `json:"cover"`
	Layouts int    `json:"layouts"`
}

// themeCard counts a theme's templates (its guide and its empty master layouts are not) and takes the first template's
// cover.
func themeCard(c *designkit.Canvas, title string) KitThemeCard {
	tc := KitThemeCard{ID: c.Theme, Title: title}
	for _, t := range c.Templates {
		if t.Section != designkit.SectionTemplates || t.Blank {
			continue
		}
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				tc.Layouts++
				if tc.Cover == "" && l.ID == t.Cover && l.Thumb != "" {
					tc.Cover = joinBase(c.Base, l.Thumb)
				}
			}
		}
	}
	return tc
}

func (a *API) kitGet(r *http.Request) (Answer, bool) {
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h), false
	}
	if status, v, hh, refused := a.APIRefusal(r); refused {
		return Answer{status, v, hh}, false
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h), false
	}
	return Answer{}, true
}

// KitCards lists the installed kits that have a canvas, newest version of each.
func KitCards(dir string) []KitCard {
	out := []KitCard{}
	for _, k := range designkit.List(dir) {
		if len(k.Versions) == 0 {
			continue
		}
		v := k.Versions[0].Version
		c, err := designkit.ReadCanvas(dir, k.ID, v, "")
		if err != nil {
			continue
		}
		card := KitCard{ID: k.ID, Title: c.Title, Version: v, Versions: c.Versions, Themes: c.Themes, CoverTheme: c.Theme}
		for _, t := range c.Templates {
			for _, s := range t.Sections {
				for _, l := range s.Layouts {
					card.Layouts++
					if card.Cover == "" && l.ID == t.Cover && l.Thumb != "" {
						card.Cover = joinBase(c.Base, l.Thumb)
					}
				}
			}
		}
		for _, g := range c.Assets {
			card.Assets += len(g.Items)
		}
		card.ThemeCards = []KitThemeCard{}
		for _, th := range c.Themes {
			tc := c
			if th.ID != c.Theme {
				if tc, err = designkit.ReadCanvas(dir, k.ID, v, th.ID); err != nil {
					continue
				}
			}
			card.ThemeCards = append(card.ThemeCards, themeCard(tc, th.Title))
		}
		out = append(out, card)
	}
	return out
}

// KitCanvasList is GET (a session): {kits: [KitCard]}.
func (a *API) KitCanvasList(r *http.Request) Answer {
	if no, ok := a.kitGet(r); !ok {
		return no
	}
	return JSON(r, http.StatusOK, struct {
		Kits []KitCard `json:"kits"`
	}{KitCards(a.L.KitsDir())}, noStore())
}

// newestVersion is the version asked for, or the newest installed one when none is.
func newestVersion(dir, id, version string) string {
	if version != "" {
		return version
	}
	for _, k := range designkit.List(dir) {
		if k.ID == id && len(k.Versions) > 0 {
			return k.Versions[0].Version
		}
	}
	return ""
}

// KitCanvas is GET ?kit=&version=&theme= (a session): the theme's canvas with its atlases. Without a version, the
// newest; without a theme, the first.
func (a *API) KitCanvas(r *http.Request) Answer {
	if no, ok := a.kitGet(r); !ok {
		return no
	}
	dir := a.L.KitsDir()
	id := QueryString(r, "kit")
	version := newestVersion(dir, id, QueryString(r, "version"))
	if !a.draftGate(r, id, version) {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The kit has no draft", "У кита нет черновика")), noStore())
	}
	c, err := designkit.ReadCanvas(dir, id, version, QueryString(r, "theme"))
	if errors.Is(err, designkit.ErrNoCanvas) {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The kit has no canvas", "У кита нет холста")), noStore())
	}
	if err != nil {
		return JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), noStore())
	}
	if err := designkit.Prepare(dir, c); err != nil && a.L.cfg.Log != nil {
		a.L.cfg.Log("warn", "kit atlases not prepared", err)
	}
	return JSON(r, http.StatusOK, c, noStore())
}

// KitFile is GET ?kit=&version=&path= (a session): a file of an installed version, or with &theme=&prep=<name> a
// prepared file of its theme. A version never changes, so the answer is kept for good.
func (a *API) KitFile(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	dir := a.L.KitsDir()
	id, version := QueryString(r, "kit"), QueryString(r, "version")
	var file string
	if !a.draftGate(r, id, version) {
		version = ""
	}
	if prep := QueryString(r, "prep"); prep != "" {
		file = designkit.PreparedFile(dir, id, version, QueryString(r, "theme"), prep)
	} else if rel := QueryString(r, "path"); rel != "" && !strings.Contains(rel, "\\") {
		file = designkit.Resolve(dir, id, version, strings.Split(strings.TrimPrefix(rel, "/"), "/"))
	}
	// Only pictures and fonts: this answer comes from the app's own origin, so a kit's page or script is never served
	// here (the live pages go through the preview slot, /kitv/), and an SVG is sandboxed.
	typ := ""
	ext := strings.ToLower(filepath.Ext(file))
	if file != "" && (imageExt[ext] || fontExt[ext]) {
		typ = kitContentType(ext)
	}
	st, err := os.Stat(file)
	if file == "" || typ == "" || err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	a.L.serveStatic(w, r, file, st, typ, ext, a.L.kitCache(file), r.Method == http.MethodHead)
}

// KitProject is POST {kit, version, theme, template} (a session): the template as a project of boards, one page per
// layout in the template's order. The same template of the same version opens the project made for it before, while
// it is there. Answers {id, title}.
func (a *API) KitProject(r *http.Request) Answer {
	body, no := a.Guard(r, http.MethodPost)
	if no != nil {
		return *no
	}
	h := noStore()
	id, _ := strField(body, "kit")
	version, _ := strField(body, "version")
	theme, _ := strField(body, "theme")
	tpl, _ := strField(body, "template")
	dir := a.L.KitsDir()
	version = newestVersion(dir, id, version)
	c, err := designkit.ReadCanvas(dir, id, version, theme)
	if err != nil {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("The kit has no canvas", "У кита нет холста")), h)
	}
	var t *designkit.Template
	for i := range c.Templates {
		if c.Templates[i].ID == tpl {
			t = &c.Templates[i]
		}
	}
	if t == nil {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such template", "Такого шаблона нет")), h)
	}
	person := a.PersonOf(r)
	key := id + "@" + version + "/" + c.Theme + "/" + t.ID
	if prev := a.L.kitProjectOf(key); prev != "" && a.L.ReadArtifact(prev) != nil && a.L.CanFor(prev, RoleView, person) {
		return JSON(r, http.StatusOK, struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}{prev, a.L.ReadArtifact(prev).Title}, h)
	}
	project, err := a.L.CreateProject(c.Title + " · " + t.Title)
	if err != nil {
		return JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), h)
	}
	a.L.AnnounceNewBy(project.ID, person)
	d := DefaultDeckSettings()
	if err := a.L.InitDeck(project.ID, DeckInit{Minutes: d.Minutes, Tone: d.Tone, Script: d.Script}); err != nil {
		return JSON(r, http.StatusInternalServerError, errorBody(err.Error()), h)
	}
	for _, s := range t.Sections {
		for _, l := range s.Layouts {
			if l.Address == "" {
				continue
			}
			kit, rest, _ := strings.Cut(l.Address, "/")
			if _, err := a.L.CreatePage(project.ID, PageSpec{Title: l.Name, Layout: kit + "@" + version + "/" + rest}); err != nil {
				return JSON(r, http.StatusUnprocessableEntity, errorBody(err.Error()), h)
			}
		}
	}
	a.L.rememberKitProject(key, project.ID)
	return JSON(r, http.StatusCreated, struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}{project.ID, project.Title}, h)
}

// joinBase is a path of a theme's folder as a path in the version.
func joinBase(base, rel string) string {
	if base == "" {
		return rel
	}
	return base + "/" + rel
}
