package design

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// A recording of a product's interface: the agent writes a script of actions, `design video record` plays it in
// a headless browser frame by frame on a clock of its own (internal/design/videorecord_run.go) and keeps two files in
// the project's records/ folder: the clip (<name>.mp4, a lossless-grade H.264 at twice the pixels of the final video) and
// the log (<name>.json: the cursor's path and every action with the element's box). The scene draws the cursor, the window
// and the camera over the clip afterwards from the log (vendor/motion/screen.js), so none of it needs the interface shot
// again.

const (
	// RecordsDir is where a project keeps its recordings, beside the boards that load them.
	RecordsDir = "records"

	recordMaxSteps  = 400
	recordMaxWaitMs = 120000
	recordMinFPS    = 10
	recordMaxFPS    = 60
)

// RecordScenario is the script of one recording, JSON or YAML (JSON is YAML).
type RecordScenario struct {
	Name     string         `yaml:"name"`
	URL      string         `yaml:"url"`      // opened first when the steps do not open anything themselves
	Viewport RecordViewport `yaml:"viewport"` // the page's size in CSS pixels
	Scale    int            `yaml:"scale"`    // device pixels per CSS pixel: 2 is twice the pixels of a 1080p video
	FPS      int            `yaml:"fps"`
	Theme    string         `yaml:"theme"` // light or dark: prefers-color-scheme of the page
	Locale   string         `yaml:"locale"`
	Steps    []RecordStep   `yaml:"steps"`
}

// RecordViewport is the page's size in CSS pixels.
type RecordViewport struct {
	Width  int `yaml:"width"`
	Height int `yaml:"height"`
}

// RecordStep is one action. Do names it; the other fields are what the action takes.
//
//	open      url
//	click     on
//	hover     on
//	type      into (clicked first; omitted: the focused field), text or secret
//	key       key (Enter, Tab, Escape, Backspace, ArrowDown, ...)
//	scroll    by (CSS pixels, down is positive) or to (an element), ms
//	wait      ms
//	wait-for  on, timeout
//
// A target is a CSS selector, or text=Words (the smallest visible element holding the words), or text=/regexp/i.
// Pause is the stillness after the step in ms.
type RecordStep struct {
	Do      string `yaml:"do"`
	URL     string `yaml:"url"`
	On      string `yaml:"on"`
	Into    string `yaml:"into"`
	Text    string `yaml:"text"`
	Secret  string `yaml:"secret"` // the name of an environment variable that holds the value: never the value itself
	Key     string `yaml:"key"`
	By      int    `yaml:"by"`
	To      string `yaml:"to"`
	Ms      int    `yaml:"ms"`
	Timeout int    `yaml:"timeout"`
	Pause   *int   `yaml:"pause"`
}

var (
	recordVerbs = map[string]bool{"open": true, "click": true, "hover": true, "type": true, "key": true, "scroll": true, "wait": true, "wait-for": true}
	recordName  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	recordEnv   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
)

// ParseRecordScenario reads and checks a script; unknown fields are refused so a misspelt one does not pass silently.
func ParseRecordScenario(raw []byte) (*RecordScenario, error) {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var sc RecordScenario
	if err := dec.Decode(&sc); err != nil {
		return nil, fmt.Errorf("%s: %w", locale.Pick("the script is not valid", "сценарий не читается"), err)
	}
	if err := sc.normalize(); err != nil {
		return nil, err
	}
	return &sc, nil
}

func recordErr(en, ru string, a ...any) error {
	return fmt.Errorf(locale.Pick(en, ru), a...)
}

