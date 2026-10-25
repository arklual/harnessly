package feed

import (
	"fmt"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// SignalKind is what an entry tells the conversation's actor beyond the feed: the agent asks, a gate resolved, a
// user entry the outbox may own.
type SignalKind string

const (
	SignalUser         SignalKind = "user"          // a user entry: maybe the message on its way
	SignalTurnOpened   SignalKind = "turn-opened"   // a user entry (or a task's notification between turns) opened a turn
	SignalTurnEnded    SignalKind = "turn-ended"    // the transcript's end of a turn
	SignalQuestion     SignalKind = "question"      // an AskUserQuestion without its answer
	SignalPlan         SignalKind = "plan"          // ExitPlanMode waiting for approval
	SignalPermission   SignalKind = "permission"    // Codex asks to approve a command or a patch
	SignalGateResolved SignalKind = "gate-resolved" // the tool result of a question, a plan or an approval
	SignalInterrupt    SignalKind = "interrupt"     // the transcript shows the turn interrupted
)

// Signal is one such thing; Offset is a lower bound of where its line starts (-1 unknown).
type Signal struct {
	K          SignalKind
	EntryID    string
	TurnID     string
	GateID     string
	Offset     int64
	At         wire.Ms
	Question   *wire.Question
	Permission *wire.Permission
}

// turn is a turn being built: its record, and where its entries are.
type turn struct {
	rec     wire.Turn
	entries []string
	// sure: a message the person (or another session) typed opened it. A turn opened by a message delivered into
	// the queue or by a task's notification only because no turn was open may be the middle of a turn whose start a
	// read from the middle of the file has not reached (feed.lead).
	sure bool
}

// builder turns the transcript's timeline entries (the shape the Legacy view draws) into the feed: wire entries with
// their stable ids, cut into turns. It is fed one entry at a time, so a whole read and an incremental one go the same
// way; what an entry changes of an earlier one (a tool's result) is an update of it with rev+1.
type builder struct {
	conv     string
	provider wire.Provider
	// model is the model the agent last answered with, as its transcript names it ("" not said).
	model string
	// context is the tokens its last answer was asked with (the input, cached or not): how full the window is.
	context int64

	entries []wire.Entry
	offsets []int64
	byID    map[string]int
	tools   map[string]string // tool use id → entry id
	gates   map[string]string // tool use id → gate id of a question, a plan or an approval
	turns   []*turn
	byTurn  map[string]int
	open    *turn
	// late is a turn the runtime ended (a stop hook ahead of its transcript lines) that the transcript has not ended
	// itself: lines read now are still its own, until the transcript's turn-end or the next user message.
	late *turn
	// outputs is the whole output of each tool entry, by its id.
	outputs map[string]string
	// tasks is the agent's task list, from the task-progress entries.
	tasks taskList
	// calls are the calls waiting for their results to tell what they made (outputs.go), by tool use id.
	calls map[string]call
	// teammates are the entries of the named teammates the agent started (an Agent call with a name), by name.
	teammates map[string]string
	// limits are the notices of a usage limit the agent hit, by entry id.
	limits map[string]Limit
}

func newBuilder(conv string, provider wire.Provider) *builder {
	return &builder{conv: conv, provider: provider, byID: map[string]int{}, tools: map[string]string{},
		gates: map[string]string{}, byTurn: map[string]int{}, outputs: map[string]string{}, calls: map[string]call{},
		teammates: map[string]string{}, limits: map[string]Limit{}}
}

// step is what one source entry did.
type step struct {
	added   []int // indexes of new entries
	updated []int // indexes of entries changed
	started []string
	ended   []string
	// regrown are the turns that took an entry after they ended: their records have changed.
	regrown []string
	signals []Signal
}

func str(o *jsvalue.Object, k string) string { return o.String(k) }

func num(o *jsvalue.Object, k string) (float64, bool) {
	v, ok := o.Get(k)
	f, isNum := v.(float64)
	return f, ok && isNum
}

func ptrNum(o *jsvalue.Object, k string) *int64 {
	if f, ok := num(o, k); ok {
		n := int64(f)
		return &n
	}
	return nil
}

func truthy(o *jsvalue.Object, k string) bool {
	v, _ := o.Get(k)
	b, ok := v.(bool)
	return ok && b
}

func sptr(s string) *string { return &s }

// add takes one source entry read at offset (a lower bound of its line, -1 unknown).
func (b *builder) add(o *jsvalue.Object, offset int64) step {
	var s step
	id := str(o, "id")
	if !wire.ValidEntryID(id) {
		return s
	}
	at := wire.Ms(0)
	if f, ok := num(o, "timestamp"); ok {
		at = wire.Ms(f)
	}
	switch typ := str(o, "type"); typ {
	case "turn-end":
		b.endTurn(&s, at, wire.TurnDone)
		b.late = nil
		return s
	case "tool-result":
		b.result(&s, o, id, at, offset)
		return s
	case "task-progress":
		b.tasks.progress(o)
		return s
	case "user-message":
		if truthy(o, "pending") {
			return s // Claude's own queue: the message is ours to show, the agent's copy would double it
		}
		if notes := peerNotes(str(o, "text")); len(notes) > 0 {
			notes = b.teammatesSettle(&s, notes)
			b.injected(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryPeer, Peers: notes}, truthy(o, "queued"), offset)
			return s
		}
		if note, ok := systemNote(str(o, "text")); ok {
			b.injected(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryNotice, Level: wire.NoticeInfo, Code: systemNoteCode,
				Detail: sptr(note)}, truthy(o, "queued"), offset)
			return s
		}
		e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryUser, Text: str(o, "text"), Attachments: images(o)}
		if truthy(o, "queued") && b.open != nil {
			// A message delivered into the running turn (steered) is part of it.
			b.push(&s, e, offset)
		} else {
			b.endTurn(&s, at, wire.TurnDone)
			b.late = nil
			b.openTurn(&s, id, at)
			b.open.sure = !truthy(o, "queued")
			b.push(&s, e, offset)
			s.signals = append(s.signals, Signal{K: SignalTurnOpened, EntryID: id, TurnID: id, Offset: offset, At: at})
		}
		s.signals = append(s.signals, Signal{K: SignalUser, EntryID: id, TurnID: b.openID(), Offset: offset, At: at})
		return s
	case "interrupt":
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryInterrupt}, offset)
		if b.open != nil {
			s.signals = append(s.signals, Signal{K: SignalInterrupt, EntryID: id, TurnID: b.openID(), Offset: offset, At: at})
			b.endTurn(&s, at, wire.TurnInterrupted)
		}
		b.late = nil
		return s
	case "assistant-message":
		if m := str(o, "model"); m != "" && m != "<synthetic>" {
			b.model = m
		}
		if v, ok := o.Get("usage"); ok {
			if u, ok := v.(*jsvalue.Object); ok {
				in, _ := num(u, "input_tokens")
				created, _ := num(u, "cache_creation_input_tokens")
				read, _ := num(u, "cache_read_input_tokens")
				if n := int64(in + created + read); n > 0 {
					b.context = n
				}
			}
		}
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntrySay, MD: str(o, "markdown")}, offset)
	case "thinking", "reasoning-summary":
		// Claude keeps its thinking to itself (a signature, no text): nothing to show, as Legacy shows nothing.
		text := str(o, "thinking")
		if typ == "reasoning-summary" {
			text = summaryText(o)
		}
		if strings.TrimSpace(text) != "" {
			b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryThink, Text: text}, offset)
		}
	case "error-notice":
		level := wire.NoticeInfo
		switch str(o, "severity") {
		case "error":
			level = wire.NoticeError
		case "warning", "warn":
			level = wire.NoticeWarn
		}
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryNotice, Level: level, Code: "agent-notice",
			Detail: sptr(str(o, "message"))}, offset)
		if l, ok := limitOf(o, b.provider, at); ok {
			b.limits[id] = l
		}
	case "session-exit":
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryNotice, Level: wire.NoticeInfo, Code: "session-exit"}, offset)
	case "task-notification":
		// A background task that wakes the agent between turns opens a turn of its own, as a message does: the agent's
		// answer to it is a turn of the feed, not lines outside any.
		opens := b.open == nil
		if opens {
			b.late = nil
			b.openTurn(&s, id, at)
		}
		returned := b.finishSubagent(&s, o, id, at, opens, offset)
		if !returned {
			b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryNotice, Level: wire.NoticeInfo, Code: "task-notification",
				Detail: sptr(str(o, "summary"))}, offset)
		}
		if opens {
			s.signals = append(s.signals, Signal{K: SignalTurnOpened, EntryID: id, TurnID: id, Offset: offset, At: at})
		}
	case "context-compacted":
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryCompaction, TokensBefore: ptrNum(o, "beforeTokens"),
			TokensAfter: ptrNum(o, "afterTokens"), Keep: []string{}}, offset)
	case "agent-group":
		if n := len(b.entries) - 1; n >= 0 && b.entries[n].T == wire.EntrySubagent && b.entries[n].AgentID == b.entries[n].ID && offset >= 0 && b.offsets[n] >= 0 &&
			b.offsets[n] < offset {
			// A read that began inside a subagent's run: its lines go on the same group, as a whole read makes one.
			e := b.entries[n]
			e.Rev++
			b.entries[n] = e
			s.updated = append(s.updated, n)
			return s
		}
		b.push(&s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntrySubagent, AgentID: id, AgentType: str(o, "agentType"),
			Description: str(o, "description"), Status: wire.SubagentDone}, offset)
	case "ask-user-question":
		b.question(&s, o, id, at, offset)
	case "plan":
		b.plan(&s, o, id, at, offset)
	case "approval-request":
		b.approval(&s, o, id, at, offset)
	case "slash-command":
		summary := strings.TrimSpace("/" + str(o, "name") + " " + str(o, "args"))
		e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryTool, ToolUseID: id, Tool: "command", Summary: summary}
		if out, ok := o.Get("output"); ok {
			text, _ := out.(string)
			e.Output, e.OK = b.output(id, text), okOf(!truthy(o, "isError"))
		}
		b.push(&s, e, offset)
	case "command-output":
		text := str(o, "output")
		if i := b.commandForOutput(); i >= 0 {
			// A read that began between a command and its output: the output joins the command, as a whole read does.
			e := b.entries[i]
			e.Output, e.OK, e.Rev = b.output(e.ID, text), okOf(!truthy(o, "isError")), e.Rev+1
			b.entries[i] = e
			s.updated = append(s.updated, i)
			return s
		}
		e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryTool, ToolUseID: id, Tool: "command", Summary: firstLine(text),
			Output: b.output(id, text), OK: okOf(!truthy(o, "isError"))}
		b.push(&s, e, offset)
	default:
		b.tool(&s, o, typ, id, at, offset)
	}
	return s
}

