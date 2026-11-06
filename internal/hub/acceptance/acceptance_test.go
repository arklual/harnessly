package acceptance

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"
)

type proc struct {
	cmd *exec.Cmd
	log string
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return fmt.Sprint(l.Addr().(*net.TCPAddr).Port)
}

func start(t *testing.T, name, bin string, env []string, args ...string) *proc {
	t.Helper()
	logf := filepath.Join(t.TempDir(), name+".log")
	f, err := os.Create(logf)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &proc{cmd: cmd, log: logf}
	t.Cleanup(p.stop)
	return p
}

func (p *proc) stop() {
	if p.cmd.ProcessState != nil {
		return
	}
	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
	}
}

func (p *proc) output() string { b, _ := os.ReadFile(p.log); return string(b) }

func waitHTTP(t *testing.T, p *proc, url string) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if r, err := http.Get(url); err == nil {
			r.Body.Close()
			return
		}
	}
	t.Fatalf("%s did not answer\n%s", url, p.output())
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

// person is a browser signed in to a hub by the email code the hub wrote to its log.
type person struct {
	t    *testing.T
	base string
	c    *http.Client
}

func (p *person) do(method, path string, body any) (int, []byte) {
	p.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, p.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := p.c.Do(req)
	if err != nil {
		p.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (p *person) must(want int, method, path string, body any, out any) {
	p.t.Helper()
	code, b := p.do(method, path, body)
	if code != want {
		p.t.Fatalf("%s %s: %d %s", method, path, code, b)
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			p.t.Fatalf("%s %s: %v: %s", method, path, err, b)
		}
	}
}

var codeRe = regexp.MustCompile(`code=(\d+)`)

func signIn(t *testing.T, hub *proc, base, email string) *person {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	p := &person{t: t, base: base, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	p.must(200, "POST", "/api/auth/email/start", map[string]string{"email": email, "intent": "signin"}, nil)
	var code string
	for end := time.Now().Add(5 * time.Second); code == "" && time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if ms := codeRe.FindAllStringSubmatch(hub.output(), -1); len(ms) > 0 {
			code = ms[len(ms)-1][1]
		}
	}
	if code == "" {
		t.Fatalf("no sign-in code in the hub log\n%s", hub.output())
	}
	p.must(200, "POST", "/api/auth/email/verify", map[string]string{"email": email, "code": code}, nil)
	return p
}

// call is a bare client of an adapter: what an operator or an installer would use.
func call(t *testing.T, method, url string, body any, headers ...string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func readToken(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// envFile reads a KEY=VALUE file such as the hub.env the pack installer writes.
func envFile(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

func pack(t *testing.T, script, prefix string, env []string, args ...string) {
	t.Helper()
	cmd := exec.Command("sh", append([]string{script}, append(args, "--prefix", prefix)...)...)
	cmd.Env = append(os.Environ(), env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ext-pack %v: %v\n%s", args, err, out)
	}
}

type opList struct {
	Ops []struct{ ID, Kind, Status string } `json:"ops"`
}

// TestCleanInstallUpdateRollback is the scenario of a public release. Build N is the previous release, build N+1 the
// one under test; both run on one database. The example pack (examples/ext-pack-example) is installed by its own
// script and is the only thing that tells the hub about the machine adapter, the memory store and the tracker.
//
//	clean install -> sign-in -> organization -> machine -> agent -> pack and hub updated -> everything works -> rollback
func TestCleanInstallUpdateRollback(t *testing.T) {
	binN, binN1, binAdapter, script := os.Getenv("HUB_BIN_N"), os.Getenv("HUB_BIN_N1"), os.Getenv("ADAPTER_BIN"), os.Getenv("PACK_SCRIPT")
	if binN == "" || binN1 == "" || binAdapter == "" || script == "" {
		t.Skip("HUB_BIN_N, HUB_BIN_N1, ADAPTER_BIN and PACK_SCRIPT name the inputs (tools/ci/acceptance-pack.sh)")
	}
	dir := t.TempDir()
	prefix := filepath.Join(dir, "pack")
	ports := []string{"PACK_PORT_MACHINE=" + freePort(t), "PACK_PORT_MEMORY=" + freePort(t), "PACK_PORT_CONNECTION=" + freePort(t)}
	hubAddr := "127.0.0.1:" + freePort(t)
	hubURL := "http://" + hubAddr

	// ---- 1. clean install: the pack, then the hub that is pointed at it ----
	pack(t, script, prefix, ports, "install", "--machine-adapter", binAdapter)
	pack(t, script, prefix, ports, "start")
	t.Cleanup(func() { pack(t, script, prefix, ports, "stop") })
	machineURL := "http://127.0.0.1:" + strings.TrimPrefix(ports[0], "PACK_PORT_MACHINE=")
	memoryURL := "http://127.0.0.1:" + strings.TrimPrefix(ports[1], "PACK_PORT_MEMORY=")
	machineAuth := []string{"Authorization", "Bearer " + readToken(t, filepath.Join(prefix, "secrets", "machine.token"))}
	memoryAuth := []string{"Authorization", "Bearer " + readToken(t, filepath.Join(prefix, "secrets", "memory.token"))}
	if code, _ := call(t, "GET", memoryURL+"/v1/describe", nil); code != 401 {
		t.Fatalf("the memory adapter answered %d without a token, want 401", code)
	}

	hubEnv := func() []string {
		env := []string{
			"HUB_PUBLIC_URL=" + hubURL, "HUB_LISTEN=" + hubAddr, "HUB_DATA_DIR=" + filepath.Join(dir, "data"),
			"HUB_DATABASE_URL=sqlite://" + filepath.Join(dir, "hub.db"), "HUB_PASSKEY=off", "HUB_RATES_URL=off",
			"HUB_ORGS=on", "HUB_MACHINE_RESOURCES=on", "HUB_ADMIN_EMAILS=owner@example.com",
		}
		return append(env, envFile(t, filepath.Join(prefix, "hub.env"))...)
	}
	extOn := func(o *person, name string) bool {
		var doc struct {
			Capabilities []struct{ ID, Status string } `json:"capabilities"`
		}
		o.must(200, "GET", "/api/capabilities", nil, &doc)
		for _, c := range doc.Capabilities {
			if c.ID == "extension."+name {
				return c.Status == "on"
			}
		}
		return false
	}

	hubN := start(t, "hub-n", binN, hubEnv())
	waitHTTP(t, hubN, hubURL+"/healthz")
	owner := signIn(t, hubN, hubURL, "owner@example.com")

	// ---- organization, machine, agent ----
	owner.must(201, "POST", "/api/orgs", map[string]string{"name": "Example Corp"}, nil)
	var sp struct{ Space struct{ ID string } }
	owner.must(201, "POST", "/api/spaces", map[string]string{"name": "Acceptance"}, &sp)
	var cd struct{ Code string }
	owner.must(201, "POST", "/api/spaces/"+sp.Space.ID+"/machines/codes", map[string]string{"profile": "dir"}, &cd)
	code, cr := call(t, "POST", machineURL+"/v1/machines", map[string]any{
		"name": "box-1", "config": map[string]any{"cpu": 2, "ramGB": 4, "diskGB": 40},
		"bootstrap": map[string]string{"hubUrl": hubURL, "code": cd.Code},
	}, append(machineAuth, "Idempotency-Key", "create-box-1")...)
	if code != 202 {
		t.Fatalf("create: %d %v", code, cr)
	}
	server, createOp := fmt.Sprint(cr["machine"]), fmt.Sprint(cr["op"])
	eventually(t, "the create operation", func() bool {
		_, o := call(t, "GET", machineURL+"/v1/ops/"+createOp, nil, machineAuth...)
		return o["state"] == "done"
	})
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	enc, _ := ecdh.X25519().GenerateKey(rand.Reader)
	reg := map[string]string{
		"code": cd.Code, "publicKey": base64.StdEncoding.EncodeToString(pub), "encKey": base64.StdEncoding.EncodeToString(enc.PublicKey().Bytes()),
		"hostname": "box-1", "os": "linux", "arch": "amd64", "unixUser": "dev", "profile": "dir", "via": "command", "agentVersion": "1.0.0",
	}
	var rr struct {
		MachineID string `json:"machineId"`
	}
	(&person{t: t, base: hubURL, c: http.DefaultClient}).must(201, "POST", "/agent/register", reg, &rr)
	if rr.MachineID == "" {
		t.Fatal("no machine id")
	}
	if code, out := call(t, "POST", machineURL+"/v1/machines/"+server+"/attach", map[string]string{"hubMachine": rr.MachineID}, machineAuth...); code != 200 {
		t.Fatalf("attach: %d %v", code, out)
	}
	owner.must(200, "PUT", "/api/machines/"+rr.MachineID+"/managed", map[string]any{"capability": "resources", "managed": true}, nil)

	// the pack's memory store holds a note across everything below
	if code, out := call(t, "POST", memoryURL+"/v1/put", map[string]string{"name": "plan.md", "text": "ship on friday"}, memoryAuth...); code != 200 {
		t.Fatalf("memory put: %d %v", code, out)
	}

	// works checks what a person gets from the hub: the machine, its operations, the pack's adapters
	works := func(o *person, label string, size int, wantExt bool) {
		t.Helper()
		var res struct {
			Provider struct {
				Supported bool `json:"supported"`
			} `json:"provider"`
			ProviderError *string `json:"providerError"`
		}
		o.must(200, "GET", "/api/machines/"+rr.MachineID+"/resources", nil, &res)
		if !res.Provider.Supported || res.ProviderError != nil {
			t.Fatalf("%s: the machine adapter is not working: %+v", label, res)
		}
		o.must(202, "POST", "/api/machines/"+rr.MachineID+"/resize", map[string]int{"cpu": size, "ramGB": size * 2, "diskGB": 80}, nil)
		eventually(t, label+": resize done", func() bool {
			var l opList
			o.must(200, "GET", "/api/machines/"+rr.MachineID+"/ops", nil, &l)
			return len(l.Ops) > 0 && l.Ops[0].Kind == "resize" && l.Ops[0].Status == "done"
		})
		if code, st := call(t, "GET", machineURL+"/v1/machines/"+server, nil, machineAuth...); code != 200 || int(st["cpu"].(float64)) != size {
			t.Fatalf("%s: the server is %v, want %d CPU", label, st, size)
		}
		code, got := call(t, "POST", memoryURL+"/v1/get", map[string]string{"name": "plan.md"}, memoryAuth...)
		if code != 200 || !strings.Contains(fmt.Sprint(got), "ship on friday") {
			t.Fatalf("%s: the note of the memory store is gone: %d %v", label, code, got)
		}
		if wantExt {
			eventually(t, label+": the pack's adapters are on in /api/capabilities", func() bool {
				return extOn(o, "example-corp-memory") && extOn(o, "example-tracker")
			})
		}
	}
	// build N may be older than the extension frame; if it knows the pack, it must show it on
	_, capsN := owner.do("GET", "/api/capabilities", nil)
	works(owner, "build N", 4, strings.Contains(string(capsN), `"extension.`))

	// ---- 2. update: the pack to its next version, the hub to build N+1, on the same database ----
	hubN.stop()
	pack(t, script, prefix, ports, "install", "--machine-adapter", binAdapter) // a second install keeps the first in previous/
	pack(t, script, prefix, ports, "stop")
	pack(t, script, prefix, ports, "start")
	hubN1 := start(t, "hub-n1", binN1, hubEnv())
	waitHTTP(t, hubN1, hubURL+"/healthz")
	owner1 := signIn(t, hubN1, hubURL, "owner@example.com")
	if code, b := owner1.do("GET", "/api/spaces/"+sp.Space.ID+"/machines", nil); code != 200 || !strings.Contains(string(b), rr.MachineID) {
		t.Fatalf("the machine is not in the hub after the update: %d %s\n%s", code, b, hubN1.output())
	}
	if code, b := owner1.do("GET", "/api/orgs", nil); code != 200 || !strings.Contains(string(b), "Example Corp") {
		t.Fatalf("the organization is not in the hub after the update: %d %s", code, b)
	}
	works(owner1, "build N+1", 2, true)

	// ---- 3. rollback: the previous pack and the previous build on the same database ----
	hubN1.stop()
	pack(t, script, prefix, ports, "rollback")
	hubBack := start(t, "hub-n-again", binN, hubEnv())
	waitHTTP(t, hubBack, hubURL+"/healthz")
	ownerBack := signIn(t, hubBack, hubURL, "owner@example.com")
	if code, b := ownerBack.do("GET", "/api/spaces/"+sp.Space.ID+"/machines", nil); code != 200 || !strings.Contains(string(b), rr.MachineID) {
		t.Fatalf("the machine is not in the hub after the rollback: %d %s\n%s", code, b, hubBack.output())
	}
	works(ownerBack, "rollback to N", 4, strings.Contains(string(capsN), `"extension.`))
	t.Logf("machine %s on server %s survived: install, update, rollback", rr.MachineID, server)
}
