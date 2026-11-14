package proxy_test

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// connectApp goes through the device flow as an app would and returns its token: the app starts, the person confirms the
// code signed in, the app picks the token up.
func connectApp(t *testing.T, s *hubtest.Stack, person *hubtest.Person, scopes []string) string {
	t.Helper()
	app := s.Browser()
	var start struct {
		DeviceCode, UserCode, VerifyURL string
		Interval                        int
	}
	if r := app.Do("POST", "/api/auth/device/start", map[string]string{"client": "Raycast", "device": "MacBook"}); r.Code != 200 {
		t.Fatalf("start: %s", r)
	} else {
		r.JSON(&start)
	}
	if start.DeviceCode == "" || !strings.Contains(start.VerifyURL, "/connect?code=") || len(start.UserCode) != 9 {
		t.Fatalf("start %+v", start)
	}
	if r := app.Do("POST", "/api/auth/device/token", map[string]string{"deviceCode": start.DeviceCode}); r.Code != 400 || r.Err() != "authorization_pending" {
		t.Fatalf("token before the person confirmed: %s", r)
	}
	var shown struct{ Client, Device string }
	if r := person.Do("GET", "/api/auth/device?code="+strings.ToLower(start.UserCode), nil); r.Code != 200 {
		t.Fatalf("show: %s", r)
	} else if r.JSON(&shown); shown.Client != "Raycast" || shown.Device != "MacBook" {
		t.Fatalf("shown %+v", shown)
	}
	if r := person.Do("POST", "/api/auth/device/approve", map[string]any{"code": start.UserCode, "allow": true, "scopes": scopes}); r.Code/100 != 2 {
		t.Fatalf("approve: %s", r)
	}
	var tok struct {
		Token   string
		TokenID string
		Scopes  []string
	}
	if r := app.Do("POST", "/api/auth/device/token", map[string]string{"deviceCode": start.DeviceCode}); r.Code != 200 {
		t.Fatalf("token: %s", r)
	} else {
		r.JSON(&tok)
	}
	if !strings.HasPrefix(tok.Token, "hlp_") || tok.TokenID == "" {
		t.Fatalf("token %+v", tok)
	}
	// The code is spent: a second pick-up gets nothing.
	if r := app.Do("POST", "/api/auth/device/token", map[string]string{"deviceCode": start.DeviceCode}); r.Code == 200 {
		t.Fatalf("the code gave a second token: %s", r)
	}
	return tok.Token
}

func bearer(tok string) []string { return []string{"Authorization", "Bearer " + tok} }

func TestClientAPIIsOffWithoutTheSwitch(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	app := s.Browser()
	var start struct{ UserCode string }
	app.Do("POST", "/api/auth/device/start", map[string]string{"client": "Raycast"}).JSON(&start)
	if r := owner.Do("GET", "/api/auth/device?code="+start.UserCode, nil); r.Code != 404 || r.Err() != "client_api_off" {
		t.Fatalf("show with the switch off: %s", r)
	}
	if r := app.Do("GET", "/api/client/v1/me", nil, bearer("hlp_nothing")...); r.Code != 401 {
		t.Fatalf("a made-up token: %s", r)
	}
	if r := app.Do("GET", "/api/client/v1/me", nil); r.Code != 401 {
		t.Fatalf("no token: %s", r)
	}
}

