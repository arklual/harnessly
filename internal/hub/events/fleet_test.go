package events

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func woken(s *FleetSub) bool {
	select {
	case <-s.wake:
		return true
	default:
		return false
	}
}

func TestFleetTouchReachesOnlyTheStreamsItConcerns(t *testing.T) {
	b := NewFleetBus()
	a := b.Subscribe("u1", []string{"s1", "s2"})
	other := b.Subscribe("u2", []string{"s3"})

	b.TouchSpace("s1")
	if !woken(a) || woken(other) {
		t.Error("a machine of s1 must wake the stream that watches s1 and nobody else")
	}
	b.TouchSpace("s9")
	if woken(a) || woken(other) {
		t.Error("a space nobody watches woke a stream")
	}
	b.TouchUser("u2")
	if woken(a) || !woken(other) || !b.takeStale(other) || b.takeStale(a) {
		t.Error("a person's change must wake their streams, marked to ask for their spaces again")
	}
	// A burst is one wake-up, and publishing never waits for a page.
	for i := 0; i < 100; i++ {
		b.TouchSpace("s2")
	}
	if !woken(a) || woken(a) {
		t.Error("a burst must be one wake-up")
	}

	b.Unsubscribe(a)
	if b.Count("u1") != 0 || b.Count("u2") != 1 {
		t.Errorf("streams: u1 %d, u2 %d", b.Count("u1"), b.Count("u2"))
	}
	b.Close()
	select {
	case <-other.Done():
	default:
		t.Error("Close left a stream open")
	}
	if b.Subscribe("u1", nil) != nil {
		t.Error("a closed bus took a stream")
	}
}

func TestBrokerTapHearsEveryEvent(t *testing.T) {
	b := NewBroker()
	var got []string
	b.SetTap(func(spaceID string, e Event) { got = append(got, spaceID+" "+e.Name) })
	b.PublishJSON("s1", "machine", struct{}{}, false)
	b.PublishMachine("s2", "m1", "machineRemoved", struct{}{}, true)
	if len(got) != 2 || got[0] != "s1 machine" || got[1] != "s2 machineRemoved" {
		t.Errorf("tap heard %q", got)
	}
}

// fleetStream serves one stream over HTTP and returns its lines.
func fleetStream(t *testing.T, b *FleetBus, s *FleetSub, spaces func(context.Context) ([]string, error)) (*bufio.Reader, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { b.Serve(w, r, s, spaces) }))
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return bufio.NewReader(res.Body), func() {
		cancel()
		res.Body.Close()
		srv.Close()
	}
}

func awaitLine(t *testing.T, r *bufio.Reader, want string) {
	t.Helper()
	found := make(chan error, 1)
	go func() {
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				found <- err
				return
			}
			if strings.TrimSpace(line) == want {
				found <- nil
				return
			}
		}
	}()
	select {
	case err := <-found:
		if err != nil {
			t.Fatalf("the stream ended before %q: %v", want, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("no %q on the stream", want)
	}
}

func TestFleetStreamTellsThePageAndAsksForTheSpacesAgain(t *testing.T) {
	old := FleetSettle
	FleetSettle = time.Millisecond
	defer func() { FleetSettle = old }()

	b := NewFleetBus()
	s := b.Subscribe("u1", []string{"s1"})
	var asked atomic.Int32
	r, stop := fleetStream(t, b, s, func(context.Context) ([]string, error) {
		asked.Add(1)
		return []string{"s1", "s2"}, nil
	})
	defer stop()
	awaitLine(t, r, ": ok")

	b.TouchSpace("s1")
	awaitLine(t, r, "event: fleet")
	if asked.Load() != 0 {
		t.Error("a machine's change asked for the person's spaces")
	}

	// The person joined s2: the stream learns it and from then on hears of the machines of s2.
	b.TouchUser("u1")
	awaitLine(t, r, "event: fleet")
	if asked.Load() != 1 {
		t.Errorf("spaces asked %d times, want once", asked.Load())
	}
	b.TouchSpace("s2")
	awaitLine(t, r, "event: fleet")
}

func TestFleetStreamEndsWhenTheSpacesCannotBeRead(t *testing.T) {
	old := FleetSettle
	FleetSettle = time.Millisecond
	defer func() { FleetSettle = old }()

	b := NewFleetBus()
	s := b.Subscribe("u1", nil)
	r, stop := fleetStream(t, b, s, func(context.Context) ([]string, error) { return nil, errors.New("gone") })
	defer stop()
	awaitLine(t, r, ": ok")
	b.TouchUser("u1")
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the stream stayed open")
	}
	if b.Count("u1") != 0 {
		t.Error("the stream is still counted")
	}
}
