// Package checkpoints keeps per-conversation snapshots of a working folder in a shadow git repository (one per tab and
// folder, under the runtime's data folder) and returns the folder to one of them. The repositories, their refs, commit
// messages and the rewind log are laid out exactly as the previous runtime laid them out, so either runtime reads what
// the other wrote.
package checkpoints

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"harnessly/internal/jscompat"
	"harnessly/internal/jsvalue"
)

const (
	KindTurn         = "turn"
	KindBeforeRewind = "before-rewind"
)

// Checkpoint is one snapshot: its id ("<ms>-<kind>[-<n>]"), when it was taken, its tree and how many files it holds.
type Checkpoint struct {
	ID    string
	Kind  string
	At    float64
	Tree  string
	Files float64
}

// View is a checkpoint as the API shows it.
func (c Checkpoint) View() *jsvalue.Object {
	return jsvalue.NewObject("id", c.ID, "kind", c.Kind, "at", c.At, "files", c.Files)
}

type Limits struct {
	MaxFileBytes   int64
	MaxFiles       int
	MaxCheckpoints int
	MinIntervalMs  int64
	MaxDiffBytes   int
	GitTimeout     time.Duration
}

var DefaultLimits = Limits{
	MaxFileBytes:   5 * 1024 * 1024,
	MaxFiles:       20_000,
	MaxCheckpoints: 30,
	MinIntervalMs:  3000,
	MaxDiffBytes:   600_000,
	GitTimeout:     30 * time.Second,
}

// Store is where the shadow repositories live and how they are named.
type Store struct {
	// Dir holds the repositories (<data>/checkpoints).
	Dir  string
	Home string
	// Namespace names the refs (refs/<ns>/checkpoints/, refs/<ns>/saved/) and the commits' author (<ns>@localhost).
	Namespace string
	// TmpTag marks the files a rewind stages beside the ones it replaces (<file>.<tag>-<hex>.tmp).
	TmpTag string
	Limits *Limits
	// Now is the clock in milliseconds; nil for the real one.
	Now func() int64
}

// Scope is one tab's folder and its shadow repository.
type Scope struct {
	Dir, Root string
	Limits    Limits
	store     *Store
}

const (
	maxSavedEdits   = 50
	staleRepo       = 14 * 24 * time.Hour
	maxRewindRecord = 50
	noConversion    = "* -text -eol -filter -ident -working-tree-encoding\n"
)

var (
	tabIDRe        = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
	checkpointIDRe = regexp.MustCompile(`^(\d{1,15})-(turn|before-rewind)(?:-(\d{1,4}))?$`)
	filesRe        = regexp.MustCompile(`files=(\d+)`)
	numstatRe      = regexp.MustCompile(`(?s)^(\d+|-)\t(\d+|-)\t(.+)$`)
	rawRe          = regexp.MustCompile(`^:(\d{6}) (\d{6}) ([0-9a-f]+) ([0-9a-f]+) ([A-Z])\d*$`)
	generatedDirs  = []string{"node_modules", ".git", ".next", ".nuxt", ".turbo", ".cache", ".venv", "__pycache__", ".gradle", ".parcel-cache", "coverage"}
)

// locks serialise the work on one repository, as the previous runtime did within its process.
var locks sync.Map

func lockOf(dir string) *sync.Mutex {
	m, _ := locks.LoadOrStore(dir, &sync.Mutex{})
	return m.(*sync.Mutex)
}

func (sc *Scope) lock() func() {
	m := lockOf(sc.Dir)
	m.Lock()
	return m.Unlock
}

func (s *Store) now() int64 {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UnixMilli()
}

func (s *Store) refPrefix() string   { return "refs/" + s.Namespace + "/checkpoints/" }
func (s *Store) savedPrefix() string { return "refs/" + s.Namespace + "/saved/" }

// ValidTabID is the previous runtime's check of a tab id.
func ValidTabID(id string) bool { return tabIDRe.MatchString(id) }

// ValidID is whether id names a checkpoint.
func ValidID(id string) bool { return checkpointIDRe.MatchString(id) }

// IsTurnEnd is whether a change of the agent's state ends a turn.
func IsTurnEnd(prev, next string) bool {
	return (prev == "busy" || prev == "needs-input") && (next == "ready-for-review" || next == "idle")
}

