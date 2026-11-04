package provision

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func fixture(t *testing.T) ([]byte, archive) {
	t.Helper()
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "chrome-linux64/chrome", Method: zip.Deflate}
	h.SetMode(0755)
	w, e := z.CreateHeader(h)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Write([]byte("test executable")); e != nil {
		t.Fatal(e)
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	b := buf.Bytes()
	sum := sha256.Sum256(b)
	return b, archive{"linux64", hex.EncodeToString(sum[:]), "chrome-linux64/chrome"}
}

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config{DataDir: t.TempDir(), GOOS: "linux", GOARCH: "amd64", Download: true,
		LookPath: func(string) (string, error) { return "", os.ErrNotExist }}
}

func TestSearchOrder(t *testing.T) {
	cfg := testConfig(t)
	paths := map[string]string{"google-chrome": "/usr/bin/google-chrome", "google-chrome-stable": "/usr/bin/google-chrome-stable",
		"chromium": "/usr/bin/chromium", "chromium-browser": "/usr/bin/chromium-browser", "chosen": "/custom/chrome"}
	cfg.LookPath = func(name string) (string, error) {
		if p := paths[name]; p != "" {
			return p, nil
		}
		return "", os.ErrNotExist
	}
	cfg.Chrome = "chosen"
	p, _, e := Find(cfg)
	if e != nil || p != "/custom/chrome" {
		t.Fatalf("override: %s %v", p, e)
	}
	cfg.Chrome = ""
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		p, _, e = Find(cfg)
		if e != nil || p != paths[name] {
			t.Fatalf("%s: %s %v", name, p, e)
		}
		delete(paths, name)
	}
	paths["chromium"] = "/snap/bin/chromium"
	a, _ := platform(cfg)
	cache := filepath.Join(cfg.DataDir, "agent-browser", "chrome", Version, a.binary)
	if e = os.MkdirAll(filepath.Dir(cache), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(cache, []byte("browser"), 0755); e != nil {
		t.Fatal(e)
	}
	p, snap, e := Find(cfg)
	if e != nil || p != cache || snap != "/snap/bin/chromium" {
		t.Fatalf("cache before snap: %s %s %v", p, snap, e)
	}
}

func TestParallelDownloadAndProgress(t *testing.T) {
	b, a := fixture(t)
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count.Add(1); _, _ = w.Write(b) }))
	defer server.Close()
	cfg := testConfig(t)
	cfg.archive = &a
	cfg.URL = server.URL
	var wg sync.WaitGroup
	var mu sync.Mutex
	var percents []int
	for range 8 {
		wg.Go(func() {
			path, err := Ensure(t.Context(), cfg, func(p Progress) { mu.Lock(); percents = append(percents, p.Percent); mu.Unlock() })
			if err != nil {
				t.Error(err)
			} else if !executable(path) {
				t.Error("missing executable")
			}
		})
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("downloads: %d", count.Load())
	}
	found := false
	for _, p := range percents {
		if p < 0 || p > 100 {
			t.Fatalf("progress %d", p)
		}
		if p == 95 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no completion: %v", percents)
	}
	files, _ := filepath.Glob(filepath.Join(cfg.DataDir, "agent-browser", "chrome", ".download-*"))
	if len(files) != 0 {
		t.Fatalf("staging left: %v", files)
	}
}