// injected is a user line someone other than the person wrote — another agent's session, the harness: delivered into
// the running turn it is a step of it, else it opens a turn as a message does. It is never the person's message, so
// it is not the outbox's to claim.
func (b *builder) injected(s *step, e wire.Entry, queued bool, offset int64) {
	id, at := e.ID, e.At
	if queued && b.open != nil {
		b.push(s, e, offset)
		return
	}
	b.endTurn(s, at, wire.TurnDone)
	b.late = nil
	b.openTurn(s, id, at)
	b.open.sure = !queued
	b.push(s, e, offset)
	s.signals = append(s.signals, Signal{K: SignalTurnOpened, EntryID: id, TurnID: id, Offset: offset, At: at})
}

func okOf(ok bool) *bool { return &ok }

func summaryText(o *jsvalue.Object) string {
	v, _ := o.Get("summary")
	switch x := v.(type) {
	case string:
		return x
	case []any:
		parts := make([]string, 0, len(x))
		for _, p := range x {
			if s, ok := p.(string); ok {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return clip(line, 200)
}

// peerSummaryMax is how much of a message to another agent its tool step carries.
const peerSummaryMax = 4000

func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func images(o *jsvalue.Object) []wire.AttachmentRef {
	out := []wire.AttachmentRef{}
	v, _ := o.Get("images")
	list, _ := v.([]any)
	for _, it := range list {
		p, ok := it.(string)
		if !ok {
			if obj, isObj := it.(*jsvalue.Object); isObj {
				p = obj.String("path")
			}
		}
		if p == "" {
			continue
		}
		out = append(out, imageRef(p, len(out)))
	}
	return out
}

// imageRef names a picture: a file by its name, one carried inline in the transcript (data:image/png;base64,…) by its
// place in the message.
func imageRef(p string, n int) wire.AttachmentRef {
	if mime, ok := strings.CutPrefix(p, "data:"); ok {
		mime, _, _ = strings.Cut(mime, ";")
		return wire.AttachmentRef{ID: p, Name: fmt.Sprintf("image-%d.%s", n+1, strings.TrimPrefix(mime, "image/")), Kind: wire.AttachmentImage, Mime: mime}
	}
	return wire.AttachmentRef{ID: p, Name: path.Base(p), Kind: wire.AttachmentImage, Mime: mimeOf(p)}
}

func mimeOf(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return "application/octet-stream"
}

// output keeps a tool's whole output and gives what an event carries: the first wire.ToolOutputMax bytes.
func (b *builder) output(id, text string) *wire.ToolOutput {
	b.outputs[id] = text
	return trimmed(text)
}

func trimmed(text string) *wire.ToolOutput {
	if len(text) <= wire.ToolOutputMax {
		return &wire.ToolOutput{Text: text}
	}
	cut := wire.ToolOutputMax
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return &wire.ToolOutput{Text: text[:cut], Truncated: &wire.OutputTrimmed{Shown: int64(cut), Total: int64(len(text))}}
}

// tool is a tool call of any kind the source has: Claude's tool-call, Codex's commands, searches, MCP calls and
// patches. A kind the feed does not know and that names no call is left out.
func (b *builder) tool(s *step, o *jsvalue.Object, typ, id string, at wire.Ms, offset int64) {
	use := str(o, "toolUseId")
	if use == "" {
		use = str(o, "callId")
	}
	name, summary := str(o, "toolName"), str(o, "summary")
	switch typ {
	case "tool-call":
	case "exec-command-stream":
		name, summary = "exec", str(o, "command")
	case "web-search":
		name, summary = "web_search", str(o, "query")
	case "mcp-tool-call":
		name = "mcp__" + str(o, "server") + "__" + str(o, "tool")
	case "patch-apply":
		name = "apply_patch"
	default:
		if use == "" {
			return
		}
		name = typ
	}
	if use == "" {
		use = id
	}
	if a, isAgent := o.Get("agent"); isAgent && typ == "tool-call" {
		if call, isObj := a.(*jsvalue.Object); isObj {
			b.subagent(s, call, id, use, at, offset)
			return
		}
	}
	if pm, ok := o.Get("peerMessage"); ok && typ == "tool-call" {
		// A message to another agent (Claude's SendMessage): the addressee on the first line, the text under it, so the
		// feed draws it as an outgoing message, not as «SendMessage <id>».
		if p, isObj := pm.(*jsvalue.Object); isObj {
			summary = str(p, "to") + "\n" + clip(str(p, "text"), peerSummaryMax)
		}
	}
	e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryTool, ToolUseID: use, Tool: name, Summary: summary, OK: status(o)}
	if out := str(o, "stdout"); out != "" {
		e.Output = b.output(id, out)
	}
	b.tools[use] = id
	b.push(s, e, offset)
	if typ == "tool-call" {
		b.touch(o, name)
		b.remember(o, use, name, summary)
	}
	if typ == "exec-command-stream" {
		b.ran(summary, str(o, "stdout"))
	}
}

func status(o *jsvalue.Object) *bool {
	switch str(o, "status") {
	case "success", "completed":
		return okOf(true)
	case "error", "failed":
		return okOf(false)
	}
	return nil
}

// touch adds the file of a Write, an Edit or a Read to the open turn's files.
func (b *builder) touch(o *jsvalue.Object, tool string) {
	p := str(o, "filePath")
	cur := b.cur()
	if p == "" || cur == nil {
		return
	}
	op := wire.FileOp("")
	switch tool {
	case "Write":
		op = wire.FileCreated
	case "Edit", "MultiEdit", "NotebookEdit":
		op = wire.FileEdited
	case "Read":
		op = wire.FileRead
	default:
		return
	}
	if op != wire.FileRead {
		if out, made := wrote(p, op == wire.FileCreated); made {
			addOutput(cur, out)
		}
	}
	files := cur.rec.Files
	for i, f := range files {
		if f.Path == p {
			if f.Op == wire.FileRead || op == wire.FileCreated {
				files[i].Op = op
			}
			return
		}
	}
	cur.rec.Files = append(files, wire.TurnFile{Path: p, Op: op})
}

func (b *builder) question(s *step, o *jsvalue.Object, id string, at wire.Ms, offset int64) {
	use := str(o, "toolUseId")
	q := wire.Question{ID: wire.QuestionID(use), ConversationID: b.conv, TurnID: b.openID(), AskedAt: at,
		Items: questionItems(o), State: wire.GateState{K: wire.GateStateOpen}}
	summary := ""
	if len(q.Items) > 0 {
		summary = q.Items[0].Question
	}
	b.tools[use], b.gates[use] = id, q.ID
	b.push(s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryTool, ToolUseID: use, Tool: "AskUserQuestion", Summary: summary}, offset)
	if str(o, "status") == "pending" && use != "" && q.TurnID != "" {
		s.signals = append(s.signals, Signal{K: SignalQuestion, EntryID: id, GateID: q.ID, Offset: offset, At: at, Question: &q})
	}
}

func questionItems(o *jsvalue.Object) []wire.QuestionItem {
	v, _ := o.Get("questions")
	list, _ := v.([]any)
	items := []wire.QuestionItem{}
	for _, it := range list {
		q, ok := it.(*jsvalue.Object)
		if !ok {
			continue
		}
		item := wire.QuestionItem{Header: q.String("header"), Question: q.String("question"), Multi: truthy(q, "multiSelect"),
			Options: []wire.QuestionOption{}}
		ov, _ := q.Get("options")
		opts, _ := ov.([]any)
		for _, op := range opts {
			oo, ok := op.(*jsvalue.Object)
			if !ok {
				continue
			}
			opt := wire.QuestionOption{Label: oo.String("label")}
			if d := oo.String("description"); d != "" {
				opt.Description = sptr(d)
			}
			if p := oo.String("preview"); p != "" {
				opt.Preview = sptr(p)
			}
			item.Options = append(item.Options, opt)
		}
		items = append(items, item)
	}
	return items
}

func (b *builder) plan(s *step, o *jsvalue.Object, id string, at wire.Ms, offset int64) {
	use := str(o, "toolUseId")
	if use == "" {
		// A plan read from its file has no call to approve: it is what the agent wrote.
		b.push(s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntrySay, MD: str(o, "markdown")}, offset)
		return
	}
	pid := wire.PermissionID(use)
	b.tools[use], b.gates[use] = id, pid
	b.push(s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryGate, PermissionID: pid}, offset)
	if str(o, "status") == "pending" {
		md := str(o, "markdown")
		p := wire.Permission{ID: pid, ConversationID: b.conv, TurnID: b.openPtr(), Kind: wire.PermPlan, AskedAt: at,
			PlanMD: &md, Options: []wire.PermissionOption{}, State: wire.GateState{K: wire.GateStateOpen}}
		s.signals = append(s.signals, Signal{K: SignalPlan, EntryID: id, GateID: pid, Offset: offset, At: at, Permission: &p})
	}
}

