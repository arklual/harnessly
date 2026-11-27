package connect

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// DeviceConfig is what pairing left on the device.
type DeviceConfig struct {
	URL  string             `json:"url"`
	ID   string             `json:"id"`
	Name string             `json:"name"`
	Key  ed25519.PrivateKey `json:"key"`
}

// keepDone is how long a finished command's output waits for a runtime that never came back for it.
const keepDone = time.Hour

const (
	chunk        = 64 << 10
	pingEvery    = 15 * time.Second
	pingTimeout  = 30 * time.Second
	maxFileBytes = 4 << 20
)

// Device is the laptop's side: it dials the runtime, keeps dialling, and runs what the runtime asks.
type Device struct {
	Cfg     DeviceConfig
	Version string
	Logf    func(format string, a ...any)
	// Status, when set, hears every change of the link: online or not, and why.
	Status func(online bool, why string)

	mu    sync.Mutex
	procs map[uint64]*proc
	link  atomic.Pointer[devLink]
	gen   atomic.Uint64

	envOnce sync.Once
	baseEnv []string
	shell   string
	tmp     string

	shimURL, shimKey, shimBin string
	shimWait                  *shimWait
}

type outFrame struct {
	binary bool
	b      []byte
}

type devLink struct {
	ws   *websocket.Conn
	out  chan outFrame
	done chan struct{}
	once sync.Once
	gen  uint64
}

func (l *devLink) close() { l.once.Do(func() { close(l.done) }) }

// send queues a frame; false when the link is gone.
func (l *devLink) send(f outFrame) bool {
	select {
	case l.out <- f:
		return true
	case <-l.done:
		return false
	}
}

func (l *devLink) sendMsg(m Msg) bool {
	b, _ := json.Marshal(m)
	return l.send(outFrame{b: b})
}

func (d *Device) logf(format string, a ...any) {
	if d.Logf != nil {
		d.Logf(format, a...)
	}
}

func (d *Device) status(online bool, why string) {
	if d.Status != nil {
		d.Status(online, why)
	}
}

// DialURL is the runtime's device endpoint for a runtime address.
func DialURL(base string) string {
	base = strings.TrimRight(base, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base + "/connect/v1/dial"
}

// Run keeps the device linked until ctx ends; then every command it holds is killed.
func (d *Device) Run(ctx context.Context) error {
	d.prepare()
	defer d.KillAll()
	go d.reaper(ctx)
	fails := 0
	for ctx.Err() == nil {
		up, err := d.dialOnce(ctx)
		if ctx.Err() != nil {
			break
		}
		var refused refusal
		if errors.As(err, &refused) && refused.final {
			d.status(false, refused.Error())
			return err
		}
		if up {
			fails = 0
		}
		fails++
		wait := backoff(fails)
		why := "нет связи"
		if err != nil {
			why = err.Error()
		}
		d.status(false, why)
		d.logf("link down (%v), next try in %s", err, wait.Round(time.Millisecond))
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
	return ctx.Err()
}

// backoff is full jitter from 1 s up to 30 s.
func backoff(fails int) time.Duration {
	ceil := time.Second << min(fails, 5)
	ceil = min(ceil, 30*time.Second)
	return time.Second/2 + time.Duration(rand.Int64N(int64(ceil)))
}

type refusal struct {
	msg   string
	final bool
}

func (r refusal) Error() string { return r.msg }

func (d *Device) dialOnce(ctx context.Context) (bool, error) {
	u := DialURL(d.Cfg.URL)
	host, err := hubproto.DialHost(u)
	if err != nil {
		return false, err
	}
	auth, err := hubproto.SignDial(d.Cfg.Key, d.Cfg.ID, time.Now(), host)
	if err != nil {
		return false, err
	}
	dctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	ws, resp, err := websocket.Dial(dctx, u, &websocket.DialOptions{
		HTTPHeader:      http.Header{"Authorization": {auth}, "X-Connect-Proto": {strconv.Itoa(Protocol)}},
		CompressionMode: websocket.CompressionDisabled,
	})
	cancel()
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusForbidden:
				return false, refusal{"устройство отозвано в harnessly: подключите его заново", true}
			case http.StatusUnauthorized:
				return false, refusal{"runtime не узнал устройство (или часы ноутбука сильно ушли)", false}
			case http.StatusUpgradeRequired:
				return false, refusal{"версия Connect не подходит к runtime: обновите приложение", true}
			}
		}
		return false, err
	}
	ws.SetReadLimit(64 << 20)
	l := &devLink{ws: ws, out: make(chan outFrame, 256), done: make(chan struct{}), gen: d.gen.Add(1)}
	lctx, lcancel := context.WithCancel(ctx)
	defer lcancel()
	go d.writer(lctx, l)
	go d.pinger(lctx, l)
	l.sendMsg(Msg{T: "hello", Name: d.Cfg.Name, OS: runtime.GOOS, Arch: runtime.GOARCH, Version: d.Version, Proto: Protocol,
		SIDs: d.sids()})
	d.link.Store(l)
	d.status(true, "")
	d.logf("linked to %s", d.Cfg.URL)
	err = d.reader(lctx, l)
	d.link.CompareAndSwap(l, nil)
	l.close()
	ws.CloseNow()
	return true, err
}

