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

type fakeAuth struct {
	p   *auth.Principal
	err error
}

func (f fakeAuth) Authenticate(*http.Request) (*auth.Principal, error) { return f.p, f.err }

// fakeSpaces knows one space, "s1": "owner" owns it, "member" is in it, everybody else is outside.
type fakeSpaces struct{ asked []string }

func (f *fakeSpaces) Access(_ context.Context, userID, spaceID string) (string, error) {
	f.asked = append(f.asked, "access "+userID+" "+spaceID)
	if spaceID != "s1" {
		return "", httpx.ErrForbidden
	}
	switch userID {
	case "owner":
		return store.RoleOwner, nil
	case "member":
		return store.RoleMember, nil
	}
	return "", httpx.ErrForbidden
}

func (f *fakeSpaces) Owner(ctx context.Context, userID, spaceID string) error {
	f.asked = append(f.asked, "owner "+userID+" "+spaceID)
	if spaceID == "s1" && userID == "owner" {
		return nil
	}
	return httpx.ErrForbidden
}

func person(id string) *auth.Principal {
	p := &auth.Principal{}
	p.User.ID = id
	return p
}

// call sends a request for the space through a guard of the level and says what came of it.
func call(t *testing.T, a fakeAuth, level Level, spaceID string) (called *Ctx, fromCtx *Ctx, sp *fakeSpaces, err error) {
	t.Helper()
	sp = &fakeSpaces{}
	g := Guard{Auth: a, Spaces: sp}
	mux := http.NewServeMux()
	mux.Handle("GET /api/spaces/{spaceId}/thing", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		err = g.Space(level, func(_ http.ResponseWriter, r *http.Request, tc *Ctx) error {
			called = tc
			fromCtx, _ = From(r.Context())
			return nil
		})(w, r)
	}))
	mux.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/spaces/"+spaceID+"/thing", nil))
	return called, fromCtx, sp, err
}

func TestGuardLetsInOnlyThoseWithAPlaceInTheSpace(t *testing.T) {
	cases := []struct {
		name    string
		a       fakeAuth
		level   Level
		space   string
		wantErr error
		role    string
	}{
		{"nobody signed in", fakeAuth{}, Member, "s1", httpx.ErrUnauthenticated, ""},
		{"a broken session", fakeAuth{err: httpx.ErrUnauthenticated}, Member, "s1", httpx.ErrUnauthenticated, ""},
		{"an outsider", fakeAuth{p: person("stranger")}, Member, "s1", httpx.ErrForbidden, ""},
		{"a member of another space", fakeAuth{p: person("member")}, Member, "s2", httpx.ErrForbidden, ""},
		{"a member", fakeAuth{p: person("member")}, Member, "s1", nil, store.RoleMember},
		{"an owner as a member", fakeAuth{p: person("owner")}, Member, "s1", nil, store.RoleOwner},
		{"a member where an owner is asked", fakeAuth{p: person("member")}, Owner, "s1", httpx.ErrForbidden, ""},
		{"an owner", fakeAuth{p: person("owner")}, Owner, "s1", nil, store.RoleOwner},
		{"an owner of another space", fakeAuth{p: person("owner")}, Owner, "s2", httpx.ErrForbidden, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			called, fromCtx, _, err := call(t, c.a, c.level, c.space)
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error %v, want %v", err, c.wantErr)
			}
			if c.wantErr != nil {
				if called != nil {
					t.Fatal("the handler ran for a refused request")
				}
				return
			}
			if called == nil || called.SpaceID != c.space || called.Role != c.role || called.Person != c.a.p {
				t.Fatalf("the handler got %+v", called)
			}
			if fromCtx != called {
				t.Error("the request's context does not carry the tenant")
			}
		})
	}
}

func TestGuardAsksNothingOfTheSpacesBeforeASession(t *testing.T) {
	_, _, sp, _ := call(t, fakeAuth{}, Owner, "s1")
	if len(sp.asked) != 0 {
		t.Errorf("the spaces were asked without a session: %v", sp.asked)
	}
}

func TestFromOutsideAGuard(t *testing.T) {
	if _, ok := From(context.Background()); ok {
		t.Error("a tenant out of nowhere")
	}
}
