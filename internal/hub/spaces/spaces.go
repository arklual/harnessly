// Package spaces is the hub's teams: spaces and their members, and the invitations that bring people in. A space is a
// team, not the runtime's workspace (a folder on a machine).
package spaces

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/authz"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/mail"
	"github.com/ly-solutions/harnessly/internal/hub/ratelimit"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hub/tenant"
)

const (
	maxNameRunes = 60
)

var onboardingSteps = map[string]bool{"machines": true, "agents": true, "access": true, "done": true}

// Deps are what the service is built from.
type Deps struct {
	Config  *config.Config
	DB      *store.DB
	Auth    *auth.Service
	Mailer  mail.Mailer
	Events  *events.Broker
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// OnMemberGone runs after a person left or was removed from a space: whatever they had open there must end.
	OnMemberGone func(spaceID, userID string)
	// OnMachineGone runs for every machine that was revoked because the person who registered it left the space: its
	// link closes and the space is told.
	OnMachineGone func(m store.Machine)
	// OrgEvents is the bus of the organizations' live stream: it hears of every change of members and invitations, and
	// ends the streams of a person who left or whose role changed. nil: there is none.
	OrgEvents events.OrgBus
	// OnPeople runs after a person joined or left a space or their role in it changed: what they may see is different.
	OnPeople func(userID string)
}

// Service is the spaces side of the hub.
type Service struct {
	cfg         *config.Config
	db          *store.DB
	auth        *auth.Service
	mailer      mail.Mailer
	events      *events.Broker
	lim         *ratelimit.Limiter
	log         *slog.Logger
	now         func() time.Time
	gone        func(spaceID, userID string)
	machineGone func(m store.Machine)
	orgBus      events.OrgBus
	people      func(userID string)

	// linkURLs remembers, in memory only, the address of each link invitation this process made, so that the owner
	// asking again gets the same address. The database keeps hashes only; after a restart the address is gone.
	mu       sync.Mutex
	linkURLs map[string]string

	sub *http.ServeMux
}

// New makes the service.
func New(d Deps) *Service {
	s := &Service{cfg: d.Config, db: d.DB, auth: d.Auth, mailer: d.Mailer, events: d.Events, lim: d.Limiter, log: d.Log, now: d.Now, gone: d.OnMemberGone, machineGone: d.OnMachineGone, orgBus: d.OrgEvents, people: d.OnPeople, linkURLs: map[string]string{}, sub: http.NewServeMux()}
	if s.now == nil {
		s.now = time.Now
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.lim == nil {
		s.lim = ratelimit.New()
	}
	if s.events == nil {
		s.events = events.NewBroker()
	}
	return s
}

// Mount registers the endpoints.
//
// ServeMux refuses `/api/spaces/by-slug/{slug}` next to any `/api/spaces/{spaceId}/x` (the path
// /api/spaces/by-slug/x matches both), so everything under a space id lives on a router of its own, and the
// by-slug lookup is told apart before it. Other packages add their space routes with Sub.
func (s *Service) Mount(mux *http.ServeMux) {
	mux.Handle("POST /api/spaces", s.auth.Require(s.create))
	mux.Handle("/api/spaces/", http.HandlerFunc(s.route))

	g := tenant.Guard{Auth: s.auth, Spaces: s}
	s.sub.Handle("GET /api/spaces/{spaceId}", g.Space(tenant.Member, s.get))
	s.sub.Handle("PATCH /api/spaces/{spaceId}", g.Space(tenant.Owner, s.patch))
	s.sub.Handle("POST /api/spaces/{spaceId}/delete", s.auth.Require(s.deleteSpace))
	s.sub.Handle("GET /api/spaces/{spaceId}/members", g.Space(tenant.Member, s.members))
	s.sub.Handle("PATCH /api/spaces/{spaceId}/members/{userId}", g.Space(tenant.Owner, s.setRole))
	s.sub.Handle("DELETE /api/spaces/{spaceId}/members/{userId}", g.Space(tenant.Member, s.removeMember))
	s.sub.Handle("GET /api/spaces/{spaceId}/events", g.Space(tenant.Member, s.stream))

	s.sub.Handle("POST /api/spaces/{spaceId}/invites", g.Space(tenant.Owner, s.inviteByEmail))
	s.sub.Handle("POST /api/spaces/{spaceId}/invites/link", g.Space(tenant.Owner, s.inviteByLink))
	s.sub.Handle("GET /api/spaces/{spaceId}/invites", g.Space(tenant.Owner, s.listInvites))
	s.sub.Handle("DELETE /api/spaces/{spaceId}/invites/{inviteId}", g.Space(tenant.Owner, s.revokeInvite))
	mux.Handle("GET /api/invites/{token}", httpx.Handler(s.peekInvite))
	mux.Handle("POST /api/invites/{token}/accept", s.auth.Require(s.acceptInvite))
}

// Sub is the router of the paths under /api/spaces/{spaceId}/…; the machine side mounts its space routes here.
func (s *Service) Sub() *http.ServeMux { return s.sub }

const bySlugPrefix = "/api/spaces/by-slug/"

func (s *Service) route(w http.ResponseWriter, r *http.Request) {
	if slug, ok := strings.CutPrefix(r.URL.Path, bySlugPrefix); ok && slug != "" && !strings.Contains(slug, "/") {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.WriteError(w, r, httpx.New(http.StatusMethodNotAllowed, "method_not_allowed", "Метод не поддерживается"))
			return
		}
		s.auth.Require(func(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
			return s.spaceBySlug(w, r, p, slug)
		}).ServeHTTP(w, r)
		return
	}
	if _, pattern := s.sub.Handler(r); pattern == "" {
		httpx.WriteError(w, r, httpx.ErrNotFound)
		return
	}
	s.sub.ServeHTTP(w, r)
}

