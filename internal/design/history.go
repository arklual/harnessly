package design

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/jscompat"
	"github.com/ly-solutions/harnessly/internal/locale"
)

// The history of the shared library: a git repository, snapshotted a while after the files stop changing and rolled
// back per board. The repository is this runtime's own and lives outside the shared folder (the data dir, 0700): the
// folder is written by other Unix users and by every agent, so anything git would read from it as configuration must
// never be read. Git runs with --git-dir and --work-tree, so no .git of the folder is looked for, and with a config of
// its own. A snapshot with nothing new is not a commit, and a restore is a new snapshot on top: nothing here deletes
// history. A subfolder holding a .git of its own is left out of what is saved and looked at.

const (
	historyQuiet     = 15 * time.Second
	historyRetry     = 5 * time.Second
	historyPoll      = 20 * time.Second
	historyRetries   = 12
	historyStaleLock = 10 * time.Minute
	historyGitTime   = 10 * time.Minute
	embeddedBudget   = 50_000
	maxSubjectBoards = 3
	maxEditors       = 5
)

const historyRepoConfig = "[core]\n\trepositoryformatversion = 0\n\tfilemode = false\n\tbare = false\n\tlogallrefupdates = true\n[gc]\n\tautoDetach = false\n"

var (
	historyIgnoredDirs   = []string{".claims", ".check", ".thumbs", ".trash", ".claude", ".playwright-mcp", ".remember", "node_modules"}
	historyStagePrefixes = []string{".importing-", ".creating-"}
	lockBusy             = regexp.MustCompile(`(?i)index\.lock|Unable to create .*\.lock|another git process`)
	addSkippable         = regexp.MustCompile(`(?i)unable to (index|stat)|Permission denied|failed to insert`)
	nothingToCommit      = regexp.MustCompile(`(?i)nothing (added )?to commit|no changes added`)
	fullSHA              = regexp.MustCompile(`^[0-9a-f]{40}$`)
	agoRe                = regexp.MustCompile(`^(\d{1,3})([mhd])$`)
	revisionRe           = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// HistoryError is a refusal worded for the person or the agent: the API returns it as is.
type HistoryError struct{ Msg string }

func (e *HistoryError) Error() string { return e.Msg }

func historyErr(msg string) error { return &HistoryError{Msg: msg} }

// gitFailure: git itself failed; its words are for the log, not for the person.
type gitFailure struct{ msg string }

func (e *gitFailure) Error() string { return e.msg }

// IsHistoryRefusal: a refusal the routes answer with 409.
func IsHistoryRefusal(err error) bool {
	var h *HistoryError
	var c *ClaimError
	return errors.As(err, &h) || errors.As(err, &c)
}

func (l *Library) excludeList() string {
	lines := []string{"# " + l.cfg.Texts.Tag + "-library-history v2", ".git"}
	for _, d := range historyIgnoredDirs {
		lines = append(lines, d+"/")
	}
	for _, p := range historyStagePrefixes {
		lines = append(lines, p+"*/")
	}
	return strings.Join(append(lines, "*.tmp"), "\n") + "\n"
}

type historyRepo struct{ root, gitDir string }

// HistoryGitDir is where this runtime keeps the history of the library at root (a real path): one repository per root.
func (l *Library) HistoryGitDir(root string) string {
	dir := l.cfg.HistoryDir
	if dir == "" {
		dir = l.cfg.DataDir
	}
	sum := sha1.Sum([]byte(root))
	return filepath.Join(dir, "design-history-"+hex.EncodeToString(sum[:])[:10]+".git")
}

func (l *Library) historyRepo() (historyRepo, error) {
	real, err := filepath.EvalSymlinks(l.Root())
	if err != nil {
		return historyRepo{}, historyErr(locale.Pick("The library folder does not exist: no history is kept", "Папки библиотеки нет: история не ведётся"))
	}
	return historyRepo{root: real, gitDir: l.HistoryGitDir(real)}, nil
}

func isInsideIgnored(segments []string) bool {
	for _, part := range segments {
		if part == ".git" {
			return true
		}
		for _, d := range historyIgnoredDirs {
			if part == d {
				return true
			}
		}
		for _, p := range historyStagePrefixes {
			if strings.HasPrefix(part, p) {
				return true
			}
		}
	}
	return false
}

// IsHistoryPath is whether a change at this path (relative to the root) is something the history keeps.
func IsHistoryPath(rel string) bool {
	segs := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' })
	return len(segs) > 0 && !isInsideIgnored(segs) && !strings.HasSuffix(segs[len(segs)-1], ".tmp")
}

type gitResult struct {
	code           int
	stdout, stderr string
}

func gitEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0", "LC_ALL=C.UTF-8")
}

