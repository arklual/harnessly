// Package datamove moves a machine's own data into the hub's store by itself (docs/migrations/0001-data-to-hub.md,
// HRN-883): it makes an inventory of what an older version kept only on the disk, puts every object into its store
// collection by sha256, checks the store has it, and keeps a journal, its own on the disk and the hub's (reported with
// the store session). A run that stopped goes on where it was; nothing on the disk is deleted.
package datamove

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// ID is the move this version makes.
const ID = "0001-data-to-hub"

// Error codes (docs/migrations/0001-data-to-hub.md says the cause and the fix of each).
const (
	CodeRead    = "MIG-READ"
	CodeGone    = "MIG-GONE"
	CodeHash    = "MIG-HASH"
	CodeTooBig  = "MIG-TOO-BIG"
	CodePut     = "MIG-PUT"
	CodeVerify  = "MIG-VERIFY"
	CodeSession = "MIG-SESSION"
	// CodeNotKept: the hub keeps no place for the object (a sidebar on a hub without history); tried again each pass.
	CodeNotKept = "MIG-NOT-KEPT"
)

// ErrNotKept is a hub that keeps no place for a delivered object (CodeNotKept).
var ErrNotKept = errors.New("the hub keeps no place for it")

// transient codes are tried again by themselves; the others wait for a person (retry or skip).
func transient(code string) bool { return code == CodePut || code == CodeVerify || code == CodeSession }

// Item is one object of the machine's data.
type Item struct {
	Kind   string
	Object string
	Path   string
	// Sha256 is what the machine recorded for the bytes ("" when it recorded nothing: they are what they hash to).
	Sha256 string
	Mime   string
	Name   string
	// Deliver, when set, is an object that is no file: Data goes to the hub by Deliver instead of into a store
	// collection, and is delivered again whenever it changes (the machine's sidebar).
	Data    []byte
	Deliver func(ctx context.Context, data []byte) error
}

// Source lists the objects of one kind.
type Source interface {
	Kind() string
	List(ctx context.Context) ([]Item, error)
}

// Target is the store the objects go to.
type Target interface {
	HasBlob(ctx context.Context, kind, sha string) (bool, error)
	PutBlob(ctx context.Context, kind, sha string, data []byte) error
}

// Journal is the hub's journal (storelink); ErrNoJournal from a hub that keeps none.
type Journal interface {
	Report(ctx context.Context, rep hubproto.MigrationReport) (hubproto.MigrationAnswer, error)
}

// ErrNoJournal is a hub without the journal (an older one): the move goes on with its own on the disk only.
var ErrNoJournal = errors.New("the hub keeps no journal of the move")

// ErrUnauthorized is a store that refused the machine's session (MIG-SESSION).
var ErrUnauthorized = errors.New("the store refused this machine")

// Options make a Mover.
type Options struct {
	Version string
	// StateFile keeps the machine's own journal.
	StateFile string
	Sources   []Source
	Target    Target
	// Journal nil keeps the journal on the disk only.
	Journal Journal
	// MaxBytes is the largest object the store takes (64 MiB).
	MaxBytes int64
	// Every is how often a move that is over takes its inventory again (6 h).
	Every time.Duration
	Log   *slog.Logger
	Now   func() time.Time
}

// Mover moves the objects and keeps the journal.
type Mover struct {
	o    Options
	wake chan struct{}

	mu       sync.Mutex
	st       state
	hub      string // "", "on", or why the hub's journal is not kept
	running  bool
	failWait time.Duration
}

type record struct {
	Kind     string `json:"kind"`
	Object   string `json:"object"`
	Sha256   string `json:"sha256,omitempty"`
	Size     int64  `json:"size,omitempty"`
	MTime    int64  `json:"mtime,omitempty"`
	Mime     string `json:"mime,omitempty"`
	Name     string `json:"name,omitempty"`
	Path     string `json:"path,omitempty"`
	State    string `json:"state"`
	Code     string `json:"code,omitempty"`
	Error    string `json:"error,omitempty"`
	Attempts int    `json:"attempts,omitempty"`
	// Retry is a person's ask to try a failed object again.
	Retry bool `json:"retry,omitempty"`
	// Dirty: changed since the hub last took it.
	Dirty bool  `json:"dirty,omitempty"`
	At    int64 `json:"at,omitempty"`
}

