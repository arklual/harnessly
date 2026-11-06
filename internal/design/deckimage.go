package design

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/imagegen"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// DesignImage is /design/image: POST {artifact, prompt, count (1-4), shape} starts a picture job for the design and
// answers {name, job}: name is the job's files' name (assets/<name>-<n>.png in the project, the data-gen-job of an
// element drawn from it), job the imagegen snapshot the page polls (`<platform>/images?id=`). GET ?artifact=&job=<name>
// lists the pictures of that name the project holds ({images: [{n, src}]}), which is how "other variants" are known
// after the job is forgotten.

// genAssetsDir is where a design's drawn pictures go.
const genAssetsDir = "assets"

// genMaxVariants is the most variants one request draws.
const genMaxVariants = 4

var genFileRe = regexp.MustCompile(`^(gen-[a-z0-9]{4,32})-([0-9]{1,2})\.(png|jpg|jpeg|webp)$`)

func newGenName() (string, error) {
	b := make([]byte, 5)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "gen-" + hex.EncodeToString(b), nil
}

// genAssetsFolder is the project's assets folder, checked to lie inside the project once its links are followed.
func (l *Library) genAssetsFolder(id string) (string, error) {
	project := l.ProjectDir(id)
	folder := filepath.Join(project, genAssetsDir)
	realProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		return "", err
	}
	// The deepest part that exists, resolved: a link in place of assets/ must not lead out.
	probe, rest := folder, ""
	for {
		if real, err := filepath.EvalSymlinks(probe); err == nil {
			probe = filepath.Join(real, rest)
			break
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return "", errors.New("no project folder")
		}
		rest = filepath.Join(filepath.Base(probe), rest)
		probe = parent
	}
	if probe != realProject && !strings.HasPrefix(probe, realProject+string(filepath.Separator)) {
		return "", errors.New("assets folder leaves the project")
	}
	return folder, nil
}

// imageBodyMax is the body of POST /design/image: an edit carries the marks the person drew as a base64 PNG, up to 1024 px a
// side; the largest such PNG is about 5.6 MB as text (measured: 3000 strokes of the thinnest brush make 0.65 MB).
const imageBodyMax = 8 << 20

func (a *API) DesignImage(r *http.Request) Answer {
	body, refused := a.guardLimit(r, imageBodyMax, http.MethodGet, http.MethodPost)
	if refused != nil {
		return *refused
	}
	h := noStore()
	if r.Method == http.MethodGet {
		return a.designImageVariants(r)
	}
	artifact := body.String("artifact")
	if !IsLibraryID(artifact) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown design", "Неизвестный макет")), h)
	}
	if no := a.gate(r, artifact, RoleEdit); no != nil {
		return *no
	}
	if a.Images == nil {
		return JSON(r, http.StatusServiceUnavailable, errorBody(locale.Pick("Pictures are not available here", "Картинки здесь недоступны")), h)
	}
	count := 1
	if n, ok := body.Number("count"); ok {
		count = int(n)
	}
	if count < 1 || count > genMaxVariants {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("1 to 4 variants", "От 1 до 4 вариантов")), h)
	}
	shape := imagegen.Shape(body.String("shape"))
	edit, refusal := a.editOf(artifact, body)
	if refusal != "" {
		return JSON(r, http.StatusBadRequest, errorBody(refusal), h)
	}
	name, err := newGenName()
	if err != nil {
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not start the picture", "Не удалось начать картинку")), h)
	}
	folder, err := a.L.genAssetsFolder(artifact)
	if err != nil {
		a.warn("design image: assets folder", err)
		return JSON(r, http.StatusConflict, errorBody(locale.Pick("The design's folder for pictures is not usable", "Папка картинок макета недоступна")), h)
	}
	snap, err := a.Images.Start(imagegen.Request{
		Prompt: body.String("prompt"), Count: count, Shape: shape,
		Owner: "design:" + artifact, CopyTo: folder, Name: name,
		Source: edit.source, Mode: edit.mode, Format: edit.format, Overlay: edit.overlay, SourceOK: edit.ok,
		// Checked right before each copy: the folder, its links followed, must still lie inside the project.
		SaveOK: func(real string) bool {
			root, err := filepath.EvalSymlinks(a.L.ProjectDir(artifact))
			if err != nil {
				return false
			}
			rel, err := filepath.Rel(root, real)
			return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
		},
	})
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, imagegen.ErrBadRequest):
			status = http.StatusBadRequest
		case errors.Is(err, imagegen.ErrBusy):
			status = http.StatusTooManyRequests
		}
		return JSON(r, status, errorBody(err.Error()), h)
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not start the picture", "Не удалось начать картинку")), h)
	}
	job, err := jsvalue.Parse(raw)
	if err != nil {
		return JSON(r, http.StatusInternalServerError, errorBody(locale.Pick("Could not start the picture", "Не удалось начать картинку")), h)
	}
	return JSON(r, http.StatusOK, jsvalue.NewObject("name", name, "job", job), h)
}