func (l *Library) safeConfig() []string {
	return []string{
		"-c", "core.hooksPath=/dev/null",
		"-c", "core.fsmonitor=false",
		"-c", "core.untrackedCache=false",
		"-c", "core.excludesFile=/dev/null",
		"-c", "core.attributesFile=/dev/null",
		"-c", "core.quotepath=false",
		"-c", "core.autocrlf=false",
		"-c", "commit.gpgsign=false",
		"-c", "diff.ignoreSubmodules=all",
		"-c", "status.submoduleSummary=false",
		"-c", "submodule.recurse=false",
		"-c", "user.name=" + l.cfg.Texts.Tag,
		"-c", "user.email=" + l.cfg.Texts.HistoryEmail,
	}
}

// spawnGit runs git without a shell; a non-zero exit is a result, not an error.
func (l *Library) spawnGit(args []string, cwd string, timeout time.Duration) (gitResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append(l.safeConfig(), args...)...)
	cmd.Env, cmd.Dir = gitEnv(), cwd
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	res := gitResult{stdout: jscompat.DecodeUTF8(out.Bytes()), stderr: jscompat.DecodeUTF8(errb.Bytes())}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return res, &gitFailure{locale.Pick("Could not start git: ", "Не удалось запустить git: ") + err.Error()}
		}
		res.code = exit.ExitCode()
		if res.code < 0 {
			res.code = 1
		}
	}
	return res, nil
}

func (l *Library) git(repo historyRepo, args ...string) (gitResult, error) {
	return l.spawnGit(append([]string{"--git-dir=" + repo.gitDir, "--work-tree=" + repo.root}, args...), repo.root, historyGitTime)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func (l *Library) gitOK(repo historyRepo, args ...string) (string, error) {
	res, err := l.git(repo, args...)
	if err != nil {
		return "", err
	}
	if res.code != 0 {
		msg := firstLine(res.stderr)
		if msg == "" {
			msg = locale.Pick("code ", "код ") + strconv.Itoa(res.code)
		}
		return "", &gitFailure{"git " + args[0] + ": " + msg}
	}
	return res.stdout, nil
}

func writeIfDifferent(file, content string) error {
	if cur, err := os.ReadFile(file); err == nil && string(cur) == content {
		return nil
	}
	temp := file + "." + randomHex(6) + ".tmp"
	f, err := os.OpenFile(temp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
	if err != nil {
		return err
	}
	_, err = f.WriteString(content)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(temp)
		return err
	}
	return os.Rename(temp, file)
}

// ensureRepo is this runtime's repository of the library, made when missing: a private folder outside the library,
// its config the known text and its ignore list in info/exclude.
func (l *Library) ensureRepo() (historyRepo, error) {
	repo, err := l.historyRepo()
	if err != nil {
		return repo, err
	}
	st, err := os.Lstat(repo.gitDir)
	if err == nil && !st.IsDir() {
		return repo, historyErr(repo.gitDir + locale.Pick(" is not a folder: no history is kept until it is removed", " не папка: история не ведётся, пока его не уберут"))
	}
	if err != nil {
		parent := filepath.Dir(repo.gitDir)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return repo, err
		}
		res, err := l.spawnGit([]string{"init", "-q", "--bare", "--initial-branch=main", "--template=", repo.gitDir}, parent, historyGitTime)
		if err != nil {
			return repo, err
		}
		if res.code != 0 {
			return repo, &gitFailure{"git init: " + firstLine(res.stderr)}
		}
	}
	if err := os.Chmod(repo.gitDir, 0o700); err != nil {
		return repo, err
	}
	if err := writeIfDifferent(filepath.Join(repo.gitDir, "config"), historyRepoConfig); err != nil {
		return repo, err
	}
	if err := os.MkdirAll(filepath.Join(repo.gitDir, "info"), 0o700); err != nil {
		return repo, err
	}
	return repo, writeIfDifferent(filepath.Join(repo.gitDir, "info", "exclude"), l.excludeList())
}

// findEmbeddedRepos are the folders (relative, /-separated) that hold a .git of their own, links not entered.
func findEmbeddedRepos(root string) []string {
	var found []string
	left := embeddedBudget
	var walk func(rel string)
	walk = func(rel string) {
		dir := root
		if rel != "" {
			dir = filepath.Join(root, rel)
		}
		entries, _ := os.ReadDir(dir)
		if rel != "" {
			for _, e := range entries {
				if e.Name() == ".git" {
					found = append(found, rel)
					return
				}
			}
		}
		for _, e := range entries {
			left--
			if left+1 <= 0 {
				return
			}
			if e.IsDir() && !isInsideIgnored([]string{e.Name()}) {
				if rel == "" {
					walk(e.Name())
				} else {
					walk(rel + "/" + e.Name())
				}
			}
		}
	}
	walk("")
	return found
}

