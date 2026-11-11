package proxy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
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
	// The person's review remarks on the chat are kept by the hub and read back; nobody else reads them.
	note := map[string]any{"id": "n1", "path": "a.go", "side": "after", "from": 3, "to": 4, "lines": "x", "text": "rename",
		"state": "draft"}
	if r := owner.Do("PUT", "/agents/api/acme/history/conversations/tab-a/review-notes/n1", note); r.Code != 200 {
		t.Fatalf("note write: %s", r)
	}
	if r := owner.Do("PUT", "/agents/api/acme/history/conversations/tab-a/review-notes/n2", note); r.Code != 400 {
		t.Errorf("a note under another id: %s", r)
	}
	var notes struct{ Notes []struct{ ID, Text string } }
	if r := owner.Do("GET", "/agents/api/acme/history/conversations/tab-a/review-notes", nil); r.Code != 200 {
		t.Fatalf("notes: %s", r)
	} else if r.JSON(&notes); len(notes.Notes) != 1 || notes.Notes[0].Text != "rename" {
		t.Errorf("notes %+v", notes)
	}
	// Somebody else reads nothing of it.
	stranger := s.SignIn("stranger@example.com")
	if r := stranger.Do("GET", "/agents/api/acme/history/conversations/tab-a/review-notes", nil); r.Code == 200 {
		var theirs struct{ Notes []any }
		if r.JSON(&theirs); len(theirs.Notes) != 0 {
			t.Errorf("a stranger reads the notes: %s", r)
		}
	}
	if r := owner.Do("DELETE", "/agents/api/acme/history/conversations/tab-a/review-notes/n1", nil); r.Code != 204 {
		t.Errorf("note delete: %s", r)
	}
	if r := stranger.Do("GET", "/agents/api/acme/history/conversations/tab-a", nil); r.Code == 200 {
		t.Errorf("a stranger reads the chat: %s", r)
	}
	if r := stranger.Do("GET", "/agents/api/acme/history/search?q=%D0%BF%D1%80%D0%B8%D0%B2", nil); r.Code == 200 && strings.Contains(r.String(), "c:t1#0") {
		t.Errorf("a stranger finds the chat: %s", r)
	}
}

