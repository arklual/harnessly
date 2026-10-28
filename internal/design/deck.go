package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The deck's model on disk (deck_types.go says what each file holds): settings in meta/deck.json, a slide's notes in
// meta/notes/<pageId>.md (and the dictation they came from in .raw.md), a comment in meta/comments/<ULID>.json, a
// resolved guest comment in meta/comments/g-<id>.json, and the look in project/deck-theme.css, written from the
// settings. One file per slide or comment: the runtimes sharing a library never write one shared file for a change.

const (
	maxDeckFileBytes   = 256 * 1024
	maxDeckNotesRunes  = 20000
	maxDeckCommentRune = 2000
	maxDeckComments    = 500
	maxDeckThemes      = 12
	maxDeckThemeVars   = 32
	deckThemeVarMax    = 64
	deckSlideDefaultW  = 1920
	deckSlideDefaultH  = 1080
)

var (
	deckVarNameRe    = regexp.MustCompile(`^--deck-[a-z0-9-]{1,32}$`)
	deckThemeIDRe    = regexp.MustCompile(`^[a-z0-9-]{1,24}$`)
	deckVarValueRe   = regexp.MustCompile(`^[#A-Za-z0-9 .,%()_+-]{1,64}$`)
	deckGuestIDRe    = regexp.MustCompile(`^g-[A-Za-z0-9_-]{1,64}$`)
	deckCommentIDRe  = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	deckCommentFile  = regexp.MustCompile(`^([0-9A-HJKMNP-TV-Z]{26}|g-[A-Za-z0-9_-]{1,64})\.json$`)
	deckBadValueWord = regexp.MustCompile(`(?i)url\(|@import|expression\(`)
)

// DeckError is a refusal a deck route words for the person.
type DeckError struct {
	Status int
	Msg    string
}

func (e *DeckError) Error() string { return e.Msg }

func deckErr(status int, en, ru string) *DeckError {
	return &DeckError{Status: status, Msg: locale.Pick(en, ru)}
}

// DeckDir is a project's folder under which the deck's files live (the project's root).
func (l *Library) deckFile(id, rel string) string {
	return filepath.Join(l.ArtifactDir(id), filepath.FromSlash(rel))
}

// IsDeck: the project is a deck. Every deck keeps its settings file from the moment it is made, and the file lives in
// the project's folder, so every runtime of a shared library tells a deck the same way.
func (l *Library) IsDeck(id string) bool {
	return IsLibraryID(id) && isRealFile(l.deckFile(id, DeckSettingsFile))
}

// requireDeck: the project exists; its model may be nil (a project without meta/ has no slides).
func (l *Library) requireDeck(id string) error {
	if !IsLibraryID(id) || !isRealDir(l.ProjectDir(id)) {
		return deckErr(http404, "No such design", "Нет такого макета")
	}
	return nil
}

const (
	http400 = 400
	http404 = 404
	http409 = 409
	http422 = 422
)

// ---- settings

// DeckThemesOf are the themes a deck offers: its own when it has any, else the built-in ones.
func DeckThemesOf(s DeckSettings) []DeckTheme {
	if len(s.Themes) > 0 {
		return s.Themes
	}
	return DefaultDeckThemes()
}

// ReadDeckSettings is meta/deck.json; the defaults when there is none. A file that does not parse is an error: the
// settings are not guessed.
func (l *Library) ReadDeckSettings(id string) (DeckSettings, error) {
	file := l.deckFile(id, DeckSettingsFile)
	raw, err := readFileLimited(file, maxDeckFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return DefaultDeckSettings(), nil
	}
	if err != nil {
		return DeckSettings{}, err
	}
	s := DefaultDeckSettings()
	if err := json.Unmarshal(raw, &s); err != nil {
		return DeckSettings{}, fmt.Errorf("meta/deck.json: %w", err)
	}
	if s.V == 0 {
		s.V = 1
	}
	return s, nil
}

func deckJSON(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return b.Bytes()
}

