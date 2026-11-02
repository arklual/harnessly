package design

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	goimage "image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// A scene's contact sheet (HRN-579): one picture with a frame per half second and the time written on each, so an agent
// sees the whole motion at the price of one image instead of opening a frame at a time.

const (
	videoSheetStep     = 500
	videoSheetMaxCells = 42
	videoSheetGap      = 4
)

// VideoSheetFile draws the scene page's contact sheet in f and answers its file.
func (l *Library) VideoSheetFile(ctx context.Context, id, page string, f VideoFormat) (string, error) {
	scenes, err := videoScenesFor(l, id)
	if err != nil {
		return "", err
	}
	sc, err := l.videoPickScene(id, scenes, page)
	if err != nil {
		return "", err
	}
	times := videoSheetTimes(sc.Dur)
	files, err := l.VideoFrameFiles(ctx, id, page, f, true, times)
	if err != nil {
		return "", err
	}
	key, err := videoFrameKeyOf(l, id, sc, f, true)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(key + "@sheet"))
	out := filepath.Join(l.deckFile(id, VideoFramesDir), hex.EncodeToString(sum[:16])+".jpg")
	if isRealFile(out) {
		return out, nil
	}
	cols := 6
	if f == VideoPortrait {
		cols = 8
	}
	cell := 320
	if f == VideoPortrait {
		cell = 180
	}
	raw, err := videoSheet(files, times, cols, cell)
	if err != nil {
		return "", err
	}
	if err := writeFileAtomic(out, raw); err != nil {
		return "", err
	}
	return out, nil
}

// videoSheetTimes are the sheet's moments: every half second, sparser when the scene is long, and its last frame.
func videoSheetTimes(dur int64) []int64 {
	step := int64(videoSheetStep)
	last := max(dur-videoCheckBeforeEnd, 0)
	if last/step+2 > videoSheetMaxCells {
		step = last / (videoSheetMaxCells - 2)
	}
	out := []int64{}
	for t := int64(0); t < last; t += step {
		out = append(out, t)
	}
	return append(out, last)
}

// videoSheet tiles the JPEG files, cols in a row, each cell wide, with its time in the corner.
func videoSheet(files []string, times []int64, cols, cell int) ([]byte, error) {
	var sheet *goimage.RGBA
	h := 0
	rows := (len(files) + cols - 1) / cols
	for i, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		src, err := jpeg.Decode(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		b := src.Bounds()
		if sheet == nil {
			h = b.Dy() * cell / b.Dx()
			sheet = goimage.NewRGBA(goimage.Rect(0, 0, cols*(cell+videoSheetGap)-videoSheetGap, rows*(h+videoSheetGap)-videoSheetGap))
			draw.Draw(sheet, sheet.Bounds(), goimage.NewUniform(color.RGBA{0x30, 0x30, 0x34, 0xff}), goimage.Point{}, draw.Src)
		}
		x0 := (i % cols) * (cell + videoSheetGap)
		y0 := (i / cols) * (h + videoSheetGap)
		videoSheetScale(sheet, goimage.Rect(x0, y0, x0+cell, y0+h), src)
		videoSheetLabel(sheet, x0+4, y0+4, strconv.FormatFloat(float64(times[i])/1000, 'f', 1, 64))
	}
	if sheet == nil {
		return nil, &DeckError{Status: http.StatusBadRequest, Msg: "the scene has no frames"}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, sheet, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// videoSheetScale averages src down into r of dst.
func videoSheetScale(dst *goimage.RGBA, r goimage.Rectangle, src goimage.Image) {
	b := src.Bounds()
	for y := 0; y < r.Dy(); y++ {
		sy0 := b.Min.Y + y*b.Dy()/r.Dy()
		sy1 := max(b.Min.Y+(y+1)*b.Dy()/r.Dy(), sy0+1)
		for x := 0; x < r.Dx(); x++ {
			sx0 := b.Min.X + x*b.Dx()/r.Dx()
			sx1 := max(b.Min.X+(x+1)*b.Dx()/r.Dx(), sx0+1)
			var cr, cg, cb, n uint32
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					pr, pg, pb, _ := src.At(sx, sy).RGBA()
					cr += pr >> 8
					cg += pg >> 8
					cb += pb >> 8
					n++
				}
			}
			dst.SetRGBA(r.Min.X+x, r.Min.Y+y, color.RGBA{uint8(cr / n), uint8(cg / n), uint8(cb / n), 0xff})
		}
	}
}

// videoSheetGlyphs is a 3×5 bitmap of what a time needs, a row per byte.
var videoSheetGlyphs = map[rune][5]byte{
	'0': {7, 5, 5, 5, 7}, '1': {2, 6, 2, 2, 7}, '2': {7, 1, 7, 4, 7}, '3': {7, 1, 7, 1, 7}, '4': {5, 5, 7, 1, 1},
	'5': {7, 4, 7, 1, 7}, '6': {7, 4, 7, 5, 7}, '7': {7, 1, 2, 2, 2}, '8': {7, 5, 7, 5, 7}, '9': {7, 5, 7, 1, 7},
	'.': {0, 0, 0, 0, 2},
}

// videoSheetLabel writes text on a dark patch at x, y, twice the bitmap's size.
func videoSheetLabel(img *goimage.RGBA, x, y int, text string) {
	const scale = 2
	w := len(text)*4*scale + 2*scale
	draw.Draw(img, goimage.Rect(x, y, x+w, y+5*scale+2*scale), goimage.NewUniform(color.RGBA{0, 0, 0, 0xff}), goimage.Point{}, draw.Src)
	for i, ch := range text {
		glyph := videoSheetGlyphs[ch]
		for row, bits := range glyph {
			for col := 0; col < 3; col++ {
				if bits&(4>>col) == 0 {
					continue
				}
				px := x + scale + i*4*scale + col*scale
				py := y + scale + row*scale
				draw.Draw(img, goimage.Rect(px, py, px+scale, py+scale), goimage.NewUniform(color.RGBA{0xff, 0xff, 0xff, 0xff}), goimage.Point{}, draw.Src)
			}
		}
	}
}
