package agent

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func TestObserveFileIsReadOnlyForTheClusterAgent(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".harnessly")
	observe := filepath.Join(data, "hub", "machine.json")
	t.Setenv("HOME", home)
	t.Setenv("AGENT_PORT", "1")
	t.Setenv("AGENT_DATA_DIR", data)
	t.Setenv("AGENT_AUTH", AuthHS256)
	t.Setenv("AGENT_RUNTIME_API", "/agents/api/agent")
	t.Setenv("AGENT_HUB_OBSERVE_FILE", observe)

	// The setting may come before the registration: no file, no link, no error.
	c, err := LoadConfig()
	if err != nil || c.ObserveMachine != nil || c.Machine != nil {
		t.Fatalf("before registering: %+v %v", c.ObserveMachine, err)
	}

	keys, err := newMachineKeys()
	if err != nil {
		t.Fatal(err)
	}
	m := keys.file(hubproto.RegisterResponse{MachineID: "abcdefghijklmnopqrst", DialURL: "wss://hub.example.com/agent/v1",
		RuntimeBasePath: "/m/abcdefghijklmnopqrst", HubURL: "https://hub.example.com"}, hubproto.ProfileFull, time.Now())
	if err := m.Save(observe); err != nil {
		t.Fatal(err)
	}
	c, err = LoadConfig()
	if err != nil || c.ObserveMachine == nil || c.Machine != nil {
		t.Fatalf("after registering: %+v %+v %v", c.ObserveMachine, c.Machine, err)
	}
	// The cluster's runtime routes keep their own base path, not the hub's.
	if c.RuntimeAPI != "/agents/api/agent" || c.StatusPath != "/api/status" {
		t.Errorf("runtime routes %q %q", c.RuntimeAPI, c.StatusPath)
	}

	// A machine that is a hub's already has no observe link besides.
	if err := m.Save(filepath.Join(data, "machine.json")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENT_AUTH", AuthAuto)
	c, err = LoadConfig()
	if err != nil || c.Machine == nil || c.ObserveMachine != nil {
		t.Fatalf("hub mode: %+v %+v %v", c.Machine, c.ObserveMachine, err)
	}
}
