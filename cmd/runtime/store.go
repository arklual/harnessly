package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/apps"
	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/design"
	"github.com/ly-solutions/harnessly/internal/designkit"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/filesync"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// storeOptions are what the runtime knows of where its store is (RUNTIME_STORE, docs/hub/store.md): a hub's machine
// syncs with the hub's store, a runtime paired with a cluster's agent leaves its data as it is, a runtime on its own
// keeps a store inside itself.
func storeOptions(cfg gate.Config) (storelink.Options, error) {
	o := storelink.Options{DataDir: cfg.DataDir, Mode: strings.ToLower(env("RUNTIME_STORE", storelink.ModeAuto)),
		Paired: os.Getenv("AGENT_SECRET_FILE") != "" || os.Getenv("AGENT_CLUSTER_URL") != "", Name: os.Getenv("RUNTIME_STORE_MACHINE"),
		Shared: sharedLibrary}
	switch o.Mode {
	case storelink.ModeAuto, storelink.ModeHub, storelink.ModeEmbedded, storelink.ModeOff:
	default:
		return o, errors.New("RUNTIME_STORE must be auto, hub, embedded or off")
	}
	for _, file := range machineFiles(cfg.DataDir) {
		m, err := agent.LoadMachine(file)
		switch {
		case err == nil:
			o.Machine = &storelink.Machine{ID: m.MachineID, DialURL: m.DialURL, Key: m.SigningKey()}
			return o, nil
		case !errors.Is(err, os.ErrNotExist):
			return o, err
		}
	}
	return o, nil
}

// machineFiles are where the runtime's hub machine may be named, in order: its own machine.json (a hub's machine), then
// the observe link of an agent that also serves a cluster (AGENT_HUB_OBSERVE_FILE, `<data>/hub/machine.json`): such
// a runtime is a hub's machine as well, and its library syncs with that hub's store, not a store of its own.
func machineFiles(dataDir string) []string {
	if f := os.Getenv("AGENT_MACHINE_FILE"); f != "" {
		return []string{f}
	}
	files := []string{agent.DefaultMachineFile(dataDir)}
	if f := os.Getenv("AGENT_HUB_OBSERVE_FILE"); f != "" {
		files = append(files, f)
	}
	return append(files, filepath.Join(dataDir, "hub", "machine.json"))
}

// sharedLibrary tells whether a folder is the team's library several runtimes of the machine read
// (RUNTIME_DESIGN_SHARED_DIR) rather than this runtime's own.
func sharedLibrary(dir string) bool {
	shared := os.Getenv("RUNTIME_DESIGN_SHARED_DIR")
	if shared == "" {
		return false
	}
	a, errA := filepath.EvalSymlinks(dir)
	b, errB := filepath.EvalSymlinks(shared)
	if errA != nil || errB != nil {
		return filepath.Clean(dir) == filepath.Clean(shared)
	}
	return a == b
}

// startStore links the runtime to its store and keeps the Sinopia library in step with it until ctx ends. A store
// that cannot be opened is said in the log and leaves the runtime as it was: the library stays a folder.
func (f *runtimeGate) startStore(ctx context.Context, cfg gate.Config) func() {
	// Versions installed before kits had seals are sealed once, here, before anything syncs: from now on a version without a
	// seal is one still arriving from another machine.
	if f.kits != nil {
		if n, err := designkit.SealLegacy(f.kits()); err != nil {
			agent.Log("warn", "kits: the installed versions were not sealed", agent.F{K: "error", V: err.Error()})
		} else if n > 0 {
			agent.Log("info", "kits: installed versions sealed", agent.F{K: "count", V: n})
		}
	}
	o, err := storeOptions(cfg)
	if err == nil {
		o.Log = slog.Default()
		f.store, err = storelink.Open(o)
	}
	if err != nil {
		agent.Log("warn", "store not linked, the data stays as it is", agent.F{K: "error", V: err.Error()})
		return func() {}
	}
	linked.Store(f.store)
	stopAppsCenter := startAppsCenter(ctx, f.store, cfg.DataDir)
	stopProfile := startProfileSync(ctx, f.store, cfg.DataDir)
	stopMove := f.startMove(ctx, f.store, cfg.DataDir)
	stopApps := func() {
		stopMove()
		stopProfile()
		stopAppsCenter()
	}
	if f.store == nil || f.library == nil || !librarySynced() {
		return stopApps
	}
	// Under a hub the store is the center: the library's history is its revisions, the machine on or off.
	if f.store.Kind() == storelink.KindHub && f.revisions != nil {
		f.revisions(storeRevisions{c: f.store.Client(filestore.KindDesign)})
	}
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.store.SyncFolder(run, filestore.KindDesign, f.library)
	}()
	kitsDone := make(chan struct{})
	go func() {
		defer close(kitsDone)
		if f.kits != nil {
			f.store.SyncFolder(run, filestore.KindKits, f.kits)
		}
	}()
	return func() {
		stop()
		<-done
		<-kitsDone
		stopApps()
		f.store.Close()
	}
}

