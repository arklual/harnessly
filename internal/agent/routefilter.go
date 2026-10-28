package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/nodejson"
)

// What the hub may reach of the runtime goes through this filter. A tunnel to the runtime ends here, not at the
// runtime: an HTTP server on loopback that reads each request, asks the local policy, and passes what is allowed to the
// runtime as it came (headers, host and WebSocket upgrades untouched). The policy is read for every request, so a change
// of the file applies to the next one.

const maxFilterBody = 1 << 20

// terminalRoutes are the prefixes of what is a shell on the machine: the page's terminal socket (the older one and the
// chat page's), and the console a hub-carried runtime opens for an agent's login, its page and its API. Denying a prefix
// covers its sub-routes too.
var terminalRoutes = []string{"/api/terminal", "/api/v2/terminal", "/console/", "/api/console/"}

// isOwnTerminals: the chat page's own shells of a conversation, `/api/v2/conversations/<id>/terminals…`.
func isOwnTerminals(p string) bool {
	rest, ok := strings.CutPrefix(p, "/api/v2/conversations/")
	if !ok {
		return false
	}
	_, tail, ok := strings.Cut(rest, "/")
	return ok && (tail == "terminals" || strings.HasPrefix(tail, "terminals/"))
}

func isTerminalRoute(p string) bool {
	if isOwnTerminals(p) {
		return true
	}
	for _, prefix := range terminalRoutes {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// routeFilter is the loopback server in front of the runtime.
type routeFilter struct {
	policy func() hubproto.Policy
	base   string
	audit  *Audit
	// protected are paths whose writing is refused on top of the built-in names: the policy file and the kill switch
	// in use (nil: the built-in ones only).
	protected func() []string
	// protectedTrees are the files and folders whose real place (after links) no write may equal or lie under: the
	// policy, the kill switch, the agent's state files and its env folder. roots are the folders a relative path of the
	// editor may be taken from (the runtime's project is one of them or a folder in them).
	protectedTrees func() []string
	roots          []string
	proxy          *httputil.ReverseProxy
	srv            *http.Server
	port           atomic.Int64
	denials        atomic.Int64
}

// newRouteFilter filters requests for the runtime at 127.0.0.1:runtimePort, whose routes live under base.
func newRouteFilter(runtimePort int, base string, policy func() hubproto.Policy, audit *Audit) *routeFilter {
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(runtimePort))}
	f := &routeFilter{policy: policy, base: strings.TrimRight(base, "/"), audit: audit}
	f.proxy = &httputil.ReverseProxy{
		// Rewrite, not Director: it adds no forwarding headers of its own, so what the hub sent is what the runtime reads.
		Rewrite: func(pr *httputil.ProxyRequest) {
			host := pr.In.Host
			pr.SetURL(target)
			pr.Out.Host = host
			// ReverseProxy drops the forwarding headers of the incoming request; the runtime is to see the hub's.
			for k, v := range pr.In.Header {
				if k == "Forwarded" || strings.HasPrefix(k, "X-Forwarded-") {
					pr.Out.Header[k] = v
				}
			}
		},
		// Server-sent events and the like must not wait for a buffer.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeFilterError(w, http.StatusBadGateway, "the runtime did not answer", "")
		},
	}
	f.srv = &http.Server{Handler: f, ReadHeaderTimeout: 10 * time.Second}
	return f
}

// Start listens on a free loopback port and serves; the port is where tunnels to the runtime go.
func (f *routeFilter) Start() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	f.port.Store(int64(port))
	go func() { _ = f.srv.Serve(ln) }()
	return port, nil
}

func (f *routeFilter) Close() { _ = f.srv.Close() }

func writeFilterError(w http.ResponseWriter, status int, msg, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]string{"error": msg}
	if code != "" {
		body["code"] = code
	}
	_ = json.NewEncoder(w).Encode(body)
}

func (f *routeFilter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	reason, err := f.decide(f.policy(), r)
	if err != nil {
		writeFilterError(w, http.StatusBadRequest, "the request body could not be read", "")
		return
	}
	if reason != "" {
		f.denials.Add(1)
		f.audit.Record("route.denied", false, map[string]any{"method": r.Method, "path": cleanForAudit(r.URL.Path), "reason": reason})
		writeFilterError(w, http.StatusForbidden, "This machine's local policy does not allow this: "+reason, "machine_policy")
		return
	}
	f.proxy.ServeHTTP(w, r)
}

