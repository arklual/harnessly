// Package chatwork keeps the work of the machine's chats: which git worktrees a chat changed or committed to, on which
// branch, from which commit, with which commits and merge request (hubproto.ChatWorkEntry). The runtime registers the
// work as the chat runs (Observer) or, for a chat older than the registry, from its transcript (Scan); the records are
// kept in one file under the runtime's data folder and handed to the hub over the replica socket.
package chatwork

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/jsonlog"
)

// Entry is one worktree of one chat.
type Entry = hubproto.ChatWorkEntry

const (
	// maxEntries bounds the registry; the records looked at longest ago go first.
	maxEntries = 5000
	// maxScanned bounds the chats remembered as scanned.
	maxScanned = 20000
)

type key struct{ conv, worktree string }

// Registry is the machine's records. Its methods are safe for concurrent use.
type Registry struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	entries  map[key]*Entry
	scanned  map[string]bool
	watchers map[int]func([]Entry)
	next     int
}

type stored struct {
	Entries []Entry  `json:"entries"`
	Scanned []string `json:"scanned"`
}

// Open reads the registry kept at path ("" keeps nothing on disk). A file that does not read starts an empty registry.
func Open(path string) *Registry {
	r := &Registry{path: path, now: time.Now, entries: map[key]*Entry{}, scanned: map[string]bool{}, watchers: map[int]func([]Entry){}}
	if path == "" {
		return r
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	var s stored
	if err := json.Unmarshal(b, &s); err != nil {
		jsonlog.Log("warn", "chatwork: the registry does not read, starting anew", jsonlog.F{K: "error", V: err.Error()})
		return r
	}
	for i := range s.Entries {
		e := s.Entries[i]
		if e.Conv != "" && e.Worktree != "" {
			r.entries[key{e.Conv, e.Worktree}] = &e
		}
	}
	for _, c := range s.Scanned {
		r.scanned[c] = true
	}
	return r
}

// Put records e (merged into the record of the same chat and worktree) and reports whether anything changed. The first
// touch, the base and the merge request are kept from the older record when e has none; a live record is not turned
// into a scanned one.
func (r *Registry) Put(e Entry) bool {
	if e.Conv == "" || e.Worktree == "" {
		return false
	}
	r.mu.Lock()
	k := key{e.Conv, e.Worktree}
	if old := r.entries[k]; old != nil {
		e = merge(*old, e)
		if same(*old, e) {
			r.mu.Unlock()
			return false
		}
	}
	if e.At == 0 {
		e.At = r.now().UnixMilli()
	}
	if e.Since == 0 {
		e.Since = e.At
	}
	r.entries[k] = &e
	r.pruneLocked()
	snap := r.saveLocked()
	watchers := r.watcherList()
	r.mu.Unlock()
	r.write(snap)
	for _, w := range watchers {
		w([]Entry{e})
	}
	return true
}

func merge(old, e Entry) Entry {
	// The base of the earlier touch stands: the work began there.
	if old.Base != "" && (e.Base == "" || e.Since == 0 || old.Since <= e.Since) {
		e.Base = old.Base
	}
	if old.Since != 0 && (e.Since == 0 || old.Since < e.Since) {
		e.Since = old.Since
	}
	if e.MR == "" {
		e.MR = old.MR
	}
	if e.Parent == "" {
		e.Parent = old.Parent
	}
	if e.Remote == "" {
		e.Remote = old.Remote
	}
	if e.Branch == "" {
		e.Branch = old.Branch
	}
	if e.Head == "" {
		e.Head = old.Head
	}
	if len(e.Commits) == 0 {
		e.Commits = old.Commits
	} else if len(old.Commits) > 0 {
		e.Commits = unionCommits(e.Commits, old.Commits)
	}
	if old.Source == hubproto.ChatWorkLive {
		e.Source = hubproto.ChatWorkLive
	}
	if e.At != 0 && e.At < old.At {
		e.At = old.At
	}
	return e
}

// unionCommits is newer followed by the commits of older it lacks, at most ChatWorkCommitsMax: a commit the chat made is
// not forgotten when a later look no longer finds it in the reflog.
func unionCommits(newer, older []string) []string {
	out := slices.Clone(newer)
	for _, c := range older {
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out[:min(len(out), hubproto.ChatWorkCommitsMax)]
}

// same: nothing but the time of the look differs.
func same(a, b Entry) bool {
	a.At, b.At = 0, 0
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	return string(ja) == string(jb)
}

func (r *Registry) pruneLocked() {
	if len(r.entries) <= maxEntries {
		return
	}
	all := make([]*Entry, 0, len(r.entries))
	for _, e := range r.entries {
		all = append(all, e)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].At < all[j].At })
	for _, e := range all[:len(all)-maxEntries] {
		delete(r.entries, key{e.Conv, e.Worktree})
	}
}

