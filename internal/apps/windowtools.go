package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
)

// The tools of an app's open window as tools of the host's server. What a view registers from its page
// (pagetools.go) is in tools/list of harnessly-app under a name of its own, <app id with '.' as '_'>__<tool>, with the
// view's description and inputSchema, so the agent calls it in one step: no page_tools, no page_call. The call goes the
// way page_call goes (the page asks the person where the tool says confirm, the same limits). The names are read from
// the page's reports at every tools/list; the GET stream tells the client when the set changes (Offering).

const (
	// WindowToolWait is how long a call of a window's tool waits for the tool to be there: the window may still load, or
	// its view has not registered the tool yet.
	WindowToolWait = 15 * time.Second
	// windowToolPoll is how often a waiting call looks at the page's reports.
	windowToolPoll = 100 * time.Millisecond
	// windowToolSep stands between the app and the tool in a window tool's name. No host tool has it in its name.
	windowToolSep = "__"
	// MaxWindowToolName: Claude Code names the tool mcp__harnessly-app__<name>, and the whole name has to fit the 64
	// characters a tool name may have.
	MaxWindowToolName = 64 - len("mcp__"+hostServer+"__")
)

// WindowTool is a tool of an app's open window under the name the agent calls it by.
type WindowTool struct {
	// Name is the tool's name in the host's server.
	Name string
	App  string
	PageTool
}

var windowNameBad = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// windowToolPrefix starts the names of an app's window tools.
func windowToolPrefix(app string) string {
	return strings.ReplaceAll(app, ".", "_") + windowToolSep
}

// windowLongName is the name as it reads: the app, the separator, the tool with what a tool name may not hold as '_'.
func windowLongName(app, tool string) string {
	return windowToolPrefix(app) + windowNameBad.ReplaceAllString(tool, "_")
}

// windowShortName is the name of a tool whose long name does not fit or is another tool's too: the start of the long
// name and eight hex digits of the hash of the app and the tool, the same at every listing.
func windowShortName(app, tool string) string {
	sum := sha256.Sum256([]byte(app + "\x00" + tool))
	tail := "_" + hex.EncodeToString(sum[:4])
	long := windowLongName(app, tool)
	if room := MaxWindowToolName - len(tail); len(long) > room {
		long = long[:room]
	}
	return long + tail
}

// WindowTools names the tools of the reports, in the reports' order. A name depends only on the app, the tool and
// whether the long name is taken twice in the reports, never on the order.
func WindowTools(reports []PageReport) []WindowTool {
	taken := map[string]int{}
	for _, r := range reports {
		for _, t := range r.Tools {
			taken[windowLongName(r.App, t.Name)]++
		}
	}
	var out []WindowTool
	named := map[string]bool{}
	for _, r := range reports {
		for _, t := range r.Tools {
			name := windowLongName(r.App, t.Name)
			if len(name) > MaxWindowToolName || taken[name] > 1 {
				name = windowShortName(r.App, t.Name)
			}
			// One name is one tool: a second tool that comes to a name already given (two hashes that met) is left out,
			// so a call never reaches another tool than the one listed.
			if named[name] {
				continue
			}
			named[name] = true
			out = append(out, WindowTool{Name: name, App: r.App, PageTool: t})
		}
	}
	return out
}

// IsWindowToolName says whether a tool name has the shape of a window tool's.
func IsWindowToolName(name string) bool { return strings.Contains(name, windowToolSep) }

// HostToolName is the name Claude Code calls a tool of the host's server by.
func HostToolName(name string) string { return "mcp__" + hostServer + "__" + name }

func (t HostTools) enabled() []string {
	if t.Enabled == nil {
		return nil
	}
	return t.Enabled()
}

// windowReports are the reports of the chat's open windows the agent may use: of an app on in the chat (or a builtin
// one), cut to what its rights open.
func (t HostTools) windowReports() []PageReport {
	if t.Pages == nil {
		return nil
	}
	enabled := t.enabled()
	var out []PageReport
	for _, r := range t.Pages.Offered(t.Conv) {
		m := PageApp(r.App, enabled, t.Registry)
		if m == nil {
			continue
		}
		out = append(out, PageReportAllowed(m, r))
	}
	return out
}

// WindowTools are the tools the open windows of the chat's apps offer now.
func (t HostTools) WindowTools() []WindowTool { return WindowTools(t.windowReports()) }

// windowOffering is the part of Offering that follows the windows' tools: their names and a hash of what tools/list
// says of each. A report renewed with the same tools leaves it as it was.
func (t HostTools) windowOffering() string {
	tools := t.WindowTools()
	parts := make([]string, 0, len(tools))
	for _, w := range tools {
		sum := sha256.Sum256(append([]byte(w.Description+"\x00"+strconv.FormatBool(w.Confirm)+"\x00"), w.InputSchema...))
		parts = append(parts, w.Name+":"+hex.EncodeToString(sum[:6]))
	}
	slices.Sort(parts)
	return strings.Join(parts, ",")
}

