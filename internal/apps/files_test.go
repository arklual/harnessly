package apps

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// workspaceWith makes a workspace and a folder beside it that stands for everything the app must not reach.
func workspaceWith(t *testing.T) (ws, outside string) {
	t.Helper()
	base := t.TempDir()
	ws = filepath.Join(base, "ws")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{"docs/sub", "src", ".git", ".ssh", filepath.Join("..", "outside")} {
		if err := os.MkdirAll(filepath.Join(ws, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"docs/a.md": "a", "docs/sub/b.md": "b", "src/main.go": "package main", ".env": "TOKEN=1", ".env.local": "TOKEN=2",
		".git/config": "[remote]", "docs/server.key": "k", ".ssh/id_rsa": "k",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("out"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws, outside
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func TestFileScopeResolve(t *testing.T) {
	ws, outside := workspaceWith(t)
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "cli-token"), []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Links in the workspace: out of it, to a secret, to a place the masks do not name, to a missing target, and a good
	// one inside the mask.
	link(t, outside, filepath.Join(ws, "docs", "out"))
	link(t, filepath.Join(outside, "secret.txt"), filepath.Join(ws, "docs", "out.txt"))
	link(t, filepath.Join(ws, ".env"), filepath.Join(ws, "docs", "env.md"))
	link(t, filepath.Join(ws, "src", "main.go"), filepath.Join(ws, "docs", "main.md"))
	link(t, filepath.Join(ws, ".git"), filepath.Join(ws, "docs", "g"))
	link(t, filepath.Join(ws, "nowhere"), filepath.Join(ws, "docs", "dangling"))
	link(t, filepath.Join(ws, "docs", "a.md"), filepath.Join(ws, "docs", "alias.md"))
	link(t, data, filepath.Join(ws, "docs", "data"))

	s := FileScope{Workspace: ws, ReadMasks: []string{"docs/**", "src/*.go"}, WriteMasks: []string{"docs/*.md", "docs/sub/**"}, Forbidden: []string{data}}
	cases := []struct {
		name  string
		path  string
		write bool
		ok    bool
	}{
		{"a file under a mask", "docs/a.md", false, true},
		{"a deep file under **", "docs/sub/b.md", false, true},
		{"a file made by the mask's *", "src/main.go", false, true},
		{"a new file to write", "docs/new.md", true, true},
		{"a new file in a new folder", "docs/sub/x/y.md", true, true},
		{"a link inside the mask to a file inside it", "docs/alias.md", false, true},
		{"a path with ./ and a double slash", "docs/./sub//b.md", false, true},

		{"outside the masks", "src/other.txt", false, false},
		{"read mask is not the write mask", "src/main.go", true, false},
		{"a write outside its mask", "docs/sub/../a.txt", true, false},
		{"parent segment", "docs/../.env", false, false},
		{"parent segment up and out", "../outside/secret.txt", false, false},
		{"parent in the middle", "docs/sub/../../src/main.go", false, false},
		{"absolute path", "/etc/passwd", false, false},
		{"absolute path to the workspace's file", filepath.Join(ws, "docs", "a.md"), false, false},
		{"home", "~/docs/a.md", false, false},
		{"empty", "", false, false},
		{"the workspace itself", ".", false, false},
		{"a NUL", "docs/a.md\x00.txt", false, false},
		{"a backslash", `docs\sub\b.md`, false, false},
		{".env under a ** mask", ".env", false, false},
		{"a key file under a mask", "docs/server.key", false, false},
		{".git/config", ".git/config", false, false},
		{"the .ssh folder", ".ssh/id_rsa", false, false},
		{"symlink out of the workspace", "docs/out/secret.txt", false, false},
		{"symlink to a file out of it", "docs/out.txt", false, false},
		{"symlink to a secret", "docs/env.md", false, false},
		{"symlink to a file outside the masks", "docs/main.md", true, false},
		{"symlink to .git", "docs/g/config", false, false},
		{"symlink to nowhere", "docs/dangling", false, false},
		{"a write through a link out", "docs/out/new.md", true, false},
		{"symlink into the host's folder", "docs/data/cli-token", false, false},
	}
	for _, c := range cases {
		_, err := s.Resolve(c.path, c.write)
		if (err == nil) != c.ok {
			t.Errorf("%s: Resolve(%q, write=%v) = %v, want ok=%v", c.name, c.path, c.write, err, c.ok)
		}
		var be *BridgeError
		if err != nil && (!errors.As(err, &be) || (be.Code != CodePermissionDenied && be.Code != CodeInvalidParams)) {
			t.Errorf("%s: the refusal is not a bridge error: %v", c.name, err)
		}
	}
}

func TestFileScopeNoRights(t *testing.T) {
	ws, _ := workspaceWith(t)
	if _, err := (FileScope{Workspace: ws, ReadMasks: []string{"docs/**"}}).Resolve("docs/a.md", true); err == nil {
		t.Error("an app with only files.read wrote")
	}
	if _, err := (FileScope{Workspace: ws}).Resolve("docs/a.md", false); err == nil {
		t.Error("an app with no file rights read")
	}
	if _, err := (FileScope{ReadMasks: []string{"**"}}).Resolve("docs/a.md", false); err == nil {
		t.Error("a scope with no workspace read")
	}
	if _, err := (FileScope{Workspace: "docs", ReadMasks: []string{"**"}}).Resolve("a.md", false); err == nil {
		t.Error("a relative workspace was taken")
	}
}

func TestFileScopeForbiddenInsideWorkspace(t *testing.T) {
	ws, _ := workspaceWith(t)
	// The host's folder can lie inside the workspace (the workspace is a home folder).
	data := filepath.Join(ws, "data")
	if err := os.MkdirAll(filepath.Join(data, "apps"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "cli-token"), []byte("t"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := FileScope{Workspace: ws, ReadMasks: []string{"**"}, WriteMasks: []string{"**"}, Forbidden: []string{data}}
	for _, p := range []string{"data/cli-token", "data/apps/installed.json", "data"} {
		for _, write := range []bool{false, true} {
			if _, err := s.Resolve(p, write); err == nil {
				t.Errorf("Resolve(%q, write=%v) reached the host's folder", p, write)
			}
		}
	}
	if _, err := s.Resolve("docs/a.md", false); err != nil {
		t.Errorf("a file beside the host's folder was refused: %v", err)
	}
}

func TestFileScopeReadWrite(t *testing.T) {
	ws, outside := workspaceWith(t)
	link(t, outside, filepath.Join(ws, "docs", "out"))
	s := FileScope{Workspace: ws, ReadMasks: []string{"docs/**"}, WriteMasks: []string{"docs/**"}}
	if err := s.Write("docs/sub/new/n.md", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read("docs/sub/new/n.md")
	if err != nil || string(got) != "hello" {
		t.Fatalf("Read = %q, %v", got, err)
	}
	if err := s.Write("docs/out/x.md", []byte("x")); err == nil {
		t.Error("wrote through a link out of the workspace")
	}
	if _, err := os.Stat(filepath.Join(outside, "x.md")); err == nil {
		t.Error("the file landed outside the workspace")
	}
	if _, err := s.Read("docs/nothing.md"); err == nil {
		t.Error("read a file that is not there")
	}
	if _, err := s.Read("docs/sub"); err == nil {
		t.Error("read a folder")
	}
	big := make([]byte, MaxFileBytes+1)
	if err := s.Write("docs/big.bin", big); err == nil {
		t.Error("wrote more than the limit")
	}
}

// A link put in the way after the check is not followed out: the open goes through the workspace's root.
func TestFileScopeSwapAfterCheck(t *testing.T) {
	ws, outside := workspaceWith(t)
	s := FileScope{Workspace: ws, ReadMasks: []string{"docs/**"}}
	if _, err := s.Resolve("docs/swap/secret.txt", false); err != nil {
		t.Fatalf("a path inside the mask that is not there yet was refused: %v", err)
	}
	link(t, outside, filepath.Join(ws, "docs", "swap"))
	if _, err := s.Read("docs/swap/secret.txt"); err == nil {
		t.Error("read through a link that was swapped in")
	}
}

func TestMatchMask(t *testing.T) {
	cases := []struct {
		mask, path string
		want       bool
	}{
		{"docs/*", "docs/a.md", true},
		{"docs/*", "docs/sub/a.md", false},
		{"docs/**", "docs/sub/a.md", true},
		{"docs/**", "docs", true},
		{"**", "a/b/c", true},
		{"**/*.md", "a/b/c.md", true},
		{"**/*.md", "c.md", true},
		{"**/*.md", "c.txt", false},
		{"*.md", "docs/a.md", false},
		{"docs/a?.md", "docs/ab.md", true},
	}
	for _, c := range cases {
		if got := MatchMask(c.mask, c.path); got != c.want {
			t.Errorf("MatchMask(%q, %q) = %v, want %v", c.mask, c.path, got, c.want)
		}
	}
}

func TestCheckMask(t *testing.T) {
	for _, ok := range []string{"docs/*", "**", "src/**/*.go", "a.md"} {
		if m := CheckMask(ok); m != "" {
			t.Errorf("CheckMask(%q) = %q", ok, m)
		}
	}
	for _, bad := range []string{"", "/etc/*", "~/x", "../x", "docs/../../x", "docs//a", "a\\b", "docs/a**b", "docs/["} {
		if CheckMask(bad) == "" {
			t.Errorf("CheckMask(%q) passed", bad)
		}
	}
}

// Case, tool configuration and hidden files, over wide masks.
func TestFileScopeHiddenAndConfig(t *testing.T) {
	ws, _ := workspaceWith(t)
	_ = os.WriteFile(filepath.Join(ws, "docs", ".hidden.md"), []byte("h"), 0o644)
	_ = os.MkdirAll(filepath.Join(ws, ".claude"), 0o755)
	_ = os.MkdirAll(filepath.Join(ws, ".notes"), 0o755)
	s := FileScope{Workspace: ws, ReadMasks: []string{"**"}, WriteMasks: []string{"**", ".notes/**"}}
	cases := []struct {
		name, path string
		write, ok  bool
	}{

		{"upper-case .GIT", ".GIT/config", false, false},
		{"upper-case .Env", ".Env", false, false},
		{"upper-case key", "docs/Server.KEY", false, false},
		{".envrc", ".envrc", false, false},
		{"a write into .claude", ".claude/settings.json", true, false},
		{"a write into .Claude", ".Claude/hooks/x.sh", true, false},
		{"a write to .mcp.json", ".mcp.json", true, false},
		{"a write to CLAUDE.md", "docs/CLAUDE.md", true, false},
		{"a write to AGENTS.md", "docs/agents.md", true, false},
		{"a write into .vscode", ".vscode/tasks.json", true, false},
		{"a write into .idea", ".idea/x.xml", true, false},
		{"a write into .harnessly", ".harnessly/x", true, false},
		{"a trailing dot", "CLAUDE.md.", true, false},
		{"a trailing dot in a folder", ".claude./settings.json", true, false},
		{"a trailing space", "docs/CLAUDE.md ", true, false},
		{"an alternate data stream", "CLAUDE.md::$DATA", true, false},
		{"a colon", "docs/a:b.md", false, false},
		{"a control character", "docs/a\x01b.md", false, false},
		{"a newline", "docs/a\nb.md", true, false},
		{"a write to CLAUDE.local.md", "CLAUDE.local.md", true, false},
		{"a write to Claude-notes.MD", "docs/Claude-notes.MD", true, false},
		{"a write to AGENTS.override.md", "AGENTS.override.md", true, false},
		{"a write to GEMINI.md", "docs/GEMINI.md", true, false},
		{"a write to .cursorrules", ".CursorRules", true, false},
		{"a write into .cursor", ".cursor/rules/a.mdc", true, false},
		{"a write to .windsurfrules", ".windsurfrules", true, false},
		{"a write to copilot-instructions.md", "docs/Copilot-Instructions.md", true, false},
		{"a read of CLAUDE.local.md", "CLAUDE.local.md", false, true},
		{"a write to a hidden file under **", "docs/.hidden.md", true, false},
		{"a write to a hidden folder under **", "docs/.cache/x.md", true, false},
		{"a read of a hidden file under **", "docs/.hidden.md", false, true},
		{"a write into a folder a mask names with its dot", ".notes/a.md", true, true},
		{"an ordinary write", "docs/new.md", true, true},
		{"a read of .claude", ".claude/notes.md", false, true},
	}
	for _, c := range cases {
		if _, err := s.Resolve(c.path, c.write); (err == nil) != c.ok {
			t.Errorf("%s: Resolve(%q, write=%v) = %v, want ok=%v", c.name, c.path, c.write, err, c.ok)
		}
	}
}

// A forbidden folder is recognised by what it is, not by how its name is spelled: through a link whose name differs, and
// through a folder above it.
func TestFileScopeForbiddenByIdentity(t *testing.T) {
	ws, _ := workspaceWith(t)
	data := filepath.Join(t.TempDir(), "Apps")
	if err := os.MkdirAll(filepath.Join(data, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A hard-to-spell route to the same folder: a bind-like second name through a link in the workspace.
	if err := os.Symlink(data, filepath.Join(ws, "docs", "elsewhere")); err != nil {
		t.Fatal(err)
	}
	s := FileScope{Workspace: ws, ReadMasks: []string{"**"}, WriteMasks: []string{"**"}, Forbidden: []string{data}}
	for _, p := range []string{"docs/elsewhere/inner/x.md", "docs/elsewhere"} {
		if _, err := s.Resolve(p, true); err == nil {
			t.Errorf("%s reaches the forbidden folder", p)
		}
	}
	// The same folder named with other letters: refused where the volume ignores case, and by identity where it does.
	other := FileScope{Workspace: ws, ReadMasks: []string{"**"}, Forbidden: []string{filepath.Join(filepath.Dir(data), "apps")}}
	_, err := other.Resolve("docs/elsewhere/inner/x.md", false)
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(data), "apps")); statErr == nil && err == nil {
		t.Error("another spelling of the forbidden folder passes where the volume sees one folder")
	}
	// A workspace folder that holds the forbidden one is refused too.
	holder := FileScope{Workspace: ws, ReadMasks: []string{"**"}, Forbidden: []string{filepath.Join(ws, "docs")}}
	if _, err := holder.Resolve("docs/readme.md", false); err == nil {
		t.Error("a file inside a forbidden folder is read")
	}
	if _, err := s.Resolve("docs/readme.md", false); err != nil {
		t.Errorf("an ordinary read is refused: %v", err)
	}
}
