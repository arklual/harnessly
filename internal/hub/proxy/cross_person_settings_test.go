package proxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// settingsFleet is two users in one space and two machines whose runtimes keep their own (machine-wide) me.json.
type settingsFleet struct {
	s            *hubtest.Stack
	alice, bob   *hubtest.Person
	space        string
	first, other *hubtest.Agent
}

// withPing adds a path a machine answers whatever its runtime knows, to tell a machine that is reached from one that is not.
func withPing(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/api/v2/ping") {
			_, _ = io.WriteString(w, "pong")
			return
		}
		h.ServeHTTP(w, r)
	})
}

func newSettingsFleet(t *testing.T) *settingsFleet {
	t.Helper()
	f := &settingsFleet{s: hubtest.New(t, fleetOn, withWorkspace)}
	f.alice, f.bob = f.s.SignIn("alice@example.com"), f.s.SignIn("bob@example.com")
	f.space, _ = f.alice.NewSpace("Team")
	f.alice.Join(f.space, f.bob)
	f.first = f.alice.Machine(f.space, withPing(&wire.Server{Me: &wire.FileMe{Path: filepath.Join(t.TempDir(), "me.json"), Name: "machine-owner"}}))
	f.other = f.alice.Machine(f.space, withPing(&wire.Server{Me: &wire.FileMe{Path: filepath.Join(t.TempDir(), "me.json")}}))
	return f
}

func readMe(t *testing.T, p *hubtest.Person, path string) wire.Me {
	t.Helper()
	r := p.Do("GET", path, nil)
	if r.Code != 200 {
		t.Fatalf("read %s: %s", path, r)
	}
	var m wire.Me
	r.JSON(&m)
	return m
}

func patchMe(p *hubtest.Person, path string, rev int64, patch map[string]any) hubtest.Resp {
	body, _ := json.Marshal(map[string]any{"rev": rev, "patch": patch})
	return p.Do("PATCH", path, json.RawMessage(body))
}

func TestPersonalSettingsDoNotCrossHubUsers(t *testing.T) {
	f := newSettingsFleet(t)
	a, b := readMe(t, f.alice, "/api/v2/me"), readMe(t, f.bob, "/api/v2/me")
	if a.Rev != 1 || b.Rev != 1 {
		t.Fatalf("a new account starts at revision 1: Alice %d, Bob %d", a.Rev, b.Rev)
	}
	if a.Email == nil || *a.Email != "alice@example.com" || b.Email == nil || *b.Email != "bob@example.com" {
		t.Errorf("identity is the hub user's: %v %v", a.Email, b.Email)
	}
	if a.Name != nil && *a.Name == "machine-owner" {
		t.Errorf("the machine's name is shown as the person: %v", a.Name)
	}
	if r := patchMe(f.alice, "/api/v2/me/settings", a.Rev, map[string]any{"defaultMachine": f.first.MachineID, "locale": "en"}); r.Code != 200 {
		t.Fatalf("alice patch: %s", r)
	}
	b = readMe(t, f.bob, "/api/v2/me")
	if b.Settings.DefaultMachine != nil || b.Rev != 1 || b.Settings.Locale != "ru" {
		t.Errorf("Alice's write leaked into Bob: rev=%d %+v", b.Rev, b.Settings)
	}
	if r := patchMe(f.bob, "/api/v2/me/settings", b.Rev, map[string]any{"defaultMachine": f.other.MachineID}); r.Code != 200 {
		t.Fatalf("bob patch: %s", r)
	}
	a = readMe(t, f.alice, "/api/v2/me")
	if a.Settings.DefaultMachine == nil || *a.Settings.DefaultMachine != f.first.MachineID || a.Rev != 2 || a.Settings.Locale != "en" {
		t.Errorf("Bob's write changed Alice's settings: rev=%d %+v", a.Rev, a.Settings)
	}
	b = readMe(t, f.bob, "/api/v2/me")
	if b.Settings.DefaultMachine == nil || *b.Settings.DefaultMachine != f.other.MachineID || b.Rev != 2 {
		t.Errorf("Bob's own setting: rev=%d %+v", b.Rev, b.Settings)
	}
	// The runtimes' own records were never written.
	if r := f.alice.Do("GET", "/api/v2/me?machine="+f.first.MachineID, nil); r.Code != 200 || strings.Contains(r.String(), "machine-owner") {
		t.Errorf("a machine named: %s", r)
	}
}

func TestPersonalSettingsConflictAndBadPatch(t *testing.T) {
	f := newSettingsFleet(t)
	if r := patchMe(f.alice, "/api/v2/me/settings", 1, map[string]any{"locale": "en"}); r.Code != 200 {
		t.Fatal(r)
	}
	r := patchMe(f.alice, "/api/v2/me/settings", 1, map[string]any{"locale": "ru"})
	if r.Code != 409 || !strings.Contains(r.String(), "conflict") {
		t.Errorf("a stale revision: %s", r)
	}
	if r := patchMe(f.alice, "/api/v2/me/settings", 2, map[string]any{"locale": "xx"}); r.Code != 400 || !strings.Contains(r.String(), "bad-request") {
		t.Errorf("a value out of its type: %s", r)
	}
	if m := readMe(t, f.alice, "/api/v2/me"); m.Rev != 2 || m.Settings.Locale != "en" {
		t.Errorf("a refused patch changed the record: %+v", m)
	}
}