func (b *builder) approval(s *step, o *jsvalue.Object, id string, at wire.Ms, offset int64) {
	call := str(o, "callId")
	pid := wire.PermissionID(call)
	b.tools[call], b.gates[call] = id, pid
	b.push(s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryGate, PermissionID: pid}, offset)
	if str(o, "status") == "pending" {
		p := wire.Permission{ID: pid, ConversationID: b.conv, TurnID: b.openPtr(), Kind: wire.PermTool, AskedAt: at,
			Options: []wire.PermissionOption{}, State: wire.GateState{K: wire.GateStateOpen}}
		tool := str(o, "approvalKind")
		p.Tool = &tool
		if c := str(o, "command"); c != "" {
			p.Command = &c
		}
		if c := str(o, "cwd"); c != "" {
			p.Cwd = &c
		}
		s.signals = append(s.signals, Signal{K: SignalPermission, EntryID: id, GateID: pid, Offset: offset, At: at, Permission: &p})
	}
}

// result is a tool's result: it settles the tool's entry (an update), answers a question (an answers entry of its
// own) and resolves a gate.
func (b *builder) result(s *step, o *jsvalue.Object, id string, at wire.Ms, offset int64) {
	use := str(o, "toolUseId")
	b.tasks.result(use, str(o, "summary"))
	b.settle(use, o, !truthy(o, "isError"))
	target, ok := b.tools[use]
	if !ok {
		return
	}
	i := b.byID[target]
	e := b.entries[i]
	ok2 := !truthy(o, "isError")
	changed := e.OK == nil || *e.OK != ok2
	if e.T == wire.EntrySubagent {
		changed = b.settleSubagent(&e, target, o, ok2)
	}
	if e.T == wire.EntryTool {
		if sum := str(o, "summary"); sum != "" && e.Tool != "AskUserQuestion" && e.Output == nil {
			e.Output = b.output(target, sum)
			changed = true
		}
		e.OK = &ok2
	}
	if changed {
		e.Rev++
		b.entries[i] = e
		s.updated = append(s.updated, i)
	}
	gate, isGate := b.gates[use]
	if !isGate {
		return
	}
	delete(b.gates, use)
	if wire.ValidQuestionID(gate) {
		pairs := []wire.AnswerPair{}
		if av, has := o.Get("answers"); has {
			if ans, isObj := av.(*jsvalue.Object); isObj {
				for _, k := range ans.Keys() {
					pairs = append(pairs, wire.AnswerPair{Q: k, A: ans.String(k)})
				}
			}
		}
		if len(pairs) > 0 {
			b.push(s, wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntryAnswers, QuestionID: gate, Pairs: pairs}, offset)
		}
	}
	s.signals = append(s.signals, Signal{K: SignalGateResolved, EntryID: id, GateID: gate, Offset: offset, At: at})
}