func cleanForAudit(p string) string {
	if len(p) > 200 {
		p = p[:200]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, p)
}

// pathForms are the paths a request can mean to the runtime. It answers its routes at /api/…, at /agents/api/… and
// under its base path, and a path that is not clean may be cleaned by something in between, so the request is checked
// as it came, cleaned, and with each mount point taken off.
func (f *routeFilter) pathForms(p string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for _, v := range []string{p, path.Clean("/" + p)} {
		add(v)
		for _, mount := range []string{f.base, "/agents"} {
			if mount == "" {
				continue
			}
			if rest, ok := cutMount(v, mount); ok {
				add(rest)
				if rest2, ok := cutMount(rest, "/agents"); ok {
					add(rest2)
				}
			}
		}
	}
	return out
}

func cutMount(p, mount string) (string, bool) {
	rest, ok := strings.CutPrefix(p, mount)
	if !ok || (rest != "" && !strings.HasPrefix(rest, "/")) {
		return "", false
	}
	if rest == "" {
		rest = "/"
	}
	return rest, true
}

// decide is the policy's verdict on one request: "" lets it through, anything else is the reason it is refused. The
// error is for a body that could not be read when it had to be.
func (f *routeFilter) decide(p hubproto.Policy, r *http.Request) (string, error) {
	forms := f.pathForms(r.URL.Path)
	for _, form := range forms {
		for _, deny := range p.DenyRoutes {
			if strings.HasPrefix(form, deny) {
				return "the route " + deny + " is switched off here", nil
			}
		}
	}
	if !isRead(r.Method) {
		if reason := f.protectedWrite(r, forms); reason != "" {
			return reason, nil
		}
	}
	if p.Terminal {
		return "", nil
	}
	for _, form := range forms {
		if isTerminalRoute(form) {
			return "the terminal is switched off here", nil
		}
	}
	if isRead(r.Method) {
		return "", nil
	}
	return f.writeAllowed(r, forms)
}

