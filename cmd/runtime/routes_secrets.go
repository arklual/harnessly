package main

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
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
//	GET <platform>/secret-codes, POST <platform>/secret-codes/<id>/put|cancel
//	                                     the person's page: a page session (Settings → Safe, the ask card, «Ждут код»)
//	GET /api/cli/secrets, POST /api/cli/secrets/ask|use|file|code|code/wait|code/cancel
//	                                     an agent of this machine: the cli-token (harnessly secret)
//
// A value comes in only by the page's PUT and goes out only by the CLI's use (into a command's environment) and file
// (into a short-lived file). A one-time code comes in by the page's put of its ask and goes out once, to the command
// that asked, by code/wait; it is kept in memory only. No list, no ask and no log line carries one.

const (
	secretBodyMax = 256 << 10
	// secretWaitMax is the longest one ask request waits; the CLI asks again for a longer wait.
	secretWaitMax = 55 * time.Second
)

// safe is the safe of a runtime: where it keeps the values (safe_backend.go), the open asks and the short-lived files.
type safe struct {
	store    *secrets.Safe
	where    safeStore
	asks     *secrets.Asks
	codes    *secrets.Codes
	files    *secrets.Files
	identity func(string) secrets.Use
}

var (
	storesMu sync.Mutex
	stores   = map[string]*secrets.Store{}
)

// storeFor is the process's one store of a data folder's safe: the routes and the apps' launches share its lock, so a
// reveal (which records the use) and a put never write the file over each other.
func storeFor(dataDir, home string) *secrets.Store {
	storesMu.Lock()
	defer storesMu.Unlock()
	if s := stores[dataDir]; s != nil {
		return s
	}
	s := secrets.Open(filepath.Join(dataDir, "secrets"), secrets.DefaultKeyFile(os.Getenv, home))
	stores[dataDir] = s
	return s
}

func newSafe(d deps) *safe {
	dir := filepath.Join(d.DataDir, "secrets")
	store, where := newSafeBackend(os.Getenv, storeFor(d.DataDir, d.Home), dir, d.DataDir)
	if where.Error != "" {
		agent.Log("warn", "safe: the server safe cannot be used: "+where.Error)
	}
	return &safe{
		store: store,
		where: where,
		asks:  secrets.NewAsks(),
		codes: secrets.NewCodes(),
		files: secrets.NewFiles(os.Getenv("RUNTIME_SECRETS_FILES_DIR")),
	}
}

// listAnswer is the list of the page and the CLI; a safe on a server also says where it is.
func (sf *safe) listAnswer(out map[string]any) map[string]any {
	if sf.where.Mode != safeModeLocal {
		out["store"] = sf.where
	}
	return out
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
	case secrets.ReasonKeyLost, secrets.ReasonReadOnly:
		status = http.StatusConflict
	case secrets.ReasonDenied:
		status = http.StatusForbidden
	case secrets.ReasonUnavailable:
		status = http.StatusServiceUnavailable
		agent.Log("warn", "safe: "+err.Error())
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
	Where      string `json:"where,omitempty"`
}

func secretRoutes(d deps, c *core) []gate.Route {
	sf := safeFor(d)
	sf.identity = func(id string) secrets.Use { return secretIdentity(c, id) }
	callers := &callerCache{panes: c.cfg.Tmux, convOf: func(session string) string {
		id, _ := convOf(c, session)
		return id
	}}
	identity := func(r *http.Request, session string) secrets.Use {
		actual, _ := callers.of(r)
		if actual == "" {
			return secrets.Use{}
		}
		// The connection names the chat; a session argument naming another chat is ignored rather than trusted, so
		// cards and usage records never carry a chat the caller is not.
		return secretIdentity(c, actual)
	}
	return sf.routes(d, func(session string) (string, string) { return convOf(c, session) }, identity)
}

func secretIdentity(c *core, id string) secrets.Use {
	v := c.v2.Load()
	if v == nil {
		return secrets.Use{}
	}
	rec, ok := v.rt.Record(id)
	if !ok {
		return secrets.Use{}
	}
	use := secrets.Use{Conv: id, Title: rec.Title}
	if rec.ProjectID != nil {
		use.Project = *rec.ProjectID
	}
	return use
}