// Events is the broker the service publishes to; the machine side shares it.
func (s *Service) Events() *events.Broker { return s.events }

// Authz is the rights module over the database: grants and organizations count only while HUB_ORGS is not off.
func (s *Service) Authz() authz.Authz {
	return authz.Authz{S: s.db.S(), MembershipOnly: s.cfg == nil || s.cfg.Orgs == config.OrgsOff}
}

// Access is the person's role in the space. A person who is not in it, and an id that is not a space at all, both get
// the same forbidden: ids are not a way to learn which spaces exist. Someone who manages the space from above (an admin
// of its organization) has the owner's rights in it without being a member; a role on the space itself or on its
// teams gives nothing to a person who is not a member (authz).
func (s *Service) Access(ctx context.Context, userID, spaceID string) (string, error) {
	role, err := s.db.S().Role(ctx, spaceID, userID)
	if err != nil {
		return "", err
	}
	if role != "" {
		return role, nil
	}
	if s.cfg != nil && s.cfg.Orgs != config.OrgsOff {
		ok, err := s.Authz().Allowed(ctx, authz.Principal{UserID: userID}, authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeTenant, ID: spaceID}})
		if err != nil && !errors.Is(err, httpx.ErrNotFound) {
			return "", err
		}
		if ok {
			return store.RoleOwner, nil
		}
	}
	return "", httpx.ErrForbidden
}

// Owner is Access for an operation only owners may do: authz.Manage on the space as a tenant.
func (s *Service) Owner(ctx context.Context, userID, spaceID string) error {
	if _, err := s.Access(ctx, userID, spaceID); err != nil {
		return err
	}
	ok, err := s.Authz().Allowed(ctx, authz.Principal{UserID: userID}, authz.Check{Action: authz.Manage, Scope: authz.Scope{Kind: store.ScopeTenant, ID: spaceID}})
	if err != nil {
		return err
	}
	if !ok {
		return httpx.New(http.StatusForbidden, "forbidden", "Это может только владелец пространства")
	}
	return nil
}

// SpaceView is a space as the API sends it.
type SpaceView struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Role           string `json:"role"`
	CreatedAt      string `json:"createdAt"`
	OnboardingStep string `json:"onboardingStep"`
}

// View is the space as the spaces endpoints answer it, for the other ways a space is made.
func View(sp store.Space, role string) SpaceView { return view(sp, role) }

func view(sp store.Space, role string) SpaceView {
	return SpaceView{ID: sp.ID, Slug: sp.Slug, Name: sp.Name, Role: role, CreatedAt: httpx.Time(sp.CreatedAt), OnboardingStep: sp.OnboardingStep}
}

// cleanName trims a name and checks it is 1–60 characters without control characters.
func cleanName(raw string) (string, error) {
	name := strings.Join(strings.Fields(raw), " ")
	n := len([]rune(name))
	if n == 0 {
		return "", httpx.Invalid("name", "Назовите пространство")
	}
	if n > maxNameRunes {
		return "", httpx.Invalid("name", "Название длиннее 60 символов")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", httpx.Invalid("name", "В названии есть недопустимые символы")
		}
	}
	return name, nil
}

