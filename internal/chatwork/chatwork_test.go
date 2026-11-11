package chatwork

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestCommandPaths(t *testing.T) {
	cases := []struct {
		cmd, cwd string
		want     []string
	}{
		{"cd /a/b && git commit -m x", "/w", []string{"/a/b"}},
		{"git -C ~/projects/r worktree add -b agent/x .worktrees/x origin/main", "/w",
			[]string{"/home/u/projects/r", "/home/u/projects/r/.worktrees/x"}},
		{"cat ./f.txt; ls ../up/dir", "/w/s", []string{"/w/s/f.txt", "/w/up/dir"}},
		{`echo "/quoted path/x" https://x.y/z`, "/w", []string{"/quoted path/x"}},
		{"ls /proc/self /tmp/claude-1/x/y", "/w", nil},
		{"cd sub && ls ./a", "/w", []string{"/w/sub", "/w/sub/a"}}, // cd moves the shell for the rest of the line
	}
	for _, c := range cases {
		got := commandPaths(c.cmd, c.cwd, "/home/u")
		if !slices.Equal(got, c.want) {
			t.Errorf("commandPaths(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}

func TestChangesGit(t *testing.T) {
	yes := []string{"git commit -m x", "git -C /r worktree add -b a .worktrees/a", "cd /r && git push -u origin a",
		"git checkout -b x", "git -c user.name=a commit --amend", "git rebase origin/main"}
	no := []string{"git status", "git log --oneline", "git diff HEAD", "ls /r", "git -C /r show HEAD"}
	for _, c := range yes {
		if !changesGit(c) {
			t.Errorf("changesGit(%q) = false", c)
		}
	}
	for _, c := range no {
		if changesGit(c) {
			t.Errorf("changesGit(%q) = true", c)
		}
	}
}

func TestSummaryPath(t *testing.T) {
	for in, want := range map[string]string{
		"Update /a/b.go (+1, -2)":        "/a/b.go",
		"Create /a/new file.md (+3, -0)": "/a/new file.md",
		"Write /a/c.txt":                 "/a/c.txt",
		"Read /a/c.txt":                  "",
		"Write relative.txt":             "",
	} {
		if got := summaryPath(in); got != want {
			t.Errorf("summaryPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRemoteAndMR(t *testing.T) {
	if got := cleanRemote("https://user:secret@gitlab.example.com/g/p.git"); strings.Contains(got, "secret") || strings.Contains(got, "user") {
		t.Fatalf("cleanRemote kept credentials: %s", got)
	}
	host, project := RemoteProject("git@gitlab.example.com:g/sub/p.git")
	if host != "gitlab.example.com" || project != "g/sub/p" {
		t.Fatalf("remoteProject = %s %s", host, project)
	}
	out := "remote: View merge request for a:\nremote:   https://gitlab.example.com/g/sub/p/-/merge_requests/12\n" +
		"https://gitlab.example.com/other/q/-/merge_requests/3"
	if got := mrOfProject(out, "https://gitlab.example.com/g/sub/p.git"); got != "https://gitlab.example.com/g/sub/p/-/merge_requests/12" {
		t.Fatalf("mrOfProject = %q", got)
	}
	// gh pr create prints the pull request's address.
	if got := mrOfProject("https://github.com/acme/app/pull/7\n", "git@github.com:acme/app.git"); got != "https://github.com/acme/app/pull/7" {
		t.Fatalf("a pull request: %q", got)
	}
}

func TestRegistryMergePersistAndWatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work.json")
	r := Open(path)
	var got []Entry
	stop := r.Watch(func(l []Entry) { got = append(got, l...) })
	r.Put(Entry{Conv: "c1", Worktree: "/w", Repo: "/w", Base: "b1", Since: 100, At: 100, Commits: []string{"x"}, Source: hubproto.ChatWorkLive})
	// A later scan neither moves the base nor forgets a commit nor turns the record into a scanned one.
	r.Put(Entry{Conv: "c1", Worktree: "/w", Repo: "/w", Base: "b2", Since: 200, At: 200, Commits: []string{"y"}, Source: hubproto.ChatWorkScan})
	if changed := r.Put(Entry{Conv: "c1", Worktree: "/w", Repo: "/w", Base: "b2", Since: 200, At: 300, Commits: []string{"y"}, Source: hubproto.ChatWorkScan}); changed {
		t.Fatal("a look that changed nothing reported a change")
	}
	stop()
	r.Put(Entry{Conv: "c2", Worktree: "/v", Repo: "/v", At: 400})
	if len(got) != 2 {
		t.Fatalf("watcher saw %d changes, want 2", len(got))
	}
	r.MarkScanned("c1")

	again := Open(path)
	es := again.Of("c1")
	if len(es) != 1 {
		t.Fatalf("reopened registry has %d records of c1", len(es))
	}
	e := es[0]
	if e.Base != "b1" || e.Since != 100 || e.Source != hubproto.ChatWorkLive || !slices.Equal(e.Commits, []string{"y", "x"}) {
		t.Fatalf("merged record = %+v", e)
	}
	if !again.Scanned("c1") || again.Scanned("c2") {
		t.Fatal("scanned chats not kept")
	}
	frames := again.WorkFrames()
	if len(frames) != 1 {
		t.Fatalf("frames = %d", len(frames))
	}
	var f struct {
		K       string  `json:"k"`
		Entries []Entry `json:"entries"`
	}
	if err := json.Unmarshal(frames[0], &f); err != nil || f.K != "work" || len(f.Entries) != 2 {
		t.Fatalf("frame = %s", frames[0])
	}
}

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func gitEnv(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@example.com")
}

// nextSecond waits for the next whole second and returns it in ms: the reflog keeps seconds, so a chat that starts
// then did not make what was committed before.
func nextSecond() int64 {
	next := time.Now().Truncate(time.Second).Add(time.Second)
	time.Sleep(time.Until(next))
	return next.UnixMilli()
}

func newRepo(t *testing.T) string {
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q", "-b", "main")
	gitT(t, repo, "config", "remote.origin.url", "https://gitlab.example.com/g/p.git")
	if err := os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-q", "-m", "init")
	return repo
}

// flush waits until the observer handled everything queued before it.
func (o *Observer) flush(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	o.jobs <- job{conv: "", done: done}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("observer did not drain")
	}
}

// A chat whose folder is no repository delegates; the delegated chat makes a worktree, writes a file and commits:
// the parent's subtree holds the branch and the commit.
func TestDelegatedCommitInWorktreeReachesParent(t *testing.T) {
	gitEnv(t)
	repo := newRepo(t)
	plain := t.TempDir()
	start := nextSecond()
	chats := []Chat{
		{ID: "parent", Cwd: plain, CreatedAt: start},
		{ID: "child", Parent: "parent", Cwd: plain, CreatedAt: start},
		{ID: "other", Cwd: plain, CreatedAt: start},
	}
	of := func(id string) (Chat, bool) {
		for _, c := range chats {
			if c.ID == id {
				return c, true
			}
		}
		return Chat{}, false
	}
	o := NewObserver(Open(""), of, "/nonexistent-home")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)

	wt := filepath.Join(repo, ".worktrees", "x")
	add := "git -C " + repo + " worktree add -q -b agent/x .worktrees/x main"
	gitT(t, repo, "worktree", "add", "-q", "-b", "agent/x", ".worktrees/x", "main")
	o.Step("child", "Bash", "$ "+add, true, "")
	if err := os.WriteFile(filepath.Join(wt, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o.Step("child", "Write", "Write "+filepath.Join(wt, "b.txt"), true, "")
	gitT(t, wt, "add", "b.txt")
	gitT(t, wt, "commit", "-q", "-m", "add b")
	sha := gitT(t, wt, "rev-parse", "HEAD")
	o.Step("child", "Bash", "$ cd "+wt+" && git push -u origin agent/x", true,
		"remote:   https://gitlab.example.com/g/p/-/merge_requests/7\n")
	o.TurnEnded("child")
	o.TurnEnded("parent")
	o.flush(t)

	if es := o.Reg.Of("parent"); len(es) != 0 {
		t.Fatalf("the parent itself worked nowhere, got %+v", es)
	}
	es := o.Reg.Of(Subtree("parent", chats)...)
	if len(es) != 1 {
		t.Fatalf("subtree records = %+v", es)
	}
	e := es[0]
	realRepo, _ := filepath.EvalSymlinks(repo)
	realWt, _ := filepath.EvalSymlinks(wt)
	if e.Conv != "child" || e.Parent != "parent" || e.Branch != "agent/x" || e.Worktree != realWt || e.Repo != realRepo {
		t.Fatalf("record = %+v", e)
	}
	if !slices.Equal(e.Commits, []string{sha}) || e.Head != sha || e.Base == "" || e.Base == sha {
		t.Fatalf("commits/base = %+v (sha %s)", e, sha)
	}
	if e.MR != "https://gitlab.example.com/g/p/-/merge_requests/7" || e.Remote != "https://gitlab.example.com/g/p.git" {
		t.Fatalf("mr/remote = %q %q", e.MR, e.Remote)
	}
	if got := o.Reg.Of("other"); len(got) != 0 {
		t.Fatalf("an unrelated chat got work: %+v", got)
	}
}

// A command that only reads git registers nothing; one in a folder the chat already works in looks at it again.
func TestReadingCommandRegistersNothing(t *testing.T) {
	gitEnv(t)
	repo := newRepo(t)
	of := func(id string) (Chat, bool) {
		return Chat{ID: id, CreatedAt: time.Now().Add(-time.Minute).UnixMilli()}, true
	}
	o := NewObserver(Open(""), of, "")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)
	o.Step("c", "Bash", "$ git -C "+repo+" log --oneline", true, "")
	o.flush(t)
	if es := o.Reg.Of("c"); len(es) != 0 {
		t.Fatalf("records = %+v", es)
	}
}

func TestScanFindsOldWork(t *testing.T) {
	gitEnv(t)
	repo := newRepo(t)
	start := nextSecond()
	if err := os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "add", "c.txt")
	gitT(t, repo, "commit", "-q", "-m", "add c")
	lines := []map[string]any{
		{"type": "user", "message": map[string]any{"content": "see https://gitlab.example.com/g/p/-/merge_requests/1"}},
		{"type": "assistant", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Edit", "input": map[string]any{"file_path": filepath.Join(repo, "c.txt")}},
			map[string]any{"type": "tool_use", "id": "t2", "name": "Bash", "input": map[string]any{"command": "cd " + repo + " && git push"}},
		}}},
		{"type": "user", "message": map[string]any{"content": []any{
			map[string]any{"type": "tool_result", "tool_use_id": "t2", "content": "remote: https://gitlab.example.com/g/p/-/merge_requests/42"},
		}}},
	}
	var b strings.Builder
	for _, l := range lines {
		j, _ := json.Marshal(l)
		b.Write(j)
		b.WriteByte('\n')
	}
	transcript := filepath.Join(t.TempDir(), "s.jsonl")
	if err := os.WriteFile(transcript, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	of := func(id string) (Chat, bool) {
		return Chat{ID: id, Cwd: t.TempDir(), CreatedAt: start, Transcript: transcript}, true
	}
	o := NewObserver(Open(""), of, "")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)
	if !o.Scan("old", 30*time.Second) {
		t.Fatal("scan did not finish")
	}
	es := o.Reg.Of("old")
	if len(es) != 1 {
		t.Fatalf("records = %+v", es)
	}
	if es[0].Source != hubproto.ChatWorkScan || es[0].MR != "https://gitlab.example.com/g/p/-/merge_requests/42" || len(es[0].Commits) != 1 {
		t.Fatalf("record = %+v", es[0])
	}
	if !o.Reg.Scanned("old") {
		t.Fatal("not marked scanned")
	}
}

// The hub's frames reach the observer: a step of a write tool registers the worktree, the end of a turn looks again.
func TestFrameRegistersWrite(t *testing.T) {
	gitEnv(t)
	repo := newRepo(t)
	of := func(id string) (Chat, bool) { return Chat{ID: id, CreatedAt: time.Now().UnixMilli()}, true }
	o := NewObserver(Open(""), of, "")
	done := make(chan struct{})
	defer close(done)
	go o.Run(done)
	step, _ := json.Marshal(map[string]any{"f": "ev", "ch": "conv:c", "seq": 1, "e": map[string]any{"type": "step",
		"entry": map[string]any{"t": "tool", "tool": "Edit", "summary": "Update " + filepath.Join(repo, "a.txt") + " (+1, -0)"}}})
	o.Frame("conv:c", step)
	o.Frame("convs", step)
	o.Frame("conv:c", []byte(`{"f":"ev","ch":"conv:c","seq":2,"e":{"type":"turn_finished","turn":{}}}`))
	o.flush(t)
	es := o.Reg.Of("c")
	if len(es) != 1 || es[0].Branch != "main" || es[0].Head == "" {
		t.Fatalf("records = %+v", es)
	}
}