// cur is the turn a new entry belongs to: the open one, else the one the runtime ended ahead of the transcript.
func (b *builder) cur() *turn {
	if b.open != nil {
		return b.open
	}
	return b.late
}

func (b *builder) openID() string {
	if b.open == nil {
		return ""
	}
	return b.open.rec.ID
}

func (b *builder) openPtr() *string {
	if b.open == nil {
		return nil
	}
	return sptr(b.open.rec.ID)
}

func (b *builder) openTurn(s *step, id string, at wire.Ms) {
	t := &turn{rec: wire.Turn{ID: id, Author: wire.TurnAuthor{Provider: b.provider}, StartedAt: at, Status: wire.TurnLive,
		Files: []wire.TurnFile{}}}
	b.turns = append(b.turns, t)
	b.byTurn[id] = len(b.turns) - 1
	b.open = t
	s.started = append(s.started, id)
}

func (b *builder) endTurn(s *step, at wire.Ms, st wire.TurnStatus) {
	if b.open == nil {
		return
	}
	t := b.open
	b.open = nil
	end := at
	if end < t.rec.StartedAt {
		end = t.rec.StartedAt
	}
	d := end - t.rec.StartedAt
	t.rec.EndedAt, t.rec.DurationMs, t.rec.Status = &end, &d, st
	s.ended = append(s.ended, t.rec.ID)
	s.signals = append(s.signals, Signal{K: SignalTurnEnded, TurnID: t.rec.ID, At: at})
}

