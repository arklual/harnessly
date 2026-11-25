package acpbridge

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Config is one bridge's start.
type Config struct {
	// Agy is the official CLI's executable.
	Agy string
	// Cwd is the chat's folder.
	Cwd string
	// Socket is where the runtime connects; "" serves no socket (the terminal alone).
	Socket string
	// Transcript names the session's transcript by its id; nil reports none.
	Transcript func(session string) string
	// Conversation resumes this session; "" starts a new one.
	Conversation string
	// Model, Effort and Mode are the session's settings at start; "" leaves the CLI's own.
	Model, Effort, Mode string
	// In and Out are the terminal; Raw puts In in raw mode (a real terminal).
	In  io.Reader
	Out io.Writer
	Raw bool
	// Env is the agent's environment; nil inherits the bridge's.
	Env []string
	// MCP are stdio MCP servers the session gets (ACP's McpServer objects).
	MCP []MCPServer
	// First is a message sent as soon as the session is up.
	First string
}

// MCPServer is a stdio MCP server of the session, as ACP names one.
type MCPServer struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args"`
	Env     []EnvVar `json:"env"`
}

// EnvVar is one variable of an MCP server's environment.
type EnvVar struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Ask modes of the protocol and the ACP modes they run on.
const (
	ModeAsk    = "ask"
	ModePlan   = "plan"
	ModeBypass = "bypass"
)

var acpMode = map[string]string{ModeAsk: "default", ModePlan: "plan", ModeBypass: "accept-edits"}

type configOption struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Category     string `json:"category"`
	CurrentValue string `json:"currentValue"`
	Options      []struct {
		Value string `json:"value"`
		Name  string `json:"name"`
	} `json:"options"`
}

type permOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

type pendingAsk struct {
	p       Permission
	options []permOption
	answer  chan string // the optionId, "" for cancelled
}

// Bridge is one chat's session.
type Bridge struct {
	cfg  Config
	term *terminal
	rpc  *rpc
	cmd  *exec.Cmd
	errs *tail

	mu          sync.Mutex
	session     string
	options     []configOption
	mode        string // the protocol's mode: ask, plan, bypass
	busy        bool
	outstanding int
	lastStop    string
	lastErr     string
	delivered   []string
	seen        map[string]bool
	asks        map[string]*pendingAsk
	askOrder    []string
	clients     map[*client]bool
	context     string // the share of the context window in use, "" unknown
	ttyN        int

	exit chan error
}

// Run starts the agent and serves the chat until the agent ends or ctx does.
func Run(ctx context.Context, cfg Config) error {
	b := &Bridge{cfg: cfg, seen: map[string]bool{}, asks: map[string]*pendingAsk{}, clients: map[*client]bool{},
		errs: &tail{max: 40}, exit: make(chan error, 1)}
	b.term = newTerminal(cfg.Out, cfg.Raw)
	if err := b.start(ctx); err != nil {
		b.term.log(styleErr("Antigravity did not start: " + err.Error()))
		if t := b.errs.String(); t != "" {
			b.term.log(t)
		}
		b.term.close()
		return err
	}
	defer b.term.close()
	var ln net.Listener
	if cfg.Socket != "" {
		var err error
		if ln, err = listen(cfg.Socket); err != nil {
			b.term.log(styleErr("the harnessly channel is off: " + err.Error()))
		} else {
			defer func() { _ = ln.Close(); _ = os.Remove(cfg.Socket) }()
			go b.serve(ln)
		}
	}
	if cfg.In != nil {
		go b.readKeys(cfg.In)
	}
	if cfg.Raw {
		OnResize(b.redraw)
	}
	b.redraw()
	if strings.TrimSpace(cfg.First) != "" {
		if err := b.prompt("first", cfg.First, nil, nil); err != nil {
			b.term.log(styleErr("the first message was not sent: " + err.Error()))
		}
	}
	select {
	case err := <-b.exit:
		return err
	case <-ctx.Done():
		b.stopAgent()
		return ctx.Err()
	}
}

