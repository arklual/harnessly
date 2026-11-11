package selfupdate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// machine is three services that run from the fleet's current. A restarted service serves what current points to;
// its chats are starting for the first probes, restoring for the next ones, then ready.
type machine struct {
	f        *Fleet
	serves   map[string]string
	probes   map[string]int
	events   []string
	broken   map[string]string // service -> the version it never comes up with
	starting int               // probes a restarted service answers "starting" to
}

func newMachine(t *testing.T, names ...string) *machine {
	t.Helper()
	m := &machine{serves: map[string]string{}, probes: map[string]int{}, broken: map[string]string{}, starting: 2}
	m.f = &Fleet{Root: filepath.Join(t.TempDir(), "fleet"), PollEvery: time.Millisecond, HealthTimeout: 100 * time.Millisecond,
		SettleTimeout: 100 * time.Millisecond}
	m.f.Restart = func(context.Context) error { t.Error("all services were restarted at once"); return nil }
	m.f.RestartOne = func(_ context.Context, name string) error {
		m.events = append(m.events, "restart "+name)
		m.probes[name] = 0
		m.serves[name] = ""
		if r, err := ReadRelease(m.f.current()); err == nil && m.broken[name] != r.Version {
			m.serves[name] = r.Version
		}
		return nil
	}
	for _, name := range names {
		m.f.Services = append(m.f.Services, Service{Name: name, Probe: func(context.Context) (Health, error) {
			if m.serves[name] == "" {
				return Health{}, errors.New("no answer")
			}
			m.probes[name]++
			h := Health{Version: m.serves[name], Chats: ChatsReady}
			switch n := m.probes[name]; {
			case n <= m.starting:
				h.Chats = ChatsStarting
			case n <= m.starting+2:
				h.Chats = "restoring"
			}
			if last := "state " + name + " " + h.Chats; len(m.events) == 0 || m.events[len(m.events)-1] != last {
				m.events = append(m.events, last)
			}
			return h, nil
		}})
	}
	return m
}

func (m *machine) stage(t *testing.T, version, commit string) string {
	t.Helper()
	p, sha := tgz(t, version+".tgz", release(commit, version))
	if _, err := m.f.Stage(StageInput{Version: version, Tarball: p, SHA256: sha}); err != nil {
		t.Fatal(err)
	}
	return sha
}

func (m *machine) restarts() []string {
	var out []string
	for _, e := range m.events {
		if name, ok := strings.CutPrefix(e, "restart "); ok {
			out = append(out, name)
		}
	}
	return out
}

func TestFleetRestartsOneServiceAtATime(t *testing.T) {
	m := newMachine(t, "a", "b", "c")
	sha := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"restart a", "state a starting", "state a restoring", "state a ready",
		"restart b", "state b starting", "state b restoring", "state b ready",
		"restart c", "state c starting", "state c restoring", "state c ready",
	}
	if !slices.Equal(m.events, want) {
		t.Fatalf("the next service must wait for the chats of the one before it:\n%s", strings.Join(m.events, "\n"))
	}
	if st := m.f.Status(); st.Version != "1.0.0" {
		t.Fatalf("status %+v", st)
	}
}

func TestFleetServiceWithoutChatsIsJudgedByItsVersion(t *testing.T) {
	m := newMachine(t, "a", "b")
	for i := range m.f.Services {
		name := m.f.Services[i].Name
		m.f.Services[i].Probe = func(context.Context) (Health, error) {
			if m.serves[name] == "" {
				return Health{}, errors.New("no answer")
			}
			return Health{Version: m.serves[name]}, nil
		}
	}
	sha := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha); err != nil {
		t.Fatal(err)
	}
	if got := m.restarts(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("restarts %v", got)
	}
}

func TestFleetChatsThatNeverLoadFailTheRelease(t *testing.T) {
	m := newMachine(t, "a", "b")
	sha1 := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	sha2 := m.stage(t, "1.1.0", commitB)
	// The new release listens and never loads its conversations; the previous one starts as it always did.
	probe := m.f.Services[0].Probe
	m.f.Services[0].Probe = func(ctx context.Context) (Health, error) {
		h, err := probe(ctx)
		if err == nil && h.Version == "1.1.0" {
			h.Chats = ChatsStarting
		}
		return h, err
	}
	m.events = nil
	err := m.f.Activate(context.Background(), "1.1.0", sha2)
	if err == nil || !strings.Contains(err.Error(), "rolled back") || !strings.Contains(err.Error(), "chats are not loaded") {
		t.Fatalf("want a rollback for the chats, got %v", err)
	}
	if st := m.f.Status(); st.Version != "1.0.0" {
		t.Fatalf("the previous release must be back: %+v", st)
	}
}