func (b *builder) push(s *step, e wire.Entry, offset int64) {
	if _, ok := b.byID[e.ID]; ok {
		// The same line read again (a page that overlaps): the entry is the one there is.
		return
	}
	if t := b.cur(); t != nil {
		id := t.rec.ID
		e.TurnID = &id
		t.entries = append(t.entries, e.ID)
		// The person's message, another agent's or the harness's note that woke the turn, is its opener, not a step.
		if e.T != wire.EntryUser && (e.ID != id || e.T != wire.EntryPeer && e.T != wire.EntryNotice) {
			t.rec.StepCount++
		}
		// The agent that woke the turn is its opener, not an action of it.
		if e.T == wire.EntryTool || e.T == wire.EntrySubagent && e.ID != id {
			t.rec.ToolCount++
		}
		if e.T == wire.EntrySay {
			t.rec.AnswerEntryID = sptr(e.ID)
		}
		if t == b.late && !slices.Contains(s.regrown, id) {
			s.regrown = append(s.regrown, id)
		}
	}
	b.entries = append(b.entries, e)
	b.offsets = append(b.offsets, offset)
	b.byID[e.ID] = len(b.entries) - 1
	s.added = append(s.added, len(b.entries)-1)
}

// commandLookback is how far back a command's output finds its command (claudeCommandLookback).
const commandLookback = 5

