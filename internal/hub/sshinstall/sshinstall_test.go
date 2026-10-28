package sshinstall_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/ly-solutions/harnessly/internal/hub/api"
	"github.com/ly-solutions/harnessly/internal/hub/hubtest"
	"github.com/ly-solutions/harnessly/internal/hub/sshinstall"
	"github.com/ly-solutions/harnessly/internal/hubproto"
)

const healthyFacts = "os=Linux\narch=x86_64\nuid=0\ncurl=1\nsudo=1\nfree_kb=419430400\nmem_kb=67108864\n"

// fakeFingerprint has the shape of a SHA256 host key fingerprint.
const fakeFingerprint = "SHA256:0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"

var tokenRe = regexp.MustCompile(`--token '(hx_[a-z2-7]+)'`)

// setup is a hub with an owner and a space, and an SSH server the hub reaches through its dialer.
type setup struct {
	s       *hubtest.Stack
	owner   *hubtest.Person
	spaceID string
	srv     *fakeServer
	stream  *hubtest.Stream
	dialed  chan string
}

// newSetup starts the hub and then the server, which must trust the hub's key; handler answers its commands.
func newSetup(t *testing.T, authorize bool, handler func(cmd string) (string, int), opts ...hubtest.Option) *setup {
	t.Helper()
	return newSetupWith(t, authorize, handler, nil, opts...)
}

// newSetupWith is newSetup with a last say on the hub's options (a test that wants the real network dialer clears
// SSHDial).
func newSetupWith(t *testing.T, authorize bool, handler func(cmd string) (string, int), tweak func(*api.Options), opts ...hubtest.Option) *setup {
	t.Helper()
	su := &setup{dialed: make(chan string, 8)}
	var serverAddr string
	opts = append(opts, hubtest.WithOptions(func(o *api.Options) {
		o.SSHOnlineWait = 2 * time.Second
		o.SSHDial = func(ctx context.Context, network, addr string) (net.Conn, error) {
			select {
			case su.dialed <- addr:
			default:
			}
			var d net.Dialer
			return d.DialContext(ctx, network, serverAddr)
		}
		if tweak != nil {
			tweak(o)
		}
	}))
	su.s = hubtest.New(t, opts...)
	su.owner = su.s.SignIn("owner@example.com")
	su.spaceID, _ = su.owner.NewSpace("Команда")
	su.stream = su.owner.Events(su.spaceID)

	key := su.publicKey(su.owner, su.spaceID)
	if !authorize {
		key = otherKey(t)
	}
	su.srv = startServer(t, key, handler)
	serverAddr = su.srv.addr()
	return su
}

// publicKey is the key the owner is shown for a space.
func (su *setup) publicKey(p *hubtest.Person, spaceID string) string {
	su.s.T.Helper()
	r := p.Do("GET", "/api/spaces/"+spaceID+"/machines/ssh", nil)
	var out struct{ PublicKey string }
	if r.Code != 200 {
		su.s.T.Fatalf("public key: %s", r)
	}
	r.JSON(&out)
	return out.PublicKey
}

func splitAddr(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	return host, n
}

// otherKey is an authorized_keys line of a key that is not the hub's.
func otherKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return string(ssh.MarshalAuthorizedKey(sp))
}

func (su *setup) start(body map[string]any) hubtest.Resp {
	return su.owner.Do("POST", "/api/spaces/"+su.spaceID+"/machines/ssh", body)
}

func (su *setup) startOK(profile string) string {
	su.srvT().Helper()
	r := su.start(map[string]any{"host": "10.0.0.7", "user": "root", "profile": profile, "confirmFull": profile == "full", "hostKeyFingerprint": su.srv.fingerprint()})
	if r.Code != http.StatusAccepted {
		su.srvT().Fatalf("start: %s", r)
	}
	var out struct{ JobID string }
	r.JSON(&out)
	return out.JobID
}

func (su *setup) srvT() *testing.T { return su.s.T }

type jobView struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	State string `json:"state"`
	Steps []struct {
		ID     string  `json:"id"`
		State  string  `json:"state"`
		Detail *string `json:"detail"`
	} `json:"steps"`
	Error *struct{ Code, Message string } `json:"error"`
}

func (j jobView) step(id string) (state, detail string) {
	for _, s := range j.Steps {
		if s.ID == id {
			if s.Detail != nil {
				detail = *s.Detail
			}
			return s.State, detail
		}
	}
	return "", ""
}

