package mcpconn

// Local servers: an MCP server that is a program of this machine, spoken to over stdin/stdout, started by
// the runtime with the person's keys in its environment. Agents reach it through the same proxy as a remote one: the
// proxy hands each JSON-RPC message of the agent here, and the answer goes back as a plain JSON answer of streamable
// HTTP. One process serves every chat of the machine: the runtime makes the initialize handshake itself once, answers
// the agents' own initialize from it, and gives every request an id of its own so two chats' "id 1" never meet. A
// process nobody called for a while is stopped; the next call starts it again.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// localIdle is how long a local server lives with no call.
	localIdle = 15 * time.Minute
	// localStart bounds starting a server and its handshake: the first npx or uvx run downloads the package.
	localStart = 150 * time.Second
	// localLineMax is the longest line a server may write: one JSON-RPC message.
	localLineMax = 32 << 20
)

// localSpec is what starts one local server.
type localSpec struct {
	command []string
	env     map[string]string
	dir     string
}

// key changes when anything that started the process changes: new keys restart it.
func (sp localSpec) key() string {
	h := sha256.New()
	for _, c := range sp.command {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	names := make([]string, 0, len(sp.env))
	for k := range sp.env {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		h.Write([]byte(k + "=" + sp.env[k]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ErrNoRuntime: the program that runs the server (npx, uvx) is not on this machine.
var ErrNoRuntime = errors.New("no-runtime")

// errBroken: the server's package does not run (a module missing, no such version): new keys will not help.
var errBroken = fmt.Errorf("%w: the package does not run", ErrUnreachable)

// brokenRe finds a package that cannot run in what a server wrote to stderr before it stopped.
var brokenRe = regexp.MustCompile(`ERR_MODULE_NOT_FOUND|Cannot find module|SyntaxError|No solution found|No matching distribution|npm ERR! (code E404|404)|npm error (code E404|404)|ModuleNotFoundError|ImportError|is not in the npm registry|Unsupported engine`)

// stderrSays tells what the server's stderr says about why it stopped, in our words only (its own may carry keys).
func (p *localProc) stderrSays() error {
	p.stderr.mu.Lock()
	tail := string(p.stderr.buf)
	p.stderr.mu.Unlock()
	if brokenRe.MatchString(tail) {
		return errBroken
	}
	return nil
}

// lookCommand finds the program: on PATH, then where installers put it (the runtime may run with a short PATH).
func lookCommand(name string) (string, error) {
	if strings.ContainsRune(name, filepath.Separator) {
		if st, err := os.Stat(name); err == nil && !st.IsDir() {
			return name, nil
		}
		return "", fmt.Errorf("%w: %s", ErrNoRuntime, filepath.Base(name))
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	if p, ok := findInstalled(name); ok {
		return p, nil
	}
	if (name == "uvx" || name == "uv") && installUV() {
		if p, ok := findInstalled(name); ok {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrNoRuntime, name)
}

var (
	uvOnce sync.Once
	uvOK   bool
)

// installUV puts uv (and uvx) in ~/.local/bin once, by Astral's installer, for the Python servers of the catalog: a
// machine that runs agents has curl and sh, and uv is one static binary of the person's own.
func installUV() bool {
	uvOnce.Do(func() {
		if _, err := exec.LookPath("sh"); err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "sh", "-c", "curl -LsSf https://astral.sh/uv/install.sh | sh")
		cmd.Env = append(os.Environ(), "UV_NO_MODIFY_PATH=1", "INSTALLER_NO_MODIFY_PATH=1")
		uvOK = cmd.Run() == nil
	})
	return uvOK
}

func findInstalled(name string) (string, bool) {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".volta", "bin"), "/usr/local/bin", "/opt/homebrew/bin", "/usr/bin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// localEnv is the environment of a server: only what a program needs to run and the server's own keys, never the
// runtime's (its tokens).
func localEnv(sp localSpec) []string {
	var env []string
	for _, k := range []string{"PATH", "HOME", "USER", "LANG", "LC_ALL", "TMPDIR", "TZ", "SSL_CERT_FILE", "SSL_CERT_DIR",
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "NODE_EXTRA_CA_CERTS",
		"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "SYSTEMROOT", "APPDATA", "LOCALAPPDATA", "USERPROFILE"} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	home, _ := os.UserHomeDir()
	extra := []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, ".cargo", "bin"), "/usr/local/bin"}
	env = append(env, "PATH="+strings.Join(append([]string{os.Getenv("PATH")}, extra...), string(os.PathListSeparator)),
		"npm_config_yes=true", "npm_config_update_notifier=false", "npm_config_fund=false", "NO_UPDATE_NOTIFIER=1",
		"PYTHONUNBUFFERED=1", "UV_NO_PROGRESS=1")
	for k, v := range sp.env {
		env = append(env, k+"="+v)
	}
	return env
}

// rpcIn is one JSON-RPC message as it comes, either way.
type rpcIn struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

func (m rpcIn) hasID() bool { return len(m.ID) > 0 && string(m.ID) != "null" }

// localProc is one running server.
type localProc struct {
	key  string
	cmd  *exec.Cmd
	in   io.WriteCloser
	wmu  sync.Mutex
	next atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan rpcIn
	used    time.Time
	dead    bool
	done    chan struct{}

	// init is the server's answer to the runtime's initialize, given to every agent's initialize.
	init json.RawMessage
	// stderr keeps the last lines the server wrote there, for the runtime's log only: they may carry its keys.
	stderr tailBuffer
}

// startLocal starts the server and makes the handshake.
func startLocal(ctx context.Context, sp localSpec) (*localProc, error) {
	if len(sp.command) == 0 {
		return nil, fmt.Errorf("%w: no command", ErrBadRequest)
	}
	path, err := lookCommand(sp.command[0])
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, sp.command[1:]...)
	cmd.Env = localEnv(sp)
	if sp.dir != "" && os.MkdirAll(sp.dir, 0o700) == nil {
		cmd.Dir = sp.dir
	}
	setProcGroup(cmd)
	p := &localProc{key: sp.key(), cmd: cmd, pending: map[int64]chan rpcIn{}, used: time.Now(), done: make(chan struct{})}
	cmd.Stderr = &p.stderr
	if p.in, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	go p.read(out)
	go func() {
		_ = cmd.Wait()
		p.die()
	}()
	res, err := p.request(ctx, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "harnessly", "version": "1"}})
	if err != nil {
		p.stop()
		if why := p.stderrSays(); why != nil {
			return nil, why
		}
		return nil, err
	}
	p.init = res
	_ = p.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	return p, nil
}

func (p *localProc) read(out io.Reader) {
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), localLineMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] != '{' {
			continue // a server that prints a banner to stdout
		}
		var m rpcIn
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		switch {
		case m.Method != "" && m.hasID():
			// The server asks the client (sampling, roots, elicitation): we offered none of it.
			_ = p.write(map[string]any{"jsonrpc": "2.0", "id": m.ID,
				"error": map[string]any{"code": -32601, "message": "not supported by this client"}})
		case m.Method != "":
			// A notification (log, progress, list_changed): agents learn the tools at their next start.
		case m.hasID():
			var id int64
			if json.Unmarshal(m.ID, &id) != nil {
				continue
			}
			p.mu.Lock()
			ch := p.pending[id]
			delete(p.pending, id)
			p.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	p.stop()
}

func (p *localProc) die() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dead {
		return
	}
	p.dead = true
	close(p.done)
	for id, ch := range p.pending {
		close(ch)
		delete(p.pending, id)
	}
}

