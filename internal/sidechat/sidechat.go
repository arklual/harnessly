// Package sidechat answers a quick question about an agent tab's Claude session from a fork that is never saved: the
// main conversation does not change and the agent may be busy. The answer streams as events: deltas, then one done or
// error.
package sidechat

import (
	"bufio"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/tmux"
)

const (
	MaxQuestion    = 4000
	MaxExcerpt     = 2000
	MaxExcerpts    = 5
	MaxHistory     = 6
	maxHistoryAns  = 20_000
	promptAnswer   = 3000
	defaultTimeout = 120 * time.Second
	killGrace      = 3 * time.Second
	maxErrorChars  = 200
)

var tabIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Excerpt is a quoted piece of the conversation; Role and EntryID are set when it names its source.
type Excerpt struct {
	Quote         string
	HasSource     bool
	EntryID, Role string
}

// Turn is an earlier question of this side chat and its answer.
type Turn struct{ Question, Answer string }

// Request is a checked and cleaned question.
type Request struct {
	TabID, Question string
	Excerpts        []Excerpt
	History         []Turn
	Model           string
}

func clean(s string) string { return tmux.StripControlChars(s) }

func parseExcerpt(raw any) (Excerpt, string) {
	o, ok := raw.(*jsvalue.Object)
	if !ok {
		return Excerpt{}, "Неверный фрагмент"
	}
	q, has := o.Get("quote")
	qs, isStr := q.(string)
	if !has || !isStr {
		return Excerpt{}, "Неверный фрагмент"
	}
	quote := jscompat.Trim(clean(qs))
	if quote == "" {
		return Excerpt{}, "Фрагмент пустой"
	}
	if jscompat.Len(quote) > MaxExcerpt {
		return Excerpt{}, fmt.Sprintf("Фрагмент длиннее %d символов", MaxExcerpt)
	}
	src, has := o.Get("source")
	if !has {
		return Excerpt{Quote: quote}, ""
	}
	so, ok := src.(*jsvalue.Object)
	if !ok {
		return Excerpt{}, "Неверный источник фрагмента"
	}
	entry, isStr := func() (string, bool) { v, _ := so.Get("entryId"); s, ok := v.(string); return s, ok }()
	role, _ := func() (any, bool) { return so.Get("role") }()
	if !isStr || role != "agent" && role != "user" {
		return Excerpt{}, "Неверный источник фрагмента"
	}
	return Excerpt{Quote: quote, HasSource: true, EntryID: jscompat.Slice(clean(entry), 200), Role: role.(string)}, ""
}

func parseTurn(raw any) (Turn, bool) {
	o, ok := raw.(*jsvalue.Object)
	if !ok {
		return Turn{}, false
	}
	q, _ := o.Get("question")
	a, _ := o.Get("answer")
	qs, ok1 := q.(string)
	as, ok2 := a.(string)
	if !ok1 || !ok2 {
		return Turn{}, false
	}
	question, answer := jscompat.Trim(clean(qs)), jscompat.Trim(clean(as))
	if question == "" || jscompat.Len(question) > MaxQuestion || jscompat.Len(answer) > maxHistoryAns {
		return Turn{}, false
	}
	return Turn{Question: question, Answer: answer}, true
}

// ParseRequest checks a body from the browser; the text is the 400 error. The session is not part of it: the server
// finds it from the tab.
func ParseRequest(body any) (Request, string) {
	o, ok := body.(*jsvalue.Object)
	if !ok {
		return Request{}, "Нужен JSON"
	}
	tabID, _ := o.Get("tabId")
	tid, isStr := tabID.(string)
	if !isStr || !tabIDRe.MatchString(tid) {
		return Request{}, "Нужен tabId"
	}
	rawQ, _ := o.Get("question")
	question := ""
	if s, ok := rawQ.(string); ok {
		question = jscompat.Trim(clean(s))
	}
	if question == "" {
		return Request{}, "Вопрос пустой"
	}
	if jscompat.Len(question) > MaxQuestion {
		return Request{}, fmt.Sprintf("Вопрос длиннее %d символов", MaxQuestion)
	}
	model := ""
	if m, has := o.Get("model"); has {
		s, ok := m.(string)
		if !ok || !agents.ModelIDRe.MatchString(s) {
			return Request{}, "Неизвестная модель"
		}
		model = s
	}
	r := Request{TabID: tid, Question: question, Model: model, Excerpts: []Excerpt{}, History: []Turn{}}
	if v, has := o.Get("excerpts"); has {
		list, ok := v.([]any)
		if !ok || len(list) > MaxExcerpts {
			return Request{}, fmt.Sprintf("Не больше %d фрагментов", MaxExcerpts)
		}
		for _, raw := range list {
			e, msg := parseExcerpt(raw)
			if msg != "" {
				return Request{}, msg
			}
			r.Excerpts = append(r.Excerpts, e)
		}
	}
	if v, has := o.Get("history"); has {
		list, ok := v.([]any)
		if !ok {
			return Request{}, "Неверная история"
		}
		if len(list) > MaxHistory {
			list = list[len(list)-MaxHistory:]
		}
		for _, raw := range list {
			t, ok := parseTurn(raw)
			if !ok {
				return Request{}, "Неверная история"
			}
			r.History = append(r.History, t)
		}
	}
	return r, ""
}

