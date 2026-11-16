package folder

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/ly-solutions/harnessly/internal/connectors"
)

// How a folder is watched. The truth is a scan: every watched file's size, modification time and (up to hashMax) hash,
// kept in the connection's folder as snapshot.json. fsnotify only says when to scan again: a change starts a quiet
// period of debounce (pushed on by more changes, never past maxWait), then one scan and one batch. Without fsnotify,
// or when it overflows, the folder is scanned every poll. After a restart the first scan is compared with the kept
// snapshot: what changed while the runtime was down comes once, nothing else. The first run of a new connection only
// learns. A batch of more than bulkMax changes (a checkout, a sync) is one event of change "bulk".

const (
	debounce = 2 * time.Second
	maxWait  = 15 * time.Second
	poll     = 60 * time.Second
	bulkMax  = 100
	// journalMax lines of changes are kept for the agent's reads.
	journalMax = 5000
)

const (
	changeCreated  = "created"
	changeModified = "modified"
	changeDeleted  = "deleted"
	changeBulk     = "bulk"
)

type fileState struct {
	Size  int64  `json:"s"`
	MTime int64  `json:"m"`
	Hash  string `json:"h,omitempty"`
	// Tasks are the hashes of the note's open tasks with a date (Obsidian).
	Tasks []string `json:"t,omitempty"`
}

type snapshot map[string]fileState

func sortedPaths(s snapshot) []string { return slices.Sorted(maps.Keys(s)) }

func matches(ms []string, name string) bool {
	if len(ms) == 0 {
		return true
	}
	for _, m := range ms {
		if ok, _ := filepath.Match(m, name); ok {
			return true
		}
	}
	return false
}

func skipDir(in connectors.Instance, name string) bool {
	return ignoredDirs[name] || (obsidian(in) && obsidianIgnored[name])
}

