package hlcli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fleetTarball(t *testing.T, commit, bin string) (string, string) {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, m := range [][2]string{
		{"release/.harnessly-release.json", `{"version":"0.0.0","commit":"` + commit + `"}`},
		{"release/bin/harnessly-runtime", bin},
		{"release/web/index.html", "page"},
	} {
		_ = tw.WriteHeader(&tar.Header{Name: m[0], Mode: 0o755, Size: int64(len(m[1])), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(m[1]))
	}
	_ = tw.Close()
	_ = zw.Close()
	p := filepath.Join(t.TempDir(), "r.tgz")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(buf.Bytes())
	return p, hex.EncodeToString(h[:])
}

func runFleet(t *testing.T, env map[string]string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(append([]string{"fleet"}, args...), Env{Getenv: func(k string) string { return env[k] }, Stdout: &out, Stderr: &errb})
	return code, out.String(), errb.String()
}

func TestFleetCommandStageActivateDelta(t *testing.T) {
	root := filepath.Join(t.TempDir(), "fleet")
	env := map[string]string{"RUNTIME_FLEET_ROOT": root, "RUNTIME_FLEET_RESTART": "true"}
	commitA, commitB := strings.Repeat("a", 40), strings.Repeat("b", 40)

	whole, sha := fleetTarball(t, commitA, "runtime one")
	if code, _, e := runFleet(t, env, "stage", "--version", "1.0.0", "--tarball", whole, "--sha256", sha); code != 0 {
		t.Fatalf("stage: %d %s", code, e)
	}
	if code, _, e := runFleet(t, env, "activate", "--version", "1.0.0", "--sha256", sha); code != 0 {
		t.Fatalf("activate: %d %s", code, e)
	}
	code, out, _ := runFleet(t, env, "status", "--json")
	if code != 0 || !strings.Contains(out, `"version":"1.0.0"`) || !strings.Contains(out, commitA) {
		t.Fatalf("status: %d %s", code, out)
	}

	next, _ := fleetTarball(t, commitB, "runtime two")
	delta := filepath.Join(t.TempDir(), "d.tgz")
	code, out, e := runFleet(t, env, "delta", "--tarball", next, "--out", delta, "--head", commitB)
	if code != 0 {
		t.Fatalf("delta: %d %s", code, e)
	}
	dsha := out[strings.LastIndex(out, " ")+1 : len(out)-1]
	// A delta against a commit the machine does not run: exit 3, nothing staged. Stage takes the tarball it is given
	// away either way, so the refused one is a copy.
	raw, err := os.ReadFile(delta)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other.tgz")
	if err := os.WriteFile(other, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ = runFleet(t, env, "stage", "--version", "1.1.0", "--tarball", other, "--sha256", dsha, "--delta-base", strings.Repeat("c", 40))
	if code != 3 {
		t.Fatalf("a delta of another base must exit 3, got %d", code)
	}
	if code, _, e := runFleet(t, env, "stage", "--version", "1.1.0", "--tarball", delta, "--sha256", dsha, "--delta-base", commitA); code != 0 {
		t.Fatalf("delta stage: %d %s", code, e)
	}
	if code, _, e := runFleet(t, env, "activate", "--version", "1.1.0", "--sha256", dsha); code != 0 {
		t.Fatalf("delta activate: %d %s", code, e)
	}
	got, _ := os.ReadFile(filepath.Join(root, "current", "bin", "harnessly-runtime"))
	page, _ := os.ReadFile(filepath.Join(root, "current", "web", "index.html"))
	if string(got) != "runtime two" || string(page) != "page" {
		t.Fatalf("the delta must replace bin and keep the rest: %q %q", got, page)
	}
	if code, _, e := runFleet(t, env, "rollback"); code != 0 {
		t.Fatalf("rollback: %d %s", code, e)
	}
	got, _ = os.ReadFile(filepath.Join(root, "current", "bin", "harnessly-runtime"))
	if string(got) != "runtime one" {
		t.Fatalf("after rollback: %q", got)
	}
}

func TestFleetCommandNeedsARoot(t *testing.T) {
	if code, _, e := runFleet(t, nil, "status"); code != 1 || !strings.Contains(e, "RUNTIME_FLEET_ROOT") {
		t.Fatalf("%d %s", code, e)
	}
	if code, _, _ := runFleet(t, nil, "nonsense"); code != 2 {
		t.Fatalf("an unknown subcommand is a usage error, got %d", code)
	}
}

func TestFleetRolloutIsOffUnlessAsked(t *testing.T) {
	code, _, e := runFleet(t, map[string]string{}, "rollout", "--fleet", "x.json", "--version", "1.0.0", "--tarball", "t.tgz")
	if code == 0 || !strings.Contains(e, "RUNTIME_ROLLOUT_DRIVER=on") {
		t.Fatalf("%d %s", code, e)
	}
}

func TestFleetRolloutDryRunReadsTheFleetAndChangesNothing(t *testing.T) {
	dir := t.TempDir()
	fleetFile := filepath.Join(dir, "fleet.json")
	tar := filepath.Join(dir, "r.tgz")
	// The "ssh" is a script that answers a status call and fails anything else: a dry run must not call anything else.
	ssh := filepath.Join(dir, "ssh.sh")
	if err := os.WriteFile(ssh, []byte("#!/bin/sh\ncase \"$2\" in *' fleet status'*) echo '{\"version\":\"1.0.0\"}';; *) echo \"unexpected: $2\" >&2; exit 9;; esac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, c := range map[string]string{
		fleetFile: `{"schema":1,"hosts":[{"name":"m1","target":"m1.example.com","wave":1},{"name":"m2","target":"m2.example.com"}]}`,
		tar:       "release",
	} {
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, out, e := runFleet(t, map[string]string{"RUNTIME_ROLLOUT_DRIVER": "on"}, "rollout", "--fleet", fleetFile,
		"--version", "1.1.0", "--tarball", tar, "--ssh", ssh, "--dry-run")
	if code != 0 || !strings.Contains(out, "wave 0: m2") || !strings.Contains(out, "wave 1: m1") {
		t.Fatalf("%d\n%s\n%s", code, out, e)
	}
}
