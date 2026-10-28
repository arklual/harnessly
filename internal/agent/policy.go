package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/BurntSushi/toml"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// The local policy is the owner of the machine's say over what a hub may do here: a file the hub cannot write, read
// at start, on SIGHUP and when it changes. It is checked on the machine, at the moment of each operation, whatever the
// hub believes.
//
// "The hub cannot write" is only as good as the file's permissions. The runtime runs as the agent's account, and the
// hub reaches the runtime, whose routes write files (the editor's save among them). A policy file that account can
// write can therefore be rewritten by the hub, so it is trusted for what it takes away and not for what it gives back:
// while the agent runs, a change of such a file that allows more than the agent allowed is held until SIGHUP or a
// restart (see PolicyStore.Reload). A file that account cannot write (root-owned, in a folder it cannot change: the
// `dir` and `container` profiles) is the owner's alone, and every change of it applies at once.

// DefaultPolicy is what a machine without a policy file allows: everything.
func DefaultPolicy() hubproto.Policy {
	return hubproto.Policy{Terminal: true, AgentLogin: true, Keys: true, Env: true, Install: true, RuntimeSession: true,
		DenyRoutes: []string{}, Source: "default"}
}

// closedPolicy is what a policy file that cannot be understood means: nothing is allowed until it is fixed. A broken
// file must never open the machine up.
func closedPolicy(source string) hubproto.Policy {
	return hubproto.Policy{DenyRoutes: []string{}, Source: source}
}

type policyFile struct {
	Terminal       *bool    `toml:"terminal"`
	AgentLogin     *bool    `toml:"agent_login"`
	Keys           *bool    `toml:"keys"`
	Env            *bool    `toml:"env"`
	Install        *bool    `toml:"install"`
	RuntimeSession *bool    `toml:"runtime_session"`
	DenyRoutes     []string `toml:"deny_routes"`
}

const maxDenyRoutes = 128

// ParsePolicy reads the text of a policy.toml over the defaults. Unknown keys, values of the wrong type and routes that
// are not paths are errors: a typo must not pass for permission.
func ParsePolicy(text, source string) (hubproto.Policy, error) {
	var f policyFile
	md, err := toml.Decode(text, &f)
	if err != nil {
		return hubproto.Policy{}, err
	}
	if un := md.Undecoded(); len(un) > 0 {
		names := make([]string, len(un))
		for i, k := range un {
			names[i] = k.String()
		}
		return hubproto.Policy{}, fmt.Errorf("unknown setting %s", strings.Join(names, ", "))
	}
	p := DefaultPolicy()
	p.Source = source
	set := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	set(&p.Terminal, f.Terminal)
	set(&p.AgentLogin, f.AgentLogin)
	set(&p.Keys, f.Keys)
	set(&p.Env, f.Env)
	set(&p.Install, f.Install)
	set(&p.RuntimeSession, f.RuntimeSession)
	if len(f.DenyRoutes) > maxDenyRoutes {
		return hubproto.Policy{}, fmt.Errorf("deny_routes has more than %d entries", maxDenyRoutes)
	}
	for _, r := range f.DenyRoutes {
		if !strings.HasPrefix(r, "/") || strings.IndexFunc(r, func(c rune) bool { return unicode.IsSpace(c) || unicode.IsControl(c) }) >= 0 {
			return hubproto.Policy{}, fmt.Errorf("deny_routes entry %q is not a path prefix starting with a slash", r)
		}
	}
	p.DenyRoutes = append([]string{}, f.DenyRoutes...)
	return p, nil
}

// PolicyStore holds the current policy and the kill switch, and notices when either changes.
type PolicyStore struct {
	// explicit is AGENT_POLICY_FILE; empty means /etc/harnessly/policy.toml if there is one, else the account's.
	explicit string
	home     string
	dataDir  string
	// etc is the system folder (a field so tests can point it elsewhere).
	etc string

	// writable says whether the agent's account can change the policy file, or put one where it is looked for (a field so
	// tests can say it does not).
	writable func(path string) bool

	mu       sync.RWMutex
	cur      hubproto.Policy
	disabled bool
	sig      fileSig
	loaded   bool
	// base is the most the policy may allow without a SIGHUP or a restart while the file can be written by the agent's
	// account: what the last start or SIGHUP read, narrowed by every valid file read since. soft says the file in use
	// is such a file; held, that a change of it which allows more is waiting for one.
	base hubproto.Policy
	soft bool
	held bool
	// everSoft is soft as the last start or SIGHUP judged it, and sticky between them.
	everSoft bool
	// OnPolicy is called (outside the lock) when the policy changed; OnSwitch when the kill switch did.
	OnPolicy func(old, now hubproto.Policy)
	OnSwitch func(off bool)
}