func (d *Device) writer(ctx context.Context, l *devLink) {
	for {
		select {
		case <-l.done:
			return
		case f := <-l.out:
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			typ := websocket.MessageText
			if f.binary {
				typ = websocket.MessageBinary
			}
			err := l.ws.Write(wctx, typ, f.b)
			cancel()
			if err != nil {
				l.close()
				l.ws.CloseNow()
				return
			}
		}
	}
}

func (d *Device) pinger(ctx context.Context, l *devLink) {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := l.ws.Ping(pctx)
			cancel()
			if err != nil {
				l.close()
				l.ws.CloseNow()
				return
			}
		}
	}
}

func (d *Device) reader(ctx context.Context, l *devLink) error {
	for {
		typ, b, err := l.ws.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == statusRevoked {
				return refusal{"устройство отозвано в harnessly", true}
			}
			return err
		}
		if typ != websocket.MessageText {
			continue
		}
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		d.handle(l, m)
	}
}

// statusRevoked closes the link of a device revoked while it was online.
const statusRevoked websocket.StatusCode = 4010

func (d *Device) handle(l *devLink, m Msg) {
	id, _ := strconv.ParseUint(m.SID, 10, 64)
	switch m.T {
	case "exec":
		d.start(l, id, m)
	case "attach":
		if p := d.proc(id); p != nil {
			p.attach(l.gen, m.Out, m.Err)
		} else {
			l.sendMsg(Msg{T: "gone", SID: m.SID})
		}
	case "stdin":
		if p := d.proc(id); p != nil {
			p.input(m.Data, m.EOF)
		}
	case "signal":
		if p := d.proc(id); p != nil {
			p.signal(m.Sig)
		}
	case "fs":
		if m.FS != nil {
			go func() { l.sendMsg(Msg{T: "fsr", SID: m.SID, FSR: d.fsOp(m.FS)}) }()
		}
	case "clir", "secretsr":
		d.shimAnswer(m)
	case "release":
		d.mu.Lock()
		p := d.procs[id]
		delete(d.procs, id)
		d.mu.Unlock()
		if p != nil {
			p.signal("KILL")
			p.stop()
		}
	}
}

func (d *Device) proc(id uint64) *proc {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.procs[id]
}

func (d *Device) sids() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]string, 0, len(d.procs))
	for id := range d.procs {
		out = append(out, strconv.FormatUint(id, 10))
	}
	return out
}

// KillAll kills every command the device holds and forgets them: the tray's «Отключить».
// The runtime is told each one's exit when the link is up, so the agent sees its commands end rather than hang.
func (d *Device) KillAll() int {
	d.mu.Lock()
	ps := make([]*proc, 0, len(d.procs))
	for _, p := range d.procs {
		ps = append(ps, p)
	}
	d.mu.Unlock()
	for _, p := range ps {
		p.signal("KILL")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && d.link.Load() != nil {
		told := true
		for _, p := range ps {
			p.mu.Lock()
			told = told && p.exitSent
			p.mu.Unlock()
		}
		if told {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	d.mu.Lock()
	for _, p := range ps {
		delete(d.procs, p.id)
		p.stop()
	}
	d.mu.Unlock()
	return len(ps)
}

// Running is how many commands are running now.
func (d *Device) Running() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, p := range d.procs {
		if !p.finished() {
			n++
		}
	}
	return n
}

func (d *Device) reaper(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.mu.Lock()
			for id, p := range d.procs {
				if at := p.finishedAt(); !at.IsZero() && time.Since(at) > keepDone {
					delete(d.procs, id)
					p.stop()
				}
			}
			d.mu.Unlock()
		}
	}
}

