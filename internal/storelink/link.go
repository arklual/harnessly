// Package storelink is a runtime's connection to the store (internal/filestore, docs/hub/store.md): the hub's when
// the machine belongs to a hub that keeps one, otherwise a store inside this process, SQLite and a folder in the data
// folder. The model is the same either way: revisions in a database, contents by sha256, the folders on the machine
// a working copy. A store inside the process is the one a hub connected later takes over (docs/hub/store.md).
package storelink

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
)

// Modes of RUNTIME_STORE.
const (
	// ModeAuto: the hub's store for a hub's machine whose hub has one; none for a runtime paired with a cluster's
	// agent (that cluster keeps its own data); a store inside the process for a runtime on its own.
	ModeAuto = "auto"
	// ModeHub: only the hub's store; a machine that is not a hub's, or a hub without one, syncs nothing.
	ModeHub = "hub"
	// ModeEmbedded: the store inside the process, whatever else there is.
	ModeEmbedded = "embedded"
	// ModeOff: nothing syncs; the runtime keeps its data as it always did.
	ModeOff = "off"
)

// Kind of link.
const (
	KindHub      = "hub"
	KindEmbedded = "embedded"
)

// Options is what a runtime knows about where it runs.
type Options struct {
	DataDir string
	Mode    string
	// Machine is the hub's machine this runtime runs on; nil for none.
	Machine *Machine
	// Paired: the runtime is one end of a pair with a cluster's agent (AGENT_SECRET_FILE, AGENT_CLUSTER_URL) and not a
	// hub's machine.
	Paired bool
	// Name is the machine's name in what a lost write leaves behind.
	Name string
	// Shared names folders several runtimes of the machine read (a team's library): one runtime syncs such a folder at
	// a time, and a file the store stops showing this machine stays there for the others.
	Shared func(dir string) bool
	// HTTP is the client to the hub; nil for a plain one.
	HTTP *http.Client
	Log  *slog.Logger
	Now  func() time.Time
}

// Link is where the runtime's collections live.
type Link struct {
	kind  string
	base  string
	token filesync.Token
	hc    *http.Client
	name  string
	// stateKey names the store in the sync's state folder: a switch to another store starts a state of its own.
	stateKey string
	dataDir  string
	log      *slog.Logger

	machine  *Machine
	sess     *sessions
	embedded *filestore.Server
	shared   func(dir string) bool
	told     sync.Map
}

// Decide is the kind of link the options call for, "" for none, and why.
func Decide(o Options) (string, string) {
	switch o.Mode {
	case ModeOff:
		return "", "RUNTIME_STORE=off"
	case ModeEmbedded:
		return KindEmbedded, "RUNTIME_STORE=embedded"
	case ModeHub:
		if o.Machine == nil {
			return "", "RUNTIME_STORE=hub and this machine is not a hub's"
		}
		return KindHub, "RUNTIME_STORE=hub"
	}
	switch {
	case o.Machine != nil:
		return KindHub, "a hub's machine"
	case o.Paired:
		return "", "paired with a cluster's agent, which keeps the data"
	}
	return KindEmbedded, "a runtime on its own: the store runs inside it"
}

