package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/vaultsvc"
	"github.com/ly-solutions/harnessly/internal/webstatic"
)

// The workspace is the runtime's own page served by the hub at /agents/ (flag runtime.fleet): one interface for every
// machine the person may open, the way a cluster's gateway serves it. The page is the hub's copy of the runtime's
// bundle (web/dist, compiled in with the webui tag), so it loads with every machine off. Its API goes the way the
// gateway's does:
//
//   - /agents/api/v2/… is the fleet's /api/v2 (fleet.go): the list of machines, ?machine=<id> to that machine;
//   - any other /agents/api/… (the platform API with its bootstrap, the design library, dictation, previews) goes to
//     the machine named by ?machine=, and without one (or with machine=local) to the person's home machine, the one a
//     standalone runtime would be.
//
// Everything a machine answers here runs on the hub's origin, next to the hub's session, whatever HUB_RUNTIME_DOMAIN
// says: only machines that pass the trust rule of path mode (auth.Service.RequireTrustedOnHub) are in the workspace,
// in either mode. The page's settings (hl-config) are the home machine's own, with the hub as the place to sign in.

const workspaceBase = "/agents"

// workspaceConfigTTL is how long the home machine's page settings are believed.
const workspaceConfigTTL = time.Minute

// the page waits on the home machine's settings at most workspaceSettingsWait, and a machine that did not give
// them is not asked again for workspaceMissTTL: a home machine whose agent keeps its link while its runtime hangs held
// every load of /agents/ for the whole wait. The settings it gave before are kept meanwhile.
const (
	workspaceSettingsWait = 2 * time.Second
	workspaceMissTTL      = 30 * time.Second
)

// workspaceMetas are the settings a runtime fills into its index.html (cmd/runtime/routes_web.go).
var (
	metaHLConfig    = regexp.MustCompile(`<meta name="hl-config" content="([^"]*)" />`)
	metaPlatformAPI = regexp.MustCompile(`<meta name="platform-api" content="([^"]*)" />`)
	platformPrefix  = regexp.MustCompile(`^/api/[A-Za-z0-9_-]+$`)
)

type workspaceConfig struct {
	config      map[string]any
	platformAPI string
	until       time.Time
	// missed: the machine did not give its settings; config is what it gave before, if anything.
	missed bool
}

// Workspace: whether this hub serves the fleet's workspace at /agents/ (it was built with the runtime's page). Whether a
// person gets it is still the runtime.fleet flag of their spaces.
func (s *Service) Workspace() bool {
	if s.web == nil {
		return false
	}
	_, err := fs.Stat(s.web, "index.html")
	return err == nil
}

func (s *Service) mountWorkspace(mux *http.ServeMux) {
	if !s.Workspace() {
		return
	}
	mux.Handle(workspaceBase+"/", http.HandlerFunc(s.serveWorkspace))
	mux.HandleFunc(workspaceBase, func(w http.ResponseWriter, r *http.Request) {
		target := workspaceBase + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
	})
}

