// Package cliversion is the runtime's contract with the agents' CLIs: which versions its screen reading was checked
// against, the version each installed CLI has now, and whether that one is among the checked.
//
// The runtime drives an agent through its terminal screen, and a new version of the CLI may draw that screen another
// way. The checked versions are those with reference screens in internal/typist/testdata/tui; a version outside them
// is not refused — it is reported, and the typist works in its cautious mode for it.
package cliversion

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Providers as the wire names them.
const (
	Claude = "claude"
	Codex  = "codex"
)

// Range is one row of the table: the versions From..To of a provider's CLI (both ends included) count as checked
// because Screens — the versions with reference screens — lie in it, its two ends among them.
type Range struct {
	Provider string
	From, To string
	Screens  []string
}

// checked is the one table. A reference screen is a file testdata/tui/<provider>-<version>-<name> of the typist; its
// test fails when a version there has no row here, or a row names a version without screens.
var checked = []Range{
	{Provider: Claude, From: "2.1.296", To: "2.1.322",
		Screens: []string{"2.1.296", "2.1.310", "2.1.311", "2.1.312", "2.1.316", "2.1.322"}},
}

// Table is the rows of the table.
func Table() []Range { return append([]Range(nil), checked...) }

// parse reads x.y.z; ok is false for anything else.
func parse(v string) (n [3]int, ok bool) {
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) != 3 {
		return n, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 {
			return n, false
		}
		n[i] = x
	}
	return n, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Verdict is how a version stands against the table.
type Verdict string

const (
	// Checked: the version is inside a row.
	Checked Verdict = "checked"
	// Unchecked: the provider has rows and the version is in none of them, or cannot be read as x.y.z.
	Unchecked Verdict = "unchecked"
	// NoContract: the provider has no rows (no versioned reference screens): nothing is said about its versions.
	NoContract Verdict = "none"
)

// Check is the verdict on one version of a provider's CLI.
func Check(provider, version string) Verdict { return checkIn(checked, provider, version) }

func checkIn(table []Range, provider, version string) Verdict {
	v, ok := parse(version)
	rows := false
	for _, r := range table {
		if r.Provider != provider {
			continue
		}
		rows = true
		from, okFrom := parse(r.From)
		to, okTo := parse(r.To)
		if ok && okFrom && okTo && !less(v, from) && !less(to, v) {
			return Checked
		}
	}
	if !rows {
		return NoContract
	}
	return Unchecked
}

// --- the version of a binary, read once per file ---

type binKey struct {
	path  string
	mtime time.Time
	size  int64
}

var bins = struct {
	sync.Mutex
	m map[string]struct {
		key     binKey
		version string
	}
}{m: map[string]struct {
	key     binKey
	version string
}{}}

// Of is the version of the CLI at path: probe (which starts the CLI) runs only when the file behind the path — its
// links followed — is another one than last time, by its place, size and time of change. A probe that fails is not
// kept, so the next call tries again.
func Of(path string, probe func() (string, error)) (string, error) {
	key, ok := keyOf(path)
	if ok {
		bins.Lock()
		c, hit := bins.m[path]
		bins.Unlock()
		if hit && c.key == key {
			return c.version, nil
		}
	}
	v, err := probe()
	if err != nil {
		return "", err
	}
	if ok {
		bins.Lock()
		bins.m[path] = struct {
			key     binKey
			version string
		}{key, v}
		bins.Unlock()
	}
	return v, nil
}

func keyOf(path string) (binKey, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return binKey{}, false
	}
	st, err := os.Stat(real)
	if err != nil || st.IsDir() {
		return binKey{}, false
	}
	return binKey{path: real, mtime: st.ModTime(), size: st.Size()}, true
}

// --- what the machine runs now ---

// State is one provider's CLI as last seen on the machine.
type State struct {
	Provider string  `json:"provider"`
	Version  string  `json:"version"`
	Verdict  Verdict `json:"verdict"`
}

var seen = struct {
	sync.Mutex
	now    map[string]State
	logged map[string]bool // provider + " " + version: said once
	log    func(provider, version string)
}{now: map[string]State{}, logged: map[string]bool{}}

// OnUnchecked sets what is told of an unchecked version, once per version for the life of the process.
func OnUnchecked(f func(provider, version string)) {
	seen.Lock()
	seen.log = f
	seen.Unlock()
}

// Note records the version a provider's CLI has now (a launch and the machine's preflight read it) and returns its
// verdict. An empty version — the CLI is gone or did not answer — keeps what was known.
func Note(provider, version string) Verdict {
	if version == "" {
		return NoContract
	}
	v := Check(provider, version)
	seen.Lock()
	seen.now[provider] = State{Provider: provider, Version: version, Verdict: v}
	key := provider + " " + version
	tell := v == Unchecked && !seen.logged[key] && seen.log != nil
	if tell {
		seen.logged[key] = true
	}
	f := seen.log
	seen.Unlock()
	if tell {
		f(provider, version)
	}
	return v
}

// Cautious says whether the provider's CLI on this machine is of an unchecked version. Before any version was read
// it is false: the runtime does not know less than it did.
func Cautious(provider string) bool {
	seen.Lock()
	defer seen.Unlock()
	return seen.now[provider].Verdict == Unchecked
}

// Seen is the CLIs noted so far, by provider.
func Seen() []State {
	seen.Lock()
	defer seen.Unlock()
	out := make([]State, 0, len(seen.now))
	for _, s := range seen.now {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// Reset forgets what was noted and said; for tests.
func Reset() {
	seen.Lock()
	seen.now, seen.logged, seen.log = map[string]State{}, map[string]bool{}, nil
	seen.Unlock()
	bins.Lock()
	clear(bins.m)
	bins.Unlock()
}