var promptTags = regexp.MustCompile(`(?i)<(/?)(excerpt|turn|question|answer|side-chat-history)`)

// keepInside breaks up only a tag that would end a block early or open another one.
func keepInside(s string) string { return promptTags.ReplaceAllString(s, `<\${1}${2}`) }

func clipAnswer(a string) string {
	if jscompat.Len(a) > promptAnswer {
		return strings.TrimRightFunc(jscompat.Slice(a, promptAnswer), jscompat.IsSpace) + "…"
	}
	return a
}

const preamble = "This is a side question about the session above. " +
	"Answer from the context you already have. No tools are available, and nothing said here is added to the session. " +
	"Be brief and answer in the language of the question."

// BuildPrompt is the one text sent to the CLI. It begins with a fixed sentence, so a question that starts with a dash
// is never an option.
func BuildPrompt(r Request) string {
	parts := []string{preamble}
	if len(r.History) > 0 {
		turns := make([]string, len(r.History))
		for i, t := range r.History {
			turns[i] = "<turn><question>" + keepInside(t.Question) + "</question><answer>" + keepInside(clipAnswer(t.Answer)) + "</answer></turn>"
		}
		parts = append(parts, "<side-chat-history>\n"+strings.Join(turns, "\n")+"\n</side-chat-history>")
	}
	if len(r.Excerpts) > 0 {
		blocks := make([]string, len(r.Excerpts))
		for i, e := range r.Excerpts {
			from := ""
			if e.HasSource {
				from = ` from="` + e.Role + ` message"`
			}
			blocks[i] = "<excerpt" + from + ">" + keepInside(e.Quote) + "</excerpt>"
		}
		parts = append(parts, strings.Join(blocks, "\n"))
	}
	parts = append(parts, keepInside(r.Question))
	return strings.Join(parts, "\n\n")
}

// Event is one event of the stream: delta {text}, done {text, costUsd?} or error {code, message}.
type Event struct {
	Type, Text, Code, Message string
	CostUSD                   *float64
}

// Format is the event as a server-sent frame.
func Format(e Event) string {
	var o *jsvalue.Object
	switch e.Type {
	case "delta":
		o = jsvalue.NewObject("type", "delta", "text", e.Text)
	case "done":
		o = jsvalue.NewObject("type", "done", "text", e.Text)
		if e.CostUSD != nil {
			o.Set("costUsd", *e.CostUSD)
		}
	default:
		o = jsvalue.NewObject("type", "error", "code", e.Code, "message", e.Message)
	}
	return "data: " + string(jsvalue.Stringify(o, "")) + "\n\n"
}

var cancelled = Event{Type: "error", Code: "cancelled", Message: "Остановлено"}

// Args make the fork answer once and leave nothing behind: its own id, no persistence, no tools, MCP servers, slash
// commands or hooks. The question goes through stdin.
func Args(sessionID, forkID, model string) ([]string, error) {
	if !agents.IsUUID(sessionID) {
		return nil, fmt.Errorf("Invalid session id")
	}
	if !agents.IsUUID(forkID) {
		return nil, fmt.Errorf("Invalid fork id")
	}
	if model != "" && !agents.ModelIDRe.MatchString(model) {
		return nil, fmt.Errorf("Invalid model")
	}
	args := []string{"--print", "--resume", sessionID, "--fork-session", "--session-id", forkID, "--no-session-persistence"}
	if model != "" {
		args = append(args, "--model", model)
	}
	return append(args, "--tools", "", "--strict-mcp-config", "--disable-slash-commands", "--settings", `{"disableAllHooks":true}`,
		"--output-format", "stream-json", "--include-partial-messages", "--verbose"), nil
}

var parentAgentVars = []string{"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_EXECPATH", "CLAUDE_CONFIG_DIR"}

var machineAuthReplaces = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "CLAUDE_CODE_OAUTH_REFRESH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR",
	"CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"ANTHROPIC_BASE_URL", "ANTHROPIC_PROFILE", "ANTHROPIC_CONFIG_DIR",
	"ANTHROPIC_FEDERATION_RULE_ID", "CLAUDE_CODE_HOST_CREDS_FILE", "CLAUDE_CODE_HOST_AUTH_ENV_VAR",
}

