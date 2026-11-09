package designkit

// Prepared pictures of a kit version: the far view of the kit screen shows hundreds of icons and layouts at once, and
// hundreds of files on the way through a gateway is what breaks a catalogue. Prepare draws each asset group and each
// template into one atlas (a grid of small cells) once per version and theme and keeps it beside the version, in
// <kits>/<id>/.<version>.prep/<theme>/; the version's own folder stays as installed. The near view takes the files.

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // asset pictures may be GIF
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const prepSchema = 2

// The atlases' cells: icons are small squares, other pictures larger ones, a template's layouts keep its aspect.
const (
	iconCell    = 48
	imageCell   = 192
	layoutCellW = 192
	iconCols    = 32
	imageCols   = 8
	layoutCols  = 12
	// A component's picture keeps the canvas tile's aspect (560×340 in kit-world.ts) at half its size.
	componentCellW = 280
	componentCellH = 170
	componentCols  = 12
	maxSourceLen   = 24 << 20
)

type prepAtlas struct {
	Atlas Atlas          `json:"atlas"`
	Cells map[string]int `json:"cells"`
}

type prepIndex struct {
	Schema    int                  `json:"schema"`
	At        int64                `json:"at"`
	Assets    map[string]prepAtlas `json:"assets"`
	Templates map[string]prepAtlas `json:"templates"`
	// Components holds an atlas per component group, by the group's id.
	Components map[string]prepAtlas `json:"components,omitempty"`
}

var prepLocks sync.Map

func prepDir(dir, id, version, theme string) string {
	return filepath.Join(dir, id, "."+version+".prep", theme)
}

// PreparedFile is a prepared file of a version's theme as a real path, or "" when there is none by that name.
func PreparedFile(dir, id, version, theme, name string) string {
	if !ValidID(id) || !validRef(version) || !ValidID(theme) || name == "" || strings.ContainsAny(name, "/\\\x00") ||
		strings.HasPrefix(name, ".") {
		return ""
	}
	p := filepath.Join(prepDir(dir, id, version, theme), name)
	if st, err := os.Lstat(p); err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return p
}

// Prepare makes the atlases of a canvas's theme once (later calls read what the first made) and writes their places
// into the canvas: each asset group's and template's Atlas, each item's Cell.
func Prepare(dir string, c *Canvas) error {
	key := c.Kit + "@" + c.Version + "/" + c.Theme
	out := prepDir(dir, c.Kit, c.Version, c.Theme)
	// The draft changes under the screen: once a file of it is newer than its atlases they are made again behind the
	// screen, which meanwhile gets the ones there are (making them takes long for a large kit).
	if c.Version == DraftVersion {
		if idx, err := readPrep(out); err == nil {
			if newestUnder(themeRoot(dir, c), idx.At) {
				remakeLater(dir, c.Kit, c.Theme, key, out)
			}
			applyPrep(c, idx)
			return nil
		}
	}
	mu, _ := prepLocks.LoadOrStore(key, &sync.Mutex{})
	mu.(*sync.Mutex).Lock()
	defer mu.(*sync.Mutex).Unlock()
	idx, err := readPrep(out)
	if err != nil {
		if idx, err = makePrep(dir, c, out); err != nil {
			return err
		}
	}
	applyPrep(c, idx)
	return nil
}

// themeRoot is the folder of the canvas's theme, "" when the version is not there: its atlases are of its files only.
func themeRoot(dir string, c *Canvas) string {
	root := Resolve(dir, c.Kit, c.Version, nil)
	if root == "" || c.Base == "" {
		return root
	}
	return filepath.Join(root, filepath.FromSlash(c.Base))
}

// remaking: the draft's atlases being made again, by key; one at a time for each.
var remaking sync.Map

