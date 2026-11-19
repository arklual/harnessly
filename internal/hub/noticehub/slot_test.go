package noticehub

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/store"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/notices"
)

func testService(t *testing.T, on func(*http.Request, string, string) bool) *Service {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/h.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := notices.EnsureSchema(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return New(notices.Wrap(db, false), on)
}

func slotEvent(t *testing.T, items ...hubproto.NoticeSlotItem) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"event": hubproto.EventNoticeSlot, "notices": items})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func question(id, kind string) hubproto.NoticeSlotItem {
	return hubproto.NoticeSlotItem{ID: id, Conversation: "c-" + id, CreatedAt: 1,
		Doc: json.RawMessage(`{"id":"` + id + `","kind":"` + kind + `","title":"t"}`)}
}

// the hub keeps a machine's waiting questions (not its statuses) and reads the ones that are gone.
func TestSlotKeepsWaitingQuestions(t *testing.T) {
	ctx := context.Background()
	s := testService(t, nil)
	m := store.Machine{ID: "m1"}
	s.Slot(ctx, m, slotEvent(t, question("q1", "confirm"), question("s1", "status"), question("q2", "choice")))
	items, err := s.Notices.Store.Items(ctx, SlotPerson("m1"), 10)
	if err != nil || len(items) != 2 {
		t.Fatalf("items: %v %+v", err, items)
	}
	s.Slot(ctx, m, slotEvent(t, question("q2", "choice")))
	items, _ = s.Notices.Store.Items(ctx, SlotPerson("m1"), 10)
	if len(items) != 1 || items[0].Key != "q2" || items[0].Chat != "c-q2" {
		t.Fatalf("after q1 ended: %+v", items)
	}
	// Another replica (no memory of the list) finds what is gone from the database.
	other := &Service{Notices: s.Notices}
	other.Slot(ctx, m, slotEvent(t))
	if items, _ = s.Notices.Store.Items(ctx, SlotPerson("m1"), 10); len(items) != 0 {
		t.Fatalf("after the slot emptied: %+v", items)
	}
	if !other.feeds(ctx, "m1") || (&Service{Notices: s.Notices}).feeds(ctx, "m2") {
		t.Fatal("the mark of a machine that sends its slot")
	}
}

// with the switch on, the page's stream of the slot is the hub's, once the machine sends its slot.
func TestSlotStreamIsTheHubsOnceTheMachineSendsIt(t *testing.T) {
	on := true
	s := testService(t, func(*http.Request, string, string) bool { return on })
	u, m := store.User{ID: "u1", Email: "a@x.ru"}, store.Machine{ID: "m1"}
	serve := func(ctx context.Context) (*httptest.ResponseRecorder, bool) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/m/m1/api/platform/notices/stream", nil).WithContext(ctx)
		return w, s.ServeMachine(w, r, u, m)
	}
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	if _, served := serve(ended); served {
		t.Fatal("a machine that never sent its slot keeps its own stream")
	}
	s.Slot(context.Background(), m, slotEvent(t))
	on = false
	if _, served := serve(ended); served {
		t.Fatal("switch off: the machine answers")
	}
	on = true

	pr, pw := ioPipe()
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	done := make(chan bool)
	go func() {
		r := httptest.NewRequest("GET", "/m/m1/api/platform/notices/stream", nil).WithContext(ctx)
		done <- s.ServeMachine(pw, r, u, m)
	}()
	lines := bufio.NewScanner(pr)
	wait := func(want string) {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			got := make(chan string, 1)
			go func() {
				if lines.Scan() {
					got <- lines.Text()
				} else {
					got <- "\x00"
				}
			}()
			select {
			case l := <-got:
				if l == want {
					return
				}
				if l == "\x00" {
					t.Fatalf("the stream ended before %q", want)
				}
			case <-deadline:
				t.Fatalf("no %q", want)
			}
		}
	}
	wait("event: hello")
	s.Slot(context.Background(), m, slotEvent(t, question("q1", "confirm")))
	wait("event: slot")
	stop()
	if !<-done {
		t.Fatal("the stream was not served by the hub")
	}
}

// an event too long for the relay goes without its document, and the replica that hears it reads it.
func TestRelayShortensLongEvents(t *testing.T) {
	ctx := context.Background()
	a := testService(t, nil)
	b := &Service{Notices: notices.New(a.Notices.Store)}
	var sent []json.RawMessage
	a.Relay(func(person string, data json.RawMessage) { sent = append(sent, data) })
	deliver := b.Relay(func(string, json.RawMessage) { t.Fatal("a delivered event is not passed on again") })
	feed, stop := b.Notices.Bus.Subscribe("u1")
	defer stop()

	long := `{"text":"` + strings.Repeat("x", 9000) + `"}`
	if _, err := a.Notices.Add(ctx, "u1", notices.Item{Key: "k1", At: 1, Doc: json.RawMessage(long)}, ""); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || len(sent[0]) > relayRoom || strings.Contains(string(sent[0]), "xxx") {
		t.Fatalf("sent: %d events, first %d bytes", len(sent), len(sent[0]))
	}
	deliver("u1", sent[0])
	select {
	case e := <-feed:
		if e.Type != notices.EventNew || e.Item == nil || string(e.Item.Doc) != long {
			t.Fatalf("delivered: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("nothing delivered")
	}

	keys := make([]string, 100)
	for i := range keys {
		keys[i] = strings.Repeat("k", 150) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	if parts := relayed(notices.Event{Type: notices.EventRead, Keys: keys}); len(parts) < 2 {
		t.Fatalf("a long read went as %d parts", len(parts))
	} else {
		n := 0
		for _, p := range parts {
			var e notices.Event
			_ = json.Unmarshal(p, &e)
			n += len(e.Keys)
		}
		if n != len(keys) {
			t.Fatalf("the parts carry %d keys of %d", n, len(keys))
		}
	}
}

// pipeWriter is a streaming response a test reads as it is written.
type pipeWriter struct {
	h http.Header
	w *io.PipeWriter
}

func (p *pipeWriter) Header() http.Header         { return p.h }
func (p *pipeWriter) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *pipeWriter) WriteHeader(int)             {}
func (p *pipeWriter) Flush()                      {}

func ioPipe() (*io.PipeReader, *pipeWriter) {
	r, w := io.Pipe()
	return r, &pipeWriter{h: http.Header{}, w: w}
}