// IsSafeRelPath: a relative path with no empty, ".", ".." or ".git" part.
func IsSafeRelPath(rel string) bool {
	if rel == "" || strings.Contains(rel, "\x00") || strings.HasPrefix(rel, "/") {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." || isDotGit(part) {
			return false
		}
	}
	return true
}

// isDotGit is part.toLowerCase() === '.git'; no other character lowercases into these four.
func isDotGit(part string) bool {
	return len(part) == 4 && part[0] == '.' && strings.EqualFold(part[1:], "git")
}

// Scope is the shadow repository of a tab's folder: the folder resolved, never the root or the home folder.
func (s *Store) Scope(tabID, cwd string) (*Scope, error) {
	if !tabIDRe.MatchString(tabID) {
		return nil, errors.New("bad tab id")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("not a directory")
	}
	home := s.Home
	if real, err := filepath.EvalSymlinks(home); err == nil {
		home = real
	}
	if root == "/" || root == home {
		return nil, errors.New("root is too broad to snapshot")
	}
	sum := sha1.Sum([]byte(root))
	limits := DefaultLimits
	if s.Limits != nil {
		limits = *s.Limits
	}
	return &Scope{Dir: filepath.Join(s.Dir, tabID+"-"+hex.EncodeToString(sum[:])[:10]+".git"), Root: root, Limits: limits, store: s}, nil
}

func (sc *Scope) env() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	ns := sc.store.Namespace
	return append(env, "GIT_DIR="+sc.Dir, "GIT_WORK_TREE="+sc.Root, "GIT_INDEX_FILE="+filepath.Join(sc.Dir, "index"),
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME="+ns, "GIT_AUTHOR_EMAIL="+ns+"@localhost",
		"GIT_COMMITTER_NAME="+ns, "GIT_COMMITTER_EMAIL="+ns+"@localhost")
}

// git runs git on the shadow repository with the working folder as its work tree; a non-zero exit is an error.
func (sc *Scope) git(input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), sc.Limits.GitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "core.autocrlf=false", "-c", "core.quotepath=off", "-c", "gc.auto=0"}, args...)...)
	cmd.Dir = sc.Root
	cmd.Env = sc.env()
	cmd.WaitDelay = 2 * time.Second
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %v: %s", args[0], err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

func (sc *Scope) gitText(input []byte, args ...string) (string, error) {
	out, err := sc.git(input, args...)
	return jscompat.DecodeUTF8(out), err
}

func (sc *Scope) gitNul(input []byte, args ...string) ([]string, error) {
	text, err := sc.gitText(input, args...)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range strings.Split(text, "\x00") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (sc *Scope) pruneStaleRepos(now int64) {
	base := filepath.Dir(sc.Dir)
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		if dir == sc.Dir || !strings.HasSuffix(e.Name(), ".git") {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if st, err := os.Stat(dir); err == nil && now-st.ModTime().UnixMilli() > staleRepo.Milliseconds() {
				_ = os.RemoveAll(dir)
			}
		}()
	}
	wg.Wait()
}

func (sc *Scope) ensureRepo(now int64) error {
	if exists(filepath.Join(sc.Dir, "HEAD")) {
		return nil
	}
	if err := os.MkdirAll(sc.Dir, 0o700); err != nil {
		return err
	}
	if _, err := sc.git(nil, "init", "--quiet", "--template="); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(sc.Dir, "info"), 0o777); err != nil {
		return err
	}
	files := [][2]string{{"info/exclude", strings.Join(generatedDirs, "\n") + "\n"}, {"info/attributes", noConversion}, {"root", sc.Root + "\n"}}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(sc.Dir, f[0]), []byte(f[1]), 0o666); err != nil {
			return err
		}
	}
	sc.pruneStaleRepos(now)
	return nil
}

// sortCheckpoints puts the newest first; at one moment, ids in descending natural order.
func sortCheckpoints(list []Checkpoint) {
	col := collate.New(language.English, collate.Numeric)
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		if a.At != b.At {
			return a.At > b.At
		}
		return col.CompareString(b.ID, a.ID) < 0
	})
}

