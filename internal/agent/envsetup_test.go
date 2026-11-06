package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

func setupItems() []hubproto.EnvItem {
	return []hubproto.EnvItem{
		{ItemID: "s1", Kind: hubproto.EnvKindScript, Name: "Сборка", Order: 1, Source: json.RawMessage(`{"from":"text","body":"echo hi"}`)},
		{ItemID: "a1", Kind: hubproto.EnvKindAccess, Name: "GitLab", Source: json.RawMessage(`{"service":"gitlab"}`)},
		{ItemID: "r1", Kind: hubproto.EnvKindRepo, Name: "Монорепо", Source: json.RawMessage(`{"url":"https://gitlab.com/acme/mono.git"}`)},
		{ItemID: "k1", Kind: hubproto.EnvKindSkill, Name: "Скилл", Source: json.RawMessage(`{"body":"Коротко."}`)},
	}
}

func TestEnvKeeperSetupKindsWaitForTheRuntime(t *testing.T) {
	dir := t.TempDir()
	k := newEnvKeeper(dir)
	items := setupItems()
	hubproto.SortEnvItems(items)
	hash := hubproto.EnvHash(items)
	st, err := k.Set(hubproto.EnvSet{Rev: 1, Hash: hash, Items: items})
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]hubproto.EnvApplied{}
	for _, a := range st.Applied {
		by[a.ItemID] = a
	}
	for _, id := range []string{"s1", "a1", "r1"} {
		if by[id].Status != hubproto.EnvPending || by[id].Message != "машина ещё настраивается" {
			t.Errorf("%s = %+v", id, by[id])
		}
	}
	if by["k1"].Status != hubproto.EnvOK {
		t.Errorf("skill = %+v", by["k1"])
	}
	// Scripts and access put nothing into the chats' plugin; the repository says where it is cloned.
	if _, err := os.Stat(filepath.Join(dir, "environment", "claude-plugin", "skills", "s1")); err == nil {
		t.Error("a script became a skill")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "environment", "claude-plugin", "skills", reposSkillID, "SKILL.md"))
	if !strings.Contains(string(raw), "~/code/mono") {
		t.Errorf("repo skill: %s", raw)
	}
	// The runtime's account for another list changes nothing; for this one it is the status.
	write := func(h string) {
		if err := writeFileAtomic(filepath.Join(dir, hubproto.EnvSetupFile), mustJSON(hubproto.EnvSetupState{Hash: h, Once: map[string]string{}, Items: []hubproto.EnvApplied{
			{ItemID: "s1", Status: hubproto.EnvError, Message: "завершился с кодом 3"}, {ItemID: "a1", Status: hubproto.EnvOK}, {ItemID: "r1", Status: hubproto.EnvOK}}})); err != nil {
			t.Fatal(err)
		}
	}
	write("other")
	if k.Reapply() {
		t.Fatal("an account of another list counted")
	}
	write(hash)
	if !k.Reapply() {
		t.Fatal("the account was not noticed")
	}
	by = map[string]hubproto.EnvApplied{}
	for _, a := range k.State().Applied {
		by[a.ItemID] = a
	}
	if by["s1"].Status != hubproto.EnvError || by["s1"].Message != "завершился с кодом 3" || by["a1"].Status != hubproto.EnvOK || by["r1"].Status != hubproto.EnvOK {
		t.Fatalf("%+v", by)
	}
}

func mustJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }

// beat is a fresh heartbeat of the runtime's setup in dir.
func beat(dir string) {
	_ = writeFileAtomic(filepath.Join(dir, hubproto.EnvRunnerFile), mustJSON(hubproto.EnvRunnerBeat{PID: 1, At: time.Now().UnixMilli()}))
}

