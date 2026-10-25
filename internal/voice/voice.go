// Package voice is dictation for the composer's server engine: the page streams whatever its recorder produces over a
// socket, ffmpeg decodes it to PCM as it arrives, and the machine's recognition service, reached over a Unix socket,
// answers with partial and final text. No audio is written to disk and no transcript is logged.
//
// The page sends {type: "start", lang}, binary audio, {type: "stop" | "abort"}; it gets {type: "ready" | "partial" |
// "final" | "error"}, an error carrying code "unavailable" when this machine cannot recognize at all (the page then
// tries another engine) and "failed" otherwise.
package voice

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sys/unix"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
)

const (
	sampleRate   = 16000
	stderrTail   = 2000
	readLimit    = 256 * 1024
	readyTimeout = 30 * time.Second
)

// The texts a failed dictation shows, in the runtime's language.
var (
	msgNoFfmpeg     = locale.T("ffmpeg was not found on this machine: server-side transcription is unavailable.", "На этой машине не найден ffmpeg — серверная расшифровка недоступна.")
	msgUnavailable  = locale.T("Server-side speech recognition is not running on this machine", "Серверное распознавание речи на этой машине не запущено")
	msgNotLoaded    = locale.T("The recognition model did not manage to load", "Модель распознавания не успела загрузиться")
	msgRestarted    = locale.T("The recognition service restarted, try again", "Сервис распознавания перезапустился, попробуйте ещё раз")
	msgServiceDown  = locale.T("The recognition service is unavailable: ", "Сервис распознавания недоступен: ")
	msgCannotDecode = locale.T("Could not decode the audio: ", "Не удалось разобрать звук: ")
)

// Config is what the deployment names.
type Config struct {
	// Socket is the recognition service's Unix socket.
	Socket string
	// FFmpeg is ffmpeg's absolute path; when empty the usual places are looked in (Candidates).
	FFmpeg     string
	Candidates []string
	// ReadyTimeout is how long the service may take to load its model (30 s).
	ReadyTimeout time.Duration
	Log          func(level, msg string, kv ...any)
}

// Server serves dictation sockets; the request is already authenticated.
type Server struct{ cfg Config }

func New(cfg Config) *Server {
	if cfg.Candidates == nil {
		cfg.Candidates = []string{"/usr/bin/ffmpeg", "/usr/local/bin/ffmpeg"}
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = readyTimeout
	}
	if cfg.Log == nil {
		cfg.Log = func(string, string, ...any) {}
	}
	return &Server{cfg: cfg}
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// ffmpeg is the decoder's binary, "" when this machine has none.
func (s *Server) ffmpeg() string {
	if s.cfg.FFmpeg != "" {
		if filepath.IsAbs(s.cfg.FFmpeg) && exists(s.cfg.FFmpeg) {
			return s.cfg.FFmpeg
		}
		return ""
	}
	for _, c := range s.cfg.Candidates {
		if exists(c) {
			return c
		}
	}
	return ""
}

// errnoText is a system error as Node names it in its messages ("ECONNRESET").
func errnoText(err error) string {
	var e syscall.Errno
	if errors.As(err, &e) {
		if name := unix.ErrnoName(e); name != "" {
			return name
		}
	}
	return err.Error()
}

// event is one thing that happened to a session; the session handles them one at a time, in order.
type event struct {
	kind string // client, binary, closed, asr, asrError, asrClosed, readyTimeout, pcm, decoderExit
	data []byte
	err  error
	msg  map[string]json.RawMessage
	exit *string // decoder: nil for a clean end
}

type session struct {
	s      *Server
	ws     *websocket.Conn
	events chan event
	out    chan []byte
	ctx    context.Context

	started, stopping, ended bool
	openedAt, stopAt         time.Time

	asr *asrConn
	dec *decoder
}

// ServeHTTP upgrades the request and serves one dictation until either side closes.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if sameorigin.Refuse(w, r) {
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true, CompressionMode: websocket.CompressionDisabled})
	if err != nil {
		return
	}
	ws.SetReadLimit(readLimit)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ss := &session{s: s, ws: ws, events: make(chan event, 256), out: make(chan []byte, 256), ctx: ctx}
	go ss.writer()
	go func() {
		for {
			typ, data, err := ws.Read(ctx)
			if err != nil {
				var ce websocket.CloseError
				if errors.As(err, &ce) || errors.Is(err, io.EOF) || ctx.Err() != nil {
					err = nil
				}
				ss.deliver(ctx, event{kind: "closed", err: err})
				return
			}
			kind := "client"
			if typ == websocket.MessageBinary {
				kind = "binary"
			}
			ss.deliver(ctx, event{kind: kind, data: data})
		}
	}()
	ss.loop()
}