// scan lists the watched files of the folder: regular files only, symlinks not followed, ignored folders skipped.
func scan(r string, in connectors.Instance) (snapshot, error) {
	ms := masks(in)
	out := snapshot{}
	err := filepath.WalkDir(r, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == r {
				return err
			}
			return nil
		}
		if d.IsDir() {
			if p != r && skipDir(in, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !matches(ms, d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if len(out) >= maxFiles {
			return errTooMany
		}
		rel, _ := filepath.Rel(r, p)
		out[filepath.ToSlash(rel)] = fileState{Size: fi.Size(), MTime: fi.ModTime().UnixNano()}
		return nil
	})
	if err == errTooMany {
		return nil, connectors.Sayf("The folder has more than %d files: choose a smaller one or a mask", maxFiles)
	}
	if err != nil {
		return nil, connectors.Sayf("The folder cannot be read")
	}
	return out, nil
}

var errTooMany = &connectors.Said{Message: "too many files"}

func hashFile(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, hashMax)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// taskRe is an open task with a date: "- [ ] call Ann 📅 2026-11-20", "* [ ] pay by 2026-11-20".
var (
	taskRe = regexp.MustCompile(`^\s*[-*+] \[ \] `)
	dateRe = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
)

type task struct {
	Hash string
	Line int
	Due  string
}

// tasks are a note's open tasks with a date.
func tasks(p string) []task {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []task
	sc := bufio.NewScanner(io.LimitReader(f, maxRead))
	sc.Buffer(make([]byte, 64<<10), maxRead)
	n := 0
	for sc.Scan() {
		n++
		line := sc.Text()
		if !taskRe.MatchString(line) {
			continue
		}
		m := dateRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		sum := sha256.Sum256([]byte(strings.TrimSpace(line)))
		out = append(out, task{Hash: hex.EncodeToString(sum[:])[:16], Line: n, Due: m[1]})
	}
	return out
}

func hashes(ts []task) []string {
	out := make([]string, 0, len(ts))
	for _, t := range ts {
		out = append(out, t.Hash)
	}
	return out
}

// change is one line of the journal of changes.
type change struct {
	At     time.Time `json:"at"`
	Change string    `json:"change"`
	Path   string    `json:"path"`
	Size   int64     `json:"size"`
}

// diff compares a scan with the last snapshot: the changes, the events and the snapshot to keep. Files whose size and
// time did not move keep their state; a moved one is hashed, and the same hash is no change.
func diff(r string, in connectors.Instance, old, cur snapshot, now time.Time) ([]change, []connectors.Event, snapshot) {
	next := snapshot{}
	var changes []change
	var events []connectors.Event
	notes := obsidian(in)
	for _, rel := range sortedPaths(cur) {
		st := cur[rel]
		was, had := old[rel]
		if had && was.Size == st.Size && was.MTime == st.MTime {
			next[rel] = was
			continue
		}
		p := filepath.Join(r, filepath.FromSlash(rel))
		if st.Size <= hashMax {
			st.Hash = hashFile(p)
		}
		if had && st.Hash != "" && st.Hash == was.Hash {
			st.Tasks = was.Tasks
			next[rel] = st
			continue
		}
		kind := changeModified
		if !had {
			kind = changeCreated
		}
		var ts []task
		if notes && strings.HasSuffix(rel, ".md") {
			ts = tasks(p)
			st.Tasks = hashes(ts)
		}
		next[rel] = st
		changes = append(changes, change{At: now, Change: kind, Path: rel, Size: st.Size})
		events = append(events, connectors.Event{Type: EventChanged, ID: rel + "@" + strconv.FormatInt(st.MTime, 10) + ":" + st.Hash,
			At: time.Unix(0, st.MTime), Title: rel, Ref: "file path=" + rel,
			Facts: map[string]string{"path": rel, "change": kind, "size": strconv.FormatInt(st.Size, 10)}})
		if notes && strings.HasSuffix(rel, ".md") {
			if !had {
				events = append(events, connectors.Event{Type: EventNoteCreated, ID: "note:" + rel, At: time.Unix(0, st.MTime),
					Title: strings.TrimSuffix(filepath.Base(rel), ".md"), Ref: "file path=" + rel, Facts: map[string]string{"path": rel}})
			}
			known := map[string]bool{}
			for _, h := range was.Tasks {
				known[h] = true
			}
			for _, t := range ts {
				if known[t.Hash] {
					continue
				}
				events = append(events, connectors.Event{Type: EventNoteTask, ID: "task:" + rel + ":" + t.Hash, At: now,
					Ref: "file path=" + rel, Facts: map[string]string{"path": rel, "due": t.Due, "line": strconv.Itoa(t.Line)}})
			}
		}
	}
	for _, rel := range sortedPaths(old) {
		if _, ok := cur[rel]; ok {
			continue
		}
		changes = append(changes, change{At: now, Change: changeDeleted, Path: rel})
		events = append(events, connectors.Event{Type: EventChanged, ID: rel + "@deleted@" + strconv.FormatInt(now.UnixNano(), 10),
			At: now, Title: rel, Facts: map[string]string{"path": rel, "change": changeDeleted}})
	}
	if len(changes) > bulkMax {
		events = []connectors.Event{{Type: EventChanged, ID: "bulk@" + strconv.FormatInt(now.UnixNano(), 10), At: now,
			Ref:   "changes since=" + now.Add(-time.Minute).UTC().Format(time.RFC3339),
			Facts: map[string]string{"change": changeBulk, "count": strconv.Itoa(len(changes))}}}
	}
	return changes, events, next
}

// cursor is the time of the last kept scan and how many files it saw; the snapshot itself is in the folder.
type cursor struct {
	At    time.Time `json:"at"`
	Files int       `json:"files"`
}

func (st state) snapFile() string { return filepath.Join(st.dir, "snapshot.json") }

type state struct{ dir string }

func (st state) load() (snapshot, bool) {
	raw, err := os.ReadFile(st.snapFile())
	if err != nil {
		return nil, false
	}
	var s snapshot
	if json.Unmarshal(raw, &s) != nil {
		return nil, false
	}
	return s, true
}

func (st state) save(s snapshot) error {
	if err := os.MkdirAll(st.dir, 0o700); err != nil {
		return err
	}
	raw, _ := json.Marshal(s)
	tmp := st.snapFile() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, st.snapFile())
}