func (p *localProc) alive() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return !p.dead
}

func (p *localProc) stop() {
	_ = p.in.Close()
	killProcGroup(p.cmd)
	p.die()
}

func (p *localProc) write(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.in.Write(append(raw, '\n'))
	return err
}

// send writes one request with an id of the runtime's own and waits for its answer.
func (p *localProc) send(ctx context.Context, method string, params json.RawMessage) (rpcIn, error) {
	id := p.next.Add(1)
	ch := make(chan rpcIn, 1)
	p.mu.Lock()
	if p.dead {
		p.mu.Unlock()
		return rpcIn{}, fmt.Errorf("%w: the server stopped", ErrUnreachable)
	}
	p.pending[id] = ch
	p.used = time.Now()
	p.mu.Unlock()
	msg := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if len(params) > 0 {
		msg["params"] = params
	}
	if err := p.write(msg); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return rpcIn{}, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return rpcIn{}, fmt.Errorf("%w: the server stopped", ErrUnreachable)
		}
		p.mu.Lock()
		p.used = time.Now()
		p.mu.Unlock()
		return m, nil
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		_ = p.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled",
			"params": map[string]any{"requestId": id, "reason": "the client gave up"}})
		return rpcIn{}, fmt.Errorf("%w: %v", ErrUnreachable, ctx.Err())
	}
}