func TestFleetFailedServiceRollsBackOnlyWhatWasRestarted(t *testing.T) {
	m := newMachine(t, "a", "b", "c")
	sha1 := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	sha2 := m.stage(t, "1.1.0", commitB)
	m.broken["b"] = "1.1.0"
	m.events = nil
	err := m.f.Activate(context.Background(), "1.1.0", sha2)
	if err == nil || !strings.Contains(err.Error(), "rolled back") || !strings.Contains(err.Error(), "b:") {
		t.Fatalf("want a rollback that names b, got %v", err)
	}
	// a and b took the new release and go back, in the same order; c was never touched.
	if got := m.restarts(); !slices.Equal(got, []string{"a", "b", "a", "b"}) {
		t.Fatalf("restarts %v", got)
	}
	if st := m.f.Status(); st.Version != "1.0.0" {
		t.Fatalf("the previous release must be back: %+v", st)
	}
	for _, name := range []string{"a", "b", "c"} {
		if m.serves[name] != "1.0.0" {
			t.Fatalf("%s serves %q after the rollback", name, m.serves[name])
		}
	}
	if _, err := os.Stat(filepath.Join(m.f.Root, "deployed.json")); err != nil {
		t.Fatal(err)
	}
}

func TestFleetSlowChatsDoNotFailTheRelease(t *testing.T) {
	m := newMachine(t, "a", "b")
	m.f.SettleTimeout = 5 * time.Millisecond
	var said []string
	m.f.Log = func(s string) { said = append(said, s) }
	probe := m.f.Services[0].Probe
	m.f.Services[0].Probe = func(ctx context.Context) (Health, error) {
		h, err := probe(ctx)
		if err == nil && h.Chats == ChatsReady {
			h.Chats = "restoring"
		}
		return h, err
	}
	sha := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha); err != nil {
		t.Fatal(err)
	}
	if got := m.restarts(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("restarts %v", got)
	}
	if !slices.ContainsFunc(said, func(s string) bool { return strings.Contains(s, "a still restores its chats") }) {
		t.Fatalf("the log must say the rollout went on: %v", said)
	}
}

func TestFleetWindowBetweenActivations(t *testing.T) {
	m := newMachine(t, "a")
	now := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)
	m.f.Now = func() time.Time { return now }
	m.f.MinGap = 24 * time.Hour
	sha1 := m.stage(t, "1.0.0", commitA)
	if err := m.f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatalf("the first release has nothing to wait for: %v", err)
	}
	sha2 := m.stage(t, "1.1.0", commitB)
	now = now.Add(30 * time.Minute)
	m.events = nil
	if err := m.f.Activate(context.Background(), "1.1.0", sha2); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("want ErrTooSoon, got %v", err)
	}
	if st := m.f.Status(); st.Version != "1.0.0" || len(m.restarts()) != 0 {
		t.Fatalf("a refused activation must change nothing: %+v %v", st, m.events)
	}
	// The release that is already active is checked, window or not.
	if err := m.f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	// A person's rollout goes through.
	m.f.Force = true
	if err := m.f.Activate(context.Background(), "1.1.0", sha2); err != nil {
		t.Fatal(err)
	}
	m.f.Force = false
	// And so does the next one once the window has passed.
	sha3 := m.stage(t, "1.2.0", commitA)
	now = now.Add(23 * time.Hour)
	if err := m.f.Activate(context.Background(), "1.2.0", sha3); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("23 h after the forced one: %v", err)
	}
	now = now.Add(2 * time.Hour)
	if err := m.f.Activate(context.Background(), "1.2.0", sha3); err != nil {
		t.Fatal(err)
	}
}

func TestFleetServicesFromEnv(t *testing.T) {
	marks := t.TempDir()
	env := map[string]string{
		"RUNTIME_FLEET_ROOT":           t.TempDir(),
		"RUNTIME_FLEET_SERVICES":       "one=http://127.0.0.1:1/api/health, two=http://127.0.0.1:2/api/health bad",
		"RUNTIME_FLEET_RESTART":        `touch "` + marks + `/$FLEET_SERVICE"`,
		"RUNTIME_FLEET_SETTLE_TIMEOUT": "7",
		"RUNTIME_FLEET_MIN_GAP":        "6h",
	}
	f := FleetFromEnv(func(k string) string { return env[k] }, nil)
	if len(f.Services) != 2 || f.Services[0].Name != "one" || f.Services[1].Name != "two" {
		t.Fatalf("services %+v", f.Services)
	}
	if f.SettleTimeout != 7*time.Second || f.MinGap != 6*time.Hour || f.RestartOne == nil {
		t.Fatalf("settings %v %v", f.SettleTimeout, f.MinGap)
	}
	if err := f.RestartOne(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(marks, "two")); err != nil {
		t.Fatalf("the command must get the service's name: %v", err)
	}
	for raw, want := range map[string]time.Duration{"": time.Hour, "0": 0, "off": 0, "90m": 90 * time.Minute, "soon": time.Hour, "-1h": time.Hour} {
		if got := GapFromEnv(raw, time.Hour); got != want {
			t.Errorf("GapFromEnv(%q) = %v, want %v", raw, got, want)
		}
	}
}
