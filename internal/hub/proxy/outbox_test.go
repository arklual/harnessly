package proxy_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

// sendRuntime is a machine that records the v2 send commands it is given and answers them with `answer`.
type sendRuntime struct {
	mu     sync.Mutex
	bodies []string
	device []string
	answer string
}

func (m *sendRuntime) handler() http.Handler {
	base := pageRuntime("Acme")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v2/cmd/send", func(w http.ResponseWriter, r *http.Request) {
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

func (m *sendRuntime) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

func (m *sendRuntime) reply(a string) {
	m.mu.Lock()
	m.answer = a
	m.mu.Unlock()
}

func sendBody(client, tab, text string) map[string]any {
	return map[string]any{"opId": client, "clientId": client, "conversationId": tab, "text": text, "attachmentIds": []string{}, "mode": "default", "kind": "prompt"}
}

func queueBody(client, tab, text string) map[string]any {
	return map[string]any{"tabId": tab, "text": text, "send": sendBody(client, tab, text)}
}

type outboxItemT struct {
	ID       string          `json:"id"`
	TabID    string          `json:"tabId"`
	Text     string          `json:"text"`
	State    string          `json:"state"`
	Error    *string         `json:"error"`
	ClientID string          `json:"clientId"`
	Send     json.RawMessage `json:"send"`
}

func listOutbox(t *testing.T, p *hubtest.Person, machine, tab string) []outboxItemT {
	t.Helper()
	r := p.Do("GET", "/agents/api/platform/offline-prompts?machine="+machine+"&tab="+tab, nil)
	if r.Code != 200 {
		t.Fatalf("list: %s", r)
	}
	var out struct{ Items []outboxItemT }
	r.JSON(&out)
	return out.Items
}

// a message written to a machine that is off waits in the hub, shows for its owner only, and is handed to the
// machine, as the page wrote it, once the machine is back.
func TestOutboxKeepsMessagesForAMachineThatIsOff(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &sendRuntime{answer: `{"r":"accepted","ch":"c","seq":1}`}
	a := owner.Machine(space, rt.handler())
	guest := s.SignIn("guest@example.com")
	owner.Join(space, guest)

	a.Close()
	s.Eventually("the machine to be off", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })

	base := "/agents/api/platform/offline-prompts?machine=" + a.MachineID
	body := queueBody("c-1", "tab-a", "привет")
	r := owner.Do("POST", base, body)
	if r.Code != 201 {
		t.Fatalf("post: %s", r)
	}
	// The page sends it again: kept once.
	if r := owner.Do("POST", base, body); r.Code != 201 {
		t.Fatalf("post again: %s", r)
	}
	if r := owner.Do("POST", base, queueBody("c-2", "tab-b", "другой чат")); r.Code != 201 {
		t.Fatalf("post b: %s", r)
	}
	items := listOutbox(t, owner, a.MachineID, "tab-a")
	if len(items) != 1 || items[0].State != "queued" || items[0].Text != "привет" || items[0].ClientID != "c-1" || items[0].Error != nil {
		t.Fatalf("items %+v", items)
	}
	var want, have map[string]any
	_ = json.Unmarshal(items[0].Send, &have)
	want = body["send"].(map[string]any)
	if have["opId"] != want["opId"] || have["text"] != want["text"] || have["conversationId"] != "tab-a" {
		t.Errorf("send kept as %s", items[0].Send)
	}
	if got := listOutbox(t, owner, a.MachineID, "tab-b"); len(got) != 1 {
		t.Errorf("tab-b %+v", got)
	}
	// Somebody else of the space sees nothing and takes nothing back.
	if got := listOutbox(t, guest, a.MachineID, "tab-a"); len(got) != 0 {
		t.Errorf("guest sees %+v", got)
	}
	if r := guest.Do("DELETE", base+"&client=c-1", nil); r.Code != 409 {
		t.Errorf("guest delete: %s", r)
	}
	// A pass while the machine is off hands over nothing.
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 0 || len(rt.got()) != 0 {
		t.Fatalf("delivered %d to a machine that is off: %v", n, rt.got())
	}

	// Taken back by clientId.
	if r := owner.Do("DELETE", base+"&client=c-2", nil); r.Code != 200 {
		t.Fatalf("delete: %s", r)
	}
	if got := listOutbox(t, owner, a.MachineID, "tab-b"); len(got) != 0 {
		t.Errorf("tab-b after delete %+v", got)
	}

	a.Connect()
	s.Eventually("the machine to be on", func() bool { return s.Hub.Registry.Get(a.MachineID) != nil })
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 1 {
		t.Fatalf("delivered %d", n)
	}
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 0 {
		t.Errorf("second pass delivered %d", n)
	}
	got := rt.got()
	if len(got) != 1 {
		t.Fatalf("machine got %v", got)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(got[0]), &sent)
	if sent["opId"] != "c-1" || sent["clientId"] != "c-1" || sent["conversationId"] != "tab-a" || sent["text"] != "привет" || sent["mode"] != "default" || sent["kind"] != "prompt" {
		t.Errorf("machine got %s", got[0])
	}
	if rt.device[0] != "hub-outbox" {
		t.Errorf("device %q", rt.device[0])
	}
	items = listOutbox(t, owner, a.MachineID, "tab-a")
	if len(items) != 1 || items[0].State != "delivered" {
		t.Errorf("after delivery %+v", items)
	}
	// What is delivered cannot be taken back.
	if r := owner.Do("DELETE", base+"&id="+items[0].ID, nil); r.Code != 409 {
		t.Errorf("delete delivered: %s", r)
	}
}

