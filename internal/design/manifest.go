package design

import (
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

const (
	canvasJSONName = "canvas.json"
	SupportJSName  = "support.js"
)

// ArtifactMeta is what artifact.json knows, read leniently.
type ArtifactMeta struct {
	Title, Source *string
	Boards        []Board
}

func metaText(v any, max int) *string {
	s, ok := v.(string)
	if !ok {
		return nil
	}
	t := jscompat.Trim(s)
	if t == "" || jscompat.Len(s) > max {
		return nil
	}
	return &t
}

func metaSize(v any) *int {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) || f <= 0 || f >= 20000 {
		return nil
	}
	n := int(math.Floor(f + 0.5))
	return &n
}

var (
	boardPathBad  = regexp.MustCompile(`[\x00-\x1f\\]`)
	boardPathHTML = regexp.MustCompile(`(?i)\.html?$`)
)

// ParseBoardPath is a path under project/ to an html page, with an optional query; "" when it is not one.
func ParseBoardPath(raw any) string {
	s, ok := raw.(string)
	if !ok || s == "" || jscompat.Len(s) > LimitBoardPath || boardPathBad.MatchString(s) {
		return ""
	}
	pathname := s
	if at := strings.IndexByte(s, '?'); at >= 0 {
		pathname = s[:at]
	}
	if !boardPathHTML.MatchString(pathname) {
		return ""
	}
	for _, seg := range strings.Split(pathname, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return ""
		}
	}
	return s
}

func parseBoards(v any) []Board {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	if len(arr) > LimitBoards {
		arr = arr[:LimitBoards]
	}
	var out []Board
	for _, item := range arr {
		obj, ok := item.(*jsvalue.Object)
		if !ok {
			continue
		}
		p := ParseBoardPath(get(obj, "path"))
		if p == "" {
			continue
		}
		title := p
		if t := metaText(get(obj, "title"), LimitTitle); t != nil {
			title = *t
		}
		out = append(out, Board{Title: title, Path: p, W: metaSize(get(obj, "w")), H: metaSize(get(obj, "h"))})
	}
	return out
}

func get(o *jsvalue.Object, k string) any {
	v, _ := o.Get(k)
	return v
}

func ParseArtifactFile(raw any) ArtifactMeta {
	obj, ok := raw.(*jsvalue.Object)
	if !ok {
		return ArtifactMeta{}
	}
	return ArtifactMeta{Title: metaText(get(obj, "title"), LimitTitle), Source: metaText(get(obj, "source"), 300), Boards: parseBoards(get(obj, "boards"))}
}

func readJSON(file string) any {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(b)))
	if err != nil {
		return nil
	}
	return v
}

func exists(file string) bool {
	_, err := os.Stat(file)
	return err == nil
}

var templateFileRe = regexp.MustCompile(`^_board-template(\.dc)?\.html$`)

// IsTemplateFile: the copy-me page of a new project is a pattern for agents, not a board.
func IsTemplateFile(name string) bool { return templateFileRe.MatchString(name) }

func scanBoards(dir string) []Board {
	entries, _ := os.ReadDir(dir)
	var files []string
	for _, e := range entries {
		if e.Type().IsRegular() && IsBoardFile(e.Name()) && !IsTemplateFile(e.Name()) {
			files = append(files, e.Name())
		}
	}
	sort.SliceStable(files, func(i, j int) bool { return compareRu(files[i], files[j]) < 0 })
	if len(files) > LimitBoards {
		files = files[:LimitBoards]
	}
	out := make([]Board, 0, len(files))
	for _, f := range files {
		out = append(out, Board{Title: BoardTitleOf(f), Path: f})
	}
	return out
}

var (
	wrapperMu    sync.Mutex
	wrapperFiles = map[string]bool{}
)

// isWrapperFile: a board file that only shows lib/page.html in a frame; remembered by path, time and size.
func isWrapperFile(file string) bool {
	st, err := os.Stat(file)
	if err != nil {
		return false
	}
	key := file + "\n" + st.ModTime().String() + "\n" + jscompat.Number(float64(st.Size()))
	wrapperMu.Lock()
	known, ok := wrapperFiles[key]
	wrapperMu.Unlock()
	if ok {
		return known
	}
	b, _ := os.ReadFile(file)
	wrapper := InnerPageBoard("", jscompat.DecodeUTF8(b)) != nil
	wrapperMu.Lock()
	if len(wrapperFiles) >= 2000 {
		wrapperFiles = map[string]bool{}
	}
	wrapperFiles[key] = wrapper
	wrapperMu.Unlock()
	return wrapper
}

func pageFileOf(boardPath string) string {
	if i := strings.IndexByte(boardPath, '?'); i >= 0 {
		return boardPath[:i]
	}
	return boardPath
}

