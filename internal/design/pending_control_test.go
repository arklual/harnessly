package design

import (
	"testing"
	"time"
)

// A command for a design no page shows waits for the next page that opens, once.
func TestQueuedControlReachesTheNextPage(t *testing.T) {
	l := testLibrary(t, t.TempDir())
	if l.HasPages("shop") {
		t.Fatal("no page yet")
	}
	l.QueueControl("shop", Control{Board: "Pay.dc.html", Panel: "show"})
	got := make(chan Control, 2)
	stop := l.OnControl("shop", func(c Control) { got <- c })
	defer stop()
	if !l.HasPages("shop") {
		t.Fatal("the page is there")
	}
	select {
	case c := <-got:
		if c.Board != "Pay.dc.html" || c.Panel != "show" {
			t.Fatalf("got %+v", c)
		}
	case <-time.After(PendingControlDelay + 2*time.Second):
		t.Fatal("the queued command did not come")
	}
	stop2 := l.OnControl("shop", func(c Control) { got <- c })
	defer stop2()
	select {
	case c := <-got:
		t.Fatalf("delivered twice: %+v", c)
	case <-time.After(PendingControlDelay + 300*time.Millisecond):
	}
}
