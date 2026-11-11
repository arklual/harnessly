package proxy_test

import (
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
)

// staleFixture is a machine whose lists can be made to hang, and a hub where the person may turn the switch on.
type staleFixture struct {
	*fixture
	hang atomic.Bool
	hits atomic.Int64
}

func staleSetup(t *testing.T) *staleFixture {
	t.Helper()
	f := &staleFixture{}
	mux := http.NewServeMux()
	list := func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if f.hang.Load() {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"chats":["one","two"]}`)
	}
	mux.HandleFunc("/m/{id}/api/v2/conversations", list)
	mux.HandleFunc("/m/{id}/api/v2/agents", list)
	mux.HandleFunc("/m/{id}/api/v2/sessions", list) // not a list the hub keeps
	base := &fixture{streamEnded: make(chan struct{}, 4)}
	base.s = hubtest.New(t)
	base.owner = base.s.SignIn("owner@example.com")
	base.spaceID, _ = base.owner.NewSpace("Команда")
	base.a = base.owner.Machine(base.spaceID, mux)
	base.id = base.a.MachineID
	f.fixture = base
	t.Cleanup(proxy.SetStale(time.Minute, 300*time.Millisecond))
	t.Cleanup(proxy.SetHeaderWait(300 * time.Millisecond))
	return f
}

func (f *staleFixture) switchOn(t *testing.T, person string) {
	t.Helper()
	body := map[string]any{"feature": "fleet-stale", "enabled": true}
	if person != "" {
		body["person"] = person
	}
	if r := f.owner.Do("PUT", "/api/admin/switches", body); r.Code != 204 {
		t.Fatalf("switch: %s", r)
	}
}

func (f *staleFixture) list(p *hubtest.Person, path string) hubtest.Resp {
	return p.Do("GET", "/m/"+f.id+"/api/v2/"+path, nil)
}

func TestStaleListAnswersForASilentMachine(t *testing.T) {
	f := staleSetup(t)
	f.switchOn(t, "owner@example.com")

	r := f.list(f.owner, "conversations")
	if r.Code != 200 || r.Header.Get(proxy.StaleHeader) != "" {
		t.Fatalf("a live answer: %s %v", r, r.Header)
	}
	f.hang.Store(true)
	start := time.Now()
	r = f.list(f.owner, "conversations")
	if r.Code != 200 || r.Header.Get(proxy.StaleHeader) == "" || !strings.Contains(string(r.Body), `"one"`) {
		t.Fatalf("a silent machine: %s %v", r, r.Header)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the page waited %s", time.Since(start))
	}
	// Marked silent: the next read does not wait for the machine again.
	hits := f.hits.Load()
	start = time.Now()
	r = f.list(f.owner, "conversations")
	if r.Header.Get(proxy.StaleHeader) == "" || f.hits.Load() != hits || time.Since(start) > 200*time.Millisecond {
		t.Errorf("second read: hits %d→%d in %s %v", hits, f.hits.Load(), time.Since(start), r.Header)
	}
	// The machine is back: the live answer returns once the mark ends.
	f.hang.Store(false)
	f.s.Clock.Advance(2 * time.Minute)
	r = f.list(f.owner, "conversations")
	if r.Code != 200 || r.Header.Get(proxy.StaleHeader) != "" || f.hits.Load() == hits {
		t.Errorf("after the mark: %s %v", r, r.Header)
	}
}

func TestStaleListIsOffUnlessSwitched(t *testing.T) {
	f := staleSetup(t)
	f.list(f.owner, "conversations")
	f.hang.Store(true)
	r := f.list(f.owner, "conversations")
	if r.Header.Get(proxy.StaleHeader) != "" || r.Code == 200 {
		t.Errorf("without the switch the hub answered from a copy: %s %v", r, r.Header)
	}
}

func TestStaleCopyIsNotShownToAnotherPerson(t *testing.T) {
	f := staleSetup(t)
	f.switchOn(t, "")
	member := f.s.SignIn("member@example.com")
	f.owner.Join(f.spaceID, member)
	f.list(f.owner, "conversations")
	f.hang.Store(true)
	if r := f.list(member, "conversations"); r.Header.Get(proxy.StaleHeader) != "" || strings.Contains(string(r.Body), `"one"`) {
		t.Errorf("the owner's copy reached another person: %s %v", r, r.Header)
	}
	if r := f.list(f.owner, "conversations"); r.Header.Get(proxy.StaleHeader) == "" {
		t.Errorf("the owner got no copy: %s", r)
	}
}

func TestStaleOnlyForKnownLists(t *testing.T) {
	f := staleSetup(t)
	f.switchOn(t, "")
	f.list(f.owner, "sessions")
	f.hang.Store(true)
	if r := f.list(f.owner, "sessions"); r.Header.Get(proxy.StaleHeader) != "" {
		t.Errorf("a path the hub keeps no copy of was answered from one: %s", r)
	}
	if r := f.owner.Do("POST", "/m/"+f.id+"/api/v2/conversations", map[string]any{}); r.Header.Get(proxy.StaleHeader) != "" {
		t.Errorf("a write was answered from a copy: %s", r)
	}
}

func TestStaleWithoutACopyIsTheOrdinaryAnswer(t *testing.T) {
	f := staleSetup(t)
	f.switchOn(t, "")
	f.hang.Store(true)
	if r := f.list(f.owner, "agents"); r.Header.Get(proxy.StaleHeader) != "" || r.Code == 200 {
		t.Errorf("no copy yet: %s %v", r, r.Header)
	}
}