func wholeTree(embedded []string) []string {
	out := []string{"--", "."}
	for _, rel := range embedded {
		out = append(out, ":(exclude,literal)"+rel)
	}
	return out
}

func clearStaleIndexLock(repo historyRepo, now time.Time) {
	lock := filepath.Join(repo.gitDir, "index.lock")
	if st, err := os.Lstat(lock); err == nil && st.Mode().IsRegular() && now.Sub(st.ModTime()) > historyStaleLock {
		_ = os.Remove(lock)
	}
}

type stagedPath struct{ status, path string }

func parseNameStatus(out string) []stagedPath {
	var parts []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	var staged []stagedPath
	for at := 0; at+1 < len(parts); at += 2 {
		staged = append(staged, stagedPath{parts[at], parts[at+1]})
	}
	return staged
}

// BoardOfPath: <project>/project/<Board>.dc.html is that board of that project.
func BoardOfPath(rel string) (project, board string, ok bool) {
	parts := strings.Split(rel, "/")
	if len(parts) != 3 || parts[1] != ProjectDirname || !IsLibraryID(parts[0]) || !IsBoardFile(parts[2]) {
		return "", "", false
	}
	return parts[0], BoardTitleOf(parts[2]), true
}

// SnapshotMessage is the words of a snapshot: which boards changed, who held them and the runtime that saved it.
func SnapshotMessage(paths, editors []string, runtime, subject string) (string, string) {
	var boards, projects []string
	seenB, seenP := map[string]bool{}, map[string]bool{}
	for _, p := range paths {
		if _, b, ok := BoardOfPath(p); ok && !seenB[b] {
			seenB[b] = true
			boards = append(boards, b)
		}
	}
	for _, p := range paths {
		first, _, _ := strings.Cut(p, "/")
		if IsLibraryID(first) && !seenP[first] {
			seenP[first] = true
			projects = append(projects, first)
		}
	}
	line := subject
	if line == "" {
		switch {
		case len(boards) > 0:
			shown := boards
			if len(shown) > maxSubjectBoards {
				shown = shown[:maxSubjectBoards]
			}
			quoted := make([]string, len(shown))
			for i, b := range shown {
				quoted[i] = locale.Pick("\""+b+"\"", "«"+b+"»")
			}
			line = locale.Pick("Edits: ", "Правки: ") + strings.Join(quoted, ", ")
			if len(boards) > maxSubjectBoards {
				line += locale.Pick(" and ", " и ещё ") + strconv.Itoa(len(boards)-maxSubjectBoards) + locale.Pick(" more", "")
			}
		case len(projects) == 1:
			line = locale.Pick("Edits to the shared files of project ", "Правки общих файлов проекта ") + projects[0]
		case len(projects) > 1:
			line = locale.Pick("Edits in projects: ", "Правки в проектах: ") + strconv.Itoa(len(projects))
		default:
			line = locale.Pick("Edits in the library", "Правки в библиотеке")
		}
	}
	trailers := []string{"Runtime: " + runtime}
	for i, e := range editors {
		if i >= maxEditors {
			break
		}
		trailers = append(trailers, "Editor: "+e)
	}
	s, ok := CleanText(line, 200)
	if !ok {
		s = locale.Pick("Edits in the library", "Правки в библиотеке")
	}
	return s, strings.Join(trailers, "\n")
}

// SnapshotKind is what a snapshot came to.
type SnapshotKind string

const (
	SnapshotCommitted SnapshotKind = "committed"
	SnapshotClean     SnapshotKind = "clean"
	SnapshotBusy      SnapshotKind = "busy"
)

type snapshotOutcome struct {
	kind SnapshotKind
	sha  string
}