func parseCheckpoints(stdout string) []Checkpoint {
	out := []Checkpoint{}
	for _, line := range strings.Split(stdout, "\n") {
		parts := strings.Split(line, "\x00")
		id := parts[0]
		tree, subject := "", ""
		if len(parts) > 1 {
			tree = parts[1]
		}
		if len(parts) > 2 {
			subject = parts[2]
		}
		m := checkpointIDRe.FindStringSubmatch(id)
		if id == "" || m == nil || tree == "" {
			continue
		}
		at, _ := strconv.ParseFloat(m[1], 64)
		files := 0.0
		if fm := filesRe.FindStringSubmatch(subject); fm != nil {
			files, _ = strconv.ParseFloat(fm[1], 64)
		}
		out = append(out, Checkpoint{ID: id, Kind: m[2], At: at, Tree: tree, Files: files})
	}
	sortCheckpoints(out)
	return out
}

func (sc *Scope) listLocked() ([]Checkpoint, error) {
	if !exists(filepath.Join(sc.Dir, "HEAD")) {
		return []Checkpoint{}, nil
	}
	out, err := sc.gitText(nil, "for-each-ref", "--format=%(refname:lstrip=3)%00%(tree)%00%(contents:subject)", sc.store.refPrefix())
	if err != nil {
		return nil, err
	}
	return parseCheckpoints(out), nil
}

var errTooManyFiles = errors.New("too-many-files")

// snapshotTree builds the tree of the working folder in the shadow index: what .gitignore and the limits let through.
func (sc *Scope) snapshotTree() (tree string, files int, err error) {
	var others, cached, ignored []string
	var errs [3]error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		others, errs[0] = sc.gitNul(nil, "ls-files", "-z", "--others", "--exclude-standard")
	}()
	go func() { defer wg.Done(); cached, errs[1] = sc.gitNul(nil, "ls-files", "-z", "--cached") }()
	go func() {
		defer wg.Done()
		ignored, errs[2] = sc.gitNul(nil, "ls-files", "-z", "--cached", "--ignored", "--exclude-standard")
	}()
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return "", 0, e
		}
	}
	ignoredSet := map[string]bool{}
	for _, n := range ignored {
		ignoredSet[n] = true
	}
	seen := map[string]bool{}
	var names []string
	add := func(n string) {
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	for _, n := range others {
		if !strings.HasSuffix(n, "/") {
			add(n)
		}
	}
	for _, n := range cached {
		if !ignoredSet[n] {
			add(n)
		}
	}
	var keep []string
	for _, n := range names {
		if !IsSafeRelPath(n) {
			continue
		}
		st, err := os.Lstat(filepath.Join(sc.Root, n))
		if err != nil {
			continue
		}
		if st.Mode()&os.ModeSymlink != 0 || (st.Mode().IsRegular() && st.Size() <= sc.Limits.MaxFileBytes) {
			keep = append(keep, n)
		}
	}
	if len(keep) > sc.Limits.MaxFiles {
		return "", 0, errTooManyFiles
	}
	kept := map[string]bool{}
	for _, n := range keep {
		kept[n] = true
	}
	var stale []string
	for _, n := range cached {
		if !kept[n] {
			stale = append(stale, n)
		}
	}
	if len(stale) > 0 {
		if _, err := sc.git([]byte(strings.Join(stale, "\x00")+"\x00"), "update-index", "--force-remove", "-z", "--stdin"); err != nil {
			return "", 0, err
		}
	}
	if len(keep) > 0 {
		if _, err := sc.git([]byte(strings.Join(keep, "\x00")+"\x00"), "update-index", "--add", "--replace", "-z", "--stdin"); err != nil {
			return "", 0, err
		}
	}
	out, err := sc.gitText(nil, "write-tree")
	if err != nil {
		return "", 0, err
	}
	return jscompat.Trim(out), len(keep), nil
}

func (sc *Scope) trimOldest() error {
	all, err := sc.listLocked()
	if err != nil {
		return err
	}
	if len(all) <= sc.Limits.MaxCheckpoints {
		return nil
	}
	for _, c := range all[sc.Limits.MaxCheckpoints:] {
		if _, err := sc.git(nil, "update-ref", "-d", sc.store.refPrefix()+c.ID); err != nil {
			return err
		}
	}
	_, _ = sc.git(nil, "prune", "--expire=10.minutes.ago")
	return nil
}

