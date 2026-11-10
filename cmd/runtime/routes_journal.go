package main

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/journal"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/locale"
	"github.com/ly-solutions/harnessly/internal/preview/devtools"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/telemetry"
)

// The machine's journal (HRN-976, internal/journal): every link of the way writes to it, the agent reads it.
//
//	POST <platform>/journal   the page: {entries: [...]} — what the page, its app views and Sinopia saw
//	GET  /api/cli/logs?since=&until=&chat=&trace=&app=&source=&level=&text=&limit=   harnessly logs: errors with their
//	     chains (level error, the default) or lines (level warn or info); &format=json for the raw lines
//
// RUNTIME_JOURNAL=off keeps no journal; RUNTIME_JOURNAL_DAYS (1–7, default 3) is how long it is kept;
// RUNTIME_JOURNAL_EXPORT=telemetry sends its warnings and errors to the runtime's telemetry endpoint too.

// journalPageMax is how many lines one report of the page carries; journalPageRate how many the page may send a minute.
const (
	journalPageMax  = 100
	journalPageRate = 1200
)

// openJournal starts the machine's journal and makes it the default, nil when switched off.
func openJournal(dataDir string) *journal.Journal {
	if strings.EqualFold(env("RUNTIME_JOURNAL", "on"), "off") {
		return nil
	}
	days, _ := strconv.Atoi(env("RUNTIME_JOURNAL_DAYS", "3"))
	if days < 1 || days > 7 {
		days = 3
	}
	j := journal.Open(filepath.Join(dataDir, "journal"), journal.Options{Keep: time.Duration(days) * 24 * time.Hour})
	journal.SetDefault(j)
	return j
}

// exportJournal sends the journal's warnings and errors as telemetry events, when the deployment asks for it.
func exportJournal(j *journal.Journal, q *telemetry.Queue) {
	if j == nil || q == nil || !q.Enabled() || env("RUNTIME_JOURNAL_EXPORT", "") != "telemetry" {
		return
	}
	j.OnEntry = func(e journal.Entry) {
		q.Push(journal.TelemetryEvent(e))
	}
}

// journalQuiet is a request whose success the journal does not write: its own routes, the agents' MCP calls (written
// as tool calls), the page's reports and the routes a page polls.
func journalQuiet(r *http.Request) bool {
	p := strings.TrimPrefix(r.URL.Path, "/agents")
	switch {
	case strings.HasSuffix(p, "/journal"), p == "/api/cli/logs",
		strings.HasSuffix(p, "/apps/view-log"), strings.HasSuffix(p, "/apps/session/renew"),
		strings.Contains(p, "/mcp"), strings.HasPrefix(p, "/api/cli/preview/report"),
		strings.Contains(p, "/statusline"), strings.Contains(p, "/hooks/"), strings.Contains(p, "/drafts"),
		strings.Contains(p, "/presence"), strings.Contains(p, "/heartbeat"), strings.Contains(p, "/devtools"):
		return true
	}
	return false
}

