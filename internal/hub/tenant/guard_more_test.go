package tenant

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/httpx"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// fakeMachines knows machine "m1" of space "s1": "owner" owns the space, "member" is in it, everybody else is outside.
type fakeMachines struct{}

func (fakeMachines) MachineFor(_ context.Context, userID, machineID string, level Level) (store.Machine, string, error) {
	if machineID != "m1" {
		return store.Machine{}, "", httpx.ErrForbidden
	}
	switch {
	case userID == "owner":
		return store.Machine{ID: "m1", SpaceID: "s1"}, store.RoleOwner, nil
	case userID == "member" && level != Owner:
		return store.Machine{ID: "m1", SpaceID: "s1"}, store.RoleMember, nil
	}
	return store.Machine{}, "", httpx.ErrForbidden
}

// fakeOrgs knows organization "o1" (slug "acme") with "member" in it.
type fakeOrgs struct{}

func (fakeOrgs) Resolve(_ context.Context, p *auth.Principal, ref string) (store.Org, error) {
	if (ref == "o1" || ref == "acme") && p.User.ID == "member" {
		return store.Org{ID: "o1", Slug: "acme"}, nil
	}
	return store.Org{}, httpx.ErrNotFound
}

func serve(t *testing.T, pattern, path string, h func(Handler) httpx.Handler) (*Ctx, error) {
	t.Helper()
	var got *Ctx
	var err error
	mux := http.NewServeMux()
	mux.Handle(pattern, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err = h(func(_ http.ResponseWriter, r *http.Request, tc *Ctx) error {
			got = tc
			if c, ok := From(r.Context()); !ok || c != tc {
				t.Error("the request's context does not carry the tenant")
			}
			return nil
		})(w, r)
	}))
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", path, nil))
	return got, err
}

func TestMachineGuard(t *testing.T) {
	for _, c := range []struct {
		who     string
		level   Level
		id      string
		want    error
		role    string
		noAuth  bool
		readyNo bool
	}{
		{who: "owner", level: Owner, id: "m1", role: store.RoleOwner},
		{who: "member", level: Viewer, id: "m1", role: store.RoleMember},
		{who: "member", level: Owner, id: "m1", want: httpx.ErrForbidden},
		{who: "stranger", level: Member, id: "m1", want: httpx.ErrForbidden},
		{who: "owner", level: Member, id: "m2", want: httpx.ErrForbidden},
		{noAuth: true, level: Member, id: "m1", want: httpx.ErrUnauthenticated},
		{who: "owner", level: Owner, id: "m1", readyNo: true, want: httpx.ErrNotFound},
	} {
		a := fakeAuth{p: person(c.who)}
		if c.noAuth {
			a.p = nil
		}
		g := Guard{Auth: a, Machines: fakeMachines{}}
		if c.readyNo {
			g.Ready = func() error { return httpx.ErrNotFound }
		}
		got, err := serve(t, "GET /api/machines/{machineId}/x", "/api/machines/"+c.id+"/x", func(h Handler) httpx.Handler { return g.Machine(c.level, h) })
		if !errors.Is(err, c.want) {
			t.Errorf("%+v: err %v", c, err)
			continue
		}
		if c.want != nil {
			if got != nil {
				t.Errorf("%+v: the handler ran", c)
			}
			continue
		}
		if got == nil || got.Machine == nil || got.Machine.ID != "m1" || got.SpaceID != "s1" || got.Role != c.role || got.Person.User.ID != c.who {
			t.Errorf("%+v: ctx %+v", c, got)
		}
	}
}

func TestOrgGuard(t *testing.T) {
	g := Guard{Auth: fakeAuth{p: person("member")}, Orgs: fakeOrgs{}}
	for _, ref := range []string{"o1", "acme"} {
		got, err := serve(t, "GET /api/orgs/{org}/x", "/api/orgs/"+ref+"/x", g.Org)
		if err != nil || got == nil || got.Org == nil || got.Org.ID != "o1" {
			t.Errorf("%s: %v %+v", ref, err, got)
		}
	}
	other := Guard{Auth: fakeAuth{p: person("stranger")}, Orgs: fakeOrgs{}}
	if got, err := serve(t, "GET /api/orgs/{org}/x", "/api/orgs/o1/x", other.Org); !errors.Is(err, httpx.ErrNotFound) || got != nil {
		t.Errorf("stranger: %v %+v", err, got)
	}
}
