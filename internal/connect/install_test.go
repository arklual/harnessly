package connect

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestInstallCommands(t *testing.T) {
	u, w := InstallCommands("https://h.example.com/", "hx_abcdefghijklmnopqrstuvwx")
	if u != "curl -fsSL https://h.example.com/connect/install/hx_abcdefghijklmnopqrstuvwx | sh" ||
		w != "irm https://h.example.com/connect/install/hx_abcdefghijklmnopqrstuvwx.ps1 | iex" {
		t.Fatalf("%q %q", u, w)
	}
	for _, bad := range []string{"", "https://h.example.com/'; rm -rf ~", "ftp://x", "https://a b"} {
		if u, _ := InstallCommands(bad, "hx_abcdefghijklmnopqrstuvwx"); u != "" {
			t.Fatalf("%q gave %q", bad, u)
		}
	}
}

func TestPublicBase(t *testing.T) {
	s := &Server{}
	r := httptest.NewRequest("GET", "http://127.0.0.1:8022/x", nil)
	if b := s.PublicBase(r); b != "" {
		t.Fatalf("loopback: %q", b)
	}
	r = httptest.NewRequest("GET", "http://127.0.0.1:8022/x", nil)
	r.Header.Set("X-Forwarded-Host", "harnessly.example.com")
	r.Header.Set("X-Forwarded-Proto", "https")
	if b := s.PublicBase(r); b != "https://harnessly.example.com" {
		t.Fatalf("proxied: %q", b)
	}
	s.URL = "https://set.example.com/"
	if b := s.PublicBase(r); b != "https://set.example.com" {
		t.Fatalf("set: %q", b)
	}
}

func TestInstallScriptRefusesBadCode(t *testing.T) {
	s := &Server{Dir: t.TempDir(), URL: "https://h.example.com", Download: "https://dl.example.com"}
	for _, p := range []string{"/connect/install/hx_aaaaaaaaaaaaaaaaaaaaaaaa", "/connect/install/x';id;'.ps1"} {
		rec := httptest.NewRecorder()
		s.ServeInstall(rec, httptest.NewRequest("GET", p, nil))
		if !strings.Contains(rec.Body.String(), "код") || strings.Contains(rec.Body.String(), "curl") {
			t.Fatalf("%s: %s", p, rec.Body.String())
		}
	}
	code, _ := s.IssueCode()
	rec := httptest.NewRecorder()
	s.ServeInstall(rec, httptest.NewRequest("GET", "/connect/install/"+code+".ps1", nil))
	if b := rec.Body.String(); !strings.Contains(b, "$CODE = '"+code+"'") || !strings.Contains(b, "$DL = 'https://dl.example.com'") {
		t.Fatalf("ps1: %s", b)
	}
}

// The shell installer, run for real on this machine: it downloads the archive, checks it, installs Connect into the
// home folder, pairs it with the code and turns autostart on; the installed Connect then comes online.
func TestInstallScriptPairs(t *testing.T) {
	if runtime.GOOS == "windows" || testing.Short() {
		t.Skip("sh installer")
	}
	work := t.TempDir()
	bin := filepath.Join(work, "connect")
	build := exec.Command("go", "build", "-o", bin, "../../cmd/connect")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	dl := filepath.Join(work, "dl")
	_ = os.MkdirAll(dl, 0o755)
	asset := "harnessly-connect_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	f, _ := os.Create(filepath.Join(dl, asset))
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	raw, _ := os.ReadFile(bin)
	_ = tw.WriteHeader(&tar.Header{Name: "./connect", Mode: 0o755, Size: int64(len(raw)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(raw)
	_ = tw.Close()
	_ = gz.Close()
	_ = f.Close()
	arc, _ := os.ReadFile(filepath.Join(dl, asset))
	sum := sha256.Sum256(arc)
	_ = os.WriteFile(filepath.Join(dl, "checksums.txt"), []byte(hex.EncodeToString(sum[:])+"  "+asset+"\n"), 0o644)

	srv := &Server{Dir: t.TempDir(), Logf: t.Logf}
	mux := http.NewServeMux()
	mux.HandleFunc("/connect/install/", srv.ServeInstall)
	mux.HandleFunc("/connect/v1/register", srv.ServeRegister)
	mux.HandleFunc("/connect/v1/dial", srv.ServeDial)
	mux.Handle("/dl/", http.StripPrefix("/dl/", http.FileServer(http.Dir(dl))))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	srv.URL, srv.Download = ts.URL, ts.URL+"/dl"

	// A home of its own, with systemctl and loginctl that only note what they were asked.
	home := filepath.Join(work, "home")
	fake := filepath.Join(work, "fake")
	_ = os.MkdirAll(home, 0o755)
	_ = os.MkdirAll(fake, 0o755)
	calls := filepath.Join(work, "calls")
	for _, n := range []string{"systemctl", "loginctl"} {
		_ = os.WriteFile(filepath.Join(fake, n), []byte("#!/bin/sh\necho \""+n+" $*\" >> "+calls+"\n"), 0o755)
	}
	code, _ := srv.IssueCode()
	unix, _ := InstallCommands(ts.URL, code)
	cmd := exec.Command("sh", "-c", unix)
	cmd.Env = []string{"HOME=" + home, "PATH=" + fake + ":" + os.Getenv("PATH"), "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "подключено") || !strings.Contains(string(out), "Готово") {
		t.Fatalf("install said:\n%s", out)
	}
	if d := srv.Devices(); len(d) != 1 {
		t.Fatalf("devices %+v", d)
	}
	unit, err := os.ReadFile(filepath.Join(home, ".config", "systemd", "user", "harnessly-connect.service"))
	installed := filepath.Join(home, ".local", "bin", "harnessly-connect")
	if err != nil || !strings.Contains(string(unit), installed) {
		t.Fatalf("unit %q %v", unit, err)
	}
	if c, _ := os.ReadFile(calls); !strings.Contains(string(c), "systemctl --user start harnessly-connect.service") {
		t.Fatalf("calls %s", c)
	}

	// The code is spent; the installed Connect comes online with what pairing kept.
	again, _ := InstallCommands(ts.URL, code)
	if o, err := exec.Command("sh", "-c", again).CombinedOutput(); err == nil || !strings.Contains(string(o), "истёк") {
		t.Fatalf("spent code: %v %s", err, o)
	}
	run := exec.Command(installed, "run")
	run.Env = cmd.Env
	run.Stdout, run.Stderr = io.Discard, io.Discard
	if err := run.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = run.Process.Kill(); _ = run.Wait() }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if d := srv.Devices(); len(d) == 1 && d[0].Online {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("installed Connect never came online")
}