func (sf *safe) routes(d deps, conv func(session string) (string, string), resolvers ...func(*http.Request, string) secrets.Use) []gate.Route {
	identity := func(r *http.Request, session string) secrets.Use {
		if len(resolvers) > 0 {
			return resolvers[0](r, session)
		}
		return secrets.Use{}
	}
	useOf := func(r *http.Request, session string) secrets.Use {
		use := identity(r, session)
		if use.Conv == "" {
			use.Conv, use.Title = conv(session)
		}
		return use
	}
	// A code ask belongs to the chat the connection proves, never to a session the caller names: else one chat's agent
	// could put an ask on another chat's panel. Only a rig without a resolver (tests) goes by the session.
	codeUse := func(r *http.Request, session string) secrets.Use {
		if len(resolvers) > 0 {
			return identity(r, session)
		}
		return useOf(r, session)
	}
	base, asksBase, codesBase := platformAPI+"/secrets", platformAPI+"/secret-asks", platformAPI+"/secret-codes"
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
		secretJSON(w, http.StatusOK, sf.listAnswer(map[string]any{"secrets": views, "asks": sf.asks.Pending()}))
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
		case http.MethodPatch:
			var b struct {
				Origins  []string `json:"origins"`
				Projects []string `json:"projects"`
			}
			if !readSecretBody(r, &b) {
				badBody(w)
				return
			}
			v, err := sf.store.Metadata(name, b.Origins, b.Projects)
			if err != nil {
				secretRefusal(w, err)
				return
			}
			secretJSON(w, http.StatusOK, map[string]any{"secret": v})
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
		if action != "decline" && action != "grant" && action != "put" {
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
			return
		}
		if r.Method != http.MethodPost {
			notAllowed(w)
			return
		}
		if action != "decline" {
			a, ok := sf.asks.Get(id)
			if !ok {
				secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonNotFound})
				return
			}
			if sf.identity != nil && a.Conv != "" {
				current := sf.identity(a.Conv)
				if current.Conv == "" || current.Project != a.Project {
					sf.asks.Decline(id)
					secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonNotFound})
					return
				}
			}
			var err error
			if action == "grant" && a.NeedsGrant() {
				if a.Grant {
					err = sf.store.Grant(a.Name, a.Project, a.Origin)
				} else {
					err = sf.store.GrantOrigin(a.Name, a.Project, a.Origin)
				}
			} else if action == "put" && !a.NeedsGrant() {
				var b struct {
					Value string `json:"value"`
				}
				if !readSecretBody(r, &b) {
					badBody(w)
					return
				}
				_, _, err = sf.store.PutAsked(a, b.Value)
			} else {
				badBody(w)
				return
			}
			if err != nil {
				secretRefusal(w, err)
				return
			}
			sf.asks.Ready(id)
			secretJSON(w, http.StatusOK, map[string]any{"ready": id})
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
			if !v.Allowed(identity(r, "").Project) {
				continue
			}
			out = append(out, cliSecret{Name: v.Name, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, LastUsedAt: v.LastUsedAt, Where: v.Where})
		}
		secretJSON(w, http.StatusOK, sf.listAnswer(map[string]any{"secrets": out}))
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
			Origin  string  `json:"origin"`
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
		if b.Origin != "" {
			b.Origin = secrets.Origin(b.Origin)
			if b.Origin == "" {
				secretRefusal(w, &secrets.Refusal{Reason: "bad-origin"})
				return
			}
		}
		use := useOf(r, b.Session)
		id, title := use.Conv, use.Title
		existing, lookupErr := sf.store.Lookup(b.Name, use.Project)
		if !b.Replace && lookupErr == nil && (b.Origin == "" || slices.Contains(existing.Origins, b.Origin)) {
			answer(secrets.AskReady)
			return
		}
		grant := sf.store.Has(b.Name) && lookupErr != nil
		originGrant := !b.Replace && lookupErr == nil && b.Origin != "" && !slices.Contains(existing.Origins, b.Origin)
		if grant && use.Project == "" {
			secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonNotFound})
			return
		}
		a, state, err := sf.asks.OpenScoped(secrets.Ask{Name: b.Name, Why: b.Why, Conv: id, Title: title,
			Replace: b.Replace, Origin: b.Origin, Project: use.Project, Grant: grant, OriginGrant: originGrant})
		if err != nil {
			secretRefusal(w, err)
			return
		}
		if state == secrets.AskWaiting {
			wait := min(time.Duration(b.Wait*float64(time.Second)), secretWaitMax)
			state = sf.asks.Wait(r.Context(), a.ID, max(wait, 0))
		}
		if state == secrets.AskReady {
			current := useOf(r, b.Session)
			view, e := sf.store.Lookup(b.Name, current.Project)
			if e != nil || current.Project != use.Project || b.Origin != "" && !slices.Contains(view.Origins, b.Origin) {
				state = secrets.AskDeclined
			}
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
			if !secrets.ValidRef(n) {
				secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonBadName})
				return
			}
		}
		use := useOf(r, b.Session)
		id := use.Conv
		values, err := sf.store.Reveal(b.Names, use)
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
		if !secrets.ValidRef(b.Name) {
			secretRefusal(w, &secrets.Refusal{Reason: secrets.ReasonBadName})
			return
		}
		use := useOf(r, b.Session)
		id := use.Conv
		values, err := sf.store.Reveal([]string{b.Name}, use)
		if err != nil {
			secretRefusal(w, err)
			return
		}
		ttl := min(max(time.Duration(b.TTL*float64(time.Second)), 0), secrets.MaxFileTTL)
		if ttl == 0 {
			ttl = secrets.DefaultFileTTL
		}
		path, err := sf.files.Write(secrets.FileName(b.Name), values[b.Name], ttl)
		if err != nil {
			secretRefusal(w, err)
			return
		}
		agent.Log("info", "safe: secret file", agent.F{K: "name", V: b.Name}, agent.F{K: "conv", V: id})
		secretJSON(w, http.StatusOK, map[string]any{"path": path, "expiresAt": time.Now().Add(ttl).UnixMilli()})
	}

	codes := func(w http.ResponseWriter, r *http.Request) {
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodGet {
			notAllowed(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"codes": sf.codes.Pending(), "now": time.Now().UnixMilli()})
	}

	// The person's answer to a code ask: put sends the code (never logged), cancel says no.
	codeAnswer := func(w http.ResponseWriter, r *http.Request) {
		rest, _ := under(r, codesBase)
		if !pageOK(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			notAllowed(w)
			return
		}
		id, action, _ := strings.Cut(rest, "/")
		switch action {
		case "put":
			var b struct {
				Value string `json:"value"`
			}
			if !readSecretBody(r, &b) {
				badBody(w)
				return
			}
			found, err := sf.codes.Put(id, b.Value)
			if !found {
				secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
				return
			}
			if err != nil {
				secretRefusal(w, err)
				return
			}
			agent.Log("info", "safe: code given", agent.F{K: "id", V: id})
			secretJSON(w, http.StatusOK, map[string]any{"ready": id})
		case "cancel":
			if !sf.codes.Cancel(id) {
				secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
				return
			}
			agent.Log("info", "safe: code cancelled", agent.F{K: "id", V: id})
			secretJSON(w, http.StatusOK, map[string]any{"cancelled": id})
		default:
			secretJSON(w, http.StatusNotFound, map[string]string{"error": "not-found"})
		}
	}

	cliCode := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string  `json:"session"`
			Why     string  `json:"why"`
			Digits  int     `json:"digits"`
			Wait    float64 `json:"wait"`
		}
		if !readSecretBody(r, &b) || strings.TrimSpace(b.Why) == "" {
			badBody(w)
			return
		}
		use := codeUse(r, b.Session)
		wait := time.Duration(b.Wait * float64(time.Second))
		if wait <= 0 {
			wait = secrets.DefaultCodeWait
		}
		a, err := sf.codes.Open(b.Why, use.Conv, use.Title, b.Digits, wait)
		if err != nil {
			secretRefusal(w, err)
			return
		}
		agent.Log("info", "safe: code asked", agent.F{K: "id", V: a.ID}, agent.F{K: "conv", V: a.Conv})
		secretJSON(w, http.StatusOK, map[string]any{"id": a.ID, "until": a.Until, "pageOpen": sf.asks.PageOpen()})
	}

	// The asking command waits on its ask; the code reaches it once, in this answer only.
	cliCodeWait := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string  `json:"session"`
			ID      string  `json:"id"`
			Wait    float64 `json:"wait"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			badBody(w)
			return
		}
		use := codeUse(r, b.Session)
		wait := max(min(time.Duration(b.Wait*float64(time.Second)), secretWaitMax), 0)
		state, value := sf.codes.Wait(r.Context(), b.ID, use.Conv, wait)
		out := map[string]any{"status": state}
		if state == secrets.CodeReady {
			out["value"] = value
		}
		if state != secrets.CodeWaiting {
			agent.Log("info", "safe: code ask ended", agent.F{K: "id", V: b.ID}, agent.F{K: "state", V: state})
		}
		secretJSON(w, http.StatusOK, out)
	}

	cliCodeCancel := func(w http.ResponseWriter, r *http.Request) {
		if !cliOK(w, r, http.MethodPost) {
			return
		}
		var b struct {
			Session string `json:"session"`
			ID      string `json:"id"`
		}
		if !readSecretBody(r, &b) || b.ID == "" {
			badBody(w)
			return
		}
		secretJSON(w, http.StatusOK, map[string]any{"cancelled": sf.codes.CancelOf(b.ID, codeUse(r, b.Session).Conv)})
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
		{Match: is(codesBase), Handler: http.HandlerFunc(codes)},
		{Match: func(r *http.Request) bool { _, ok := under(r, codesBase); return ok }, Handler: http.HandlerFunc(codeAnswer)},
		{Match: is("/api/cli/secrets/code"), Handler: http.HandlerFunc(cliCode)},
		{Match: is("/api/cli/secrets/code/wait"), Handler: http.HandlerFunc(cliCodeWait)},
		{Match: is("/api/cli/secrets/code/cancel"), Handler: http.HandlerFunc(cliCodeCancel)},
	}
}
