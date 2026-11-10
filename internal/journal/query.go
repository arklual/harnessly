package journal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/locale"
)

// Filter picks lines.
type Filter struct {
	Since, Until time.Time
	Chat, Trace  string
	App          string
	// Source is one source or several separated by commas.
	Source string
	// Level is the least severe level taken: error takes errors, warn warnings and errors, info (or "") all.
	Level string
	// Text is a substring of the line's event, text, address or tool.
	Text  string
	Limit int
}

func rank(level string) int {
	switch level {
	case Error:
		return 2
	case Warn:
		return 1
	}
	return 0
}

func (f Filter) sourceOK(src string) bool {
	if f.Source == "" {
		return true
	}
	for _, s := range strings.Split(f.Source, ",") {
		if strings.TrimSpace(s) == src {
			return true
		}
	}
	return false
}

func (f Filter) match(e Entry) bool {
	if !f.Since.IsZero() && e.At < f.Since.UnixMilli() {
		return false
	}
	if !f.Until.IsZero() && e.At > f.Until.UnixMilli() {
		return false
	}
	if rank(e.Level) < rank(f.Level) || !f.sourceOK(e.Source) {
		return false
	}
	if f.Chat != "" && e.Chat != f.Chat && !strings.HasPrefix(e.Chat, f.Chat) {
		return false
	}
	if f.Trace != "" && e.Trace != f.Trace {
		return false
	}
	if f.App != "" && e.App != f.App {
		return false
	}
	if f.Text != "" {
		q := strings.ToLower(f.Text)
		if !strings.Contains(strings.ToLower(e.Event+" "+e.Text+" "+e.URL+" "+e.Tool+" "+e.Method), q) {
			return false
		}
	}
	return true
}

// read is every line written between since and until, oldest first.
func (j *Journal) read(since, until time.Time) []Entry {
	if j == nil {
		return nil
	}
	j.Flush()
	var out []Entry
	for _, p := range j.files(since, until) {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) != nil {
				continue
			}
			if (!since.IsZero() && e.At < since.UnixMilli()) || (!until.IsZero() && e.At > until.UnixMilli()) {
				continue
			}
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].At < out[b].At })
	return out
}

