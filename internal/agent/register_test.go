package agent

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ly-solutions/harnessly/internal/hubproto"
)

type fakeRegistry struct {
	srv   *httptest.Server
	got   []hubproto.RegisterRequest
	reply func(w http.ResponseWriter, req hubproto.RegisterRequest)
}

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	f := &fakeRegistry{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent/register" || r.Header.Get("Content-Type") != "application/json" {
			http.NotFound(w, r)
			return
		}
		var req hubproto.RegisterRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad", 400)
			return
		}
		f.got = append(f.got, req)
		if f.reply != nil {
			f.reply(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(hubproto.RegisterResponse{
			MachineID: "abcdefghijklmnopqrst", DialURL: "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/agent/v1",
			RuntimeBasePath: "/m/abcdefghijklmnopqrst", HubURL: f.srv.URL,
		})
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestRegisterWritesTheMachineFile(t *testing.T) {
	hub := newFakeRegistry(t)
	file := filepath.Join(t.TempDir(), "data", "machine.json")
	m, err := Register(context.Background(), RegisterOptions{Hub: hub.srv.URL + "/", Code: " hx_secretcode ", Profile: hubproto.ProfileDir, MachineFile: file, Version: "1.2.3"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hub.got) != 1 {
		t.Fatalf("%d calls", len(hub.got))
	}
	req := hub.got[0]
	if req.Code != "hx_secretcode" || req.Profile != "dir" || req.Via != "command" || req.AgentVersion != "1.2.3" || req.OS == "" || req.Arch == "" || req.Hostname == "" {
		t.Fatalf("%+v", req)
	}

	// The file is private, and what the hub was told is the public half of what it holds.
	st, err := os.Stat(file)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, st)
	}
	if st, _ := os.Stat(filepath.Dir(file)); st.Mode().Perm() != 0o700 {
		t.Errorf("folder %v", st.Mode().Perm())
	}
	loaded, err := LoadMachine(file)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.MachineID != "abcdefghijklmnopqrst" || loaded.RuntimeBasePath != "/m/abcdefghijklmnopqrst" || loaded.Profile != "dir" || loaded.HubURL != hub.srv.URL || loaded.RegisteredAt == "" {
		t.Fatalf("%+v", loaded)
	}
	pub, _ := base64.StdEncoding.DecodeString(req.PublicKey)
	if !loaded.SigningKey().Public().(ed25519.PublicKey).Equal(ed25519.PublicKey(pub)) || len(pub) != 32 {
		t.Error("the hub was told another signing key than the file holds")
	}
	enc, _ := base64.StdEncoding.DecodeString(req.EncKey)
	x, err := ecdh.X25519().NewPrivateKey(loaded.SealingKey())
	if err != nil || !bytes.Equal(x.PublicKey().Bytes(), enc) {
		t.Error("the hub was told another sealing key than the file holds")
	}
	if m.MachineID != loaded.MachineID {
		t.Error("the returned file is not the written one")
	}
	// What it signs, the hub's public key verifies.
	host, _ := hubproto.DialHost(loaded.DialURL)
	hdr, err := hubproto.SignDial(loaded.SigningKey(), loaded.MachineID, mustNow(), host)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := hubproto.ParseDialHeader(hdr)
	if err != nil || hubproto.VerifyDial(ed25519.PublicKey(pub), auth, host, mustNow()) != nil {
		t.Errorf("%v", err)
	}
}

func TestRegisterRefusesToOverwriteAndForceDoes(t *testing.T) {
	hub := newFakeRegistry(t)
	file := filepath.Join(t.TempDir(), "machine.json")
	opts := RegisterOptions{Hub: hub.srv.URL, Code: "hx_1", Profile: hubproto.ProfileFull, Via: hubproto.ViaLocal, MachineFile: file}
	first, err := Register(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if hub.got[0].Via != "local" || hub.got[0].Profile != "full" {
		t.Errorf("%+v", hub.got[0])
	}
	if _, err := Register(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "already registered as abcdefghijklmnopqrst") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("%v", err)
	}
	if len(hub.got) != 1 {
		t.Error("the hub was called for a registration that was refused here")
	}
	opts.Force = true
	second, err := Register(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if first.Ed25519 == second.Ed25519 {
		t.Error("new registration, same key")
	}
}

func TestRegisterFailureLeavesNoFile(t *testing.T) {
	hub := newFakeRegistry(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "machine.json")
	hub.reply = func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"error":"expired","message":"Код регистрации истёк"}`))
	}
	_, err := Register(context.Background(), RegisterOptions{Hub: hub.srv.URL, Code: "hx_old", Profile: hubproto.ProfileDir, MachineFile: file})
	if err == nil || !strings.Contains(err.Error(), "Код регистрации истёк") || !strings.Contains(err.Error(), "410") {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Errorf("a file after a refusal: %v", err)
	}

	for name, reply := range map[string]func(w http.ResponseWriter, _ hubproto.RegisterRequest){
		"an answer that is not JSON": func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
			w.WriteHeader(201)
			_, _ = w.Write([]byte("<html>"))
		},
		"an answer without an id": func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"dialUrl":"ws://x/agent/v1"}`))
		},
		"a base path with a slash on its end": func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"machineId":"a","dialUrl":"ws://x/agent/v1","runtimeBasePath":"/m/a/","hubUrl":"http://x"}`))
		},
		"a plain-text error": func(w http.ResponseWriter, _ hubproto.RegisterRequest) {
			w.WriteHeader(502)
			_, _ = w.Write([]byte("bad gateway"))
		},
	} {
		hub.reply = reply
		if _, err := Register(context.Background(), RegisterOptions{Hub: hub.srv.URL, Code: "hx_1", Profile: hubproto.ProfileDir, MachineFile: file}); err == nil {
			t.Errorf("%s: no error", name)
		}
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Errorf("%s: a file was written", name)
		}
	}
}

func TestRegisterChecksWhatItIsGiven(t *testing.T) {
	hub := newFakeRegistry(t)
	file := filepath.Join(t.TempDir(), "machine.json")
	ok := RegisterOptions{Hub: hub.srv.URL, Code: "hx_1", Profile: hubproto.ProfileDir, MachineFile: file}
	for name, mut := range map[string]func(o *RegisterOptions){
		"no code":                 func(o *RegisterOptions) { o.Code = "  " },
		"a bad profile":           func(o *RegisterOptions) { o.Profile = "root" },
		"a bad way":               func(o *RegisterOptions) { o.Via = "telepathy" },
		"no hub":                  func(o *RegisterOptions) { o.Hub = "" },
		"a hub that is not a URL": func(o *RegisterOptions) { o.Hub = "hub.example.com" },
		"a hub with a path query": func(o *RegisterOptions) { o.Hub = "https://hub.example.com/?x=1" },
		"a file nowhere":          func(o *RegisterOptions) { o.MachineFile = "" },
	} {
		o := ok
		mut(&o)
		if _, err := Register(context.Background(), o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(hub.got) != 0 {
		t.Errorf("the hub was called %d times for requests that were wrong", len(hub.got))
	}
	// A hub nobody answers for: a clear error, no file.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	o := ok
	o.Hub = url
	if _, err := Register(context.Background(), o); err == nil || !strings.Contains(err.Error(), "could not reach the hub") {
		t.Errorf("%v", err)
	}
}

func TestLoadMachineRejectsDamage(t *testing.T) {
	hub := newFakeRegistry(t)
	file := filepath.Join(t.TempDir(), "machine.json")
	if _, err := Register(context.Background(), RegisterOptions{Hub: hub.srv.URL, Code: "hx_1", Profile: hubproto.ProfileDir, MachineFile: file}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(file)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for name, mut := range map[string]func(){
		"no id":          func() { m["machineId"] = "" },
		"a short seed":   func() { m["ed25519"] = base64.StdEncoding.EncodeToString([]byte("short")) },
		"a short x25519": func() { m["x25519"] = "AAAA" },
		"a profile":      func() { m["profile"] = "everything" },
		"a base path":    func() { m["runtimeBasePath"] = "m/x" },
	} {
		var c map[string]any
		_ = json.Unmarshal(raw, &c)
		m = c
		mut()
		b, _ := json.Marshal(m)
		bad := filepath.Join(t.TempDir(), "m.json")
		_ = os.WriteFile(bad, b, 0o600)
		if _, err := LoadMachine(bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := LoadMachine(filepath.Join(t.TempDir(), "none.json")); !os.IsNotExist(err) {
		t.Errorf("%v", err)
	}
}

func mustNow() time.Time { return time.Now() }

func TestRegisterNameReplacesTheHostName(t *testing.T) {
	hub := newFakeRegistry(t)
	dir := t.TempDir()
	opts := RegisterOptions{Hub: hub.srv.URL, Code: "hx_1", Profile: hubproto.ProfileFull, MachineFile: filepath.Join(dir, "machine.json")}
	opts.Name = "  build box 2 "
	if _, err := Register(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := hub.got[0].Hostname; got != "build box 2" {
		t.Errorf("%q", got)
	}
	for _, bad := range []string{strings.Repeat("a", 254), "a\nb", "a\x00b"} {
		opts.Name, opts.Force = bad, true
		if _, err := Register(context.Background(), opts); err == nil || !strings.Contains(err.Error(), "--name") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if len(hub.got) != 1 {
		t.Errorf("the hub was asked after a refused name: %d", len(hub.got))
	}
}
