package docs

import (
	"bytes"
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

// circleRig is two organizations on one hub: the author and a mate in one space, a stranger in another, and an
// administrator of the installation in neither. With single there is the first space only, and the stranger is in no
// space at all. The documents and boards services list everything to the hub.
func circleRig(t *testing.T, shared, single bool) (*Service, map[string]*auth.Principal, *bytes.Buffer) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	st := db.S()
	who := map[string]*auth.Principal{}
	for _, n := range []string{"author", "mate", "stranger", "admin"} {
		u := &store.User{Email: n + "@example.com", Name: n}
		must(st.CreateUser(ctx, u))
		must(st.CreateIdentity(ctx, &store.Identity{UserID: u.ID, Provider: "email", Subject: u.Email, Email: u.Email, EmailVerified: true}))
		who[n] = &auth.Principal{User: *u, Admin: n == "admin"}
	}
	ours := &store.Space{Slug: "ours", Name: "Ours", CreatedBy: who["author"].User.ID}
	must(st.CreateSpace(ctx, ours))
	must(st.AddMember(ctx, ours.ID, who["mate"].User.ID, store.RoleMember))
	if !single {
		theirs := &store.Space{Slug: "theirs", Name: "Theirs", CreatedBy: who["stranger"].User.ID}
		must(st.CreateSpace(ctx, theirs))
	}
	machine := &store.Machine{SpaceID: ours.ID, Name: "box", KeyPub: "k", CreatedBy: who["author"].User.ID}
	must(st.CreateMachine(ctx, machine))

	art := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []artifact{}
		for _, slug := range []string{"plan", "private", "agent", "loose"} {
			items = append(items, artifact{Slug: slug, Title: slug})
		}
		items = append(items, artifact{Slug: "sketch", Title: "sketch", Author: "Author@example.com"})
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	t.Cleanup(art.Close)
	boards := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"boards": []board{
			{Slug: "wall", Title: "wall", Author: "author@example.com"}, {Slug: "anon", Title: "anon", Author: "agent"}}})
	}))
	t.Cleanup(boards.Close)
	logs := &bytes.Buffer{}
	s := New(Deps{Config: &config.Config{Docs: true, ResourceAccess: shared, PublicURL: "https://hub.example.com",
		ArtifactsURL: art.URL, ArtifactsToken: "t", BoardsURL: boards.URL, BoardsToken: "t"}, DB: db,
		Log: slog.New(slog.NewTextHandler(logs, nil)),
		Now: func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) }})
	// plan is open to the team, private to nobody, agent was published by a machine nobody owns. The hub has no record
	// of the rest: sketch and the board wall name the author, loose and the board anon name nobody the hub knows.
	must(s.putMeta(ctx, meta{Slug: "plan", OwnerEmail: "author@example.com", Title: "plan", TeamLevel: levelComment}))
	must(s.putMeta(ctx, meta{Slug: "private", OwnerEmail: "author@example.com", Title: "private", TeamLevel: levelNone}))
	must(s.putMeta(ctx, meta{Slug: "agent", MachineID: machine.ID, Title: "agent", TeamLevel: levelView}))
	return s, who, logs
}

func seen(t *testing.T, s *Service, p *auth.Principal) string {
	t.Helper()
	list, err := s.summaries(context.Background(), p, "")
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, d := range list {
		slugs = append(slugs, d.Slug)
	}
	return strings.Join(slugs, ",")
}

