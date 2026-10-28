package design

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The MP4 of a recording: one picture of the board for every state the show went through (a slide at a step), laid out
// on the timeline of the montage, with the camera as a circle in a corner, the voice and the subtitles. Pictures come
// from the same headless browser the gallery uses; ffmpeg runs niced. The finished file is kept as a render of the
// recording, handed to the job, and published for guests as .media/published/video.mp4 with chapters.json.

func init() {
	RegisterDeckExporter(DeckExportMP4, DeckExporter{Needs: []string{"chrome", "ffmpeg"}, Ext: "mp4", Run: runDeckMP4})
}

const (
	// mp4Version changes when the look of the render does, so the renders kept by an older runtime are made again.
	mp4Version = 1
	mp4Width   = 1920
	mp4FPS     = 30
	// The camera circle, as a share of the video's width, with its border and its distance from the edge.
	mp4CamShare    = 0.104
	mp4CamBorder   = 4
	mp4CamMargin   = 36
	mp4SubWords    = 9
	mp4SubMinMs    = 700
	mp4FrameSettle = 1200 * time.Millisecond
	mp4StepSettle  = 350 * time.Millisecond
	mp4PageWait    = 90 * time.Second
	// Progress: the pictures are the first part of the job, the encode the rest.
	mp4FramesShare = 0.3
)

// ---- the frames: which state of the show is on screen when

// mp4Frame is a state of the show and how long it is on screen in the video, ms.
type mp4Frame struct {
	Page string
	Step int
	Ms   int64
}