// Snapshot outcomes besides success.
const (
	Throttled    = "throttled"
	TooManyFiles = "too-many-files"
	GitFailed    = "git-failed"
)

func (sc *Scope) createLocked(kind string, now int64) (Checkpoint, string) {
	if err := sc.ensureRepo(now); err != nil {
		return Checkpoint{}, GitFailed
	}
	existing, err := sc.listLocked()
	if err != nil {
		return Checkpoint{}, GitFailed
	}
	if kind == KindTurn {
		for _, c := range existing {
			if c.Kind == KindTurn {
				if float64(now)-c.At < float64(sc.Limits.MinIntervalMs) {
					return Checkpoint{}, Throttled
				}
				break
			}
		}
	}
	tree, files, err := sc.snapshotTree()
	if errors.Is(err, errTooManyFiles) {
		return Checkpoint{}, TooManyFiles
	}
	if err != nil {
		return Checkpoint{}, GitFailed
	}
	taken := map[string]bool{}
	for _, c := range existing {
		taken[c.ID] = true
	}
	base := strconv.FormatInt(now, 10) + "-" + kind
	id := base
	for n := 2; taken[id]; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	commit, err := sc.gitText(nil, "commit-tree", tree, "-m", "checkpoint files="+strconv.Itoa(files))
	if err != nil {
		return Checkpoint{}, GitFailed
	}
	if _, err := sc.git(nil, "update-ref", sc.store.refPrefix()+id, jscompat.Trim(commit)); err != nil {
		return Checkpoint{}, GitFailed
	}
	t := time.UnixMilli(now)
	_ = os.Chtimes(sc.Dir, t, t)
	return Checkpoint{ID: id, Kind: kind, At: float64(now), Tree: tree, Files: float64(files)}, ""
}

// Take snapshots the working folder as a turn's checkpoint; the person's own repository is never touched. The reason
// is "" on success.
func (sc *Scope) Take(now int64) (Checkpoint, string) {
	defer sc.lock()()
	c, reason := sc.createLocked(KindTurn, now)
	if reason == "" {
		_ = sc.trimOldest()
	}
	return c, reason
}

// List is the checkpoints, the newest first.
func (sc *Scope) List() ([]Checkpoint, error) {
	defer sc.lock()()
	return sc.listLocked()
}

func (sc *Scope) rewindsFile() string { return filepath.Join(sc.Dir, "rewinds.json") }

func (sc *Scope) readRewinds() []any {
	raw, err := os.ReadFile(sc.rewindsFile())
	if err != nil {
		return []any{}
	}
	v, err := jsvalue.Parse([]byte(jscompat.DecodeUTF8(raw)))
	if arr, ok := v.([]any); err == nil && ok {
		return arr
	}
	return []any{}
}

// Rewinds is the log of the rewinds, as written.
func (sc *Scope) Rewinds() []any {
	defer sc.lock()()
	return sc.readRewinds()
}

func (sc *Scope) recordRewind(record *jsvalue.Object) error {
	records := append(sc.readRewinds(), record)
	if len(records) > maxRewindRecord {
		records = records[len(records)-maxRewindRecord:]
	}
	var b [4]byte
	_, _ = rand.Read(b[:])
	tmp := sc.rewindsFile() + "." + hex.EncodeToString(b[:]) + ".tmp"
	if err := os.WriteFile(tmp, jsvalue.Stringify(records, ""), 0o666); err != nil {
		return err
	}
	return os.Rename(tmp, sc.rewindsFile())
}

// truncateDiff cuts a patch at the last file boundary before max (UTF-16 units, as the previous runtime counted).
func truncateDiff(diff string, max int) (string, bool) {
	if jscompat.Len(diff) <= max {
		return diff, false
	}
	cut := 0
	if boundary := jscompat.LastIndexOf(diff, "\ndiff --git", max); boundary > 0 {
		cut = boundary + 1
	} else if cut = jscompat.LastIndexOf(diff, "\n", max) + 1; cut == 0 {
		cut = max
	}
	return jscompat.Slice(diff, cut), true
}

type stat struct{ additions, deletions float64 }