// finished waits for the job to end and returns the last view and the machine id the event carried.
func (su *setup) finished(jobID string) (jobView, string) {
	su.s.T.Helper()
	ev := su.stream.Await("the end of job "+jobID, func(e hubtest.Event) bool {
		if e.Name != "job" {
			return false
		}
		var d struct{ Job jobView }
		return json.Unmarshal(e.Data, &d) == nil && d.Job.ID == jobID && d.Job.State != "running"
	})
	var d struct {
		MachineID string
		Job       jobView
	}
	if err := json.Unmarshal(ev.Data, &d); err != nil {
		su.s.T.Fatal(err)
	}
	return d.Job, d.MachineID
}

// installingHandler answers like a healthy server whose installer registers the machine and starts its agent.
func installingHandler(su func() *setup, connect bool) func(string) (string, int) {
	return func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return healthyFacts, 0
		}
		m := tokenRe.FindStringSubmatch(cmd)
		if m == nil {
			return "no token in the command", 1
		}
		s := su().s
		a := s.NewAgent(nil)
		if r := a.Register(m[1], hubproto.ProfileDir, hubproto.ViaCommand); r.Code != http.StatusCreated {
			return "register: " + r.String(), 1
		}
		if connect {
			go func() {
				time.Sleep(300 * time.Millisecond)
				a.TryConnect()
			}()
		}
		return "installed\n", 0
	}
}

func TestInstallOverSSH(t *testing.T) {
	var su *setup
	su = newSetup(t, true, installingHandler(func() *setup { return su }, true))
	jobID := su.startOK("dir")
	job, machineID := su.finished(jobID)

	if job.State != "done" || job.Kind != "ssh-install" || job.Error != nil {
		t.Fatalf("job = %+v", job)
	}
	for _, id := range []string{"ssh", "preflight", "install", "online"} {
		if st, _ := job.step(id); st != "ok" {
			t.Errorf("step %s = %q", id, st)
		}
	}
	if _, fp := job.step("ssh"); fp != su.srv.fingerprint() {
		t.Errorf("ssh detail = %q, want the host key fingerprint %q", fp, su.srv.fingerprint())
	}
	if _, d := job.step("preflight"); d != "400 ГБ свободно · 64 ГБ памяти" {
		t.Errorf("preflight detail = %q", d)
	}
	if machineID == "" {
		t.Fatal("the last event does not name the machine")
	}
	select {
	case addr := <-su.dialed:
		if addr != "10.0.0.7:22" {
			t.Errorf("dialed %q, want the port to default to 22", addr)
		}
	default:
		t.Error("the hub never dialed")
	}

	var got struct {
		Machine struct{ Via, Status, Profile string }
	}
	su.owner.Do("GET", "/api/machines/"+machineID, nil).JSON(&got)
	if got.Machine.Via != "ssh" || got.Machine.Status != "online" || got.Machine.Profile != "dir" {
		t.Errorf("machine = %+v", got.Machine)
	}

	cmds := su.srv.commands()
	if len(cmds) != 2 {
		t.Fatalf("the server received %d commands, want a look and an install: %q", len(cmds), cmds)
	}
	install := cmds[1]
	for _, want := range []string{"curl -fsSL '" + su.s.URL + "/install'", "--profile 'dir'", "XDG_RUNTIME_DIR"} {
		if !strings.Contains(install, want) {
			t.Errorf("install command lacks %q:\n%s", want, install)
		}
	}
	if strings.Contains(install, "sudo") {
		t.Errorf("root does not need sudo:\n%s", install)
	}
	if code := tokenRe.FindStringSubmatch(install); code != nil && strings.Contains(su.s.Logs.String(), code[1]) {
		t.Error("the registration code reached the hub log")
	}
}

func TestInstallUsesSudoOutsideRoot(t *testing.T) {
	var su *setup
	inner := installingHandler(func() *setup { return su }, true)
	su = newSetup(t, true, func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return strings.Replace(healthyFacts, "uid=0", "uid=1000", 1), 0
		}
		return inner(cmd)
	})
	job, _ := su.finished(su.startOK("dir"))
	if job.State != "done" {
		t.Fatalf("job = %+v", job)
	}
	if install := su.srv.commands()[1]; !strings.Contains(install, "sudo -n sh") {
		t.Errorf("a normal user installs through sudo:\n%s", install)
	}
}

