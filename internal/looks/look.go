// Package looks is the person's own look of harnessly as data: a base mode, one to three hues, a
// background (a colour, a gradient, a picture or an animated scene), grain, dim and blur, the roundness, the density,
// a font and a few safe variants of the chat's parts. It still holds no code itself: every field is a number in a range
// or one of a closed list, so a look from anywhere (an agent, a shared link) can only recolour the page, never hide its
// buttons. The one field that reaches code is fx (a live effect): it names a built-in effect or the hash of a script
// kept like a picture (PUT /api/v2/me/looks), and the page runs that script only in a sandboxed opaque-origin frame
// without network, which sees only what the page tells it (the pointer, the blocks' rectangles, the agent's state;
// never the chat's text). The page turns a look into its tokens (web/app/next/features/looks/tokens.ts) and checks the
// contrast; the same rules are here so an agent's look is refused before it reaches a page.
//
// The page keeps the looks in the person's prefs under hx-looks: {v, all, folders: {id: look}, chats: {id: look}}.
package looks

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Hue is one dot of the picker: a hue in degrees and how strong it is (0–100).
type Hue struct {
	H float64 `json:"h"`
	S float64 `json:"s"`
}

// Background is what stands behind the chat.
type Background struct {
	// Kind: none (the theme's own surface), color, gradient (the hues), image (a picture by hash), scene (animated).
	Kind string `json:"kind"`
	// Image is the sha256 of a picture kept by /api/v2/me/looks (kind image).
	Image string `json:"image,omitempty"`
	// Scene is one of Scenes (kind scene).
	Scene string `json:"scene,omitempty"`
	// Fit: cover or tile (kind image).
	Fit string `json:"fit,omitempty"`
	// Speed of a scene, 0–100.
	Speed *float64 `json:"speed,omitempty"`
}

// Parts are the safe variants of the chat's parts.
type Parts struct {
	Bubble   string `json:"bubble,omitempty"`
	Turns    string `json:"turns,omitempty"`
	Composer string `json:"composer,omitempty"`
	Sidebar  string `json:"sidebar,omitempty"`
}

// Fx is a live effect over (or behind) the chat, drawn by a built-in effect or by a script in a sandboxed frame
// (docs/looks.md, «Живые эффекты»). Exactly one of Preset and Script.
type Fx struct {
	// Preset is one of FxPresets.
	Preset string `json:"preset,omitempty"`
	// Script is the sha256 (lowercase hex) of a script kept by /api/v2/me/looks (it starts with FxMagic).
	Script string `json:"script,omitempty"`
	// Back puts the effect behind the chat's blocks instead of over them.
	Back bool `json:"back,omitempty"`
	// Power is how strong it is, 0–100.
	Power *float64 `json:"power,omitempty"`
}

// Look is one look.
type Look struct {
	V    int    `json:"v"`
	Name string `json:"name,omitempty"`
	// Mode: auto (follows the app's light or dark), light or dark.
	Mode      string     `json:"mode"`
	Hues      []Hue      `json:"hues"`
	Intensity float64    `json:"intensity"`
	Accent    *Hue       `json:"accent,omitempty"`
	Bg        Background `json:"bg"`
	Grain     float64    `json:"grain"`
	Dim       float64    `json:"dim"`
	Blur      float64    `json:"blur"`
	Radius    string     `json:"radius,omitempty"`
	Density   string     `json:"density,omitempty"`
	Font      string     `json:"font,omitempty"`
	Parts     Parts      `json:"parts"`
	Fx        *Fx        `json:"fx,omitempty"`
}

// The closed lists of a look.
var (
	Modes    = []string{"auto", "light", "dark"}
	BgKinds  = []string{"none", "color", "gradient", "image", "scene"}
	Fits     = []string{"", "cover", "tile"}
	Radii    = []string{"", "sharp", "default", "round"}
	Densitys = []string{"", "compact", "cozy"}
	Fonts    = []string{"", "system", "inter", "serif", "mono", "rounded"}
	Bubbles  = []string{"", "soft", "solid", "outline", "glass"}
	Turnss   = []string{"", "plain", "card", "glass"}
	Composer = []string{"", "solid", "glass", "outline"}
	Sidebars = []string{"", "plain", "tinted", "glass"}
	// FxPresets are the built-in live effects of the page.
	FxPresets = []string{"snowplay", "nightlamp", "laser", "newyear", "elves", "weather"}
	// Scenes are the animated (and one still) backgrounds the page draws (features/looks/scenes).
	Scenes = []string{"matrix", "stars", "aurora", "rain", "synthwave", "twinkle", "paper", "lava", "snow", "topo"}
)

// FxPreset describes a built-in live effect for the agents' tools and `harnessly theme fx-presets`.
type FxPreset struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	About string `json:"about"`
}

// FxPresetList is FxPresets with what each one does.
var FxPresetList = []FxPreset{
	{"snowplay", "Снежная игра", "реалистичный снег ложится шапками на блоки, мышь разгоняет и сметает, двойной клик — снегоуборщик, пролетает Дед Мороз"},
	{"nightlamp", "Ночник", "тёплый свет из угла, пока агент работает"},
	{"laser", "Лазер", "лазер выпечатывает новый ответ строка за строкой, новый промпт обводит бегущий луч"},
	{"newyear", "Мастерская", "уютная новогодняя мастерская вокруг чата: гирлянда, лампа, снег за окном, пар над какао; снег на блоках, бант на готовом ответе"},
	{"elves", "Фабрика эльфов", "мастерская Деда Мороза: конвейер с подарками, промпт на ленте, эльфы мастерят, пока агент работает, готовый ответ упакован бантом"},
	{"weather", "Погода", "небо и пейзаж по погоде и времени там, где человек: рассвет, день, закат, ночь, облака, дождь, снег, туман, гроза; двойной клик показывает варианты"},
}

