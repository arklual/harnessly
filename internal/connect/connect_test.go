package connect

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type rig struct {
	srv    *Server
	ts     *httptest.Server
	dev    *Device
	cancel context.CancelFunc
}

func newRig(t *testing.T) *rig {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash tests")
	}
	srv := &Server{Dir: t.TempDir(), Logf: t.Logf}
	mux := http.NewServeMux()
	mux.HandleFunc("/connect/v1/register", srv.ServeRegister)
	mux.HandleFunc("/connect/v1/dial", srv.ServeDial)
	mux.HandleFunc("/api/cli/connect/fs", srv.ServeFS)
	mux.HandleFunc("/api/cli/connect/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-cli-token") != "tok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		srv.ServeExec(w, r)
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	code, _ := srv.IssueCode()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	body, _ := json.Marshal(registerReq{Code: code, Name: "laptop", OS: runtime.GOOS, Pub: pub})
	resp, err := http.Post(ts.URL+"/connect/v1/register", "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("register: %v %v", err, resp.Status)
	}
	var out struct{ ID string }
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()

	// A code is good once.
	resp, _ = http.Post(ts.URL+"/connect/v1/register", "application/json", bytes.NewReader(body))
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("second use of a code: %s", resp.Status)
	}

	dev := &Device{Cfg: DeviceConfig{URL: ts.URL, ID: out.ID, Name: "laptop", Key: priv}, Logf: t.Logf}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go dev.Run(ctx)
	r := &rig{srv: srv, ts: ts, dev: dev, cancel: cancel}
	r.waitOnline(t, true)
	return r
}

func (r *rig) waitOnline(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if l := r.srv.Devices(); len(l) == 1 && l[0].Online == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("device online != %v", want)
}

func (r *rig) bash(t *testing.T, cwd string, environ []string, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := RunBash(args, BashEnv{
		Getenv:  func(k string) string { return map[string]string{DeviceEnv: "laptop"}[k] },
		Environ: environ, Cwd: cwd, Stdout: &out, Stderr: &errb,
		Endpoint: func() (string, string, string, error) { return r.ts.URL, "tok", "x-cli-token", nil },
		Fallback: func([]string) int { t.Fatal("fallback"); return 0 },
	})
	return out.String(), errb.String(), code
}