func isRead(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

// --- what may be written while the terminal is off ---------------------------------------------------------------------

// With the terminal off a request that changes anything is refused unless it is on this list: the routes of the chat
// page, of the layout of agents' tabs and of the page's own files. A route that is not here (the runtime has tens: a
// tmux session's raw keys, the CLI's, the agents' settings with their MCP servers and rules, the page's config with
// its permission switch, tailscale…) is not made safe by a guess about what it does. The routes that make tabs look into
// their bodies, read the way the runtime reads them.

// writeRoute is one entry of the list: the methods, the path (a segment `*` is any one segment) and, for the routes that
// can make a shell, a look at the body.
type writeRoute struct {
	methods string
	pattern string
	check   func(c *bodyCtx) string
}

// refusalTerminal is what a refused request that would have made a terminal says.
const refusalTerminal = "the terminal is switched off here"

// refusalApproval is what a refused approval of a tool call says.
const refusalApproval = "the terminal is switched off here: an agent's tool calls and plans are not approved from the hub"

// checkNoApproval refuses whatever it is given. Approving a permission prompt or a plan is the owner's to do with the
// terminal off: an agent that asks to run a command and is told yes has run it, which is what a terminal is. (A message
// that makes the agent ask is not refused: a message is not an approval.)
func checkNoApproval(*bodyCtx) string { return refusalApproval }

var writeRoutes = []writeRoute{
	// The chat page. Deciding on a permission or a plan comes before the rest of the page's commands, which it would
	// otherwise match.
	{methods: "POST", pattern: "/api/v2/cmd/decide", check: checkNoApproval},
	{methods: "POST", pattern: "/api/v2/cmd/*"},
	{methods: "POST", pattern: "/api/v2/uploads"},
	{methods: "POST", pattern: "/api/v2/voice"},
	{methods: "PATCH", pattern: "/api/v2/me/settings"},
	{methods: "PUT", pattern: "/api/v2/runtime-settings"},
	{methods: "POST", pattern: "/api/v2/conversations/*/side-chat"},
	{methods: "PUT", pattern: "/api/v2/conversations/*/file"},
	{methods: "POST", pattern: "/api/platform/prompt-queue"},
	{methods: "POST", pattern: "/api/platform/prompt-text"},
	{methods: "POST", pattern: "/api/platform/send-now"},
	{methods: "POST", pattern: "/api/platform/interrupt"},
	{methods: "POST", pattern: "/api/platform/switch-model"},
	{methods: "POST", pattern: "/api/platform/commands/run"},
	{methods: "POST", pattern: "/api/platform/commands/screen"},
	{methods: "POST", pattern: "/api/platform/conversations"},
	{methods: "POST", pattern: "/api/platform/side-chat"},
	{methods: "POST", pattern: "/api/platform/preview/resolve"},
	{methods: "POST", pattern: "/api/platform/file-preview"},
	// Workspaces, panes and tabs: what makes a terminal looks into the body.
	{methods: "PATCH", pattern: "/api/layout"},
	{methods: "POST", pattern: "/api/layout/pane", check: checkNewPane},
	{methods: "DELETE PATCH", pattern: "/api/layout/pane/*"},
	{methods: "POST", pattern: "/api/layout/pane/*/tabs", check: checkNewTab},
	{methods: "PATCH", pattern: "/api/layout/pane/*/tabs/order"},
	{methods: "POST DELETE PATCH", pattern: "/api/layout/pane/*/tabs/*", check: checkRestartTab},
	{methods: "POST", pattern: "/api/layout/pane/*/tabs/*/move"},
	{methods: "POST", pattern: "/api/workspace", check: checkNewWorkspace},
	{methods: "DELETE PATCH", pattern: "/api/workspace/*"},
	{methods: "PATCH", pattern: "/api/workspace/active"},
	{methods: "PATCH", pattern: "/api/workspace/reorder"},
	{methods: "POST", pattern: "/api/workspace/group"},
	{methods: "DELETE PATCH", pattern: "/api/workspace/group/*"},
	{methods: "PATCH", pattern: "/api/workspace/group/reorder"},
	// Files of the page: the editor's save (never the policy or the agent's own files, see protectedWrite) and uploads.
	{methods: "PUT", pattern: "/api/editor/file"},
	{methods: "POST", pattern: "/api/upload-file"},
	{methods: "POST", pattern: "/api/upload-image"},
	{methods: "POST", pattern: "/api/uploads/cleanup"},
	// The page's own housekeeping.
	{methods: "POST", pattern: "/api/push/*"},
	{methods: "POST", pattern: "/api/auth/logout"},
	{methods: "POST", pattern: "/api/integrations/boards/sign-in"},
}

// apiSegments are the segments of a path as the runtime splits it (empty ones dropped), when it is under /api.
func apiSegments(p string) []string {
	var segs []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 || segs[0] != "api" {
		return nil
	}
	return segs
}

func matchPattern(pattern string, segs []string) bool {
	want := strings.Split(strings.Trim(pattern, "/"), "/")
	if len(want) != len(segs) {
		return false
	}
	for i, w := range want {
		if w != "*" && w != segs[i] {
			return false
		}
	}
	return true
}

func (r writeRoute) allows(method string) bool {
	for _, m := range strings.Fields(r.methods) {
		if m == method {
			return true
		}
	}
	return false
}

// writeAllowed decides a request that changes something while the terminal is off. Every reading of its path that is
// under /api (as it came, cleaned, and with each mount point taken off) must be on the list, whatever the runtime makes
// of it, and the checks of the routes it matches run on the body.
func (f *routeFilter) writeAllowed(r *http.Request, forms []string) (string, error) {
	// A slash in an encoded segment is one segment to the runtime's layout routes and two to its other routes: no
	// route of the list has one, so nothing is guessed.
	if strings.Contains(strings.ToLower(r.URL.EscapedPath()), "%2f") {
		return refusalTerminal + ": the path has an encoded slash", nil
	}
	var checks []func(*bodyCtx) string
	found := false
	for _, form := range forms {
		segs := apiSegments(form)
		if segs == nil {
			continue
		}
		var route *writeRoute
		for i := range writeRoutes {
			if writeRoutes[i].allows(r.Method) && matchPattern(writeRoutes[i].pattern, segs) {
				route = &writeRoutes[i]
				break
			}
		}
		if route == nil {
			return refusalTerminal + ": this route is not one that may change things while it is off", nil
		}
		found = true
		if route.check != nil {
			checks = append(checks, route.check)
		}
	}
	if !found {
		return refusalTerminal + ": this route is not one that may change things while it is off", nil
	}
	if len(checks) == 0 {
		return "", nil
	}
	c := &bodyCtx{r: r}
	for _, check := range checks {
		if reason := check(c); reason != "" {
			return reason, nil
		}
		if c.err != nil {
			return "", c.err
		}
	}
	return "", nil
}

// bodyCtx reads a request's body once, the way the runtime reads it, and puts it back for the runtime.
type bodyCtx struct {
	r      *http.Request
	done   bool
	raw    []byte
	object *jsvalue.Object
	err    error
}

// fields is the body as the runtime's routes see it (nodejson.ParseBody, the one parser they use): a JSON body only for
// a JSON content type, a form as an object, anything else as text, which has no fields. A body that does not parse, or
// is not an object, has none; names are exact and the last of a repeated one wins, as in JavaScript.
func (c *bodyCtx) load() {
	if c.done {
		return
	}
	c.done = true
	raw, err := io.ReadAll(io.LimitReader(c.r.Body, maxFilterBody+1))
	if err != nil {
		c.err = err
		return
	}
	if len(raw) > maxFilterBody {
		c.err = errors.New("body too large")
		return
	}
	c.r.Body = io.NopCloser(bytes.NewReader(raw))
	c.raw = raw
	probe := *c.r
	probe.Body = io.NopCloser(bytes.NewReader(raw))
	v, _, status, _ := nodejson.ParseBody(&probe, maxFilterBody, "1mb")
	if status != 0 {
		return
	}
	c.object, _ = v.(*jsvalue.Object)
}

func (c *bodyCtx) field(name string) (any, bool) {
	c.load()
	if c.object == nil {
		return nil, false
	}
	return c.object.Get(name)
}

// ambiguous says that a name the route reads is spelled more than once in a JSON body, or in another case. The runtime
// reads the exact name and the last of a repeat, and so does field; a body that leaves room for another reading is not
// let through on the strength of that one.
func (c *bodyCtx) ambiguous(names ...string) bool {
	c.load()
	kind, _, _ := strings.Cut(c.r.Header.Get("Content-Type"), ";")
	if k := strings.ToLower(strings.TrimSpace(kind)); k != "application/json" && k != "application/ld+json" {
		return false
	}
	dec := json.NewDecoder(bytes.NewReader(bytes.TrimPrefix(c.raw, []byte("\xef\xbb\xbf"))))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return false
	}
	seen := map[string]bool{}
	for dec.More() {
		t, err := dec.Token()
		key, isKey := t.(string)
		if err != nil || !isKey {
			return false
		}
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return false
		}
		for _, n := range names {
			if !strings.EqualFold(key, n) {
				continue
			}
			if key != n || seen[n] {
				return true
			}
			seen[n] = true
		}
	}
	return false
}

