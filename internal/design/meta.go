package design

import (
	"crypto/rand"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The project model: <project>/meta/project.json says what the project is built on (its engine release, its kits) and
// <project>/meta/pages/<id>.json is one page each, under a permanent id. When meta/ is there it is the truth: the
// listing, the claims and the commands read it, and project/canvas.json and artifact.json are written from it for the
// tools that read those. A project without meta/ is read the way it always was. One file per page, so that the
// runtimes sharing a library never write one shared file for a page change; an id is a ULID, made without asking
// anyone.

const (
	MetaDirname      = "meta"
	MetaProjectFile  = "project.json"
	MetaPagesDirname = "pages"
	// LegacyRelease is the engine release of a project made before releases were tracked.
	LegacyRelease = "legacy"
)

var (
	pageIDRe    = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	canvasKeyRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
)

// IsPageID: a page id is a ULID in Crockford's base32, upper case.
func IsPageID(v string) bool { return pageIDRe.MatchString(v) }

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// ULID is the 26-character id of a time in ms and 80 bits of entropy.
func ULID(ms int64, entropy [10]byte) string {
	var b [16]byte
	t := uint64(ms) & (1<<48 - 1)
	for i := 0; i < 6; i++ {
		b[i] = byte(t >> (8 * (5 - i)))
	}
	copy(b[6:], entropy[:])
	// 128 bits as 26 five-bit digits, the first one carrying the top 3 bits.
	out := make([]byte, 26)
	var acc uint64
	bits := 2 // 130 bits of digits for 128 of data: two leading zero bits
	j := 0
	for _, c := range b {
		acc = acc<<8 | uint64(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out[j] = crockford[(acc>>uint(bits))&31]
			j++
		}
	}
	return string(out)
}

// NewPageID is a fresh ULID of now.
func NewPageID() string {
	var e [10]byte
	_, _ = rand.Read(e[:])
	return ULID(nowMs(), e)
}

// ProjectMeta is meta/project.json.
type ProjectMeta struct {
	Title     string
	Source    *string
	CreatedAt string
	Release   string
	Kits      []any
	Tailwind  bool
	// CanvasKey is the key of canvas.json the project's own data goes under, fixed when meta/ is made so that every
	// runtime and command builds the same file whatever its settings.
	CanvasKey string
	raw       *jsvalue.Object
}

// Page is meta/pages/<id>.json: File is the board under project/ (a *.dc.html, or a page with its query).
type Page struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	File      string   `json:"file"`
	Route     *string  `json:"route,omitempty"`
	Widths    []int    `json:"widths,omitempty"`
	VariantOf *string  `json:"variantOf,omitempty"`
	W         *int     `json:"w"`
	H         *int     `json:"h"`
	Expand    *string  `json:"expand,omitempty"`
	Order     *float64 `json:"order,omitempty"`
	raw       *jsvalue.Object
}

// ProjectModel is a project's meta/ as read: the pages in their order.
type ProjectModel struct {
	Meta  ProjectMeta
	Pages []Page
}

func (l *Library) MetaDir(id string) string { return filepath.Join(l.ArtifactDir(id), MetaDirname) }

func (l *Library) pagesDir(id string) string { return filepath.Join(l.MetaDir(id), MetaPagesDirname) }

func isRealDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.IsDir()
}

func isRealFile(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().IsRegular()
}

// HasMeta: the project keeps its model in meta/ (a plain folder with a plain project.json).
func (l *Library) HasMeta(id string) bool {
	if !IsLibraryID(id) {
		return false
	}
	return isRealDir(l.MetaDir(id)) && isRealFile(filepath.Join(l.MetaDir(id), MetaProjectFile))
}

const maxMetaBytes = 64 * 1024

func readSmallJSON(file string) (*jsvalue.Object, bool) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxMetaBytes {
		return nil, false
	}
	buf := make([]byte, st.Size())
	n, _ := f.ReadAt(buf, 0)
	raw, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(buf[:n])))
	if err != nil {
		return nil, false
	}
	o, ok := raw.(*jsvalue.Object)
	return o, ok
}

func parseProjectMeta(o *jsvalue.Object, fallbackTitle string) ProjectMeta {
	m := ProjectMeta{Title: fallbackTitle, Release: LegacyRelease, Kits: []any{}, raw: o}
	if t := metaText(get(o, "title"), LimitTitle); t != nil {
		m.Title = *t
	}
	m.Source = metaText(get(o, "source"), 300)
	if s, ok := CleanText(get(o, "createdAt"), 40); ok {
		m.CreatedAt = s
	}
	if s, ok := CleanText(get(o, "release"), 80); ok {
		m.Release = s
	}
	if k, ok := get(o, "kits").([]any); ok {
		m.Kits = k
	}
	m.Tailwind, _ = get(o, "tailwind").(bool)
	if s, ok := CleanText(get(o, "canvasKey"), 64); ok && canvasKeyRe.MatchString(s) {
		m.CanvasKey = s
	}
	return m
}

