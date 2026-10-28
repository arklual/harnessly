package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

var (
	msgOS         = locale.T("Built-in speech recognition runs on Linux and macOS only.", "Встроенное распознавание речи работает только на Linux и macOS.")
	msgArch       = locale.T("Built-in speech recognition has no build for this processor.", "Для этого процессора нет сборки встроенного распознавания речи.")
	msgNoAVX2     = locale.T("Built-in speech recognition needs a processor with AVX2.", "Встроенному распознаванию речи нужен процессор с AVX2.")
	msgMacBinary  = locale.T("Install the recognition server first: brew install whisper-cpp", "Установите сервер распознавания: brew install whisper-cpp")
	msgNoNetwork  = locale.T("Could not download the speech model: ", "Не удалось скачать модель распознавания: ")
	msgNoBinary   = locale.T("Could not get the recognition server: ", "Не удалось получить сервер распознавания: ")
	msgOffline    = locale.T("Downloads are off (RUNTIME_VOICE_DOWNLOAD=off) and this file is missing: ", "Скачивание выключено (RUNTIME_VOICE_DOWNLOAD=off), а этого файла нет: ")
	msgBadServer  = locale.T("The recognition server did not start: ", "Сервер распознавания не запустился: ")
	msgBadAnswer  = locale.T("The recognition server answered with an error: ", "Сервер распознавания ответил ошибкой: ")
	msgCheckModel = locale.T("The speech model file does not match the pinned checksum.", "Файл модели не совпадает с закреплённой контрольной суммой.")
)

// Config is what the deployment names; every field has a default.
type Config struct {
	// Dir is the engine's folder (the runtime's data folder, voice/): bin/ and models/ are in it.
	Dir string
	// Bin is a whisper-server of the operator's own; ModelFile a ggml model of their own (no pin, no download).
	Bin, ModelFile string
	// Model is "tiny", "base" or "small"; "" picks by the processor count.
	Model string
	// Threads is what the recognition may use; 0 is the processor count up to four.
	Threads int
	// NoDownload keeps the engine from touching the network: it uses only the files that are already there.
	NoDownload bool
	// Idle is how long the server stays loaded after the last recognition (5 minutes).
	Idle time.Duration

	Client                *http.Client
	ModelBase, BinaryBase string
	GOOS, GOARCH          string
	CPUFlags              func() string // the processor's feature flags; /proc/cpuinfo's by default
	LookPath              func(string) (string, error)
	Log                   func(level, msg string, kv ...any)
}

// Status is where the preparation stands: "idle" (nothing asked yet), "preparing" (Stage is "binary" or "model" and
// Percent how far), "ready" and "failed" (Error says why).
type Status struct {
	State   string
	Stage   string
	Percent int
	Error   string
}

// Engine is the recognition of one runtime.
type Engine struct {
	cfg Config

	mu       sync.Mutex
	st       Status
	done     chan struct{} // closed when the running preparation ends
	failedAt time.Time
	bin      string
	model    string
	remote   *Remote // a server of the person's own recognizes instead (remote.go)

	srvMu sync.Mutex // the server process and what is asked of it, one at a time
	proc  *server
	idle  *time.Timer
}

// New is an engine; nothing is started or fetched until Prepare.
func New(cfg Config) *Engine {
	if cfg.GOOS == "" {
		cfg.GOOS = runtime.GOOS
	}
	if cfg.GOARCH == "" {
		cfg.GOARCH = runtime.GOARCH
	}
	if cfg.Client == nil {
		cfg.Client = &http.Client{}
	}
	if cfg.ModelBase == "" {
		cfg.ModelBase = ModelBase
	}
	if cfg.BinaryBase == "" {
		cfg.BinaryBase = BinaryBase
	}
	if cfg.LookPath == nil {
		cfg.LookPath = exec.LookPath
	}
	if cfg.CPUFlags == nil {
		cfg.CPUFlags = func() string { b, _ := os.ReadFile("/proc/cpuinfo"); return string(b) }
	}
	if cfg.Idle == 0 {
		cfg.Idle = 5 * time.Minute
	}
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	if cfg.Threads <= 0 {
		cfg.Threads = min(runtime.NumCPU(), 4)
	}
	return &Engine{cfg: cfg, st: Status{State: "idle"}}
}

