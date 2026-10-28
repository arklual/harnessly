package design

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// The montage of a recording: what was said (the transcript, built from the speech recogniser's finals in events.jsonl),
// which pieces the montage drops (pauses found by ffmpeg, fillers found in the text, the earlier takes of a retaken
// slide) and the list of pieces that stay. Every time here is ms on the recording's own timeline (av.webm).

const (
	// Silence the montage may cut: ffmpeg reports silences of at least recSilenceMinSec; of each one the montage keeps
	// recSilencePadMs at both ends, so a pause is shortened, never removed, and a cut shorter than recCutMinMs is not
	// worth the click.
	recSilenceNoise  = "-35dB"
	recSilenceMinSec = "1.0"
	recSilencePadMs  = 250
	recCutMinMs      = 400
	// A kept piece shorter than this is a glitch of the cut, not content.
	recKeepMinMs = 80
	// A final with no usable times is given at least this long, plus recWordMs a word.
	recSegMinMs = 500
	recWordMs   = 300
)

// deckSpan is a stretch of the recording, [A, B) in ms.
type deckSpan struct{ A, B int64 }

func (s deckSpan) len() int64 { return s.B - s.A }

func spansLen(spans []deckSpan) int64 {
	var n int64
	for _, s := range spans {
		n += s.len()
	}
	return n
}

// spansMinus takes cut out of spans. Both lists are in any order; the result is sorted, with no empty piece.
func spansMinus(spans, cut []deckSpan) []deckSpan {
	cuts := append([]deckSpan(nil), cut...)
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].A < cuts[j].A })
	out := []deckSpan{}
	for _, s := range spans {
		from := s.A
		for _, c := range cuts {
			if c.B <= from || c.A >= s.B {
				continue
			}
			if c.A > from {
				out = append(out, deckSpan{from, c.A})
			}
			if c.B > from {
				from = c.B
			}
			if from >= s.B {
				break
			}
		}
		if from < s.B {
			out = append(out, deckSpan{from, s.B})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].A < out[j].A })
	return out
}

// ---- takes

// deckTakes reads the takes of every slide from the events: a slide event closes the piece being said and opens one on
// its slide, a retake event closes it and opens the next take of the slide. A slide visited again without a retake
// goes on with its take (the pieces of one take share its n). slides are the deck's pages in order; the recording
// starts on the first one.
func deckTakes(slides []string, evs []DeckEvent, durMs int64) []DeckTake {
	out := []DeckTake{}
	counts := map[string]int{}
	cur, start := "", int64(0)
	if len(slides) > 0 {
		cur = slides[0]
		counts[cur] = 1
	}
	shut := func(at int64) {
		if cur != "" && at > start {
			out = append(out, DeckTake{Page: cur, N: counts[cur], StartMs: start, EndMs: at})
		}
	}
	for _, ev := range sortedEvents(evs) {
		switch ev.K {
		case DeckEvSlide:
			if ev.Page == "" {
				continue
			}
			shut(ev.T)
			cur, start = ev.Page, ev.T
			if counts[cur] == 0 {
				counts[cur] = 1
			}
		case DeckEvRetake:
			page := ev.Page
			if page == "" {
				page = cur
			}
			if page == "" {
				continue
			}
			shut(ev.T)
			cur, start = page, ev.T
			counts[cur]++
		}
	}
	if durMs < start {
		durMs = start
	}
	shut(durMs)
	return out
}