func TestExecOutputAndCode(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	out, errOut, code := r.bash(t, dir, []string{"CLAUDECODE=1", "HOME=/nope", "SECRET=x"},
		"-c", `echo "out $CLAUDECODE $SECRET"; echo err >&2; pwd; exit 3`)
	if code != 3 || errOut != "err\n" {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if out != "out 1 \n"+dir+"\n" && out != "out 1 \n"+real+"\n" {
		t.Fatalf("stdout %q", out)
	}
}

func TestMissingCwd(t *testing.T) {
	r := newRig(t)
	_, errOut, code := r.bash(t, "/no/such/dir", nil, "-c", "true")
	if code != 1 || !strings.Contains(errOut, "нет папки") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

// The agent's snapshot goes to the device and its cwd file comes back, as Claude Code's Bash tool needs them.
func TestMirroredFiles(t *testing.T) {
	r := newRig(t)
	here := t.TempDir()
	snap := filepath.Join(here, "shell-snapshots", "snapshot-bash-1-abc.sh")
	_ = os.MkdirAll(filepath.Dir(snap), 0o700)
	_ = os.WriteFile(snap, []byte("greet() { echo hello-from-snapshot; }\n"), 0o600)
	cwdFile := filepath.Join(here, "claude-58a8-cwd")
	work := t.TempDir()
	cmd := "source " + snap + " && eval 'greet; cd ..' < /dev/null && pwd -P >| " + cwdFile
	out, errOut, code := r.bash(t, work, nil, "-c", cmd)
	if code != 0 || out != "hello-from-snapshot\n" {
		t.Fatalf("code %d out %q err %q", code, out, errOut)
	}
	got, err := os.ReadFile(cwdFile)
	parent, _ := filepath.EvalSymlinks(filepath.Dir(work))
	if err != nil || strings.TrimSpace(string(got)) != parent {
		t.Fatalf("cwd file %q (%v), want %q", got, err, parent)
	}

	// Building the snapshot: the file the device writes comes back here.
	built := filepath.Join(here, "shell-snapshots", "snapshot-bash-2-def.sh")
	_, _, code = r.bash(t, work, nil, "-c", "-l", "SNAPSHOT_FILE="+built+"\necho '# Snapshot file' >| \"$SNAPSHOT_FILE\"")
	if raw, _ := os.ReadFile(built); code != 0 || string(raw) != "# Snapshot file\n" {
		t.Fatalf("built snapshot %q code %d", raw, code)
	}
}

// A link that drops mid-command comes back and the output arrives whole, once, in order.
func TestSurvivesLinkDrop(t *testing.T) {
	r := newRig(t)
	done := make(chan struct{})
	var out string
	var code int
	go func() {
		out, _, code = r.bash(t, t.TempDir(), nil, "-c", `for i in 1 2 3 4 5 6 7 8; do echo line$i; sleep 0.25; done`)
		close(done)
	}()
	time.Sleep(600 * time.Millisecond)
	if l := r.dev.link.Load(); l != nil {
		l.ws.CloseNow()
	} else {
		t.Fatal("no link")
	}
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("command never finished")
	}
	want := ""
	for i := 1; i <= 8; i++ {
		want += "line" + string(rune('0'+i)) + "\n"
	}
	if code != 0 || out != want {
		t.Fatalf("code %d out %q", code, out)
	}
}

func TestOfflineFailsFast(t *testing.T) {
	r := newRig(t)
	r.cancel()
	r.waitOnline(t, false)
	start := time.Now()
	_, errOut, code := r.bash(t, t.TempDir(), nil, "-c", "true")
	if code != 1 || !strings.Contains(errOut, "офлайн") || time.Since(start) > 3*time.Second {
		t.Fatalf("code %d err %q in %s", code, errOut, time.Since(start))
	}
}

func TestKillAll(t *testing.T) {
	r := newRig(t)
	done := make(chan int)
	go func() {
		_, _, code := r.bash(t, t.TempDir(), nil, "-c", "sleep 30")
		done <- code
	}()
	deadline := time.Now().Add(5 * time.Second)
	for r.dev.Running() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := r.dev.KillAll(); n != 1 {
		t.Fatalf("killed %d", n)
	}
	select {
	case code := <-done:
		if code == 0 {
			t.Fatal("killed command exited 0")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wrapper hangs after KillAll")
	}
}

func TestFSOps(t *testing.T) {
	r := newRig(t)
	dir := t.TempDir()
	ctx := context.Background()
	do := func(req FSReq) *FSResp { return r.srv.FS(ctx, "laptop", &req) }
	if e := do(FSReq{Op: "create", Path: dir + "/a.txt", Mode: 0o644, Excl: true}).Errno; e != "" {
		t.Fatal(e)
	}
	if e := do(FSReq{Op: "create", Path: dir + "/a.txt", Mode: 0o644, Excl: true}).Errno; e != "EEXIST" {
		t.Fatalf("excl create: %q", e)
	}
	if w := do(FSReq{Op: "write", Path: dir + "/a.txt", Data: []byte("hello world")}); w.N != 11 {
		t.Fatalf("write %+v", w)
	}
	if rd := do(FSReq{Op: "read", Path: dir + "/a.txt", Off: 6, Size: 100}); string(rd.Data) != "world" {
		t.Fatalf("read %q", rd.Data)
	}
	if st := do(FSReq{Op: "stat", Path: dir + "/a.txt"}).Stat; st == nil || st.Size != 11 || st.Mode&SIFMT != SIFREG {
		t.Fatalf("stat %+v", st)
	}
	_ = do(FSReq{Op: "mkdir", Path: dir + "/sub", Mode: 0o755})
	_ = do(FSReq{Op: "rename", Path: dir + "/a.txt", To: dir + "/sub/b.txt"})
	ls := do(FSReq{Op: "readdir", Path: dir + "/sub"})
	if len(ls.Entries) != 1 || ls.Entries[0].Name != "b.txt" {
		t.Fatalf("readdir %+v", ls)
	}
	if e := do(FSReq{Op: "rmdir", Path: dir + "/sub"}).Errno; e != "ENOTEMPTY" {
		t.Fatalf("rmdir non-empty: %q", e)
	}
	if e := do(FSReq{Op: "stat", Path: dir + "/nope"}).Errno; e != "ENOENT" {
		t.Fatalf("missing: %q", e)
	}
	r.cancel()
	r.waitOnline(t, false)
	if e := do(FSReq{Op: "stat", Path: dir}).Errno; e != "ENOTCONN" {
		t.Fatalf("offline: %q", e)
	}
}
