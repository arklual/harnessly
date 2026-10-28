package design

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The deck's model routes: the deck's state, its settings, notes, dictation tidied into notes, comments and the style
// read from a company template. The files they keep are in deck.go.

// MaxPptxBytes caps a company template.
const MaxPptxBytes = 50 << 20

const deckTidyTimeout = 150 * time.Second

// deckGuarded is the start of every JSON deck route: the guard, the project and the role.
func (a *API) deckGuarded(r *http.Request, min Role, methods ...string) (body *jsvalue.Object, artifact string, no *Answer) {
	body, no = a.Guard(r, methods...)
	if no != nil {
		return nil, "", no
	}
	artifact = QueryString(r, "artifact")
	if r.Method != http.MethodGet {
		artifact = ""
		if v := Field(body, "artifact"); v != nil {
			artifact = jscompat.String(v)
		}
	}
	if !IsLibraryID(artifact) {
		ans := JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), noStore())
		return nil, "", &ans
	}
	if gated := a.gate(r, artifact, min); gated != nil {
		return nil, "", gated
	}
	return body, artifact, nil
}

// deckFail words an error of the deck's files as an answer.
func (a *API) deckFail(r *http.Request, err error, what string) Answer {
	var de *DeckError
	if errors.As(err, &de) {
		return JSON(r, de.Status, errorBody(de.Msg), noStore())
	}
	a.warn("design deck: "+what, err)
	return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not "+what, "Не удалось: "+what)), noStore())
}

// Deck is GET ?artifact= (view): the DeckState.
func (a *API) Deck(r *http.Request) Answer {
	_, id, no := a.deckGuarded(r, RoleView, http.MethodGet)
	if no != nil {
		return *no
	}
	st, err := a.L.DeckState(id)
	if err != nil {
		return a.deckFail(r, err, "read the deck")
	}
	return JSON(r, http.StatusOK, st, noStore())
}

// DeckPdf is POST {artifact, auto} (edit): starts or stops keeping the deck's PDF for guests; answers {published} at
// once, the build runs in the background.
func (a *API) DeckPdf(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	auto, ok := Field(body, "auto").(bool)
	if !ok {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("auto is expected", "Нужно auto")), noStore())
	}
	p, err := a.L.SetDeckPdfAuto(id, auto)
	if err != nil {
		return a.deckFail(r, err, "keep the PDF")
	}
	return JSON(r, http.StatusOK, struct {
		Published DeckPublished `json:"published"`
	}{p}, noStore())
}

// DeckSettings is PUT {artifact, settings: part of DeckSettings} (edit): {settings}; rewrites DeckThemeCSS.
func (a *API) DeckSettings(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPut)
	if no != nil {
		return *no
	}
	obj, ok := Field(body, "settings").(*jsvalue.Object)
	if !ok {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("settings is an object", "settings — объект")), noStore())
	}
	var patch map[string]json.RawMessage
	if err := json.Unmarshal(jsvalue.Stringify(obj, ""), &patch); err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("settings is not valid", "settings неверны")), noStore())
	}
	s, err := a.L.SetDeckSettings(id, patch)
	if err != nil {
		return a.deckFail(r, err, "save the deck's settings")
	}
	return JSON(r, http.StatusOK, struct {
		Settings DeckSettings `json:"settings"`
	}{s}, noStore())
}

// DeckNotes is PUT {artifact, page, text} (edit): {slide: DeckSlide}; removes the page's .raw.md.
func (a *API) DeckNotes(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPut)
	if no != nil {
		return *no
	}
	page, _ := strField(body, "page")
	text, ok := strField(body, "text")
	if !ok || page == "" {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page and text are expected", "Нужны page и text")), noStore())
	}
	slide, err := a.L.SetDeckNotes(id, page, text)
	if err != nil {
		return a.deckFail(r, err, "save the notes")
	}
	return JSON(r, http.StatusOK, struct {
		Slide DeckSlide `json:"slide"`
	}{slide}, noStore())
}

// DeckTidy is POST {artifact, page: string|null, raw, lang} (edit): {slides: [DeckTidied]}. One call of a.OneShot;
// page null is the whole deck, which the model splits by slide. Writes the .md and the .raw.md of each slide.
func (a *API) DeckTidy(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleEdit, http.MethodPost)
	if no != nil {
		return *no
	}
	if a.OneShot == nil {
		return JSON(r, http.StatusServiceUnavailable, errorBody(locale.Pick("The agent's model is not reachable here", "Модель агента здесь недоступна")), noStore())
	}
	raw, _ := strField(body, "raw")
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\r\n", "\n"))
	if raw == "" || len([]rune(raw)) > maxDeckNotesRunes {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("The dictation is empty or over 20000 characters", "Диктовка пуста или длиннее 20000 знаков")), noStore())
	}
	lang, _ := strField(body, "lang")
	page := ""
	if v := Field(body, "page"); v != nil {
		s, ok := v.(string)
		if !ok || s == "" {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page is a slide or null", "page — слайд или null")), noStore())
		}
		page = s
	}
	ctx, cancel := context.WithTimeout(r.Context(), deckTidyTimeout)
	defer cancel()
	out, err := a.L.TidyDeck(ctx, a.OneShot, id, page, raw, lang)
	if err != nil {
		return a.deckFail(r, err, "make the notes from the dictation")
	}
	return JSON(r, http.StatusOK, struct {
		Slides []DeckTidied `json:"slides"`
	}{out}, noStore())
}

