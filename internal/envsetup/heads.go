package envsetup

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// A script of a repository outside the list (`repoUrl`) runs from the repository's current commit, but nothing in the
// list changes when that repository does. So a machine done with its list looks at those branches every half an hour,
// and a branch that moved is a reason to work on the list again: the script runs from the new commit (a `once` script
// too, its ledger keys on the commit).

// repoURLScript says whether an item is a script of a repository outside the list.
func repoURLScript(it hubproto.EnvItem, src source) bool {
	return it.Kind == hubproto.EnvKindScript && (src.From == "" || src.From == "repo") && src.Repo == "" && src.RepoURL != ""
}

// packFromURL says whether an item is an environment pack in a repository outside the list: a new commit of that
// repository is a new version of the pack, which is how a pack reaches the machines without a new harnessly.
func packFromURL(it hubproto.EnvItem, src source) bool {
	return it.Kind == hubproto.EnvKindPack && src.Repo == "" && src.RepoURL != ""
}

// onceHash is what the ledger of a `once` script keys on: its source, and for a script of a repository outside the
// list also the commit it runs from (head; "" when not known yet).
func (ex *execution) onceHash(i int, head string) string {
	it := ex.items[i]
	if head == "" || !repoURLScript(it, ex.srcs[i]) {
		return hashOf(it.Source)
	}
	return hashOf(append(append([]byte{}, it.Source...), []byte("@"+head)...))
}

// headsMoved looks at the branches of the list's scripts of repositories outside it, at most every retryEvery, and
// says whether one moved since it was last seen. The commits seen go into the state, so the run that follows keys its
// ledger on them. A branch it cannot read (no network, no access) changes nothing.
func (r *Runner) headsMoved(ctx context.Context, st *hubproto.EnvSetupState, all []hubproto.EnvItem) bool {
	type look struct{ id, url, branch string }
	var looks []look
	for _, it := range setupItems(all) {
		var src source
		if len(it.Source) == 0 || json.Unmarshal(it.Source, &src) != nil || !(repoURLScript(it, src) || packFromURL(it, src)) || gitURLOK(src.RepoURL) != nil {
			continue
		}
		looks = append(looks, look{it.ItemID, src.RepoURL, src.Branch})
	}
	since := st.HeadsAt
	if since == 0 {
		since = st.At // never looked: the run that cloned them saw them
	}
	if len(looks) == 0 || r.now().Sub(time.UnixMilli(since)) < retryEvery {
		return false
	}
	ex := &execution{r: r, ctx: ctx, home: r.Home}
	moved := false
	for _, l := range looks {
		ref := "HEAD"
		if l.branch != "" {
			ref = "refs/heads/" + l.branch
		}
		out, exit, err := ex.quiet(Cmd{Name: "git", Args: []string{"ls-remote", "--", l.url, ref}, Env: ex.baseEnv(), Timeout: loginTimeout})
		if err != nil || exit != 0 {
			continue
		}
		f := strings.Fields(out)
		if len(f) == 0 {
			continue
		}
		if st.Heads == nil {
			st.Heads = map[string]string{}
		}
		if seen := st.Heads[l.id]; seen != "" && seen != f[0] {
			moved = true
		}
		st.Heads[l.id] = f[0]
	}
	st.HeadsAt = r.now().UnixMilli()
	st.Moved = st.Moved || moved
	if err := r.writeState(*st); err != nil {
		r.logf("warn", "envsetup: state not written", "error", err.Error())
	}
	return moved
}