// remakeLater makes the draft theme's atlases again in the background, unless that is under way.
func remakeLater(dir, id, theme, key, out string) {
	if _, busy := remaking.LoadOrStore(key, true); busy {
		return
	}
	go func() {
		defer remaking.Delete(key)
		mu, _ := prepLocks.LoadOrStore(key, &sync.Mutex{})
		mu.(*sync.Mutex).Lock()
		defer mu.(*sync.Mutex).Unlock()
		if c, err := ReadCanvas(dir, id, DraftVersion, theme); err == nil {
			_, _ = makePrep(dir, c, out)
		}
	}()
}

// SeedVersionPrep gives a version published from the draft the draft's atlases of each theme that are not older than
// the theme's files (the version keeps the draft files' times), so its first open does not make them again.
func SeedVersionPrep(dir, id, version string) {
	root := Resolve(dir, id, version, nil)
	if root == "" {
		return
	}
	for _, th := range Themes(dir, id, version) {
		from := prepDir(dir, id, DraftVersion, th.ID)
		idx, err := readPrep(from)
		if err != nil {
			continue
		}
		folder := root
		if th.Folder != "" {
			folder = filepath.Join(root, filepath.FromSlash(th.Folder))
		}
		to := prepDir(dir, id, version, th.ID)
		if _, err := os.Stat(to); err == nil || newestUnder(folder, idx.At) {
			continue
		}
		_, _, _ = CopyTree(from, to, nil)
	}
}

// SeedDraftPrep gives a new draft the atlases of the version it was copied from: the copy keeps the files' times, so
// they stand until a file of the draft is edited.
func SeedDraftPrep(dir, id, base string) error {
	if !ValidID(id) || !ValidVersion(base) {
		return os.ErrInvalid
	}
	from := filepath.Join(dir, id, "."+base+".prep")
	to := filepath.Join(dir, id, "."+DraftVersion+".prep")
	if err := os.RemoveAll(to); err != nil {
		return err
	}
	if st, err := os.Stat(from); err != nil || !st.IsDir() {
		return nil
	}
	_, _, err := CopyTree(from, to, nil)
	return err
}

// newestUnder is whether a file under root was modified after at (ms).
func newestUnder(root string, at int64) bool {
	if root == "" {
		return false
	}
	newer := false
	_ = filepath.WalkDir(root, func(p string, e os.DirEntry, err error) error {
		if newer {
			return filepath.SkipAll
		}
		if err != nil {
			return nil
		}
		if e.IsDir() && p != root && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if st, err := e.Info(); err == nil && !e.IsDir() && st.ModTime().UnixMilli() > at {
			newer = true
		}
		return nil
	})
	return newer
}

func readPrep(out string) (*prepIndex, error) {
	raw, err := readLimited(filepath.Join(out, "prep.json"), 8<<20)
	if err != nil {
		return nil, err
	}
	var idx prepIndex
	if err := json.Unmarshal(raw, &idx); err != nil || idx.Schema != prepSchema {
		return nil, fmt.Errorf("prep.json: stale")
	}
	return &idx, nil
}

func applyPrep(c *Canvas, idx *prepIndex) {
	c.PreparedAt = idx.At
	for gi := range c.Assets {
		g := &c.Assets[gi]
		pa, ok := idx.Assets[g.ID]
		if !ok {
			continue
		}
		a := pa.Atlas
		g.Atlas = &a
		for i := range g.Items {
			if cell, ok := pa.Cells[g.Items[i].Name]; ok {
				g.Items[i].Cell = cell
			}
		}
	}
	for gi := range c.Groups {
		g := &c.Groups[gi]
		pa, ok := idx.Components[g.ID]
		if !ok {
			continue
		}
		a := pa.Atlas
		g.Atlas = &a
		for i := range g.Components {
			if cell, ok := pa.Cells[g.Components[i].ID]; ok {
				g.Components[i].Cell = cell
			}
		}
	}
	for ti := range c.Templates {
		t := &c.Templates[ti]
		pa, ok := idx.Templates[t.ID]
		if !ok {
			continue
		}
		a := pa.Atlas
		t.Atlas = &a
		for si := range t.Sections {
			for li := range t.Sections[si].Layouts {
				if cell, ok := pa.Cells[t.Sections[si].Layouts[li].ID]; ok {
					t.Sections[si].Layouts[li].Cell = cell
				}
			}
		}
	}
}