func (b *Bridge) start(ctx context.Context) error {
	cmd := exec.Command(b.cfg.Agy, "acp")
	cmd.Dir = b.cfg.Cwd
	if b.cfg.Env != nil {
		cmd.Env = b.cfg.Env
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = b.errs
	if err := cmd.Start(); err != nil {
		return err
	}
	b.cmd = cmd
	go func() {
		err := cmd.Wait()
		if err == nil {
			err = errors.New("Antigravity ended")
		}
		b.term.log(styleErr("Antigravity ended: " + err.Error()))
		if t := b.errs.String(); t != "" {
			b.term.log(t)
		}
		b.exit <- err
	}()
	b.rpc = newRPC(out, in, b.handle)
	ictx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var init struct {
		AgentCapabilities struct {
			SessionCapabilities map[string]json.RawMessage `json:"sessionCapabilities"`
		} `json:"agentCapabilities"`
	}
	if err := b.rpc.call(ictx, "initialize", map[string]any{"protocolVersion": 1,
		"clientCapabilities": map[string]any{"fs": map[string]bool{"readTextFile": false, "writeTextFile": false}, "terminal": false},
		"clientInfo":         map[string]string{"name": "harnessly", "version": "1"}}, &init); err != nil {
		b.stopAgent()
		return err
	}
	var sess struct {
		SessionID     string         `json:"sessionId"`
		ConfigOptions []configOption `json:"configOptions"`
	}
	servers := []MCPServer{}
	if b.cfg.MCP != nil {
		servers = b.cfg.MCP
	}
	params := map[string]any{"cwd": b.cfg.Cwd, "mcpServers": servers}
	if id := b.cfg.Conversation; id != "" {
		params["sessionId"] = id
		method := "session/load"
		if _, ok := init.AgentCapabilities.SessionCapabilities["resume"]; ok {
			// Resume takes the session back without replaying its history to a client that does not draw it.
			method = "session/resume"
		}
		b.term.log(styleDim("resuming " + id + " …"))
		if err := b.rpc.call(ictx, method, params, &sess); err != nil {
			b.stopAgent()
			return err
		}
		sess.SessionID = id
	} else if err := b.rpc.call(ictx, "session/new", params, &sess); err != nil {
		b.stopAgent()
		return err
	}
	b.mu.Lock()
	b.session = sess.SessionID
	if len(sess.ConfigOptions) > 0 {
		b.options = sess.ConfigOptions
	}
	b.mode = ModeAsk
	b.mu.Unlock()
	mode := b.cfg.Mode
	if mode == "" {
		mode = ModeAsk
	}
	if err := b.configure(ictx, b.cfg.Model, b.cfg.Effort, mode); err != nil {
		b.term.log(styleErr("settings not applied: " + err.Error()))
	}
	b.term.log(styleDim("Antigravity session " + sess.SessionID + " in " + b.cfg.Cwd))
	return nil
}

func (b *Bridge) stopAgent() {
	if b.cmd != nil && b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
	}
}

// --- settings ---

func (b *Bridge) option(id string) *configOption {
	for i := range b.options {
		if b.options[i].ID == id || b.options[i].Category == id {
			return &b.options[i]
		}
	}
	return nil
}

// resolve finds the value of an option a name asks for: the value itself, or the one value it is the family of
// (a model asked by «gpt-oss-120b» whose only value is «gpt-oss-120b-medium»).
func resolve(o *configOption, want string) (string, bool) {
	if o == nil || want == "" {
		return "", false
	}
	var prefixed []string
	for _, v := range o.Options {
		if v.Value == want {
			return want, true
		}
		if strings.HasPrefix(v.Value, want+"-") {
			prefixed = append(prefixed, v.Value)
		}
	}
	if len(prefixed) == 1 {
		return prefixed[0], true
	}
	return "", false
}

