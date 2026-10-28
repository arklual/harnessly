package design

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// A company template (.pptx) gives a new deck its look: the colours and fonts of ppt/theme/theme1.xml and the count
// of ppt/slideLayouts. Nothing else is read (no logo, no master shapes).

const (
	pptxMaxEntries  = 20000
	pptxThemeMaxXML = 1 << 20
)

// ErrNotPptx: the bytes are not a PowerPoint file with a theme.
var ErrNotPptx = errors.New("not a pptx")

var pptxLayoutRe = regexp.MustCompile(`^ppt/slideLayouts/slideLayout\d+\.xml$`)

type pptxColor struct {
	RGB *struct {
		Val string `xml:"val,attr"`
	} `xml:"srgbClr"`
	Sys *struct {
		Last string `xml:"lastClr,attr"`
	} `xml:"sysClr"`
}

func (c pptxColor) hex() string {
	v := ""
	switch {
	case c.RGB != nil:
		v = c.RGB.Val
	case c.Sys != nil:
		v = c.Sys.Last
	}
	if len(v) != 6 {
		return ""
	}
	if _, err := strconv.ParseUint(v, 16, 32); err != nil {
		return ""
	}
	return "#" + strings.ToLower(v)
}

type pptxFont struct {
	Latin struct {
		Typeface string `xml:"typeface,attr"`
	} `xml:"latin"`
}

type pptxTheme struct {
	Name     string `xml:"name,attr"`
	Elements struct {
		Colors struct {
			DK1     pptxColor `xml:"dk1"`
			LT1     pptxColor `xml:"lt1"`
			DK2     pptxColor `xml:"dk2"`
			LT2     pptxColor `xml:"lt2"`
			Accent1 pptxColor `xml:"accent1"`
			Accent2 pptxColor `xml:"accent2"`
			Accent3 pptxColor `xml:"accent3"`
			Accent4 pptxColor `xml:"accent4"`
			Accent5 pptxColor `xml:"accent5"`
			Accent6 pptxColor `xml:"accent6"`
		} `xml:"clrScheme"`
		Fonts struct {
			Major pptxFont `xml:"majorFont"`
			Minor pptxFont `xml:"minorFont"`
		} `xml:"fontScheme"`
	} `xml:"themeElements"`
}

// ReadPptxStyle reads a template's look. name is the file's name as the person attached it.
func ReadPptxStyle(data []byte, name string) (DeckPptxStyle, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(zr.File) > pptxMaxEntries {
		return DeckPptxStyle{}, ErrNotPptx
	}
	var themeFile *zip.File
	layouts := 0
	for _, f := range zr.File {
		switch {
		case f.Name == "ppt/theme/theme1.xml":
			themeFile = f
		case pptxLayoutRe.MatchString(f.Name):
			layouts++
		}
	}
	if themeFile == nil {
		return DeckPptxStyle{}, ErrNotPptx
	}
	rc, err := themeFile.Open()
	if err != nil {
		return DeckPptxStyle{}, ErrNotPptx
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, pptxThemeMaxXML+1))
	if err != nil || len(raw) > pptxThemeMaxXML {
		return DeckPptxStyle{}, ErrNotPptx
	}
	var th pptxTheme
	if err := xml.Unmarshal(raw, &th); err != nil {
		return DeckPptxStyle{}, ErrNotPptx
	}
	c := th.Elements.Colors
	ink, bg, acc := orHex(c.DK1.hex(), "#1c1c1f"), orHex(c.LT1.hex(), "#ffffff"), orHex(c.Accent1.hex(), "#7b61ff")
	accents := []string{}
	for _, a := range []pptxColor{c.Accent1, c.Accent2, c.Accent3, c.Accent4, c.Accent5, c.Accent6} {
		if h := a.hex(); h != "" && !containsStr(accents, h) {
			accents = append(accents, h)
		}
	}
	for _, h := range []string{c.DK2.hex(), ink} {
		if h != "" && !containsStr(accents, h) && len(accents) < 3 {
			accents = append(accents, h)
		}
	}
	for len(accents) < 3 {
		accents = append(accents, acc)
	}
	label := strings.TrimSpace(name)
	if label == "" {
		label = strings.TrimSpace(th.Name)
	}
	st := DeckPptxStyle{Name: label, Colors: accents[:3], Layouts: layouts, Fonts: DeckPptxFonts{
		Head: strings.TrimSpace(th.Elements.Fonts.Major.Latin.Typeface), Body: strings.TrimSpace(th.Elements.Fonts.Minor.Latin.Typeface)}}
	st.Vars = pptxVars(ink, bg, acc)
	return st, nil
}

func orHex(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

type rgb struct{ r, g, b float64 }

func parseHex(h string) rgb {
	n, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	return rgb{float64(n >> 16 & 255), float64(n >> 8 & 255), float64(n & 255)}
}

func (c rgb) hex() string {
	q := func(v float64) int { return int(math.Round(math.Max(0, math.Min(255, v)))) }
	return fmt.Sprintf("#%02x%02x%02x", q(c.r), q(c.g), q(c.b))
}

// mix is a over b: t of a, the rest of b.
func mix(a, b rgb, t float64) rgb {
	return rgb{a.r*t + b.r*(1-t), a.g*t + b.g*(1-t), a.b*t + b.b*(1-t)}
}

func lum(c rgb) float64 {
	f := func(v float64) float64 {
		v /= 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*f(c.r) + 0.7152*f(c.g) + 0.0722*f(c.b)
}

func contrast(a, b rgb) float64 {
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// pptxVars derives the deck's variables from a template's ink, background and first accent.
func pptxVars(inkHex, bgHex, accHex string) map[string]string {
	ink, bg, acc := parseHex(inkHex), parseHex(bgHex), parseHex(accHex)
	onAcc := ink
	if contrast(bg, acc) > contrast(ink, acc) {
		onAcc = bg
	}
	accInk := acc
	for i := 0; i < 10 && contrast(accInk, bg) < 4.5; i++ {
		accInk = mix(ink, accInk, 0.2+0.08*float64(i))
	}
	if contrast(accInk, bg) < 4.5 {
		accInk = ink
	}
	return map[string]string{
		"--deck-bg": bg.hex(), "--deck-ink": ink.hex(), "--deck-mute": mix(ink, bg, 0.78).hex(), "--deck-acc": acc.hex(),
		"--deck-on-acc": onAcc.hex(), "--deck-acc-soft": mix(acc, bg, 0.14).hex(), "--deck-acc-ink": accInk.hex(),
		"--deck-panel": mix(ink, bg, 0.04).hex(), "--deck-line": mix(ink, bg, 0.15).hex(),
	}
}
