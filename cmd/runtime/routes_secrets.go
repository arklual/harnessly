package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/sameorigin"
	"github.com/ly-solutions/harnessly/internal/secrets"
)

// The safe (internal/secrets): secrets the person puts for the agents, kept encrypted on this machine. Who may call
// what:
//
//	GET <platform>/secrets, PUT|DELETE <platform>/secrets/<name>,
//	GET <platform>/secret-asks, POST <platform>/secret-asks/<id>/decline
//	                                     the person's page: a page session (Settings → Safe, the ask card)
//	GET /api/cli/secrets, POST /api/cli/secrets/ask|use|file
//	                                     an agent of this machine: the cli-token (harnessly secret)
//
// A value comes in only by the page's PUT and goes out only by the CLI's use (into a command's environment) and file
// (into a short-lived file). No list, no ask and no log line carries one.

const (
	secretBodyMax = 256 << 10
	// secretWaitMax is the longest one ask request waits; the CLI asks again for a longer wait.
	secretWaitMax = 55 * time.Second
)

// safe is the safe of a runtime: its store, the open asks and the short-lived files.
type safe struct {
	store *secrets.Store
	asks  *secrets.Asks
	files *secrets.Files
}

func newSafe(d deps) *safe {
	return &safe{
		store: secrets.Open(filepath.Join(d.DataDir, "secrets"), secrets.DefaultKeyFile(os.Getenv, d.Home)),
		asks:  secrets.NewAsks(),
		files: secrets.NewFiles(os.Getenv("RUNTIME_SECRETS_FILES_DIR")),
	}
}

func secretJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func readSecretBody(r *http.Request, out any) bool {
	raw, err := io.ReadAll(io.LimitReader(r.Body, secretBodyMax+1))
	if err != nil || len(raw) > secretBodyMax {
		return false
	}
	return json.Unmarshal(raw, out) == nil
}

// secretRefusal answers an error of the safe as {error: <reason>} with a status that fits it.
func secretRefusal(w http.ResponseWriter, err error) {
	reason := secrets.ReasonOf(err)
	status := http.StatusBadRequest
	switch reason {
	case secrets.ReasonNotFound:
		status = http.StatusNotFound
	case secrets.ReasonKeyLost:
		status = http.StatusConflict
	case secrets.ReasonRateLimited, secrets.ReasonTooMany:
		status = http.StatusTooManyRequests
	case secrets.ReasonFailed:
		status = http.StatusInternalServerError
		// The error names a path or a system call, never a value.
		agent.Log("warn", "safe: "+err.Error())
	}
	secretJSON(w, status, map[string]string{"error": reason})
}

// convOf is the conversation running in a tmux session and its title, "" for none.
func convOf(c *core, session string) (string, string) {
	rt := (peerLocal{c: c}).rt()
	if rt == nil || session == "" {
		return "", ""
	}
	conv := rt.ConvOfSession(session)
	if conv == "" {
		return "", ""
	}
	rec, _ := rt.Record(conv)
	return conv, rec.Title
}

// cliSecret is the CLI's view of a secret: its name and dates, not who used it.
type cliSecret struct {
	Name       string `json:"name"`
	CreatedAt  int64  `json:"createdAt"`
	UpdatedAt  int64  `json:"updatedAt"`
	LastUsedAt int64  `json:"lastUsedAt"`
}

func secretRoutes(d deps, c *core) []gate.Route {
	sf := newSafe(d)
	return sf.routes(d, func(session string) (string, string) { return convOf(c, session) })
}