// configure sets the model, the effort and the mode; "" leaves one as it is. The model goes first: a model has its
// own efforts.
func (b *Bridge) configure(ctx context.Context, model, effort, mode string) error {
	var errs []error
	set := func(id, want string) {
		b.mu.Lock()
		o := b.option(id)
		value, ok := resolve(o, want)
		cur, sid := "", b.session
		if o != nil {
			cur = o.CurrentValue
		}
		b.mu.Unlock()
		switch {
		case want == "":
			return
		case o == nil:
			errs = append(errs, fmt.Errorf("Antigravity has no %s setting", id))
			return
		case !ok:
			errs = append(errs, fmt.Errorf("Antigravity has no %s %q", id, want))
			return
		case value == cur:
			return
		}
		var res struct {
			ConfigOptions []configOption `json:"configOptions"`
		}
		if err := b.rpc.call(ctx, "session/set_config_option", map[string]any{"sessionId": sid, "configId": o.ID, "value": value}, &res); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			return
		}
		b.mu.Lock()
		if len(res.ConfigOptions) > 0 {
			b.options = res.ConfigOptions
		} else if o := b.option(id); o != nil {
			o.CurrentValue = value
		}
		b.mu.Unlock()
	}
	set("model", model)
	set("thought_level", effort)
	if mode != "" {
		acp, ok := acpMode[mode]
		if !ok {
			errs = append(errs, fmt.Errorf("unknown mode %q", mode))
		} else {
			set("mode", acp)
			b.mu.Lock()
			if o := b.option("mode"); o != nil && o.CurrentValue == acp {
				b.mode = mode
			}
			b.mu.Unlock()
		}
	}
	b.redraw()
	b.broadcastState()
	return errors.Join(errs...)
}

// State is the session as it is now.
func (b *Bridge) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stateLocked()
}

func (b *Bridge) stateLocked() State {
	s := State{Session: b.session, Mode: b.mode, Busy: b.busy, Delivered: append([]string(nil), b.delivered...)}
	if b.cfg.Transcript != nil && b.session != "" {
		s.Transcript = b.cfg.Transcript(b.session)
	}
	if o := b.option("model"); o != nil {
		s.Model = o.CurrentValue
	}
	if o := b.option("thought_level"); o != nil {
		s.Effort = o.CurrentValue
	}
	return s
}

// --- turns ---

// prompt hands a message to the session. A message the session took before (by its id) is not sent again. A message
// while a turn runs joins that turn: Antigravity reads it at its next step.
func (b *Bridge) prompt(id, text string, images, files []string) error {
	blocks := []map[string]any{}
	if strings.TrimSpace(text) != "" {
		blocks = append(blocks, map[string]any{"type": "text", "text": text})
	}
	for _, p := range images {
		block, err := imageBlock(p)
		if err != nil {
			return err
		}
		blocks = append(blocks, block)
	}
	for _, p := range files {
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		blocks = append(blocks, map[string]any{"type": "resource_link", "uri": (&url.URL{Scheme: "file", Path: abs}).String(), "name": filepath.Base(abs)})
	}
	if len(blocks) == 0 {
		return errors.New("an empty message")
	}
	b.mu.Lock()
	if id != "" && b.seen[id] {
		b.mu.Unlock()
		return nil
	}
	if id != "" {
		b.seen[id] = true
		b.delivered = append(b.delivered, id)
		if len(b.delivered) > 200 {
			b.delivered = b.delivered[len(b.delivered)-200:]
		}
	}
	sid := b.session
	started := b.outstanding == 0
	b.outstanding++
	b.busy = true
	if started {
		b.lastStop, b.lastErr = "", ""
	}
	b.mu.Unlock()
	b.flushText()
	shown := text
	for _, p := range append(append([]string(nil), images...), files...) {
		shown += " [" + filepath.Base(p) + "]"
	}
	b.term.log(styleUser("› " + shown))
	if started {
		b.broadcast(Event{Ev: "turn-started"})
	}
	b.redraw()
	go func() {
		var res struct {
			StopReason string `json:"stopReason"`
		}
		err := b.rpc.call(context.Background(), "session/prompt", map[string]any{"sessionId": sid, "prompt": blocks}, &res)
		b.mu.Lock()
		b.outstanding--
		if err != nil {
			b.lastStop, b.lastErr = "error", err.Error()
		} else if b.lastStop != "error" {
			b.lastStop = res.StopReason
		}
		ended := b.outstanding == 0
		stop, detail := b.lastStop, b.lastErr
		if ended {
			b.busy = false
		}
		b.mu.Unlock()
		if !ended {
			return
		}
		b.flushText()
		switch {
		case err != nil:
			b.term.log(styleErr("the turn failed: " + err.Error()))
		case stop == "cancelled":
			b.term.log(styleDim("stopped"))
		case stop != "" && stop != "end_turn":
			b.term.log(styleDim("turn ended: " + stop))
		}
		b.term.log("")
		b.broadcast(Event{Ev: "turn-ended", Stop: stop, Error: detail})
		b.redraw()
	}()
	return nil
}