// MergeBoards is what artifact.json lists, in its order and only while each page is there, plus every other board
// file at the root of project/ that no listed board names by path or title and that is not a wrapper.
func MergeBoards(project string, listed []Board) []Board {
	var kept []Board
	known := map[string]bool{}
	for _, b := range listed {
		if exists(filepath.Join(append([]string{project}, strings.Split(pageFileOf(b.Path), "/")...)...)) {
			kept = append(kept, b)
			known[b.Path] = true
		}
	}
	titles := map[string]bool{}
	for _, b := range listed {
		titles[b.Title] = true
	}
	out := append([]Board{}, kept...)
	for _, b := range scanBoards(project) {
		if known[b.Path] || titles[b.Title] || isWrapperFile(filepath.Join(project, b.Path)) {
			continue
		}
		out = append(out, b)
	}
	if len(out) > LimitBoards {
		out = out[:LimitBoards]
	}
	return out
}

func mtimeMs(st os.FileInfo) float64 { return float64(st.ModTime().UnixNano()) / 1e6 }

func round(f float64) int64 { return int64(math.Floor(f + 0.5)) }

// newestIn is the newest modification time of the files under dir, at most budget entries looked at.
func newestIn(dir string, budget *int) float64 {
	newest := 0.0
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if *budget <= 0 {
			break
		}
		*budget--
		full := filepath.Join(dir, e.Name())
		switch {
		case e.IsDir():
			if e.Name() != "node_modules" && e.Name() != ".git" {
				newest = math.Max(newest, newestIn(full, budget))
			}
		case e.Type().IsRegular():
			if st, err := os.Stat(full); err == nil {
				newest = math.Max(newest, mtimeMs(st))
			}
		}
	}
	return newest
}

const (
	versionTTL     = 700 * time.Millisecond
	listVersionTTL = 4 * time.Second
)

// ArtifactVersion is the newest modification time in the artifact (ms). While the library's watcher runs it is kept
// until a change under the artifact; without the watcher it is remembered for maxAge, as the old runtime did.
func (l *Library) ArtifactVersion(ref string, maxAge time.Duration) int64 {
	dir := l.ProjectDirOf(ref)
	if dir == "" {
		return 0
	}
	return l.listing.version(ref, maxAge, l.watching(), func() int64 {
		budget := 6000
		project := newestIn(dir, &budget)
		meta := 0.0
		if st, err := os.Stat(filepath.Join(l.ArtifactDir(ref), ArtifactFile)); err == nil {
			meta = mtimeMs(st)
		}
		return round(math.Max(project, meta))
	})
}

func (l *Library) readProject(ref string) *Artifact {
	project := l.ProjectDirOf(ref)
	if project == "" {
		return nil
	}
	if st, err := os.Stat(project); err != nil || !st.IsDir() {
		return nil
	}
	meta := ParseArtifactFile(readJSON(filepath.Join(l.ArtifactDir(ref), ArtifactFile)))
	var canvas []CanvasBoard
	var canvasTitle *string
	if raw := readJSON(filepath.Join(project, canvasJSONName)); raw != nil {
		canvas, canvasTitle = ParseCanvasJSON(raw)
	}
	fromCanvas := make([]Board, 0, len(canvas))
	for _, b := range canvas {
		fromCanvas = append(fromCanvas, Board{Title: b.Title, Path: b.File, W: metaSize(b.W), H: metaSize(b.H)})
	}
	var listed []Board
	switch {
	case meta.Boards != nil:
		listed = MergeBoards(project, meta.Boards)
	case len(fromCanvas) > 0:
		listed = fromCanvas
	default:
		listed = scanBoards(project)
	}
	sizes := map[string]Board{}
	for _, b := range fromCanvas {
		if _, ok := sizes[b.Path]; !ok {
			sizes[b.Path] = b
		}
	}
	boards := make([]Board, 0, len(listed))
	for _, b := range listed {
		if known, ok := sizes[b.Path]; ok && b.W == nil && b.H == nil {
			b.W, b.H = known.W, known.H
		}
		boards = append(boards, b)
	}
	title := ref
	if meta.Title != nil {
		title = *meta.Title
	} else if canvasTitle != nil {
		title = *canvasTitle
	}
	return &Artifact{
		ID: ref, Title: title, Source: meta.Source, UpdatedAt: l.ArtifactVersion(ref, listVersionTTL), Boards: boards,
		HasSupport: exists(filepath.Join(project, SupportJSName)), ProjectPath: project, Agents: []Agent{}, Claims: []ClaimView{},
	}
}

// ReadArtifact is one artifact by its ref; nil when there is none. The answer is a copy the caller may change.
func (l *Library) ReadArtifact(ref string) *Artifact {
	if !IsLibraryID(ref) {
		return nil
	}
	a := l.listing.artifact(ref, l.watching(), func() *Artifact { return l.readProject(ref) })
	if a == nil {
		return nil
	}
	return a.clone()
}

// ListArtifacts are the library's projects, newest first.
func (l *Library) ListArtifacts() []*Artifact {
	refs := l.listing.refs(l.watching(), func() []string {
		entries, _ := os.ReadDir(l.Root())
		var out []string
		for _, e := range entries {
			if e.IsDir() && IsLibraryID(e.Name()) {
				out = append(out, e.Name())
			}
		}
		return out
	})
	out := make([]*Artifact, 0, len(refs))
	for _, ref := range refs {
		if a := l.ReadArtifact(ref); a != nil {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out
}