func intList(v any) []int {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []int
	for _, x := range arr {
		if n := metaSize(x); n != nil && len(out) < 12 {
			out = append(out, *n)
		}
	}
	return out
}

// parsePage reads a page file leniently; ok false when it is not a page of this name.
func parsePage(o *jsvalue.Object, name string) (Page, bool) {
	id, _ := get(o, "id").(string)
	if !IsPageID(id) || id+".json" != name {
		return Page{}, false
	}
	file := ParseBoardPath(get(o, "file"))
	if file == "" {
		return Page{}, false
	}
	p := Page{ID: id, File: file, Title: BoardTitleOf(pageFileOf(file)), raw: o}
	if t := metaText(get(o, "title"), LimitTitle); t != nil {
		p.Title = *t
	}
	if r, ok := CleanText(get(o, "route"), 300); ok && strings.HasPrefix(r, "/") {
		p.Route = &r
	}
	p.Widths = intList(get(o, "widths"))
	if v, _ := get(o, "variantOf").(string); IsPageID(v) && v != id {
		p.VariantOf = &v
	}
	p.W, p.H = metaSize(get(o, "w")), metaSize(get(o, "h"))
	if e, ok := get(o, "expand").(string); ok && e == "fill" {
		p.Expand = &e
	}
	if f, ok := get(o, "order").(float64); ok && !math.IsNaN(f) && !math.IsInf(f, 0) {
		p.Order = &f
	}
	return p, true
}

func sortPages(pages []Page) {
	sort.SliceStable(pages, func(i, j int) bool {
		a, b := pages[i], pages[j]
		switch {
		case a.Order != nil && b.Order != nil && *a.Order != *b.Order:
			return *a.Order < *b.Order
		case a.Order != nil && b.Order == nil:
			return true
		case a.Order == nil && b.Order != nil:
			return false
		}
		return a.ID < b.ID
	})
}

// ReadModel is the project's meta/; nil when the project has none.
func (l *Library) ReadModel(id string) *ProjectModel {
	if !l.HasMeta(id) {
		return nil
	}
	o, ok := readSmallJSON(filepath.Join(l.MetaDir(id), MetaProjectFile))
	if !ok {
		o = jsvalue.NewObject()
	}
	m := &ProjectModel{Meta: parseProjectMeta(o, id), Pages: []Page{}}
	dir := l.pagesDir(id)
	if !isRealDir(dir) {
		return m
	}
	names, _ := readNames(dir)
	seen := map[string]bool{}
	for _, n := range names {
		if !strings.HasSuffix(n, ".json") || !IsPageID(strings.TrimSuffix(n, ".json")) {
			continue
		}
		po, ok := readSmallJSON(filepath.Join(dir, n))
		if !ok {
			continue
		}
		p, ok := parsePage(po, n)
		if !ok || seen[p.File] {
			continue
		}
		seen[p.File] = true
		m.Pages = append(m.Pages, p)
		if len(m.Pages) >= LimitBoards {
			break
		}
	}
	sortPages(m.Pages)
	return m
}

func sizeValue(n *int) any {
	if n == nil {
		return nil
	}
	return float64(*n)
}

// document is the page's file: its own fields in a fixed order, then whatever else the file had.
func (p Page) document() []byte {
	o := jsvalue.NewObject("id", p.ID, "title", p.Title, "file", p.File)
	if p.Route != nil {
		o.Set("route", *p.Route)
	}
	if len(p.Widths) > 0 {
		ws := make([]any, 0, len(p.Widths))
		for _, w := range p.Widths {
			ws = append(ws, float64(w))
		}
		o.Set("widths", ws)
	}
	if p.VariantOf != nil {
		o.Set("variantOf", *p.VariantOf)
	}
	o.Set("w", sizeValue(p.W))
	o.Set("h", sizeValue(p.H))
	if p.Expand != nil {
		o.Set("expand", *p.Expand)
	}
	if p.Order != nil {
		o.Set("order", *p.Order)
	}
	if p.raw != nil {
		for _, k := range p.raw.Keys() {
			if !o.Has(k) && !pageKeys[k] {
				v, _ := p.raw.Get(k)
				o.Set(k, v)
			}
		}
	}
	return append(jsvalue.Stringify(o, "  "), '\n')
}

var pageKeys = map[string]bool{"id": true, "title": true, "file": true, "route": true, "widths": true, "variantOf": true, "w": true, "h": true, "expand": true, "order": true}