func TestFullProfileNeedsNeitherRootNorSudo(t *testing.T) {
	facts := "os=Linux\narch=aarch64\nuid=1000\ncurl=1\nsudo=0\nfree_kb=419430400\nmem_kb=8388608\n"
	su := newSetup(t, true, func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return facts, 0
		}
		return "installed\n", 0
	})
	job, _ := su.finished(su.startOK("full"))
	// The machine never registers in this test, but the install itself must have been tried without sudo.
	if st, _ := job.step("install"); st != "ok" {
		t.Fatalf("install = %q, job %+v", st, job)
	}
	if install := su.srv.commands()[1]; strings.Contains(install, "sudo") || !strings.Contains(install, "--profile 'full'") {
		t.Errorf("install = %s", install)
	}
}

func TestKeyNotAccepted(t *testing.T) {
	su := newSetup(t, false, func(string) (string, int) { return healthyFacts, 0 })
	job, _ := su.finished(su.startOK("dir"))
	if job.State != "failed" || job.Error == nil || job.Error.Code != "ssh_auth" {
		t.Fatalf("job = %+v err=%+v", job, job.Error)
	}
	if !strings.Contains(job.Error.Message, "authorized_keys") {
		t.Errorf("message = %q", job.Error.Message)
	}
	// The fingerprint is shown even when the key is refused: the person can compare it with the server's.
	if st, fp := job.step("ssh"); st != "fail" || fp != su.srv.fingerprint() {
		t.Errorf("ssh step = %q %q", st, fp)
	}
	if st, _ := job.step("preflight"); st != "wait" {
		t.Errorf("preflight = %q, want it untouched", st)
	}
	if len(su.srv.commands()) != 0 {
		t.Error("nothing may run on a server that refused the key")
	}
}

func TestServerNotReachable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
		text string
	}{
		// A closed port, a silent address and a name that does not resolve read the same.
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "ssh_unreachable", "Не достучались до server.example.com:2222"},
		{"timeout", context.DeadlineExceeded, "ssh_unreachable", "Не достучались до server.example.com:2222"},
		{"no such name", &net.DNSError{Err: "no such host", Name: "server.example.com", IsNotFound: true}, "ssh_unreachable", "Не достучались до server.example.com:2222"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := hubtest.New(t, hubtest.WithOptions(func(o *api.Options) {
				o.SSHDial = func(context.Context, string, string) (net.Conn, error) { return nil, c.err }
			}))
			owner := s.SignIn("owner@example.com")
			spaceID, _ := owner.NewSpace("Команда")
			stream := owner.Events(spaceID)
			r := owner.Do("POST", "/api/spaces/"+spaceID+"/machines/ssh", map[string]any{"host": "server.example.com", "port": 2222, "user": "deploy", "profile": "dir", "hostKeyFingerprint": fakeFingerprint})
			if r.Code != http.StatusAccepted {
				t.Fatal(r)
			}
			ev := stream.Await("failure", func(e hubtest.Event) bool { return e.Name == "job" && strings.Contains(string(e.Data), `"failed"`) })
			var d struct{ Job jobView }
			_ = json.Unmarshal(ev.Data, &d)
			if d.Job.Error == nil || d.Job.Error.Code != c.code || !strings.Contains(d.Job.Error.Message, c.text) {
				t.Errorf("error = %+v", d.Job.Error)
			}
		})
	}
}