// A failure to list the person's machines is a failure, not «the copy is off»: answered as off, the page
// swapped the person's sidebar for a machine's own and back with the next answer, and the favorites jumped.
func TestAFailureToListTheMachinesIsNotTheCopyOff(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	a := s.NewAgent(pageRuntime("Acme"))
	a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureHistory)
	if r := a.Register(owner.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	var list struct{ Enabled bool }
	if r := owner.Do("GET", "/agents/api/acme/history/conversations", nil); r.Code != 200 {
		t.Fatalf("list: %s", r)
	} else if r.JSON(&list); !list.Enabled {
		t.Fatalf("the copy is off before the failure: %s", r)
	}
	if _, err := s.DB.Exec(context.Background(), `ALTER TABLE machines RENAME TO machines_away`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.Exec(context.Background(), `ALTER TABLE machines_away RENAME TO machines`) })
	for _, path := range []string{"/agents/api/acme/history/conversations", "/agents/api/acme/history/sidebar"} {
		r := owner.Do("GET", path, nil)
		if r.Code < 500 {
			t.Errorf("%s with the machines unreadable: %d %s, want a failure, not «off»", path, r.Code, r)
		}
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

// A list that has not changed is answered by its tag alone, without reading the chats; a chat that changes, or a
// machine that goes off, makes the next answer a full one with a new tag.
func TestAnUnchangedChatListIsAnsweredByItsTag(t *testing.T) {
	reads := proxy.CountChatListReads(t)
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	a := s.NewAgent(pageRuntime("Acme"))
	a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureHistory)
	if r := a.Register(owner.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	upsert := func(seq, rev int) {
		t.Helper()
		conv := `{"id":"tab-a","rev":` + strconv.Itoa(rev) + `,"title":"Отчёт","lastPromptAt":5,"machineId":"local"}`
		a.Emit(map[string]any{"event": hubproto.EventHistoryEvents, "ch": "convs", "epoch": "ep-1", "fromSeq": seq,
			"events": []json.RawMessage{json.RawMessage(`{"type":"conversation_upsert","conv":` + conv + `}`)}})
		waitFor(t, func() bool { return len(a.Calls(hubproto.OpHistoryAck)) == seq })
	}
	const list = "/agents/api/acme/history/conversations"
	upsert(1, 1)

	first := owner.Do("GET", list, nil)
	tag := first.Header.Get("ETag")
	if first.Code != 200 || !strings.HasPrefix(tag, `W/"`) || !strings.Contains(first.String(), "tab-a") {
		t.Fatalf("first: %s, tag %q", first, tag)
	}
	before := reads.Load()
	// A proxy that compresses may hand the tag back among others; the comparison is weak.
	for _, sent := range []string{tag, strings.TrimPrefix(tag, "W/"), `"other", ` + tag} {
		r := owner.Do("GET", list, nil, "If-None-Match", sent)
		if r.Code != http.StatusNotModified || len(r.Body) != 0 || r.Header.Get("ETag") != tag {
			t.Errorf("If-None-Match %s: %d, %d bytes, tag %q", sent, r.Code, len(r.Body), r.Header.Get("ETag"))
		}
	}
	if got := reads.Load(); got != before {
		t.Errorf("an unchanged list read the chats %d times", got-before)
	}
	if r := owner.Do("GET", list, nil, "If-None-Match", `W/"stale"`); r.Code != 200 || r.Header.Get("ETag") != tag {
		t.Errorf("a stale tag: %d %q", r.Code, r.Header.Get("ETag"))
	}

	upsert(2, 2)
	changed := owner.Do("GET", list, nil, "If-None-Match", tag)
	if changed.Code != 200 || changed.Header.Get("ETag") == tag || !strings.Contains(changed.String(), `"rev":2`) {
		t.Fatalf("after a change: %s, tag %q", changed, changed.Header.Get("ETag"))
	}
	tag = changed.Header.Get("ETag")

	// The answer names which machines are on: a machine that goes off is a change too.
	a.Close()
	waitFor(t, func() bool {
		r := owner.Do("GET", list, nil, "If-None-Match", tag)
		return r.Code == 200 && strings.Contains(r.String(), `"offline"`) && r.Header.Get("ETag") != tag
	})
}

// a machine's own page (/m/<id>/…) asks the hub for the chats and for where a chat went, under the machine's
// base. The runtime beside it has neither route; on a clean installation (a hub and one machine, no gateway) that was a
// 404 on the list, the sidebar and the move status, and the chats of the hub were never read there.
func TestMachinePageReadsTheChatsFromTheHub(t *testing.T) {
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
	a.Emit(map[string]any{"event": hubproto.EventHistoryEvents, "ch": "convs", "epoch": "ep-1", "fromSeq": 1,
		"events": []json.RawMessage{json.RawMessage(`{"type":"conversation_upsert","conv":{"id":"tab-a","rev":1,"title":"Отчёт","lastPromptAt":5,"machineId":"local"}}`)}})

	base := "/m/" + a.MachineID
	var list struct {
		Enabled       bool `json:"enabled"`
		Conversations []struct{ ID string }
	}
	waitFor(t, func() bool {
		r := owner.Do("GET", base+"/api/platform/history/conversations", nil)
		list.Conversations = nil
		r.JSON(&list)
		return r.Code == 200 && len(list.Conversations) == 1
	})
	if !list.Enabled || list.Conversations[0].ID != "tab-a" {
		t.Errorf("list %+v", list)
	}
	if r := owner.Do("GET", base+"/api/platform/history/sidebar", nil); r.Code != 200 {
		t.Errorf("sidebar: %s", r)
	}
	// The status of a move is the hub's too, not the runtime's 404.
	if r := owner.Do("GET", base+"/api/v2/chat-move/status?machine="+a.MachineID+"&conv=tab-a", nil); r.Code == 404 {
		t.Errorf("chat-move status: %s", r)
	}
}