// request is send for the runtime's own calls: the result, or the server's error as ErrUnreachable.
func (p *localProc) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	var raw json.RawMessage
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	m, err := p.send(ctx, method, raw)
	if err != nil {
		return nil, err
	}
	if len(m.Error) > 0 && string(m.Error) != "null" {
		return nil, fmt.Errorf("%w: %s failed", ErrUnreachable, method)
	}
	return m.Result, nil
}

// tools is tools/list of the process, every page.
func (p *localProc) tools(ctx context.Context) ([]Tool, error) {
	var out []Tool
	cursor := ""
	for range 20 {
		var params any
		if cursor != "" {
			params = map[string]any{"cursor": cursor}
		}
		res, err := p.request(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &page); err != nil {
			return nil, fmt.Errorf("%w: tools/list", ErrUnreachable)
		}
		for _, t := range page.Tools {
			out = append(out, Tool{Name: t.Name, Description: oneLine(t.Description)})
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	return out, nil
}

// answer is the agent's message answered by the process: the JSON to send back, nil for a notification.
func (p *localProc) answer(ctx context.Context, m rpcIn) (json.RawMessage, error) {
	reply := func(field string, v any) (json.RawMessage, error) {
		return json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, field: v})
	}
	if !m.hasID() {
		switch m.Method {
		case "notifications/initialized", "notifications/cancelled", "":
			// The handshake is the runtime's; a cancel names the agent's id, which the server never saw.
		default:
			_ = p.write(map[string]any{"jsonrpc": "2.0", "method": m.Method, "params": m.Params})
		}
		return nil, nil
	}
	switch m.Method {
	case "initialize":
		return reply("result", p.init)
	case "ping":
		return reply("result", map[string]any{})
	case "":
		return nil, nil // an answer of the agent to a request the server never sent through us
	}
	got, err := p.send(ctx, m.Method, m.Params)
	if err != nil {
		return reply("error", map[string]any{"code": -32000, "message": "the local server did not answer"})
	}
	if len(got.Error) > 0 && string(got.Error) != "null" {
		return reply("error", got.Error)
	}
	if len(got.Result) == 0 {
		got.Result = json.RawMessage("{}")
	}
	return reply("result", got.Result)
}

// localRunner keeps the running servers of the machine, by server id.
type localRunner struct {
	mu    sync.Mutex
	procs map[string]*localProc
	// starting serializes the start of one server.
	starting map[string]*sync.Mutex
	idle     time.Duration
}

var runner = &localRunner{procs: map[string]*localProc{}, starting: map[string]*sync.Mutex{}, idle: localIdle}

func (r *localRunner) startLock(id string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	l := r.starting[id]
	if l == nil {
		l = &sync.Mutex{}
		r.starting[id] = l
	}
	return l
}

// get is the running process of id started as sp, starting it (again) when it is not running or sp changed.
func (r *localRunner) get(ctx context.Context, id string, sp localSpec) (*localProc, error) {
	l := r.startLock(id)
	l.Lock()
	defer l.Unlock()
	r.mu.Lock()
	p := r.procs[id]
	r.mu.Unlock()
	if p != nil && p.alive() && p.key == sp.key() {
		return p, nil
	}
	if p != nil {
		p.stop()
	}
	ctx, cancel := context.WithTimeout(ctx, localStart)
	defer cancel()
	p, err := startLocal(ctx, sp)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.procs[id] = p
	r.mu.Unlock()
	go r.reap(id, p)
	return p, nil
}

// reap stops p after it was idle long enough, and forgets it once it stops.
func (r *localRunner) reap(id string, p *localProc) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			r.forget(id, p)
			return
		case <-t.C:
			p.mu.Lock()
			idle := time.Since(p.used) > r.idle && len(p.pending) == 0
			p.mu.Unlock()
			if idle {
				p.stop()
				r.forget(id, p)
				return
			}
		}
	}
}

func (r *localRunner) forget(id string, p *localProc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.procs[id] == p {
		delete(r.procs, id)
	}
}