func parseNumstat(tokens []string) map[string]stat {
	out := map[string]stat{}
	num := func(s string) float64 {
		if s == "-" {
			return 0
		}
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	for _, t := range tokens {
		if m := numstatRe.FindStringSubmatch(t); m != nil {
			out[m[3]] = stat{num(m[1]), num(m[2])}
		}
	}
	return out
}

type rawChange struct {
	status, oldMode, newMode, oldSha, newSha, path string
}

func parseRaw(tokens []string) []rawChange {
	var out []rawChange
	for i := 0; i+1 < len(tokens); i += 2 {
		if m := rawRe.FindStringSubmatch(tokens[i]); m != nil {
			out = append(out, rawChange{oldMode: m[1], newMode: m[2], oldSha: m[3], newSha: m[4], status: m[5], path: tokens[i+1]})
		}
	}
	return out
}

func (sc *Scope) diffTrees(from, to string) ([]rawChange, error) {
	tokens, err := sc.gitNul(nil, "diff-tree", "-r", "--no-renames", "-z", "--raw", from, to)
	if err != nil {
		return nil, err
	}
	return parseRaw(tokens), nil
}

// Diff is what a rewind would change, as a patch from the working folder to the snapshot.
type Diff struct {
	Diff      string
	Truncated bool
	Files     []*jsvalue.Object
}

// JSON is the diff as the API answers it.
func (d *Diff) JSON() *jsvalue.Object {
	files := make([]any, len(d.Files))
	for i, f := range d.Files {
		files[i] = f
	}
	return jsvalue.NewObject("diff", d.Diff, "truncated", d.Truncated, "files", files)
}

// DiffTo compares the working folder with a snapshot; nil when there is no such snapshot. It writes nothing to the
// working folder (the shadow index is refreshed).
func (sc *Scope) DiffTo(id string) (*Diff, error) {
	defer sc.lock()()
	return sc.diffLocked(id)
}

func (sc *Scope) diffLocked(id string) (*Diff, error) {
	if !ValidID(id) {
		return nil, nil
	}
	list, err := sc.listLocked()
	if err != nil {
		return nil, err
	}
	var target *Checkpoint
	for i := range list {
		if list[i].ID == id {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return nil, nil
	}
	current, _, err := sc.snapshotTree()
	if err != nil {
		return nil, err
	}
	var raw []rawChange
	var numstat []string
	var patch string
	var errs [3]error
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); raw, errs[0] = sc.diffTrees(current, target.Tree) }()
	go func() {
		defer wg.Done()
		numstat, errs[1] = sc.gitNul(nil, "diff-tree", "-r", "--no-renames", "-z", "--numstat", current, target.Tree)
	}()
	go func() {
		defer wg.Done()
		patch, errs[2] = sc.gitText(nil, "diff-tree", "-r", "-p", "--no-renames", "--no-color", "--no-ext-diff", current, target.Tree)
	}()
	wg.Wait()
	for _, e := range errs {
		if e != nil {
			return nil, e
		}
	}
	stats := parseNumstat(numstat)
	files := make([]*jsvalue.Object, 0, len(raw))
	for _, c := range raw {
		status := c.status
		if status != "A" && status != "D" && status != "T" {
			status = "M"
		}
		s := stats[c.path]
		files = append(files, jsvalue.NewObject("path", c.path, "status", status, "additions", s.additions, "deletions", s.deletions))
	}
	d, truncated := truncateDiff(patch, sc.Limits.MaxDiffBytes)
	return &Diff{Diff: d, Truncated: truncated, Files: files}, nil
}

func blobSha(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

type currentFile struct {
	exists  bool
	sha     string // "" when it is neither a plain file nor a symlink, or too big
	mode    os.FileMode
	content []byte
}

func readCurrent(full string, maxBytes int64) (currentFile, error) {
	st, err := os.Lstat(full)
	if err != nil {
		return currentFile{}, nil
	}
	f := currentFile{exists: true, mode: st.Mode()}
	if st.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(full)
		if err != nil {
			return f, err
		}
		f.content = []byte(target)
	} else if st.Mode().IsRegular() && st.Size() <= maxBytes {
		if f.content, err = os.ReadFile(full); err != nil {
			return f, err
		}
		if f.content == nil {
			f.content = []byte{}
		}
	}
	if f.content != nil {
		f.sha = blobSha(f.content)
	}
	return f, nil
}