// snapshotNow commits whatever changed since the last snapshot; busy when another process holds git's lock.
func (l *Library) snapshotNow(runtime string, editors []string, subject string, now time.Time) (snapshotOutcome, error) {
	repo, err := l.ensureRepo()
	if err != nil {
		return snapshotOutcome{}, err
	}
	clearStaleIndexLock(repo, now)
	added, err := l.git(repo, append([]string{"add", "-A", "--ignore-errors"}, wholeTree(findEmbeddedRepos(repo.root))...)...)
	if err != nil {
		return snapshotOutcome{}, err
	}
	if added.code != 0 {
		if lockBusy.MatchString(added.stderr) {
			return snapshotOutcome{kind: SnapshotBusy}, nil
		}
		if !addSkippable.MatchString(added.stderr) {
			return snapshotOutcome{}, &gitFailure{"git add: " + firstLine(added.stderr)}
		}
	}
	out, err := l.gitOK(repo, "diff", "--cached", "--name-status", "-z", "--no-renames", "--ignore-submodules=all")
	if err != nil {
		return snapshotOutcome{}, err
	}
	staged := parseNameStatus(out)
	if len(staged) == 0 {
		return snapshotOutcome{kind: SnapshotClean}, nil
	}
	head, err := l.git(repo, "rev-parse", "--verify", "--quiet", "HEAD")
	if err != nil {
		return snapshotOutcome{}, err
	}
	if subject == "" && head.code != 0 {
		subject = locale.Pick("Initial snapshot of the library", "Начальный снимок библиотеки")
	}
	paths := make([]string, len(staged))
	for i, s := range staged {
		paths[i] = s.path
	}
	subj, body := SnapshotMessage(paths, editors, runtime, subject)
	committed, err := l.git(repo, "commit", "-q", "--no-verify", "--no-gpg-sign", "-m", subj, "-m", body)
	if err != nil {
		return snapshotOutcome{}, err
	}
	if committed.code != 0 {
		if lockBusy.MatchString(committed.stderr) {
			return snapshotOutcome{kind: SnapshotBusy}, nil
		}
		if nothingToCommit.MatchString(committed.stdout + committed.stderr) {
			return snapshotOutcome{kind: SnapshotClean}, nil
		}
		msg := committed.stderr
		if msg == "" {
			msg = committed.stdout
		}
		return snapshotOutcome{}, &gitFailure{"git commit: " + firstLine(msg)}
	}
	sha, err := l.gitOK(repo, "rev-parse", "HEAD")
	if err != nil {
		return snapshotOutcome{}, err
	}
	return snapshotOutcome{kind: SnapshotCommitted, sha: strings.TrimSpace(sha)}, nil
}

// HistoryEntry is one snapshot touching a scope.
type HistoryEntry struct {
	ID, Subject string
	At          int64
	Editors     []string
	// Runtime is nil when the snapshot names none.
	Runtime *string
}

const (
	logUnit   = "\x1f"
	logRecord = "\x1e"
	logGroup  = "\x1d"
)

// jsNumber is Number(s) of a string: whitespace trimmed, empty is 0, NaN when it is not a number.
func jsNumber(s string) (float64, bool) {
	t := jscompat.Trim(s)
	if t == "" {
		return 0, true
	}
	if strings.HasPrefix(t, "0x") || strings.HasPrefix(t, "0X") {
		n, err := strconv.ParseUint(t[2:], 16, 64)
		return float64(n), err == nil
	}
	if strings.ContainsAny(t, "_pPxX") {
		return 0, false
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// ParseLog reads the records of the log format readHistory asks for.
func ParseLog(out string) []HistoryEntry {
	var entries []HistoryEntry
	for _, rec := range strings.Split(out, logRecord) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		f := strings.Split(rec, logUnit)
		for len(f) < 5 {
			f = append(f, "")
		}
		hasAt := len(strings.Split(rec, logUnit)) > 1
		at, ok := jsNumber(f[1])
		if !fullSHA.MatchString(f[0]) || !hasAt || !ok {
			continue
		}
		subject, _ := CleanText(f[2], 200)
		editors := []string{}
		for _, line := range strings.Split(f[3], logGroup) {
			if e, ok := CleanText(line, 200); ok {
				editors = append(editors, e)
			}
		}
		var runtime *string
		first, _, _ := strings.Cut(f[4], logGroup)
		if r, ok := CleanText(first, 64); ok {
			runtime = &r
		}
		entries = append(entries, HistoryEntry{ID: f[0], At: int64(at * 1000), Subject: subject, Editors: editors, Runtime: runtime})
	}
	return entries
}

// ScopePath is the path a scope names in the repository: a project, or one board file of it.
func ScopePath(project string, file *string) (string, error) {
	if !IsLibraryID(project) {
		return "", historyErr(locale.Pick("No such project", "Нет такого проекта"))
	}
	if file == nil {
		return project, nil
	}
	f := *file
	if !IsBoardFile(f) || strings.ContainsAny(f, "\\\x00\n\r") || strings.HasPrefix(f, ".") {
		return "", historyErr(locale.Pick("History exists for board files (*.dc.html)", "История есть у досок-файлов (*.dc.html)"))
	}
	return project + "/" + ProjectDirname + "/" + f, nil
}

type scopeHistory struct {
	entries []HistoryEntry
	unsaved bool
}

func (l *Library) readHistory(project string, file *string, limit float64) (scopeHistory, error) {
	target, err := ScopePath(project, file)
	if err != nil {
		return scopeHistory{}, err
	}
	repo, err := l.ensureRepo()
	if err != nil {
		return scopeHistory{}, err
	}
	n := int(limit)
	if limit < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	format := "--format=" + logRecord + "%H" + logUnit + "%ct" + logUnit + "%s" + logUnit +
		"%(trailers:key=Editor,valueonly,separator=%x1d)" + logUnit + "%(trailers:key=Runtime,valueonly,separator=%x1d)"
	log, err := l.git(repo, "log", "-n"+strconv.Itoa(n), format, "--", target)
	if err != nil {
		return scopeHistory{}, err
	}
	var entries []HistoryEntry
	if log.code == 0 {
		entries = ParseLog(log.stdout)
	}
	status, err := l.gitOK(repo, "status", "--porcelain=v1", "-z", "--ignore-submodules=all", "--", target)
	if err != nil {
		return scopeHistory{}, err
	}
	return scopeHistory{entries: entries, unsaved: status != ""}, nil
}

// WhoOf is «who» for a person: the snapshot's editors, else that nobody held the board.
func WhoOf(e HistoryEntry) string {
	if len(e.Editors) > 0 {
		return strings.Join(e.Editors, ", ")
	}
	return locale.Pick("autosnapshot", "автоснимок")
}

// BoardSnapshot is a saved state of a board as the panel lists it.
type BoardSnapshot struct {
	ID      string `json:"id"`
	At      int64  `json:"at"`
	Who     string `json:"who"`
	Current bool   `json:"current"`
}

func toSnapshots(h scopeHistory) []BoardSnapshot {
	out := []BoardSnapshot{}
	for i, e := range h.entries {
		out = append(out, BoardSnapshot{ID: e.ID, At: e.At, Who: WhoOf(e), Current: i == 0 && !h.unsaved})
	}
	return out
}

// RestoreTarget is a revision, or how long ago.
type RestoreTarget struct {
	Revision string
	Ago      time.Duration
}

// ParseRestoreTarget reads --at: a revision (7–40 hex digits) or 30m, 2h, 1d.
func ParseRestoreTarget(v string) (RestoreTarget, bool) {
	t := strings.ToLower(jscompat.Trim(v))
	if m := agoRe.FindStringSubmatch(t); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		return RestoreTarget{Ago: time.Duration(n) * unit}, true
	}
	if revisionRe.MatchString(t) {
		return RestoreTarget{Revision: t}, true
	}
	return RestoreTarget{}, false
}

