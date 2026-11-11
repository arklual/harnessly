// Package tenant is the one place a request of the hub's API learns whose data it touches. A route is registered
// through a Guard: the guard signs the person in, reads the space of the path, checks the person's place in it and
// only then calls the handler, with the answer in a Ctx. A handler behind a guard never reads the space id from the
// path and cannot forget the check: without it the handler is not called at all.
//
// The package also keeps the inventory of every route of the hub (routes.go): where each takes its tenant from and
// which check guards it. A test fails when a route is registered and the inventory does not name it.
package tenant

import (
	"context"
	"net/http"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// SpaceParam is the name of the path wildcard that carries a space id.
const SpaceParam = "spaceId"

// Level is what a route asks of the person in the space.
type Level int

const (
	// Member lets in everyone who has a place in the space (or manages it from above).
	Member Level = iota
	// Owner lets in only those who may manage the space.
	Owner
	// Viewer is Member for a resource with an access list of its own (a machine): the person must also be on it.
	// For a space it is Member.
	Viewer
)

// Ctx is what a guarded handler knows about the request: who asks, which space, and their role in it.
type Ctx struct {
	Person  *auth.Principal
	SpaceID string
	// Role is the person's role in the space (store.RoleOwner or store.RoleMember).
	Role string
	// Machine is the machine of the path, for a route guarded by Machine.
	Machine *store.Machine
	// Org is the organization of the path, for a route guarded by Org.
	Org *store.Org
}

// Spaces answers the two questions a guard asks; *spaces.Service does.
type Spaces interface {
	// Access is the person's role in the space, or the refusal a person outside it gets.
	Access(ctx context.Context, userID, spaceID string) (string, error)
	// Owner refuses everyone who may not manage the space.
	Owner(ctx context.Context, userID, spaceID string) error
}

// Authenticator finds the signed-in person of a request (nil when nobody is signed in); *auth.Service does.
type Authenticator interface {
	Authenticate(r *http.Request) (*auth.Principal, error)
}

// Machines finds the machine of a path for a person; *machines.Service does. It answers a machine the person may act
// on at the level with the person's role in its space, or the refusal: an id that is no machine and a machine of
// another space are the same forbidden.
type Machines interface {
	MachineFor(ctx context.Context, userID, machineID string, level Level) (store.Machine, string, error)
}

// Orgs finds the organization of a path (by id or slug) for a person who has something in it, or answers that it does
// not exist; orgs.Resolver does.
type Orgs interface {
	Resolve(ctx context.Context, p *auth.Principal, ref string) (store.Org, error)
}

// Guard makes handlers that run only for a person with a place in the tenant of the path.
type Guard struct {
	Auth     Authenticator
	Spaces   Spaces
	Machines Machines
	Orgs     Orgs
	// Ready, when set, is asked after the sign-in and before the tenant is looked up: a part of the hub that is
	// switched off answers its own refusal.
	Ready func() error
}

// Handler is a handler behind a guard.
type Handler func(w http.ResponseWriter, r *http.Request, t *Ctx) error

type ctxKey struct{}

// From is the tenant of a request that came through a guard.
func From(ctx context.Context) (*Ctx, bool) {
	t, ok := ctx.Value(ctxKey{}).(*Ctx)
	return t, ok
}

// Space guards a route whose pattern has {spaceId}: the person must be signed in and have the asked place in that
// space. The refusals are the ones the checks give everywhere else: 401 without a session, 403 outside the space.
func (g Guard) Space(level Level, h Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := g.person(r)
		if err != nil {
			return err
		}
		t := &Ctx{Person: p, SpaceID: r.PathValue(SpaceParam)}
		if t.SpaceID == "" {
			return httpx.ErrForbidden
		}
		ctx := r.Context()
		if level == Owner {
			if err := g.Spaces.Owner(ctx, p.User.ID, t.SpaceID); err != nil {
				return err
			}
			t.Role = store.RoleOwner
		} else {
			role, err := g.Spaces.Access(ctx, p.User.ID, t.SpaceID)
			if err != nil {
				return err
			}
			t.Role = role
		}
		return h(w, r.WithContext(context.WithValue(ctx, ctxKey{}, t)), t)
	}
}

// MachineParam is the name of the path wildcard that carries a machine id.
const MachineParam = "machineId"

// OrgParam is the name of the path wildcard that carries an organization's id or slug.
const OrgParam = "org"

// person is the signed-in person of the request, after which Ready is asked.
func (g Guard) person(r *http.Request) (*auth.Principal, error) {
	p, err := g.Auth.Authenticate(r)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, httpx.ErrUnauthenticated
	}
	if g.Ready != nil {
		if err := g.Ready(); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// Machine guards a route whose pattern has {machineId}: the machine's space is the tenant, and the person must have the
// asked place in it (Viewer: and be on the machine's access list). The handler gets the machine in the Ctx.
func (g Guard) Machine(level Level, h Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := g.person(r)
		if err != nil {
			return err
		}
		id := r.PathValue(MachineParam)
		if id == "" {
			return httpx.ErrForbidden
		}
		ctx := r.Context()
		m, role, err := g.Machines.MachineFor(ctx, p.User.ID, id, level)
		if err != nil {
			return err
		}
		t := &Ctx{Person: p, SpaceID: m.SpaceID, Role: role, Machine: &m}
		return h(w, r.WithContext(context.WithValue(ctx, ctxKey{}, t)), t)
	}
}

// Org guards a route whose pattern has {org}: the person must have something in the organization (an instance
// administrator has everything); otherwise it does not exist for them. What the person may do in it the handler
// still decides with the rights module.
func (g Guard) Org(h Handler) httpx.Handler {
	return func(w http.ResponseWriter, r *http.Request) error {
		p, err := g.person(r)
		if err != nil {
			return err
		}
		ref := r.PathValue(OrgParam)
		if ref == "" {
			return httpx.ErrNotFound
		}
		o, err := g.Orgs.Resolve(r.Context(), p, ref)
		if err != nil {
			return err
		}
		t := &Ctx{Person: p, Org: &o}
		return h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, t)), t)
	}
}
