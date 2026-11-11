package proxy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// createRuntime is a machine that records the conversation.create commands it is given and answers them with `answer`.
type createRuntime struct {
	mu     sync.Mutex
	bodies []string
	device []string
	answer string
}

func (m *createRuntime) handler() http.Handler {
	base := pageRuntime("Acme")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/cmd/conversation.create", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.bodies = append(m.bodies, string(b))
		m.device = append(m.device, r.Header.Get("X-Harnessly-Device"))
		ans := m.answer
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, ans)
	})
	mux.Handle("/", base)
	return mux
}

func (m *createRuntime) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

func (m *createRuntime) reply(a string) {
	m.mu.Lock()
	m.answer = a
	m.mu.Unlock()
}

func createBody(client, text string) map[string]any {
	return map[string]any{
		"opId": client, "cwd": "/home/me/work", "provider": "claude", "model": map[string]any{"id": "opus", "effort": "high"},
		"first": map[string]any{"clientId": client, "text": text, "attachmentIds": []string{}},
	}
}

func chatQueueBody(client, text string) map[string]any {
	return map[string]any{"text": text, "create": createBody(client, text)}
}

type queuedChatT struct {
	ID        string  `json:"id"`
	MachineID string  `json:"machineId"`
	ClientID  string  `json:"clientId"`
	Text      string  `json:"text"`
	State     string  `json:"state"`
	Error     *string `json:"error"`
	ConvID    *string `json:"convId"`
	CreatedAt int64   `json:"createdAt"`
}

func listQueuedChats(t *testing.T, p *hubtest.Person) []queuedChatT {
	t.Helper()
	r := p.Do("GET", "/agents/api/platform/offline-chats", nil)
	if r.Code != 200 {
		t.Fatalf("list: %s", r)
	}
	var out struct{ Items []queuedChatT }
	r.JSON(&out)
	return out.Items
}

// with none of the person's machines answering, the first message of a new chat waits in the hub; the hub
// makes the chat on the machine, with that message, once the machine is back, whether a page is open or not.
func TestOutboxChatsMakeTheChatOnceTheMachineIsBack(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &createRuntime{answer: `{"r":"accepted","ch":"convs","seq":3,"rev":null,"id":"tab-new1","code":""}`}
	a := owner.Machine(space, rt.handler())
	guest := s.SignIn("guest@example.com")
	owner.Join(space, guest)

	a.Close()
	s.Eventually("the machine to be off", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })

	base := "/agents/api/platform/offline-chats?machine=" + a.MachineID
	body := chatQueueBody("c-1", "сделай отчёт")
	if r := owner.Do("POST", base, body); r.Code != 201 {
		t.Fatalf("post: %s", r)
	}
	// The page sends it again: kept once.
	if r := owner.Do("POST", base, body); r.Code != 201 {
		t.Fatalf("post again: %s", r)
	}
	items := listQueuedChats(t, owner)
	if len(items) != 1 || items[0].State != "queued" || items[0].Text != "сделай отчёт" || items[0].ClientID != "c-1" ||
		items[0].MachineID != a.MachineID || items[0].ConvID != nil || items[0].Error != nil || items[0].CreatedAt == 0 {
		t.Fatalf("items %+v", items)
	}
	if got := listQueuedChats(t, guest); len(got) != 0 {
		t.Errorf("guest sees %+v", got)
	}
	if r := guest.Do("DELETE", base+"&id="+items[0].ID, nil); r.Code != 409 {
		t.Errorf("guest delete: %s", r)
	}
	if n := s.Hub.Proxy.DeliverOutboxChats(t.Context()); n != 0 || len(rt.got()) != 0 {
		t.Fatalf("made %d on a machine that is off: %v", n, rt.got())
	}

	// Another one, taken back before the machine is there.
	if r := owner.Do("POST", base, chatQueueBody("c-2", "передумал")); r.Code != 201 {
		t.Fatalf("post c-2: %s", r)
	}
	two := listQueuedChats(t, owner)
	if len(two) != 2 {
		t.Fatalf("two: %+v", two)
	}
	for _, it := range two {
		if it.ClientID == "c-2" {
			if r := owner.Do("DELETE", base+"&id="+it.ID, nil); r.Code != 200 {
				t.Fatalf("delete: %s", r)
			}
		}
	}

	a.Connect()
	s.Eventually("the machine to be on", func() bool { return s.Hub.Registry.Get(a.MachineID) != nil })
	if n := s.Hub.Proxy.DeliverOutboxChats(t.Context()); n != 1 {
		t.Fatalf("made %d", n)
	}
	if n := s.Hub.Proxy.DeliverOutboxChats(t.Context()); n != 0 {
		t.Errorf("second pass made %d", n)
	}
	got := rt.got()
	if len(got) != 1 {
		t.Fatalf("machine got %v", got)
	}
	var sent struct {
		OpID     string `json:"opId"`
		Cwd      string `json:"cwd"`
		Provider string `json:"provider"`
		Model    struct {
			ID string `json:"id"`
		} `json:"model"`
		First struct {
			ClientID string `json:"clientId"`
			Text     string `json:"text"`
		} `json:"first"`
	}
	_ = json.Unmarshal([]byte(got[0]), &sent)
	if sent.OpID != "c-1" || sent.First.ClientID != "c-1" || sent.First.Text != "сделай отчёт" || sent.Cwd != "/home/me/work" ||
		sent.Provider != "claude" || sent.Model.ID != "opus" {
		t.Errorf("machine got %s", got[0])
	}
	if rt.device[0] != "hub-outbox" {
		t.Errorf("device %q", rt.device[0])
	}
	items = listQueuedChats(t, owner)
	if len(items) != 1 || items[0].State != "delivered" || items[0].ConvID == nil || *items[0].ConvID != "tab-new1" {
		t.Fatalf("after delivery %+v", items)
	}
	if r := owner.Do("DELETE", base+"&id="+items[0].ID, nil); r.Code != 409 {
		t.Errorf("delete delivered: %s", r)
	}
}