func TestEnvRunOverTheHubLink(t *testing.T) {
	r := startHubAgent(t, rigOptions{beforeStart: beat})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if !slices.Contains(hello.Features, hubproto.FeatureEnvSetup) || !hello.Policy.Setup {
		t.Fatalf("hello %v setup=%v", hello.Features, hello.Policy.Setup)
	}
	c.mustCall("link.subscribe", map[string]any{"events": []string{hubproto.EventEnvRun, hubproto.EventEnvState}})
	dataDir := filepath.Join(r.rig.Home, ".harnessly")
	waitAllow(t, dataDir, true)

	// The held list, and the runtime's account of it, reach the hub as env.state.
	items := setupItems()
	hubproto.SortEnvItems(items)
	hash := hubproto.EnvHash(items)
	c.mustCall(hubproto.OpEnvSet, map[string]any{"rev": 1, "hash": hash, "items": items})
	if err := writeFileAtomic(filepath.Join(dataDir, hubproto.EnvSetupFile), mustJSON(hubproto.EnvSetupState{Hash: hash, Once: map[string]string{}, Items: []hubproto.EnvApplied{
		{ItemID: "s1", Status: hubproto.EnvOK}, {ItemID: "a1", Status: hubproto.EnvOK}, {ItemID: "r1", Status: hubproto.EnvOK}}})); err != nil {
		t.Fatal(err)
	}
	ev, _ := c.waitEvent(hubproto.EventEnvState, 0, 10*time.Second)
	for ev != nil {
		var st hubproto.EnvState
		_ = json.Unmarshal(ev["state"], &st)
		done := true
		for _, a := range st.Applied {
			if a.ItemID == "s1" && a.Status != hubproto.EnvOK {
				done = false
			}
		}
		if done {
			break
		}
		ev, _ = c.waitEvent(hubproto.EventEnvState, 1, 10*time.Second)
	}
	if ev == nil {
		t.Fatal("no env.state after the runtime's account")
	}

	// A request: written for the runtime, answered at once; the same id again is busy; bad ones are refused.
	res := decode[map[string]any](t, c.mustCall(hubproto.OpEnvRun, map[string]any{"runId": "run-12345678", "mode": "check", "by": "Анна"}))
	if res["runId"] != "run-12345678" || res["accepted"] != true {
		t.Fatalf("%v", res)
	}
	var req hubproto.EnvRunRequest
	raw, err := os.ReadFile(filepath.Join(dataDir, hubproto.EnvRequestsDir, "run-12345678.json"))
	if err != nil || json.Unmarshal(raw, &req) != nil || req.Mode != "check" || req.By != "Анна" {
		t.Fatalf("request file: %s %v", raw, err)
	}
	if _, e := c.call(hubproto.OpEnvRun, map[string]any{"runId": "run-12345678", "mode": "check"}); !strings.HasPrefix(e, "busy: ") {
		t.Errorf("same id: %q", e)
	}
	for name, params := range map[string]map[string]any{
		"bad id":     {"runId": "../x", "mode": "check"},
		"apply":      {"runId": "run-abcdefgh", "mode": "apply"},
		"no mode":    {"runId": "run-abcdefgh", "mode": "format"},
		"items only": {"runId": "run-abcdefgh", "mode": "update", "items": items},
		"bad item":   {"runId": "run-abcdefgh", "mode": "sandbox", "items": []hubproto.EnvItem{{ItemID: "../x", Kind: "script"}}},
	} {
		if _, e := c.call(hubproto.OpEnvRun, params); !strings.HasPrefix(e, "invalid: ") {
			t.Errorf("%s: %q", name, e)
		}
	}
	sb := decode[map[string]any](t, c.mustCall(hubproto.OpEnvRun, map[string]any{"runId": "try-12345678", "mode": "sandbox", "items": items}))
	if sb["accepted"] != true {
		t.Fatalf("%v", sb)
	}

	// A run file the runtime writes is sent as env.run, and again when it changes.
	run := hubproto.EnvRun{RunID: "run-12345678", Mode: "check", Hash: hash, Status: hubproto.EnvRunning, StartedAt: 1, Steps: []hubproto.EnvStep{{ItemID: "s1", Kind: "script", Name: "Сборка", Status: hubproto.EnvRunning}}}
	if err := writeFileAtomic(filepath.Join(dataDir, hubproto.EnvRunsDir, "run-12345678.json"), mustJSON(run)); err != nil {
		t.Fatal(err)
	}
	n := 0
	got := func(status string) {
		t.Helper()
		for {
			ev, next := c.waitEvent(hubproto.EventEnvRun, n, 10*time.Second)
			if ev == nil {
				t.Fatalf("no env.run %s", status)
			}
			n = next
			var e hubproto.EnvRun
			_ = json.Unmarshal(ev["run"], &e)
			if e.RunID == "run-12345678" && e.Status == status {
				return
			}
		}
	}
	got(hubproto.EnvRunning)
	run.Status, run.FinishedAt = hubproto.EnvOK, 2
	time.Sleep(10 * time.Millisecond)
	if err := writeFileAtomic(filepath.Join(dataDir, hubproto.EnvRunsDir, "run-12345678.json"), mustJSON(run)); err != nil {
		t.Fatal(err)
	}
	got(hubproto.EnvOK)
}