// prepare finds the shell, the folder for mirrored files, and the environment of a login shell: a desktop app is
// started with a bare environment, and the person's PATH (nvm, pyenv, brew) lives in their shell's profile.
func (d *Device) prepare() {
	d.envOnce.Do(func() {
		if d.procs == nil {
			d.procs = map[uint64]*proc{}
		}
		d.shell = findShell()
		d.tmp = filepath.Join(os.TempDir(), "harnessly-connect-"+d.Cfg.ID)
		_ = os.MkdirAll(d.tmp, 0o700)
		d.baseEnv = loginEnv(d.shell)
		d.startShim()
		d.logf("shell %s, %d env vars", d.shell, len(d.baseEnv))
	})
}

func loginEnv(shell string) []string {
	if shell == "" {
		return os.Environ()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-c", "env -0")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return os.Environ()
	}
	var env []string
	for _, kv := range strings.Split(string(out), "\x00") {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || k == "_" || k == "SHLVL" || k == "PWD" || k == "OLDPWD" {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func mergeEnv(base []string, over map[string]string) []string {
	out := make([]string, 0, len(base)+len(over))
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := over[k]; !ok {
			out = append(out, kv)
		}
	}
	for k, v := range over {
		out = append(out, k+"="+v)
	}
	return out
}

// local turns a path carrying TmpToken into the device's own.
func (d *Device) local(p string) string {
	return strings.ReplaceAll(p, TmpToken, toShellPath(d.tmp))
}

func (d *Device) start(l *devLink, id uint64, m Msg) {
	p := newProc(id, l.gen)
	d.mu.Lock()
	if old := d.procs[id]; old != nil {
		d.mu.Unlock()
		old.attach(l.gen, 0, 0)
		return
	}
	d.procs[id] = p
	d.mu.Unlock()
	go p.pump(d)

	fail := func(msg string) {
		p.bufs[1].write([]byte("harnessly connect: " + msg + "\n"))
		p.finish(1, nil)
	}
	if d.shell == "" {
		fail(shellMissing())
		return
	}
	cwd := fromShellPath(m.Cwd)
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		fail(fmt.Sprintf("на устройстве %q нет папки %s", d.Cfg.Name, m.Cwd))
		return
	}
	for _, f := range m.FilesIn {
		path := fromShellPath(d.local(f.Path))
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		if err := os.WriteFile(path, f.Data, 0o600); err != nil {
			fail("не записать " + path + ": " + err.Error())
			return
		}
	}
	args := make([]string, len(m.Args))
	for i, a := range m.Args {
		args[i] = d.local(a)
	}
	cmd := exec.Command(d.shell, args...)
	cmd.Dir = cwd
	vars := map[string]string{}
	for k, v := range m.Env {
		vars[k] = v
	}
	for k, v := range d.shimEnv(m.SID) {
		vars[k] = v
	}
	cmd.Env = d.withShimPath(mergeEnv(d.baseEnv, vars))
	var outW, errW io.Writer = writerFunc(p.bufs[0].write), writerFunc(p.bufs[1].write)
	flush := func() {}
	if runtime.GOOS == "windows" {
		// The agent sees Git Bash's paths; what Windows programs print is put in that form too.
		ro, re := &lineRewriter{out: p.bufs[0].write}, &lineRewriter{out: p.bufs[1].write}
		outW, errW = ro, re
		flush = func() { ro.flush(); re.flush() }
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	stdin, _ := cmd.StdinPipe()
	setGroup(cmd)
	if err := cmd.Start(); err != nil {
		fail("не запустить " + d.shell + ": " + err.Error())
		return
	}
	p.setCmd(cmd, stdin)
	d.logf("exec %d in %s", id, m.Cwd)
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			code = 1
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = exitCode(ee)
			}
		}
		flush()
		var files []File
		for _, path := range m.FilesOut {
			raw, err := os.ReadFile(fromShellPath(d.local(path)))
			if err == nil && len(raw) <= maxFileBytes {
				files = append(files, File{Path: path, Data: raw})
			}
		}
		p.finish(code, files)
		d.logf("exit %d: %d", id, code)
	}()
}