func (sf *safe) routes(d deps, conv func(session string) (string, string)) []gate.Route {
	base, asksBase := platformAPI+"/secrets", platformAPI+"/secret-asks"
	under := func(r *http.Request, p string) (string, bool) {
		for _, prefix := range []string{p + "/", "/agents" + p + "/"} {
			if rest, ok := strings.CutPrefix(r.URL.Path, prefix); ok && rest != "" {
				return rest, true
			}
		}
		return "", false
	}
	pageOK := func(w http.ResponseWriter, r *http.Request) bool {
		// The session cookie goes with a request of any site: a page of another origin must not put or remove.
		if sameorigin.Refuse(w, r) {
			return false
		}
		if !d.SessionOK(r) {
			secretJSON(w, http.StatusUnauthorized, map[string]string{"error": "Unauthorized"})
			return false
		}
		return true
	}
	cliOK := func(w http.ResponseWriter, r *http.Request, method string) bool {
		if d.CLI == nil || !d.CLI.OK(r) {
			secretJSON(w, http.StatusForbidden, map[string]string{"error": "Forbidden"})
			return false
		}
		if r.Method != method {
			secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
			return false
		}
		return true
	}
	badBody := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusBadRequest, map[string]string{"error": "bad-request"})
	}
	notAllowed := func(w http.ResponseWriter) {
		secretJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
	}

	list := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		views, err := sf.store.List()
		if err != nil {
			secretRefusal(w, err)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"secrets": views, "asks": sf.asks.Pending()})
	}

	one := func(w http.ResponseWriter, r *http.Request) {
		name, _ := under(r, base)
		if !pageOK(w, r) {
			return
		}
		switch r.Method {
		case http.MethodPut:
			var b struct {
				Value string `json:"value"`
			}
			if !readSecretBody(r, &b) {
				badBody(w)
				return
			}
			v, created, err := sf.store.Put(name, b.Value)
			if err != nil {
				secretRefusal(w, err)
				return
			}
			sf.asks.Fulfil(name)
			agent.Log("info", "safe: secret put", agent.F{K: "name", V: name}, agent.F{K: "created", V: created})
			secretJSON(w, http.StatusOK, map[string]any{"secret": v, "created": created})
		case http.MethodDelete:
			if err := sf.store.Remove(name); err != nil {
				secretRefusal(w, err)
				return
			}
			agent.Log("info", "safe: secret removed", agent.F{K: "name", V: name})
			secretJSON(w, http.StatusOK, map[string]any{"removed": name})
		default:
			notAllowed(w)
		}
	}

	asks := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"asks": sf.asks.Pending()})
	}

	decline := func(w http.ResponseWriter, r *http.Request) {
		rest, _ := under(r, asksBase)
		if !pageOK(w, r) {
			return
		}
		id, action, _ := strings.Cut(rest, "/")
		if action != "decline" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		if r.Method != http.MethodPost {
			notAllowed(w)
			return
		}
		if !sf.asks.Decline(id) {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		agent.Log("info", "safe: ask declined", agent.F{K: "id", V: id})
		secretJSON(w, http.StatusOK, map[string]any{"declined": id})
	}

	cliList := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodGet) {
			return
		}
		views, err := sf.store.List()
		if err != nil {
			secretRefusal(w, err)
			return
		}
		out := make([]cliSecret, 0, len(views))
		for _, v := range views {
			out = append(out, cliSecret{Name: v.Name, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, LastUsedAt: v.LastUsedAt})
		}
		secretJSON(w, http.StatusOK, map[string]any{"secrets": out})
	}

	cliAsk := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string  `json:"session"`
			Name    string  `json:"name"`
			Why     string  `json:"why"`
			Replace bool    `json:"replace"`
			Wait    float64 `json:"wait"`
		}
		if !readSecretBody(r, &b) {
			badBody(w)
			return
		}
		if !secrets.ValidName(b.Name) {
			secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonBadName})
			return
		}
		answer := func(status string) {
			secretJSON(w, http.StatusOK, map[string]any{"name": b.Name, "status": status, "pageOpen": sf.asks.PageOpen()})
		}
		if !b.Replace && sf.store.Has(b.Name) {
			answer(secrets.AskReady)
			return
		}
		id, title := conv(b.Session)
		a, state, err := sf.asks.Open(b.Name, b.Why, id, title, b.Replace)
		if err != nil {
			secretRefusal(w, err)
			return
		}
		if state == secrets.AskWaiting {
			wait := min(time.Duration(b.Wait*float64(time.Second)), secretWaitMax)
			state = sf.asks.Wait(r.Context(), a.ID, max(wait, 0))
		}
		agent.Log("info", "safe: ask", agent.F{K: "name", V: b.Name}, agent.F{K: "conv", V: id}, agent.F{K: "state", V: state})
		answer(state)
	}

	cliUse := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string   `json:"session"`
			Names   []string `json:"names"`
		}
		if !readSecretBody(r, &b) || len(b.Names) == 0 || len(b.Names) > 32 {
			badBody(w)
			return
		}
		for _, n := range b.Names {
			if !secrets.ValidName(n) {
				secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonBadName})
				return
			}
		}
		id, title := conv(b.Session)
		values, err := sf.store.Reveal(b.Names, secrets.Use{Conv: id, Title: title})
		if err != nil {
			secretRefusal(w, err)
			return
		}
		agent.Log("info", "safe: secrets used", agent.F{K: "names", V: strings.Join(b.Names, ",")}, agent.F{K: "conv", V: id})
		secretJSON(w, http.StatusOK, map[string]any{"values": values})
	}

	cliFile := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string  `json:"session"`
			Name    string  `json:"name"`
			TTL     float64 `json:"ttl"`
		}
		if !readSecretBody(r, &b) {
			badBody(w)
			return
		}
		if !secrets.ValidName(b.Name) {
			secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonBadName})
			return
		}
		id, title := conv(b.Session)
		values, err := sf.store.Reveal([]string{b.Name}, secrets.Use{Conv: id, Title: title})
		if err != nil {
			secretRefusal(w, err)
			return
		}
		ttl := min(max(time.Duration(b.TTL*float64(time.Second)), 0), secrets.MaxFileTTL)
		if ttl == 0 {
			ttl = secrets.DefaultFileTTL
		}
		path, err := sf.files.Write(b.Name, values[b.Name], ttl)
		if err != nil {
			secretRefusal(w, err)
			return
		}
		agent.Log("info", "safe: secret file", agent.F{K: "name", V: b.Name}, agent.F{K: "conv", V: id})
		secretJSON(w, http.StatusOK, map[string]any{"path": path, "expiresAt": time.Now().Add(ttl).UnixMilli()})
	}

	is := func(p string) func(*http.Request) bool { return func(r *http.Request) bool { return pathIs(r, p) } }
	return []gate.Route{
		{Match: is(base), Handler: http.HandlerFunc(list)},
		{Match: func(r *http.Request) bool { _, ok := under(r, base); return ok }, Handler: http.HandlerFunc(one)},
		{Match: is(asksBase), Handler: http.HandlerFunc(asks)},
		{Match: func(r *http.Request) bool { _, ok := under(r, asksBase); return ok }, Handler: http.HandlerFunc(decline)},
		{Match: is("/api/cli/secrets"), Handler: http.HandlerFunc(cliList)},
		{Match: is("/api/cli/secrets/ask"), Handler: http.HandlerFunc(cliAsk)},
		{Match: is("/api/cli/secrets/use"), Handler: http.HandlerFunc(cliUse)},
		{Match: is("/api/cli/secrets/file"), Handler: http.HandlerFunc(cliFile)},
	}
}