// writer sends the session's messages in order while the socket is open.
func (ss *session) writer() {
	for b := range ss.out {
		wctx, cancel := context.WithTimeout(ss.ctx, 10*time.Second)
		err := ss.ws.Write(wctx, websocket.MessageText, b)
		cancel()
		if err != nil {
			for range ss.out {
			}
			return
		}
	}
}

func (ss *session) send(v any) {
	select {
	case ss.out <- jsvalue.Stringify(v, ""):
	default: // the page stopped reading
	}
}

func (ss *session) loop() {
	defer func() {
		ss.finish(nil)
		close(ss.out)
		if ss.ws != nil {
			_ = ss.ws.CloseNow()
		}
	}()
	for ev := range ss.events {
		switch ev.kind {
		case "closed":
			if ev.err != nil {
				ss.s.cfg.Log("warn", "websocket error: "+ev.err.Error())
			}
			return
		case "binary":
			if ss.started && !ss.stopping && !ss.ended && ss.dec != nil {
				ss.dec.write(ev.data)
			}
		case "client":
			ss.onClient(ev.data)
		default:
			ss.onInternal(ev)
		}
	}
}

func (ss *session) onClient(raw []byte) {
	v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
	if err != nil {
		return
	}
	msg, ok := v.(*jsvalue.Object)
	if !ok {
		return
	}
	switch typ, _ := msg.Get("type"); typ {
	case "start":
		lang, _ := msg.Get("lang")
		ss.start(lang)
	case "stop":
		if !ss.started || ss.ended || ss.stopping {
			return
		}
		ss.stopping = true
		ss.stopAt = time.Now()
		// The decoder drains what it still holds before exiting; the service is told to finish after that.
		if ss.dec != nil {
			ss.dec.stop()
		}
	case "abort":
		ss.finish(nil)
	}
}

type failure struct {
	message any // the service's own message may be any JSON value, or none (absent)
	code    string
}

// absent is a value JSON.stringify leaves out with its key (undefined).
type absent struct{}

func (ss *session) finish(f *failure) {
	if ss.ended {
		return
	}
	ss.ended = true
	if ss.dec != nil {
		ss.dec.stop()
	}
	if ss.asr != nil {
		ss.asr.close()
	}
	if f != nil {
		ss.s.cfg.Log("warn", "voice session failed ("+f.code+"): "+jscompat.String(f.message))
		o := jsvalue.NewObject("type", "error")
		if _, none := f.message.(absent); !none {
			o.Set("message", f.message)
		}
		o.Set("code", f.code)
		ss.send(o)
	}
}

func (ss *session) start(lang any) {
	if ss.started || ss.ended {
		return
	}
	ss.started = true
	bin := ss.s.ffmpeg()
	if bin == "" {
		ss.finish(&failure{msgNoFfmpeg.String(), "unavailable"})
		return
	}
	resolved := "ru"
	if l, ok := lang.(string); ok && (l == "ru" || l == "en" || l == "auto") {
		resolved = l
	}
	ss.openedAt = time.Now()
	ss.asr = ss.dialASR(resolved)
	ss.dec = ss.startDecoder(bin)
}

func (ss *session) onInternal(ev event) {
	switch ev.kind {
	case "asr", "asrError", "asrClosed", "readyTimeout":
		if ss.asr != nil {
			ss.asr.handle(ss, ev)
		}
	case "pcm":
		if ss.asr != nil {
			ss.asr.feed(ev.data)
		}
	case "decoderExit":
		if ss.stopping {
			if ss.asr != nil {
				ss.asr.stop()
			}
		} else if ev.exit != nil {
			ss.finish(&failure{msgCannotDecode.String() + *ev.exit, "failed"})
		}
	}
}

// --- the recognition service ---------------------------------------------------------------------------------------

// asrConn is one dictation's connection to the recognition service: newline-separated JSON both ways.
type asrConn struct {
	lang        string
	out         chan []byte // nil once the connection is done
	ready, done bool
	timer       *time.Timer
	cancel      context.CancelFunc
}