func TestClientAPIReadsChatsAndSendsPrompts(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	if err := s.Hub.Switches.Set(context.Background(), "client-api", "owner@example.com", "*", true, "", "test"); err != nil {
		t.Fatal(err)
	}
	space, _ := owner.NewSpace("Команда")
	rt := &sendRuntime{answer: `{"r":"accepted","ch":"c","seq":1}`}
	a := s.NewAgent(rt.handler())
	a.Hello.Features = append(slices.Clone(a.Hello.Features), hubproto.FeatureHistory)
	if r := a.Register(owner.NewCode(space, "dir"), hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
		t.Fatalf("register: %s", r)
	}
	a.Connect()
	waitFor(t, func() bool { return len(a.Calls(hubproto.OpHistoryStart)) == 1 })
	convs := []string{
		`{"id":"tab-a","rev":1,"title":"Отчёт","cwd":"/work/shop","provider":"claude","lastPromptAt":5,"createdAt":1,"machineId":"local",` +
			`"state":{"k":"ready","since":3},"seenAt":1,"lastTurn":{"id":"c:t1#0","status":"done","startedAt":1,"endedAt":2,"toolCount":0,"snippet":"готово"},` +
			`"queue":{"count":0,"paused":null},"gate":null,"recap":{"text":"Собрал отчёт","at":2}}`,
		`{"id":"tab-b","rev":1,"title":"Сборка","cwd":"/work/shop","provider":"codex","lastPromptAt":4,"createdAt":1,"machineId":"local",` +
			`"state":{"k":"working","since":3,"turnId":null,"compacting":false},"seenAt":9,"lastTurn":null,"queue":{"count":0,"paused":null},"gate":null}`,
		`{"id":"tab-t","rev":1,"title":"Тест","cwd":"/work/shop","provider":"claude","lastPromptAt":3,"machineId":"local","test":true,` +
			`"state":{"k":"idle","since":1},"seenAt":0,"lastTurn":null,"queue":{"count":0,"paused":null},"gate":null}`,
	}
	var events []json.RawMessage
	for _, c := range convs {
		events = append(events, json.RawMessage(`{"type":"conversation_upsert","conv":`+c+`}`))
	}
	a.Emit(map[string]any{"event": hubproto.EventHistoryEvents, "ch": "convs", "epoch": "ep-1", "fromSeq": 1, "events": events})
	a.Emit(map[string]any{"event": hubproto.EventHistoryBackfill, "ch": "conv:tab-a", "events": []json.RawMessage{json.RawMessage(
		`{"type":"feed_reset","turns":[{"id":"c:t1#0","startedAt":1,"endedAt":2,"status":"done","answerEntryId":"c:t1#1"}],` +
			`"entries":[{"id":"c:t1#0","turnId":"c:t1#0","at":1,"t":"user","text":"собери отчёт"},` +
			`{"id":"c:t1#1","turnId":"c:t1#0","at":2,"t":"say","md":"Отчёт готов"}],"beforeTurnId":null,"history":{"from":""}}`)}})

	tok := connectApp(t, s, owner, []string{"read", "prompt"})
	app := s.Browser()

	var me struct {
		User     struct{ Email string }
		Machines []struct {
			ID     string
			Online bool
		}
	}
	if r := app.Do("GET", "/api/client/v1/me", nil, bearer(tok)...); r.Code != 200 {
		t.Fatalf("me: %s", r)
	} else if r.JSON(&me); me.User.Email != "owner@example.com" || len(me.Machines) != 1 || me.Machines[0].ID != a.MachineID || !me.Machines[0].Online {
		t.Fatalf("me %+v", me)
	}

	type chat struct {
		ID, Title, Attention, State, Project, URL string
		Recap                                     *string
		Unread                                    bool
	}
	var list struct {
		Chats  []chat
		Counts map[string]int
	}
	waitFor(t, func() bool {
		r := app.Do("GET", "/api/client/v1/chats", nil, bearer(tok)...)
		r.JSON(&list)
		return r.Code == 200 && len(list.Chats) == 2
	})
	if c := list.Chats[0]; c.ID != "tab-a" || c.Attention != "unread" || !c.Unread || c.Recap == nil || *c.Recap != "Собрал отчёт" ||
		c.Project != "shop" || !strings.Contains(c.URL, "/agents/?c=agent%3A"+a.MachineID+"%3Atab-a") {
		t.Errorf("first chat %+v", c)
	}
	if list.Counts["attention"] != 1 || list.Counts["working"] != 1 || list.Counts["all"] != 2 {
		t.Errorf("counts %v", list.Counts)
	}
	if r := app.Do("GET", "/api/client/v1/chats?filter=working", nil, bearer(tok)...); r.Code != 200 {
		t.Fatalf("working: %s", r)
	} else if r.JSON(&list); len(list.Chats) != 1 || list.Chats[0].ID != "tab-b" {
		t.Errorf("working %+v", list.Chats)
	}

	var one struct {
		Chat  chat
		Turns []struct{ Prompt, Answer, Status string }
	}
	if r := app.Do("GET", "/api/client/v1/chats/tab-a", nil, bearer(tok)...); r.Code != 200 {
		t.Fatalf("chat: %s", r)
	} else if r.JSON(&one); len(one.Turns) != 1 || one.Turns[0].Prompt != "собери отчёт" || one.Turns[0].Answer != "Отчёт готов" {
		t.Errorf("chat %+v", one)
	}
	if r := app.Do("GET", "/api/client/v1/chats/tab-x", nil, bearer(tok)...); r.Code != 404 {
		t.Errorf("unknown chat: %s", r)
	}

	// A prompt to a machine that is on goes to it as the page's send.
	var sent struct{ Delivery, ClientID string }
	if r := app.Do("POST", "/api/client/v1/chats/tab-a/messages", map[string]string{"text": "ещё раз"}, bearer(tok)...); r.Code != 200 {
		t.Fatalf("send: %s", r)
	} else if r.JSON(&sent); sent.Delivery != "delivered" {
		t.Fatalf("send %+v", sent)
	}
	if got := rt.got(); len(got) != 1 || !strings.Contains(got[0], `"text":"ещё раз"`) || !strings.Contains(got[0], `"conversationId":"tab-a"`) {
		t.Fatalf("the machine got %v", got)
	}
	if r := app.Do("POST", "/api/client/v1/chats/tab-a/messages", map[string]string{"text": "  "}, bearer(tok)...); r.Code != 400 {
		t.Errorf("empty text: %s", r)
	}

	var projects struct {
		Projects []struct {
			Cwd       string
			Chats     int
			Providers []string
		}
	}
	if r := app.Do("GET", "/api/client/v1/projects", nil, bearer(tok)...); r.Code != 200 {
		t.Fatalf("projects: %s", r)
	} else if r.JSON(&projects); len(projects.Projects) != 1 || projects.Projects[0].Cwd != "/work/shop" || projects.Projects[0].Chats != 2 {
		t.Errorf("projects %+v", projects)
	}

	// The machine goes off: the prompt waits in the hub.
	a.Close()
	s.Eventually("the machine to be off", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })
	if r := app.Do("POST", "/api/client/v1/chats/tab-a/messages", map[string]string{"text": "когда вернёшься"}, bearer(tok)...); r.Code != 202 {
		t.Fatalf("send while off: %s", r)
	} else if r.JSON(&sent); sent.Delivery != "queued" {
		t.Fatalf("send while off %+v", sent)
	}
	if r := app.Do("POST", "/api/client/v1/chats/tab-a/stop", nil, bearer(tok)...); r.Code/100 == 2 {
		t.Errorf("stop while off: %s", r)
	}

	// A token that may only read cannot write.
	reader := connectApp(t, s, owner, []string{"read"})
	if r := app.Do("POST", "/api/client/v1/chats/tab-a/messages", map[string]string{"text": "нельзя"}, bearer(reader)...); r.Code != 403 {
		t.Errorf("read-only token wrote: %s", r)
	}

	// The person sees both apps and revokes one: it stops working at once.
	var mine struct {
		On     bool
		Tokens []struct{ ID, Client string }
	}
	if r := owner.Do("GET", "/api/me/tokens", nil); r.Code != 200 {
		t.Fatalf("tokens: %s", r)
	} else if r.JSON(&mine); !mine.On || len(mine.Tokens) != 2 {
		t.Fatalf("tokens %+v", mine)
	}
	for _, x := range mine.Tokens {
		if r := owner.Do("DELETE", "/api/me/tokens/"+x.ID, nil); r.Code/100 != 2 {
			t.Fatalf("revoke: %s", r)
		}
	}
	if r := app.Do("GET", "/api/client/v1/me", nil, bearer(tok)...); r.Code != 401 {
		t.Errorf("a revoked token: %s", r)
	}
}