func TestPreflightRefusals(t *testing.T) {
	cases := []struct {
		name    string
		facts   string
		profile string
		want    string
	}{
		{"low disk", "os=Linux\narch=x86_64\nuid=0\ncurl=1\nsudo=1\nfree_kb=1048576\nmem_kb=8388608\n", "dir", "нужно хотя бы 3 ГБ"},
		{"low memory", "os=Linux\narch=x86_64\nuid=0\ncurl=1\nsudo=1\nfree_kb=419430400\nmem_kb=1048576\n", "dir", "нужно хотя бы 2 ГБ"},
		{"no curl", "os=Linux\narch=x86_64\nuid=0\ncurl=0\nsudo=1\nfree_kb=419430400\nmem_kb=8388608\n", "dir", "нет curl"},
		{"no root", "os=Linux\narch=x86_64\nuid=1000\ncurl=1\nsudo=0\nfree_kb=419430400\nmem_kb=8388608\n", "dir", "root или sudo"},
		{"windows-ish", "os=MINGW64_NT\narch=x86_64\nuid=1000\ncurl=1\nsudo=0\nfree_kb=1\nmem_kb=1\n", "full", "Linux и macOS"},
		{"arm32", "os=Linux\narch=armv7l\nuid=0\ncurl=1\nsudo=1\nfree_kb=419430400\nmem_kb=8388608\n", "dir", "amd64 и arm64"},
		{"mac in a folder", "os=Darwin\narch=arm64\nuid=501\ncurl=1\nsudo=0\nfree_kb=419430400\nmem_bytes=17179869184\n", "dir", "macOS"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			su := newSetup(t, true, func(string) (string, int) { return c.facts, 0 })
			job, _ := su.finished(su.startOK(c.profile))
			if job.State != "failed" || job.Error == nil || job.Error.Code != "preflight" || !strings.Contains(job.Error.Message, c.want) {
				t.Fatalf("job = %+v err = %+v", job, job.Error)
			}
			if st, _ := job.step("preflight"); st != "fail" {
				t.Errorf("preflight = %q", st)
			}
			if st, _ := job.step("install"); st != "wait" {
				t.Errorf("install = %q, it must not start", st)
			}
			if len(su.srv.commands()) != 1 {
				t.Errorf("commands = %q, only the look is allowed", su.srv.commands())
			}
		})
	}
}

func TestMacOSWithTheFullProfile(t *testing.T) {
	facts := "os=Darwin\narch=arm64\nuid=501\ncurl=1\nsudo=0\nmem_bytes=17179869184\nfree_kb=419430400\n"
	su := newSetup(t, true, func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return facts, 0
		}
		return "ok", 0
	})
	job, _ := su.finished(su.startOK("full"))
	if st, _ := job.step("preflight"); st != "ok" {
		t.Fatalf("preflight = %q, job %+v", st, job)
	}
}

func TestInstallerFailsWithItsOutputAndNoCode(t *testing.T) {
	var code string
	su := newSetup(t, true, func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return healthyFacts, 0
		}
		code = tokenRe.FindStringSubmatch(cmd)[1]
		return "downloading\nsha256 mismatch for agent-linux-amd64 (token " + code + ")\n", 3
	})
	job, _ := su.finished(su.startOK("dir"))
	if job.State != "failed" || job.Error == nil || job.Error.Code != "install_failed" || !strings.Contains(job.Error.Message, "код 3") {
		t.Fatalf("job = %+v err = %+v", job, job.Error)
	}
	st, detail := job.step("install")
	if st != "fail" || !strings.Contains(detail, "sha256 mismatch") {
		t.Errorf("install step = %q %q", st, detail)
	}
	if strings.Contains(detail, code) {
		t.Error("the registration code is on the page")
	}
}

func TestMachineNeverComesOnline(t *testing.T) {
	var su *setup
	su = newSetup(t, true, installingHandler(func() *setup { return su }, false))
	job, _ := su.finished(su.startOK("dir"))
	if job.State != "failed" || job.Error == nil || job.Error.Code != "not_online" || !strings.Contains(job.Error.Message, "вышел на связь") {
		t.Fatalf("job = %+v err = %+v", job, job.Error)
	}
	if st, _ := job.step("install"); st != "ok" {
		t.Errorf("install = %q", st)
	}
}

func TestInstallerThatRegistersNothing(t *testing.T) {
	su := newSetup(t, true, func(cmd string) (string, int) {
		if !strings.Contains(cmd, "--token") {
			return healthyFacts, 0
		}
		return "done\n", 0
	})
	job, _ := su.finished(su.startOK("dir"))
	if job.Error == nil || job.Error.Code != "not_online" || !strings.Contains(job.Error.Message, "не зарегистрировалась") {
		t.Fatalf("job = %+v err = %+v", job, job.Error)
	}
}

