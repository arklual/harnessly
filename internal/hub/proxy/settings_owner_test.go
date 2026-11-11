package proxy_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/proxy"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// sharedMachine is two users in one space and one machine they share, whose runtime has a record somebody changed
// before the hub kept settings.
type sharedMachine struct {
	s          *hubtest.Stack
	alice, bob *hubtest.Person
	machine    *hubtest.Agent
	me         *wire.FileMe
	engine     string
}

func newSharedMachine(t *testing.T, legacy string) *sharedMachine {
	t.Helper()
	f := &sharedMachine{s: hubtest.New(t, fleetOn, withWorkspace)}
	f.alice, f.bob = f.s.SignIn("alice@example.com"), f.s.SignIn("bob@example.com")
	space, _ := f.alice.NewSpace("Team")
	f.alice.Join(space, f.bob)
	dir := t.TempDir()
	f.me = &wire.FileMe{Path: filepath.Join(dir, "me.json"), Name: "machine-owner"}
	if legacy != "" {
		if _, err := f.me.Patch(1, json.RawMessage(legacy)); err != nil {
			t.Fatal(err)
		}
	}
	f.machine = f.alice.Machine(space, underItsAddress(&wire.Server{
		Me:              f.me,
		RuntimeSettings: &wire.FileRuntimeSettings{Path: filepath.Join(dir, "runtime-settings.json")},
		Voice: func(_ context.Context, r io.Reader, _, engine string) (string, error) {
			_, _ = io.ReadAll(r)
			f.engine = engine
			return "открой синопия", nil
		},
	}))
	return f
}

// underItsAddress serves the runtime's API under the machine's own address too (/m/<id>/api/v2/…), as a runtime that
// knows its base does.
func underItsAddress(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if i := strings.Index(r.URL.Path, "/api/v2/"); i > 0 && strings.HasPrefix(r.URL.Path, "/m/") {
			r = r.Clone(r.Context())
			r.URL.Path = r.URL.Path[i:]
		}
		h.ServeHTTP(w, r)
	})
}

// A person without a record in the hub starts from what the machine kept; on a shared machine each person gets the
// same start and they part from there. The hub's record is the truth afterwards: the machine's file changing, or the
// machine going away, changes nothing.
func TestPersonalSettingsAreTakenOverFromTheMachineOnce(t *testing.T) {
	f := newSharedMachine(t, `{"locale":"en","textSize":"l","quickPrompts":["review this"]}`)
	a := readMe(t, f.alice, "/api/v2/me")
	if a.Settings.Locale != "en" || a.Settings.TextSize != "l" || len(a.Settings.QuickPrompts) != 1 || a.Rev != 2 {
		t.Fatalf("Alice starts from the machine's record: rev=%d %+v", a.Rev, a.Settings)
	}
	if a.Email == nil || *a.Email != "alice@example.com" || (a.Name != nil && *a.Name == "machine-owner") {
		t.Errorf("identity is the hub user's: %v %v", a.Email, a.Name)
	}
	b := readMe(t, f.bob, "/api/v2/me")
	if b.Settings.Locale != "en" || b.Settings.TextSize != "l" || b.Rev != 2 {
		t.Fatalf("Bob starts from the same record: rev=%d %+v", b.Rev, b.Settings)
	}
	// They part: Bob's change is Bob's.
	if r := patchMe(f.bob, "/api/v2/me/settings", b.Rev, map[string]any{"textSize": "s"}); r.Code != 200 {
		t.Fatalf("bob patch: %s", r)
	}
	if a := readMe(t, f.alice, "/api/v2/me"); a.Settings.TextSize != "l" || a.Rev != 2 {
		t.Errorf("Bob's write changed Alice's settings: rev=%d %+v", a.Rev, a.Settings)
	}
	// The machine's file is no longer read.
	cur, _ := f.me.Me()
	if _, err := f.me.Patch(cur.Rev, json.RawMessage(`{"locale":"ru","textSize":"m"}`)); err != nil {
		t.Fatal(err)
	}
	if a := readMe(t, f.alice, "/api/v2/me"); a.Settings.Locale != "en" || a.Rev != 2 {
		t.Errorf("the machine's later record reached Alice: rev=%d %+v", a.Rev, a.Settings)
	}
	f.machine.Close()
	if b := readMe(t, f.bob, "/api/v2/me"); b.Settings.Locale != "en" || b.Settings.TextSize != "s" || b.Rev != 3 {
		t.Errorf("Bob with the machine off: rev=%d %+v", b.Rev, b.Settings)
	}
}

// A machine nobody changed settings on gives nothing to take over: the account starts at the defaults, revision 1.
func TestPersonalSettingsOfAnUnchangedMachineAreTheDefaults(t *testing.T) {
	f := newSharedMachine(t, "")
	if a := readMe(t, f.alice, "/api/v2/me"); a.Rev != 1 || a.Settings.Locale != "ru" {
		t.Errorf("rev=%d %+v", a.Rev, a.Settings)
	}
}

