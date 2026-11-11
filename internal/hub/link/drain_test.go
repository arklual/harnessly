package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// serverLinks makes n links over real sockets, entered in a registry as the dialer enters them.
func serverLinks(t *testing.T, n int) (*Registry, []*Link) {
	t.Helper()
	conns := make(chan *websocket.Conn, n)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conns <- c
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	reg := NewRegistry()
	var links []*Link
	for i := range n {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		cancel()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { client.CloseNow() })
		l := New(<-conns, "m"+string(rune('a'+i)), nil)
		reg.Add(l)
		links = append(links, l)
	}
	return reg, links
}

func closedOf(links []*Link) int {
	n := 0
	for _, l := range links {
		if l.Closed() {
			n++
		}
	}
	return n
}

// A drain closes the links one by one with a pause between two closes, each pause near the window's share of a link
// and never the same beat, and tells the agents the hub goes away so they dial again at once.
func TestDrainClosesLinksOneAtATimeWithSpreadPauses(t *testing.T) {
	reg, links := serverLinks(t, 4)
	window := 8 * time.Second
	step := window / 4
	var pauses []time.Duration
	closed := reg.drain(context.Background(), window, func(_ context.Context, d time.Duration) bool {
		pauses = append(pauses, d)
		if got := closedOf(links); got != len(pauses) {
			t.Errorf("before pause %d: %d links closed, want %d", len(pauses), got, len(pauses))
		}
		return true
	})
	if closed != 4 || closedOf(links) != 4 {
		t.Fatalf("closed %d, links closed %d", closed, closedOf(links))
	}
	if len(pauses) != 3 {
		t.Fatalf("pauses: %v", pauses)
	}
	for _, p := range pauses {
		if p < step/2 || p >= step/2+step {
			t.Errorf("pause %v is outside [%v, %v)", p, step/2, step/2+step)
		}
	}
	for _, l := range links {
		if l.CloseCode() != websocket.StatusGoingAway {
			t.Errorf("%s closed with %d", l.MachineID, l.CloseCode())
		}
	}
}

// A drain that runs out of time stops where it is: the rest is left for the stop that follows.
func TestDrainStopsWhenItsTimeIsOver(t *testing.T) {
	reg, links := serverLinks(t, 3)
	closed := reg.drain(context.Background(), 3*time.Second, func(context.Context, time.Duration) bool { return false })
	if closed != 1 || closedOf(links) != 1 {
		t.Errorf("closed %d, links closed %d", closed, closedOf(links))
	}
}

// A dialer that refuses keeps the link it has and answers a new dial 409, which an agent takes as «dial again».
func TestRefuseKeepsLinksAndTurnsDialsAway(t *testing.T) {
	h := newTestHub(t)
	conn, status, body := h.dialRaw(dialOpts{})
	if conn == nil {
		t.Fatalf("dial refused: %d %s", status, body)
	}
	t.Cleanup(func() { conn.CloseNow() })
	var l *Link
	select {
	case l = <-h.connected:
	case <-time.After(3 * time.Second):
		t.Fatal("the hub did not register the link")
	}
	h.dialer.Refuse()
	if l.Closed() {
		t.Error("Refuse ended the link it had")
	}
	if conn, status, _ := h.dialRaw(dialOpts{}); conn != nil || status != http.StatusConflict {
		t.Errorf("a dial while refusing: %d", status)
	}
}
