package apps

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// What the wall takes from the agent besides the apps: whatever would let it pass for the person, or have its code run
// outside the wall later. The runtime's data folder is shut by default and opened by name. The person's start-up files
// are read-only, also those not there yet, which are made empty first: a file the agent could create is as good as one
// it could change, and a bind needs something to bind.
//
// This cannot be the whole list of programs that read a file of the person's and act on it. It is the ones that run at a
// login, a new terminal, or in another agent's chat without the person doing anything else.

// Seal says what to seal.
type Seal struct {
	// Data is the runtime's data folder, Home the person's; "" leaves that part out.
	Data, Home string
}

// What the agent's launch and CLI read from the data folder. Everything else there reads as nothing: the password and the
// key sessions are signed with (config.json), the safe (secrets/), the machine's token (agent.json), the runtime's state,
// and the scripts and settings other chats and the tmux server run, which the agent must not be able to change.
var (
	dataReadonly = []string{"port", "bin", "agent-skills", "status-hook.sh", "statusline.sh", "codex-hook.sh", "codex-launcher.js",
		"hooks.json", "launch-prompts", "prompt-handoff", "providers", "uploads", "images", "fs-uploads", "design-library-root"}
	// Sinopia's projects, the chat's own folder when it works in one.
	dataWritable = []string{"design"}
	// Under each workspace's folder only what a launch names.
	dataWorkspaceFiles = []string{"claude-prompt.md", "codex-prompt.md"}
)

func (s *Seal) dataArgs(bound []string) []string {
	if s == nil || s.Data == "" {
		return nil
	}
	data, err := realPath(s.Data)
	if err != nil {
		return nil
	}
	args := []string{"--tmpfs", data}
	open := func(flag, rel string) {
		p := filepath.Join(data, rel)
		if _, err := os.Stat(p); err == nil && !slices.ContainsFunc(bound, func(d string) bool { return within(d, p) }) {
			args = append(args, flag, p, p)
		}
	}
	for _, rel := range dataWritable {
		open("--bind", rel)
	}
	for _, rel := range dataReadonly {
		open("--ro-bind", rel)
	}
	for _, name := range dataWorkspaceFiles {
		found, _ := filepath.Glob(filepath.Join(data, "workspaces", "*", name))
		slices.Sort(found)
		for _, p := range found {
			args = append(args, "--ro-bind", p, p)
		}
	}
	return args
}

// The person's start-up files and folders, relative to the home folder.
var (
	homeFiles = []string{
		// Shells at a login or a new terminal. .bash_login is left to startupBashProfile: bash reads only the first it finds.
		".bashrc", ".bash_logout", ".profile", ".zshrc", ".zshenv", ".zprofile", ".zlogin", ".zlogout",
		// tmux, git and npm run what these name; vim runs them when the person opens it in a terminal.
		".tmux.conf", ".gitconfig", ".npmrc", ".vimrc",
		// What the person's other chats take as their settings and instructions.
		".claude/CLAUDE.md", ".codex/config.toml", ".codex/AGENTS.md",
	}
	homeJSONFiles = []string{".claude/settings.json", ".claude/settings.local.json", ".codex/hooks.json"}
	homeFolders   = []string{
		// A login key added here, and ssh's own config and rc.
		".ssh",
		// Units the person's systemd starts, and what a desktop or a login reads.
		".config/systemd", ".config/autostart", ".config/environment.d",
		".config/git", ".config/tmux", ".config/fish", ".config/nvim", ".vim",
		".claude/hooks", ".claude/skills", ".claude/agents", ".claude/commands", ".claude/plugins",
		".codex/skills", ".codex/prompts",
	}
	// Under a read-only folder, what its program must go on writing.
	homeWritableFiles = []string{".ssh/known_hosts"}
)

func (s *Seal) homeArgs(bound []string) []string {
	if s == nil || s.Home == "" {
		return nil
	}
	var args []string
	ro := func(p string) {
		if slices.ContainsFunc(bound, func(d string) bool { return within(d, p) }) {
			return
		}
		if real, err := realPath(p); err == nil {
			args = append(args, "--ro-bind", real, real)
		}
	}
	made := func(p string, err error) {
		if err != nil {
			// Left writable: the chat still starts, and the log says what the seal did not cover.
			jsonlog.Log("warn", "apps: a start-up file could not be sealed", jsonlog.F{K: "path", V: p}, jsonlog.F{K: "error", V: err.Error()})
			return
		}
		ro(p)
	}
	for _, rel := range homeFolders {
		p := filepath.Join(s.Home, rel)
		made(p, ensureFolder(p))
	}
	for _, rel := range homeFiles {
		p := filepath.Join(s.Home, rel)
		made(p, ensureFile(p, ""))
	}
	for _, rel := range homeJSONFiles {
		p := filepath.Join(s.Home, rel)
		made(p, ensureFile(p, "{}\n"))
	}
	p, err := startupBashProfile(s.Home)
	made(p, err)
	for _, rel := range homeWritableFiles {
		if p := filepath.Join(s.Home, rel); isRegular(p) {
			args = append(args, "--bind", p, p)
		}
	}
	// A program of the same name in a folder on the PATH runs in place of the real one, outside the wall too.
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if clean := filepath.Clean(dir); filepath.IsAbs(dir) && within(s.Home, clean) && clean != filepath.Clean(s.Home) {
			if info, err := os.Stat(clean); err == nil && info.IsDir() {
				ro(clean)
			}
		}
	}
	return dedupeBinds(args)
}

// startupBashProfile is the first file a login bash reads, made if there is none: it then reads the one it read before,
// so nothing changes for the person, and the agent has no file left to create ahead of it.
func startupBashProfile(home string) (string, error) {
	p := filepath.Join(home, ".bash_profile")
	if isRegular(p) {
		return p, nil
	}
	next := ".profile"
	if isRegular(filepath.Join(home, ".bash_login")) {
		next = ".bash_login"
	}
	return p, ensureFile(p, fmt.Sprintf("[ -f \"$HOME/%s\" ] && . \"$HOME/%s\"\n", next, next))
}

func ensureFolder(p string) error {
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.MkdirAll(p, 0o700)
}

func ensureFile(p, body string) error {
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(body)
	return err
}

func isRegular(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// dedupeBinds drops a bind that repeats an earlier one (the same folder twice on the PATH), keeping the order.
func dedupeBinds(args []string) []string {
	seen := map[string]bool{}
	var out []string
	for i := 0; i+2 < len(args); i += 3 {
		key := strings.Join(args[i:i+3], "\x00")
		if !seen[key] {
			seen[key] = true
			out = append(out, args[i:i+3]...)
		}
	}
	return out
}

// socketMask takes away the servers that run what they are asked as someone else: a container started through the
// Docker or Podman socket can mount the whole machine.
func socketMask() []string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	var out, seen []string
	for _, p := range []string{"/var/run/docker.sock", "/run/podman/podman.sock", filepath.Join(dir, "docker.sock"), filepath.Join(dir, "podman", "podman.sock")} {
		real, err := filepath.EvalSymlinks(p)
		if err != nil || slices.Contains(seen, real) {
			continue
		}
		if info, err := os.Stat(real); err == nil && info.Mode()&os.ModeSocket != 0 {
			seen = append(seen, real)
			out = append(out, "--ro-bind", "/dev/null", real)
		}
	}
	return out
}