func TestOutboxRefusals(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &sendRuntime{answer: `{"r":"refused","code":"text-too-long"}`}
	a := owner.Machine(space, rt.handler())
	base := "/agents/api/platform/offline-prompts?machine=" + a.MachineID

	for _, bad := range []map[string]any{
		{"tabId": "t", "text": "x"}, // no send
		{"tabId": "t", "text": "x", "send": sendBody("c-9", "other", "x")},
		{"tabId": "t", "text": "x", "send": map[string]any{"opId": "a", "clientId": "b", "conversationId": "t", "text": "x", "attachmentIds": []string{}, "mode": "default", "kind": "prompt"}},
		{"tabId": "t", "text": "x", "send": map[string]any{"opId": "c", "clientId": "c", "conversationId": "t", "text": "x", "attachmentIds": []string{"f"}, "mode": "default", "kind": "prompt"}},
	} {
		if r := owner.Do("POST", base, bad); r.Code != 400 {
			t.Errorf("%v: %s", bad, r)
		}
	}
	if r := owner.Do("PUT", base, nil); r.Code != 405 {
		t.Errorf("put: %s", r)
	}

	if r := owner.Do("POST", base, queueBody("c-1", "t", "слишком длинно")); r.Code != 201 {
		t.Fatalf("post: %s", r)
	}
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 0 {
		t.Errorf("delivered %d", n)
	}
	items := listOutbox(t, owner, a.MachineID, "t")
	if len(items) != 1 || items[0].State != "failed" || items[0].Error == nil || !strings.Contains(*items[0].Error, "text-too-long") {
		t.Fatalf("items %+v", items)
	}
	if len(rt.got()) != 1 {
		t.Errorf("machine got %v", rt.got())
	}
	// A failed one is not sent again, and may be taken back.
	s.Hub.Proxy.DeliverOutbox(t.Context())
	if len(rt.got()) != 1 {
		t.Errorf("sent again: %v", rt.got())
	}
	if r := owner.Do("DELETE", base+"&client=c-1", nil); r.Code != 200 {
		t.Errorf("delete failed one: %s", r)
	}

	// Not now: it waits and goes again.
	rt.reply(`{"r":"refused","code":"machine-offline"}`)
	owner.Do("POST", base, queueBody("c-2", "t", "потом"))
	owner.Do("POST", base, queueBody("c-3", "t", "ещё"))
	s.Hub.Proxy.DeliverOutbox(t.Context())
	if items := listOutbox(t, owner, a.MachineID, "t"); len(items) != 2 || items[0].State != "queued" || items[1].State != "queued" {
		t.Fatalf("after not-now %+v", items)
	}
	if n := len(rt.got()); n != 2 { // the machine that did not answer stops the pass: one more call, not two
		t.Errorf("calls %d", n)
	}
	rt.reply(`{"r":"queued","ch":"c","seq":2}`)
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 2 {
		t.Errorf("delivered %d", n)
	}
	got := rt.got()
	if len(got) != 4 || !strings.Contains(got[2], `"c-2"`) || !strings.Contains(got[3], `"c-3"`) {
		t.Errorf("order %v", got)
	}
}

// The messages of a machine that stays off do not crowd out those of a machine that is back, and one machine keeps at
// most 200 waiting.
func TestOutboxAMachineThatStaysOffDoesNotHoldUpAnother(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rtA := &sendRuntime{answer: `{"r":"accepted","ch":"c","seq":1}`}
	a := owner.Machine(space, rtA.handler())
	b := owner.Machine(space, (&sendRuntime{answer: `{"r":"accepted"}`}).handler())
	b.Close()
	s.Eventually("b to be off", func() bool { return s.Hub.Registry.Get(b.MachineID) == nil })

	offBase := "/agents/api/platform/offline-prompts?machine=" + b.MachineID
	for i := range 200 {
		id := fmt.Sprintf("b-%03d", i)
		if r := owner.Do("POST", offBase, queueBody(id, fmt.Sprintf("tab-%d", i/20), "ждёт")); r.Code != 201 {
			t.Fatalf("post %s: %s", id, r)
		}
	}
	if r := owner.Do("POST", offBase, queueBody("b-x", "tab-x", "лишнее")); r.Code != 409 {
		t.Errorf("the 201st message: %s", r)
	}
	if r := owner.Do("POST", "/agents/api/platform/offline-prompts?machine="+a.MachineID, queueBody("a-1", "tab-a", "привет")); r.Code != 201 {
		t.Fatalf("post a: %s", r)
	}
	if n := s.Hub.Proxy.DeliverOutbox(t.Context()); n != 1 || len(rtA.got()) != 1 {
		t.Fatalf("delivered %d, a got %v", n, rtA.got())
	}
}
