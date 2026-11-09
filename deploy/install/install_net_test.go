package install

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The company's network: CA, proxy, package mirror, offline files, the check before installing and which Docker is used.

const testCA = "-----BEGIN CERTIFICATE-----\nMIIBexample\n-----END CERTIFICATE-----\n"

func (e *env) ca() string {
	e.t.Helper()
	p := filepath.Join(e.dir, "corp-ca.pem")
	if err := os.WriteFile(p, []byte(testCA), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func TestNetworkSettingsReachEveryComponent(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ca := e.ca()
	r := e.run(nil, "--token", "hx_net", "--profile", "dir", "--wait", "4", "--ca-file", ca,
		"--proxy", "http://proxy.example.com:3128", "--no-proxy", ".example.com")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	for _, f := range []string{"runtime.env", "agent.env"} {
		mustContain(t, f, e.read(e.path("/etc/harnessly/"+f)),
			"SSL_CERT_FILE="+ca+"\n", "NODE_EXTRA_CA_CERTS="+ca+"\n", "HTTPS_PROXY=http://proxy.example.com:3128\n",
			"https_proxy=http://proxy.example.com:3128\n", "NO_PROXY=127.0.0.1,localhost,::1,127.0.0.1,.example.com\n")
	}
	// The apps' calls and Connections take them by their own names (internal/egress).
	mustContain(t, "runtime.env", e.read(e.path("/etc/harnessly/runtime.env")), "RUNTIME_APPS_CA_FILE="+ca+"\n",
		"RUNTIME_APPS_PROXY=http://proxy.example.com:3128\n", "RUNTIME_APPS_NO_PROXY=127.0.0.1,localhost,::1,127.0.0.1,.example.com\n")

	// Running again changes nothing: the same files, no doubled lines.
	before := e.read(e.path("/etc/harnessly/runtime.env"))
	if r = e.run(nil, "--token", "hx_net", "--profile", "dir", "--wait", "4", "--ca-file", ca,
		"--proxy", "http://proxy.example.com:3128", "--no-proxy", ".example.com"); r.code != 0 {
		t.Fatalf("again: exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	if after := e.read(e.path("/etc/harnessly/runtime.env")); after != before {
		t.Errorf("runtime.env changed on the second run:\n%s\n---\n%s", before, after)
	}

	// Without the options nothing about the network is written.
	e2 := newEnv(t)
	if r = e2.run(nil, "--token", "hx_net", "--profile", "dir", "--wait", "4"); r.code != 0 {
		t.Fatalf("plain: exit %d\n%s", r.code, r.err)
	}
	mustNotContain(t, "runtime.env", e2.read(e2.path("/etc/harnessly/runtime.env")), "SSL_CERT_FILE", "PROXY", "proxy")
}

func TestNetworkSettingsReachTheContainer(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	ca := e.ca()
	r := e.run([]string{"FAKE_UID=1000"}, "--token", "hx_ctr", "--profile", "container", "--image", "x/y:1", "--wait", "2",
		"--ca-file", ca, "--proxy", "http://proxy.example.com:3128")
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	calls := e.calledWith()
	// Both the registration and the machine get the CA (mounted) and the proxy.
	if n := strings.Count(calls, "-v "+ca+":/etc/harnessly/ca.pem:ro,z"); n != 2 {
		t.Errorf("the CA is mounted %d times, want 2:\n%s", n, calls)
	}
	mustContain(t, "calls", calls, "-e NODE_EXTRA_CA_CERTS=/etc/harnessly/ca.pem", "-e SSL_CERT_FILE=/etc/harnessly/ca.pem",
		"-e HTTPS_PROXY=http://proxy.example.com:3128", "-e NO_PROXY=127.0.0.1,localhost,::1,127.0.0.1")
}

func TestNetworkOptionsAreChecked(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"--ca-file", filepath.Join(e.dir, "missing.pem")}, "is not a readable file"},
		{[]string{"--proxy", "http://p;rm -rf /"}, "--proxy has odd characters"},
		{[]string{"--docker", "both"}, "--docker must be"},
	} {
		r := e.run(nil, append([]string{"--token", "hx_net", "--profile", "dir"}, c.args...)...)
		if r.code == 0 || !strings.Contains(r.err, c.want) {
			t.Errorf("%v: exit %d\n%s", c.args, r.code, r.err)
		}
	}
	// A file that is not a certificate.
	bad := filepath.Join(e.dir, "bad.pem")
	if err := os.WriteFile(bad, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r := e.run(nil, "--token", "hx_net", "--profile", "dir", "--ca-file", bad); r.code == 0 || !strings.Contains(r.err, "no PEM certificate") {
		t.Errorf("not a PEM: exit %d\n%s", r.code, r.err)
	}
}

func TestCheckLooksAndChangesNothing(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	r := e.run([]string{"FAKE_UID=1000"}, "--check", "--profile", "container", "--image", "x/y:1", "--ca-file", e.ca())
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	mustContain(t, "output", r.out, "ok    the hub answers: "+e.hub.URL, "ok    Docker: a daemon that is already running answers", "ready")
	if calls := e.calledWith(); strings.Contains(calls, "pull") || strings.Contains(calls, "run ") {
		t.Errorf("--check changed something:\n%s", calls)
	}
	// The journal has the steps.
	log := e.read(filepath.Join(e.root, e.home, ".harnessly/install.log"))
	mustContain(t, "journal", log, " start ", " step  Checking this machine", " ok    the hub answers")

	// A hub that is not there: the check says so and fails.
	e2 := newEnv(t)
	e2.hub.Close()
	r = e2.run([]string{"FAKE_UID=1000"}, "--check", "--profile", "container", "--image", "x/y:1")
	if r.code == 0 || !strings.Contains(r.out, "FAIL  the hub is not reachable") {
		t.Errorf("hub down: exit %d\n%s", r.code, r.out)
	}
}

func TestDockerExternalOrOwn(t *testing.T) {
	t.Parallel()
	// No daemon, no dockerd: the check fails and says what to do.
	e := newEnv(t)
	r := e.run([]string{"FAKE_UID=0", "FAKE_NO_ENGINE=1", "FAKE_NO_SUDO=1"}, "--check", "--profile", "container", "--image", "x/y:1")
	if r.code == 0 || !strings.Contains(r.out, "FAIL  Docker: no daemon answers") {
		t.Errorf("no docker: exit %d\n%s", r.code, r.out)
	}

	// No daemon, but dockerd and root: its own is possible; --docker external refuses.
	e2 := newEnv(t)
	if err := os.WriteFile(filepath.Join(e2.stubs, "dockerd"), []byte("#!/bin/sh\necho \"dockerd $*\" >> \"$FAKE_CALLS\"\ntouch \"$FAKE_DIR/own.sock\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{"FAKE_UID=0", "FAKE_NO_ENGINE=1", "FAKE_NO_SUDO=1"}
	r = e2.run(env, "--check", "--profile", "container", "--image", "x/y:1")
	if r.code != 0 || !strings.Contains(r.out, "ok    Docker: no daemon is running; dockerd is installed and the installer can start its own") {
		t.Errorf("own possible: exit %d\n%s", r.code, r.out)
	}
	r = e2.run(env, "--check", "--profile", "container", "--image", "x/y:1", "--docker", "external")
	if r.code == 0 || !strings.Contains(r.out, "--docker external was asked") {
		t.Errorf("external asked: exit %d\n%s", r.code, r.out)
	}
	// A dry run says it would start one.
	r = e2.run(env, "--dry-run", "--token", "hx_ctr", "--profile", "container", "--image", "x/y:1")
	if !strings.Contains(r.out, "start its own Docker daemon: dockerd --host unix://") {
		t.Errorf("dry run:\n%s\n%s", r.out, r.err)
	}
}

func TestOfflineTakesTheProgramsFromAFolder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	dir := filepath.Join(e.dir, "offline")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// The folder is a copy of the hub's /dl; the hub itself then serves nothing of it.
	rec := newEnv(t)
	for _, f := range []string{"manifest.json", "agent-linux-amd64", "agent-linux-arm64", "runtime-linux-amd64", "runtime-linux-arm64", "agent-darwin-arm64", "runtime-darwin-arm64", "agent-darwin-amd64", "runtime-darwin-amd64"} {
		body := httpGet(t, rec.hub.URL+"/dl/"+f)
		if err := os.WriteFile(filepath.Join(dir, f), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := e.run(nil, "--token", "hx_off", "--profile", "dir", "--wait", "4", "--offline", dir)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s\n%s", r.code, r.out, r.err)
	}
	if !e.exists(e.path("/usr/local/bin/harnessly-agent")) {
		t.Error("the agent was not installed from the folder")
	}
	// A folder without the files stops with a clear word.
	r = e.run(nil, "--check", "--profile", "dir", "--offline", filepath.Join(e.dir, "nothing"))
	if r.code == 0 || !strings.Contains(r.out, "FAIL") {
		t.Errorf("empty folder: exit %d\n%s", r.code, r.out)
	}
}

func httpGet(t *testing.T, url string) []byte {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
