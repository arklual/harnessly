package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/delivery"
	"github.com/ly-solutions/harnessly/internal/filestore"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// storeOptions are what the runtime knows of where its store is (RUNTIME_STORE, docs/hub/store.md): a hub's machine
// syncs with the hub's store, a runtime paired with a cluster's agent leaves its data as it is, a runtime on its own
// keeps a store inside itself.
func storeOptions(cfg gate.Config) (storelink.Options, error) {
	o := storelink.Options{DataDir: cfg.DataDir, Mode: strings.ToLower(env("RUNTIME_STORE", storelink.ModeAuto)),
		Paired: os.Getenv("AGENT_SECRET_FILE") != "" || os.Getenv("AGENT_CLUSTER_URL") != "", Name: os.Getenv("RUNTIME_STORE_MACHINE")}
	switch o.Mode {
	case storelink.ModeAuto, storelink.ModeHub, storelink.ModeEmbedded, storelink.ModeOff:
	default:
		return o, errors.New("RUNTIME_STORE must be auto, hub, embedded or off")
	}
	m, err := agent.LoadMachine(env("AGENT_MACHINE_FILE", agent.DefaultMachineFile(cfg.DataDir)))
	switch {
	case err == nil:
		o.Machine = &storelink.Machine{ID: m.MachineID, DialURL: m.DialURL, Key: m.SigningKey()}
	case !errors.Is(err, os.ErrNotExist):
		return o, err
	}
	return o, nil
}

// startStore links the runtime to its store and keeps the Sinopia library in step with it until ctx ends. A store
// that cannot be opened is said in the log and leaves the runtime as it was: the library stays a folder.
func (f *runtimeGate) startStore(ctx context.Context, cfg gate.Config) func() {
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
	if f.store == nil || f.library == nil {
		return func() {}
	}
	run, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.store.SyncFolder(run, filestore.KindDesign, f.library)
	}()
	return func() {
		stop()
		<-done
		f.store.Close()
	}
}

// linked is the runtime's store link for what is put into it outside a route's own deps (the files the agent hands over).
var linked atomic.Pointer[storelink.Link]

// deliveredMax is the largest handed-over file put into the hub's store; a bigger one is read from the machine only.
const deliveredMax = 64 << 20

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
