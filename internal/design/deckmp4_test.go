package design

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestMP4FramesFollowTheKeptTime(t *testing.T) {
	slides := []string{"A", "B", "C"}
	n := func(v int) *int { return &v }
	evs := []DeckEvent{
		{T: 2000, K: DeckEvSlide, Page: "B"},
		{T: 3000, K: DeckEvStep, N: n(1)},
		{T: 4000, K: DeckEvStep, N: n(2)},
		{T: 6000, K: DeckEvSlide, Page: "C"},
	}
	// 3500..4500 is cut out: step 1 lasts 500 ms in the video, step 2 starts at 500 ms of its state's time.
	keep := []deckSpan{{0, 3500}, {4500, 8000}}
	got := mp4Frames(slides, evs, keep, 8000)
	want := []mp4Frame{{"A", 0, 2000}, {"B", 0, 1000}, {"B", 1, 500}, {"B", 2, 1500}, {"C", 0, 2000}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("frames %+v, want %+v", got, want)
	}
	var sum int64
	for _, f := range got {
		sum += f.Ms
	}
	if sum != spansLen(keep) {
		t.Errorf("frames last %d, the video %d", sum, spansLen(keep))
	}
	ch := mp4Chapters(append(got, mp4Frame{"A", 0, 100}), func(p string) string { return "Slide " + p })
	if !reflect.DeepEqual(ch, []DeckChapter{{"A", "Slide A", 0}, {"B", "Slide B", 2000}, {"C", "Slide C", 5000}}) {
		t.Errorf("chapters %+v", ch)
	}
	// A recording with no events is its first slide.
	if got := mp4Frames(slides, nil, []deckSpan{{0, 1000}}, 1000); !reflect.DeepEqual(got, []mp4Frame{{"A", 0, 1000}}) {
		t.Errorf("no events: %+v", got)
	}
}

func TestMP4ConcatList(t *testing.T) {
	got := mp4Concat([]string{"a.png", "b.png"}, []mp4Frame{{"A", 0, 1500}, {"B", 0, 2250}})
	want := "file 'a.png'\nduration 1.500\nfile 'b.png'\nduration 2.250\nfile 'b.png'\n"
	if got != want {
		t.Errorf("got %q", got)
	}
}

func TestMP4Subs(t *testing.T) {
	tr := DeckTranscript{Segments: []DeckSegment{{T0: 0, T1: 4000, Page: "A", Parts: []DeckPart{
		{Text: "ну", Kind: DeckPartFiller, Cut: true, T0: 0, T1: 500},
		{Text: "one two three four five six seven eight nine ten eleven", Kind: DeckPartWord, T0: 500, T1: 4000},
	}}, {T0: 4000, T1: 5000, Page: "A", Parts: []DeckPart{{Text: "pause", Kind: DeckPartPause, Cut: true, T0: 4000, T1: 5000}}}}}
	// The filler is cut out (500 ms) and the pause with it (1000 ms).
	keep := []deckSpan{{500, 4000}}
	subs := mp4Subs(tr, keep)
	if len(subs) != 2 || subs[0].Text != "one two three four five six seven eight nine" || subs[1].Text != "ten eleven" {
		t.Fatalf("subs %+v", subs)
	}
	if subs[0].A != 0 || subs[0].B > subs[1].A || subs[1].B > 3600 {
		t.Errorf("times %+v", subs)
	}
	ass := mp4ASS(1920, 1080, subs)
	if !strings.Contains(ass, "PlayResX: 1920") || !strings.Contains(ass, "Dialogue: 0,0:00:00.00,") || !strings.Contains(ass, ",,one two three") {
		t.Errorf("ass:\n%s", ass)
	}
	if assTime(3_723_450) != "1:02:03.45" {
		t.Errorf("assTime = %s", assTime(3_723_450))
	}
}