func TestStartValidation(t *testing.T) {
	su := newSetup(t, true, func(string) (string, int) { return healthyFacts, 0 })
	cases := []struct {
		name  string
		body  map[string]any
		code  int
		field string
	}{
		{"no host", map[string]any{"user": "root", "profile": "dir"}, 400, "host"},
		{"host with a space", map[string]any{"host": "a b", "user": "root", "profile": "dir"}, 400, "host"},
		{"host with a shell", map[string]any{"host": "a;rm -rf", "user": "root", "profile": "dir"}, 400, "host"},
		{"port too big", map[string]any{"host": "a.example.com", "port": 70000, "user": "root", "profile": "dir"}, 400, "port"},
		{"negative port", map[string]any{"host": "a.example.com", "port": -1, "user": "root", "profile": "dir"}, 400, "port"},
		{"user with a quote", map[string]any{"host": "a.example.com", "user": "ro'ot", "profile": "dir"}, 400, "user"},
		{"no user", map[string]any{"host": "a.example.com", "profile": "dir"}, 400, "user"},
		{"unknown profile", map[string]any{"host": "a.example.com", "user": "root", "profile": "vm"}, 400, "profile"},
		{"fingerprint of another shape", map[string]any{"host": "a.example.com", "user": "root", "profile": "dir", "hostKeyFingerprint": "MD5:aa:bb"}, 400, "hostKeyFingerprint"},
		{"full without confirmation", map[string]any{"host": "a.example.com", "user": "root", "profile": "full"}, 400, "confirmFull"},
		{"container without an image", map[string]any{"host": "a.example.com", "user": "root", "profile": "container"}, 422, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := su.start(c.body)
			if r.Code != c.code {
				t.Fatalf("status = %s", r)
			}
			if c.field != "" && r.Detail("field") != c.field {
				t.Errorf("field = %v", r.Detail("field"))
			}
		})
	}
	if n := len(su.srv.commands()); n != 0 {
		t.Errorf("%d commands ran for refused requests", n)
	}
}

func TestOnlyOwnersStartAnInstall(t *testing.T) {
	su := newSetup(t, true, func(string) (string, int) { return healthyFacts, 0 })
	member := su.s.SignIn("member@example.com")
	su.owner.Join(su.spaceID, member)
	body := map[string]any{"host": "a.example.com", "user": "root", "profile": "dir"}
	if r := member.Do("POST", "/api/spaces/"+su.spaceID+"/machines/ssh", body); r.Code != 403 {
		t.Errorf("member: %s", r)
	}
	if r := su.s.Browser().Do("POST", "/api/spaces/"+su.spaceID+"/machines/ssh", body); r.Code != 401 {
		t.Errorf("signed out: %s", r)
	}
	stranger := su.s.SignIn("stranger@example.com")
	if r := stranger.Do("POST", "/api/spaces/"+su.spaceID+"/machines/ssh", body); r.Code != 403 {
		t.Errorf("stranger: %s", r)
	}
}

func TestSwitchedOff(t *testing.T) {
	s := hubtest.New(t, hubtest.Env("HUB_SSH", "off"))
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Команда")
	r := owner.Do("POST", "/api/spaces/"+spaceID+"/machines/ssh", map[string]any{"host": "a.example.com", "user": "root", "profile": "dir"})
	if r.Code != 422 || r.Err() != "provider_off" {
		t.Errorf("start: %s", r)
	}
	if r := owner.Do("GET", "/api/spaces/"+spaceID+"/machines/ssh", nil); r.Code != 422 || r.Err() != "provider_off" {
		t.Errorf("key: %s", r)
	}
	var cfg struct {
		Connect struct {
			SSH struct {
				Available bool
				PublicKey *string
			}
		}
	}
	owner.Do("GET", "/api/config", nil).JSON(&cfg)
	if cfg.Connect.SSH.Available || cfg.Connect.SSH.PublicKey != nil {
		t.Errorf("config = %+v", cfg.Connect.SSH)
	}
}

