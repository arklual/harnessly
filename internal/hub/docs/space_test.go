package docs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/auth"
	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/resaccess"
	"github.com/ly-solutions/harnessly/internal/hub/store"
)

// lastBeforeSpaces is the last migration of a database whose documents had no space yet.
const lastBeforeSpaces = 48

// A database filled before documents had a space: after the migration a document is in the space of its machine or of
// its owner's only space, the people of another space do not see it, and the documents whose space cannot be told are
// all still there and behave as they did.
func TestMigrationPutsDocumentsInTheirSpace(t *testing.T) {
	ctx := context.Background()
	url := "sqlite://" + filepath.Join(t.TempDir(), "hub.db")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	old, err := store.OpenWith(ctx, url, store.Options{Last: lastBeforeSpaces})
	must(err)
	st := old.S()
	who := map[string]*auth.Principal{}
	for _, n := range []string{"north", "south", "both", "admin"} {
		u := &store.User{Email: n + "@example.com", Name: n}
		must(st.CreateUser(ctx, u))
		who[n] = &auth.Principal{User: *u, Admin: n == "admin"}
	}
	north := &store.Space{Slug: "north", Name: "North", CreatedBy: who["north"].User.ID}
	must(st.CreateSpace(ctx, north))
	south := &store.Space{Slug: "south", Name: "South", CreatedBy: who["south"].User.ID}
	must(st.CreateSpace(ctx, south))
	must(st.AddMember(ctx, north.ID, who["both"].User.ID, store.RoleMember))
	must(st.AddMember(ctx, south.ID, who["both"].User.ID, store.RoleMember))
	machine := &store.Machine{SpaceID: north.ID, Name: "box", KeyPub: "k", CreatedBy: who["north"].User.ID}
	must(st.CreateMachine(ctx, machine))
	for _, d := range []struct {
		slug, owner string
		machine     any
	}{
		{"by-machine", "north@example.com", machine.ID}, // the machine's space
		{"by-owner", "South@example.com", nil},          // the owner's only space
		{"two-spaces", "both@example.com", nil},         // an owner in two spaces: not told
		{"no-owner", "gone@example.com", nil},           // an owner the hub does not know: not told
	} {
		_, err := old.Exec(ctx, `INSERT INTO doc_meta (slug, owner_email, machine_id, title, team_level, created_at, updated_at) VALUES (?, ?, ?, ?, 'view', 1, 1)`,
			d.slug, d.owner, d.machine, d.slug)
		must(err)
	}
	_, err = old.Exec(ctx, `INSERT INTO resource_access (kind, id, owner_email, team_level, created_at, updated_at) VALUES ('doc', 'by-machine', 'north@example.com', 'view', 1, 1)`)
	must(err)
	_, err = old.Exec(ctx, `INSERT INTO resource_access_people (kind, id, email, role, granted_at) VALUES ('doc', 'by-machine', 'guest@example.com', 'view', 1)`)
	must(err)
	must(old.Close())

	db, err := store.Open(ctx, url)
	must(err)
	t.Cleanup(func() { db.Close() })
	spaceOf := func(query string, args ...any) string {
		t.Helper()
		var v *string
		must(db.QueryRow(ctx, query, args...).Scan(&v))
		if v == nil {
			return ""
		}
		return *v
	}
	for slug, want := range map[string]string{"by-machine": north.ID, "by-owner": south.ID, "two-spaces": "", "no-owner": ""} {
		if got := spaceOf(`SELECT space_id FROM doc_meta WHERE slug = ?`, slug); got != want {
			t.Errorf("%s is in space %q, want %q", slug, got, want)
		}
	}
	for _, tbl := range []string{"resource_access", "resource_access_people"} {
		if got := spaceOf(`SELECT space_id FROM ` + tbl + ` WHERE kind = 'doc' AND id = 'by-machine'`); got != north.ID {
			t.Errorf("%s of by-machine is in space %q, want %q", tbl, got, north.ID)
		}
	}

	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []artifact{}
		for _, slug := range []string{"by-machine", "by-owner", "two-spaces", "no-owner"} {
			items = append(items, artifact{Slug: slug, Title: slug})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(art.Close)
	for name, shared := range map[string]bool{"doc_meta": false, "shared model": true} {
		t.Run(name, func(t *testing.T) {
			s := New(Deps{Config: &config.Config{Docs: true, ResourceAccess: shared, ArtifactsURL: art.URL, ArtifactsToken: "t"}, DB: db,
				Log: slog.New(slog.NewTextHandler(&strings.Builder{}, nil)), Now: func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) }})
			for person, want := range map[string]string{
				"north": "by-machine,two-spaces",
				"south": "by-owner,two-spaces",
				"both":  "by-machine,by-owner,two-spaces",
				"admin": "by-machine,by-owner,two-spaces,no-owner",
			} {
				if got := seen(t, s, who[person]); got != want {
					t.Errorf("%s lists %q, want %q", person, got, want)
				}
			}
			// Out of reach is closed, not unrecorded.
			if m, elsewhere, err := s.meta(ctx, "by-machine", s.reachOf(ctx, who["south"])); err != nil || m != nil || !elsewhere {
				t.Errorf("a document of another space: %+v, elsewhere %v, %v", m, elsewhere, err)
			}
		})
	}
}