func (sc *RecordScenario) normalize() error {
	if sc.Viewport.Width == 0 {
		sc.Viewport.Width = 1920
	}
	if sc.Viewport.Height == 0 {
		sc.Viewport.Height = 1080
	}
	if sc.Scale == 0 {
		sc.Scale = 2
	}
	if sc.FPS == 0 {
		sc.FPS = 30
	}
	if sc.Theme == "" {
		sc.Theme = "light"
	}
	if sc.Viewport.Width < 320 || sc.Viewport.Width > 3840 || sc.Viewport.Height < 320 || sc.Viewport.Height > 2160 {
		return recordErr("viewport is from 320x320 to 3840x2160 CSS pixels", "viewport от 320x320 до 3840x2160 CSS-пикселей")
	}
	if sc.Scale < 1 || sc.Scale > 3 {
		return recordErr("scale is 1, 2 or 3", "scale: 1, 2 или 3")
	}
	if sc.FPS < recordMinFPS || sc.FPS > recordMaxFPS {
		return recordErr("fps is from %d to %d", "fps от %d до %d", recordMinFPS, recordMaxFPS)
	}
	if sc.Viewport.Width*sc.Scale > 7680 || sc.Viewport.Height*sc.Scale > 4320 {
		return recordErr("the shot is larger than 7680x4320 pixels", "кадр больше 7680x4320 пикселей")
	}
	if sc.Theme != "light" && sc.Theme != "dark" {
		return recordErr("theme is light or dark", "theme: light или dark")
	}
	if sc.Name != "" && !recordName.MatchString(sc.Name) {
		return recordErr("name is lowercase letters, digits, '.', '_' and '-'", "name: строчные буквы, цифры, '.', '_' и '-'")
	}
	if len(sc.Steps) == 0 {
		return recordErr("the script has no steps", "в сценарии нет шагов")
	}
	if len(sc.Steps) > recordMaxSteps {
		return recordErr("at most %d steps", "не больше %d шагов", recordMaxSteps)
	}
	if sc.URL != "" {
		if err := recordURL(sc.URL); err != nil {
			return err
		}
	}
	opened := sc.URL != ""
	for i := range sc.Steps {
		s := &sc.Steps[i]
		s.Do = strings.ToLower(strings.TrimSpace(s.Do))
		if err := s.check(); err != nil {
			return fmt.Errorf("%s %d (%s): %w", locale.Pick("step", "шаг"), i+1, s.Do, err)
		}
		if s.Do == "open" {
			opened = true
		} else if !opened {
			return recordErr("the first step opens a page (open, or url at the top)", "первый шаг открывает страницу (open или url в начале)")
		}
	}
	return nil
}

func recordURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return recordErr("not an http(s) address: %q", "это не адрес http(s): %q", raw)
	}
	return nil
}

func (s *RecordStep) check() error {
	if !recordVerbs[s.Do] {
		return recordErr("unknown action; there are open, click, hover, type, key, scroll, wait and wait-for", "неизвестное действие; есть open, click, hover, type, key, scroll, wait, wait-for")
	}
	if s.Ms < 0 || s.Ms > recordMaxWaitMs || s.Timeout < 0 || s.Timeout > 600000 {
		return recordErr("ms is up to %d, timeout up to 600000", "ms до %d, timeout до 600000", recordMaxWaitMs)
	}
	if s.Pause != nil && (*s.Pause < 0 || *s.Pause > recordMaxWaitMs) {
		return recordErr("pause is up to %d ms", "pause до %d мс", recordMaxWaitMs)
	}
	need := func(ok bool, what string) error {
		if ok {
			return nil
		}
		return recordErr("needs %s", "нужно поле %s", what)
	}
	switch s.Do {
	case "open":
		if err := need(s.URL != "", "url"); err != nil {
			return err
		}
		return recordURL(s.URL)
	case "click", "hover", "wait-for":
		return need(strings.TrimSpace(s.On) != "", "on")
	case "type":
		if s.Text != "" && s.Secret != "" {
			return recordErr("text or secret, not both", "text или secret, не оба")
		}
		if s.Text == "" && s.Secret == "" {
			return recordErr("needs text or secret", "нужно text или secret")
		}
		if s.Secret != "" && !recordEnv.MatchString(s.Secret) {
			return recordErr("secret names an environment variable (letters, digits, '_'), not the value: run the command with `harnessly secret run --env NAME=safe-name -- ...`",
				"secret называет переменную окружения (буквы, цифры, '_'), а не значение: запускай команду через `harnessly secret run --env NAME=имя-в-сейфе -- ...`")
		}
	case "key":
		if _, ok := recordKeys[s.Key]; !ok {
			return recordErr("unknown key %q", "неизвестная клавиша %q", s.Key)
		}
	case "scroll":
		if (s.By == 0) == (s.To == "") {
			return recordErr("scroll takes by or to", "scroll принимает by или to")
		}
	case "wait":
		return need(s.Ms > 0, "ms")
	}
	return nil
}

// ---- the log ---------------------------------------------------------------------------------------------------------

// RecordLog is <name>.json: what the scene needs to draw the cursor and aim the camera. T is in ms from the clip's first
// frame, X and Y in CSS pixels of the page, a box is [x, y, w, h].
type RecordLog struct {
	V        int           `json:"v"`
	Name     string        `json:"name"`
	Video    string        `json:"video"`
	FPS      int           `json:"fps"`
	Frames   int           `json:"frames"`
	Dropped  int           `json:"dropped"` // frames the browser gave no picture for (the previous one stays)
	DurMs    int           `json:"durMs"`
	Width    int           `json:"width"` // the clip's pixels
	Height   int           `json:"height"`
	Viewport [2]int        `json:"viewport"`
	Scale    int           `json:"scale"`
	Theme    string        `json:"theme"`
	Events   []RecordEvent `json:"events"`
	// Cursor is [t, x, y] of every frame.
	Cursor [][3]float64 `json:"cursor"`
}

