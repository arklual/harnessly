// Package filesync keeps one machine's folder in step with a collection of the store (internal/filestore): local
// edits go up with the revision they were made on, other machines' edits come down and land atomically, and an edit
// made on a stale copy is kept (revision.go) instead of being lost. It is the agent of design-sync, carried into
// harnessly: a runtime runs one for its Sinopia library.
package filesync

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"os/user"
	"path"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
)

// Config is one folder.
type Config struct {
	Root     string // the folder: a Sinopia library
	StateDir string // where the sync keeps what it knows, outside the folder's synced paths
	// Store is the collection the folder syncs with.
	Store   *Client
	Machine string // shown in what a lost write leaves behind
	Policy  filestore.Policy
	Codec   Codec
	// TmpDir is where downloads are staged before the rename (relative to Root, same filesystem).
	TmpDir string
	// ConflictDir, when set, takes a losing local edit (relative to Root, never synced) instead of the handling by
	// revision (revision.go).
	ConflictDir string
	// AdoptRemote: on the very first sync, a local file the collection does not have goes to ConflictDir instead of
	// up. That is joining a collection whose truth lives on another machine: what only this machine had is kept
	// aside, not spread.
	AdoptRemote bool
	// KeepUnseen: a file the store stops showing this machine stays in the folder (a folder several runtimes share,
	// where another runtime's person may still have a role in it); otherwise an unedited one is removed.
	KeepUnseen bool
	// OnApplied is called after another machine's revision lands (outside the agent's lock).
	OnApplied func(rel string)
	// Log is where the sync says what it did (nil: slog's default).
	Log *slog.Logger
}

// Agent syncs one folder.
type Agent struct {
	cfg      Config
	root     *os.Root
	state    *state
	hub      *Client
	log      *slog.Logger
	policy   filestore.Policy
	codec    Codec
	tmp      string
	adopting bool
	applied  []string
	pending  map[string]bool

	mu    sync.Mutex // one file operation at a time: a download and a scan of the same path must not interleave
	users map[uint32]string
	now   func() time.Time
}

// New opens the folder and the agent's state.
func New(cfg Config) (*Agent, error) {
	root, err := os.OpenRoot(cfg.Root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, err
	}
	st, err := openState(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	if cfg.Store == nil {
		root.Close()
		st.db.Close()
		return nil, errors.New("filesync: no store")
	}
	log := cfg.Log
	if log == nil {
		log = slog.Default()
	}
	a := &Agent{cfg: cfg, root: root, state: st, hub: cfg.Store,
		log: log.With("collection", cfg.Store.Collection()), users: map[uint32]string{}, now: time.Now,
		policy: cfg.Policy, codec: cfg.Codec, tmp: cfg.TmpDir, pending: map[string]bool{}}
	if a.policy == nil {
		a.policy = filestore.Design{}
	}
	if a.codec == nil {
		a.codec = Plain{}
	}
	if a.tmp == "" {
		a.tmp = filestore.TmpDir
	}
	if err := a.root.MkdirAll(a.tmp, 0o700); err != nil {
		return nil, err
	}
	a.cleanTmp()
	return a, nil
}

// Close releases the folder and the state.
func (a *Agent) Close() {
	a.root.Close()
	a.state.db.Close()
}

func (a *Agent) cleanTmp() {
	entries, err := fs.ReadDir(a.root.FS(), a.tmp)
	if err != nil {
		return
	}
	for _, e := range entries {
		_ = a.root.Remove(path.Join(a.tmp, e.Name()))
	}
}

func sha(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func mtime(fi fs.FileInfo) int64 { return fi.ModTime().UnixNano() }

// author is the owner of the file: on a shared machine that is whose agent wrote it.
func (a *Agent) author(fi fs.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	if name, ok := a.users[st.Uid]; ok {
		return name
	}
	name := strconv.FormatUint(uint64(st.Uid), 10)
	if u, err := user.LookupId(name); err == nil {
		name = u.Username
	}
	a.users[st.Uid] = name
	return name
}

// readRegular reads a regular file without following a symlink at its place.
func (a *Agent) readRegular(rel string) ([]byte, fs.FileInfo, error) {
	f, err := a.root.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, nil, errNotRegular
	}
	if fi.Size() > filestore.MaxFileSize {
		return nil, nil, errTooBig
	}
	data, err := io.ReadAll(io.LimitReader(f, filestore.MaxFileSize+1))
	if err != nil {
		return nil, nil, err
	}
	return data, fi, nil
}

var (
	errNotRegular = errors.New("not a regular file")
	errTooBig     = errors.New("bigger than the sync limit")
)

// Scan looks at one path (a file, a folder or something gone) and sends what changed to the hub.
func (a *Agent) Scan(ctx context.Context, rel string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.scan(ctx, rel)
}

func (a *Agent) scan(ctx context.Context, rel string) error {
	fi, err := a.root.Lstat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return a.scanGone(ctx, rel)
	case err != nil:
		return err
	case fi.IsDir():
		return a.scanDir(ctx, rel)
	case !fi.Mode().IsRegular() || !a.policy.Synced(rel):
		return nil
	}
	return a.scanFile(ctx, rel, fi)
}

