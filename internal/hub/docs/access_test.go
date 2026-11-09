package docs

import (
	"context"
	"encoding/json"
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

func accessRig(t *testing.T) (*Service, *http.ServeMux, *[]string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, e := range []string{"author@example.com", "mate@example.com"} {
		if err := db.S().CreateUser(ctx, &store.User{Email: e, Name: e}); err != nil {
			t.Fatal(err)
		}
	}
	var mails []string
	s := New(Deps{Config: &config.Config{ResourceAccess: true, PublicURL: "https://hub.example.com"}, DB: db,
		Now: func() time.Time { return time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC) },
		Notice: func(_ context.Context, to, subject, _, _ string) error {
			mails = append(mails, to+": "+subject)
			return nil
		}})
	mux := http.NewServeMux()
	s.Mount(mux)
	return s, mux, &mails
}

func TestMentionsAreKeptAndMailedOnce(t *testing.T) {
	s, _, mails := accessRig(t)
	ctx := context.Background()
	by := &auth.Principal{User: store.User{Email: "author@example.com", Name: "Author"}}
	id, cid, err := s.addThread(ctx, "plan", []byte(`{}`), personOf(by), "x")
	if err != nil || id == "" {
		t.Fatal(err)
	}
	body := "hi @[Mate](person:mate@example.com) and @[Mate](person:MATE@example.com), @[Nobody](person:nobody@example.com), @[Me](person:author@example.com)"
	s.mention(ctx, summary{Slug: "plan", Title: "Plan"}, cid, body, by)
	s.mention(ctx, summary{Slug: "plan", Title: "Plan"}, cid, body, by)
	if len(*mails) != 1 || !strings.HasPrefix((*mails)[0], "mate@example.com") {
		t.Fatalf("exactly the known, other person is told, once: %v", *mails)
	}
	var n int
	if err := s.db.S().Q().QueryRow(ctx, `SELECT COUNT(*) FROM doc_comment_mentions WHERE comment_id = ? AND notified_at IS NOT NULL`, cid).Scan(&n); err != nil || n != 1 {
		t.Fatalf("mentions kept: %d %v", n, err)
	}
}

func TestGuestCommentsNeedTheCommentRight(t *testing.T) {
	s, mux, _ := accessRig(t)
	owner := resaccess.Actor{Email: "author@example.com", Name: "Author"}
	token := func(rights string) string {
		r := httptest.NewRequest("POST", "/x", strings.NewReader(`{"rights":"`+rights+`","ttl":"1d"}`))
		w := httptest.NewRecorder()
		s.access.Serve(w, r, owner, resaccess.Ref{Kind: resaccess.KindDoc, ID: "plan"}, []string{"shares"}, resaccess.Info{Owner: owner.Email}, nil)
		var out struct{ URL string }
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 201 {
			t.Fatalf("link: %d %s", w.Code, w.Body.String())
		}
		return strings.TrimPrefix(out.URL, "https://hub.example.com/d/")
	}
	post := func(tok, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", s.prefix+"-guest/"+tok+"/comments", strings.NewReader(body)))
		return w
	}
	if w := post(token("view"), `{"name":"Guest","body":"hello"}`); w.Code != 403 {
		t.Fatalf("a view link must not comment: %d", w.Code)
	}
	tok := token("comment")
	if w := post(tok, `{"name":"Guest","body":"hello"}`); w.Code != 201 {
		t.Fatalf("comment: %d %s", w.Code, w.Body.String())
	}
	if w := post(tok, `{"name":"","body":"hello"}`); w.Code != 400 {
		t.Fatalf("a guest must name themselves: %d", w.Code)
	}
	if w := post(tok, `{"name":"Bot","body":"spam","website":"http://x"}`); w.Code != 201 {
		t.Fatalf("a bot is answered as if it worked: %d", w.Code)
	}
	if w := post("short", `{"name":"G","body":"x"}`); w.Code != 404 {
		t.Fatalf("unknown token: %d", w.Code)
	}
	th, err := s.threads(context.Background(), "plan")
	if err != nil || len(th) != 1 || th[0].CreatedBy.Kind != "guest" {
		t.Fatalf("one guest thread expected (a bot's is not kept): %+v %v", th, err)
	}
}
