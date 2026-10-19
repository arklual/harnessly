// Package usagereport is a runtime's usage report: what its Claude Code and Codex transcripts say about prompts,
// sessions, tokens and their API-equivalent cost, per day, project and model. A transcript is reduced once to compact
// per-file facts, kept by size and modification time in memory and in a file shared with the previous runtime, and the
// report is a reduction of those facts.
package usagereport

import (
	"bufio"
	"bytes"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"harnessly/internal/jscompat"
)

// The rows of a file's facts, each with its dedup key: streamed chunks repeat one usage, and resumed or forked
// sessions copy the earlier history into a new file with the original ids. A key is a JSON value, mostly a string.

// MsgRow is a model call: key, time (epoch seconds), model and project (indexes), input, output, cache read and cache
// writes of the 5-minute and 1-hour tiers.
type MsgRow struct {
	Key                                any
	Ts                                 float64
	Model, Project                     int
	Input, Output, CacheRead, W5m, W1h float64
}

// PromptRow is a prompt of a person: key, time, project.
type PromptRow struct {
	Key     any
	Ts      float64
	Project int
}

// TurnRow is a turn's working time: key, time, milliseconds.
type TurnRow struct {
	Key any
	Ts  float64
	Ms  float64
}

// ToolRow is a tool result: key, time, 1 for an error.
type ToolRow struct {
	Key   any
	Ts    float64
	Error float64
}

// EventRow is an interrupt, a denied tool or a compaction: key and time.
type EventRow struct {
	Key any
	Ts  float64
}

// Facts is one transcript reduced.
type Facts struct {
	Provider  string
	SessionID string
	// Sidechain: a subagent's transcript, counted toward its parent session and never as prompts.
	Sidechain          bool
	Title, FirstPrompt *string
	// Models are strings, except where a transcript named a model with another JSON value (the report then fails, as
	// the previous runtime's did).
	Models      []any
	Projects    []string
	Tasks, PRs  []string
	FirstTs     float64
	LastTs      float64
	Messages    []MsgRow
	Prompts     []PromptRow
	Turns       []TurnRow
	Tools       []ToolRow
	Interrupts  []EventRow
	Denials     []EventRow
	Compactions []EventRow
}

const (
	noProject      = "без проекта"
	outsideRepos   = "вне репозиториев"
	interruptText  = "[Request interrupted by user"
	denialText     = "The user doesn't want to proceed with this tool use"
	defaultCodexAI = "gpt-5"
)

// harnessPrefix: user entries the harness wrote (command echoes, notifications, peer messages), not a person.
var harnessPrefix = regexp.MustCompile(`^` + jscompat.SpaceClass + `*<(local-command|command-|task-notification|system-reminder|teammate-message|cross-session-message|user-prompt-submit-hook|bash-(input|stdout|stderr))`)

// Projects names projects from working folders the same way on every machine: the repository git names, or the folder
// under the workspace root, a worktree without its task suffix.
type Projects struct {
	// Root is the name of the folder that holds the repositories ("" when there is none).
	Root string
	// TaskPrefixes are the tracker's task key prefixes (upper case): a task key in a prompt or branch is collected, and a
	// worktree folder named after a task loses the suffix.
	TaskPrefixes []string

	once     sync.Once
	taskRE   *regexp.Regexp
	suffixRE *regexp.Regexp
	mu       sync.Mutex
	gitRepos map[string]*string
}

var hashSuffix = regexp.MustCompile(`-[0-9a-f]{8}$`)

func (p *Projects) init() {
	p.once.Do(func() {
		quoted := make([]string, 0, len(p.TaskPrefixes))
		for _, t := range p.TaskPrefixes {
			if t != "" {
				quoted = append(quoted, regexp.QuoteMeta(t))
			}
		}
		alts := strings.Join(quoted, "|")
		if alts != "" {
			p.taskRE = regexp.MustCompile(`\b(?:` + alts + `)-\d{1,6}\b`)
			p.suffixRE = regexp.MustCompile(`(?i)(-(` + alts + `)-?\d+|-w\d+|-\d+)$`)
		} else {
			p.suffixRE = regexp.MustCompile(`(?i)(-w\d+|-\d+)$`)
		}
		p.gitRepos = map[string]*string{}
	})
}

