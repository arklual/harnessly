package proxy_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/events"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// fleetStream is an open GET /api/v2/machines/events of a test person: every `fleet` event is one tick.
type fleetStream struct {
	ticks chan struct{}
}

func openFleetStream(t *testing.T, p *hubtest.Person, path string) *fleetStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", p.S.URL+path, nil)
	res, err := p.Client.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); res.Body.Close() })
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("the fleet stream: %d %v", res.StatusCode, res.Header)
	}
	s := &fleetStream{ticks: make(chan struct{}, 64)}
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "event: fleet" {
				s.ticks <- struct{}{}
			}
		}
	}()
	return s
}

func (s *fleetStream) told() bool {
	select {
	case <-s.ticks:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// quiet drains what is on its way and says whether nothing was.
func (s *fleetStream) quiet() bool {
	select {
	case <-s.ticks:
		return false
	case <-time.After(300 * time.Millisecond):
		return true
	}
}

func waitFleetStreams(t *testing.T, bus *events.FleetBus, userID string, n int) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if bus.Count(userID) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%d fleet streams open, want %d", bus.Count(userID), n)
}

func settleFast(t *testing.T) {
	t.Helper()
	old := events.FleetSettle
	events.FleetSettle = 5 * time.Millisecond
	t.Cleanup(func() { events.FleetSettle = old })
}

func TestFleetStreamIsDarkWithoutTheFlagAndForNobody(t *testing.T) {
	f := setup(t)
	if r := f.owner.Do("GET", "/api/v2/machines/events", nil); r.Code != http.StatusNotFound {
		t.Errorf("without the flag: %s", r)
	}
	g := setup(t, fleetOn)
	if r := g.s.Browser().Do("GET", "/api/v2/machines/events", nil); r.Code != http.StatusUnauthorized {
		t.Errorf("nobody signed in: %s", r)
	}
	if r := g.owner.Do("POST", "/api/v2/machines/events", map[string]any{}); r.Code != http.StatusNotFound {
		t.Errorf("a POST: %s", r)
	}
}

func TestFleetStreamTellsOfMachinesAddedRenamedAndRemoved(t *testing.T) {
	settleFast(t)
	f := setup(t, fleetOn)
	bus := f.s.Hub.Fleet
	stranger := f.s.SignIn("stranger@example.com")
	stranger.NewSpace("Чужая")

	mine := openFleetStream(t, f.owner, "/api/v2/machines/events")
	theirs := openFleetStream(t, stranger, "/api/v2/machines/events")
	waitFleetStreams(t, bus, f.owner.ID, 1)
	waitFleetStreams(t, bus, stranger.ID, 1)

	// A machine registers in the owner's space.
	a := f.s.NewAgent(nil)
	if r := a.Register(f.owner.NewCode(f.spaceID, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatal(r)
	}
	if !mine.told() {
		t.Fatal("a new machine: the page was not told")
	}
	for !mine.quiet() {
	}

	// It is renamed, then removed.
	if r := f.owner.Do("PATCH", "/api/machines/"+a.MachineID, map[string]any{"name": "Сборочная"}); r.Code >= 300 {
		t.Fatal(r)
	}
	if !mine.told() {
		t.Fatal("a renamed machine: the page was not told")
	}
	for !mine.quiet() {
	}
	if r := f.owner.Do("DELETE", "/api/machines/"+a.MachineID, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	if !mine.told() {
		t.Fatal("a removed machine: the page was not told")
	}

	// Nothing of it reached a person with no part in the space.
	if !theirs.quiet() {
		t.Error("a stranger's page was told of another space's machines")
	}
}

func TestFleetStreamFollowsThePersonIntoAndOutOfASpace(t *testing.T) {
	settleFast(t)
	f := setup(t, fleetOn)
	bus := f.s.Hub.Fleet
	guest := f.s.SignIn("guest@example.com")
	guest.NewSpace("Своя")
	page := openFleetStream(t, guest, "/api/v2/machines/events")
	waitFleetStreams(t, bus, guest.ID, 1)

	// Joining a space tells the page, and from then on the machines of that space do.
	f.owner.Join(f.spaceID, guest)
	if !page.told() {
		t.Fatal("the person joined a space: their page was not told")
	}
	for !page.quiet() {
	}
	if r := f.owner.Do("PATCH", "/api/machines/"+f.id, map[string]any{"name": "Общая"}); r.Code >= 300 {
		t.Fatal(r)
	}
	if !page.told() {
		t.Fatal("a machine of the space the person joined: their page was not told")
	}
	for !page.quiet() {
	}

	// Removed from the space: the page is told (its list no longer has the machines), and hears no more of them.
	if r := f.owner.Do("DELETE", "/api/spaces/"+f.spaceID+"/members/"+guest.ID, nil); r.Code != http.StatusNoContent {
		t.Fatal(r)
	}
	if !page.told() {
		t.Fatal("the person was removed from a space: their page was not told")
	}
	for !page.quiet() {
	}
	if r := f.owner.Do("PATCH", "/api/machines/"+f.id, map[string]any{"name": "Закрытая"}); r.Code >= 300 {
		t.Fatal(r)
	}
	if !page.quiet() {
		t.Error("a removed member's page still hears of the space's machines")
	}
}