func (ss *session) dialASR(lang string) *asrConn {
	a := &asrConn{lang: lang, out: make(chan []byte, 1024)}
	ctx, cancel := context.WithCancel(ss.ctx)
	a.cancel = cancel
	a.timer = time.AfterFunc(ss.s.cfg.ReadyTimeout, func() { ss.deliver(ctx, event{kind: "readyTimeout"}) })
	a.out <- jsonLine(jsvalue.NewObject("cmd", "start", "lang", lang))
	out := a.out
	go func() {
		var d net.Dialer
		conn, err := d.DialContext(ctx, "unix", ss.s.cfg.Socket)
		if err != nil {
			ss.deliver(ctx, event{kind: "asrError", err: dialError(err, ss.s.cfg.Socket)})
			ss.deliver(ctx, event{kind: "asrClosed"})
			return
		}
		go func() { <-ctx.Done(); _ = conn.Close() }()
		go func() {
			for b := range out {
				if _, err := conn.Write(b); err != nil {
					// The service hung up: its end of the stream, read on the other side, tells the session. A write
					// error is the failure only when nothing comes back for a second.
					time.AfterFunc(time.Second, func() { ss.deliver(ctx, event{kind: "asrError", err: opError("write", err)}) })
					for range out {
					}
					return
				}
			}
			if cw, ok := conn.(interface{ CloseWrite() error }); ok {
				_ = cw.CloseWrite()
			}
		}()
		buf := make([]byte, 0, 4096)
		chunk := make([]byte, 32*1024)
		for {
			n, err := conn.Read(chunk)
			buf = append(buf, chunk[:n]...)
			for {
				i := bytes.IndexByte(buf, '\n')
				if i < 0 {
					break
				}
				line := jscompat.DecodeUTF8(buf[:i])
				buf = buf[i+1:]
				if jscompat.Trim(line) == "" {
					continue
				}
				var msg map[string]json.RawMessage
				if json.Unmarshal([]byte(line), &msg) != nil || msg == nil {
					continue // a malformed line from the service is dropped, the session goes on
				}
				ss.deliver(ctx, event{kind: "asr", msg: msg})
			}
			if err != nil {
				if !errors.Is(err, io.EOF) && ctx.Err() == nil {
					ss.deliver(ctx, event{kind: "asrError", err: opError("read", err)})
				}
				ss.deliver(ctx, event{kind: "asrClosed"})
				return
			}
		}
	}()
	return a
}

// deliver hands an event to the session unless the connection it came from is gone.
func (ss *session) deliver(ctx context.Context, ev event) {
	select {
	case ss.events <- ev:
	case <-ctx.Done():
	}
}

type serviceError struct {
	missing bool
	text    string
}

func (e *serviceError) Error() string { return e.text }

func dialError(err error, path string) error {
	var e syscall.Errno
	missing := errors.As(err, &e) && (e == syscall.ENOENT || e == syscall.ECONNREFUSED || e == syscall.EACCES)
	return &serviceError{missing: missing, text: "connect " + errnoText(err) + " " + path}
}

func opError(op string, err error) error { return &serviceError{text: op + " " + errnoText(err)} }

func jsonLine(v any) []byte { return append(jsvalue.Stringify(v, ""), '\n') }

func (a *asrConn) send(v any) {
	if a.done || a.out == nil {
		return
	}
	select {
	case a.out <- jsonLine(v):
	default:
	}
}

func (a *asrConn) feed(pcm []byte) {
	if len(pcm) > 0 {
		a.send(jsvalue.NewObject("cmd", "audio", "pcm", base64.StdEncoding.EncodeToString(pcm)))
	}
}

// stop: no more audio, the service answers with one last final.
func (a *asrConn) stop() { a.send(jsvalue.NewObject("cmd", "stop")) }

func (a *asrConn) endWriting() {
	if a.out != nil {
		close(a.out)
		a.out = nil
	}
}

func (a *asrConn) close() {
	a.done = true
	a.timer.Stop()
	a.endWriting()
	a.cancel()
}

func (a *asrConn) code() string {
	if a.ready {
		return "failed"
	}
	return "unavailable"
}

func (a *asrConn) fail(ss *session, message any, code string) {
	if a.done {
		return
	}
	a.done = true
	a.timer.Stop()
	a.endWriting()
	a.cancel()
	ss.finish(&failure{message, code})
}

func rawValue(raw json.RawMessage) any {
	if raw == nil {
		return nil
	}
	v, err := jsvalue.Parse(raw)
	if err != nil {
		return nil
	}
	return v
}