// A machine publishes into its own space, and not over a document of another space.
func TestPublishKeepsADocumentInItsSpace(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	ctx := context.Background()
	var ours, theirs string
	if err := s.db.QueryRow(ctx, `SELECT id FROM spaces WHERE slug = 'ours'`).Scan(&ours); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(ctx, `SELECT id FROM spaces WHERE slug = 'theirs'`).Scan(&theirs); err != nil {
		t.Fatal(err)
	}
	if err := s.putMeta(ctx, meta{Slug: "report", SpaceID: ours, OwnerEmail: "author@example.com", Title: "report", TeamLevel: levelView}); err != nil {
		t.Fatal(err)
	}
	if m, elsewhere, err := s.meta(ctx, "report", reachMachine(theirs)); err != nil || m != nil || !elsewhere {
		t.Errorf("a machine of another space reads the record: %+v, elsewhere %v, %v", m, elsewhere, err)
	}
	rep := s.publish(ctx, Who{OwnerID: who["stranger"].User.ID, SpaceID: theirs}, publishEvent{Title: "x", Content: "x", Slug: "report"})
	if rep.Status != http.StatusForbidden {
		t.Errorf("publishing over a document of another space: status %d, want 403", rep.Status)
	}
	// The record of another space is not rewritten either.
	if err := s.putMeta(ctx, meta{Slug: "report", SpaceID: theirs, Title: "taken", TeamLevel: levelView}); err != nil {
		t.Fatal(err)
	}
	if m, _, _ := s.meta(ctx, "report", reachAll()); m == nil || m.SpaceID != ours || m.Title != "report" {
		t.Errorf("the record after a publish from another space: %+v", m)
	}
	// A record no space was known for takes the space of the machine it is published from again, with its access rows.
	ref := resaccess.Ref{Kind: resaccess.KindDoc, ID: "plan"}
	w := httptest.NewRecorder()
	s.access.Serve(w, httptest.NewRequest(http.MethodPut, "/x", strings.NewReader(`{"level":"edit"}`)),
		resaccess.Actor{Email: "author@example.com"}, ref, []string{"team"}, resaccess.Info{Owner: "author@example.com"}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("team level: %d %s", w.Code, w.Body.String())
	}
	if err := s.putMeta(ctx, meta{Slug: "plan", SpaceID: ours, OwnerEmail: "author@example.com", Title: "plan", TeamLevel: levelComment}); err != nil {
		t.Fatal(err)
	}
	m, _, _ := s.meta(ctx, "plan", reachAll())
	if m == nil || m.SpaceID != ours {
		t.Fatalf("plan after a publish from a machine: %+v", m)
	}
	if got := s.rank(ctx, "plan", m, "", who["mate"], nil); got != levelRank[levelEdit] {
		t.Errorf("mate on plan after it took its space: rank %d, want edit", got)
	}
	if got := s.rank(ctx, "plan", m, "", who["stranger"], nil); got != 0 {
		t.Errorf("stranger on plan: rank %d, want 0", got)
	}
}
