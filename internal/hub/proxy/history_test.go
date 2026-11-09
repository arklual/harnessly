package proxy_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// a machine whose agent offers the chats is told where the hub's copy stands and subscribed to them; what it
// sends is stored under the person who added it, and the workspace reads the list and a chat from the hub — with the
// machine off as well — instead of from the home machine.
func TestWorkspaceReadsTheChatsFromTheHubsCopy(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	a := s.NewAgent(pageRuntime("Acme"))
	a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureHistory)
	if r := a.Register(owner.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	waitFor(t, func() bool { return len(a.Calls(hubproto.OpHistoryStart)) == 1 })
	if c := a.Calls(hubproto.OpHistoryStart)[0]; string(c.Params["enabled"]) != "true" || c.Params["pending"] == nil {
		t.Errorf("history.start %v", c.Params)
	}
	subscribed := false
	for _, c := range a.Calls(hubproto.OpLinkSubscribe) {
		subscribed = subscribed || strings.Contains(string(c.Params["events"]), hubproto.EventHistoryEvents)
	}
	if !subscribed {
		t.Fatal("the machine was not subscribed to its chats")
	}

	conv := `{"id":"tab-a","rev":1,"title":"Отчёт","lastPromptAt":5,"machineId":"local"}`
	a.Emit(map[string]any{"event": hubproto.EventHistoryEvents, "ch": "convs", "epoch": "ep-1", "fromSeq": 1,
		"events": []json.RawMessage{json.RawMessage(`{"type":"conversation_upsert","conv":` + conv + `}`)}})
	a.Emit(map[string]any{"event": hubproto.EventHistoryBackfill, "ch": "conv:tab-a", "events": []json.RawMessage{json.RawMessage(
		`{"type":"feed_reset","turns":[{"id":"c:t1#0","startedAt":1,"endedAt":2,"status":"done"}],` +
			`"entries":[{"id":"c:t1#0","turnId":"c:t1#0","at":1,"t":"user","text":"привет"}],"beforeTurnId":null,"history":{"from":""}}`)}})
	waitFor(t, func() bool { return len(a.Calls(hubproto.OpHistoryAck)) == 1 })
	if c := a.Calls(hubproto.OpHistoryAck)[0]; string(c.Params["ackSeq"]) != "1" || c.Str("ch") != "convs" {
		t.Errorf("history.ack %v", c.Params)
	}

	read := func() (list struct {
		Enabled       bool `json:"enabled"`
		Machines      []struct{ ID, Name, State string }
		Conversations []struct{ ID, Title, MachineID string }
	}, chat struct {
		Machine      string            `json:"machine"`
		MachineID    string            `json:"machineId"`
		Turns        []json.RawMessage `json:"turns"`
		Entries      []json.RawMessage `json:"entries"`
		BeforeTurnID *string           `json:"beforeTurnId"`
	}) {
		t.Helper()
		var r hubtest.Resp
		waitFor(t, func() bool {
			r = owner.Do("GET", "/agents/api/acme/history/conversations", nil)
			r.JSON(&list)
			return r.Code == 200 && len(list.Conversations) == 1
		})
		if r := owner.Do("GET", "/agents/api/acme/history/conversations/tab-a?limit=33", nil); r.Code != 200 {
			t.Fatalf("chat: %s", r)
		} else {
			r.JSON(&chat)
		}
		return list, chat
	}
	list, chat := read()
	if !list.Enabled || len(list.Machines) != 1 || list.Machines[0].State != "online" {
		t.Errorf("list %+v", list)
	}
	if c := list.Conversations[0]; c.ID != "tab-a" || c.Title != "Отчёт" || c.MachineID != a.MachineID {
		t.Errorf("chat in the list %+v", c)
	}
	if chat.MachineID != a.MachineID || len(chat.Turns) != 1 || len(chat.Entries) != 1 || chat.BeforeTurnID != nil {
		t.Errorf("chat %+v", chat)
	}
	// The machine goes off: the chat is still there, from the hub.
	a.Close()
	waitFor(t, func() bool {
		l, _ := read()
		return len(l.Machines) == 1 && l.Machines[0].State == "offline"
	})
	if _, chat := read(); len(chat.Turns) != 1 || chat.Machine != "offline" {
		t.Errorf("with the machine off %+v", chat)
	}
	// The search and the sidebar answer from the hub too.
	var found struct {
		Hits []struct{ ConvID, EntryID, Role string }
	}
	if r := owner.Do("GET", "/agents/api/acme/history/search?q=%D0%BF%D1%80%D0%B8%D0%B2&machine=local", nil); r.Code != 200 {
		t.Fatalf("search: %s", r)
	} else if r.JSON(&found); len(found.Hits) != 1 || found.Hits[0].EntryID != "c:t1#0" || found.Hits[0].Role != "user" {
		t.Errorf("search %+v", found)
	}
	doc := map[string]any{"projects": []any{}, "favorites": map[string]any{"groups": []any{}, "loose": []any{"tab-a"}}, "merged": []any{}}
	if r := owner.Do("PUT", "/agents/api/acme/history/sidebar", map[string]any{"baseRev": 0, "doc": doc}); r.Code != 200 {
		t.Fatalf("sidebar write: %s", r)
	}
	if r := owner.Do("PUT", "/agents/api/acme/history/sidebar", map[string]any{"baseRev": 0, "doc": doc}); r.Code != http.StatusConflict {
		t.Errorf("sidebar over a newer one: %s", r)
	}
	var side struct {
		Enabled bool
		Rev     int
		Doc     struct{ Favorites struct{ Loose []string } }
	}
	if r := owner.Do("GET", "/agents/api/acme/history/sidebar", nil); r.Code != 200 {
		t.Fatalf("sidebar: %s", r)
	} else if r.JSON(&side); !side.Enabled || side.Rev != 1 || len(side.Doc.Favorites.Loose) != 1 {
		t.Errorf("sidebar %+v", side)
	}
	if r := owner.Do("GET", "/agents/api/acme/history/conversations/tab-a/rewinds/0/turns", nil); r.Code != 404 {
		t.Errorf("a rewind the chat has not: %s", r)
	}
	// Somebody else reads nothing of it.
	stranger := s.SignIn("stranger@example.com")
	if r := stranger.Do("GET", "/agents/api/acme/history/conversations/tab-a", nil); r.Code == 200 {
		t.Errorf("a stranger reads the chat: %s", r)
	}
	if r := stranger.Do("GET", "/agents/api/acme/history/search?q=%D0%BF%D1%80%D0%B8%D0%B2", nil); r.Code == 200 && strings.Contains(r.String(), "c:t1#0") {
		t.Errorf("a stranger finds the chat: %s", r)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