func TestPersonalSettingsDefaultMachineNeedsAccessAndIsNoGrant(t *testing.T) {
	f := newSettingsFleet(t)
	// Carol belongs to no space of the machines.
	carol := f.s.SignIn("carol@example.com")
	own, _ := carol.NewSpace("Carol's")
	mine := carol.Machine(own, &wire.Server{Me: &wire.FileMe{Path: filepath.Join(t.TempDir(), "me.json")}})
	if r := patchMe(f.bob, "/api/v2/me/settings", 1, map[string]any{"defaultMachine": mine.MachineID}); r.Code != 400 {
		t.Errorf("a machine Bob cannot open: %s", r)
	}
	if r := patchMe(f.bob, "/api/v2/me/settings", 1, map[string]any{"defaultMachine": "m_nothing"}); r.Code != 400 {
		t.Errorf("a machine that is not there: %s", r)
	}
	if m := readMe(t, f.bob, "/api/v2/me"); m.Rev != 1 || m.Settings.DefaultMachine != nil {
		t.Errorf("a refused preference was saved: %+v", m)
	}
	if r := patchMe(f.bob, "/api/v2/me/settings", 1, map[string]any{"defaultMachine": nil}); r.Code != 200 {
		t.Errorf("clearing the main machine: %s", r)
	}

	// Bob picks a machine of the shared space, then loses the space: the preference stays, the access does not come back.
	if r := patchMe(f.bob, "/api/v2/me/settings", 2, map[string]any{"defaultMachine": f.first.MachineID}); r.Code != 200 {
		t.Fatalf("a machine Bob may open: %s", r)
	}
	if r := f.bob.Do("GET", "/api/v2/ping?machine="+f.first.MachineID, nil); r.Code != 200 {
		t.Fatalf("Bob reaches the machine: %s", r)
	}
	f.bob.NewSpace("Bob's own") // the fleet is on for Bob after he leaves the shared space
	if r := f.alice.Do("DELETE", "/api/spaces/"+f.space+"/members/"+f.bob.ID, nil); r.Code != 204 {
		t.Fatal(r)
	}
	m := readMe(t, f.bob, "/api/v2/me")
	if m.Settings.DefaultMachine == nil || *m.Settings.DefaultMachine != f.first.MachineID {
		t.Errorf("the stored preference: %+v", m.Settings)
	}
	for _, p := range []string{"/api/v2/ping?machine=" + f.first.MachineID, "/agents/api/v2/ping?machine=" + f.first.MachineID} {
		if r := f.bob.Do("GET", p, nil); r.Code == 200 {
			t.Errorf("the preference opened %s: %s", p, r)
		}
	}
}

func TestPersonalSettingsAreAnsweredByTheHubOnEveryRoute(t *testing.T) {
	f := newSettingsFleet(t)
	id := f.first.MachineID
	// Write a marker through the workspace route; every route reads it back and none shows the machine's record.
	if r := patchMe(f.alice, "/agents/api/v2/me/settings?machine="+f.other.MachineID, 1, map[string]any{"locale": "en"}); r.Code != 200 {
		t.Fatalf("workspace patch: %s", r)
	}
	for _, p := range []string{
		"/api/v2/me", "/api/v2/me/", "/api/v2/me?machine=" + id, "/api/v2/me?machine=" + f.other.MachineID,
		"/agents/api/v2/me", "/agents/api/v2/me?machine=" + id, "/m/" + id + "/api/v2/me", "/m/" + id + "/agents/api/v2/me",
	} {
		m := readMe(t, f.alice, p)
		if m.Email == nil || *m.Email != "alice@example.com" || m.Rev != 2 || m.Settings.Locale != "en" {
			t.Errorf("%s: %+v", p, m)
		}
		if m.Name != nil && *m.Name == "machine-owner" {
			t.Errorf("%s: the machine's name", p)
		}
	}
	for _, p := range []string{"/api/v2/me/settings?machine=" + id, "/m/" + id + "/api/v2/me/settings"} {
		cur := readMe(t, f.alice, "/api/v2/me")
		if r := patchMe(f.alice, p, cur.Rev, map[string]any{"enterSends": false}); r.Code != 200 {
			t.Errorf("%s: %s", p, r)
		}
	}
	if m := readMe(t, f.alice, "/api/v2/me"); m.Settings.EnterSends || m.Rev != 4 {
		t.Errorf("writes by the other routes: %+v", m)
	}
}

func TestPersonalSettingsAnswerWithTheMachinesOff(t *testing.T) {
	f := newSettingsFleet(t)
	f.first.Close()
	f.other.Close()
	m := readMe(t, f.bob, "/api/v2/me")
	if m.Email == nil || *m.Email != "bob@example.com" || m.Rev != 1 {
		t.Errorf("read with every machine off: %+v", m)
	}
	if r := patchMe(f.bob, "/api/v2/me/settings", 1, map[string]any{"textSize": "l"}); r.Code != 200 {
		t.Errorf("write with every machine off: %s", r)
	}
	if m := readMe(t, f.bob, "/agents/api/v2/me"); m.Settings.TextSize != "l" {
		t.Errorf("%+v", m)
	}
}