// Open opens the link the options call for; nil (and no error) when there is none.
func Open(o Options) (*Link, error) {
	kind, why := Decide(o)
	log := o.Log
	if log == nil {
		log = slog.Default()
	}
	log = log.With("module", "store")
	if kind == "" {
		log.Info("store: none, the data stays as it is", "why", why)
		return nil, nil
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	name := o.Name
	if name == "" {
		name, _ = os.Hostname()
	}
	l := &Link{kind: kind, name: sanitize(name), dataDir: o.DataDir, log: log, hc: hc, shared: o.Shared}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	switch kind {
	case KindHub:
		base, err := origin(o.Machine.DialURL)
		if err != nil {
			return nil, err
		}
		l.machine = o.Machine
		l.base = base + "/store"
		s := &sessions{hc: hc, machine: *o.Machine, now: now, log: log}
		l.sess = s
		l.token = s.token
		l.stateKey = "hub-" + short(base+"\x00"+o.Machine.ID)
	case KindEmbedded:
		srv, err := filestore.OpenFolder(filepath.Join(o.DataDir, "store"), func(r *http.Request, coll string) (filestore.Access, error) {
			return filestore.Access{Member: l.name, Room: coll}, nil
		})
		if err != nil {
			return nil, fmt.Errorf("store inside the runtime: %w", err)
		}
		l.embedded = srv
		l.base = "http://store.invalid"
		l.hc = &http.Client{Transport: Handler(srv.Handler())}
		l.stateKey = KindEmbedded
	}
	log.Info("store: linked", "kind", kind, "why", why)
	return l, nil
}

// Person is the name the hub gives this machine's person in the apps collection (hubproto.StoreSession.Person); ""
// for a store inside the runtime or a hub that names nobody.
func (l *Link) Person(ctx context.Context) (string, error) {
	if l.sess == nil {
		return "", nil
	}
	if _, err := l.sess.token(ctx, false); err != nil {
		return "", err
	}
	l.sess.mu.Lock()
	defer l.sess.mu.Unlock()
	return l.sess.cur.Person, nil
}

// MachineID is the hub's id of this machine, "" for a store inside the process.
func (l *Link) MachineID() string {
	if l.machine == nil {
		return ""
	}
	return l.machine.ID
}

// Serves says whether the hub opens a collection to this machine (hubproto.StoreSession.Collections): an older hub
// does not know the newer kinds. A store inside the process serves every kind it knows.
func (l *Link) Serves(ctx context.Context, collection string) (bool, error) {
	if l.sess == nil {
		return filestore.KnownKind(collection), nil
	}
	if _, err := l.sess.token(ctx, false); err != nil {
		return false, err
	}
	l.sess.mu.Lock()
	defer l.sess.mu.Unlock()
	return slices.Contains(l.sess.cur.Collections, collection), nil
}

// Kind is KindHub or KindEmbedded.
func (l *Link) Kind() string { return l.kind }

// Embedded is the store inside the process, nil for the hub's (backup and the move into a hub reach it here).
func (l *Link) Embedded() *filestore.Server { return l.embedded }

// Client is a client of one of the link's collections.
func (l *Link) Client(collection string) *filesync.Client {
	return filesync.NewClient(l.base, collection, l.token, l.hc)
}

// Close closes the store inside the process.
func (l *Link) Close() {
	if l != nil && l.embedded != nil {
		l.embedded.Close()
	}
}

// Ready waits until the store answers that it is there: at once for the store inside the process; for the hub's, once
// its capabilities list the store. A hub without one is asked again every recheck, since it may be upgraded.
func (l *Link) Ready(ctx context.Context, recheck time.Duration) error {
	if l.kind != KindHub {
		return nil
	}
	base := strings.TrimSuffix(l.base, "/store")
	wait := 5 * time.Second
	told := false
	for {
		err := HubHasStore(ctx, l.hc, base)
		if err == nil {
			// The store is there; a machine that is not in a space of the hub has no session to use it with. It waits
			// for the connection (the session's own pauses) instead of every loop failing on its own timer.
			for l.sess != nil {
				if _, serr := l.sess.token(ctx, false); !errors.Is(serr, ErrNotConnected) {
					return nil
				}
				if !sleep(ctx, max(l.sess.retryIn(), time.Second)) {
					return ctx.Err()
				}
			}
			return nil
		}
		if errors.Is(err, errNoStore) {
			if !told {
				l.log.Info("store: the hub has none yet, the data stays as it is", "hub", base)
				told = true
			}
			if !sleep(ctx, recheck) {
				return ctx.Err()
			}
			continue
		}
		l.log.Warn("store: the hub does not answer", "hub", base, "err", err)
		if !sleep(ctx, wait) {
			return ctx.Err()
		}
		wait = min(wait*2, 5*time.Minute)
	}
}

// SyncFolder keeps the folder root() names in step with a collection until ctx ends. The folder is asked again every
// minute: a library that moved (another workspace) is synced from where it is now, with a state of its own.
func (l *Link) SyncFolder(ctx context.Context, collection string, root func() string) {
	if err := l.Ready(ctx, 10*time.Minute); err != nil {
		return
	}
	for ctx.Err() == nil {
		dir := root()
		if dir == "" {
			if !sleep(ctx, time.Minute) {
				return
			}
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			l.log.Warn("store: the folder cannot be made", "dir", dir, "err", err)
			if !sleep(ctx, time.Minute) {
				return
			}
			continue
		}
		if !l.follow(ctx, collection, root, dir) {
			return
		}
	}
}

// follow syncs dir until ctx ends (false), the sync stops with an error, or root() names another folder (true: go on
// with it).
func (l *Link) follow(ctx context.Context, collection string, root func() string, dir string) bool {
	run, stop := context.WithCancel(ctx)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- l.syncOnce(run, collection, dir) }()
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if err := <-done; err != nil {
				l.log.Debug("store: sync stopped", "dir", dir, "err", err)
			}
			return false
		case err := <-done:
			if errors.Is(err, errBusy) {
				// Another runtime of this machine syncs the folder; it is asked again in a while.
				return sleep(ctx, time.Minute)
			}
			if err != nil && ctx.Err() == nil {
				l.log.Warn("store: sync stopped, starting again", "dir", dir, "err", err)
				return sleep(ctx, 10*time.Second)
			}
			return ctx.Err() == nil
		case <-tick.C:
			if now := root(); now != dir {
				l.log.Info("store: the folder moved", "from", dir, "to", now)
				stop()
				if err := <-done; err != nil {
					l.log.Debug("store: sync stopped", "dir", dir, "err", err)
				}
				return true
			}
		}
	}
}

