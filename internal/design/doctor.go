package design

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/deploy/renderkit"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The render doctor (HRN-744). "Chrome and ffmpeg are on PATH" says nothing about whether a picture or a video comes
// out: a Chrome without a sandbox, a missing font set or an ffmpeg without x264 all pass that test and fail on the first
// export. The doctor draws a real frame with the built-in engine's own browser pipeline, encodes a two-second MP4 with
// its own ffmpeg command line and reads the result back with ffprobe, step by step, each step with a code of its own.

// Codes of a failed step. They are stable: docs/render-kit.md lists them with what to do.
const (
	DoctorChromeMissing  = "RND-101"
	DoctorFFmpegMissing  = "RND-102"
	DoctorFFprobeMissing = "RND-103"
	DoctorBrowserStart   = "RND-201"
	DoctorFrameCapture   = "RND-202"
	DoctorFrameDecode    = "RND-203"
	DoctorFrameSize      = "RND-204"
	DoctorFrameBlank     = "RND-205"
	DoctorVideoEncode    = "RND-301"
	DoctorVideoProbe     = "RND-302"
	DoctorVideoCodec     = "RND-303"
	DoctorVideoDuration  = "RND-304"
	DoctorVideoSize      = "RND-305"
	DoctorKitVersions    = "RND-401" // a warning: the kit differs from the pinned versions
)

// Sizes of the probe: a small frame draws in a blink, a 2 s clip is what the issue asks for.
const (
	doctorW, doctorH = 640, 360
	doctorFPS        = 6
	doctorFrames     = 12 // doctorFrames / doctorFPS = 2 s
)