func (p *Projects) tasks(text string, into *ordered) {
	p.init()
	if p.taskRE == nil {
		return
	}
	for _, m := range p.taskRE.FindAllString(text, -1) {
		into.add(m)
	}
}

var gitdirLine = regexp.MustCompile(`(?m)^gitdir:` + jscompat.SpaceClass + `*([^\n\r\x{2028}\x{2029}]+)`)
var worktreeOf = regexp.MustCompile(`[/\\]([^/\\]+)[/\\]\.git[/\\]worktrees[/\\]`)

// dirname is Node's path.dirname for the absolute paths transcripts carry.
func dirname(p string) string {
	if p == "" {
		return "."
	}
	trimmed := strings.TrimRight(p, "/")
	if trimmed == "" {
		return "/"
	}
	i := strings.LastIndexByte(trimmed, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	d := strings.TrimRight(trimmed[:i], "/")
	if d == "" {
		return "/"
	}
	return d
}

// gitRepo is the repository that owns a folder, read from git itself: a worktree's .git file points at
// <repo>/.git/worktrees/<name>. Remembered for the life of the process, as before.
func (p *Projects) gitRepo(cwd string) *string {
	p.mu.Lock()
	if r, ok := p.gitRepos[cwd]; ok {
		p.mu.Unlock()
		return r
	}
	p.mu.Unlock()
	var repo *string
	for dir := cwd; dir != dirname(dir); dir = dirname(dir) {
		marker := filepath.Join(dir, ".git")
		st, err := os.Stat(marker)
		if err != nil {
			continue
		}
		name := jscompat.Basename(dir, "")
		if st.IsDir() {
			repo = &name
			break
		}
		if raw, err := os.ReadFile(marker); err == nil {
			text := jscompat.DecodeUTF8(raw)
			if m := gitdirLine.FindStringSubmatch(text); m != nil {
				if gitdir := jscompat.Trim(m[1]); gitdir != "" {
					var resolved string
					if !strings.HasPrefix(gitdir, "/") {
						resolved = filepath.Join(dir, gitdir)
					} else {
						resolved = filepath.Clean(gitdir)
					}
					if w := worktreeOf.FindStringSubmatch(resolved); w != nil {
						name = w[1]
					}
				}
			}
		}
		repo = &name
		break
	}
	p.mu.Lock()
	p.gitRepos[cwd] = repo
	p.mu.Unlock()
	return repo
}

// Of is the project of a working folder; "" (or none) has none.
func (p *Projects) Of(cwd string, has bool) string {
	p.init()
	if !has || cwd == "" {
		return noProject
	}
	if repo := p.gitRepo(cwd); repo != nil && *repo != "" && *repo != p.Root && *repo != "workspace" {
		return *repo
	}
	var parts []string
	for _, s := range strings.Split(cwd, "/") {
		if s != "" {
			parts = append(parts, s)
		}
	}
	worktreeDir := func(s string) bool { return s == ".worktrees" || s == ".wt" || s == ".claude" }
	rootAt := -1
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == p.Root && (i == 0 || !worktreeDir(parts[i-1])) {
			rootAt = i
			break
		}
	}
	if rootAt == -1 {
		return outsideRepos
	}
	if rootAt+1 >= len(parts) {
		return p.Root
	}
	repo := parts[rootAt+1]
	if !worktreeDir(repo) {
		return repo
	}
	if rootAt+2 >= len(parts) {
		return p.Root
	}
	wt := parts[rootAt+2]
	name := p.suffixRE.ReplaceAllString(hashSuffix.ReplaceAllString(wt, ""), "")
	if name == "" {
		return wt
	}
	return name
}

// ordered is a set of strings in the order they were added.
type ordered struct {
	list []string
	seen map[string]bool
}