// restoreActor is who asks for a restore: for the message and for the claim check (a person at the panel has no chat).
type restoreActor struct {
	runtime, line string
	owner         *ClaimOwner
}

type restoreResult struct {
	revision string
	snapshot snapshotOutcome
}

func isPlainFolder(parent, name string) bool {
	st, err := os.Lstat(filepath.Join(parent, name))
	return err == nil && st.IsDir()
}

// restoreBoard puts a board back as it was: what is there now is saved first, the version is checked out over the
// working file and the result saved as a snapshot of its own. A board a different live chat holds is not touched.
func (l *Library) restoreBoard(project, file, at string, actor restoreActor, now time.Time) (restoreResult, error) {
	l.hist.chain.Lock()
	defer l.hist.chain.Unlock()
	target, err := ScopePath(project, &file)
	if err != nil {
		return restoreResult{}, err
	}
	wanted, ok := ParseRestoreTarget(at)
	if !ok {
		return restoreResult{}, historyErr(locale.Pick("Give the version like this: --at <sha | 30m | 2h | 1d>", "Версию задают так: --at <sha | 30m | 2h | 1d>"))
	}
	board := BoardTitleOf(file)
	for _, c := range l.LiveClaims(project, now.UnixMilli()) {
		if !l.claimNamesFile(project, c.Board, file) {
			continue
		}
		if actor.owner == nil || !sameOwner(c.Owner, *actor.owner) {
			return restoreResult{}, historyErr(locale.Pick("The board \""+board+"\" is being edited by "+c.Owner.User+" (chat \""+c.Owner.ChatTitle+"\"): a version can be restored when they finish", "Доску «"+board+"» сейчас правит "+c.Owner.User+" (чат «"+c.Owner.ChatTitle+"»): вернуть версию можно, когда он закончит"))
		}
		break
	}
	repo, err := l.ensureRepo()
	if err != nil {
		return restoreResult{}, err
	}
	if !isPlainFolder(repo.root, project) || !isPlainFolder(filepath.Join(repo.root, project), ProjectDirname) {
		return restoreResult{}, historyErr(locale.Pick("The project folder is not a plain folder: a version cannot be restored", "Папка проекта не обычная папка: вернуть версию нельзя"))
	}
	var revision string
	if wanted.Revision != "" {
		res, err := l.git(repo, "rev-parse", "--verify", "--quiet", "--end-of-options", wanted.Revision+"^{commit}")
		if err != nil {
			return restoreResult{}, err
		}
		if res.code != 0 {
			return restoreResult{}, historyErr(locale.Pick("No such version in the history", "Такой версии нет в истории"))
		}
		revision = strings.TrimSpace(res.stdout)
	} else {
		before := now.Add(-wanted.Ago).UnixMilli() / 1000
		res, err := l.git(repo, "rev-list", "-n1", "--before=@"+strconv.FormatInt(before, 10), "HEAD", "--", target)
		if err != nil {
			return restoreResult{}, err
		}
		if res.code == 0 {
			revision = strings.TrimSpace(res.stdout)
		}
		if revision == "" {
			return restoreResult{}, historyErr(locale.Pick("The board had no saved versions before that time", "Раньше этого времени у доски не было сохранённых версий"))
		}
	}
	existed, err := l.git(repo, "cat-file", "-e", revision+":"+target)
	if err != nil {
		return restoreResult{}, err
	}
	if existed.code != 0 {
		return restoreResult{}, historyErr(locale.Pick("This board did not exist in that version yet", "В этой версии такой доски ещё не было"))
	}
	editors := []string{actor.line}
	pre, err := l.snapshotNow(actor.runtime, editors, locale.Pick("Before restoring \""+board+"\"", "Перед возвратом «"+board+"»"), now)
	if err != nil {
		return restoreResult{}, err
	}
	if pre.kind == SnapshotBusy {
		return restoreResult{}, historyErr(locale.Pick("The history is busy with another save: try again in a few seconds", "История сейчас занята другим сохранением: повторите через несколько секунд"))
	}
	if _, err := l.gitOK(repo, "restore", "--source="+revision, "--worktree", "--", target); err != nil {
		return restoreResult{}, err
	}
	snap, err := l.snapshotNow(actor.runtime, editors, locale.Pick("Restored \""+board+"\" to version ", "Вернул «"+board+"» к версии ")+revision[:min(7, len(revision))], now)
	if err != nil {
		return restoreResult{}, err
	}
	return restoreResult{revision: revision, snapshot: snap}, nil
}

