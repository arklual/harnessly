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

// moveRuntime is a machine's move (internal/chatmove) as far as the hub can tell: it answers the steps and remembers
// what it was asked, so a test sees what the hub did to each end.
type moveRuntime struct {
	mu      sync.Mutex
	calls   []string
	bodies  map[string]map[string]any
	sealed  map[string]any
	holdErr bool
}

func newMoveRuntime() *moveRuntime {
	return &moveRuntime{bodies: map[string]map[string]any{}}
}

func (m *moveRuntime) steps() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return strings.Join(m.calls, ",")
}

func (m *moveRuntime) body(step string) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bodies[step]
}

func (m *moveRuntime) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	step := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	raw, _ := io.ReadAll(r.Body)
	var in map[string]any
	_ = json.Unmarshal(raw, &in)
	m.mu.Lock()
	m.calls = append(m.calls, step)
	m.bodies[step] = in
	holdErr, sealed := m.holdErr, m.sealed
	if step == "seal" {
		m.sealed = in["to"].(map[string]any)
	}
	m.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	say := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	switch step {
	case "describe":
		say(map[string]any{"project": nil, "clean": true, "busy": false, "hasHistory": true, "title": "Work"})
	case "preflight":
		say(map[string]any{"checks": []any{}, "filesOk": true, "agents": []any{
			map[string]any{"provider": "claude", "profile": "", "short": "Claude", "models": []any{map[string]any{"id": "opus", "label": "Opus", "default": true}}},
		}})
	case "export":
		say(map[string]any{"bundleEnc": map[string]string{"enc": "aes-256-gcm", "nonce": "n", "ct": "c"}, "fence": "100"})
	case "import":
		say(map[string]any{"conversationId": "tab-moved", "existing": false})
	case "hold":
		if holdErr {
			w.WriteHeader(http.StatusConflict)
			say(map[string]any{"code": "changed", "message": "В чате появились новые сообщения: перенос отменён, чат остался здесь"})
			return
		}
		say(map[string]any{})
	case "status":
		say(map[string]any{"moved": sealed})
	default:
		say(map[string]any{})
	}
}

type moveFixture struct {
	*fixture
	src, dst   *moveRuntime
	dstID      string
	dstMachine *hubtest.Agent
}

// moveSetup is the owner's two machines, in two organizations' tenants, each with a runtime that takes moves.
func moveSetup(t *testing.T) *moveFixture {
	t.Helper()
	f := &fixture{streamEnded: make(chan struct{}, 4)}
	f.s = hubtest.New(t, fleetOn)
	f.owner = f.s.SignIn("owner@example.com")
	mf := &moveFixture{fixture: f, src: newMoveRuntime(), dst: newMoveRuntime()}
	f.spaceID, _ = f.owner.NewSpace("Команда")
	f.a = f.owner.Machine(f.spaceID, mf.src)
	f.id = f.a.MachineID
	other, _ := f.owner.NewSpace("Вторая")
	mf.dstMachine = f.owner.Machine(other, mf.dst)
	mf.dstID = mf.dstMachine.MachineID
	return mf
}

func (m *moveFixture) move(p *hubtest.Person, target string) hubtest.Resp {
	return p.Do("POST", "/api/v2/chat-move", map[string]any{
		"opId": "op-1", "machineId": m.id, "convId": "tab-src", "targetMachineId": target,
		"provider": "claude", "model": map[string]any{"id": "opus"}, "withoutFiles": true,
	})
}

func TestChatMoveIsDarkWithoutTheFlag(t *testing.T) {
	f := setup(t)
	for _, path := range []string{"/api/v2/chat-move/status?machine=" + f.id + "&conv=tab-src", "/api/v2/chat-move/targets?machine=" + f.id + "&conv=tab-src"} {
		if r := f.owner.Do("GET", path, nil); r.Code != 404 {
			t.Errorf("%s: %s", path, r)
		}
	}
	if r := f.owner.Do("POST", "/api/v2/chat-move", map[string]any{"opId": "op-1", "machineId": f.id, "convId": "tab-src", "targetMachineId": "aaaaaaaaaaaaaaaaaaaa", "provider": "claude", "model": map[string]any{"id": "opus"}}); r.Code != 404 {
		t.Errorf("move: %s", r)
	}
}

func TestChatMoveListsTheOtherMachinesWithTheirOrganization(t *testing.T) {
	f := moveSetup(t)
	var out struct {
		Source  struct{ OK bool } `json:"source"`
		Targets []struct {
			MachineID string `json:"machineId"`
			Name      string `json:"name"`
			OrgName   string `json:"orgName"`
			Online    bool   `json:"online"`
			OK        bool   `json:"ok"`
			Agents    []struct{ Provider string }
		} `json:"targets"`
	}
	r := f.owner.Do("GET", "/api/v2/chat-move/targets?machine="+f.id+"&conv=tab-src", nil)
	if r.Code != 200 {
		t.Fatalf("targets: %s", r)
	}
	r.JSON(&out)
	if !out.Source.OK || len(out.Targets) != 1 || out.Targets[0].MachineID != f.dstID || !out.Targets[0].OK ||
		!out.Targets[0].Online || out.Targets[0].OrgName == "" || len(out.Targets[0].Agents) != 1 {
		t.Errorf("targets: %s", r)
	}
}

