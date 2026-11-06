package envsetup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// projectFolder makes ~/code/loki with a platform's marker folder in it, and a list cloning into it.
func projectFolder(t *testing.T, cloneExit int, extra string) (*fixture, string, *int) {
	f := newFixture(t)
	dir := filepath.Join(f.home, "code", "loki")
	if err := os.MkdirAll(filepath.Join(dir, ".marker", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if extra != "" {
		if err := os.WriteFile(filepath.Join(dir, extra), []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	clones := 0
	f.handle = func(c Cmd) reply {
		if c.Name != "git" {
			return reply{}
		}
		if contains(c.Args, "remote") {
			return reply{exit: 128} // not a repository
		}
		if c.Args[0] == "clone" {
			clones++
			if cloneExit != 0 {
				return reply{exit: cloneExit}
			}
			to := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(to, ".git"), 0o755)
			_ = os.WriteFile(filepath.Join(to, "README"), []byte("hi"), 0o644)
		}
		return reply{}
	}
	f.setList(hubproto.EnvItem{ItemID: "r1", Kind: "repo", Name: "loki", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/loki.git","dir":"~/code/loki"}`)})
	return f, dir, &clones
}

func names(t *testing.T, dir string) string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return strings.Join(out, ",")
}

func TestRepoClonesIntoAnEmptyProjectFolder(t *testing.T) {
	f, dir, _ := projectFolder(t, 0, "")
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "r1"); s.Status != hubproto.EnvOK {
		t.Fatalf("%+v", s)
	}
	if got := names(t, dir); got != ".git,.marker,README" {
		t.Fatalf("folder: %s", got)
	}
	if got := names(t, filepath.Dir(dir)); got != "loki" {
		t.Fatalf("the temporary clone is left: %s", got)
	}
}

func TestProjectFolderWithAFileIsNotTouched(t *testing.T) {
	f, dir, clones := projectFolder(t, 0, "notes.txt")
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "r1"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "уже есть, но это не клон") {
		t.Fatalf("%+v", s)
	}
	if *clones != 0 || names(t, dir) != ".marker,notes.txt" {
		t.Fatalf("clones %d, folder %s", *clones, names(t, dir))
	}
}

func TestFailedCloneLeavesTheFolderAsItWas(t *testing.T) {
	f, dir, _ := projectFolder(t, 128, "")
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "r1"); s.Status != hubproto.EnvError || !strings.Contains(s.Message, "git clone не удался") {
		t.Fatalf("%+v", s)
	}
	if names(t, dir) != ".marker" || names(t, filepath.Dir(dir)) != "loki" {
		t.Fatalf("folder %s, parent %s", names(t, dir), names(t, filepath.Dir(dir)))
	}
}

func TestAFileWrittenDuringTheCloneIsKept(t *testing.T) {
	f, dir, _ := projectFolder(t, 0, "")
	inner := f.handle
	f.handle = func(c Cmd) reply {
		rep := inner(c)
		if c.Name == "git" && c.Args[0] == "clone" {
			to := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(to, "src"), 0o755)
			_ = os.WriteFile(filepath.Join(to, "src", "a.go"), []byte("repo"), 0o644)
			_ = os.MkdirAll(filepath.Join(dir, "src"), 0o755) // a chat writes into the folder meanwhile
			_ = os.WriteFile(filepath.Join(dir, "src", "new.go"), []byte("mine"), 0o644)
		}
		return rep
	}
	f.r.Tick(context.Background())
	if s := f.step(f.onlyRun(), "r1"); s.Status != hubproto.EnvError {
		t.Fatalf("%+v", s)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "src", "new.go")); err != nil || string(b) != "mine" {
		t.Fatalf("the person's file is gone: %v", err)
	}
}