func (s *Service) serveWorkspace(w http.ResponseWriter, r *http.Request) {
	if s.gate == nil {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	if !s.sandboxStoreFile(r) && refuseCrossSite(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, workspaceBase)
	api := rest == "/api" || strings.HasPrefix(rest, "/api/")
	p, err := s.auth.Authenticate(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p == nil && api {
		p = s.keyedFilePrincipal(r)
	}
	if p == nil {
		if !api && isNavigation(r) {
			http.Redirect(w, r, "/signin?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
			return
		}
		httpx.WriteError(w, r, httpx.ErrUnauthenticated)
		return
	}
	if api {
		s.workspaceAPI(w, r, p, rest)
		return
	}
	// The page: only for a person whose organization has the fleet, so a hub without it keeps /agents/ answering 404.
	if _, err := s.fleet(r.Context(), p.User.ID, true); err != nil {
		s.fail(w, r, err)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	s.workspacePage(w, r, p.User, rest)
}

// workspaceAPI sends /agents/api/… on: v2 to the fleet, the rest to the named machine or the home one.
func (s *Service) workspaceAPI(w http.ResponseWriter, r *http.Request, p *auth.Principal, rest string) {
	if rest == "/api/v2" || strings.HasPrefix(rest, "/api/v2/") {
		clone := r.Clone(r.Context())
		u := *r.URL
		u.Path, u.RawPath = rest, ""
		clone.URL = &u
		clone.RequestURI = ""
		s.fleetCall(w, clone, p.User, true)
		return
	}
	if uploadsPath.MatchString(rest) && s.uploadsStore() != nil {
		s.serveUploads(w, r, p, rest)
		return
	}
	if chatGuestList.MatchString(rest) {
		s.listGuestChatSnapshots(w, r, p.User.ID)
		return
	}
	if match := chatSharePage.FindStringSubmatch(rest); match != nil {
		s.serveChatSharePage(w, r, p, match[1])
		return
	}
	if v := s.vaultService(); v != nil && vaultPath.MatchString(rest) {
		v.Serve(w, r, vaultsvc.Person{ID: p.User.ID, Email: p.User.Email}, vaultPath.ReplaceAllString(rest, ""))
		return
	}
	if s.serveDocs(w, r, rest) {
		return
	}
	if outboxPath.MatchString(rest) {
		s.serveOutbox(w, r, p)
		return
	}
	if outboxChatsPath.MatchString(rest) {
		s.serveOutboxChats(w, r, p)
		return
	}
	if s.voiceTakes && voiceTakesPath.MatchString(rest) {
		s.serveVoiceTakes(w, r, p, rest)
		return
	}
	if s.history && historyPath.MatchString(rest) {
		s.serveHistory(w, r, p, rest)
		return
	}
	q := r.URL.Query()
	id := q.Get("machine")
	if id == "" || id == "local" {
		id = s.anchorOf(r.Context(), p.User.ID, rest)
	}
	if id == "" {
		if id = s.home(r.Context(), p.User.ID, true); id == "" {
			s.fail(w, r, s.workspaceEmpty(r.Context(), p.User.ID))
			return
		}
	}
	if !machineID.MatchString(id) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	m, err := s.admit(r.Context(), p.User.ID, id)
	if err == nil {
		err = s.trustedOnHub(r.Context(), m)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sp, err := s.db.S().SpaceByID(r.Context(), m.SpaceID)
	if err != nil || !s.fleetOn(r.Context(), sp) {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	q.Del("machine")
	if r.Method == http.MethodPost && rest == "/api/auth/logout" {
		// Signing out of the workspace is signing out of the hub: the machines hold no session of their own here.
		s.logout(w, r, "")
		return
	}
	// The design access API is the hub's (designaccess): it reads the path as the runtime's platform API, /api/<name>/….
	if s.design != nil {
		asked := r.Clone(r.Context())
		u := url.URL{Path: rest, RawQuery: q.Encode()}
		asked.URL = &u
		asked.RequestURI = ""
		home := func(ctx context.Context, w http.ResponseWriter) {
			to := r.Clone(ctx)
			u := url.URL{Path: workspaceBase + rest, RawQuery: q.Encode()}
			to.URL = &u
			to.RequestURI = ""
			s.forward(w, to, p.User, m)
		}
		if s.serveHome(w, asked, p.User, m, home) {
			return
		}
		if s.design.ServeMachine(w, asked, p.User, m) {
			return
		}
	}
	// The runtime knows every route of its API in the /agents/api/… form (cmd/runtime apiAlias), whatever its base.
	clone := r.Clone(r.Context())
	u := url.URL{Path: workspaceBase + rest, RawQuery: q.Encode()}
	clone.URL = &u
	clone.RequestURI = ""
	if s.serveChatShares(w, clone, p.User, m, rest) {
		return
	}
	if r.Method == http.MethodGet && platformMachines.MatchString(rest) {
		// what is left of the agents' subscriptions is kept as it passes, for the page to draw before any
		// machine answers.
		tee := &usageTee{ResponseWriter: w}
		s.forward(tee, clone, p.User, m)
		s.keepUsage(r.Context(), m.ID, tee)
		return
	}
	s.forward(w, clone, p.User, m)
}

// workspaceEmpty is why there is no home machine: the fleet is off, or none of the person's machines is there.
func (s *Service) workspaceEmpty(ctx context.Context, userID string) error {
	if _, err := s.fleet(ctx, userID, true); err != nil {
		return err
	}
	return httpx.ErrNotFound
}

// workspacePage serves the bundle's files and, for any other path, the page itself.
func (s *Service) workspacePage(w http.ResponseWriter, r *http.Request, u store.User, rest string) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	frameHeaders(h)
	name := strings.TrimPrefix(path.Clean("/"+rest), "/")
	if name != "" && name != "index.html" {
		if s.static.Serve(w, r, s.web, name) {
			return
		}
		// An app's page carries its id, and ids are dotted (harnessly.cashback): not a file.
		if path.Ext(name) != "" && !strings.HasPrefix(name, "apps/") {
			httpx.WriteError(w, r, httpx.ErrNotFound)
			return
		}
	}
	raw, err := s.workspaceIndex(r, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-cache")
	webstatic.WriteBody(w, r, raw, nil)
}

// workspaceIndex is index.html with the hub's base, the workspace's API prefix and the home machine's settings.
func (s *Service) workspaceIndex(r *http.Request, u store.User) ([]byte, error) {
	raw, err := fs.ReadFile(s.web, "index.html")
	if err != nil {
		return nil, err
	}
	base := s.cfg.BasePath + workspaceBase
	cfg, platformAPI := s.workspaceSettings(r, u)
	page, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	raw = bytes.Replace(raw, []byte(`<base href="/" />`), []byte(`<base href="`+html.EscapeString(base)+`/" />`), 1)
	raw = bytes.Replace(raw, []byte(`<meta name="api-base" content="" />`),
		[]byte(`<meta name="api-base" content="`+html.EscapeString(base)+`" />`), 1)
	raw = bytes.Replace(raw, []byte(`<meta name="platform-api" content="/api/platform" />`),
		[]byte(`<meta name="platform-api" content="`+html.EscapeString(platformAPI)+`" />`), 1)
	raw = bytes.Replace(raw, []byte(`<meta name="hl-config" content="{}" />`),
		[]byte(`<meta name="hl-config" content="`+html.EscapeString(string(page))+`" />`), 1)
	if name, _ := cfg["productName"].(string); name != "" {
		raw = bytes.Replace(raw, []byte(`<title>harnessly</title>`), []byte(`<title>`+html.EscapeString(name)+`</title>`), 1)
	}
	return raw, nil
}

// workspaceSettings are the page settings of the person's home machine (its own index.html, read through its tunnel and
// kept for a minute), with the hub as where the page signs in and links back. A machine that is off or does not answer
// leaves the defaults: the page still lists the machines.
func (s *Service) workspaceSettings(r *http.Request, u store.User) (map[string]any, string) {
	cfg, platformAPI := map[string]any{}, "/api/platform"
	if id := s.home(r.Context(), u.ID, true); id != "" {
		if m, err := s.admit(r.Context(), u.ID, id); err == nil && s.trustedOnHub(r.Context(), m) == nil {
			if got, ok := s.machineSettings(r, u, m); ok {
				for k, v := range got.config {
					cfg[k] = v
				}
				platformAPI = got.platformAPI
			}
		}
	}
	// The workspace's own places: the page's base and API prefix are the hub's, sign-in and «Хаб» are the hub.
	delete(cfg, "basePath")
	delete(cfg, "platformApi")
	cfg["hubUrl"] = s.cfg.PublicURL
	// the page puts what a person attaches into the hub's store too, and reads the chat's uploads from here.
	cfg["centralUploads"] = s.uploadsStore() != nil
	if link, _ := cfg["hubLink"].(string); link == "" {
		cfg["hubLink"] = s.cfg.PublicURL
	}
	return cfg, platformAPI
}

func (s *Service) machineSettings(r *http.Request, u store.User, m store.Machine) (workspaceConfig, bool) {
	s.mu.Lock()
	had, ok := s.pages[m.ID]
	s.mu.Unlock()
	if ok && s.now().Before(had.until) {
		return had, !had.missed || had.config != nil
	}
	if s.reg.Get(m.ID) == nil {
		return workspaceConfig{}, false
	}
	got, ok := s.askSettings(r, u, m)
	if !ok && r.Context().Err() != nil {
		// The page's own request went away: that says nothing of the machine.
		return had, had.config != nil
	}
	if !ok {
		// Not asked again for a while; what it gave before stands in.
		miss := workspaceConfig{config: had.config, platformAPI: had.platformAPI, until: s.now().Add(workspaceMissTTL), missed: true}
		s.keepSettings(m.ID, miss)
		return miss, miss.config != nil
	}
	got.until = s.now().Add(workspaceConfigTTL)
	s.keepSettings(m.ID, got)
	return got, true
}

func (s *Service) keepSettings(id string, c workspaceConfig) {
	s.mu.Lock()
	if len(s.pages) >= maxAuthz {
		s.pages = map[string]workspaceConfig{}
	}
	s.pages[id] = c
	s.mu.Unlock()
}

// askSettings reads the settings from the machine's own index.html, waiting for it at most workspaceSettingsWait.
func (s *Service) askSettings(r *http.Request, u store.User, m store.Machine) (workspaceConfig, bool) {
	// The wait is the machine's alone: a page that goes away meanwhile does not make it look hung.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), workspaceSettingsWait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/m/"+m.ID+"/", nil)
	if err != nil {
		return workspaceConfig{}, false
	}
	req.Host = r.Host
	req.RemoteAddr = r.RemoteAddr
	req.Header.Set("Accept", "text/html")
	rec := &pageRecorder{header: http.Header{}, status: http.StatusOK}
	s.forward(rec, req, u, m)
	if rec.status != http.StatusOK || !strings.HasPrefix(rec.header.Get("Content-Type"), "text/html") {
		return workspaceConfig{}, false
	}
	got, err := parseSettings(rec.body.Bytes())
	if err != nil {
		s.log.Warn("workspace: the home machine's page settings are unreadable", "machine", m.ID, "err", err)
		return workspaceConfig{}, false
	}
	return got, true
}

// parseSettings reads hl-config and platform-api out of a runtime's page.
func parseSettings(page []byte) (workspaceConfig, error) {
	out := workspaceConfig{config: map[string]any{}, platformAPI: "/api/platform"}
	if m := metaHLConfig.FindSubmatch(page); m != nil {
		if err := json.Unmarshal([]byte(html.UnescapeString(string(m[1]))), &out.config); err != nil {
			return workspaceConfig{}, err
		}
		if out.config == nil {
			out.config = map[string]any{}
		}
	}
	if m := metaPlatformAPI.FindSubmatch(page); m != nil {
		v := strings.TrimRight(html.UnescapeString(string(m[1])), "/")
		if !platformPrefix.MatchString(v) {
			return workspaceConfig{}, errors.New("platform-api " + v + " is not /api/<name>")
		}
		out.platformAPI = v
	}
	return out, nil
}

// pageRecorder keeps a machine's answer for the hub to read (at most 4 MiB of it).
type pageRecorder struct {
	header http.Header
	status int
	wrote  bool
	body   bytes.Buffer
}

const maxPage = 4 << 20

func (p *pageRecorder) Header() http.Header { return p.header }

func (p *pageRecorder) WriteHeader(code int) {
	if !p.wrote {
		p.status, p.wrote = code, true
	}
}

func (p *pageRecorder) Write(b []byte) (int, error) {
	p.WriteHeader(http.StatusOK)
	if p.body.Len()+len(b) > maxPage {
		return 0, errors.New("the page is too big")
	}
	return p.body.Write(b)
}

func (p *pageRecorder) Flush() {}

// storeFilePath is a file of a project in the hub's store (designaccess team.go): <base>/api/<name>/design/store/<id>/f/….
var storeFilePath = regexp.MustCompile(`^` + workspaceBase + `/api/[A-Za-z0-9_-]+/design/store/[^/]+/f/.`)

// storeKeyedPath is the same file at the board's keyed address (designaccess filekey.go): <id>/k/<key>/f/….
var storeKeyedPath = regexp.MustCompile(`^` + workspaceBase + `/api/[A-Za-z0-9_-]+/design/store/[^/]+/k/[0-9a-z]+\.[0-9a-z]+\.[A-Za-z0-9_-]+/f/.`)

// KeyedFiles is the design service's side of a board's keyed address: whose live key a request for a file there
// carries, "" when it carries none.
type KeyedFiles interface {
	KeyedFileUser(r *http.Request) string
}

// keyedFilePrincipal: the board's own files come from its sandbox without the hub's session (the cookie is SameSite=Lax
// and these requests are cross-site), so on the hub's own address every one was 401. The key at the address
// is the hub's MAC of the person and the project, so it signs in as that person for this read only; the project's
// access is then checked as theirs, as for any other read.
func (s *Service) keyedFilePrincipal(r *http.Request) *auth.Principal {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil
	}
	if !storeKeyedPath.MatchString(r.URL.Path) {
		return nil
	}
	kf, ok := s.design.(KeyedFiles)
	if !ok {
		return nil
	}
	id := kf.KeyedFileUser(r)
	if id == "" {
		return nil
	}
	u, err := s.db.S().UserByID(r.Context(), id)
	if err != nil || u.DisabledAt != nil {
		return nil
	}
	return &auth.Principal{User: u}
}

// sandboxStoreFile: a board shown from the hub's store loads its own files (support.js, styles, scripts). The board runs
// sandboxed on an opaque origin, so the browser asks for them as «cross-site», and the cross-site refusal turned every
// one away with 403: the board drew as raw markup. Such a request reads a file and changes nothing, and its
// Referer names this host: the store's answers ask for it (Referrer-Policy strict-origin), and a page of another site
// cannot make its browser send it. The person is still checked as for any other request, and the answer stays inert.
func (s *Service) sandboxStoreFile(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.Header.Get("Sec-Fetch-Site") != "cross-site" {
		return false
	}
	// A sandboxed board sends no Referer at all (its origin is opaque): its files come at its keyed address, whose key
	// designaccess checks against the signed-in person before anything is read.
	if storeKeyedPath.MatchString(r.URL.Path) {
		return true
	}
	if !storeFilePath.MatchString(r.URL.Path) {
		return false
	}
	ref, err := url.Parse(r.Header.Get("Referer"))
	if err != nil || ref.Host == "" {
		return false
	}
	fwd, _, _ := strings.Cut(r.Header.Get("X-Forwarded-Host"), ",")
	for _, own := range []string{r.Host, strings.TrimSpace(fwd), hostOf(s.cfg.PublicURL)} {
		if own != "" && strings.EqualFold(ref.Host, own) {
			return true
		}
	}
	return false
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// anchorOf is the machine that holds the project a hub route names and the person may open (admit), "" when the path
// is not one or no such machine holds it: the route is then answered as for the home machine. A project
// held only on a teammate's own machine is closed to the others by the owner's rule (own-work): taking that machine
// would refuse the person at the door instead of letting the hub answer «ask for access» from the home machine.
func (s *Service) anchorOf(ctx context.Context, userID, rest string) string {
	a, ok := s.design.(Anchor)
	if !ok {
		return ""
	}
	byID, _, err := s.fleetSpaces(ctx, userID)
	if err != nil {
		return ""
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range a.AnchorMachines(ctx, ids, rest) {
		if m, err := s.admit(ctx, userID, id); err == nil && s.trustedOnHub(ctx, m) == nil {
			return id
		}
	}
	return ""
}