func TestChecksumRejectedAndRetry(t *testing.T) {
	b, a := fixture(t)
	var bad atomic.Bool
	bad.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if bad.Load() {
			_, _ = w.Write([]byte("corrupted"))
		} else {
			_, _ = w.Write(b)
		}
	}))
	defer server.Close()
	cfg := testConfig(t)
	cfg.archive = &a
	cfg.URL = server.URL
	_, err := Ensure(t.Context(), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("checksum: %v", err)
	}
	root := filepath.Join(cfg.DataDir, "agent-browser", "chrome")
	entries, _ := os.ReadDir(root)
	if len(entries) != 1 || entries[0].Name() != "download.lock" {
		t.Fatalf("unverified files remain: %v", entries)
	}
	bad.Store(false)
	if _, err = Ensure(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCanceledLock(t *testing.T) {
	cfg := testConfig(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Ensure(ctx, cfg, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestDiagnostics(t *testing.T) {
	for _, tc := range []struct{ release, want string }{
		{"ID=ubuntu\nVERSION_ID=\"24.04\"", "libasound2t64"},
		{"ID=ubuntu\nVERSION_ID=\"22.04\"", "libasound2 "},
		{"ID=debian\nVERSION_ID=\"12\"", "libasound2"},
		{"ID=fedora", "dnf"}, {"ID=alpine", "apk"}, {"ID=arch", "pacman"},
	} {
		got := LibrariesCommand(tc.release)
		if !strings.Contains(got+" ", tc.want) {
			t.Errorf("%s: %s", tc.release, got)
		}
	}
	help := SandboxHelp("/opt/browser/chrome")
	for _, want := range []string{"/opt/browser/chrome", "apparmor_parser", "4755", "NoNewPrivileges", "WARNING",
		"Settings > Browser", "system Google Chrome"} {
		if !strings.Contains(help, want) {
			t.Errorf("missing %s", want)
		}
	}
	cfg := testConfig(t)
	cfg.GOARCH = "riscv64"
	_, err := platform(cfg)
	if err == nil || !strings.Contains(err.Error(), "no pinned") || !strings.Contains(err.Error(), "chromium") {
		t.Fatal(err)
	}
}

func TestArchivePathsAndFrameworkLinks(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		mode         os.FileMode
		ok           bool
	}{
		{"../escape", "bad", 0644, false}, {"/escape", "bad", 0644, false},
		{"framework/Current", "../../outside", os.ModeSymlink | 0777, false},
		{"framework/Current", "Versions/157", os.ModeSymlink | 0777, true},
	} {
		t.Run(fmt.Sprint(tc.name, tc.target), func(t *testing.T) {
			f, e := os.CreateTemp(t.TempDir(), "archive")
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			z := zip.NewWriter(f)
			h := &zip.FileHeader{Name: tc.name}
			h.SetMode(tc.mode)
			w, e := z.CreateHeader(h)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = w.Write([]byte(tc.target)); e != nil {
				t.Fatal(e)
			}
			if e = z.Close(); e != nil {
				t.Fatal(e)
			}
			st, _ := f.Stat()
			e = unpack(f, st.Size(), filepath.Join(t.TempDir(), "out"))
			if (e == nil) != tc.ok {
				t.Fatalf("unpack: %v", e)
			}
		})
	}
}

func TestDownloadDisabledStillUsesCache(t *testing.T) {
	cfg := testConfig(t)
	cfg.Download = false
	_, err := Ensure(t.Context(), cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "DOWNLOAD=off") {
		t.Fatalf("offline: %v", err)
	}
	a, _ := platform(cfg)
	path := filepath.Join(cfg.DataDir, "agent-browser", "chrome", Version, a.binary)
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("cached"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := Ensure(t.Context(), cfg, nil)
	if err != nil || got != path {
		t.Fatalf("cache unavailable offline: %s %v", got, err)
	}
}

func TestSnapWrapperAndDisplayMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "chromium-browser")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexec /snap/bin/chromium \"$@\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if !isSnap(path) {
		t.Fatal("snap wrapper treated as a native browser")
	}
	t.Setenv("PATH", t.TempDir())
	if Mode("auto") != "headless" {
		t.Fatal("missing Xvfb must select headless")
	}
}

func TestAbandonedDownloadRemoved(t *testing.T) {
	b, a := fixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(b) }))
	defer server.Close()
	cfg := testConfig(t)
	cfg.archive = &a
	cfg.URL = server.URL
	stale := filepath.Join(cfg.DataDir, "agent-browser", "chrome", ".download-abandoned")
	if err := os.MkdirAll(stale, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stale, "chrome.zip"), []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Ensure(t.Context(), cfg, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale archive remains: %v", err)
	}
}

