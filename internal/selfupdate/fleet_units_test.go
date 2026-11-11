package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const gateUnit = `[Service]
User={{.user}}
Environment=AGENT_VERSION={{.version}}
Environment=RUNTIME_GATE_LISTEN=127.0.0.1:{{.port}}
ExecStart={{.release}}/bin/runtime
`

func unitsSpec() UnitsSpec {
	return UnitsSpec{
		Schema: 1,
		Vars:   map[string]string{"org": "example"},
		Pairs: []UnitsPair{
			{Vars: map[string]string{"user": "anna", "port": "8001"}},
			{Vars: map[string]string{"user": "boris", "port": "8002"}},
			{Host: "other", Vars: map[string]string{"user": "carl", "port": "8003"}},
		},
		Files: []UnitsFile{
			{Path: "{{.org}}-gate-{{.user}}.service", Content: gateUnit, PerPair: true, Enable: true},
			{Path: "{{.org}}-gate-{{.user}}.service.d/memory.conf", Content: "[Service]\nMemoryMax=80%\n", PerPair: true},
		},
	}
}

func TestUnitsPlanRendersPerPairOfThisHost(t *testing.T) {
	plan, err := unitsSpec().Plan("box", "1.2.3", "/opt/r")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Want) != 4 {
		t.Fatalf("want 2 pairs x 2 files, got %v", plan.Want)
	}
	got := plan.Want["example-gate-anna.service"]
	if !strings.Contains(got, "AGENT_VERSION=1.2.3") || !strings.Contains(got, "127.0.0.1:8001") || !strings.Contains(got, "/opt/r/bin/runtime") {
		t.Fatalf("not rendered: %s", got)
	}
	if _, ok := plan.Want["example-gate-carl.service"]; ok {
		t.Fatal("a pair of another host must not be rendered")
	}
	if len(plan.Enable) != 2 {
		t.Fatalf("enable: %v", plan.Enable)
	}
	other, _ := unitsSpec().Plan("other", "1.2.3", "/opt/r")
	if _, ok := other.Want["example-gate-carl.service"]; !ok || len(other.Want) != 6 {
		t.Fatalf("host other takes the pairs without a host and its own: %v", other.Want)
	}
}

func TestUnitsPlanRefusesWhatLeavesTheFolder(t *testing.T) {
	s := unitsSpec()
	s.Files = []UnitsFile{{Path: "../evil.service", Content: "x"}}
	if _, err := s.Plan("box", "1.0.0", "/r"); err == nil {
		t.Fatal("a path outside the unit folder must be refused")
	}
	s.Files = []UnitsFile{{Path: "a.service", Content: "{{.nothing}}"}}
	if _, err := s.Plan("box", "1.0.0", "/r"); err == nil {
		t.Fatal("a missing variable must be an error, not an empty text")
	}
}

func TestApplyUnitsWritesOnlyWhatChanged(t *testing.T) {
	dir := t.TempDir()
	plan, _ := unitsSpec().Plan("box", "1.0.0", "/r")
	ch, err := ApplyUnits(dir, plan, false)
	if err != nil || len(ch.Written) != 4 || len(ch.Enable) != 2 {
		t.Fatalf("first apply: %+v %v", ch, err)
	}
	again, err := ApplyUnits(dir, plan, false)
	if err != nil || !again.Empty() {
		t.Fatalf("the same plan must change nothing: %+v %v", again, err)
	}
	next, _ := unitsSpec().Plan("box", "1.0.1", "/r")
	dry, _ := ApplyUnits(dir, next, true)
	if len(dry.Written) != 2 {
		t.Fatalf("only the two gate units carry the version: %+v", dry)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "example-gate-anna.service")); !strings.Contains(string(b), "1.0.0") {
		t.Fatal("a dry run must not write")
	}
	ch2, _ := ApplyUnits(dir, next, false)
	if len(ch2.Enable) != 0 {
		t.Fatalf("a unit that exists is not enabled again: %+v", ch2)
	}
	if err := ch2.Restore(dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "example-gate-boris.service")); !strings.Contains(string(b), "1.0.0") {
		t.Fatal("Restore must put the old text back")
	}
}

func TestApplyUnitsRemovesTheListedAndEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "old.service"), []byte("old"), 0o644)
	s := UnitsSpec{Schema: 1, Files: []UnitsFile{{Path: "gone.service", Content: ""}, {Path: "kept.service", Content: "k"}}, Remove: []string{"old.service"}}
	_ = os.WriteFile(filepath.Join(dir, "gone.service"), []byte("x"), 0o644)
	plan, err := s.Plan("box", "1.0.0", "/r")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := ApplyUnits(dir, plan, false)
	if err != nil || len(ch.Removed) != 2 || len(ch.Written) != 1 {
		t.Fatalf("%+v %v", ch, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.service")); !os.IsNotExist(err) {
		t.Fatal("old.service must be gone")
	}
}

func TestFleetActivateWritesUnitsWithOneReloadAndOneEnable(t *testing.T) {
	f, _, _ := newFleet(t)
	spec := unitsSpec()
	f.Units, f.Host, f.UnitDir = &spec, "box", t.TempDir()
	var calls []string
	f.Systemctl = func(_ context.Context, args ...string) error {
		calls = append(calls, strings.Join(args, " "))
		return nil
	}
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "one"))
	if _, err := f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1}); err != nil {
		t.Fatal(err)
	}
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	want := []string{"daemon-reload", "enable --no-reload example-gate-anna.service example-gate-boris.service"}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("systemctl calls: %v, want %v", calls, want)
	}
	b, _ := os.ReadFile(filepath.Join(f.UnitDir, "example-gate-anna.service"))
	if !strings.Contains(string(b), "AGENT_VERSION=1.0.0") {
		t.Fatalf("unit: %s", b)
	}
}

func TestFleetFailedCheckPutsTheUnitsBack(t *testing.T) {
	f, _, serving := newFleet(t)
	spec := unitsSpec()
	f.Units, f.Host, f.UnitDir = &spec, "box", t.TempDir()
	reloads := 0
	f.Systemctl = func(_ context.Context, args ...string) error {
		if args[0] == "daemon-reload" {
			reloads++
		}
		return nil
	}
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "one"))
	_, _ = f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1})
	if err := f.Activate(context.Background(), "1.0.0", sha1); err != nil {
		t.Fatal(err)
	}
	tar2, sha2 := tgz(t, "r2.tgz", release(commitB, "two"))
	_, _ = f.Stage(StageInput{Version: "1.1.0", Tarball: tar2, SHA256: sha2})
	f.Restart = func(context.Context) error {
		r, _ := ReadRelease(f.current())
		if r.Version == "1.0.0" {
			*serving = "1.0.0"
		} else {
			*serving = ""
		}
		return nil
	}
	if err := f.Activate(context.Background(), "1.1.0", sha2); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("want a rollback, got %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(f.UnitDir, "example-gate-anna.service"))
	if !strings.Contains(string(b), "AGENT_VERSION=1.0.0") {
		t.Fatalf("the units of the old release must be back: %s", b)
	}
	if reloads != 3 { // first activation, second activation, the way back
		t.Fatalf("daemon-reload %d times, want 3", reloads)
	}
}

func TestFleetBadUnitsSpecStopsTheActivation(t *testing.T) {
	f, _, _ := newFleet(t)
	f.UnitsErr = os.ErrInvalid
	tar1, sha1 := tgz(t, "r1.tgz", release(commitA, "one"))
	_, _ = f.Stage(StageInput{Version: "1.0.0", Tarball: tar1, SHA256: sha1})
	if err := f.Activate(context.Background(), "1.0.0", sha1); err == nil || f.Active() != "" {
		t.Fatalf("an unreadable spec must refuse the activation with nothing changed: %v %q", err, f.Active())
	}
}