// The key to add to a server is the space's own, for the owners of the space; the config of the hub, which anyone
// can read, has none.
func TestTheKeyToAddIsTheSpacesOwnAndForItsOwners(t *testing.T) {
	s := hubtest.New(t)
	var cfg struct {
		Connect struct {
			SSH struct {
				Available bool
				PublicKey *string
			}
		}
	}
	s.Browser().Do("GET", "/api/config", nil).JSON(&cfg)
	if !cfg.Connect.SSH.Available || cfg.Connect.SSH.PublicKey != nil {
		t.Fatalf("config = %+v: it must offer the way and no key", cfg.Connect.SSH)
	}

	owner := s.SignIn("owner@example.com")
	spaceA, _ := owner.NewSpace("Первое")
	spaceB, _ := owner.NewSpace("Второе")
	su := &setup{s: s}
	keyA, keyB := su.publicKey(owner, spaceA), su.publicKey(owner, spaceB)
	pub, comment, _, _, err := ssh.ParseAuthorizedKey([]byte(keyA))
	if err != nil || pub.Type() != "ssh-ed25519" || comment != "harnessly" {
		t.Errorf("key line %q: %v %q", keyA, err, comment)
	}
	if keyA == keyB {
		t.Error("two spaces share a key")
	}
	if again := su.publicKey(owner, spaceA); again != keyA {
		t.Error("the key of a space changed between two requests")
	}

	member := s.SignIn("member@example.com")
	owner.Join(spaceA, member)
	other := s.SignIn("other@example.com")
	for name, p := range map[string]*hubtest.Person{"a member": member, "a stranger": other} {
		r := p.Do("GET", "/api/spaces/"+spaceA+"/machines/ssh", nil)
		if r.Code != 403 || strings.Contains(string(r.Body), keyA) {
			t.Errorf("%s: %s", name, r)
		}
	}
	if r := s.Browser().Do("GET", "/api/spaces/"+spaceA+"/machines/ssh", nil); r.Code != 401 {
		t.Errorf("signed out: %s", r)
	}
	// the owner of another space sees their own key there, and cannot ask for this one
	if r := other.Do("GET", "/api/spaces/"+spaceB+"/machines/ssh", nil); r.Code != 403 {
		t.Errorf("another space's key: %s", r)
	}
}

// The reason for a key per space: an owner who authorizes their space's key on their server has not given a key that
// installs on it to the owners of any other space.
func TestAnotherSpacesOwnerCannotInstallOnMyServer(t *testing.T) {
	var su *setup
	su = newSetup(t, true, installingHandler(func() *setup { return su }, true))
	// su.srv trusts the key of the first space only. Someone else's space, on the same hub:
	intruder := su.s.SignIn("intruder@example.com")
	theirs, _ := intruder.NewSpace("Чужое")
	r := intruder.Do("POST", "/api/spaces/"+theirs+"/machines/ssh", map[string]any{"host": "10.0.0.7", "user": "root", "profile": "dir", "hostKeyFingerprint": su.srv.fingerprint()})
	if r.Code != http.StatusAccepted {
		t.Fatalf("start: %s", r)
	}
	ev := intruder.Events(theirs).Await("failure", func(e hubtest.Event) bool { return e.Name == "job" && strings.Contains(string(e.Data), `"failed"`) })
	var d struct{ Job jobView }
	_ = json.Unmarshal(ev.Data, &d)
	if d.Job.Error == nil || d.Job.Error.Code != "ssh_auth" {
		t.Fatalf("the intruder's job = %+v", d.Job)
	}
	if len(su.srv.commands()) != 0 {
		t.Errorf("commands ran for the intruder: %q", su.srv.commands())
	}
	// the rightful space installs
	if job, _ := su.finished(su.startOK("dir")); job.State != "done" {
		t.Errorf("the space's own install: %+v", job)
	}
}

func TestThePrivateKeyIsEncryptedAtRest(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Команда")
	su := &setup{s: s}
	su.publicKey(owner, spaceID)

	var enc string
	if err := s.DB.QueryRow(context.Background(), `SELECT private_key_enc FROM space_ssh_keys WHERE space_id = ?`, spaceID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		t.Fatalf("the stored value is not what the hub writes: %v", err)
	}
	for _, marker := range []string{"PRIVATE KEY", "openssh-key-v1", "ssh-ed25519"} {
		if strings.Contains(enc, marker) || strings.Contains(string(raw), marker) {
			t.Errorf("%q is in the stored private key", marker)
		}
	}
	info, err := os.Stat(filepath.Join(s.Cfg.DataDir, sshinstall.SecretKeyFile))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the hub's encryption key file: %v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(s.Cfg.DataDir, "ssh_ed25519")); err == nil {
		t.Error("the hub still makes a key of its own for all the spaces")
	}
}