// ValidateDeckSettings checks a whole settings value against the bounds of deck_types.go.
func ValidateDeckSettings(s DeckSettings) error {
	if s.V != 1 {
		return deckErr(http400, "settings.v must be 1", "settings.v должен быть 1")
	}
	if s.Minutes != 0 && (s.Minutes < DeckMinutesMin || s.Minutes > DeckMinutesMax) {
		return deckErr(http400, "minutes: from 1 to 180, or 0 for any", "minutes: от 1 до 180 или 0, если не важно")
	}
	if s.Slides != nil && (*s.Slides < DeckSlidesMin || *s.Slides > DeckSlidesMax) {
		return deckErr(http400, "slides: from 2 to 40, or none", "slides: от 2 до 40 или пусто")
	}
	okTone := false
	for _, t := range DeckTones {
		okTone = okTone || t == s.Tone
	}
	if !okTone {
		return deckErr(http400, "tone: talk, biz or short", "tone: talk, biz или short")
	}
	if _, ok := DeckFonts[s.Font]; !ok {
		return deckErr(http400, "font: inter, golos or serif", "font: inter, golos или serif")
	}
	if s.Style != nil {
		if utf8.RuneCountInString(s.Style.Source) > 200 || s.Style.Layouts < 0 || s.Style.Layouts > 200 {
			return deckErr(http400, "style is out of bounds", "style вне допустимых границ")
		}
		if s.Style.System != nil && utf8.RuneCountInString(*s.Style.System) > 200 {
			return deckErr(http400, "style.system is too long", "style.system слишком длинный")
		}
	}
	if len(s.Themes) > maxDeckThemes {
		return deckErr(http400, "themes: too many", "themes: слишком много")
	}
	seen := map[string]bool{}
	for _, t := range s.Themes {
		if !deckThemeIDRe.MatchString(t.ID) || seen[t.ID] || t.Name == "" || utf8.RuneCountInString(t.Name) > 64 {
			return deckErr(http400, "themes: a bad id or name", "themes: плохой id или название")
		}
		seen[t.ID] = true
		if err := validateDeckVars(t.Vars); err != nil {
			return err
		}
	}
	for _, t := range DeckThemesOf(s) {
		if t.ID == s.Theme {
			return nil
		}
	}
	return deckErr(http400, "theme: no such theme", "theme: нет такой темы")
}

func validateDeckVars(vars map[string]string) error {
	if len(vars) > maxDeckThemeVars {
		return deckErr(http400, "a theme has too many variables", "в теме слишком много переменных")
	}
	for k, v := range vars {
		if !deckVarNameRe.MatchString(k) || !deckVarValueRe.MatchString(v) || deckBadValueWord.MatchString(v) || utf8.RuneCountInString(v) > deckThemeVarMax {
			return deckErr(http400, "a theme variable is not a --deck-* name with a plain value", "переменная темы — не --deck-* с простым значением")
		}
	}
	return nil
}