// genOverlayMax bounds the marks a request carries, as base64 text (the imagegen service bounds the decoded picture).
const genOverlayMax = 16 << 20

// genEdit is the part of a request that changes a picture of the project instead of drawing a new one.
type genEdit struct {
	source  string
	mode    imagegen.Mode
	format  string
	overlay []byte
	ok      func(string) bool
}

// projectPicture is a picture of the project named by its path in it (`assets/a.png`), as a real path confined to the
// project once its links are followed; "" when the path is not one, leaves the project or is not a held picture.
func (l *Library) projectPicture(id, rel string) string {
	if rel == "" || len(rel) > 400 || strings.HasPrefix(rel, "/") || strings.Contains(rel, "://") {
		return ""
	}
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".png", ".jpg", ".jpeg", ".webp":
	default:
		return ""
	}
	return l.ResolveProjectFile(id, strings.Split(rel, "/"))
}

// editOf reads the request's source, mode, format and marks. The picture must be one the project holds; the checking
// is repeated right when the job starts (ok), so a link changed meanwhile is not followed out of the project.
func (a *API) editOf(artifact string, body *jsvalue.Object) (genEdit, string) {
	rel := body.String("source")
	if rel == "" {
		if body.String("mode") != "" || body.String("format") != "" || body.String("overlay") != "" {
			return genEdit{}, locale.Pick("A change needs the picture to change", "Для правки нужна картинка")
		}
		return genEdit{}, ""
	}
	real := a.L.projectPicture(artifact, rel)
	if real == "" {
		return genEdit{}, locale.Pick("This picture cannot be changed from here", "Эту картинку отсюда не изменить")
	}
	var overlay []byte
	if text := body.String("overlay"); text != "" {
		if len(text) > genOverlayMax {
			return genEdit{}, locale.Pick("The marks are too big", "Пометок слишком много")
		}
		raw, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return genEdit{}, locale.Pick("The marks are not a picture", "Пометки не картинка")
		}
		overlay = raw
	}
	return genEdit{
		source: real, mode: imagegen.Mode(body.String("mode")), format: body.String("format"), overlay: overlay,
		ok: func(path string) bool { return path == real && a.L.projectPicture(artifact, rel) == real },
	}, ""
}

func (a *API) designImageVariants(r *http.Request) Answer {
	h := noStore()
	artifact := r.URL.Query().Get("artifact")
	if r.URL.Query().Has("tpl") {
		return a.designImageSource(r, artifact)
	}
	name := r.URL.Query().Get("job")
	if !IsLibraryID(artifact) || !genJobRe.MatchString(name) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown picture", "Неизвестная картинка")), h)
	}
	if no := a.gate(r, artifact, RoleView); no != nil {
		return *no
	}
	folder, err := a.L.genAssetsFolder(artifact)
	if err != nil {
		return JSON(r, http.StatusConflict, errorBody(locale.Pick("The design's folder for pictures is not usable", "Папка картинок макета недоступна")), h)
	}
	type pic struct {
		n   int
		src string
	}
	var pics []pic
	entries, _ := os.ReadDir(folder)
	for _, e := range entries {
		m := genFileRe.FindStringSubmatch(e.Name())
		if m == nil || m[1] != name || !e.Type().IsRegular() {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		pics = append(pics, pic{n, genAssetsDir + "/" + e.Name()})
	}
	sort.Slice(pics, func(i, j int) bool { return pics[i].n < pics[j].n })
	images := make([]any, 0, len(pics))
	for _, p := range pics {
		images = append(images, jsvalue.NewObject("n", float64(p.n), "src", p.src))
	}
	return JSON(r, http.StatusOK, jsvalue.NewObject("images", images), h)
}