// fileSig is what Reload compares to know the policy file changed: its text, not its mtime and size, since an edit that
// keeps the length ("true" for "fals") inside one clock tick would otherwise go unseen, and this file is the owner's
// brake. It is small and read once a second.
type fileSig struct {
	path    string
	exists  bool
	content string
	err     string
}

// maxPolicyBytes is the most of the file the agent reads; more is a file that is not a policy.
const maxPolicyBytes = 64 << 10

// NewPolicyStore starts with the defaults; Reload reads the file.
func NewPolicyStore(explicit, home, dataDir string) *PolicyStore {
	return &PolicyStore{explicit: explicit, home: home, dataDir: dataDir, etc: "/etc/harnessly", cur: DefaultPolicy(), base: DefaultPolicy(),
		writable: fileWritableByAgent}
}

// Soft says the policy file in use is one the agent's account can change, so a change of it that allows more waits for a
// SIGHUP or a restart. False for a file only root can change, and when there is no file.
func (s *PolicyStore) Soft() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.soft
}

// Held says a change of the file that allows more than the agent allows is waiting for a SIGHUP or a restart.
func (s *PolicyStore) Held() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.held
}

// ProtectedPaths are the files whose writing the route filter refuses to the hub: the policy file and the kill switch.
func (s *PolicyStore) ProtectedPaths() []string {
	return append([]string{s.Path()}, s.SwitchFiles()...)
}

// Path is the policy file in use: the explicit one, else the system one when it exists, else the account's.
func (s *PolicyStore) Path() string {
	if s.explicit != "" {
		return s.explicit
	}
	system := filepath.Join(s.etc, "policy.toml")
	if _, err := os.Stat(system); err == nil {
		return system
	}
	return filepath.Join(s.home, ".config", "harnessly", "policy.toml")
}

// SwitchFiles are the places a `disabled` file switches the agent off: next to the policy file (both candidates when
// the file is not named), and in the data folder.
func (s *PolicyStore) SwitchFiles() []string {
	var dirs []string
	if s.explicit != "" {
		dirs = []string{filepath.Dir(s.explicit)}
	} else {
		dirs = []string{s.etc, filepath.Join(s.home, ".config", "harnessly")}
	}
	dirs = append(dirs, s.dataDir)
	files := make([]string, 0, len(dirs))
	for _, d := range dirs {
		files = append(files, filepath.Join(d, "disabled"))
	}
	return files
}

// Current is the policy in force.
func (s *PolicyStore) Current() hubproto.Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p := s.cur
	p.DenyRoutes = append([]string{}, p.DenyRoutes...)
	return p
}

// Disabled says the kill switch is on.
func (s *PolicyStore) Disabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.disabled
}

func statSig(path string) fileSig {
	sig := fileSig{path: path}
	f, err := os.Open(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			sig.exists, sig.err = true, err.Error()
		}
		return sig
	}
	defer f.Close()
	sig.exists = true
	raw, err := io.ReadAll(io.LimitReader(f, maxPolicyBytes+1))
	switch {
	case err != nil:
		sig.err = err.Error()
	case len(raw) > maxPolicyBytes:
		sig.err = fmt.Sprintf("more than %d bytes", maxPolicyBytes)
	default:
		sig.content = string(raw)
	}
	return sig
}

