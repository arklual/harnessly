package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
)

var launchKey = "launch-" + strings.Repeat("k", 40)

// launchRuntime is a machine that records the conversations it is asked to start.
type launchRuntime struct {
	mu      sync.Mutex
	bodies  []string
	cookies []string
	status  int
	answer  string
}

func (m *launchRuntime) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agents/api/acme/conversations", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.bodies = append(m.bodies, string(b))
		m.cookies = append(m.cookies, r.Header.Get("Cookie"))
		status, ans := m.status, m.answer
		m.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, ans)
	})
	mux.Handle("/", pageRuntime("Acme"))
	return mux
}

func (m *launchRuntime) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

func launchPost(p *hubtest.Person, token string, body any) hubtest.Resp {
	if token == "" {
		return p.Do("POST", "/api/platform/launch", body)
	}
	return p.Do("POST", "/api/platform/launch", body, "Authorization", "Bearer "+token)
}

func switchLaunch(t *testing.T, admin *hubtest.Person, person string) {
	t.Helper()
	if r := admin.Do("PUT", "/api/admin/switches", map[string]any{"feature": "prompt-launch", "enabled": true, "person": person}); r.Code != 204 {
		t.Fatalf("switch: %s", r)
	}
}

func TestLaunchStartsAConversationOnThePersonsMachine(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn, hubtest.Env("HUB_LAUNCH_TOKEN", launchKey))
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &launchRuntime{status: 201, answer: `{"conversationId":"conv-1","provider":"claude"}`}
	a := owner.Machine(space, rt.handler())
	guest := s.SignIn("guest@example.com")
	guestSpace, _ := guest.NewSpace("Гость")
	guestRT := &launchRuntime{status: 201, answer: `{"conversationId":"conv-2"}`}
	g := guest.Machine(guestSpace, guestRT.handler())

	body := map[string]any{"person": "Owner@example.com", "prompt": "посмотри ошибку", "provider": "codex"}

	// Off until the switch: a person's machine is not reached by the token on its own.
	if r := launchPost(owner, launchKey, body); r.Code != 503 || !strings.Contains(string(r.Body), "launch_off") {
		t.Fatalf("switch off: %s", r)
	}
	if len(rt.got()) != 0 {
		t.Fatalf("started with the switch off: %v", rt.got())
	}
	switchLaunch(t, owner, "owner@example.com")

	r := launchPost(owner, launchKey, body)
	if r.Code != 201 {
		t.Fatalf("launch: %s", r)
	}
	var out map[string]any
	r.JSON(&out)
	if out["conversationId"] != "conv-1" || out["machineId"] != a.MachineID || out["machineName"] == "" || out["machineName"] == nil {
		t.Errorf("answer %v", out)
	}
	url, _ := out["url"].(string)
	if !strings.HasPrefix(url, s.URL+"/agents/?c=agent%3A"+a.MachineID+"%3Aconv-1") {
		t.Errorf("url %q", url)
	}
	got := rt.got()
	if len(got) != 1 {
		t.Fatalf("runtime saw %v", got)
	}
	var sent map[string]any
	_ = json.Unmarshal([]byte(got[0]), &sent)
	if sent["prompt"] != "посмотри ошибку" || sent["provider"] != "codex" || len(sent) != 2 {
		t.Errorf("sent %v", sent)
	}
	if !strings.Contains(rt.cookies[0], "=") || strings.Contains(rt.cookies[0], launchKey) {
		t.Errorf("cookie %q", rt.cookies[0])
	}

	// Naming the machine is the same, naming somebody else's is not allowed, and neither is the guest's by the owner.
	body["machineId"] = a.MachineID
	if r := launchPost(owner, launchKey, body); r.Code != 201 {
		t.Errorf("named own: %s", r)
	}
	body["machineId"] = g.MachineID
	if r := launchPost(owner, launchKey, body); r.Code != 404 {
		t.Errorf("named another person's machine: %s", r)
	}
	if len(guestRT.got()) != 0 {
		t.Fatalf("another person's machine was started: %v", guestRT.got())
	}
	delete(body, "machineId")

	// Refusals before anything is looked at.
	if r := launchPost(owner, "", body); r.Code != 403 {
		t.Errorf("no token: %s", r)
	}
	if r := launchPost(owner, launchKey+"x", body); r.Code != 403 {
		t.Errorf("wrong token: %s", r)
	}
	if r := launchPost(owner, launchKey, map[string]any{"person": "nobody@example.com", "prompt": "x"}); r.Code != 404 {
		t.Errorf("unknown person: %s", r)
	}
	for name, bad := range map[string]map[string]any{
		"no prompt":     {"person": "owner@example.com", "prompt": "  "},
		"no person":     {"prompt": "x"},
		"bad provider":  {"person": "owner@example.com", "prompt": "x", "provider": "gpt"},
		"bad machineId": {"person": "owner@example.com", "prompt": "x", "machineId": "../x"},
	} {
		if r := launchPost(owner, launchKey, bad); r.Code != 400 {
			t.Errorf("%s: %s", name, r)
		}
	}
	if n := len(rt.got()); n != 2 {
		t.Errorf("the runtime started %d conversations, want 2", n)
	}
}

func TestLaunchIsShutWithoutAToken(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn)
	owner := s.SignIn("owner@example.com")
	if r := launchPost(owner, launchKey, map[string]any{"person": "owner@example.com", "prompt": "x"}); r.Code != 404 {
		t.Errorf("no HUB_LAUNCH_TOKEN: %s", r)
	}
}

func TestLaunchWhenTheMachineCannotTakeIt(t *testing.T) {
	s := hubtest.New(t, withWorkspace, fleetOn, hubtest.Env("HUB_LAUNCH_TOKEN", launchKey))
	owner := s.SignIn("owner@example.com")
	space, _ := owner.NewSpace("Команда")
	rt := &launchRuntime{status: 500, answer: `{"error":"boom"}`}
	a := owner.Machine(space, rt.handler())
	switchLaunch(t, owner, "")
	body := map[string]any{"person": "owner@example.com", "prompt": "x"}

	// The runtime answered with an error: it may have started something, so the caller is not told to repeat.
	r := launchPost(owner, launchKey, body)
	if r.Code != 502 || strings.Contains(string(r.Body), "retryable") || !strings.Contains(string(r.Body), "runtime_error") {
		t.Errorf("runtime error: %s", r)
	}

	// The machine is not there: nothing was started, the caller may repeat.
	a.Close()
	s.Eventually("the machine to be off", func() bool { return s.Hub.Registry.Get(a.MachineID) == nil })
	r = launchPost(owner, launchKey, body)
	if r.Code != 502 || !strings.Contains(string(r.Body), `"retryable":true`) || !strings.Contains(string(r.Body), "machine_offline") {
		t.Errorf("machine off: %s", r)
	}
	if n := len(rt.got()); n != 1 {
		t.Errorf("runtime saw %d requests", n)
	}
}
