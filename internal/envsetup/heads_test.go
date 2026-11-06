package envsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// headsFixture answers a script of a repository outside the list: the clone writes the file, rev-parse and ls-remote
// say the commit in *head, ls-remote exits with *lsExit.
func headsFixture(t *testing.T, head *string, lsExit *int, lsCalls *int) (*fixture, *time.Time) {
	f := newFixture(t)
	clock := time.Unix(1_700_000_000, 0)
	f.r.Now = func() time.Time { return clock }
	f.handle = func(c Cmd) reply {
		if c.Name != "git" {
			return reply{}
		}
		switch {
		case c.Args[0] == "clone":
			dir := c.Args[len(c.Args)-1]
			_ = os.MkdirAll(filepath.Join(dir, "ci"), 0o755)
			_ = os.WriteFile(filepath.Join(dir, "ci", "go.sh"), []byte("echo"), 0o755)
		case contains(c.Args, "rev-parse"):
			return reply{out: []string{*head + "\n"}}
		case c.Args[0] == "ls-remote":
			*lsCalls++
			if *lsExit != 0 {
				return reply{exit: *lsExit}
			}
			return reply{out: []string{*head + "\trefs/heads/main\n"}}
		}
		return reply{}
	}
	f.setList(script("s1", "Плагины", 1, `{"from":"repo","repoUrl":"https://gitlab.com/acme/tools.git","branch":"main","path":"ci/go.sh","rerun":"once"}`))
	return f, &clock
}

func TestRepoScriptRunsAgainWhenItsBranchMoves(t *testing.T) {
	head, lsExit, ls := "aaa", 0, 0
	f, clock := headsFixture(t, &head, &lsExit, &ls)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 1 {
		t.Fatalf("first run: %d", n)
	}
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 1 {
		t.Fatalf("the branch did not move, nothing runs: %d", n)
	}
	head = "bbb"
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 2 {
		t.Fatalf("the branch moved, the script runs again: %d", n)
	}
	runs := f.runsAll()
	var latest hubproto.EnvRun
	for _, r := range runs {
		if r.StartedAt >= latest.StartedAt {
			latest = r
		}
	}
	if latest.Mode != hubproto.RunApply || f.step(latest, "s1").Status != hubproto.EnvOK {
		t.Fatalf("%+v", latest)
	}
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 2 {
		t.Fatalf("the new commit ran once: %d", n)
	}
}

func TestHeadsAreNotCheckedMoreOftenThanHalfAnHour(t *testing.T) {
	head, lsExit, ls := "aaa", 0, 0
	f, clock := headsFixture(t, &head, &lsExit, &ls)
	f.r.Tick(context.Background())
	f.r.Tick(context.Background())
	f.r.Tick(context.Background())
	if ls != 0 {
		t.Fatalf("checked right after the run: %d", ls)
	}
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	f.r.Tick(context.Background())
	if ls != 1 {
		t.Fatalf("once per half an hour: %d", ls)
	}
}

func TestUnreachableBranchStartsNothing(t *testing.T) {
	head, lsExit, ls := "aaa", 0, 0
	f, clock := headsFixture(t, &head, &lsExit, &ls)
	f.r.Tick(context.Background())
	lsExit = 128
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 1 || ls != 1 {
		t.Fatalf("runs %d, ls-remote %d", n, ls)
	}
	if st := f.r.readState(); st.HeadsAt != clock.UnixMilli() {
		t.Fatalf("the check is remembered: %+v", st)
	}
}

func TestAMoveSeenBeforeACrashStillRuns(t *testing.T) {
	head, lsExit, ls := "aaa", 0, 0
	f, clock := headsFixture(t, &head, &lsExit, &ls)
	f.r.Tick(context.Background())
	head = "bbb"
	*clock = clock.Add(31 * time.Minute)
	st := f.r.readState()
	h, _ := f.r.readHeld()
	if !f.r.headsMoved(context.Background(), &st, h.Items) {
		t.Fatal("the move is seen")
	}
	// The runtime stops here; the next tick must still do the run (the look itself is not due again).
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 2 {
		t.Fatalf("the move was lost: %d runs", n)
	}
}

func TestRunOnlyScriptPullsAndKeysOnTheRealCommit(t *testing.T) {
	head, lsExit, ls := "aaa", 0, 0
	f, clock := headsFixture(t, &head, &lsExit, &ls)
	f.setList(script("s1", "Плагины", 1, `{"from":"repo","repoUrl":"https://gitlab.com/acme/tools.git","branch":"main","run":"bash ci/go.sh","rerun":"once"}`))
	pulls := 0
	inner := f.handle
	f.handle = func(c Cmd) reply {
		if c.Name == "git" && contains(c.Args, "pull") {
			pulls++
		}
		return inner(c)
	}
	f.r.Tick(context.Background())
	head = "bbb"
	*clock = clock.Add(31 * time.Minute)
	f.r.Tick(context.Background())
	if n := len(f.scriptRuns()); n != 2 || pulls == 0 {
		t.Fatalf("runs %d, pulls %d", n, pulls)
	}
}