// Reload reads the policy file when it changed since the last read (or always, with force), and looks at the kill
// switch. It reports what changed through OnPolicy and OnSwitch.
//
// force is the start and SIGHUP: what a person at the machine does. Without it, a file the agent's account can write
// (see the top of this file) is trusted only to narrow the policy: what it allows beyond the policy the agent runs with
// is held, and the next SIGHUP or restart applies it. A file with an error still closes everything at once, and mending
// it opens up to what the agent ran with, no further.
func (s *PolicyStore) Reload(force bool) {
	path := s.Path()
	sig := statSig(path)
	soft := s.writable(path)
	s.mu.Lock()
	// A file that was judged writable stays so until a SIGHUP or a restart looks again: a link or a folder that changes
	// under the agent's feet must not turn a writable file into a trusted one and let it widen the policy.
	if force || !s.loaded {
		s.everSoft = soft
	} else if s.everSoft {
		soft = true
	}
	first := !s.loaded
	changed := force || first || sig != s.sig
	var next hubproto.Policy
	if changed {
		var valid bool
		next, valid = s.read(path, sig)
		s.sig, s.loaded = sig, true
		switch {
		case force || first || !soft:
			// A person's word (start, SIGHUP), or a file nobody but root can write.
			s.base = next
			if !valid {
				s.base = DefaultPolicy()
				s.base.Source = next.Source
			}
			s.held = false
		case !valid:
			// Closed at once; the base stays, so that mending the file opens up to it.
			s.held = false
		default:
			if Narrowed(next, s.base) {
				Log("warn", "policy file can be written by the agent's account: what it allows beyond the running policy waits for SIGHUP or a restart",
					F{"file", path})
				s.held = true
			} else {
				s.held = false
			}
			m := meet(s.base, next)
			if !sig.exists {
				m.Source = s.base.Source
			}
			next, s.base = m, m
		}
	}
	s.soft = soft && sig.exists
	old := s.cur
	policyChanged := changed && !samePolicy(old, next)
	if policyChanged {
		s.cur = next
	}
	off := false
	for _, f := range s.SwitchFiles() {
		if _, err := os.Stat(f); err == nil {
			off = true
			break
		}
	}
	switchChanged := off != s.disabled
	s.disabled = off
	s.mu.Unlock()
	if policyChanged && s.OnPolicy != nil {
		s.OnPolicy(old, next)
	}
	if switchChanged && s.OnSwitch != nil {
		s.OnSwitch(off)
	}
}

// read is the policy a file means; valid is false for the closed policy of a file that cannot be read or understood.
func (s *PolicyStore) read(path string, sig fileSig) (p hubproto.Policy, valid bool) {
	if !sig.exists {
		return DefaultPolicy(), true
	}
	if sig.err != "" {
		Log("error", "policy file not readable, everything is switched off until it is", F{"file", path}, F{"error", sig.err})
		return closedPolicy(path), false
	}
	p, err := ParsePolicy(sig.content, path)
	if err != nil {
		Log("error", "policy file not understood, everything is switched off until it is fixed", F{"file", path}, F{"error", err.Error()})
		return closedPolicy(path), false
	}
	return p, true
}

func samePolicy(a, b hubproto.Policy) bool { return reflect.DeepEqual(a, b) }

// meet is what both policies allow: a capability only when both do, and every route either denies.
func meet(a, b hubproto.Policy) hubproto.Policy {
	out := hubproto.Policy{
		Terminal: a.Terminal && b.Terminal, AgentLogin: a.AgentLogin && b.AgentLogin, Keys: a.Keys && b.Keys, Env: a.Env && b.Env,
		Install: a.Install && b.Install, RuntimeSession: a.RuntimeSession && b.RuntimeSession, DenyRoutes: []string{}, Source: b.Source,
	}
	seen := map[string]bool{}
	for _, r := range append(append([]string{}, a.DenyRoutes...), b.DenyRoutes...) {
		if !seen[r] {
			seen[r] = true
			out.DenyRoutes = append(out.DenyRoutes, r)
		}
	}
	return out
}

// Narrowed says the new policy allows less than the old one did: something was switched off or a route denied.
func Narrowed(old, now hubproto.Policy) bool {
	if (old.Terminal && !now.Terminal) || (old.AgentLogin && !now.AgentLogin) || (old.Keys && !now.Keys) ||
		(old.Env && !now.Env) || (old.Install && !now.Install) || (old.RuntimeSession && !now.RuntimeSession) {
		return true
	}
	have := map[string]bool{}
	for _, r := range old.DenyRoutes {
		have[r] = true
	}
	for _, r := range now.DenyRoutes {
		if !have[r] {
			return true
		}
	}
	return false
}

// Watch looks at the file and the kill switch every d until stop closes.
func (s *PolicyStore) Watch(stop <-chan struct{}, d time.Duration) {
	t := time.NewTicker(d)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			s.Reload(false)
		}
	}
}