// commandForOutput is the last command still without its output among the last entries, -1 none.
func (b *builder) commandForOutput() int {
	for i := len(b.entries) - 1; i >= max(0, len(b.entries)-commandLookback); i-- {
		if e := b.entries[i]; e.T == wire.EntryTool && e.Tool == "command" && e.Output == nil && e.ToolUseID == e.ID {
			return i
		}
	}
	return -1
}

// counts are the turn's counters of an entry, nil outside a turn.
func (b *builder) counts(e wire.Entry) *wire.TurnCounts {
	if e.TurnID == nil {
		return nil
	}
	i, ok := b.byTurn[*e.TurnID]
	if !ok {
		return nil
	}
	t := b.turns[i].rec
	return &wire.TurnCounts{ToolCount: t.ToolCount, StepCount: t.StepCount}
}

// subagent is an Agent call: the agent it starts shows in the feed as a subagent entry, running until its result (a
// foreground agent) or its task notification (a background one) says what it returned. Until the result names the
// agent its entry id stands for it.
func (b *builder) subagent(s *step, call *jsvalue.Object, id, use string, at wire.Ms, offset int64) {
	kind := str(call, "name")
	if kind == "" {
		kind = str(call, "type")
	}
	e := wire.Entry{ID: id, At: at, Rev: 1, T: wire.EntrySubagent, AgentID: use, AgentType: kind,
		Description: str(call, "description"), Status: wire.SubagentRunning}
	if m := str(call, "model"); m != "" {
		e.Model = sptr(m)
	}
	if name := str(call, "name"); name != "" {
		b.teammates[name] = id
	}
	b.tools[use] = id
	b.push(s, e, offset)
}

