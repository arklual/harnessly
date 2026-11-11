package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// cardMachine is what a runtime says of itself at GET /api/v2/machines: its agents with their models.
const cardMachine = `[{"id":"local","name":"box","host":"box","reach":"online","lastSeenAt":null,` +
	`"runtime":{"version":"9.9.9","proto":2,"features":["machine"]},` +
	`"agents":[{"provider":"claude","version":"2.1.0","status":"ready","canSwitchModel":true,` +
	`"models":[{"id":"opus","label":"Opus","hint":"","efforts":["low","high"],"defaultEffort":"high"}]}]}]`

// cardUsage is the runtime's own answer to the platform's …/machines: what is left of each agent's subscription.
const cardUsage = `{"machines":[{"id":"local","limits":{"claude":{"fiveHour":{"usedPct":40,"resetsAt":1800000000}}}}]}`

// cardRuntime answers its card and its usage, and hangs on everything while hang is set.
func cardRuntime(name string, hang *atomic.Bool, caps *atomic.Value) http.Handler {
	page := pageRuntime(name)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hang.Load() {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		switch r.URL.Path {
		case "/api/v2/machines":
			caps.Store(r.URL.Query().Get("caps"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, cardMachine)
		case "/agents/api/platform/machines":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, cardUsage)
		default:
			page.ServeHTTP(w, r)
		}
	})
}

type cardItem struct {
	ID     string `json:"id"`
	Reach  string `json:"reach"`
	Hangs  bool   `json:"hangs"`
	Agents []struct {
		Provider string `json:"provider"`
		Models   []struct {
			ID string `json:"id"`
		} `json:"models"`
	} `json:"agents"`
	Usage json.RawMessage `json:"usage"`
}

func cardList(t *testing.T, p *hubtest.Person) map[string]cardItem {
	t.Helper()
	var list []cardItem
	r := p.Do("GET", "/agents/api/v2/machines", nil)
	if r.Code != 200 {
		t.Fatalf("machines: %s", r)
	}
	r.JSON(&list)
	out := map[string]cardItem{}
	for _, m := range list {
		out[m.ID] = m
	}
	return out
}

func waitCard(t *testing.T, p *hubtest.Person, id string, ok func(cardItem) bool) cardItem {
	t.Helper()
	deadline := time.Now().Add(6 * time.Second)
	for {
		m := cardList(t, p)[id]
		if ok(m) {
			return m
		}
		if time.Now().After(deadline) {
			t.Fatalf("the list never came to it: %+v", m)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// the start page drew no model pill and no limits while the machine on top hanged, because the hub listed
// machines without agents and the page knew them only from a live welcome. The hub keeps what each machine last said
// of its agents and of its subscription's limits, and lists it whatever the machine does now; a machine whose runtime
// does not answer is listed as hanging.
func TestFleetListKeepsWhatEachMachineLastSaid(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	first, _ := owner.NewSpace("Команда")
	second, _ := owner.NewSpace("Вторая")
	var hangA, hangB atomic.Bool
	var capsA, capsB atomic.Value
	a := owner.Machine(first, cardRuntime("Acme", &hangA, &capsA))
	hangB.Store(true)
	defer hangB.Store(false)
	b := owner.Machine(second, cardRuntime("Beta", &hangB, &capsB))

	got := waitCard(t, owner, a.MachineID, func(m cardItem) bool { return len(m.Agents) > 0 })
	if got.Agents[0].Provider != "claude" || len(got.Agents[0].Models) != 1 || got.Agents[0].Models[0].ID != "opus" {
		t.Errorf("agents of the machine: %+v", got.Agents)
	}
	if caps, _ := capsA.Load().(string); !strings.Contains(caps, "profiles") {
		t.Errorf("the machine was asked for its agents without the page's caps: %q", caps)
	}
	if got.Hangs {
		t.Errorf("a machine that answers is listed as hanging")
	}
	if len(got.Usage) != 0 && string(got.Usage) != "null" {
		t.Errorf("usage before anyone asked the machine for it: %s", got.Usage)
	}
	waitCard(t, owner, b.MachineID, func(m cardItem) bool { return m.Hangs })

	if r := owner.Do("GET", "/agents/api/platform/machines?machine="+a.MachineID, nil); r.Code != 200 || !strings.Contains(r.String(), "usedPct") {
		t.Fatalf("usage through the hub: %s", r)
	}
	got = cardList(t, owner)[a.MachineID]
	if !strings.Contains(string(got.Usage), `"usedPct":40`) {
		t.Errorf("the usage the machine gave last is not listed: %s", got.Usage)
	}

	// The machine goes off: the list still says what it said last.
	a.Close()
	gone := waitCard(t, owner, a.MachineID, func(m cardItem) bool { return m.Reach == "unreachable" })
	if len(gone.Agents) != 1 || gone.Agents[0].Provider != "claude" {
		t.Errorf("an off machine lost its agents: %+v", gone.Agents)
	}
	if !strings.Contains(string(gone.Usage), `"usedPct":40`) {
		t.Errorf("an off machine lost its usage: %s", gone.Usage)
	}
	if gone.Hangs {
		t.Errorf("an off machine is listed as hanging")
	}
}
