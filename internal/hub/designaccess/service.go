// Package designaccess is the hub's side of who may see and change a machine's designs (Sinopia): the access records
// of each project, the page's access API (<platform api>/design/access/*), the snapshot the runtime enforces, and the
// guest links (/s/<token>) served on a host of their own.
package designaccess

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const (
	maxBody         = 64 << 10
	pushDebounce    = 300 * time.Millisecond
	pushTimeout     = 15 * time.Second
	sweepEvery      = time.Minute
	refreshEvery    = 5 * time.Minute
	eventsPerDay    = 200
	defaultPlatform = "/api/platform"
)

// Deps are what the service needs from the hub.
type Deps struct {
	DB  *store.DB
	Reg *link.Registry
	Log *slog.Logger
	Now func() time.Time
	// HubURL is the hub's public address: the invite links point there.
	HubURL string
	// ShareURL is the origin of the guest links; "" leaves them off. It must not be the hub's origin: a guest page of a
	// project would run next to the hub's session.
	ShareURL string
	// PlatformAPI is the runtime's platform prefix when a machine's page never said its own.
	PlatformAPI string
	// Transport reaches a machine's runtime through its tunnel.
	Transport func(id string) http.RoundTripper
}

type Service struct {
	db          *store.DB
	reg         *link.Registry
	log         *slog.Logger
	now         func() time.Time
	hubURL      string
	shareURL    string
	shareHost   string
	platformAPI string
	transport   func(id string) http.RoundTripper

	mu       sync.Mutex
	serving  map[string]bool   // machines whose agent serves design-access
	prefixes map[string]string // a machine's platform prefix, as its page uses it
	timers   map[string]*time.Timer
	lastAt   time.Time
	events   map[string]*dayCount
	tokens   map[string]cachedToken
	budget   *budget
}

type dayCount struct {
	day string
	n   int
}

func New(d Deps) *Service {
	s := &Service{
		db: d.DB, reg: d.Reg, log: d.Log, now: d.Now, hubURL: strings.TrimRight(d.HubURL, "/"), platformAPI: d.PlatformAPI,
		transport: d.Transport,
		serving:   map[string]bool{}, prefixes: map[string]string{}, timers: map[string]*time.Timer{},
		events: map[string]*dayCount{}, tokens: map[string]cachedToken{}, budget: newBudget(),
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.platformAPI == "" {
		s.platformAPI = defaultPlatform
	}
	if u, err := url.Parse(strings.TrimRight(d.ShareURL, "/")); err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
		s.shareURL = u.Scheme + "://" + u.Host
		s.shareHost = strings.ToLower(u.Host)
	}
	return s
}

// IsShareHost: the request is for the guest links' host.
func (s *Service) IsShareHost(host string) bool {
	return s.shareHost != "" && strings.EqualFold(host, s.shareHost)
}

// Connected is a machine's link coming up: a machine that serves design access gets its snapshot at once.
func (s *Service) Connected(m store.Machine, features []string) {
	on := false
	for _, f := range features {
		if f == hubproto.FeatureDesignAccess {
			on = true
		}
	}
	s.mu.Lock()
	s.serving[m.ID] = on
	s.mu.Unlock()
	if on {
		go s.push(context.Background(), m.ID)
	}
}