func (o *ordered) add(s string) {
	if o.seen[s] {
		return
	}
	if o.seen == nil {
		o.seen = map[string]bool{}
	}
	o.seen[s] = true
	o.list = append(o.list, s)
}

func (o *ordered) values() []string {
	if o.list == nil {
		return []string{}
	}
	return o.list
}

// interner keeps the first index of each value, as a JavaScript Map does: strings, numbers and booleans by value,
// objects each on their own.
type interner struct {
	values []any
	index  map[string]int
}

func (in *interner) add(v any) int {
	k, unique := setKey(v)
	if !unique {
		if at, ok := in.index[k]; ok {
			return at
		}
	}
	at := len(in.values)
	in.values = append(in.values, v)
	if !unique {
		if in.index == nil {
			in.index = map[string]int{}
		}
		in.index[k] = at
	}
	return at
}

// setKey is how a JavaScript Set or Map tells keys apart: unique is true for an object or array, never equal to another.
func setKey(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return "s" + x, false
	case float64:
		if x == 0 {
			return "n0", false
		}
		return "n" + strconv.FormatFloat(x, 'g', -1, 64), false
	case bool:
		if x {
			return "t", false
		}
		return "f", false
	case nil:
		return "z", false
	}
	return "", true
}

// template is `${v}` of a field: "undefined" when missing.
func template(v any, present bool) string {
	if !present {
		return "undefined"
	}
	return jscompat.String(v)
}

func tsSec(o object) float64 {
	s, ok := o.str("timestamp")
	if !ok {
		return 0
	}
	ms := jscompat.ParseDate(s)
	if math.IsNaN(ms) || math.IsInf(ms, 0) {
		return 0
	}
	return math.Floor(ms / 1000)
}

