package apps

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newRegistry(t *testing.T) *Registry {
	t.Helper()
	return &Registry{Root: filepath.Join(t.TempDir(), "apps")}
}

// versioned is a package of the pane-chat template whose manifest says version.
func versioned(t *testing.T, id, version string) string {
	t.Helper()
	dir := scaffold(t, id, "pane-chat")
	if err := setManifestVersion(dir, version); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPublishNumbersAreNeverReused(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.demo", "1.3.0")

	b1, _, err := r.Publish(dir, ChannelBeta, PublishOptions{})
	if err != nil || b1.Version != "1.3.0-beta.1" {
		t.Fatalf("first beta: %v %+v", err, b1)
	}
	b2, _, err := r.Publish(dir, ChannelBeta, PublishOptions{})
	if err != nil || b2.Version != "1.3.0-beta.2" {
		t.Fatalf("second beta: %v %+v", err, b2)
	}
	if pkg, _ := LoadPackage(b1.Dir); pkg == nil || pkg.Manifest.Version != "1.3.0-beta.1" {
		t.Fatalf("the published copy says its own version: %+v", pkg)
	}
	s, _, err := r.Publish(dir, ChannelStable, PublishOptions{})
	if err != nil || s.Version != "1.3.0" {
		t.Fatalf("stable: %v %+v", err, s)
	}
	if _, _, err := r.Publish(dir, ChannelStable, PublishOptions{}); !errors.Is(err, ErrVersionExists) {
		t.Fatalf("a stable number twice: %v", err)
	}
	pre := versioned(t, "acme.demo", "1.4.0-rc.1")
	if _, _, err := r.Publish(pre, ChannelStable, PublishOptions{}); !errors.Is(err, ErrChannelVersion) {
		t.Fatalf("a pre-release on stable: %v", err)
	}
}

func TestPromoteAndRollback(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.demo", "1.3.0")
	if _, _, err := r.Publish(dir, ChannelDev, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	b, err := r.Promote("acme.demo", "1.3.0", ChannelBeta, PromoteOptions{})
	if err != nil || b.Version != "1.3.0-beta.1" {
		t.Fatalf("dev to beta: %v %+v", err, b)
	}
	s, err := r.Promote("acme.demo", "1.3.0", ChannelStable, PromoteOptions{})
	if err != nil || s.Version != "1.3.0" {
		t.Fatalf("beta to stable: %v %+v", err, s)
	}
	if pkg, _ := LoadPackage(s.Dir); pkg == nil || pkg.Manifest.Version != "1.3.0" {
		t.Fatalf("the stable copy says 1.3.0: %+v", pkg)
	}

	// 1.3.1 follows on stable, then goes back.
	next := versioned(t, "acme.demo", "1.3.1")
	if _, _, err := r.Publish(next, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	chs, _ := r.Channels("acme.demo")
	if chs[ChannelStable].Current != "1.3.1" || chs[ChannelStable].Previous != "1.3.0" {
		t.Fatalf("stable: %+v", chs[ChannelStable])
	}
	back, err := r.Rollback("acme.demo", ChannelStable)
	if err != nil || back.Version != "1.3.0" {
		t.Fatalf("rollback: %v %+v", err, back)
	}
	if _, err := os.Stat(r.VersionDir("acme.demo", "1.3.1")); err != nil {
		t.Fatalf("the version rolled back from stays published: %v", err)
	}
	if _, _, err := r.Publish(next, ChannelStable, PublishOptions{}); !errors.Is(err, ErrVersionExists) {
		t.Fatalf("a rolled-back number is not free again: %v", err)
	}
	if _, err := r.Rollback("acme.demo", ChannelStable); !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("nothing before 1.3.0: %v", err)
	}
	if _, err := r.Rollback("acme.demo", ChannelDev); err != nil && !errors.Is(err, ErrNoPrevious) {
		t.Fatalf("dev: %v", err)
	}
	if _, err := r.Promote("acme.demo", "9.9.9", ChannelStable, PromoteOptions{}); !errors.Is(err, ErrNoVersion) {
		t.Fatalf("unknown version: %v", err)
	}
	if _, err := r.Promote("acme.demo", "1.3.0", ChannelDev, PromoteOptions{}); !errors.Is(err, ErrChannelVersion) {
		t.Fatalf("promote down: %v", err)
	}
}

func TestStableAsksTheEvals(t *testing.T) {
	r := newRegistry(t)
	dir := versioned(t, "acme.demo", "1.0.0")
	if err := os.MkdirAll(filepath.Join(dir, "evals"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "evals", "a.json"), []byte(`{"prompt":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Publish(dir, ChannelBeta, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Promote("acme.demo", "1.0.0", ChannelStable, PromoteOptions{}); !errors.Is(err, ErrEvalGate) {
		t.Fatalf("no report: %v", err)
	}

	// A report in the package (eval writes it) travels with the published copy; a low rate does not pass.
	write := func(rate string) string {
		d := versioned(t, "acme.demo", "1.0.1")
		_ = os.MkdirAll(filepath.Join(d, "evals"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "evals", "a.json"), []byte(`{"prompt":"x"}`), 0o644)
		_ = os.MkdirAll(filepath.Join(d, ".harnessly"), 0o755)
		_ = os.WriteFile(filepath.Join(d, EvalReportPath), []byte(`{"passRate":`+rate+`}`), 0o644)
		return d
	}
	low := write("0.5")
	if _, _, err := r.Publish(low, ChannelBeta, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Promote("acme.demo", "1.0.1", ChannelStable, PromoteOptions{}); !errors.Is(err, ErrEvalGate) {
		t.Fatalf("50%% passed: %v", err)
	}
	if _, err := r.Promote("acme.demo", "1.0.1", ChannelStable, PromoteOptions{EvalThreshold: 0.4}); err != nil {
		t.Fatalf("a lower bar: %v", err)
	}

	good := versioned(t, "acme.demo", "1.0.2")
	_ = os.MkdirAll(filepath.Join(good, "evals"), 0o755)
	_ = os.WriteFile(filepath.Join(good, "evals", "a.json"), []byte(`{"prompt":"x"}`), 0o644)
	_ = os.MkdirAll(filepath.Join(good, ".harnessly"), 0o755)
	_ = os.WriteFile(filepath.Join(good, EvalReportPath), []byte(`{"passRate":0.9}`), 0o644)
	if _, _, err := r.Publish(good, ChannelBeta, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Promote("acme.demo", "1.0.2", ChannelStable, PromoteOptions{}); err != nil {
		t.Fatalf("90%%: %v", err)
	}
}

func TestInstallsFollowTheirChannel(t *testing.T) {
	r := newRegistry(t)
	host := FirstVersion
	publish := func(version string) {
		t.Helper()
		if _, _, err := r.Publish(versioned(t, "acme.demo", version), ChannelStable, PublishOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	publish("1.0.0")
	got, _, err := r.InstallChannel("acme.demo", ChannelStable, InstallOptions{HostVersion: host})
	if err != nil || got.Version != "1.0.0" || got.Channel != "stable" {
		t.Fatalf("install: %v %+v", err, got)
	}

	// A minor reaches the install at once.
	publish("1.1.0")
	res, err := r.SyncChannels(SyncOptions{HostVersion: host})
	if err != nil || len(res) != 1 || !res[0].Applied || res[0].To != "1.1.0" {
		t.Fatalf("minor: %v %+v", err, res)
	}
	inst, _ := r.Get("acme.demo")
	if inst.Version != "1.1.0" || inst.Previous != "1.0.0" {
		t.Fatalf("installed: %+v", inst)
	}

	// A major waits for a word.
	publish("2.0.0")
	res, _ = r.SyncChannels(SyncOptions{HostVersion: host})
	if len(res) != 1 || res[0].Applied || res[0].Reason != "major" {
		t.Fatalf("major: %+v", res)
	}
	if inst, _ = r.Get("acme.demo"); inst.Version != "1.1.0" {
		t.Fatalf("the major was not applied: %s", inst.Version)
	}
	res, _ = r.SyncChannels(SyncOptions{HostVersion: host, Confirm: []string{"acme.demo"}})
	if len(res) != 1 || !res[0].Applied {
		t.Fatalf("confirmed major: %+v", res)
	}

	// The channel goes back: the install follows at once, whatever the version.
	if _, err := r.Rollback("acme.demo", ChannelStable); err != nil {
		t.Fatal(err)
	}
	res, _ = r.SyncChannels(SyncOptions{HostVersion: host})
	if len(res) != 1 || !res[0].Applied || res[0].To != "1.1.0" {
		t.Fatalf("rollback: %+v", res)
	}
	if inst, _ = r.Get("acme.demo"); inst.Version != "1.1.0" {
		t.Fatalf("after rollback: %s", inst.Version)
	}
}

func TestNewRightsWaitForAWord(t *testing.T) {
	r := newRegistry(t)
	if _, _, err := r.Publish(versioned(t, "acme.demo", "1.0.0"), ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.InstallChannel("acme.demo", ChannelStable, InstallOptions{HostVersion: FirstVersion}); err != nil {
		t.Fatal(err)
	}
	more := versioned(t, "acme.demo", "1.0.1")
	m, _ := LoadPackage(more)
	m.Manifest.Permissions = append(m.Manifest.Permissions, "notify")
	writeManifest(t, more, m.Manifest)
	if _, _, err := r.Publish(more, ChannelStable, PublishOptions{}); err != nil {
		t.Fatal(err)
	}
	res, _ := r.SyncChannels(SyncOptions{HostVersion: FirstVersion})
	if len(res) != 1 || res[0].Applied || res[0].Reason != "permissions" {
		t.Fatalf("a new right: %+v", res)
	}
}

func writeManifest(t *testing.T, dir string, m *Manifest) {
	t.Helper()
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ManifestPath), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}