// Of are the records of the given chats, the newest look first.
func (r *Registry) Of(convs ...string) []Entry {
	want := map[string]bool{}
	for _, c := range convs {
		want[c] = true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Entry
	for _, e := range r.entries {
		if want[e.Conv] {
			out = append(out, *e)
		}
	}
	sortEntries(out)
	return out
}

// All are every record, the newest look first.
func (r *Registry) All() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, *e)
	}
	sortEntries(out)
	return out
}

func sortEntries(list []Entry) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].At != list[j].At {
			return list[i].At > list[j].At
		}
		if list[i].Conv != list[j].Conv {
			return list[i].Conv < list[j].Conv
		}
		return list[i].Worktree < list[j].Worktree
	})
}

// Scanned reports whether a chat's transcript was already looked through.
func (r *Registry) Scanned(conv string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.scanned[conv]
}

// MarkScanned remembers that a chat's transcript was looked through.
func (r *Registry) MarkScanned(conv string) {
	r.mu.Lock()
	if r.scanned[conv] {
		r.mu.Unlock()
		return
	}
	r.scanned[conv] = true
	if len(r.scanned) > maxScanned {
		for c := range r.scanned { // any of them: a chat scanned again only finds the same work
			if c != conv {
				delete(r.scanned, c)
				break
			}
		}
	}
	snap := r.saveLocked()
	r.mu.Unlock()
	r.write(snap)
}

// Watch calls fn with every record that changes from now on, outside the registry's lock. stop ends it.
func (r *Registry) Watch(fn func([]Entry)) (stop func()) {
	r.mu.Lock()
	id := r.next
	r.next++
	r.watchers[id] = fn
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		delete(r.watchers, id)
		r.mu.Unlock()
	}
}

func (r *Registry) watcherList() []func([]Entry) {
	out := make([]func([]Entry), 0, len(r.watchers))
	for _, w := range r.watchers {
		out = append(out, w)
	}
	return out
}

func (r *Registry) saveLocked() []byte {
	if r.path == "" {
		return nil
	}
	s := stored{Entries: make([]Entry, 0, len(r.entries)), Scanned: make([]string, 0, len(r.scanned))}
	for _, e := range r.entries {
		s.Entries = append(s.Entries, *e)
	}
	sortEntries(s.Entries)
	for c := range r.scanned {
		s.Scanned = append(s.Scanned, c)
	}
	sort.Strings(s.Scanned)
	b, _ := json.Marshal(s)
	return b
}

// writeMu keeps the writes of the file in order.
var writeMu sync.Mutex

func (r *Registry) write(b []byte) {
	if r.path == "" || b == nil {
		return
	}
	writeMu.Lock()
	defer writeMu.Unlock()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o700); err != nil {
		return
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		jsonlog.Log("warn", "chatwork: the registry not written", jsonlog.F{K: "error", V: err.Error()})
		return
	}
	_ = os.Rename(tmp, r.path)
}

// WorkFrames are the replica socket's frames of every record ({"k":"work","entries":[…]}), at most
// hubproto.ChatWorkFrameMax records each.
func (r *Registry) WorkFrames() [][]byte {
	all := r.All()
	var out [][]byte
	for len(all) > 0 {
		n := min(len(all), hubproto.ChatWorkFrameMax)
		out = append(out, frame(all[:n]))
		all = all[n:]
	}
	return out
}

// WatchWork calls fn with a replica frame of every record that changes from now on.
func (r *Registry) WatchWork(fn func(frame []byte)) (stop func()) {
	return r.Watch(func(list []Entry) { fn(frame(list)) })
}

func frame(list []Entry) []byte {
	b, _ := json.Marshal(map[string]any{"k": "work", "entries": list})
	return b
}
