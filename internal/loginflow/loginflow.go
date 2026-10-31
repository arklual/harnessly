// Package loginflow signs the machine's agent CLIs in and out for the page. The CLI does the signing in; the service
// drives it through a terminal of its own and lets the page see only what the person needs: the vendor's link, the
// one-time code of a device flow, and where the flow is.
//
// What it keeps from the CLI's output is the link and that code, taken before anything is typed; after that the output
// is read and thrown away. A code the person pastes back goes to the terminal's input and is dropped; an API key goes
// to a CLI's standard input (never its arguments or environment). Nothing here is logged: not the output, not the code,
// not the key.
package loginflow

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Limits of the service (variables so that a test can shorten them).
var (
	// FlowTimeout is how long a flow lives: the vendors' own codes last 10 to 15 minutes.
	FlowTimeout = 15 * time.Minute
	// CheckTimeout is how long the CLI has to take a pasted code.
	CheckTimeout = 90 * time.Second
	// StartTimeout is how long the CLI has to print its link.
	StartTimeout = 30 * time.Second
	// KeyTimeout bounds one call that hands a key or signs out.
	KeyTimeout = 30 * time.Second
)

const (
	// StartsPer is how many flows a CLI may start in StartWindow.
	StartsPer   = 5
	StartWindow = 10 * time.Minute
	// grabMax is how much output is looked at for the link before the start gives up.
	grabMax = 16 << 10
)

// Config is what the service needs from the machine.
type Config struct {
	// Bin finds the CLI; an error means it is not installed.
	Bin func(p wire.Provider) (string, error)
	// Env is the environment a CLI starts with, built by the caller from the person's own (HOME, PATH, proxies…): the
	// service adds nothing to it and passes on nothing else, so none of the runtime's tokens reach a CLI.
	Env func() []string
	// OnChange is told when an account changed: a flow ended in a sign-in, a key was saved, the CLI signed out.
	OnChange func(p wire.Provider)
	// Now is the clock; nil is the system's.
	Now func() time.Time
}

// Service is the wire's LoginSource over the CLIs.
type Service struct {
	cfg Config

	mu    sync.Mutex
	flows map[wire.Provider]*flow
	log   map[wire.Provider][]time.Time
}

var _ wire.LoginSource = (*Service)(nil)

// New is a service over cfg.
func New(cfg Config) *Service {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Service{cfg: cfg, flows: map[wire.Provider]*flow{}, log: map[wire.Provider][]time.Time{}}
}

type flow struct {
	view wire.LoginFlow
	// in is the terminal's input: set while the flow can take a code.
	term   *os.File
	cancel context.CancelFunc
	// done is closed when the process is reaped.
	done chan struct{}
}

// argv are the arguments of each way of signing in. Fixed here: nothing of the page goes into them.
func argv(p wire.Provider, m wire.LoginMode) ([]string, bool) {
	switch {
	case p == wire.ProviderClaude && m == wire.LoginSubscription:
		return []string{"auth", "login", "--claudeai"}, true
	case p == wire.ProviderClaude && m == wire.LoginConsole:
		return []string{"auth", "login", "--console"}, true
	case p == wire.ProviderCodex && m == wire.LoginDevice:
		return []string{"login", "--device-auth"}, true
	}
	return nil, false
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Service) ms() wire.Ms { return wire.Ms(s.cfg.Now().UnixMilli()) }

func (s *Service) Login(_ context.Context, p wire.Provider) (wire.LoginStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(p), nil
}

func (s *Service) statusLocked(p wire.Provider) wire.LoginStatus {
	f, ok := s.flows[p]
	if !ok {
		return wire.LoginStatus{}
	}
	v := f.view
	return wire.LoginStatus{Flow: &v}
}

func (f *flow) live() bool {
	switch f.view.State {
	case wire.LoginStarting, wire.LoginAwaiting, wire.LoginChecking:
		return true
	}
	return false
}

