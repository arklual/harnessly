package design

import (
	"bytes"
	"context"
	"image/jpeg"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// A video's check: the static lint of the boards' scripts, moments past a scene's end, and in Chrome at each scene's
// end per enabled format text overflow, blocks off screen, small text in 9:16 and frames that are not deterministic.

const (
	// videoMinFont is the smallest text in a 9:16 frame 1080 px wide.
	videoMinFont = 28
	// videoCheckBeforeEnd is how far before a scene's end the check looks (one frame at 30 fps).
	videoCheckBeforeEnd = 1000 / VideoFPS
	// videoWalkStep is the check's step through a scene's time; videoWalkOutside is how many steps in a row a block may
	// stay outside the frame (it may be on its way in or the camera may be passing it).
	videoWalkStep    = 250
	videoWalkOutside = 4
	// Two captures of one moment differ when more than this share of pixels is further apart than this level of 255.
	videoFrameDifferLevel = 48
	videoFrameDifferShare = 0.002
)

var (
	videoScriptRe = regexp.MustCompile(`(?is)<script\b([^>]*)>(.*?)</script>`)
	videoSrcRe    = regexp.MustCompile(`(?i)\ssrc\s*=`)
	// videoOutsideRe: an address a scene would load from outside the project and the kit.
	videoOutsideRe = regexp.MustCompile(`(?i)<(?:script|link|img|source|iframe|video|audio)\b[^>]*\s(?:src|href)\s*=\s*["']?((?:https?:)?//[^"'\s>]+)`)
	videoGsapRe    = regexp.MustCompile(`\bgsap\.(?:to|from|fromTo|set|timeline)\s*\(`)
	videoLints     = []struct {
		re    *regexp.Regexp
		code  string
		level VideoCheckLevel
		en    string
		ru    string
	}{
		{regexp.MustCompile(`\bset(?:Timeout|Interval)\s*\(`), VideoCheckTimer, VideoCheckError,
			"setTimeout / setInterval run on the wall clock: a frame of the video will not match its time. Drive motion by data-tk-at, @keyframes, tkVideo.motion or tkVideo.register",
			"setTimeout / setInterval идут по настенным часам: кадр видео не совпадёт со своим временем. Движение — через data-tk-at, @keyframes, tkVideo.motion или tkVideo.register"},
		{regexp.MustCompile(`\brequestAnimationFrame\s*\(`), VideoCheckRaf, VideoCheckWarn,
			"A requestAnimationFrame loop remembers the previous frame, so seeking breaks it: draw in tkVideo.register's seek(ms) instead",
			"Цикл requestAnimationFrame помнит прошлый кадр, и перемотка его ломает: рисуй в seek(ms) через tkVideo.register"},
		{regexp.MustCompile(`\bMath\.random\s*\(`), VideoCheckRandom, VideoCheckWarn,
			"Math.random is seeded in a video: the same numbers every time, which may not be what you meant",
			"Math.random в видео с фиксированным зерном: числа каждый раз одни и те же — проверьте, что так и задумано"},
		{regexp.MustCompile(`\bDate\.now\s*\(|\bnew\s+Date\s*\(\s*\)`), VideoCheckClock, VideoCheckWarn,
			"Date.now / new Date() give the scene's time in a video, not today's date",
			"Date.now / new Date() в видео дают время сцены, а не сегодняшнюю дату"},
	}
)

// videoScenesFor is the video's scenes (video_scenes.go) without keys; a variable so the tests can give their own.
var videoScenesFor = func(l *Library, id string) ([]VideoScene, error) { return l.VideoScenes(id, VideoSettings{}) }

// VideoCheck checks the scene page (nil: every scene) in format f (nil: every enabled format).
func (l *Library) VideoCheck(ctx context.Context, id string, page *string, f *VideoFormat) ([]VideoCheckIssue, error) {
	settings, err := l.ReadVideoSettings(id)
	if err != nil {
		return nil, err
	}
	formats := settings.Formats
	if f != nil {
		if !videoHasFormat(settings, *f) {
			return nil, deckErr(http.StatusBadRequest, "The video has no format "+string(*f), "У видео нет формата "+string(*f))
		}
		formats = []VideoFormat{*f}
	}
	scenes, err := l.videoPickScenes(id, page)
	if err != nil {
		return nil, err
	}
	issues := []VideoCheckIssue{}
	for _, sc := range scenes {
		issues = append(issues, l.videoLint(id, sc)...)
		issues = append(issues, videoPastEnd(sc)...)
	}
	if l.findChrome() == "" {
		return nil, &DeckError{Status: http.StatusServiceUnavailable, Msg: errNoChrome().Error()}
	}
	s, err := l.newVideoShooter(id)
	if err != nil {
		return nil, err
	}
	defer s.close()
	tab, release, err := s.videoTab(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	for _, sc := range scenes {
		if sc.Blank {
			continue
		}
		for _, fm := range formats {
			found, err := l.videoCheckInChrome(ctx, tab, id, sc, fm)
			if err != nil {
				return nil, err
			}
			issues = append(issues, found...)
		}
	}
	return issues, nil
}

func videoHasFormat(s VideoSettings, f VideoFormat) bool {
	for _, x := range s.Formats {
		if x == f {
			return true
		}
	}
	return false
}

// videoPickScenes is every scene, or the one page names (its id, file or title).
func (l *Library) videoPickScenes(id string, page *string) ([]VideoScene, error) {
	scenes, err := videoScenesFor(l, id)
	if err != nil {
		return nil, err
	}
	if page == nil {
		return scenes, nil
	}
	sc, err := l.videoPickScene(id, scenes, *page)
	if err != nil {
		return nil, err
	}
	return []VideoScene{sc}, nil
}

// videoPickScene is the scene page names: its id, file or title.
func (l *Library) videoPickScene(id string, scenes []VideoScene, page string) (VideoScene, error) {
	pid := page
	if p, err := l.deckPage(id, page); err == nil {
		pid = p.ID
	}
	for _, sc := range scenes {
		if sc.ID == pid {
			return sc, nil
		}
	}
	return VideoScene{}, deckErr(http.StatusNotFound, "No such scene: "+page, "Нет такой сцены: "+page)
}

// videoLint finds in the board's own scripts what runs on the wall clock.
func (l *Library) videoLint(id string, sc VideoScene) []VideoCheckIssue {
	file := l.ResolveProjectFile(id, strings.Split(sc.Path, "/"))
	if file == "" {
		return nil
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	return videoLintSource(sc.ID, jscompat.DecodeUTF8(raw))
}

// videoLintSource is videoLint on a board's source: one finding per code.
func videoLintSource(scene, src string) []VideoCheckIssue {
	var code strings.Builder
	for _, m := range videoScriptRe.FindAllStringSubmatch(src, -1) {
		if videoSrcRe.MatchString(m[1]) {
			continue
		}
		code.WriteString(m[2])
		code.WriteByte('\n')
	}
	out := []VideoCheckIssue{}
	for _, lint := range videoLints {
		if lint.re.MatchString(code.String()) {
			out = append(out, VideoCheckIssue{Scene: scene, Code: lint.code, Level: lint.level, Msg: locale.Pick(lint.en, lint.ru)})
		}
	}
	if videoGsapRe.MatchString(code.String()) && !strings.Contains(code.String(), "tkVideo.motion") {
		out = append(out, VideoCheckIssue{Scene: scene, Code: VideoCheckUnbound, Level: VideoCheckError,
			Msg: locale.Pick("GSAP outside tkVideo.motion runs on its own clock and does not follow the scene's time: build it in tkVideo.motion(fn)",
				"GSAP вне tkVideo.motion идёт по своим часам и не следует времени сцены: собирай его в tkVideo.motion(fn)")})
	}
	if m := videoOutsideRe.FindStringSubmatch(src); m != nil {
		out = append(out, VideoCheckIssue{Scene: scene, Code: VideoCheckExternal, Level: VideoCheckError,
			Msg: locale.Pick("A scene loads nothing from outside the project and the kit: "+m[1], "Сцена не грузит ничего извне проекта и кита: "+m[1])})
	}
	return out
}

// videoPastEnd: moments that start after the scene has ended.
func videoPastEnd(sc VideoScene) []VideoCheckIssue {
	out := []VideoCheckIssue{}
	for _, m := range sc.Moments {
		if m.At < sc.Dur {
			continue
		}
		at, block := m.At, m.Block
		out = append(out, VideoCheckIssue{Scene: sc.ID, At: &at, Block: &block, Code: VideoCheckPastEnd, Level: VideoCheckError,
			Msg: locale.Pick("The moment starts after the scene ends ("+videoSecondsText(m.At)+" of "+videoSecondsText(sc.Dur)+")",
				"Момент начинается после конца сцены ("+videoSecondsText(m.At)+" из "+videoSecondsText(sc.Dur)+")")})
	}
	return out
}

// videoSecondsText is ms as seconds for a person: 1.8 s.
func videoSecondsText(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64) + locale.Pick(" s", " с")
}

// videoAudit is what the runtime's audit answers.
type videoAudit struct {
	Code   string
	Block  *string
	Px     int
	Detail string
}

// videoCheckInChrome captures two moments twice to see they stay the same, whichever way they are reached, looks at the
// scene's last frame, then walks the scene in time and reads what the board reported.
func (l *Library) videoCheckInChrome(ctx context.Context, tab *deckTab, id string, sc VideoScene, f VideoFormat) ([]VideoCheckIssue, error) {
	end := max(sc.Dur-videoCheckBeforeEnd, 0)
	mid := sc.Dur / 2
	shots := map[int64][]byte{}
	// mid, end, then mid and end again after the start: a frame must not depend on the way it was reached.
	order := []int64{mid, end, 0, mid, end}
	var unstable *int64
	err := l.videoFrames(ctx, tab, id, sc, f, false, order, func(v videoShot) error {
		if v.I < 2 {
			shots[v.T] = v.Image
			return nil
		}
		if v.I > 2 && unstable == nil && !videoFramesClose(shots[v.T], v.Image) {
			at := v.T
			unstable = &at
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	fm := f
	out := []VideoCheckIssue{}
	if unstable != nil {
		out = append(out, VideoCheckIssue{Scene: sc.ID, Format: &fm, At: unstable, Code: VideoCheckUnstable, Level: VideoCheckError,
			Msg: locale.Pick("The same time draws a different frame each time: the board has motion that is not driven by the scene's time",
				"Одно и то же время рисует разные кадры: на доске есть движение не от времени сцены")})
	}
	minFont := 0
	if f == VideoPortrait {
		minFont = videoMinFont
	}
	raw, err := tab.eval(`(async () => { await window.tkVideo.seek(` + strconv.FormatInt(end, 10) + `); return window.tkVideo.audit({minFont: ` + strconv.Itoa(minFont) + `}); })()`)
	if err != nil {
		return nil, err
	}
	for _, a := range videoAudits(raw) {
		issue, ok := videoAuditIssue(a, sc, fm, end)
		if ok {
			out = append(out, issue)
		}
	}
	walked, err := videoWalk(ctx, tab, sc, fm)
	if err != nil {
		return nil, err
	}
	out = append(out, walked...)
	reported, err := videoReported(tab, sc, fm)
	if err != nil {
		return nil, err
	}
	return append(out, reported...), nil
}

// videoAuditIssue words a finding of the audit at the scene's end.
func videoAuditIssue(a videoAudit, sc VideoScene, f VideoFormat, at int64) (VideoCheckIssue, bool) {
	level := VideoCheckWarn
	switch a.Code {
	case VideoCheckOverflow, VideoCheckOffscreen, VideoCheckOverlap:
		level = VideoCheckError
	case VideoCheckSmallText:
	default:
		return VideoCheckIssue{}, false
	}
	return VideoCheckIssue{Scene: sc.ID, Format: &f, At: &at, Block: a.Block, Code: a.Code, Level: level, Msg: videoAuditMsg(a.Code, a.Px, a.Detail)}, true
}

// videoWalk seeks through the scene in time without capturing and reads the audit at every step: text that overflows at
// any moment is an error, a block that stays outside the frame for over a second is a warning (one on its way in is not).
func videoWalk(ctx context.Context, tab *deckTab, sc VideoScene, f VideoFormat) ([]VideoCheckIssue, error) {
	out := []VideoCheckIssue{}
	reported := map[string]bool{}
	outside := map[string]int{}
	for t := int64(0); t < sc.Dur; t += videoWalkStep {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		raw, err := tab.eval(`(async () => { await window.tkVideo.seek(` + strconv.FormatInt(t, 10) + `); return window.tkVideo.audit({}); })()`)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, a := range videoAudits(raw) {
			block := ""
			if a.Block != nil {
				block = *a.Block
			}
			at := t
			switch a.Code {
			case VideoCheckOverflow:
				if !reported[a.Code+block] {
					reported[a.Code+block] = true
					out = append(out, VideoCheckIssue{Scene: sc.ID, Format: &f, At: &at, Block: a.Block, Code: a.Code, Level: VideoCheckError,
						Msg: locale.Pick("Text overflows its box at this moment", "Текст не помещается в свой блок в этот момент")})
				}
			case VideoCheckOffscreen:
				seen[block] = true
				outside[block]++
				if outside[block] > videoWalkOutside && !reported[a.Code+block] {
					reported[a.Code+block] = true
					out = append(out, VideoCheckIssue{Scene: sc.ID, Format: &f, At: &at, Block: a.Block, Code: a.Code, Level: VideoCheckWarn,
						Msg: locale.Pick("A block stays outside the frame for over a second", "Блок больше секунды остаётся за кадром")})
				}
			}
		}
		for block := range outside {
			if !seen[block] {
				delete(outside, block)
			}
		}
	}
	return out, nil
}

// videoReported is what the board itself reported: failed engines, scripts and files, outside addresses, motion past the
// scene's end.
func videoReported(tab *deckTab, sc VideoScene, f VideoFormat) ([]VideoCheckIssue, error) {
	raw, err := tab.eval(`({problems: window.tkVideo.problems(), info: window.tkVideo.info()})`)
	if err != nil {
		return nil, err
	}
	m, _ := raw.(map[string]any)
	out := []VideoCheckIssue{}
	seen := map[string]bool{}
	list, _ := m["problems"].([]any)
	for _, item := range list {
		p, _ := item.(map[string]any)
		code, _ := p["code"].(string)
		msg, _ := p["msg"].(string)
		if code == "" || seen[code+msg] {
			continue
		}
		seen[code+msg] = true
		out = append(out, VideoCheckIssue{Scene: sc.ID, Format: &f, Code: code, Level: VideoCheckError, Msg: videoProblemMsg(code, msg)})
	}
	info, _ := m["info"].(map[string]any)
	if d, ok := info["motionDur"].(float64); ok && int64(d) > sc.Dur {
		at := sc.Dur
		out = append(out, VideoCheckIssue{Scene: sc.ID, Format: &f, At: &at, Code: VideoCheckMotionEnd, Level: VideoCheckWarn,
			Msg: locale.Pick("The motion runs to "+videoSecondsText(int64(d))+", the scene ends at "+videoSecondsText(sc.Dur),
				"Анимация идёт до "+videoSecondsText(int64(d))+", а сцена кончается на "+videoSecondsText(sc.Dur))})
	}
	return out, nil
}

func videoProblemMsg(code, msg string) string {
	switch code {
	case VideoCheckExternal:
		return locale.Pick("The board reached outside the project and the kit: ", "Доска обратилась за пределы проекта и кита: ") + msg
	case VideoCheckAsset:
		return locale.Pick("A file did not load: ", "Файл не загрузился: ") + msg
	case VideoCheckEngine:
		return locale.Pick("An engine failed: ", "Движок упал: ") + msg
	}
	return locale.Pick("A script failed: ", "Скрипт упал: ") + msg
}

// videoFramesClose: two pictures of one moment show the same thing. Chrome's raster leaves noise on the edges of text
// between two draws of the same page, so a byte compare would fail a healthy scene: what counts is pixels that differ
// visibly, and more than a few of them.
func videoFramesClose(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	ia, err := jpeg.Decode(bytes.NewReader(a))
	if err != nil {
		return false
	}
	ib, err := jpeg.Decode(bytes.NewReader(b))
	if err != nil || ia.Bounds() != ib.Bounds() {
		return false
	}
	r := ia.Bounds()
	differ := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			r1, g1, b1, _ := ia.At(x, y).RGBA()
			r2, g2, b2, _ := ib.At(x, y).RGBA()
			if videoChannelFar(r1, r2) || videoChannelFar(g1, g2) || videoChannelFar(b1, b2) {
				differ++
			}
		}
	}
	return float64(differ) <= videoFrameDifferShare*float64(r.Dx()*r.Dy())
}

func videoChannelFar(a, b uint32) bool {
	d := int64(a>>8) - int64(b>>8)
	return d > videoFrameDifferLevel || d < -videoFrameDifferLevel
}

// videoAudits reads the runtime's audit; what is not one of its findings is left out.
func videoAudits(raw any) []videoAudit {
	list, _ := raw.([]any)
	out := []videoAudit{}
	for _, item := range list {
		m, _ := item.(map[string]any)
		code, _ := m["code"].(string)
		if code == "" {
			continue
		}
		a := videoAudit{Code: code}
		if b, ok := m["block"].(string); ok {
			a.Block = &b
		}
		if px, ok := m["px"].(float64); ok {
			a.Px = int(px)
		}
		if d, ok := m["detail"].(string); ok {
			a.Detail = d
		}
		out = append(out, a)
	}
	return out
}

// videoAuditMsg words a finding of the runtime for a person.
func videoAuditMsg(code string, px int, detail string) string {
	switch code {
	case VideoCheckOverlap:
		return locale.Pick("Two tweens drive one property at one time, so the frame depends on how it was reached: ", "Два твина ведут одно свойство в одно время, и кадр зависит от пути к нему: ") + detail
	case VideoCheckOverflow:
		return locale.Pick("Text overflows its box at the scene's end", "Текст не помещается в свой блок к концу сцены")
	case VideoCheckOffscreen:
		return locale.Pick("A block is outside the frame at the scene's end", "Блок выходит за кадр к концу сцены")
	}
	n := strconv.Itoa(px)
	return locale.Pick("Text of "+n+" px in 9:16: under 28 px is hard to read on a phone", "Текст "+n+" px в 9:16: мельче 28 px на телефоне не прочесть")
}