// startAppsCenter keeps the apps' user, space, chat and doc areas and the team's apps in the hub's store:
// the same on every machine of the space and there with this one off. Only under a hub; on a machine of its own they
// stay on its disk.
func startAppsCenter(ctx context.Context, l *storelink.Link, dataDir string) func() {
	if l == nil || l.Kind() != storelink.KindHub {
		return func() {}
	}
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		var person string
		for {
			p, err := l.Person(run)
			if err == nil {
				person = p
				break
			}
			// A machine that is not in a space yet is told once, by the store link; the apps wait for the connection.
			if !errors.Is(err, storelink.ErrNotConnected) {
				agent.Log("warn", "apps: no store session yet, the storage stays on the machine", agent.F{K: "error", V: err.Error()})
			}
			select {
			case <-run.Done():
				return
			case <-time.After(30 * time.Second):
			}
		}
		apps.OpenStorage(dataDir).RunCenter(run, apps.HubLink{Client: l.Client(filestore.KindApps), Person: person,
			Registry: apps.Open(dataDir), Host: env("AGENT_VERSION", agent.BuildVersion), Log: slog.Default()})
	}()
	return func() {
		stop()
		<-done
	}
}

// librarySynced: the Sinopia library syncs with the store unless RUNTIME_STORE_LIBRARY=off, which a machine whose
// library another sync still keeps (the design-sync agent of a team's /srv) sets until that one is retired: two syncs of
// one folder never run side by side. The store stays linked for the rest (the files the agent hands over).
func librarySynced() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("RUNTIME_STORE_LIBRARY"))) {
	case "off", "false", "0", "no":
		return false
	}
	return true
}

// linked is the runtime's store link for what is put into it outside a route's own deps (the files the agent hands over).
var linked atomic.Pointer[storelink.Link]

// deliveredMax is the largest handed-over file put into the hub's store; a bigger one is read from the machine only.
const deliveredMax = filestore.MaxFileSize

// keepDelivered puts a file the agent handed over into the `files` collection of the hub's store by its sha256
// (docs/hub/history.md): the chat's card then reads it from the hub, the machine on or off. Only under a hub,
// in the background and best effort; bytes that no longer hash to what the card says are left alone.
func keepDelivered(info delivery.Info) {
	l := linked.Load()
	if l == nil || l.Kind() != storelink.KindHub || len(info.SHA256) != 64 || info.Size <= 0 || info.Size > deliveredMax {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		c := l.Client(filestore.KindFiles)
		if ok, err := c.HasBlob(ctx, info.SHA256); err == nil && ok {
			return
		}
		data, err := os.ReadFile(info.Path)
		if err != nil || int64(len(data)) > deliveredMax {
			return
		}
		if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != info.SHA256 {
			return
		}
		if err := c.PutBlob(ctx, info.SHA256, data); err != nil {
			agent.Log("info", "a handed-over file is not in the hub's store", agent.F{K: "error", V: err.Error()})
		}
	}()
}

// storeRevisions is the design collection's history in the hub's store as the Sinopia library reads it.
type storeRevisions struct{ c *filesync.Client }

func revisionsOf(es []filestore.Entry) []design.Revision {
	out := make([]design.Revision, 0, len(es))
	for _, e := range es {
		out = append(out, design.Revision{Rev: e.Rev, Path: e.Path, Sha: e.Sha, Author: e.Author, Machine: e.Machine, At: e.At, Deleted: e.Deleted})
	}
	return out
}

func (s storeRevisions) History(ctx context.Context, path string) ([]design.Revision, error) {
	es, err := s.c.History(ctx, path)
	return revisionsOf(es), err
}

func (s storeRevisions) HistoryUnder(ctx context.Context, prefix string, limit int) ([]design.Revision, error) {
	es, err := s.c.HistoryUnder(ctx, prefix, limit)
	return revisionsOf(es), err
}

func (s storeRevisions) Snapshot(ctx context.Context, prefix string, at int64) ([]design.Revision, error) {
	snap, err := s.c.Snapshot(ctx, prefix, at)
	return revisionsOf(snap.Entries), err
}

// TakeLease is the store's short claim of a task one machine of the space does for all (design.RevisionLeases).
func (s storeRevisions) TakeLease(ctx context.Context, key, chat string) (bool, error) {
	out, err := s.c.TakeLease(ctx, filestore.LeaseRequest{Key: key, Chat: chat})
	if err != nil {
		return false, err
	}
	return !out.Held, nil
}

// revisionMax is the largest board read back from the history.
const revisionMax = 64 << 20

func (s storeRevisions) Read(ctx context.Context, sha string) ([]byte, error) {
	var buf limitedBuffer
	if err := s.c.GetBlob(ctx, sha, &buf); err != nil {
		return nil, err
	}
	return buf.b, nil
}

type limitedBuffer struct{ b []byte }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if len(l.b)+len(p) > revisionMax {
		return 0, errors.New("a version too big to read back")
	}
	l.b = append(l.b, p...)
	return len(p), nil
}