type state struct {
	Migration  string             `json:"migration"`
	Records    map[string]*record `json:"records"`
	StartedAt  int64              `json:"startedAt,omitempty"`
	ChangedAt  int64              `json:"changedAt,omitempty"`
	FinishedAt int64              `json:"finishedAt,omitempty"`
	// VerifiedIn is the version that first finished the move: the local copy stays three versions more.
	VerifiedIn string `json:"verifiedIn,omitempty"`
}

func key(kind, object string) string { return kind + "/" + object }

// New makes a Mover and reads its journal from the disk.
func New(o Options) *Mover {
	if o.MaxBytes <= 0 {
		o.MaxBytes = 64 << 20
	}
	if o.Every <= 0 {
		o.Every = 6 * time.Hour
	}
	if o.Log == nil {
		o.Log = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	m := &Mover{o: o, wake: make(chan struct{}, 1), st: state{Migration: ID, Records: map[string]*record{}}}
	if raw, err := os.ReadFile(o.StateFile); err == nil {
		var st state
		if json.Unmarshal(raw, &st) == nil && st.Migration == ID && st.Records != nil {
			m.st = st
		}
	}
	if o.Journal == nil {
		m.hub = "no hub"
	}
	return m
}

// Wake asks the move to take another pass now.
func (m *Mover) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run moves until ctx ends: a pass now, then again when woken, after a while when something transient failed, and
// every Every when the move is over (what came to the disk since).
func (m *Mover) Run(ctx context.Context) {
	for ctx.Err() == nil {
		wait := m.pass(ctx)
		select {
		case <-ctx.Done():
			return
		case <-m.wake:
		case <-time.After(wait):
		}
	}
}

func (m *Mover) pass(ctx context.Context) time.Duration {
	m.mu.Lock()
	m.running = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running = false
		m.mu.Unlock()
	}()
	items := m.inventory(ctx)
	if ctx.Err() != nil {
		return time.Minute
	}
	m.take(items)
	m.report(ctx)
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		if !m.due(it) {
			continue
		}
		m.move(ctx, it)
		if m.dirty() >= 200 {
			m.report(ctx)
		}
	}
	m.finish()
	m.report(ctx)
	m.save()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.st.Records {
		if r.State == hubproto.MigrationFailed && transient(r.Code) {
			if m.failWait == 0 {
				m.failWait = time.Minute
			} else if m.failWait < 30*time.Minute {
				m.failWait *= 2
			}
			return m.failWait
		}
	}
	m.failWait = 0
	return m.o.Every
}

func (m *Mover) inventory(ctx context.Context) []Item {
	var out []Item
	seen := map[string]bool{}
	for _, s := range m.o.Sources {
		items, err := s.List(ctx)
		if err != nil {
			m.o.Log.Warn("move: an inventory failed, its objects wait for the next pass", "kind", s.Kind(), "err", err)
			continue
		}
		for _, it := range items {
			k := key(it.Kind, it.Object)
			if !seen[k] {
				seen[k] = true
				out = append(out, it)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return key(out[i].Kind, out[i].Object) < key(out[j].Kind, out[j].Object) })
	return out
}

// take writes the inventory into the journal: new objects wait; one that left the disk before it moved leaves the
// journal (a verified one stays: it is in the hub).
func (m *Mover) take(items []Item) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.o.Now().UnixMilli()
	if m.st.StartedAt == 0 {
		m.st.StartedAt = now
	}
	in := map[string]bool{}
	for _, it := range items {
		k := key(it.Kind, it.Object)
		in[k] = true
		r, ok := m.st.Records[k]
		if !ok {
			r = &record{Kind: it.Kind, Object: it.Object, State: hubproto.MigrationPending, Dirty: true, At: now}
			m.st.Records[k] = r
		}
		r.Path, r.Mime, r.Name = it.Path, it.Mime, it.Name
		if it.Sha256 != "" && r.State != hubproto.MigrationVerified {
			r.Sha256 = it.Sha256
		}
	}
	for k, r := range m.st.Records {
		if !in[k] && r.State != hubproto.MigrationVerified {
			delete(m.st.Records, k)
		}
	}
}