// panelAllowed: a panel type that is not a terminal. An absent, empty or unknown one is a terminal tab to the runtime.
func panelAllowed(v any) bool {
	s, ok := v.(string)
	return ok && (s == agents.ClaudePanel || s == agents.CodexPanel || s == "web-browser")
}

// noCommand: a field that makes nothing run (absent, null or empty).
func noCommand(v any) bool { return v == nil || v == "" }

// checkNewPane: POST /api/layout/pane splits a pane when it has a source and an orientation, and then makes a tab of
// the panel type it names; any other is a new pane with a terminal in it.
func checkNewPane(c *bodyCtx) string {
	if c.ambiguous("sourcePaneId", "orientation", "panelType") {
		return refusalTerminal + ": the body names a field twice"
	}
	source, _ := c.field("sourcePaneId")
	orientation, _ := c.field("orientation")
	if !jsvalue.Truthy(source) || !jsvalue.Truthy(orientation) {
		return refusalTerminal + ": terminal tabs are not made"
	}
	if pt, _ := c.field("panelType"); !panelAllowed(pt) {
		return refusalTerminal + ": terminal tabs are not made"
	}
	return ""
}

// checkNewTab: POST …/tabs makes a tab of the panel type it names, running a command if it has one.
func checkNewTab(c *bodyCtx) string {
	if c.ambiguous("panelType", "command") {
		return refusalTerminal + ": the body names a field twice"
	}
	if pt, _ := c.field("panelType"); !panelAllowed(pt) {
		return refusalTerminal + ": terminal tabs are not made"
	}
	if cmd, _ := c.field("command"); !noCommand(cmd) {
		return refusalTerminal + ": tabs that run a command are not made"
	}
	return ""
}

