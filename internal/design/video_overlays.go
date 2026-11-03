package design

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// The overlay layer of a scene: titles, lower thirds, callouts, highlights, key caps, counters and badges that
// are drawn over the scene's board (or over a recorded interface) as a pure function of time. They are a declarative list
// in meta/scenes/<pageId>.json, so a person edits them in the player and an agent through the CLI or the MCP tools, and
// neither needs the board's HTML. video-runtime.js draws them in the live player and in the render alike.
//
// Positions are fractions of the frame (0..1, from the top left). The 9:16 format has its own optional place
// (Portrait); without one the 16:9 place is moved into the format's safe zone.

// VideoOverlayKind is what an overlay is.
type VideoOverlayKind string

const (
	VideoOverlayTitle     VideoOverlayKind = "title"     // a big line or two
	VideoOverlayCaption   VideoOverlayKind = "caption"   // a lower third: a plate with an accent bar
	VideoOverlayCallout   VideoOverlayKind = "callout"   // a note with an arrow to a point of the frame
	VideoOverlayHighlight VideoOverlayKind = "highlight" // a frame around an area, the rest dimmed
	VideoOverlayKey       VideoOverlayKind = "key"       // key caps: "Ctrl + K"
	VideoOverlayCounter   VideoOverlayKind = "counter"   // a number that counts up
	VideoOverlayBadge     VideoOverlayKind = "badge"     // a sticker
)

// VideoOverlayKinds is every kind, in the order the page offers them.
var VideoOverlayKinds = []VideoOverlayKind{VideoOverlayTitle, VideoOverlayCaption, VideoOverlayCallout, VideoOverlayHighlight,
	VideoOverlayKey, VideoOverlayCounter, VideoOverlayBadge}

// VideoOverlayEnter is how an overlay comes in. The text styles (typewriter, mask, words, scramble) act on the text; on
// a kind that is not text (highlight, key caps) they are played as a fade.
type VideoOverlayEnter string

// VideoOverlayEnters is every way in.
var VideoOverlayEnters = []VideoOverlayEnter{"fade", "rise", "pop", "typewriter", "mask", "words", "scramble"}

// VideoOverlayLeave is how an overlay goes out.
type VideoOverlayLeave string

// VideoOverlayLeaves is every way out.
var VideoOverlayLeaves = []VideoOverlayLeave{"none", "fade", "fall", "shrink"}

// VideoOverlayTheme is the overlay's look, made of the board's deck variables (--deck-ink, --deck-acc, --deck-bg ...).
type VideoOverlayTheme string

// VideoOverlayThemes is every theme.
var VideoOverlayThemes = []VideoOverlayTheme{"auto", "accent", "dark", "light"}

// VideoOverlayAnchor is how the text sits in its box.
type VideoOverlayAnchor string

// VideoOverlayAnchors is every anchor.
var VideoOverlayAnchors = []VideoOverlayAnchor{"left", "center", "right"}

// VideoOverlayAuthor tells who wrote an overlay last: a person's edit wins over an agent's.
type VideoOverlayAuthor string

const (
	VideoAuthorHuman VideoOverlayAuthor = "human"
	VideoAuthorAgent VideoOverlayAuthor = "agent"
)

// The bounds of an overlay.
const (
	VideoOverlaysMax    = 24
	VideoOverlayTextMax = 200
	VideoOverlayMinSize = 0.02
	VideoOverlayMinMs   = 100
)

// VideoOverlayPlace is where an overlay stands in a format: a box in fractions of the frame and, for a callout, the point
// its arrow ends at.
type VideoOverlayPlace struct {
	X  float64  `json:"x"`
	Y  float64  `json:"y"`
	W  float64  `json:"w"`
	H  float64  `json:"h"`
	TX *float64 `json:"tx,omitempty"`
	TY *float64 `json:"ty,omitempty"`
}

// VideoOverlay is one element of a scene's overlay layer. At and Out are ms from the scene's start (Out nil: to the
// scene's end). Value, From and Decimals belong to a counter, whose number is Value counted from From; Text then is the
// label ("{n} ₽" puts the number where {n} is, any other text follows it).
type VideoOverlay struct {
	ID        string             `json:"id"`
	Kind      VideoOverlayKind   `json:"kind"`
	Text      string             `json:"text"`
	At        int64              `json:"at"`
	Out       *int64             `json:"out"`
	X         float64            `json:"x"`
	Y         float64            `json:"y"`
	W         float64            `json:"w"`
	H         float64            `json:"h"`
	TX        *float64           `json:"tx,omitempty"`
	TY        *float64           `json:"ty,omitempty"`
	Portrait  *VideoOverlayPlace `json:"portrait,omitempty"`
	Anchor    VideoOverlayAnchor `json:"anchor"`
	Enter     VideoOverlayEnter  `json:"enter"`
	Leave     VideoOverlayLeave  `json:"leave"`
	Theme     VideoOverlayTheme  `json:"theme"`
	Value     *float64           `json:"value,omitempty"`
	From      float64            `json:"from,omitempty"`
	Decimals  int                `json:"decimals,omitempty"`
	Author    VideoOverlayAuthor `json:"author"`
	UpdatedAt string             `json:"updatedAt"`
}