// OwnerLine is a claim's holder as a snapshot's Editor trailer names it.
func OwnerLine(o ClaimOwner) string {
	return locale.Pick(o.User+" · chat \""+o.ChatTitle+"\" ("+o.Runtime+")", o.User+" · чат «"+o.ChatTitle+"» ("+o.Runtime+")")
}

// historyState is the history of this runtime while its design slice runs: a snapshot follows every burst of changes
// by the quiet time, the tree is also looked at every poll (a watcher can miss a change), and a busy lock is retried.
type historyState struct {
	chain sync.Mutex

	mu         sync.Mutex
	started    bool
	stopSub    func()
	timer      *time.Timer
	pollTicker *time.Ticker
	pollStop   chan struct{}
	retries    int
	running    bool
	again      bool
	runtime    string
	quiet      time.Duration
	editors    map[string]bool
	editorList []string
	lastLookup map[string]time.Time
	failure    string
	onFailure  func(string)
}

func (l *Library) noteEditors(rel string) {
	project, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	if !IsLibraryID(project) {
		return
	}
	h := &l.hist
	now := time.Now()
	h.mu.Lock()
	if now.Sub(h.lastLookup[project]) < time.Second {
		h.mu.Unlock()
		return
	}
	h.lastLookup[project] = now
	h.mu.Unlock()
	_, changed, isBoard := BoardOfPath(filepath.ToSlash(rel))
	claims := l.LiveClaims(project, now.UnixMilli())
	var held []Claim
	for _, c := range claims {
		if !isBoard || l.claimNamesFile(project, c.Board, changed+dcSuffix) {
			held = append(held, c)
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, c := range held {
		line := OwnerLine(c.Owner)
		if !h.editors[line] {
			h.editors[line] = true
			h.editorList = append(h.editorList, line)
		}
	}
}

// arm (the lock held) sets the next snapshot delay from now.
func (l *Library) arm(delay time.Duration) {
	h := &l.hist
	if !h.started {
		return
	}
	if h.timer != nil {
		h.timer.Stop()
	}
	h.timer = time.AfterFunc(delay, l.runSnapshot)
}

func (l *Library) runSnapshot() {
	h := &l.hist
	h.mu.Lock()
	h.timer = nil
	if !h.started {
		h.mu.Unlock()
		return
	}
	if h.running {
		h.again = true
		h.mu.Unlock()
		return
	}
	h.running = true
	editors := append([]string{}, h.editorList...)
	runtime := h.runtime
	h.mu.Unlock()

	h.chain.Lock()
	out, err := l.snapshotNow(runtime, editors, "", time.Now())
	h.chain.Unlock()

	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case err != nil:
		h.failure = err.Error()
		if h.onFailure != nil {
			go h.onFailure(h.failure)
		}
	case out.kind == SnapshotBusy:
		h.retries++
		if h.retries <= historyRetries {
			l.arm(historyRetry)
		}
	default:
		h.retries, h.failure = 0, ""
		used := map[string]bool{}
		for _, e := range editors {
			used[e] = true
			delete(h.editors, e)
		}
		kept := h.editorList[:0:0]
		for _, e := range h.editorList {
			if !used[e] {
				kept = append(kept, e)
			}
		}
		h.editorList = kept
	}
	h.running = false
	if h.again {
		h.again = false
		l.arm(h.quiet)
	}
}

// poll arms a snapshot for a change nobody told the watcher about.
func (l *Library) pollHistory() {
	h := &l.hist
	h.mu.Lock()
	busy := h.running || h.timer != nil || !h.started
	h.mu.Unlock()
	if busy {
		return
	}
	fail := func(err error) {
		h.mu.Lock()
		h.failure = err.Error()
		cb := h.onFailure
		h.mu.Unlock()
		if cb != nil {
			cb(err.Error())
		}
	}
	repo, err := l.ensureRepo()
	if err != nil {
		fail(err)
		return
	}
	status, err := l.git(repo, append([]string{"status", "--porcelain=v1", "-z", "--ignore-submodules=all"}, wholeTree(findEmbeddedRepos(repo.root))...)...)
	if err != nil {
		fail(err)
		return
	}
	if status.code != 0 || status.stdout == "" {
		return
	}
	var changed []string
	for _, entry := range strings.Split(status.stdout, "\x00") {
		if len(entry) > 3 && IsHistoryPath(entry[3:]) {
			changed = append(changed, entry[3:])
		}
	}
	if len(changed) == 0 {
		return
	}
	for _, rel := range changed {
		go l.noteEditors(rel)
	}
	h.mu.Lock()
	l.arm(h.quiet)
	h.mu.Unlock()
}

// startHistory starts the history once while its slice runs: what changed while nobody watched is saved, and from
// then on a snapshot follows every burst of changes. Nothing starts while the library folder is not there.
func (l *Library) startHistory(runtime string, onFailure func(string)) {
	l.startHistoryWith(runtime, onFailure, historyQuiet, historyPoll)
}

func (l *Library) startHistoryWith(runtime string, onFailure func(string), quiet, poll time.Duration) {
	h := &l.hist
	h.mu.Lock()
	if h.started {
		h.mu.Unlock()
		return
	}
	if _, err := filepath.EvalSymlinks(l.Root()); err != nil {
		h.mu.Unlock()
		return
	}
	h.started, h.runtime, h.quiet, h.onFailure = true, runtime, quiet, onFailure
	h.retries, h.running, h.again, h.failure = 0, false, false, ""
	h.editors, h.editorList, h.lastLookup = map[string]bool{}, nil, map[string]time.Time{}
	h.pollStop = make(chan struct{})
	stopCh := h.pollStop
	h.mu.Unlock()

	stop, _ := l.Subscribe(func(rel string) {
		if !IsHistoryPath(rel) {
			return
		}
		go l.noteEditors(rel)
		h.mu.Lock()
		h.retries = 0
		l.arm(h.quiet)
		h.mu.Unlock()
	})
	h.mu.Lock()
	h.stopSub = stop
	h.pollTicker = time.NewTicker(poll)
	ticker := h.pollTicker
	l.arm(0)
	h.mu.Unlock()
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				// A watcher that broke is started again; the look at the tree covers what happened meanwhile.
				l.watching()
				l.pollHistory()
			}
		}
	}()
}