// Runtime is the Claude binary the machine pins and its login folder ("" for the default login).
type Runtime struct {
	Binary, AuthDirectory string
}

// ReadRuntime reads `claude_binary` and `auth_directory` from the machine's runtime config; `claude` and the
// default login when it says nothing usable. Not the managed launcher: it takes a per-session lock the main agent
// holds while it runs.
func ReadRuntime(file string) Runtime {
	rt := Runtime{Binary: "claude"}
	raw, err := os.ReadFile(file)
	if err != nil || file == "" {
		return rt
	}
	v, err := jsvalue.Parse(raw)
	o, ok := v.(*jsvalue.Object)
	if err != nil || !ok {
		return rt
	}
	if b, _ := o.Get("claude_binary"); b != nil {
		if s, ok := b.(string); ok && filepath.IsAbs(s) {
			rt.Binary = s
		}
	}
	if a, _ := o.Get("auth_directory"); a != nil {
		if s, ok := a.(string); ok {
			rt.AuthDirectory = s
		}
	}
	return rt
}

// Env is the child's environment: free of the parent agent, auto-memory and updates off, the login the managed
// launcher would pick.
func Env(base []string, rt Runtime) []string {
	drop := map[string]bool{}
	for _, k := range parentAgentVars {
		drop[k] = true
	}
	set := map[string]string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1", "DISABLE_AUTOUPDATER": "1"}
	if rt.AuthDirectory != "" {
		for _, k := range machineAuthReplaces {
			drop[k] = true
		}
		set["CLAUDE_SECURESTORAGE_CONFIG_DIR"] = rt.AuthDirectory
	}
	var out []string
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] {
			continue
		}
		if _, replaced := set[k]; replaced {
			continue
		}
		out = append(out, kv)
	}
	for _, k := range []string{"CLAUDE_CODE_DISABLE_AUTO_MEMORY", "DISABLE_AUTOUPDATER", "CLAUDE_SECURESTORAGE_CONFIG_DIR"} {
		if v, ok := set[k]; ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

var (
	activeMu sync.Mutex
	active   = map[string]*int{}
	forks    = map[string]bool{}
)

// Claim takes the tab's one running question; nil when it already has one.
func Claim(tabID string) func() {
	activeMu.Lock()
	defer activeMu.Unlock()
	if _, busy := active[tabID]; busy {
		return nil
	}
	token := new(int)
	active[tabID] = token
	return func() {
		activeMu.Lock()
		defer activeMu.Unlock()
		if active[tabID] == token {
			delete(active, tabID)
		}
	}
}

// IsFork: the session id is a fork a side chat answers from right now; it is no conversation and stays out of lists.
func IsFork(id string) bool {
	activeMu.Lock()
	defer activeMu.Unlock()
	return forks[id]
}

func setFork(id string, on bool) {
	activeMu.Lock()
	defer activeMu.Unlock()
	if on {
		forks[id] = true
	} else {
		delete(forks, id)
	}
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

var (
	tooLong = regexp.MustCompile(`(?i)prompt is too long|context.*(limit|window)`)
	noLogin = regexp.MustCompile(`(?i)log ?in|authenticat|credential|401|unauthori[sz]ed`)
	spaces  = regexp.MustCompile(`\s+`)
)

func failureMessage(cli string) string {
	if tooLong.MatchString(cli) {
		return "Контекст сессии слишком велик для быстрого вопроса"
	}
	if noLogin.MatchString(cli) {
		return "Claude не авторизован на этой машине"
	}
	c := jscompat.Slice(jscompat.Trim(spaces.ReplaceAllString(clean(cli), " ")), maxErrorChars)
	if c != "" {
		return "Claude не смог ответить: " + c
	}
	return "Claude не смог ответить"
}

type streamState struct {
	text     string
	sawDelta bool
	result   *struct {
		text    string
		isError bool
		cost    *float64
	}
}

func (st *streamState) readLine(line string, emit func(Event)) {
	v, err := jsvalue.Parse([]byte(line))
	o, ok := v.(*jsvalue.Object)
	if err != nil || !ok {
		return
	}
	switch o.String("type") {
	case "stream_event":
		inner := o.Object("event")
		if inner == nil || inner.String("type") != "content_block_delta" {
			return
		}
		d := inner.Object("delta")
		if d != nil && d.String("type") == "text_delta" && d.String("text") != "" {
			st.sawDelta = true
			st.text += d.String("text")
			emit(Event{Type: "delta", Text: d.String("text")})
		}
	case "assistant":
		if st.sawDelta {
			return
		}
		var b strings.Builder
		if m := o.Object("message"); m != nil {
			for _, c := range m.Array("content") {
				if block, ok := c.(*jsvalue.Object); ok && block.String("type") == "text" && block.String("text") != "" {
					b.WriteString(block.String("text"))
				}
			}
		}
		if t := b.String(); t != "" {
			st.text += t
			emit(Event{Type: "delta", Text: t})
		}
	case "result":
		r := &struct {
			text    string
			isError bool
			cost    *float64
		}{text: st.text}
		if res, ok := func() (string, bool) { v, _ := o.Get("result"); s, ok := v.(string); return s, ok }(); ok {
			r.text = res
		}
		ie, _ := o.Get("is_error")
		r.isError = ie == true
		if c, ok := func() (float64, bool) { v, _ := o.Get("total_cost_usd"); f, ok := v.(float64); return f, ok }(); ok {
			r.cost = &c
		}
		st.result = r
	}
}

// Run is one question.
type Run struct {
	SessionID, Cwd, Prompt, Model string
	Runtime                       Runtime
	BaseEnv                       []string
	Timeout                       time.Duration
	// ProjectDir is the Claude projects folder of Cwd: a stray fork transcript there is removed afterwards.
	ProjectDir string
	Log        func(level, msg string, kv ...any)
}

// Ask asks a fork of the session one question and reports the answer as events: any number of deltas, then exactly
// one done or error. A cancelled ctx or the timeout kills the process.
func Ask(ctx context.Context, run Run, onEvent func(Event)) {
	if ctx.Err() != nil {
		onEvent(cancelled)
		return
	}
	logf := func(level, msg string, kv ...any) {
		if run.Log != nil {
			run.Log(level, msg, kv...)
		}
	}
	forkID := newUUID()
	setFork(forkID, true)
	var outcome *Event
	finish := func(e Event) {
		if outcome == nil {
			outcome = &e
		}
	}
	var mu sync.Mutex
	emit := func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		if outcome == nil {
			onEvent(e)
		}
	}
	defer func() {
		if run.ProjectDir != "" {
			_ = os.Remove(filepath.Join(run.ProjectDir, forkID+".jsonl"))
			_ = os.RemoveAll(filepath.Join(run.ProjectDir, forkID))
		}
		setFork(forkID, false)
		mu.Lock()
		e := cancelled
		if outcome != nil {
			e = *outcome
		}
		mu.Unlock()
		onEvent(e)
	}()
	args, err := Args(run.SessionID, forkID, run.Model)
	if err != nil {
		logf("warn", "side chat failed to start", "err", err.Error())
		finish(Event{Type: "error", Code: "unavailable", Message: "Claude не запускается на этой машине"})
		return
	}
	cmd := exec.Command(run.Runtime.Binary, args...)
	cmd.Dir = run.Cwd
	cmd.Env = Env(run.BaseEnv, run.Runtime)
	cmd.Stdin = strings.NewReader(run.Prompt)
	stdout, _ := cmd.StdoutPipe()
	var stderr tail
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		logf("warn", "side chat could not start the CLI", "err", err.Error())
		finish(Event{Type: "error", Code: "unavailable", Message: "Claude не запускается на этой машине"})
		return
	}
	timeout := run.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	stopped := make(chan struct{})
	stop := func(e Event) {
		mu.Lock()
		finish(e)
		mu.Unlock()
		_ = cmd.Process.Signal(syscall.SIGTERM)
		go func() {
			select {
			case <-stopped:
			case <-time.After(killGrace):
				_ = cmd.Process.Kill()
			}
		}()
	}
	go func() {
		select {
		case <-stopped:
		case <-ctx.Done():
			stop(cancelled)
		case <-time.After(timeout):
			stop(Event{Type: "error", Code: "timeout", Message: "Агент не ответил вовремя"})
		}
	}()
	st := &streamState{}
	r := bufio.NewReader(stdout)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if strings.TrimSpace(line) != "" {
				st.readLine(line, emit)
			}
			break
		}
		st.readLine(strings.TrimSuffix(line, "\n"), emit)
	}
	waitErr := cmd.Wait()
	close(stopped)
	mu.Lock()
	defer mu.Unlock()
	switch {
	case st.result != nil && !st.result.isError:
		finish(Event{Type: "done", Text: st.result.text, CostUSD: st.result.cost})
	case st.result != nil:
		finish(Event{Type: "error", Code: "failed", Message: failureMessage(st.result.text)})
	case waitErr == nil && st.text != "":
		finish(Event{Type: "done", Text: st.text})
	case outcome == nil:
		logf("warn", "side chat CLI ended without an answer", "stderr", stderr.String())
		finish(Event{Type: "error", Code: "failed", Message: "Claude не смог ответить"})
	}
}

// tail keeps the last 2000 bytes written.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > 2000 {
		t.buf = t.buf[len(t.buf)-2000:]
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

var _ io.Writer = (*tail)(nil)