// What the hub stores for a space is that space's: the row of another space does not open.
func TestAStoredKeyOfAnotherSpaceDoesNotOpen(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	a, _ := owner.NewSpace("Первое")
	b, _ := owner.NewSpace("Второе")
	su := &setup{s: s}
	keyA, keyB := su.publicKey(owner, a), su.publicKey(owner, b)
	ctx := context.Background()
	var encA string
	if err := s.DB.QueryRow(ctx, `SELECT private_key_enc FROM space_ssh_keys WHERE space_id = ?`, a).Scan(&encA); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.Exec(ctx, `UPDATE space_ssh_keys SET private_key_enc = ? WHERE space_id = ?`, encA, b); err != nil {
		t.Fatal(err)
	}
	// B's row now holds A's ciphertext: it does not decrypt for B, and B gets a pair of its own instead of A's key.
	if got := su.publicKey(owner, b); got == keyA || got == keyB {
		t.Errorf("B was given %q", got)
	}
}

func TestAKeyThatCannotBeOpenedIsReplaced(t *testing.T) {
	s := hubtest.New(t)
	owner := s.SignIn("owner@example.com")
	spaceID, _ := owner.NewSpace("Команда")
	su := &setup{s: s}
	before := su.publicKey(owner, spaceID)
	if _, err := s.DB.Exec(context.Background(), `UPDATE space_ssh_keys SET private_key_enc = ? WHERE space_id = ?`, "bm90IGEga2V5", spaceID); err != nil {
		t.Fatal(err)
	}
	after := su.publicKey(owner, spaceID)
	if after == before {
		t.Error("the unreadable key was kept")
	}
	if again := su.publicKey(owner, spaceID); again != after {
		t.Error("the replacement was not kept")
	}
	if !strings.Contains(s.Logs.String(), "cannot be decrypted") {
		t.Error("the replacement is not in the log")
	}
}

// The first request of an install goes as far as the handshake to show the server's key, and stops before the hub
// offers a key, runs anything or makes a registration code. The same request with the fingerprint installs.
func TestTheHostKeyIsConfirmedByTheOwner(t *testing.T) {
	var su *setup
	su = newSetup(t, true, installingHandler(func() *setup { return su }, true))
	body := map[string]any{"host": "10.0.0.7", "user": "root", "profile": "dir"}
	r := su.start(body)
	if r.Code != http.StatusAccepted {
		t.Fatalf("first request: %s", r)
	}
	var out struct{ JobID string }
	r.JSON(&out)
	job, _ := su.finished(out.JobID)
	if job.State != "failed" || job.Error == nil || job.Error.Code != "host_key_unconfirmed" {
		t.Fatalf("job = %+v err = %+v", job, job.Error)
	}
	if st, fp := job.step("ssh"); st != "fail" || fp != su.srv.fingerprint() {
		t.Errorf("ssh step = %q %q, want the server's fingerprint %q", st, fp, su.srv.fingerprint())
	}
	if !strings.Contains(job.Error.Message, su.srv.fingerprint()) {
		t.Errorf("the message does not carry the fingerprint: %q", job.Error.Message)
	}
	if su.srv.authAttempts() != 0 || len(su.srv.commands()) != 0 {
		t.Errorf("the hub offered a key (%d) or ran commands (%q) on a server nobody confirmed", su.srv.authAttempts(), su.srv.commands())
	}
	if n, _ := su.s.DB.S().CountActiveRegCodes(context.Background(), su.spaceID, su.s.Clock.Now()); n != 0 {
		t.Errorf("%d registration codes were made for a look at a host key", n)
	}

	body["hostKeyFingerprint"] = su.srv.fingerprint()
	r = su.start(body)
	if r.Code != http.StatusAccepted {
		t.Fatalf("confirmed request: %s", r)
	}
	r.JSON(&out)
	if job, _ := su.finished(out.JobID); job.State != "done" {
		t.Errorf("confirmed job = %+v err = %+v", job, job.Error)
	}
}

func TestAnotherHostKeyThanConfirmedIsRefused(t *testing.T) {
	su := newSetup(t, true, func(string) (string, int) { return healthyFacts, 0 })
	r := su.start(map[string]any{"host": "10.0.0.7", "user": "root", "profile": "dir", "hostKeyFingerprint": fakeFingerprint})
	if r.Code != http.StatusAccepted {
		t.Fatalf("start: %s", r)
	}
	var out struct{ JobID string }
	r.JSON(&out)
	job, _ := su.finished(out.JobID)
	if job.State != "failed" || job.Error == nil || job.Error.Code != "host_key_mismatch" {
		t.Fatalf("job = %+v err = %+v", job, job.Error)
	}
	if _, fp := job.step("ssh"); fp != su.srv.fingerprint() {
		t.Errorf("the fingerprint shown is %q, want what the server presented", fp)
	}
	if su.srv.authAttempts() != 0 || len(su.srv.commands()) != 0 {
		t.Errorf("a server with another key got the hub's key (%d) or commands (%q)", su.srv.authAttempts(), su.srv.commands())
	}
}