type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

// proc is one command the device holds.
type proc struct {
	id    uint64
	bufs  [2]*obuf
	wake  chan struct{}
	quit  chan struct{}
	qonce sync.Once

	mu       sync.Mutex
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	pending  []string // signals that came before the process started
	done     bool
	doneAt   time.Time
	code     int
	files    []File
	gen      uint64 // the link it sends on
	sent     [2]int64
	exitSent bool
}

func newProc(id, gen uint64) *proc {
	p := &proc{id: id, bufs: [2]*obuf{{}, {}}, wake: make(chan struct{}, 1), quit: make(chan struct{}), gen: gen}
	return p
}

func (p *proc) poke() {
	select {
	case p.wake <- struct{}{}:
	default:
	}
}

func (p *proc) stop() { p.qonce.Do(func() { close(p.quit) }) }

func (p *proc) setCmd(cmd *exec.Cmd, stdin io.WriteCloser) {
	p.mu.Lock()
	p.cmd, p.stdin = cmd, stdin
	pending := p.pending
	p.pending = nil
	p.mu.Unlock()
	for _, s := range pending {
		p.signal(s)
	}
}

func (p *proc) finish(code int, files []File) {
	p.mu.Lock()
	p.done, p.doneAt, p.code, p.files = true, time.Now(), code, files
	p.mu.Unlock()
	p.poke()
}

func (p *proc) finished() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *proc) finishedAt() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.doneAt
}

func (p *proc) attach(gen uint64, out, errOff int64) {
	p.mu.Lock()
	p.gen, p.sent, p.exitSent = gen, [2]int64{out, errOff}, false
	p.mu.Unlock()
	p.poke()
}

func (p *proc) input(data []byte, eof bool) {
	p.mu.Lock()
	in := p.stdin
	p.mu.Unlock()
	if in == nil {
		return
	}
	if len(data) > 0 {
		_, _ = in.Write(data)
	}
	if eof {
		_ = in.Close()
	}
}

func (p *proc) signal(sig string) {
	p.mu.Lock()
	cmd, done := p.cmd, p.done
	if cmd == nil && !done {
		p.pending = append(p.pending, sig)
	}
	p.mu.Unlock()
	if cmd != nil && !done {
		signalGroup(cmd, sig)
	}
}

// pump sends the command's output to the link it is attached to, from the offsets the runtime has, then its exit.
func (p *proc) pump(d *Device) {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-p.quit:
			return
		case <-p.wake:
		case <-tick.C:
		}
		for p.flush(d) {
		}
	}
}

// flush sends one round; true when there may be more to send at once.
func (p *proc) flush(d *Device) bool {
	l := d.link.Load()
	p.mu.Lock()
	gen, sent, done, exitSent := p.gen, p.sent, p.done, p.exitSent
	p.mu.Unlock()
	if l == nil || l.gen != gen || exitSent {
		return false
	}
	more := false
	for i, kind := range []byte{KindStdout, KindStderr} {
		off, b := p.bufs[i].read(sent[i], chunk)
		if len(b) == 0 {
			continue
		}
		if !l.send(outFrame{binary: true, b: EncodeData(kind, p.id, off, b)}) {
			return false
		}
		p.mu.Lock()
		if p.gen == gen {
			p.sent[i] = off + int64(len(b))
		}
		p.mu.Unlock()
		more = true
	}
	if more || !done {
		return more
	}
	if p.bufs[0].end() > sent[0] || p.bufs[1].end() > sent[1] {
		return true
	}
	p.mu.Lock()
	code, files := p.code, p.files
	p.mu.Unlock()
	if l.sendMsg(Msg{T: "exit", SID: strconv.FormatUint(p.id, 10), Code: code, Files: files}) {
		p.mu.Lock()
		if p.gen == gen {
			p.exitSent = true
		}
		p.mu.Unlock()
	}
	return false
}