// cancel stops the running turn: the asks it holds are cancelled too.
func (b *Bridge) cancel() {
	b.mu.Lock()
	sid, busy := b.session, b.busy
	asks := make([]*pendingAsk, 0, len(b.asks))
	for _, a := range b.asks {
		asks = append(asks, a)
	}
	b.mu.Unlock()
	for _, a := range asks {
		select {
		case a.answer <- "":
		default:
		}
	}
	if busy {
		_ = b.rpc.notify("session/cancel", map[string]string{"sessionId": sid})
	}
}

func imageBlock(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 20<<20 {
		return nil, fmt.Errorf("%s is too large to attach", filepath.Base(path))
	}
	mt := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if !strings.HasPrefix(mt, "image/") {
		mt = http.DetectContentType(raw)
	}
	if !strings.HasPrefix(mt, "image/") {
		return nil, fmt.Errorf("%s is not an image", filepath.Base(path))
	}
	mt, _, _ = strings.Cut(mt, ";")
	return map[string]any{"type": "image", "mimeType": mt, "data": base64.StdEncoding.EncodeToString(raw)}, nil
}

// --- what the agent sends ---

func (b *Bridge) handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "session/update":
		b.update(params)
		return nil, nil
	case "session/request_permission":
		return b.requestPermission(params)
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

type toolCall struct {
	ToolCallID string          `json:"toolCallId"`
	Name       string          `json:"name"`
	Kind       string          `json:"kind"`
	Title      string          `json:"title"`
	Status     string          `json:"status"`
	RawInput   json.RawMessage `json:"rawInput"`
}

