package fleetdriver

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeFleet plays the machines: it answers the commands the driver sends the way `harnessly fleet` does.
type fakeFleet struct {
	mu         sync.Mutex
	version    map[string]string // target -> running version
	prev       map[string]string
	failStage  map[string]bool
	failActive map[string]bool
	deltaMiss  map[string]bool // machines that run another commit than the delta's base
	log        []string
	uploads    []string
}

func newFake(targets ...string) *fakeFleet {
	f := &fakeFleet{version: map[string]string{}, prev: map[string]string{}, failStage: map[string]bool{}, failActive: map[string]bool{}, deltaMiss: map[string]bool{}}
	for _, t := range targets {
		f.version[t] = "1.0.0"
	}
	return f
}

func arg(cmd, flag string) string {
	f := strings.Fields(cmd)
	for i, a := range f {
		if a == flag && i+1 < len(f) {
			return f[i+1]
		}
	}
	return ""
}

func (f *fakeFleet) Run(_ context.Context, target, cmd string, stdin io.Reader, stdout io.Writer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case strings.Contains(cmd, "cat >"):
		_, _ = io.Copy(io.Discard, stdin)
		f.uploads = append(f.uploads, target+" "+filepath.Base(strings.Fields(cmd)[len(strings.Fields(cmd))-1]))
		return nil
	case strings.Contains(cmd, " fleet status"):
		_, _ = io.WriteString(stdout, `{"version":"`+f.version[target]+`"}`+"\n")
		return nil
	case strings.Contains(cmd, " fleet stage"):
		f.log = append(f.log, target+" stage")
		if strings.Contains(cmd, "--delta-base") && f.deltaMiss[target] {
			return &ExitError{Code: 3}
		}
		if f.failStage[target] {
			return &ExitError{Code: 1}
		}
		return nil
	case strings.Contains(cmd, " fleet activate"):
		f.log = append(f.log, target+" activate")
		if f.failActive[target] {
			return &ExitError{Code: 1}
		}
		f.prev[target], f.version[target] = f.version[target], arg(cmd, "--version")
		return nil
	case strings.Contains(cmd, " fleet rollback"):
		f.log = append(f.log, target+" rollback")
		f.version[target] = f.prev[target]
		return nil
	}
	return &ExitError{Code: 127}
}

func (f *fakeFleet) did(s string) bool {
	for _, l := range f.log {
		if l == s {
			return true
		}
	}
	return false
}

func testPlan(t *testing.T, f *fakeFleet, hosts ...Host) *Plan {
	t.Helper()
	tar := filepath.Join(t.TempDir(), "r.tgz")
	if err := os.WriteFile(tar, []byte("release"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Plan{
		Fleet:   &Fleet{Schema: 1, Hosts: hosts},
		Runner:  f,
		Version: "1.1.0", Tarball: tar,
		sleep: func(time.Duration) {}, UploadDelay: time.Millisecond,
	}
}

func hs(spec ...string) []Host { // "name:wave"
	var out []Host
	for _, s := range spec {
		n, w, _ := strings.Cut(s, ":")
		wave := 0
		if w != "" {
			wave = int(w[0] - '0')
		}
		out = append(out, Host{Name: n, Target: "root@" + n, Wave: wave, EnvFile: "/etc/fleet/" + n + ".env"})
	}
	return out
}

func TestRunWavesInOrder(t *testing.T) {
	f := newFake("root@a", "root@b", "root@c")
	p := testPlan(t, f, hs("a:0", "b:1", "c:1")...)
	res, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Activated) != 3 || len(res.Failed) != 0 {
		t.Fatalf("%+v", res)
	}
	// Every stage precedes every activation, and wave 0 (a) is activated before wave 1 (b, c).
	last := -1
	first := len(f.log)
	for i, l := range f.log {
		if strings.HasSuffix(l, " stage") && i > last {
			last = i
		}
		if strings.HasSuffix(l, " activate") && i < first {
			first = i
		}
	}
	if last > first {
		t.Fatalf("a stage after an activation: %v", f.log)
	}
	if f.log[first] != "root@a activate" {
		t.Fatalf("wave 0 first: %v", f.log)
	}
}

func TestRunFailedWaveRollsBackTheMoved(t *testing.T) {
	f := newFake("root@a", "root@b", "root@c")
	f.failActive["root@b"] = true
	p := testPlan(t, f, hs("a:0", "b:1", "c:2")...)
	res, err := p.Run(context.Background())
	if err == nil {
		t.Fatal("a failed machine must fail the rollout")
	}
	if !f.did("root@a rollback") || f.did("root@b rollback") || f.did("root@c activate") {
		t.Fatalf("a goes back, the failed b put itself back, c is never touched: %v", f.log)
	}
	if f.version["root@a"] != "1.0.0" {
		t.Fatalf("a is on %s", f.version["root@a"])
	}
	if strings.Join(res.Failed, ",") != "b" || strings.Join(res.RolledBack, ",") != "a" || strings.Join(res.NotTouched, ",") != "c" || len(res.Activated) != 0 {
		t.Fatalf("%+v", res)
	}
}

func TestRunStageFailureChangesNothing(t *testing.T) {
	f := newFake("root@a", "root@b")
	f.failStage["root@b"] = true
	p := testPlan(t, f, hs("a", "b")...)
	if _, err := p.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "no machine was changed") {
		t.Fatalf("%v", err)
	}
	if f.did("root@a activate") || f.did("root@b activate") {
		t.Fatalf("nothing is activated when a stage fails: %v", f.log)
	}
}