// scanGone handles a path that is not there: the file was deleted, or a folder moved away with everything in it.
func (a *Agent) scanGone(ctx context.Context, rel string) error {
	paths, err := a.state.live(rel)
	if err != nil {
		return err
	}
	if k, err := a.state.get(rel); err == nil && k != nil && !k.Deleted {
		paths = append(paths, rel)
	}
	for _, p := range paths {
		if !a.policy.Synced(p) {
			// The policy stopped syncing this path after it was synced: forget it here, the room keeps its last
			// revision. The hub refuses the path now, so a delete would only fail forever.
			if err := a.state.forget(p); err != nil {
				return err
			}
			a.log.Info("no longer synced, forgotten", "path", p)
			continue
		}
		if _, err := a.root.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := a.pushDelete(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

func (a *Agent) scanDir(ctx context.Context, rel string) error {
	if a.policy.SkippedDir(rel) {
		return nil
	}
	return fs.WalkDir(a.root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && a.policy.SkippedDir(p) || p == a.tmp {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !a.policy.Synced(p) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if err := a.scanFile(ctx, p, fi); err != nil {
			a.log.Warn("scan", "path", p, "err", err)
		}
		return nil
	})
}

func (a *Agent) scanFile(ctx context.Context, rel string, fi fs.FileInfo) error {
	return a.scanFileDepth(ctx, rel, fi, 0)
}

func (a *Agent) scanFileDepth(ctx context.Context, rel string, fi fs.FileInfo, depth int) error {
	k, err := a.state.get(rel)
	if err != nil {
		return err
	}
	if k != nil && !k.Deleted && k.Size == fi.Size() && k.Mtime == mtime(fi) {
		return nil
	}
	data, fi, err := a.readRegular(rel)
	if errors.Is(err, errTooBig) || errors.Is(err, errNotRegular) || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	canon := a.codec.Canonical(rel, data)
	tag := a.codec.Tag(canon)
	if k != nil && !k.Deleted && k.Sha == tag {
		k.Size, k.Mtime = fi.Size(), mtime(fi)
		return a.state.put(rel, *k)
	}
	if a.adopting && k == nil {
		return a.setAside(rel, data, "not in the room when this machine joined")
	}
	var base int64
	if k != nil {
		base = k.Rev
	}
	blob, err := a.codec.Seal(rel, canon)
	if err != nil {
		return err
	}
	sum := sha(blob)
	if err := a.upload(ctx, sum, blob); err != nil {
		return err
	}
	resp, err := a.hub.Commit(ctx, filestore.CommitRequest{Path: rel, Base: base, Sha: sum, Tag: tag, Size: int64(len(canon)),
		Exec: fi.Mode()&0o100 != 0, Author: a.author(fi)})
	if errors.Is(err, ErrRefused) {
		return a.refused(rel, base, tag, fi)
	}
	if err != nil {
		return err
	}
	if resp.Conflict {
		return a.resolve(ctx, rel, data, canon, *resp.Entry, depth)
	}
	a.log.Info("sent", "path", rel, "rev", resp.Entry.Rev)
	if err := a.keepBase(rel, canon); err != nil {
		return err
	}
	return a.state.put(rel, known{Rev: resp.Entry.Rev, Sha: tag, Size: fi.Size(), Mtime: mtime(fi)})
}

// refused remembers a local file the store does not take from this machine (a project none of its people may edit,
// HRN-880): it is not sent again until it changes, and a newer revision from the store replaces it as usual.
func (a *Agent) refused(rel string, base int64, tag string, fi fs.FileInfo) error {
	a.log.Info("not sent: none of this machine's people may edit it", "path", rel)
	return a.state.put(rel, known{Rev: base, Sha: refusedPrefix + tag, Size: fi.Size(), Mtime: mtime(fi)})
}

// refusedPrefix marks what the agent knows of a file the store did not take: it is not the store's revision, so it is
// never treated as an unedited copy (a newer revision keeps it aside, a lost role does not delete it).
const refusedPrefix = "refused:"

func isRefused(k *known) bool { return k != nil && strings.HasPrefix(k.Sha, refusedPrefix) }

// keepBase remembers the last synced canonical contents of a file the codec merges: the base of a three-way merge.
func (a *Agent) keepBase(rel string, canon []byte) error {
	if m, ok := a.codec.(interface{ Mergeable(string) bool }); ok && m.Mergeable(rel) {
		return a.state.putBase(rel, canon)
	}
	return nil
}

// fetch downloads and opens a revision's canonical contents.
func (a *Agent) fetch(ctx context.Context, e filestore.Entry) ([]byte, error) {
	var buf bytes.Buffer
	if err := a.hub.GetBlob(ctx, e.Sha, &limitWriter{w: &buf, n: filestore.MaxFileSize + 1024}); err != nil {
		return nil, err
	}
	if sha(buf.Bytes()) != e.Sha {
		return nil, fmt.Errorf("download of %s does not hash to %s", e.Path, e.Sha)
	}
	canon, err := a.codec.Open(e.Path, buf.Bytes())
	if err != nil {
		return nil, err
	}
	if !e.Same(a.codec.Tag(canon)) {
		return nil, fmt.Errorf("contents of %s rev %d do not match their fingerprint", e.Path, e.Rev)
	}
	return canon, nil
}

type limitWriter struct {
	w io.Writer
	n int64
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > l.n {
		return 0, errTooBig
	}
	l.n -= int64(len(p))
	return l.w.Write(p)
}

// merge tries a three-way merge of a local edit with another machine's revision. On success the merged file is in
// place and the agent's record says "at the remote revision, with local changes", so the next scan sends the merge
// on top of it.
func (a *Agent) merge(ctx context.Context, rel string, local, canon []byte, remote filestore.Entry, localNewer bool, depth int) (bool, error) {
	m, ok := a.codec.(interface{ Mergeable(string) bool })
	if !ok || !m.Mergeable(rel) || remote.Deleted || depth > 2 {
		return false, nil
	}
	base, err := a.state.getBase(rel)
	if err != nil || base == nil {
		return false, err
	}
	theirs, err := a.fetch(ctx, remote)
	if err != nil {
		return false, err
	}
	merged, ok := a.codec.Merge(rel, base, canon, theirs, localNewer)
	if !ok {
		return false, nil
	}
	if err := a.place(rel, bytes.NewReader(a.codec.Local(rel, merged, local)), false); err != nil {
		return false, err
	}
	if err := a.state.putBase(rel, theirs); err != nil {
		return false, err
	}
	if err := a.state.put(rel, known{Rev: remote.Rev, Sha: a.codec.Tag(theirs), Size: -1}); err != nil {
		return false, err
	}
	a.log.Info("merged", "path", rel, "with", remote.Rev, "by", remote.Author+"@"+remote.Machine)
	fi, err := a.root.Lstat(rel)
	if err != nil {
		return true, err
	}
	return true, a.scanFileDepth(ctx, rel, fi, depth+1)
}

func (a *Agent) upload(ctx context.Context, sum string, data []byte) error {
	ok, err := a.hub.HasBlob(ctx, sum)
	if err != nil || ok {
		return err
	}
	return a.hub.PutBlob(ctx, sum, data)
}

func (a *Agent) pushDelete(ctx context.Context, rel string) error {
	k, err := a.state.get(rel)
	if err != nil || k == nil || k.Deleted {
		return err
	}
	resp, err := a.hub.Commit(ctx, filestore.CommitRequest{Path: rel, Base: k.Rev, Deleted: true})
	if errors.Is(err, ErrRefused) {
		a.log.Info("delete not taken: none of this machine's people may edit it", "path", rel)
		return a.state.forget(rel)
	}
	if err != nil {
		return err
	}
	if resp.Conflict {
		// Someone changed the file after our copy: their version wins over our delete, nothing is lost.
		a.log.Info("delete refused, file changed elsewhere", "path", rel, "rev", resp.Entry.Rev)
		return a.apply(ctx, *resp.Entry)
	}
	a.log.Info("sent delete", "path", rel, "rev", resp.Entry.Rev)
	return a.state.put(rel, known{Rev: resp.Entry.Rev, Deleted: true})
}

// resolve handles a local edit made on top of a stale copy: a file the codec merges is merged; otherwise the local
// contents go aside and the file takes the hub's current revision: in a room with a quarantine (ConflictDir) into it,
// in the design library by revision (revision.go: kept with a notice, a bigger edit of a board also as a variant
// project), in an older room as a conflict copy beside the file (keepConflict).
func (a *Agent) resolve(ctx context.Context, rel string, data, canon []byte, current filestore.Entry, depth int) error {
	if !current.Deleted && current.Same(a.codec.Tag(canon)) {
		fi, err := a.root.Lstat(rel)
		if err != nil {
			return err
		}
		if err := a.keepBase(rel, canon); err != nil {
			return err
		}
		return a.state.put(rel, known{Rev: current.Rev, Sha: a.codec.Tag(canon), Size: fi.Size(), Mtime: mtime(fi)})
	}
	if ok, err := a.merge(ctx, rel, data, canon, current, true, depth); ok || err != nil {
		return err
	}
	if a.cfg.ConflictDir == "" {
		return a.byRevision(ctx, rel, data, current)
	}
	copyRel, beside, err := a.keepConflict(rel, data)
	if err != nil {
		return err
	}
	a.log.Warn("conflict: local edit kept aside", "path", rel, "copy", copyRel, "current", current.Rev, "by", current.Author+"@"+current.Machine)
	if err := a.apply(ctx, current); err != nil {
		return err
	}
	if !beside {
		return nil
	}
	fi, err := a.root.Lstat(copyRel)
	if err != nil {
		return err
	}
	return a.scanFile(ctx, copyRel, fi)
}

// ConflictName is where a conflicting local edit of rel is kept: `Main (конфликт vm-b 2026-10-18 2141).dc.html`,
// a board of its own in the same project, so the person and the agents see it.
func ConflictName(rel, machine string, at time.Time) string {
	dir, file := path.Split(rel)
	stem, ext := file, ""
	for _, e := range []string{".dc.html", ".tar.gz"} {
		if strings.HasSuffix(file, e) && len(file) > len(e) {
			stem, ext = strings.TrimSuffix(file, e), e
			break
		}
	}
	if ext == "" {
		if i := strings.LastIndex(file, "."); i > 0 {
			stem, ext = file[:i], file[i:]
		}
	}
	return dir + fmt.Sprintf("%s (конфликт %s %s)%s", stem, machine, at.Format("2006-01-02 1504"), ext)
}

// keepConflict puts a losing local edit aside: into the quarantine when the room has one, otherwise beside the file
// as a copy of its own that syncs (beside = true).
func (a *Agent) keepConflict(rel string, data []byte) (string, bool, error) {
	if a.cfg.ConflictDir != "" {
		q := path.Join(a.cfg.ConflictDir, rel+"."+a.cfg.Machine+"."+a.now().UTC().Format("20060102-150405"))
		return q, false, a.place(q, bytes.NewReader(data), false)
	}
	name := ConflictName(rel, a.cfg.Machine, a.now())
	candidate := name
	for i := 2; ; i++ {
		if _, err := a.root.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			break
		}
		ext := path.Ext(name)
		if strings.HasSuffix(name, ".dc.html") {
			ext = ".dc.html"
		}
		candidate = strings.TrimSuffix(name, ext) + " " + strconv.Itoa(i) + ext
	}
	return candidate, true, a.place(candidate, bytes.NewReader(data), false)
}

// setAside moves a local-only file into the quarantine (AdoptRemote): kept, not spread, out of the way.
func (a *Agent) setAside(rel string, data []byte, why string) error {
	if a.cfg.ConflictDir == "" {
		return nil
	}
	q, _, err := a.keepConflict(rel, data)
	if err != nil {
		return err
	}
	if err := a.root.Remove(rel); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	a.pruneEmpty(path.Dir(rel))
	a.log.Info("set aside", "path", rel, "to", q, "why", why)
	return nil
}

// place writes contents into rel atomically: a staging file in the agent's own tmp folder, fsync, rename. The rename
// keeps readers (the library server, Claude reading its settings) from ever seeing half a file.
func (a *Agent) place(rel string, r io.Reader, exec bool) error {
	if dir := path.Dir(rel); dir != "." {
		if err := a.root.MkdirAll(dir, 0o777); err != nil {
			return err
		}
	}
	var rnd [8]byte
	_, _ = rand.Read(rnd[:])
	tmp := path.Join(a.tmp, hex.EncodeToString(rnd[:]))
	mode := os.FileMode(0o666)
	if exec {
		mode = 0o777
	}
	f, err := a.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(r, filestore.MaxFileSize+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = a.root.Rename(tmp, rel)
	}
	if err != nil {
		_ = a.root.Remove(tmp)
	}
	return err
}

// Apply brings one revision from the hub into the folder.
func (a *Agent) Apply(ctx context.Context, e filestore.Entry) error {
	a.mu.Lock()
	err := a.applyRemote(ctx, e)
	if err != nil && errors.Is(err, fs.ErrPermission) {
		// A file without rights must not stop the whole queue: set it aside, say so, go on.
		a.log.Warn("quarantined: no rights on the local file", "path", e.Path, "rev", e.Rev, "err", err)
		if qerr := a.state.quarantine(e.Path, e.Rev, err.Error(), a.now()); qerr != nil {
			err = qerr
		} else {
			err = nil
		}
	} else if err == nil {
		_ = a.state.release(e.Path)
	}
	applied := a.applied
	a.applied = nil
	a.mu.Unlock()
	if a.cfg.OnApplied != nil {
		for _, rel := range applied {
			a.cfg.OnApplied(rel)
		}
	}
	return err
}

// Quarantined lists the files set aside because the agent had no rights on them.
func (a *Agent) Quarantined() ([]Quarantined, error) { return a.state.quarantined() }

// RetryQuarantine tries the files in quarantine again once their rights may be back: it reads the hub's history from
// the oldest revision set aside and applies the latest one of each such file. A file still without rights stays.
func (a *Agent) RetryQuarantine(ctx context.Context) error {
	q, err := a.state.quarantined()
	if err != nil || len(q) == 0 {
		return err
	}
	want := make(map[string]bool, len(q))
	since := q[0].Rev
	for _, f := range q {
		want[f.Path] = true
		since = min(since, f.Rev)
	}
	latest := map[string]filestore.Entry{}
	for since--; ; {
		ch, err := a.hub.Changes(ctx, since, 0)
		if err != nil {
			return err
		}
		for _, e := range ch.Entries {
			if want[e.Path] {
				latest[e.Path] = e
			}
		}
		if !ch.More || ch.Rev <= since {
			break
		}
		since = ch.Rev
	}
	for _, f := range q {
		e, ok := latest[f.Path]
		if !ok {
			// The hub forgot it (gc): nothing to bring back.
			_ = a.state.release(f.Path)
			continue
		}
		if err := a.Apply(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// applyRemote takes another machine's revision. A local edit that has not reached the hub yet is not overwritten:
// it is merged (a file the codec merges) or put aside first.
func (a *Agent) applyRemote(ctx context.Context, e filestore.Entry) error {
	if !a.policy.Synced(e.Path) {
		return nil
	}
	k, err := a.state.get(e.Path)
	if err != nil {
		return err
	}
	if k != nil && k.Rev >= e.Rev {
		return nil
	}
	fi, err := a.root.Lstat(e.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if e.Deleted {
			return a.state.put(e.Path, known{Rev: e.Rev, Deleted: true})
		}
		return a.apply(ctx, e)
	case err != nil:
		return err
	case !fi.Mode().IsRegular():
		a.log.Warn("not a regular file locally, skipped", "path", e.Path)
		return nil
	}
	clean := k != nil && !k.Deleted && k.Size == fi.Size() && k.Mtime == mtime(fi) && !isRefused(k)
	if !clean {
		data, _, err := a.readRegular(e.Path)
		if err != nil {
			return err
		}
		canon := a.codec.Canonical(e.Path, data)
		tag := a.codec.Tag(canon)
		switch {
		case !e.Deleted && e.Same(tag):
			if err := a.keepBase(e.Path, canon); err != nil {
				return err
			}
			return a.state.put(e.Path, known{Rev: e.Rev, Sha: tag, Size: fi.Size(), Mtime: mtime(fi)})
		case k != nil && !k.Deleted && tag == k.Sha:
			// Only the mtime moved: the contents are what the agent already knew.
		default:
			localNewer := fi.ModTime().UnixMilli() > e.At
			if ok, err := a.merge(ctx, e.Path, data, canon, e, localNewer, 0); ok || err != nil {
				return err
			}
			if a.cfg.ConflictDir == "" {
				return a.byRevision(ctx, e.Path, data, e)
			}
			copyRel, beside, err := a.keepConflict(e.Path, data)
			if err != nil {
				return err
			}
			a.log.Warn("conflict: unsent local edit kept aside", "path", e.Path, "copy", copyRel, "incoming", e.Rev)
			if err := a.apply(ctx, e); err != nil {
				return err
			}
			if !beside {
				return nil
			}
			cfi, err := a.root.Lstat(copyRel)
			if err != nil {
				return err
			}
			return a.scanFile(ctx, copyRel, cfi)
		}
	}
	return a.apply(ctx, e)
}

// apply makes the local file be revision e, whatever is there now.
func (a *Agent) apply(ctx context.Context, e filestore.Entry) error {
	if e.Deleted {
		if fi, err := a.root.Lstat(e.Path); err == nil && fi.Mode().IsRegular() {
			if err := a.root.Remove(e.Path); err != nil {
				return err
			}
			a.pruneEmpty(path.Dir(e.Path))
		}
		a.log.Info("removed", "path", e.Path, "rev", e.Rev, "by", e.Author+"@"+e.Machine)
		a.applied = append(a.applied, e.Path)
		return a.state.put(e.Path, known{Rev: e.Rev, Deleted: true})
	}
	canon, err := a.fetch(ctx, e)
	if err != nil {
		return err
	}
	var current []byte
	if data, _, err := a.readRegular(e.Path); err == nil {
		current = data
	}
	if err := a.place(e.Path, bytes.NewReader(a.codec.Local(e.Path, canon, current)), e.Exec); err != nil {
		return err
	}
	fi, err := a.root.Lstat(e.Path)
	if err != nil {
		return err
	}
	a.log.Info("received", "path", e.Path, "rev", e.Rev, "by", e.Author+"@"+e.Machine)
	if err := a.keepBase(e.Path, canon); err != nil {
		return err
	}
	a.applied = append(a.applied, e.Path)
	return a.state.put(e.Path, known{Rev: e.Rev, Sha: a.codec.Tag(canon), Size: fi.Size(), Mtime: mtime(fi)})
}

// pruneEmpty removes folders left empty by a delete, up to and including the project folder: a project whose last
// file went away should not stay behind as an empty card in the library. A folder that still holds anything (even
// a file that does not sync) stays.
func (a *Agent) pruneEmpty(dir string) {
	for dir != "." && dir != "" && dir != "/" {
		if err := a.root.Remove(dir); err != nil {
			return
		}
		dir = path.Dir(dir)
	}
}

// Pull asks the hub for everything after the agent's cursor and applies it; with wait, it holds until something
// changes or the wait passes. It returns whether the hub had more.
func (a *Agent) Pull(ctx context.Context, wait time.Duration) (bool, error) {
	since, err := a.state.cursor()
	if err != nil {
		return false, err
	}
	ch, err := a.hub.Changes(ctx, since, wait)
	if err != nil {
		return false, err
	}
	if ch.Rev < since {
		a.log.Warn("hub is behind the agent's cursor, catching up from the start", "cursor", since, "hub", ch.Rev)
		return true, a.state.setCursor(0)
	}
	if was, err := a.state.scope(); err != nil {
		return false, err
	} else if was != ch.Scope {
		return true, a.rescope(ctx, was, ch.Scope)
	}
	for _, e := range ch.Entries {
		if err := a.Apply(ctx, e); err != nil {
			// Keep the cursor before this entry: the next pull tries it again.
			a.log.Warn("apply", "path", e.Path, "rev", e.Rev, "err", err)
			if e.Rev-1 > since {
				_ = a.state.setCursor(e.Rev - 1)
			}
			return false, err
		}
	}
	return ch.More, a.state.setCursor(ch.Rev)
}

// rescope follows a change of what the store shows this machine (a role given or taken on a project): the
// room is read again from the start, so a project that became visible arrives whole, and a file the agent knew that
// the store no longer shows it is removed when it was not edited here (an edit stays, unsynced). The first scope a
// store names (was "") only reads the room again.
func (a *Agent) rescope(ctx context.Context, was, now string) error {
	seen := map[string]bool{}
	var since int64
	for {
		ch, err := a.hub.Changes(ctx, since, 0)
		if err != nil {
			return err
		}
		if ch.Scope != now {
			// It moved again while it was read: the next pull starts over.
			return nil
		}
		for _, e := range ch.Entries {
			seen[e.Path] = true
			if err := a.Apply(ctx, e); err != nil {
				a.log.Warn("apply", "path", e.Path, "rev", e.Rev, "err", err)
			}
		}
		if !ch.More || ch.Rev <= since {
			since = ch.Rev
			break
		}
		since = ch.Rev
	}
	if was != "" && !a.cfg.KeepUnseen {
		if err := a.dropUnseen(seen); err != nil {
			return err
		}
	}
	a.log.Info("what the store shows this machine changed, read again", "rev", since)
	if err := a.state.setCursor(since); err != nil {
		return err
	}
	return a.state.setScope(now)
}

// dropUnseen removes the files the agent knew that the store no longer shows this machine.
func (a *Agent) dropUnseen(seen map[string]bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	paths, err := a.state.live("")
	if err != nil {
		return err
	}
	for _, p := range paths {
		if seen[p] {
			continue
		}
		k, err := a.state.get(p)
		if err != nil || k == nil {
			continue
		}
		if fi, err := a.root.Lstat(p); err == nil && fi.Mode().IsRegular() && k.Size == fi.Size() && k.Mtime == mtime(fi) && !isRefused(k) {
			if err := a.root.Remove(p); err == nil {
				a.pruneEmpty(path.Dir(p))
			}
		}
		if err := a.state.forget(p); err != nil {
			return err
		}
		a.log.Info("no longer shown to this machine, removed", "path", p)
	}
	return nil
}

// ScanAll walks the whole folder and every file the agent believes in: catches edits made while it was not running
// and events the watcher missed.
func (a *Agent) ScanAll(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.scanDir(ctx, "."); err != nil {
		return err
	}
	return a.scanGone(ctx, "")
}

// SyncOnce pulls everything, then sends everything local: what the agent does at start.
func (a *Agent) SyncOnce(ctx context.Context) error {
	if !a.codec.Ready() {
		return errNoKey
	}
	first := false
	if cur, err := a.state.cursor(); err == nil && cur == 0 && a.cfg.AdoptRemote {
		if n, err := a.state.count(); err == nil && n == 0 {
			first = true
		}
	}
	for {
		more, err := a.Pull(ctx, 0)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	a.mu.Lock()
	a.adopting = first
	a.mu.Unlock()
	err := a.ScanAll(ctx)
	a.mu.Lock()
	a.adopting = false
	a.mu.Unlock()
	return err
}

var errNoKey = errors.New("the collection's key is not here yet")