func TestTeamLevelReachesTheDocumentsOrganizationOnly(t *testing.T) {
	for name, shared := range map[string]bool{"doc_meta": false, "shared model": true} {
		t.Run(name, func(t *testing.T) {
			s, who, _ := circleRig(t, shared, false)
			ctx := context.Background()
			for person, want := range map[string]string{
				"author":   "plan,private,agent,sketch,wall",
				"mate":     "plan,agent,sketch,wall",
				"stranger": "",
				"admin":    "plan,private,agent,loose,sketch,wall,anon",
			} {
				if got := seen(t, s, who[person]); got != want {
					t.Errorf("%s lists %q, want %q", person, got, want)
				}
			}
			authors := map[string]string{"sketch": "Author@example.com", "wall": "author@example.com", "anon": "agent"}
			rank := func(person, slug string) int {
				m, _, err := s.meta(ctx, slug, reachAll())
				if err != nil {
					t.Fatal(err)
				}
				return s.rank(ctx, slug, m, authors[slug], who[person], nil)
			}
			for _, c := range []struct {
				person, slug string
				want         int
			}{
				{"author", "plan", rankManage}, {"author", "private", rankManage},
				{"mate", "plan", levelRank[levelComment]}, {"mate", "private", 0}, {"mate", "agent", levelRank[levelView]},
				{"stranger", "plan", 0}, {"stranger", "agent", 0}, {"stranger", "loose", 0},
				{"mate", "loose", 0}, {"author", "loose", 0}, {"admin", "loose", rankManage},
				{"mate", "sketch", levelRank[levelView]}, {"mate", "wall", levelRank[levelView]}, {"author", "wall", levelRank[levelView]},
				{"stranger", "sketch", 0}, {"stranger", "wall", 0}, {"mate", "anon", 0}, {"stranger", "anon", 0},
			} {
				if got := rank(c.person, c.slug); got != c.want {
					t.Errorf("%s on %s: rank %d, want %d", c.person, c.slug, got, c.want)
				}
			}
			// Opening goes the same way as the list.
			open := func(person, slug string) error {
				r := httptest.NewRequest("GET", "/x", nil)
				r.SetPathValue("slug", slug)
				_, _, err := s.open(r, who[person], levelRank[levelView])
				return err
			}
			if err := open("stranger", "plan"); err != errNoAccess {
				t.Errorf("a stranger opens the team's document: %v", err)
			}
			if err := open("mate", "plan"); err != nil {
				t.Errorf("a mate opens the team's document: %v", err)
			}
			if err := open("author", "private"); err != nil {
				t.Errorf("the author opens their own document: %v", err)
			}
			if err := open("mate", "sketch"); err != nil {
				t.Errorf("a mate opens a document the hub has no record of: %v", err)
			}
			if err := open("mate", "wall"); err != nil {
				t.Errorf("a mate opens a board: %v", err)
			}
			for _, slug := range []string{"sketch", "wall", "loose"} {
				if err := open("stranger", slug); err != errNoDoc {
					t.Errorf("a stranger opens %s: %v", slug, err)
				}
			}
		})
	}
}

func TestAShareByNameReachesAStranger(t *testing.T) {
	s, who, _ := circleRig(t, true, false)
	ctx := context.Background()
	put := func(slug string, by *auth.Principal, owner string) {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("PUT", "/x", strings.NewReader(`{"role":"comment"}`))
		s.access.Serve(w, r, resaccess.Actor{Email: by.User.Email, Admin: by.Admin}, resaccess.Ref{Kind: resaccess.KindDoc, ID: slug},
			[]string{"people", "stranger@example.com"}, resaccess.Info{Owner: owner}, nil)
		if w.Code >= 300 {
			t.Fatalf("share %s: %d %s", slug, w.Code, w.Body.String())
		}
	}
	put("private", who["author"], "author@example.com")
	put("loose", who["admin"], "")
	if got := seen(t, s, who["stranger"]); got != "private,loose" {
		t.Errorf("a stranger with two shares lists %q", got)
	}
	m, _, _ := s.meta(ctx, "private", reachAll())
	if got := s.rank(ctx, "private", m, "", who["stranger"], nil); got != levelRank[levelComment] {
		t.Errorf("the share gives rank %d", got)
	}
	if got := s.rank(ctx, "loose", nil, "", who["mate"], nil); got != 0 {
		t.Errorf("a document nobody recorded stays closed to the others: rank %d", got)
	}
}

// On a hub of one organization nothing changes for its people: what the hub cannot tie to an organization is theirs.
func TestOneOrganizationKeepsWhatItsPeopleSaw(t *testing.T) {
	for name, shared := range map[string]bool{"doc_meta": false, "shared model": true} {
		t.Run(name, func(t *testing.T) {
			s, who, logs := circleRig(t, shared, true)
			for person, want := range map[string]string{
				"author":   "plan,private,agent,loose,sketch,wall,anon",
				"mate":     "plan,agent,loose,sketch,wall,anon",
				"stranger": "", // signed in, in no space
				"admin":    "plan,private,agent,loose,sketch,wall,anon",
			} {
				if got := seen(t, s, who[person]); got != want {
					t.Errorf("%s lists %q, want %q", person, got, want)
				}
			}
			s.reportLoose(context.Background())
			if logs.Len() != 0 {
				t.Errorf("nothing to report on one organization: %s", logs)
			}
		})
	}
}

func TestTheStartNamesWhatBelongsToNoOrganization(t *testing.T) {
	s, _, logs := circleRig(t, true, false)
	s.reportLoose(context.Background())
	out := logs.String()
	if strings.Count(out, "\n") != 1 || !strings.Contains(out, "count=2") || !strings.Contains(out, `slugs="loose anon"`) {
		t.Errorf("one line with the two documents nothing places: %q", out)
	}
}