func (s *Service) create(w http.ResponseWriter, r *http.Request, p *auth.Principal) error {
	var req struct{ Name string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	ctx := r.Context()
	if ok, retry := s.lim.Allow("space:"+p.User.ID, 30, time.Hour); !ok {
		return httpx.RateLimited(ratelimit.RetrySeconds(retry))
	}
	if n, err := s.db.S().CountOwnedSpaces(ctx, p.User.ID); err != nil {
		return err
	} else if n >= s.cfg.MaxSpaces() {
		return httpx.Conflict("space_limit", "У вас уже много пространств")
	}
	sp := store.Space{Name: name, CreatedBy: p.User.ID, CreatedAt: s.now().UTC()}
	base := slugBase(name)
	// A taken slug is a unique violation; the next suffix is tried.
	for n := 1; ; n++ {
		sp.Slug = slugCandidate(base, n)
		sp.ID = ""
		err := s.db.Tx(ctx, func(st store.Store) error {
			if err := st.CreateSpace(ctx, &sp); err != nil {
				return err
			}
			return s.journal(ctx, st, sp.ID, p.User.ID, "member.add", p.User.ID, "", store.RoleOwner)
		})
		if err == nil {
			break
		}
		if !store.IsUnique(err) || n > 24 {
			return err
		}
	}
	s.auth.Audit(ctx, p.User.ID, "space_created", r, sp.ID)
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{"space": view(sp, store.RoleOwner)})
	return nil
}