// textOf is the text blocks of a message's content joined by newlines, or the content when it is a string.
func textOf(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		return decodeString(raw)
	}
	var parts []string
	for _, b := range elements(raw) {
		o := objectOf(b)
		if o == nil || !o.is("type", "text") {
			continue
		}
		if t, ok := o.str("text"); ok {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n")
}

// errNullLine: the previous runtime read `null` as an entry and failed on it, giving up the whole file.
type errNullLine struct{}

func (errNullLine) Error() string { return "a transcript line is null" }

// eachLine calls on for every non-empty line, as Node's readline splits them (\n, \r\n and a lone \r), invalid UTF-8
// replaced as Node decodes it.
func eachLine(path string, on func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 1<<20)
	var long []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if err == bufio.ErrBufferFull {
			long = append(long, chunk...)
			continue
		}
		line := chunk
		if long != nil {
			line = append(long, chunk...)
			long = nil
		}
		for len(line) > 0 {
			i := bytes.IndexAny(line, "\r\n")
			part := line
			if i >= 0 {
				part, line = line[:i], line[i+1:]
			} else {
				line = nil
			}
			if len(part) == 0 {
				continue
			}
			if !utf8.Valid(part) {
				part = []byte(jscompat.DecodeUTF8(part))
			}
			if e := on(part); e != nil {
				return e
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// parseLine is JSON.parse of a line as an entry: ok false when it does not parse; a null entry fails the file.
func parseLine(line []byte) (object, bool, error) {
	if !valid(line) {
		return nil, false, nil
	}
	switch line[skipWS(line, 0)] {
	case '{':
		return objectOf(line[skipWS(line, 0):]), true, nil
	case 'n':
		return nil, false, errNullLine{}
	}
	return object{}, true, nil // a number, string, array or boolean: every field undefined
}

func newFacts(provider, sessionID string, sidechain bool) *Facts {
	return &Facts{Provider: provider, SessionID: sessionID, Sidechain: sidechain, Models: []any{}, Projects: []string{},
		Tasks: []string{}, PRs: []string{}, Messages: []MsgRow{}, Prompts: []PromptRow{}, Turns: []TurnRow{}, Tools: []ToolRow{},
		Interrupts: []EventRow{}, Denials: []EventRow{}, Compactions: []EventRow{}}
}

func (f *Facts) touch(ts float64) {
	if ts == 0 {
		return
	}
	if f.FirstTs == 0 || ts < f.FirstTs {
		f.FirstTs = ts
	}
	if ts > f.LastTs {
		f.LastTs = ts
	}
}

func (f *Facts) firstPromptOnce(text string) {
	if f.FirstPrompt == nil || *f.FirstPrompt == "" {
		s := jscompat.Slice(text, 200)
		f.FirstPrompt = &s
	}
}

func projectStrings(in *interner) []string {
	out := make([]string, len(in.values))
	for i, v := range in.values {
		out[i], _ = v.(string)
	}
	return out
}

var (
	fileHistoryMark = []byte(`"type":"file-history`)
	progressMark    = []byte(`"type":"progress"`)
)

// ParseClaude reduces a Claude Code transcript.
func (p *Projects) ParseClaude(path string) (*Facts, error) {
	sidechainFile := strings.Contains(path, "/subagents/")
	facts := newFacts("claude", jscompat.Basename(path, ".jsonl"), sidechainFile)
	models, projects := &interner{}, &interner{}
	var tasks, prs ordered
	// Streamed chunks of one response share (requestId, message.id); the largest value of each field is kept, the final
	// chunk carries the full output count.
	msgIndex := map[string]int{}
	sessionSet := false
	err := eachLine(path, func(line []byte) error {
		if bytes.Contains(line, fileHistoryMark) || bytes.Contains(line, progressMark) {
			return nil
		}
		e, ok, err := parseLine(line)
		if err != nil || !ok {
			return err
		}
		if !sessionSet {
			if s, ok := e.str("sessionId"); ok {
				facts.SessionID, sessionSet = s, true
			}
		}
		if b, ok := e.str("gitBranch"); ok {
			p.tasks(b, &tasks)
		}
		typ, _ := e.str("type")
		if typ == "ai-title" {
			if t, ok := e.str("aiTitle"); ok {
				facts.Title = &t
				return nil
			}
		}
		if typ == "pr-link" {
			if u, ok := e.str("prUrl"); ok {
				prs.add(u)
				return nil
			}
		}
		ts := tsSec(e)
		sidechain := sidechainFile || e.isTrue("isSidechain")
		cwd, hasCwd := e.str("cwd")
		project := projects.add(p.Of(cwd, hasCwd))

		switch typ {
		case "assistant":
			msg := objectOf(e.raw("message"))
			usage := objectOf(msg.raw("usage"))
			model, _ := msg.value("model")
			if !msg.truthy("usage") || !jscompat.Truthy(model) || model == "<synthetic>" || e.isTrue("isApiErrorMessage") {
				return nil
			}
			facts.touch(ts)
			req, _ := e.str("requestId")
			id, ok := msg.nullish("id")
			idPresent := ok
			if !ok {
				id, idPresent = e.nullish("uuid")
			}
			idText := ""
			if idPresent {
				idText = jscompat.String(id)
			}
			key := req + ":" + idText
			cache := objectOf(usage.raw("cache_creation"))
			cache5m, cache1h := cache.num("ephemeral_5m_input_tokens"), cache.num("ephemeral_1h_input_tokens")
			write5m := usage.num("cache_creation_input_tokens")
			// Older transcripts report only the total; it is the 5-minute tier.
			if cache5m != 0 || cache1h != 0 {
				write5m = cache5m
			}
			row := MsgRow{Key: key, Ts: ts, Model: models.add(model), Project: project, Input: usage.num("input_tokens"),
				Output: usage.num("output_tokens"), CacheRead: usage.num("cache_read_input_tokens"), W5m: write5m, W1h: cache1h}
			if i, has := msgIndex[key]; has {
				prev := &facts.Messages[i]
				prev.Input, prev.Output = math.Max(prev.Input, row.Input), math.Max(prev.Output, row.Output)
				prev.CacheRead, prev.W5m, prev.W1h = math.Max(prev.CacheRead, row.CacheRead), math.Max(prev.W5m, row.W5m), math.Max(prev.W1h, row.W1h)
				return nil
			}
			msgIndex[key] = len(facts.Messages)
			facts.Messages = append(facts.Messages, row)
			return nil
		case "system":
			uuid, present := e.nullish("uuid")
			key := tsKey(uuid, present, ts)
			if e.is("subtype", "turn_duration") && !sidechain {
				facts.Turns = append(facts.Turns, TurnRow{Key: key, Ts: ts, Ms: e.num("durationMs")})
			} else if e.is("subtype", "compact_boundary") {
				facts.Compactions = append(facts.Compactions, EventRow{Key: key, Ts: ts})
			}
			return nil
		case "attachment":
			att := objectOf(e.raw("attachment"))
			if !att.is("type", "queued_command") || sidechain {
				return nil
			}
			text, ok := att.str("prompt")
			if !ok {
				text = textOf(att.raw("prompt"))
			}
			if jscompat.Trim(text) == "" || harnessPrefix.MatchString(text) {
				return nil
			}
			facts.touch(ts)
			uuid, present := e.nullish("uuid")
			facts.Prompts = append(facts.Prompts, PromptRow{Key: tsKey(uuid, present, ts), Ts: ts, Project: project})
			p.tasks(text, &tasks)
			facts.firstPromptOnce(text)
			return nil
		case "user":
		default:
			return nil
		}
		content := objectOf(e.raw("message")).raw("content")
		if len(content) > 0 && content[0] == '[' {
			for _, b := range elements(content) {
				o := objectOf(b)
				if o == nil || !o.is("type", "tool_result") {
					continue
				}
				id, ok := o.nullish("tool_use_id")
				if !ok {
					uuid, present := e.value("uuid")
					id = template(uuid, present)
				}
				isErr := 0.0
				if o.isTrue("is_error") {
					isErr = 1
				}
				facts.Tools = append(facts.Tools, ToolRow{Key: id, Ts: ts, Error: isErr})
				text, ok := o.str("content")
				if !ok {
					text = textOf(o.raw("content"))
				}
				if strings.HasPrefix(text, denialText) {
					facts.Denials = append(facts.Denials, EventRow{Key: id, Ts: ts})
				}
			}
		}
		if e.isTrue("isMeta") || e.isTrue("isCompactSummary") || sidechain {
			return nil
		}
		text := textOf(content)
		if jscompat.Trim(text) == "" {
			return nil
		}
		uuid, present := e.nullish("uuid")
		if strings.HasPrefix(text, interruptText) {
			facts.Interrupts = append(facts.Interrupts, EventRow{Key: tsKey(uuid, present, ts), Ts: ts})
			return nil
		}
		if harnessPrefix.MatchString(text) {
			return nil
		}
		facts.touch(ts)
		facts.Prompts = append(facts.Prompts, PromptRow{Key: tsKey(uuid, present, ts), Ts: ts, Project: project})
		p.tasks(text, &tasks)
		facts.firstPromptOnce(text)
		return nil
	})
	if err != nil {
		return nil, err
	}
	facts.Models = models.values
	if facts.Models == nil {
		facts.Models = []any{}
	}
	facts.Projects = projectStrings(projects)
	facts.Tasks, facts.PRs = tasks.values(), prs.values()
	return facts, nil
}

// tsKey is String(uuid ?? ts).
func tsKey(uuid any, present bool, ts float64) string {
	if present {
		return jscompat.String(uuid)
	}
	return jscompat.Number(ts)
}

// ParseCodex reduces a Codex rollout.
func (p *Projects) ParseCodex(path string) (*Facts, error) {
	facts := newFacts("codex", jscompat.Basename(path, ".jsonl"), false)
	models, projects := &interner{}, &interner{}
	var tasks ordered
	turnStarts := map[string]float64{}
	model := defaultCodexAI
	project := projects.add(noProject)
	var prev [4]float64 // input, cached input, cache write, output of the last cumulative total
	hasPrev := false
	prompt := func(key any, ts float64, text string) {
		if jscompat.Trim(text) == "" || harnessPrefix.MatchString(text) {
			return
		}
		facts.touch(ts)
		facts.Prompts = append(facts.Prompts, PromptRow{Key: key, Ts: ts, Project: project})
		p.tasks(text, &tasks)
		facts.firstPromptOnce(text)
	}
	err := eachLine(path, func(line []byte) error {
		e, ok, err := parseLine(line)
		if err != nil || !ok {
			return err
		}
		payload := objectOf(e.raw("payload"))
		ts := tsSec(e)
		typ, _ := e.str("type")
		switch typ {
		case "session_meta":
			if id, ok := payload.str("id"); ok {
				facts.SessionID = id
			}
			if cwd, ok := payload.str("cwd"); ok {
				project = projects.add(p.Of(cwd, true))
			}
			// Subagent threads replay their parent's history; they count as sidechains.
			if src, ok := payload.str("thread_source"); ok && src != "user" {
				facts.Sidechain = true
			}
			return nil
		case "turn_context":
			if m, ok := payload.str("model"); ok {
				model = m
			}
			if cwd, ok := payload.str("cwd"); ok {
				project = projects.add(p.Of(cwd, true))
			}
			return nil
		case "event_msg":
		default:
			return nil
		}
		ptype, _ := payload.str("type")
		switch ptype {
		case "user_message":
			if !facts.Sidechain {
				v, ok := payload.nullish("message")
				text := ""
				if ok {
					text = jscompat.String(v)
				}
				prompt(jscompat.Number(ts)+":"+jscompat.Slice(text, 64), ts, text)
			}
		case "item_completed":
			item := objectOf(payload.raw("item"))
			if item.is("type", "UserMessage") && !facts.Sidechain {
				key, ok := item.nullish("id")
				if !ok {
					key = jscompat.Number(ts)
				}
				prompt(key, ts, textOf(item.raw("content")))
			}
		case "task_started":
			if id, ok := payload.str("turn_id"); ok {
				turnStarts[id] = ts
			}
		case "task_complete":
			v, ok := payload.nullish("turn_id")
			id := ""
			if ok {
				id = jscompat.String(v)
			}
			if started := turnStarts[id]; started != 0 && !facts.Sidechain {
				facts.Turns = append(facts.Turns, TurnRow{Key: id, Ts: ts, Ms: (ts - started) * 1000})
			}
		case "turn_aborted":
			v, ok := payload.nullish("turn_id")
			id := ""
			if ok {
				id = jscompat.String(v)
			}
			facts.Interrupts = append(facts.Interrupts, EventRow{Key: jscompat.Number(ts) + ":" + id, Ts: ts})
		case "context_compacted":
			facts.Compactions = append(facts.Compactions, EventRow{Key: jscompat.Number(ts), Ts: ts})
		case "token_count":
			info := objectOf(payload.raw("info"))
			if !info.truthy("total_token_usage") {
				return nil
			}
			total := objectOf(info.raw("total_token_usage"))
			cur := [4]float64{total.num("input_tokens"), total.num("cached_input_tokens"), total.num("cache_write_input_tokens"), total.num("output_tokens")}
			// Snapshots repeat; only a growing cumulative total is new usage.
			if hasPrev && cur[0] == prev[0] && cur[3] == prev[3] {
				return nil
			}
			d := cur
			if hasPrev {
				for i := range d {
					d[i] = cur[i] - prev[i]
				}
			}
			prev, hasPrev = cur, true
			facts.touch(ts)
			cached := math.Max(0, d[1])
			stamp, present := e.value("timestamp")
			// Codex counts cached tokens inside the input; they are split out like Claude's.
			facts.Messages = append(facts.Messages, MsgRow{
				Key: template(stamp, present) + ":" + jscompat.Number(cur[0]) + ":" + jscompat.Number(cur[3]), Ts: ts,
				Model: models.add(model), Project: project, Input: math.Max(0, d[0]-cached), Output: math.Max(0, d[3]),
				CacheRead: cached, W5m: math.Max(0, d[2]),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	facts.Models = models.values
	if facts.Models == nil {
		facts.Models = []any{}
	}
	facts.Projects = projectStrings(projects)
	facts.Tasks = tasks.values()
	return facts, nil
}
