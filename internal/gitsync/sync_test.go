package gitsync

import "testing"

// The routes' golden (cmd/runtime) runs real repositories; these are the kinds a local remote cannot produce.
func TestClassify(t *testing.T) {
	for _, c := range []struct {
		r    result
		want string
	}{
		{result{killed: true, stderr: "fatal: no upstream"}, "timeout"},
		{result{stderr: "fatal: Authentication failed for 'https://example.com/x.git/'"}, "auth"},
		{result{stderr: "git@example.com: Permission denied (publickey).\nfatal: Could not read from remote repository."}, "auth"},
		{result{stderr: "To example.com:x.git\n ! [rejected]        main -> main (non-fast-forward)\nerror: failed to push some refs"}, "rejected"},
		{result{stderr: "fatal: Not possible to fast-forward, aborting."}, "diverged"},
		{result{stderr: " ! [remote rejected] main -> main (pre-receive hook declined)"}, "rejected"},
		{result{stdout: "There is no tracking information for the current branch."}, "no-upstream"},
		{result{stderr: "error: Your local changes to the following files would be overwritten by merge:"}, "local-changes"},
		{result{stderr: "something else"}, "unknown"},
	} {
		if got := classify(c.r); got != c.want {
			t.Errorf("%q: %s, want %s", c.r.stderr+c.r.stdout, got, c.want)
		}
	}
}