func TestMP4FilterGraph(t *testing.T) {
	p := mp4Plan{W: 1920, H: 1080, Keep: []deckSpan{{0, 5000}, {7000, 9000}}, Total: 7000, Cam: "br", Subs: "ass"}
	got := mp4Filter(p)
	want := strings.Join([]string{
		"[1:v]fps=30,scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=white,setsar=1,format=yuv420p[base]",
		"[0:v]trim=start=0.000:end=5.000,setpts=PTS-STARTPTS[c0]",
		"[0:a]atrim=start=0.000:end=5.000,asetpts=PTS-STARTPTS[a0]",
		"[0:v]trim=start=7.000:end=9.000,setpts=PTS-STARTPTS[c1]",
		"[0:a]atrim=start=7.000:end=9.000,asetpts=PTS-STARTPTS[a1]",
		"[c0][a0][c1][a1]concat=n=2:v=1:a=1[cam][a]",
		"[cam]fps=30,crop='min(iw,ih)':'min(iw,ih)',scale=200:200,hflip,format=yuva420p,geq=lum='lum(X,Y)':cb='cb(X,Y)':cr='cr(X,Y)':a='if(lte(pow(X-W/2,2)+pow(Y-H/2,2),pow(W/2,2)),255,0)'[circ]",
		"color=c=white:s=208x208:r=30:d=7.000,format=yuva420p,geq=lum=255:cb=128:cr=128:a='if(lte(pow(X-W/2,2)+pow(Y-H/2,2),pow(W/2,2)),255,0)'[ring]",
		"[base][ring]overlay=x=1680:y=840[v1]",
		"[v1][circ]overlay=x=1684:y=844[v2]",
		"[v2]subtitles=subs.ass[v]",
	}, ";")
	if got != want {
		t.Errorf("graph:\n%s\nwant:\n%s", got, want)
	}

	// No camera: the sound alone is joined and the pictures are the video.
	got = mp4Filter(mp4Plan{W: 1280, H: 720, Keep: []deckSpan{{1000, 2500}}, Total: 1500})
	want = strings.Join([]string{
		"[1:v]fps=30,scale=1280:720:force_original_aspect_ratio=decrease,pad=1280:720:(ow-iw)/2:(oh-ih)/2:color=white,setsar=1,format=yuv420p[base]",
		"[0:a]atrim=start=1.000:end=2.500,asetpts=PTS-STARTPTS[a0]",
		"[a0]concat=n=1:v=0:a=1[a]",
		"[base]null[v]",
	}, ";")
	if got != want {
		t.Errorf("graph without camera:\n%s\nwant:\n%s", got, want)
	}

	// A corner moves the circle; drawtext makes a filter for each line.
	got = mp4Filter(mp4Plan{W: 1920, H: 1080, Keep: []deckSpan{{0, 1000}}, Total: 1000, Cam: "tl", Subs: "drawtext",
		SubLines: []mp4Sub{{0, 500, "a"}, {500, 1000, "b"}}})
	for _, frag := range []string{"overlay=x=32:y=32[v1]", "overlay=x=36:y=36[v2]", "[v2]drawtext=textfile=sub0.txt:", "enable='between(t,0.500,1.000)'[v]", "[t0]drawtext=textfile=sub1.txt"} {
		if !strings.Contains(got, frag) {
			t.Errorf("missing %q in\n%s", frag, got)
		}
	}
	args := mp4Args("av.webm", "matroska", "frames.txt", "G", "out.mp4", 7000)
	if args[len(args)-1] != "out.mp4" || !reflect.DeepEqual(args[len(args)-3:len(args)-1], []string{"-t", "7.000"}) {
		t.Errorf("args %v", args)
	}
	if j := strings.Join(args, " "); !strings.Contains(j, "-protocol_whitelist file -f matroska -i av.webm -protocol_whitelist file -f concat -safe 0 -i frames.txt") {
		t.Errorf("inputs are not pinned: %s", j)
	}
}

func TestMP4IsRegistered(t *testing.T) {
	e, ok := DeckExporterOf(DeckExportMP4)
	if !ok || e.Ext != "mp4" || !reflect.DeepEqual(e.Needs, []string{"chrome", "ffmpeg"}) || e.Run == nil {
		t.Fatalf("exporter %+v %v", e, ok)
	}
}

func TestMP4SaysWhyItCannotRun(t *testing.T) {
	l, _, id, _ := deckLibrary(t)
	t.Setenv("RUNTIME_DESIGN_FFMPEG", filepath.Join(t.TempDir(), "no-ffmpeg"))
	err := runDeckMP4(context.Background(), DeckExportRun{L: l, Artifact: id})
	if err == nil || !strings.Contains(err.Error(), "ffmpeg") {
		t.Fatalf("err = %v", err)
	}
	t.Setenv("RUNTIME_DESIGN_FFMPEG", "")
	if DeckFFmpeg() == "" {
		t.Skip("no ffmpeg on this machine")
	}
	err = runDeckMP4(context.Background(), DeckExportRun{L: l, Artifact: id})
	if err == nil || !strings.Contains(err.Error(), "recording") && !strings.Contains(err.Error(), "записи") {
		t.Fatalf("no recording: %v", err)
	}
}