func sortedEvents(evs []DeckEvent) []DeckEvent {
	out := append([]DeckEvent(nil), evs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

// supersededSpans are the pieces of takes that a later take of their slide replaced: the last take wins.
func supersededSpans(takes []DeckTake) []deckSpan {
	last := map[string]int{}
	for _, t := range takes {
		if t.N > last[t.Page] {
			last[t.Page] = t.N
		}
	}
	out := []deckSpan{}
	for _, t := range takes {
		if t.N < last[t.Page] {
			out = append(out, deckSpan{t.StartMs, t.EndMs})
		}
	}
	return out
}

// retakenSlides are the 1-based numbers of the slides (in the recording's order) that were taken again.
func retakenSlides(slides []string, takes []DeckTake) []int {
	last := map[string]int{}
	for _, t := range takes {
		if t.N > last[t.Page] {
			last[t.Page] = t.N
		}
	}
	out := []int{}
	for i, p := range slides {
		if last[p] > 1 {
			out = append(out, i+1)
		}
	}
	return out
}

// ---- the cut list

// deckCutSpans are the pieces the montage takes out because of the edit: the pauses and fillers of the transcript that
// the person did not keep (none when the cut is off).
func deckCutSpans(tr DeckTranscript, edit DeckEdit) []deckSpan {
	out := []deckSpan{}
	if !edit.Cut {
		return out
	}
	kept := map[string]bool{}
	for _, k := range edit.Kept {
		kept[k] = true
	}
	for si, seg := range tr.Segments {
		for pi, p := range seg.Parts {
			if p.Cut && !kept[strconv.Itoa(si)+"."+strconv.Itoa(pi)] && p.T1 > p.T0 {
				out = append(out, deckSpan{p.T0, p.T1})
			}
		}
	}
	return out
}

// deckKeep is the list of pieces of the recording that make the video, in order: the whole recording without the
// earlier takes of retaken slides (always) and without the cut pauses and fillers (when the edit cuts). cutMs is what
// the edit's cut took out, the number the montage shows.
func deckKeep(durMs int64, takes []DeckTake, tr DeckTranscript, edit DeckEdit) (keep []deckSpan, cutMs int64) {
	whole := []deckSpan{}
	if durMs > 0 {
		whole = []deckSpan{{0, durMs}}
	}
	afterRetake := spansMinus(whole, supersededSpans(takes))
	final := spansMinus(afterRetake, deckCutSpans(tr, edit))
	out := final[:0:0]
	for _, s := range final {
		if s.len() >= recKeepMinMs {
			out = append(out, s)
		}
	}
	return out, spansLen(afterRetake) - spansLen(final)
}

// mapOut is where a moment of the recording lands in the video: the time of the kept pieces before it. A moment in a
// dropped piece lands where the piece was.
func mapOut(keep []deckSpan, t int64) int64 {
	var out int64
	for _, s := range keep {
		switch {
		case t >= s.B:
			out += s.len()
		case t > s.A:
			return out + t - s.A
		default:
			return out
		}
	}
	return out
}

// ---- the transcript

var (
	silenceStartRe = regexp.MustCompile(`silence_start:\s*(-?[0-9.]+)`)
	silenceEndRe   = regexp.MustCompile(`silence_end:\s*(-?[0-9.]+)`)
)

// parseSilences reads the silences from ffmpeg's silencedetect output; one with no end runs to the recording's end.
func parseSilences(out string, durMs int64) []deckSpan {
	secs := func(s string) int64 {
		f, _ := strconv.ParseFloat(s, 64)
		ms := int64(f*1000 + 0.5)
		if ms < 0 {
			ms = 0
		}
		return ms
	}
	spans := []deckSpan{}
	open := int64(-1)
	for _, ln := range strings.Split(out, "\n") {
		if m := silenceStartRe.FindStringSubmatch(ln); m != nil {
			open = secs(m[1])
		} else if m := silenceEndRe.FindStringSubmatch(ln); m != nil && open >= 0 {
			if end := secs(m[1]); end > open {
				spans = append(spans, deckSpan{open, end})
			}
			open = -1
		}
	}
	if open >= 0 && durMs > open {
		spans = append(spans, deckSpan{open, durMs})
	}
	return spans
}

// The words the montage takes for fillers, lower case, without the punctuation around them. "как бы" is two words.
var deckFillers = map[string]bool{
	"э": true, "ээ": true, "эээ": true, "э-э": true, "э-э-э": true, "эм": true, "эмм": true, "мм": true, "ммм": true,
	"м-м": true, "ну": true, "вот": true, "короче": true, "типа": true,
	"um": true, "umm": true, "uh": true, "uhh": true, "er": true, "erm": true,
}

func fillerKey(tok string) string {
	k := strings.TrimFunc(strings.ToLower(tok), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	return strings.ReplaceAll(k, "ё", "е")
}

// splitSpeech cuts the text of one final into words and fillers. The recogniser gives the segment's times, not the
// words', so a part's time is its share of the segment by the length of its words: an estimate, good to a few hundred
// ms, which is why a cut of a filler keeps a little of the sound around it only by luck.
func splitSpeech(text string, t0, t1 int64) []DeckPart {
	toks := strings.Fields(text)
	if len(toks) == 0 {
		return []DeckPart{}
	}
	filler := make([]bool, len(toks))
	for i, tok := range toks {
		k := fillerKey(tok)
		if deckFillers[k] {
			filler[i] = true
		}
		if k == "как" && i+1 < len(toks) && fillerKey(toks[i+1]) == "бы" {
			filler[i], filler[i+1] = true, true
		}
	}
	weight := make([]int64, len(toks))
	var total int64
	for i, tok := range toks {
		w := int64(len([]rune(tok)))
		if w < 2 {
			w = 2
		}
		weight[i] = w + 1
		total += weight[i]
	}
	span := t1 - t0
	if span < 0 {
		span = 0
	}
	parts := []DeckPart{}
	var acc int64
	at := func(w int64) int64 { return t0 + span*w/total }
	for i := 0; i < len(toks); {
		j := i
		var w int64
		for j < len(toks) && filler[j] == filler[i] {
			w += weight[j]
			j++
		}
		kind := DeckPartWord
		if filler[i] {
			kind = DeckPartFiller
		}
		parts = append(parts, DeckPart{Text: strings.Join(toks[i:j], " "), Cut: filler[i], Kind: kind, T0: at(acc), T1: at(acc + w)})
		acc += w
		i = j
	}
	parts[len(parts)-1].T1 = t1
	return parts
}

func pauseText(ms int64) string {
	s := fmt.Sprintf("%.1f", float64(ms)/1000)
	return locale.Pick("pause "+s+" s", "пауза "+strings.Replace(s, ".", ",", 1)+" с")
}

// buildTranscript makes the transcript of a recording: a segment for every final of the speech recogniser (an asr
// event) on the slide it began on, a pause-only segment for every silence that falls between segments, without the
// pieces of earlier takes of retaken slides. slides are the deck's pages in order; silences may be nil (no ffmpeg).
func buildTranscript(slides []string, evs []DeckEvent, takes []DeckTake, silences []deckSpan, durMs int64) DeckTranscript {
	evs = sortedEvents(evs)
	pageAt := func(t int64) string {
		page := ""
		if len(slides) > 0 {
			page = slides[0]
		}
		for _, ev := range evs {
			if ev.T > t {
				break
			}
			if (ev.K == DeckEvSlide || ev.K == DeckEvRetake) && ev.Page != "" {
				page = ev.Page
			}
		}
		return page
	}
	segs := []DeckSegment{}
	var prevEnd int64
	for _, ev := range evs {
		if ev.K != DeckEvASR {
			continue
		}
		text := strings.TrimSpace(ev.Text)
		if text == "" {
			continue
		}
		t0, t1 := ev.T, ev.T
		if ev.T0 != nil && *ev.T0 >= 0 && *ev.T0 <= ev.T {
			t0 = *ev.T0
		}
		if t0 < prevEnd {
			t0 = prevEnd
		}
		if t1 <= t0 {
			t1 = t0 + recSegMinMs + recWordMs*int64(len(strings.Fields(text)))
		}
		prevEnd = t1
		segs = append(segs, DeckSegment{T0: t0, T1: t1, Page: pageAt(t0), Parts: splitSpeech(text, t0, t1)})
	}
	// Silences count only outside the speech: inside a final the recogniser says somebody is talking.
	speech := make([]deckSpan, len(segs))
	for i, s := range segs {
		speech[i] = deckSpan{s.T0, s.T1}
	}
	for _, gap := range spansMinus(silences, speech) {
		cut := deckSpan{gap.A + recSilencePadMs, gap.B - recSilencePadMs}
		if gap.A == 0 {
			cut.A = 0
		}
		if gap.B >= durMs && durMs > 0 {
			cut.B = gap.B
		}
		if cut.len() < recCutMinMs {
			continue
		}
		segs = append(segs, DeckSegment{T0: cut.A, T1: cut.B, Page: pageAt(cut.A),
			Parts: []DeckPart{{Text: pauseText(cut.len()), Cut: true, Kind: DeckPartPause, T0: cut.A, T1: cut.B}}})
	}
	sort.SliceStable(segs, func(i, j int) bool { return segs[i].T0 < segs[j].T0 })
	// What was said in a take that a retake replaced is not in the video, so it is not in the montage's text.
	old := supersededSpans(takes)
	out := []DeckSegment{}
	for _, s := range segs {
		mid := (s.T0 + s.T1) / 2
		dropped := false
		for _, o := range old {
			dropped = dropped || (mid >= o.A && mid < o.B)
		}
		if !dropped {
			out = append(out, s)
		}
	}
	return DeckTranscript{Segments: out}
}

// ---- reading and writing a recording's files

func (l *Library) recDir(id, rec string) string {
	return filepath.Join(l.ArtifactDir(id), filepath.FromSlash(DeckRecDir), rec)
}

func readRecEvents(file string) []DeckEvent {
	f, err := os.Open(file)
	if err != nil {
		return []DeckEvent{}
	}
	defer f.Close()
	out := []DeckEvent{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var ev DeckEvent
		if json.Unmarshal(sc.Bytes(), &ev) == nil && ev.K != "" {
			out = append(out, ev)
		}
	}
	return out
}

var (
	ffmpegFiltersOnce sync.Map // ffmpeg path -> map[string]bool of its filters
)

// ffmpegHas says whether the ffmpeg at bin has the filter (a build without libass has no "subtitles").
func ffmpegHas(bin, filter string) bool {
	if v, ok := ffmpegFiltersOnce.Load(bin); ok {
		return v.(map[string]bool)[filter]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-hide_banner", "-filters").Output()
	have := map[string]bool{}
	if err == nil {
		for _, ln := range strings.Split(string(out), "\n") {
			if f := strings.Fields(ln); len(f) >= 2 {
				have[f[1]] = true
			}
		}
		ffmpegFiltersOnce.Store(bin, have)
	}
	return have[filter]
}

// recNice is a command niced the way the machine's heavy work is; without the nice binary it runs as it is.
func recNice(ctx context.Context, name string, args ...string) *exec.Cmd {
	if n, err := exec.LookPath("nice"); err == nil {
		return exec.CommandContext(ctx, n, append([]string{"-n", "15", name}, args...)...)
	}
	return exec.CommandContext(ctx, name, args...)
}

// DeckFFmpeg is the ffmpeg the montage runs: RUNTIME_DESIGN_FFMPEG, else the one on PATH; "" when there is none.
func DeckFFmpeg() string {
	if p := os.Getenv("RUNTIME_DESIGN_FFMPEG"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	p, _ := exec.LookPath("ffmpeg")
	return p
}

var recBuilding sync.Map

// startTranscript builds the transcript of a finished recording behind the caller, once at a time per recording.
func (l *Library) startTranscript(id, rec string) {
	key := l.Root() + "|" + id + "|" + rec
	if _, busy := recBuilding.LoadOrStore(key, true); busy {
		return
	}
	go func() {
		defer recBuilding.Delete(key)
		lowerPriority()
		_ = l.buildRecTranscript(context.Background(), id, rec)
	}()
}

// buildRecTranscript finds the silences of av.webm (when there is ffmpeg), reads the events and writes
// transcript.json.
func (l *Library) buildRecTranscript(ctx context.Context, id, rec string) error {
	meta, err := l.readRecMeta(id, rec)
	if err != nil {
		return err
	}
	dir := l.recDir(id, rec)
	var silences []deckSpan
	if bin := DeckFFmpeg(); bin != "" && fileSize(filepath.Join(dir, recMediaFile)) > 0 {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		cmd := recNice(cctx, bin, "-hide_banner", "-nostdin", "-protocol_whitelist", "file", "-f", recDemuxer(meta.Mime), "-i", filepath.Join(dir, recMediaFile), "-vn",
			"-af", "silencedetect=noise="+recSilenceNoise+":d="+recSilenceMinSec, "-f", "null", "-")
		out, _ := cmd.CombinedOutput()
		silences = parseSilences(string(out), meta.DurationMs)
	}
	evs := readRecEvents(filepath.Join(dir, recEventsFile))
	takes := deckTakes(meta.Slides, evs, meta.DurationMs)
	tr := buildTranscript(meta.Slides, evs, takes, silences, meta.DurationMs)
	raw, err := json.Marshal(tr)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, recTranscriptFile), raw)
}

func fileSize(file string) int64 {
	st, err := os.Stat(file)
	if err != nil {
		return 0
	}
	return st.Size()
}

func (l *Library) readRecTranscript(id, rec string) (DeckTranscript, bool) {
	raw, err := os.ReadFile(filepath.Join(l.recDir(id, rec), recTranscriptFile))
	if err != nil {
		return DeckTranscript{Segments: []DeckSegment{}}, false
	}
	var tr DeckTranscript
	if json.Unmarshal(raw, &tr) != nil {
		return DeckTranscript{Segments: []DeckSegment{}}, false
	}
	if tr.Segments == nil {
		tr.Segments = []DeckSegment{}
	}
	for i := range tr.Segments {
		if tr.Segments[i].Parts == nil {
			tr.Segments[i].Parts = []DeckPart{}
		}
	}
	return tr, true
}
