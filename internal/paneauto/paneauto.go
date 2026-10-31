// Package paneauto opens in the right pane what the agent of a conversation works on, without being asked: a design of
// the machine's library whose files it writes, a document it publishes, a board it draws on the workspace's portal or
// the machine's Chrome it drives through a chrome MCP.
// It reads the conversation's finished tool calls and keeps a pane intent for the thing (the same intents `open`
// keeps), once per conversation: the next edit of a design changes nothing (its board updates by itself), the next
// publication of a document asks a tab that shows it to reload. Conservative on purpose: only the pane's own apps, only
// what a successful call names.
package paneauto

import (
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/paneintents"
	"github.com/ly-solutions/harnessly/internal/panel"
	"github.com/ly-solutions/harnessly/internal/wire"
)

const (
	// Fresh: a call older than this is history read again (a restart, a transcript found late), not work going on.
	Fresh = 10 * time.Minute
	// ReloadEvery: a document published again within this of the last reload asks no other.
	ReloadEvery = 15 * time.Second
	// rememberedEntries bounds the calls already looked at (an entry is published again as it updates).
	rememberedEntries = 512
)

// Env is what the watcher reads and writes.
type Env struct {
	// Targets classifies the links a call names, by the rules of `open`.
	Targets panel.Targets
	// DesignOf is the design of the library an absolute path lies in, "" for none; nil: the machine has no library.
	DesignOf func(path string) string
	// Intents keeps what the pane is asked; TabOf is the layout tab of a conversation, whose pane it is ("" for none).
	Intents *paneintents.Store
	TabOf   func(conv string) string
	// Changed is told of a tab whose intents changed (the sync socket's "pane-intents").
	Changed func(tabID string)
	Now     func() time.Time
	// Chrome: the machine shows its Chrome in the pane; a call of a chrome MCP opens it.
	Chrome bool
}

// Found is a thing of the pane a call worked on.
type Found struct {
	Tab, Target string
	// Reloadable: a later call on it reloads the tab that shows it (a document published again).
	Reloadable bool
}

var (
	editTools   = map[string]bool{"Write": true, "Edit": true, "MultiEdit": true, "NotebookEdit": true, "apply_patch": true}
	shellTools  = map[string]bool{"Bash": true, "exec": true, "command": true}
	absPathRe   = regexp.MustCompile(`(?:^|[\s'"=(])(/(?:[\w.@+-]+/)*[\w.@+-]+)`)
	linkRe      = regexp.MustCompile(`https?://[^\s'"<>()\[\]` + "`" + `]+`)
	publishRe   = regexp.MustCompile(`(?:^|[\s;&|(/])[\w-]*artifact\s+(?:publish|update)\b`)
	boardEditRe = regexp.MustCompile(`(?:^|[\s;&|(/])[\w-]*board\s+(?:create|edit|bind)\b`)
)

// IsChromeTool says whether a tool is one of a chrome MCP (mcp__chrome__click, mcp__chrome-devtools__navigate_page).
func IsChromeTool(tool string) bool {
	rest, ok := strings.CutPrefix(tool, "mcp__")
	if !ok {
		return false
	}
	server, _, ok := strings.Cut(rest, "__")
	return ok && strings.Contains(strings.ToLower(server), "chrome")
}

// Detect is what a finished, successful tool call worked on; nothing for any other entry.
func Detect(e wire.Entry, env Env) []Found {
	if e.T != wire.EntryTool || e.OK == nil || !*e.OK {
		return nil
	}
	switch {
	case env.Chrome && IsChromeTool(e.Tool):
		return []Found{{Tab: "chrome"}}
	case editTools[e.Tool]:
		if env.DesignOf == nil {
			return nil
		}
		var out []Found
		seen := map[string]bool{}
		for _, m := range absPathRe.FindAllStringSubmatch(e.Summary, -1) {
			if id := env.DesignOf(m[1]); id != "" && !seen[id] {
				seen[id] = true
				out = append(out, Found{Tab: "design", Target: id})
			}
		}
		return out
	case shellTools[e.Tool]:
		command := strings.TrimPrefix(e.Summary, "$ ")
		publish, board := publishRe.MatchString(command), boardEditRe.MatchString(command)
		if !publish && !board {
			return nil
		}
		// What the command printed names the document first (a new one has no address before); then the command's own.
		texts := []string{command}
		if e.Output != nil {
			texts = append([]string{e.Output.Text}, texts...)
		}
		for _, text := range texts {
			for _, link := range linkRe.FindAllString(text, -1) {
				link = strings.TrimRight(link, ".,;:!?")
				if tab, target := env.Targets.Classify(link); tab == "artifacts" {
					// A board draws itself again as it changes; a document is read again.
					return []Found{{Tab: tab, Target: target, Reloadable: publish && !strings.Contains(target, "/b/")}}
				}
			}
		}
	}
	return nil
}