func TestRunSkipsMachineThatRunsTheVersion(t *testing.T) {
	f := newFake("root@a", "root@b")
	f.version["root@a"] = "1.1.0"
	p := testPlan(t, f, hs("a", "b")...)
	res, err := p.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if f.did("root@a stage") || f.did("root@a activate") || strings.Join(res.Skipped, ",") != "a" || strings.Join(res.Activated, ",") != "b" {
		t.Fatalf("%v %+v", f.log, res)
	}
}

func TestRunOnlyOneMachine(t *testing.T) {
	f := newFake("root@a", "root@b")
	p := testPlan(t, f, hs("a", "b")...)
	p.Only = []string{"b"}
	if _, err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.did("root@a stage") || !f.did("root@b activate") {
		t.Fatalf("%v", f.log)
	}
	p.Only = []string{"zzz"}
	if _, err := p.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not in the fleet: zzz") {
		t.Fatalf("%v", err)
	}
}

func TestRunDeltaFallsBackToWhole(t *testing.T) {
	f := newFake("root@a", "root@b")
	f.deltaMiss["root@b"] = true
	p := testPlan(t, f, hs("a", "b")...)
	p.Delta = filepath.Join(t.TempDir(), "d.tgz")
	if err := os.WriteFile(p.Delta, []byte("delta"), 0o644); err != nil {
		t.Fatal(err)
	}
	p.DeltaBase = strings.Repeat("a", 40)
	if _, err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(f.uploads, ";")
	if !strings.Contains(got, "root@a 1.1.0.delta.tgz") || strings.Contains(got, "root@a 1.1.0.tgz") {
		t.Fatalf("a takes the delta only: %s", got)
	}
	if !strings.Contains(got, "root@b 1.1.0.delta.tgz") || !strings.Contains(got, "root@b 1.1.0.tgz") {
		t.Fatalf("b gets the delta refused, then the whole release: %s", got)
	}
}

func TestRunDryRunChangesNothing(t *testing.T) {
	f := newFake("root@a")
	p := testPlan(t, f, hs("a")...)
	p.DryRun = true
	if _, err := p.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.log) != 0 || len(f.uploads) != 0 {
		t.Fatalf("%v %v", f.log, f.uploads)
	}
}

func TestRemoteSourcesTheMachinesSettings(t *testing.T) {
	p := &Plan{Fleet: &Fleet{Schema: 1}}
	h := Host{Name: "a", Target: "root@a", EnvFile: "/etc/fleet/a.env", Root: "/opt/x"}
	got := p.remote(h, "rollback")
	want := "set -e; set -a; . /etc/fleet/a.env; set +a; harnessly fleet rollback --root /opt/x"
	if got != want {
		t.Fatalf("%q", got)
	}
}

func TestValidateRefusesShellCharacters(t *testing.T) {
	for _, bad := range []Host{
		{Name: "a", Target: "root@a; rm -rf /"},
		{Name: "a b", Target: "root@a"},
		{Name: "a", Target: "root@a", EnvFile: "/etc/x.env; id"},
		{Name: "a", Target: "root@a", Root: "$(id)"},
	} {
		f := Fleet{Schema: 1, Hosts: []Host{bad}}
		if f.Validate() == nil {
			t.Errorf("%+v passed", bad)
		}
	}
	if (&Fleet{Schema: 1, Hosts: append(hs("a"), hs("a")...)}).Validate() == nil {
		t.Error("a machine twice passed")
	}
	if (&Fleet{Schema: 2, Hosts: hs("a")}).Validate() == nil {
		t.Error("schema 2 passed")
	}
}

func TestLoadRefusesUnknownFields(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fleet.json")
	if err := os.WriteFile(p, []byte(`{"schema":1,"hosts":[{"name":"a","target":"root@a","wvae":1}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("a typo in a key must not be ignored")
	}
}

func TestExecRunnerLocalTargetRunsHere(t *testing.T) {
	var out strings.Builder
	r := ExecRunner{} // no ssh command at all: the local target needs none
	if err := r.Run(context.Background(), LocalTarget, "cat; echo done", strings.NewReader("in "), &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "in done\n" {
		t.Fatalf("%q", out.String())
	}
	err := r.Run(context.Background(), LocalTarget, "exit 3", nil, nil)
	if exitCodeOf(err) != 3 {
		t.Fatalf("%v", err)
	}
}