// FxMagic starts every fx script (the store tells a script from a picture by it).
const FxMagic = "//hx-fx v1\n"

// MaxName is the longest name of a look, in characters.
const MaxName = 60

// ErrBad is a look that is not one.
var ErrBad = errors.New("not a look")

func bad(format string, a ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrBad}, a...)...)
}

func in(list []string, v, field string) error {
	if !slices.Contains(list, v) {
		return bad("%s %q is none of %v", field, v, list)
	}
	return nil
}

func rng(v, lo, hi float64, field string) error {
	if v != v || v < lo || v > hi {
		return bad("%s %v is not in %v–%v", field, v, lo, hi)
	}
	return nil
}

func validHue(h Hue, field string) error {
	if err := rng(h.H, 0, 360, field+".h"); err != nil {
		return err
	}
	return rng(h.S, 0, 100, field+".s")
}

func (f Fx) validate(allowNoEffect bool) error {
	switch {
	case f.Preset != "" && f.Script != "":
		return bad("fx: a preset or a script, not both")
	case f.Preset == "" && f.Script == "":
		if !allowNoEffect {
			return bad("fx: give a preset (%v) or a script (sha256)", FxPresets)
		}
	case f.Preset != "":
		if err := in(FxPresets, f.Preset, "fx.preset"); err != nil {
			return err
		}
	default:
		if !validHash(f.Script) {
			return bad("fx.script must be the sha256 of an uploaded script")
		}
	}
	if f.Power != nil {
		return rng(*f.Power, 0, 100, "fx.power")
	}
	return nil
}

func validHash(h string) bool {
	if len(h) != 64 || strings.ToLower(h) != h {
		return false
	}
	_, err := hex.DecodeString(h)
	return err == nil
}

// Validate says whether the look is one the page takes.
func (l Look) Validate() error { return l.validate(false) }

// validate: withCode is a look that comes with its script's code (Request.FxCode), whose fx may not name the script yet.
func (l Look) validate(withCode bool) error {
	if l.V != 1 {
		return bad("v must be 1")
	}
	if utf8.RuneCountInString(l.Name) > MaxName {
		return bad("name is longer than %d characters", MaxName)
	}
	if err := in(Modes, l.Mode, "mode"); err != nil {
		return err
	}
	if len(l.Hues) < 1 || len(l.Hues) > 3 {
		return bad("hues: one to three")
	}
	for i, h := range l.Hues {
		if err := validHue(h, fmt.Sprintf("hues[%d]", i)); err != nil {
			return err
		}
	}
	if l.Accent != nil {
		if err := validHue(*l.Accent, "accent"); err != nil {
			return err
		}
	}
	for _, c := range []struct {
		v     float64
		hi    float64
		field string
	}{{l.Intensity, 100, "intensity"}, {l.Grain, 100, "grain"}, {l.Dim, 100, "dim"}, {l.Blur, 24, "blur"}} {
		if err := rng(c.v, 0, c.hi, c.field); err != nil {
			return err
		}
	}
	if err := in(BgKinds, l.Bg.Kind, "bg.kind"); err != nil {
		return err
	}
	switch l.Bg.Kind {
	case "image":
		if len(l.Bg.Image) != 64 {
			return bad("bg.image must be the sha256 of an uploaded picture")
		}
		if _, err := hex.DecodeString(l.Bg.Image); err != nil {
			return bad("bg.image must be the sha256 of an uploaded picture")
		}
	case "scene":
		if err := in(Scenes, l.Bg.Scene, "bg.scene"); err != nil {
			return err
		}
	}
	if err := in(Fits, l.Bg.Fit, "bg.fit"); err != nil {
		return err
	}
	if l.Bg.Speed != nil {
		if err := rng(*l.Bg.Speed, 0, 100, "bg.speed"); err != nil {
			return err
		}
	}
	for _, c := range []struct {
		list  []string
		v     string
		field string
	}{{Radii, l.Radius, "radius"}, {Densitys, l.Density, "density"}, {Fonts, l.Font, "font"},
		{Bubbles, l.Parts.Bubble, "parts.bubble"}, {Turnss, l.Parts.Turns, "parts.turns"},
		{Composer, l.Parts.Composer, "parts.composer"}, {Sidebars, l.Parts.Sidebar, "parts.sidebar"}} {
		if err := in(c.list, c.v, c.field); err != nil {
			return err
		}
	}
	if l.Fx != nil {
		if err := l.Fx.validate(withCode); err != nil {
			return err
		}
		if withCode && l.Fx.Preset != "" {
			return bad("fx: a preset and fxCode together")
		}
	}
	return nil
}

// Parse reads a look strictly (no field it does not know) and checks it.
func Parse(raw []byte) (Look, error) { return parse(raw, false) }

func parse(raw []byte, withCode bool) (Look, error) {
	var l Look
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&l); err != nil {
		return Look{}, bad("%v", err)
	}
	if dec.More() {
		return Look{}, bad("one JSON object only")
	}
	if err := l.validate(withCode); err != nil {
		return Look{}, err
	}
	return l, nil
}
