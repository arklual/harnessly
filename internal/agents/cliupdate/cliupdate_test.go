package cliupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ly-solutions/harnessly/internal/preflight"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"0.169.1", "0.147.0", true},
		{"0.147.0", "0.169.1", false},
		{"0.169.1", "0.169.1", false},
		{"0.170.0", "0.170.0-alpha.3", true},
		{"0.170.0-alpha.3", "0.169.9", true},
		{"2.1.327", "2.1.99", true},
		{"0.169.1", "", true},
		{"", "0.1.0", false},
		{"dev", "0.1.0", false},
	} {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestParseCopies(t *testing.T) {
	out := "/home/a/.local/bin/codex\t/home/a/.local/lib/node_modules/@openai/codex/bin/codex.js\tcodex-cli 0.147.0\n" +
		"/opt/p/bin/codex\t/opt/p/bin/codex-0.169.1/bin/codex\tcodex-cli 0.169.1\n" +
		"/usr/bin/codex\t/opt/p/bin/codex-0.169.1/bin/codex\tcodex-cli 0.169.1\n" +
		"relative\tx\ty\n" +
		"/usr/local/bin/codex\t\t\n"
	got := ParseCopies(out)
	if len(got) != 3 {
		t.Fatalf("copies = %+v", got)
	}
	if got[0].Version != "0.147.0" || got[1].Version != "0.169.1" || got[2].Path != "/usr/local/bin/codex" || got[2].Real != "/usr/local/bin/codex" || got[2].Version != "" {
		t.Fatalf("copies = %+v", got)
	}
}

func TestNewestAndShadowing(t *testing.T) {
	old := Copy{Path: "/h/.local/bin/codex", Real: "/h/lib/codex.js", Version: "0.147.0"}
	sys := Copy{Path: "/opt/bin/codex", Real: "/opt/codex-0.169.1", Version: "0.169.1"}
	same := Copy{Path: "/usr/bin/codex", Real: "/usr/codex", Version: "0.169.1"}
	broken := Copy{Path: "/x/codex", Real: "/x/codex", Version: ""}
	if n, ok := Newest([]Copy{old, broken, sys, same}); !ok || n.Path != sys.Path {
		t.Fatalf("Newest = %+v %v, want the first of the 0.169.1", n, ok)
	}
	if n, ok := Shadowing([]Copy{old, sys}); !ok || n.Path != sys.Path {
		t.Fatalf("Shadowing = %+v %v", n, ok)
	}
	if _, ok := Shadowing([]Copy{sys, old}); ok {
		t.Fatal("the newest first shadows nothing")
	}
	if _, ok := Shadowing([]Copy{same, sys}); ok {
		t.Fatal("an equal version shadows nothing")
	}
	if _, ok := Shadowing([]Copy{old}); ok {
		t.Fatal("one copy shadows nothing")
	}
	if n, ok := Shadowing([]Copy{broken, sys}); !ok || n.Path != sys.Path {
		t.Fatal("a copy that prints no version is shadowing a working one")
	}
}

func TestRelinkReplacesOnlyLinks(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "codex")
	if err := os.Symlink("/old", link); err != nil {
		t.Fatal(err)
	}
	if err := relink(link, "/new"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.Readlink(link); got != "/new" {
		t.Fatalf("link → %q", got)
	}
	prog := filepath.Join(dir, "claude")
	_ = os.WriteFile(prog, []byte("#!/bin/sh\n"), 0o755)
	if err := relink(prog, "/new"); err == nil {
		t.Fatal("a program was replaced")
	}
	if _, err := os.Lstat(prog + ".hl-new"); err == nil {
		t.Fatal("a temporary link was left")
	}
}

func TestPruneKeepsTheNewestAndTheRunning(t *testing.T) {
	root := t.TempDir()
	for _, v := range []string{"0.160.0", "0.165.0", "0.168.0", "0.169.0", "0.170.0", "junk"} {
		_ = os.MkdirAll(filepath.Join(root, v), 0o755)
	}
	prune(root, "0.170.0", filepath.Join(root, "0.160.0", "node_modules", ".bin", "codex"))
	var left []string
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	if strings.Join(left, ",") != "0.160.0,0.169.0,0.170.0,junk" {
		t.Fatalf("left %v", left)
	}
}

// fakeShell answers the updater's scripts by what they do.
type fakeShell struct {
	mu      sync.Mutex
	list    map[string]string // cli → listing
	latest  map[string]string
	install string // the output of the npm install script
	scripts []string
}

func (f *fakeShell) RunLogin(_ context.Context, script string, _ ...string) (preflight.LoginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts = append(f.scripts, script)
	switch {
	case strings.Contains(script, "for d in $PATH"):
		for cli, out := range f.list {
			if strings.Contains(script, `f="$1/`+cli+`"`) {
				return preflight.LoginResult{Stdout: out}, nil
			}
		}
		return preflight.LoginResult{}, nil
	case strings.Contains(script, "npm view"):
		for cli, pkg := range map[string]string{"codex": "@openai/codex", "claude": "@anthropic-ai/claude-code"} {
			if strings.Contains(script, pkg) {
				return preflight.LoginResult{Stdout: f.latest[cli]}, nil
			}
		}
	case strings.Contains(script, "npm install"):
		return preflight.LoginResult{Stdout: f.install}, nil
	}
	return preflight.LoginResult{}, nil
}

func TestRunPointsAShadowingLinkAtTheNewerCopy(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o755)
	old := filepath.Join(home, ".local", "lib", "node_modules", "@openai", "codex", "bin", "codex.js")
	_ = os.MkdirAll(filepath.Dir(old), 0o755)
	_ = os.WriteFile(old, []byte("//"), 0o755)
	link := filepath.Join(bin, "codex")
	_ = os.Symlink(old, link)
	sys := t.TempDir()
	sysBin := filepath.Join(sys, "codex")
	sh := &fakeShell{
		list: map[string]string{"codex": link + "\t" + old + "\tcodex-cli 0.147.0\n" +
			sysBin + "\t" + sysBin + "\tcodex-cli 0.169.1\n"},
		latest: map[string]string{"codex": "0.169.1"},
	}
	var changed []string
	u := New(Deps{Shell: sh, Home: home, Changed: func(c string) { changed = append(changed, c) }})
	u.Run(context.Background())
	if got, _ := os.Readlink(link); got != sysBin {
		t.Fatalf("link → %q, want %q", got, sysBin)
	}
	st := codexOnly(u)
	if len(st) != 1 || st[0].CLI != "codex" || st[0].State != StateUpdated || st[0].From != "0.147.0" || st[0].To != "0.169.1" {
		t.Fatalf("status %+v", st)
	}
	if len(changed) != 1 || changed[0] != "codex" {
		t.Fatalf("changed %v", changed)
	}
}