func TestEnvRunNeedsThePolicyOnAnOwnLink(t *testing.T) {
	r := startHubAgent(t, rigOptions{policy: "setup = false\n", beforeStart: beat})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if slices.Contains(hello.Features, hubproto.FeatureEnvSetup) || hello.Policy.Setup {
		t.Fatalf("hello %v setup=%v", hello.Features, hello.Policy.Setup)
	}
	if _, e := c.call(hubproto.OpEnvRun, map[string]any{"runId": "run-12345678", "mode": "check"}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("%q", e)
	}
	if _, err := os.Stat(filepath.Join(r.rig.Home, ".harnessly", hubproto.EnvRequestsDir)); err == nil {
		t.Error("a refused request was written")
	}

	// An own link under `setup = false` also keeps the runtime from running anything: the permission file says no, and a
	// script of the list is pending with the reason.
	dataDir := filepath.Join(r.rig.Home, ".harnessly")
	waitAllow(t, dataDir, false)
	script := []hubproto.EnvItem{{ItemID: "s1", Kind: hubproto.EnvKindScript, Name: "Сборка", Order: 1, Source: json.RawMessage(`{"from":"text","body":"echo hi"}`)}}
	st := decode[hubproto.EnvState](t, c.mustCall(hubproto.OpEnvSet, map[string]any{"rev": 1, "hash": hubproto.EnvHash(script), "items": script}))
	if len(st.Applied) != 1 || st.Applied[0].Status != hubproto.EnvPending || !strings.Contains(st.Applied[0].Message, "политика setup") {
		t.Fatalf("%+v", st.Applied)
	}
}

