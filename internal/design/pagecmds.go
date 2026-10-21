package design

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The page commands of a project with meta/: create a page (a new id, a board file made from the project's board
// template), rename it (its title; its file too when asked, the id stays), remove it (the board and the page file go
// to the library's .trash/), list them. The panel's board edits of such a project go through the same model. Edits of
// one project queue behind one lock.

// PageError is a refusal worded for the person or the agent, with the HTTP status that fits it.
type PageError struct {
	Msg    string
	Status int
}

func (e *PageError) Error() string { return e.Msg }

func pageErr(status int, msg string) error { return &PageError{Msg: msg, Status: status} }

// IsPageError tells a refusal from a failure.
func IsPageError(err error) bool {
	var e *PageError
	return errors.As(err, &e)
}

func (l *Library) noMetaError(id string) error {
	return pageErr(409, locale.Pick(
		"Project "+id+" keeps no page model yet (no meta/): run `"+l.cfg.Texts.CLI+" design migrate` first",
		"У проекта "+id+" ещё нет модели страниц (нет meta/): сначала `"+l.cfg.Texts.CLI+" design migrate`"))
}

func (l *Library) requireModel(id string) (*ProjectModel, error) {
	if !IsLibraryID(id) || !isRealDir(l.ProjectDir(id)) {
		return nil, pageErr(404, locale.Pick("No such project: see ", "Нет такого проекта: смотрите ")+l.cfg.Texts.CLI+" design list")
	}
	m := l.ReadModel(id)
	if m == nil {
		return nil, l.noMetaError(id)
	}
	return m, nil
}

// ResolvePage is the page a person or an agent named: its id, its file (with or without the suffix) or its title.
func ResolvePage(m *ProjectModel, ref string) (Page, error) {
	ref = jscompat.Trim(ref)
	if ref == "" {
		return Page{}, pageErr(400, locale.Pick("Name the page: its id, file or title", "Назовите страницу: номер, файл или название"))
	}
	for _, p := range m.Pages {
		if p.ID == ref {
			return p, nil
		}
	}
	for _, p := range m.Pages {
		if p.File == ref || pageFileOf(p.File) == ref || (IsBoardFile(p.File) && (BoardTitleOf(p.File) == ref || p.File == ref+dcSuffix)) {
			return p, nil
		}
	}
	var found []Page
	for _, p := range m.Pages {
		if p.Title == ref {
			found = append(found, p)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return Page{}, pageErr(404, locale.Pick("No such page: \""+ref+"\"", "Нет такой страницы: «"+ref+"»"))
	}
	return Page{}, pageErr(409, locale.Pick("Several pages are called \""+ref+"\": name it by its id", "Страниц с названием «"+ref+"» несколько: назовите её номером"))
}

var fileNameBad = regexp.MustCompile("[\x00-\x1f\x7f/\\\\:*?\"<>|]+")

// fileStemOf is a board file name made from a title: what a file name cannot hold becomes a dash.
func fileStemOf(title string) string {
	s := jscompat.Trim(spaceRun.ReplaceAllString(fileNameBad.ReplaceAllString(title, "-"), " "))
	s = strings.TrimLeft(s, ".- ")
	s = jscompat.Trim(jscompat.Slice(s, 100))
	if s == "" || IsTemplateFile(s+dcSuffix) {
		s = locale.Pick("Page", "Страница")
	}
	return s
}

// cleanBoardFileName is a file an agent asked for: a *.dc.html at the root of project/.
func cleanBoardFileName(v string) (string, error) {
	name := jscompat.Trim(v)
	if name != "" && !strings.HasSuffix(name, dcSuffix) {
		name += dcSuffix
	}
	if !IsBoardFile(name) || ParseBoardPath(name) == "" || IsTemplateFile(name) || strings.HasPrefix(name, ".") || jscompat.Len(name) > 160 {
		return "", pageErr(400, locale.Pick("A page file is a name ending in .dc.html, without folders", "Файл страницы — имя с .dc.html на конце, без папок"))
	}
	return name, nil
}

func (l *Library) freeFileName(id string, m *ProjectModel, stem string) string {
	for n := 1; n < 1000; n++ {
		name := stem
		if n > 1 {
			name += " " + strconv.Itoa(n)
		}
		name += dcSuffix
		if !exists(filepath.Join(l.ProjectDir(id), name)) && m.PageOfFile(name) == "" {
			return name
		}
	}
	return stem + " " + randomHex(4) + dcSuffix
}

// boardSeed is what a new page's file starts as: the project's own board template, else the one shipped with the
// runtime.
func (l *Library) boardSeed(id string) []byte {
	for _, name := range []string{"_board-template.html", "_board-template.dc.html"} {
		file := filepath.Join(l.ProjectDir(id), name)
		if isRealFile(file) {
			if b, err := readFileLimited(file, 2<<20); err == nil {
				return b
			}
		}
	}
	return defaultBoardTemplate()
}

func writeExclusive(file string, data []byte) error {
	f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o666)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return pageErr(409, locale.Pick("The file is already there", "Такой файл уже есть"))
		}
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func maxOrder(m *ProjectModel) float64 {
	top := 0.0
	for i, p := range m.Pages {
		v := float64(i + 1)
		if p.Order != nil {
			v = *p.Order
		}
		top = math.Max(top, v)
	}
	return top
}

// PageSpec is what a new page is asked to be; Title is required.
type PageSpec struct {
	Title     string
	File      string
	Route     *string
	VariantOf string
	W, H      *int
	Expand    bool
	Widths    []int
}

func cleanRoute(v *string) (*string, error) {
	if v == nil {
		return nil, nil
	}
	r, ok := CleanText(*v, 300)
	if !ok {
		return nil, nil
	}
	if !strings.HasPrefix(r, "/") {
		return nil, pageErr(400, locale.Pick("A route starts with /", "Адрес начинается с /"))
	}
	return &r, nil
}

func (l *Library) afterPageChange(id string) {
	l.listing.invalidatePath(id)
	if _, err := l.WriteDerived(id); err != nil {
		l.warn("design: the files built from meta/ were not written", err)
	}
}

// CreatePage adds a page: a new id, the file named after the title (or as asked), its content the project's board
// template, or a copy of the page it is a variant of. A file that is there and no page names is taken as it is.
func (l *Library) CreatePage(id string, s PageSpec) (Page, error) {
	mu := l.lockOf("boards\x00" + id)
	mu.Lock()
	defer mu.Unlock()
	m, err := l.requireModel(id)
	if err != nil {
		return Page{}, err
	}
	title, err := cleanBoardTitle(&s.Title)
	if err != nil {
		return Page{}, pageErr(400, err.Error())
	}
	route, err := cleanRoute(s.Route)
	if err != nil {
		return Page{}, err
	}
	var base *Page
	if s.VariantOf != "" {
		b, err := ResolvePage(m, s.VariantOf)
		if err != nil {
			return Page{}, err
		}
		base = &b
	}
	file, adopt := "", false
	if s.File != "" {
		if file, err = cleanBoardFileName(s.File); err != nil {
			return Page{}, err
		}
		if other := m.PageOfFile(file); other != "" {
			return Page{}, pageErr(409, locale.Pick("The file "+file+" is already page "+other, "Файл "+file+" уже страница "+other))
		}
		adopt = exists(filepath.Join(l.ProjectDir(id), file))
		if adopt {
			if _, err := l.boardFile(id, file); err != nil {
				return Page{}, pageErr(409, err.Error())
			}
		}
	} else {
		file = l.freeFileName(id, m, fileStemOf(title))
	}
	if len(m.Pages) >= LimitBoards {
		return Page{}, pageErr(422, locale.Pick("The project has too many pages", "В проекте слишком много страниц"))
	}
	order := maxOrder(m) + 1
	p := Page{ID: NewPageID(), Title: title, File: file, Route: route, W: s.W, H: s.H, Widths: s.Widths, Order: &order}
	if s.Expand {
		fill := "fill"
		p.Expand = &fill
	}
	if base != nil {
		p.VariantOf = &base.ID
		if p.W == nil && p.H == nil {
			p.W, p.H = base.W, base.H
		}
	}
	if !adopt {
		seed := l.boardSeed(id)
		if base != nil && IsBoardFile(base.File) {
			if src, err := l.boardFile(id, base.File); err == nil {
				if b, err := readFileLimited(src, 16<<20); err == nil {
					seed = b
				}
			}
		}
		if err := writeExclusive(filepath.Join(l.ProjectDir(id), file), seed); err != nil {
			return Page{}, err
		}
	}
	if _, err := l.writePage(id, p); err != nil {
		if !adopt {
			_ = os.Remove(filepath.Join(l.ProjectDir(id), file))
		}
		return Page{}, err
	}
	l.afterPageChange(id)
	return p, nil
}

// RenamePage changes a page's title and, when file is given, its board file's name; the id stays, and with it the
// page's claims and comments.
func (l *Library) RenamePage(id, ref string, title, file *string) (Page, error) {
	mu := l.lockOf("boards\x00" + id)
	mu.Lock()
	defer mu.Unlock()
	m, err := l.requireModel(id)
	if err != nil {
		return Page{}, err
	}
	p, err := ResolvePage(m, ref)
	if err != nil {
		return Page{}, err
	}
	if title == nil && file == nil {
		return Page{}, pageErr(400, locale.Pick("Give a new title (--title) or file (--file)", "Укажите новое название (--title) или файл (--file)"))
	}
	if title != nil {
		t, err := cleanBoardTitle(title)
		if err != nil {
			return Page{}, pageErr(400, err.Error())
		}
		p.Title = t
	}
	if file != nil {
		name, err := cleanBoardFileName(*file)
		if err != nil {
			return Page{}, err
		}
		if name != p.File {
			if !IsBoardFile(p.File) {
				return Page{}, pageErr(422, locale.Pick("Only a board file can be renamed; this page shows "+p.File, "Переименовать можно только файл доски; эта страница показывает "+p.File))
			}
			if other := m.PageOfFile(name); other != "" || exists(filepath.Join(l.ProjectDir(id), name)) {
				return Page{}, pageErr(409, locale.Pick("The file "+name+" is already there", "Файл "+name+" уже есть"))
			}
			src, err := l.boardFile(id, p.File)
			if err != nil {
				return Page{}, pageErr(404, err.Error())
			}
			// A file that appeared at the new name in between is never replaced.
			if err := renameInDir(filepath.Dir(src), filepath.Base(src), name); err != nil {
				if isExist(err) {
					return Page{}, pageErr(409, locale.Pick("The file "+name+" is already there", "Файл "+name+" уже есть"))
				}
				return Page{}, err
			}
			p.File = name
			if _, err := l.writePage(id, p); err != nil {
				_ = renameInDir(filepath.Dir(src), name, filepath.Base(src))
				return Page{}, err
			}
			l.afterPageChange(id)
			return p, nil
		}
	}
	if _, err := l.writePage(id, p); err != nil {
		return Page{}, err
	}
	l.afterPageChange(id)
	return p, nil
}

func (l *Library) trashPageFile(id, pageID string) error {
	trash := filepath.Join(l.Root(), ".trash")
	if err := os.MkdirAll(trash, 0o777); err != nil {
		return err
	}
	return os.Rename(filepath.Join(l.pagesDir(id), pageID+".json"), filepath.Join(trash, id+"-"+strconv.FormatInt(nowMs(), 10)+"-"+MetaDirname+"-"+pageID+".json"))
}

func (l *Library) removePageLocked(id string, m *ProjectModel, p Page) error {
	still := false
	for _, o := range m.Pages {
		still = still || (o.ID != p.ID && pageFileOf(o.File) == pageFileOf(p.File))
	}
	if IsBoardFile(p.File) && !still && exists(filepath.Join(l.ProjectDir(id), p.File)) {
		if err := l.trashFile(id, p.File); err != nil {
			return err
		}
	}
	return l.trashPageFile(id, p.ID)
}

// RemovePage moves the page's board file (unless another page shows it) and its page file to the library's .trash/.
func (l *Library) RemovePage(id, ref string) (Page, error) {
	mu := l.lockOf("boards\x00" + id)
	mu.Lock()
	defer mu.Unlock()
	m, err := l.requireModel(id)
	if err != nil {
		return Page{}, err
	}
	p, err := ResolvePage(m, ref)
	if err != nil {
		return Page{}, err
	}
	if err := l.removePageLocked(id, m, p); err != nil {
		return Page{}, err
	}
	l.afterPageChange(id)
	return p, nil
}

// PageView is a page with whether its board file is there.
type PageView struct {
	Page
	Present bool `json:"present"`
}

// PageListing is a project's pages, and the board files of project/ no page names.
type PageListing struct {
	Project      string     `json:"project"`
	Release      string     `json:"release"`
	Pages        []PageView `json:"pages"`
	Unregistered []string   `json:"unregistered"`
}

func (l *Library) ListPages(id string) (PageListing, error) {
	m, err := l.requireModel(id)
	if err != nil {
		return PageListing{}, err
	}
	out := PageListing{Project: id, Release: m.Meta.Release, Pages: []PageView{}, Unregistered: []string{}}
	project := l.ProjectDir(id)
	for _, p := range m.Pages {
		out.Pages = append(out.Pages, PageView{Page: p, Present: exists(filepath.Join(append([]string{project}, strings.Split(pageFileOf(p.File), "/")...)...))})
	}
	for _, b := range modelBoards(project, m) {
		if b.ID == "" {
			out.Unregistered = append(out.Unregistered, b.Path)
		}
	}
	return out, nil
}

// adoptUnregistered gives every board the listing shows without a page one, after the others: the panel changes the
// list as it sees it.
func (l *Library) adoptUnregistered(id string, m *ProjectModel) (*ProjectModel, error) {
	order := maxOrder(m)
	adopted := false
	for _, b := range modelBoards(l.ProjectDir(id), m) {
		if b.ID != "" {
			continue
		}
		order++
		o := order
		if _, err := l.writePage(id, Page{ID: NewPageID(), Title: b.Title, File: b.Path, W: b.W, H: b.H, Order: &o}); err != nil {
			return nil, err
		}
		adopted = true
	}
	if !adopted {
		return m, nil
	}
	return l.ReadModel(id), nil
}

// renumber writes the pages' order as their positions in pages.
func (l *Library) renumber(id string, pages []Page) error {
	for i, p := range pages {
		o := float64(i + 1)
		p.Order = &o
		if _, err := l.writePage(id, p); err != nil {
			return err
		}
	}
	return nil
}

// editPages is EditBoards of a project with meta/.
func (l *Library) editPages(id string, e BoardEdit) ([]Board, error) {
	m := l.ReadModel(id)
	if m == nil {
		return nil, editErr(locale.Pick("The design is gone", "Макета больше нет"))
	}
	if e.Action == "title" {
		t, err := cleanBoardTitle(e.Title)
		if err != nil {
			return nil, err
		}
		m.Meta.Title = t
		if _, err := l.writeProjectMeta(id, m.Meta); err != nil {
			return nil, err
		}
		l.afterPageChange(id)
		return modelBoards(l.ProjectDir(id), m), nil
	}
	m, err := l.adoptUnregistered(id, m)
	if err != nil {
		return nil, err
	}
	project := l.ProjectDir(id)
	shown := modelBoards(project, m)
	at := -1
	for i, b := range shown {
		if e.Path != nil && b.Path == *e.Path {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, editErr(locale.Pick("No such board", "Такой доски нет"))
	}
	page, _ := m.Page(shown[at].ID)
	pages := append([]Page{}, m.Pages...)
	pos := -1
	for i, p := range pages {
		if p.ID == page.ID {
			pos = i
		}
	}
	switch e.Action {
	case "rename":
		t, err := cleanBoardTitle(e.Title)
		if err != nil {
			return nil, err
		}
		page.Title = t
		if _, err := l.writePage(id, page); err != nil {
			return nil, err
		}
	case "move":
		d := 0.0
		if e.Delta != nil {
			d = *e.Delta
		}
		// Moved past its neighbour on the list as shown; pages whose files are gone keep their places.
		to := at
		if d > 0 && at+1 < len(shown) {
			to = at + 1
		} else if d < 0 && at > 0 {
			to = at - 1
		}
		if to != at {
			other := -1
			for i, p := range pages {
				if p.ID == shown[to].ID {
					other = i
				}
			}
			pages[pos], pages[other] = pages[other], pages[pos]
			if err := l.renumber(id, pages); err != nil {
				return nil, err
			}
		}
	case "duplicate":
		if !IsBoardFile(page.File) {
			return nil, editErr(locale.Pick("Only a board file can be copied; the admin screen is built from a shared bundle", "Копировать можно доску-файл; экран админки собран из общего бандла"))
		}
		name, err := l.copyName(project, page.File)
		if err != nil {
			return nil, err
		}
		src, err := l.boardFile(id, page.File)
		if err != nil {
			return nil, err
		}
		if err := copyExclusive(src, filepath.Join(project, name)); err != nil {
			return nil, err
		}
		dup := Page{ID: NewPageID(), Title: page.Title + locale.Pick(" copy", " копия"), File: name, W: page.W, H: page.H, Expand: page.Expand}
		pages = append(pages[:pos+1], append([]Page{dup}, pages[pos+1:]...)...)
		if err := l.renumber(id, pages); err != nil {
			return nil, err
		}
	case "remove":
		if len(shown) == 1 {
			return nil, editErr(locale.Pick("The last board cannot be removed: remove the whole design", "Последнюю доску не удалить: удалите макет целиком"))
		}
		if err := l.removePageLocked(id, m, page); err != nil {
			return nil, err
		}
	default:
		return nil, editErr(locale.Pick("Unknown action", "Неизвестное действие"))
	}
	l.afterPageChange(id)
	return modelBoards(project, l.ReadModel(id)), nil
}

var releaseRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// SetRelease sets the engine release a project with meta/ is built on, and rebuilds the files built from meta/.
func (l *Library) SetRelease(id, release string) error {
	mu := l.lockOf("boards\x00" + id)
	mu.Lock()
	defer mu.Unlock()
	m, err := l.requireModel(id)
	if err != nil {
		return err
	}
	if !releaseRe.MatchString(release) {
		return pageErr(400, locale.Pick("A release is a name like 1792547779-0491 or legacy", "Выпуск — имя вроде 1792547779-0491 или legacy"))
	}
	m.Meta.Release = release
	if _, err := l.writeProjectMeta(id, m.Meta); err != nil {
		return err
	}
	l.afterPageChange(id)
	return nil
}