func (b *Bridge) update(params json.RawMessage) {
	var p struct {
		Update json.RawMessage `json:"update"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	var u struct {
		SessionUpdate string          `json:"sessionUpdate"`
		Content       json.RawMessage `json:"content"` // a block for a chunk, a list of them for a tool call
		toolCall
		ConfigOptions []configOption `json:"configOptions"`
		CurrentModeID string         `json:"currentModeId"`
		Used          float64        `json:"used"`
		Size          float64        `json:"size"`
		Entries       []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
		} `json:"entries"`
	}
	if json.Unmarshal(p.Update, &u) != nil {
		return
	}
	switch u.SessionUpdate {
	case "agent_message_chunk":
		var c struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(u.Content, &c) == nil && c.Type == "text" {
			b.term.text(c.Text)
		}
	case "agent_thought_chunk":
		b.term.thinking()
	case "tool_call":
		b.flushText()
		b.term.log(styleTool("● " + toolLine(u.toolCall)))
	case "tool_call_update":
		switch u.Status {
		case "completed":
			b.flushText()
			b.term.log(styleDim("  ✓ " + toolLine(u.toolCall)))
		case "failed":
			b.flushText()
			b.term.log(styleErr("  ✗ " + toolLine(u.toolCall)))
		}
	case "plan":
		b.flushText()
		var lines []string
		for _, e := range u.Entries {
			mark := "☐"
			switch e.Status {
			case "completed":
				mark = "☒"
			case "in_progress":
				mark = "▸"
			}
			lines = append(lines, "  "+mark+" "+e.Content)
		}
		if len(lines) > 0 {
			b.term.log(styleDim("plan\n" + strings.Join(lines, "\n")))
		}
	case "config_option_update":
		if len(u.ConfigOptions) > 0 {
			b.mu.Lock()
			b.options = u.ConfigOptions
			b.mu.Unlock()
			b.broadcastState()
		}
	case "current_mode_update":
		b.mu.Lock()
		if o := b.option("mode"); o != nil {
			o.CurrentValue = u.CurrentModeID
		}
		for mode, acp := range acpMode {
			if acp == u.CurrentModeID && b.mode != ModeBypass {
				b.mode = mode
			}
		}
		b.mu.Unlock()
		b.broadcastState()
	case "usage_update":
		if u.Size > 0 {
			b.mu.Lock()
			b.context = strconv.Itoa(int(u.Used*100/u.Size+0.5)) + "%"
			b.mu.Unlock()
		}
	default:
		return
	}
	b.redraw()
}

func toolLine(t toolCall) string {
	title := strings.TrimSpace(t.Title)
	name := t.Name
	if name == "" {
		name = t.Kind
	}
	if title == "" || title == name {
		return name
	}
	if i := strings.IndexByte(title, '\n'); i >= 0 {
		title = title[:i] + " …"
	}
	return name + ": " + title
}

func (b *Bridge) flushText() { b.term.flushText() }

func (b *Bridge) requestPermission(params json.RawMessage) (any, error) {
	var p struct {
		ToolCall toolCall     `json:"toolCall"`
		Options  []permOption `json:"options"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	pick := func(kinds ...string) string {
		for _, k := range kinds {
			for _, o := range p.Options {
				if o.Kind == k {
					return o.OptionID
				}
			}
		}
		return ""
	}
	selected := func(id string) any {
		if id == "" {
			return map[string]any{"outcome": map[string]string{"outcome": "cancelled"}}
		}
		return map[string]any{"outcome": map[string]string{"outcome": "selected", "optionId": id}}
	}
	b.mu.Lock()
	bypass := b.mode == ModeBypass
	b.mu.Unlock()
	b.flushText()
	if bypass {
		if id := pick("allow_once", "allow_always"); id != "" {
			return selected(id), nil
		}
	}
	id := p.ToolCall.ToolCallID
	if id == "" {
		b.mu.Lock()
		b.ttyN++
		id = "ask-" + strconv.Itoa(b.ttyN)
		b.mu.Unlock()
	}
	ask := &pendingAsk{options: p.Options, answer: make(chan string, 1), p: Permission{ID: id, Tool: p.ToolCall.Name,
		Kind: p.ToolCall.Kind, Title: p.ToolCall.Title, Input: p.ToolCall.RawInput, Always: pick("allow_always") != ""}}
	b.mu.Lock()
	b.asks[id] = ask
	b.askOrder = append(b.askOrder, id)
	b.mu.Unlock()
	perm := ask.p
	b.broadcast(Event{Ev: "permission", Permission: &perm})
	b.redraw()
	answer := <-ask.answer
	b.mu.Lock()
	delete(b.asks, id)
	for i, x := range b.askOrder {
		if x == id {
			b.askOrder = append(b.askOrder[:i], b.askOrder[i+1:]...)
			break
		}
	}
	b.mu.Unlock()
	b.broadcast(Event{Ev: "permission-gone", Permission: &perm})
	var option string
	switch answer {
	case "allow":
		option = pick("allow_once", "allow_always")
		b.term.log(styleDim("  allowed: " + toolLine(p.ToolCall)))
	case "always":
		option = pick("allow_always", "allow_once")
		b.term.log(styleDim("  allowed from now on: " + toolLine(p.ToolCall)))
	case "deny":
		option = pick("reject_once", "reject_always")
		b.term.log(styleDim("  declined: " + toolLine(p.ToolCall)))
	}
	b.redraw()
	return selected(option), nil
}

// decide answers an ask: allow, always, deny or cancel (which stops the turn too).
func (b *Bridge) decide(id, decision string) error {
	b.mu.Lock()
	a := b.asks[id]
	b.mu.Unlock()
	if a == nil {
		return errors.New("no such question: it was answered or withdrawn")
	}
	switch decision {
	case "allow", "always", "deny":
		select {
		case a.answer <- decision:
		default:
		}
	case "cancel":
		select {
		case a.answer <- "":
		default:
		}
		b.cancel()
	default:
		return fmt.Errorf("unknown decision %q", decision)
	}
	return nil
}

// oldestAsk is the ask the terminal answers.
func (b *Bridge) oldestAsk() *pendingAsk {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.askOrder) == 0 {
		return nil
	}
	return b.asks[b.askOrder[0]]
}

// --- the socket ---

type client struct {
	mu sync.Mutex
	w  io.Writer
}

func (c *client) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = c.w.Write(append(raw, '\n'))
}

