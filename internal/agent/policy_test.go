package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestParsePolicy(t *testing.T) {
	all := DefaultPolicy()
	with := func(f func(p *hubproto.Policy)) hubproto.Policy {
		p := DefaultPolicy()
		p.Source = "x.toml"
		f(&p)
		return p
	}
	tests := []struct {
		name string
		text string
		want hubproto.Policy
		err  string
	}{
		{"empty is everything allowed", "", with(func(*hubproto.Policy) {}), ""},
		{"comments only", "# nothing here\n", with(func(*hubproto.Policy) {}), ""},
		{"terminal off", "terminal = false\n", with(func(p *hubproto.Policy) { p.Terminal = false }), ""},
		{"every switch", "terminal=false\nagent_login=false\nkeys=false\nenv=false\ninstall=false\nruntime_session=false\n",
			with(func(p *hubproto.Policy) {
				p.Terminal, p.AgentLogin, p.Keys, p.Env, p.Install, p.RuntimeSession = false, false, false, false, false, false
			}), ""},
		{"routes", `deny_routes = ["/api/terminal", "/api/fs/write"]`, with(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/api/terminal", "/api/fs/write"} }), ""},
		{"an explicit true stays true", "keys = true\n", with(func(*hubproto.Policy) {}), ""},
		{"unknown key", "termnial = false\n", hubproto.Policy{}, "unknown setting termnial"},
		{"wrong type", "terminal = \"no\"\n", hubproto.Policy{}, "terminal"},
		{"not toml", "terminal = = false", hubproto.Policy{}, ""},
		{"route without a slash", `deny_routes = ["api/terminal"]`, hubproto.Policy{}, "starting with a slash"},
		{"route with a space", `deny_routes = ["/api/x y"]`, hubproto.Policy{}, "starting with a slash"},
		{"routes of the wrong type", `deny_routes = "/api"`, hubproto.Policy{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePolicy(tc.text, "x.toml")
			if (err != nil) != (tc.err != "" || tc.want.Source == "") {
				t.Fatalf("err %v", err)
			}
			if err != nil {
				if !strings.Contains(err.Error(), tc.err) {
					t.Errorf("error %q lacks %q", err, tc.err)
				}
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
	if !all.Terminal || !all.AgentLogin || !all.Keys || !all.Env || !all.Install || !all.RuntimeSession || all.Source != "default" || all.DenyRoutes == nil {
		t.Errorf("defaults %+v", all)
	}
}

func TestNarrowed(t *testing.T) {
	d := DefaultPolicy()
	off := func(f func(*hubproto.Policy)) hubproto.Policy { p := DefaultPolicy(); f(&p); return p }
	for name, tc := range map[string]struct {
		old, now hubproto.Policy
		want     bool
	}{
		"same":                  {d, d, false},
		"terminal off":          {d, off(func(p *hubproto.Policy) { p.Terminal = false }), true},
		"install off":           {d, off(func(p *hubproto.Policy) { p.Install = false }), true},
		"a route denied":        {d, off(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/api/x"} }), true},
		"a route allowed again": {off(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/api/x"} }), d, false},
		"widened":               {off(func(p *hubproto.Policy) { p.Keys = false }), d, false},
		"another route added":   {off(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/a"} }), off(func(p *hubproto.Policy) { p.DenyRoutes = []string{"/a", "/b"} }), true},
	} {
		if got := Narrowed(tc.old, tc.now); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

type policyRecorder struct {
	mu       sync.Mutex
	policies []hubproto.Policy
	switches []bool
}

func (r *policyRecorder) attach(s *PolicyStore) {
	s.OnPolicy = func(_, now hubproto.Policy) { r.mu.Lock(); r.policies = append(r.policies, now); r.mu.Unlock() }
	s.OnSwitch = func(off bool) { r.mu.Lock(); r.switches = append(r.switches, off); r.mu.Unlock() }
}

// rootOwned and writableByAgent stand in for the policy file's permissions: a test's temp folder belongs to the account
// that runs it, so on its own every file there is one the agent could write.
func rootOwned(string) bool       { return false }
func writableByAgent(string) bool { return true }

func newTrustedStore(file, dir string) *PolicyStore {
	s := NewPolicyStore(file, dir, filepath.Join(dir, "data"))
	s.writable = rootOwned
	return s
}

func newSoftStore(file, dir string) *PolicyStore {
	s := NewPolicyStore(file, dir, filepath.Join(dir, "data"))
	s.writable = writableByAgent
	return s
}

func TestPolicyStoreReload(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newTrustedStore(file, dir)
	var rec policyRecorder
	rec.attach(s)

	// No file: the defaults, and nothing to report.
	s.Reload(false)
	if p := s.Current(); p.Source != "default" || !p.Terminal {
		t.Fatalf("%+v", p)
	}
	if len(rec.policies) != 0 {
		t.Errorf("reported %v", rec.policies)
	}

	writeFile(t, file, "terminal = false\ndeny_routes = [\"/api/fs/write\"]\n")
	s.Reload(false)
	p := s.Current()
	if p.Terminal || p.Source != file || !reflect.DeepEqual(p.DenyRoutes, []string{"/api/fs/write"}) || len(rec.policies) != 1 {
		t.Fatalf("%+v %v", p, rec.policies)
	}

	// The same file is not read again, nor reported.
	s.Reload(false)
	if len(rec.policies) != 1 {
		t.Errorf("reported again: %d", len(rec.policies))
	}

	// A file nobody can understand closes the machine rather than opening it.
	writeFile(t, file, "terminal = fales\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.AgentLogin || p.Keys || p.Env || p.Install || p.RuntimeSession || p.Source != file {
		t.Fatalf("a broken file left something open: %+v", p)
	}

	// Mended, it opens as written.
	writeFile(t, file, "keys = false\n")
	s.Reload(false)
	if p := s.Current(); !p.Terminal || p.Keys {
		t.Fatalf("%+v", p)
	}

	// Removed: the defaults.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	s.Reload(false)
	if p := s.Current(); p.Source != "default" || !p.Keys {
		t.Fatalf("%+v", p)
	}

	// The copy handed out cannot change the store.
	cur := s.Current()
	cur.DenyRoutes = append(cur.DenyRoutes, "/x")
	cur.Terminal = false
	if !s.Current().Terminal {
		t.Error("the store was changed through a copy")
	}
}

func TestPolicyFileChoice(t *testing.T) {
	home, etc := t.TempDir(), t.TempDir()
	s := NewPolicyStore("", home, filepath.Join(home, "data"))
	s.etc = etc
	account := filepath.Join(home, ".config", "harnessly", "policy.toml")
	if got := s.Path(); got != account {
		t.Errorf("without a system file: %s", got)
	}
	writeFile(t, filepath.Join(etc, "policy.toml"), "terminal = false\n")
	if got := s.Path(); got != filepath.Join(etc, "policy.toml") {
		t.Errorf("with a system file: %s", got)
	}
	// And an explicit file beats both.
	e := NewPolicyStore("/somewhere/else.toml", home, home)
	e.etc = etc
	if e.Path() != "/somewhere/else.toml" {
		t.Errorf("%s", e.Path())
	}
	s.Reload(false)
	if s.Current().Terminal {
		t.Error("the system policy was not read")
	}
}

func TestKillSwitch(t *testing.T) {
	home, etc := t.TempDir(), t.TempDir()
	data := filepath.Join(home, "data")
	s := NewPolicyStore("", home, data)
	s.etc = etc
	var rec policyRecorder
	rec.attach(s)
	s.Reload(false)
	if s.Disabled() {
		t.Fatal("off from the start")
	}

	// Next to the system policy, even before there is a policy file.
	writeFile(t, filepath.Join(etc, "disabled"), "")
	s.Reload(false)
	if !s.Disabled() || !reflect.DeepEqual(rec.switches, []bool{true}) {
		t.Fatalf("%v %v", s.Disabled(), rec.switches)
	}
	s.Reload(false)
	if len(rec.switches) != 1 {
		t.Errorf("reported again: %v", rec.switches)
	}
	if err := os.Remove(filepath.Join(etc, "disabled")); err != nil {
		t.Fatal(err)
	}
	s.Reload(false)
	if s.Disabled() || !reflect.DeepEqual(rec.switches, []bool{true, false}) {
		t.Fatalf("%v %v", s.Disabled(), rec.switches)
	}

	// In the data folder; and in the account's folder.
	writeFile(t, filepath.Join(data, "disabled"), "")
	s.Reload(false)
	if !s.Disabled() {
		t.Fatal("the data folder's switch is ignored")
	}
	_ = os.Remove(filepath.Join(data, "disabled"))
	writeFile(t, filepath.Join(home, ".config", "harnessly", "disabled"), "")
	s.Reload(false)
	if !s.Disabled() {
		t.Fatal("the account's switch is ignored")
	}

	// A named policy file moves the places: its folder and the data folder only.
	named := NewPolicyStore(filepath.Join(home, "named", "p.toml"), home, data)
	if got := named.SwitchFiles(); !reflect.DeepEqual(got, []string{filepath.Join(home, "named", "disabled"), filepath.Join(data, "disabled")}) {
		t.Errorf("%v", got)
	}
}

func TestAuditHasNoSecretsAndIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "agent-audit.log")
	a := NewAudit(path)
	a.Record("key.deliver", true, map[string]any{"provider": "claude", "op": "spoofed", "ok": "no"})
	a.Record("env.remove", false, map[string]any{"name": "TOKEN"})
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, st)
	}
	raw, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"op":"key.deliver"`) || !strings.Contains(lines[0], `"ok":true`) || strings.Contains(lines[0], "spoofed") {
		t.Errorf("%q", raw)
	}
	var nilAudit *Audit
	nilAudit.Record("x", true, nil) // a missing log is not a crash
}

// An edit that keeps the file's size, saved inside the same clock tick, is still an edit.
func TestPolicyStoreSeesAnEditOfTheSameSizeAndTime(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newTrustedStore(file, dir)
	writeFile(t, file, "keys = false\nenv = true\n")
	s.Reload(false)
	if p := s.Current(); p.Keys || !p.Env {
		t.Fatalf("%+v", p)
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, file, "keys = true\nenv = false\n")
	if err := os.Chtimes(file, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	s.Reload(false)
	if p := s.Current(); !p.Keys || p.Env {
		t.Fatalf("the edit was not seen: %+v", p)
	}
}

func TestPolicyStoreRefusesAFileThatIsNotAPolicy(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := NewPolicyStore(file, dir, filepath.Join(dir, "data"))
	writeFile(t, file, "# "+strings.Repeat("x", maxPolicyBytes)+"\nterminal = true\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.Keys || p.Source != file {
		t.Fatalf("a huge file left something open: %+v", p)
	}
}

// A policy file the agent's account can write is trusted for what it takes away and not for what it gives back: the
// hub reaches the runtime, and the runtime's routes write files.
func TestSoftPolicyOnlyNarrowsWhileTheAgentRuns(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newSoftStore(file, dir)
	var rec policyRecorder
	rec.attach(s)
	writeFile(t, file, "keys = false\nenv = false\n")
	s.Reload(true) // the start: what the owner wrote is the policy
	if p := s.Current(); p.Keys || p.Env || !p.Terminal {
		t.Fatalf("%+v", p)
	}
	if !s.Soft() || s.Held() {
		t.Fatalf("soft %v, held %v", s.Soft(), s.Held())
	}

	// The hub, through the editor, writes a file that allows more: nothing changes, and it is said to be waiting.
	writeFile(t, file, "terminal = true\nkeys = true\nenv = true\n")
	s.Reload(false)
	if p := s.Current(); p.Keys || p.Env {
		t.Fatalf("a widening was taken at once: %+v", p)
	}
	if !s.Held() {
		t.Error("the held widening was not noticed")
	}
	// It stays held on every look, as the file does not change.
	s.Reload(false)
	if p := s.Current(); p.Keys || p.Env {
		t.Fatalf("%+v", p)
	}

	// A mix that allows some more and some less takes only the less.
	writeFile(t, file, "terminal = false\nkeys = true\nenv = false\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.Keys || p.Env {
		t.Fatalf("a mixed file: %+v", p)
	}

	// Narrowing is taken at once, and what was narrowed stays narrowed while the file is widened again.
	writeFile(t, file, "terminal = true\nkeys = true\nenv = true\ninstall = false\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.Keys || p.Env || p.Install {
		t.Fatalf("%+v", p)
	}

	// A SIGHUP or a restart is the owner's word: it applies the file as it is.
	s.Reload(true)
	if p := s.Current(); !p.Terminal || !p.Keys || !p.Env || p.Install || s.Held() {
		t.Fatalf("after the SIGHUP: %+v held=%v", p, s.Held())
	}
	if last := rec.policies[len(rec.policies)-1]; !last.Terminal {
		t.Errorf("the change was not reported: %+v", last)
	}
}

func TestSoftPolicyDenyRoutesOnlyGrow(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newSoftStore(file, dir)
	writeFile(t, file, "deny_routes = [\"/api/a\"]\n")
	s.Reload(true)
	writeFile(t, file, "deny_routes = [\"/api/b\"]\n")
	s.Reload(false)
	if got := s.Current().DenyRoutes; !reflect.DeepEqual(got, []string{"/api/a", "/api/b"}) {
		t.Fatalf("a route came off the list: %v", got)
	}
	writeFile(t, file, "deny_routes = []\n")
	s.Reload(false)
	if got := s.Current().DenyRoutes; !reflect.DeepEqual(got, []string{"/api/a", "/api/b"}) {
		t.Fatalf("an empty list opened the routes: %v", got)
	}
	s.Reload(true)
	if got := s.Current().DenyRoutes; len(got) != 0 {
		t.Fatalf("after the SIGHUP: %v", got)
	}
}

func TestSoftPolicyBrokenFileClosesAtOnceAndMendingOpensOnlyToTheBase(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newSoftStore(file, dir)
	writeFile(t, file, "terminal = false\n")
	s.Reload(true)

	writeFile(t, file, "terminal = fales\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.AgentLogin || p.Keys || p.Env || p.Install || p.RuntimeSession {
		t.Fatalf("a broken file left something open: %+v", p)
	}

	// Mended, but into a file that allows the terminal: the terminal stays off, everything the base allowed is back.
	writeFile(t, file, "terminal = true\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || !p.Keys || !p.Env || !p.Install || !p.AgentLogin || !p.RuntimeSession {
		t.Fatalf("%+v", p)
	}
	if !s.Held() {
		t.Error("the widening was not marked as held")
	}
	s.Reload(true)
	if p := s.Current(); !p.Terminal || s.Held() {
		t.Fatalf("%+v", p)
	}
}

func TestSoftPolicyBrokenAtStartIsClosedUntilTheOwnerMendsIt(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newSoftStore(file, dir)
	writeFile(t, file, "terminal = fales\n")
	s.Reload(true)
	if p := s.Current(); p.Terminal || p.Keys {
		t.Fatalf("%+v", p)
	}
	// Mended while running: the owner's file is read as it is, up to what the agent may be given by default.
	writeFile(t, file, "keys = false\n")
	s.Reload(false)
	if p := s.Current(); !p.Terminal || p.Keys {
		t.Fatalf("%+v", p)
	}
}

func TestSoftPolicyDeletedFileDoesNotWiden(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newSoftStore(file, dir)
	writeFile(t, file, "terminal = false\n")
	s.Reload(true)
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	s.Reload(false)
	if p := s.Current(); p.Terminal {
		t.Fatalf("a deleted file opened the terminal: %+v", p)
	}
	if p := s.Current(); p.Source != file {
		t.Errorf("source %q", p.Source)
	}
	// Written back with less, it narrows.
	writeFile(t, file, "terminal = false\nkeys = false\n")
	s.Reload(false)
	if p := s.Current(); p.Terminal || p.Keys {
		t.Fatalf("%+v", p)
	}
	// The owner's SIGHUP over a missing file is the defaults.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	s.Reload(true)
	if p := s.Current(); !p.Terminal || !p.Keys || p.Source != "default" {
		t.Fatalf("%+v", p)
	}
}

func TestTrustedPolicyAppliesEveryChangeAtOnce(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newTrustedStore(file, dir)
	writeFile(t, file, "terminal = false\n")
	s.Reload(true)
	if s.Soft() {
		t.Error("a file nobody but root writes is not soft")
	}
	writeFile(t, file, "terminal = true\n")
	s.Reload(false)
	if p := s.Current(); !p.Terminal || s.Held() {
		t.Fatalf("%+v", p)
	}
}

func TestSoftIsFalseWithoutAFile(t *testing.T) {
	dir := t.TempDir()
	s := newSoftStore(filepath.Join(dir, "policy.toml"), dir)
	s.Reload(true)
	if s.Soft() {
		t.Error("there is no file to be soft")
	}
}

func TestMeet(t *testing.T) {
	a := DefaultPolicy()
	a.Terminal = false
	a.DenyRoutes = []string{"/x", "/y"}
	b := DefaultPolicy()
	b.Keys = false
	b.DenyRoutes = []string{"/y", "/z"}
	b.Source = "file"
	m := meet(a, b)
	if m.Terminal || m.Keys || !m.Env || !m.Install || !m.AgentLogin || !m.RuntimeSession || m.Source != "file" {
		t.Fatalf("%+v", m)
	}
	if !reflect.DeepEqual(m.DenyRoutes, []string{"/x", "/y", "/z"}) {
		t.Fatalf("%v", m.DenyRoutes)
	}
	if Narrowed(m, a) || Narrowed(m, b) {
		t.Error("the meet allows more than one of its sides")
	}
}

func TestProtectedPathsAreThePolicyAndTheKillSwitch(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	s := newTrustedStore(file, dir)
	got := s.ProtectedPaths()
	if len(got) < 2 || got[0] != file {
		t.Fatalf("%v", got)
	}
	for _, g := range got[1:] {
		if filepath.Base(g) != "disabled" {
			t.Errorf("%q is not a kill switch file", g)
		}
	}
}

func TestFileWritableByAgent(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "policy.toml")
	// The account's own file in its own folder: the agent can write it, there or not.
	if !fileWritableByAgent(file) {
		t.Error("a file that is not there, in a folder the account owns, is writable")
	}
	writeFile(t, file, "terminal = false\n")
	if !fileWritableByAgent(file) {
		t.Error("an own file is writable")
	}
	// Read-only, but in a folder the account owns, it is replaced.
	if err := os.Chmod(file, 0o400); err != nil {
		t.Fatal(err)
	}
	if !fileWritableByAgent(file) {
		t.Error("a read-only file in an own folder is still replaced")
	}
	// A link to it is no different.
	link := filepath.Join(dir, "link.toml")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if !fileWritableByAgent(link) {
		t.Error("a link to an own file")
	}
	// Folder unknown: writable, as it cannot be shown otherwise.
	if !fileWritableByAgent(filepath.Join(dir, "nope", "deeper", "policy.toml")) {
		t.Error("a path that cannot be looked at must count as writable")
	}

	if os.Geteuid() == 0 {
		t.Skip("root writes everything")
	}
	// The system's own file is not.
	if fileWritableByAgent("/etc/passwd") {
		t.Error("/etc/passwd is writable by an unprivileged account")
	}
	if fileWritableByAgent("/etc/harnessly/policy.toml") {
		t.Error("a file that would be made in /etc is writable by an unprivileged account")
	}
	// A link from an own folder to a file that is the system's is a name in an own folder: the account can point it
	// elsewhere, so it is not trusted (see policytrust_link_test.go).
	sys := filepath.Join(dir, "sys.toml")
	if err := os.Symlink("/etc/passwd", sys); err != nil {
		t.Fatal(err)
	}
	if !fileWritableByAgent(sys) {
		t.Error("a link in an own folder is writable whatever it points at")
	}
}

func TestShielded(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is never shielded")
	}
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skip("no /tmp")
	}
	// Somebody else's file in a sticky folder cannot be replaced; the account's own can.
	if !shielded("/tmp", "/etc/passwd") {
		t.Error("another account's file in /tmp is shielded")
	}
	dir := t.TempDir()
	own := filepath.Join(dir, "own")
	writeFile(t, own, "x")
	if shielded(filepath.Dir(dir), own) && shielded(dir, own) {
		t.Error("an own file is never shielded")
	}
	if shielded(dir, own) {
		t.Error("an own folder without the sticky bit shields nothing")
	}
}
