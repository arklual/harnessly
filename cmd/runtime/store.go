package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/ly-solutions/harnessly/internal/agent"
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