func (s *Service) spaceBySlug(w http.ResponseWriter, r *http.Request, p *auth.Principal, slug string) error {
	ctx := r.Context()
	sp, err := s.db.S().SpaceBySlug(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		return httpx.ErrNotFound
	}
	if err != nil {
		return err
	}
	role, err := s.db.S().Role(ctx, sp.ID, p.User.ID)
	if err != nil {
		return err
	}
	if role == "" {
		// A slug is typed into the address bar; not being a member looks like there being no such space.
		return httpx.ErrNotFound
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": view(sp, role)})
	return nil
}

func (s *Service) get(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx, role := r.Context(), t.Role
	sp, err := s.db.S().SpaceByID(ctx, t.SpaceID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": view(sp, role)})
	return nil
}

func (s *Service) patch(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	var req struct {
		Name           *string
		OnboardingStep *string
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	id := t.SpaceID
	st := s.db.S()
	sp, err := st.SpaceByID(ctx, id)
	if err != nil {
		return err
	}
	if req.Name != nil {
		name, err := cleanName(*req.Name)
		if err != nil {
			return err
		}
		sp.Name = name
	}
	if req.OnboardingStep != nil {
		if !onboardingSteps[*req.OnboardingStep] {
			return httpx.Invalid("onboardingStep", "Неизвестный шаг")
		}
		sp.OnboardingStep = *req.OnboardingStep
	}
	if err := st.UpdateSpace(ctx, id, sp.Name, sp.OnboardingStep); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"space": view(sp, store.RoleOwner)})
	return nil
}

// MemberView is a member as the API sends it.
type MemberView struct {
	UserID   string `json:"userId"`
	Email    string `json:"email"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	JoinedAt string `json:"joinedAt"`
}

func memberView(m store.Member) MemberView {
	return MemberView{UserID: m.UserID, Email: m.Email, Name: m.Name, Role: m.Role, JoinedAt: httpx.Time(m.JoinedAt)}
}

func (s *Service) members(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx, id := r.Context(), t.SpaceID
	ms, err := s.db.S().Members(ctx, id)
	if err != nil {
		return err
	}
	out := make([]MemberView, 0, len(ms))
	for _, m := range ms {
		out = append(out, memberView(m))
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"members": out})
	return nil
}

// lockMembers makes the transaction the only one deciding who is in the space: the count of owners it reads is the one
// it changes, whatever else is asked for at the same moment.
func lockMembers(ctx context.Context, st store.Store, spaceID string) error {
	ok, err := st.LockSpace(ctx, spaceID)
	if err == nil && !ok {
		return httpx.ErrNotFound
	}
	return err
}

// journal writes a change of who is in the space, or in what role, into the journal of changes; it is called in the
// transaction that makes the change, so there is no change without its line and no line without its change. was and
// now are the person's role before and after ("" for not a member).
func (s *Service) journal(ctx context.Context, st store.Store, spaceID, actor, action, userID, was, now string) error {
	sp, err := st.SpaceByID(ctx, spaceID)
	if err != nil {
		return err
	}
	role := func(r string) string {
		if r == "" {
			return ""
		}
		b, _ := json.Marshal(map[string]string{"tenantRole": r})
		return string(b)
	}
	return st.AddAudit(ctx, &store.AuditEvent{OrgID: sp.OrgID, SpaceID: sp.ID, ActorID: actor, ScopeKind: store.ScopeTenant, ScopeID: sp.ID,
		Action: action, TargetKind: "user", TargetID: userID, Before: role(was), After: role(now), Self: actor == userID, At: s.now().UTC()})
}

var errLastOwner = httpx.Conflict("last_owner", "В пространстве должен остаться хотя бы один владелец")

func (s *Service) setRole(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	p := t.Person
	var req struct{ Role string }
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	if req.Role != store.RoleOwner && req.Role != store.RoleMember {
		return httpx.Invalid("role", "Роль — владелец или участник")
	}
	ctx := r.Context()
	spaceID, userID := t.SpaceID, r.PathValue("userId")
	var m store.Member
	err := s.db.Tx(ctx, func(st store.Store) error {
		if err := lockMembers(ctx, st, spaceID); err != nil {
			return err
		}
		cur, err := st.MemberByUser(ctx, spaceID, userID)
		if errors.Is(err, store.ErrNotFound) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Role == store.RoleOwner && req.Role != store.RoleOwner {
			if n, err := st.CountOwners(ctx, spaceID); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		if err := st.SetRole(ctx, spaceID, userID, req.Role); err != nil {
			return err
		}
		was := cur.Role
		cur.Role = req.Role
		m = cur
		if was == req.Role {
			return nil
		}
		return s.journal(ctx, st, spaceID, p.User.ID, "member.role", userID, was, req.Role)
	})
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "member_role", r, spaceID+" "+userID+" "+req.Role)
	// The person's open streams were opened with the old role: they reconnect and are given what the new one allows.
	s.events.Kick(spaceID, userID)
	s.events.PublishJSON(spaceID, "members", struct{}{}, false)
	s.peopleChanged(ctx, spaceID, events.KindMembers, userID, true)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"member": memberView(m)})
	return nil
}

func (s *Service) removeMember(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	ctx, p := r.Context(), t.Person
	spaceID, userID := t.SpaceID, r.PathValue("userId")
	if userID != p.User.ID && t.Role != store.RoleOwner {
		return httpx.New(http.StatusForbidden, "forbidden", "Это может только владелец пространства")
	}
	var machines []store.Machine
	err := s.db.Tx(ctx, func(st store.Store) error {
		if err := lockMembers(ctx, st, spaceID); err != nil {
			return err
		}
		cur, err := st.MemberByUser(ctx, spaceID, userID)
		if errors.Is(err, store.ErrNotFound) {
			return httpx.ErrNotFound
		}
		if err != nil {
			return err
		}
		if cur.Role == store.RoleOwner {
			if n, err := st.CountOwners(ctx, spaceID); err != nil {
				return err
			} else if n <= 1 {
				return errLastOwner
			}
		}
		if err := st.RemoveMember(ctx, spaceID, userID); err != nil {
			return err
		}
		// The machines the person registered are theirs: they leave with the person. A machine the space wants to
		// keep is given to another member first (the machine's owner is changed), and is then not among these.
		if machines, err = st.RevokeMachinesOf(ctx, spaceID, userID, s.now().UTC()); err != nil {
			return err
		}
		return s.journal(ctx, st, spaceID, p.User.ID, "member.remove", userID, cur.Role, "")
	})
	if err != nil {
		return err
	}
	s.auth.Audit(ctx, p.User.ID, "member_removed", r, spaceID+" "+userID)
	s.events.Kick(spaceID, userID)
	if s.gone != nil {
		s.gone(spaceID, userID)
	}
	for _, m := range machines {
		s.auth.Audit(ctx, p.User.ID, "machine_revoked", r, m.ID+" member_removed")
		if s.machineGone != nil {
			s.machineGone(m)
		}
	}
	s.events.PublishJSON(spaceID, "members", struct{}{}, false)
	s.peopleChanged(ctx, spaceID, events.KindMembers, userID, true)
	httpx.NoContent(w)
	return nil
}

func (s *Service) stream(w http.ResponseWriter, r *http.Request, t *tenant.Ctx) error {
	sub := s.events.Subscribe(t.SpaceID, t.Person.User.ID, t.Role == store.RoleOwner)
	if sub == nil {
		return httpx.New(http.StatusServiceUnavailable, "unavailable", "Сервер перезапускается")
	}
	s.events.Serve(w, r, sub)
	return nil
}