// StartLogin starts a flow and answers once the CLI has given its link (or failed to), so the page has something to show.
func (s *Service) StartLogin(ctx context.Context, p wire.Provider, mode wire.LoginMode) (wire.LoginStatus, error) {
	args, ok := argv(p, mode)
	if !ok {
		return wire.LoginStatus{}, wire.ErrLoginUnsupported
	}
	bin, err := s.cfg.Bin(p)
	if err != nil {
		return wire.LoginStatus{}, wire.ErrLoginNotInstalled
	}
	s.mu.Lock()
	if f, ok := s.flows[p]; ok && f.live() {
		s.mu.Unlock()
		return wire.LoginStatus{}, wire.ErrLoginBusy
	}
	now := s.cfg.Now()
	recent := s.log[p][:0:0]
	for _, t := range s.log[p] {
		if now.Sub(t) < StartWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= StartsPer {
		s.log[p] = recent
		s.mu.Unlock()
		return wire.LoginStatus{}, wire.ErrLoginRate
	}
	s.log[p] = append(recent, now)

	fctx, cancel := context.WithTimeout(context.Background(), FlowTimeout)
	cmd := exec.CommandContext(fctx, bin, args...)
	cmd.Env = s.cfg.Env()
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	term, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 4000})
	if err != nil {
		cancel()
		s.mu.Unlock()
		return wire.LoginStatus{}, err
	}
	started := s.ms()
	f := &flow{cancel: cancel, term: term, done: make(chan struct{}),
		view: wire.LoginFlow{ID: newID(), Provider: p, Mode: mode, State: wire.LoginStarting, Input: inputOf(mode),
			StartedAt: started, ExpiresAt: started + wire.Ms(FlowTimeout.Milliseconds())}}
	s.flows[p] = f
	s.mu.Unlock()

	linked := make(chan struct{})
	go s.run(fctx, f, cmd, linked)

	select {
	case <-linked:
	case <-time.After(StartTimeout):
		s.finish(f, wire.LoginFailed, wire.LoginStartFailed)
		f.cancel()
	case <-ctx.Done():
		s.finish(f, wire.LoginCancelled, "")
		f.cancel()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(p), nil
}

func inputOf(m wire.LoginMode) wire.LoginInput {
	if m == wire.LoginDevice {
		return wire.LoginInputNone
	}
	return wire.LoginInputCode
}

// run reads the CLI's output until it has the link, then reads and drops the rest while it waits for the process.
func (s *Service) run(ctx context.Context, f *flow, cmd *exec.Cmd, linked chan struct{}) {
	defer close(f.done)
	defer f.cancel()
	var once sync.Once
	signal := func() { once.Do(func() { close(linked) }) }
	defer signal()

	grabbed := make(chan struct{})
	go func() {
		defer close(grabbed)
		s.grab(f, signal)
	}()
	waitErr := cmd.Wait()
	_ = f.term.Close()
	<-grabbed

	s.mu.Lock()
	defer s.mu.Unlock()
	f.term = nil
	if !f.live() {
		return
	}
	switch {
	case waitErr == nil:
		f.view.State = wire.LoginDone
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		f.view.State = wire.LoginExpired
	case f.view.State == wire.LoginChecking:
		f.view.State, f.view.Reason = wire.LoginFailed, ptr(wire.LoginCodeRejected)
	case f.view.State == wire.LoginAwaiting:
		f.view.State, f.view.Reason = wire.LoginFailed, ptr(wire.LoginRefused)
	default:
		f.view.State, f.view.Reason = wire.LoginFailed, ptr(wire.LoginStartFailed)
	}
	if f.view.State == wire.LoginDone && s.cfg.OnChange != nil {
		go s.cfg.OnChange(f.view.Provider)
	}
}

func ptr[T any](v T) *T { return &v }