// DeckComments is POST {artifact, page, x, y, label, text} (comment): the DeckComment; and PATCH {artifact, id,
// resolved} (comment): the DeckComment, or {id, resolved} for a guest comment's id g-<n>.
func (a *API) DeckComments(r *http.Request) Answer {
	body, id, no := a.deckGuarded(r, RoleComment, http.MethodPost, http.MethodPatch)
	if no != nil {
		return *no
	}
	if r.Method == http.MethodPatch {
		cid, _ := strField(body, "id")
		resolved, isBool := Field(body, "resolved").(bool)
		if cid == "" || !isBool {
			return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("id and resolved are expected", "Нужны id и resolved")), noStore())
		}
		v, err := a.L.ResolveDeckComment(id, cid, resolved)
		if err != nil {
			return a.deckFail(r, err, "change the comment")
		}
		return JSON(r, http.StatusOK, v, noStore())
	}
	page, _ := strField(body, "page")
	text, _ := strField(body, "text")
	label, _ := strField(body, "label")
	x, okX := Field(body, "x").(float64)
	y, okY := Field(body, "y").(float64)
	if page == "" || !okX || !okY || math.IsNaN(x) || math.IsNaN(y) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page, x, y and text are expected", "Нужны page, x, y и text")), noStore())
	}
	c, err := a.L.AddDeckComment(id, DeckCommentSpec{Page: page, X: x, Y: y, Label: label, Text: text})
	if err != nil {
		return a.deckFail(r, err, "save the comment")
	}
	return JSON(r, http.StatusOK, c, noStore())
}

// DeckPptxStyle is POST with a .pptx body of at most 50 MB (edit of any project; used before the deck exists): the
// DeckPptxStyle read from ppt/theme/theme1.xml and the count of slideLayouts. x-design-filename (URI-encoded) names it.
func (a *API) DeckPptxStyle(w http.ResponseWriter, r *http.Request) {
	writeAnswer(w, a.deckPptxStyle(r))
}

func (a *API) deckPptxStyle(r *http.Request) Answer {
	h := noStore()
	if r.Method != http.MethodPost {
		h.Set("Allow", "POST")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	if status, v, hh, refused := a.APIRefusal(r); refused {
		return Answer{status, v, hh}
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	if r.ContentLength > MaxPptxBytes {
		return JSON(r, http.StatusRequestEntityTooLarge, errorBody(locale.Pick("The file is over 50 MB", "Файл больше 50 МБ")), h)
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, MaxPptxBytes+1))
	if err != nil {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Could not read the file", "Не удалось прочитать файл")), h)
	}
	if len(data) > MaxPptxBytes {
		return JSON(r, http.StatusRequestEntityTooLarge, errorBody(locale.Pick("The file is over 50 MB", "Файл больше 50 МБ")), h)
	}
	name := ""
	if v, err := url.PathUnescape(r.Header.Get("X-Design-Filename")); err == nil {
		parts := pathSep.Split(v, -1)
		name = jscompat.Slice(parts[len(parts)-1], 200)
	}
	st, err := ReadPptxStyle(data, name)
	if errors.Is(err, ErrNotPptx) {
		return JSON(r, http.StatusUnprocessableEntity, errorBody(locale.Pick("This is not a PowerPoint template (.pptx)", "Это не шаблон PowerPoint (.pptx)")), h)
	}
	if err != nil {
		a.warn("design deck: reading a template", err)
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not read the template", "Не удалось прочитать шаблон")), h)
	}
	return JSON(r, http.StatusOK, st, h)
}

// CLINotes is the agents' notes command of a deck: GET ?project=&page= answers {project, slides: [DeckSlide]} (one
// slide when page is given: an id, file or title); POST {project, page, text} sets a slide's notes.
func (a *API) CLINotes(r *http.Request) Answer {
	raw, no := a.CLIGuard(r)
	if no != nil {
		return *no
	}
	h := noStore()
	if r.Method == http.MethodGet {
		project := QueryString(r, "project")
		if no := a.gate(r, project, RoleView); no != nil {
			return *no
		}
		if err := a.L.requireDeck(project); err != nil {
			return a.deckFail(r, err, "read the notes")
		}
		slides := a.L.DeckSlides(project)
		if page := QueryString(r, "page"); page != "" {
			p, err := a.L.deckPage(project, page)
			if err != nil {
				return a.deckFail(r, err, "read the notes")
			}
			one := []DeckSlide{}
			for _, s := range slides {
				if s.ID == p.ID {
					one = append(one, s)
				}
			}
			slides = one
		}
		return JSON(r, http.StatusOK, struct {
			Project string      `json:"project"`
			Slides  []DeckSlide `json:"slides"`
		}{project, slides}, h)
	}
	if r.Method != http.MethodPost {
		h.Set("Allow", "GET, POST")
		return Empty(http.StatusMethodNotAllowed, h)
	}
	body := Fields(raw)
	project, _ := strField(body, "project")
	if no := a.gate(r, project, RoleEdit); no != nil {
		return *no
	}
	page, _ := strField(body, "page")
	text, ok := strField(body, "text")
	if !ok || page == "" {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("page and text are expected", "Нужны page и text")), h)
	}
	slide, err := a.L.SetDeckNotes(project, page, text)
	if err != nil {
		return a.deckFail(r, err, "save the notes")
	}
	return JSON(r, http.StatusOK, struct {
		OK      bool      `json:"ok"`
		Project string    `json:"project"`
		Slide   DeckSlide `json:"slide"`
	}{true, project, slide}, h)
}
