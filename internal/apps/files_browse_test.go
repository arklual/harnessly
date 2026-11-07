package apps

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func paths[T any](list []T, of func(T) string) []string {
	out := []string{}
	for _, x := range list {
		out = append(out, of(x))
	}
	return out
}

func TestFilesListInsideMasks(t *testing.T) {
	root := tree(t, map[string]string{
		"src/a.go": "package a", "src/b.txt": "b", "src/deep/c.go": "package c", "docs/x.md": "x",
		"src/.env": "KEY=1", "README.md": "r", ".git/config": "[core]",
	})
	s := FileScope{Workspace: root, ReadMasks: []string{"src/**/*.go", "README.md"}}
	top, err := s.List("")
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(top, func(e FileEntry) string { return e.Path }); !slices.Equal(got, []string{"src", "README.md"}) {
		t.Fatalf("top: %v (docs is out of the masks, .git never shown)", got)
	}
	in, err := s.List("src")
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(in, func(e FileEntry) string { return e.Path }); !slices.Equal(got, []string{"src/deep", "src/a.go"}) {
		t.Fatalf("src: %v (b.txt is out of the mask, .env is a secret)", got)
	}
	if _, err := s.List("docs"); err == nil {
		t.Fatal("a folder no mask reaches is refused")
	}
	if _, err := s.List("../"); err == nil {
		t.Fatal("out of the workspace is refused")
	}
	if _, err := (FileScope{Workspace: root}).List(""); err == nil {
		t.Fatal("no files.read, no listing")
	}
}

func TestFilesSearch(t *testing.T) {
	root := tree(t, map[string]string{
		"src/a.go": "package a\n// TODO: fix\n", "src/b.go": "package b\n", "src/.env": "TODO=secret",
		"node_modules/x/y.go": "// TODO", "docs/n.md": "TODO",
	})
	s := FileScope{Workspace: root, ReadMasks: []string{"src/**", "node_modules/**/*.go"}}
	hits, cut, err := s.Search("todo", SearchOptions{})
	if err != nil || cut {
		t.Fatal(err, cut)
	}
	got := paths(hits, func(h SearchHit) string { return h.Path })
	if !slices.Contains(got, "src/a.go") || slices.Contains(got, "src/.env") || slices.Contains(got, "docs/n.md") {
		t.Fatalf("hits %v", got)
	}
	if !slices.Contains(got, "node_modules/x/y.go") {
		t.Fatalf("a mask that names node_modules walks it: %v", got)
	}
	if hits[slices.Index(got, "src/a.go")].Line != 2 {
		t.Fatalf("line: %+v", hits)
	}
	byName, _, err := s.Search("", SearchOptions{Glob: "src/*.go"})
	if err != nil || len(byName) != 2 {
		t.Fatalf("by name: %+v %v", byName, err)
	}
	if _, _, err := s.Search("TODO", SearchOptions{Case: true, Glob: "../x"}); err == nil {
		t.Fatal("a glob out of the workspace is refused")
	}
	(func() {
		// Without the mask naming it, node_modules is not walked.
		s := FileScope{Workspace: root, ReadMasks: []string{"**"}}
		hits, _, _ := s.Search("TODO", SearchOptions{Case: true})
		if slices.Contains(paths(hits, func(h SearchHit) string { return h.Path }), "node_modules/x/y.go") {
			t.Fatal("node_modules walked without a mask naming it")
		}
	})()
}

func TestFilesWatch(t *testing.T) {
	root := tree(t, map[string]string{"src/a.go": "a", "docs/x.md": "x"})
	fw := NewFileWatches()
	got := make(chan FileChange, 4)
	defer fw.OnChange(func(c FileChange) { got <- c })()
	s := FileScope{Workspace: root, ReadMasks: []string{"src/**"}}
	if err := fw.Start("tok", s, func() bool { return true }); err != nil {
		t.Fatal(err)
	}
	defer fw.Stop("tok")
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(root, "docs/x.md"), []byte("y"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "src/.env"), []byte("K=1"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "src/a.go"), []byte("b"), 0o644)
	select {
	case c := <-got:
		if c.Session != "tok" || !slices.Equal(c.Paths, []string{"src/a.go"}) {
			t.Fatalf("change %+v: only the file under the mask, no secret", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no change heard")
	}
	fw.Stop("tok")
	if fw.Watching("tok") {
		t.Fatal("stopped")
	}
}

func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := tree(t, map[string]string{"app/a.txt": "one\n", "app/.env": "KEY=1\n", "other.txt": "o\n"})
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("add", "-A")
	run("commit", "-q", "-m", "first")
	return root
}

