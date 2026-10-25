package machines

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The disk of the Machine tool: how full it is, and what the used part is made of as far as the runtime can tell cheaply.
// The filesystem says how much is used and free (statfs); it cannot say whose it is. The runtime adds up the folders it
// knows by name (the workspace, the tools' caches, the agents' own data) in the background, at low priority and within a
// time budget, every few minutes; what is used and belongs to none of them is "other". A walk cut short by the budget is
// partial: the numbers are lower bounds, and the answer says so.

const (
	// DiskRescan is how often the folders are walked again; DiskBudget how long one walk may take.
	DiskRescan = 10 * time.Minute
	DiskBudget = 45 * time.Second

	// A walk rests this long after this many entries, so a large tree does not keep a core or the disk to itself.
	restEvery = 256
	restFor   = 2 * time.Millisecond
)

// The parts of the layout, in the order they are drawn. Other is the used part no folder above accounts for.
const (
	DiskWorkspace = "workspace"
	DiskCaches    = "caches"
	DiskAgents    = "agents"
	DiskOther     = "other"
	DiskFree      = "free"
)

// DiskSpec names one part and the folders that make it up.
type DiskSpec struct {
	ID    string
	Roots []string
	// SkipHiddenTop leaves out the hidden entries directly under a root: the workspace can be the home folder, whose dot
	// folders belong to other parts (or to nobody the page should count).
	SkipHiddenTop bool
}

// DiskPart is one part of the layout with its size in bytes.
type DiskPart struct {
	ID    string
	Bytes int64
}

// DiskLayout is the answer. MeasuredAt is 0 before the first walk has ended, and Parts then holds the used part as other.
type DiskLayout struct {
	Path        string
	Total, Free int64
	Parts       []DiskPart
	MeasuredAt  int64 // ms
	Partial     bool
}

// DiskDeps is what the scanner reads.
type DiskDeps struct {
	// Path is a folder of the filesystem to report (the runtime user's home).
	Path  string
	Specs []DiskSpec
	// Rescan and Budget default to DiskRescan and DiskBudget.
	Rescan, Budget time.Duration
	Now            func() time.Time
	// Rest waits between batches of a walk; tests make it a no-op.
	Rest func(time.Duration)
}

// DiskScanner measures the layout in the background.
type DiskScanner struct {
	d DiskDeps

	mu      sync.Mutex
	running bool
	sizes   map[string]int64
	at      time.Time
	partial bool
}

func NewDiskScanner(d DiskDeps) *DiskScanner {
	if d.Rescan == 0 {
		d.Rescan = DiskRescan
	}
	if d.Budget == 0 {
		d.Budget = DiskBudget
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.Rest == nil {
		d.Rest = time.Sleep
	}
	return &DiskScanner{d: d}
}

// Layout is the filesystem now and the folders as the last walk found them. Asking starts a walk when there was none or
// the last is old; the answer never waits for it.
func (s *DiskScanner) Layout() DiskLayout {
	out := DiskLayout{Path: s.d.Path, Parts: []DiskPart{}}
	var st syscall.Statfs_t
	if syscall.Statfs(filepath.Clean(s.d.Path), &st) == nil {
		out.Total, out.Free = int64(st.Blocks)*int64(st.Bsize), int64(st.Bavail)*int64(st.Bsize)
	}
	s.mu.Lock()
	if !s.running && (s.at.IsZero() || s.d.Now().Sub(s.at) > s.d.Rescan) {
		s.running = true
		go s.scan()
	}
	sizes, at, partial := s.sizes, s.at, s.partial
	s.mu.Unlock()

	used := max(0, int64(st.Blocks)-int64(st.Bfree)) * int64(st.Bsize)
	var known int64
	for _, spec := range s.d.Specs {
		n := min(sizes[spec.ID], used-known)
		if n <= 0 {
			continue
		}
		known += n
		out.Parts = append(out.Parts, DiskPart{ID: spec.ID, Bytes: n})
	}
	out.Parts = append(out.Parts, DiskPart{ID: DiskOther, Bytes: used - known}, DiskPart{ID: DiskFree, Bytes: out.Free})
	if !at.IsZero() {
		out.MeasuredAt, out.Partial = at.UnixMilli(), partial
	}
	return out
}

// scan walks every spec's folders within the budget and keeps what it found.
func (s *DiskScanner) scan() {
	ctx, cancel := context.WithTimeout(context.Background(), s.d.Budget)
	defer cancel()
	w := &walker{ctx: ctx, rest: s.d.Rest, seen: map[inode]bool{}, claimed: map[string]bool{}}
	var fs syscall.Stat_t
	if syscall.Stat(filepath.Clean(s.d.Path), &fs) == nil {
		w.dev = uint64(fs.Dev)
	}
	// A folder named by a spec is that part's, also where a wider spec (the workspace) would walk through it.
	for _, spec := range s.d.Specs {
		for _, r := range spec.Roots {
			w.claimed[filepath.Clean(r)] = true
		}
	}
	sizes := map[string]int64{}
	for _, spec := range s.d.Specs {
		for _, r := range spec.Roots {
			sizes[spec.ID] += w.size(filepath.Clean(r), true, spec.SkipHiddenTop)
		}
	}
	s.mu.Lock()
	s.sizes, s.at, s.partial, s.running = sizes, s.d.Now(), ctx.Err() != nil, false
	s.mu.Unlock()
}

type inode struct{ dev, ino uint64 }

type walker struct {
	ctx     context.Context
	rest    func(time.Duration)
	dev     uint64
	seen    map[inode]bool
	claimed map[string]bool
	n       int
}

// size is the disk space the tree under path takes (blocks, not lengths: a sparse file is only what it holds), on the
// filesystem of the walk only. A link is not followed; a file with several names counts once.
func (w *walker) size(path string, top, skipHiddenTop bool) int64 {
	if w.ctx.Err() != nil {
		return 0
	}
	if w.n++; w.n%restEvery == 0 {
		w.rest(restFor)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return 0
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || (w.dev != 0 && uint64(st.Dev) != w.dev) {
		return 0
	}
	own := st.Blocks * 512
	if !info.IsDir() {
		if st.Nlink > 1 {
			key := inode{uint64(st.Dev), st.Ino}
			if w.seen[key] {
				return 0
			}
			w.seen[key] = true
		}
		return own
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return own
	}
	total := own
	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if skipHiddenTop && top && strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if w.claimed[child] {
			continue
		}
		total += w.size(child, false, false)
	}
	return total
}