// settleSubagent applies the result of an Agent call to its entry and reports whether the entry changed. A background
// agent's launch result only names the agent; a foreground agent's result is what it returned.
func (b *builder) settleSubagent(e *wire.Entry, id string, o *jsvalue.Object, ok bool) bool {
	changed := false
	var agent *jsvalue.Object
	if v, has := o.Get("agent"); has {
		agent, _ = v.(*jsvalue.Object)
	}
	if agent != nil {
		if aid := str(agent, "id"); aid != "" && aid != e.AgentID {
			e.AgentID = aid
			changed = true
		}
	}
	if agent == nil || str(agent, "status") != "completed" && str(agent, "status") != "failed" {
		if ok || e.Status == wire.SubagentFailed {
			return changed
		}
		e.Status = wire.SubagentFailed
		return true
	}
	e.Status = wire.SubagentDone
	if !ok || str(agent, "status") == "failed" {
		e.Status = wire.SubagentFailed
	}
	if text := str(agent, "text"); text != "" {
		e.Output = b.output(id, text)
	}
	e.DurationMs = ptrNum(agent, "durationMs")
	return true
}

// teammatesSettle: a named teammate that went idle or shut down is done with what it was sent, and a background
// subagent that handed its report back is done. Its Agent call's entry, running since the launch (the launch result
// only names it), shows it done, with the words it finished with when it said any. A subagent's note is signed with
// what it was started for, not its id.
func (b *builder) teammatesSettle(s *step, notes []wire.PeerNote) []wire.PeerNote {
	for k, n := range notes {
		finished := n.Kind == wire.PeerNoteIdle || n.Kind == wire.PeerNoteDone ||
			n.Kind == wire.PeerNoteEvent && n.Event != nil && *n.Event == "shutdown_approved"
		i, known, handBack := -1, false, false
		if id, named := b.teammates[n.From]; named {
			i, known = b.byID[id]
			notes[k].AgentID = nil
		} else if n.AgentID != nil {
			for j := len(b.entries) - 1; j >= 0 && !known; j-- {
				if b.entries[j].T == wire.EntrySubagent && b.entries[j].AgentID == n.From {
					i, known = j, true
					handBack = n.Kind == wire.PeerNoteDone
				}
			}
			notes[k].From = ""
			if known {
				notes[k].From = b.entries[i].Description
				notes[k].Model = b.entries[i].Model
			}
		}
		if !finished || !known || b.entries[i].T != wire.EntrySubagent {
			continue
		}
		// A hand-back that comes after the task notification still carries the only report the agent gives.
		if b.entries[i].Status != wire.SubagentRunning && !(handBack && b.entries[i].Output == nil) {
			continue
		}
		e := b.entries[i]
		if e.Status == wire.SubagentRunning {
			e.Status = wire.SubagentDone
		}
		if n.Kind == wire.PeerNoteDone && e.Output == nil {
			e.Output = b.output(e.ID, n.MD)
		}
		e.Rev++
		b.entries[i] = e
		s.updated = append(s.updated, i)
	}
	return notes
}