// due: the object is to be moved in this pass.
func (m *Mover) due(it Item) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.st.Records[key(it.Kind, it.Object)]
	switch {
	case r == nil:
		return false
	case r.State == hubproto.MigrationSkipped:
		return it.Deliver != nil && r.Code == CodeNotKept
	case r.State == hubproto.MigrationVerified && it.Deliver != nil:
		return sumOf(it.Data) != r.Sha256
	case r.State == hubproto.MigrationVerified:
		// Bytes that changed on the disk since are another object of the same name: moved again.
		fi, err := os.Lstat(r.Path)
		return err == nil && (fi.Size() != r.Size || fi.ModTime().UnixMilli() != r.MTime) && r.Kind != "files"
	case r.State == hubproto.MigrationFailed:
		return r.Retry || transient(r.Code)
	}
	return true
}

func (m *Mover) move(ctx context.Context, it Item) {
	sha, size, mtime, code, err := m.put(ctx, it)
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.st.Records[key(it.Kind, it.Object)]
	if r == nil {
		return
	}
	r.Dirty, r.Retry, r.At = true, false, m.o.Now().UnixMilli()
	if code == CodeNotKept {
		r.State, r.Code, r.Error = hubproto.MigrationSkipped, code, err.Error()
		return
	}
	if code != "" {
		r.State, r.Code, r.Error = hubproto.MigrationFailed, code, err.Error()
		r.Attempts++
		m.o.Log.Warn("move: an object failed", "kind", it.Kind, "object", it.Object, "code", code, "err", err)
		return
	}
	r.State, r.Code, r.Error = hubproto.MigrationVerified, "", ""
	r.Sha256, r.Size, r.MTime = sha, size, mtime
}

// put moves one object and checks it: the code of what failed, "" when the store has it.
func (m *Mover) put(ctx context.Context, it Item) (sha string, size, mtime int64, code string, err error) {
	if it.Deliver != nil {
		return m.deliver(ctx, it)
	}
	fi, err := os.Lstat(it.Path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", 0, 0, CodeGone, fmt.Errorf("%s is not on the disk any more", it.Path)
	case err != nil:
		return "", 0, 0, CodeRead, err
	case !fi.Mode().IsRegular():
		// A link is not followed: what moves is the file the record names, not where a link leads now.
		return "", 0, 0, CodeRead, fmt.Errorf("%s is not a regular file", it.Path)
	case fi.Size() > m.o.MaxBytes:
		return "", 0, 0, CodeTooBig, fmt.Errorf("bigger than the store takes (%d MiB)", m.o.MaxBytes>>20)
	}
	data, err := readSame(it.Path, fi, m.o.MaxBytes)
	if err != nil {
		return "", 0, 0, CodeRead, err
	}
	sum := sha256.Sum256(data)
	sha = hex.EncodeToString(sum[:])
	if it.Sha256 != "" && it.Sha256 != sha {
		return "", 0, 0, CodeHash, errors.New("the bytes on the disk are not the ones the machine recorded")
	}
	size, mtime = int64(len(data)), fi.ModTime().UnixMilli()
	has, err := m.o.Target.HasBlob(ctx, it.Kind, sha)
	if err != nil {
		return "", 0, 0, storeCode(err, CodePut), err
	}
	if has {
		return sha, size, mtime, "", nil
	}
	if err := m.o.Target.PutBlob(ctx, it.Kind, sha, data); err != nil {
		return "", 0, 0, storeCode(err, CodePut), err
	}
	has, err = m.o.Target.HasBlob(ctx, it.Kind, sha)
	if err != nil {
		return "", 0, 0, storeCode(err, CodeVerify), err
	}
	if !has {
		return "", 0, 0, CodeVerify, errors.New("the store answered the upload and then did not have the blob")
	}
	return sha, size, mtime, "", nil
}

func sumOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// deliver hands an object that is no file to the hub.
func (m *Mover) deliver(ctx context.Context, it Item) (sha string, size, mtime int64, code string, err error) {
	if int64(len(it.Data)) > m.o.MaxBytes {
		return "", 0, 0, CodeTooBig, fmt.Errorf("bigger than the hub takes (%d MiB)", m.o.MaxBytes>>20)
	}
	switch err := it.Deliver(ctx, it.Data); {
	case errors.Is(err, ErrNotKept):
		return "", 0, 0, CodeNotKept, err
	case err != nil:
		return "", 0, 0, storeCode(err, CodePut), err
	}
	return sumOf(it.Data), int64(len(it.Data)), 0, "", nil
}

