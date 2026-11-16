package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hub/history"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func historyPost(t *testing.T, s *hubtest.Stack, a *hubtest.Agent, in hubproto.HistoryRead) hubtest.Resp {
	t.Helper()
	host, _ := hubproto.DialHost(s.Cfg.PublicURL)
	auth, _ := hubproto.SignHistory(a.Priv, a.MachineID, s.Clock.Now(), host)
	return s.Browser().Do(http.MethodPost, hubproto.HistoryReadPath, in, "Authorization", auth)
}

// A machine reads the copied chat of another machine of the same person after that source went away; another person cannot.
func TestAgentReadsOwnCentralHistoryAcrossMachines(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_HISTORY", "on"))
	owner, stranger := s.SignIn("owner@example.com"), s.SignIn("stranger@example.com")
	space, _ := owner.NewSpace("Acme")
	owner.Join(space, stranger)
	a, b := owner.Machine(space, nil), owner.Machine(space, nil)
	if err := s.DB.S().SetRole(context.Background(), space, stranger.ID, "owner"); err != nil {
		t.Fatal(err)
	}
	other := stranger.Machine(space, nil)
	conv := json.RawMessage(`{"type":"conversation_upsert","conv":{"id":"tab-cross","rev":1,"title":"Copied","lastPromptAt":1,"state":{"k":"idle"}}}`)
	if _, err := history.ApplyFrame(context.Background(), s.DB, history.Owner{MachineID: a.MachineID, UserID: owner.ID}, hubproto.HistoryEvents{Ch: "convs", Epoch: "e", FromSeq: 1, Events: []json.RawMessage{conv}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	turn := json.RawMessage(`{"type":"feed_reset","turns":[{"id":"c:t#0","startedAt":1,"endedAt":2,"status":"done"}],"entries":[{"id":"c:t#0","turnId":"c:t#0","at":1,"t":"user","text":"from source"}],"beforeTurnId":null,"history":{"from":""}}`)
	if err := history.ApplyBackfill(context.Background(), s.DB, history.Owner{MachineID: a.MachineID, UserID: owner.ID}, hubproto.HistoryBackfill{Ch: "conv:tab-cross", Events: []json.RawMessage{turn}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	a.Close()
	r := historyPost(t, s, b, hubproto.HistoryRead{Conversation: "tab-cross", Limit: 100})
	if r.Code != http.StatusOK {
		t.Fatalf("other own machine: %s", r)
	}
	var page hubproto.HistoryPage
	r.JSON(&page)
	if page.MachineID != a.MachineID || len(page.Entries) != 1 || page.Complete != true {
		t.Fatalf("page: %+v", page)
	}
	if r := historyPost(t, s, other, hubproto.HistoryRead{Conversation: "tab-cross", Limit: 100}); r.Code != http.StatusForbidden {
		t.Fatalf("another person was not refused clearly: %s", r)
	}
}
