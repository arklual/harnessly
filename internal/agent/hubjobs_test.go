package agent

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/agents/agentstest"
	"github.com/ly-solutions/harnessly/internal/builders"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

// startBuilderAgent runs a real agent as the machine m of the stack s.
func startBuilderAgent(t *testing.T, s *hubtest.Stack, m *hubtest.Agent) *Agent {
	t.Helper()
	rig := agentstest.New(t)
	rt := newFakeRuntimeWith(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	dataDir := filepath.Join(rig.Home, ".harnessly")
	writeFile(t, filepath.Join(dataDir, "secret"), hubTestSecret)
	base := "/m/" + m.MachineID
	mf := hubMachine(s, m)
	mf.RuntimeBasePath, mf.Profile = base, hubproto.ProfileDir
	cfg := Config{
		Home: rig.Home, Host: "127.0.0.1", Port: port, Version: "9.9.9-test", ScanInterval: time.Hour,
		DataDir: dataDir, ConfigFile: filepath.Join(dataDir, "config.json"), SecretFile: filepath.Join(dataDir, "secret"),
		RuntimePort: rt.port, ListenAudience: "agent", DialAudience: "ingest",
		RuntimeAPI: base + "/api/agent", RuntimePlatformAPI: base + "/api/platform", StateChangedPath: base + "/api/agent/state-changed", StatusPath: base + "/api/status", SyncPath: base + "/api/sync", PeersPath: base + "/api/peers",
		RuntimeCookie: "session-token", LocalTokenHeader: "x-agent-local-token", ReportPortEnv: "AGENT_REPORT_PORT",
		Auth: AuthEd25519, Machine: mf, MachineFile: filepath.Join(dataDir, "machine.json"), PolicyFile: filepath.Join(rig.Home, "policy.toml"),
		EnvDir: rig.EnvDir, WorkDir: rig.Home,
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	a.hub.shell = rig.Checker
	a.hub.check = fakeProviderChecker(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = a.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			t.Error("the agent did not stop")
		}
	})
	return a
}

// A job from one real agent runs on another one that the hub made a builder: the input goes there, the command runs in a
// folder of its own, the result file comes back to the submitter, and the status shows the builder that was chosen. With the
// builder off the same call falls back to the submitter's own slots.
func TestJobRunsOnABuilderAndTheResultComesBack(t *testing.T) {
	t.Setenv(jobsOptIn, "on")
	t.Setenv(jobsToolsVar, "sh")
	t.Setenv(jobsCapsVar, "blender")
	t.Setenv("HARNESSLY_HEAVY_DIR", t.TempDir())
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Render")
	home := owner.Machine(spaceID, nil)
	build := owner.Machine(spaceID, nil)
	home.Close()
	build.Close()

	startBuilderAgent(t, s, build)
	ha := startBuilderAgent(t, s, home) // the submitter: a real agent too, its loopback /jobs is the client's way in
	var client *builders.Client
	var err error
	s.Eventually("the agent's loopback is up", func() bool {
		client, err = builders.NewLocal(ha.cfg.DataDir, "x-agent-local-token")
		return err == nil
	})
	builders.PollEvery = 50 * time.Millisecond

	s.Eventually("both agents linked", func() bool {
		return s.Hub.Registry.Get(build.MachineID) != nil && s.Hub.Registry.Get(home.MachineID) != nil
	})
	if r := owner.Do("PUT", "/api/machines/"+build.MachineID+"/builder", map[string]any{"role": "builder"}); r.Code != 200 {
		t.Fatalf("role: %s", r)
	}
	var list []builders.Builder
	s.Eventually("the builder is ready with the labels its agent detected", func() bool {
		list, err = client.Builders(context.Background())
		return err == nil && len(list) == 1 && list[0].Ready && strings.Contains(strings.Join(list[0].Caps, ","), "blender")
	})
	go func() { // the hub's scheduler (HUB loops are not started by the test stack)
		for ctx := context.Background(); ; time.Sleep(100 * time.Millisecond) {
			if s.T.Context().Err() != nil {
				return
			}
			s.Hub.Jobs.Tick(ctx)
		}
	}()

	in := t.TempDir()
	_ = os.WriteFile(filepath.Join(in, "scene.txt"), []byte("cube"), 0o644)
	var log strings.Builder
	res, err := client.Run(context.Background(), builders.Spec{Title: "draft", Caps: []string{"blender"}, Builder: build.MachineID,
		Argv: []string{"sh", "-c", "mkdir -p out && tr a-z A-Z < scene.txt > out/frame.txt && echo rendered"}, InDir: in, Outputs: []string{"out/*.txt"}, Log: &log})
	if err != nil {
		t.Fatalf("%v\n%s", err, log.String())
	}
	if res.Job.Local || res.Job.Builder == nil || res.Job.Builder.ID != build.MachineID || res.Job.Choice.Mode != "builder" || len(res.Files) != 1 {
		t.Fatalf("%+v\n%s", res.Job, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(in, "out", "frame.txt")); string(b) != "CUBE" {
		t.Fatalf("the result file: %q", b)
	}
	if !strings.Contains(res.Job.Log, "rendered") {
		t.Fatalf("log %q", res.Job.Log)
	}

	// a command the machine does not run is refused where it matters: on the builder, with the reason
	_, err = client.Run(context.Background(), builders.Spec{Caps: []string{"blender"}, Argv: []string{"rm", "-rf", "x"}, Log: &log})
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("an unlisted tool ran: %v", err)
	}

	// a failing command keeps its exit code
	_, err = client.Run(context.Background(), builders.Spec{Caps: []string{"blender"}, Argv: []string{"sh", "-c", "exit 7"}, Log: &log})
	if err == nil || !strings.Contains(err.Error(), "exit code 7") {
		t.Fatalf("%v", err)
	}

	// the machine goes back to chats: the pool is empty and the job runs here, with the reason
	if r := owner.Do("PUT", "/api/machines/"+build.MachineID+"/builder", map[string]any{"role": "agents"}); r.Code != 200 {
		t.Fatal(r)
	}
	log.Reset()
	res, err = client.Run(context.Background(), builders.Spec{Caps: []string{"blender"}, Argv: []string{"sh", "-c", "echo local-$HARNESSLY_JOB_LOCAL > l.txt"}, InDir: in, Outputs: []string{"l.txt"}, Log: &log})
	if err != nil || !res.Job.Local || !strings.Contains(log.String(), "no builders") {
		t.Fatalf("%v %+v\n%s", err, res.Job, log.String())
	}
	if b, _ := os.ReadFile(filepath.Join(in, "l.txt")); strings.TrimSpace(string(b)) != "local-1" {
		t.Fatalf("local run: %q", b)
	}
}