// readSame reads the file Lstat saw, and only it: a file swapped for a link (or another file) between the look and the
// open is refused.
func readSame(path string, seen fs.FileInfo, max int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(fi, seen) || !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s changed while it was opened", path)
	}
	return io.ReadAll(io.LimitReader(f, max+1))
}

func storeCode(err error, otherwise string) string {
	if errors.Is(err, ErrUnauthorized) {
		return CodeSession
	}
	return otherwise
}

func (m *Mover) dirty() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.st.Records {
		if r.Dirty {
			n++
		}
	}
	return n
}

// finish marks the move over when every object is verified or skipped.
func (m *Mover) finish() {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.statusLocked()
	now := m.o.Now().UnixMilli()
	m.st.ChangedAt = now
	if st.MigrationDone() {
		if m.st.FinishedAt == 0 {
			m.st.FinishedAt = now
			m.o.Log.Info("move: every object is in the hub", "objects", st.Total, "skipped", st.Skipped)
		}
		if m.st.VerifiedIn == "" {
			m.st.VerifiedIn = m.o.Version
		}
		return
	}
	m.st.FinishedAt = 0
}

// report sends what changed to the hub, in batches, and takes what it asks back.
func (m *Mover) report(ctx context.Context) {
	if m.o.Journal == nil {
		return
	}
	for ctx.Err() == nil {
		m.mu.Lock()
		rep := hubproto.MigrationReport{Migration: ID, Version: m.o.Version, Total: int64(len(m.st.Records))}
		var sent []*record
		for _, r := range m.sorted() {
			if !r.Dirty {
				continue
			}
			rep.Objects = append(rep.Objects, hubproto.MigrationObject{Kind: r.Kind, Object: r.Object, Sha256: r.Sha256, Size: r.Size,
				Mime: r.Mime, Name: r.Name, State: r.State, Code: r.Code, Error: r.Error, Attempts: r.Attempts})
			sent = append(sent, r)
			if len(rep.Objects) == hubproto.MigrationReportMax {
				break
			}
		}
		m.mu.Unlock()
		ans, err := m.o.Journal.Report(ctx, rep)
		m.mu.Lock()
		switch {
		case errors.Is(err, ErrNoJournal):
			m.hub = "the hub keeps no journal (an older version): the move is kept on this machine only"
			m.mu.Unlock()
			return
		case err != nil:
			m.hub = "the hub did not take the report: " + err.Error()
			m.mu.Unlock()
			m.o.Log.Warn("move: the hub did not take the report, it is sent again later", "err", err)
			return
		}
		m.hub = "on"
		for _, r := range sent {
			r.Dirty = false
		}
		m.apply(ans)
		more := len(sent) == hubproto.MigrationReportMax
		m.mu.Unlock()
		if !more {
			return
		}
	}
}

// apply takes the hub's asks: retry (a person's «Повторить») and skip.
func (m *Mover) apply(ans hubproto.MigrationAnswer) {
	for _, k := range ans.Retry {
		if r := m.st.Records[key(k.Kind, k.Object)]; r != nil && r.State == hubproto.MigrationFailed && !r.Retry {
			r.Retry = true
			m.Wake()
		}
	}
	for _, k := range ans.Skip {
		if r := m.st.Records[key(k.Kind, k.Object)]; r != nil && r.State != hubproto.MigrationVerified && r.State != hubproto.MigrationSkipped {
			r.State, r.Retry = hubproto.MigrationSkipped, false
		}
	}
}

func (m *Mover) sorted() []*record {
	out := make([]*record, 0, len(m.st.Records))
	for _, r := range m.st.Records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return key(out[i].Kind, out[i].Object) < key(out[j].Kind, out[j].Object) })
	return out
}

func (m *Mover) save() {
	m.mu.Lock()
	raw, err := json.Marshal(m.st)
	m.mu.Unlock()
	if err != nil || m.o.StateFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.o.StateFile), 0o700); err != nil {
		m.o.Log.Warn("move: the journal is not kept on the disk", "err", err)
		return
	}
	tmp := m.o.StateFile + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err == nil {
		err = os.Rename(tmp, m.o.StateFile)
		if err != nil {
			m.o.Log.Warn("move: the journal is not kept on the disk", "err", err)
		}
	}
}
