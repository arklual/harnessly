package design

import (
	"encoding/json"
	"regexp"
	"strings"

	"harnessly/internal/jsvalue"
)

const dcSuffix = ".dc.html"

var (
	innerPageRe    = regexp.MustCompile(`lib/page\.html\?id=([\w-]+)&v='`)
	innerVariantRe = regexp.MustCompile(`props\.variant\s*\?\?\s*'([\w-]+)'`)
	innerMobileRe  = regexp.MustCompile(`&m=1\b`)
)

// InnerPageBoard is the page a wrapper board shows in a nested frame (the element picker does not follow into it), so
// the board is listed as the page itself; nil for any other board.
func InnerPageBoard(title, source string) *Board {
	m := innerPageRe.FindStringSubmatch(source)
	if m == nil {
		return nil
	}
	variant := "Desktop"
	if v := innerVariantRe.FindStringSubmatch(source); v != nil {
		variant = v[1]
	}
	mobile := ""
	if innerMobileRe.MatchString(source) {
		mobile = "&m=1"
	}
	return &Board{Title: title, Path: "lib/page.html?id=" + m[1] + "&v=" + variant + "&theme=dark" + mobile}
}

func BoardTitleOf(file string) string { return strings.TrimSuffix(file, dcSuffix) }

func IsBoardFile(file string) bool {
	return strings.HasSuffix(file, dcSuffix) && !strings.Contains(file, "/")
}

// BoardKeyOf is what a board is called in a claim: its file without the suffix, or the title of a wrapper's page.
func BoardKeyOf(b Board) string {
	if IsBoardFile(b.Path) {
		return BoardTitleOf(b.Path)
	}
	return b.Title
}

// BoardTweaks are the tweaks a board declares in data-props of its script: screen variants and the theme switch.
type BoardTweaks struct {
	Variants []string `json:"variants"`
	Themes   bool     `json:"themes"`
}

var (
	dataPropsRe = regexp.MustCompile(`data-props=(?:"([^"]*)"|'([^']*)')`)
	entityRe    = regexp.MustCompile(`&(?:quot|#39|apos|lt|gt|amp);`)
	variantRe   = regexp.MustCompile(`^[\w-]{1,40}$`)
	entities    = map[string]string{"&quot;": `"`, "&#39;": "'", "&apos;": "'", "&lt;": "<", "&gt;": ">", "&amp;": "&"}
)

func ParseBoardTweaks(source string) *BoardTweaks {
	m := dataPropsRe.FindStringSubmatch(source)
	if m == nil {
		return nil
	}
	attr := m[1]
	if attr == "" {
		attr = m[2]
	}
	attr = entityRe.ReplaceAllStringFunc(attr, func(e string) string { return entities[e] })
	var props any
	if json.Unmarshal([]byte(attr), &props) != nil {
		return nil
	}
	obj, ok := props.(map[string]any)
	if !ok {
		if _, isArr := props.([]any); !isArr {
			return nil
		}
		obj = map[string]any{}
	}
	variants := []string{}
	if v, ok := obj["variant"].(map[string]any); ok {
		if opts, ok := v["options"].([]any); ok {
			for _, o := range opts {
				if s, ok := o.(string); ok && variantRe.MatchString(s) {
					variants = append(variants, s)
				}
			}
			if len(variants) > 24 {
				variants = variants[:24]
			}
		}
	}
	_, themes := obj["theme"].(map[string]any)
	if !themes {
		_, themes = obj["theme"].([]any)
	}
	if len(variants) == 0 && !themes {
		return nil
	}
	return &BoardTweaks{Variants: variants, Themes: themes}
}

// CanvasBoard is where a board sits on the canvas of a project's canvas.json.
type CanvasBoard struct {
	File, Title string
	X, Y, W, H  float64
}

func num(v any, fallback float64) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return fallback
}

// ParseCanvasJSON reads canvas.json (parsed by jsvalue, so keys keep JavaScript's order) leniently: a broken entry is
// dropped, not fatal.
func ParseCanvasJSON(raw any) ([]CanvasBoard, *string) {
	obj, ok := raw.(*jsvalue.Object)
	if !ok {
		return nil, nil
	}
	boards := obj.Object("boards")
	if boards == nil {
		return nil, nil
	}
	var order []string
	if v, _ := obj.Get("order"); v != nil {
		if arr, ok := v.([]any); ok {
			for _, f := range arr {
				if s, ok := f.(string); ok {
					order = append(order, s)
				}
			}
		} else {
			order = boards.Keys()
		}
	} else {
		order = boards.Keys()
	}
	seen := map[string]bool{}
	var out []CanvasBoard
	for _, file := range order {
		if seen[file] || nodeBasename(file) != file || !strings.HasSuffix(file, dcSuffix) {
			continue
		}
		seen[file] = true
		entry := boards.Object(file)
		if entry == nil {
			continue
		}
		title := strings.TrimSuffix(file, dcSuffix)
		if t, ok := entry.Get("title"); ok {
			if ts, ok := t.(string); ok && ts != "" {
				title = ts
			}
		}
		x, _ := entry.Get("x")
		y, _ := entry.Get("y")
		w, _ := entry.Get("w")
		h, _ := entry.Get("h")
		out = append(out, CanvasBoard{File: file, Title: title, X: num(x, 0), Y: num(y, 0), W: num(w, 1440), H: num(h, 900)})
	}
	var title *string
	if t, ok := obj.Get("title"); ok {
		if ts, ok := t.(string); ok {
			title = &ts
		}
	}
	return out, title
}

// nodeBasename is node's path.basename: trailing slashes dropped, "" for "/".
func nodeBasename(p string) string {
	p = strings.TrimRight(p, "/")
	return p[strings.LastIndex(p, "/")+1:]
}
