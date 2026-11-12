package docs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/config"
	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type gateFake struct{ on map[string]bool }

func (g gateFake) On(_ context.Context, feature, person, machine string) bool {
	return g.on[feature+"|"+person+"|"+machine]
}

type linkFake struct {
	mu    sync.Mutex
	calls []map[string]any
}

func (l *linkFake) Get(string) *link.Link { return nil }
func (l *linkFake) Do(_ context.Context, _, op string, params any, _ time.Duration) (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, _ := params.(map[string]any)
	if op == hubproto.OpDocsDeliver {
		l.calls = append(l.calls, m)
	}
	return json.RawMessage(`{}`), nil
}

type boardRig struct {
	s       *Service
	machine store.Machine
	owner   store.User
	gate    gateFake
	links   *linkFake
}

func newBoardRig(t *testing.T) *boardRig {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(ctx, "sqlite://"+filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	st := db.S()
	owner := &store.User{Email: "owner@example.com", Name: "owner"}
	if err := st.CreateUser(ctx, owner); err != nil {
		t.Fatal(err)
	}
	space := &store.Space{Slug: "ours", Name: "Ours", CreatedBy: owner.ID}
	if err := st.CreateSpace(ctx, space); err != nil {
		t.Fatal(err)
	}
	m := &store.Machine{SpaceID: space.ID, Name: "box", KeyPub: "k", CreatedBy: owner.ID}
	if err := st.CreateMachine(ctx, m); err != nil {
		t.Fatal(err)
	}
	r := &boardRig{machine: *m, owner: *owner, gate: gateFake{on: map[string]bool{}}, links: &linkFake{}}
	cfg := &config.Config{Docs: true, PublicURL: "https://hub.example.com"}
	cfg.BoardEventsSecret = "board-secret"
	cfg.BoardsPublicURL = "https://boards.example.com"
	r.s = New(Deps{Config: cfg, DB: db, Log: nil, Now: time.Now, Gate: r.gate, Links: r.links})
	return r
}

func (r *boardRig) bind(t *testing.T, slug string) {
	t.Helper()
	tab, sess := "tab-1", "session-1"
	err := r.s.bind(context.Background(), r.machine, hubproto.DocBinding{Slug: slug, Kind: "board", Title: "Схема", TabID: &tab, SessionName: &sess, Provider: "claude"})
	if err != nil {
		t.Fatal(err)
	}
}

func (r *boardRig) post(t *testing.T, secret, body string) (int, map[string]string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/platform/docs/_board-events", strings.NewReader(body))
	if secret != "" {
		req.Header.Set("X-Boards-Webhook-Secret", secret)
	}
	w := httptest.NewRecorder()
	r.s.Handler().ServeHTTP(w, req)
	var out map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func (r *boardRig) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := r.s.db.S().Q().QueryRow(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

const created = `{"event":"thread.created","slug":"flow-board","thread":{"id":"t1","anchor":{"elementId":"el-7","label":"Касса","x":1,"y":2}},
	"message":{"id":"m1","text":"Тут нужен @[Аня](person:anya@example.com) и таймаут","author":"Гость","authorEmail":"guest@example.com","authorKind":"person"}}`

func TestBoardEventsSecretAndShape(t *testing.T) {
	r := newBoardRig(t)
	if code, out := r.post(t, "", created); code != http.StatusForbidden || out["error"] != "Forbidden" {
		t.Errorf("no secret: %d %v", code, out)
	}
	if code, _ := r.post(t, "wrong", created); code != http.StatusForbidden {
		t.Errorf("wrong secret: %d", code)
	}
	if code, out := r.post(t, "board-secret", "{"); code != http.StatusBadRequest || out["error"] != "bad json" {
		t.Errorf("bad json: %d %v", code, out)
	}
	for _, body := range []string{`{"event":"x","slug":"flow-board","thread":{"id":"t"}}`, `{"event":"thread.created","slug":"A","thread":{"id":"t"}}`,
		`{"event":"thread.created","slug":"flow-board","thread":{}}`} {
		if code, out := r.post(t, "board-secret", body); code != http.StatusBadRequest || !strings.Contains(out["error"], "required") {
			t.Errorf("%s: %d %v", body, code, out)
		}
	}
	// Without a secret on the hub the route is not there.
	r.s.cfg.BoardEventsSecret = ""
	if code, _ := r.post(t, "board-secret", created); code != http.StatusNotFound {
		t.Errorf("no secret on the hub: %d, want 404", code)
	}
}

func TestBoardEventNotTakenIsIgnoredOrForwarded(t *testing.T) {
	r := newBoardRig(t)
	// No binding: nobody to take it.
	if code, out := r.post(t, "board-secret", created); code != http.StatusAccepted || out["outcome"] != "ignored" {
		t.Fatalf("unbound: %d %v", code, out)
	}
	// Bound, but the owner's switch is off: the event goes on to the old address, with the same secret and body.
	var got struct{ secret, body string }
	fwd := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		got.secret, got.body = req.Header.Get("X-Boards-Webhook-Secret"), string(b)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer fwd.Close()
	r.s.cfg.BoardEventsForwardURL = fwd.URL
	r.bind(t, "flow-board")
	if code, out := r.post(t, "board-secret", created); code != http.StatusAccepted || out["outcome"] != "forwarded" {
		t.Fatalf("switch off: %d %v", code, out)
	}
	if got.secret != "board-secret" || !strings.Contains(got.body, `"id":"m1"`) {
		t.Errorf("forwarded %+v", got)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads`); n != 0 {
		t.Errorf("a forwarded event was mirrored: %d threads", n)
	}
}

func TestBoardEventMirrorsOnceAndDelivers(t *testing.T) {
	r := newBoardRig(t)
	r.bind(t, "flow-board")
	r.gate.on[SwitchBoardEvents+"|owner@example.com|"+r.machine.ID] = true
	// Delivery in the background is not under test here: the comment is handed over by hand below.
	r.s.links = nil
	// The same event twice changes nothing.
	for i := 0; i < 2; i++ {
		if code, out := r.post(t, "board-secret", created); code != http.StatusAccepted || out["outcome"] != "applied" {
			t.Fatalf("attempt %d: %d %v", i, code, out)
		}
	}
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads WHERE slug = ? AND external_id = ?`, "flow-board", "t1"); n != 1 {
		t.Fatalf("threads: %d", n)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM doc_comments`); n != 1 {
		t.Fatalf("comments: %d", n)
	}
	// An agent's reply, then a resolve.
	reply := `{"event":"thread.replied","slug":"flow-board","thread":{"id":"t1"},"message":{"id":"m2","text":"Поправил","author":"Агент","authorKind":"agent"}}`
	r.post(t, "board-secret", reply)
	r.post(t, "board-secret", `{"event":"thread.resolved","slug":"flow-board","thread":{"id":"t1","resolved":true}}`)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_comments WHERE author_kind = 'agent' AND author_email = 'owner@example.com'`); n != 1 {
		t.Errorf("agent comment: %d", n)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads WHERE status = 'resolved'`); n != 1 {
		t.Errorf("resolved: %d", n)
	}
	r.post(t, "board-secret", `{"event":"thread.reopened","slug":"flow-board","thread":{"id":"t1"}}`)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads WHERE status = 'open'`); n != 1 {
		t.Errorf("reopened: %d", n)
	}

	// The person's comment is handed to the conversation the board was drawn in, in the gateway's words.
	var id string
	if err := r.s.db.S().Q().QueryRow(context.Background(), `SELECT id FROM doc_comments WHERE author_kind = 'person'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	r.s.links = r.links
	r.s.deliver(context.Background(), id, false)
	if len(r.links.calls) != 1 {
		t.Fatalf("deliveries: %d", len(r.links.calls))
	}
	call := r.links.calls[0]
	msg, _ := call["message"].(string)
	for _, want := range []string{"Комментарий на твоей доске «Схема» (https://boards.example.com/b/flow-board), тред t1", "у фигуры «Касса» (id el-7)",
		"Гость пишет:", "@Аня и таймаут", "board comment flow-board --reply t1", "--resolve t1"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message misses %q:\n%s", want, msg)
		}
	}
	if call["tabId"] != "tab-1" || call["sessionName"] != "session-1" {
		t.Errorf("target: %v", call)
	}
	// Once handed over, it is not sent again.
	r.s.deliver(context.Background(), id, false)
	if len(r.links.calls) != 1 {
		t.Errorf("delivered twice: %d", len(r.links.calls))
	}

	// Removing the thread removes its comments.
	r.post(t, "board-secret", `{"event":"thread.removed","slug":"flow-board","thread":{"id":"t1"}}`)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads`) + r.count(t, `SELECT COUNT(*) FROM doc_comments`); n != 0 {
		t.Errorf("left after removal: %d", n)
	}
}