func waitAllow(t *testing.T, dir string, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var a hubproto.EnvAllow
		raw, err := os.ReadFile(filepath.Join(dir, hubproto.EnvAllowFile))
		if err == nil && json.Unmarshal(raw, &a) == nil && a.Setup == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never said setup=%v (%s %v)", hubproto.EnvAllowFile, want, raw, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// An observe link is off unless the operator opts in on the machine.
func TestEnvRunOnAnObserveLinkNeedsTheOptIn(t *testing.T) {
	t.Setenv("AGENT_HUB_SETUP", "")
	o := startHubAgent(t, rigOptions{observe: true})
	oc := o.connect()
	hello := decode[hubproto.Hello](t, oc.mustCall("hello", nil))
	if hello.Policy.Setup || slices.Contains(hello.Features, hubproto.FeatureEnvSetup) {
		t.Fatalf("observe hello %v setup=%v", hello.Features, hello.Policy.Setup)
	}
	if _, e := oc.call(hubproto.OpEnvRun, map[string]any{"runId": "run-12345678", "mode": "update"}); !strings.HasPrefix(e, "policy_denied: ") {
		t.Fatalf("%q", e)
	}
	hubDir := filepath.Join(o.rig.Home, ".harnessly", "hub")
	waitAllow(t, hubDir, false)
	if _, err := os.Stat(filepath.Join(hubDir, hubproto.EnvRequestsDir)); err == nil {
		t.Error("a refused request was written")
	}

	t.Setenv("AGENT_HUB_SETUP", "on")
	o2 := startHubAgent(t, rigOptions{observe: true, beforeStart: func(d string) { beat(filepath.Join(d, "hub")) }})
	oc2 := o2.connect()
	hello = decode[hubproto.Hello](t, oc2.mustCall("hello", nil))
	if !slices.Contains(hello.Features, hubproto.FeatureEnvSetup) {
		t.Fatalf("observe hello with the opt-in %v", hello.Features)
	}
	oc2.mustCall(hubproto.OpEnvRun, map[string]any{"runId": "run-12345678", "mode": "update"})
	hubDir2 := filepath.Join(o2.rig.Home, ".harnessly", "hub")
	if _, err := os.Stat(filepath.Join(hubDir2, hubproto.EnvRequestsDir, "run-12345678.json")); err != nil {
		t.Fatalf("the observe link's request is in its own folder: %v", err)
	}
	waitAllow(t, hubDir2, true)
}

func TestPolicySetupKey(t *testing.T) {
	p, err := ParsePolicy("setup = false\n", "x")
	if err != nil || p.Setup || !p.Terminal {
		t.Fatalf("%+v %v", p, err)
	}
	// A file that does not mention `setup` does not allow it; no file at all does.
	if p, _ := ParsePolicy("terminal = true\n", "x"); p.Setup {
		t.Fatal("a policy file without `setup` allows the setup")
	}
	if p, _ := ParsePolicy("setup = true\n", "x"); !p.Setup {
		t.Fatal("setup = true is not on")
	}
	if !DefaultPolicy().Setup {
		t.Fatal("setup is on when there is no file")
	}
	if !Narrowed(DefaultPolicy(), p) || meet(DefaultPolicy(), p).Setup {
		t.Fatal("setup is not part of narrowing")
	}
}

// With no runtime to do the setup (no heartbeat), the feature is not offered and the items say why.
func TestNoFeatureWithoutAFreshRunnerHeartbeat(t *testing.T) {
	r := startHubAgent(t, rigOptions{})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if slices.Contains(hello.Features, hubproto.FeatureEnvSetup) {
		t.Fatalf("%v", hello.Features)
	}
	items := setupItems()
	hubproto.SortEnvItems(items)
	st := decode[hubproto.EnvState](t, c.mustCall(hubproto.OpEnvSet, map[string]any{"rev": 1, "hash": hubproto.EnvHash(items), "items": items}))
	for _, a := range st.Applied {
		if (a.ItemID == "s1" || a.ItemID == "a1" || a.ItemID == "r1") && a.Message != "настройщик машины не запущен" {
			t.Errorf("%+v", a)
		}
	}
	// A stale beat is no beat; a fresh one is.
	_ = writeFileAtomic(filepath.Join(r.rig.Home, ".harnessly", hubproto.EnvRunnerFile), mustJSON(hubproto.EnvRunnerBeat{PID: 1, At: time.Now().Add(-3 * time.Minute).UnixMilli()}))
	if r.agent.hub.runnerAlive() {
		t.Error("a stale beat counts")
	}
	beat(filepath.Join(r.rig.Home, ".harnessly"))
	if !r.agent.hub.runnerAlive() {
		t.Error("a fresh beat does not count")
	}
}

// With the setup off, a repository keeps its status (its chat reference), the scripts and logins wait with the reason.
func TestRepoStaysOkWhenSetupIsOff(t *testing.T) {
	k := newEnvKeeper(t.TempDir())
	k.setupAllowed = func() bool { return false }
	items := setupItems()
	hubproto.SortEnvItems(items)
	st, err := k.Set(hubproto.EnvSet{Rev: 1, Hash: hubproto.EnvHash(items), Items: items})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range st.Applied {
		switch a.ItemID {
		case "r1", "k1":
			if a.Status != hubproto.EnvOK {
				t.Errorf("%+v", a)
			}
		case "s1", "a1":
			if a.Status != hubproto.EnvPending || !strings.Contains(a.Message, "политика setup") {
				t.Errorf("%+v", a)
			}
		}
	}
}

// Turning the setup off while the link is up reaches the hub without a restart: the outcomes are worked out again and
// the link calls again, so the hub's new `hello` no longer offers the setup.
func TestSetupOffChangesHelloWithoutARestart(t *testing.T) {
	r := startHubAgent(t, rigOptions{beforeStart: beat})
	c := r.connect()
	hello := decode[hubproto.Hello](t, c.mustCall("hello", nil))
	if !slices.Contains(hello.Features, hubproto.FeatureEnvSetup) {
		t.Fatalf("hello %v", hello.Features)
	}
	writeFile(t, filepath.Join(r.rig.Home, "policy.toml"), "setup = false\n")
	again := r.hub.waitConn(20 * time.Second)
	if again == nil {
		t.Fatal("the link did not call again after the setup was turned off")
	}
	hello = decode[hubproto.Hello](t, again.mustCall("hello", nil))
	if slices.Contains(hello.Features, hubproto.FeatureEnvSetup) || hello.Policy.Setup {
		t.Fatalf("hello after the change %v setup=%v", hello.Features, hello.Policy.Setup)
	}
}