// Watch scans, waits for fsnotify or the poll, and emits each batch of changes.
func (folderConn) Watch(ctx context.Context, in connectors.Instance, from connectors.Cursor, emit connectors.Emit) error {
	r, err := root(in)
	if err != nil {
		return err
	}
	st := state{dir: in.Dir}
	old, had := st.load()
	if !had || from == "" {
		// The first run only learns the folder.
		cur, err := scan(r, in)
		if err != nil {
			return err
		}
		_, _, old = diff(r, in, snapshot{}, cur, time.Now())
		if err := st.save(old); err != nil {
			return err
		}
		if err := emit(nil, encode(cursor{At: time.Now().UTC(), Files: len(old)})); err != nil {
			return err
		}
	}
	w, werr := fsnotify.NewWatcher()
	if werr == nil {
		defer w.Close()
		addDirs(w, r, in)
	}
	step := func() error {
		cur, err := scan(r, in)
		if err != nil {
			return err
		}
		now := time.Now()
		changes, events, next := diff(r, in, old, cur, now)
		if len(changes) == 0 && len(events) == 0 {
			return nil
		}
		if err := st.save(next); err != nil {
			return err
		}
		journal{dir: in.Dir}.add(changes)
		if err := emit(events, encode(cursor{At: now.UTC(), Files: len(next)})); err != nil {
			return err
		}
		old = next
		return nil
	}
	if err := step(); err != nil {
		return err
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if w != nil && werr == nil {
		events, errs = w.Events, w.Errors
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	var quiet <-chan time.Time
	var first time.Time
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ev.Has(fsnotify.Create) && w != nil {
				if fi, err := os.Lstat(ev.Name); err == nil && fi.IsDir() && !skipDir(in, filepath.Base(ev.Name)) {
					addDirs(w, ev.Name, in)
				}
			}
			now := time.Now()
			if quiet == nil {
				first = now
			}
			wait := min(debounce, maxWait-now.Sub(first))
			quiet = time.After(max(wait, 0))
		case _, ok := <-errs:
			if !ok {
				errs = nil
			}
			// An overflow: the next scan finds what was missed.
			quiet = time.After(debounce)
		case <-quiet:
			quiet = nil
			if err := step(); err != nil {
				return err
			}
		case <-tick.C:
			if quiet == nil {
				if err := step(); err != nil {
					return err
				}
			}
		}
	}
}

// addDirs watches a folder and its sub-folders, without the ignored ones and without following symlinks.
func addDirs(w *fsnotify.Watcher, dir string, in connectors.Instance) {
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if p != dir && skipDir(in, d.Name()) {
			return filepath.SkipDir
		}
		if n++; n > 4000 {
			return filepath.SkipAll
		}
		_ = w.Add(p)
		return nil
	})
}

func encode(c cursor) connectors.Cursor {
	raw, _ := json.Marshal(c)
	return connectors.Cursor(raw)
}

// journal is changes.jsonl in the connection's folder: the changes for the agent's reads, the newest journalMax.
type journal struct{ dir string }

func (j journal) file() string { return filepath.Join(j.dir, "changes.jsonl") }

func (j journal) read() []change {
	raw, err := os.ReadFile(j.file())
	if err != nil {
		return nil
	}
	var out []change
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var c change
		if len(line) > 0 && json.Unmarshal(line, &c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func (j journal) add(cs []change) {
	if len(cs) == 0 {
		return
	}
	list := append(j.read(), cs...)
	if len(list) > journalMax {
		list = list[len(list)-journalMax:]
	}
	var b bytes.Buffer
	for _, c := range list {
		raw, _ := json.Marshal(c)
		b.Write(raw)
		b.WriteByte('\n')
	}
	tmp := j.file() + ".tmp"
	if os.WriteFile(tmp, b.Bytes(), 0o600) == nil {
		_ = os.Rename(tmp, j.file())
	}
}

// between is the changes in [since, until], newest first.
func (j journal) between(since, until time.Time, limit int) []change {
	var out []change
	list := j.read()
	for i := len(list) - 1; i >= 0 && len(out) < limit; i-- {
		c := list[i]
		if c.At.Before(since) || c.At.After(until) {
			continue
		}
		out = append(out, c)
	}
	return out
}
