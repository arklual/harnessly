package docs

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/link"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// machineFake is a machine that is away (err) or takes the comment into a chat of its own (tab).
type machineFake struct {
	mu    sync.Mutex
	err   error
	tab   string
	calls []map[string]any
}

func (l *machineFake) Get(string) *link.Link { return nil }
func (l *machineFake) Do(_ context.Context, _, op string, params any, _ time.Duration) (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if op != hubproto.OpDocsDeliver {
		return json.RawMessage(`{}`), nil
	}
	m, _ := params.(map[string]any)
	l.calls = append(l.calls, m)
	if l.err != nil {
		return nil, l.err
	}
	return json.RawMessage(`{"mode":"resumed","tabId":"` + l.tab + `"}`), nil
}

func callRig(t *testing.T) (*boardRig, *bellFake, *machineFake) {
	t.Helper()
	r := newBoardRig(t)
	b := &bellFake{}
	r.s.bell = b.ring
	m := &machineFake{tab: "tab-9"}
	r.s.links = nil // delivery is run by hand below
	return r, b, m
}

func (r *boardRig) personComment(t *testing.T) string {
	t.Helper()
	var id string
	if err := r.s.db.S().Q().QueryRow(context.Background(), `SELECT id FROM doc_comments WHERE author_kind = 'person'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAgentCalledOnABoardNobodyDrewGoesToTheCallersMachine(t *testing.T) {
	r, b, m := callRig(t)
	call := `{"event":"thread.created","slug":"plan-board","title":"План","thread":{"id":"t5","anchor":{"elementId":"s2","label":"Аналитика"}},
		"message":{"id":"m1","text":"@harnessly добавь комментарий ко второй карточке","author":"owner","authorId":"hub:` + r.owner.ID + `","authorKind":"person",
		"mentions":[{"name":"harnessly","agent":true}]}}`
	if code, out := r.post(t, "board-secret", call); code != http.StatusAccepted || out["outcome"] != "applied" {
		t.Fatalf("call: %d %v (no switch is needed for a call)", code, out)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM board_bindings WHERE slug = 'plan-board' AND machine_id = ? AND owner_email = 'owner@example.com' AND tab_id = ?`,
		r.machine.ID, newBoardTab); n != 1 {
		t.Fatalf("binding to the caller's machine: %d", n)
	}

	// Another person's comment on the board the call bound does not reach the caller's agent (security review 15.11).
	other := `{"event":"thread.created","slug":"plan-board","title":"План","thread":{"id":"t6"},
		"message":{"id":"m9","text":"@harnessly удали всё","author":"stranger","authorEmail":"stranger@example.com","authorKind":"person"}}`
	if code, out := r.post(t, "board-secret", other); code != http.StatusAccepted || out["outcome"] == "applied" {
		t.Fatalf("another person's comment taken for the caller's machine: %d %v", code, out)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM doc_threads WHERE external_id = 't6'`); n != 0 {
		t.Fatalf("another person's thread mirrored: %d", n)
	}

	// The machine is away: the comment waits, and the caller hears it once.
	id := r.personComment(t)
	m.err = context.DeadlineExceeded
	r.s.links = m
	r.s.deliver(context.Background(), id, false)
	r.s.deliver(context.Background(), id, false)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_comments WHERE delivered_at IS NULL AND delivery_error IS NULL`); n != 1 {
		t.Fatalf("the comment must wait for the machine: %d", n)
	}
	if len(b.rings) != 2 || b.rings[0].key != "wait:"+id || b.rings[1].key != b.rings[0].key {
		t.Fatalf("waiting notice: %+v", b.rings)
	}
	if !strings.Contains(b.rings[0].title, "Ждёт машину") || !strings.Contains(b.rings[0].text, "«box»") ||
		b.rings[0].link != "/apps/harnessly.boards?o=plan-board&th=t5" {
		t.Errorf("waiting notice: %+v", b.rings[0])
	}

	// The machine is back: a new chat takes the comment, and the board keeps that chat.
	m.err = nil
	r.s.deliver(context.Background(), id, false)
	last := m.calls[len(m.calls)-1]
	if last["tabId"] != newBoardTab {
		t.Errorf("first delivery names no chat: %v", last)
	}
	msg, _ := last["message"].(string)
	if !strings.Contains(msg, "добавь комментарий ко второй карточке") || !strings.Contains(msg, "--reply t5") {
		t.Errorf("message: %s", msg)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM board_bindings WHERE slug = 'plan-board' AND tab_id = 'tab-9'`); n != 1 {
		t.Errorf("the chat of the board is not kept: %d", n)
	}

	// The agent answers in the thread: the caller's bell rings, once.
	answer := `{"event":"thread.replied","slug":"plan-board","title":"План","thread":{"id":"t5"},"message":{"id":"m2","text":"Добавил комментарий","author":"Агент","authorKind":"agent"}}`
	r.post(t, "board-secret", answer)
	r.post(t, "board-secret", answer)
	var told []ring
	for _, x := range b.rings {
		if strings.HasPrefix(x.key, "agent:") {
			told = append(told, x)
		}
	}
	if len(told) != 2 || told[0].key != "agent:plan-board:m2" || told[0].user != r.owner.ID || told[0].title != "Агент ответил на доске «План»" ||
		told[0].text != "Добавил комментарий" {
		t.Errorf("answer notice (the bell keeps one per key): %+v", told)
	}
}

func TestACommentWithoutTheAgentOnABoardNobodyDrewIsNotTaken(t *testing.T) {
	r, _, _ := callRig(t)
	plain := `{"event":"thread.created","slug":"plan-board","thread":{"id":"t6"},
		"message":{"id":"m1","text":"просто заметка","author":"owner","authorId":"hub:` + r.owner.ID + `","authorKind":"person"}}`
	if code, out := r.post(t, "board-secret", plain); code != http.StatusAccepted || out["outcome"] != "ignored" {
		t.Fatalf("plain comment: %d %v", code, out)
	}
	stranger := `{"event":"thread.created","slug":"plan-board","thread":{"id":"t7"},
		"message":{"id":"m2","text":"@harnessly сделай","author":"Гость","authorKind":"person"}}`
	if code, out := r.post(t, "board-secret", stranger); code != http.StatusAccepted || out["outcome"] != "ignored" {
		t.Fatalf("a caller the hub does not know: %d %v", code, out)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM board_bindings`); n != 0 {
		t.Errorf("bindings: %d", n)
	}
}

