package upgrade

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ly-solutions/harnessly/internal/release"
)

func sum(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

type rig struct {
	dir   string
	m     *release.Manifest
	files map[string]string // download name -> content
	t     []Target
}

func newRig(t *testing.T) *rig {
	t.Helper()
	d := t.TempDir()
	r := &rig{dir: d, files: map[string]string{"runtime-linux-amd64": "NEW-RUNTIME", "harnessly-linux-amd64": "NEW-CLI"}}
	comp := func(key string) release.Component {
		return release.Component{Version: "2.0.0", Files: map[string]release.File{key: {SHA256: sum(r.files[key]), Size: int64(len(r.files[key]))}}}
	}
	r.m = &release.Manifest{Schema: 2, Version: "2.0.0", Data: release.Data{SchemaVersion: 14, Additive: true},
		Components:   map[string]release.Component{release.Runtime: comp("runtime-linux-amd64"), release.CLI: comp("harnessly-linux-amd64")},
		ExtensionAPI: map[string]release.Range{"harnessly.machine-provider": {Min: 1, Max: 1}}}
	rt, cli := filepath.Join(d, "harnessly-runtime"), filepath.Join(d, "harnessly")
	_ = os.WriteFile(rt, []byte("OLD-RUNTIME"), 0o755)
	_ = os.WriteFile(cli, []byte("OLD-CLI"), 0o755)
	// data the upgrade must never touch
	_ = os.WriteFile(filepath.Join(d, "machine.json"), []byte("registration"), 0o600)
	r.t = []Target{{Component: release.Runtime, Path: rt}, {Component: release.CLI, Path: cli}}
	return r
}

func (r *rig) opts() Options {
	return Options{Fetch: func(_ context.Context, key string) (io.ReadCloser, error) {
		c, ok := r.files[key]
		if !ok {
			return nil, errors.New("404 " + key)
		}
		return io.NopCloser(strings.NewReader(c)), nil
	}}
}

func (r *rig) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(r.dir, name))
	return string(b)
}

func TestPlanChangesNothing(t *testing.T) {
	r := newRig(t)
	p, err := MakePlan(r.m, r.t, "linux", "amd64", []Extension{{"a", "harnessly.machine-provider/v2"}, {"b", "harnessly.credentials/v1"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Changes() || !p.Blocked() || len(p.Items) != 2 || p.Items[0].Action != "update" {
		t.Fatalf("%+v", p)
	}
	if p.Extensions[0].Verdict != release.Incompatible || p.Extensions[1].Verdict != release.Unknown {
		t.Errorf("%+v", p.Extensions)
	}
	if r.read("harnessly-runtime") != "OLD-RUNTIME" {
		t.Error("plan changed a file")
	}
	if _, err := Apply(context.Background(), p, r.opts()); !errors.Is(err, ErrBlocked) {
		t.Errorf("a blocked plan applied: %v", err)
	}
	if r.read("harnessly") != "OLD-CLI" {
		t.Error("a refused apply changed a file")
	}
}

func TestApplyKeepsDataAndLeavesPrev(t *testing.T) {
	r := newRig(t)
	p, _ := MakePlan(r.m, r.t, "linux", "amd64", []Extension{{"a", "harnessly.machine-provider/v1"}}, nil)
	var verified []Item
	o := r.opts()
	o.Verify = func(_ context.Context, items []Item) error { verified = items; return nil }
	res, err := Apply(context.Background(), p, o)
	if err != nil || res.RolledBack || len(res.Replaced) != 2 {
		t.Fatalf("%+v %v", res, err)
	}
	if r.read("harnessly-runtime") != "NEW-RUNTIME" || r.read("harnessly") != "NEW-CLI" || len(verified) != 2 {
		t.Error("not replaced")
	}
	if r.read("machine.json") != "registration" {
		t.Error("data touched")
	}
	if r.read("harnessly-runtime.prev") != "OLD-RUNTIME" {
		t.Error("no .prev")
	}
	// a second plan: nothing to do
	p2, _ := MakePlan(r.m, r.t, "linux", "amd64", nil, nil)
	if p2.Changes() {
		t.Error("still has changes")
	}
	// and an explicit rollback
	back, err := Rollback(r.t)
	if err != nil || len(back) != 2 || r.read("harnessly-runtime") != "OLD-RUNTIME" {
		t.Errorf("%v %v", back, err)
	}
}

func TestFailedCheckRollsBack(t *testing.T) {
	r := newRig(t)
	p, _ := MakePlan(r.m, r.t, "linux", "amd64", nil, nil)
	restarts := 0
	o := r.opts()
	o.Restart = func(context.Context) error { restarts++; return nil }
	o.Verify = func(context.Context, []Item) error { return errors.New("not healthy") }
	res, err := Apply(context.Background(), p, o)
	if err == nil || !res.RolledBack {
		t.Fatalf("%+v %v", res, err)
	}
	if r.read("harnessly-runtime") != "OLD-RUNTIME" || r.read("harnessly") != "OLD-CLI" || r.read("machine.json") != "registration" {
		t.Error("not put back")
	}
	if restarts != 2 {
		t.Errorf("restarted %d times, want 2 (new, then old)", restarts)
	}
}

func TestBadDownloadLeavesEverything(t *testing.T) {
	r := newRig(t)
	p, _ := MakePlan(r.m, r.t, "linux", "amd64", nil, nil)
	r.files["harnessly-linux-amd64"] = "BAD-CLI"
	if _, err := Apply(context.Background(), p, r.opts()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("%v", err)
	}
	if r.read("harnessly-runtime") != "OLD-RUNTIME" || r.read("harnessly") != "OLD-CLI" {
		t.Error("a failed download changed a file")
	}
	ents, _ := os.ReadDir(r.dir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".new-") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestOptionalMissingIsInstalledAndRemovedOnRollback(t *testing.T) {
	r := newRig(t)
	ag := filepath.Join(r.dir, "harnessly-agent")
	r.files["agent-linux-amd64"] = "NEW-AGENT"
	r.m.Components[release.Agent] = release.Component{Version: "2.0.0", Files: map[string]release.File{"agent-linux-amd64": {SHA256: sum("NEW-AGENT"), Size: 9}}}
	r.t = append(r.t, Target{Component: release.Agent, Path: ag, Optional: true})
	p, _ := MakePlan(r.m, r.t, "linux", "amd64", nil, nil)
	if p.Items[2].Action != "install" {
		t.Fatalf("%+v", p.Items)
	}
	o := r.opts()
	o.Verify = func(context.Context, []Item) error { return errors.New("no") }
	if _, err := Apply(context.Background(), p, o); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(ag); !os.IsNotExist(err) {
		t.Error("the installed agent stayed after a rollback")
	}
}

func TestPlanAddAppsBlocksOnlyAppsThatAreOn(t *testing.T) {
	p := &Plan{}
	p.AddApps([]AppCheck{
		{ID: "a.on", Version: "1.0.0", Errors: []string{"needs >=3"}},
		{ID: "a.off", Version: "1.0.0", Off: true, Errors: []string{"needs >=3"}},
		{ID: "a.warn", Version: "1.0.0", Warnings: []string{"soon"}},
	})
	if len(p.Apps) != 3 || len(p.Blocking) != 1 || !strings.Contains(p.Blocking[0], "a.on") {
		t.Fatalf("%+v", p)
	}
}