// lockName is the folder's sync lock (hidden: it never syncs).
const lockName = ".store-sync.lock"

// errBusy: another runtime of this machine syncs the folder.
var errBusy = errors.New("another runtime of this machine syncs the folder")

func (l *Link) syncOnce(ctx context.Context, collection, dir string) error {
	// A library several people's runtimes share (a team machine's /srv) is synced by each of them, every one with its
	// person's projects: the folder holds what any of them has a role in, and nothing is removed from it. The lock then
	// only keeps one runtime from syncing it twice. Any other folder is synced by one runtime of the machine at a time.
	shared := l.shared != nil && l.shared(dir)
	name := lockName
	if shared {
		name = ".store-sync-" + short(l.stateKey) + ".lock"
	}
	unlock, err := lockFolder(dir, name)
	if err != nil {
		l.busyOnce(dir)
		return err
	}
	defer unlock()
	state := filepath.Join(l.dataDir, "store-sync", l.stateKey, collection+"-"+short(dir))
	cfg := filesync.Config{Root: dir, StateDir: state, Store: l.Client(collection), Machine: l.name,
		Policy: filestore.PolicyFor(collection), Log: l.log, KeepUnseen: shared}
	if collection == filestore.KindDesign {
		cfg.RefusalGroup = designRefusalGroup
	}
	if collection == filestore.KindKits {
		// A kit version is not a board: a local file that lost to the room's goes aside, not into the library's
		// by-revision notices.
		cfg.ConflictDir = ".conflicts"
	}
	a, err := filesync.New(cfg)
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Run(ctx)
}

// designRefusalGroup names the paths of the design library the store refuses together: a write is judged by the
// role in its project (the first folder), so one refused file of a project speaks for the rest of it. The owner file
// is judged on its own, and so is what lies outside a project; a folder whose name is not plain ASCII takes no
// write at all.
func designRefusalGroup(rel string) string {
	id, rest, ok := strings.Cut(rel, "/")
	if !ok || rest == "" {
		return ""
	}
	for i := 0; i < len(id); i++ {
		if id[i] >= 0x80 {
			return "folder:" + id
		}
	}
	if strings.HasPrefix(id, ".") || strings.EqualFold(rest, filestore.OwnerFile) {
		return ""
	}
	return "project:" + strings.ToLower(id)
}

// busyOnce says once per folder that another runtime syncs it.
func (l *Link) busyOnce(dir string) {
	if _, told := l.told.LoadOrStore(dir, true); !told {
		l.log.Info("store: another runtime of this machine syncs the folder", "dir", dir)
	}
}

func short(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:6])
}

// sanitize keeps a machine's name to what fits a file name.
func sanitize(name string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		switch {
		case c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_':
			b.WriteRune(c)
		case b.Len() > 0:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out = strings.Trim(out, "-"); out == "" {
		return "machine"
	}
	return out
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// Handler is a transport that answers every request with h inside the process: the store inside the runtime is
// spoken to over the same API as the hub's, without a socket.
func Handler(h http.Handler) http.RoundTripper { return handlerTransport{h} }

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// A pipe, not a recorder: a long poll holds its answer, and the caller must be able to give up on it.
	pr, pw := io.Pipe()
	rw := &pipeWriter{header: http.Header{}, pw: pw, ready: make(chan struct{})}
	in := req.Clone(req.Context())
	if in.Body == nil {
		in.Body = http.NoBody
	}
	in.RequestURI = req.URL.RequestURI()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				_ = pw.CloseWithError(fmt.Errorf("store handler panicked: %v", p))
			}
		}()
		t.h.ServeHTTP(rw, in)
		rw.WriteHeader(http.StatusOK)
		_ = pw.Close()
	}()
	select {
	case <-rw.ready:
	case <-req.Context().Done():
		_ = pr.CloseWithError(req.Context().Err())
		return nil, req.Context().Err()
	}
	res := &http.Response{StatusCode: rw.status, Status: http.StatusText(rw.status), Header: rw.header, Body: pr,
		Request: req, Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, ContentLength: -1}
	if req.Method == http.MethodHead {
		res.Body = http.NoBody
		go func() { _, _ = io.Copy(io.Discard, pr) }()
	}
	return res, nil
}

type pipeWriter struct {
	header http.Header
	pw     *io.PipeWriter
	status int
	ready  chan struct{}
}

func (w *pipeWriter) Header() http.Header { return w.header }

func (w *pipeWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.header = w.header.Clone()
	close(w.ready)
}

func (w *pipeWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	return w.pw.Write(p)
}