// differsFrom: the file exists and is not the content seen in the snapshot ("" for none).
func differsFrom(f currentFile, seen string) bool {
	return f.exists && (f.sha == "" || f.sha != seen)
}

// saveEdit keeps content found in a file the rewind did not dare to touch, reachable so pruning leaves it.
func (sc *Scope) saveEdit(content []byte) string {
	if content == nil {
		return ""
	}
	out, err := sc.gitText(content, "hash-object", "-w", "--no-filters", "--stdin")
	if err != nil {
		return ""
	}
	sha := jscompat.Trim(out)
	if _, err := sc.git(nil, "update-ref", sc.store.savedPrefix()+strconv.FormatInt(sc.store.now(), 10)+"-"+jscompat.Slice(sha, 8), sha); err != nil {
		return ""
	}
	list, err := sc.gitText(nil, "for-each-ref", "--format=%(refname)", sc.store.savedPrefix())
	if err != nil {
		return ""
	}
	var refs []string
	for _, r := range strings.Split(list, "\n") {
		if r != "" {
			refs = append(refs, r)
		}
	}
	sort.Strings(refs)
	if len(refs) > maxSavedEdits {
		for _, r := range refs[:len(refs)-maxSavedEdits] {
			if _, err := sc.git(nil, "update-ref", "-d", r); err != nil {
				return ""
			}
		}
	}
	return sha
}

// prepareParents walks the parents of rel inside root: a symlink or a file on the way makes the path unsafe.
func prepareParents(root, rel string, create bool) string {
	parts := strings.Split(rel, "/")
	dir := root
	for _, part := range parts[:len(parts)-1] {
		dir = filepath.Join(dir, part)
		st, err := os.Lstat(dir)
		if err != nil {
			if !create {
				return "missing"
			}
			_ = os.Mkdir(dir, 0o777)
			st, err = os.Lstat(dir)
		}
		if err != nil || !st.IsDir() {
			return "unsafe"
		}
	}
	return "ok"
}