// Place is the overlay's base (16:9) place.
func (o VideoOverlay) Place() VideoOverlayPlace {
	return VideoOverlayPlace{X: o.X, Y: o.Y, W: o.W, H: o.H, TX: o.TX, TY: o.TY}
}

// videoSafe is a format's safe zone: the margins (fractions) that text keeps clear of. The 9:16 bottom is wide: the
// players of short videos put their own buttons and captions there.
type videoSafe struct{ l, t, r, b float64 }

func videoSafeZone(f VideoFormat) videoSafe {
	if f == VideoPortrait {
		return videoSafe{0.06, 0.08, 0.06, 0.17}
	}
	return videoSafe{0.04, 0.06, 0.04, 0.06}
}

// VideoOverlayPlaceIn is where the overlay stands in f: 16:9 is its own place; 9:16 is its Portrait place, or the base
// place moved into the safe zone (a highlight is an area of the picture and is not moved).
func VideoOverlayPlaceIn(o VideoOverlay, f VideoFormat) VideoOverlayPlace {
	if f != VideoPortrait {
		return o.Place()
	}
	if o.Portrait != nil {
		return *o.Portrait
	}
	p := o.Place()
	if o.Kind == VideoOverlayHighlight {
		return p
	}
	z := videoSafeZone(f)
	p.W = math.Min(p.W, 1-z.l-z.r)
	p.H = math.Min(p.H, 1-z.t-z.b)
	p.X = math.Min(math.Max(p.X, z.l), 1-z.r-p.W)
	p.Y = math.Min(math.Max(p.Y, z.t), 1-z.b-p.H)
	return p
}

var videoOverlayIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func inList[T comparable](list []T, v T) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func overlayErr(en, ru string, a ...any) error {
	return deckErr(http400, fmt.Sprintf(en, a...), fmt.Sprintf(ru, a...))
}

func finiteIn(v, lo, hi float64) bool { return !math.IsNaN(v) && v >= lo && v <= hi }

func validPlace(o VideoOverlay, p VideoOverlayPlace, which string) error {
	if !finiteIn(p.X, 0, 1) || !finiteIn(p.Y, 0, 1) || !finiteIn(p.W, VideoOverlayMinSize, 1) || !finiteIn(p.H, VideoOverlayMinSize, 1) ||
		p.X+p.W > 1.0001 || p.Y+p.H > 1.0001 {
		return overlayErr("%s: x, y, w, h are fractions of the frame (0..1) and the box stays inside it", "%s: x, y, w, h — доли кадра (0..1), блок не выходит за кадр", which)
	}
	if o.Kind == VideoOverlayCallout {
		if p.TX == nil || p.TY == nil || !finiteIn(*p.TX, 0, 1) || !finiteIn(*p.TY, 0, 1) {
			return overlayErr("%s: a callout needs tx and ty (0..1), the point its arrow ends at", "%s: у выноски нужны tx и ty (0..1) — точка, куда идёт стрелка", which)
		}
	}
	return nil
}