// themeFile is a file of the canvas's theme (a path relative to the theme's folder) as a real path, or "".
func themeFile(dir string, c *Canvas, rel string) string {
	if rel = cleanRel(rel); rel == "" {
		return ""
	}
	segs := strings.Split(rel, "/")
	if c.Base != "" {
		segs = append(strings.Split(c.Base, "/"), segs...)
	}
	return Resolve(dir, c.Kit, c.Version, segs)
}

func decodeFile(p string) (image.Image, error) {
	ext := strings.ToLower(filepath.Ext(p))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" {
		return nil, fmt.Errorf("not a raster picture")
	}
	if st, err := os.Stat(p); err != nil || st.Size() > maxSourceLen {
		return nil, fmt.Errorf("too large")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > 40_000_000 {
		return nil, fmt.Errorf("bad picture")
	}
	if _, err := f.Seek(0, 0); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(f)
	return img, err
}

type cellSource struct {
	key  string
	file string
}

// drawAtlas puts each source (that decodes) into the next cell, fitted and centred, and says where each went.
func drawAtlas(sources []cellSource, cellW, cellH, cols int, opaque bool) (*image.RGBA, map[string]int) {
	cells := map[string]int{}
	type placed struct {
		key string
		img image.Image
	}
	var ok []placed
	for _, s := range sources {
		if s.file == "" {
			continue
		}
		if img, err := decodeFile(s.file); err == nil {
			ok = append(ok, placed{s.key, img})
		}
	}
	if len(ok) == 0 {
		return nil, cells
	}
	if cols > len(ok) {
		cols = len(ok)
	}
	rows := (len(ok) + cols - 1) / cols
	atlas := image.NewRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	if opaque {
		draw.Draw(atlas, atlas.Bounds(), &image.Uniform{color.RGBA{0xee, 0xee, 0xef, 0xff}}, image.Point{}, draw.Src)
	}
	for i, p := range ok {
		x, y := (i%cols)*cellW, (i/cols)*cellH
		fitInto(atlas, image.Rect(x, y, x+cellW, y+cellH), p.img)
		cells[p.key] = i
	}
	return atlas, cells
}

