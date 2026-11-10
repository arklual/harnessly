package filestore

import (
	"path"
	"strings"
)

// Profile is a person's Claude Code profile, rooted at their HOME: what makes Claude theirs on any
// machine — settings, skills, agents, commands, rules, their CLAUDE.md — and nothing that belongs to the machine (the
// subscription login, account state, transcripts, caches, plugin installs: each machine installs the plugins the
// settings enable) or that is a secret. Tokens and logins (.gitconfig with its credential helpers, glab's config,
// ~/.config/<app>) never go into the store: they live in the hub's safe (docs/safe.md), and the settings' secret
// environment variables stay on the machine (filesync.ProfileCodec).
type Profile struct{}

// profileFiles are single files that sync.
var profileFiles = map[string]bool{
	".claude/settings.json":    true,
	".claude/CLAUDE.md":        true,
	".claude/keybindings.json": true,
}

// profileTrees are folders whose files sync.
var profileTrees = []string{
	".claude/skills/",
	".claude/agents/",
	".claude/commands/",
	".claude/rules/",
	".claude/output-styles/",
	".claude/hooks/",
}

// ProfileSecretPaths are paths design-sync's profile synced and the store's profile never does: secrets, which the hub's
// safe keeps. A revision of one of them in an old room stays there and never reaches a machine again.
var ProfileSecretPaths = []string{".gitconfig", ".config/glab-cli/"} // and every RUNTIME_SECRET_CONFIG_DIRS folder: .config is never walked

// secretName is a file name inside a tree that holds a secret by its look: never synced.
func secretName(name string) bool {
	n := strings.ToLower(name)
	switch {
	case n == ".env" || strings.HasPrefix(n, ".env.") || strings.HasPrefix(n, ".credentials"):
		return true
	case strings.HasSuffix(n, ".pem") || strings.HasSuffix(n, ".key") || strings.HasSuffix(n, ".p12") ||
		strings.HasSuffix(n, ".pfx") || strings.HasPrefix(n, "id_rsa") || strings.HasPrefix(n, "id_ed25519"):
		return true
	}
	return false
}

func (Profile) Synced(rel string) bool {
	if !SafeRel(rel) {
		return false
	}
	name := path.Base(rel)
	if scratch.MatchString(name) || name == ".DS_Store" || strings.HasPrefix(name, ".sync-") || secretName(name) {
		return false
	}
	if profileFiles[rel] {
		return true
	}
	// Skills Claude pulls from the claude.ai account the machine is logged in with: they belong to that login.
	if strings.HasPrefix(rel, ".claude/skills/synced/") {
		return false
	}
	for _, tree := range profileTrees {
		if strings.HasPrefix(rel, tree) {
			for _, part := range strings.Split(strings.TrimPrefix(rel, tree), "/") {
				if part == ".git" || part == "node_modules" || part == "__pycache__" {
					return false
				}
			}
			return true
		}
	}
	return false
}

func (Profile) SkippedDir(rel string) bool {
	if rel == "." || rel == "" {
		return false
	}
	if base := path.Base(rel); base == ".git" || base == "node_modules" || base == "__pycache__" || rel == ".claude/skills/synced" {
		return true
	}
	dir := rel + "/"
	for _, tree := range profileTrees {
		// Inside a tree, or above one (".claude"): worth entering.
		if strings.HasPrefix(dir, tree) || strings.HasPrefix(tree, dir) {
			return false
		}
	}
	for file := range profileFiles {
		if strings.HasPrefix(file, dir) {
			return false
		}
	}
	return true
}