// The whole render: a recording made by ffmpeg, boards served by a stand-in of the library server, drawn by the
// machine's browser, put together by its ffmpeg.
func TestMP4RendersARecording(t *testing.T) {
	if testing.Short() {
		t.Skip("drives a browser and ffmpeg")
	}
	ffmpeg := DeckFFmpeg()
	if ffmpeg == "" {
		t.Skip("no ffmpeg on this machine")
	}
	probe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("no ffprobe on this machine")
	}
	l, _, id, pages := deckLibrary(t)
	if l.findChrome() == "" {
		t.Skip("no browser on this machine")
	}
	project := l.ProjectDir(id)
	writeFile(t, filepath.Join(project, "Two.dc.html"),
		`<html><body style="background:#fff"><h1>Two</h1><p data-step="1">Second point</p></body></html>`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref, file, ok := ParsePagePath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		real := l.ResolveProjectFile(ref, strings.Split(file, "/"))
		if real == "" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, real)
	}))
	defer srv.Close()
	prev := libraryBase
	libraryBase = func(*Library) (string, error) { return srv.URL, nil }
	defer func() { libraryBase = prev }()
	defer l.thumbs().closeBrowser()

	m, err := l.CreateRec(id, true, "video/webm")
	if err != nil {
		t.Fatal(err)
	}
	av := filepath.Join(l.recDir(id, m.ID), recMediaFile)
	gen := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-y", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=15",
		"-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000", "-t", "6", "-c:v", "libvpx", "-deadline", "realtime", "-cpu-used", "8",
		"-b:v", "200k", "-c:a", "libopus", "-f", "webm", av)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot make a test recording: %v %s", err, out)
	}
	n := func(v int) *int { return &v }
	if _, err := l.AppendRecEvents(id, m.ID, []DeckEvent{
		{T: 0, K: DeckEvSlide, Page: pages[0]}, {T: 2000, K: DeckEvSlide, Page: pages[1]}, {T: 3000, K: DeckEvStep, N: n(1)},
		{T: 4500, K: DeckEvSlide, Page: pages[2]},
		{T: 1500, K: DeckEvASR, T0: i64(500), Text: "ну вот первое слово и второе слово"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.FinishRec(id, m.ID, 6000); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) && fileSize(filepath.Join(l.recDir(id, m.ID), recTranscriptFile)) == 0 {
		time.Sleep(100 * time.Millisecond)
	}
	got, err := l.ReadRecording(id, m.ID)
	if err != nil || len(got.Segments) == 0 || got.CutMs <= 0 {
		t.Fatalf("recording %+v %v", got, err)
	}

	out := filepath.Join(t.TempDir(), "talk.mp4")
	var last float64
	err = runDeckMP4(context.Background(), DeckExportRun{L: l, Artifact: id, Rec: m.ID, Out: out, Progress: func(p float64) {
		if p < last {
			t.Errorf("progress went back: %v after %v", p, last)
		}
		last = p
	}})
	if err != nil {
		t.Fatal(err)
	}
	if last != 1 {
		t.Errorf("progress ended at %v", last)
	}
	info := func(file string) (dur float64, streams string) {
		o, err := exec.Command(probe, "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", file).Output()
		if err != nil {
			t.Fatalf("ffprobe %s: %v", file, err)
		}
		var j struct {
			Streams []struct {
				Type string `json:"codec_type"`
			}
			Format struct{ Duration string }
		}
		_ = json.Unmarshal(o, &j)
		f, _ := strconv.ParseFloat(j.Format.Duration, 64)
		for _, s := range j.Streams {
			streams += s.Type + " "
		}
		return f, streams
	}
	dur, streams := info(out)
	want := float64(6000-got.CutMs) / 1000
	if dur < want-0.4 || dur > want+0.4 || !strings.Contains(streams, "video") || !strings.Contains(streams, "audio") {
		t.Errorf("video %.2fs %q, want about %.2fs", dur, streams, want)
	}
	pub := filepath.Join(l.ArtifactDir(id), DeckPublishedDir)
	if d2, _ := info(filepath.Join(pub, "video.mp4")); d2 != dur {
		t.Errorf("published %.2f, export %.2f", d2, dur)
	}
	var chapters []DeckChapter
	if err := json.Unmarshal([]byte(readFileT(t, filepath.Join(pub, "chapters.json"))), &chapters); err != nil || len(chapters) != 3 ||
		chapters[0].T != 0 || chapters[1].Title != "Two" || chapters[1].T <= 0 || chapters[2].T <= chapters[1].T {
		t.Errorf("chapters %+v %v", chapters, err)
	}
	if st := l.DeckPublishedState(id); !st.Video || st.PDF {
		t.Errorf("published %+v", st)
	}
	// A second run finds the render made and takes no pictures: it works with the browser gone.
	l.thumbs().closeBrowser()
	libraryBase = func(*Library) (string, error) { return "http://127.0.0.1:1", nil }
	if err := runDeckMP4(context.Background(), DeckExportRun{L: l, Artifact: id, Rec: m.ID, Out: filepath.Join(t.TempDir(), "again.mp4")}); err != nil {
		t.Errorf("render again: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(l.recDir(id, m.ID), "renders")); len(entries) != 1 {
		t.Errorf("renders: %v", entries)
	}
}