// grab reads the terminal: until the link (and, for a device flow, its code) is found, what is read is looked through
// for them and kept at most grabMax long; after that it is only read and dropped, so the CLI never blocks on a full
// terminal. The buffer is gone with the link.
func (s *Service) grab(f *flow, linked func()) {
	var buf []byte
	chunk := make([]byte, 4096)
	found := false
	for {
		n, err := f.term.Read(chunk)
		if n > 0 && !found {
			buf = append(buf, chunk[:n]...)
			if len(buf) > grabMax {
				buf = buf[len(buf)-grabMax:]
			}
			if url, code, ok := Scan(f.view.Provider, f.view.Mode, buf); ok {
				found = true
				buf = nil
				s.mu.Lock()
				if f.view.State == wire.LoginStarting {
					f.view.State, f.view.URL, f.view.UserCode = wire.LoginAwaiting, &url, code
				}
				s.mu.Unlock()
				linked()
			}
		}
		if err != nil {
			return
		}
	}
}

// The links and the code a CLI prints. Only a link on the vendor's own hosts is taken, so whatever else the CLI (or a
// proxy page) prints can not turn into a link the page shows.
var (
	ansiRE   = regexp.MustCompile(`\x1b\[[0-9;?<>=]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[()][A-Za-z0-9]|\x1b[=>78c]`)
	osc8RE   = regexp.MustCompile(`\x1b\]8;[^;\x07\x1b]*;(https://[^\x07\x1b\s]+)(?:\x07|\x1b\\)`)
	claudeRE = regexp.MustCompile(`https://(?:claude\.com|claude\.ai|platform\.claude\.com|console\.anthropic\.com)/[^\s\x07\x1b"'<>]+`)
	codexRE  = regexp.MustCompile(`https://auth\.openai\.com/[^\s\x07\x1b"'<>]+`)
	codeRE   = regexp.MustCompile(`(?m)^\s*([A-Z0-9]{4}-[A-Z0-9]{4,6})\s*$`)
)

// MaxLinkLen bounds a link: the longest the vendors print is under 1 KB.
const MaxLinkLen = 2048

// Scan looks through the terminal output for the link of a flow and, for a device flow, its one-time code; ok is false
// until all the flow needs is in the output.
func Scan(p wire.Provider, mode wire.LoginMode, out []byte) (url string, code *string, ok bool) {
	var linkRE *regexp.Regexp
	switch p {
	case wire.ProviderClaude:
		linkRE = claudeRE
	case wire.ProviderCodex:
		linkRE = codexRE
	default:
		return "", nil, false
	}
	for _, m := range osc8RE.FindAllSubmatch(out, -1) {
		if linkRE.Match(m[1]) && linkRE.FindString(string(m[1])) == string(m[1]) {
			url = string(m[1])
			break
		}
	}
	plain := ansiRE.ReplaceAll(bytes.ReplaceAll(out, []byte("\r"), nil), nil)
	if url == "" {
		url = linkRE.FindString(string(plain))
	}
	if url == "" || len(url) > MaxLinkLen {
		return "", nil, false
	}
	if mode == wire.LoginDevice {
		after := plain[strings.Index(string(plain), url)+len(url):]
		m := codeRE.FindSubmatch(after)
		if m == nil {
			return "", nil, false
		}
		c := string(m[1])
		code = &c
	}
	return url, code, true
}