func (l *Library) stopHistory() {
	h := &l.hist
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.started {
		return
	}
	h.started = false
	if h.stopSub != nil {
		h.stopSub()
		h.stopSub = nil
	}
	if h.timer != nil {
		h.timer.Stop()
		h.timer = nil
	}
	if h.pollTicker != nil {
		h.pollTicker.Stop()
	}
	close(h.pollStop)
}

// HistoryFailure is why the history is not being kept, "" while it works.
func (l *Library) HistoryFailure() string {
	l.hist.mu.Lock()
	defer l.hist.mu.Unlock()
	return l.hist.failure
}

// The history for its two callers: an agent through the CLI (a project or a board by the name it knows) and the person
// at the panel (a board by its file).

func (l *Library) historyProject(project any) (*Artifact, error) {
	if s, ok := project.(string); ok && IsLibraryID(s) {
		if a := l.ReadArtifact(s); a != nil {
			return a, nil
		}
	}
	return nil, historyErr(locale.Pick("No such project: see ", "Нет такого проекта: смотрите ") + l.cfg.Texts.CLI + " design list")
}

// CLIHistoryLine is a version as `design history` prints it.
type CLIHistoryLine struct {
	ID      string  `json:"id"`
	ShortID string  `json:"shortId"`
	At      int64   `json:"at"`
	Who     string  `json:"who"`
	Subject string  `json:"subject"`
	Runtime *string `json:"runtime"`
}