// A machine that refuses the chat for good fails it with words; one that is busy for a moment keeps it waiting.
func TestOutboxChatsOutcomes(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &createRuntime{answer: `{"r":"refused","code":"timeout"}`}
	a := owner.Machine(space, rt.handler())
	base := "/agents/api/platform/offline-chats?machine=" + a.MachineID
	if r := owner.Do("POST", base, chatQueueBody("c-1", "раз")); r.Code != 201 {
		t.Fatalf("post: %s", r)
	}
	if n := s.Hub.Proxy.DeliverOutboxChats(t.Context()); n != 0 {
		t.Fatalf("made %d on a refusal", n)
	}
	if items := listQueuedChats(t, owner); len(items) != 1 || items[0].State != "queued" {
		t.Fatalf("a passing refusal: %+v", items)
	}
	rt.reply(`{"r":"refused","code":"bad-request"}`)
	s.Hub.Proxy.DeliverOutboxChats(t.Context())
	items := listQueuedChats(t, owner)
	if len(items) != 1 || items[0].State != "failed" || items[0].Error == nil || *items[0].Error == "" {
		t.Fatalf("a refusal for good: %+v", items)
	}
	// A failed one is taken back by the person.
	if r := owner.Do("DELETE", base+"&id="+items[0].ID, nil); r.Code != 200 {
		t.Errorf("delete failed: %s", r)
	}
}

// Only a plain first message of a new chat is kept: nothing attached, no launch of several, no other chat's.
func TestOutboxChatsRefusals(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	a := owner.Machine(space, (&createRuntime{}).handler())
	base := "/agents/api/platform/offline-chats?machine=" + a.MachineID
	bad := map[string]func(map[string]any){
		"attachments": func(c map[string]any) {
			c["first"].(map[string]any)["attachmentIds"] = []string{"u1"}
		},
		"launch":       func(c map[string]any) { c["launch"] = map[string]any{"id": "l", "prompt": "p"} },
		"parent":       func(c map[string]any) { c["parentId"] = "tab-x" },
		"both places":  func(c map[string]any) { c["projectId"] = "p1" },
		"no place":     func(c map[string]any) { delete(c, "cwd") },
		"other client": func(c map[string]any) { c["first"].(map[string]any)["clientId"] = "c-other" },
		"no first":     func(c map[string]any) { delete(c, "first") },
		"empty text":   func(c map[string]any) { c["first"].(map[string]any)["text"] = " " },
		"provider":     func(c map[string]any) { c["provider"] = "Not A Word" },
	}
	for name, spoil := range bad {
		body := chatQueueBody("c-1", "текст")
		spoil(body["create"].(map[string]any))
		if r := owner.Do("POST", base, body); r.Code != 400 {
			t.Errorf("%s: %s", name, r)
		}
	}
	if items := listQueuedChats(t, owner); len(items) != 0 {
		t.Errorf("kept %+v", items)
	}
	stranger := s.SignIn("stranger@example.com")
	if r := stranger.Do("POST", base, chatQueueBody("c-9", "чужая")); r.Code != 403 && r.Code != 404 {
		t.Errorf("a stranger's chat: %s", r)
	}
}

// Chats waiting for machines that stay off do not hold up the chats of a machine that is back, however many there are.
func TestOutboxChatsOfAMachineThatIsOffDoNotStarveOthers(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &createRuntime{answer: `{"r":"accepted","ch":"convs","seq":3,"rev":null,"id":"tab-new1","code":""}`}
	a := owner.Machine(space, rt.handler())
	for i := range 250 {
		if _, err := s.DB.Exec(t.Context(), `INSERT INTO offline_chats (id, user_id, machine_id, client_id, text, create_cmd, state,
			created_at, changed_at) VALUES (?, ?, 'machine-off', ?, 'x', '{}', 'queued', ?, ?)`,
			fmt.Sprintf("old-%d", i), fmt.Sprintf("someone-%d", i), fmt.Sprintf("c-%d", i), i+1, i+1); err != nil {
			t.Fatal(err)
		}
	}
	if r := owner.Do("POST", "/agents/api/platform/offline-chats?machine="+a.MachineID, chatQueueBody("c-new", "сделай отчёт")); r.Code != 201 {
		t.Fatalf("post: %s", r)
	}
	if n := s.Hub.Proxy.DeliverOutboxChats(t.Context()); n != 1 || len(rt.got()) != 1 {
		t.Fatalf("made %d, machine got %v", n, rt.got())
	}
}