func journalRoutes(d deps) []gate.Route {
	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	var mu sync.Mutex
	window, count := time.Time{}, 0
	take := func(n int, now time.Time) int {
		mu.Lock()
		defer mu.Unlock()
		if now.Sub(window) >= time.Minute {
			window, count = now, 0
		}
		if left := journalPageRate - count; n > left {
			n = left
		}
		count += n
		return n
	}

	report := func(w http.ResponseWriter, r *http.Request) {
		if sameorigin.Refuse(w, r) {
			return
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return
		}
		var b struct {
			Entries []journal.Entry `json:"entries"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		j := journal.Default()
		if j == nil {
			secretJSON(w, http.StatusOK, map[string]any{"kept": 0})
			return
		}
		lines := b.Entries
		if len(lines) > journalPageMax {
			lines = lines[:journalPageMax]
		}
		n := take(len(lines), time.Now())
		for _, e := range lines[:n] {
			// The page speaks for itself, its views and Sinopia, never for the runtime.
			switch e.Source {
			case journal.SourceApp, journal.SourceSinopia:
			default:
				e.Source = journal.SourceWeb
			}
			j.Add(e)
		}
		secretJSON(w, http.StatusOK, map[string]any{"kept": n})
	}

	logs := func(w http.ResponseWriter, r *http.Request) {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return
		}
		j := journal.Default()
		if j == nil {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "This machine keeps no journal (RUNTIME_JOURNAL=off)."})
			return
		}
		q := r.URL.Query()
		now := time.Now()
		since, ok := journal.ParseSince(q.Get("since"), now, 30*time.Minute)
		if !ok {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "since: 10m, 2h, 1d, 15:04 or an RFC 3339 time"})
			return
		}
		f := journal.Filter{Since: since, Chat: q.Get("chat"), Trace: q.Get("trace"), App: q.Get("app"),
			Source: q.Get("source"), Level: q.Get("level"), Text: q.Get("text")}
		if u := q.Get("until"); u != "" {
			if t, ok := journal.ParseSince(u, now, 0); ok {
				f.Until = t
			}
		}
		f.Limit, _ = strconv.Atoi(q.Get("limit"))
		answer := map[string]any{"dropped": j.Dropped()}
		if f.Level == "" || f.Level == journal.Error {
			chains := j.Chains(f)
			answer["chains"] = chains
			answer["text"] = journal.FormatChains(chains)
		} else {
			lines := j.Query(f)
			answer["lines"] = lines
			answer["text"] = journal.FormatLines(lines)
		}
		secretJSON(w, http.StatusOK, answer)
	}

	// reason: the page's question when a view did not connect, «what does the machine know of why?» — the last error
	// of the app (and chat) since the view was mounted, in a few words, "" when nothing is known.
	reason := func(w http.ResponseWriter, r *http.Request) {
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return
		}
		q := r.URL.Query()
		since, _ := strconv.ParseInt(q.Get("since"), 10, 64)
		if since <= 0 || time.Since(time.UnixMilli(since)) > time.Hour {
			since = time.Now().Add(-time.Minute).UnixMilli()
		}
		secretJSON(w, http.StatusOK, map[string]string{"reason": journal.Default().Reason(q.Get("app"), q.Get("chat"), q.Get("trace"), time.UnixMilli(since))})
	}

	return []gate.Route{
		{Match: is(platformAPI + "/journal/reason"), Handler: http.HandlerFunc(reason)},
		{Match: is("/agents" + platformAPI + "/journal/reason"), Handler: http.HandlerFunc(reason)},
		{Match: is(platformAPI + "/journal"), Handler: http.HandlerFunc(report)},
		{Match: is("/agents" + platformAPI + "/journal"), Handler: http.HandlerFunc(report)},
		{Match: is("/api/cli/logs"), Handler: http.HandlerFunc(logs)},
	}
}

// journalTools is the journal's tool of the agents' MCP server, told the calling chat when the connection named it.
type journalTools struct {
	// own: the caller is an agent inside the wall, kept to its own chat's lines.
	own bool
}

func (journalTools) Instructions() string { return journal.Tools{}.Instructions() }
func (journalTools) Tools() []any         { return journal.Tools{}.Tools() }
func (journalTools) Has(name string) bool { return name == journal.ToolName }
func (t journalTools) Call(ctx context.Context, name string, args *jsvalue.Object) *jsvalue.Object {
	_, chat, _ := journal.ScopeOf(ctx).Of()
	return journal.Tools{J: journal.Default(), Chat: chat, Own: t.own}.Call(ctx, name, args)
}

// journalToolCall writes down an agent's tool call: the tool, the chat, how long it took and, when it failed, the
// first words of the failure.
func journalToolCall(server string) func(ctx context.Context, tool string, took time.Duration, result *jsvalue.Object) {
	return func(ctx context.Context, tool string, took time.Duration, result *jsvalue.Object) {
		e := journal.Fill(ctx, journal.Entry{Level: journal.Info, Source: journal.SourceRuntime, Event: "mcp.call",
			Tool: server + "/" + tool, Dur: took.Milliseconds()})
		if failed, _ := result.Get("isError"); failed == true {
			e.Level = journal.Error
			if content, ok := result.Get("content"); ok {
				if list, ok := content.([]any); ok && len(list) > 0 {
					if first, ok := list[0].(*jsvalue.Object); ok {
						if text, ok := first.Get("text"); ok {
							if s, ok := text.(string); ok {
								e.Text = clip(s, 200)
							}
						}
					}
				}
			}
		}
		journal.Add(e)
	}
}

// journalBridgeCall tells the journal's line of a call of an app's view (session, storage, tool, files, …) the app, the
// view, the chat and the method, and for a refusal its code and words, from the view's session (HRN-976).
func journalBridgeCall(ctx context.Context, views *apps.ViewServer, b bridgeBody, status int, out any) {
	if journal.ScopeOf(ctx) == nil {
		return
	}
	app, view, chat := b.App, b.View, b.Conversation
	if b.Session != "" && views != nil {
		if sess, ok := views.Sessions.Get(b.Session); ok {
			app, view, chat = sess.App, sess.View, sess.Conversation
		}
	}
	journal.SetApp(ctx, app)
	journal.SetChat(ctx, chat)
	// A session just issued keeps the page's trace for the view server's lines.
	if trace, _, _ := journal.ScopeOf(ctx).Of(); trace != "" && status < 400 && views != nil {
		if m, ok := out.(map[string]any); ok {
			if token, ok := m["session"].(string); ok && token != "" {
				views.Sessions.SetTrace(token, trace)
			}
		}
	}
	method := b.Op
	if b.Name != "" {
		method = b.Name
	}
	if b.Op != "" && b.Scope != "" {
		method = b.Scope + "." + b.Op
	}
	note := ""
	if status >= 400 {
		note = bridgeRefusalWords(out)
	}
	journal.SetCall(ctx, view, method, note)
}

// bridgeRefusalWords is the code and the message of a refusal the bridge answered, nothing else of it.
func bridgeRefusalWords(out any) string {
	var code, msg string
	switch v := out.(type) {
	case map[string]string:
		code, msg = v["error"], v["message"]
	case map[string]any:
		code, _ = v["error"].(string)
		msg, _ = v["message"].(string)
	}
	return strings.TrimSpace(strings.Trim(code+": "+clip(msg, 200), ": "))
}

// journalPreviewRequest writes down a request of a preview slot that failed: blocked, refused or with no answer. A
// file of an app's view names its app, chat and trace (HRN-976).
func journalPreviewRequest(slot string, e devtools.NetworkEntry) {
	je := journal.Entry{At: e.At, Level: journal.Warn, Source: journal.SourcePreview, Event: "preview.request",
		Method: e.Method, URL: e.URL, View: "slot " + slot}
	if e.Status != nil {
		je.Status = int(*e.Status)
		if je.Status >= 500 {
			je.Level = journal.Error
		}
	}
	if e.DurationMs != nil {
		je.Dur = *e.DurationMs
	}
	switch {
	case e.Blocked:
		je.Level, je.Text = journal.Error, locale.Pick("blocked in the preview's DevTools", "заблокирован в DevTools превью")
	case e.Error != nil:
		je.Level, je.Text = journal.Error, *e.Error
	}
	if u, err := url.Parse(e.URL); err == nil {
		if sess, ok := appViewSessionOf(u.Path); ok {
			je.Level, je.Source = journal.Error, apps.JournalSource(sess.App)
			je.App, je.View, je.Chat, je.Trace = sess.App, sess.View, sess.Conversation, sess.Trace
		} else if isSDKPath(u.Path) {
			je.Level = journal.Error
		}
	}
	journal.Add(je)
}

// journalPreviewConsole writes down a console error of a preview slot's page.
func journalPreviewConsole(slot string, e devtools.ConsoleEntry) {
	je := journal.Entry{At: e.At, Level: journal.Error, Source: journal.SourcePreview, Event: "preview.console",
		Text: "slot " + slot + ": " + clip(e.Text, 400)}
	if e.URL != nil {
		je.URL = *e.URL
	}
	journal.Add(je)
}