// fitInto draws src scaled down (never up past its size) into cell keeping its aspect, centred; each pixel is the
// average of the source pixels it covers, with alpha.
func fitInto(dst *image.RGBA, cell image.Rectangle, src image.Image) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	cw, ch := cell.Dx(), cell.Dy()
	scale := minf(float64(cw)/float64(sw), float64(ch)/float64(sh))
	if scale > 1 {
		scale = 1
	}
	w, h := int(float64(sw)*scale+0.5), int(float64(sh)*scale+0.5)
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	ox, oy := cell.Min.X+(cw-w)/2, cell.Min.Y+(ch-h)/2
	rgba := image.NewRGBA(image.Rect(0, 0, sw, sh))
	draw.Draw(rgba, rgba.Bounds(), src, sb.Min, draw.Src)
	for y := 0; y < h; y++ {
		y0, y1 := y*sh/h, (y+1)*sh/h
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := x*sw/w, (x+1)*sw/w
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, a, n uint32
			for yy := y0; yy < y1; yy++ {
				row := rgba.Pix[yy*rgba.Stride:]
				for xx := x0; xx < x1; xx++ {
					p := row[xx*4 : xx*4+4]
					r += uint32(p[0])
					g += uint32(p[1])
					b += uint32(p[2])
					a += uint32(p[3])
					n++
				}
			}
			// Premultiplied «over»: the atlas's own pixel shows under a transparent one (the grey of an opaque atlas).
			d := dst.Pix[(oy+y-dst.Rect.Min.Y)*dst.Stride+(ox+x-dst.Rect.Min.X)*4:]
			sa := a / n
			d[0] = uint8(r/n + uint32(d[0])*(255-sa)/255)
			d[1] = uint8(g/n + uint32(d[1])*(255-sa)/255)
			d[2] = uint8(b/n + uint32(d[2])*(255-sa)/255)
			d[3] = uint8(sa + uint32(d[3])*(255-sa)/255)
		}
	}
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func writeImage(p string, img image.Image, asJPEG bool) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if asJPEG {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 82})
	} else {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func makePrep(dir string, c *Canvas, out string) (*prepIndex, error) {
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, ".prep-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	idx := &prepIndex{Schema: prepSchema, At: time.Now().UnixMilli(), Assets: map[string]prepAtlas{}, Templates: map[string]prepAtlas{}}
	for _, g := range c.Assets {
		cw, ch, cols := imageCell, imageCell, imageCols
		if g.Kind == "icon" {
			cw, ch, cols = iconCell, iconCell, iconCols
		}
		var src []cellSource
		for _, a := range g.Items {
			src = append(src, cellSource{a.Name, themeFile(dir, c, a.File)})
		}
		img, cells := drawAtlas(src, cw, ch, cols, false)
		if img == nil {
			continue
		}
		name := "assets-" + g.ID + ".png"
		if !ValidID(g.ID) {
			name = fmt.Sprintf("assets-%d.png", len(idx.Assets))
		}
		if err := writeImage(filepath.Join(tmp, name), img, false); err != nil {
			return nil, err
		}
		idx.Assets[g.ID] = prepAtlas{Atlas{File: name, CellW: cw, CellH: ch, Cols: min(cols, len(cells)), Count: len(cells)}, cells}
	}
	for ti, t := range c.Templates {
		cw := layoutCellW
		ch := cw * t.Aspect[1] / t.Aspect[0]
		var src []cellSource
		for _, s := range t.Sections {
			for _, l := range s.Layouts {
				src = append(src, cellSource{l.ID, themeFile(dir, c, l.Thumb)})
			}
		}
		img, cells := drawAtlas(src, cw, ch, layoutCols, true)
		if img == nil {
			continue
		}
		name := fmt.Sprintf("template-%d.jpg", ti)
		if err := writeImage(filepath.Join(tmp, name), img, true); err != nil {
			return nil, err
		}
		idx.Templates[t.ID] = prepAtlas{Atlas{File: name, CellW: cw, CellH: ch, Cols: min(layoutCols, len(cells)), Count: len(cells)}, cells}
	}
	idx.Components = map[string]prepAtlas{}
	for gi, g := range c.Groups {
		var src []cellSource
		for _, k := range g.Components {
			if k.Thumb != "" {
				src = append(src, cellSource{k.ID, themeFile(dir, c, k.Thumb)})
			}
		}
		if len(src) == 0 {
			continue
		}
		img, cells := drawAtlas(src, componentCellW, componentCellH, componentCols, false)
		if img == nil {
			continue
		}
		name := fmt.Sprintf("components-%d.png", gi)
		if err := writeImage(filepath.Join(tmp, name), img, false); err != nil {
			return nil, err
		}
		idx.Components[g.ID] = prepAtlas{Atlas{File: name, CellW: componentCellW, CellH: componentCellH,
			Cols: min(componentCols, len(cells)), Count: len(cells)}, cells}
	}
	raw, _ := json.Marshal(idx)
	if err := os.WriteFile(filepath.Join(tmp, "prep.json"), raw, 0o644); err != nil {
		return nil, err
	}
	_ = os.RemoveAll(out)
	if err := os.Rename(tmp, out); err != nil {
		return nil, err
	}
	return idx, nil
}