func (m ProjectMeta) document() []byte {
	o := jsvalue.NewObject("v", 1.0, "title", m.Title)
	if m.Source != nil {
		o.Set("source", *m.Source)
	}
	if m.CreatedAt != "" {
		o.Set("createdAt", m.CreatedAt)
	}
	release := m.Release
	if release == "" {
		release = LegacyRelease
	}
	kits := m.Kits
	if kits == nil {
		kits = []any{}
	}
	o.Set("release", release)
	o.Set("kits", kits)
	o.Set("tailwind", m.Tailwind)
	if m.CanvasKey != "" {
		o.Set("canvasKey", m.CanvasKey)
	}
	if m.raw != nil {
		for _, k := range m.raw.Keys() {
			if !o.Has(k) && k != "source" && k != "createdAt" && k != "canvasKey" {
				v, _ := m.raw.Get(k)
				o.Set(k, v)
			}
		}
	}
	return append(jsvalue.Stringify(o, "  "), '\n')
}

// writeIfChanged puts data at file through a fresh temporary file renamed over it, and only when the bytes differ:
// two runtimes writing the same model write nothing, so a folder sync has nothing to carry.
func writeIfChanged(file string, data []byte) (bool, error) {
	if st, err := os.Lstat(file); err == nil {
		if !st.Mode().IsRegular() {
			return false, errors.New(file + " is not a plain file")
		}
		if cur, err := os.ReadFile(file); err == nil && string(cur) == string(data) {
			return false, nil
		}
	}
	temp := filepath.Join(filepath.Dir(file), "."+filepath.Base(file)+"."+randomHex(6)+".tmp")
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o666)
	if err != nil {
		return false, err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(temp, file)
	}
	if err != nil {
		_ = os.Remove(temp)
		return false, err
	}
	return true, nil
}

// ensureMetaDirs makes meta/ and meta/pages/ as plain folders; a link or a file in their place is refused.
func (l *Library) ensureMetaDirs(id string) error {
	for _, d := range []string{l.MetaDir(id), l.pagesDir(id)} {
		if err := os.Mkdir(d, 0o777); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		if !isRealDir(d) {
			return errors.New(d + " is not a plain folder")
		}
	}
	return nil
}

func (l *Library) writePage(id string, p Page) (bool, error) {
	if err := l.ensureMetaDirs(id); err != nil {
		return false, err
	}
	return writeIfChanged(filepath.Join(l.pagesDir(id), p.ID+".json"), p.document())
}

func (l *Library) writeProjectMeta(id string, m ProjectMeta) (bool, error) {
	if err := l.ensureMetaDirs(id); err != nil {
		return false, err
	}
	return writeIfChanged(filepath.Join(l.MetaDir(id), MetaProjectFile), m.document())
}

// boardOfPage is the page as the listing shows it.
func boardOfPage(p Page) Board {
	return Board{ID: p.ID, Title: p.Title, Path: p.File, W: p.W, H: p.H}
}

// modelBoards are the pages whose file is there, then every other board file at the root of project/ that no page
// names and that is not a wrapper: a board an agent made by hand still shows, without an id until it is registered.
func modelBoards(project string, m *ProjectModel) []Board {
	out := []Board{}
	named := map[string]bool{}
	titles := map[string]bool{}
	for _, p := range m.Pages {
		named[pageFileOf(p.File)] = true
		titles[p.Title] = true
		if exists(filepath.Join(append([]string{project}, strings.Split(pageFileOf(p.File), "/")...)...)) {
			out = append(out, boardOfPage(p))
		}
	}
	for _, b := range scanBoards(project) {
		if named[b.Path] || titles[b.Title] || isWrapperFile(filepath.Join(project, b.Path)) {
			continue
		}
		out = append(out, b)
	}
	if len(out) > LimitBoards {
		out = out[:LimitBoards]
	}
	return out
}

// PageOfFile is the id of the page whose board is file ("" when none or no meta).
func (m *ProjectModel) PageOfFile(file string) string {
	if m == nil {
		return ""
	}
	for _, p := range m.Pages {
		if p.File == file || pageFileOf(p.File) == file {
			return p.ID
		}
	}
	return ""
}

// Page is a page by id.
func (m *ProjectModel) Page(id string) (Page, bool) {
	if m != nil {
		for _, p := range m.Pages {
			if p.ID == id {
				return p, true
			}
		}
	}
	return Page{}, false
}

func readFileLimited(file string, max int64) ([]byte, error) {
	f, err := os.OpenFile(file, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New(file + " is not a small plain file")
	}
	buf := make([]byte, st.Size())
	n, err := f.ReadAt(buf, 0)
	if err != nil && n != len(buf) {
		return nil, err
	}
	return buf[:n], nil
}