// model is the model to use: the named one, else "small" from four processors up and "base" below.
func (e *Engine) pick() Model {
	e.mu.Lock()
	name := e.cfg.Model
	e.mu.Unlock()
	if m, ok := Models[name]; ok {
		return m
	}
	if runtime.NumCPU() >= 4 {
		return Models["small"]
	}
	return Models["base"]
}

// SetModel changes the model ("tiny", "base", "small"; "" picks by the processor count). The next dictation gets it: a
// ready engine goes back to idle and its server is stopped, so the next one prepares (downloads) the new model. A
// preparation under way finishes with the model it started with. False when nothing changed.
func (e *Engine) SetModel(name string) bool {
	e.mu.Lock()
	if e.cfg.Model == name {
		e.mu.Unlock()
		return false
	}
	e.cfg.Model = name
	if e.cfg.ModelFile == "" && e.st.State != "preparing" {
		e.st = Status{State: "idle"}
	}
	e.mu.Unlock()
	e.srvMu.Lock()
	if e.proc != nil {
		e.kill(e.proc)
		e.proc = nil
	}
	e.srvMu.Unlock()
	return true
}

// Eligible is nil when this machine can recognize with the engine (maybe after a download), else why not.
func (e *Engine) Eligible() error {
	if e.UsesRemote() {
		return nil
	}
	if e.cfg.Bin != "" || e.localBinary() != "" {
		return nil
	}
	switch e.cfg.GOOS {
	case "linux":
	case "darwin":
		return errors.New(msgMacBinary.String())
	default:
		return errors.New(msgOS.String())
	}
	if e.cfg.GOARCH != "amd64" && e.cfg.GOARCH != "arm64" {
		return errors.New(msgArch.String())
	}
	if e.cfg.GOARCH == "amd64" {
		flags := e.cfg.CPUFlags()
		if flags != "" && !(strings.Contains(flags, " avx2") && strings.Contains(flags, " fma")) {
			return errors.New(msgNoAVX2.String())
		}
	}
	return nil
}