func TestMissingLibrariesDoesNotClaimSandboxReady(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "ldd"), []byte("#!/bin/sh\necho 'libfixture.so => not found'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("RUNTIME_BROWSER_MODE", "")
	info, err := Probe(t.Context(), "/missing/chrome", t.TempDir())
	var diagnostic *Diagnostic
	if !errors.As(err, &diagnostic) || diagnostic.Command == "" || info.Sandbox != "not yet checked" {
		t.Fatalf("info=%+v error=%v", info, err)
	}
}

func TestDownloadSupportReportsRepairWithoutNetwork(t *testing.T) {
	cfg := testConfig(t)
	if err := DownloadSupport(cfg); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"riscv64", "amd64"} {
		cfg.GOARCH = arch
		cfg.Download = arch == "riscv64"
		var diagnostic *Diagnostic
		if err := DownloadSupport(cfg); !errors.As(err, &diagnostic) || diagnostic.Command == "" {
			t.Fatalf("%s: %v", arch, err)
		}
	}
}

func TestLinuxArm64Pin(t *testing.T) {
	cfg := testConfig(t)
	cfg.GOARCH = "arm64"
	a, err := platform(cfg)
	if err != nil || a.platform != "linux-arm64" || a.binary != "chrome-linux-arm64/chrome" ||
		a.sha256 != "5517510e61657ff43ee9ab804ecd1a499b3d5bd3caf72f097516bd6565ba5a17" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSandboxRepairCommand(t *testing.T) {
	for _, tc := range []struct{ path, want string }{
		{"/srv/data/agent-browser/chrome/123.0.1.2/chrome-linux64/chrome", "/srv/data/agent-browser/chrome/*/chrome-linux64/chrome"},
		{"/srv/data/agent-browser/chrome/123.0.1.2/chrome-linux-arm64/chrome", "/srv/data/agent-browser/chrome/*/chrome-linux-arm64/chrome"},
		{"/opt/custom/chrome", "/opt/custom/chrome"},
	} {
		if got := sandboxCommand(tc.path); !strings.Contains(got, tc.want) || !strings.Contains(got, "-f /etc/apparmor.d/abi/$v") {
			t.Fatal(got)
		}
	}
	help := SandboxHelp("/opt/browser/chrome")
	if strings.Index(help, "AppArmor profile") > strings.Index(help, "system Google Chrome") ||
		!strings.Contains(help, "outside harnessly (normal SSH or local terminal)") || !strings.Contains(help, "DOES NOT work") {
		t.Fatal(help)
	}
	debURL := "https://dl.google.com/linux/direct/google-chrome-stable_current_amd64.deb"
	if got := installCommand("ID=ubuntu", "amd64"); !strings.Contains(got, debURL) || strings.Contains(got, "chromium-browser") {
		t.Fatal(got)
	}
	if got := installCommand("ID=ubuntu", "arm64"); !strings.Contains(got, "chromium-browser") {
		t.Fatal(got)
	}
}

func TestSandboxCommandAppArmorSyntax(t *testing.T) {
	parser, err := exec.LookPath("apparmor_parser")
	if err != nil {
		t.Skip("AppArmor parser is not installed")
	}
	for _, path := range []string{
		"/srv/browser [data]/agent-browser/chrome/123.0.0.1/chrome-linux64/chrome",
		"/srv/browser/agent-browser/chrome/123.0.0.1/chrome-linux-arm64/chrome",
		"/opt/custom browser/chrome",
	} {
		file := filepath.Join(t.TempDir(), "profile")
		command := sandboxCommand(path)
		command = strings.ReplaceAll(command, "/etc/apparmor.d/harnessly-browser", shellQuote(file))
		command = strings.ReplaceAll(command, "sudo tee ", "cat > ")
		command = strings.ReplaceAll(command, "sudo apparmor_parser -r", shellQuote(parser)+" -QK")
		if strings.Contains(command, "sudo") {
			t.Fatal("unsafe syntax check", command)
		}
		out, e := exec.Command("sh", "-c", command).CombinedOutput()
		if e != nil {
			t.Fatalf("syntax: %v\n%s", e, out)
		}
	}
}