// ValidateVideoOverlay checks one overlay against the contract's bounds.
func ValidateVideoOverlay(o VideoOverlay) error {
	if !videoOverlayIDRe.MatchString(o.ID) {
		return overlayErr("id: lower case letters, digits and dashes, up to 40", "id: строчные буквы, цифры и дефисы, до 40 знаков")
	}
	if !inList(VideoOverlayKinds, o.Kind) {
		return overlayErr("kind: one of %s", "kind: одно из %s", joinKinds())
	}
	if utf8.RuneCountInString(o.Text) > VideoOverlayTextMax {
		return overlayErr("text: up to %d characters", "text: до %d знаков", VideoOverlayTextMax)
	}
	if strings.TrimSpace(o.Text) == "" && o.Kind != VideoOverlayHighlight && !(o.Kind == VideoOverlayCounter && o.Value != nil) {
		return overlayErr("text is expected", "Нужен text")
	}
	if o.At < 0 || o.At > VideoSceneDurMax {
		return overlayErr("at: from 0 to %d ms", "at: от 0 до %d мс", VideoSceneDurMax)
	}
	if o.Out != nil && (*o.Out < o.At+VideoOverlayMinMs || *o.Out > VideoSceneDurMax) {
		return overlayErr("out: after at, from at+100 to %d ms, or null for the scene's end", "out: позже at, от at+100 до %d мс, или null — до конца сцены", VideoSceneDurMax)
	}
	if err := validPlace(o, o.Place(), "x, y, w, h"); err != nil {
		return err
	}
	if o.Portrait != nil {
		if err := validPlace(o, *o.Portrait, "portrait"); err != nil {
			return err
		}
	}
	if !inList(VideoOverlayAnchors, o.Anchor) {
		return overlayErr("anchor: left, center or right", "anchor: left, center или right")
	}
	if !inList(VideoOverlayEnters, o.Enter) {
		return overlayErr("enter: one of %s", "enter: одно из %s", joinEnters())
	}
	if !inList(VideoOverlayLeaves, o.Leave) {
		return overlayErr("leave: none, fade, fall or shrink", "leave: none, fade, fall или shrink")
	}
	if !inList(VideoOverlayThemes, o.Theme) {
		return overlayErr("theme: auto, accent, dark or light", "theme: auto, accent, dark или light")
	}
	if o.Kind == VideoOverlayCounter {
		if o.Value == nil || math.IsNaN(*o.Value) || math.Abs(*o.Value) > 1e12 || math.IsNaN(o.From) || math.Abs(o.From) > 1e12 {
			return overlayErr("value: the number a counter counts to", "value: число, до которого считает счётчик")
		}
		if o.Decimals < 0 || o.Decimals > 3 {
			return overlayErr("decimals: from 0 to 3", "decimals: от 0 до 3")
		}
	}
	if o.Author != VideoAuthorHuman && o.Author != VideoAuthorAgent {
		return overlayErr("author: human or agent", "author: human или agent")
	}
	return nil
}

func joinKinds() string {
	out := make([]string, len(VideoOverlayKinds))
	for i, k := range VideoOverlayKinds {
		out[i] = string(k)
	}
	return strings.Join(out, ", ")
}

func joinEnters() string {
	out := make([]string, len(VideoOverlayEnters))
	for i, k := range VideoOverlayEnters {
		out[i] = string(k)
	}
	return strings.Join(out, ", ")
}

// ValidateVideoOverlays checks a scene's list: each overlay, unique ids, at most VideoOverlaysMax.
func ValidateVideoOverlays(list []VideoOverlay) error {
	if len(list) > VideoOverlaysMax {
		return overlayErr("a scene holds at most %d overlays", "В сцене не больше %d надписей", VideoOverlaysMax)
	}
	seen := map[string]bool{}
	for _, o := range list {
		if err := ValidateVideoOverlay(o); err != nil {
			return fmt.Errorf("%s: %w", o.ID, err)
		}
		if seen[o.ID] {
			return overlayErr("overlay id %q is used twice", "id надписи %q повторяется", o.ID)
		}
		seen[o.ID] = true
	}
	return nil
}

// videoOverlayDefaults is a new overlay of kind k: where it stands and how it moves when nothing else is said.
func videoOverlayDefaults(k VideoOverlayKind) VideoOverlay {
	o := VideoOverlay{Kind: k, Anchor: "center", Enter: "fade", Leave: "fade", Theme: "auto", X: 0.1, Y: 0.4, W: 0.8, H: 0.16}
	half := func(v float64) *float64 { return &v }
	switch k {
	case VideoOverlayTitle:
		o.Enter = "rise"
	case VideoOverlayCaption:
		o.X, o.Y, o.W, o.H, o.Anchor, o.Enter = 0.06, 0.76, 0.5, 0.1, "left", "mask"
	case VideoOverlayCallout:
		o.X, o.Y, o.W, o.H, o.Anchor, o.Enter, o.Theme = 0.58, 0.18, 0.3, 0.1, "left", "pop", "accent"
		o.TX, o.TY = half(0.5), half(0.45)
	case VideoOverlayHighlight:
		o.X, o.Y, o.W, o.H, o.Enter = 0.3, 0.3, 0.4, 0.3, "fade"
	case VideoOverlayKey:
		o.X, o.Y, o.W, o.H, o.Enter = 0.38, 0.8, 0.24, 0.09, "pop"
	case VideoOverlayCounter:
		o.X, o.Y, o.W, o.H, o.Enter, o.Theme = 0.2, 0.4, 0.6, 0.2, "rise", "accent"
		v := 100.0
		o.Value = &v
	case VideoOverlayBadge:
		o.X, o.Y, o.W, o.H, o.Enter, o.Theme = 0.7, 0.1, 0.22, 0.08, "pop", "accent"
	}
	return o
}