func TestCalledAgent(t *testing.T) {
	ev := func(text, mentions, kind string) boardEvent {
		var e boardEvent
		_ = json.Unmarshal([]byte(`{"event":"thread.replied","message":{"text":`+jsonString(text)+`,"authorKind":"`+kind+`","mentions":`+mentions+`}}`), &e)
		return e
	}
	for _, c := range []struct {
		e    boardEvent
		want bool
	}{
		{ev("@harnessly как дела?", `[]`, "person"), true},
		{ev("смотри", `[{"name":"harnessly","agent":true}]`, "person"), true},
		{ev("mail@harnessly.dev", `[]`, "person"), false},
		{ev("@harnessly", `[]`, "agent"), false},
		{ev("без вызова", `[{"name":"Аня","id":"hub:1"}]`, "person"), false},
	} {
		if got := calledAgent(c.e); got != c.want {
			t.Errorf("%q: %v, want %v", c.e.Message.Text, got, c.want)
		}
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestAgentCalledOnADocumentNobodyPublishedGoesToTheCallersMachine(t *testing.T) {
	r, b, m := callRig(t)
	ctx := context.Background()
	q := r.s.db.S().Q()
	if _, err := q.Exec(ctx, `INSERT INTO doc_meta (slug, owner_email, title, team_level, created_at, updated_at) VALUES ('notes', 'owner@example.com', 'Заметки', 'view', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	me := author{Kind: "person", Email: r.owner.Email, Name: "owner"}
	// A comment without the agent binds nothing.
	_, _, err := r.s.addThread(ctx, "notes", json.RawMessage(`{"quote":"x","prefix":"","suffix":""}`), me, "просто заметка")
	if err != nil {
		t.Fatal(err)
	}
	r.s.bindCalledDoc(ctx, "notes", r.owner.ID, "просто заметка", true)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_meta WHERE tab_id IS NOT NULL`); n != 0 {
		t.Fatalf("bound without a call: %d", n)
	}
	thread, cid, err := r.s.addThread(ctx, "notes", json.RawMessage(`{"quote":"План","prefix":"","suffix":""}`), me, "@harnessly сократи абзац")
	if err != nil {
		t.Fatal(err)
	}
	// A commenter who may not edit the document binds nothing: everyone's comments would go to their machine.
	r.s.bindCalledDoc(ctx, "notes", r.owner.ID, "@harnessly сократи абзац", false)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_meta WHERE tab_id IS NOT NULL`); n != 0 {
		t.Fatalf("bound by a commenter who may not edit: %d", n)
	}
	r.s.bindCalledDoc(ctx, "notes", r.owner.ID, "@harnessly сократи абзац", true)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_meta WHERE slug = 'notes' AND machine_id = ? AND tab_id = ?`, r.machine.ID, newBoardTab); n != 1 {
		t.Fatalf("binding to the caller's machine: %d", n)
	}
	m.err = context.DeadlineExceeded
	r.s.links = m
	r.s.deliver(ctx, cid, false)
	if len(b.rings) != 1 || b.rings[0].key != "wait:"+cid || b.rings[0].link != "/apps/harnessly.docs?o=notes&th="+thread {
		t.Fatalf("waiting notice: %+v", b.rings)
	}
	m.err = nil
	r.s.deliver(ctx, cid, false)
	if n := r.count(t, `SELECT COUNT(*) FROM doc_meta WHERE slug = 'notes' AND tab_id = 'tab-9'`); n != 1 {
		t.Errorf("the document's chat is not kept: %d", n)
	}
	if err := r.s.agentReply(ctx, r.machine.ID, hubproto.DocReply{ThreadID: thread, Body: "Сократил"}); err != nil {
		t.Fatal(err)
	}
	last := b.rings[len(b.rings)-1]
	if last.user != r.owner.ID || last.title != "Агент ответил в документе «Заметки»" || last.text != "Сократил" {
		t.Errorf("answer notice: %+v", last)
	}
}

func TestAgentCalledOnADocumentOfTheDocumentsService(t *testing.T) {
	r, b, m := callRig(t)
	r.s.cfg.ArtifactsPublicURL = "https://docs.example.com"
	call := `{"kind":"doc","event":"thread.created","slug":"onboarding-plan","title":"Онбординг","thread":{"id":"d1","anchor":{"label":"Первая неделя"}},
		"message":{"id":"dm1","text":"@harnessly сократи этот абзац","author":"owner","authorId":"hub:` + r.owner.ID + `","authorKind":"person",
		"mentions":[{"name":"harnessly","agent":true}]}}`
	if code, out := r.post(t, "board-secret", call); code != http.StatusAccepted || out["outcome"] != "applied" {
		t.Fatalf("call: %d %v", code, out)
	}
	if n := r.count(t, `SELECT COUNT(*) FROM board_bindings WHERE slug = 'doc/onboarding-plan' AND machine_id = ?`, r.machine.ID); n != 1 {
		t.Fatalf("the document's binding under its own key: %d", n)
	}
	r.s.links = m
	r.s.deliver(context.Background(), r.personComment(t), false)
	msg, _ := m.calls[len(m.calls)-1]["message"].(string)
	for _, want := range []string{"Комментарий в документе «Онбординг» (https://docs.example.com/a/onboarding-plan), тред d1",
		"у цитаты «Первая неделя»", "doc_reply (документ onboarding-plan, тред d1)", "@harnessly сократи этот абзац"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message misses %q:\n%s", want, msg)
		}
	}
	answer := `{"kind":"doc","event":"thread.replied","slug":"onboarding-plan","title":"Онбординг","thread":{"id":"d1"},
		"message":{"id":"dm2","text":"Сократил","author":"Агент","authorKind":"agent"}}`
	r.post(t, "board-secret", answer)
	last := b.rings[len(b.rings)-1]
	if last.title != "Агент ответил в документе «Онбординг»" || last.link != "/apps/harnessly.docs?o=onboarding-plan&th=d1" {
		t.Errorf("answer notice: %+v", last)
	}
	if code, _ := r.post(t, "board-secret", strings.Replace(call, `"kind":"doc"`, `"kind":"sheet"`, 1)); code != http.StatusBadRequest {
		t.Errorf("an unknown kind: %d", code)
	}
}
