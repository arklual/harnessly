package design

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// A scene's time, read from the board's source. This is the one place that reads data-tk-at, data-tk-out,
// data-build-in, data-block, data-step and data-transition; the rest of the model takes VideoScene as it is. Durations
// are computed here and nowhere else (D5).

const (
	// maxVideoBoardBytes caps a board read for its moments.
	maxVideoBoardBytes = 8 << 20
	// videoDerivedFirst and videoDerivedGap place a block that has a step or an effect but no data-tk-at: the first at
	// 0.4 s, then one a second, in the order of data-step, ties in document order.
	videoDerivedFirst = 400
	videoDerivedGap   = 1000
	// videoFadeMs is the fade of a board that says data-transition="fade|push|magic" and sets no length.
	videoFadeMs = 500
)

// videoTag is one start tag of a board: its name and its attributes (names lower case, values unescaped).
type videoTag struct {
	name  string
	attrs map[string]string
}

// videoTags walks the start tags of a board's source in document order. Comments and the bodies of script, style and
// template are skipped, quoted values may hold ">".
func videoTags(src string, each func(videoTag)) {
	i := 0
	for i < len(src) {
		j := strings.IndexByte(src[i:], '<')
		if j < 0 {
			return
		}
		i += j
		if strings.HasPrefix(src[i:], "<!--") {
			end := strings.Index(src[i+4:], "-->")
			if end < 0 {
				return
			}
			i += 4 + end + 3
			continue
		}
		if i+1 >= len(src) || !isTagStart(src[i+1]) {
			i++
			continue
		}
		tag, next := readVideoTag(src, i+1)
		i = next
		each(tag)
		switch tag.name {
		case "script", "style":
			end := indexFold(src[i:], "</"+tag.name)
			if end < 0 {
				return
			}
			i += end
		}
	}
}