// videoOverlayKeys are the fields a patch may name; id, author and updatedAt are the store's.
var videoOverlayKeys = map[string]bool{
	"kind": true, "text": true, "at": true, "out": true, "x": true, "y": true, "w": true, "h": true, "tx": true, "ty": true,
	"portrait": true, "anchor": true, "enter": true, "leave": true, "theme": true, "value": true, "from": true, "decimals": true,
}

// mergeOverlay lays a patch over an overlay: a key that is absent stays, null clears an optional one.
func mergeOverlay(base VideoOverlay, patch map[string]json.RawMessage) (VideoOverlay, error) {
	raw, err := json.Marshal(base)
	if err != nil {
		return base, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return base, err
	}
	for k, v := range patch {
		if !videoOverlayKeys[k] {
			return base, overlayErr("%s: unknown or fixed field", "%s: неизвестное или неизменяемое поле", k)
		}
		all[k] = v
	}
	merged, err := json.Marshal(all)
	if err != nil {
		return base, err
	}
	dec := json.NewDecoder(bytes.NewReader(merged))
	dec.DisallowUnknownFields()
	var out VideoOverlay
	if err := dec.Decode(&out); err != nil {
		return base, overlayErr("The overlay has a wrong value: %v", "В надписи неверное значение: %v", err)
	}
	return out, nil
}

// ---- the store

// videoMetaMu serializes the read-modify-write of scene meta files: a person and an agent may edit at once.
var videoMetaMu sync.Mutex

// VideoOverlayActor is who edits: the page's routes are a person (human), the CLI and the MCP tools are the agent.
type VideoOverlayActor = VideoOverlayAuthor

// humanOverlayErr is a refusal to let an agent change what a person changed, unless the agent forces it.
func humanOverlayErr(o VideoOverlay) error {
	when := o.UpdatedAt
	if t, err := time.Parse(time.RFC3339, o.UpdatedAt); err == nil {
		when = t.UTC().Format("2006-01-02 15:04")
	}
	return deckErr(http409,
		fmt.Sprintf("Overlay %q was edited by a person (%s UTC), so it was left as it is. If the person asked for this change, repeat it with force (--force).", o.ID, when),
		fmt.Sprintf("Надпись %q правил человек (%s UTC), поэтому она не тронута. Если человек просил именно это — повтори с force (--force).", o.ID, when))
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

// withOverlays reads the scene's meta, lets fn change its overlays, validates and writes it, and answers the scene.
func (l *Library) withOverlays(id, ref string, fn func(list []VideoOverlay) ([]VideoOverlay, error)) (VideoScene, error) {
	if err := l.requireVideo(id); err != nil {
		return VideoScene{}, err
	}
	p, err := l.deckPage(id, ref)
	if err != nil {
		return VideoScene{}, err
	}
	videoMetaMu.Lock()
	defer videoMetaMu.Unlock()
	meta, err := l.ReadVideoSceneMeta(id, p.ID)
	if err != nil {
		return VideoScene{}, err
	}
	list, err := fn(append([]VideoOverlay(nil), meta.Overlays...))
	if err != nil {
		return VideoScene{}, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].At < list[j].At })
	if err := ValidateVideoOverlays(list); err != nil {
		return VideoScene{}, err
	}
	meta.Overlays = list
	if err := l.writeVideoSceneMeta(id, p.ID, meta); err != nil {
		return VideoScene{}, err
	}
	return l.videoSceneByID(id, p.ID)
}

func overlayIndex(list []VideoOverlay, oid string) int {
	for i, o := range list {
		if o.ID == oid {
			return i
		}
	}
	return -1
}

func noOverlayErr(oid string) error {
	return deckErr(http404, fmt.Sprintf("No overlay %q in the scene", oid), fmt.Sprintf("В сцене нет надписи %q", oid))
}

// nextOverlayID is "<kind>-<n>" with the least n that is free.
func nextOverlayID(list []VideoOverlay, k VideoOverlayKind) string {
	for n := 1; ; n++ {
		id := fmt.Sprintf("%s-%d", k, n)
		if overlayIndex(list, id) < 0 {
			return id
		}
	}
}

