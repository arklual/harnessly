package main

import (
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// What an app spends of the person's model (HRN-853), each within its budget of the day (internal/apps spend.go):
//
//	POST <platform>/apps/ai          ai: one answer of a model, no tools → {text, usd, spentToday, budget}
//	POST <platform>/apps/agents/run  agents.run: a task in a new chat under the app's chat → {id}

// aiPerApp is how many model calls of one app run at once.
const aiPerApp = 2

// runPromptMax bounds the task an app gives a new chat.
const runPromptMax = 20000

// appAIRun is the model's program; tests put their own.
var appAIRun = apps.RunClaude(claudeBin())

func claudeBin() string {
	if bin := os.Getenv("RUNTIME_APPS_CLAUDE_BIN"); bin != "" {
		return bin
	}
	return "claude"
}

func appAIRoutes(d deps, reg *apps.Registry, chats appChats, live appLive) []gate.Route {
	var mu sync.Mutex
	running := map[string]int{}
	take := func(id string) bool {
		mu.Lock()
		defer mu.Unlock()
		if running[id] >= aiPerApp {
			return false
		}
		running[id]++
		return true
	}
	done := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		running[id]--
	}
	// open is what both have in common: the page's session, the app live in it, the right, and the person's yes while
	// the agent uses the view.
	open := func(w http.ResponseWriter, r *http.Request, b bridgeBody, method string) (apps.Session, apps.Installed, bool) {
		refuse := func(err error) {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
		}
		sess, app, err := live(b)
		if err != nil {
			refuse(err)
			return sess, app, false
		}
		if err := app.Manifest.Authorize(method, sess.Target, nil); err != nil {
			refuse(err)
			return sess, app, false
		}
		if !b.Confirmed && appPages.Driving(sess.Conversation, sess.App) {
			refuse(&apps.BridgeError{Code: apps.CodePermissionDenied, Message: "the agent is using the app: this needs the person's yes"})
			return sess, app, false
		}
		return sess, app, true
	}
	head := func(w http.ResponseWriter, r *http.Request) bool {
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		if r.Method != http.MethodPost {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}

	complete := func(w http.ResponseWriter, r *http.Request) {
		if !head(w, r) {
			return
		}
		var b struct {
			bridgeBody
			apps.AIRequest
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		sess, app, ok := open(w, r, b.bridgeBody, "harnessly/ai.complete")
		if !ok {
			return
		}
		q := b.AIRequest
		if err := q.Check(); err != nil {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
			return
		}
		if !take(sess.App) {
			secretJSON(w, http.StatusTooManyRequests, map[string]string{"error": apps.CodeRateLimited, "message": "two model calls of the app run already"})
			return
		}
		defer done(sess.App)
		budget := apps.BudgetOf(app.Manifest)
		ans, spent, err := reg.ChargeAI(r.Context(), sess.App, budget, appAIRun, q, time.Now)
		_ = reg.Journal(sess.App, apps.JournalEntry{What: "ai", Class: apps.ClassHigh, Chat: sess.Conversation, OK: err == nil,
			Detail: q.Model + " " + apps.Money(ans.USD) + ", " + apps.Money(spent.USD) + " of " + apps.Money(budget.USDPerDay) + " today"})
		if err != nil {
			st, out := bridgeRefused(err)
			secretJSON(w, st, out)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"text": ans.Text, "usd": ans.USD, "spentToday": spent.USD, "budget": budget.USDPerDay})
	}

	run := func(w http.ResponseWriter, r *http.Request) {
		if !head(w, r) {
			return
		}
		var b struct {
			bridgeBody
			Prompt string `json:"prompt"`
			Title  string `json:"title"`
		}
		if !readSecretBody(r, &b) {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
			return
		}
		sess, app, ok := open(w, r, b.bridgeBody, "harnessly/agents.run")
		if !ok {
			return
		}
		prompt := strings.TrimSpace(b.Prompt)
		if prompt == "" || utf8.RuneCountInString(prompt) > runPromptMax || utf8.RuneCountInString(b.Title) > 120 {
			secretJSON(w, http.StatusBadRequest, map[string]string{"error": apps.CodeInvalidParams, "message": "prompt is 1 to 20000 characters, title up to 120"})
			return
		}
		rec, ok := chats.record(sess.Conversation)
		if !ok {
			st, out := bridgeRefused(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "the chat is not on this machine"})
			secretJSON(w, st, out)
			return
		}
		// Only an app on in the chat starts work in its folder.
		if !slices.Contains(rec.Apps, sess.App) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "not-on"})
			return
		}
		budget := apps.BudgetOf(app.Manifest)
		journal := func(ok bool, detail string) {
			_ = reg.Journal(sess.App, apps.JournalEntry{What: "agents.run", Class: apps.ClassHigh, Chat: sess.Conversation, OK: ok, Detail: detail})
		}
		if err := reg.TakeRun(sess.App, budget, time.Now()); err != nil {
			journal(false, "over budget")
			secretJSON(w, http.StatusForbidden, map[string]string{"error": apps.CodeQuotaExceeded,
				"message": "the app's chats of the day are spent: " + strconv.Itoa(budget.RunsPerDay) + "; they renew at 00:00 UTC"})
			return
		}
		title := strings.TrimSpace(b.Title)
		if title == "" {
			title = firstLine(prompt, 60)
		}
		cmd := wire.ConversationCreateCmd{OpID: "app-run-" + randomToken(), Cwd: rec.Cwd, Provider: rec.Provider, ParentID: sess.Conversation,
			Title: app.Manifest.Name + ": " + title,
			First: &wire.FirstMessage{ClientID: randomToken(), Text: prompt, AttachmentIDs: []string{}}}
		if rec.ProjectID != nil {
			cmd.ProjectID = *rec.ProjectID
		}
		res := chats.do(r.Context(), cmd)
		if res.R != wire.ResultAccepted || res.ID == nil {
			_ = reg.ReturnRun(sess.App, time.Now())
			journal(false, "not started: "+string(res.Code))
			st, out := bridgeRefused(&apps.BridgeError{Code: apps.CodeHostUnavailable, Message: "the chat did not start: " + string(res.Code)})
			secretJSON(w, st, out)
			return
		}
		journal(true, *res.ID+" "+clipDetail(title))
		secretJSON(w, http.StatusOK, map[string]string{"id": *res.ID})
	}

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(platformAPI + "/apps/ai"), Handler: http.HandlerFunc(complete)},
		{Match: is("/agents" + platformAPI + "/apps/ai"), Handler: http.HandlerFunc(complete)},
		{Match: is(platformAPI + "/apps/agents/run"), Handler: http.HandlerFunc(run)},
		{Match: is("/agents" + platformAPI + "/apps/agents/run"), Handler: http.HandlerFunc(run)},
	}
}

// firstLine is the start of a text's first line, up to n characters.
func firstLine(s string, n int) string {
	line, _, _ := strings.Cut(s, "\n")
	line = strings.TrimSpace(line)
	if utf8.RuneCountInString(line) <= n {
		return line
	}
	return string([]rune(line)[:n]) + "…"
}
