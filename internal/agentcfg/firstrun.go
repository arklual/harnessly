package agentcfg

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/ly-solutions/harnessly/internal/jsvalue"
	"github.com/ly-solutions/harnessly/internal/wire"
)

// A conversation the platform starts has nobody at the terminal to answer the CLI's first-run screens: Claude Code's
// theme, "press Enter", "do you trust this folder" and the bypass-mode warning, Codex's "trust this folder". FirstRun
// answers them in the CLIs' own files before the agent starts, so the agent opens straight at the prompt. It only adds
// what is missing (and the auto-mode offer, see claudeOnboarded): a choice the person made (another theme, a folder marked untrusted in Codex) stays theirs.

// FirstRun prepares the CLI of provider p to start in cwd without its first-run screens.
func (s *Store) FirstRun(p wire.Provider, cwd string) error {
	dirs := trustDirs(cwd)
	switch p {
	case wire.ProviderClaude:
		return errors.Join(s.claudeSkipBypassPrompt(), s.claudeOnboarded(dirs))
	case wire.ProviderCodex:
		return s.codexTrust(dirs)
	}
	return nil
}

// trustDirs are the folders a CLI asks about for cwd: cwd as given and resolved, and the repository it is in (Claude
// Code looks for the trust no higher than the repository's root, Codex trusts a repository by its root).
func trustDirs(cwd string) []string {
	if cwd == "" || !filepath.IsAbs(cwd) {
		return nil
	}
	cwd = filepath.Clean(cwd)
	real := cwd
	if r, err := filepath.EvalSymlinks(cwd); err == nil {
		real = r
	}
	return Unique(cwd, real, repoRoot(cwd), repoRoot(real))
}

// repoRoot is the nearest folder from dir up that holds .git (a folder, or a file in a worktree), "" when none does.
func repoRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
}

// claudeSkipBypassPrompt puts skipDangerousModePermissionPrompt in the user's settings.json. Without it Claude Code
// shows its bypass warning on a start in bypass mode, and downgrades a bypassPermissions defaultMode to default.
func (s *Store) claudeSkipBypassPrompt() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _ := s.file(wire.ProviderClaude)
	raw, err := readFile(path)
	if err != nil {
		return err
	}
	obj, err := jsonObject(raw)
	if err != nil {
		return nil // a file that does not read is the person's to fix, never written over
	}
	if v, _ := dig(obj, "skipDangerousModePermissionPrompt"); v == true {
		return nil
	}
	put(obj, true, "skipDangerousModePermissionPrompt")
	return writeFile(path, append(jsvalue.Stringify(obj, "  "), '\n'))
}

// claudeOnboarded marks Claude Code's onboarding done (a theme chosen when there is none) and the folders trusted in
// its .claude.json. Claude Code rewrites that file all the time under a lock of its own (a folder <file>.lock), so the
// write takes the same lock and reads the file again under it.
func (s *Store) claudeOnboarded(dirs []string) error {
	path := s.cfg.ClaudeJSON
	unlock, err := lockDir(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	raw, err := readFile(path)
	if err != nil {
		return err
	}
	obj, err := jsonObject(raw)
	if err != nil {
		return nil
	}
	changed := false
	if v, _ := dig(obj, "hasCompletedOnboarding"); v != true {
		put(obj, true, "hasCompletedOnboarding")
		changed = true
	}
	if _, ok := dig(obj, "theme"); !ok {
		put(obj, "dark", "theme")
		changed = true
	}
	// With a defaultMode in settings.json Claude Code offers to make auto mode the default in a dialog the platform's
	// first Enter would answer "yes", changing the person's settings.
	// A false the CLI wrote itself shows the dialog too, so anything but true is made true. Its notice of the same
	// offer goes with it.
	for _, key := range []string{"hasSeenAutoDefaultNudge", "hasSeenAutoDefaultNotice"} {
		if v, _ := dig(obj, key); v != true {
			put(obj, true, key)
			changed = true
		}
	}
	projects, _ := dig(obj, "projects")
	if _, ok := projects.(*jsvalue.Object); !ok && projects != nil {
		return nil // not the shape Claude Code writes: left alone
	}
	for _, d := range dirs {
		if v, _ := dig(obj, "projects", d, "hasTrustDialogAccepted"); v != true {
			if p, ok := dig(obj, "projects", d); ok {
				if _, isObj := p.(*jsvalue.Object); !isObj {
					continue
				}
			}
			put(obj, true, "projects", d, "hasTrustDialogAccepted")
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeFile(path, append(jsvalue.Stringify(obj, "  "), '\n'))
}

// lockDir takes a lock the way Claude Code's proper-lockfile does, by making a folder; one older than its stale time
// was left by a process that died and is taken over.
func lockDir(dir string) (func(), error) {
	const stale = 10 * time.Second
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return func() { _ = os.Remove(dir) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, serr := os.Stat(dir); serr == nil && time.Since(info.ModTime()) > stale {
			_ = os.Remove(dir)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("agentcfg: " + dir + " is held")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// codexTrust marks the folders trusted in Codex's config.toml; a folder the person gave another trust_level keeps it.
func (s *Store) codexTrust(dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	path, _ := s.file(wire.ProviderCodex)
	raw, err := readFile(path)
	if err != nil {
		return err
	}
	text := string(raw)
	before, err := tomlMap(text)
	if err != nil {
		return nil
	}
	doc := parseTOMLText(text)
	want, _ := tomlMap(text)
	added := false
	for _, d := range dirs {
		if _, ok := tomlDig(before, []string{"projects", d}, "trust_level"); ok {
			continue
		}
		if p, ok := tomlDig(before, []string{"projects"}, d); ok {
			if _, isTable := p.(map[string]any); !isTable {
				continue
			}
		}
		doc.set([]string{"projects", d}, "trust_level", tomlQuote("trusted"))
		setIn(want, []string{"projects", d}, "trust_level", "trusted", true)
		added = true
	}
	if !added {
		return nil
	}
	out := doc.String()
	got, err := tomlMap(out)
	if err != nil || !reflect.DeepEqual(got, want) {
		return errors.New("agentcfg: the trust would not write cleanly into " + s.display(path))
	}
	return writeFile(path, []byte(out))
}