func (a *asrConn) handle(ss *session, ev event) {
	switch ev.kind {
	case "readyTimeout":
		a.fail(ss, msgNotLoaded.String(), "unavailable")
	case "asrError":
		var se *serviceError
		if errors.As(ev.err, &se) && se.missing {
			a.fail(ss, msgUnavailable.String(), a.code())
		} else {
			a.fail(ss, msgServiceDown.String()+ev.err.Error(), a.code())
		}
	case "asrClosed":
		a.fail(ss, msgRestarted.String(), a.code())
	case "asr":
		if a.done {
			return
		}
		var typ string
		_ = json.Unmarshal(ev.msg["type"], &typ)
		text, hasText := ev.msg["text"]
		withText := func(kind string) *jsvalue.Object {
			o := jsvalue.NewObject("type", kind)
			if hasText {
				o.Set("text", rawValue(text))
			}
			return o
		}
		switch typ {
		case "ready":
			a.ready = true
			a.timer.Stop()
			ss.s.cfg.Log("info", "voice session ready in "+strconv.FormatInt(time.Since(ss.openedAt).Milliseconds(), 10)+" ms ("+a.lang+")")
			ss.send(jsvalue.NewObject("type", "ready"))
		case "partial":
			ss.send(withText("partial"))
		case "final":
			a.done = true
			a.endWriting()
			since := int64(0)
			if !ss.stopAt.IsZero() {
				since = time.Since(ss.stopAt).Milliseconds()
			}
			ss.s.cfg.Log("info", "voice session final "+strconv.FormatInt(since, 10)+" ms after stop, "+
				strconv.FormatInt(int64(time.Since(ss.openedAt).Seconds()+0.5), 10)+" s long")
			ss.send(withText("final"))
			ss.ended = true
			if ss.dec != nil {
				ss.dec.stop()
			}
		case "error":
			var m any = absent{}
			if message, has := ev.msg["message"]; has {
				m = rawValue(message)
			}
			a.fail(ss, m, a.code())
		}
	}
}

// --- the decoder ---------------------------------------------------------------------------------------------------

// decoder is one ffmpeg turning the recorder's container (webm/opus, mp4/aac) into mono 16 kHz PCM16LE.
type decoder struct {
	stdin io.WriteCloser
	in    chan []byte
	ended bool
}

func (ss *session) startDecoder(bin string) *decoder {
	d := &decoder{in: make(chan []byte, 1024)}
	cmd := exec.Command(bin, "-hide_banner", "-loglevel", "error", "-i", "pipe:0", "-f", "s16le", "-ar", strconv.Itoa(sampleRate), "-ac", "1", "pipe:1")
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	var stderr tail
	cmd.Stderr = &stderr
	ctx := ss.ctx
	if err := errors.Join(err1, err2); err != nil {
		msg := "spawn " + bin + " " + errnoText(err)
		go ss.deliver(ctx, event{kind: "decoderExit", exit: &msg})
		return d
	}
	if err := cmd.Start(); err != nil {
		msg := "spawn " + bin + " " + errnoText(err)
		go ss.deliver(ctx, event{kind: "decoderExit", exit: &msg})
		return d
	}
	d.stdin = stdin
	go func() {
		for b := range d.in {
			if _, err := stdin.Write(b); err != nil {
				for range d.in {
				}
				break
			}
		}
		_ = stdin.Close()
	}()
	go func() { <-ctx.Done(); _ = cmd.Process.Kill() }()
	go func() {
		// The end of decoding is ffmpeg's exit after its output is read to the end, so the last samples come first.
		buf := make([]byte, 64*1024)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				ss.deliver(ctx, event{kind: "pcm", data: append([]byte(nil), buf[:n]...)})
			}
			if err != nil {
				break
			}
		}
		err := cmd.Wait()
		var exit *string
		if err != nil {
			code := ""
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
					code = unix.SignalName(st.Signal())
				} else {
					code = strconv.Itoa(ee.ExitCode())
				}
			} else {
				code = err.Error()
			}
			lines := strings.Split(jscompat.Trim(stderr.String()), "\n")
			msg := "ffmpeg exited (" + code + "): " + lines[len(lines)-1]
			exit = &msg
		}
		ss.deliver(ctx, event{kind: "decoderExit", exit: exit})
	}()
	return d
}

func (d *decoder) write(b []byte) {
	if d.stdin == nil || d.ended {
		return
	}
	select {
	case d.in <- b:
	default:
	}
}

// stop ends ffmpeg's input; its exit, once it has drained the stream, ends the decoding.
func (d *decoder) stop() {
	if d.ended || d.stdin == nil {
		return
	}
	d.ended = true
	close(d.in)
}

// tail keeps the end of what ffmpeg wrote to stderr.
type tail struct{ b []byte }

func (t *tail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > stderrTail {
		t.b = t.b[len(t.b)-stderrTail:]
	}
	return len(p), nil
}

func (t *tail) String() string { return jscompat.DecodeUTF8(t.b) }