// RecordEvent is one thing that happened: open, click, hover, type, key, scroll.
type RecordEvent struct {
	T      int       `json:"t"`
	End    int       `json:"end,omitempty"`
	Type   string    `json:"type"`
	X      *float64  `json:"x,omitempty"`
	Y      *float64  `json:"y,omitempty"`
	BBox   []float64 `json:"bbox,omitempty"`
	On     string    `json:"on,omitempty"`
	Tag    string    `json:"tag,omitempty"`
	Label  string    `json:"label,omitempty"`
	Text   string    `json:"text,omitempty"`
	Masked bool      `json:"masked,omitempty"`
	Key    string    `json:"key,omitempty"`
	URL    string    `json:"url,omitempty"`
	Title  string    `json:"title,omitempty"`
	By     int       `json:"by,omitempty"`
}

// RecordMask is what the log shows instead of a secret or a password field's text.
const RecordMask = "••••••••"

// secretForms is every spelling a secret may take in a log: as it is, escaped for a query, a path and the browser's
// encodeURIComponent (a space is %20), in hex of either case, and as JSON text.
func secretForms(values []string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, v := range values {
		if len(v) < 3 {
			continue
		}
		q := url.QueryEscape(v)
		comp := strings.ReplaceAll(q, "+", "%20")
		for _, f := range []string{v, q, url.PathEscape(v), comp} {
			add(f)
			add(strings.ToLower(f))
		}
		if j, err := json.Marshal(v); err == nil {
			add(string(j[1 : len(j)-1]))
		}
	}
	// longest first, so a form that holds another is masked whole
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// redact puts the mask over every spelling of a secret value in the log's text fields and drops the query and the
// fragment of every address (they carry tokens). A value under 3 characters is masked only where the step said it was a
// secret.
func (lg *RecordLog) redact(values []string) {
	for i := range lg.Events {
		lg.Events[i].URL = stripURL(lg.Events[i].URL)
	}
	forms := secretForms(values)
	if len(forms) == 0 {
		return
	}
	r := strings.NewReplacer(replacerPairs(forms, RecordMask)...)
	for i := range lg.Events {
		e := &lg.Events[i]
		e.On, e.Label, e.Text, e.URL, e.Title, e.Key = r.Replace(e.On), r.Replace(e.Label), r.Replace(e.Text), r.Replace(e.URL), r.Replace(e.Title), r.Replace(e.Key)
	}
}

// stripURL is an address without its query and fragment.
func stripURL(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}

// leaks says whether a secret is in the log's JSON in any form: raw, JSON-unescaped, or percent-decoded.
func leaks(raw []byte, values []string) bool {
	var short []string
	for _, v := range values {
		if len(v) >= 3 {
			short = append(short, v)
		}
	}
	if len(short) == 0 {
		return false
	}
	has := func(s string) bool {
		for _, v := range short {
			if strings.Contains(s, v) || strings.Contains(strings.ToLower(s), strings.ToLower(v)) {
				return true
			}
		}
		return false
	}
	if has(string(raw)) {
		return true
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return true
	}
	var walk func(any) bool
	walk = func(x any) bool {
		switch t := x.(type) {
		case string:
			if has(t) {
				return true
			}
			if d, err := url.PathUnescape(t); err == nil && has(d) {
				return true
			}
			if d, err := url.QueryUnescape(t); err == nil && has(d) {
				return true
			}
		case []any:
			for _, e := range t {
				if walk(e) {
					return true
				}
			}
		case map[string]any:
			for k, e := range t {
				if has(k) || walk(e) {
					return true
				}
			}
		}
		return false
	}
	return walk(doc)
}

func replacerPairs(olds []string, mask string) []string {
	out := make([]string, 0, len(olds)*2)
	for _, o := range olds {
		out = append(out, o, mask)
	}
	return out
}

func roundTo(v float64, places int) float64 {
	k := math.Pow(10, float64(places))
	return math.Round(v*k) / k
}

func errNoShell() error {
	return errors.New(locale.Pick(
		"no chrome-headless-shell on this machine: the recording needs it (an ordinary Chrome does not step frames on a clock of its own). Install it with `npx @puppeteer/browsers install chrome-headless-shell@stable --path ~/.cache/chrome-headless-shell` or name it in RUNTIME_DESIGN_HEADLESS_SHELL",
		"на машине нет chrome-headless-shell: запись без него не идёт (обычный Chrome не ведёт кадры по своим часам). Поставь: `npx @puppeteer/browsers install chrome-headless-shell@stable --path ~/.cache/chrome-headless-shell` или укажи путь в RUNTIME_DESIGN_HEADLESS_SHELL"))
}