// drop stops the server of id, if it runs.
func (r *localRunner) drop(id string) {
	r.mu.Lock()
	p := r.procs[id]
	delete(r.procs, id)
	r.mu.Unlock()
	if p != nil {
		p.stop()
	}
}

// tailBuffer keeps the last few kilobytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if over := len(t.buf) - 8<<10; over > 0 {
		t.buf = slices.Clone(t.buf[over:])
	}
	return len(b), nil
}

// localSpecOf is how the server srv starts with the keys env.
func (s *Service) localSpecOf(srv Server, env map[string]string) localSpec {
	all := map[string]string{}
	for k, v := range srv.FixedEnv {
		all[k] = v
	}
	for k, v := range env {
		all[k] = v
	}
	dir := filepath.Join(filepath.Dir(s.file), "mcp-local", srv.ID)
	for _, f := range srv.Env {
		if !f.File || all[f.Name] == "" {
			continue
		}
		path := filepath.Join(dir, "keys", strings.ToLower(f.Name))
		if os.MkdirAll(filepath.Dir(path), 0o700) == nil && os.WriteFile(path, []byte(all[f.Name]), 0o600) == nil {
			all[f.Name] = path
		}
	}
	return localSpec{command: expandArgs(srv.Command, srv.Env, all), env: all, dir: dir}
}

// expandArgs puts the typed value of a field into the command line where an argument holds "{FIELD}": a server that
// takes its setting as an argument, not an environment variable (a SQLite file's path). A field left empty keeps the
// braces out of the line: the argument is dropped.
func expandArgs(command []string, fields []EnvField, values map[string]string) []string {
	out := make([]string, 0, len(command))
	for _, arg := range command {
		drop := false
		for _, f := range fields {
			tag := "{" + f.Name + "}"
			if strings.Contains(arg, tag) {
				if values[f.Name] == "" {
					drop = true
				}
				arg = strings.ReplaceAll(arg, tag, values[f.Name])
			}
		}
		if !drop {
			out = append(out, arg)
		}
	}
	return out
}

// localEnvOf reads the keys of a local server from the safe; none kept is an empty set (a server without keys).
func (s *Service) localEnvOf(srv Server) (map[string]string, error) {
	cr, err := s.readCred(srv.ID)
	if err != nil {
		if !slices.ContainsFunc(srv.Env, func(f EnvField) bool { return !f.Optional }) {
			out := map[string]string{}
			for _, f := range srv.Env {
				if f.Default != "" {
					out[f.Name] = f.Default
				}
			}
			return out, nil
		}
		return nil, err
	}
	if cr.Env == nil {
		cr.Env = map[string]string{}
	}
	for _, f := range srv.Env {
		if cr.Env[f.Name] == "" && f.Default != "" {
			cr.Env[f.Name] = f.Default
		}
	}
	return cr.Env, nil
}

// inspectLocal starts the server with env apart from the running ones, lists its tools and stops it.
func (s *Service) inspectLocal(ctx context.Context, srv Server, env map[string]string) ([]Tool, error) {
	ctx, cancel := context.WithTimeout(ctx, localStart)
	defer cancel()
	p, err := startLocal(ctx, s.localSpecOf(srv, env))
	if err != nil {
		return nil, err
	}
	defer p.stop()
	return p.tools(ctx)
}

// ServeLocal answers the body of an agent's POST to a local server: one JSON-RPC message or a batch. The answer is nil
// when the body held only notifications.
func (s *Service) ServeLocal(ctx context.Context, id string, body []byte) ([]byte, error) {
	srv, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if srv.Transport != Stdio {
		return nil, fmt.Errorf("%w: not a local server", ErrBadRequest)
	}
	env, err := s.localEnvOf(srv)
	if err != nil {
		return nil, err
	}
	p, err := runner.get(ctx, id, s.localSpecOf(srv, env))
	if err != nil {
		return nil, err
	}
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		var batch []rpcIn
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, fmt.Errorf("%w: not JSON-RPC", ErrBadRequest)
		}
		var out []json.RawMessage
		for _, m := range batch {
			a, err := p.answer(ctx, m)
			if err != nil {
				return nil, err
			}
			if a != nil {
				out = append(out, a)
			}
		}
		if len(out) == 0 {
			return nil, nil
		}
		return json.Marshal(out)
	}
	var m rpcIn
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("%w: not JSON-RPC", ErrBadRequest)
	}
	return p.answer(ctx, m)
}