// AddVideoOverlay adds an overlay to a scene. patch names its fields over the kind's defaults ("kind" is required); id,
// when given, must be free. The answer is the scene and the new overlay's id.
func (l *Library) AddVideoOverlay(id, scene string, oid string, patch map[string]json.RawMessage, actor VideoOverlayActor) (VideoScene, string, error) {
	var made string
	var kind VideoOverlayKind
	if raw, ok := patch["kind"]; ok {
		if err := json.Unmarshal(raw, &kind); err != nil {
			return VideoScene{}, "", overlayErr("kind: a wrong value", "kind: неверное значение")
		}
	}
	if !inList(VideoOverlayKinds, kind) {
		return VideoScene{}, "", overlayErr("kind: one of %s", "kind: одно из %s", joinKinds())
	}
	sc, err := l.withOverlays(id, scene, func(list []VideoOverlay) ([]VideoOverlay, error) {
		base := videoOverlayDefaults(kind)
		o, err := mergeOverlay(base, patch)
		if err != nil {
			return nil, err
		}
		if oid == "" {
			oid = nextOverlayID(list, o.Kind)
		} else if overlayIndex(list, oid) >= 0 {
			return nil, deckErr(http409, fmt.Sprintf("Overlay %q already exists", oid), fmt.Sprintf("Надпись %q уже есть", oid))
		}
		o.ID, o.Author, o.UpdatedAt = oid, actor, nowRFC3339()
		made = oid
		return append(list, o), nil
	})
	return sc, made, err
}

// SetVideoOverlay changes an overlay. An agent may not change what a person wrote last unless force is set; whoever
// changes it is its author after. The answer is the scene.
func (l *Library) SetVideoOverlay(id, scene, oid string, patch map[string]json.RawMessage, actor VideoOverlayActor, force bool) (VideoScene, error) {
	return l.withOverlays(id, scene, func(list []VideoOverlay) ([]VideoOverlay, error) {
		i := overlayIndex(list, oid)
		if i < 0 {
			return nil, noOverlayErr(oid)
		}
		if actor == VideoAuthorAgent && list[i].Author == VideoAuthorHuman && !force {
			return nil, humanOverlayErr(list[i])
		}
		o, err := mergeOverlay(list[i], patch)
		if err != nil {
			return nil, err
		}
		o.ID, o.Author, o.UpdatedAt = oid, actor, nowRFC3339()
		list[i] = o
		return list, nil
	})
}

// RemoveVideoOverlay removes an overlay, by the same rule as SetVideoOverlay.
func (l *Library) RemoveVideoOverlay(id, scene, oid string, actor VideoOverlayActor, force bool) (VideoScene, error) {
	return l.withOverlays(id, scene, func(list []VideoOverlay) ([]VideoOverlay, error) {
		i := overlayIndex(list, oid)
		if i < 0 {
			return nil, noOverlayErr(oid)
		}
		if actor == VideoAuthorAgent && list[i].Author == VideoAuthorHuman && !force {
			return nil, humanOverlayErr(list[i])
		}
		return append(list[:i], list[i+1:]...), nil
	})
}

// VideoOverlaysOf is a scene's overlays, in time order.
func (l *Library) VideoOverlaysOf(id, scene string) ([]VideoOverlay, error) {
	if err := l.requireVideo(id); err != nil {
		return nil, err
	}
	p, err := l.deckPage(id, scene)
	if err != nil {
		return nil, err
	}
	meta, err := l.ReadVideoSceneMeta(id, p.ID)
	if err != nil {
		return nil, err
	}
	return meta.Overlays, nil
}

// VideoOverlayClashes names the overlays that stand on the one with this id at the same time: boxes that cross by more
// than a fifth of the smaller one while both are on screen. A highlight is an area, not a text, and is left out. It is a
// warning for whoever placed the overlay, not a refusal.
func VideoOverlayClashes(list []VideoOverlay, id string) []string {
	i := overlayIndex(list, id)
	if i < 0 || list[i].Kind == VideoOverlayHighlight {
		return nil
	}
	end := func(o VideoOverlay) int64 {
		if o.Out == nil {
			return 1 << 60
		}
		return *o.Out
	}
	a := list[i]
	var out []string
	for j, b := range list {
		if j == i || b.Kind == VideoOverlayHighlight || a.At >= end(b) || b.At >= end(a) {
			continue
		}
		w := min(a.X+a.W, b.X+b.W) - max(a.X, b.X)
		h := min(a.Y+a.H, b.Y+b.H) - max(a.Y, b.Y)
		if w <= 0 || h <= 0 {
			continue
		}
		if w*h > 0.2*min(a.W*a.H, b.W*b.H) {
			out = append(out, b.ID)
		}
	}
	return out
}