// localBinary is a whisper-server already on the machine: the engine's own folder, then the PATH and Homebrew.
func (e *Engine) localBinary() string {
	own := filepath.Join(e.cfg.Dir, "bin", "whisper-server-"+WhisperVersion)
	if st, err := os.Stat(own); err == nil && st.Mode().IsRegular() {
		return own
	}
	if p, err := e.cfg.LookPath("whisper-server"); err == nil {
		return p
	}
	for _, p := range []string{"/opt/homebrew/bin/whisper-server", "/usr/local/bin/whisper-server", "/home/linuxbrew/.linuxbrew/bin/whisper-server"} {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// Status is the preparation's state now; an engine that sends to a server of the person's own is always ready.
func (e *Engine) Status() Status {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.remote != nil {
		return Status{State: "ready"}
	}
	return e.st
}

// Ready says the binary and the model are in place and checked.
func (e *Engine) Ready() bool { return e.Status().State == "ready" }

// retryAfter is how long a failed preparation waits before the next dictation tries again.
const retryAfter = 20 * time.Second

// Prepare starts getting the binary and the model unless that is done, under way, or failed a moment ago. It returns
// at once; Status says how it goes and Wait blocks for it.
func (e *Engine) Prepare() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.remote != nil || e.st.State == "ready" || e.st.State == "preparing" {
		return
	}
	if e.st.State == "failed" && time.Since(e.failedAt) < retryAfter {
		return
	}
	e.st = Status{State: "preparing"}
	done := make(chan struct{})
	e.done = done
	go func() {
		err := e.prepare()
		e.mu.Lock()
		if err != nil {
			e.st = Status{State: "failed", Error: err.Error()}
			e.failedAt = time.Now()
			e.cfg.Log("warn", "speech engine is not ready: "+err.Error())
		} else {
			e.st = Status{State: "ready"}
			e.cfg.Log("info", "speech engine is ready ("+filepath.Base(e.model)+")")
		}
		e.mu.Unlock()
		close(done)
	}()
}

// Wait blocks until the preparation that Prepare started ends, and says how.
func (e *Engine) Wait(ctx context.Context) error {
	e.mu.Lock()
	done := e.done
	e.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if st := e.Status(); st.State == "failed" {
		return errors.New(st.Error)
	}
	return nil
}

func (e *Engine) setProgress(stage string, done, total int64) {
	pct := 0
	if total > 0 {
		pct = int(done * 100 / total)
	}
	e.mu.Lock()
	e.st.Stage, e.st.Percent = stage, pct
	e.mu.Unlock()
}

func (e *Engine) prepare() error {
	ctx := context.Background()
	if err := e.Eligible(); err != nil {
		return err
	}
	bin := e.cfg.Bin
	if bin == "" {
		bin = e.localBinary()
	}
	if bin == "" {
		if e.cfg.NoDownload {
			return errors.New(msgOffline.String() + "whisper-server")
		}
		var err error
		if bin, err = e.fetchBinary(ctx); err != nil {
			return errors.New(msgNoBinary.String() + err.Error())
		}
	}
	model := e.cfg.ModelFile
	if model == "" {
		var err error
		if model, err = e.fetchModel(ctx); err != nil {
			return err
		}
	}
	e.mu.Lock()
	e.bin, e.model = bin, model
	e.mu.Unlock()
	return nil
}

func (e *Engine) fetchBinary(ctx context.Context) (string, error) {
	name := "whisper-server-" + e.cfg.GOOS + "-" + e.cfg.GOARCH
	e.setProgress("binary", 0, 0)
	sums, err := fetchBytes(ctx, e.cfg.Client, e.cfg.BinaryBase+BinarySums, 64<<10)
	if err != nil {
		return "", err
	}
	want := sumOf(sums, name)
	if want == "" {
		return "", fmt.Errorf("%s is not in %s", name, BinarySums)
	}
	dest := filepath.Join(e.cfg.Dir, "bin", "whisper-server-"+WhisperVersion)
	err = download(ctx, e.cfg.Client, e.cfg.BinaryBase+name, dest, want, 0o755, func(d, t int64) { e.setProgress("binary", d, t) })
	return dest, err
}

func (e *Engine) fetchModel(ctx context.Context) (string, error) {
	m := e.pick()
	dest := filepath.Join(e.cfg.Dir, "models", m.File)
	if st, err := os.Stat(dest); err == nil && st.Size() == m.Size && fileSum(dest) == m.SHA256 {
		return dest, nil
	}
	if e.cfg.NoDownload {
		return "", errors.New(msgOffline.String() + dest)
	}
	e.setProgress("model", 0, m.Size)
	err := download(ctx, e.cfg.Client, e.cfg.ModelBase+m.File, dest, m.SHA256, 0o644, func(d, _ int64) { e.setProgress("model", d, m.Size) })
	if errors.Is(err, ErrChecksum) {
		return "", errors.New(msgCheckModel.String())
	}
	if err != nil {
		return "", errors.New(msgNoNetwork.String() + err.Error())
	}
	return dest, nil
}

// --- the server ------------------------------------------------------------------------------------------------

type server struct {
	cmd  *exec.Cmd
	base string
	gone chan struct{}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startServer runs whisper-server on a loopback port of its own and waits until its model is loaded. srvMu is held.
func (e *Engine) startServer(ctx context.Context) (*server, error) {
	e.mu.Lock()
	bin, model := e.bin, e.model
	e.mu.Unlock()
	e.reapStale()
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	args := []string{"-m", model, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "-t", strconv.Itoa(e.cfg.Threads),
		"-nt", "-sns", "-mc", "0", "-nf", "-bo", "1"}
	var stderr tail
	cmd := exec.Command(bin, args...)
	cmd.Stderr = &stderr
	cmd.Stdout = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, errors.New(msgBadServer.String() + err.Error())
	}
	e.writePid(cmd.Process.Pid)
	s := &server{cmd: cmd, base: "http://127.0.0.1:" + strconv.Itoa(port), gone: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(s.gone) }()
	started := time.Now()
	for {
		select {
		case <-s.gone:
			return nil, errors.New(msgBadServer.String() + stderr.String())
		case <-ctx.Done():
			e.kill(s)
			return nil, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
		if time.Since(started) > 120*time.Second {
			e.kill(s)
			return nil, errors.New(msgBadServer.String() + "timeout")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/health", nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		var body struct{ Status string }
		_ = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK && body.Status == "ok" {
			e.cfg.Log("info", fmt.Sprintf("speech server loaded in %d ms", time.Since(started).Milliseconds()))
			return s, nil
		}
	}
}

func (e *Engine) kill(s *server) {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	select {
	case <-s.gone:
	case <-time.After(3 * time.Second):
	}
	_ = os.Remove(filepath.Join(e.cfg.Dir, "server.pid"))
}

func (e *Engine) writePid(pid int) {
	_ = os.MkdirAll(e.cfg.Dir, 0o755)
	_ = os.WriteFile(filepath.Join(e.cfg.Dir, "server.pid"), []byte(strconv.Itoa(pid)), 0o600)
}

// reapStale stops a server a previous runtime left behind (a crash): the pid file names it, and only a process that
// still is a whisper-server counts.
func (e *Engine) reapStale() {
	path := filepath.Join(e.cfg.Dir, "server.pid")
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || pid <= 1 {
		return
	}
	cmdline, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil || !bytes.Contains(cmdline, []byte("whisper-server")) {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
	}
}

// Close stops the server.
func (e *Engine) Close() {
	e.srvMu.Lock()
	defer e.srvMu.Unlock()
	if e.idle != nil {
		e.idle.Stop()
	}
	if e.proc != nil {
		e.kill(e.proc)
		e.proc = nil
	}
}

// Infer recognizes PCM16 mono 16 kHz audio; lang is "ru", "en" or "auto". The server is started when it is not
// running and stopped after Idle without a request. Cancelling ctx stops the pass.
func (e *Engine) Infer(ctx context.Context, pcm []byte, lang string) (string, error) {
	if r := e.remoteServer(); r != nil {
		return e.inferRemote(ctx, r, pcm, lang)
	}
	if !e.Ready() {
		return "", errors.New("speech engine is not ready")
	}
	e.srvMu.Lock()
	defer e.srvMu.Unlock()
	if e.idle != nil {
		e.idle.Stop()
	}
	defer func() {
		e.idle = time.AfterFunc(e.cfg.Idle, func() {
			e.srvMu.Lock()
			defer e.srvMu.Unlock()
			if e.proc != nil {
				e.kill(e.proc)
				e.proc = nil
			}
		})
	}()
	if e.proc != nil {
		select {
		case <-e.proc.gone:
			e.proc = nil
		default:
		}
	}
	if e.proc == nil {
		s, err := e.startServer(ctx)
		if err != nil {
			return "", err
		}
		e.proc = s
	}
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	hdr := textproto.MIMEHeader{}
	hdr.Set("Content-Disposition", `form-data; name="file"; filename="audio.wav"`)
	hdr.Set("Content-Type", "audio/wav")
	part, _ := w.CreatePart(hdr)
	_, _ = part.Write(wav(padPCM(pcm)))
	for k, v := range map[string]string{"response_format": "json", "temperature": "0.0", "no_timestamps": "true", "language": lang} {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.proc.base+"/inference", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			e.kill(e.proc)
			e.proc = nil
		}
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	var out struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &out)
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		return "", errors.New(msgBadAnswer.String() + resp.Status + " " + out.Error)
	}
	return clean(out.Text), nil
}

// padPCM gives a recording shorter than a second the silence whisper wants to have.
func padPCM(pcm []byte) []byte {
	const floor = sampleRate * 2 * 11 / 10
	if len(pcm) >= floor {
		return pcm
	}
	return append(append([]byte(nil), pcm...), make([]byte, floor-len(pcm))...)
}

var (
	annotation = regexp.MustCompile(`^[\[(][^\])]*[\])]$`)
	blanks     = regexp.MustCompile(`\s+`)
)

// clean is the text of an answer: whisper marks what is not speech as "[BLANK_AUDIO]" or "(music)"; such a text is
// silence.
func clean(text string) string {
	text = strings.TrimSpace(blanks.ReplaceAllString(text, " "))
	if annotation.MatchString(text) {
		return ""
	}
	return text
}

// tail keeps the end of what a process wrote to stderr.
type tail struct {
	mu sync.Mutex
	b  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 600 {
		t.b = t.b[len(t.b)-600:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.b))
}