func removeEmptyParents(root, rel string) {
	dir := filepath.Dir(filepath.Join(root, rel))
	for dir != root && strings.HasPrefix(dir, root+"/") {
		if syscall.Rmdir(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// Hooks are the moments a rewind asks about or lets a test act in.
type Hooks struct {
	// IsBusy is asked once the snapshot before the rewind exists and before anything is written: true stops it.
	IsBusy func() bool
	// BeforeApply runs once the rewind is decided; BeforeReplace right before a file is replaced or deleted.
	BeforeApply   func()
	BeforeReplace func(rel string)
}

// Skipped is a file the rewind left alone, and why.
type Skipped struct {
	Path, Reason string
	// Blob holds the content found in a file that turned out to have changed, kept in the shadow repository.
	Blob string
}

// Rewind outcomes besides success.
const (
	NotFound       = "not-found"
	SnapshotFailed = "snapshot-failed"
	Busy           = "busy"
)

// RewindResult is a rewind done (Reason "") or refused.
type RewindResult struct {
	Reason  string
	Before  Checkpoint
	Changed []string
	Skipped []Skipped
}

// JSON is the answer to a done rewind.
func (r *RewindResult) JSON() *jsvalue.Object {
	changed := make([]any, len(r.Changed))
	for i, c := range r.Changed {
		changed[i] = c
	}
	skipped := make([]any, len(r.Skipped))
	for i, s := range r.Skipped {
		o := jsvalue.NewObject("path", s.Path, "reason", s.Reason)
		if s.Blob != "" {
			o.Set("blob", s.Blob)
		}
		skipped[i] = o
	}
	return jsvalue.NewObject("before", r.Before.View(), "changed", changed, "skipped", skipped)
}

func randomHex() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// applyChange checks the file twice: first to skip early, then again with the replacement already written beside it,
// right before the rename (or unlink), so an edit can be overwritten only within the rename itself.
func (sc *Scope) applyChange(c rawChange, hooks Hooks) (status, blob string, err error) {
	root := sc.Root
	full := filepath.Join(root, c.path)
	if !IsSafeRelPath(c.path) {
		return "unsafe", "", nil
	}
	removal := c.status == "D"
	if !removal && c.newMode == "160000" {
		return "unsupported", "", nil
	}
	switch prepareParents(root, c.path, !removal) {
	case "unsafe":
		return "unsafe", "", nil
	case "missing":
		return "noop", "", nil
	}
	seen := c.oldSha
	if c.status == "A" {
		seen = ""
	}
	now, err := readCurrent(full, sc.Limits.MaxFileBytes)
	if err != nil {
		return "", "", err
	}
	if differsFrom(now, seen) {
		return "changed", "", nil
	}
	if !now.exists && removal {
		return "noop", "", nil
	}
	tmp := full + "." + sc.store.TmpTag + "-" + randomHex() + ".tmp"
	fail := func(e error) (string, string, error) {
		_ = os.RemoveAll(tmp)
		return "", "", e
	}
	if !removal {
		base := os.FileMode(0o644)
		if now.exists {
			base = now.mode & 0o777
		}
		mode := base &^ 0o111
		if c.newMode == "100755" {
			mode = base | 0o111
		}
		content, err := sc.git(nil, "cat-file", "blob", c.newSha)
		if err != nil {
			return fail(err)
		}
		if c.newMode == "120000" {
			if err := os.Symlink(string(content), tmp); err != nil {
				return fail(err)
			}
		} else {
			if err := os.WriteFile(tmp, content, mode); err != nil {
				return fail(err)
			}
			if err := os.Chmod(tmp, mode); err != nil {
				return fail(err)
			}
		}
	}
	if hooks.BeforeReplace != nil {
		hooks.BeforeReplace(c.path)
	}
	again, err := readCurrent(full, sc.Limits.MaxFileBytes)
	if err != nil {
		return fail(err)
	}
	if differsFrom(again, seen) || (!again.exists && seen != "") {
		_ = os.RemoveAll(tmp)
		return "changed", sc.saveEdit(again.content), nil
	}
	if removal {
		if err := os.Remove(full); err != nil {
			return fail(err)
		}
		removeEmptyParents(root, c.path)
	} else if err := os.Rename(tmp, full); err != nil {
		return fail(err)
	}
	return "applied", "", nil
}

// Rewind returns the working folder to a snapshot. The current state is snapshotted first (no rewind without it), only
// files both states know are written, and a file that changed in the meantime is skipped.
func (sc *Scope) Rewind(id string, hooks Hooks) (RewindResult, error) {
	defer sc.lock()()
	return sc.rewindLocked(id, hooks)
}

func (sc *Scope) rewindLocked(id string, hooks Hooks) (RewindResult, error) {
	if !ValidID(id) {
		return RewindResult{Reason: NotFound}, nil
	}
	list, err := sc.listLocked()
	if err != nil {
		return RewindResult{}, err
	}
	var target *Checkpoint
	for i := range list {
		if list[i].ID == id {
			target = &list[i]
			break
		}
	}
	if target == nil {
		return RewindResult{Reason: NotFound}, nil
	}
	saved, reason := sc.createLocked(KindBeforeRewind, sc.store.now())
	if reason != "" {
		return RewindResult{Reason: SnapshotFailed}, nil
	}
	if hooks.IsBusy != nil && hooks.IsBusy() {
		_, _ = sc.git(nil, "update-ref", "-d", sc.store.refPrefix()+saved.ID)
		return RewindResult{Reason: Busy}, nil
	}
	if hooks.BeforeApply != nil {
		hooks.BeforeApply()
	}
	changes, err := sc.diffTrees(saved.Tree, target.Tree)
	if err != nil {
		return RewindResult{}, err
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].status == "D" && changes[j].status != "D" })
	result := RewindResult{Before: saved, Changed: []string{}, Skipped: []Skipped{}}
	for _, c := range changes {
		status, blob, err := sc.applyChange(c, hooks)
		if err != nil {
			status = "failed"
		}
		switch status {
		case "applied":
			result.Changed = append(result.Changed, c.path)
		case "noop":
		default:
			result.Skipped = append(result.Skipped, Skipped{Path: c.path, Reason: status, Blob: blob})
		}
	}
	_ = sc.recordRewind(jsvalue.NewObject("at", float64(sc.store.now()), "targetId", target.ID, "targetAt", target.At, "beforeId", saved.ID,
		"changed", len(result.Changed), "skipped", len(result.Skipped)))
	_ = sc.trimOldest()
	return result, nil
}