// Query is the lines the filter takes, oldest first, the last Limit of them (0: 500).
func (j *Journal) Query(f Filter) []Entry {
	all := j.read(f.Since, f.Until)
	out := []Entry{}
	for _, e := range all {
		if f.match(e) {
			out = append(out, e)
		}
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Chain is an error with the lines around it that tell how it came about: the same trace, else the same chat and app
// a little before it.
type Chain struct {
	Errors []Entry `json:"errors"`
	Lines  []Entry `json:"lines"`
	// Hints are conclusions drawn from the lines (a file the page could not load that never reached the machine).
	Hints []string `json:"hints,omitempty"`
}

// chainBefore and chainAfter are how far around an error lines with no trace in common are taken by chat and app.
const (
	chainBefore = 60 * time.Second
	chainAfter  = 15 * time.Second
	chainMax    = 40
)

// Chains are the errors the filter takes (whatever its level says, errors only), each with its chain; errors of one
// trace make one chain. Newest last; at most Limit chains (0: 20).
func (j *Journal) Chains(f Filter) []Chain {
	lf := f
	lf.Level = Error
	all := j.read(f.Since.Add(-chainBefore), f.Until)
	var chains []Chain
	byTrace := map[string]int{}
	for _, e := range all {
		if !lf.match(e) {
			continue
		}
		if e.Trace != "" {
			if at, ok := byTrace[e.Trace]; ok {
				chains[at].Errors = append(chains[at].Errors, e)
				continue
			}
			byTrace[e.Trace] = len(chains)
		}
		chains = append(chains, Chain{Errors: []Entry{e}})
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 20
	}
	if len(chains) > limit {
		chains = chains[len(chains)-limit:]
	}
	for i := range chains {
		chains[i].Lines = related(all, chains[i].Errors)
		chains[i].Hints = hints(chains[i].Lines)
	}
	return chains
}

// related is the lines of a chain, oldest first: those of the errors' trace, and those of the same chat or app (no
// other chat or trace named) in the window around each error.
func related(all []Entry, errs []Entry) []Entry {
	first, last := errs[0], errs[len(errs)-1]
	from, to := first.At-chainBefore.Milliseconds(), last.At+chainAfter.Milliseconds()
	traces, chats, apps := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, e := range errs {
		if e.Trace != "" {
			traces[e.Trace] = true
		}
		if e.Chat != "" {
			chats[e.Chat] = true
		}
		if e.App != "" {
			apps[e.App] = true
		}
	}
	var out []Entry
	for _, e := range all {
		if e.At < from || e.At > to {
			continue
		}
		// Another trace is another action: a view opened before or after this one is not its story.
		if len(traces) > 0 && e.Trace != "" && !traces[e.Trace] {
			continue
		}
		switch {
		case e.Trace != "" && traces[e.Trace]:
		case e.Chat != "" && chats[e.Chat] && (e.App == "" || apps[e.App] || len(apps) == 0):
		case e.App != "" && apps[e.App] && (e.Chat == "" || len(chats) == 0):
		default:
			continue
		}
		out = append(out, e)
	}
	if len(out) > chainMax {
		// The errors stay; of the rest the nearest to them.
		keep := out[:0:0]
		for _, e := range out {
			if e.Level == Error {
				keep = append(keep, e)
			}
		}
		rest := chainMax - len(keep)
		for i := len(out) - 1; i >= 0 && rest > 0; i-- {
			if out[i].Level != Error {
				keep = append(keep, out[i])
				rest--
			}
		}
		sort.SliceStable(keep, func(a, b int) bool { return keep[a].At < keep[b].At })
		out = keep
	}
	return out
}

// hints draws conclusions a reader would: a file a view or the page could not load, and whether the machine saw it.
func hints(lines []Entry) []string {
	var out []string
	seen := map[string]bool{}
	for _, e := range lines {
		if e.Level != Error || (e.Event != "resource" && e.Event != "view.file") || e.URL == "" {
			continue
		}
		file := tail(e.URL)
		if seen[file] {
			continue
		}
		seen[file] = true
		reached := false
		for _, o := range lines {
			if o.Source == SourceRuntime && strings.HasSuffix(o.URL, file) {
				reached = true
				if o.Status != 0 {
					out = append(out, locale.Pick("file "+file+": the machine answered ", "файл "+file+": машина ответила ")+itoa(o.Status))
				}
				break
			}
		}
		if !reached && e.Status == 0 {
			out = append(out, locale.Pick("file "+file+" did not load in the browser, and the machine never saw the request: it was stopped on the way (a block in the preview, a gateway, a proxy, a browser extension)",
				"файл "+file+" не загрузился в браузере, а машина такого запроса не видела: его остановили по дороге (блокировка в превью, шлюз, прокси, расширение браузера)"))
		}
	}
	return out
}

// tail is the last two parts of a path.
func tail(p string) string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [12]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Reason is the most telling known cause of a view's failure: the last error line of the app (and chat, when one is
// named) since the moment given, as one short phrase, "" when none is known. A visible error carries it.
func (j *Journal) Reason(app, chat, trace string, since time.Time) string {
	if j == nil || app == "" {
		return ""
	}
	lines := j.Query(Filter{Since: since, App: app, Chat: chat, Limit: 200})
	// One mount's lines when the page names it, and those of no trace (a request the runtime could not tie to a mount):
	// an earlier view of the same app is another story.
	if trace != "" {
		own := lines[:0:0]
		for _, e := range lines {
			if e.Trace == trace || e.Trace == "" {
				own = append(own, e)
			}
		}
		lines = own
	}
	// The latest failed attempt, told by its first error: the file that failed first is the cause, the rest follow it.
	cause := func(e Entry) bool { return e.Level == Error && e.Event != "ui.initialize" && Describe(e) != "" }
	for i := len(lines) - 1; i >= 0; i-- {
		if !cause(lines[i]) {
			continue
		}
		if t := lines[i].Trace; t != "" {
			for _, e := range lines[:i] {
				if e.Trace == t && cause(e) {
					return Describe(e)
				}
			}
		}
		return Describe(lines[i])
	}
	// No error, but the view server's own lines: the page came and the SDK did not, or the SDK came and never connected.
	page, shim := false, false
	for _, e := range lines {
		switch e.Event {
		case "view.page":
			page = true
		case "view.sdk":
			shim = shim || strings.HasSuffix(e.URL, "bridge-shim.js")
		}
	}
	switch {
	case page && !shim:
		return locale.Pick("the view's page came from the machine, but its script __hl/bridge-shim.js never reached it: stopped on the way (a gateway, a proxy, a browser extension) or blocked by the browser",
			"страница окна пришла с машины, а скрипт __hl/bridge-shim.js до машины не дошёл: его остановили по дороге (шлюз, прокси, расширение браузера) или заблокировал браузер")
	case page && shim:
		return locale.Pick("the page and the SDK's script came from the machine, but the view did not start its SDK (harnessly.connect)",
			"страница и скрипт SDK пришли с машины, но окно не запустило SDK (harnessly.connect)")
	}
	return ""
}

// Describe is a line in a few words: "скрипт __hl/bridge-shim.js: 401", "tools.call x — timeout".
func Describe(e Entry) string {
	switch {
	case e.URL != "" && (e.Event == "resource" || e.Event == "view.file" || e.Event == "fetch" || e.Event == "http" ||
		e.Event == "preview.request" || e.Event == "preview.slot"):
		what := locale.Pick("file", "файл")
		if strings.HasSuffix(e.URL, ".js") || strings.HasSuffix(e.URL, ".mjs") {
			what = locale.Pick("script", "скрипт")
		}
		s := what + " " + tail(e.URL)
		switch {
		case e.Status != 0 && e.Text != "":
			s += ": " + itoa(e.Status) + " (" + e.Text + ")"
		case e.Status != 0:
			s += ": " + itoa(e.Status)
		case e.Text != "":
			s += ": " + e.Text
		default:
			s += ": " + locale.Pick("did not load", "не загрузился")
		}
		return s
	case e.Text != "":
		return e.Text
	}
	return e.Event
}
