package sources

import (
	"sort"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/outputs"
	"github.com/ly-solutions/harnessly/internal/preview/weburl"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// MadeLink is a document, a board, a design or a tracker task the conversation made, opened or changed. Ref is the
// page's address (a board keeps its ?k= key, which opens it), the design's id or the task's address.
type MadeLink struct {
	Kind  wire.MadeKind
	Ref   string
	Title string
	Op    wire.OutputOp
	At    wire.Ms
}

// youtrackTools are the tracker's tools (the MCP server's names, after its prefix) that make or change a task, and
// what each does to it. A search or a read of a list is not here: it touches nothing.
var youtrackTools = map[string]wire.OutputOp{
	"create_issue":          wire.OutputCreated,
	"create_draft_issue":    wire.OutputCreated,
	"update_issue":          wire.OutputChanged,
	"add_issue_comment":     wire.OutputChanged,
	"change_issue_assignee": wire.OutputChanged,
	"link_issues":           wire.OutputChanged,
	"log_work":              wire.OutputChanged,
	"manage_issue_tags":     wire.OutputChanged,
	"get_issue":             wire.OutputChanged,
	"get_issue_comments":    wire.OutputChanged,
}

// madeTasksMax is the most tasks one chat lists: past it the oldest are left out.
const madeTasksMax = 24

// trackerCall is a call of a tracker tool waiting for its result: what it does and the task keys its text named.
type trackerCall struct {
	op   wire.OutputOp
	keys []string
}

const (
	// madePendingMax bounds the calls waiting for their result; a transcript never lets one wait for long.
	madePendingMax = 1024
	madeMax        = 4096
)

// Made collects what a conversation made or touched: what package outputs tells of its calls (a published page, a
// page shown with `harnessly open`, a write into a design) and every page of the catalogue named by the link list of
// a successful tool result (a board made by a script, a page read back). A link only the agent's text or the person
// mentions is not one: it is quoted, not touched.
type Made struct {
	Links *Links

	pending map[string]outputs.Call
	tracker map[string]trackerCall
	byKey   map[string]*MadeLink
	order   []string
}

func (m *Made) config() outputs.Config {
	if m.Links == nil {
		return outputs.Config{}
	}
	return outputs.Config{DocsHost: m.Links.DocsHost, ReaderHost: m.Links.ReaderHost, ReaderPath: m.Links.ReaderPath}
}

func tsMs(e *jsvalue.Object) wire.Ms {
	f := toNumber(mustGet(e, "timestamp"))
	if f != f || f < 0 {
		return 0
	}
	return wire.Ms(f)
}

func mustGet(e *jsvalue.Object, k string) any {
	v, _ := e.Get(k)
	return v
}

// Add takes in the calls and results among entries.
func (m *Made) Add(entries []*jsvalue.Object) {
	for _, e := range entries {
		at := tsMs(e)
		switch e.String("type") {
		case "tool-call":
			m.call(e, at)
		case "tool-result":
			use := e.String("toolUseId")
			if tc, ok := m.tracker[use]; ok {
				delete(m.tracker, use)
				if v, _ := e.Get("isError"); v != true {
					m.takeTask(tc, e, at)
				}
			}
			c, known := m.pending[use]
			delete(m.pending, use)
			if v, _ := e.Get("isError"); v == true {
				continue
			}
			var links []string
			for _, v := range e.Array("links") {
				if s, ok := v.(string); ok {
					links = append(links, s)
				}
			}
			if known {
				m.take(c, links, at)
			}
			m.takeLinks(links, at)
		case "exec-command-stream":
			if s := e.String("status"); s == "error" || s == "failed" {
				continue
			}
			links := outputs.LinksIn(e.String("stdout"))
			m.take(outputs.Call{Tool: "Bash", Command: e.String("command")}, links, at)
			m.takeLinks(links, at)
		}
	}
}

func (m *Made) call(e *jsvalue.Object, at wire.Ms) {
	tool := e.String("toolName")
	if p := e.String("filePath"); p != "" {
		switch tool {
		case "Write", "Edit", "MultiEdit", "NotebookEdit":
			if out, ok := outputs.Wrote(p, tool == "Write"); ok && out.Kind == wire.OutputDesign {
				m.put(MadeLink{Kind: wire.MadeDesign, Ref: out.Ref, Title: "", Op: wire.OutputChanged, At: at}, "design:"+out.Ref)
			}
		}
	}
	use := e.String("toolUseId")
	if use == "" {
		return
	}
	if op, ok := trackerOp(tool); ok && m.Links != nil {
		if m.tracker == nil || len(m.tracker) >= madePendingMax {
			m.tracker = map[string]trackerCall{}
		}
		m.tracker[use] = trackerCall{op: op, keys: m.Links.issueKeys(e.String("summary"))}
		return
	}
	c := outputs.Call{Tool: tool}
	if v, ok := e.Get("publish"); ok {
		c.Publish, _ = v.(*jsvalue.Object)
	}
	if tool == "Bash" {
		c.Command = strings.TrimPrefix(e.String("summary"), "$ ")
	}
	if c.Publish == nil && c.Command == "" {
		return
	}
	if m.pending == nil || len(m.pending) >= madePendingMax {
		m.pending = map[string]outputs.Call{}
	}
	m.pending[use] = c
}

// trackerOp is what a tool of the tracker's MCP server does to a task.
func trackerOp(tool string) (wire.OutputOp, bool) {
	i := strings.LastIndex(tool, "youtrack__")
	if !strings.HasPrefix(tool, "mcp__") || i < 0 {
		return "", false
	}
	op, ok := youtrackTools[tool[i+len("youtrack__"):]]
	return op, ok
}

// takeTask keeps the task a finished tracker call made or touched: the key its text named, else the one its result
// links to (a created task is only known by its result).
func (m *Made) takeTask(c trackerCall, result *jsvalue.Object, at wire.Ms) {
	keys := c.keys
	if c.op == wire.OutputCreated || len(keys) == 0 {
		keys = nil
		for _, v := range result.Array("links") {
			if s, ok := v.(string); ok {
				if key := m.Links.issueKeyOf(s); key != "" {
					keys = append(keys, key)
					break
				}
			}
		}
	}
	for _, key := range keys {
		m.put(MadeLink{Kind: wire.MadeTask, Ref: m.Links.issueURL(key), Op: c.op, At: at}, "task:"+key)
	}
}

// take keeps the pages and designs a finished call made.
func (m *Made) take(c outputs.Call, links []string, at wire.Ms) {
	for _, out := range outputs.Of(m.config(), c, links) {
		switch out.Kind {
		case wire.OutputArtifact:
			op := out.Op
			if !outputs.Publishes(c) {
				op = wire.OutputChanged // shown, not made
			}
			m.page(out.Ref, out.Title, op, at)
		case wire.OutputDesign:
			m.put(MadeLink{Kind: wire.MadeDesign, Ref: out.Ref, Op: out.Op, At: at}, "design:"+out.Ref)
		}
	}
}

// takeLinks keeps every page of the catalogue the result names.
func (m *Made) takeLinks(links []string, at wire.Ms) {
	if m.Links == nil {
		return
	}
	for _, l := range links {
		if m.Links.docSlug(cleanURL(l)) != "" {
			m.page(l, "", wire.OutputChanged, at)
		}
	}
}

// page keeps one page of the catalogue, a board when its path says so; a claude.ai artifact counts as a document.
func (m *Made) page(ref, title string, op wire.OutputOp, at wire.Ms) {
	ref = cleanURL(ref)
	kind, key := wire.MadeDoc, "ref:"+ref
	if u, ok := weburl.Parse(ref); ok {
		if isBoardPath(u.Path) {
			kind = wire.MadeBoard
		}
		if m.Links != nil {
			if slug := m.Links.docSlug(ref); slug != "" {
				key = "doc:" + slug
			}
		}
	}
	if title == outputs.SlugOf(ref) {
		title = ""
	}
	m.put(MadeLink{Kind: kind, Ref: ref, Title: title, Op: op, At: at}, key)
}

// put adds a thing once: it keeps the newest time, a name once known, a board's key once seen, and «created» wins.
func (m *Made) put(l MadeLink, key string) {
	if m.byKey == nil {
		m.byKey = map[string]*MadeLink{}
	}
	have := m.byKey[key]
	if have == nil {
		if len(m.order) >= madeMax {
			return
		}
		c := l
		m.byKey[key] = &c
		m.order = append(m.order, key)
		return
	}
	newer := l.At > have.At
	if newer {
		have.At = l.At
	}
	// A board's key is what opens it: a link without one never replaces a link with one.
	keyed := func(ref string) bool { return strings.Contains(ref, "?k=") }
	if l.Kind != wire.MadeDesign && (keyed(l.Ref) || newer && !keyed(have.Ref)) {
		have.Ref = l.Ref
	}
	if have.Title == "" {
		have.Title = l.Title
	}
	if l.Op == wire.OutputCreated {
		have.Op = l.Op
	}
}

// List is the things, newest first; of the tasks only the newest madeTasksMax.
func (m *Made) List() []MadeLink {
	out := make([]MadeLink, 0, len(m.order))
	for _, k := range m.order {
		out = append(out, *m.byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At > out[j].At })
	tasks := 0
	kept := out[:0]
	for _, l := range out {
		if l.Kind == wire.MadeTask {
			if tasks == madeTasksMax {
				continue
			}
			tasks++
		}
		kept = append(kept, l)
	}
	return kept
}