// Event is a project made, taken or deleted on the machine.
func (s *Service) Event(m store.Machine, e hubproto.DesignEvent) {
	if !projectIDRe.MatchString(e.Project) || !s.spendEvent(m.ID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	who, err := s.eventPerson(ctx, m, e.Person)
	if err != nil {
		s.log.Warn("design event person not resolved", "machine", m.ID, "error", err)
		return
	}
	switch e.Kind {
	case "design.created":
		_, err = s.registerCreated(ctx, m, e.Project, who)
	case "design.claim":
		_, err = s.registerClaim(ctx, m, e.Project, who)
	case "design.deleted":
		_, err = s.registerDeleted(ctx, m, e.Project, who)
	default:
		return
	}
	if err != nil {
		s.log.Warn("design event not recorded", "machine", m.ID, "kind", e.Kind, "error", err)
	}
	s.kick(m.ID)
}

// eventPerson: the address the owner file names, when that person may open the machine; otherwise the machine's
// person. A machine cannot make someone else the owner of its projects.
func (s *Service) eventPerson(ctx context.Context, m store.Machine, person string) (string, error) {
	if person = normEmail(person); person != "" {
		w, err := newWorld(ctx, s.db.S(), m)
		if err != nil {
			return "", err
		}
		if w.mayOpen(person) {
			return person, nil
		}
	}
	u, err := s.db.S().UserByID(ctx, m.CreatedBy)
	if err != nil {
		return "", err
	}
	return normEmail(u.Email), nil
}

func (s *Service) spendEvent(id string) bool {
	day := s.now().UTC().Format("2006-01-02")
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.events[id]
	if c == nil || c.day != day {
		c = &dayCount{day: day}
		s.events[id] = c
	}
	c.n++
	return c.n <= eventsPerDay
}

// kick sends a machine its snapshot soon; changes in a burst go as one.
func (s *Service) kick(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.serving[id] {
		return
	}
	if t := s.timers[id]; t != nil {
		t.Reset(pushDebounce)
		return
	}
	s.timers[id] = time.AfterFunc(pushDebounce, func() {
		s.mu.Lock()
		delete(s.timers, id)
		s.mu.Unlock()
		s.push(context.Background(), id)
	})
}

// Run sweeps what ran out (grants, invites) and keeps every machine's snapshot fresh.
func (s *Service) Run(ctx context.Context) {
	sweep := time.NewTicker(sweepEvery)
	defer sweep.Stop()
	last := s.now()
	for {
		select {
		case <-ctx.Done():
			return
		case <-sweep.C:
		}
		ids, err := s.expireDue(ctx)
		if err != nil && ctx.Err() == nil {
			s.log.Warn("design access sweep failed", "error", err)
		}
		for _, id := range ids {
			s.kick(id)
		}
		s.budget.sweep(s.now())
		s.dropTokens()
		if s.now().Sub(last) >= refreshEvery {
			last = s.now()
			s.mu.Lock()
			var all []string
			for id, on := range s.serving {
				if on {
					all = append(all, id)
				}
			}
			s.mu.Unlock()
			for _, id := range all {
				s.kick(id)
			}
		}
	}
}

// ---- the page's API ---------------------------------------------------------------------------------------------

// apiPath: <prefix>/design/access[/...] or <prefix>/docs/people under a platform prefix /api/<name>.
var apiPath = regexp.MustCompile(`^(/api/[A-Za-z0-9_-]+)(/design/access(?:/.*)?|/docs/people)$`)

// ServeMachine answers the design access API of a machine the person may open; anything else goes to the machine.
func (s *Service) ServeMachine(w http.ResponseWriter, r *http.Request, u store.User, m store.Machine) bool {
	p := r.URL.EscapedPath()
	p = strings.TrimPrefix(p, "/m/"+m.ID)
	match := apiPath.FindStringSubmatch(p)
	if match == nil {
		return false
	}
	s.mu.Lock()
	s.prefixes[m.ID] = match[1]
	s.mu.Unlock()
	rest := match[2]
	if rest == "/docs/people" {
		if r.Method != http.MethodGet {
			return false
		}
		s.people(w, r, m)
		return true
	}
	s.serveOld(w, r, u, m, strings.TrimPrefix(strings.TrimPrefix(rest, "/design/access"), "/"))
	return true
}

func (s *Service) prefixOf(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.prefixes[id]; p != "" {
		return p
	}
	return s.platformAPI
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Service) writeError(w http.ResponseWriter, err error) {
	if e, ok := asError(err); ok {
		status := statusOf[e.Code]
		if status == 0 {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": e.Message, "code": e.Code})
		return
	}
	s.log.Error("design access request failed", "error", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Внутренняя ошибка", "code": "internal"})
}

var errBadBody = fail("bad_request", "Некорректное тело запроса")

// readBody is the request's JSON object; an empty body is an empty object.
func readBody(r *http.Request) (map[string]any, error) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(raw) > maxBody {
		return nil, errBadBody
	}
	if strings.TrimSpace(string(raw)) == "" {
		return map[string]any{}, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, errBadBody
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, errBadBody
	}
	return obj, nil
}

// people is the docs' person search: the members of the machine's space whose name or address holds the query.
func (s *Service) people(w http.ResponseWriter, r *http.Request, m store.Machine) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	members, err := s.db.S().Members(r.Context(), m.SpaceID)
	if err != nil {
		s.writeError(w, err)
		return
	}
	out := []map[string]string{}
	for _, mb := range members {
		email := normEmail(mb.Email)
		if q != "" && !strings.Contains(email, q) && !strings.Contains(strings.ToLower(mb.Name), q) {
			continue
		}
		name := mb.Name
		if name == "" {
			name = localPart(email)
		}
		out = append(out, map[string]string{"id": email, "name": name})
		if len(out) >= 20 {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"people": out})
}

var errNoMachine = errors.New("machine offline")