func (t HostTools) appName(id string) string {
	if m := PageApp(id, t.enabled(), t.Registry); m != nil && m.Name != "" {
		return m.Name
	}
	return id
}

// windowToolDefs are the windows' tools as tools/list gives them. They are marked always loaded: the agent has their
// exact names from the live hook's block and calls them with no search before.
func (t HostTools) windowToolDefs() []any {
	var out []any
	for _, w := range t.WindowTools() {
		schema := jsvalue.NewObject("type", "object")
		if len(w.InputSchema) > 0 {
			if v, err := jsvalue.Parse(w.InputSchema); err == nil {
				if o, ok := v.(*jsvalue.Object); ok {
					schema = o
				}
			}
		}
		desc := strings.TrimSpace(w.Description)
		if desc != "" && !strings.HasSuffix(desc, ".") {
			desc += "."
		}
		desc = strings.TrimSpace(desc + " A tool of the open window of " + t.appName(w.App) + ": it runs in the person's page.")
		if w.Confirm {
			desc += " The person is asked before it runs."
		}
		out = append(out, jsvalue.NewObject("name", w.Name, "description", desc, "inputSchema", schema,
			"_meta", jsvalue.NewObject("anthropic/alwaysLoad", true)))
	}
	return out
}

// windowCall runs a window's tool by its name in the server. A tool that is not there yet is waited for (the window
// loads, the view registers its tools after the handshake); past WindowToolWait the answer says in words what is missing.
func (t HostTools) windowCall(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	if t.Pages == nil {
		return refuse("this host has no page tools")
	}
	w, got := t.awaitWindowTool(ctx, func(w WindowTool) bool { return w.Name == name })
	switch got {
	case windowToolCut:
		return refuse("the call was cut off")
	case windowToolAbsent:
		return refuse(t.windowMissing(name))
	}
	return t.windowRun(ctx, w, args)
}

const (
	windowToolFound = iota
	windowToolAbsent
	windowToolCut
)

// awaitWindowTool is the first tool of the chat's open windows that is the one asked for, waited for as long as a
// call waits for a window (WindowToolWait): the one wait of a tool called by its own name and of page_call.
func (t HostTools) awaitWindowTool(ctx context.Context, is func(WindowTool) bool) (WindowTool, int) {
	wait := t.Pages.ToolWait
	if wait == 0 {
		wait = WindowToolWait
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	look := time.NewTicker(windowToolPoll)
	defer look.Stop()
	for {
		for _, w := range t.WindowTools() {
			if is(w) {
				return w, windowToolFound
			}
		}
		select {
		case <-ctx.Done():
			return WindowTool{}, windowToolCut
		case <-deadline.C:
			return WindowTool{}, windowToolAbsent
		case <-look.C:
		}
	}
}

func (t HostTools) windowRun(ctx context.Context, w WindowTool, args *jsvalue.Object) *jsvalue.Object {
	a := Installed{ID: w.App, Manifest: builtinOf(w.App)}
	if a.Manifest == nil {
		if t.Live != nil {
			if why := t.Live(w.App); why != "" {
				return refuse(why)
			}
		}
		installed, err := t.Registry.Get(w.App)
		if err != nil || installed.Manifest == nil {
			return refuse(w.App + " is not installed")
		}
		a = installed
	}
	raw := json.RawMessage("{}")
	if args != nil {
		raw = json.RawMessage(jsvalue.Stringify(args, ""))
	}
	return t.pageCallTool(ctx, a, w.PageTool, raw)
}

// Tool is the tool's name in its window.
func (w WindowTool) Tool() string { return w.PageTool.Name }

// windowMissing says why no window offers the tool: the app's window is not open, or it is and has no such tool.
func (t HostTools) windowMissing(name string) string {
	ids := append(slices.Clone(t.enabled()), builtinIDs()...)
	app := ""
	for _, id := range ids {
		if strings.HasPrefix(name, windowToolPrefix(id)) && len(id) > len(app) {
			app = id
		}
	}
	if app == "" {
		return "no open window offers " + name + ": open the app's window with the `open` tool and call the tool again"
	}
	reports := t.windowReports()
	for _, r := range reports {
		if r.App != app {
			continue
		}
		var have []string
		for _, w := range WindowTools(reports) {
			if w.App == app {
				have = append(have, w.Name)
			}
		}
		if len(have) == 0 {
			return "the window of " + app + " is open, but it has registered no tools yet (asked for " + name + ")"
		}
		return "the window of " + app + " is open, but the tool " + strings.TrimPrefix(name, windowToolPrefix(app)) +
			" is not registered; it offers: " + strings.Join(have, ", ")
	}
	return "the window of " + app + " is not open: open it with the `open` tool and call the tool again"
}

func builtinIDs() []string {
	var out []string
	for _, m := range Builtins() {
		out = append(out, m.ID)
	}
	return out
}
