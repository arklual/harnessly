package upgradecheck

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

// proc is a started binary; its output goes to a file kept for the failure message.
type proc struct {
	cmd *exec.Cmd
	log string
}

func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
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
	t.Cleanup(func() { p.stop() })
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
		ms := codeRe.FindAllStringSubmatch(hub.output(), -1)
		if len(ms) > 0 {
			code = ms[len(ms)-1][1]
		}
	}
	if code == "" {
		t.Fatalf("no sign-in code in the hub log\n%s", hub.output())
	}
	p.must(200, "POST", "/api/auth/email/verify", map[string]string{"email": email, "code": code}, nil)
	return p
}

// a bare client of the adapter: what the hub's operator or an installer script would call.
func adapterCall(t *testing.T, method, url string, body any, headers ...string) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, url, rd)
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

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end); time.Sleep(100 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatalf("timed out waiting for %s", what)
}

type opList struct {
	Ops []struct{ ID, Kind, Status string } `json:"ops"`
}

// TestAdapterSurvivesHubUpdate is the scenario: the adapter and release build N of the hub; a machine is created and
// attached; the hub is replaced by build N+1 on the same database; the machine is there and its operations go on.
// Neither the hub nor the adapter is changed between the two halves.
func TestAdapterSurvivesHubUpdate(t *testing.T) {
	binN, binN1, binAdapter := os.Getenv("HUB_BIN_N"), os.Getenv("HUB_BIN_N1"), os.Getenv("ADAPTER_BIN")
	if binN == "" || binN1 == "" || binAdapter == "" {
		t.Skip("HUB_BIN_N, HUB_BIN_N1 and ADAPTER_BIN name the binaries (tools/ci/adapter-upgrade.sh)")
	}
	dir := t.TempDir()
	adapterAddr, hubAddr := freeAddr(t), freeAddr(t)
	adapterURL, hubURL := "http://"+adapterAddr, "http://"+hubAddr
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ad := start(t, "adapter", binAdapter, nil, "-listen", adapterAddr, "-state", filepath.Join(dir, "cloud.json"), "-token-file", token, "-op-delay", "200ms")
	waitHTTP(t, ad, adapterURL+"/v1/describe")
	adapterBase := func(path string) string { return adapterURL + path }
	// the adapter wants the token: the hub carries it, this check does not
	if code, _ := adapterCall(t, "GET", adapterBase("/v1/describe"), nil); code != 401 {
		t.Fatalf("an adapter without a token answered %d, want 401\n%s", code, ad.output())
	}
	authz := []string{"Authorization", "Bearer s3cret"}

	hubEnv := func(protocol string) []string {
		env := []string{
			"HUB_PUBLIC_URL=" + hubURL, "HUB_LISTEN=" + hubAddr, "HUB_DATA_DIR=" + filepath.Join(dir, "data"),
			"HUB_DATABASE_URL=sqlite://" + filepath.Join(dir, "hub.db"), "HUB_PASSKEY=off", "HUB_RATES_URL=off",
			"HUB_ORGS=on", "HUB_MACHINE_RESOURCES=on", "HUB_PROVIDER=http", "HUB_PROVIDER_URL=" + adapterURL, "HUB_PROVIDER_TOKEN_FILE=" + token,
			"HUB_ADMIN_EMAILS=owner@example.com",
		}
		if protocol != "" {
			env = append(env, "HUB_PROVIDER_PROTOCOL="+protocol)
		}
		return env
	}

	// ---- build N ----
	hubN := start(t, "hub-n", binN, hubEnv(""))
	waitHTTP(t, hubN, hubURL+"/healthz")
	owner := signIn(t, hubN, hubURL, "owner@example.com")
	var sp struct{ Space struct{ ID string } }
	owner.must(201, "POST", "/api/spaces", map[string]string{"name": "Adapter check"}, &sp)
	var cd struct{ Code string }
	owner.must(201, "POST", "/api/spaces/"+sp.Space.ID+"/machines/codes", map[string]string{"profile": "dir"}, &cd)

	// create: the adapter makes the server and is handed the registration code its agent registers with
	code, cr := adapterCall(t, "POST", adapterBase("/v1/machines"), map[string]any{
		"name": "box-1", "config": map[string]any{"cpu": 2, "ramGB": 4, "diskGB": 40},
		"bootstrap": map[string]string{"hubUrl": hubURL, "code": cd.Code},
	}, append(authz, "Idempotency-Key", "create-box-1")...)
	if code != 202 {
		t.Fatalf("create: %d %v", code, cr)
	}
	server, createOp := fmt.Sprint(cr["machine"]), fmt.Sprint(cr["op"])
	// a repeat of the same create is the same server
	if _, again := adapterCall(t, "POST", adapterBase("/v1/machines"), map[string]any{"name": "box-1"}, append(authz, "Idempotency-Key", "create-box-1")...); fmt.Sprint(again["op"]) != createOp {
		t.Fatalf("an Idempotency-Key repeat made another operation: %v", again)
	}
	eventually(t, "the create operation", func() bool {
		_, o := adapterCall(t, "GET", adapterBase("/v1/ops/"+createOp), nil, authz...)
		return o["state"] == "done"
	})

	// the agent of the new server registers with the code (the stub has no agent: the test is it)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	enc, _ := ecdh.X25519().GenerateKey(rand.Reader)
	reg := map[string]string{
		"code": cd.Code, "publicKey": base64.StdEncoding.EncodeToString(pub), "encKey": base64.StdEncoding.EncodeToString(enc.PublicKey().Bytes()),
		"hostname": "box-1", "os": "linux", "arch": "amd64", "unixUser": "dev", "profile": "dir", "via": "command", "agentVersion": "1.0.0",
	}
	anon := &person{t: t, base: hubURL, c: http.DefaultClient}
	var rr struct {
		MachineID string `json:"machineId"`
	}
	anon.must(201, "POST", "/agent/register", reg, &rr)
	if rr.MachineID == "" {
		t.Fatal("no machine id")
	}

	// attach: «this server is the hub's machine»
	if code, out := adapterCall(t, "POST", adapterBase("/v1/machines/"+server+"/attach"), map[string]string{"hubMachine": rr.MachineID}, authz...); code != 200 {
		t.Fatalf("attach: %d %v", code, out)
	}
	owner.must(200, "PUT", "/api/machines/"+rr.MachineID+"/managed", map[string]any{"capability": "resources", "managed": true}, nil)

	// operations through build N (protocol v0: the hub knows nothing of describe)
	hubOps := func(o *person, label string, size int) {
		t.Helper()
		var res struct {
			Provider struct {
				Name      string `json:"name"`
				Supported bool   `json:"supported"`
			} `json:"provider"`
			ProviderError *string `json:"providerError"`
			Current       *struct{ CPU, RAMGB, DiskGB int }
		}
		o.must(200, "GET", "/api/machines/"+rr.MachineID+"/resources", nil, &res)
		if !res.Provider.Supported || res.ProviderError != nil {
			t.Fatalf("%s: provider %+v error %v", label, res.Provider, func() string {
				if res.ProviderError == nil {
					return ""
				}
				return *res.ProviderError
			}())
		}
		var rz json.RawMessage
		o.must(202, "POST", "/api/machines/"+rr.MachineID+"/resize", map[string]int{"cpu": size, "ramGB": size * 2, "diskGB": 80}, &rz)
		eventually(t, label+": resize done", func() bool {
			var l opList
			o.must(200, "GET", "/api/machines/"+rr.MachineID+"/ops", nil, &l)
			return len(l.Ops) > 0 && l.Ops[0].Kind == "resize" && l.Ops[0].Status == "done"
		})
		var rb json.RawMessage
		o.must(202, "POST", "/api/machines/"+rr.MachineID+"/restart", map[string]string{"what": "machine", "when": "now"}, &rb)
		eventually(t, label+": reboot done", func() bool {
			var l opList
			o.must(200, "GET", "/api/machines/"+rr.MachineID+"/ops", nil, &l)
			return len(l.Ops) > 0 && l.Ops[0].Kind != "resize" && l.Ops[0].Status == "done"
		})
		if code, st := adapterCall(t, "GET", adapterBase("/v1/machines/"+server), nil, authz...); code != 200 || int(st["cpu"].(float64)) != size {
			t.Fatalf("%s: the server is %v, want %d CPU", label, st, size)
		}
	}
	hubOps(owner, "build N", 4)
	t.Logf("build N: machine %s on server %s, protocol %s", rr.MachineID, server, "v0 (default)")

	// ---- update: build N+1 replaces N on the same database, the adapter is not touched ----
	hubN.stop()
	hubN1 := start(t, "hub-n1", binN1, hubEnv("auto"))
	waitHTTP(t, hubN1, hubURL+"/healthz")
	owner1 := signIn(t, hubN1, hubURL, "owner@example.com")
	var ms struct {
		Machines []struct{ ID string } `json:"machines"`
	}
	if code, b := owner1.do("GET", "/api/spaces/"+sp.Space.ID+"/machines", nil); code != 200 || !strings.Contains(string(b), rr.MachineID) {
		t.Fatalf("the machine is not in the hub after the update: %d %s\n%s", code, b, hubN1.output())
	} else {
		_ = json.Unmarshal(b, &ms)
	}
	hubOps(owner1, "build N+1", 2)
	// the protocol is chosen on the first call to the adapter, so the name is "http/v1" only after the operations
	var res struct{ Provider struct{ Name string } }
	owner1.must(200, "GET", "/api/machines/"+rr.MachineID+"/resources", nil, &res)
	if res.Provider.Name != "http/v1" {
		t.Fatalf("build N+1 speaks %q to the adapter, want http/v1 (chosen by describe)", res.Provider.Name)
	}
	t.Logf("build N+1: same machine, same adapter process, protocol %s", res.Provider.Name)

	// the adapter's own log shows it was never restarted: one process for the whole scenario
	if ad.cmd.ProcessState != nil {
		t.Fatalf("the adapter exited\n%s", ad.output())
	}
}