func TestGitRead(t *testing.T) {
	root := gitRepo(t)
	app := filepath.Join(root, "app")
	_ = os.WriteFile(filepath.Join(app, "a.txt"), []byte("two\n"), 0o644)
	_ = os.WriteFile(filepath.Join(app, ".env"), []byte("KEY=2\n"), 0o644)
	_ = os.WriteFile(filepath.Join(app, "new.txt"), []byte("n\n"), 0o644)
	_ = os.WriteFile(filepath.Join(root, "other.txt"), []byte("p\n"), 0o644)
	// The repository's config names a program for diffs and for status: git must not run it.
	mark := filepath.Join(t.TempDir(), "ran")
	// A filter driver that leaves a mark and passes the content on: run by git, it would show.
	script := filepath.Join(t.TempDir(), "filter.sh")
	_ = os.WriteFile(script, []byte("#!/bin/sh\ntouch "+mark+"\ncat\n"), 0o755)
	cfg := filepath.Join(root, ".git", "config")
	raw, _ := os.ReadFile(cfg)
	evil := "\n[diff]\n\texternal = touch " + mark + "\n[core]\n\tfsmonitor = touch " + mark +
		"\n[filter \"evil\"]\n\tclean = " + script + "\n\trequired = true" +
		"\n[log]\n\tshowSignature = true\n[gpg]\n\tprogram = touch " + mark + "\n"
	_ = os.WriteFile(cfg, append(raw, evil...), 0o644)
	_ = os.WriteFile(filepath.Join(root, ".gitattributes"), []byte("* filter=evil\n"), 0o644)

	g := GitScope{Workspace: app}
	ctx := context.Background()
	st, err := g.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files := paths(st.Files, func(f GitFile) string { return f.Path + ":" + f.Staged + f.Unstaged })
	if st.Branch != "main" || !slices.Equal(files, []string{"a.txt:M", "new.txt:?"}) {
		t.Fatalf("status %+v %v (.env and the file above the folder are left out)", st, files)
	}
	diff, _, err := g.Diff(ctx, GitDiffOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "+two") || strings.Contains(diff, "KEY=") || strings.Contains(diff, "other.txt") {
		t.Fatalf("diff:\n%s", diff)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("git ran a program the repository's config names")
	}
	log, err := g.Log(ctx, 5, "")
	if err != nil || len(log) != 1 || log[0].Subject != "first" {
		t.Fatalf("log %+v %v", log, err)
	}
	if _, _, err := g.Diff(ctx, GitDiffOptions{Ref: "--output=/tmp/x"}); err == nil {
		t.Fatal("a ref that is an option is refused")
	}
	if _, err := (GitScope{Workspace: t.TempDir()}).Status(ctx); err == nil {
		t.Fatal("no repository, an error")
	}
}

func TestEventsPermission(t *testing.T) {
	m := &Manifest{Permissions: []string{"events:turn"}}
	if !m.Allows("events") || !slices.Equal(m.EventKindsOf(), []string{"turn"}) {
		t.Fatal("events:turn holds events")
	}
	a := AccessOf(m, nil)
	if len(a) != 1 || a[0].Label.Ru != "Узнавать, когда агент закончил ход" {
		t.Fatalf("access %+v", a)
	}
}

func TestSecretsOfAHomeFolder(t *testing.T) {
	for _, p := range []string{".bash_history", ".zsh_history", ".claude.json", ".claude/.credentials.json", ".codex/auth.json",
		".config/gh/hosts.yml", ".password-store/x.gpg", "sub/.python_history"} {
		if !secret(p) {
			t.Errorf("%s is a secret", p)
		}
	}
	for _, p := range []string{"src/auth.json", "history.md", "docs/config.md", ".claude/agents/a.md"} {
		if secret(p) {
			t.Errorf("%s is not a secret", p)
		}
	}
}