// CLIHistory is GET /api/cli/design/history.
type CLIHistory struct {
	Unsaved bool             `json:"unsaved"`
	Lines   []CLIHistoryLine `json:"lines"`
}

// HistoryForCLI lists the versions of a project, or of one board when board is not nil.
func (l *Library) HistoryForCLI(project any, board any, limit float64) (CLIHistory, error) {
	a, err := l.historyProject(project)
	if err != nil {
		return CLIHistory{}, err
	}
	var file *string
	if board != nil {
		key, err := ResolveBoardKey(a, board)
		if err != nil {
			return CLIHistory{}, err
		}
		key, _ = claimShown(a, key)
		f := key + dcSuffix
		file = &f
	}
	h, err := l.readHistory(a.ID, file, limit)
	if err != nil {
		return CLIHistory{}, err
	}
	out := CLIHistory{Unsaved: h.unsaved, Lines: []CLIHistoryLine{}}
	for _, e := range h.entries {
		out.Lines = append(out.Lines, CLIHistoryLine{ID: e.ID, ShortID: e.ID[:7], At: e.At, Who: WhoOf(e), Subject: e.Subject, Runtime: e.Runtime})
	}
	return out, nil
}

// CLIRestored is what an agent's restore answers.
type CLIRestored struct {
	Board, Revision string
	Saved           bool
}

// RestoreForCLI is the agent's restore: the chat it speaks from (session, when given) is the actor, so a board held by
// that same chat can be restored by it.
func (l *Library) RestoreForCLI(session string, project, board, at any) (CLIRestored, error) {
	a, err := l.historyProject(project)
	if err != nil {
		return CLIRestored{}, err
	}
	key, err := ResolveBoardKey(a, board)
	if err != nil {
		return CLIRestored{}, err
	}
	key, _ = claimShown(a, key)
	atS, ok := at.(string)
	if !ok {
		return CLIRestored{}, historyErr(locale.Pick("Give the version like this: --at <sha | 30m | 2h | 1d>", "Версию задают так: --at <sha | 30m | 2h | 1d>"))
	}
	id, err := l.Identity()
	if err != nil {
		return CLIRestored{}, err
	}
	actor := restoreActor{runtime: id.Runtime, line: id.Person}
	if session != "" {
		caller, err := l.CallerOf(session)
		if err != nil {
			return CLIRestored{}, err
		}
		owner := ClaimOwner{User: id.Person, Runtime: id.Runtime, TabID: caller.TabID, ChatTitle: caller.ChatTitle}
		actor.owner, actor.line = &owner, OwnerLine(owner)
	}
	res, err := l.restoreBoard(a.ID, key+dcSuffix, atS, actor, time.Now())
	if err != nil {
		return CLIRestored{}, err
	}
	return CLIRestored{Board: key, Revision: res.revision, Saved: res.snapshot.kind == SnapshotCommitted}, nil
}

func panelFile(board any) (string, error) {
	if s, ok := board.(string); ok && IsBoardFile(s) {
		return s, nil
	}
	return "", historyErr(locale.Pick("History exists for board files; a screen built from a shared bundle has no versions", "История есть у досок-файлов; экран, собранный из общего бандла, версий не имеет"))
}

// HistoryForPanel lists the latest versions of a board file.
func (l *Library) HistoryForPanel(artifact, board any, limit float64) ([]BoardSnapshot, error) {
	a, err := l.historyProject(artifact)
	if err != nil {
		return nil, err
	}
	file, err := panelFile(board)
	if err != nil {
		return nil, err
	}
	h, err := l.readHistory(a.ID, &file, limit)
	if err != nil {
		return nil, err
	}
	return toSnapshots(h), nil
}

// RestoreForPanel is the person's restore: no chat speaks for them, so a board some chat holds is left alone.
func (l *Library) RestoreForPanel(artifact, board, revision any) error {
	a, err := l.historyProject(artifact)
	if err != nil {
		return err
	}
	rev, ok := revision.(string)
	if !ok || !fullSHA.MatchString(rev) {
		return historyErr(locale.Pick("Could not tell which version to restore", "Не удалось понять, какую версию вернуть"))
	}
	id, err := l.Identity()
	if err != nil {
		return err
	}
	file, err := panelFile(board)
	if err != nil {
		return err
	}
	_, err = l.restoreBoard(a.ID, file, rev, restoreActor{runtime: id.Runtime, line: id.Person}, time.Now())
	return err
}