// checkRestartTab: POST …/tabs/<id> starts a tab's session again and types the command it has into it.
func checkRestartTab(c *bodyCtx) string {
	if c.r.Method != http.MethodPost {
		return ""
	}
	if c.ambiguous("command") {
		return refusalTerminal + ": the body names a field twice"
	}
	if cmd, _ := c.field("command"); !noCommand(cmd) {
		return refusalTerminal + ": a tab is not restarted with a command"
	}
	return ""
}

// checkNewWorkspace: POST /api/workspace makes a layout whose tab is a terminal, unless it resumes an agent's
// conversation, which makes the agent's tab.
func checkNewWorkspace(c *bodyCtx) string {
	if c.ambiguous("resumeSessionId", "panelType") {
		return refusalTerminal + ": the body names a field twice"
	}
	resume, _ := c.field("resumeSessionId")
	id, isString := resume.(string)
	if !jsvalue.Truthy(resume) || !isString || !agents.IsUUID(id) {
		return refusalTerminal + ": workspaces with a terminal are not made"
	}
	pt, has := c.field("panelType")
	if !has || pt == nil {
		pt = agents.ClaudePanel
	}
	if s, _ := pt.(string); s != agents.ClaudePanel && s != agents.CodexPanel {
		return refusalTerminal + ": workspaces with a terminal are not made"
	}
	return ""
}

// --- the files the policy lives in ------------------------------------------------------------------------------------

// protectedNames are file names the hub never writes through the runtime, wherever they are: the policy and the kill
// switch, and the agent's own state (its identity, its audit log).
var protectedNames = map[string]bool{
	"policy.toml": true, "disabled": true, "machine.json": true, "agent-audit.log": true, "agent-link.json": true,
}

// protectedDirs are folders nothing in is written: the agent's env files are sourced into every agent launch, so a line
// written there runs code past the names `env.deliver` accepts.
var protectedDirs = map[string]bool{agents.EnvDirName: true}

// protectedPair: a folder inside another that holds the machine's policy.
var protectedPairs = [][2]string{{".config", "harnessly"}, {"etc", "harnessly"}}

// protectedPath says a path (relative to a project, as the editor takes it) names one of those files, however it is
// spelled: cleaned, in any case (a file system may not tell them apart).
func protectedPath(p string, extra map[string]bool) bool {
	var segs []string
	for _, s := range strings.Split(path.Clean("/"+strings.ReplaceAll(p, "\\", "/")), "/") {
		if s != "" {
			segs = append(segs, strings.ToLower(s))
		}
	}
	if len(segs) == 0 {
		return false
	}
	if base := segs[len(segs)-1]; protectedNames[base] || extra[base] {
		return true
	}
	for i, s := range segs {
		if protectedDirs[s] {
			return true
		}
		for _, pair := range protectedPairs {
			if s == pair[0] && i+1 < len(segs) && segs[i+1] == pair[1] {
				return true
			}
		}
	}
	return false
}

// protectedWrite refuses a save into the policy file, the kill switch or the agent's own files. The routes that write a
// project's files take the file's path in the query: the editor's, the chat page's and the uploads'; an upload also names
// its files in the body. A path is judged twice: by its name, and by where it really lands once links are followed.
// What cannot be worked out (a link loop, too many places it might mean, a body that does not parse) is refused.
func (f *routeFilter) protectedWrite(r *http.Request, forms []string) string {
	writes, upload := false, false
	for _, form := range forms {
		segs := apiSegments(form)
		for _, pat := range fileWriteRoutes {
			if matchPattern(pat, segs) {
				writes = true
				upload = upload || isUploadPattern(pat)
			}
		}
	}
	if !writes {
		return ""
	}
	extra := map[string]bool{}
	if f.protected != nil {
		for _, p := range f.protected() {
			extra[strings.ToLower(path.Base(p))] = true
		}
	}
	const refusal = "the machine's policy and the agent's own files are not written from here"
	const unclear = "the machine's policy and the agent's own files are not written from here: where this lands cannot be told"
	var trees []string
	if f.protectedTrees != nil {
		for _, t := range f.protectedTrees() {
			real, ok := realTarget(t)
			if !ok {
				return unclear
			}
			trees = append(trees, real)
		}
	}
	var names []string
	for k, vs := range r.URL.Query() {
		if strings.EqualFold(k, "path") {
			names = append(names, vs...)
		}
	}
	if upload {
		var query []string
		for _, vs := range r.URL.Query() {
			query = append(query, vs...)
		}
		more, ok := uploadNames(r, query)
		if !ok {
			return unclear
		}
		names = append(names, more...)
	}
	for _, v := range names {
		if protectedPath(v, extra) {
			return refusal
		}
		cands, complete := f.candidates(v, trees)
		if !complete {
			return unclear
		}
		for _, cand := range cands {
			real, ok := realTarget(cand)
			if !ok {
				return unclear
			}
			if lies(real, trees, caseInsensitiveFS) {
				return refusal
			}
		}
	}
	return ""
}

