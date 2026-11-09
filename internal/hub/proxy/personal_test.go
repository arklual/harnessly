package proxy_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// #57: the person's list holds the chats of their own machines only. A machine they reach as an administrator is named
// apart and opened on purpose; its chats never join their list, search or sidebar. Giving the machine to the person it
// was added for keeps its id and moves its chats with it.
func TestAdministeredMachinesStayOutOfThePersonalList(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	admin := s.SignIn("admin@example.com")
	space, _ := admin.NewSpace("Команда")
	alice := s.SignIn("alice@example.com")
	bob := s.SignIn("bob@example.com")
	admin.Join(space, alice)
	admin.Join(space, bob)

	machine := func(name, conv, title string) *hubtest.Agent {
		t.Helper()
		a := s.NewAgent(pageRuntime(name))
		a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureHistory)
		if r := a.Register(admin.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
			t.Fatalf("register: %s", r)
		}
		a.Connect()
		waitFor(t, func() bool { return len(a.Calls(hubproto.OpHistoryStart)) >= 1 })
		emit(a, conv, title, 1)
		return a
	}
	// The administrator adds both machines; each is for one employee.
	am := machine("Alice", "tab-a", "Отчёт Алисы")
	bm := machine("Bob", "tab-b", "Отчёт Боба")
	waitFor(t, func() bool { return len(listOf(t, admin).Conversations) == 2 })

	for _, give := range []struct {
		m  *hubtest.Agent
		to *hubtest.Person
	}{{am, alice}, {bm, bob}} {
		r := admin.Do("POST", "/api/machines/"+give.m.MachineID+"/owner", map[string]any{"userId": give.to.ID})
		if r.Code != http.StatusOK {
			t.Fatalf("assign: %s", r)
		}
		var out struct{ Machine struct{ ID, OwnerID string } }
		if r.JSON(&out); out.Machine.ID != give.m.MachineID || out.Machine.OwnerID != give.to.ID {
			t.Errorf("assigned %+v", out.Machine)
		}
		// The link dials again and is read with its new owner; the id and key are the same.
		starts := len(give.m.Calls(hubproto.OpHistoryStart))
		give.m.WaitClosed()
		give.m.Connect()
		waitFor(t, func() bool { return len(give.m.Calls(hubproto.OpHistoryStart)) > starts })
	}
	// Only a space owner gives a machine away, and only to a member of its space.
	if r := alice.Do("POST", "/api/machines/"+bm.MachineID+"/owner", map[string]any{"userId": alice.ID}); r.Code == http.StatusOK {
		t.Errorf("a member took a colleague's machine: %s", r)
	}
	stranger := s.SignIn("stranger@example.com")
	if r := admin.Do("POST", "/api/machines/"+bm.MachineID+"/owner", map[string]any{"userId": stranger.ID}); r.Code == http.StatusOK {
		t.Errorf("a machine given outside its space: %s", r)
	}

	// What comes after the assignment is the new owner's too.
	emit(bm, "tab-b2", "Новый чат Боба", 2)
	// The machine stays as trusted as the administrator who connected it made it: the employee opens it on the hub.
	waitFor(t, func() bool { return len(listOf(t, bob).Conversations) == 2 })

	// Each employee sees their own machine and chats, and nothing of the colleague's.
	for _, c := range []struct {
		p     *hubtest.Person
		mine  string
		other string
		chats []string
	}{{alice, am.MachineID, bm.MachineID, []string{"tab-a"}}, {bob, bm.MachineID, am.MachineID, []string{"tab-b", "tab-b2"}}} {
		l := listOf(t, c.p)
		if !l.Enabled || len(l.Machines) != 1 || l.Machines[0].ID != c.mine {
			t.Errorf("%s: machines %+v", c.p.Email, l.Machines)
		}
		var got []string
		for _, x := range l.Conversations {
			got = append(got, x.ID)
		}
		slices.Sort(got)
		if !slices.Equal(got, c.chats) {
			t.Errorf("%s: chats %v, want %v", c.p.Email, got, c.chats)
		}
	}
	// The administrator keeps both machines in the inventory, marked as not theirs, and none of their chats.
	l := listOf(t, admin)
	if l.Enabled || len(l.Machines) != 0 || len(l.Others) != 2 || len(l.Conversations) != 0 {
		t.Errorf("admin's list %+v", l)
	}
	var fleet []struct {
		ID   string
		Mine bool
	}
	if r := admin.Do("GET", "/api/v2/machines", nil); r.Code != 200 {
		t.Fatalf("fleet: %s", r)
	} else if r.JSON(&fleet); len(fleet) != 2 || fleet[0].Mine || fleet[1].Mine {
		t.Errorf("admin's fleet %+v", fleet)
	}
	if r := admin.Do("GET", "/agents/api/acme/history/conversations/tab-a", nil); r.Code == 200 {
		t.Errorf("the admin reads an employee's chat from the copy: %s", r)
	}
	if r := admin.Do("GET", "/agents/api/acme/history/search?q=%D0%9E%D1%82%D1%87%D1%91%D1%82", nil); r.Code == 200 && len(r.String()) > 0 {
		var found struct{ Hits []json.RawMessage }
		if r.JSON(&found); len(found.Hits) != 0 {
			t.Errorf("the admin finds employees' chats: %s", r)
		}
	}
	// The sidebar document is a person's own machines' (it stays off for one who has none).
	var side struct{ Enabled bool }
	if r := admin.Do("GET", "/agents/api/acme/history/sidebar", nil); r.Code != 200 {
		t.Fatalf("sidebar: %s", r)
	} else if r.JSON(&side); side.Enabled {
		t.Errorf("admin's sidebar is on with no machine of their own")
	}
	if r := alice.Do("GET", "/agents/api/acme/history/sidebar", nil); r.Code != 200 {
		t.Fatalf("sidebar: %s", r)
	} else if r.JSON(&side); !side.Enabled {
		t.Errorf("alice's sidebar is off")
	}
	// Opening a colleague's machine on purpose still works for the administrator.
	if r := admin.Do("GET", "/api/machines/"+am.MachineID, nil); r.Code != 200 {
		t.Errorf("admin's inventory detail: %s", r)
	}
}

type personalList struct {
	Enabled       bool `json:"enabled"`
	Machines      []struct{ ID, Name, State string }
	Others        []struct{ ID, Name, State string }
	Conversations []struct{ ID, Title, MachineID string }
}

func listOf(t *testing.T, p *hubtest.Person) personalList {
	t.Helper()
	var l personalList
	r := p.Do("GET", "/agents/api/acme/history/conversations", nil)
	if r.Code != 200 {
		t.Fatalf("list: %s", r)
	}
	r.JSON(&l)
	return l
}

func emit(a *hubtest.Agent, conv, title string, seq int) {
	c, _ := json.Marshal(map[string]any{"id": conv, "rev": 1, "title": title, "lastPromptAt": 5, "machineId": "local"})
	a.Emit(map[string]any{"event": hubproto.EventHistoryEvents, "ch": "convs", "epoch": "ep-1", "fromSeq": seq,
		"events": []json.RawMessage{json.RawMessage(`{"type":"conversation_upsert","conv":` + string(c) + `}`)}})
}