// handBackPointer opens a task notification's result when the agent handed its report back as a message instead: the
// result only points there, and the report comes with the hand-back.
const handBackPointer = "This agent's report was delivered to you as a message from"

// finishSubagent is a background agent's task notification: it settles the agent's entry with what the agent returned.
// In a turn the agent's answer opens, the notification shows as a copy of the entry, the first of the turn; in a turn
// that is running, the entry above is the one that changed. It reports whether the notification found an agent.
func (b *builder) finishSubagent(s *step, o *jsvalue.Object, id string, at wire.Ms, opens bool, offset int64) bool {
	i := -1
	if target, has := b.tools[str(o, "toolUseId")]; has {
		i = b.byID[target]
	}
	task := str(o, "taskId")
	for k := len(b.entries) - 1; i < 0 && k >= 0; k-- {
		if b.entries[k].T == wire.EntrySubagent && b.entries[k].AgentID == task {
			i = k
		}
	}
	if i < 0 || b.entries[i].T != wire.EntrySubagent {
		return false
	}
	e := b.entries[i]
	e.Status = wire.SubagentDone
	switch str(o, "status") {
	case "failed", "killed", "error", "stopped":
		e.Status = wire.SubagentFailed
	}
	res := str(o, "result")
	if strings.HasPrefix(res, handBackPointer) {
		res = ""
	}
	if res != "" {
		e.Output = b.output(e.ID, res)
	}
	if u, has := o.Get("usage"); has {
		if usage, isObj := u.(*jsvalue.Object); isObj {
			e.DurationMs = ptrNum(usage, "durationMs")
		}
	}
	e.Rev++
	b.entries[i] = e
	s.updated = append(s.updated, i)
	if opens {
		card := e
		card.ID, card.At, card.Rev, card.TurnID = id, at, 1, nil
		if res != "" {
			card.Output = b.output(id, res)
		} else if e.Output != nil {
			card.Output = b.output(id, b.outputs[e.ID])
		}
		b.push(s, card, offset)
	}
	return true
}
