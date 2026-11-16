package kitadd

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The test binary is its own sandbox helper, as the runtime and the CLI are.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == SandboxHelperArg {
		if err := RunSandboxHelper(os.Args[2:]); err != nil {
			os.Stderr.WriteString(err.Error() + "\n")
		}
		os.Exit(3)
	}
	os.Exit(m.Run())
}

// A package that is malicious in the module the build loads cannot read what is outside the build folder, see the
// runtime's environment, or reach the network (TCP, UDP, a unix socket).
func TestSandboxedNodeSeesNeitherTheHomeNorTheNetwork(t *testing.T) {
	if !SandboxAvailable() {
		t.Skip("no Landlock")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("no node")
	}
	secretDir := t.TempDir()
	secret := filepath.Join(secretDir, "token")
	if err := os.WriteFile(secret, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIT_TEST_SECRET", "hunter2")
	var hits atomic.Int32
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			c.Close()
		}
	}()
	dir := t.TempDir()
	script := `
const fs = require("fs"), net = require("net"), dgram = require("dgram");
const out = {};
let printed = false;
const done = () => { if (!printed) { printed = true; console.log(JSON.stringify(out)); process.exit(0); } };
setTimeout(() => { out.udp = out.udp || "denied"; done(); }, 2000);
try { out.secret = fs.readFileSync(process.argv[1], "utf8"); } catch (e) { out.secret = "denied " + e.code; }
try { fs.writeFileSync("ok.txt", "x"); out.write = "ok"; } catch (e) { out.write = "denied " + e.code; }
try { out.home = fs.readdirSync(require("path").dirname(process.argv[1])).length >= 0 ? "listed" : ""; } catch (e) { out.home = "denied"; }
try { out.environ = fs.readFileSync("/proc/1/environ", "utf8").length >= 0 ? "read" : ""; } catch (e) { out.environ = "denied"; }
out.env = process.env.KIT_TEST_SECRET || "none";
const s = net.connect(Number(process.argv[2]), "127.0.0.1");
s.on("error", e => { out.tcp = "error " + e.code; const u = dgram.createSocket("udp4"); u.on("error", () => { out.udp = "denied"; done(); }); u.send("x", Number(process.argv[2]), "127.0.0.1", err => { out.udp = err ? "denied" : "sent"; u.close(); done(); }); });
s.on("connect", () => { out.tcp = "connected"; done(); });
`
	o := &Options{Sandbox: true, SandboxHelper: []string{os.Args[0], SandboxHelperArg}}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	got, err := o.runCmd(dir, "node", "-e", script, secret, port)
	if err != nil {
		t.Fatalf("node in the sandbox: %v", err)
	}
	var res map[string]string
	if err := json.Unmarshal(got, &res); err != nil {
		t.Fatalf("answer %q: %v", got, err)
	}
	// The control: the same script outside the sandbox does read, connect and write, so the denials above are the
	// sandbox's and not a broken script.
	plain := &Options{}
	free, err := plain.runCmd(dir, "node", "-e", script, secret, port)
	if err != nil {
		t.Fatalf("node without the sandbox: %v", err)
	}
	var open map[string]string
	if err := json.Unmarshal(free, &open); err != nil || open["secret"] != "hunter2" || open["tcp"] != "connected" {
		t.Fatalf("the control run: %q %v", free, err)
	}
	hits.Store(0)
	if !strings.HasPrefix(res["secret"], "denied") {
		t.Errorf("a file outside the build folder was read: %v", res)
	}
	if res["write"] != "ok" {
		t.Errorf("the build folder is not writable: %v", res)
	}
	if res["home"] != "denied" || res["environ"] != "denied" {
		t.Errorf("home or another process's environment is visible: %v", res)
	}
	if res["env"] != "none" {
		t.Errorf("the runtime's environment leaked: %v", res)
	}
	if !strings.HasPrefix(res["tcp"], "error") || res["udp"] != "denied" {
		t.Errorf("the network is reachable: %v", res)
	}
	if hits.Load() != 0 {
		t.Errorf("the listener was connected to %d times", hits.Load())
	}
}

