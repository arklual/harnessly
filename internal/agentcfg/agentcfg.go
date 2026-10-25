// Package agentcfg reads and writes the settings files of the agent CLIs on this machine for the Settings screen of the
// new interface: Claude Code's settings.json and Codex's config.toml. It is wire.AgentSettingsSource.
//
// The files are the person's and the CLIs' own: a write changes only the keys the patch names, keeps the order of the
// rest, keeps keys nobody here models, and is refused (a conflict) when the file is not the one the page read. A file
// that does not read is never overwritten. Secrets never leave: the account is a method, a plan and a name, an
// environment value that looks like a secret is cut to its tail.
package agentcfg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ly-solutions/harnessly/internal/wire"
)

// Config says where the CLIs keep their files; the runtime passes the folders it already uses for them.
type Config struct {
	// Home is the person's home; it is shortened to ~ in the paths the page sees.
	Home string
	// ClaudeDir is $CLAUDE_CONFIG_DIR, else ~/.claude; CodexDir is $CODEX_HOME, else ~/.codex.
	ClaudeDir, CodexDir string
	// ClaudeJSON is the file Claude Code keeps its account and servers in: <ClaudeDir>/.claude.json when
	// $CLAUDE_CONFIG_DIR is set, else ~/.claude.json.
	ClaudeJSON string
	// Getenv reads the environment the agents start in (os.Getenv when nil).
	Getenv func(string) string
}

// Store serves the files of one machine.
type Store struct {
	cfg Config
	mu  sync.Mutex
}

var _ wire.AgentSettingsSource = (*Store)(nil)

// New is the store of a machine.
func New(cfg Config) *Store {
	if cfg.Getenv == nil {
		cfg.Getenv = os.Getenv
	}
	if cfg.ClaudeJSON == "" {
		cfg.ClaudeJSON = filepath.Join(cfg.Home, ".claude.json")
	}
	return &Store{cfg: cfg}
}

func (s *Store) file(p wire.Provider) (string, error) {
	switch p {
	case wire.ProviderClaude:
		return filepath.Join(s.cfg.ClaudeDir, "settings.json"), nil
	case wire.ProviderCodex:
		return filepath.Join(s.cfg.CodexDir, "config.toml"), nil
	}
	return "", fmt.Errorf("agentcfg: no settings of %q", p)
}

// display is a path with the home shortened to ~.
func (s *Store) display(path string) string {
	if s.cfg.Home != "" && (path == s.cfg.Home || strings.HasPrefix(path, s.cfg.Home+string(filepath.Separator))) {
		return "~" + strings.TrimPrefix(path, s.cfg.Home)
	}
	return path
}

// revOf is the revision of a file's bytes; a file that does not exist is the revision of nothing.
func revOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

// readFile is a file's bytes; a missing file is empty, which the CLI reads as no settings.
func readFile(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

// writeFile replaces a file with the bytes through a temporary file beside it, keeping the file's mode (0600 for a new
// one); a symbolic link is followed, so a settings file kept in a dotfiles repository stays a link.
func writeFile(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".harnessly-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(mode)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, target)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}

// AgentSettings reads the settings of one CLI.
func (s *Store) AgentSettings(_ context.Context, p wire.Provider) (wire.AgentSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch p {
	case wire.ProviderClaude:
		return s.readClaude()
	case wire.ProviderCodex:
		return s.readCodex()
	}
	return wire.AgentSettings{}, fmt.Errorf("agentcfg: no settings of %q", p)
}

// PatchAgentSettings changes the keys a patch names, if the file is still the one the page read.
func (s *Store) PatchAgentSettings(_ context.Context, p wire.Provider, rev string, patch wire.AgentSettingsPatch) (wire.AgentSettings, error) {
	if err := patch.ValidateFor(p); err != nil {
		return wire.AgentSettings{}, fmt.Errorf("%w: %v", wire.ErrAgentSettingsRefused, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch p {
	case wire.ProviderClaude:
		return s.patchClaude(rev, patch)
	case wire.ProviderCodex:
		return s.patchCodex(rev, patch)
	}
	return wire.AgentSettings{}, fmt.Errorf("agentcfg: no settings of %q", p)
}

// options lists every option of a CLI in the page's order with the value the file holds (nil while it holds none).
func options(p wire.Provider, values map[wire.AgentOptionKey]any) []wire.AgentOption {
	specs := wire.AgentOptionSpecs(p)
	out := make([]wire.AgentOption, len(specs))
	for i, spec := range specs {
		v := values[spec.Key]
		if spec.CheckRead(v) != nil {
			v = nil
		}
		out[i] = wire.AgentOption{Key: spec.Key, Value: v}
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// tail hides a secret: bullets and its last four characters.
func tail(v string) string {
	r := []rune(v)
	if len(r) <= 4 {
		return "••••"
	}
	return "••••" + string(r[len(r)-4:])
}

// title is a plan as a name: "max" is "Max".
func title(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	return strings.ToUpper(v[:1]) + v[1:]
}