func TestChatMoveCarriesTheChatAndLeavesALinkOnTheOldMachine(t *testing.T) {
	f := moveSetup(t)
	r := f.move(f.owner, f.dstID)
	if r.Code != 200 {
		t.Fatalf("move: %s", r)
	}
	var out struct{ TargetMachineID, TargetName, ConvID string }
	r.JSON(&out)
	if out.TargetMachineID != f.dstID || out.ConvID != "tab-moved" || out.TargetName == "" {
		t.Errorf("answer: %s", r)
	}
	if got := f.src.steps(); got != "export,hold,seal" {
		t.Errorf("the source was asked %q", got)
	}
	if got := f.dst.steps(); got != "import" {
		t.Errorf("the target was asked %q", got)
	}
	// The same one-time key goes to both ends, and it is neither empty nor a secret of either machine.
	k1, k2 := f.src.body("export")["key"], f.dst.body("import")["key"]
	if k, _ := k1.(string); len(k) < 40 || k1 != k2 {
		t.Errorf("keys: %v / %v", k1, k2)
	}
	if to := f.src.body("seal")["to"].(map[string]any); to["machineId"] != f.dstID || to["conversationId"] != "tab-moved" {
		t.Errorf("seal: %v", to)
	}
	// The old machine says where the chat went; the hub itself keeps nothing.
	var st struct {
		Enabled bool
		Moved   struct{ ToMachine, ToName, ToConv string }
	}
	rs := f.owner.Do("GET", "/api/v2/chat-move/status?machine="+f.id+"&conv=tab-src", nil)
	rs.JSON(&st)
	if rs.Code != 200 || !st.Enabled || st.Moved.ToMachine != f.dstID || st.Moved.ToConv != "tab-moved" {
		t.Errorf("status: %s", rs)
	}
}

func TestChatMoveToAMachineThatIsAwayIsRefusedInWords(t *testing.T) {
	f := moveSetup(t)
	f.dstMachine.Close()
	f.s.Eventually("the target to go", func() bool {
		var l fleetList
		f.owner.Do("GET", "/api/me/machines", nil).JSON(&l)
		for _, m := range l.Machines {
			if m.ID == f.dstID {
				return m.Status == "offline"
			}
		}
		return false
	})
	r := f.move(f.owner, f.dstID)
	if r.Code != http.StatusConflict || r.Err() == "" || !strings.Contains(string(r.Body), "не на связи") || !strings.Contains(string(r.Body), "machine_offline") {
		t.Errorf("move to a machine that is away: %s", r)
	}
	if got := f.src.steps(); got != "" {
		t.Errorf("the source was touched: %q", got)
	}
	// The list says the same before anyone presses the button.
	var out struct {
		Targets []struct {
			OK   bool   `json:"ok"`
			Code string `json:"code"`
		} `json:"targets"`
	}
	f.owner.Do("GET", "/api/v2/chat-move/targets?machine="+f.id+"&conv=tab-src", nil).JSON(&out)
	if len(out.Targets) != 1 || out.Targets[0].OK || out.Targets[0].Code != "machine_offline" {
		t.Errorf("targets: %+v", out.Targets)
	}
}

func TestChatMoveToAMachineWithoutAccessIsRefusedAndNothingIsAsked(t *testing.T) {
	f := moveSetup(t)
	stranger := f.s.SignIn("stranger@example.com")
	space, _ := stranger.NewSpace("Чужая")
	theirs := newMoveRuntime()
	foreign := stranger.Machine(space, theirs).MachineID

	// The owner may not send a chat to a machine of someone else.
	r := f.move(f.owner, foreign)
	if r.Code != http.StatusForbidden || !strings.Contains(string(r.Body), "no_access") || !strings.Contains(string(r.Body), "нет доступа") {
		t.Errorf("to a stranger's machine: %s", r)
	}
	// Nor a stranger take a chat off the owner's machine, to their own or to anyone's.
	if r := stranger.Do("POST", "/api/v2/chat-move", map[string]any{
		"opId": "op-2", "machineId": f.id, "convId": "tab-src", "targetMachineId": foreign,
		"provider": "claude", "model": map[string]any{"id": "opus"},
	}); r.Code != http.StatusNotFound {
		t.Errorf("from a machine of someone else: %s", r)
	}
	if r := stranger.Do("GET", "/api/v2/chat-move/targets?machine="+f.id+"&conv=tab-src", nil); r.Code != http.StatusNotFound {
		t.Errorf("targets of someone else's machine: %s", r)
	}
	for name, rt := range map[string]*moveRuntime{"source": f.src, "target": f.dst, "stranger": theirs} {
		if got := rt.steps(); got != "" {
			t.Errorf("the %s was asked %q", name, got)
		}
	}
}

func TestChatMoveThatLosesTheRaceLeavesTheChatWhereItWas(t *testing.T) {
	f := moveSetup(t)
	f.src.mu.Lock()
	f.src.holdErr = true
	f.src.mu.Unlock()
	r := f.move(f.owner, f.dstID)
	if r.Code != http.StatusConflict || !strings.Contains(string(r.Body), "чат остался здесь") {
		t.Errorf("move: %s", r)
	}
	if got := f.src.steps(); got != "export,hold,release" {
		t.Errorf("the source was asked %q", got)
	}
	if got := f.dst.steps(); got != "import,discard" {
		t.Errorf("the target was asked %q", got)
	}
}

func TestChatMoveToTheSameMachineIsRefused(t *testing.T) {
	f := moveSetup(t)
	if r := f.move(f.owner, f.id); r.Code != http.StatusBadRequest || !strings.Contains(string(r.Body), "same_machine") {
		t.Errorf("move: %s", r)
	}
}