// DesignImageFile is GET ?artifact=&file= — a picture a job drew into the project (assets/<job>-<n>.png), for the
// thumbnails of the choice. ?file= serves only a job's own file names, from the project's assets folder; ?path= any
// picture the project holds (the original of a change), by its path in the project, links followed and confined to it.
func (a *API) DesignImageFile(w http.ResponseWriter, r *http.Request) {
	if !a.imageGuard(w, r) {
		return
	}
	artifact, file, rel := QueryString(r, "artifact"), QueryString(r, "file"), QueryString(r, "path")
	if !IsLibraryID(artifact) || (rel == "" && !genFileRe.MatchString(file)) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if a.denyImage(w, r, artifact) {
		return
	}
	var path string
	if rel != "" {
		path, file = a.L.projectPicture(artifact, rel), filepath.Base(rel)
	} else {
		path = a.L.ResolveProjectFile(artifact, []string{genAssetsDir, file})
	}
	data, err := os.ReadFile(path)
	if path == "" || err != nil {
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNotFound)
		return
	}
	h := w.Header()
	h.Set("Content-Type", ContentType(extname(file)))
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

var cssURLRe = regexp.MustCompile(`url\(\s*(?:"([^"]*)"|'([^']*)'|([^)\s"']*))\s*\)`)

// pictureOf is the picture the board's source gives an element: an img's src, or the url() of a block's background.
// The page sees heavy files by their content address (/__h/…), so the file's own path is only here. "" when the element
// names no picture of its own.
func (b *dcBoard) pictureOf(e *dcElem) string {
	var raw string
	if e.name == "img" {
		raw, _ = attrOf(e.node, "src")
	} else {
		style, _ := attrOf(e.node, "style")
		for _, d := range parseStyle(style) {
			if d.prop != "background-image" && d.prop != "background" {
				continue
			}
			if m := cssURLRe.FindStringSubmatch(d.text); m != nil {
				raw = m[1] + m[2] + m[3]
			}
		}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "{{") || strings.Contains(raw, "://") || strings.HasPrefix(raw, "/") {
		return ""
	}
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	raw = strings.TrimPrefix(raw, "./")
	if !pictureSrcOK(raw) {
		return ""
	}
	return raw
}

// designImageSource is GET ?artifact=&(page=|file=)&tpl=&was= — the project's own picture of an element of a board, as
// {src}: what the editor starts from. 409 when the element is no longer the one the page saw, 422 when it has no
// picture of the project's own (a remote one, an interpolated one) or the file is not one the project holds.
func (a *API) designImageSource(r *http.Request, artifact string) Answer {
	h := noStore()
	q := r.URL.Query()
	tpl, err := strconv.Atoi(q.Get("tpl"))
	page, file, was := q.Get("page"), q.Get("file"), q.Get("was")
	if !IsLibraryID(artifact) || err != nil || tpl < 0 || was == "" || (!IsPageID(page) && !(page == "" && IsBoardFile(file))) {
		return JSON(r, http.StatusBadRequest, errorBody(locale.Pick("Unknown picture", "Неизвестная картинка")), h)
	}
	if no := a.gate(r, artifact, RoleEdit); no != nil {
		return *no
	}
	if page != "" {
		p, ok := a.L.ReadModel(artifact).Page(page)
		if !ok {
			return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such slide", "Нет такого слайда")), h)
		}
		file = pageFileOf(p.File)
	}
	if !IsBoardFile(file) {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such slide", "Нет такого слайда")), h)
	}
	raw, err := readFileLimited(filepath.Join(a.L.ProjectDir(artifact), file), deckBoardMax)
	if err != nil {
		return JSON(r, http.StatusNotFound, errorBody(locale.Pick("No such slide", "Нет такого слайда")), h)
	}
	board, ok := parseDcBoard(string(raw))
	if !ok {
		return JSON(r, http.StatusConflict, jsvalue.NewObject("error", locale.Pick("The block has changed", "Блок изменился"), "code", DeckPatchChanged), h)
	}
	e := board.byFingerprint(tpl, was)
	if e == nil {
		return JSON(r, http.StatusConflict, jsvalue.NewObject("error", locale.Pick("The block has changed", "Блок изменился"), "code", DeckPatchChanged), h)
	}
	src := board.pictureOf(e)
	if src == "" || a.L.projectPicture(artifact, src) == "" {
		return JSON(r, http.StatusUnprocessableEntity, errorBody(locale.Pick("This picture cannot be changed from here", "Эту картинку отсюда не изменить")), h)
	}
	return JSON(r, http.StatusOK, jsvalue.NewObject("src", src), h)
}