// mp4Frames lays the states of the show (a slide, a step of it) on the timeline of the video: a state lasts from its
// event to the next one, counted in the kept pieces only. Neighbours in the same state are one frame.
func mp4Frames(slides []string, evs []DeckEvent, keep []deckSpan, durMs int64) []mp4Frame {
	type state struct {
		page string
		step int
		at   int64
	}
	var states []state
	cur := ""
	if len(slides) > 0 {
		cur = slides[0]
		states = append(states, state{cur, 0, 0})
	}
	for _, ev := range sortedEvents(evs) {
		at := ev.T
		if at > durMs {
			at = durMs
		}
		switch ev.K {
		case DeckEvSlide:
			if ev.Page != "" {
				cur = ev.Page
				states = append(states, state{cur, 0, at})
			}
		case DeckEvRetake:
			if ev.Page != "" {
				cur = ev.Page
			}
			if cur != "" {
				states = append(states, state{cur, 0, at})
			}
		case DeckEvStep:
			if cur != "" && ev.N != nil && *ev.N >= 0 {
				states = append(states, state{cur, *ev.N, at})
			}
		}
	}
	out := []mp4Frame{}
	for i, s := range states {
		end := durMs
		if i+1 < len(states) {
			end = states[i+1].at
		}
		ms := mapOut(keep, end) - mapOut(keep, s.at)
		if ms <= 0 {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Page == s.page && out[n-1].Step == s.step {
			out[n-1].Ms += ms
			continue
		}
		out = append(out, mp4Frame{s.page, s.step, ms})
	}
	return out
}

// mp4Chapters are the slides' starts in the video, a slide once, at its first frame.
func mp4Chapters(frames []mp4Frame, title func(page string) string) []DeckChapter {
	out := []DeckChapter{}
	seen := map[string]bool{}
	var at int64
	for _, f := range frames {
		if !seen[f.Page] {
			seen[f.Page] = true
			out = append(out, DeckChapter{Page: f.Page, Title: title(f.Page), T: at})
		}
		at += f.Ms
	}
	return out
}

// mp4Concat is the concat demuxer's list of the frames' pictures; the last picture is named again, without a
// duration, because the demuxer drops the duration of the last entry.
func mp4Concat(files []string, frames []mp4Frame) string {
	var b strings.Builder
	for i, f := range frames {
		fmt.Fprintf(&b, "file '%s'\nduration %.3f\n", files[i], float64(f.Ms)/1000)
	}
	if len(files) > 0 {
		fmt.Fprintf(&b, "file '%s'\n", files[len(files)-1])
	}
	return b.String()
}

// ---- subtitles

// mp4Sub is a line of subtitles: its words, and when it is on screen in the video, ms.
type mp4Sub struct {
	A, B int64
	Text string
}

// mp4Subs makes the subtitles of the montage out of the transcript: the words that stay, in lines of at most
// mp4SubWords, each on screen from where its first word is spoken to where its last one ends. A part's words share
// its time evenly (the recogniser gave no time to a word).
func mp4Subs(tr DeckTranscript, keep []deckSpan) []mp4Sub {
	type word struct {
		a, b int64
		w    string
	}
	out := []mp4Sub{}
	for _, seg := range tr.Segments {
		var words []word
		for _, p := range seg.Parts {
			if p.Kind == DeckPartPause {
				continue
			}
			a, b := mapOut(keep, p.T0), mapOut(keep, p.T1)
			if b <= a {
				continue
			}
			toks := strings.Fields(p.Text)
			for i, tok := range toks {
				n := int64(len(toks))
				words = append(words, word{a + (b-a)*int64(i)/n, a + (b-a)*int64(i+1)/n, tok})
			}
		}
		for i := 0; i < len(words); i += mp4SubWords {
			j := i + mp4SubWords
			if j > len(words) {
				j = len(words)
			}
			parts := make([]string, 0, j-i)
			for _, w := range words[i:j] {
				parts = append(parts, w.w)
			}
			a, b := words[i].a, words[j-1].b
			if b-a < mp4SubMinMs {
				b = a + mp4SubMinMs
			}
			out = append(out, mp4Sub{a, b, strings.Join(parts, " ")})
		}
	}
	// A line never runs into the next one.
	for i := 0; i+1 < len(out); i++ {
		if out[i].B > out[i+1].A {
			out[i].B = out[i+1].A
		}
	}
	kept := out[:0]
	for _, s := range out {
		if s.B > s.A {
			kept = append(kept, s)
		}
	}
	return kept
}

func assTime(ms int64) string {
	cs := ms / 10
	return fmt.Sprintf("%d:%02d:%02d.%02d", cs/360000, cs/6000%60, cs/100%60, cs%100)
}

func assText(s string) string {
	return strings.NewReplacer("{", "(", "}", ")", "\n", " ", "\\", "").Replace(s)
}

// mp4ASS is the subtitles as an Advanced SubStation file for libass: white on a dark box at the bottom centre.
func mp4ASS(w, h int, subs []mp4Sub) string {
	var b strings.Builder
	size := h / 22
	fmt.Fprintf(&b, "[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\nWrapStyle: 0\n\n", w, h)
	b.WriteString("[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, " +
		"Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	fmt.Fprintf(&b, "Style: Default,Arial,%d,&H00FFFFFF,&H000000FF,&H8C000000,&H8C000000,0,0,0,0,100,100,0,0,3,%d,0,2,%d,%d,%d,1\n\n",
		size, size/5, w/8, w/8, h/18)
	b.WriteString("[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
	for _, s := range subs {
		fmt.Fprintf(&b, "Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", assTime(s.A), assTime(s.B), assText(s.Text))
	}
	return b.String()
}

// ---- the filtergraph

// mp4Plan is everything the filtergraph depends on.
type mp4Plan struct {
	W, H  int
	Keep  []deckSpan
	Total int64 // ms, the length of the video
	// Cam is the corner of the camera circle ("br", "bl", "tr", "tl"); "" for no camera.
	Cam string
	// Subs is how the subtitles are drawn: "ass" (the subtitles filter, from subs.ass), "drawtext" (a filter for each
	// line of SubLines) or "" for none.
	Subs     string
	SubLines []mp4Sub
}

func secs(ms int64) string { return fmt.Sprintf("%.3f", float64(ms)/1000) }

const circleAlpha = "if(lte(pow(X-W/2,2)+pow(Y-H/2,2),pow(W/2,2)),255,0)"

// mp4CamBox is the circle's size and where its corner puts it.
func mp4CamBox(p mp4Plan) (d, x, y int) {
	d = int(math.Round(float64(p.W)*mp4CamShare/2)) * 2
	x, y = p.W-d-mp4CamMargin, p.H-d-mp4CamMargin
	switch p.Cam {
	case "bl":
		x = mp4CamMargin
	case "tr":
		y = mp4CamMargin
	case "tl":
		x, y = mp4CamMargin, mp4CamMargin
	}
	return d, x, y
}

// mp4Filter is the -filter_complex of the render. Input 0 is the recording (audio, and the camera when Cam is set),
// input 1 the frames' pictures. It ends in [v] and [a]. The pieces of Keep are cut out of the recording and joined;
// the camera is a circle with a white border, mirrored as it was on screen.
func mp4Filter(p mp4Plan) string {
	var f []string
	f = append(f, fmt.Sprintf("[1:v]fps=%d,scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2:color=white,setsar=1,format=yuv420p[base]",
		mp4FPS, p.W, p.H, p.W, p.H))
	var join strings.Builder
	for i, k := range p.Keep {
		if p.Cam != "" {
			f = append(f, fmt.Sprintf("[0:v]trim=start=%s:end=%s,setpts=PTS-STARTPTS[c%d]", secs(k.A), secs(k.B), i))
		}
		f = append(f, fmt.Sprintf("[0:a]atrim=start=%s:end=%s,asetpts=PTS-STARTPTS[a%d]", secs(k.A), secs(k.B), i))
		if p.Cam != "" {
			fmt.Fprintf(&join, "[c%d]", i)
		}
		fmt.Fprintf(&join, "[a%d]", i)
	}
	cur := "base"
	if p.Cam != "" {
		f = append(f, fmt.Sprintf("%sconcat=n=%d:v=1:a=1[cam][a]", join.String(), len(p.Keep)))
		d, x, y := mp4CamBox(p)
		ring := d + 2*mp4CamBorder
		f = append(f,
			fmt.Sprintf("[cam]fps=%d,crop='min(iw,ih)':'min(iw,ih)',scale=%d:%d,hflip,format=yuva420p,geq=lum='lum(X,Y)':cb='cb(X,Y)':cr='cr(X,Y)':a='%s'[circ]",
				mp4FPS, d, d, circleAlpha),
			fmt.Sprintf("color=c=white:s=%dx%d:r=%d:d=%s,format=yuva420p,geq=lum=255:cb=128:cr=128:a='%s'[ring]",
				ring, ring, mp4FPS, secs(p.Total), circleAlpha),
			fmt.Sprintf("[%s][ring]overlay=x=%d:y=%d[v1]", cur, x-mp4CamBorder, y-mp4CamBorder),
			fmt.Sprintf("[v1][circ]overlay=x=%d:y=%d[v2]", x, y))
		cur = "v2"
	} else {
		f = append(f, fmt.Sprintf("%sconcat=n=%d:v=0:a=1[a]", join.String(), len(p.Keep)))
	}
	switch p.Subs {
	case "ass":
		f = append(f, fmt.Sprintf("[%s]subtitles=subs.ass[v]", cur))
	case "drawtext":
		size := p.H / 22
		for i, s := range p.SubLines {
			out := "t" + strconv.Itoa(i)
			if i == len(p.SubLines)-1 {
				out = "v"
			}
			f = append(f, fmt.Sprintf("[%s]drawtext=textfile=sub%d.txt:fontsize=%d:fontcolor=white:box=1:boxcolor=black@0.55:boxborderw=%d:x=(w-text_w)/2:y=h-text_h-%d:enable='between(t,%s,%s)'[%s]",
				cur, i, size, size/3, p.H/14, secs(s.A), secs(s.B), out))
			cur = out
		}
		if len(p.SubLines) == 0 {
			f = append(f, fmt.Sprintf("[%s]null[v]", cur))
		}
	default:
		f = append(f, fmt.Sprintf("[%s]null[v]", cur))
	}
	return strings.Join(f, ";")
}

// mp4Args is the ffmpeg command line of the render (run in the folder of subs.ass and the text files).
func mp4Args(in, demuxer, frames, graph, out string, total int64) []string {
	return []string{"-hide_banner", "-nostdin", "-y", "-loglevel", "error", "-progress", "pipe:1", "-nostats",
		"-protocol_whitelist", "file", "-f", demuxer, "-i", in,
		"-protocol_whitelist", "file", "-f", "concat", "-safe", "0", "-i", frames,
		"-filter_complex", graph, "-map", "[v]", "-map", "[a]",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "23", "-r", strconv.Itoa(mp4FPS), "-pix_fmt", "yuv420p", "-threads", "2",
		"-c:a", "aac", "-b:a", "128k", "-movflags", "+faststart", "-t", secs(total), out}
}

// ---- the pictures

var stepScript = `(function(n){document.querySelectorAll('[data-step]').forEach(function(el){` +
	`var s=Number(el.getAttribute('data-step'));el.style.visibility=s>n?'hidden':'';});})(%d)`

const boardReady = `document.fonts.ready.then(function(){return !!document.body&&(document.body.innerText.trim().length>0||` +
	`!!document.querySelector('img,svg,canvas'));})`

// captureBoard opens the board once at its size and takes a picture of it for each of the steps.
func (l *Library) captureBoard(ctx context.Context, br *browser, url string, w, h int, scale float64, steps []int, files []string) error {
	t, err := br.cdp.send("Target.createTarget", map[string]any{"url": "about:blank"}, "")
	if err != nil {
		return err
	}
	targetID := t["targetId"]
	defer func() { _, _ = br.cdp.send("Target.closeTarget", map[string]any{"targetId": targetID}, "") }()
	a, err := br.cdp.send("Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, "")
	if err != nil {
		return err
	}
	session, _ := a["sessionId"].(string)
	for _, d := range []string{"Page", "Runtime"} {
		if _, err := br.cdp.send(d+".enable", nil, session); err != nil {
			return err
		}
	}
	loaded := make(chan struct{})
	var once sync.Once
	stop := br.cdp.on(func(ev cdpEvent) {
		if ev.SessionID == session && ev.Method == "Page.loadEventFired" {
			once.Do(func() { close(loaded) })
		}
	})
	defer stop()
	if _, err := br.cdp.send("Emulation.setDeviceMetricsOverride", map[string]any{"width": w, "height": h, "deviceScaleFactor": scale, "mobile": false}, session); err != nil {
		return err
	}
	if _, err := br.cdp.send("Page.navigate", map[string]any{"url": url}, session); err != nil {
		return err
	}
	select {
	case <-loaded:
	case <-time.After(checkLoadTimeout):
	case <-ctx.Done():
		return ctx.Err()
	}
	sleep := func(d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := sleep(mp4FrameSettle); err != nil {
		return err
	}
	evaluate := func(expr string, wait bool) (map[string]any, error) {
		return br.cdp.send("Runtime.evaluate", map[string]any{"expression": expr, "awaitPromise": wait, "returnByValue": true}, session)
	}
	if _, err := evaluate(dropBar, false); err != nil {
		return err
	}
	ready := false
	for i := 0; i < 6 && !ready; i++ {
		if i > 0 {
			if err := sleep(time.Second); err != nil {
				return err
			}
		}
		res, err := evaluate(boardReady, true)
		if err != nil {
			return err
		}
		if r, ok := res["result"].(map[string]any); ok {
			ready, _ = r["value"].(bool)
		}
	}
	if !ready {
		return errors.New(locale.Pick("the slide is empty", "слайд пустой"))
	}
	for i, step := range steps {
		if _, err := evaluate(fmt.Sprintf(stepScript, step), false); err != nil {
			return err
		}
		if err := sleep(mp4StepSettle); err != nil {
			return err
		}
		shot, err := br.cdp.send("Page.captureScreenshot", map[string]any{"format": "png"}, session)
		if err != nil {
			return err
		}
		data, _ := shot["data"].(string)
		raw, err := base64.StdEncoding.DecodeString(data)
		if err != nil || len(raw) == 0 {
			return errors.New(locale.Pick("no picture of the slide", "нет картинки слайда"))
		}
		if err := os.WriteFile(files[i], raw, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// drawBoards takes the pictures of the boards at the steps asked for: want maps a page to its steps, ascending; files
// are named for page and step. It reports how many pictures are done.
func (l *Library) drawBoards(ctx context.Context, id string, pages map[string]Page, want map[string][]int, dir string, width int, done func(n int)) (map[string]string, error) {
	bin := l.findChrome()
	if bin == "" {
		return nil, errors.New(locale.Pick("no browser to draw the slides", "нет браузера, чтобы нарисовать слайды"))
	}
	base, err := libraryBase(l)
	if err != nil {
		return nil, err
	}
	order := make([]string, 0, len(want))
	for p := range want {
		order = append(order, p)
	}
	sort.Strings(order)
	files := map[string]string{}
	var count int
	for _, pid := range order {
		page, ok := pages[pid]
		if !ok {
			return nil, errors.New(locale.Pick("the slide is gone from the deck", "слайда больше нет в презентации"))
		}
		w, h := 1920, 1080
		if page.W != nil && page.H != nil && *page.W > 0 && *page.H > 0 {
			w, h = *page.W, *page.H
		}
		scale := math.Min(3, math.Max(0.5, float64(width)/float64(w)))
		steps := want[pid]
		names := make([]string, len(steps))
		for i, s := range steps {
			names[i] = filepath.Join(dir, fmt.Sprintf("%s-%d.png", pid, s))
			files[pid+"/"+strconv.Itoa(s)] = names[i]
		}
		err := func() error {
			s := l.thumbs()
			s.takeSlot("deck-mp4", false)
			defer s.freeSlot()
			br, err := l.thumbBrowser(s, bin)
			if err != nil {
				return err
			}
			cctx, cancel := context.WithTimeout(ctx, mp4PageWait)
			defer cancel()
			return l.captureBoard(cctx, br, base+BoardHref(id, page.File), w, h, scale, steps, names)
		}()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", page.Title, err)
		}
		count += len(steps)
		done(count)
	}
	return files, nil
}

// ---- the render

// runDeckMP4 is the mp4 exporter: the recording run.Rec (the newest finished one when none is named) rendered as its
// montage says.
func runDeckMP4(ctx context.Context, run DeckExportRun) error {
	l, id := run.L, run.Artifact
	progress := func(p float64) {
		if run.Progress != nil {
			run.Progress(math.Min(1, math.Max(0, p)))
		}
	}
	bin := DeckFFmpeg()
	if bin == "" {
		return errors.New(locale.Pick("ffmpeg is not installed on this machine", "на этой машине нет ffmpeg"))
	}
	rec := run.Rec
	if rec == "" {
		for _, info := range l.DeckRecordings(id) {
			if info.State == DeckRecDone {
				rec = info.ID
				break
			}
		}
	}
	if rec == "" {
		return errors.New(locale.Pick("there is no recording to make a video of", "нет записи, из которой сделать видео"))
	}
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		return errors.New(locale.Pick("no such recording", "нет такой записи"))
	}
	if meta.State != DeckRecDone {
		return errors.New(locale.Pick("the recording is not finished", "запись ещё не закончена"))
	}
	dir := l.recDir(id, rec)
	if fileSize(filepath.Join(dir, recMediaFile)) == 0 {
		return errors.New(locale.Pick("the recording has no sound or picture", "в записи нет ни звука, ни картинки"))
	}
	if _, have := l.readRecTranscript(id, rec); !have {
		if err := l.buildRecTranscript(ctx, id, rec); err != nil {
			return err
		}
	}
	tr, _ := l.readRecTranscript(id, rec)
	edit := l.readRecEdit(id, rec)
	evs := readRecEvents(filepath.Join(dir, recEventsFile))
	keep, _ := deckKeep(meta.DurationMs, meta.Takes, tr, edit)
	if len(keep) == 0 {
		return errors.New(locale.Pick("the montage leaves nothing of the recording", "после монтажа от записи ничего не остаётся"))
	}
	total := spansLen(keep)
	frames := mp4Frames(meta.Slides, evs, keep, meta.DurationMs)
	if len(frames) == 0 {
		return errors.New(locale.Pick("the recording has no slides", "в записи нет слайдов"))
	}
	model := l.ReadModel(id)
	if model == nil {
		return errors.New(locale.Pick("no such deck", "нет такой презентации"))
	}
	pages := map[string]Page{}
	for _, p := range model.Pages {
		pages[p.ID] = p
	}
	// The video's size is the first slide's shape at mp4Width.
	w, h := mp4Width, mp4Width*9/16
	if p, ok := pages[frames[0].Page]; ok && p.W != nil && p.H != nil && *p.W > 0 && *p.H > 0 {
		h = int(math.Round(float64(mp4Width)*float64(*p.H)/float64(*p.W)/2)) * 2
	}
	h &^= 1

	cam := ""
	if meta.Camera && edit.Cam != "off" {
		cam = edit.Cam
	}
	plan := mp4Plan{W: w, H: h, Keep: keep, Total: total, Cam: cam}
	var subs []mp4Sub
	if edit.Subs {
		subs = mp4Subs(tr, keep)
		switch {
		case len(subs) == 0:
		case ffmpegHas(bin, "subtitles"):
			plan.Subs = "ass"
		case ffmpegHas(bin, "drawtext"):
			plan.Subs, plan.SubLines = "drawtext", subs
		default:
			return errors.New(locale.Pick("this ffmpeg cannot draw subtitles: turn them off in the montage", "этот ffmpeg не умеет рисовать субтитры: выключите их в монтаже"))
		}
	}

	hash := mp4Hash(l, id, rec, meta, tr, edit, plan)
	renders := filepath.Join(dir, "renders")
	cached := filepath.Join(renders, hash+".mp4")
	chapters := mp4Chapters(frames, func(page string) string { return pages[page].Title })
	if fileSize(cached) == 0 {
		if err := os.MkdirAll(renders, 0o755); err != nil {
			return err
		}
		work, err := os.MkdirTemp(renders, hash+".work-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(work)
		want := map[string][]int{}
		for _, f := range frames {
			if !containsInt(want[f.Page], f.Step) {
				want[f.Page] = append(want[f.Page], f.Step)
			}
		}
		for _, s := range want {
			sort.Ints(s)
		}
		var totalPics int
		for _, s := range want {
			totalPics += len(s)
		}
		pics, err := l.drawBoards(ctx, id, pages, want, work, w, func(n int) { progress(mp4FramesShare * float64(n) / float64(totalPics)) })
		if err != nil {
			return err
		}
		names := make([]string, len(frames))
		for i, f := range frames {
			names[i] = pics[f.Page+"/"+strconv.Itoa(f.Step)]
		}
		if err := os.WriteFile(filepath.Join(work, "frames.txt"), []byte(mp4Concat(names, frames)), 0o644); err != nil {
			return err
		}
		switch plan.Subs {
		case "ass":
			if err := os.WriteFile(filepath.Join(work, "subs.ass"), []byte(mp4ASS(w, h, subs)), 0o644); err != nil {
				return err
			}
		case "drawtext":
			for i, s := range subs {
				if err := os.WriteFile(filepath.Join(work, fmt.Sprintf("sub%d.txt", i)), []byte(s.Text), 0o644); err != nil {
					return err
				}
			}
		}
		tmp := filepath.Join(work, "out.mp4")
		if err := runFFmpeg(ctx, bin, work, mp4Args(filepath.Join(dir, recMediaFile), recDemuxer(meta.Mime), "frames.txt", mp4Filter(plan), tmp, total), total,
			func(p float64) { progress(mp4FramesShare + (1-mp4FramesShare)*p*0.98) }); err != nil {
			return err
		}
		if err := os.Rename(tmp, cached); err != nil {
			return err
		}
		dropOldRenders(renders, hash+".mp4")
	}

	if run.Out != "" {
		if err := publishCopy(cached, run.Out); err != nil {
			return err
		}
	}
	pub := filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckPublishedDir))
	if err := os.MkdirAll(pub, 0o755); err != nil {
		return err
	}
	if err := publishCopy(cached, filepath.Join(pub, "video.mp4")); err != nil {
		return err
	}
	raw, err := json.Marshal(chapters)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(pub, "chapters.json"), raw); err != nil {
		return err
	}
	progress(1)
	return nil
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// mp4Hash names a render by what it was made of: the recording, its transcript, the montage, the look of the video
// and the deck's boards. The same name is the same video.
func mp4Hash(l *Library, id, rec string, meta DeckRecMeta, tr DeckTranscript, edit DeckEdit, plan mp4Plan) string {
	h := sha1.New()
	trRaw, _ := json.Marshal(tr)
	editRaw, _ := json.Marshal(edit)
	fmt.Fprintf(h, "v%d|%s|%s|%d|%s|%d|%d|%s|%s|%s|%d", mp4Version, id, rec, meta.DurationMs, meta.State, plan.W, plan.H, plan.Cam, plan.Subs,
		trRaw, deckBoardsRev(l, id))
	_, _ = h.Write(editRaw)
	return hex.EncodeToString(h.Sum(nil))[:20]
}

// deckBoardsRev is the newest mtime (ms) of the deck's boards, their meta and the theme: when one is edited, the
// renders made of the old ones are stale.
func deckBoardsRev(l *Library, id string) int64 {
	var rev int64
	note := func(st os.FileInfo) {
		if st.ModTime().UnixMilli() > rev {
			rev = st.ModTime().UnixMilli()
		}
	}
	for _, root := range []string{l.ProjectDir(id), filepath.Join(l.MetaDir(id), MetaPagesDirname)} {
		_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() && p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			if st, err := d.Info(); err == nil && st.Mode().IsRegular() {
				note(st)
			}
			return nil
		})
	}
	return rev
}

func dropOldRenders(dir, keep string) {
	names, _ := readNames(dir)
	for _, n := range names {
		if n != keep && strings.HasSuffix(n, ".mp4") {
			_ = os.Remove(filepath.Join(dir, n))
		}
	}
}

// publishCopy puts src at dst: a hard link where the folders share a disk, a copy otherwise; written beside dst and
// renamed over it, so a reader of dst never sees half a file.
func publishCopy(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(dst), "."+filepath.Base(dst)+"."+randomHex(6)+".tmp")
	defer os.Remove(tmp)
	if err := os.Link(src, tmp); err != nil {
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, in)
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
	}
	return os.Rename(tmp, dst)
}

// runFFmpeg runs ffmpeg niced in dir and reports the progress (0..1 of total ms) it prints on -progress. A failure
// carries the last of what ffmpeg said.
func runFFmpeg(ctx context.Context, bin, dir string, args []string, total int64, progress func(float64)) error {
	cmd := recNice(ctx, bin, args...)
	cmd.Dir = dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var tail tailBuffer
	cmd.Stderr = &tail
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "out_time_us="); ok {
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && total > 0 {
				progress(math.Min(1, float64(us)/1000/float64(total)))
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		msg := strings.TrimSpace(tail.String())
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(locale.Pick("ffmpeg failed: ", "ffmpeg не справился: ") + msg)
	}
	return nil
}

// tailBuffer keeps the last 2 KB written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2048 {
		t.buf = t.buf[len(t.buf)-2048:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
