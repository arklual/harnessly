package engine

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The socket speaks what internal/voice speaks to a recognition service: one connection per dictation, one JSON object
// per line.
//
//	in  {"cmd":"start","lang":"ru"|"en"|"auto"}   {"cmd":"audio","pcm":"<base64 s16le mono 16 kHz>"}   {"cmd":"stop"}
//	out {"type":"preparing","stage":"binary"|"model","percent":N}   the engine is being got ready (first use only)
//	out {"type":"ready"}   {"type":"partial","text":"…"}   {"type":"final","text":"…"}
//	out {"type":"error","message":"…","code":"failed"}
//	in  {"cmd":"status"} -> {"type":"status","ready":bool}   for health checks, no session
//
// Whisper takes a whole recording at a time, so a partial is the whole recording so far recognized again: one every
// couple of seconds while the person speaks, none past the half minute whisper reads at once.

const (
	partialEvery = 2 * time.Second
	partialMax   = 30 * time.Second
	// A recording longer than this is not taken any further.
	pcmMax = 15 * 60 * sampleRate * 2
	// Below this loudness a recording is silence: nothing is asked of the model, which would make words up.
	silenceRMS = 0.002
	finalLimit = 5 * time.Minute
)

// Listen serves the dictation socket at path (a folder of mode 0700 holds it, the socket is the owner's alone) until
// the listener is closed.
func (e *Engine) Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go e.serve(c)
		}
	}()
	return ln, nil
}

type session struct {
	e    *Engine
	conn net.Conn
	ctx  context.Context
	stop context.CancelFunc

	wmu sync.Mutex

	mu       sync.Mutex
	lang     string
	started  bool
	pcm      []byte
	stopping bool
	ready    chan struct{} // closed when the engine is ready
	failed   bool
	pass     context.CancelFunc // the running partial pass
	passDone chan struct{}
}

func (s *session) emit(v map[string]any) {
	b, _ := json.Marshal(v)
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, _ = s.conn.Write(append(b, '\n'))
}

func (s *session) fail(err error) {
	s.mu.Lock()
	if s.failed {
		s.mu.Unlock()
		return
	}
	s.failed = true
	s.mu.Unlock()
	s.emit(map[string]any{"type": "error", "message": err.Error(), "code": "failed"})
	s.stop()
	_ = s.conn.Close()
}

func (e *Engine) serve(c net.Conn) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &session{e: e, conn: c, ctx: ctx, stop: cancel, ready: make(chan struct{})}
	defer func() {
		cancel()
		_ = c.Close()
	}()
	r := bufio.NewReaderSize(c, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var msg struct {
				Cmd  string `json:"cmd"`
				Lang string `json:"lang"`
				PCM  string `json:"pcm"`
			}
			if json.Unmarshal(line, &msg) == nil {
				s.handle(msg.Cmd, msg.Lang, msg.PCM)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *session) handle(cmd, lang, pcm string) {
	switch cmd {
	case "status":
		s.emit(map[string]any{"type": "status", "ready": s.e.Ready()})
	case "start":
		s.mu.Lock()
		if s.started {
			s.mu.Unlock()
			return
		}
		s.started = true
		if lang != "ru" && lang != "en" && lang != "auto" {
			lang = "ru"
		}
		s.lang = lang
		s.mu.Unlock()
		go s.begin()
	case "audio":
		b, err := base64.StdEncoding.DecodeString(pcm)
		if err != nil {
			return
		}
		s.mu.Lock()
		if !s.stopping && len(s.pcm)+len(b) <= pcmMax {
			s.pcm = append(s.pcm, b[:len(b)/2*2]...)
		}
		s.mu.Unlock()
	case "stop":
		s.mu.Lock()
		first := s.started && !s.stopping
		s.stopping = true
		s.mu.Unlock()
		if first {
			go s.final()
		}
	}
}

// begin gets the engine ready, telling the page how far it is, then says ready and runs the partials.
func (s *session) begin() {
	if !s.e.Ready() {
		s.e.Prepare()
		var last Status
		var lastAt time.Time
		for {
			st := s.e.Status()
			if st.State == "failed" {
				s.fail(errors.New(st.Error))
				return
			}
			if st.State == "ready" {
				break
			}
			// A repeat every ten seconds keeps the service's wait for ready from running out on a slow download.
			if st != last || time.Since(lastAt) > 10*time.Second {
				last, lastAt = st, time.Now()
				s.emit(map[string]any{"type": "preparing", "stage": st.Stage, "percent": st.Percent})
			}
			select {
			case <-time.After(500 * time.Millisecond):
			case <-s.ctx.Done():
				return
			}
		}
	}
	close(s.ready)
	s.emit(map[string]any{"type": "ready"})
	s.partials()
}

// partials recognizes the recording so far every few seconds while it is being made.
func (s *session) partials() {
	var lastLen int
	var lastText string
	var spent time.Duration
	var lastEnd time.Time
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		stopping := s.stopping
		n := len(s.pcm)
		due := n-lastLen >= int(partialEvery/time.Second)*sampleRate*2 && n <= int(partialMax/time.Second)*sampleRate*2 &&
			time.Since(lastEnd) >= max(time.Second, spent/2)
		var snap []byte
		var ctx context.Context
		var done chan struct{}
		if !stopping && due {
			snap = append([]byte(nil), s.pcm...)
			var cancel context.CancelFunc
			ctx, cancel = context.WithCancel(s.ctx)
			done = make(chan struct{})
			s.pass, s.passDone = cancel, done
		}
		s.mu.Unlock()
		if stopping {
			return
		}
		if snap == nil {
			continue
		}
		lastLen = len(snap)
		started := time.Now()
		var text string
		var err error
		if rms(snap) >= silenceRMS {
			text, err = s.e.Infer(ctx, snap, s.lang)
		}
		spent, lastEnd = time.Since(started), time.Now()
		close(done)
		if err == nil && text != "" && text != lastText && ctx.Err() == nil {
			lastText = text
			s.emit(map[string]any{"type": "partial", "text": text, "ms": spent.Milliseconds()})
		}
	}
}

// final recognizes the whole recording once it is complete.
func (s *session) final() {
	select {
	case <-s.ready:
	case <-s.ctx.Done():
		return
	}
	s.mu.Lock()
	pass, passDone := s.pass, s.passDone
	pcm := s.pcm
	s.mu.Unlock()
	if pass != nil {
		pass() // a partial still running is of no use now
		<-passDone
	}
	started := time.Now()
	var text string
	if rms(pcm) >= silenceRMS && len(pcm) >= sampleRate/5*2 {
		ctx, cancel := context.WithTimeout(s.ctx, finalLimit)
		defer cancel()
		var err error
		if text, err = s.e.Infer(ctx, pcm, s.lang); err != nil {
			s.fail(err)
			return
		}
	}
	s.emit(map[string]any{"type": "final", "text": text, "ms": time.Since(started).Milliseconds()})
}