// Watcher keeps the intents of what the conversations' agents work on.
type Watcher struct {
	env Env

	mu      sync.Mutex
	looked  map[string]bool
	order   []string
	opened  map[string]bool
	reloads map[string]time.Time
}

// New is a watcher over env.
func New(env Env) *Watcher {
	if env.Now == nil {
		env.Now = time.Now
	}
	return &Watcher{env: env, looked: map[string]bool{}, opened: map[string]bool{}, reloads: map[string]time.Time{}}
}

func sameThing(cmd *jsvalue.Object, f Found) bool {
	return cmd != nil && cmd.String("kind") == "open" && cmd.String("tab") == f.Tab && cmd.String("target") == f.Target
}

func isReload(cmd *jsvalue.Object) bool { r, _ := cmd.Get("reload"); return r == true }

func (w *Watcher) remember(id string) bool {
	if w.looked[id] {
		return false
	}
	w.looked[id] = true
	w.order = append(w.order, id)
	if len(w.order) > rememberedEntries {
		delete(w.looked, w.order[0])
		w.order = w.order[1:]
	}
	return true
}

// Entry looks at an entry of a conversation as it is published; it returns whether it kept an intent.
func (w *Watcher) Entry(conv string, e wire.Entry) bool {
	found := Detect(e, w.env)
	if len(found) == 0 || w.env.Intents == nil || w.env.TabOf == nil {
		return false
	}
	now := w.env.Now()
	if time.UnixMilli(int64(e.At)).Before(now.Add(-Fresh)) {
		return false
	}
	tabID := w.env.TabOf(conv)
	if tabID == "" {
		return false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.remember(conv + "\x00" + e.ID) {
		return false
	}
	kept := false
	for _, f := range found {
		if w.keep(tabID, f, now) {
			kept = true
		}
	}
	if kept && w.env.Changed != nil {
		w.env.Changed(tabID)
	}
	return kept
}

// keep adds the open of a thing the conversation's pane was never asked for, or the reload of one it was.
func (w *Watcher) keep(tabID string, f Found, now time.Time) bool {
	key := tabID + "\x00" + f.Tab + "\x00" + f.Target
	known := w.opened[key]
	pendingReload := false
	for _, it := range w.env.Intents.List(tabID) {
		cmd := it.Object("command")
		if !sameThing(cmd, f) {
			continue
		}
		if !isReload(cmd) {
			known = true
		} else if at, _ := it.Get("appliedAt"); at == nil {
			pendingReload = true
		}
	}
	w.opened[key] = true
	if !known {
		w.env.Intents.Add(tabID, panel.Command{Kind: "open", Tab: f.Tab, Target: f.Target, Auto: true}.JSON())
		return true
	}
	if !f.Reloadable || pendingReload || now.Sub(w.reloads[key]) < ReloadEvery {
		return false
	}
	w.reloads[key] = now
	w.env.Intents.Add(tabID, panel.Command{Kind: "open", Tab: f.Tab, Target: f.Target, Auto: true, Reload: true}.JSON())
	return true
}

// OpenOnce asks the conversation's pane to open a tab on a target, unless the conversation was asked for the very thing
// already (within the intents' lifetime): the way a handed-over file opens, once, as `open` does. It returns whether it
// kept an intent, which the caller tells the pages of.
func OpenOnce(intents *paneintents.Store, tabID, tab, target string) bool {
	for _, it := range intents.List(tabID) {
		if cmd := it.Object("command"); sameThing(cmd, Found{Tab: tab, Target: target}) && !isReload(cmd) {
			return false
		}
	}
	intents.Add(tabID, panel.Command{Kind: "open", Tab: tab, Target: target, Auto: true}.JSON())
	return true
}
