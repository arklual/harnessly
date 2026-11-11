package checkpoints

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The pool is one object store per working folder, shared by the shadow repositories of the tabs opened in it
// (Store.Pool). A tab's repository borrows from the pool through git's alternates, so the first snapshot of a tab
// writes only what no other tab of the folder has snapshotted yet. From time to time a repository packs what only it
// holds and hands the pack over; the pool keeps a copy of every tab's refs (refs/tabs/<hex of the tab id>/) so its own
// collection knows what is in use, and it drops an unreachable object only after poolGrace, longer than a repository
// lives without a snapshot.
//
// A runtime without the pool reads such a repository as any other: alternates are git's own, and the pool's folder has
// no ".git" suffix, so the stale sweep of an older runtime leaves it alone.

const (
	// poolEvery is how often a repository hands its objects over.
	poolEvery = time.Hour
	// poolGCEvery is how often a pool is repacked into one pack.
	poolGCEvery = 24 * time.Hour
	// poolGrace is how long the pool keeps an object no ref reaches; staleRepo is 14 days.
	poolGrace      = "3.weeks.ago"
	poolGitTimeout = 5 * time.Minute
	poolMark       = "pooled"
	poolGCMark     = "collected"
)

// poolWork serialises the long work on a pool (packs moving in, collection); poolInit its creation and removal.
var poolWork, poolInit, sweeps sync.Map

func mutexOf(m *sync.Map, key string) *sync.Mutex {
	v, _ := m.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// repoName splits "<tab id>-<10 hex of the folder>.git".
func repoName(dir string) (tabID, key string, ok bool) {
	n := strings.TrimSuffix(filepath.Base(dir), ".git")
	if len(n) < 12 || n[len(n)-11] != '-' || n == filepath.Base(dir) {
		return "", "", false
	}
	return n[:len(n)-11], n[len(n)-10:], true
}

func (sc *Scope) poolDir() string {
	_, key, _ := repoName(sc.Dir)
	return filepath.Join(filepath.Dir(sc.Dir), "pool-"+key)
}

// gitAt runs git on the repository at dir, with no work tree.
func gitAt(dir string, input []byte, env []string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), poolGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "gc.auto=0", "-c", "core.quotepath=off"}, args...)...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(append(cmd.Env, "GIT_DIR="+dir, "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"), env...)
	cmd.WaitDelay = 2 * time.Second
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// keepMtime puts a repository folder's time back: it is the time of its last snapshot, which the stale sweep reads.
func keepMtime(dir string) func() {
	st, err := os.Stat(dir)
	if err != nil {
		return func() {}
	}
	return func() { _ = os.Chtimes(dir, st.ModTime(), st.ModTime()) }
}

// attachPool makes the folder's pool if there is none and points the repository's alternates at it.
func (sc *Scope) attachPool(now int64) error {
	pool := sc.poolDir()
	m := mutexOf(&poolInit, pool)
	m.Lock()
	defer m.Unlock()
	if !exists(filepath.Join(pool, "HEAD")) {
		if err := os.MkdirAll(pool, 0o700); err != nil {
			return err
		}
		if _, err := gitAt(pool, nil, nil, "init", "--quiet", "--bare", "--template="); err != nil {
			return err
		}
		touch(filepath.Join(pool, poolGCMark), now)
	}
	alt := filepath.Join(sc.Dir, "objects", "info", "alternates")
	if exists(alt) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(alt), 0o777); err != nil {
		return err
	}
	// Relative to the repository's objects folder, so the data folder can move.
	return os.WriteFile(alt, []byte("../../"+filepath.Base(pool)+"/objects\n"), 0o666)
}

func touch(path string, now int64) {
	_ = os.WriteFile(path, nil, 0o666)
	t := time.UnixMilli(now)
	_ = os.Chtimes(path, t, t)
}

func olderThan(path string, now int64, d time.Duration) bool {
	st, err := os.Stat(path)
	return err != nil || now-st.ModTime().UnixMilli() >= d.Milliseconds()
}

// handOver packs what only this repository holds, moves the pack into the pool and copies the refs there. The scope's
// lock is held. A pool busy with its collection is left for the next time.
func (sc *Scope) handOver(now int64) error {
	defer keepMtime(sc.Dir)()
	if err := sc.attachPool(now); err != nil {
		return err
	}
	pool := sc.poolDir()
	work := mutexOf(&poolWork, pool)
	if !work.TryLock() {
		return nil
	}
	defer work.Unlock()
	index := []string{"GIT_INDEX_FILE=" + filepath.Join(sc.Dir, "index")}
	_, _ = gitAt(sc.Dir, nil, index, "prune", "--expire=10.minutes.ago")
	// -l leaves out what the pool already has; what is loose here and packed there goes with -d.
	if _, err := gitAt(sc.Dir, nil, index, "repack", "-a", "-d", "-l", "-q"); err != nil {
		return err
	}
	if err := movePacks(filepath.Join(sc.Dir, "objects", "pack"), filepath.Join(pool, "objects", "pack")); err != nil {
		return err
	}
	if err := mirrorRefs(sc.Dir, pool, sc.store.Namespace); err != nil {
		return err
	}
	touch(filepath.Join(sc.Dir, poolMark), now)
	return nil
}