// caseInsensitiveFS: the file systems of these systems usually do not tell Policy.toml from policy.toml.
var caseInsensitiveFS = runtime.GOOS == "darwin" || runtime.GOOS == "windows"

// fileWriteRoutes are the routes that write a file named in the query or, for uploads, in the body.
var fileWriteRoutes = []string{
	"/api/editor/file", "/api/v2/conversations/*/file", "/api/upload-file", "/api/upload-image", "/api/v2/uploads",
}

func isUploadPattern(p string) bool { return p != "/api/editor/file" && !strings.HasSuffix(p, "/file") }

// maxUploadScan is the most of an upload's body the filter holds to read the names in it; a larger one is refused.
const maxUploadScan = 32 << 20

// maxUploadPieces bounds the strings of one upload that are judged alone and in pairs.
const maxUploadPieces = 8

// uploadNames are the places an upload can mean: every file name and text field of its body (read the way the runtime
// reads one), each alone and each pair joined as folder and name. The body is put back for the runtime. ok is false for
// whatever cannot be read with certainty: a content type the filter does not fully understand, a body that does not
// parse, a path-like field that is not a string, too many fields.
func uploadNames(r *http.Request, query []string) ([]string, bool) {
	if r.Body == nil || r.Body == http.NoBody {
		return nil, true
	}
	// A body is read here to know it is empty; it is put back, and read again by what follows.
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxUploadScan+1))
	if err != nil || len(raw) > maxUploadScan {
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if len(raw) == 0 {
		return withPairs(query)
	}
	rawKind := strings.TrimSpace(r.Header.Get("Content-Type"))
	if rawKind == "" {
		return nil, false
	}
	kind, params, err := mime.ParseMediaType(rawKind)
	if err != nil {
		return nil, false
	}
	pieces := append([]string{}, query...)
	switch kind {
	case "multipart/form-data":
		more, ok := multipartPieces(r, params)
		if !ok {
			return nil, false
		}
		pieces = append(pieces, more...)
	case "application/json", "application/ld+json", "application/x-www-form-urlencoded":
		if strings.HasPrefix(strings.ToLower(rawKind), "multipart/") {
			return nil, false
		}
		c := &bodyCtx{r: r}
		c.load()
		if c.err != nil {
			return nil, false
		}
		if c.raw != nil && len(bytes.TrimSpace(c.raw)) > 0 && c.object == nil {
			return nil, false
		}
		if c.object != nil {
			var ok bool
			if pieces, ok = walkStrings(c.object, pieces, 0); !ok {
				return nil, false
			}
		}
	default:
		return nil, false
	}
	return withPairs(pieces)
}

// walkStrings adds every string of a decoded body, at any depth; ok is false for a value of a kind it does not know or a
// body nested deeper than it is willing to look.
func walkStrings(v any, out []string, depth int) ([]string, bool) {
	if depth > 16 {
		return nil, false
	}
	switch x := v.(type) {
	case string:
		return append(out, x), true
	case nil, bool, float64, int, int64, json.Number:
		return out, true
	case []any:
		for _, e := range x {
			var ok bool
			if out, ok = walkStrings(e, out, depth+1); !ok {
				return nil, false
			}
		}
		return out, true
	case *jsvalue.Object:
		for _, k := range x.Keys() {
			e, _ := x.Get(k)
			var ok bool
			if out, ok = walkStrings(e, out, depth+1); !ok {
				return nil, false
			}
		}
		return out, true
	}
	return nil, false
}

// withPairs is the pieces alone and joined two by two in both orders.
func withPairs(pieces []string) ([]string, bool) {
	if len(pieces) > maxUploadPieces {
		return nil, false
	}
	out := append([]string{}, pieces...)
	for i, a := range pieces {
		for j, b := range pieces {
			if i != j && a != "" && b != "" {
				out = append(out, a+"/"+b)
			}
		}
	}
	return out, true
}