// DoctorStep is one line of the report.
type DoctorStep struct {
	// ID names the step; Code is set on a failure (and on the warning of the versions step).
	ID   string `json:"id"`
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Warn: the step passed but says something worth reading; it never fails the report.
	Warn bool `json:"warn,omitempty"`
	// Skipped: an earlier step failed, so this one could not run.
	Skipped bool   `json:"skipped,omitempty"`
	Code    string `json:"code,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Ms      int64  `json:"ms"`
}

// DoctorReport is the result of RenderDoctor.
type DoctorReport struct {
	OK    bool         `json:"ok"`
	At    time.Time    `json:"at"`
	Ms    int64        `json:"ms"`
	Kit   int          `json:"kit"` // the pinned kit's revision this doctor knows
	Steps []DoctorStep `json:"steps"`
}

// FailedStep is the first step that failed, nil when the report is OK.
func (r DoctorReport) FailedStep() *DoctorStep {
	for i := range r.Steps {
		if !r.Steps[i].OK && !r.Steps[i].Skipped {
			return &r.Steps[i]
		}
	}
	return nil
}

// DoctorFFprobe is the ffprobe the doctor reads the clip with: RUNTIME_DESIGN_FFPROBE, else the one next to ffmpeg, else PATH.
func DoctorFFprobe() string {
	if p := os.Getenv("RUNTIME_DESIGN_FFPROBE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return ""
	}
	if ff := DeckFFmpeg(); ff != "" {
		next := filepath.Join(filepath.Dir(ff), "ffprobe")
		if _, err := os.Stat(next); err == nil {
			return next
		}
	}
	p, _ := exec.LookPath("ffprobe")
	return p
}

// doctorPage is what the frame shows: shapes, a gradient and text in two scripts, so a missing font or a browser that
// draws nothing is seen. {{N}} is the step of the clip's animation.
const doctorPage = `<!doctype html><meta charset="utf-8"><body style="margin:0;width:640px;height:360px;overflow:hidden;
background:linear-gradient(135deg,#1e3a8a,#be185d);font:700 44px sans-serif;color:#fff">
<div id="box" style="position:absolute;left:20px;top:120px;width:120px;height:120px;border-radius:24px;background:#facc15"></div>
<div style="position:absolute;left:40px;top:30px">Render kit</div>
<div style="position:absolute;left:40px;top:270px;font-size:32px">Проверка 0123456789</div></body>`

const doctorMove = `(() => { const b = document.getElementById('box'); b.style.left = (20 + %d * 20) + 'px'; return true; })()`

type doctorRun struct {
	l      *Library
	rep    DoctorReport
	failed bool
}

// step runs fn unless an earlier step failed. fn returns the code, detail and hint of a failure, or an empty code.
func (d *doctorRun) step(id, name string, fn func() (code, detail, hint string)) {
	s := DoctorStep{ID: id, Name: name}
	if d.failed {
		s.Skipped = true
		d.rep.Steps = append(d.rep.Steps, s)
		return
	}
	t := time.Now()
	code, detail, hint := fn()
	s.Ms = time.Since(t).Milliseconds()
	s.Detail, s.Hint = detail, hint
	if code == "" {
		s.OK = true
	} else {
		s.Code = code
		d.failed = true
	}
	d.rep.Steps = append(d.rep.Steps, s)
}

// RenderDoctor draws a real picture and a two-second video with this machine's browser and ffmpeg and reads them back.
// It writes only into a temporary folder, and does not need a running library server.
func (l *Library) RenderDoctor(ctx context.Context) DoctorReport {
	start := time.Now()
	d := &doctorRun{l: l, rep: DoctorReport{At: start.UTC(), Kit: renderkit.Major(renderkit.Pins()["RENDER_KIT_REV"])}}
	var chrome, ffmpeg, ffprobe string
	var frames [][]byte
	dir, derr := os.MkdirTemp("", l.cfg.Texts.Tag+"-doctor-")
	if derr == nil {
		defer os.RemoveAll(dir)
	}

	d.step("tools", locale.Pick("Tools are installed", "Инструменты установлены"), func() (string, string, string) {
		if chrome = l.findChrome(); chrome == "" {
			return DoctorChromeMissing, "no Chrome or Chromium found",
				"install chromium (or Google Chrome), or set RUNTIME_DESIGN_CHROME to its path; the container image has it"
		}
		if ffmpeg = DeckFFmpeg(); ffmpeg == "" {
			return DoctorFFmpegMissing, "no ffmpeg found", "install ffmpeg, or set RUNTIME_DESIGN_FFMPEG to its path"
		}
		if ffprobe = DoctorFFprobe(); ffprobe == "" {
			return DoctorFFprobeMissing, "no ffprobe found (it comes with ffmpeg)", "install the ffmpeg package that has ffprobe, or set RUNTIME_DESIGN_FFPROBE"
		}
		if derr != nil {
			return DoctorBrowserStart, derr.Error(), "the temporary folder cannot be made: check TMPDIR and free space"
		}
		return "", chrome + ", " + ffmpeg + ", " + ffprobe, ""
	})

	d.step("frame", locale.Pick("A picture is drawn", "Кадр рисуется"), func() (string, string, string) {
		page := filepath.Join(dir, "doctor.html")
		if err := os.WriteFile(page, []byte(doctorPage), 0o600); err != nil {
			return DoctorBrowserStart, err.Error(), "check free space in the temporary folder"
		}
		br, err := l.launchBrowser(chrome)
		if err != nil {
			return DoctorBrowserStart, err.Error(), "the browser did not start; in a container set RUNTIME_DESIGN_CHROME_ARGS=--no-sandbox"
		}
		defer br.close()
		sh := &deckShooter{l: l, br: br}
		tab, err := sh.tab()
		if err != nil {
			return DoctorBrowserStart, err.Error(), "the browser started but did not answer; run it by hand with --headless=new to see why"
		}
		defer tab.close()
		tab.lossless = true
		if err := tab.view(doctorW, doctorH, 1); err != nil {
			return DoctorFrameCapture, err.Error(), "the browser refused to set the frame size"
		}
		if err := tab.navigate("file://" + page); err != nil {
			return DoctorFrameCapture, err.Error(), "the browser could not open the test page"
		}
		if _, err := tab.eval(deckReady); err != nil {
			return DoctorFrameCapture, err.Error(), "the test page did not finish loading"
		}
		for i := 0; i < doctorFrames; i++ {
			if err := ctx.Err(); err != nil {
				return DoctorFrameCapture, err.Error(), "the check was cancelled"
			}
			if _, err := tab.eval(fmt.Sprintf(doctorMove, i)); err != nil {
				return DoctorFrameCapture, err.Error(), "the test page did not run its script"
			}
			pic, err := tab.png()
			if err != nil {
				return DoctorFrameCapture, err.Error(), "the browser gave no picture; check /dev/shm size and the sandbox switches"
			}
			frames = append(frames, pic)
		}
		img, err := png.Decode(bytes.NewReader(frames[0]))
		if err != nil {
			return DoctorFrameDecode, err.Error(), "the browser's picture is not a PNG"
		}
		b := img.Bounds()
		if b.Dx() != doctorW || b.Dy() != doctorH {
			return DoctorFrameSize, fmt.Sprintf("the picture is %dx%d, asked %dx%d", b.Dx(), b.Dy(), doctorW, doctorH), "the browser ignores the frame size: use a current Chromium"
		}
		if colors, text := frameStats(img); colors < 24 || text < 200 {
			return DoctorFrameBlank, fmt.Sprintf("%d colours, %d bright pixels: the frame is flat", colors, text),
				"the page drew nothing or no font: install fonts-liberation and fonts-noto-color-emoji (docs/render-kit.md)"
		}
		return "", fmt.Sprintf("PNG %dx%d, %d KB", b.Dx(), b.Dy(), len(frames[0])/1024), ""
	})

	clip := ""
	d.step("encode", locale.Pick("A 2-second MP4 is encoded", "MP4 на 2 секунды кодируется"), func() (string, string, string) {
		clip = filepath.Join(dir, "doctor.mp4")
		args := []string{"-hide_banner", "-y", "-loglevel", "error", "-nostdin",
			"-f", "image2pipe", "-c:v", "png", "-framerate", strconv.Itoa(doctorFPS), "-i", "pipe:0",
			"-vf", fmt.Sprintf("scale=%d:%d:out_color_matrix=bt709:out_range=tv,setsar=1,format=yuv420p", doctorW, doctorH),
			"-an", "-c:v", "libx264", "-preset", "ultrafast", "-crf", "28", "-r", strconv.Itoa(doctorFPS), "-pix_fmt", "yuv420p"}
		args = append(append(args, videoColorArgs()...), "-threads", "2", "-f", "mp4", clip)
		cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, ffmpeg, args...)
		cmd.Stdin = bytes.NewReader(bytes.Join(frames, nil))
		var out bytes.Buffer
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(out.String())
			if msg == "" {
				msg = err.Error()
			}
			hint := "ffmpeg failed; it needs the libx264 encoder (`ffmpeg -encoders | grep libx264`)"
			if strings.Contains(msg, "Unknown encoder") || strings.Contains(msg, "libx264") {
				hint = "this ffmpeg has no libx264: install the distribution's ffmpeg, not a minimal build"
			}
			return DoctorVideoEncode, lastLines(msg, 3), hint
		}
		return "", "libx264, " + strconv.Itoa(doctorFrames) + " frames", ""
	})

	d.step("probe", locale.Pick("ffprobe confirms codec, duration and size", "ffprobe подтверждает кодек, длительность и размер"), func() (string, string, string) {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		raw, err := exec.CommandContext(cctx, ffprobe, "-v", "error", "-select_streams", "v:0", "-show_entries",
			"stream=codec_name,width,height,pix_fmt:format=duration", "-of", "json", clip).Output()
		if err != nil {
			return DoctorVideoProbe, err.Error(), "ffprobe could not read the clip ffmpeg wrote"
		}
		var p struct {
			Streams []struct {
				Codec  string `json:"codec_name"`
				W      int    `json:"width"`
				H      int    `json:"height"`
				PixFmt string `json:"pix_fmt"`
			} `json:"streams"`
			Format struct {
				Duration string `json:"duration"`
			} `json:"format"`
		}
		if err := json.Unmarshal(raw, &p); err != nil || len(p.Streams) == 0 {
			return DoctorVideoProbe, "ffprobe found no video stream", "the clip is empty: see the encode step"
		}
		s := p.Streams[0]
		dur, _ := strconv.ParseFloat(p.Format.Duration, 64)
		detail := fmt.Sprintf("%s %dx%d %s, %.2f s", s.Codec, s.W, s.H, s.PixFmt, dur)
		if s.Codec != "h264" || s.PixFmt != "yuv420p" {
			return DoctorVideoCodec, detail, "the clip must be H.264 in yuv420p to play everywhere: this ffmpeg encodes something else"
		}
		if math.Abs(dur-float64(doctorFrames)/doctorFPS) > 0.3 {
			return DoctorVideoDuration, detail, "the clip should last 2 s: the encoder dropped or repeated frames"
		}
		if s.W != doctorW || s.H != doctorH {
			return DoctorVideoSize, detail, fmt.Sprintf("the clip should be %dx%d", doctorW, doctorH)
		}
		return "", detail, ""
	})

	// Versions: a warning only. A machine outside the image may run other ones and still draw.
	if !d.failed {
		s := DoctorStep{ID: "versions", Name: locale.Pick("Versions match the pinned kit", "Версии совпадают с закреплённым набором"), OK: true}
		pins := renderkit.Pins()
		var diff []string
		if v := toolVersion(chrome, "--version"); v != "" {
			if m := firstNumber(v); m > 0 && m != renderkit.Major(pins["RENDER_KIT_CHROMIUM"]) {
				diff = append(diff, fmt.Sprintf("Chrome %d, pinned %s", m, pins["RENDER_KIT_CHROMIUM"]))
			}
		}
		if v := toolVersion(ffmpeg, "-version"); v != "" {
			if m := firstNumber(strings.TrimPrefix(v, "ffmpeg version ")); m > 0 && m != renderkit.Major(pins["RENDER_KIT_FFMPEG"]) {
				diff = append(diff, fmt.Sprintf("ffmpeg %d, pinned %s", m, pins["RENDER_KIT_FFMPEG"]))
			}
		}
		if len(diff) > 0 {
			s.Warn, s.Code = true, DoctorKitVersions
			s.Detail = strings.Join(diff, "; ")
			s.Hint = "the picture and the video came out, so this works; the image and the installer pin other versions (deploy/renderkit/versions.env)"
		} else {
			s.Detail = "kit revision " + pins["RENDER_KIT_REV"]
		}
		d.rep.Steps = append(d.rep.Steps, s)
	}

	d.rep.OK = !d.failed
	d.rep.Ms = time.Since(start).Milliseconds()
	return d.rep
}

// frameStats counts the distinct colours (in 4-bit steps, over a grid) and the bright pixels (the text is white).
func frameStats(img stdimage.Image) (colors, bright int) {
	seen := map[uint16]bool{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			r, g, bl, _ := img.At(x, y).RGBA()
			seen[uint16(r>>12)<<8|uint16(g>>12)<<4|uint16(bl>>12)] = true
			if r>>8 > 235 && g>>8 > 235 && bl>>8 > 235 {
				bright++
			}
		}
	}
	return len(seen), bright
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}

func toolVersion(bin, flag string) string {
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, flag).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}

// firstNumber is the first run of digits in s, 0 when none.
func firstNumber(s string) int {
	start := strings.IndexAny(s, "0123456789")
	if start < 0 {
		return 0
	}
	end := start
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[start:end])
	return n
}

// ---- readiness from the doctor's result

// ReadyProbeEnv turns the engine's readiness in the API into the doctor's verdict. Off (the default), Ready stays true
// as before, and Chrome/Ffmpeg only say the programs are there.
const ReadyProbeEnv = "RUNTIME_DESIGN_READY_PROBE"

const (
	readyOKTTL   = 15 * time.Minute
	readyFailTTL = time.Minute
)

// RenderHealth is the cached outcome of the doctor, as the engine status shows it.
type RenderHealth struct {
	// Checking: no result yet, the first check runs in the background.
	Checking bool       `json:"checking,omitempty"`
	OK       bool       `json:"ok"`
	At       *time.Time `json:"at,omitempty"`
	// Code and Hint are the first failed step's.
	Code string `json:"code,omitempty"`
	Step string `json:"step,omitempty"`
	Hint string `json:"hint,omitempty"`
}

type readyCache struct {
	mu      sync.Mutex
	rep     *DoctorReport
	key     string
	running bool
}

func readyProbeOn() bool { return os.Getenv(ReadyProbeEnv) == "1" }

// renderKey changes when the machine's tools do, so a new Chrome is looked at again at once.
func (l *Library) renderKey() string {
	var b strings.Builder
	for _, p := range []string{l.findChrome(), DeckFFmpeg(), DoctorFFprobe()} {
		b.WriteString(p)
		if st, err := os.Stat(p); p != "" && err == nil {
			b.WriteString(st.ModTime().Format(time.RFC3339Nano))
		}
		b.WriteByte('|')
	}
	return b.String()
}

// RenderHealth is the doctor's result for the API: the cached one while it is fresh, else a new check runs behind the
// caller (never on the request) and the stale result, if any, is shown meanwhile.
func (l *Library) RenderHealth() RenderHealth {
	c := &l.ready
	key := l.renderKey()
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh := false
	if c.rep != nil && c.key == key {
		ttl := readyOKTTL
		if !c.rep.OK {
			ttl = readyFailTTL
		}
		fresh = time.Since(c.rep.At) < ttl
	}
	if !fresh && !c.running {
		c.running = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			rep := l.RenderDoctor(ctx)
			c.mu.Lock()
			c.rep, c.key, c.running = &rep, key, false
			c.mu.Unlock()
		}()
	}
	if c.rep == nil {
		return RenderHealth{Checking: true}
	}
	h := RenderHealth{OK: c.rep.OK, At: &c.rep.At}
	if f := c.rep.FailedStep(); f != nil {
		h.Code, h.Step, h.Hint = f.Code, f.ID, f.Hint
	}
	return h
}

var errDoctorFailed = errors.New("render doctor failed")

// DoctorError is nil for an OK report.
func (r DoctorReport) Err() error {
	if r.OK {
		return nil
	}
	return errDoctorFailed
}
