package design

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/designkit"
)

// Fonts of a deck come from two places: the three built in (DeckFonts) and the fonts an installed kit lists in its
// kit.json. A kit font is picked by its address kit:<kit>:<version>:<font>, which pins the version: a version never
// changes, so the deck looks the same later. Its @font-face is written into the deck's theme stylesheet and the files
// are the kit's, served at /kit/<kit>/<version>/....

// kitFontFileRe is a safe font file path inside a kit: no quotes, parentheses, spaces or other CSS-significant characters.
var kitFontFileRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,200}$`)

var kitFontIDRe = regexp.MustCompile(`^kit:([a-z0-9][a-z0-9-]{0,62}):(\d{1,9}\.\d{1,9}\.\d{1,9}(?:-[0-9A-Za-z][0-9A-Za-z.-]{0,40})?):([a-z0-9][a-z0-9-]{0,62})$`)

// KitFontFace is one weight of a kit font: a file inside the kit.
type KitFontFace struct {
	Weight int    `json:"weight"`
	Style  string `json:"style,omitempty"`
	File   string `json:"file"`
}

// KitFont is a font a kit offers to decks.
type KitFont struct {
	// ID is the address a deck stores: kit:<kit>:<version>:<font>.
	ID         string        `json:"id"`
	Family     string        `json:"family"`
	Kit        string        `json:"kit"`
	Version    string        `json:"version"`
	Restricted bool          `json:"restricted"`
	Faces      []KitFontFace `json:"faces"`
}

type kitFontRecord struct {
	ID      string        `json:"id"`
	Family  string        `json:"family"`
	Faces   []KitFontFace `json:"faces"`
	License struct {
		Kind string `json:"kind"`
	} `json:"license"`
}

func readKitFonts(root, kit, version string) []KitFont {
	raw, err := os.ReadFile(filepath.Join(root, "kit.json"))
	if err != nil {
		return nil
	}
	var rec struct {
		Fonts []kitFontRecord `json:"fonts"`
	}
	if json.Unmarshal(raw, &rec) != nil {
		return nil
	}
	var out []KitFont
	for _, f := range rec.Fonts {
		if !designkit.ValidID(f.ID) || f.Family == "" || strings.ContainsAny(f.Family, "\"\\{}<>;\n") || len(f.Faces) == 0 {
			continue
		}
		var faces []KitFontFace
		for _, face := range f.Faces {
			clean := path.Clean(face.File)
			if clean != face.File || !kitFontFileRe.MatchString(clean) || strings.HasPrefix(clean, "../") || path.IsAbs(clean) || face.Weight < 100 || face.Weight > 900 {
				continue
			}
			if face.Style != "" && face.Style != "normal" && face.Style != "italic" {
				continue
			}
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); err != nil {
				continue
			}
			faces = append(faces, face)
		}
		if len(faces) == 0 {
			continue
		}
		out = append(out, KitFont{ID: fmt.Sprintf("kit:%s:%s:%s", kit, version, f.ID), Family: f.Family, Kit: kit, Version: version,
			Restricted: f.License.Kind == "restricted", Faces: faces})
	}
	return out
}

// KitFonts is the fonts of every installed kit's newest version, by kit and font.
func (l *Library) KitFonts() []KitFont {
	dir := l.KitsDir()
	out := []KitFont{}
	for _, k := range designkit.List(dir) {
		if len(k.Versions) == 0 {
			continue
		}
		v := k.Versions[0].Version
		if root := designkit.Resolve(dir, k.ID, v, nil); root != "" {
			out = append(out, readKitFonts(root, k.ID, v)...)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// kitFont is the font a deck stored, from the exact kit version it names; false when the kit or the font is not here.
func (l *Library) kitFont(id string) (KitFont, bool) {
	m := kitFontIDRe.FindStringSubmatch(id)
	if m == nil {
		return KitFont{}, false
	}
	root := designkit.Resolve(l.KitsDir(), m[1], m[2], nil)
	if root == "" {
		return KitFont{}, false
	}
	for _, f := range readKitFonts(root, m[1], m[2]) {
		if f.ID == id {
			return f, true
		}
	}
	return KitFont{}, false
}

// IsKitFontID is whether id has the form of a kit font's address (not whether the kit is installed).
func IsKitFontID(id string) bool { return kitFontIDRe.MatchString(id) }

// kitFontCSS is the @font-face rules of a kit font and the stack to use for it.
func kitFontCSS(f KitFont) (faces, stack string) {
	var b strings.Builder
	for _, face := range f.Faces {
		style := face.Style
		if style == "" {
			style = "normal"
		}
		fmt.Fprintf(&b, "@font-face{font-family:\"%s\";font-weight:%d;font-style:%s;font-display:swap;src:url(\"/kit/%s/%s/%s\");}\n",
			f.Family, face.Weight, style, f.Kit, f.Version, face.File)
	}
	return b.String(), `"` + f.Family + `", system-ui, sans-serif`
}

// KitFontsList is GET (a session): {fonts: [KitFont]}, the fonts of the installed kits a deck can take.
func (a *API) KitFontsList(r *http.Request) Answer {
	h := noStore()
	if r.Method != http.MethodGet {
		h.Set("Allow", "GET")
		return JSON(r, http.StatusMethodNotAllowed, errorBody("Method not allowed"), h)
	}
	if status, v, hh, refused := a.APIRefusal(r); refused {
		return Answer{status, v, hh}
	}
	if !a.Session(r) {
		return JSON(r, http.StatusForbidden, errorBody("Forbidden"), h)
	}
	return JSON(r, http.StatusOK, struct {
		Fonts []KitFont `json:"fonts"`
	}{a.L.KitFonts()}, h)
}