// Without a dialer of the tests, the hub dials for real, and refuses the addresses its own side of the network owns.
func TestPrivateAddressesAreRefusedInEveryEdition(t *testing.T) {
	for _, env := range [][]hubtest.Option{
		nil,
		{hubtest.Env("HUB_EDITION", "cloud"), hubtest.Env("HUB_RUNTIME_DOMAIN", "m.example.test"), hubtest.Env("HUB_SSH_ALLOW_PRIVATE", "on")},
	} {
		s := hubtest.New(t, env...)
		owner := s.SignIn("owner@example.com")
		spaceID, _ := owner.NewSpace("Команда")
		stream := owner.Events(spaceID)
		for _, host := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1", "::ffff:127.0.0.1", "localhost"} {
			r := owner.Do("POST", "/api/spaces/"+spaceID+"/machines/ssh", map[string]any{"host": host, "user": "root", "profile": "dir", "hostKeyFingerprint": fakeFingerprint})
			if r.Code != http.StatusAccepted {
				t.Fatalf("%s: %s", host, r)
			}
			var out struct{ JobID string }
			r.JSON(&out)
			ev := stream.Await("failure of "+host, func(e hubtest.Event) bool {
				return e.Name == "job" && strings.Contains(string(e.Data), out.JobID) && strings.Contains(string(e.Data), `"failed"`)
			})
			var d struct{ Job jobView }
			_ = json.Unmarshal(ev.Data, &d)
			if d.Job.Error == nil || d.Job.Error.Code != "ssh_blocked" {
				t.Errorf("%s: %+v", host, d.Job.Error)
			}
		}
	}
}

// A self-hosted hub with servers on its LAN says so, and then reaches them.
func TestPrivateAddressesCanBeAllowedWhenSelfHosted(t *testing.T) {
	var su *setup
	su = newSetupWith(t, true, installingHandler(func() *setup { return su }, true), func(o *api.Options) { o.SSHDial = nil },
		hubtest.Env("HUB_SSH_ALLOW_PRIVATE", "on"))
	srvHost, srvPort := splitAddr(t, su.srv.addr())
	r := su.start(map[string]any{"host": srvHost, "port": srvPort, "user": "root", "profile": "dir", "hostKeyFingerprint": su.srv.fingerprint()})
	if r.Code != http.StatusAccepted {
		t.Fatalf("start: %s", r)
	}
	var out struct{ JobID string }
	r.JSON(&out)
	if job, _ := su.finished(out.JobID); job.State != "done" {
		t.Errorf("job = %+v err = %+v", job, job.Error)
	}
}

func TestTheNumberOfStartsIsLimited(t *testing.T) {
	su := newSetup(t, true, func(string) (string, int) { return "os=SunOS\n", 0 })
	var last hubtest.Resp
	accepted := 0
	for tries := 0; tries < 200 && accepted < 12; tries++ {
		last = su.start(map[string]any{"host": "a.example.com", "user": "root", "profile": "dir", "hostKeyFingerprint": su.srv.fingerprint()})
		switch last.Code {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict: // all the slots are busy for a moment
			time.Sleep(20 * time.Millisecond)
		case http.StatusTooManyRequests:
			if accepted != 10 {
				t.Errorf("limited after %d starts, want 10", accepted)
			}
			if last.Header.Get("Retry-After") == "" {
				t.Error("no Retry-After")
			}
			return
		default:
			t.Fatalf("start: %s", last)
		}
	}
	t.Fatalf("never limited, last: %s", last)
}

func TestMembersDoNotSeeTheInstall(t *testing.T) {
	su := newSetup(t, true, func(string) (string, int) { return healthyFacts, 0 })
	member := su.s.SignIn("member@example.com")
	su.owner.Join(su.spaceID, member)
	ms := member.Events(su.spaceID)
	job, _ := su.finished(su.startOK("dir"))
	_ = job
	time.Sleep(50 * time.Millisecond)
	if got := ms.Named("job"); len(got) != 0 {
		t.Errorf("a member received %d job events", len(got))
	}
}
