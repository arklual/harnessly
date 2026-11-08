package main

import (
	"encoding/base64"
	"testing"

	"github.com/ly-solutions/harnessly/internal/agent"
	"github.com/ly-solutions/harnessly/internal/gate"
	"github.com/ly-solutions/harnessly/internal/hubproto"
	"github.com/ly-solutions/harnessly/internal/storelink"
)

// RUNTIME_STORE picks the store from where the runtime runs: a hub's machine (machine.json in the data folder) syncs
// with the hub, a runtime paired with a cluster's agent keeps its data as it is, one on its own keeps a store inside.
func TestStoreOptions(t *testing.T) {
	for _, k := range []string{"RUNTIME_STORE", "AGENT_SECRET_FILE", "AGENT_CLUSTER_URL", "AGENT_MACHINE_FILE", "RUNTIME_STORE_MACHINE"} {
		t.Setenv(k, "")
	}
	alone, hubs := gate.Config{DataDir: t.TempDir()}, gate.Config{DataDir: t.TempDir()}
	seed := base64.StdEncoding.EncodeToString(make([]byte, 32))
	m := &agent.MachineFile{MachineID: "m-1", HubURL: "https://hub.example.org", DialURL: "wss://hub.example.org/agent/v1",
		RuntimeBasePath: "/m/m-1", Profile: hubproto.ProfileDir, Ed25519: seed, X25519: seed}
	if err := m.Save(agent.DefaultMachineFile(hubs.DataDir)); err != nil {
		t.Fatal(err)
	}
	kind := func(cfg gate.Config) string {
		t.Helper()
		o, err := storeOptions(cfg)
		if err != nil {
			t.Fatal(err)
		}
		k, _ := storelink.Decide(o)
		return k
	}
	if k := kind(alone); k != storelink.KindEmbedded {
		t.Errorf("on its own: %q", k)
	}
	if k := kind(hubs); k != storelink.KindHub {
		t.Errorf("a hub's machine: %q", k)
	}
	if o, _ := storeOptions(hubs); o.Machine == nil || o.Machine.ID != "m-1" || len(o.Machine.Key) == 0 {
		t.Errorf("the machine: %+v", o.Machine)
	}
	t.Setenv("AGENT_SECRET_FILE", "/run/agent.secret")
	if k := kind(alone); k != "" {
		t.Errorf("paired with a cluster's agent: %q", k)
	}
	t.Setenv("RUNTIME_STORE", "embedded")
	if k := kind(alone); k != storelink.KindEmbedded {
		t.Errorf("RUNTIME_STORE=embedded: %q", k)
	}
	t.Setenv("RUNTIME_STORE", "Off")
	if k := kind(hubs); k != "" {
		t.Errorf("RUNTIME_STORE=off: %q", k)
	}
	t.Setenv("RUNTIME_STORE", "sometimes")
	if _, err := storeOptions(alone); err == nil {
		t.Error("a bad RUNTIME_STORE was taken")
	}
}
