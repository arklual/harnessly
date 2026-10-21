package design

import (
	"math"
	"path/filepath"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The files written from meta/: project/canvas.json in the design tool's own index format (v3), so that its agents and
// its rules find the boards where they expect them, and artifact.json (v1) for the tools that predate meta/. Both are
// rebuilt whole from the model; what else they hold (a key a newer tool added, the canvas' notes and design systems,
// a board's own settings) is kept. Neither is ever read back into the model.

const (
	canvasDefaultW = 1440
	canvasDefaultH = 900
	canvasColumns  = 4
	canvasGap      = 160
)

// DefaultCanvasKey is where canvas.json carries this runtime's own data when the deployment names no other key.
const DefaultCanvasKey = "harnessly"

func (l *Library) canvasKey() string {
	if l.cfg.CanvasKey != "" && canvasKeyRe.MatchString(l.cfg.CanvasKey) {
		return l.cfg.CanvasKey
	}
	return DefaultCanvasKey
}

// canvasKeyOf is the project's own key (fixed in meta/project.json), else this runtime's.
func (l *Library) canvasKeyOf(m *ProjectModel) string {
	if m.Meta.CanvasKey != "" {
		return m.Meta.CanvasKey
	}
	return l.canvasKey()
}

func numberOf(o *jsvalue.Object, k string) (float64, bool) {
	f, ok := o.Number(k)
	return f, ok && !math.IsNaN(f) && !math.IsInf(f, 0)
}

func sizeOr(n *int, entry *jsvalue.Object, k string, fallback int) float64 {
	if n != nil {
		return float64(*n)
	}
	if f, ok := numberOf(entry, k); ok && f > 0 {
		return f
	}
	return float64(fallback)
}

func pageSummary(p Page) *jsvalue.Object {
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
	return o
}

// CanvasDocument is canvas.json built from the model over what the file holds now (nil when there is none).
func CanvasDocument(m *ProjectModel, current *jsvalue.Object, key string) []byte {
	doc := jsvalue.NewObject()
	if current != nil {
		doc = jsvalue.Clone(current).(*jsvalue.Object)
	}
	createdAt := m.Meta.CreatedAt
	if createdAt == "" {
		createdAt = "1970-01-01T00:00:00.000Z"
	}
	defaults := []any{
		"v", 3.0,
		"createdOnFiles", jsvalue.NewObject("v", 1.0, "at", createdAt),
		"title", m.Meta.Title,
		"launch", jsvalue.NewObject("view", "canvas"),
		"pages", []any{},
		"boards", jsvalue.NewObject(),
		"order", []any{},
		"notes", jsvalue.NewObject(),
		"designSystems", []any{},
	}
	for i := 0; i+1 < len(defaults); i += 2 {
		if !doc.Has(defaults[i].(string)) {
			doc.Set(defaults[i].(string), defaults[i+1])
		}
	}
	doc.Set("v", 3.0)
	doc.Set("title", m.Meta.Title)
	old := doc.Object("boards")
	boards := jsvalue.NewObject()
	order := []any{}
	var placed []bool
	var entries []*jsvalue.Object
	maxW, maxH := 0.0, 0.0
	for _, p := range m.Pages {
		if !IsBoardFile(p.File) || boards.Has(p.File) {
			continue
		}
		entry := jsvalue.NewObject()
		if e := old.Object(p.File); e != nil {
			entry = e.Copy()
		}
		_, hasX := numberOf(entry, "x")
		_, hasY := numberOf(entry, "y")
		if !hasX || !hasY {
			entry.Set("x", 0.0)
			entry.Set("y", 0.0)
		}
		w, h := sizeOr(p.W, entry, "w", canvasDefaultW), sizeOr(p.H, entry, "h", canvasDefaultH)
		entry.Set("w", w)
		entry.Set("h", h)
		maxW, maxH = math.Max(maxW, w), math.Max(maxH, h)
		entry.Set("title", p.Title)
		if p.Expand != nil {
			entry.Set("expand", *p.Expand)
		} else {
			entry.Delete("expand")
		}
		boards.Set(p.File, entry)
		order = append(order, p.File)
		placed = append(placed, hasX && hasY)
		entries = append(entries, entry)
	}
	// A board without a place goes into a grid cell by its position in the order.
	for i, e := range entries {
		if placed[i] {
			continue
		}
		e.Set("x", float64(i%canvasColumns)*(maxW+canvasGap))
		e.Set("y", float64(i/canvasColumns)*(maxH+canvasGap))
	}
	doc.Set("boards", boards)
	doc.Set("order", order)
	pages := make([]any, 0, len(m.Pages))
	for _, p := range m.Pages {
		pages = append(pages, pageSummary(p))
	}
	doc.Set(key, jsvalue.NewObject("v", 1.0, "source", MetaDirname+"/", "release", m.Meta.Release, "pages", pages))
	return append(jsvalue.Stringify(doc, "  "), '\n')
}

// ArtifactDocument is artifact.json built from the model over what the file holds now.
func ArtifactDocument(m *ProjectModel, current *jsvalue.Object) []byte {
	doc := jsvalue.NewObject("v", 1.0)
	if current != nil {
		doc = jsvalue.Clone(current).(*jsvalue.Object)
	}
	doc.Set("v", 1.0)
	doc.Set("title", m.Meta.Title)
	if m.Meta.Source != nil {
		doc.Set("source", *m.Meta.Source)
	} else {
		doc.Delete("source")
	}
	boards := make([]any, 0, len(m.Pages))
	for _, p := range m.Pages {
		boards = append(boards, jsvalue.NewObject("id", p.ID, "title", p.Title, "path", p.File, "w", sizeValue(p.W), "h", sizeValue(p.H)))
	}
	doc.Set("boards", boards)
	return append(jsvalue.Stringify(doc, "  "), '\n')
}

// DerivedWrite is one file the model produces and whether it differs from the file on disk.
type DerivedWrite struct {
	Path    string `json:"path"`
	Changed bool   `json:"changed"`
	data    []byte
}

func (l *Library) derivedFiles(id string, m *ProjectModel) []DerivedWrite {
	canvasFile := filepath.Join(l.ProjectDir(id), canvasJSONName)
	artifactFile := filepath.Join(l.ArtifactDir(id), ArtifactFile)
	cur, _ := readJSON(canvasFile).(*jsvalue.Object)
	art, _ := readJSON(artifactFile).(*jsvalue.Object)
	out := []DerivedWrite{
		{Path: canvasFile, data: CanvasDocument(m, cur, l.canvasKeyOf(m))},
		{Path: artifactFile, data: ArtifactDocument(m, art)},
	}
	for i := range out {
		out[i].Changed = !sameBytesOnDisk(out[i].Path, out[i].data)
	}
	return out
}

func sameBytesOnDisk(file string, data []byte) bool {
	if !isRealFile(file) {
		return false
	}
	b, err := readFileLimited(file, 16<<20)
	return err == nil && string(b) == string(data)
}

// WriteDerived rebuilds canvas.json and artifact.json of a project with meta/ and returns the files it changed.
func (l *Library) WriteDerived(id string) ([]string, error) {
	m := l.ReadModel(id)
	if m == nil {
		return nil, nil
	}
	if !isRealDir(l.ProjectDir(id)) {
		return nil, nil
	}
	defer l.listing.invalidatePath(id)
	var changed []string
	for _, w := range l.derivedFiles(id, m) {
		ok, err := writeIfChanged(w.Path, w.data)
		if err != nil {
			return changed, err
		}
		if ok {
			changed = append(changed, w.Path)
		}
	}
	return changed, nil
}