func TestRunInstallsBesideAndSwitchesTheLink(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o755)
	cur := filepath.Join(Dir(home), "codex", "0.169.1", "node_modules", ".bin", "codex")
	_ = os.MkdirAll(filepath.Dir(cur), 0o755)
	_ = os.WriteFile(cur, []byte("//"), 0o755)
	link := filepath.Join(bin, "codex")
	_ = os.Symlink(cur, link)
	sh := &fakeShell{
		list:    map[string]string{"codex": link + "\t" + cur + "\tcodex-cli 0.169.1\n"},
		latest:  map[string]string{"codex": "0.170.0"},
		install: "codex-cli 0.170.0\n",
	}
	u := New(Deps{Shell: sh, Home: home})
	u.Run(context.Background())
	want := filepath.Join(Dir(home), "codex", "0.170.0", "node_modules", ".bin", "codex")
	if got, _ := os.Readlink(link); got != want {
		t.Fatalf("link → %q, want %q", got, want)
	}
	if st := codexOnly(u); st[0].State != StateUpdated || st[0].Version != "0.170.0" || st[0].From != "0.169.1" {
		t.Fatalf("status %+v", st)
	}
	installs := 0
	for _, s := range sh.scripts {
		if strings.Contains(s, "npm install") {
			installs++
			if !strings.Contains(s, "'@openai/codex@0.170.0'") || !strings.Contains(s, ".tmp") {
				t.Fatalf("install script %s", s)
			}
		}
	}
	if installs != 1 {
		t.Fatalf("%d installs", installs)
	}
}

func TestRunLeavesTheAdminsCopy(t *testing.T) {
	home := t.TempDir()
	sys := "/opt/managed/codex"
	sh := &fakeShell{
		list:   map[string]string{"codex": sys + "\t" + sys + "\tcodex-cli 0.169.1\n"},
		latest: map[string]string{"codex": "0.170.0"},
	}
	u := New(Deps{Shell: sh, Home: home})
	u.Run(context.Background())
	st := codexOnly(u)
	if st[0].State != StateManaged || st[0].Latest != "0.170.0" {
		t.Fatalf("status %+v", st)
	}
	for _, s := range sh.scripts {
		if strings.Contains(s, "npm install") {
			t.Fatal("installed over the admin's copy")
		}
	}
}

func TestInstallRefusesAWrongVersion(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	_ = os.MkdirAll(bin, 0o755)
	cur := filepath.Join(home, "codex.js")
	_ = os.WriteFile(cur, []byte("//"), 0o755)
	link := filepath.Join(bin, "codex")
	_ = os.Symlink(cur, link)
	sh := &fakeShell{
		list:    map[string]string{"codex": link + "\t" + cur + "\tcodex-cli 0.169.1\n"},
		latest:  map[string]string{"codex": "0.170.0"},
		install: "codex-cli 0.169.9\n",
	}
	u := New(Deps{Shell: sh, Home: home})
	u.Run(context.Background())
	if got, _ := os.Readlink(link); got != cur {
		t.Fatalf("link moved to %q", got)
	}
	if st := codexOnly(u); st[0].State != StateFailed {
		t.Fatalf("status %+v", st)
	}
}

func TestScanLiveModels(t *testing.T) {
	body := strings.Repeat("x", 3<<20) + "gpt-live-1-codex\x00" + strings.Repeat("y", 1<<20-5) + "gpt-live-1.2-codex" + "\x00gpt-live-1-codex"
	got := ScanLiveModels(strings.NewReader(body))
	if strings.Join(got, ",") != "gpt-live-1-codex,gpt-live-1.2-codex" {
		t.Fatalf("got %v", got)
	}
}

// codexOnly is the status of Codex: the fake shell lists no Claude Code, which is reported missing.
func codexOnly(u *Updater) []Status {
	var out []Status
	for _, s := range u.Status() {
		if s.CLI == "claude" && s.State != StateMissing {
			panic("claude is not missing")
		}
		if s.CLI == "codex" {
			out = append(out, s)
		}
	}
	return out
}