// SubmitLoginCode types the code into the CLI's terminal. The code is checked again here for what a terminal would take
// as more than text, and is dropped when it has been written.
func (s *Service) SubmitLoginCode(_ context.Context, p wire.Provider, id, code string) (wire.LoginStatus, error) {
	if err := (wire.LoginCode{ID: id, Code: code}).Validate(); err != nil {
		return wire.LoginStatus{}, wire.ErrLoginInput
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.flows[p]
	if !ok || f.view.ID != id || f.view.State != wire.LoginAwaiting || f.view.Input != wire.LoginInputCode || f.term == nil {
		return wire.LoginStatus{}, wire.ErrLoginStale
	}
	_ = f.term.SetWriteDeadline(s.cfg.Now().Add(5 * time.Second))
	if _, err := f.term.Write([]byte(code + "\r")); err != nil {
		return wire.LoginStatus{}, wire.ErrLoginStale
	}
	f.view.State = wire.LoginChecking
	go s.watchCheck(f)
	return s.statusLocked(p), nil
}

// watchCheck gives up on a CLI that does not answer a code.
func (s *Service) watchCheck(f *flow) {
	select {
	case <-f.done:
	case <-time.After(CheckTimeout):
		s.mu.Lock()
		if f.view.State == wire.LoginChecking {
			f.view.State, f.view.Reason = wire.LoginFailed, ptr(wire.LoginTimeout)
		}
		s.mu.Unlock()
		f.cancel()
	}
}

// CancelLogin ends the flow of a CLI that is on.
func (s *Service) CancelLogin(_ context.Context, p wire.Provider) (wire.LoginStatus, error) {
	s.mu.Lock()
	f, ok := s.flows[p]
	if ok && f.live() {
		f.view.State = wire.LoginCancelled
		f.view.URL, f.view.UserCode = nil, nil
	}
	s.mu.Unlock()
	if ok {
		f.cancel()
		select {
		case <-f.done:
		case <-time.After(3 * time.Second):
		}
	}
	return s.Login(context.Background(), p)
}

// finish ends a flow that is on with a state.
func (s *Service) finish(f *flow, st wire.LoginState, reason wire.LoginReason) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !f.live() {
		return
	}
	f.view.State = st
	if reason != "" {
		f.view.Reason = &reason
	}
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// The terminal made the CLI a session leader: the group takes the helpers it started with it.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}

// SaveAPIKey hands an API key to the CLI on its standard input: Codex reads it there and writes it into its own file.
// Claude reads none that way.
func (s *Service) SaveAPIKey(ctx context.Context, p wire.Provider, key string) error {
	if p != wire.ProviderCodex {
		return wire.ErrLoginUnsupported
	}
	if err := (wire.ProfileKeyEdit{Key: key}).Validate(); err != nil {
		return wire.ErrLoginInput
	}
	s.mu.Lock()
	if f, ok := s.flows[p]; ok && f.live() {
		s.mu.Unlock()
		return wire.ErrLoginBusy
	}
	s.mu.Unlock()
	if err := s.once(ctx, p, []string{"login", "--with-api-key"}, strings.NewReader(key+"\n")); err != nil {
		if errors.Is(err, wire.ErrLoginNotInstalled) {
			return err
		}
		return wire.ErrLoginInput
	}
	s.changed(p)
	return nil
}

// Logout signs the CLI out; a flow that is on ends first.
func (s *Service) Logout(ctx context.Context, p wire.Provider) error {
	var args []string
	switch p {
	case wire.ProviderClaude:
		args = []string{"auth", "logout"}
	case wire.ProviderCodex:
		args = []string{"logout"}
	default:
		return wire.ErrLoginUnsupported
	}
	if _, err := s.CancelLogin(ctx, p); err != nil {
		return err
	}
	if err := s.once(ctx, p, args, nil); err != nil {
		return err
	}
	s.changed(p)
	return nil
}

func (s *Service) changed(p wire.Provider) {
	if s.cfg.OnChange != nil {
		s.cfg.OnChange(p)
	}
}

// once runs a CLI to its end with fixed arguments, its input from in and its output thrown away: the exit status is
// all that is read.
func (s *Service) once(ctx context.Context, p wire.Provider, args []string, in io.Reader) error {
	bin, err := s.cfg.Bin(p)
	if err != nil {
		return wire.ErrLoginNotInstalled
	}
	ctx, cancel := context.WithTimeout(ctx, KeyTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = s.cfg.Env()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = in, io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killGroup(cmd) }
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}