// movePacks renames every pack of from into to, the index last: git sees a pack once its index is there.
func movePacks(from, to string) error {
	entries, err := os.ReadDir(from)
	if err != nil {
		return nil
	}
	if err := os.MkdirAll(to, 0o777); err != nil {
		return err
	}
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".pack")
		if !ok || !strings.HasPrefix(base, "pack-") {
			continue
		}
		for _, ext := range []string{".pack", ".rev", ".idx"} {
			src := filepath.Join(from, base+ext)
			if !exists(src) {
				continue
			}
			if err := os.Rename(src, filepath.Join(to, base+ext)); err != nil {
				return err
			}
		}
	}
	return nil
}

func tabRefs(tabID string) string { return "refs/tabs/" + hex.EncodeToString([]byte(tabID)) + "/" }

// mirrorRefs makes the pool's copy of a repository's refs equal to the refs.
func mirrorRefs(repo, pool, ns string) error {
	tabID, _, ok := repoName(repo)
	if !ok {
		return nil
	}
	mine, err := gitAt(repo, nil, nil, "for-each-ref", "--format=%(objectname) %(refname)", "refs/"+ns+"/")
	if err != nil {
		return err
	}
	prefix := tabRefs(tabID)
	theirs, err := gitAt(pool, nil, nil, "for-each-ref", "--format=%(refname)", prefix)
	if err != nil {
		return err
	}
	var in strings.Builder
	keep := map[string]bool{}
	for _, line := range strings.Split(mine, "\n") {
		sha, ref, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		name := prefix + strings.TrimPrefix(ref, "refs/")
		keep[name] = true
		in.WriteString("update " + name + " " + sha + "\n")
	}
	for _, name := range strings.Split(theirs, "\n") {
		if name != "" && !keep[name] {
			in.WriteString("delete " + name + "\n")
		}
	}
	if in.Len() == 0 {
		return nil
	}
	_, err = gitAt(pool, []byte(in.String()), nil, "update-ref", "--stdin")
	return err
}

// collect repacks a pool: the refs of every repository of the folder are copied first, the refs of the repositories
// that are gone are dropped, and a pool no repository borrows from is removed.
func (s *Store) collect(pool string, now int64) error {
	key := strings.TrimPrefix(filepath.Base(pool), "pool-")
	work := mutexOf(&poolWork, pool)
	work.Lock()
	defer work.Unlock()
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for _, e := range entries {
		tabID, k, ok := repoName(e.Name())
		if !ok || k != key {
			continue
		}
		repo := filepath.Join(s.Dir, e.Name())
		if !exists(filepath.Join(repo, "HEAD")) {
			continue
		}
		live[tabRefs(tabID)] = true
		if err := mirrorRefs(repo, pool, s.Namespace); err != nil {
			return err
		}
	}
	if len(live) == 0 {
		m := mutexOf(&poolInit, pool)
		m.Lock()
		defer m.Unlock()
		return os.RemoveAll(pool)
	}
	refs, err := gitAt(pool, nil, nil, "for-each-ref", "--format=%(refname)", "refs/tabs/")
	if err != nil {
		return err
	}
	var in strings.Builder
	for _, name := range strings.Split(refs, "\n") {
		parts := strings.SplitN(name, "/", 4)
		if len(parts) == 4 && !live[strings.Join(parts[:3], "/")+"/"] {
			in.WriteString("delete " + name + "\n")
		}
	}
	if in.Len() > 0 {
		if _, err := gitAt(pool, []byte(in.String()), nil, "update-ref", "--stdin"); err != nil {
			return err
		}
	}
	if _, err := gitAt(pool, nil, nil, "-c", "gc.cruftPacks=true", "gc", "--quiet", "--prune="+poolGrace); err != nil {
		return err
	}
	touch(filepath.Join(pool, poolGCMark), now)
	return nil
}

// pool is the pool's share of a snapshot: hand the objects over when it is time, and collect the pool when it is its.
func (sc *Scope) pool(now int64) {
	if !olderThan(filepath.Join(sc.Dir, poolMark), now, poolEvery) {
		return
	}
	if err := sc.handOver(now); err != nil {
		return
	}
	if pool := sc.poolDir(); olderThan(filepath.Join(pool, poolGCMark), now, poolGCEvery) {
		touch(filepath.Join(pool, poolGCMark), now)
		go func() { _ = sc.store.collect(pool, now) }()
	}
}

// Sweep hands over the objects of every repository that never did, the ones of closed tabs too, and collects the
// pools. One repository at a time, with a pause, so a data folder of hundreds of them does not take the disk.
func (s *Store) Sweep(pause time.Duration) {
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	now := s.now()
	limits := DefaultLimits
	if s.Limits != nil {
		limits = *s.Limits
	}
	pools := map[string]bool{}
	for _, e := range entries {
		if _, _, ok := repoName(e.Name()); !ok {
			continue
		}
		dir := filepath.Join(s.Dir, e.Name())
		sc := &Scope{Dir: dir, Limits: limits, store: s}
		pools[sc.poolDir()] = true
		if !exists(filepath.Join(dir, "HEAD")) || exists(filepath.Join(dir, poolMark)) {
			continue
		}
		unlock := sc.lock()
		_ = sc.handOver(now)
		unlock()
		time.Sleep(pause)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "pool-") {
			pools[filepath.Join(s.Dir, e.Name())] = true
		}
	}
	for pool := range pools {
		if exists(filepath.Join(pool, "HEAD")) {
			_ = s.collect(pool, now)
		}
	}
}

// sweepOnce starts the sweep of this data folder once in the life of the process.
func (s *Store) sweepOnce() {
	once, _ := sweeps.LoadOrStore(s.Dir, &sync.Once{})
	once.(*sync.Once).Do(func() { go s.Sweep(500 * time.Millisecond) })
}
