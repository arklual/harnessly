package resaccess

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
)

func newService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(context.Background(), "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) }, "https://hub.example.com")
}

var ref = Ref{Kind: KindDoc, ID: "plan"}
var owner = Actor{Email: "owner@example.com", Name: "Owner"}

func call(t *testing.T, s *Service, a Actor, method, path, body string, want int) map[string]any {
	t.Helper()
	r := httptest.NewRequest(method, "/x", strings.NewReader(body))
	w := httptest.NewRecorder()
	var rest []string
	for _, p := range strings.Split(path, "/") {
		if p != "" {
			rest = append(rest, p)
		}
	}
	s.Serve(w, r, a, ref, rest, Info{Owner: owner.Email, OwnerName: owner.Name}, func(v any) (string, error) { return "", nil })
	if w.Code != want {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, want, w.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

func role(t *testing.T, s *Service, a Actor) string {
	t.Helper()
	r, err := s.Role(context.Background(), ref, a)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAccessLifecycle(t *testing.T) {
	s := newService(t)
	mate, stranger := Actor{Email: "mate@example.com"}, Actor{Email: "stranger@example.com"}

	if v := call(t, s, owner, "GET", "", "", 200); v["role"] != "owner" || v["stored"] != false {
		t.Fatalf("a resource without a row is the owner's: %v", v)
	}
	// Closed to the team: a stranger sees nothing and may only ask.
	call(t, s, owner, "PUT", "team", `{"level":"none"}`, 200)
	if role(t, s, stranger) != "" {
		t.Fatal("stranger must not have a role in a closed resource")
	}
	call(t, s, stranger, "GET", "people", "", 404)
	req := call(t, s, stranger, "POST", "requests", `{"level":"comment","message":"please"}`, 200)
	if req["request"] == nil {
		t.Fatalf("the request is not shown to who made it: %v", req)
	}
	call(t, s, stranger, "POST", "requests", `{"level":"comment"}`, 200) // the same open request, not a second
	open := call(t, s, owner, "GET", "", "", 200)["requests"].([]any)
	if len(open) != 1 {
		t.Fatalf("one open request expected: %v", open)
	}
	id := open[0].(map[string]any)["id"].(string)
	call(t, s, stranger, "POST", "requests/"+id, `{"decision":"allow","role":"comment"}`, 404)
	call(t, s, owner, "POST", "requests/"+id, `{"decision":"allow","role":"comment"}`, 200)
	if got := role(t, s, stranger); got != "comment" {
		t.Fatalf("allowed request gives its role, got %q", got)
	}

	// A person with edit, and one who may not widen it.
	call(t, s, owner, "PUT", "people/mate@example.com", `{"role":"edit"}`, 200)
	if got := role(t, s, mate); got != "edit" {
		t.Fatalf("got %q", got)
	}
	call(t, s, mate, "PUT", "people/other@example.com", `{"role":"view"}`, 403)
	call(t, s, owner, "DELETE", "people/mate@example.com", "", 200)
	if got := role(t, s, mate); got != "" {
		t.Fatalf("revoked person keeps %q", got)
	}
	// An administrator manages whatever the owner did.
	if got := role(t, s, Actor{Email: "root@example.com", Admin: true}); got != "manage" {
		t.Fatalf("admin role %q", got)
	}
	if log := call(t, s, owner, "GET", "log", "", 200)["log"].([]any); len(log) < 4 {
		t.Fatalf("every change is logged: %d", len(log))
	}
}

func TestGuestLink(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	made := call(t, s, owner, "POST", "shares", `{"rights":"comment","ttl":"7d","title":"for a friend"}`, 201)
	u, _ := made["url"].(string)
	if !strings.HasPrefix(u, "https://hub.example.com/d/") {
		t.Fatalf("link address %q", u)
	}
	token := strings.TrimPrefix(u, "https://hub.example.com/d/")
	g, err := s.Resolve(ctx, token)
	if err != nil || g.Rights != "comment" || g.Ref != ref {
		t.Fatalf("resolve: %+v %v", g, err)
	}
	if _, err := s.Resolve(ctx, "short"); err == nil {
		t.Fatal("a token nobody made must not open")
	}
	for i := 0; i < guestPerWindow; i++ {
		if err := s.GuestCommentAllowed(ctx, g.LinkID); err != nil {
			t.Fatalf("comment %d refused: %v", i, err)
		}
		if err := s.NoteGuestComment(ctx, g.LinkID, "t", "Guest"); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.GuestCommentAllowed(ctx, g.LinkID); err == nil {
		t.Fatal("too many comments must be refused")
	}
	id := made["id"].(string)
	call(t, s, owner, "PATCH", "shares/"+id, `{"rights":"view"}`, 200)
	if g, _ := s.Resolve(ctx, token); g == nil || g.Rights != "view" {
		t.Fatalf("rights not changed: %+v", g)
	}
	call(t, s, owner, "DELETE", "shares/"+id, "", 200)
	if _, err := s.Resolve(ctx, token); err == nil {
		t.Fatal("a revoked link must not open")
	}
	_ = http.StatusOK
}