// multipartPieces are the file names and the text fields of a multipart body, which is put back for the runtime.
func multipartPieces(r *http.Request, params map[string]string) ([]string, bool) {
	boundary := params["boundary"]
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxUploadScan+1))
	if err != nil || len(raw) > maxUploadScan || boundary == "" {
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	mr := multipart.NewReader(bytes.NewReader(raw), boundary)
	var out []string
	parts := 0
	for {
		part, err := mr.NextRawPart()
		if err == io.EOF {
			return out, parts > 0 || len(bytes.TrimSpace(raw)) == 0
		}
		if err != nil {
			return nil, false
		}
		parts++
		switch strings.ToLower(strings.TrimSpace(part.Header.Get("Content-Transfer-Encoding"))) {
		case "", "7bit", "8bit", "binary":
		default:
			return nil, false
		}
		_, dp, perr := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
		if perr != nil {
			return nil, false
		}
		if n := dp["name"]; n != "" {
			out = append(out, n)
		}
		if fn := dp["filename"]; fn != "" {
			out = append(out, fn)
			if _, err := io.Copy(io.Discard, part); err != nil {
				return nil, false
			}
			continue
		}
		// A text field: its value is a name, and one too long to be judged is refused whatever the field is called.
		val, err := io.ReadAll(io.LimitReader(part, 4097))
		if err != nil || len(val) > 4096 {
			return nil, false
		}
		if len(val) > 0 {
			out = append(out, string(val))
		}
	}
}

// maxCandidates is the most places one relative path is taken to mean; more is a folder too crowded to look at, and the
// write is refused.
var maxCandidates = 4000

// candidates are the places a path of the editor can mean: itself when absolute, else under each root and under the
// folders one and two levels below it (the runtime's project is one of those; the filter is not told which). A folder
// that is itself a protected one is no project and is not taken as a base. complete is false when the list was cut off
// or there is no root to take a relative path from.
func (f *routeFilter) candidates(p string, trees []string) (out []string, complete bool) {
	p = strings.ReplaceAll(p, "\\", "/")
	if filepath.IsAbs(p) {
		return []string{filepath.Clean(p)}, true
	}
	add := func(dir string) bool {
		real, ok := realTarget(dir)
		if ok && lies(real, trees, caseInsensitiveFS) {
			return true
		}
		if len(out) >= maxCandidates {
			return false
		}
		out = append(out, filepath.Join(dir, p))
		return true
	}
	bases := 0
	for _, root := range f.roots {
		if root == "" {
			continue
		}
		bases++
		if !add(root) {
			return nil, false
		}
		l1, _ := os.ReadDir(root)
		for _, e := range l1 {
			if !e.IsDir() {
				continue
			}
			d1 := filepath.Join(root, e.Name())
			if !add(d1) {
				return nil, false
			}
			l2, _ := os.ReadDir(d1)
			for _, e2 := range l2 {
				if e2.IsDir() && !add(filepath.Join(d1, e2.Name())) {
					return nil, false
				}
			}
		}
	}
	return out, bases > 0
}

// maxLinkHops is the most links followed to find where a write lands; a chain longer than that is a loop or a trap.
const maxLinkHops = 40

// realTarget is where writing to p lands: p with every link followed, or, when p does not exist yet, its nearest
// existing folder followed with the rest put back (a dangling link counts as the place it points to). ok is false when
// more than maxLinkHops links had to be followed.
func realTarget(p string) (string, bool) {
	hops := 0
	return realTargetN(p, &hops)
}

func realTargetN(p string, hops *int) (string, bool) {
	p = filepath.Clean(p)
	rest := ""
	for cur := p; ; {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(real, rest), true
		}
		if fi, err := os.Lstat(cur); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			*hops++
			if *hops > maxLinkHops {
				return "", false
			}
			if dest, rerr := os.Readlink(cur); rerr == nil {
				if !filepath.IsAbs(dest) {
					dest = filepath.Join(filepath.Dir(cur), dest)
				}
				return realTargetN(filepath.Join(dest, rest), hops)
			}
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p, true
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// lies says target is one of the trees or inside one; with fold, without telling cases apart.
func lies(target string, trees []string, fold bool) bool {
	if fold {
		target = strings.ToLower(target)
	}
	for _, t := range trees {
		if t == "" {
			continue
		}
		if fold {
			t = strings.ToLower(t)
		}
		if target == t || strings.HasPrefix(target, strings.TrimRight(t, string(filepath.Separator))+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