// DeckThemeCSS is the stylesheet the boards link: the theme's variables and the font, sorted so the same settings give
// the same bytes.
func DeckThemeCSSOf(s DeckSettings) string {
	vars := map[string]string{}
	for _, t := range DeckThemesOf(s) {
		if t.ID == s.Theme {
			for k, v := range t.Vars {
				vars[k] = v
			}
		}
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("/* Written by the runtime from meta/deck.json: change the deck's theme and font in its settings. */\n:root{")
	for _, k := range keys {
		b.WriteString(k + ":" + vars[k] + ";")
	}
	b.WriteString("--deck-font:" + DeckFonts[s.Font] + ";}\n")
	return b.String()
}

// WriteDeckSettings validates and writes meta/deck.json and, beside the boards, deck-theme.css.
func (l *Library) WriteDeckSettings(id string, s DeckSettings) error {
	if err := ValidateDeckSettings(s); err != nil {
		return err
	}
	if err := l.requireDeck(id); err != nil {
		return err
	}
	if err := l.ensureMetaDirs(id); err != nil {
		return err
	}
	if _, err := writeIfChanged(l.deckFile(id, DeckSettingsFile), deckJSON(s)); err != nil {
		return err
	}
	_, err := writeIfChanged(l.deckFile(id, DeckThemeCSS), []byte(DeckThemeCSSOf(s)))
	return err
}

// deckSettingsPatch merges the keys of a PUT onto the settings: a key that is absent stays, slides may be null.
func deckSettingsPatch(cur DeckSettings, patch map[string]json.RawMessage) (DeckSettings, error) {
	s := cur
	bad := func(k string) error {
		return deckErr(http400, "settings."+k+": a wrong value", "settings."+k+": неверное значение")
	}
	for k, raw := range patch {
		var err error
		switch k {
		case "minutes":
			err = json.Unmarshal(raw, &s.Minutes)
		case "slides":
			var n *int
			if err = json.Unmarshal(raw, &n); err == nil {
				s.Slides = n
			}
		case "tone":
			err = json.Unmarshal(raw, &s.Tone)
		case "script":
			err = json.Unmarshal(raw, &s.Script)
		case "theme":
			err = json.Unmarshal(raw, &s.Theme)
		case "font":
			err = json.Unmarshal(raw, &s.Font)
		case "style":
			var st *DeckStyle
			if err = json.Unmarshal(raw, &st); err == nil {
				s.Style = st
			}
		case "themes":
			var ts []DeckTheme
			if err = json.Unmarshal(raw, &ts); err == nil {
				s.Themes = ts
			}
		case "v":
		default:
			return s, deckErr(http400, "settings."+k+": unknown field", "settings."+k+": неизвестное поле")
		}
		if err != nil {
			return s, bad(k)
		}
	}
	return s, nil
}

// SetDeckSettings applies a partial settings value (the PUT) and returns what was written.
func (l *Library) SetDeckSettings(id string, patch map[string]json.RawMessage) (DeckSettings, error) {
	if err := l.requireDeck(id); err != nil {
		return DeckSettings{}, err
	}
	cur, err := l.ReadDeckSettings(id)
	if err != nil {
		return DeckSettings{}, err
	}
	next, err := deckSettingsPatch(cur, patch)
	if err != nil {
		return DeckSettings{}, err
	}
	if err := l.WriteDeckSettings(id, next); err != nil {
		return DeckSettings{}, err
	}
	return next, nil
}

// DeckInit is what a new deck is made with (the create dialog's settings).
type DeckInit struct {
	Minutes int
	Slides  *int
	Tone    DeckTone
	Script  bool
	// Source, Layouts and Vars come from a company template (POST /design/deck/pptx-style); Source "" when none.
	Source  string
	Layouts int
	Vars    map[string]string
}

// BuildDeckSettings are the settings of a new deck. A template's variables make a theme of their own on top of the
// built-in company theme's, and the deck starts on it.
func BuildDeckSettings(in DeckInit) DeckSettings {
	s := DefaultDeckSettings()
	s.Minutes, s.Slides, s.Tone, s.Script = in.Minutes, in.Slides, in.Tone, in.Script
	if in.Source != "" {
		s.Style = &DeckStyle{Source: in.Source, Layouts: in.Layouts}
		if len(in.Vars) > 0 {
			vars := map[string]string{}
			for k, v := range DefaultDeckThemes()[0].Vars {
				vars[k] = v
			}
			for k, v := range in.Vars {
				vars[k] = v
			}
			name := strings.TrimSuffix(in.Source, filepath.Ext(in.Source))
			if name == "" || utf8.RuneCountInString(name) > 64 {
				name = locale.Pick("Template", "Шаблон")
			}
			s.Themes = append([]DeckTheme{{ID: "tpl", Name: name, Vars: vars}}, DefaultDeckThemes()...)
			s.Theme = "tpl"
		}
	}
	return s
}

// InitDeck writes the settings of a new deck.
func (l *Library) InitDeck(id string, in DeckInit) error {
	return l.WriteDeckSettings(id, BuildDeckSettings(in))
}

// ---- notes

func (l *Library) notesFile(id, page string) string {
	return filepath.Join(l.deckFile(id, DeckNotesDir), page+".md")
}

func (l *Library) rawNotesFile(id, page string) string {
	return filepath.Join(l.deckFile(id, DeckNotesDir), page+".raw.md")
}

func readDeckText(file string) (string, bool) {
	b, err := readFileLimited(file, maxDeckFileBytes)
	if err != nil {
		return "", false
	}
	return strings.TrimRight(strings.ReplaceAll(string(b), "\r\n", "\n"), "\n\t "), true
}

// DeckWords counts the words of notes the way the page does (deck/notes-len.ts): runs of anything but white space.
func DeckWords(text string) int { return len(strings.Fields(text)) }

// slideOf builds a slide with its notes.
func (l *Library) slideOf(id string, p Page) DeckSlide {
	notes, _ := readDeckText(l.notesFile(id, p.ID))
	_, voice := readDeckText(l.rawNotesFile(id, p.ID))
	w, h := deckSlideDefaultW, deckSlideDefaultH
	if p.W != nil {
		w = *p.W
	}
	if p.H != nil {
		h = *p.H
	}
	return DeckSlide{ID: p.ID, Path: p.File, Title: p.Title, W: w, H: h, Notes: notes, Voice: voice && notes != "",
		Seconds: DeckNotesSeconds(DeckWords(notes)), Version: l.thumbVersion(id, p.File), Blank: l.blankBoard(id, p.File)}
}

var (
	boardMainRe    = regexp.MustCompile(`(?is)<main\b[^>]*\bclass\s*=\s*["'][^"']*\bboard\b[^"']*["'][^>]*>(.*)</main>`)
	htmlCommentRe  = regexp.MustCompile(`(?s)<!--.*?-->`)
	boardBodyRe    = regexp.MustCompile(`(?is)<body\b[^>]*>(.*)</body>`)
	boardScriptsRe = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script>`)
)

// blankBoard says a slide's board has nothing drawn on it: the file is empty, or its <main class="board"> holds only
// white space and comments (the template as the page command made it). A board without that main is blank only when
// its body has nothing but scripts. An unreadable file is not called blank: the stage shows what the server serves.
func (l *Library) blankBoard(id, boardPath string) bool {
	real := l.ResolveProjectFile(id, strings.Split(pageFileOf(boardPath), "/"))
	if real == "" {
		return false
	}
	b, err := readFileLimited(real, maxDeckFileBytes)
	if err != nil {
		return false
	}
	return BlankBoardHTML(string(b))
}

// BlankBoardHTML is blankBoard on the board's text.
func BlankBoardHTML(html string) bool {
	if strings.TrimSpace(html) == "" {
		return true
	}
	if m := boardMainRe.FindStringSubmatch(html); m != nil {
		return strings.TrimSpace(htmlCommentRe.ReplaceAllString(m[1], "")) == ""
	}
	if m := boardBodyRe.FindStringSubmatch(html); m != nil {
		return strings.TrimSpace(boardScriptsRe.ReplaceAllString(htmlCommentRe.ReplaceAllString(m[1], ""), "")) == ""
	}
	return false
}

// DeckSlides are the pages of the deck whose board file is there, in order.
func (l *Library) DeckSlides(id string) []DeckSlide {
	out := []DeckSlide{}
	m := l.ReadModel(id)
	if m == nil {
		return out
	}
	project := l.ProjectDir(id)
	for _, p := range m.Pages {
		if exists(filepath.Join(append([]string{project}, strings.Split(pageFileOf(p.File), "/")...)...)) {
			out = append(out, l.slideOf(id, p))
		}
	}
	return out
}

// deckPage finds a slide by id, file or title.
func (l *Library) deckPage(id, ref string) (Page, error) {
	m := l.ReadModel(id)
	if m == nil {
		return Page{}, deckErr(http404, "The project has no pages (meta/ is missing)", "В проекте нет страниц (нет meta/)")
	}
	if p, ok := m.Page(ref); ok {
		return p, nil
	}
	var byTitle []Page
	for _, p := range m.Pages {
		if p.File == ref || pageFileOf(p.File) == ref {
			return p, nil
		}
		if strings.EqualFold(p.Title, ref) {
			byTitle = append(byTitle, p)
		}
	}
	if len(byTitle) == 1 {
		return byTitle[0], nil
	}
	if len(byTitle) > 1 {
		return Page{}, deckErr(http409, "Several slides have that title: use the id", "У нескольких слайдов такое название: укажите номер")
	}
	return Page{}, deckErr(http404, "No such slide", "Нет такого слайда")
}

func (l *Library) writeNotes(id, page, text, raw string) error {
	dir := l.deckFile(id, DeckNotesDir)
	if err := l.ensureMetaDirs(id); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o777); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if !isRealDir(dir) {
		return errors.New(dir + " is not a plain folder")
	}
	remove := func(f string) error {
		if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if text == "" {
		if err := remove(l.notesFile(id, page)); err != nil {
			return err
		}
	} else if _, err := writeIfChanged(l.notesFile(id, page), []byte(text+"\n")); err != nil {
		return err
	}
	if raw == "" {
		return remove(l.rawNotesFile(id, page))
	}
	_, err := writeIfChanged(l.rawNotesFile(id, page), []byte(raw+"\n"))
	return err
}

func cleanDeckNotes(text string) (string, error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if utf8.RuneCountInString(text) > maxDeckNotesRunes {
		return "", deckErr(http400, "The notes are over 20000 characters", "Заметки длиннее 20000 знаков")
	}
	if strings.ContainsRune(text, 0) {
		return "", deckErr(http400, "The notes have a null character", "В заметках нулевой символ")
	}
	return text, nil
}

// SetDeckNotes writes a slide's notes by hand (or by the agent's command): the dictation they may have come from is
// dropped, the notes are no longer "из диктовки".
func (l *Library) SetDeckNotes(id, ref, text string) (DeckSlide, error) {
	if err := l.requireDeck(id); err != nil {
		return DeckSlide{}, err
	}
	p, err := l.deckPage(id, ref)
	if err != nil {
		return DeckSlide{}, err
	}
	text, err = cleanDeckNotes(text)
	if err != nil {
		return DeckSlide{}, err
	}
	if err := l.writeNotes(id, p.ID, text, ""); err != nil {
		return DeckSlide{}, err
	}
	return l.slideOf(id, p), nil
}

// SetDeckNotesFromDictation writes the notes the model made and the dictation they came from.
func (l *Library) SetDeckNotesFromDictation(id, pageID, notes, raw string) (DeckSlide, error) {
	p, err := l.deckPage(id, pageID)
	if err != nil {
		return DeckSlide{}, err
	}
	if notes, err = cleanDeckNotes(notes); err != nil {
		return DeckSlide{}, err
	}
	raw = strings.TrimSpace(raw)
	if utf8.RuneCountInString(raw) > maxDeckNotesRunes {
		raw = string([]rune(raw)[:maxDeckNotesRunes])
	}
	if err := l.writeNotes(id, p.ID, notes, raw); err != nil {
		return DeckSlide{}, err
	}
	return l.slideOf(id, p), nil
}

// ---- comments

func (l *Library) commentsDir(id string) string { return l.deckFile(id, DeckCommentsDir) }

// ReadDeckComments are the team comments (oldest first) and the ids of the guest comments marked resolved.
func (l *Library) ReadDeckComments(id string) ([]DeckComment, []string) {
	comments, guests := []DeckComment{}, []string{}
	dir := l.commentsDir(id)
	if !isRealDir(dir) {
		return comments, guests
	}
	names, _ := readNames(dir)
	for _, n := range names {
		m := deckCommentFile.FindStringSubmatch(n)
		if m == nil {
			continue
		}
		raw, err := readFileLimited(filepath.Join(dir, n), 64*1024)
		if err != nil {
			continue
		}
		if strings.HasPrefix(m[1], DeckGuestPrefix) {
			var g DeckGuestResolution
			if json.Unmarshal(raw, &g) == nil && g.Resolved {
				guests = append(guests, m[1])
			}
			continue
		}
		var c DeckComment
		if json.Unmarshal(raw, &c) != nil || c.ID != m[1] {
			continue
		}
		comments = append(comments, c)
		if len(comments) >= maxDeckComments {
			break
		}
	}
	sort.SliceStable(comments, func(i, j int) bool {
		if comments[i].At != comments[j].At {
			return comments[i].At < comments[j].At
		}
		return comments[i].ID < comments[j].ID
	})
	sort.Strings(guests)
	return comments, guests
}

// DeckCommentSpec is a new comment's content.
type DeckCommentSpec struct {
	Page  string
	X, Y  float64
	Label string
	Text  string
}

func cleanCommentText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	for _, r := range s {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (l *Library) author() (DeckPerson, error) {
	who, err := l.Identity()
	if err != nil {
		return DeckPerson{}, err
	}
	return DeckPerson{Name: who.Person, Email: who.PersonID}, nil
}

func (l *Library) writeCommentFile(id, name string, v any) error {
	dir := l.commentsDir(id)
	if err := l.ensureMetaDirs(id); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o777); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if !isRealDir(dir) {
		return errors.New(dir + " is not a plain folder")
	}
	_, err := writeIfChanged(filepath.Join(dir, name+".json"), deckJSON(v))
	return err
}

// AddDeckComment pins a comment to a point of a slide, in the name of the runtime's person.
func (l *Library) AddDeckComment(id string, spec DeckCommentSpec) (DeckComment, error) {
	if err := l.requireDeck(id); err != nil {
		return DeckComment{}, err
	}
	p, err := l.deckPage(id, spec.Page)
	if err != nil {
		return DeckComment{}, err
	}
	text := cleanCommentText(spec.Text)
	if text == "" || utf8.RuneCountInString(text) > maxDeckCommentRune {
		return DeckComment{}, deckErr(http400, "The comment is empty or over 2000 characters", "Комментарий пуст или длиннее 2000 знаков")
	}
	bad := func(f float64) bool { return math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 100 }
	if bad(spec.X) || bad(spec.Y) {
		return DeckComment{}, deckErr(http400, "x and y are percents from 0 to 100", "x и y — проценты от 0 до 100")
	}
	label, _ := CleanText(spec.Label, 80)
	if existing, _ := l.ReadDeckComments(id); len(existing) >= maxDeckComments {
		return DeckComment{}, deckErr(http409, "Too many comments on the deck", "Слишком много комментариев к презентации")
	}
	by, err := l.author()
	if err != nil {
		return DeckComment{}, err
	}
	c := DeckComment{ID: NewPageID(), Page: p.ID, X: math.Round(spec.X*100) / 100, Y: math.Round(spec.Y*100) / 100, Label: label,
		Text: text, By: by, At: l.clock().UTC().Format(time.RFC3339)}
	if err := l.writeCommentFile(id, c.ID, c); err != nil {
		return DeckComment{}, err
	}
	return c, nil
}

// DeckResolved is the answer of a resolve of a guest comment.
type DeckResolved struct {
	ID       string `json:"id"`
	Resolved bool   `json:"resolved"`
}

// ResolveDeckComment marks a comment resolved or open. A team comment answers whole; a guest comment (g-<id>, kept by
// the gateway) keeps only its flag here.
func (l *Library) ResolveDeckComment(id, commentID string, resolved bool) (any, error) {
	if err := l.requireDeck(id); err != nil {
		return nil, err
	}
	by, err := l.author()
	if err != nil {
		return nil, err
	}
	at := l.clock().UTC().Format(time.RFC3339)
	if deckGuestIDRe.MatchString(commentID) {
		if err := l.writeCommentFile(id, commentID, DeckGuestResolution{Resolved: resolved, By: by.Name, At: at}); err != nil {
			return nil, err
		}
		return DeckResolved{ID: commentID, Resolved: resolved}, nil
	}
	if !deckCommentIDRe.MatchString(commentID) {
		return nil, deckErr(http400, "Not a comment id", "Это не номер комментария")
	}
	file := filepath.Join(l.commentsDir(id), commentID+".json")
	raw, err := readFileLimited(file, 64*1024)
	if err != nil {
		return nil, deckErr(http404, "No such comment", "Нет такого комментария")
	}
	var c DeckComment
	if json.Unmarshal(raw, &c) != nil || c.ID != commentID {
		return nil, deckErr(http404, "No such comment", "Нет такого комментария")
	}
	c.Resolved = resolved
	if resolved {
		c.ResolvedBy, c.ResolvedAt = &by.Name, &at
	} else {
		c.ResolvedBy, c.ResolvedAt = nil, nil
	}
	if err := l.writeCommentFile(id, c.ID, c); err != nil {
		return nil, err
	}
	return c, nil
}

// ---- state

// deckRev is the newest mtime (ms) of the deck's files and of the folders that hold them (a deleted file moves its
// folder's), so a change anywhere in the deck moves it; recordings and what is published come from DeckRecsRev.
func (l *Library) deckRev(id string) int64 {
	var newest int64
	touch := func(path string) {
		if st, err := os.Lstat(path); err == nil && st.ModTime().UnixMilli() > newest {
			newest = st.ModTime().UnixMilli()
		}
	}
	scan := func(dir string) {
		touch(dir)
		names, _ := readNames(dir)
		for _, n := range names {
			touch(filepath.Join(dir, n))
		}
	}
	touch(l.deckFile(id, DeckSettingsFile))
	touch(l.deckFile(id, DeckThemeCSS))
	scan(l.pagesDir(id))
	scan(l.deckFile(id, DeckNotesDir))
	scan(l.commentsDir(id))
	touch(l.deckFile(id, DeckPublishedDir))
	touch(l.deckFile(id, DeckRecDir))
	if rev := l.DeckRecsRev(id); rev > newest {
		newest = rev
	}
	return newest
}

// DeckState is everything the deck page reads in one go.
func (l *Library) DeckState(id string) (DeckState, error) {
	if err := l.requireDeck(id); err != nil {
		return DeckState{}, err
	}
	settings, err := l.ReadDeckSettings(id)
	if err != nil {
		return DeckState{}, err
	}
	slides := l.DeckSlides(id)
	comments, guests := l.ReadDeckComments(id)
	return DeckState{Rev: l.deckRev(id), Settings: settings, Chrome: l.findChrome() != "", Ffmpeg: DeckFFmpeg() != "",
		Slides: slides, Comments: comments, GuestResolved: guests, Recordings: l.DeckRecordings(id),
		Published: l.DeckPublishedState(id)}, nil
}
