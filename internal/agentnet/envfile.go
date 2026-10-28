// Package agentnet is what the machine agent does with the network and with the agent CLIs' environment: the env files
// their launches source, the proxy around them (with a local HTTP bridge for SOCKS), and the check that a provider's
// servers answer.
package agentnet

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/agents"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// Scopes of an env file: every agent, or one.
const (
	ScopeAll    = agents.EnvScopeAll
	ScopeClaude = agents.Claude
	ScopeCodex  = agents.Codex
)

// ValidScope is all, claude or codex.
func ValidScope(s string) bool { return s == ScopeAll || s == ScopeClaude || s == ScopeCodex }

const envHeader = "# Written by harnessly-agent. A launch of the agent CLIs sources this file; edits are overwritten.\n"

// EnvDir is the agent's folder of env files, kept 0700 with 0600 files. Safe for concurrent use.
type EnvDir struct {
	Dir string
	mu  sync.Mutex
}

// NewEnvDir is the env files in dir.
func NewEnvDir(dir string) *EnvDir { return &EnvDir{Dir: dir} }

// File is the path of a scope's file.
func (e *EnvDir) File(scope string) string { return agents.EnvFile(e.Dir, scope) }

// Quote is s as one single-quoted shell word: a quote inside becomes '\”.
func Quote(s string) string { return agents.ShellQuote(s) }

type envEntry struct{ name, value string }

// parseEnv reads the KEY='value' entries of a file this package wrote. A line it cannot read is dropped.
func parseEnv(raw string) []envEntry {
	var out []envEntry
	for i := 0; i < len(raw); {
		line := raw[i:]
		if end := strings.IndexByte(line, '\n'); end >= 0 {
			line = line[:end]
		}
		name, _, ok := strings.Cut(line, "=")
		if !ok || strings.HasPrefix(line, "#") || !validEntryName(name) {
			i += len(line) + 1
			continue
		}
		// The value may span lines inside its quotes.
		value, next, ok := unquote(raw, i+len(name)+1)
		if !ok {
			i += len(line) + 1
			continue
		}
		out = append(out, envEntry{name, value})
		i = next
	}
	return out
}

// unquote reads a shell word made of 'single-quoted' parts and \' escapes, from s[start:], up to the newline that ends
// the entry. It returns the value and the index after that newline.
func unquote(s string, start int) (string, int, bool) {
	var b strings.Builder
	i := start
	for i < len(s) {
		switch s[i] {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "", 0, false
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 2
		case '\\':
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				i += 2
				continue
			}
			return "", 0, false
		case '\n':
			return b.String(), i + 1, true
		default:
			return "", 0, false
		}
	}
	return b.String(), len(s), true
}

var (
	envNameRe   = hubproto.EnvNameRe
	proxyNameRe = regexp.MustCompile(`^(?i:https?_proxy|no_proxy|all_proxy)$`)
)

// validEntryName: a name the files may hold, a secret's or one of the proxy's.
func validEntryName(name string) bool {
	return envNameRe.MatchString(name) || proxyNameRe.MatchString(name)
}

func (e *EnvDir) read(scope string) ([]envEntry, error) {
	raw, err := os.ReadFile(e.File(scope))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseEnv(string(raw)), nil
}

func (e *EnvDir) write(scope string, entries []envEntry) error {
	if err := os.MkdirAll(e.Dir, 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(envHeader)
	for _, en := range entries {
		b.WriteString(en.name + "=" + Quote(en.value) + "\n")
	}
	file := e.File(scope)
	tmp := fmt.Sprintf("%s.%d.tmp", file, os.Getpid())
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, file); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Set writes the variables into the scope's file, replacing ones of the same name. Names are the caller's to check
// (CheckName); a value may not hold a NUL.
func (e *EnvDir) Set(scope string, vars map[string]string) error {
	if !ValidScope(scope) {
		return fmt.Errorf("unknown scope %q", scope)
	}
	for name, v := range vars {
		if strings.ContainsRune(v, 0) || !validEntryName(name) {
			return fmt.Errorf("variable %q cannot be written", name)
		}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	entries, err := e.read(scope)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(vars))
	for name := range vars {
		names = append(names, name)
	}
	sort.Strings(names)
	kept := entries[:0:0]
	for _, en := range entries {
		if _, replaced := vars[en.name]; !replaced {
			kept = append(kept, en)
		}
	}
	for _, name := range names {
		kept = append(kept, envEntry{name, vars[name]})
	}
	return e.write(scope, kept)
}

// Unset removes the named variables; the file goes when nothing is left.
func (e *EnvDir) Unset(scope string, names ...string) error {
	if !ValidScope(scope) {
		return fmt.Errorf("unknown scope %q", scope)
	}
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	entries, err := e.read(scope)
	if err != nil {
		return err
	}
	kept := entries[:0:0]
	for _, en := range entries {
		if !drop[en.name] {
			kept = append(kept, en)
		}
	}
	if len(kept) == len(entries) {
		return nil
	}
	if len(kept) == 0 {
		if err := os.Remove(e.File(scope)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return e.write(scope, kept)
}

// Get is a variable of a scope's file.
func (e *EnvDir) Get(scope, name string) (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	entries, _ := e.read(scope)
	for _, en := range entries {
		if en.name == name {
			return en.value, true
		}
	}
	return "", false
}

// Names are the variables of a scope's file that skip does not hide, sorted.
func (e *EnvDir) Names(scope string, skip func(name string) bool) []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	entries, _ := e.read(scope)
	names := []string{}
	for _, en := range entries {
		if skip == nil || !skip(en.name) {
			names = append(names, en.name)
		}
	}
	sort.Strings(names)
	return names
}

// Merged is the variables a launch would have from the files: the shared scope's, then the provider's over them. Only
// the agent's own probes use it (a key check), and the values never leave the process.
func (e *EnvDir) Merged(provider string) map[string]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := map[string]string{}
	for _, scope := range []string{ScopeAll, provider} {
		entries, _ := e.read(scope)
		for _, en := range entries {
			out[en.name] = en.value
		}
	}
	return out
}

// CheckName: a name `env.deliver` accepts (docs/hub/api.md). The rule is hubproto's, so the hub applies the same one
// before it relays; the error says why not.
func CheckName(name string) error { return hubproto.CheckEnvName(name) }

// CheckRemove: a name `env.remove` accepts. Any well-formed name may go (a name the rules of today would refuse can only
// be in the file from an earlier version), except the proxy variables, which the proxy settings own.
func CheckRemove(name string) error {
	if !envNameRe.MatchString(name) {
		return errors.New("a variable name is capital letters, digits and _, up to 64, not starting with a digit")
	}
	if IsProxyName(name) {
		return fmt.Errorf("%s belongs to the proxy settings", name)
	}
	return nil
}

// IsProxyName: a variable the proxy writes, which the secrets list does not show.
func IsProxyName(name string) bool { return proxyNameRe.MatchString(name) }