func isTagStart(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

func indexFold(s, sub string) int { return strings.Index(strings.ToLower(s), sub) }

// readVideoTag reads a start tag whose name begins at src[i]; it returns the tag and the index after its ">".
func readVideoTag(src string, i int) (videoTag, int) {
	start := i
	for i < len(src) && !strings.ContainsRune(" \t\r\n/>", rune(src[i])) {
		i++
	}
	tag := videoTag{name: strings.ToLower(src[start:i]), attrs: map[string]string{}}
	for i < len(src) {
		for i < len(src) && strings.ContainsRune(" \t\r\n/", rune(src[i])) {
			i++
		}
		if i >= len(src) {
			break
		}
		if src[i] == '>' {
			return tag, i + 1
		}
		ns := i
		for i < len(src) && !strings.ContainsRune(" \t\r\n/>=", rune(src[i])) {
			i++
		}
		name := strings.ToLower(src[ns:i])
		for i < len(src) && strings.ContainsRune(" \t\r\n", rune(src[i])) {
			i++
		}
		val := ""
		if i < len(src) && src[i] == '=' {
			i++
			for i < len(src) && strings.ContainsRune(" \t\r\n", rune(src[i])) {
				i++
			}
			if i < len(src) && (src[i] == '"' || src[i] == '\'') {
				q := src[i]
				end := strings.IndexByte(src[i+1:], q)
				if end < 0 {
					return tag, len(src)
				}
				val = src[i+1 : i+1+end]
				i += end + 2
			} else {
				vs := i
				for i < len(src) && !strings.ContainsRune(" \t\r\n>", rune(src[i])) {
					i++
				}
				val = src[vs:i]
			}
		}
		if name != "" {
			if _, dup := tag.attrs[name]; !dup {
				tag.attrs[name] = html.UnescapeString(val)
			}
		}
	}
	return tag, i
}

// videoSeconds reads a data-tk-* value, seconds as a decimal string, as ms. ok false: absent or not a number ≥ 0.
func videoSeconds(raw string, has bool) (int64, bool) {
	if !has {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 24*3600 {
		return 0, false
	}
	return int64(math.Round(f * 1000)), true
}

// VideoBoard is what a board's source says about time.
type VideoBoard struct {
	Moments []VideoMoment
	// Transition is the board's own data-transition on <main>; nil when it says none.
	Transition *VideoTransition
}

// ParseVideoBoard reads a board's moments and transition. An element is a moment when it has data-tk-at or data-tk-out
// (written time), or else a data-step or a data-build-in (derived time). A value that is not a number is read as
// absent; the check (videocheck.go) is what tells the agent. Moments come out in time order, ties in document order.
func ParseVideoBoard(src string) VideoBoard {
	var out VideoBoard
	type derived struct {
		m    VideoMoment
		step int
		seq  int
	}
	var ders []derived
	seq := 0
	videoTags(src, func(t videoTag) {
		if v, ok := t.attrs["data-transition"]; ok && t.name == "main" && out.Transition == nil {
			out.Transition = boardTransition(v)
		}
		atRaw, hasAt := t.attrs["data-tk-at"]
		if !hasAt {
			// A mark is a moment on the time line that moves nothing (GSAP moves the block).
			atRaw, hasAt = t.attrs["data-tk-mark"]
		}
		outRaw, hasOut := t.attrs["data-tk-out"]
		stepRaw, hasStep := t.attrs["data-step"]
		buildRaw, hasBuild := t.attrs["data-build-in"]
		at, okAt := videoSeconds(atRaw, hasAt)
		end, okOut := videoSeconds(outRaw, hasOut)
		step, stepErr := strconv.Atoi(strings.TrimSpace(stepRaw))
		okStep := hasStep && stepErr == nil && step > 0
		build := strings.TrimSpace(buildRaw)
		okBuild := false
		for _, b := range VideoBuildIns {
			okBuild = okBuild || b == build
		}
		if !okBuild {
			build = ""
		}
		m := VideoMoment{Block: strings.TrimSpace(t.attrs["data-block"]), BuildIn: build}
		if n, err := strconv.Atoi(strings.TrimSpace(t.attrs["data-dc-tpl"])); err == nil && n >= 0 {
			m.Tpl = &n
		}
		seq++
		switch {
		case okAt || okOut:
			m.At = at
			if okOut && end > at {
				m.Out = &end
			}
			out.Moments = append(out.Moments, m)
		case okStep || (hasBuild && okBuild):
			m.Derived = true
			s := math.MaxInt32
			if okStep {
				s = step
			}
			ders = append(ders, derived{m, s, seq})
		}
	})
	sort.SliceStable(ders, func(i, j int) bool {
		if ders[i].step != ders[j].step {
			return ders[i].step < ders[j].step
		}
		return ders[i].seq < ders[j].seq
	})
	for k, d := range ders {
		d.m.At = videoDerivedFirst + int64(k)*videoDerivedGap
		out.Moments = append(out.Moments, d.m)
	}
	sort.SliceStable(out.Moments, func(i, j int) bool { return out.Moments[i].At < out.Moments[j].At })
	if out.Moments == nil {
		out.Moments = []VideoMoment{}
	}
	return out
}

// boardTransition reads a board's data-transition: fade, push and magic play as a fade (D3), none is none, anything
// else is read as no word.
func boardTransition(v string) *VideoTransition {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "fade", "push", "magic":
		return &VideoTransition{Kind: VideoTransitionFade, Ms: videoFadeMs}
	case "none":
		return &VideoTransition{Kind: VideoTransitionNone}
	}
	return nil
}

// lastMomentEnd is when the last thing on the scene has appeared or left: the largest of At and Out.
func lastMomentEnd(ms []VideoMoment) int64 {
	var end int64
	for _, m := range ms {
		end = max(end, m.At)
		if m.Out != nil {
			end = max(end, *m.Out)
		}
	}
	return end
}

// ---- meta/scenes/<pageId>.json

func (l *Library) sceneMetaFile(id, page string) string {
	return filepath.Join(l.deckFile(id, VideoScenesDir), page+".json")
}

// ValidateVideoSceneMeta checks a scene's meta against the contract's bounds.
func ValidateVideoSceneMeta(m VideoSceneMeta) error {
	if m.V != 1 {
		return deckErr(http400, "scene.v must be 1", "scene.v должен быть 1")
	}
	if m.Dur != nil && (*m.Dur < VideoSceneDurMin || *m.Dur > VideoSceneDurMax) {
		return deckErr(http400, "dur: from 1000 to 60000 ms, or null for automatic", "dur: от 1000 до 60000 мс или null — по тексту")
	}
	if t := m.Transition; t != nil {
		okKind := false
		for _, k := range VideoTransitionKinds {
			okKind = okKind || k == t.Kind
		}
		if !okKind {
			return deckErr(http400, "transition.kind: none or fade", "transition.kind: none или fade")
		}
		if t.Ms < 0 || t.Ms > VideoTransitionMsMax {
			return deckErr(http400, "transition.ms: from 0 to 2000", "transition.ms: от 0 до 2000")
		}
	}
	return nil
}

// ReadVideoSceneMeta is meta/scenes/<page>.json; a scene with no file is automatic. A file that does not parse is an
// error: it is not guessed.
func (l *Library) ReadVideoSceneMeta(id, page string) (VideoSceneMeta, error) {
	raw, err := readFileLimited(l.sceneMetaFile(id, page), maxDeckFileBytes)
	if errors.Is(err, os.ErrNotExist) {
		return VideoSceneMeta{V: 1}, nil
	}
	if err != nil {
		return VideoSceneMeta{}, err
	}
	var m VideoSceneMeta
	if err := json.Unmarshal(raw, &m); err != nil {
		return VideoSceneMeta{}, fmt.Errorf("%s/%s.json: %w", VideoScenesDir, page, err)
	}
	if err := ValidateVideoSceneMeta(m); err != nil {
		return VideoSceneMeta{}, fmt.Errorf("%s/%s.json: %w", VideoScenesDir, page, err)
	}
	return m, nil
}

// writeVideoSceneMeta writes a scene's meta; a scene with nothing set has no file.
func (l *Library) writeVideoSceneMeta(id, page string, m VideoSceneMeta) error {
	if err := ValidateVideoSceneMeta(m); err != nil {
		return err
	}
	file := l.sceneMetaFile(id, page)
	if m.Dur == nil && m.Transition == nil {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := l.ensureMetaDirs(id); err != nil {
		return err
	}
	dir := l.deckFile(id, VideoScenesDir)
	if err := os.Mkdir(dir, 0o777); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	if !isRealDir(dir) {
		return errors.New(dir + " is not a plain folder")
	}
	_, err := writeIfChanged(file, deckJSON(m))
	return err
}

// ---- scenes

// videoSceneOf is one page as a scene, less its place on the time line (Start) and its cache keys.
func (l *Library) videoSceneOf(id string, p Page) (VideoScene, error) {
	slide := l.slideOf(id, p)
	sc := VideoScene{ID: p.ID, Path: p.File, Title: p.Title, Words: DeckWords(slide.Notes), Notes: slide.Notes,
		Version: slide.Version, Blank: slide.Blank, Moments: []VideoMoment{}, Keys: map[VideoFormat]string{}}
	var board VideoBoard
	if real := l.ResolveProjectFile(id, strings.Split(pageFileOf(p.File), "/")); real != "" {
		src, err := readFileLimited(real, maxVideoBoardBytes)
		if err != nil {
			return VideoScene{}, err
		}
		board = ParseVideoBoard(string(src))
	}
	sc.Moments = board.Moments
	if sc.Moments == nil {
		sc.Moments = []VideoMoment{}
	}
	meta, err := l.ReadVideoSceneMeta(id, p.ID)
	if err != nil {
		return VideoScene{}, err
	}
	sc.Dur, sc.Auto = VideoAutoDur(lastMomentEnd(sc.Moments), sc.Words), true
	if meta.Dur != nil {
		sc.Dur, sc.Auto = *meta.Dur, false
	}
	switch {
	case meta.Transition != nil:
		sc.Transition = *meta.Transition
	case board.Transition != nil:
		sc.Transition = *board.Transition
	default:
		sc.Transition = VideoTransition{Kind: VideoTransitionNone}
	}
	return sc, nil
}

// VideoScenes are the pages of the video whose board file is there, in order, on the time line: each starts where the
// one before ends. Keys has the clip key per enabled format; a key the render could not make is left out.
func (l *Library) VideoScenes(id string, s VideoSettings) ([]VideoScene, error) {
	out := []VideoScene{}
	m := l.ReadModel(id)
	if m == nil {
		return out, nil
	}
	project := l.ProjectDir(id)
	var start int64
	for _, p := range m.Pages {
		if !exists(filepath.Join(append([]string{project}, strings.Split(pageFileOf(p.File), "/")...)...)) {
			continue
		}
		sc, err := l.videoSceneOf(id, p)
		if err != nil {
			return nil, err
		}
		sc.Start = start
		start += sc.Dur
		for _, f := range s.Formats {
			if key, err := l.VideoSceneKey(id, sc, f, false); err == nil {
				sc.Keys[f] = key
			}
		}
		out = append(out, sc)
	}
	return out, nil
}