func TestLockWithAnAddressOutsideTheRegistryIsRefused(t *testing.T) {
	hosts := map[string]bool{"registry.npmjs.org": true}
	good := `{"packages":{"":{},"node_modules/a":{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz"},"node_modules/b":{"inBundle":true}}}`
	bad := map[string]string{
		"git":       `{"packages":{"node_modules/a":{"resolved":"git+ssh://git@github.com/x/a.git#abc"}}}`,
		"git-http":  `{"packages":{"node_modules/a":{"resolved":"git+https://github.com/x/a.git#abc"}}}`,
		"file":      `{"packages":{"node_modules/a":{"resolved":"file:../a"}}}`,
		"link":      `{"packages":{"node_modules/a":{"resolved":"../a","link":true}}}`,
		"tarball":   `{"packages":{"node_modules/a":{"resolved":"https://evil.example.org/a.tgz"}}}`,
		"http":      `{"packages":{"node_modules/a":{"resolved":"http://registry.npmjs.org/a.tgz"}}}`,
		"userinfo":  `{"packages":{"node_modules/a":{"resolved":"https://u:p@registry.npmjs.org/a.tgz"}}}`,
		"none":      `{"packages":{"node_modules/a":{"version":"1.0.0"}}}`,
		"nohost":    `{"packages":{"node_modules/a":{"resolved":"https:///a.tgz"}}}`,
		"lookalike": `{"packages":{"node_modules/a":{"resolved":"https://registry.npmjs.org.evil.example.org/a.tgz"}}}`,
	}
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(good)
	if err := checkLock(dir, hosts); err != nil {
		t.Fatalf("a lock of the registry: %v", err)
	}
	for name, lock := range bad {
		write(lock)
		if err := checkLock(dir, hosts); err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The tree is installed (npm ci) only after its lock was checked, and a lock with a git dependency installs nothing.
func TestSandboxedInstallChecksTheLockBeforeCi(t *testing.T) {
	for _, c := range []struct {
		name, lock string
		wantCi     bool
	}{
		{"registry", `{"packages":{"node_modules/a":{"resolved":"https://registry.npmjs.org/a/-/a-1.0.0.tgz"}}}`, true},
		{"git", `{"packages":{"node_modules/a":{"resolved":"git+ssh://git@github.com/x/a.git#abc"}}}`, false},
	} {
		var calls []string
		dir := t.TempDir()
		o := &Options{Sandbox: true, Npm: func(d string, args ...string) error {
			calls = append(calls, args[0])
			if args[0] == "install" {
				if !strings.Contains(strings.Join(args, " "), "--package-lock-only") || !strings.Contains(strings.Join(args, " "), "--ignore-scripts") {
					t.Errorf("%s: the first step is not a lock-only install without scripts: %v", c.name, args)
				}
				return os.WriteFile(filepath.Join(d, "package-lock.json"), []byte(c.lock), 0o644)
			}
			return nil
		}}
		err := npmInstall(o, dir, []string{"a"})
		hasCi := strings.Join(calls, ",") == "install,ci"
		if hasCi != c.wantCi || (err != nil) == c.wantCi {
			t.Errorf("%s: calls %v, err %v", c.name, calls, err)
		}
	}
}

// KITADD_LIVE=1: a real npm in the sandbox installs a real package through the checked lock.
func TestSandboxedNpmInstallsThroughTheCheckedLock(t *testing.T) {
	if os.Getenv("KITADD_LIVE") == "" || !SandboxAvailable() {
		t.Skip("KITADD_LIVE not set")
	}
	dir := t.TempDir()
	o := &Options{Sandbox: true, SandboxHelper: []string{os.Args[0], SandboxHelperArg}, Log: os.Stderr}
	if err := npmInstall(o, dir, []string{"left-pad@1.3.0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "left-pad", "package.json")); err != nil {
		t.Fatal(err)
	}
}