// The person is the one who signed in: no name of another user in the address or the body moves a read or a write.
func TestPersonalSettingsAreOnlyTheSignedInPersons(t *testing.T) {
	f := newSharedMachine(t, "")
	q := "?user_id=" + f.alice.ID + "&user=" + f.alice.ID + "&userId=" + f.alice.ID
	body, _ := json.Marshal(map[string]any{"rev": 1, "patch": map[string]any{"locale": "en"}, "user_id": f.alice.ID, "userId": f.alice.ID})
	if r := f.bob.Do("PATCH", "/api/v2/me/settings"+q, json.RawMessage(body), "X-User-Id", f.alice.ID); r.Code != 200 {
		t.Fatalf("bob patch: %s", r)
	}
	if a := readMe(t, f.alice, "/api/v2/me"); a.Rev != 1 || a.Settings.Locale != "ru" {
		t.Errorf("Bob wrote Alice's settings: rev=%d %+v", a.Rev, a.Settings)
	}
	b := readMe(t, f.bob, "/api/v2/me"+q)
	if b.Email == nil || *b.Email != "bob@example.com" || b.Settings.Locale != "en" || b.Rev != 2 {
		t.Errorf("Bob reads with Alice's id in the address: %+v", b)
	}
	// A key that is not a setting refuses the patch.
	if r := patchMe(f.bob, "/api/v2/me/settings", 2, map[string]any{"user_id": f.alice.ID}); r.Code != 400 {
		t.Errorf("a patch of the user: %s", r)
	}
}

// The runtime recognizes speech by the settings of the person who asks, not by its machine-wide record, and nothing a
// browser sends in the hub's header reaches it.
func TestRuntimeGetsTheAskingPersonsSettings(t *testing.T) {
	f := newSharedMachine(t, "")
	voice := map[string]any{"voice": map[string]any{"engine": "openai", "words": []map[string]any{{"word": "Sinopia", "heard": []string{"синопия"}}}}}
	if r := patchMe(f.alice, "/api/v2/me/settings", 1, voice); r.Code != 200 {
		t.Fatalf("alice patch: %s", r)
	}
	say := func(p *hubtest.Person, path string, headers ...string) string {
		t.Helper()
		// A recording is not JSON: the page marks its own upload, as it does for every file it sends.
		r := p.Do("POST", path, []byte("a recording"), append([]string{"X-Harnessly-Client", "web"}, headers...)...)
		if r.Code != 200 {
			t.Fatalf("%s: %s", path, r)
		}
		var got wire.Transcript
		r.JSON(&got)
		return got.Text
	}
	id := f.machine.MachineID
	for _, path := range []string{"/api/v2/voice?machine=" + id, "/agents/api/v2/voice?machine=" + id, "/m/" + id + "/api/v2/voice"} {
		if text := say(f.alice, path); text != "открой Sinopia" || f.engine != "openai" {
			t.Errorf("%s as Alice: %q by %q", path, text, f.engine)
		}
		if text := say(f.bob, path); text != "открой синопия" || f.engine != "local" {
			t.Errorf("%s as Bob: %q by %q", path, text, f.engine)
		}
	}
	forged := wire.EncodePersonHeader(wire.RuntimePerson{Voice: wire.VoiceSettings{Engine: wire.VoiceEngineYandex,
		Words: []wire.VoiceWord{{Word: "Forged", Heard: []string{"синопия"}}}}})
	if text := say(f.bob, "/api/v2/voice?machine="+id, wire.PersonHeader, forged); text != "открой синопия" || f.engine != "local" {
		t.Errorf("a header of the browser's reached the runtime: %q by %q", text, f.engine)
	}
}

// The machine's own settings are the machine's: the hub passes them on, keeps what the machine said and answers a
// read from that copy while the machine is away. A write waits for the machine.
func TestMachineSettingsAreReadFromTheHubsCopyWhileTheMachineIsAway(t *testing.T) {
	f := newSharedMachine(t, "")
	path := "/api/v2/runtime-settings?machine=" + f.machine.MachineID + "&caps=delegation,workflows,profiles"
	if r := f.alice.Do("PUT", path, map[string]any{"idleStop": "6h", "delegation": map[string]any{"maxChildren": 3, "anyChat": false}}); r.Code != 200 {
		t.Fatalf("write: %s", r)
	}
	live := f.bob.Do("GET", path, nil)
	if live.Code != 200 || live.Header.Get(proxy.SettingsCopyHeader) != "" {
		t.Fatalf("the machine's own answer: %s %v", live, live.Header)
	}
	f.machine.Close()
	kept := f.bob.Do("GET", path, nil)
	var got wire.RuntimeSettings
	kept.JSON(&got)
	if kept.Code != 200 || kept.Header.Get(proxy.SettingsCopyHeader) == "" || got.IdleStop != wire.IdleStop6h || got.Delegation == nil || got.Delegation.MaxChildren != 3 {
		t.Fatalf("the copy: %s %+v", kept, got)
	}
	// A page that does not read the delegation gets the copy without it, as from the machine.
	narrow := f.bob.Do("GET", "/agents/api/v2/runtime-settings?machine="+f.machine.MachineID, nil)
	if narrow.Code != 200 || strings.Contains(narrow.String(), "delegation") {
		t.Errorf("the copy for an older page: %s", narrow)
	}
	if r := f.alice.Do("PUT", path, map[string]any{"idleStop": "1h"}); r.Code == 200 {
		t.Errorf("a write with the machine off: %s", r)
	}
	// The CLI's settings are not copied: they wait for the machine.
	if r := f.alice.Do("GET", "/api/v2/agent-settings/claude?machine="+f.machine.MachineID, nil); r.Code == 200 {
		t.Errorf("agent settings with the machine off: %s", r)
	}
}