func listen(socket string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socket), 0o700); err != nil {
		return nil, err
	}
	if err := ownDir(filepath.Dir(socket)); err != nil {
		return nil, err
	}
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	_ = os.Chmod(socket, 0o600)
	return ln, nil
}

func (b *Bridge) serve(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go b.serveConn(conn)
	}
}

func (b *Bridge) serveConn(conn net.Conn) {
	defer conn.Close()
	c := &client{w: conn}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	defer func() {
		b.mu.Lock()
		delete(b.clients, c)
		b.mu.Unlock()
	}()
	for sc.Scan() {
		var r Request
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		b.request(c, r)
	}
}

func (b *Bridge) request(c *client, r Request) {
	reply := Reply{Req: r.Req, OK: true}
	fail := func(err error) {
		if err != nil {
			reply.OK, reply.Error = false, err.Error()
		}
	}
	switch r.Op {
	case "hello":
		b.mu.Lock()
		b.clients[c] = true
		st := b.stateLocked()
		asks := make([]Permission, 0, len(b.askOrder))
		for _, id := range b.askOrder {
			asks = append(asks, b.asks[id].p)
		}
		b.mu.Unlock()
		reply.State = &st
		c.send(reply)
		// What waits for the person is told again to whoever connects.
		for i := range asks {
			c.send(Event{Ev: "permission", Permission: &asks[i]})
		}
		return
	case "prompt":
		fail(b.prompt(r.ID, r.Text, r.Images, r.Files))
	case "cancel":
		b.cancel()
	case "config":
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		fail(b.configure(ctx, r.Model, r.Effort, r.Mode))
		cancel()
		st := b.State()
		reply.State = &st
	case "permission":
		fail(b.decide(r.Permission, r.Decision))
	default:
		fail(fmt.Errorf("unknown op %q", r.Op))
	}
	c.send(reply)
}

func (b *Bridge) broadcast(e Event) {
	b.mu.Lock()
	cs := make([]*client, 0, len(b.clients))
	for c := range b.clients {
		cs = append(cs, c)
	}
	b.mu.Unlock()
	for _, c := range cs {
		c.send(e)
	}
}

func (b *Bridge) broadcastState() {
	st := b.State()
	b.broadcast(Event{Ev: "state", State: &st})
}

// --- the frame's footer ---

func (b *Bridge) footer() (model string, effort string, busy bool, extra []string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if o := b.option("model"); o != nil {
		model = o.CurrentValue
		for _, v := range o.Options {
			if v.Value == o.CurrentValue && v.Name != "" {
				model = v.Name
			}
		}
	}
	effort = "none"
	if o := b.option("thought_level"); o != nil && o.CurrentValue != "" {
		effort = o.CurrentValue
	}
	busy = b.busy
	switch b.mode {
	case ModePlan:
		extra = append(extra, "plan mode")
	case ModeBypass:
		extra = append(extra, "auto-approve")
	}
	if b.context != "" {
		extra = append(extra, "context "+b.context)
	}
	if len(b.askOrder) > 0 {
		extra = append(extra, "waiting for approval")
	}
	return model, effort, busy, extra
}

func (b *Bridge) redraw() {
	model, effort, busy, extra := b.footer()
	var ask string
	if a := b.oldestAsk(); a != nil {
		ask = "? allow " + toolLine(toolCall{Name: a.p.Tool, Kind: a.p.Kind, Title: a.p.Title}) + "   y yes"
		if a.p.Always {
			ask += " · a always"
		}
		ask += " · n no   (or answer in harnessly)"
	}
	b.term.frame(model, effort, busy, extra, ask)
}

// tail keeps the last lines an agent wrote to its error stream.
type tail struct {
	mu    sync.Mutex
	max   int
	lines []string
	part  []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.part = append(t.part, p...)
	for {
		i := bytes.IndexByte(t.part, '\n')
		if i < 0 {
			break
		}
		t.lines = append(t.lines, string(t.part[:i]))
		t.part = t.part[i+1:]
	}
	if len(t.lines) > t.max {
		t.lines = t.lines[len(t.lines)-t.max:]
	}
	if len(t.part) > 4096 {
		t.part = t.part[:0]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.Join(t.lines, "\n")
}
